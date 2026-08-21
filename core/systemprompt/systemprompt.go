// Package systemprompt is a Go port of packages/core/system-prompt from
// DeepSeek Harness: composable, ordered prompt sections plus strict
// {{variable}} interpolation, assembled into one final prompt string
// alongside an ordered tool-schema list. What's cut relative to the TS
// original — the live ctx.systemPrompt service, agent-scoped layer
// shadowing, the system-prompt/assemble waterfall, dynamic runtime-context
// snapshots, and the invariant companion — is documented in README.md.
package systemprompt

import (
	"fmt"
	"sort"

	"go-harness/core/agentloop"
)

// Order bands a section's Order value conventionally falls into, mirroring
// the TS original's own convention — this package doesn't enforce the
// ranges, they're just where the pieces it contributes itself (harness
// identity, deployment persona) land relative to a caller's own sections.
const (
	OrderHarnessIdentity   = -100
	OrderDeploymentPersona = 0
	OrderToolGuidanceMin   = 100
	OrderToolGuidanceMax   = 199
)

// ToolOrderRest is the placement marker WithToolOrder's list must contain
// exactly once: every tool schema not otherwise named in the list is
// inserted, in name order, at this marker's position.
const ToolOrderRest = "<unlisted-tools>"

// DefaultHarnessIdentity is the fixed opening line WithHarnessIdentity(true)
// (the Builder default) contributes at OrderHarnessIdentity.
const DefaultHarnessIdentity = "You are an AI agent powered by DeepSeek Harness."

// Section is one named, ordered contribution to a prompt. Sections render
// in ascending Order, ties broken by contribution order. A Complete
// section — Assemble rejects more than one — suppresses every other
// section, becoming the entire rendered prompt verbatim (after its own
// variable interpolation) instead of being concatenated with the rest.
type Section struct {
	Name     string
	Order    int
	Text     string
	Complete bool
}

// Assembly is Builder.Assemble's result: every contributed section, not
// yet rendered (RenderPrompt does the interpolation and joining), the
// ordered tool schema list, and the variable values sections may reference.
type Assembly struct {
	Sections  []Section
	Tools     []agentloop.ToolSchema
	Variables map[string]string
}

// Builder collects sections, variables, and tool schemas for one prompt.
// Unlike the TS original's ctx.systemPrompt service — a long-lived
// registry serving many concurrent agents, with per-agent layers that
// shadow global contributions and are re-queried on every step — this
// port's core/agentloop.Agent takes one static system string at
// construction and never reassembles it. A Builder is therefore built
// fresh, used once via Assemble, and discarded; see README.md.
type Builder struct {
	includeHarnessIdentity bool
	persona                string
	toolOrder              []string
	sections               []Section
	variables              map[string]string
	tools                  []agentloop.ToolSchema
}

// New returns a Builder with the TS original's own defaults:
// includeHarnessIdentity true, no persona, no explicit tool order
// (alphabetical).
func New() *Builder {
	return &Builder{includeHarnessIdentity: true, variables: map[string]string{}}
}

// WithHarnessIdentity toggles the fixed DefaultHarnessIdentity section —
// disable only when a deployment's own persona/sections already establish
// full identity, matching the TS original's own guidance for this switch.
func (b *Builder) WithHarnessIdentity(include bool) *Builder {
	b.includeHarnessIdentity = include
	return b
}

// WithPersona sets the deployment-persona text rendered at
// OrderDeploymentPersona. It is itself a template — {{variable}} groups in
// it are interpolated the same as any other section's text. An empty
// persona contributes no section at all, matching the TS original's own
// "" ⇒ omitted convention for its persona config key.
func (b *Builder) WithPersona(text string) *Builder {
	b.persona = text
	return b
}

// WithToolOrder sets an explicit tool ordering — see ToolOrderRest.
// Omitted or nil leaves tools in alphabetical order.
func (b *Builder) WithToolOrder(names ...string) *Builder {
	b.toolOrder = names
	return b
}

// Section contributes one prompt section. Panics on a duplicate name,
// mirroring the TS original's own "same layer, duplicate name" throw —
// sections are supplied by this process's own code, not untrusted input,
// so a caller that can't already guarantee uniqueness should check first.
func (b *Builder) Section(s Section) *Builder {
	for _, existing := range b.sections {
		if existing.Name == s.Name {
			panic(fmt.Sprintf("systemprompt: duplicate section name %q", s.Name))
		}
	}
	b.sections = append(b.sections, s)
	return b
}

// Variable registers name's value for {{name}} interpolation. A name
// registered with an empty value is still "known" to RenderPrompt — only
// a name never registered at all is rejected. This collapses the TS
// original's two distinct interpolation failures ("unknown reference" and
// "registered but no value for this assembly") into one, since this
// port's variables are plain resolved strings rather than per-assembly
// provider functions that can decline to answer — see the Builder doc
// comment for why providers aren't ported.
func (b *Builder) Variable(name, value string) *Builder {
	b.variables[name] = value
	return b
}

// Tools sets the candidate tool schemas WithToolOrder (if set) reorders.
func (b *Builder) Tools(schemas []agentloop.ToolSchema) *Builder {
	b.tools = schemas
	return b
}

// Assemble validates and returns the collected contributions: at most one
// Complete section (reducing Sections to just that one when present), and
// — if WithToolOrder was used — every listed name must name an actual
// tool, with exactly one ToolOrderRest marker and no duplicates.
func (b *Builder) Assemble() (Assembly, error) {
	sections := make([]Section, 0, len(b.sections)+2)
	if b.includeHarnessIdentity {
		sections = append(sections, Section{Name: "harness:identity", Order: OrderHarnessIdentity, Text: DefaultHarnessIdentity})
	}
	if b.persona != "" {
		sections = append(sections, Section{Name: "deployment:persona", Order: OrderDeploymentPersona, Text: b.persona})
	}
	sections = append(sections, b.sections...)

	// complete is a value copy, not a pointer into sections: sort.SliceStable
	// below reorders that slice's backing array in place, so a pointer taken
	// beforehand would end up aimed at whatever section the sort happened to
	// swap into that index — not necessarily the complete one anymore.
	var complete Section
	completeCount := 0
	for _, s := range sections {
		if s.Complete {
			completeCount++
			complete = s
		}
	}
	if completeCount > 1 {
		return Assembly{}, fmt.Errorf("systemprompt: %d complete sections contributed, want at most 1", completeCount)
	}

	sort.SliceStable(sections, func(i, j int) bool { return sections[i].Order < sections[j].Order })

	if completeCount == 1 {
		sections = []Section{complete}
	}

	tools, err := applyToolOrder(b.toolOrder, b.tools)
	if err != nil {
		return Assembly{}, err
	}

	vars := make(map[string]string, len(b.variables))
	for k, v := range b.variables {
		vars[k] = v
	}

	return Assembly{Sections: sections, Tools: tools, Variables: vars}, nil
}

// applyToolOrder reorders tools per order (see WithToolOrder/ToolOrderRest).
// A nil/empty order leaves tools sorted alphabetically by name, matching
// the TS original's own "absent ⇒ straight alphabetical" default.
func applyToolOrder(order []string, tools []agentloop.ToolSchema) ([]agentloop.ToolSchema, error) {
	byName := make(map[string]agentloop.ToolSchema, len(tools))
	for _, t := range tools {
		byName[t.Name] = t
	}

	if len(order) == 0 {
		sorted := append([]agentloop.ToolSchema(nil), tools...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
		return sorted, nil
	}

	restCount := 0
	restIdx := -1
	seen := make(map[string]bool, len(order))
	for i, name := range order {
		if name == ToolOrderRest {
			restCount++
			restIdx = i
			continue
		}
		if seen[name] {
			return nil, fmt.Errorf("systemprompt: tool order lists %q more than once", name)
		}
		seen[name] = true
	}
	if restCount != 1 {
		return nil, fmt.Errorf("systemprompt: tool order must contain exactly one %q marker, found %d", ToolOrderRest, restCount)
	}

	var unlisted []string
	for name := range byName {
		if !seen[name] {
			unlisted = append(unlisted, name)
		}
	}
	sort.Strings(unlisted)

	result := make([]agentloop.ToolSchema, 0, len(tools))
	for i, name := range order {
		if i == restIdx {
			for _, u := range unlisted {
				result = append(result, byName[u])
			}
			continue
		}
		schema, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("systemprompt: tool order names %q, which isn't among the known tools", name)
		}
		result = append(result, schema)
	}
	return result, nil
}
