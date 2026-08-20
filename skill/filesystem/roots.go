package filesystem

import (
	"os"
	"path/filepath"
)

// Rank values for each root source — lower sorts first in skill.Registry's
// winning-candidate order, so project-local skills shadow user-global ones
// of the same name, and both shadow anything at a lower priority.
const (
	RankProjectDsh    = 100
	RankProjectAgents = 200
	RankCustom        = 300
	RankUserDsh       = 400
	RankUserAgents    = 500
)

type rootSpec struct {
	path string
	rank int
}

// Config configures a Provider's discovery. The zero value is not usable
// directly for default-root discovery — use DefaultConfig(), which sets
// IncludeDefaultRoots: true.
//
// Not ported: the TS original's sixth root, a bundled-skills directory
// shipped with the deepseek-harness product itself
// ($DSH_BUNDLED_SKILL_DIR) — this Go module ships no bundled skills, so
// there is nothing for that root to point at. See README.md.
type Config struct {
	// IncludeDefaultRoots controls whether the project/user roots below are
	// scanned at all. false isolates discovery to CustomSkillDirs only —
	// useful for tests, or a deployment that wants no ambient filesystem
	// skills at all.
	IncludeDefaultRoots bool
	// DshHome overrides where the user-dsh root is computed from. Defaults
	// to $DSH_HOME, then ~/.dsh.
	DshHome string
	// AgentsHome overrides where the user-agents root is computed from.
	// Defaults to $DSH_AGENTS_HOME, then ~/.agents.
	AgentsHome string
	// CustomSkillDirs are additional roots, ranked between the project and
	// user roots (RankCustom).
	CustomSkillDirs []string
}

func DefaultConfig() Config { return Config{IncludeDefaultRoots: true} }

func (c Config) resolvedDshHome() string {
	if c.DshHome != "" {
		return c.DshHome
	}
	if v := os.Getenv("DSH_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".dsh")
}

func (c Config) resolvedAgentsHome() string {
	if c.AgentsHome != "" {
		return c.AgentsHome
	}
	if v := os.Getenv("DSH_AGENTS_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agents")
}

// projectRoot walks upward from cwd looking for a ".git" entry, returning
// the nearest ancestor that has one. Falls back to cwd itself when no
// ancestor does — "without one, the current cwd is used."
func projectRoot(cwd string) string {
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd
		}
		dir = parent
	}
}

// roots computes the ranked list of directories to scan for cwd. A blank
// cwd omits the two project-relative roots entirely (there's no directory
// to walk up from) but still includes custom and user roots.
func (c Config) roots(cwd string) []rootSpec {
	var roots []rootSpec
	if c.IncludeDefaultRoots && cwd != "" {
		root := projectRoot(cwd)
		roots = append(roots,
			rootSpec{path: filepath.Join(root, ".dsh", "skills"), rank: RankProjectDsh},
			rootSpec{path: filepath.Join(root, ".agents", "skills"), rank: RankProjectAgents},
		)
	}
	for _, dir := range c.CustomSkillDirs {
		roots = append(roots, rootSpec{path: dir, rank: RankCustom})
	}
	if c.IncludeDefaultRoots {
		roots = append(roots,
			rootSpec{path: filepath.Join(c.resolvedDshHome(), "skills"), rank: RankUserDsh},
			rootSpec{path: filepath.Join(c.resolvedAgentsHome(), "skills"), rank: RankUserAgents},
		)
	}
	return roots
}
