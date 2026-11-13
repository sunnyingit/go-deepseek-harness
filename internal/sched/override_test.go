package sched

import (
	"testing"
	"time"
)

func TestEffectiveTriggerOverrideAndSoftDelete(t *testing.T) {
	dir := t.TempDir()
	st := OpenStore(dir + "/sched.json")
	clock := time.Date(2026, 11, 11, 21, 0, 0, 0, Zone)
	s := New(st, LoadCalendar(""), func() time.Time { return clock })
	s.Register(&Spec{
		ID: "a", Title: "测试", Category: CatScan,
		Trigger: Trigger{Kind: "daily", At: "20:30", WindowMin: 30, Days: "trading"},
		Handler: func(c *JobCtx) (any, error) { return map[string]any{"ok": true}, nil },
	})
	sp, _ := s.Spec("a")
	tr := s.EffectiveTrigger(sp)
	if tr.At != "20:30" {
		t.Fatalf("default at = %s", tr.At)
	}
	st.SetOverride("a", TriggerOverride{At: "21:15", WindowMin: 45})
	tr = s.EffectiveTrigger(sp)
	if tr.At != "21:15" || tr.WindowMin != 45 || tr.Kind != "daily" {
		t.Fatalf("override = %+v", tr)
	}

	st.SoftDelete("a")
	s.plan(clock)
	if runs := st.Runs("a", 10); len(runs) != 0 {
		t.Fatalf("soft-deleted task still planned: %+v", runs)
	}
	if _, err := s.Manual("a", nil, map[string]any{"on_demand": true}); err == nil {
		t.Fatal("manual on deleted should fail")
	}
	st.Restore("a")
	if st.Deleted("a") || st.Paused("a") {
		t.Fatal("restore should clear deleted and paused")
	}
	if _, err := s.Manual("a", nil, map[string]any{"on_demand": true}); err != nil {
		t.Fatal(err)
	}

	st.SetOverride("a", TriggerOverride{At: "18:00", WindowMin: 15, Days: "any"})
	st.SoftDelete("a")
	st.Flush()
	st2 := OpenStore(dir + "/sched.json")
	if !st2.Deleted("a") || !st2.Paused("a") {
		t.Fatal("restart should keep soft-delete")
	}
	o, ok := st2.Override("a")
	if !ok || o.At != "18:00" || o.WindowMin != 15 || o.Days != "any" {
		t.Fatalf("restart override = %+v ok=%v", o, ok)
	}
}
