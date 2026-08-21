package systemprompt

import (
	"fmt"
	"strings"
)

// RenderPrompt interpolates every section's {{variable}} references
// against assembly.Variables, drops any section whose fully-interpolated
// text is empty, and joins what's left with one blank line between
// sections, in the sections' own order (Builder.Assemble has already
// sorted them, and reduced them to just one if a Complete section won).
//
// Strict interpolation rules, matching the TS original's own "explicit
// failure beats a malformed prompt" stance: an unknown reference (a name
// never registered via Builder.Variable) or a malformed {{...}} group
// (its content isn't a bare identifier — e.g. "{{{model}}}", whose inner
// content is "{model") are both errors. A "{{" with no "}}" anywhere later
// in the text is not an error: it passes through as a literal, since some
// section text may legitimately want to show literal double braces.
// Replacement text is never itself re-scanned for further interpolation.
func RenderPrompt(assembly Assembly) (string, error) {
	rendered := make([]string, 0, len(assembly.Sections))
	for _, s := range assembly.Sections {
		text, err := interpolate(s.Text, assembly.Variables)
		if err != nil {
			return "", fmt.Errorf("systemprompt: section %q: %w", s.Name, err)
		}
		if text == "" {
			continue
		}
		rendered = append(rendered, text)
	}
	return strings.Join(rendered, "\n\n"), nil
}

// interpolate scans text once, left to right, replacing each well-formed
// {{name}} group with vars[name].
func interpolate(text string, vars map[string]string) (string, error) {
	var b strings.Builder
	i := 0
	for i < len(text) {
		if i+1 < len(text) && text[i] == '{' && text[i+1] == '{' {
			rest := text[i+2:]
			closeIdx := strings.Index(rest, "}}")
			if closeIdx == -1 {
				// No closing "}}" anywhere later in the text: literal
				// passthrough of everything from here to the end.
				b.WriteString(text[i:])
				break
			}
			name := rest[:closeIdx]
			if !isBareIdentifier(name) {
				return "", fmt.Errorf("malformed {{%s}} group", name)
			}
			value, known := vars[name]
			if !known {
				return "", fmt.Errorf("unknown variable {{%s}}", name)
			}
			b.WriteString(value)
			i = i + 2 + closeIdx + 2
			continue
		}
		b.WriteByte(text[i])
		i++
	}
	return b.String(), nil
}

// isBareIdentifier reports whether s is a valid {{...}} reference body:
// non-empty, ASCII letters/digits/underscore, not starting with a digit.
func isBareIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			continue
		case c >= '0' && c <= '9' && i > 0:
			continue
		default:
			return false
		}
	}
	return true
}
