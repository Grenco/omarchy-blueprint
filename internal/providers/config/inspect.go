package config

import (
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/Grenco/omarchy-blueprint/internal/content"
)

type FileInspection struct {
	Hash      string
	Sensitive bool
	TextLike  bool
	BytesRead int64
}

// InspectRegularFile reads untrusted content exactly once, feeding the same
// stream to the sensitive matcher and hash accumulator.
func InspectRegularFile(path string, maxBytes int64) (FileInspection, error) {
	f, _, err := content.OpenRegularFile(path)
	if err != nil {
		return FileInspection{}, err
	}
	defer f.Close()
	if sensitiveContentInspection != nil {
		sensitiveContentInspection()
	}
	hash := sha256.New()
	chunk := make([]byte, sensitiveContentChunkSize)
	var previous []byte
	var total int64
	textLike := true
	for {
		n, readErr := f.Read(chunk)
		if n > 0 {
			for _, b := range chunk[:n] {
				if b == 0 {
					textLike = false
					break
				}
			}
			total += int64(n)
			if total > maxBytes {
				return FileInspection{BytesRead: total}, nil
			}
			if _, err := hash.Write(chunk[:n]); err != nil {
				return FileInspection{}, err
			}
			window := append(append([]byte{}, previous...), chunk[:n]...)
			if sensitiveContentWindow(window) {
				return FileInspection{Sensitive: true, BytesRead: total}, nil
			}
			if len(window) > sensitiveContentOverlap {
				previous = append(previous[:0], window[len(window)-sensitiveContentOverlap:]...)
			} else {
				previous = append(previous[:0], window...)
			}
		}
		if readErr == io.EOF {
			return FileInspection{Hash: hex.EncodeToString(hash.Sum(nil)), BytesRead: total, TextLike: textLike}, nil
		}
		if readErr != nil {
			return FileInspection{}, readErr
		}
	}
}
