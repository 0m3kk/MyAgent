package api

import (
	"net/http"

	"myagent/internal/model"
	"myagent/internal/workflow"
)

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	f := model.RunFilter{
		Status: r.URL.Query().Get("status"),
		Source: r.URL.Query().Get("source"),
		Limit:  int(parseInt64(r.URL.Query().Get("limit"), 100)),
		Offset: int(parseInt64(r.URL.Query().Get("offset"), 0)),
	}
	if wf := r.URL.Query().Get("workflow"); wf != "" {
		if w, err := s.app.Store.GetWorkflowByName(r.Context(), wf); err == nil {
			f.TargetID = w.ID
		} else {
			f.TargetID = wf
		}
	}
	runs, err := s.app.Store.ListRuns(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.app.Store.GetRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "run not found", nil)
		return
	}
	events, _ := s.app.Store.RunEvents(r.Context(), run.ID, 0, 1000)
	approvals, _ := s.app.Store.ApprovalsForRun(r.Context(), run.ID)
	outputs, _ := s.app.RunOutputs(r.Context(), run.ID)
	stepOutputs := s.app.StepOutputs(r.Context(), run.ID)
	resp := map[string]any{
		"run":          run,
		"events":       events,
		"approvals":    approvals,
		"outputs":      outputs,
		"step_outputs": stepOutputs,
	}
	if run.TargetKind == "workflow" {
		if v, err := s.app.Store.GetWorkflowVersion(r.Context(), run.TargetVer); err == nil {
			resp["version"] = v
			if spec, err := workflow.Parse([]byte(v.YAML)); err == nil {
				resp["spec"] = spec
			}
			if w, err := s.app.Store.GetWorkflowByID(r.Context(), v.WorkflowID); err == nil {
				resp["workflow"] = w
			}
		} else if w, err := s.app.Store.GetWorkflowByID(r.Context(), run.TargetID); err == nil {
			// The exact version was removed, but the workflow still exists.
			// Fall back to its current graph so the canvas remains useful.
			resp["workflow"] = w
			if w.CurrentVersionID != "" {
				if v, err := s.app.Store.GetWorkflowVersion(
					r.Context(),
					w.CurrentVersionID,
				); err == nil {
					resp["version"] = v
					if spec, err := workflow.Parse([]byte(v.YAML)); err == nil {
						resp["spec"] = spec
						resp["spec_stale"] = true
					}
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	after := parseInt64(r.URL.Query().Get("after"), 0)
	limit := int(parseInt64(r.URL.Query().Get("limit"), 1000))
	events, err := s.app.Store.RunEvents(r.Context(), r.PathValue("id"), after, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) handlePauseRun(w http.ResponseWriter, r *http.Request) {
	if err := s.app.PauseRun(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleResumeRun(w http.ResponseWriter, r *http.Request) {
	if err := s.app.ResumeRun(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	if err := s.app.CancelRun(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRerun(w http.ResponseWriter, r *http.Request) {
	run, err := s.app.Rerun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": run})
}

func (s *Server) handleAnalyzeRun(w http.ResponseWriter, r *http.Request) {
	analysis, err := s.app.AnalyzeRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, analysis)
}
