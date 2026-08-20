# filesystem

A Go port of [`packages/skill/skill-filesystem`](https://github.com/deepseek-ai/deepseek-harness/tree/master/packages/skill/skill-filesystem)
(`dsh-skill-filesystem`) from DeepSeek Harness: a [`skill.Provider`](../skill/)
that discovers skills from local directories.

This package imports `go-harness/skill/skill`; nothing imports back.

**This is the module's first external dependency**: `gopkg.in/yaml.v3`, for
real YAML frontmatter parsing (an arbitrary, possibly-nested `metadata`
object genuinely needs a real parser — a hand-rolled key:value scanner
would be a worse, less faithful port of a format that's supposed to accept
real YAML).

## Skill file format

Two layouts, both frontmatter + body:

```
<root>/<name>/SKILL.md      # directory bundle — resourceBase is the bundle's own dir
<root>/<name>.md            # flat file — resourceBase is the shared root
```

```markdown
---
name: my-skill
description: what it does
whenToUse: optional guidance on when to reach for it
metadata:
  anything: goes here
disable-model-invocation: false
user-invocable: true
---
The skill's body — everything after the closing `---`.
```

`name`/`description` are required (a missing or empty value rejects the
whole file); `whenToUse` and `metadata` are optional; the two invocation
fields default to "invocable" when absent
(`ModelInvocable`/`UserInvocable: true`). `disable-model-invocation`
/`user-invocable` accept a real boolean or a case-insensitive
`true`/`yes`/`on`/`1` / `false`/`no`/`off`/`0` string — anything else is
rejected outright, not defaulted permissively ("invalid data could expose a
skill on a disabled surface"). The three legacy camelCase keys
(`disableModelInvocation`, `modelInvocable`, `userInvocable`) are rejected
by name, pointing at the current kebab-case field, rather than silently
accepted or ignored.

**A YAML gotcha this package's own demo hit during development**: an
unquoted scalar value containing `: ` (colon-space) is invalid YAML plain
scalar syntax — `description: fix(login): handle the crash` fails to parse
("mapping values are not allowed in this context") and the whole file is
silently skipped, exactly as designed. Quote a description that contains a
colon: `description: "fix(login): handle the crash"`.

## Discovery

Five ranked roots (lower rank wins a same-name tie, matching `skill.Registry`'s
winning-candidate order):

| Rank | Source | Path |
|---|---|---|
| 100 | project-dsh | `<projectRoot>/.dsh/skills` |
| 200 | project-agents | `<projectRoot>/.agents/skills` |
| 300 | custom | `Config.CustomSkillDirs` |
| 400 | user-dsh | `Config.DshHome` (or `$DSH_HOME`, or `~/.dsh`) `/skills` |
| 500 | user-agents | `Config.AgentsHome` (or `$DSH_AGENTS_HOME`, or `~/.agents`) `/skills` |

`projectRoot` is the nearest ancestor of `LookupOptions.CWD` containing
`.git`; without one, `CWD` itself is used. `Config.IncludeDefaultRoots:
false` scans only `CustomSkillDirs`, isolating discovery — useful for tests
or a deployment that wants no ambient filesystem skills.

Discovery is one level deep only — no `**/SKILL.md` nested search. A
missing root is valid empty state, not an error; any other read failure
marks that `List` call's observation incomplete but keeps scanning the
remaining roots (one bad root must not hide what a good one found); a
malformed or unreadable individual skill file is skipped, not fatal to
discovery as a whole.

`Get` re-reads and re-parses the file fresh every call — no body cache of
its own. If the file's frontmatter `name` has changed since discovery (a
rename), this provider does nothing special about it: it just returns a
`Definition` carrying the *current* name, and `skill.Registry.Get` — one
layer up — is what notices the mismatch against the requested name and
invalidates its own cache accordingly. See `TestGetRenameIsCaughtByTheRegistryLayer`.

## Deliberate simplifications vs. the TS original

- **No real file watching**: the TS original watches every root via
  Chokidar — batched, debounced, symlink-aware, with an LRU of per-project
  watchers and synchronous invalidation hooks from first-party `write`/`edit`
  tools. This port has none of that; `watch.go`'s `Register` offers a
  `pollInterval` instead — when positive, a goroutine just calls
  `control.Invalidate()` on a fixed timer, clearing `skill.Registry`'s `Get`
  cache periodically regardless of whether anything actually changed. This
  matters less than it sounds: `Snapshot`/`List` are *always* fresh in
  `skill.Registry` (see its own README) — only `Get`'s internal
  candidate-resolution cache can go stale between calls, and only until the
  next poll (or a manual `Invalidate()`) clears it.
- **No bundled-skills root**: the TS original's sixth root points at skills
  shipped with the `deepseek-harness` product itself
  (`$DSH_BUNDLED_SKILL_DIR`). This Go module ships no bundled skills, so
  there's nothing for that root to point at.
- **No `ctx.fs` service indirection**: the TS original can route through a
  Cordis filesystem service (`ctx.fs.listDir`/`readText`) or fall back to
  Node's own `fs`. This port always uses `os`/`io/fs` directly — the same
  simplification every other package in this codebase already makes for
  Cordis service abstractions.

## Run

```sh
go build ./...
go test ./... -race -v
```

See [`../../examples/skill/`](../../examples/skill/) for a runnable demo: a
real `SKILL.md` directory bundle (with genuine YAML frontmatter, parsed by
this package, not a hand-rolled stand-in) mixed with an embedded runtime
skill, driving a real DeepSeek + ReAct loop.
