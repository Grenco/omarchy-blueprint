package workflow

import (
	"context"
	"errors"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncprofile"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// A scoped Capture may call a provider whose artifacts are finalized inside
// Capture itself. Keep a private complete managed-tree checkpoint in addition
// to the existing provider transactions, until fingerprint rechecks and Save
// have succeeded. This is rollback storage, never observed mutation authority.
func (s *Session) captureScopedWithCheckpoint(ctx context.Context, ids []string, approved *CaptureInspection, scope TargetSelection) (result CaptureResult, resultErr error) {
	if err := s.Reload(); err != nil {
		return result, err
	}
	root := s.ProfileDir()
	backup, err := syncprofile.NewBuilder(s.profile)
	if err != nil {
		return result, err
	}
	defer backup.Close()
	for _, name := range profile.ManagedTopLevelPaths {
		if _, err := os.Lstat(filepath.Join(root, name)); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return result, err
		}
		if err := backup.ReplaceArtifact(ctx, root, name); err != nil {
			return result, errors.New("scoped Capture cannot checkpoint managed artifacts safely")
		}
	}
	defer func() {
		if resultErr == nil {
			return
		}
		recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := restoreCaptureCheckpoint(recovery, backup.Root(), root); err != nil {
			resultErr = errors.Join(resultErr, errors.New("HIGH SEVERITY: scoped Capture rollback failed; repair profile before continuing"))
			return
		}
		if err := s.Reload(); err != nil {
			resultErr = errors.Join(resultErr, errors.New("scoped Capture rollback profile reload failed"))
		}
	}()
	return s.captureManyTargets(ctx, ids, approved, scope)
}

// Both roots are locally owned lifetime values: a private validated checkpoint
// and Session's canonical profile root. Only enumerated managed paths change.
func restoreCaptureCheckpoint(ctx context.Context, source, dest string) error {
	for _, name := range profile.ManagedTopLevelPaths {
		if err := ctx.Err(); err != nil {
			return err
		}
		to := filepath.Join(dest, name)
		if err := os.RemoveAll(to); err != nil {
			return err
		}
		from := filepath.Join(source, name)
		if _, err := os.Lstat(from); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			rel, err := filepath.Rel(from, path)
			if err != nil {
				return err
			}
			target := filepath.Join(to, rel)
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.IsDir() {
				return os.MkdirAll(target, 0700)
			}
			if !info.Mode().IsRegular() {
				return errors.New("unsupported Capture rollback artifact")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, in)
			return errors.Join(copyErr, out.Close())
		}); err != nil {
			return err
		}
	}
	return nil
}
