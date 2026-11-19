package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go-harness/internal/sched"
)

// registerSysRoutes exposes system-task list/edit/soft-delete and run I/O for the tasks page.
func (s *Server) registerSysRoutes(mux *http.ServeMux) {
	const base = "/api/scan/sys"
	mux.HandleFunc("GET "+base+"/tasks", s.handleSysTasks)
	mux.HandleFunc("PUT "+base+"/tasks/{id}", s.handleSysTaskPut)
	mux.HandleFunc("DELETE "+base+"/tasks/{id}", s.handleSysTaskDelete)
	mux.HandleFunc("POST "+base+"/tasks/{id}/restore", s.handleSysTaskRestore)
	mux.HandleFunc("POST "+base+"/tasks/{id}/run", s.handleSysTaskRun)
	mux.HandleFunc("GET "+base+"/tasks/{id}/runs", s.handleSysTaskRuns)
	mux.HandleFunc("GET "+base+"/runs/{run_id}/jobs", s.handleSysRunJobs)
}

func (s *Server) sysSpec(id string) (*sched.Spec, bool) {
	sp, ok := s.sch.Spec(id)
	if !ok || sp.Category == "" || sp.Category == sched.CatMine || strings.HasPrefix(id, "custom.") {
		return nil, false
	}
	return sp, true
}

func triggerView(t sched.Trigger) map[string]any {
	return map[string]any{
		"kind": t.Kind, "at": t.At, "window_min": t.WindowMin, "days": t.Days, "every_min": t.EveryMin,
	}
}

func (s *Server) sysTaskItem(sp *sched.Spec) map[string]any {
	def := sp.Trigger
	eff := s.sch.EffectiveTrigger(sp)
	item := map[string]any{
		"task_id": sp.ID, "title": sp.Title, "category": sp.Category, "note": sp.Note,
		"queue": sp.Queue, "frequent": sp.Frequent, "editable": true,
		"default_trigger": triggerView(def), "trigger": triggerView(eff),
		"trigger_text": scheduleText(eff),
		"paused":       s.sch.Store.Paused(sp.ID),
		"deleted":      s.sch.Store.Deleted(sp.ID),
	}
	if len(sp.Trace) > 0 {
		item["trace"] = sp.Trace
	}
	if at, ok := s.nextDaily(eff, s.now()); ok {
		item["next"] = at.Format("2006-01-02 15:04")
		item["when_text"] = whenText(at, s.now())
	}
	if runs := s.sch.Store.Runs(sp.ID, 1); len(runs) > 0 {
		r := runs[0]
		item["last"] = map[string]any{
			"run_id": r.ID, "state": r.State, "trigger": r.Trigger,
			"at": formatRunAt(r), "counts": r.Counts, "note": r.Note, "input": r.Input,
		}
	}
	return item
}

func formatRunAt(r sched.Run) string {
	ms := r.FinishedAt
	if ms == 0 {
		ms = r.StartedAt
	}
	if ms == 0 {
		ms = r.ScheduledFor
	}
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).In(sched.Zone).Format("01-02 15:04")
}

func (s *Server) handleSysTasks(w http.ResponseWriter, r *http.Request) {
	if s.sch == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "调度还没有启动"})
		return
	}
	cat := r.URL.Query().Get("category")
	includeDeleted := r.URL.Query().Get("include_deleted") == "1"
	var tasks []map[string]any
	var deleted []map[string]any
	for _, sp := range s.sch.Specs() {
		if sp.Category == "" || sp.Category == sched.CatMine || strings.HasPrefix(sp.ID, "custom.") {
			continue
		}
		if cat != "" && sp.Category != cat {
			continue
		}
		item := s.sysTaskItem(sp)
		if s.sch.Store.Deleted(sp.ID) {
			if includeDeleted {
				deleted = append(deleted, item)
			}
			continue
		}
		tasks = append(tasks, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tasks": tasks, "deleted": deleted,
		"categories": []map[string]string{
			{"key": sched.CatScan, "label": "研判战绩"},
			{"key": sched.CatMarket, "label": "行情"},
			{"key": sched.CatNotify, "label": "通知"},
			{"key": sched.CatWatch, "label": "盯盘"},
		},
	})
}

func (s *Server) handleSysTaskPut(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sp, ok := s.sysSpec(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("没有这个系统任务"))
		return
	}
	var body struct {
		Paused  *bool `json:"paused"`
		Trigger *struct {
			Kind      string `json:"kind"`
			At        string `json:"at"`
			WindowMin int    `json:"window_min"`
			Days      string `json:"days"`
			EveryMin  int    `json:"every_min"`
		} `json:"trigger"`
	}
	_ = readJSON(r, &body)
	if body.Paused != nil {
		if s.sch.Store.Deleted(id) && !*body.Paused {
			writeError(w, http.StatusConflict, fmt.Errorf("任务已删除，请先恢复"))
			return
		}
		s.sch.Store.SetPaused(id, *body.Paused)
	}
	if body.Trigger != nil {
		tr := body.Trigger
		kind := tr.Kind
		if kind == "" {
			kind = sp.Trigger.Kind
		}
		switch kind {
		case "daily":
			if tr.At == "" {
				writeError(w, http.StatusBadRequest, fmt.Errorf("请填写触发时刻，例如 20:30"))
				return
			}
			if _, err := time.Parse("15:04", tr.At); err != nil {
				writeError(w, http.StatusBadRequest, fmt.Errorf("时刻格式不对，用 20:30 这种"))
				return
			}
		case "interval", "every":
			if tr.EveryMin <= 0 {
				writeError(w, http.StatusBadRequest, fmt.Errorf("请填写间隔分钟"))
				return
			}
		default:
			writeError(w, http.StatusBadRequest, fmt.Errorf("不支持的触发类型"))
			return
		}
		days := tr.Days
		if days == "" {
			days = sp.Trigger.Days
			if days == "" {
				days = "trading"
			}
		}
		if days != "trading" && days != "any" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("天数只能是 trading 或 any"))
			return
		}
		s.sch.Store.SetOverride(id, sched.TriggerOverride{
			Kind: kind, At: tr.At, WindowMin: tr.WindowMin, Days: days, EveryMin: tr.EveryMin,
		})
	}
	s.sch.Store.Flush()
	sp, _ = s.sch.Spec(id)
	writeJSON(w, http.StatusOK, s.sysTaskItem(sp))
}

func (s *Server) handleSysTaskDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.sysSpec(id); !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("没有这个系统任务"))
		return
	}
	s.sch.Store.SoftDelete(id)
	s.sch.Store.Flush()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": true})
}

func (s *Server) handleSysTaskRestore(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sp, ok := s.sysSpec(id)
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("没有这个系统任务"))
		return
	}
	s.sch.Store.Restore(id)
	s.sch.Store.Flush()
	writeJSON(w, http.StatusOK, s.sysTaskItem(sp))
}

func (s *Server) handleSysTaskRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.sysSpec(id); !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("没有这个系统任务"))
		return
	}
	run, err := s.sch.Manual(id, nil, map[string]any{"on_demand": true, "reason": "tasks_page"})
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	s.sch.Tick()
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": true, "run_id": run.ID})
}

func (s *Server) handleSysTaskRuns(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.sch.Spec(id); !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("没有这个任务"))
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	runs := s.sch.Store.Runs(id, limit)
	out := make([]map[string]any, 0, len(runs))
	for _, run := range runs {
		out = append(out, map[string]any{
			"run_id": run.ID, "task_id": run.TaskID, "state": run.State, "trigger": run.Trigger,
			"period_key": run.PeriodKey, "scheduled_for": run.ScheduledFor,
			"started_at": run.StartedAt, "finished_at": run.FinishedAt,
			"at": formatRunAt(run), "counts": run.Counts, "note": run.Note, "input": run.Input,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out})
}

func (s *Server) handleSysRunJobs(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	if _, ok := s.sch.Store.Run(runID); !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("没有这次运行"))
		return
	}
	jobs := s.sch.Store.Jobs(runID)
	out := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		item := map[string]any{
			"job_id": j.ID, "run_id": j.RunID, "task_id": j.TaskID, "unit_key": j.UnitKey,
			"state": j.State, "attempt": j.Attempt, "input": j.Input, "output": j.Output,
		}
		if j.Error != nil {
			item["error"] = map[string]any{"kind": j.Error.Kind, "message": j.Error.Message}
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}
