package systemprompt

import "testing"

func TestRenderPromptInterpolatesKnownVariable(t *testing.T) {
	a := Assembly{
		Sections:  []Section{{Name: "s", Text: "model is {{model}}"}},
		Variables: map[string]string{"model": "deepseek-chat"},
	}
	got, err := RenderPrompt(a)
	if err != nil {
		t.Fatal(err)
	}
	if got != "model is deepseek-chat" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderPromptUnknownVariableErrors(t *testing.T) {
	a := Assembly{
		Sections:  []Section{{Name: "s", Text: "{{nope}}"}},
		Variables: map[string]string{},
	}
	if _, err := RenderPrompt(a); err == nil {
		t.Fatal("expected an error for an unregistered variable reference")
	}
}

func TestRenderPromptRegisteredEmptyValueIsNotAnError(t *testing.T) {
	a := Assembly{
		Sections:  []Section{{Name: "s", Text: "cwd=[{{cwd}}]"}},
		Variables: map[string]string{"cwd": ""},
	}
	got, err := RenderPrompt(a)
	if err != nil {
		t.Fatal(err)
	}
	if got != "cwd=[]" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderPromptMalformedGroupErrors(t *testing.T) {
	a := Assembly{
		Sections:  []Section{{Name: "s", Text: "weird {{{model}}} thing"}},
		Variables: map[string]string{"model": "x"},
	}
	if _, err := RenderPrompt(a); err == nil {
		t.Fatal("expected an error for a malformed {{{model}}} group")
	}
}

func TestRenderPromptUnclosedBracePassesThroughLiterally(t *testing.T) {
	a := Assembly{
		Sections:  []Section{{Name: "s", Text: "literal {{ never closes"}},
		Variables: map[string]string{},
	}
	got, err := RenderPrompt(a)
	if err != nil {
		t.Fatal(err)
	}
	if got != "literal {{ never closes" {
		t.Fatalf("got %q, want the unclosed brace preserved verbatim", got)
	}
}

func TestRenderPromptDropsEmptySectionsAndJoinsWithBlankLine(t *testing.T) {
	a := Assembly{
		Sections: []Section{
			{Name: "a", Text: "first"},
			{Name: "empty", Text: ""},
			{Name: "b", Text: "second"},
		},
		Variables: map[string]string{},
	}
	got, err := RenderPrompt(a)
	if err != nil {
		t.Fatal(err)
	}
	if got != "first\n\nsecond" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderPromptReplacementTextIsNeverReScanned(t *testing.T) {
	a := Assembly{
		Sections:  []Section{{Name: "s", Text: "{{payload}}"}},
		Variables: map[string]string{"payload": "{{model}}"}, // looks like a reference, must not be interpolated again
	}
	got, err := RenderPrompt(a)
	if err != nil {
		t.Fatal(err)
	}
	if got != "{{model}}" {
		t.Fatalf("got %q, want the literal substituted value untouched", got)
	}
}

func TestIsBareIdentifier(t *testing.T) {
	cases := map[string]bool{
		"model":     true,
		"cwd_2":     true,
		"_ok":       true,
		"":          false,
		"2model":    false,
		"has space": false,
		"{model":    false,
		"model}":    false,
	}
	for in, want := range cases {
		if got := isBareIdentifier(in); got != want {
			t.Errorf("isBareIdentifier(%q) = %v, want %v", in, got, want)
		}
	}
}
