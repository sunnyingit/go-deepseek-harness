package bashsandbox

import "bytes"

// limitedBuffer caps how many bytes it retains — an io.Writer, so it can be
// used directly as an exec.Cmd's Stdout or Stderr.
type limitedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (w *limitedBuffer) Write(p []byte) (int, error) {
	remaining := w.max - w.buf.Len()
	if remaining <= 0 {
		w.truncated = true
		return len(p), nil // report success — the process must not see a write error over this
	}
	if len(p) > remaining {
		w.buf.Write(p[:remaining])
		w.truncated = true
		return len(p), nil
	}
	w.buf.Write(p)
	return len(p), nil
}

func (w *limitedBuffer) String() string {
	if w.truncated {
		return w.buf.String() + "\n...[output truncated]"
	}
	return w.buf.String()
}
