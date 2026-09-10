package sensitive

import (
	"bytes"
	"strings"
	"testing"
)

func TestScanReaderDetectsPrivateKeyAndToken(t *testing.T) {
	for _, input := range []string{"-----BEGIN OPENSSH PRIVATE KEY-----\n", "api_token = abcdefghijklmnopqrstuvwxyz"} {
		result, err := ScanReader(strings.NewReader(input), 1024)
		if err != nil || !result.Sensitive {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	}
}

func TestScanReaderRejectsLimit(t *testing.T) {
	if _, err := ScanReader(bytes.NewReader([]byte("abcd")), 3); err == nil {
		t.Fatal("oversized reader accepted")
	}
}
