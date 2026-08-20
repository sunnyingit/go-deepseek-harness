// Port of index.ts's renderSkillContent/escapeText: renders a loaded
// Definition as the canonical <skill_content> XML block a consumer injects
// into a message.
package skill

import (
	"fmt"
	"strings"
)

// IsModelInvocable reports whether s's policy lets a model invoke it.
func IsModelInvocable(s Summary) bool { return s.Invocation.ModelInvocable }

// IsUserInvocable reports whether s's policy lets a user invoke it directly.
func IsUserInvocable(s Summary) bool { return s.Invocation.UserInvocable }

// RenderSkillContent renders def as the canonical <skill_content> block:
// name attribute-escaped, resource hint text-escaped, body embedded
// verbatim (a skill's own instructions may legitimately contain markup —
// escaping them would corrupt the very thing being presented).
func RenderSkillContent(def Definition) string {
	var b strings.Builder
	b.WriteString(`<skill_content name="`)
	b.WriteString(escapeAttr(def.Name))
	b.WriteString("\">\n")
	b.WriteString("  <skill_resources>\n")
	b.WriteString(escapeText(resourceHintText(def)))
	b.WriteString("\n  </skill_resources>\n\n")
	b.WriteString("  <skill_instructions>\n")
	b.WriteString(def.Content)
	b.WriteString("\n  </skill_instructions>\n")
	b.WriteString("</skill_content>")
	return b.String()
}

// resourceHintText composes the (not-yet-escaped) prose telling a consumer
// how to resolve resources this skill's body mentions.
func resourceHintText(def Definition) string {
	rb := def.ResourceBase
	if rb == nil {
		return fmt.Sprintf("Resources for this skill are managed by provider %q.\nLoad referenced resources only as needed.", def.Provider)
	}
	switch rb.Kind {
	case ResourceDirectory:
		return fmt.Sprintf(
			"Base directory for this skill: %s\nResolve relative paths mentioned by this skill against the base directory before using them. Load referenced resources only as needed.",
			rb.Path)
	case ResourceURL:
		return fmt.Sprintf(
			"Base URL for this skill: %s\nResolve relative URLs mentioned by this skill against the base URL before using them. Load referenced resources only as needed.",
			rb.URL)
	case ResourceOpaque:
		return fmt.Sprintf("Resources for this skill: %s\nLoad referenced resources only as needed.", rb.Description)
	default:
		return fmt.Sprintf("Resources for this skill are managed by provider %q.\nLoad referenced resources only as needed.", def.Provider)
	}
}

// escapeAttr escapes text embedded inside a double-quoted XML attribute:
// & first (so it doesn't double-escape the entities this itself produces),
// then " and <. Matches the TS original's attribute-escaping rule exactly
// — notably not >, which is never ambiguous inside an attribute value.
var escapeAttrReplacer = strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;")

func escapeAttr(s string) string { return escapeAttrReplacer.Replace(s) }

// escapeText escapes text embedded as XML element content: &, <, and >.
// Not ": a literal quote is never ambiguous outside an attribute value.
var escapeTextReplacer = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func escapeText(s string) string { return escapeTextReplacer.Replace(s) }
