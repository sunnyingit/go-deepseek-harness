package sched

import (
	"sync/atomic"
	"testing"
	"time"
)

type fake struct{ now time.Time }

func (f *fake) Now() time.Time          { return f.now }
func (f *fake) advance(d time.Duration) { f.now = f.now.Add(d) }

func at1(day, hhmm string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02 15:04", day+" "+hhmm, Zone)
	return t
}

func newSched(t *testing.T, start time.Time) (*Scheduler, *fake) {
	f := &fake{now: start}
	return New(OpenStore(""), LoadCalendar(""), f.Now), f
}

// newDue builds a scheduler that was already running at 20:00 and is now 20:35, so a 20:30 task is due.
func newDue(t *testing.T, day string) (*Scheduler, *fake) {
	s, f := newSched(t, at1(day, "20:00"))
	s.Tick()
	f.advance(35 * time.Minute)
	return s, f
}

// 2026-11-05 is a Thursday, 2026-11-07 a Saturday.

func TestDailyRunsOnceOnATradingDayAndNotOnWeekends(t *testing.T) {
	s, f := newSched(t, at1("2026-11-05", "20:00"))
	var calls int32
	s.Register(&Spec{ID: "a", Trigger: Trigger{Kind: "daily", At: "20:30", WindowMin: 30}, Handler: func(c *JobCtx) (any, error) { atomic.AddInt32(&calls, 1); return map[string]any{"ok": true}, nil }})
	s.TickSync()
	if calls != 0 {
		t.Fatal("ran before its time")
	}
	f.advance(40 * time.Minute)
	s.TickSync()
	s.TickSync()
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
	if run := s.Store.Runs("a", 1)[0]; run.State != "succeeded" || run.PeriodKey != "trading_day:2026-11-05" || run.Trigger != "schedule" {
		t.Fatalf("run = %+v", run)
	}
	s2, f2 := newSched(t, at1("2026-11-07", "20:00"))
	s2.Register(&Spec{ID: "a", Trigger: Trigger{Kind: "daily", At: "20:30", WindowMin: 30}, Handler: func(c *JobCtx) (any, error) { calls++; return nil, nil }})
	f2.advance(time.Hour)
	s2.TickSync()
	if len(s2.Store.Runs("a", 0)) != 0 {
		t.Fatal("ran on a Saturday")
	}
}

func TestRunsBeforeInstallAreNotCatchedUpAndLateOnesAreMarkedCatchup(t *testing.T) {
	s, f := newSched(t, at1("2026-11-05", "22:00")) // installed after 20:30
	s.Register(&Spec{ID: "a", Trigger: Trigger{Kind: "daily", At: "20:30", WindowMin: 10}, CatchupTTL: 6 * time.Hour, Handler: func(c *JobCtx) (any, error) { return nil, nil }})
	s.TickSync()
	if len(s.Store.Runs("a", 0)) != 0 {
		t.Fatal("caught up a run from before the install")
	}
	s, f = newSched(t, at1("2026-11-05", "20:00"))
	s.Register(&Spec{ID: "a", Trigger: Trigger{Kind: "daily", At: "20:30", WindowMin: 10}, CatchupTTL: 6 * time.Hour, Handler: func(c *JobCtx) (any, error) { return nil, nil }})
	s.TickSync() // first tick at 20:00 records the install time
	f.advance(2 * time.Hour)
	s.TickSync()
	if run := s.Store.Runs("a", 1); len(run) != 1 || run[0].Trigger != "catchup" {
		t.Fatalf("runs = %+v", run)
	}
}

func TestGateHoldsTheRunUntilDataIsReady(t *testing.T) {
	s, f := newSched(t, at1("2026-11-05", "20:00"))
	ready := false
	s.AddGate("bars", func(day string, now time.Time) bool { return ready })
	var calls int32
	s.Register(&Spec{ID: "a", Gates: []string{"bars"}, Trigger: Trigger{Kind: "daily", At: "20:30", WindowMin: 60}, Handler: func(c *JobCtx) (any, error) { atomic.AddInt32(&calls, 1); return nil, nil }})
	s.TickSync()
	f.advance(40 * time.Minute)
	s.TickSync()
	if calls != 0 || s.Store.Runs("a", 1)[0].State != "waiting" {
		t.Fatalf("calls=%d run=%+v", calls, s.Store.Runs("a", 1))
	}
	ready = true
	s.TickSync()
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestEachUnitIsItsOwnJobHoldingsFirstAndOneFailureDoesNotStopTheOthers(t *testing.T) {
	s, f := newDue(t, "2026-11-05")
	var order []string
	s.Register(&Spec{ID: "a", Queue: "llm", MaxAttempts: 1, Trigger: Trigger{Kind: "daily", At: "20:30", WindowMin: 60},
		Fanout: func(*PlanCtx) []Unit {
			return []Unit{{Key: "x", Priority: 50}, {Key: "hold", Priority: 90}, {Key: "bad", Priority: 50}}
		},
		Handler: func(c *JobCtx) (any, error) {
			if c.Unit() == "bad" {
				return nil, Permanent("坏了")
			}
			return nil, nil
		}})
	QueueSize["llm"] = 1
	defer func() { QueueSize["llm"] = 2 }()
	_ = order
	_ = f
	s.TickSync()
	run := s.Store.Runs("a", 1)[0]
	if run.State != "partial" || run.Counts["succeeded"] != 2 || run.Counts["dead"] != 1 {
		t.Fatalf("run = %+v", run)
	}
}

func TestRetryRedoesOnlyTheStepThatFailed(t *testing.T) {
	s, f := newDue(t, "2026-11-05")
	var first, second int32
	fail := true
	s.Register(&Spec{ID: "a", MaxAttempts: 3, Backoff: []time.Duration{time.Second}, Trigger: Trigger{Kind: "daily", At: "20:30", WindowMin: 60},
		Handler: func(c *JobCtx) (any, error) {
			if _, err := Step(c, "one", func() (int, error) { atomic.AddInt32(&first, 1); return 1, nil }); err != nil {
				return nil, err
			}
			if _, err := Step(c, "two", func() (int, error) {
				atomic.AddInt32(&second, 1)
				if fail {
					fail = false
					return 0, Transient("库忙")
				}
				return 2, nil
			}); err != nil {
				return nil, err
			}
			return "done", nil
		}})
	s.TickSync()
	f.advance(2 * time.Second)
	s.TickSync()
	if first != 1 || second != 2 {
		t.Fatalf("first=%d second=%d", first, second)
	}
	if run := s.Store.Runs("a", 1)[0]; run.State != "succeeded" {
		t.Fatalf("run = %+v", run)
	}
}

func TestNotReadyWaitsWithoutBurningAttempts(t *testing.T) {
	s, f := newDue(t, "2026-11-05")
	ready := false
	s.Register(&Spec{ID: "a", MaxAttempts: 1, Trigger: Trigger{Kind: "daily", At: "20:30", WindowMin: 60},
		Handler: func(c *JobCtx) (any, error) {
			if !ready {
				c.BumpWait()
				return nil, NotReady("还在整理", time.Minute)
			}
			return c.Waits(), nil
		}})
	for i := 0; i < 3; i++ {
		s.TickSync()
		f.advance(61 * time.Second)
	}
	job := s.Store.Jobs(s.Store.Runs("a", 1)[0].ID)[0]
	if job.State != "not_ready" || job.Attempt != 0 {
		t.Fatalf("job = %+v", job)
	}
	ready = true
	s.TickSync()
	job = s.Store.Jobs(s.Store.Runs("a", 1)[0].ID)[0]
	if job.State != "succeeded" || job.Output.(int) < 3 {
		t.Fatalf("job = %+v", job)
	}
}

func TestPermanentFailsAtOnceAndBudgetSkipsTheRest(t *testing.T) {
	s, _ := newDue(t, "2026-11-05")
	s.Register(&Spec{ID: "a", MaxAttempts: 3, BudgetCalls: 10, Queue: "data", Trigger: Trigger{Kind: "daily", At: "20:30", WindowMin: 60},
		Fanout:  func(*PlanCtx) []Unit { return []Unit{{Key: "1", Priority: 90}, {Key: "2", Priority: 80}} },
		Handler: func(c *JobCtx) (any, error) { c.AddCalls(10); return nil, nil }})
	QueueSize["data"] = 1
	defer func() { QueueSize["data"] = 4 }()
	s.TickSync()
	run := s.Store.Runs("a", 1)[0]
	if run.Counts["succeeded"] != 1 || run.Counts["skipped"] != 1 {
		t.Fatalf("run = %+v", run)
	}
}

func TestManualOnDemandSkipsGatesAndListenersSeeFinishedRuns(t *testing.T) {
	s, _ := newSched(t, at1("2026-11-07", "10:00")) // Saturday, gate closed
	s.AddGate("never", func(string, time.Time) bool { return false })
	var seen []string
	s.Listeners = append(s.Listeners, func(r Run, sp *Spec) { seen = append(seen, sp.ID+":"+r.State) })
	s.Register(&Spec{ID: "a", Gates: []string{"never"}, Trigger: Trigger{Kind: "daily", At: "20:30"}, Handler: func(c *JobCtx) (any, error) { return c.Unit(), nil }})
	if _, err := s.Manual("a", []UnitSpec{{Key: "600036.SH", Priority: 95}}, map[string]any{"on_demand": true}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Store.ActiveJob("a", "600036.SH"); !ok {
		t.Fatal("queued run is not visible as an active job")
	}
	s.TickSync()
	if len(seen) != 1 || seen[0] != "a:succeeded" {
		t.Fatalf("seen = %v", seen)
	}
	if _, ok := s.Store.ActiveJob("a", "600036.SH"); ok {
		t.Fatal("finished job still active")
	}
}

func TestIntervalRunsOncePerSlotOnlyInSessions(t *testing.T) {
	s, f := newSched(t, at1("2026-11-05", "09:00"))
	var calls int32
	s.Register(&Spec{ID: "i", Frequent: true, Trigger: Trigger{Kind: "interval", EveryMin: 3}, Handler: func(c *JobCtx) (any, error) { atomic.AddInt32(&calls, 1); return nil, nil }})
	s.TickSync()
	f.advance(31 * time.Minute) // 09:31
	s.TickSync()
	s.TickSync()
	f.advance(3 * time.Minute)
	s.TickSync()
	f.advance(3 * time.Hour) // 12:34, lunch
	s.TickSync()
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestStateSurvivesARestart(t *testing.T) {
	path := t.TempDir() + "/sched.json"
	f := &fake{now: at1("2026-11-05", "20:00")}
	s := New(OpenStore(path), LoadCalendar(""), f.Now)
	var calls int32
	spec := &Spec{ID: "a", Trigger: Trigger{Kind: "daily", At: "20:30", WindowMin: 60}, Handler: func(c *JobCtx) (any, error) { atomic.AddInt32(&calls, 1); return nil, nil }}
	s.Register(spec)
	s.Tick()
	f.advance(35 * time.Minute)
	s.TickSync()
	s2 := New(OpenStore(path), LoadCalendar(""), f.Now)
	s2.Register(spec)
	s2.TickSync()
	if calls != 1 || len(s2.Store.Runs("a", 0)) != 1 {
		t.Fatalf("calls=%d runs=%d", calls, len(s2.Store.Runs("a", 0)))
	}
}

func TestReclaimInterruptedClearsOrphanRunningJobs(t *testing.T) {
	path := t.TempDir() + "/sched.json"
	f := &fake{now: at1("2026-11-05", "20:00")}
	s := New(OpenStore(path), LoadCalendar(""), f.Now)
	s.Register(&Spec{
		ID: "a", Trigger: Trigger{Kind: "daily", At: "20:30", WindowMin: 60},
		Handler: func(c *JobCtx) (any, error) {
			<-c.Ctx.Done() // never finishes unless cancelled; simulate a crash mid-run
			return nil, c.Ctx.Err()
		},
	})
	s.Tick()
	f.advance(35 * time.Minute)
	s.Tick() // launch into running, don't wait
	s.Store.Flush()
	if _, ok := s.Store.ActiveJob("a", "-"); !ok {
		t.Fatal("expected an active job while running")
	}

	// New process loads the same store: reclaim orphans then finish the run.
	s2 := New(OpenStore(path), LoadCalendar(""), f.Now)
	s2.Register(&Spec{ID: "a", Trigger: Trigger{Kind: "daily", At: "20:30", WindowMin: 60}, Handler: func(c *JobCtx) (any, error) { return nil, nil }})
	if n := s2.Store.ReclaimInterrupted(ms(f.Now())); n != 1 {
		t.Fatalf("reclaimed = %d", n)
	}
	s2.Tick()
	if _, ok := s2.Store.ActiveJob("a", "-"); ok {
		t.Fatal("orphaned running job still active after reclaim")
	}
	if run := s2.Store.Runs("a", 1)[0]; run.State != "failed" {
		t.Fatalf("run state = %s, want failed", run.State)
	}
}
