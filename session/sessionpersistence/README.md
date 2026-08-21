# sessionpersistence

A Go port of [`packages/session/session-persistence-jsonl`](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/session/session-persistence-jsonl)
from DeepSeek Harness: durable, crash-tolerant, append-only JSONL storage
for `core/session` sessions. Until this package, `core/session.Store` had
no persistence at all — every other package in this module that mentions
that fact (`llm/llmretry`, `llm/tokenmeter`, `core/sessionquery`) was
describing a real, load-bearing gap. This package is the first to close it.

## Usage

```go
store, err := sessionpersistence.Open("/var/lib/myapp/sessions")

// Durably record whatever a session has accumulated so far.
sess := session.New("", session.Header{CWD: "/work/proj"})
sess.Append("turn/start", 1)
// ... more Appends via the normal core/session API ...
err = store.Sync(sess) // or store.Append(sess.Header(), sess.Events()) directly

// Later, or in a fresh process:
sess, err = store.Load("session-1", "/work/proj")
// sess is a real, usable *session.Session — DeriveMessages(), Events(),
// Append all work exactly as they would on one that was never persisted.

// Discover what's on disk without knowing ids or cwds up front:
snaps, err := store.ListSnapshots()
for _, snap := range snaps {
    sess, err := store.LoadPath(snap.Path)
    // ...
}
```

`Sync` is the write path this port actually expects a caller to use in a
running server: call it after whatever point you consider a save boundary
(e.g. once per completed turn) rather than after every single event.

## What this port leaves out relative to the TS original

The TS original is genuinely two things bolted together: a storage format
(JSONL, optionally zstd-framed) and a generic `PersistenceCoordinator` that
the JSONL and SQLite backends both plug into via a small
`PersistenceBackend` hook interface, sharing write-batching, prepared
-session caching, HMR takeover, and legacy-format migration. This port only
ever has one backend, so building the generic coordinator layer for it
would be pure speculative generality — this package implements the
storage format directly, with the coordinator's *behavioral guarantees*
(atomic first-publish, append-only, torn-tail recovery, unanswered-tool
-call closing) folded straight into `Store`, not its abstraction.

Specifically cut, and why:

- **No zstd compression.** The TS original defaults to `.jsonl.zstd`
  frames and falls back to raw `.jsonl` only when `compression: 'none'` is
  configured. This port only implements that raw mode. Adding zstd would
  mean a new external dependency, and — separately from that — this port
  was built in an environment where a `go get` for a much smaller
  dependency (`modernc.org/sqlite`, for a different, since-abandoned
  feature) hung for over 20 minutes against two different module proxies;
  rather than risk the same for zstd, plain JSONL was kept as the only
  supported encoding. A caller who wants compression can still get most of
  the benefit by compressing the whole `root` directory out-of-band (e.g.
  a nightly `tar czf`) — that's not live-appendable the way `.jsonl.zstd`
  frames are, but this port never offered live-appendable compression to
  begin with.
- **No packed chunk-run line compaction.** The TS original can fold 3+
  consecutive same-kind `assistant/chunk` deltas into one compact line
  (measured ~60% smaller logs on real sessions). Pure storage-size
  optimization, orthogonal to correctness — one JSON line per event always,
  here.
- **No background write-batching coordinator.** The TS original queues
  events and flushes on a fixed debounce window
  (`writeBatchMaxDelayMs`, default 200ms) so a burst of events costs one
  fsync instead of many. This port's `Append`/`Sync` write and fsync
  synchronously, on the caller's own goroutine, every time they're called —
  matching this codebase's already-established "poll/pull, don't
  push/subscribe" convention (see `web/README.md`'s own note on why its SSE
  stream polls `Session.Events()` rather than subscribing to a push
  source: `core/session.Session` has no event-emitted hook for either
  package to subscribe to). A caller that wants batching controls it
  themselves, by choosing how often to call `Sync`.
- **No `Prepare`/`inspect`/`readFrom`/HMR takeover.** These exist in the TS
  original to serve a coordinator managing many concurrent readers/writers
  across process reloads (Node's HMR) and hot ownership handoff between a
  live in-memory session and a cold on-disk one. This port has no hot
  -module-reload concept at all (that's a dev-server-specific idea, not
  something a compiled Go binary has), and `Store` doesn't track "is this
  session live somewhere else in the process" — a caller owns that
  question themselves, the same way `core/session.Store` already expects
  a caller to know which session it's touching.
- **Crash-recovery closer synthesis is narrower.** The TS original
  synthesizes `tool/result` (for a call with no result), `step/end`, and
  `turn/end {interrupted}` closers to balance a torn-off open turn. This
  port only synthesizes the `tool/result` — verified against this
  codebase's actual runtime, not assumed: `core/agentloop.Agent`'s
  `NewAgent` seeds its next turn number from `Session.LastTurn()`, which
  only reads the highest `turn/start` ever seen and does not care whether
  it was ever closed (`core/session/session.go`'s `LastTurn`), and `Agent`
  never attempts to resume mid-turn — every `Followup` opens a fresh next
  turn. A dangling `turn/start`/`step/start` with no matching `.../end` is
  therefore inert here: it doesn't confuse resume, doesn't appear in
  `DeriveMessages()` (turn/step boundaries aren't surface-eligible at all),
  and costs nothing to leave unclosed. An **unanswered `tool/call` is
  different** — if left alone, `DeriveMessages()` would still surface the
  assistant message that issued it with no accompanying tool response,
  which most real chat-completion APIs reject outright as a malformed
  request. That's the one repair this port treats as load-bearing, and the
  one it performs — see `recover.go`.
- **One process-wide mutex, not per-session write coordination.** The TS
  original's coordinator serializes per session id, allowing concurrent
  writers across different sessions to proceed independently. `Store` here
  holds one lock across every operation on every session — simpler,
  correct, and a real throughput ceiling only if a deployment is
  concurrently writing to many different sessions at once, which this
  demo-scale port doesn't need to optimize for.
- **`Snapshot.Size`+`ModTime` instead of device+inode+size+mtime.** The TS
  original's revision identity uses `stat`'s device and inode numbers
  specifically so two different files can never collide onto the same
  revision token even if size and mtime happen to match. Device/inode are
  POSIX-specific (`syscall.Stat_t`, unavailable in the same shape on
  Windows) and would need per-OS branches to expose portably; size+mtime
  is close enough for this port's actual use (deciding whether a
  previously-seen file might have changed) without that complexity.
- **No `agentPreset` field, no legacy-format migration, no HMR-specific
  ownership transfer.** None of `agentPreset` (this port has no deployment
  -composition/preset concept — every session in this codebase already
  gets its tools and system prompt from the same fixed construction, see
  `web/server.go`'s `buildAgent`), pre-format-version records, or
  hot-reload-driven ownership handoff exist anywhere else in this port, so
  there's nothing for this package to migrate from or hand off to.

None of these change what's actually persisted for the event types this
codebase produces — `codec.go`'s `decodeEventData` reconstructs the exact
concrete Go type (`session.Message`, `session.ContentBlock`, plain `int`)
each event's `Data` needs to keep working with `core/session.DeriveEventMessage`
and everything downstream of it, verified in `codec_test.go` by round
-tripping every event type `core/agentloop` and `plan/planmode` actually
append and confirming `DeriveEventMessage` still recognizes the result. An
event type this package has never seen a producer's Go struct for still
round-trips too, just as a generic decoded JSON shape rather than that
producer's exact type — the same "best-effort for anything we don't
specifically recognize" fallback `core/sessionquery`'s own text extraction
already uses for the same reason.
