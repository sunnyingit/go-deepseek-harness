// Package sessionpersistence is a Go port of
// packages/session/session-persistence-jsonl from DeepSeek Harness: a
// durable, append-only, crash-tolerant JSONL storage backend for
// core/session sessions, laid out one file per session under a project
// -cwd-derived directory tree. What's cut relative to the TS original —
// no zstd compression, no packed chunk-run line compaction, no background
// write-batching coordinator, no Prepare/inspect/readFrom/HMR-takeover, a
// single process-wide mutex instead of per-session write coordination — is
// documented in README.md, along with why the crash-recovery repair this
// port performs (closing an unanswered tool call) is narrower than the TS
// original's (which also synthesizes turn/end and step/end closers this
// port's core/agentloop doesn't need).
package sessionpersistence

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go-harness/core/session"
)

// maxLineBytes bounds one JSONL line — generous relative to bufio.Scanner's
// 64KiB default, since a single assistant message or tool result can
// legitimately be larger than that.
const maxLineBytes = 16 << 20 // 16MiB

// Store is one root directory's session archive. Safe for concurrent use
// (a single mutex serializes every operation) — see README.md for why this
// is a process-wide lock rather than per-session coordination.
type Store struct {
	root string

	mu sync.Mutex
	// durableCount caches how many events are already durable on disk per
	// session, so ordinary Append/Sync calls don't need to re-read a
	// session's file just to check contiguity. Populated lazily on first
	// touch (Append falls back to counting on-disk lines) and kept exact
	// by every successful Append this Store performs — see README.md for
	// the single-writer assumption this cache depends on.
	durableCount map[session.SessionId]int
}

// Open returns a Store rooted at root, creating it (owner-only permissions)
// if it doesn't exist yet.
func Open(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("session-persistence: creating root %q: %w", root, err)
	}
	return &Store{root: root, durableCount: map[session.SessionId]int{}}, nil
}

// Append durably writes events (which must start at whatever seq is
// already durable for header.ID) to that session's file, materializing it
// first if this is the session's first-ever append.
func (st *Store) Append(header session.Header, events []session.Event) error {
	if len(events) == 0 {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()

	path := sessionFile(st.root, header.ID, header.CWD)
	already, ok := st.durableCount[header.ID]
	if !ok {
		n, err := countDurableEvents(path)
		if err != nil {
			return err
		}
		already = n
	}
	if events[0].Seq != already {
		return fmt.Errorf("session-persistence: append for session %q starts at seq %d, but %d events are already durable",
			header.ID, events[0].Seq, already)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := materializeSession(path, header, events); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("session-persistence: stat %q: %w", path, err)
	} else {
		if err := appendLines(path, events); err != nil {
			return err
		}
	}

	st.durableCount[header.ID] = already + len(events)
	return nil
}

// Sync appends whatever events sess has accumulated since the last Sync
// (or Load/Append) touched its id — the caller-driven write path this port
// uses in place of the TS original's background write-batching coordinator
// (see README.md). A caller wanting sess durable calls this after
// whatever point it considers a save boundary (e.g. web/server.go could
// call it once per completed turn).
func (st *Store) Sync(sess *session.Session) error {
	header := sess.Header()
	events := sess.Events()

	st.mu.Lock()
	already, ok := st.durableCount[header.ID]
	st.mu.Unlock()
	if !ok {
		n, err := countDurableEvents(sessionFile(st.root, header.ID, header.CWD))
		if err != nil {
			return err
		}
		already = n
	}
	if already >= len(events) {
		return nil
	}
	return st.Append(header, events[already:])
}

// Load reads and returns id's full session, recovering from a crash mid
// -write if needed (see recover.go). cwd must match the value the session
// was created with — Load has no way to search for an id across every cwd
// directory; use ListSnapshots to discover a path first if the cwd isn't
// already known.
func (st *Store) Load(id session.SessionId, cwd string) (*session.Session, error) {
	return st.LoadPath(sessionFile(st.root, id, cwd))
}

// LoadPath loads the session at a specific file path — what ListSnapshots
// returns paths for, and what Load itself resolves to internally.
func (st *Store) LoadPath(path string) (*session.Session, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("session-persistence: opening %q: %w", path, err)
	}

	rawLines := bytes.Split(raw, []byte("\n"))
	// A well-formed file ends with a trailing newline, producing one empty
	// final element from Split; a torn write may not, in which case there
	// is no trailing empty element and the true last line is itself the
	// incomplete one, handled below by the decode-error break.
	if len(rawLines) > 0 && len(rawLines[len(rawLines)-1]) == 0 {
		rawLines = rawLines[:len(rawLines)-1]
	}
	if len(rawLines) == 0 {
		return nil, fmt.Errorf("session-persistence: %q is empty", path)
	}

	var header session.Header
	if err := json.Unmarshal(rawLines[0], &header); err != nil {
		return nil, fmt.Errorf("session-persistence: %q has a malformed header: %w", path, err)
	}

	var events []session.Event
	for _, line := range rawLines[1:] {
		if len(bytes.TrimSpace(line)) == 0 {
			break // a blank line only ever comes from a torn partial flush; nothing after it is trustworthy either
		}
		ev, err := decodeEvent(line)
		if err != nil {
			break // torn or corrupt tail — recoverPrefix below finds how much of what parsed cleanly is actually usable
		}
		events = append(events, ev)
	}

	s, n, err := recoverPrefix(header.ID, header, events)
	if err != nil {
		return nil, err
	}

	// Physically truncate a torn/corrupt tail so a later Append can't land
	// after garbage bytes that a subsequent Load would then choke on again.
	if n < len(rawLines)-1 {
		validBytes := int64(len(rawLines[0]) + 1)
		for _, line := range rawLines[1 : 1+n] {
			validBytes += int64(len(line) + 1)
		}
		if err := os.Truncate(path, validBytes); err != nil {
			return nil, fmt.Errorf("session-persistence: truncating torn tail of %q: %w", path, err)
		}
	}

	st.mu.Lock()
	st.durableCount[header.ID] = n
	st.mu.Unlock()

	closeUnansweredToolCalls(s)
	if repaired := s.Events(); len(repaired) > n {
		if err := st.Append(header, repaired[n:]); err != nil {
			return nil, fmt.Errorf("session-persistence: committing crash-recovery repair for %q: %w", header.ID, err)
		}
	}

	return s, nil
}

// Snapshot is one session's lightweight on-disk identity: enough to decide
// whether it changed since a previous observation, without loading its
// full log. Size+ModTime stand in for the TS original's device/inode-based
// revision — see README.md for why.
type Snapshot struct {
	ID      session.SessionId
	Path    string
	Size    int64
	ModTime time.Time
}

// ListSnapshots enumerates every session file under root without loading
// any of them, for a caller deciding what to load or whether a previously
// -loaded session changed on disk.
func (st *Store) ListSnapshots() ([]Snapshot, error) {
	paths, err := st.sessionPaths()
	if err != nil {
		return nil, err
	}
	out := make([]Snapshot, 0, len(paths))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		h, err := readHeaderOnly(p)
		if err != nil {
			continue
		}
		out = append(out, Snapshot{ID: h.ID, Path: p, Size: info.Size(), ModTime: info.ModTime()})
	}
	return out, nil
}

// List returns every stored session's header — cheap, header-only reads,
// no full-log parsing. A session that failed to materialize (Create with
// no Append yet — this port doesn't even offer that distinction, see
// README.md) or whose file is unreadable/corrupt is silently omitted
// rather than failing the whole call, so one bad session can't hide every
// other one.
func (st *Store) List() ([]session.Header, error) {
	paths, err := st.sessionPaths()
	if err != nil {
		return nil, err
	}
	headers := make([]session.Header, 0, len(paths))
	for _, p := range paths {
		h, err := readHeaderOnly(p)
		if err != nil {
			continue
		}
		headers = append(headers, h)
	}
	return headers, nil
}

func (st *Store) sessionPaths() ([]string, error) {
	var paths []string
	err := filepath.WalkDir(st.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "session.jsonl" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("session-persistence: scanning %q: %w", st.root, err)
	}
	return paths, nil
}

func readHeaderOnly(path string) (session.Header, error) {
	f, err := os.Open(path)
	if err != nil {
		return session.Header{}, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	if !scanner.Scan() {
		return session.Header{}, fmt.Errorf("session-persistence: %q has no header line", path)
	}
	var h session.Header
	if err := json.Unmarshal(scanner.Bytes(), &h); err != nil {
		return session.Header{}, err
	}
	return h, nil
}

// countDurableEvents returns how many event lines (i.e. excluding the
// header) exist in path, or 0 if the file doesn't exist yet — Append's
// fallback when durableCount has no cached entry for a session.
func countDurableEvents(path string) (int, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("session-persistence: opening %q: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	n := -1 // the first line is the header, not an event
	for scanner.Scan() {
		n++
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("session-persistence: reading %q: %w", path, err)
	}
	if n < 0 {
		n = 0
	}
	return n, nil
}

// materializeSession performs a session's first-ever append: encode header
// + events into a temp file in the session's own directory, fsync it,
// then publish it atomically via a no-clobber hardlink (os.Link fails if
// the target already exists, so a race between two writers materializing
// the same session for the first time can't silently overwrite one).
func materializeSession(path string, header session.Header, events []session.Event) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("session-persistence: creating %q: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".session-*.jsonl.tmp")
	if err != nil {
		return fmt.Errorf("session-persistence: creating temp file in %q: %w", dir, err)
	}
	tmpPath := tmp.Name()
	published := false
	defer func() {
		tmp.Close()
		if !published {
			os.Remove(tmpPath)
		}
	}()

	headerLine, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("session-persistence: encoding header for %q: %w", header.ID, err)
	}
	if err := writeLine(tmp, headerLine); err != nil {
		return fmt.Errorf("session-persistence: writing header: %w", err)
	}
	for _, ev := range events {
		line, err := encodeEvent(ev)
		if err != nil {
			return err
		}
		if err := writeLine(tmp, line); err != nil {
			return fmt.Errorf("session-persistence: writing event %d: %w", ev.Seq, err)
		}
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("session-persistence: fsync %q: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("session-persistence: closing %q: %w", tmpPath, err)
	}

	if err := os.Link(tmpPath, path); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("session-persistence: %q already exists — a concurrent writer materialized it first: %w", path, err)
		}
		return fmt.Errorf("session-persistence: publishing %q: %w", path, err)
	}
	published = true // path now holds the content; the deferred cleanup must leave tmpPath's link count alone

	if dirF, err := os.Open(dir); err == nil {
		_ = dirF.Sync() // best-effort: not every OS/filesystem supports or requires fsync on a directory
		dirF.Close()
	}
	_ = os.Remove(tmpPath) // unlinks the temp name only; path (the other hardlink to the same content) remains
	return nil
}

// appendLines appends events to an already-materialized session file and
// fsyncs the result.
func appendLines(path string, events []session.Event) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("session-persistence: opening %q for append: %w", path, err)
	}
	defer f.Close()

	var buf bytes.Buffer
	for _, ev := range events {
		line, err := encodeEvent(ev)
		if err != nil {
			return err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("session-persistence: appending to %q: %w", path, err)
	}
	return f.Sync()
}

func writeLine(f *os.File, line []byte) error {
	if _, err := f.Write(line); err != nil {
		return err
	}
	_, err := f.Write([]byte("\n"))
	return err
}
