package systemprompt

import (
	"testing"

	"go-harness/core/agentloop"
)

func TestAssembleDefaultsIncludeHarnessIdentityOnly(t *testing.T) {
	a, err := New().Assemble()
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Sections) != 1 || a.Sections[0].Name != "harness:identity" {
		t.Fatalf("Sections = %+v, want just harness:identity", a.Sections)
	}
	if a.Sections[0].Text != DefaultHarnessIdentity {
		t.Fatalf("Text = %q, want %q", a.Sections[0].Text, DefaultHarnessIdentity)
	}
}

func TestWithHarnessIdentityFalseOmitsIt(t *testing.T) {
	a, err := New().WithHarnessIdentity(false).Assemble()
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Sections) != 0 {
		t.Fatalf("Sections = %+v, want none", a.Sections)
	}
}

func TestEmptyPersonaContributesNoSection(t *testing.T) {
	a, err := New().WithPersona("").Assemble()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range a.Sections {
		if s.Name == "deployment:persona" {
			t.Fatal("an empty persona should not contribute a section at all")
		}
	}
}

func TestSectionsOrderedAscendingWithStableTiebreak(t *testing.T) {
	b := New().WithHarnessIdentity(false)
	b.Section(Section{Name: "z-first-registered", Order: 100, Text: "z"})
	b.Section(Section{Name: "a-second-registered", Order: 100, Text: "a"})
	b.Section(Section{Name: "early", Order: 50, Text: "e"})
	a, err := b.Assemble()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(a.Sections))
	for i, s := range a.Sections {
		names[i] = s.Name
	}
	want := []string{"early", "z-first-registered", "a-second-registered"}
	if len(names) != len(want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %v, want %v (order-100 ties must break by registration order)", names, want)
		}
	}
}

func TestSectionDuplicateNamePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic for a duplicate section name")
		}
	}()
	New().Section(Section{Name: "dup", Text: "a"}).Section(Section{Name: "dup", Text: "b"})
}

func TestAssembleRejectsMoreThanOneCompleteSection(t *testing.T) {
	b := New().WithHarnessIdentity(false)
	b.Section(Section{Name: "c1", Text: "one", Complete: true})
	b.Section(Section{Name: "c2", Text: "two", Complete: true})
	if _, err := b.Assemble(); err == nil {
		t.Fatal("expected an error for two complete sections")
	}
}

func TestCompleteSectionSuppressesEveryOtherSection(t *testing.T) {
	b := New() // harness identity included by default
	b.Section(Section{Name: "guidance", Order: 100, Text: "some tool guidance"})
	b.Section(Section{Name: "override", Order: 0, Text: "the whole prompt", Complete: true})
	a, err := b.Assemble()
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Sections) != 1 || a.Sections[0].Name != "override" {
		t.Fatalf("Sections = %+v, want just the complete override", a.Sections)
	}
}

func TestVariablesAreCopiedIntoTheAssembly(t *testing.T) {
	b := New().WithHarnessIdentity(false)
	b.Variable("model", "deepseek-chat")
	a, err := b.Assemble()
	if err != nil {
		t.Fatal(err)
	}
	if a.Variables["model"] != "deepseek-chat" {
		t.Fatalf("Variables = %+v", a.Variables)
	}
}

func schema(name string) agentloop.ToolSchema { return agentloop.ToolSchema{Name: name} }

func TestApplyToolOrderDefaultsToAlphabetical(t *testing.T) {
	b := New().WithHarnessIdentity(false)
	b.Tools([]agentloop.ToolSchema{schema("write"), schema("bash"), schema("read")})
	a, err := b.Assemble()
	if err != nil {
		t.Fatal(err)
	}
	got := []string{a.Tools[0].Name, a.Tools[1].Name, a.Tools[2].Name}
	want := []string{"bash", "read", "write"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestApplyToolOrderExplicitWithRestMarker(t *testing.T) {
	b := New().WithHarnessIdentity(false)
	b.Tools([]agentloop.ToolSchema{schema("write"), schema("bash"), schema("read"), schema("edit")})
	b.WithToolOrder("bash", ToolOrderRest, "write")
	a, err := b.Assemble()
	if err != nil {
		t.Fatal(err)
	}
	// bash first, then unlisted (edit, read — alphabetical) at the marker, then write last.
	got := make([]string, len(a.Tools))
	for i, s := range a.Tools {
		got[i] = s.Name
	}
	want := []string{"bash", "edit", "read", "write"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestApplyToolOrderRejectsMissingRestMarker(t *testing.T) {
	b := New().WithHarnessIdentity(false)
	b.Tools([]agentloop.ToolSchema{schema("bash")})
	b.WithToolOrder("bash")
	if _, err := b.Assemble(); err == nil {
		t.Fatal("expected an error: tool order with no rest marker")
	}
}

func TestApplyToolOrderRejectsDuplicateRestMarker(t *testing.T) {
	b := New().WithHarnessIdentity(false)
	b.Tools([]agentloop.ToolSchema{schema("bash")})
	b.WithToolOrder(ToolOrderRest, ToolOrderRest)
	if _, err := b.Assemble(); err == nil {
		t.Fatal("expected an error: two rest markers")
	}
}

func TestApplyToolOrderRejectsDuplicateName(t *testing.T) {
	b := New().WithHarnessIdentity(false)
	b.Tools([]agentloop.ToolSchema{schema("bash")})
	b.WithToolOrder("bash", "bash", ToolOrderRest)
	if _, err := b.Assemble(); err == nil {
		t.Fatal("expected an error: duplicate listed tool name")
	}
}

func TestApplyToolOrderRejectsUnknownListedName(t *testing.T) {
	b := New().WithHarnessIdentity(false)
	b.Tools([]agentloop.ToolSchema{schema("bash")})
	b.WithToolOrder("not-a-real-tool", ToolOrderRest)
	if _, err := b.Assemble(); err == nil {
		t.Fatal("expected an error: listed name isn't among the known tools")
	}
}

func TestFullAssembleAndRenderRoundTrip(t *testing.T) {
	b := New().
		WithPersona("You help with {{project}}.").
		WithToolOrder("read", ToolOrderRest)
	b.Section(Section{Name: "tool:read", Order: OrderToolGuidanceMin, Text: "read: reads a file at {{cwd}}."})
	b.Variable("project", "go-harness")
	b.Variable("cwd", "/work")
	b.Tools([]agentloop.ToolSchema{schema("write"), schema("read")})

	assembly, err := b.Assemble()
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := RenderPrompt(assembly)
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultHarnessIdentity + "\n\n" + "You help with go-harness." + "\n\n" + "read: reads a file at /work."
	if prompt != want {
		t.Fatalf("prompt =\n%s\nwant:\n%s", prompt, want)
	}
	if len(assembly.Tools) != 2 || assembly.Tools[0].Name != "read" || assembly.Tools[1].Name != "write" {
		t.Fatalf("Tools = %+v, want [read, write]", assembly.Tools)
	}
}
