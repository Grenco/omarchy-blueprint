package services

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

// planArtifact is deliberately narrower than unit ownership: callers first
// establish effective systemd provenance. This helper then confines a file
// effect to the persistent user config root with exact execution preconditions.
func (p *Provider) planArtifact(unit, relative, destination string, desired capturedFile, force, deletion bool) (model.Operation, string, error) {
	if !withinUserRoot(destination, p.Roots.UserConfigDir) {
		return model.Operation{}, "Service artifact is outside the persistent user configuration root", nil
	}
	for parent := filepath.Dir(destination); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return model.Operation{}, "Service artifact has a linked or non-directory parent", nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return model.Operation{}, "", err
		}
		if parent == filepath.Dir(parent) {
			break
		}
	}
	info, err := os.Lstat(destination)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return model.Operation{}, "", err
	}
	if !missing && !info.Mode().IsRegular() {
		return model.Operation{}, "Service target is not an owned regular file; Force cannot replace it", nil
	}
	mode := uint32(0o644)
	if desired.mode != "" {
		parsed, err := strconv.ParseUint(desired.mode, 8, 32)
		if err != nil || parsed > 0o777 {
			return model.Operation{}, "", fmt.Errorf("invalid Services artifact mode %q", desired.mode)
		}
		mode = uint32(parsed)
	}
	var currentHash string
	var currentMode uint32
	var observedFile capturedFile
	if !missing {
		observedFile, err = readServiceFile(destination)
		if err != nil {
			return model.Operation{}, "Service target cannot be safely inspected; Force cannot bypass sensitivity or source protection", nil
		}
		currentHash = observedFile.hash
		parsed, err := strconv.ParseUint(observedFile.mode, 8, 32)
		if err != nil {
			return model.Operation{}, "", err
		}
		currentMode = uint32(parsed)
		if desired.mode == "" {
			mode = currentMode
		}
	}
	if deletion && missing {
		return model.Operation{}, "", nil
	}
	if deletion && (desired.hash == "" || desired.hash != currentHash || desired.mode != "" && mode != currentMode) {
		return model.Operation{}, "Exact Service removal lacks matching prior managed content/mode; target left untouched", nil
	}
	if !deletion && desired.hash == "" {
		return model.Operation{}, "", fmt.Errorf("desired Services artifact %q has no content provenance", relative)
	}
	if !deletion && !missing && desired.hash == currentHash && mode == currentMode {
		return model.Operation{}, "", nil
	}
	if !deletion && !missing && !force {
		return model.Operation{}, "Service file conflicts with captured intent; Safe preserves it", nil
	}
	sum := sha256.Sum256([]byte(relative))
	op := model.Operation{ID: fmt.Sprintf("services.file.%x", sum[:12]), Provider: "services", Resource: unit, Risk: model.RiskMedium, Reversible: true}
	precondition := regularFilePrecondition(observedFile, currentMode)
	if deletion {
		op.Action = "remove"
		op.Delete = &model.FileDelete{Destination: destination, ExpectedExisting: precondition, Backup: true, RejectSymlinkParents: true}
	} else {
		op.Action = "write"
		op.File = &model.FileWrite{Source: filepath.Join(p.ProfileDir, "services", filepath.FromSlash(relative)), SourceHash: desired.hash, Destination: destination, Mode: &mode, ExpectedMissing: missing, RejectSymlinkParents: true}
		if !missing {
			op.File.ReplaceExisting, op.File.ExpectedExisting, op.File.Backup = true, precondition, true
		}
	}
	return op, "", nil
}

// Match content.HashFilesystemObject's single-file identity using the exact
// bytes/mode observed for content proof, rather than reopening a raced path.
func regularFilePrecondition(file capturedFile, mode uint32) *model.FilesystemPrecondition {
	hash := sha256.New()
	fmt.Fprintf(hash, ".\x00file\x00%o\x00", mode)
	hash.Write(file.data)
	return &model.FilesystemPrecondition{Type: "file", Mode: mode, Hash: fmt.Sprintf("%x", hash.Sum(nil))}
}

func unsafeServiceParents(destination string, roots Roots) string {
	if !withinUserRoot(destination, roots.UserConfigDir) {
		return "Service mutation target is outside persistent user configuration"
	}
	for parent := filepath.Dir(destination); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return "Service mutation has linked or non-directory parents"
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "Service mutation parent cannot be inspected safely"
		}
		if parent == filepath.Dir(parent) {
			break
		}
	}
	return ""
}
