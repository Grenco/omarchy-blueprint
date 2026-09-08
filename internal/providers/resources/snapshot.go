package resources

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type SnapshotScan struct {
	Hash      string
	Mode      os.FileMode
	FileCount int
	Links     []RawLink
}

type RawLink struct {
	SourceAbsolute string
	RawTarget      string
}

func ScanCopyResource(root string) (SnapshotScan, error) { return scanCopyResource(root, "") }

func StageCopyResource(source, destination string) (SnapshotScan, error) {
	return scanCopyResource(source, destination)
}

func HashSnapshotTree(root string) (string, error) {
	scan, err := ScanCopyResource(root)
	return scan.Hash, err
}

func ValidateSnapshotTree(root, expectedHash string) error {
	scan, err := ScanCopyResource(root)
	if err != nil {
		return err
	}
	if len(scan.Links) != 0 {
		return fmt.Errorf("snapshot contains symlink: %s", scan.Links[0].SourceAbsolute)
	}
	if scan.Hash != expectedHash {
		return fmt.Errorf("snapshot hash mismatch: got %s want %s", scan.Hash, expectedHash)
	}
	return nil
}

func scanCopyResource(root, destination string) (SnapshotScan, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return SnapshotScan{}, err
	}
	if err := validateCopyEntry(root, filepath.Base(root), info); err != nil {
		return SnapshotScan{}, err
	}
	scan := SnapshotScan{Mode: info.Mode().Perm()}
	hash := sha256.New()
	if info.Mode().IsRegular() {
		if err := hashCopyFile(hash, root, "root", info.Mode()); err != nil {
			return SnapshotScan{}, err
		}
		scan.FileCount = 1
		if destination != "" {
			if err := copySnapshotFile(root, destination, info.Mode()); err != nil {
				return SnapshotScan{}, err
			}
		}
		scan.Hash = fmt.Sprintf("%x", hash.Sum(nil))
		return scan, nil
	}
	if !info.IsDir() {
		return SnapshotScan{}, fmt.Errorf("unsupported file %s", root)
	}
	fmt.Fprintf(hash, "root-dir\x00%04o\x00", info.Mode().Perm())
	if destination != "" {
		if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
			return SnapshotScan{}, err
		}
		if err := os.Chmod(destination, info.Mode().Perm()); err != nil {
			return SnapshotScan{}, err
		}
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			scan.Links = append(scan.Links, RawLink{SourceAbsolute: path, RawTarget: target})
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if err := validateCopyEntry(path, relative, info); err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		if info.IsDir() {
			fmt.Fprintf(hash, "dir\x00%s\x00%04o\x00", name, info.Mode().Perm())
			if destination != "" {
				return os.MkdirAll(filepath.Join(destination, relative), info.Mode().Perm())
			}
			return nil
		}
		if err := hashCopyFile(hash, path, name, info.Mode()); err != nil {
			return err
		}
		scan.FileCount++
		if destination != "" {
			return copySnapshotFile(path, filepath.Join(destination, relative), info.Mode())
		}
		return nil
	})
	if err != nil {
		return SnapshotScan{}, err
	}
	scan.Hash = fmt.Sprintf("%x", hash.Sum(nil))
	return scan, nil
}

func hashCopyFile(hash io.Writer, path, relative string, mode os.FileMode) error {
	if _, err := fmt.Fprintf(hash, "file\x00%s\x00%04o\x00", filepath.ToSlash(relative), mode.Perm()); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	_, err = hash.Write([]byte{0})
	return err
}

func copySnapshotFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

func validateCopyEntry(path, relative string, info os.FileInfo) error {
	if sensitiveResourcePath(relative) {
		return fmt.Errorf("sensitive resource path: %s", relative)
	}
	if info.Mode().IsRegular() && hasPrivateKeyMarker(path) {
		return fmt.Errorf("sensitive private key content: %s", relative)
	}
	if !info.IsDir() && !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("unsupported file %s", relative)
	}
	return nil
}

func sensitiveResourcePath(relative string) bool {
	base := strings.ToLower(filepath.Base(relative))
	switch base {
	case ".env", "credentials", "credentials.json", "id_rsa", "id_ed25519", "id_ecdsa", "id_dsa", ".gnupg":
		return true
	}
	return strings.HasPrefix(base, ".env.") || (strings.HasSuffix(base, ".key") && strings.Contains(strings.ToLower(filepath.ToSlash(relative)), ".ssh/"))
}

func hasPrivateKeyMarker(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	buffer := make([]byte, 64*1024)
	n, _ := file.Read(buffer)
	return strings.Contains(string(buffer[:n]), "-----BEGIN ") && strings.Contains(string(buffer[:n]), "PRIVATE KEY-----")
}
