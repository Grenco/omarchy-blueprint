package autosync

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/localstate"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/pelletier/go-toml/v2"
	"golang.org/x/sys/unix"
)

const (
	grantFile  = "auto-sync.toml"
	grantLock  = "auto-sync.lock"
	grantLimit = 256 << 10
)

var (
	ErrInvalidGrant     = errors.New("Auto Sync settings on this computer cannot be read safely; Auto Sync stays off until they are set again")
	ErrConcurrentChange = errors.New("Auto Sync settings changed while you were editing them; review them again")
	ErrApprovalRequired = errors.New("this change gives Auto Sync new permissions and must be confirmed by a person at this computer")
	ErrApprovalMismatch = errors.New("the confirmation does not match these Auto Sync permissions")
)

// Store keeps the grant in the profile-keyed private state directory next
// to the Sync cursor. It never reads from the profile checkout.
type Store struct {
	StateHome, ProfileRoot string
	Registry               Registry
}

func (s Store) dir() (string, error) {
	return machine.ProfileStateDir(s.StateHome, s.ProfileRoot)
}

func (s Store) registry() Registry {
	if s.Registry.Version == "" {
		return DefaultRegistry()
	}
	return s.Registry
}

// Load returns the stored grant, or DefaultGrant with exists=false. A
// present but unreadable, malformed, unsupported or oversized grant fails
// closed with ErrInvalidGrant; it is never treated as missing.
func (s Store) Load() (Grant, bool, error) {
	dir, err := s.dir()
	if err != nil {
		return Grant{}, false, err
	}
	// Ownership and permissions are the grant's protection: the authority
	// digest is a consistency check, not a signature.
	if err := localstate.InspectPrivateDir(s.StateHome, dir); errors.Is(err, os.ErrNotExist) {
		return DefaultGrant(), false, nil
	} else if err != nil {
		return Grant{}, false, ErrInvalidGrant
	}
	data, err := localstate.ReadPrivateFile(filepath.Join(dir, grantFile), grantLimit)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultGrant(), false, nil
	}
	if err != nil {
		return Grant{}, false, ErrInvalidGrant
	}
	var g Grant
	// TOML diagnostics can quote input; never expose file contents.
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&g); err != nil {
		return Grant{}, false, ErrInvalidGrant
	}
	if err := g.Validate(s.registry()); err != nil {
		return Grant{}, false, ErrInvalidGrant
	}
	if g.Identity.ProfileRoot != "" {
		root, err := machine.CanonicalProfileRoot(s.ProfileRoot)
		if err != nil || root != g.Identity.ProfileRoot {
			return Grant{}, false, ErrInvalidGrant
		}
	}
	return g, true, nil
}

// Save writes next if base is still the stored grant. Widening authority
// (enabling, adding scope, raising a ceiling, allowing code to run or
// privileged installs) and lifting the safety guard require a LocalApproval
// for exactly next's authority, confirmed against the stored grant. Pausing, turning off and narrowing never do.
// The grant lock is held only for this read-modify-write; it is never held
// while waiting for the profile lock.
func (s Store) Save(base, next Grant, approval *LocalApproval) (Grant, error) {
	if err := next.Validate(s.registry()); err != nil {
		return Grant{}, err
	}
	if next.Identity.ProfileRoot != "" {
		root, err := machine.CanonicalProfileRoot(s.ProfileRoot)
		if err != nil || root != next.Identity.ProfileRoot {
			return Grant{}, ErrIdentityMismatch
		}
	}
	dir, err := s.dir()
	if err != nil {
		return Grant{}, err
	}
	if err := localstate.PrivateDir(s.StateHome, dir); err != nil {
		return Grant{}, err
	}
	unlock, err := lockFile(filepath.Join(dir, grantLock))
	if err != nil {
		return Grant{}, err
	}
	defer unlock()
	current, _, err := s.Load()
	if err != nil && !errors.Is(err, ErrInvalidGrant) {
		return Grant{}, err
	}
	if err == nil && current.Generation != base.Generation {
		return Grant{}, ErrConcurrentChange
	}
	if errors.Is(err, ErrInvalidGrant) {
		// An unreadable grant can only be replaced from a clean slate,
		// which authorises nothing until confirmed again.
		current = DefaultGrant()
	}
	next.Generation = current.Generation + 1
	next.Approved = current.Approved
	if needsApproval(current, next) {
		if approval == nil {
			return Grant{}, ErrApprovalRequired
		}
		if approval.digest != next.AuthorityDigest() || approval.base != baseDigest(current) {
			return Grant{}, ErrApprovalMismatch
		}
		next.Approved = approval.digest
	} else if next.Setting == SettingOn && Narrows(current, next) {
		next.Approved = next.AuthorityDigest()
	}
	data, err := toml.Marshal(next)
	if err != nil {
		return Grant{}, err
	}
	if err := localstate.WriteFile(filepath.Join(dir, grantFile), data); err != nil {
		return Grant{}, err
	}
	return next, nil
}

func lockFile(path string) (func(), error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, errors.New("open Auto Sync settings lock failed")
	}
	file := os.NewFile(uintptr(fd), path)
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("Auto Sync settings lock is not a regular file")
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		file.Close()
		return nil, errors.New("lock Auto Sync settings failed")
	}
	return func() {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		file.Close()
	}, nil
}

var machineIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// HostIdentity derives a one-way host identifier from the systemd machine
// ID, so a grant copied to another computer does not match. The raw ID is
// never stored or shown.
func HostIdentity(read func(string) ([]byte, error)) (string, error) {
	if read == nil {
		read = os.ReadFile
	}
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		data, err := read(path)
		if err != nil {
			continue
		}
		id := strings.TrimSpace(string(data))
		if machineIDPattern.MatchString(id) {
			return fmt.Sprintf("%x", sha256.Sum256([]byte("omarchy-blueprint/auto-sync/host\x00"+id))), nil
		}
	}
	return "", errors.New("this computer's identity is unavailable, so Auto Sync cannot be bound to it")
}
