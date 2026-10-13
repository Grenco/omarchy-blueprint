package profile

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"sort"
	"strings"
	"syscall"
)

// A profile is untrusted input: it is usually a Git repository, and Git can
// commit symlinks. Every read and write of profile metadata therefore goes
// through an os.Root opened at the profile directory, which resolves each
// path component without following a symlink out of the profile (including
// one swapped in while Blueprint runs). A symlinked managed directory such as
// machines/ -> ~/.config can never make Blueprint read, replace or delete a
// file outside the profile.
//
// Saving is a redo-journal transaction (see saveTransaction), so an
// interrupted save leaves either the previous profile or the new one, never
// a mix, once the next Load or Save has recovered it.

const (
	// transactionDir holds a save in progress. It is hidden, outside every
	// managed path, and exists only while a save runs or after one was
	// interrupted.
	transactionDir = ".blueprint-save"
	// transactionManifest is created by an atomic rename: its presence is
	// the save's commit point.
	transactionManifest = transactionDir + "/manifest.json"
	transactionFiles    = transactionDir + "/files"
)

// saveFault lets tests interrupt a save at a named point, as a crash would.
// It is a no-op in production.
var saveFault = func(point string) error { return nil }

// dirSynced lets tests observe the durability barriers a save makes. It is
// a no-op in production.
var dirSynced = func(rel string) {}

// profileStore is an open, confined profile directory.
type profileStore struct {
	dir  string
	root *os.Root
	lock *os.File
}

// openStore opens dir confined and takes the profile's exclusive lock, so a
// save, its recovery and a load never interleave (across processes too).
// The lock is held on the profile directory itself, so no lock file appears
// in the user's repository.
func openStore(dir string) (*profileStore, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	lock, err := os.Open(dir)
	if err != nil {
		root.Close()
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		lock.Close()
		root.Close()
		return nil, fmt.Errorf("lock profile: %w", err)
	}
	return &profileStore{dir: dir, root: root, lock: lock}, nil
}

func (s *profileStore) Close() error {
	_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
	return errors.Join(s.lock.Close(), s.root.Close())
}

func (s *profileStore) readFile(rel string) ([]byte, error) { return s.root.ReadFile(rel) }

func (s *profileStore) readList(rel string) ([]string, error) {
	b, err := s.readFile(rel)
	if err != nil {
		return nil, err
	}
	return normalize(strings.Split(string(b), "\n")), nil
}

// regularFiles lists the regular files directly in rel with the given
// extension. A symlink, even one that stays inside the profile, is never
// treated as profile metadata.
func (s *profileStore) regularFiles(rel, ext string) ([]string, error) {
	f, err := s.root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if pathpkg.Ext(entry.Name()) != ext {
			continue
		}
		info, err := s.root.Lstat(pathpkg.Join(rel, entry.Name()))
		if err != nil {
			return nil, err
		}
		if info.Mode().IsRegular() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// syncDir makes a directory's entries durable.
func (s *profileStore) syncDir(rel string) error {
	f, err := s.root.Open(rel)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return err
	}
	dirSynced(rel)
	return nil
}

// syncDirs makes every directory in dirs durable, with each ancestor up to
// the profile root, so a directory created on the way exists after a power
// loss too. Deepest directories go first.
func (s *profileStore) syncDirs(dirs map[string]bool) error {
	all := map[string]bool{}
	for dir := range dirs {
		for {
			all[dir] = true
			if dir == "." {
				break
			}
			dir = pathpkg.Dir(dir)
		}
	}
	ordered := make([]string, 0, len(all))
	for dir := range all {
		ordered = append(ordered, dir)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if di, dj := strings.Count(ordered[i], "/"), strings.Count(ordered[j], "/"); di != dj {
			return di > dj
		}
		return ordered[i] > ordered[j]
	})
	for _, dir := range ordered {
		if err := s.syncDir(dir); err != nil {
			return err
		}
	}
	return nil
}

// writeFileDurable writes rel through a temporary sibling and an atomic
// rename, so readers see the old or the new content, never a partial file.
func (s *profileStore) writeFileDurable(rel string, data []byte) error {
	dir := pathpkg.Dir(rel)
	if err := s.root.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp := pathpkg.Join(dir, ".omarchy-blueprint-"+randomSuffix()+".tmp")
	f, err := s.root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = s.root.Rename(temp, rel)
	}
	if err != nil {
		_ = s.root.Remove(temp)
		return err
	}
	return s.syncDir(dir)
}

func randomSuffix() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// saveTransaction is one save's complete set of changes to the profile.
type saveTransaction struct {
	Writes  []string `json:"writes"`
	Removes []string `json:"removes"`
	// Hashes is each write's SHA-256, so recovery can tell a write that
	// was already applied from a staged file that was lost.
	Hashes map[string]string `json:"hashes"`
	// CommitResourcesCapture marks the Resources generation installed for
	// this save committed together with it (see SaveOptions).
	CommitResourcesCapture bool `json:"commit_resources_capture,omitempty"`

	contents map[string][]byte
}

func newSaveTransaction() *saveTransaction {
	return &saveTransaction{contents: map[string][]byte{}}
}

func (t *saveTransaction) write(rel string, data []byte) {
	if _, ok := t.contents[rel]; !ok {
		t.Writes = append(t.Writes, rel)
	}
	t.contents[rel] = data
}

func (t *saveTransaction) remove(rel string) { t.Removes = append(t.Removes, rel) }

// commit stages every write inside the profile, makes the manifest durable
// with an atomic rename (the commit point), then applies it. Nothing live is
// touched before the commit point, so a failure or crash before it leaves
// the previous profile intact; after it, apply (or a later recovery replay)
// completes the new one.
func (s *profileStore) commit(t *saveTransaction) error {
	if err := s.root.RemoveAll(transactionDir); err != nil {
		return err
	}
	t.Hashes = map[string]string{}
	staged := map[string]bool{transactionDir: true}
	for _, rel := range t.Writes {
		if err := validateTransactionPath(rel); err != nil {
			return err
		}
		path := pathpkg.Join(transactionFiles, rel)
		if err := s.root.MkdirAll(pathpkg.Dir(path), 0o755); err != nil {
			return err
		}
		if err := writeSynced(s.root, path, t.contents[rel]); err != nil {
			return err
		}
		t.Hashes[rel] = contentHash(t.contents[rel])
		staged[pathpkg.Dir(path)] = true
	}
	// Every staged file's directory entry must be durable before the
	// manifest can be: a manifest that survived a power loss without them
	// would name files that are gone.
	if err := s.syncDirs(staged); err != nil {
		return err
	}
	for _, rel := range t.Removes {
		if err := validateTransactionPath(rel); err != nil {
			return err
		}
	}
	manifest, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if err := saveFault("staged"); err != nil {
		return err
	}
	pending := transactionDir + "/manifest.pending"
	if err := writeSynced(s.root, pending, manifest); err != nil {
		return err
	}
	if err := s.root.Rename(pending, transactionManifest); err != nil {
		return err
	}
	if err := s.syncDir(transactionDir); err != nil {
		return &CommittedSaveError{Err: err}
	}
	if err := saveFault("committed"); err != nil {
		return &CommittedSaveError{Err: err}
	}
	if err := s.apply(t); err != nil {
		return &CommittedSaveError{Err: err}
	}
	return nil
}

// apply moves a committed transaction's staged files into place and removes
// what it removes. It is idempotent: a write whose staged file is gone was
// already moved, so a replay after a crash finishes the job.
func (s *profileStore) apply(t *saveTransaction) error {
	dirs := map[string]bool{}
	for _, rel := range t.Writes {
		if err := validateTransactionPath(rel); err != nil {
			return err
		}
		staged := pathpkg.Join(transactionFiles, rel)
		if _, err := s.root.Lstat(staged); errors.Is(err, fs.ErrNotExist) {
			// Already moved into place, unless it was lost: never
			// complete a save with a file it does not hold.
			if err := s.confirmApplied(t, rel); err != nil {
				return err
			}
			continue
		} else if err != nil {
			return err
		}
		if err := s.root.MkdirAll(pathpkg.Dir(rel), 0o755); err != nil {
			return err
		}
		if err := s.root.Rename(staged, rel); err != nil {
			return err
		}
		dirs[pathpkg.Dir(rel)] = true
		if err := saveFault("applying"); err != nil {
			return err
		}
	}
	for _, rel := range t.Removes {
		if err := validateTransactionPath(rel); err != nil {
			return err
		}
		if err := s.root.Remove(rel); errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		dirs[pathpkg.Dir(rel)] = true
	}
	if err := s.syncDirs(dirs); err != nil {
		return err
	}
	if t.CommitResourcesCapture {
		// Only a generation still waiting for this save is committed.
		if marker, err := s.root.ReadFile(resourcesCaptureMarker); err == nil && strings.TrimSpace(string(marker)) == resourcesRollback {
			if err := s.writeFileDurable(resourcesCaptureMarker, []byte(resourcesCommitted+"\n")); err != nil {
				return err
			}
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if err := s.root.RemoveAll(transactionDir); err != nil {
		return err
	}
	return s.syncDir(".")
}

// confirmApplied checks that a write whose staged file is gone was already
// moved into place, by its content.
func (s *profileStore) confirmApplied(t *saveTransaction, rel string) error {
	want, ok := t.Hashes[rel]
	if ok {
		if live, err := s.root.ReadFile(rel); err == nil && contentHash(live) == want {
			return nil
		}
	}
	return fmt.Errorf("the interrupted profile save cannot be completed: its staged %s is missing and the profile's copy does not match it. The profile may be partly saved; check it, then remove %s to keep it as it is", rel, transactionDir)
}

func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// recover finishes or discards a save that was interrupted: a committed
// one is replayed, an uncommitted one is discarded (the live profile was
// never touched). Callers hold the profile lock.
func (s *profileStore) recover() error {
	if _, err := s.root.Lstat(transactionDir); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	manifest, err := s.root.ReadFile(transactionManifest)
	if errors.Is(err, fs.ErrNotExist) {
		return s.root.RemoveAll(transactionDir)
	}
	if err != nil {
		return err
	}
	var t saveTransaction
	if err := json.Unmarshal(manifest, &t); err != nil {
		return fmt.Errorf("recover interrupted profile save: %w", err)
	}
	if err := s.apply(&t); err != nil {
		return fmt.Errorf("recover interrupted profile save: %w", err)
	}
	return nil
}

// validateTransactionPath accepts only managed, slash-separated profile
// paths, so a tampered manifest can never name a path outside the
// profile's managed files (os.Root already keeps it inside the profile).
func validateTransactionPath(rel string) error {
	if !IsManagedRepositoryPath(rel) {
		return fmt.Errorf("profile save names an unmanaged path %q", rel)
	}
	return nil
}

func writeSynced(root *os.Root, rel string, data []byte) error {
	f, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	return errors.Join(err, f.Close())
}

// CommittedSaveError is a save that passed its commit point but could not
// finish applying. The new profile is durable and the next Load or Save
// completes it, so a caller must not undo work it depends on (such as a
// Resources generation) as if the save had failed.
type CommittedSaveError struct{ Err error }

func (e *CommittedSaveError) Error() string {
	return "profile save committed but not yet applied; it completes on the next load: " + e.Err.Error()
}
func (e *CommittedSaveError) Unwrap() error { return e.Err }

// WriteManagedFile durably writes rel, a managed path inside the profile at
// profileDir, confined to that profile. Providers that keep artifacts in
// the profile (outside Save's metadata) write them this way.
func WriteManagedFile(profileDir, rel string, data []byte) error {
	if err := validateTransactionPath(rel); err != nil {
		return err
	}
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return err
	}
	store, err := openStore(profileDir)
	if err != nil {
		return err
	}
	defer store.Close()
	return store.writeFileDurable(rel, data)
}

// RemoveManagedFile removes rel, a managed path inside the profile at
// profileDir, confined to that profile. A missing file is not an error.
func RemoveManagedFile(profileDir, rel string) error {
	if err := validateTransactionPath(rel); err != nil {
		return err
	}
	store, err := openStore(profileDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.root.Remove(rel); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
