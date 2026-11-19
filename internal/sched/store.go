package sched

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Run is one planned execution of a task (one period).
type Run struct {
	ID           string         `json:"run_id"`
	TaskID       string         `json:"task_id"`
	PeriodKey    string         `json:"period_key"`
	Trigger      string         `json:"trigger"` // schedule | catchup | manual
	ScheduledFor int64          `json:"scheduled_for"`
	State        string         `json:"state"` // planned | waiting | running | succeeded | partial | failed | skipped | expired
	Counts       map[string]int `json:"counts,omitempty"`
	StartedAt    int64          `json:"started_at,omitempty"`
	FinishedAt   int64          `json:"finished_at,omitempty"`
	Note         string         `json:"note,omitempty"`
	Units        []UnitSpec     `json:"units,omitempty"`
	Input        map[string]any `json:"input,omitempty"`
}

// UnitSpec names one unit of a manual run.
type UnitSpec struct {
	Key      string         `json:"key"`
	Priority int            `json:"priority,omitempty"`
	Input    map[string]any `json:"input,omitempty"`
}

// JobError is the last failure of a job.
type JobError struct {
	Kind    Kind   `json:"kind"`
	Message string `json:"message"`
}

// Job is one unit (one stock, usually) of a run.
type Job struct {
	ID          string         `json:"job_id"`
	RunID       string         `json:"run_id"`
	TaskID      string         `json:"task_id"`
	UnitKey     string         `json:"unit_key"`
	Priority    int            `json:"priority"`
	State       string         `json:"state"` // pending | running | retrying | not_ready | succeeded | dead | skipped
	Attempt     int            `json:"attempt"`
	MaxAttempts int            `json:"max_attempts"`
	DueAt       int64          `json:"due_at"`
	Input       map[string]any `json:"input,omitempty"`
	Output      any            `json:"output,omitempty"`
	Error       *JobError      `json:"error,omitempty"`
	Refs        []string       `json:"refs,omitempty"`
	Calls       int            `json:"calls,omitempty"`
	UpdatedAt   int64          `json:"updated_at"`
}

func (j *Job) active() bool {
	switch j.State {
	case "pending", "running", "retrying", "not_ready":
		return true
	}
	return false
}

// TriggerOverride is a user edit of when a system task runs. Empty fields keep the Spec default.
type TriggerOverride struct {
	Kind      string `json:"kind,omitempty"`
	At        string `json:"at,omitempty"`
	WindowMin int    `json:"window_min,omitempty"`
	Days      string `json:"days,omitempty"`
	EveryMin  int    `json:"every_min,omitempty"`
}

type state struct {
	Seq       int                        `json:"seq"`
	FirstTick int64                      `json:"first_tick"`
	Runs      []*Run                     `json:"runs"`
	Jobs      []*Job                     `json:"jobs"`
	Steps     map[string]json.RawMessage `json:"steps"`
	Usage     map[string]int             `json:"usage"`
	Paused    map[string]bool            `json:"paused"`
	Deleted   map[string]bool            `json:"deleted,omitempty"`
	Overrides map[string]TriggerOverride `json:"overrides,omitempty"`
	KV        map[string]json.RawMessage `json:"kv"`
}

// Store keeps scheduler state in one JSON file. The data is small (days of runs, not years), the
// mutations are batched by Flush, and no database driver is needed.
type Store struct {
	mu    sync.Mutex
	path  string
	s     state
	dirty bool
}

func OpenStore(path string) *Store {
	st := &Store{path: path, s: state{Steps: map[string]json.RawMessage{}, Usage: map[string]int{}, Paused: map[string]bool{}, Deleted: map[string]bool{}, Overrides: map[string]TriggerOverride{}, KV: map[string]json.RawMessage{}}}
	if path == "" {
		return st
	}
	if raw, err := os.ReadFile(path); err == nil {
		var loaded state
		if json.Unmarshal(raw, &loaded) == nil {
			st.s = loaded
		}
	}
	if st.s.Steps == nil {
		st.s.Steps = map[string]json.RawMessage{}
	}
	if st.s.Usage == nil {
		st.s.Usage = map[string]int{}
	}
	if st.s.Paused == nil {
		st.s.Paused = map[string]bool{}
	}
	if st.s.Deleted == nil {
		st.s.Deleted = map[string]bool{}
	}
	if st.s.Overrides == nil {
		st.s.Overrides = map[string]TriggerOverride{}
	}
	if st.s.KV == nil {
		st.s.KV = map[string]json.RawMessage{}
	}
	return st
}

// ReclaimInterrupted marks jobs left in "running" after a process exit as dead.
// Workers live only in memory; a persisted running job will never finish, and
// ActiveJob would keep pages waiting on "正在分析" forever.
func (st *Store) ReclaimInterrupted(nowMs int64) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	n := 0
	for _, j := range st.s.Jobs {
		if j.State != "running" {
			continue
		}
		j.State = "dead"
		j.Error = &JobError{Kind: KindPermanent, Message: "进程重启，上次任务中断"}
		j.UpdatedAt = nowMs
		n++
	}
	if n > 0 {
		st.touch()
	}
	return n
}

func (st *Store) touch() { st.dirty = true }

// Flush writes the state if anything changed.
func (st *Store) Flush() {
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.dirty || st.path == "" {
		st.dirty = false
		return
	}
	raw, err := json.Marshal(st.s)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(st.path), 0o755); err != nil {
		return
	}
	tmp := st.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil && os.Rename(tmp, st.path) == nil {
		st.dirty = false
	}
}

func (st *Store) nextID(prefix string) string {
	st.s.Seq++
	return prefix + itoa(st.s.Seq)
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func (st *Store) firstTick(now int64) int64 {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.s.FirstTick == 0 {
		st.s.FirstTick = now
		st.touch()
	}
	return st.s.FirstTick
}

func (st *Store) runByPeriod(taskID, period string) *Run {
	for _, r := range st.s.Runs {
		if r.TaskID == taskID && r.PeriodKey == period {
			return r
		}
	}
	return nil
}

// Runs returns a copy of the runs, newest first. taskID "" means all.
func (st *Store) Runs(taskID string, limit int) []Run {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []Run
	for i := len(st.s.Runs) - 1; i >= 0; i-- {
		r := st.s.Runs[i]
		if taskID == "" || r.TaskID == taskID {
			out = append(out, *r)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out
}

func (st *Store) Run(id string) (Run, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, r := range st.s.Runs {
		if r.ID == id {
			return *r, true
		}
	}
	return Run{}, false
}

// Jobs returns copies of a run's jobs.
func (st *Store) Jobs(runID string) []Job {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []Job
	for _, j := range st.s.Jobs {
		if j.RunID == runID {
			out = append(out, *j)
		}
	}
	return out
}

// JobsOf returns copies of the jobs of a task for one unit key, newest first.
func (st *Store) JobsOf(taskID, unit string) []Job {
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []Job
	for i := len(st.s.Jobs) - 1; i >= 0; i-- {
		j := st.s.Jobs[i]
		if j.TaskID == taskID && (unit == "" || j.UnitKey == unit) {
			out = append(out, *j)
		}
	}
	return out
}

// ActiveJob returns the unfinished job for a task and unit, or a pending manual run's placeholder.
func (st *Store) ActiveJob(taskID, unit string) (Job, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, j := range st.s.Jobs {
		if j.TaskID == taskID && j.UnitKey == unit && j.active() {
			return *j, true
		}
	}
	for _, r := range st.s.Runs {
		if r.TaskID != taskID || (r.State != "planned" && r.State != "waiting") {
			continue
		}
		for _, u := range r.Units {
			if u.Key == unit {
				return Job{RunID: r.ID, TaskID: taskID, UnitKey: unit, State: "pending"}, true
			}
		}
	}
	return Job{}, false
}

// StepNames lists the finished steps of a job.
func (st *Store) StepNames(jobID string) []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	prefix := jobID + "/"
	var out []string
	for k := range st.s.Steps {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			out = append(out, k[len(prefix):])
		}
	}
	sort.Strings(out)
	return out
}

func (st *Store) usage(day string) int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Usage[day]
}

// Usage is the number of budgeted calls spent on a day.
func (st *Store) Usage(day string) int { return st.usage(day) }

func (st *Store) addUsage(day string, n int) {
	if n == 0 {
		return
	}
	st.mu.Lock()
	st.s.Usage[day] += n
	st.touch()
	st.mu.Unlock()
}

// KVGet reads a small value kept by tasks (wait counters, markers).
func (st *Store) KVGet(key string, out any) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	raw, ok := st.s.KV[key]
	if !ok {
		return false
	}
	return json.Unmarshal(raw, out) == nil
}

func (st *Store) KVSet(key string, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	st.mu.Lock()
	st.s.KV[key] = raw
	st.touch()
	st.mu.Unlock()
}

// Paused tells whether a task is switched off.
func (st *Store) Paused(taskID string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Paused[taskID]
}

func (st *Store) SetPaused(taskID string, paused bool) {
	st.mu.Lock()
	if paused {
		st.s.Paused[taskID] = true
	} else {
		delete(st.s.Paused, taskID)
	}
	st.touch()
	st.mu.Unlock()
}

// Deleted tells whether a system task was soft-deleted (hidden and not planned).
func (st *Store) Deleted(taskID string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Deleted[taskID]
}

// SoftDelete marks a task deleted and paused so it stops running until Restore.
func (st *Store) SoftDelete(taskID string) {
	st.mu.Lock()
	st.s.Deleted[taskID] = true
	st.s.Paused[taskID] = true
	st.touch()
	st.mu.Unlock()
}

// Restore clears soft-delete and re-enables the task.
func (st *Store) Restore(taskID string) {
	st.mu.Lock()
	delete(st.s.Deleted, taskID)
	delete(st.s.Paused, taskID)
	st.touch()
	st.mu.Unlock()
}

// Override returns a user's trigger edit for a task, if any.
func (st *Store) Override(taskID string) (TriggerOverride, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	o, ok := st.s.Overrides[taskID]
	return o, ok
}

// SetOverride writes or clears a trigger edit. A zero value clears the override.
func (st *Store) SetOverride(taskID string, o TriggerOverride) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if o.Kind == "" && o.At == "" && o.Days == "" && o.WindowMin == 0 && o.EveryMin == 0 {
		delete(st.s.Overrides, taskID)
	} else {
		st.s.Overrides[taskID] = o
	}
	st.touch()
}

// prune drops old runs, their jobs and steps. Frequent tasks keep two days, the rest keep thirty.
func (st *Store) prune(now int64, frequent map[string]bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	const day = int64(86400000)
	keepRuns := st.s.Runs[:0]
	gone := map[string]bool{}
	for _, r := range st.s.Runs {
		ttl := 30 * day
		if frequent[r.TaskID] {
			ttl = 2 * day
		}
		if r.FinishedAt > 0 && now-r.FinishedAt > ttl {
			gone[r.ID] = true
			continue
		}
		keepRuns = append(keepRuns, r)
	}
	if len(gone) == 0 {
		return
	}
	st.s.Runs = keepRuns
	keepJobs := st.s.Jobs[:0]
	dropped := map[string]bool{}
	for _, j := range st.s.Jobs {
		if gone[j.RunID] {
			dropped[j.ID] = true
			continue
		}
		keepJobs = append(keepJobs, j)
	}
	st.s.Jobs = keepJobs
	for k := range st.s.Steps {
		for id := range dropped {
			if len(k) > len(id) && k[:len(id)+1] == id+"/" {
				delete(st.s.Steps, k)
				break
			}
		}
	}
	st.touch()
}
