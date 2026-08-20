package skill

import (
	"strings"
	"testing"
)

func TestRenderSkillContentNoResourceBase(t *testing.T) {
	def := Definition{Summary: Summary{Name: "my-skill", Provider: "files"}, Content: "do the thing"}
	got := RenderSkillContent(def)
	want := `<skill_content name="my-skill">
  <skill_resources>
Resources for this skill are managed by provider "files".
Load referenced resources only as needed.
  </skill_resources>

  <skill_instructions>
do the thing
  </skill_instructions>
</skill_content>`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderSkillContentDirectoryResourceBase(t *testing.T) {
	def := Definition{
		Summary: Summary{Name: "my-skill", ResourceBase: &ResourceBase{Kind: ResourceDirectory, Path: "/app/skills"}},
		Content: "# My Skill\n\nUse this skill to accomplish X.",
	}
	got := RenderSkillContent(def)
	want := `<skill_content name="my-skill">
  <skill_resources>
Base directory for this skill: /app/skills
Resolve relative paths mentioned by this skill against the base directory before using them. Load referenced resources only as needed.
  </skill_resources>

  <skill_instructions>
# My Skill

Use this skill to accomplish X.
  </skill_instructions>
</skill_content>`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderSkillContentURLResourceBase(t *testing.T) {
	def := Definition{Summary: Summary{Name: "api-skill", ResourceBase: &ResourceBase{Kind: ResourceURL, URL: "https://api.example.com"}}}
	got := RenderSkillContent(def)
	if !strings.Contains(got, "Base URL for this skill: https://api.example.com") {
		t.Fatalf("got:\n%s", got)
	}
	if !strings.Contains(got, "Resolve relative URLs mentioned by this skill against the base URL") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestRenderSkillContentOpaqueResourceBase(t *testing.T) {
	def := Definition{Summary: Summary{Name: "s3-skill", ResourceBase: &ResourceBase{Kind: ResourceOpaque, Description: "S3 bucket"}}}
	got := RenderSkillContent(def)
	if !strings.Contains(got, "Resources for this skill: S3 bucket") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestRenderSkillContentEscapesNameAttribute(t *testing.T) {
	def := Definition{Summary: Summary{Name: `a"b&c<d`}}
	got := RenderSkillContent(def)
	if !strings.Contains(got, `name="a&quot;b&amp;c&lt;d"`) {
		t.Fatalf("got:\n%s", got)
	}
}

func TestRenderSkillContentEscapesResourceHintText(t *testing.T) {
	def := Definition{Summary: Summary{Name: "x", ResourceBase: &ResourceBase{Kind: ResourceOpaque, Description: "A & B < C > D"}}}
	got := RenderSkillContent(def)
	if !strings.Contains(got, "A &amp; B &lt; C &gt; D") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestRenderSkillContentBodyIsNotEscaped(t *testing.T) {
	def := Definition{Summary: Summary{Name: "x"}, Content: "raw <tag> & \"quotes\" here"}
	got := RenderSkillContent(def)
	if !strings.Contains(got, "raw <tag> & \"quotes\" here") {
		t.Fatalf("body was escaped, want it embedded verbatim:\n%s", got)
	}
}

func TestEscapeAttrDoesNotEscapeGreaterThan(t *testing.T) {
	if got := escapeAttr("a>b"); got != "a>b" {
		t.Fatalf("escapeAttr(%q) = %q, want unchanged — attribute escaping never touches >", "a>b", got)
	}
}

func TestEscapeTextDoesNotEscapeQuote(t *testing.T) {
	if got := escapeText(`a"b`); got != `a"b` {
		t.Fatalf(`escapeText(%q) = %q, want unchanged — text escaping never touches "`, `a"b`, got)
	}
}

func TestIsModelInvocableAndIsUserInvocable(t *testing.T) {
	s := Summary{Invocation: InvocationPolicy{ModelInvocable: true, UserInvocable: false}}
	if !IsModelInvocable(s) || IsUserInvocable(s) {
		t.Fatalf("s = %+v", s)
	}
}
