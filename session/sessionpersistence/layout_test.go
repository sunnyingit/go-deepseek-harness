package sessionpersistence

import "testing"

func TestEscapeSegmentNoCWD(t *testing.T) {
	if got := escapeSegment(""); got != noCWDDir {
		t.Fatalf("escapeSegment(\"\") = %q, want %q", got, noCWDDir)
	}
}

func TestEscapeSegmentStripsUnsafeChars(t *testing.T) {
	got := escapeSegment("/Users/me/my project!!")
	if got == "" || got == noCWDDir {
		t.Fatalf("escapeSegment produced an empty/placeholder result for real input: %q", got)
	}
	for _, r := range got {
		safe := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_'
		if !safe {
			t.Fatalf("escapeSegment(%q) contains unsafe rune %q: %q", "/Users/me/my project!!", r, got)
		}
	}
}

func TestEscapeSegmentDeterministic(t *testing.T) {
	a := escapeSegment("/work/proj")
	b := escapeSegment("/work/proj")
	if a != b {
		t.Fatalf("escapeSegment is not deterministic: %q vs %q", a, b)
	}
}

func TestEscapeSegmentCollapsesRuns(t *testing.T) {
	got := escapeSegment("a///b")
	if got != "a_b" {
		t.Fatalf("escapeSegment(%q) = %q, want a_b (collapsed run, no leading/trailing separators)", "a///b", got)
	}
}

func TestSessionFileLayout(t *testing.T) {
	path := sessionFile("/root", "sess-1", "/work/proj")
	want := "/root/" + escapeSegment("/work/proj") + "/" + escapeSegment("sess-1") + "/session.jsonl"
	if path != want {
		t.Fatalf("sessionFile = %q, want %q", path, want)
	}

	noCWDPath := sessionFile("/root", "sess-1", "")
	want = "/root/" + noCWDDir + "/" + escapeSegment("sess-1") + "/session.jsonl"
	if noCWDPath != want {
		t.Fatalf("sessionFile with empty cwd = %q, want %q", noCWDPath, want)
	}
}
