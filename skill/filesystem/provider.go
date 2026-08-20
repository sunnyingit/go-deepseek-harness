// Package filesystem is a Go port of packages/skill/skill-filesystem
// (dsh-skill-filesystem) from DeepSeek Harness: a skill.Provider that
// discovers skills from local directories — either a bundle
// (<name>/SKILL.md) or a flat Markdown file (<name>.md) — across a ranked
// set of project and user roots.
//
// Deliberate simplifications vs. the TS original are documented in README.md.
package filesystem

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go-harness/skill/skill"
)

// DefaultProviderName is what Register uses when no name is given.
const DefaultProviderName = "filesystem"

const skillFileName = "SKILL.md"

// Provider implements skill.Provider over Config's ranked directories.
type Provider struct {
	name   string
	config Config
}

// New returns a Provider ready to register — most callers want Register,
// which also handles skill.Registry.RegisterProvider's Control plumbing.
func New(name string, config Config) *Provider {
	if name == "" {
		name = DefaultProviderName
	}
	return &Provider{name: name, config: config}
}

func (p *Provider) Name() string { return p.name }

// List scans every configured root for the two skill layouts: a directory
// bundle (<root>/<name>/SKILL.md) and a flat Markdown file
// (<root>/<name>.md). Nested discovery is deliberately excluded — only one
// level deep under each root, matching the TS original.
//
// A missing root is valid empty state, not an error. Any other read
// failure marks the observation incomplete but keeps scanning the
// remaining roots — one bad root must not hide skills a good one found. A
// malformed or unreadable individual skill file is skipped silently; it
// does not affect completeness (the TS original logs a warning here this
// port has no logger to route through).
func (p *Provider) List(ctx context.Context, opts skill.LookupOptions) (skill.ProviderObservation, error) {
	var candidates []skill.Candidate
	complete := true

	for _, root := range p.config.roots(opts.CWD) {
		if ctx.Err() != nil {
			return skill.ProviderObservation{}, ctx.Err()
		}
		entries, err := os.ReadDir(root.path)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				complete = false
			}
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				dir := filepath.Join(root.path, e.Name())
				if c, ok := p.loadCandidate(filepath.Join(dir, skillFileName), root.rank, dir); ok {
					candidates = append(candidates, c)
				}
				continue
			}
			if !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if c, ok := p.loadCandidate(filepath.Join(root.path, e.Name()), root.rank, root.path); ok {
				candidates = append(candidates, c)
			}
		}
	}

	return skill.ProviderObservation{Candidates: candidates, Complete: complete}, nil
}

// loadCandidate reads and parses the frontmatter of one skill file for the
// discovery catalog — path's body is not needed yet (Get reads it fresh
// later), only the header.
func (p *Provider) loadCandidate(path string, rank int, resourceDir string) (skill.Candidate, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return skill.Candidate{}, false
	}
	yamlText, _, ok := splitFrontmatter(string(data))
	if !ok {
		return skill.Candidate{}, false
	}
	parsed, err := parseFrontmatter(yamlText)
	if err != nil {
		return skill.Candidate{}, false
	}
	return skill.Candidate{
		Summary: skill.Summary{
			Name: parsed.Name, Description: parsed.Description, WhenToUse: parsed.WhenToUse,
			Invocation:   parsed.Invocation,
			ResourceBase: &skill.ResourceBase{Kind: skill.ResourceDirectory, Path: resourceDir},
		},
		Rank: rank, Locator: path, Path: path, Metadata: parsed.Metadata,
	}, true
}

// Get re-reads and re-parses candidate's file fresh — no cache of its own.
// Body edits since List take effect immediately; nothing needs
// invalidating for that case. If the file's current frontmatter name no
// longer matches what candidate was discovered as (renamed on disk since
// List ran), this returns a Definition carrying the *new* name — it's
// skill.Registry.Get, one layer up, that notices the mismatch against the
// requested name and invalidates its own cache accordingly; this provider
// doesn't need its own stale-rename detection to get that behavior.
func (p *Provider) Get(_ context.Context, candidate skill.Candidate, _ skill.LookupOptions) (*skill.Definition, error) {
	path, _ := candidate.Locator.(string)
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	yamlText, body, ok := splitFrontmatter(string(data))
	if !ok {
		return nil, nil
	}
	parsed, err := parseFrontmatter(yamlText)
	if err != nil {
		return nil, nil
	}
	return &skill.Definition{
		Summary: skill.Summary{
			Name: parsed.Name, Description: parsed.Description, WhenToUse: parsed.WhenToUse,
			Invocation: parsed.Invocation, ResourceBase: candidate.ResourceBase,
		},
		Content: strings.TrimSpace(body), Path: path, Metadata: parsed.Metadata,
	}, nil
}
