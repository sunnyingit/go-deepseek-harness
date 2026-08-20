// Frontmatter parsing: the "---\n<yaml>\n---\n<body>" split, and the field
// rules a skill file's YAML header must satisfy. Port of the parsing half
// of packages/skill/skill-filesystem/src/index.ts.
package filesystem

import (
	"fmt"
	"strings"

	"go-harness/skill/skill"
	"gopkg.in/yaml.v3"
)

type parsedFrontmatter struct {
	Name        string
	Description string
	WhenToUse   string
	Metadata    map[string]any
	Invocation  skill.InvocationPolicy
}

// splitFrontmatter splits raw into its YAML header and body. ok is false
// when raw doesn't open with a "---" delimiter line, or never closes one —
// either way the whole file is rejected, matching the TS original (no
// partial/best-effort parse of a malformed file).
func splitFrontmatter(raw string) (yamlText, body string, ok bool) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(raw, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return "", "", false
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			return strings.Join(lines[1:i], "\n"), strings.Join(lines[i+1:], "\n"), true
		}
	}
	return "", "", false
}

// legacyFrontmatterKeys rejects the camelCase names an earlier iteration of
// this format used, directing an author to the current kebab-case field —
// "invalid data could expose a skill on a disabled surface," so a
// recognizable-but-wrong key fails closed instead of being silently ignored.
var legacyFrontmatterKeys = map[string]string{
	"disableModelInvocation": "disable-model-invocation",
	"modelInvocable":         "disable-model-invocation",
	"userInvocable":          "user-invocable",
}

// parseFrontmatter validates and extracts yamlText's fields. Returning an
// error here always means "reject this whole file" — callers (List/Get)
// treat it as "this candidate doesn't exist," not a fatal condition for
// discovery as a whole.
func parseFrontmatter(yamlText string) (parsedFrontmatter, error) {
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(yamlText), &raw); err != nil {
		return parsedFrontmatter{}, fmt.Errorf("invalid YAML frontmatter: %w", err)
	}
	if raw == nil {
		return parsedFrontmatter{}, fmt.Errorf("empty frontmatter")
	}
	for legacy, canonical := range legacyFrontmatterKeys {
		if _, present := raw[legacy]; present {
			return parsedFrontmatter{}, fmt.Errorf("%q is not a valid frontmatter field; use %q instead", legacy, canonical)
		}
	}

	name, _ := raw["name"].(string)
	description, _ := raw["description"].(string)
	if name == "" || description == "" {
		return parsedFrontmatter{}, fmt.Errorf("frontmatter must have a non-empty name and description")
	}
	whenToUse, _ := raw["whenToUse"].(string)

	// A non-object metadata value is ignored, not rejected — the skill
	// itself is still valid, it just carries no structured metadata.
	var metadata map[string]any
	if m, ok := raw["metadata"].(map[string]any); ok {
		metadata = m
	}

	policy := skill.InvocationPolicy{ModelInvocable: true, UserInvocable: true}
	if v, present := raw["disable-model-invocation"]; present {
		disabled, err := parseBooleanLike(v)
		if err != nil {
			return parsedFrontmatter{}, fmt.Errorf("disable-model-invocation: %w", err)
		}
		policy.ModelInvocable = !disabled
	}
	if v, present := raw["user-invocable"]; present {
		invocable, err := parseBooleanLike(v)
		if err != nil {
			return parsedFrontmatter{}, fmt.Errorf("user-invocable: %w", err)
		}
		policy.UserInvocable = invocable
	}

	return parsedFrontmatter{
		Name: name, Description: description, WhenToUse: whenToUse,
		Metadata: metadata, Invocation: policy,
	}, nil
}

// parseBooleanLike accepts a real boolean, a case-insensitive
// true/yes/on/1 or false/no/off/0 string, or the integers 1/0 — anything
// else is rejected outright rather than falling back to a permissive
// default, matching "wrong-typed optional fields are omitted" for the rest
// of the format but a hard reject specifically for the two fields that
// gate invocation policy.
func parseBooleanLike(v any) (bool, error) {
	switch x := v.(type) {
	case bool:
		return x, nil
	case string:
		switch strings.ToLower(x) {
		case "true", "yes", "on", "1":
			return true, nil
		case "false", "no", "off", "0":
			return false, nil
		}
	case int:
		if x == 1 {
			return true, nil
		}
		if x == 0 {
			return false, nil
		}
	}
	return false, fmt.Errorf("expected a boolean-like value, got %#v", v)
}
