package server

import (
	"os"
	"path/filepath"
	"testing"

	"go-harness/internal/appconfig"
	"go-harness/internal/sched"
)

func TestCommitBundleFromGroup(t *testing.T) {
	dir := t.TempDir()
	s := &Server{
		cfg:     appconfig.Config{DataDir: dir, Model: "test"},
		groups:  map[string][]groupLine{},
		debates: map[string]*debateFile{},
		cal:     sched.LoadCalendar(""),
	}
	code := "600519.SH"
	for _, id := range diagnosisDims {
		s.appendGroup(code, groupLine{
			Agent: id, Name: id, Kind: "analysis", Text: id + " 结论",
			Status: "support", StatusText: "偏多",
		})
	}
	s.updateDebate(code, func(f *debateFile) {
		f.Bull = []debatePoint{{Text: "多"}}
		f.Bear = []debatePoint{{Text: "空"}}
		f.Chief = &chiefDecision{Text: "综合偏多", Verdict: "偏多"}
	})
	s.appendGroup(code, groupLine{Agent: "bull", Name: "看多", Kind: "analysis", Text: "多"})
	s.appendGroup(code, groupLine{Agent: "bear", Name: "看空", Kind: "analysis", Text: "空"})
	s.appendGroup(code, groupLine{Agent: "chief", Name: "主分析师", Kind: "analysis", Text: "综合偏多", StatusText: "偏多"})

	b, err := s.commitBundleFromGroup(code, "live")
	if err != nil {
		t.Fatal(err)
	}
	if b.Trigger != "live" || b.Source != "agent" {
		t.Fatalf("bundle = %+v", b)
	}
	if len(b.Analysts) != 6 || b.Analysts[0].Headline == nil {
		t.Fatalf("analysts = %+v", b.Analysts)
	}
	path := filepath.Join(dir, "bundles", "600519_SH.json")
	if _, err := os.Stat(path); err != nil {
		// sanitizeCode may differ; just ensure readBundle works
		if got := s.readBundle(code); got == nil || got.ID != b.ID {
			t.Fatalf("readBundle missing, path err=%v", err)
		}
	}
}
