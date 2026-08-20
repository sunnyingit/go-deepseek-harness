package filesystem

import "testing"

func TestSplitFrontmatterBasic(t *testing.T) {
	raw := "---\nname: x\ndescription: y\n---\nbody text\nmore body"
	yamlText, body, ok := splitFrontmatter(raw)
	if !ok {
		t.Fatal("want ok")
	}
	if yamlText != "name: x\ndescription: y" {
		t.Fatalf("yamlText = %q", yamlText)
	}
	if body != "body text\nmore body" {
		t.Fatalf("body = %q", body)
	}
}

func TestSplitFrontmatterCRLF(t *testing.T) {
	raw := "---\r\nname: x\r\ndescription: y\r\n---\r\nbody\r\n"
	yamlText, body, ok := splitFrontmatter(raw)
	if !ok {
		t.Fatal("want ok")
	}
	if yamlText != "name: x\ndescription: y" {
		t.Fatalf("yamlText = %q", yamlText)
	}
	if body != "body\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestSplitFrontmatterNoOpeningDelimiter(t *testing.T) {
	if _, _, ok := splitFrontmatter("name: x\ndescription: y\n"); ok {
		t.Fatal("want not ok — no opening --- delimiter")
	}
}

func TestSplitFrontmatterNoClosingDelimiter(t *testing.T) {
	if _, _, ok := splitFrontmatter("---\nname: x\ndescription: y\n"); ok {
		t.Fatal("want not ok — no closing --- delimiter")
	}
}

func TestSplitFrontmatterEmptyBody(t *testing.T) {
	yamlText, body, ok := splitFrontmatter("---\nname: x\ndescription: y\n---\n")
	if !ok || yamlText != "name: x\ndescription: y" || body != "" {
		t.Fatalf("yamlText=%q body=%q ok=%v", yamlText, body, ok)
	}
}

func TestParseFrontmatterRequiredFields(t *testing.T) {
	p, err := parseFrontmatter("name: my-skill\ndescription: does a thing")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "my-skill" || p.Description != "does a thing" {
		t.Fatalf("p = %+v", p)
	}
}

func TestParseFrontmatterMissingNameOrDescription(t *testing.T) {
	if _, err := parseFrontmatter("description: only"); err == nil {
		t.Fatal("want an error for a missing name")
	}
	if _, err := parseFrontmatter("name: only"); err == nil {
		t.Fatal("want an error for a missing description")
	}
}

func TestParseFrontmatterEmptyYAMLIsRejected(t *testing.T) {
	if _, err := parseFrontmatter(""); err == nil {
		t.Fatal("want an error for empty frontmatter")
	}
}

func TestParseFrontmatterWhenToUse(t *testing.T) {
	p, err := parseFrontmatter("name: x\ndescription: y\nwhenToUse: use it when needed")
	if err != nil {
		t.Fatal(err)
	}
	if p.WhenToUse != "use it when needed" {
		t.Fatalf("whenToUse = %q", p.WhenToUse)
	}
}

func TestParseFrontmatterMetadataObject(t *testing.T) {
	p, err := parseFrontmatter("name: x\ndescription: y\nmetadata:\n  version: 2\n  tags:\n    - a\n    - b")
	if err != nil {
		t.Fatal(err)
	}
	if p.Metadata["version"] != 2 {
		t.Fatalf("metadata = %+v", p.Metadata)
	}
}

func TestParseFrontmatterMetadataNonObjectIsIgnoredNotRejected(t *testing.T) {
	p, err := parseFrontmatter("name: x\ndescription: y\nmetadata: \"just a string\"")
	if err != nil {
		t.Fatal(err)
	}
	if p.Metadata != nil {
		t.Fatalf("metadata = %+v, want nil (non-object silently ignored)", p.Metadata)
	}
}

func TestParseFrontmatterDefaultInvocationPolicyIsBothTrue(t *testing.T) {
	p, err := parseFrontmatter("name: x\ndescription: y")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Invocation.ModelInvocable || !p.Invocation.UserInvocable {
		t.Fatalf("policy = %+v", p.Invocation)
	}
}

func TestParseFrontmatterDisableModelInvocationVariants(t *testing.T) {
	cases := map[string]bool{
		"true": true, "yes": true, "on": true, "1": true,
		"false": false, "no": false, "off": false, "0": false,
	}
	for raw, disabled := range cases {
		p, err := parseFrontmatter("name: x\ndescription: y\ndisable-model-invocation: " + raw)
		if err != nil {
			t.Fatalf("raw=%q: %v", raw, err)
		}
		if p.Invocation.ModelInvocable != !disabled {
			t.Fatalf("raw=%q: ModelInvocable = %v, want %v", raw, p.Invocation.ModelInvocable, !disabled)
		}
	}
}

func TestParseFrontmatterUserInvocableVariants(t *testing.T) {
	p, err := parseFrontmatter("name: x\ndescription: y\nuser-invocable: false")
	if err != nil {
		t.Fatal(err)
	}
	if p.Invocation.UserInvocable {
		t.Fatalf("policy = %+v", p.Invocation)
	}
	p, err = parseFrontmatter("name: x\ndescription: y\nuser-invocable: true")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Invocation.UserInvocable {
		t.Fatalf("policy = %+v", p.Invocation)
	}
}

func TestParseFrontmatterInvalidBooleanLikeIsRejected(t *testing.T) {
	if _, err := parseFrontmatter("name: x\ndescription: y\ndisable-model-invocation: maybe"); err == nil {
		t.Fatal("want an error for a non-boolean-like value")
	}
}

func TestParseFrontmatterActualBooleanValue(t *testing.T) {
	p, err := parseFrontmatter("name: x\ndescription: y\ndisable-model-invocation: true")
	if err != nil {
		t.Fatal(err)
	}
	if p.Invocation.ModelInvocable {
		t.Fatalf("policy = %+v", p.Invocation)
	}
}

func TestParseFrontmatterLegacyKeysRejected(t *testing.T) {
	for _, legacy := range []string{"disableModelInvocation", "modelInvocable", "userInvocable"} {
		_, err := parseFrontmatter("name: x\ndescription: y\n" + legacy + ": true")
		if err == nil {
			t.Errorf("legacy key %q should have been rejected", legacy)
		}
	}
}

func TestParseFrontmatterInvalidYAMLIsRejected(t *testing.T) {
	if _, err := parseFrontmatter("name: [unterminated"); err == nil {
		t.Fatal("want an error for invalid YAML")
	}
}
