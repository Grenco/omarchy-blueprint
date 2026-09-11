package sensitive

import (
	"bytes"
	"fmt"
	"io"
	"regexp"

	"github.com/Grenco/omarchy-blueprint/internal/content"
)

const (
	ReasonPrivateKey = "private-key"
	ReasonToken      = "credential-token"
	chunkSize        = 32 << 10
	overlap          = 4 << 10
)

type Result struct {
	Sensitive bool
	Reason    string
}

var (
	pemPrivateKey   = regexp.MustCompile(`(?m)^-----BEGIN (?:[A-Z0-9]+ )?PRIVATE KEY-----\r?$`)
	structuredToken = regexp.MustCompile(`(?i)["']?(?:api_token|access_token|auth_token|refresh_token|client_secret)["']?\s*[:=]\s*["']?[A-Za-z0-9._~-]{16,}`)
	tokenKeys       = [][]byte{[]byte("api_token"), []byte("access_token"), []byte("auth_token"), []byte("refresh_token"), []byte("client_secret")}
)

func ScanRegularFile(path string, maxBytes int64) (Result, error) {
	f, _, err := content.OpenRegularFile(path)
	if err != nil {
		return Result{}, err
	}
	defer f.Close()
	return ScanReader(f, maxBytes)
}

func ScanReader(r io.Reader, maxBytes int64) (Result, error) {
	chunk, previous := make([]byte, chunkSize), []byte(nil)
	var total int64
	for {
		n, err := r.Read(chunk)
		if n > 0 {
			total += int64(n)
			if total > maxBytes {
				return Result{}, fmt.Errorf("content exceeds %d bytes", maxBytes)
			}
			window := append(append([]byte{}, previous...), chunk[:n]...)
			if result := ScanWindow(window); result.Sensitive {
				return result, nil
			}
			if len(window) > overlap {
				previous = append(previous[:0], window[len(window)-overlap:]...)
			} else {
				previous = append(previous[:0], window...)
			}
		}
		if err == io.EOF {
			return Result{}, nil
		}
		if err != nil {
			return Result{}, err
		}
	}
}

// ScanWindow inspects a bounded content window. Callers streaming content must
// retain an overlap between windows so signatures spanning chunks are visible.
func ScanWindow(window []byte) Result {
	for start := 0; ; {
		i := bytes.Index(window[start:], []byte("-----BEGIN "))
		if i < 0 {
			break
		}
		i += start
		if i == 0 || window[i-1] == '\n' {
			if pemPrivateKey.Match(window[i:]) {
				return Result{Sensitive: true, Reason: ReasonPrivateKey}
			}
		}
		start = i + 1
	}
	lower := bytes.ToLower(window)
	for _, key := range tokenKeys {
		if bytes.Contains(lower, key) && structuredToken.Match(window) {
			return Result{Sensitive: true, Reason: ReasonToken}
		}
	}
	return Result{}
}
