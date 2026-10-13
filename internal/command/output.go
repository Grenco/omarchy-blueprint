package command

import (
	"bytes"
	"fmt"
	"os/exec"
	"sync"
)

var (
	// maxCommandOutput bounds what Run collects. Callers parse that output
	// (package lists, Git porcelain), so it is never silently truncated: a
	// command that exceeds it is stopped and Run fails, rather than letting
	// a runaway or hostile command exhaust memory.
	maxCommandOutput int64 = 32 << 20
	// maxDiagnosticOutput bounds output kept only to explain a failure
	// (RunOutput's stderr, RunDetached's output). Its tail is kept, where
	// error messages usually are.
	maxDiagnosticOutput = 256 << 10
)

// tailBuffer keeps the last limit bytes written to it and counts the rest,
// so a command can write any amount without growing memory or failing.
type tailBuffer struct {
	mu      sync.Mutex
	limit   int
	data    []byte
	dropped int64
}

func newTailBuffer(limit int) *tailBuffer { return &tailBuffer{limit: limit} }

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if len(p) >= b.limit {
		b.dropped += int64(len(b.data) + len(p) - b.limit)
		b.data = append(b.data[:0], p[len(p)-b.limit:]...)
		return n, nil
	}
	if over := len(b.data) + len(p) - b.limit; over > 0 {
		b.dropped += int64(over)
		b.data = append(b.data[:0], b.data[over:]...)
	}
	b.data = append(b.data, p...)
	return n, nil
}

// String is the kept output, noting how much was dropped before it.
func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dropped == 0 {
		return string(b.data)
	}
	return fmt.Sprintf("[… %d earlier bytes of output omitted]\n%s", b.dropped, b.data)
}

// boundedBuffer collects output up to limit bytes. Past it, the write fails
// (exec then closes the pipe) and stop is called once, so a command that
// ignores the closed pipe cannot keep running. It is safe for the two copy
// goroutines exec uses when stdout and stderr share it.
type boundedBuffer struct {
	mu      sync.Mutex
	data    bytes.Buffer
	limit   int64
	stop    func()
	stopped bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if int64(b.data.Len()+len(p)) > b.limit {
		if !b.stopped && b.stop != nil {
			b.stopped = true
			b.stop()
		}
		return 0, errOutputLimit
	}
	return b.data.Write(p)
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.String()
}

func (b *boundedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Bytes()
}

// killOnLimit stops cmd's process once its output passes the limit.
func killOnLimit(cmd *exec.Cmd) func() {
	return func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
}

// diagnosticPrefix is at most maxDiagnosticOutput bytes of s, for an error.
func diagnosticPrefix(s string) string {
	if len(s) <= maxDiagnosticOutput {
		return s
	}
	return s[:maxDiagnosticOutput] + fmt.Sprintf("\n[… %d more bytes of output omitted]", len(s)-maxDiagnosticOutput)
}
