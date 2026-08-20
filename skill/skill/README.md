# skill

A Go port of [`packages/skill/skill`](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/skill/skill/src/index.ts)
(`dsh-skill`) from DeepSeek Harness: a registry for discovering and loading
"skills" — named, providered instruction bundles a model or a user can
invoke — through a pluggable `Provider` model, plus embedded "runtime"
skills registered directly in code.

See [`../filesystem/`](../filesystem/) for a real `Provider` implementation
(a port of `packages/skill/skill-filesystem`) — local `SKILL.md` bundles and
flat Markdown files, with genuine YAML frontmatter.

## Ported

- **The vocabulary**: `InvocationPolicy`, `ResourceBase` (directory/URL
  /opaque), `Summary` (discovery result), `Candidate` (one provider's offer,
  with `Rank` and an opaque `Locator`), `Definition` (full loaded skill),
  `Registration` (what `Register` accepts for an embedded skill).
- **`Provider`**: `Name()`, `List(ctx, opts) (ProviderObservation, error)`,
  `Get(ctx, candidate, opts) (*Definition, error)` — discover, then load on
  demand. `Get` is called fresh every time (never cached by the registry
  itself), matching the TS original's "providers load body on every `get()`
  call."
- **The winning-candidate algorithm** (`collect` in `registry.go`): every
  runtime skill plus every registered provider's `List` result, sorted by
  `(Rank, provider registration order, within-provider order)` and
  deduplicated by name — first occurrence after that sort wins, later
  duplicates are silently discarded. A `Rank` of 250 for runtime skills is
  fixed, not automatic priority: a provider candidate with a lower `Rank`
  still wins over a same-name runtime skill (see
  `TestListLowerRankWinsOverRuntimeSameName`).
- **`Registry.Provider` stamping**: the registry — not the provider —
  authoritatively sets each collected candidate's `Summary.Provider` field
  from `Provider.Name()` (or `"runtime"` for an embedded skill), the same
  way it already had to for runtime skills. A provider that forgets to set
  its own name on a `Candidate`, or gets it wrong, still can't lie about
  whose candidate this is. This was a real gap the port's own tests caught
  — the first draft trusted each `Candidate.Summary.Provider` as given,
  which meant an honest provider that never bothered to fill the field
  (there's no reason it should have to — the registry already knows) turned
  every one of its candidates' `Provider` into an empty string.
- **`Register`'s tolerant-duplicate semantics**: a second `Register` call
  for a name already taken is a no-op (a disposer that does nothing),
  *not* an error — matching "duplicates log warning" rather than fail.
  `RegisterProvider`'s duplicate provider name is the opposite: a hard
  error, matching the TS original's own asymmetry between the two.
- **`Get`'s validation and cache-invalidation-on-mismatch**: if the
  `Definition` a provider's `Get` returns doesn't actually match the
  `name` requested, the registry treats its own cached candidate
  collection as stale, invalidates it, and returns `(nil, nil)` rather than
  the mismatched result.
- **`RenderSkillContent`**: the `<skill_content name="…">` /
  `<skill_resources>` / `<skill_instructions>` XML block, including the
  exact per-context escaping rules — the `name` attribute escapes
  `& " <` (never `>`, never ambiguous inside an attribute), resource-hint
  text escapes `& < >` (never `"`), and the skill body is embedded
  **verbatim, unescaped** — a skill's own instructions may legitimately
  contain markup, and escaping them would corrupt the very thing being
  presented.
- **The candidate-collection cache**: `Get` caches the winning-candidate
  collection per `CWD` so resolving which provider owns a name doesn't
  force a fresh discovery on every single load; `Snapshot`/`List` never
  read or write this cache — they're always fresh, matching the TS
  original's explicit "never cached" for the public listing methods versus
  its separate "completed catalogs cached" note for the internal
  candidate-resolution path `Get` uses.

## Deliberate simplifications vs. the TS original

- **No Cordis scope/layer system**: TS's registry is layered per agent
  scope, with a nearest-scope-wins merge across the chain on every read.
  This codebase has no equivalent per-agent context system (the same call
  already made for `core/tools`'s dropped `presentAs`/`restrict`) — this
  port has one global `Registry`. A caller that wants per-agent skill
  visibility can run one `Registry` per agent, the same way `core/tools`
  suggests doing for per-agent tool visibility.
- **No real LRU cache eviction**: TS bounds its candidate cache at 128
  entries with real least-recently-used eviction. This port keeps the same
  default cap but, once it's reached, simply stops caching new entries
  rather than evicting an old one — a `Get` call still works, just without
  the cache's speedup, once more than 128 distinct `CWD` values have been
  looked up in one process's lifetime. Good enough for what this package is
  actually for (a handful of working directories per process), not worth a
  real LRU for.
- **No `ask`-style approval integration, no `tool-skill` consumer**:
  presenting skills to a model, injecting loaded content at a user gesture
  boundary, and stamping a `skill-invocation` message-source kind are all
  `@deepseek-ai/dsh-tool-skill`'s job, a separate package this request
  didn't ask for. `examples/skill/` shows the shape such a consumer would
  take (a `list_skills`/`load_skill` tool pair) without porting that
  package's own policy.
- **`OnChange` carries no data**: matches the TS original's own
  `skills/change` event exactly (no diff — refetch `Snapshot` yourself),
  not a simplification, but worth stating since it's easy to expect
  otherwise.

## Run

```sh
go build ./...
go test ./... -race -v
```

See [`../../examples/skill/`](../../examples/skill/) for a runnable demo: an
embedded runtime skill plus a real `skill/filesystem.Provider` (a `SKILL.md`
directory bundle with genuine YAML frontmatter), driving a real DeepSeek +
ReAct loop where the model discovers, loads, and actually follows a loaded
skill's instructions.
