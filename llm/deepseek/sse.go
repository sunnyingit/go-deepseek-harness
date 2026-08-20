// Port of sse.ts: decode an SSE byte stream into `data:` payloads. Framing
// only — comments and non-data fields are skipped, multi-line `data:`
// fields join with "\n". The literal "[DONE]" is yielded like any other
// payload so the caller owns final flushing; EOF before it is truncation,
// not a flushable payload.
package deepseek

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
)

// doneMarker is the terminal payload DeepSeek (and OpenAI) send after the
// last chunk.
const doneMarker = "[DONE]"

// errStreamClosed reports an SSE body that ended without doneMarker.
var errStreamClosed = errors.New("deepseek: SSE stream ended without [DONE]")

// parseSSE reads r line by line, dispatching one payload per blank-line-
// terminated event to out in arrival order. It returns nil after
// successfully sending doneMarker, errStreamClosed if the reader is
// exhausted first, ctx.Err() if ctx is cancelled while a send is blocked,
// or a scanner I/O error otherwise.
func parseSSE(ctx context.Context, r io.Reader, out chan<- string) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	send := func(payload string) (done bool, err error) {
		select {
		case out <- payload:
			return payload == doneMarker, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}

	var data []string
	flush := func() (bool, error) {
		if len(data) == 0 {
			return false, nil
		}
		payload := strings.Join(data, "\n")
		data = data[:0]
		return send(payload)
	}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			done, err := flush()
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		case strings.HasPrefix(line, ":"):
			// Comment / keep-alive: transport activity, never a payload.
		case strings.HasPrefix(line, "data:"):
			field := strings.TrimPrefix(line, "data:")
			field = strings.TrimPrefix(field, " ")
			data = append(data, field)
		default:
			// Other SSE fields (event:, id:, retry:) — unused by this wire protocol.
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	// A trailing unterminated event at EOF is truncation, not a flushable
	// payload — do not flush it; falling through to errStreamClosed is the
	// correct signal either way.
	return errStreamClosed
}
