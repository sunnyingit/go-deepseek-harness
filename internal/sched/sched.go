// Package sched is the client's task scheduler: daily and interval triggers on the A-share calendar,
// per-unit jobs (one per stock), checkpointed steps so a retry only redoes what failed, per-day call
// budgets, and listeners that turn finished runs into inbox receipts.
package sched

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Trigger says when a task runs.
type Trigger struct {
	Kind      string // daily | interval (inside the trading sessions) | every (any time of day) | manual
	At        string // "20:30" for daily
	WindowMin int    // a daily run later than At+window is a catch-up
	Days      string // trading | any
	EveryMin  int    // interval, inside the trading sessions
}

// Unit is one thing a run works on (a stock code).
type Unit struct {
	Key      string
	Priority int
	Input    map[string]any
}

// PlanCtx is handed to Fanout.
type PlanCtx struct {
	Day   string
	Now   time.Time
	Input map[string]any
}

// Task category labels used by the tasks page tabs.
const (
	CatMine   = "mine"   // user custom tasks
	CatScan   = "scan"   // 研判战绩
	CatMarket = "market" // 行情
	CatNotify = "notify" // 通知
	CatWatch  = "watch"  // 盯盘
)

// Spec describes a task.
type Spec struct {
	ID       string
	Title    string
	Category string // CatMine / CatScan / CatMarket / CatNotify / CatWatch
	Note     string // one-line description for the UI
	// Trace is merged into Job.Input when the task has no fan-out, so the
	// tasks page can show what this run will call even without user input.
	Trace       map[string]any
	Trigger     Trigger
	Queue       string
	Fanout      func(*PlanCtx) []Unit
	Handler     func(*JobCtx) (any, error)
	Gates       []string
	MaxAttempts int
	Backoff     []time.Duration
	Timeout     time.Duration
	CatchupTTL  time.Duration
	BudgetCalls int  // per day; 0 = unlimited
	Frequent    bool // runs many times a day: receipts only for the first success of the day
}

// Gate says whether the data a task needs is ready for a trading day.
type Gate func(day string, now time.Time) bool

// Scheduler plans runs, executes jobs and reports finished runs to listeners.
type Scheduler struct {
	Store     *Store
	Cal       *Calendar
	Clock     func() time.Time
	Listeners []func(Run, *Spec)

	mu     sync.Mutex
	specs  map[string]*Spec
	order  []string
	gates  map[string]Gate
	queues map[string]chan struct{}
	wg     sync.WaitGroup
}

// QueueSize is how many jobs of one queue run at once.
var QueueSize = map[string]int{"llm": 2, "data": 4, "light": 8}

func New(store *Store, cal *Calendar, clock func() time.Time) *Scheduler {
	if clock == nil {
		clock = time.Now
	}
	return &Scheduler{Store: store, Cal: cal, Clock: clock, specs: map[string]*Spec{}, gates: map[string]Gate{}, queues: map[string]chan struct{}{}}
}

func (s *Scheduler) Register(spec *Spec) {
	if spec.MaxAttempts == 0 {
		spec.MaxAttempts = 3
	}
	if spec.Queue == "" {
		spec.Queue = "light"
	}
	if spec.Timeout == 0 {
		spec.Timeout = 15 * time.Minute
	}
	if spec.Trigger.Days == "" {
		spec.Trigger.Days = "trading"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.specs[spec.ID]; !ok {
		s.order = append(s.order, spec.ID)
	}
	s.specs[spec.ID] = spec
}

// Unregister removes a task. Its past runs stay in the history.
func (s *Scheduler) Unregister(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.specs[id]; !ok {
		return
	}
	delete(s.specs, id)
	for i, x := range s.order {
		if x == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

func (s *Scheduler) AddGate(name string, g Gate) {
	s.mu.Lock()
	s.gates[name] = g
	s.mu.Unlock()
}

// Specs lists the registered tasks in registration order.
func (s *Scheduler) Specs() []*Spec {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Spec, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.specs[id])
	}
	return out
}

func (s *Scheduler) Spec(id string) (*Spec, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, ok := s.specs[id]
	return sp, ok
}

func ms(t time.Time) int64 { return t.UnixMilli() }

func (s *Scheduler) queue(name string) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.queues[name]
	if !ok {
		size := QueueSize[name]
		if size == 0 {
			size = 4
		}
		q = make(chan struct{}, size)
		s.queues[name] = q
	}
	return q
}

// Tick plans what is due, starts the jobs that can run now, and finishes runs whose jobs are done.
// It never blocks on the jobs themselves.
func (s *Scheduler) Tick() {
	now := s.Clock()
	s.Store.firstTick(ms(now))
	s.plan(now)
	s.advance(now)
	s.launch(now)
	s.finishRuns(now)
	s.Store.Flush()
}

// TickSync is Tick followed by waiting for every started job and a closing Tick; tests and the
// "run now" command use it.
func (s *Scheduler) TickSync() {
	s.Tick()
	s.wg.Wait()
	s.Tick()
	s.wg.Wait()
}

// Wait blocks until the started jobs finish.
func (s *Scheduler) Wait() { s.wg.Wait() }

// Run ticks until the context ends.
func (s *Scheduler) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	// Drop in-flight work left on disk by the previous process before planning
	// anything new, so orphaned "running" jobs cannot block the UI.
	if n := s.Store.ReclaimInterrupted(ms(s.Clock())); n > 0 {
		s.finishRuns(s.Clock())
		s.Store.Flush()
	}
	s.Tick()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Tick()
		}
	}
}

// ---- planning

// EffectiveTrigger merges a Spec's default Trigger with any persisted user override.
func (s *Scheduler) EffectiveTrigger(sp *Spec) Trigger {
	t := sp.Trigger
	if o, ok := s.Store.Override(sp.ID); ok {
		if o.Kind != "" {
			t.Kind = o.Kind
		}
		if o.At != "" {
			t.At = o.At
		}
		if o.Days != "" {
			t.Days = o.Days
		}
		if o.WindowMin != 0 {
			t.WindowMin = o.WindowMin
		}
		if o.EveryMin != 0 {
			t.EveryMin = o.EveryMin
		}
	}
	if t.Days == "" {
		t.Days = "trading"
	}
	return t
}

func (s *Scheduler) plan(now time.Time) {
	frequent := map[string]bool{}
	for _, sp := range s.Specs() {
		frequent[sp.ID] = sp.Frequent
		if s.Store.Deleted(sp.ID) || s.Store.Paused(sp.ID) {
			continue
		}
		tr := s.EffectiveTrigger(sp)
		switch tr.Kind {
		case "daily":
			s.planDaily(sp, tr, now)
		case "interval", "every":
			s.planInterval(sp, tr, now)
		}
	}
	s.Store.prune(ms(now), frequent)
}

func (s *Scheduler) createRun(sp *Spec, period, trigger string, scheduled time.Time, now time.Time, units []UnitSpec, input map[string]any) *Run {
	st := s.Store
	st.mu.Lock()
	defer st.mu.Unlock()
	if period != "" && st.runByPeriod(sp.ID, period) != nil {
		return nil
	}
	if period == "" {
		period = "manual:" + st.nextID("m")
	}
	run := &Run{ID: st.nextID("r"), TaskID: sp.ID, PeriodKey: period, Trigger: trigger, ScheduledFor: ms(scheduled), State: "planned", Units: units, Input: input}
	st.s.Runs = append(st.s.Runs, run)
	st.touch()
	return run
}

func (s *Scheduler) planDaily(sp *Spec, tr Trigger, now time.Time) {
	first := s.Store.firstTick(ms(now))
	for back := 0; back <= 1; back++ {
		day := now.In(Zone).AddDate(0, 0, -back)
		if tr.Days == "trading" && !s.Cal.IsTradingDay(day) {
			continue
		}
		due := at(day, tr.At)
		if now.Before(due) || ms(due) < first {
			continue
		}
		window := time.Duration(tr.WindowMin) * time.Minute
		trigger := "schedule"
		if now.After(due.Add(window)) {
			trigger = "catchup"
			ttl := sp.CatchupTTL
			if ttl == 0 {
				ttl = 12 * time.Hour
			}
			if now.After(due.Add(ttl)) {
				continue
			}
		}
		prefix := "trading_day:"
		if tr.Days != "trading" {
			prefix = "day:"
		}
		s.createRun(sp, prefix+Day(day), trigger, due, now, nil, nil)
	}
}

func (s *Scheduler) planInterval(sp *Spec, tr Trigger, now time.Time) {
	if tr.EveryMin <= 0 {
		return
	}
	if tr.Kind == "interval" && (!s.Cal.IsTradingDay(now) || !InSession(now)) {
		return
	}
	n := now.In(Zone)
	mins := n.Hour()*60 + n.Minute()
	slot := mins / tr.EveryMin * tr.EveryMin
	due := time.Date(n.Year(), n.Month(), n.Day(), slot/60, slot%60, 0, 0, Zone)
	s.createRun(sp, fmt.Sprintf("slot:%sT%02d:%02d", Day(n), slot/60, slot%60), "schedule", due, now, nil, nil)
}

// Manual queues a run of a task by hand. units may be nil (the task's own fan-out is used). With
// input["on_demand"] the run does not wait for data gates. Soft-deleted tasks must be restored first.
func (s *Scheduler) Manual(taskID string, units []UnitSpec, input map[string]any) (Run, error) {
	sp, ok := s.Spec(taskID)
	if !ok {
		return Run{}, fmt.Errorf("没有这个任务：%s", taskID)
	}
	if s.Store.Deleted(taskID) {
		return Run{}, fmt.Errorf("任务已删除，请先恢复：%s", taskID)
	}
	run := s.createRun(sp, "", "manual", s.Clock(), s.Clock(), units, input)
	return *run, nil
}

// ---- advancing runs

func (s *Scheduler) gatesReady(sp *Spec, run *Run, now time.Time) bool {
	if run.Trigger == "manual" && run.Input != nil && run.Input["on_demand"] == true {
		return true
	}
	day := tradeDate(run, now)
	for _, name := range sp.Gates {
		s.mu.Lock()
		g := s.gates[name]
		s.mu.Unlock()
		if g != nil && !g(day, now) {
			return false
		}
	}
	return true
}

func tradeDate(run *Run, now time.Time) string {
	if i := strings.IndexByte(run.PeriodKey, ':'); i >= 0 && len(run.PeriodKey) >= i+11 {
		if d := run.PeriodKey[i+1 : i+11]; d[4] == '-' && d[7] == '-' {
			return d
		}
	}
	if v, ok := run.Input["trade_date"].(string); ok && v != "" {
		return v
	}
	return Day(time.UnixMilli(run.ScheduledFor))
}

func (s *Scheduler) advance(now time.Time) {
	st := s.Store
	st.mu.Lock()
	runs := append([]*Run(nil), st.s.Runs...)
	st.mu.Unlock()
	for _, run := range runs {
		if run.State != "planned" && run.State != "waiting" {
			continue
		}
		sp, ok := s.Spec(run.TaskID)
		if !ok {
			continue
		}
		if !s.gatesReady(sp, run, now) {
			tr := s.EffectiveTrigger(sp)
			deadline := time.UnixMilli(run.ScheduledFor).Add(maxDur(time.Duration(tr.WindowMin)*time.Minute, sp.CatchupTTL, 12*time.Hour))
			st.mu.Lock()
			if now.After(deadline) {
				run.State, run.FinishedAt, run.Note = "expired", ms(now), "等到时限数据也没有就绪"
			} else {
				run.State, run.Note = "waiting", "等数据就绪"
			}
			st.touch()
			st.mu.Unlock()
			if run.State == "expired" {
				s.report(run, sp)
			}
			continue
		}
		units := s.expand(sp, run, now)
		st.mu.Lock()
		run.State, run.StartedAt, run.Note = "running", ms(now), ""
		for _, u := range units {
			job := &Job{ID: st.nextID("j"), RunID: run.ID, TaskID: sp.ID, UnitKey: u.Key, Priority: u.Priority, State: "pending",
				MaxAttempts: sp.MaxAttempts, DueAt: ms(now), Input: u.Input, UpdatedAt: ms(now)}
			st.s.Jobs = append(st.s.Jobs, job)
		}
		if len(units) == 0 {
			run.State, run.FinishedAt, run.Note = "skipped", ms(now), "没有要处理的对象"
		}
		st.touch()
		st.mu.Unlock()
		if run.State == "skipped" {
			s.report(run, sp)
		}
	}
}

func maxDur(ds ...time.Duration) time.Duration {
	var m time.Duration
	for _, d := range ds {
		if d > m {
			m = d
		}
	}
	return m
}

func (s *Scheduler) expand(sp *Spec, run *Run, now time.Time) []Unit {
	if len(run.Units) > 0 {
		out := make([]Unit, 0, len(run.Units))
		for _, u := range run.Units {
			p := u.Priority
			if p == 0 {
				p = 50
			}
			out = append(out, Unit{Key: u.Key, Priority: p, Input: mergeInput(run.Input, u.Input)})
		}
		return out
	}
	if sp.Fanout == nil {
		return []Unit{{Key: "-", Priority: 50, Input: mergeInput(sp.Trace, run.Input)}}
	}
	units := sp.Fanout(&PlanCtx{Day: tradeDate(run, now), Now: now, Input: run.Input})
	for i := range units {
		units[i].Input = mergeInput(mergeInput(sp.Trace, run.Input), units[i].Input)
	}
	return units
}

func mergeInput(a, b map[string]any) map[string]any {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// ---- executing jobs

func (s *Scheduler) launch(now time.Time) {
	st := s.Store
	st.mu.Lock()
	var due []*Job
	for _, j := range st.s.Jobs {
		if (j.State == "pending" || j.State == "retrying" || j.State == "not_ready") && j.DueAt <= ms(now) {
			due = append(due, j)
		}
	}
	sort.SliceStable(due, func(a, b int) bool { return due[a].Priority > due[b].Priority })
	for _, j := range due {
		j.State = "running"
		j.UpdatedAt = ms(now)
	}
	st.touch()
	st.mu.Unlock()
	for _, j := range due {
		job := j
		sp, ok := s.Spec(job.TaskID)
		if !ok {
			continue
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			q := s.queue(sp.Queue)
			q <- struct{}{}
			defer func() { <-q }()
			s.execute(sp, job)
		}()
	}
}

func (s *Scheduler) runOf(id string) *Run {
	for _, r := range s.Store.s.Runs {
		if r.ID == id {
			return r
		}
	}
	return nil
}

func (s *Scheduler) execute(sp *Spec, job *Job) {
	st := s.Store
	now := s.Clock()
	day := Day(now)
	if sp.BudgetCalls > 0 && st.Usage(day) >= sp.BudgetCalls {
		st.mu.Lock()
		job.State, job.Error = "skipped", &JobError{Kind: KindPermanent, Message: "今天的调用预算用完了"}
		job.UpdatedAt = ms(now)
		st.touch()
		st.mu.Unlock()
		return
	}
	st.mu.Lock()
	job.Attempt++
	attempt := job.Attempt
	run := s.runOf(job.RunID)
	var runCopy Run
	if run != nil {
		runCopy = *run
	}
	jobCopy := *job
	st.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), sp.Timeout)
	defer cancel()
	jc := &JobCtx{Ctx: ctx, Spec: sp, sched: s, jobID: job.ID, unit: job.UnitKey, attempt: attempt, maxAttempts: job.MaxAttempts, input: jobCopy.Input, run: runCopy}
	out, err := safeCall(sp.Handler, jc)

	end := s.Clock()
	st.mu.Lock()
	job.UpdatedAt = ms(end)
	jc.mu.Lock()
	calls, refs := jc.calls, jc.refs
	jc.mu.Unlock()
	job.Calls += calls
	job.Refs = append(job.Refs, refs...)
	switch {
	case err == nil:
		job.State, job.Output, job.Error = "succeeded", out, nil
	default:
		kind, after := classify(err)
		job.Error = &JobError{Kind: kind, Message: err.Error()}
		switch {
		case kind == KindPermanent:
			job.State = "dead"
		case kind == KindNotReady:
			job.State, job.Attempt, job.DueAt = "not_ready", attempt-1, ms(end.Add(after))
		case attempt >= job.MaxAttempts:
			job.State = "dead"
		default:
			job.State = "retrying"
			wait := time.Minute
			if len(sp.Backoff) > 0 {
				idx := attempt - 1
				if idx >= len(sp.Backoff) {
					idx = len(sp.Backoff) - 1
				}
				wait = sp.Backoff[idx]
			}
			job.DueAt = ms(end.Add(wait))
		}
	}
	st.touch()
	st.mu.Unlock()
	st.addUsage(day, calls)
}

func safeCall(h func(*JobCtx) (any, error), jc *JobCtx) (out any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = Transient(fmt.Sprintf("任务崩溃：%v", r))
		}
	}()
	return h(jc)
}

// ---- finishing runs

func (s *Scheduler) finishRuns(now time.Time) {
	st := s.Store
	st.mu.Lock()
	var finished []*Run
	for _, run := range st.s.Runs {
		if run.State != "running" {
			continue
		}
		counts := map[string]int{}
		open := false
		for _, j := range st.s.Jobs {
			if j.RunID != run.ID {
				continue
			}
			counts["jobs"]++
			counts[j.State]++
			if j.active() {
				open = true
			}
		}
		run.Counts = counts
		if open {
			continue
		}
		done, skipped, bad := counts["succeeded"], counts["skipped"], counts["dead"]
		switch {
		case bad == 0 && done == 0 && skipped > 0:
			run.State = "skipped"
		case bad == 0:
			run.State = "succeeded"
		case done > 0:
			run.State = "partial"
		default:
			run.State = "failed"
		}
		run.FinishedAt = ms(now)
		finished = append(finished, run)
		st.touch()
	}
	st.mu.Unlock()
	for _, run := range finished {
		if sp, ok := s.Spec(run.TaskID); ok {
			s.report(run, sp)
		}
	}
}

func (s *Scheduler) report(run *Run, sp *Spec) {
	s.Store.mu.Lock()
	copied := *run
	s.Store.mu.Unlock()
	for _, l := range s.Listeners {
		l(copied, sp)
	}
}

// ---- job context

// JobCtx is what a handler sees.
type JobCtx struct {
	Ctx         context.Context
	Spec        *Spec
	sched       *Scheduler
	jobID       string
	unit        string
	attempt     int
	maxAttempts int
	input       map[string]any
	run         Run
	mu          sync.Mutex // steps may run in parallel goroutines
	calls       int
	refs        []string
}

func (c *JobCtx) Unit() string          { return c.unit }
func (c *JobCtx) Attempt() int          { return c.attempt }
func (c *JobCtx) Input() map[string]any { return c.input }
func (c *JobCtx) LastTry() bool         { return c.attempt >= c.maxAttempts }
func (c *JobCtx) Now() time.Time        { return c.sched.Clock() }
func (c *JobCtx) RunID() string         { return c.run.ID }
func (c *JobCtx) JobID() string         { return c.jobID }
func (c *JobCtx) Trigger() string       { return c.run.Trigger }
func (c *JobCtx) AddCalls(n int) {
	c.mu.Lock()
	c.calls += n
	c.mu.Unlock()
}
func (c *JobCtx) Ref(ref string) {
	c.mu.Lock()
	c.refs = append(c.refs, ref)
	c.mu.Unlock()
}
func (c *JobCtx) TradeDate() string { return tradeDate(&c.run, c.sched.Clock()) }
func (c *JobCtx) Store() *Store     { return c.sched.Store }
func (c *JobCtx) InputString(k string) string {
	v, _ := c.input[k].(string)
	return v
}

// Waits counts how many times this job answered "not ready"; those retries do not burn attempts, so
// the handler uses this to give up waiting after a while.
func (c *JobCtx) Waits() int {
	var n int
	c.sched.Store.KVGet("waits:"+c.jobID, &n)
	return n
}

func (c *JobCtx) BumpWait() { c.sched.Store.KVSet("waits:"+c.jobID, c.Waits()+1) }

// Step runs fn once per job: a retry returns the saved result instead of running it again, so work
// that was paid for (a model call) is not repeated.
func Step[T any](c *JobCtx, name string, fn func() (T, error)) (T, error) {
	st := c.sched.Store
	key := c.jobID + "/" + name
	st.mu.Lock()
	raw, ok := st.s.Steps[key]
	st.mu.Unlock()
	var zero T
	if ok {
		var saved T
		if json.Unmarshal(raw, &saved) == nil {
			return saved, nil
		}
	}
	value, err := fn()
	if err != nil {
		return zero, err
	}
	if enc, err := json.Marshal(value); err == nil {
		st.mu.Lock()
		st.s.Steps[key] = enc
		st.touch()
		st.mu.Unlock()
	}
	return value, nil
}
