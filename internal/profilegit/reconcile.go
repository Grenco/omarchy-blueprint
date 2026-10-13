package profilegit

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncprofile"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

type SemanticMergeRequest struct {
	BaseSHA, LocalSHA, RemoteSHA string
	StagingRoot, Message         string
}

type UnmanagedHistoryError struct{ Paths []string }

func (e *UnmanagedHistoryError) Error() string {
	return "divergent history changes non-Blueprint paths; reconcile Git history outside Sync"
}

func (s Service) CommitSemanticMerge(ctx context.Context, req SemanticMergeRequest) (result Result, resultErr error) {
	for _, sha := range []string{req.BaseSHA, req.LocalSHA, req.RemoteSHA} {
		if !exactCommitID.MatchString(sha) {
			return Result{}, errors.New("semantic reconciliation requires exact commit identities")
		}
	}
	if err := s.checkReconciliationEntry(ctx, req.LocalSHA, req.RemoteSHA); err != nil {
		return Result{}, err
	}
	for _, tip := range []string{req.LocalSHA, req.RemoteSHA} {
		ok, err := s.IsAncestor(ctx, req.BaseSHA, tip)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			return Result{}, errors.New("reviewed base is not an ancestor of both histories")
		}
	}
	for _, ends := range [][2]string{{req.LocalSHA, req.RemoteSHA}, {req.RemoteSHA, req.LocalSHA}} {
		ok, err := s.IsAncestor(ctx, ends[0], ends[1])
		if err != nil {
			return Result{}, err
		}
		if ok {
			return Result{}, errors.New("semantic merge requires divergent histories")
		}
	}
	unmanaged := map[string]bool{}
	for _, tip := range []string{req.LocalSHA, req.RemoteSHA} {
		paths, err := s.CommittedPaths(ctx, req.BaseSHA, tip)
		if err != nil {
			return Result{}, err
		}
		for _, path := range paths {
			if !profile.IsManagedRepositoryPath(path) {
				unmanaged[path] = true
			}
		}
	}
	if len(unmanaged) > 0 {
		paths := make([]string, 0, len(unmanaged))
		for path := range unmanaged {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		return Result{}, &UnmanagedHistoryError{Paths: paths}
	}
	data, err := profile.Load(req.StagingRoot)
	if err != nil {
		return Result{}, errors.New("invalid semantic staging profile")
	}
	stage, err := syncprofile.NewBuilder(data)
	if err != nil {
		return Result{}, err
	}
	defer stage.Close()
	for _, root := range profile.ManagedTopLevelPaths {
		if _, err := os.Lstat(filepath.Join(req.StagingRoot, root)); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return Result{}, err
		}
		if err := stage.ReplaceArtifact(ctx, req.StagingRoot, root); err != nil {
			return Result{}, errors.New("invalid semantic staging artifact")
		}
	}
	if _, err := profile.Load(stage.Root()); err != nil {
		return Result{}, errors.New("invalid copied semantic profile")
	}
	expected, err := managedFileIdentities(stage.Root())
	if err != nil {
		return Result{}, err
	}
	original, err := s.archiveTree(ctx, req.LocalSHA)
	if err != nil {
		return Result{}, err
	}
	for _, entry := range original {
		if !profile.IsManagedRepositoryPath(entry.name) {
			expected[entry.name] = managedFileIdentity{Hash: entry.hash, Mode: entry.mode}
		}
	}
	backup, err := os.MkdirTemp("", "blueprint-sync-rollback-*")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(backup)
	if err := s.MaterializeCommit(ctx, req.LocalSHA, backup); err != nil {
		return Result{}, err
	}
	oldFiles, err := managedFileIdentities(backup)
	if err != nil {
		return Result{}, errors.New("unsupported managed rollback tree; reconcile outside Sync")
	}
	liveFiles, err := managedFileIdentities(s.Root)
	if err != nil || !reflect.DeepEqual(oldFiles, liveFiles) {
		return Result{}, errors.New("live managed files do not exactly represent committed rollback tree")
	}
	beforeIndex, err := s.revisionRun(ctx, "write-tree")
	if err != nil {
		return Result{}, revisionError(ctx, "inspect original index", err)
	}
	if err := s.checkReconciliationEntry(ctx, req.LocalSHA, req.RemoteSHA); err != nil {
		return Result{}, err
	}
	started := false
	defer func() {
		if !started || resultErr == nil {
			return
		}
		// Recovery must run even if the caller cancelled, but remains bounded.
		recovery, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		head, err := s.ResolveCommit(recovery, "HEAD")
		if err != nil || head != req.LocalSHA {
			resultErr = errors.Join(resultErr, ErrReconciliationOutcomeUncertain)
			return
		}
		_, abortErr := s.revisionRun(recovery, "merge", "--abort")
		copyErr := replaceManagedTree(recovery, backup, s.Root)
		index, indexErr := s.revisionRun(recovery, "write-tree")
		status, statusErr := s.ReadOnlyStatus(recovery)
		if abortErr != nil || copyErr != nil || indexErr != nil || statusErr != nil || index != beforeIndex || status.Head != req.LocalSHA || len(status.Changes) != 0 {
			resultErr = errors.Join(resultErr, ErrReconciliationRollbackFailed)
		}
	}()
	started = true
	if _, err := s.revisionRun(ctx, "merge", "--no-commit", "--no-ff", "-s", "ours", "--", req.RemoteSHA); err != nil {
		return Result{}, revisionError(ctx, "prepare semantic merge ancestry", err)
	}
	if err := replaceManagedTree(ctx, stage.Root(), s.Root); err != nil {
		return Result{}, errors.New("replace semantic managed tree failed")
	}
	// Only existing or tracked managed roots can be passed to git add: an
	// absent, never-tracked pathspec is an error, while tracked deletions must stay.
	args := []string{"add", "-A", "--"}
	tracked, err := s.revisionOutput(ctx, "ls-files", "-z")
	if err != nil {
		return Result{}, revisionError(ctx, "inspect managed staging paths", err)
	}
	for _, root := range profile.ManagedTopLevelPaths {
		_, existsErr := os.Lstat(filepath.Join(s.Root, root))
		exists := existsErr == nil
		for _, path := range strings.Split(string(tracked), "\x00") {
			if path == root || strings.HasPrefix(path, root+"/") {
				exists = true
			}
		}
		if exists {
			args = append(args, root)
		}
	}
	if _, err := s.revisionRun(ctx, args...); err != nil {
		return Result{}, revisionError(ctx, "stage semantic managed tree", err)
	}
	staged, err := s.ReadOnlyStatus(ctx)
	if err != nil {
		return Result{}, err
	}
	for _, change := range staged.Changes {
		if !change.Managed || (change.OriginalPath != "" && !change.OriginalManaged) {
			return Result{}, errors.New("semantic staging includes unmanaged changes")
		}
	}
	if _, err := profile.Load(s.Root); err != nil {
		return Result{}, errors.New("invalid reconciled profile checkout")
	}
	indexTree, err := s.revisionRun(ctx, "write-tree")
	if err != nil {
		return Result{}, revisionError(ctx, "inspect staged semantic tree", err)
	}
	if err := s.verifySemanticTree(ctx, strings.TrimSpace(indexTree), expected); err != nil {
		return Result{}, err
	}
	upstream, err := s.UpstreamCommit(ctx)
	if err != nil {
		return Result{}, err
	}
	if upstream != req.RemoteSHA {
		return Result{}, errors.New("cached upstream changed during semantic reconciliation")
	}
	message := req.Message
	if strings.TrimSpace(message) == "" {
		message = "Reconcile Blueprint profile intent"
	}
	if _, err := s.revisionRun(ctx, "commit", "-m", message); err != nil {
		return Result{}, revisionError(ctx, "commit semantic reconciliation", err)
	}
	started = false // A committed result must never be silently rolled back.
	parents, err := s.revisionRun(ctx, "show", "-s", "--format=%P", "HEAD")
	if err != nil {
		return Result{}, revisionError(ctx, "verify semantic parents", err)
	}
	if strings.TrimSpace(parents) != req.LocalSHA+" "+req.RemoteSHA {
		return Result{}, errors.New("semantic reconciliation parent verification failed")
	}
	if err := s.verifySemanticTree(ctx, "HEAD", expected); err != nil {
		return Result{}, err
	}
	status, err := s.ReadOnlyStatus(ctx)
	if err != nil {
		return Result{}, err
	}
	if len(status.Changes) != 0 {
		return Result{}, errors.New("semantic reconciliation left dirty checkout")
	}
	return Result{Changed: true, Commit: status.Head, Status: status}, nil
}

// Source trees are private validated copies; destinations are exact repository
// roots. Only explicitly enumerated managed paths are replaced, never .git or
// unrelated user files. A failed copy remains an error, not partial authority.
func replaceManagedTree(ctx context.Context, source, dest string) error {
	for _, root := range profile.ManagedTopLevelPaths {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := filepath.Join(dest, root)
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		from := filepath.Join(source, root)
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
			to := filepath.Join(target, rel)
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.IsDir() {
				return os.MkdirAll(to, 0700)
			}
			if !info.Mode().IsRegular() {
				return errors.New("unsupported semantic artifact type")
			}
			if err := os.MkdirAll(filepath.Dir(to), 0700); err != nil {
				return err
			}
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			defer in.Close()
			out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, in)
			closeErr := out.Close()
			return errors.Join(copyErr, closeErr)
		}); err != nil {
			return err
		}
	}
	return ctx.Err()
}

type managedFileIdentity struct{ Hash, Mode string }

func managedFileIdentities(root string) (map[string]managedFileIdentity, error) {
	result := map[string]managedFileIdentity{}
	for _, name := range profile.ManagedTopLevelPaths {
		path := filepath.Join(root, name)
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		if err := filepath.WalkDir(path, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return errors.New("unsupported managed artifact type")
			}
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			defer in.Close()
			hash := sha1.New()
			fmt.Fprintf(hash, "blob %d\x00", info.Size())
			if _, err := io.Copy(hash, in); err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			mode := "100644"
			if info.Mode().Perm()&0111 != 0 {
				mode = "100755"
			}
			result[filepath.ToSlash(rel)] = managedFileIdentity{Hash: fmt.Sprintf("%x", hash.Sum(nil)), Mode: mode}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s Service) verifySemanticTree(ctx context.Context, tree string, expected map[string]managedFileIdentity) error {
	entries, err := s.archiveTree(ctx, tree)
	if err != nil {
		return err
	}
	actual := map[string]managedFileIdentity{}
	for _, entry := range entries {
		actual[entry.name] = managedFileIdentity{Hash: entry.hash, Mode: entry.mode}
	}
	if !reflect.DeepEqual(actual, expected) {
		return errors.New("semantic tree differs from reviewed managed intent or original unmanaged content")
	}
	return nil
}

// FastForwardTo follows only the exact reviewed cached revision. Both reviewed
// ends are supplied so changed HEAD cannot silently alter the approved operation.
func (s Service) FastForwardTo(ctx context.Context, expectedLocalSHA, reviewedSHA string) (Result, error) {
	if !exactCommitID.MatchString(expectedLocalSHA) || !exactCommitID.MatchString(reviewedSHA) {
		return Result{}, errors.New("pinned reconciliation requires exact commit identities")
	}
	if err := s.checkReconciliationEntry(ctx, expectedLocalSHA, reviewedSHA); err != nil {
		return Result{}, err
	}
	ancestor, err := s.IsAncestor(ctx, expectedLocalSHA, reviewedSHA)
	if err != nil {
		return Result{}, err
	}
	if !ancestor {
		return Result{}, errors.New("reviewed remote revision cannot fast-forward local history")
	}
	if _, err := s.revisionRun(ctx, "merge", "--ff-only", "--no-edit", "--", reviewedSHA); err != nil {
		return Result{}, revisionError(ctx, "fast-forward reviewed profile history", err)
	}
	status, err := s.ReadOnlyStatus(ctx)
	if err != nil {
		return Result{}, err
	}
	if status.Head != reviewedSHA || len(status.Changes) != 0 {
		return Result{}, errors.New("pinned fast-forward verification failed; inspect profile repository before continuing")
	}
	return Result{Changed: expectedLocalSHA != reviewedSHA, Commit: reviewedSHA, Status: status}, nil
}

func (s Service) checkReconciliationEntry(ctx context.Context, local, remote string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := s.CurrentRevisionInfo(ctx)
	if err != nil {
		return err
	}
	if info.SHA != local {
		return errors.New("reviewed local revision changed; reinspect Sync")
	}
	r, err := s.UpstreamCommit(ctx)
	if err != nil {
		return err
	}
	if r != remote {
		return errors.New("reviewed cached upstream revision changed; reinspect Sync")
	}
	for _, marker := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply", "sequencer", "BISECT_START"} {
		out, err := s.revisionRun(ctx, "rev-parse", "--path-format=absolute", "--git-path", marker)
		if err != nil {
			return revisionError(ctx, "inspect Git transaction state", err)
		}
		path := strings.TrimSpace(out)
		if !filepath.IsAbs(path) {
			return errors.New("invalid Git transaction path")
		}
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("profile has an unfinished Git %s transaction; repair it outside Sync", marker)
		} else if !os.IsNotExist(err) {
			return errors.New("cannot inspect Git transaction state")
		}
	}
	status, err := s.ReadOnlyStatus(ctx)
	if err != nil {
		return err
	}
	if len(status.Changes) != 0 {
		return errors.New("Sync reconciliation requires a clean worktree and index")
	}
	ignored, err := s.revisionOutput(ctx, "ls-files", "--others", "--ignored", "--exclude-standard", "-z")
	if err != nil {
		return revisionError(ctx, "inspect ignored repository content", err)
	}
	for _, path := range strings.Split(string(ignored), "\x00") {
		if profile.IsManagedRepositoryPath(path) {
			return errors.New("ignored managed content blocks Sync; review it outside Sync")
		}
	}
	return ctx.Err()
}
