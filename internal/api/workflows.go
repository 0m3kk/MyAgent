package api

import (
	"errors"
	"net/http"

	"myagent/internal/app"
)

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	list, err := s.app.WorkflowList(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflows": list})
}

func (s *Server) handleValidateWorkflow(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 2<<20)
	if !ok {
		return
	}
	yamlText, _ := body["yaml"].(string)
	forRun, _ := body["for_run"].(bool)
	_, res, err := s.app.ValidateWorkflowYAML(r.Context(), yamlText, forRun)
	if err != nil && len(res.Errors) == 0 {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSaveWorkflow(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 2<<20)
	if !ok {
		return
	}
	yamlText, _ := body["yaml"].(string)
	changeNote, _ := body["change_note"].(string)
	base := intFrom(body["base_version"], 0)
	createdBy, _ := body["created_by"].(string)
	if createdBy == "" {
		createdBy = "user"
	}
	res, err := s.app.SaveWorkflowDraft(r.Context(), yamlText, changeNote, createdBy, base)
	if err != nil && res.Version.ID == "" {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), map[string]any{"validation": res.Validation})
		return
	}
	if res.Version.ID == "" {
		writeError(w, http.StatusUnprocessableEntity, "workflow has validation errors", map[string]any{"validation": res.Validation})
		return
	}
	if approve, _ := body["approve"].(bool); approve {
		if err := s.app.ApproveWorkflowVersion(r.Context(), res.Workflow.ID, res.Version.ID); err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
			return
		}
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	detail, err := s.app.WorkflowDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "workflow not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleWorkflowEditChat(w http.ResponseWriter, r *http.Request) {
	conv, err := s.app.StartWorkflowEditChat(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, conv)
}

func (s *Server) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Store.DeleteWorkflow(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleApproveWorkflow(w http.ResponseWriter, r *http.Request) {
	if err := s.app.ApproveWorkflowVersion(r.Context(), r.PathValue("id"), r.PathValue("vid")); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRunWorkflow(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 2<<20)
	if !ok {
		return
	}
	inputs := mapFrom(body["inputs"])
	versionID, _ := body["version_id"].(string)
	source, _ := body["source"].(string)
	if source == "" {
		source = r.URL.Query().Get("source")
	}
	if source == "" {
		source = "manual"
	}
	if source != "manual" && source != "cli" && source != "chat" {
		source = "manual"
	}
	unattended, _ := body["unattended"].(string)
	run, err := s.app.RunWorkflow(r.Context(), r.PathValue("id"), versionID, inputs, source, "", false, unattended, "")
	if err != nil {
		if errors.Is(err, app.ErrConcurrency) {
			writeError(w, http.StatusConflict, err.Error(), nil)
			return
		}
		writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": run})
}

func (s *Server) handleListTriggers(w http.ResponseWriter, r *http.Request) {
	list, err := s.app.TriggerList(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"triggers": list})
}

func (s *Server) handleCreateTrigger(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	kind, _ := body["kind"].(string)
	enabled := true
	if v, ok := body["enabled"].(bool); ok {
		enabled = v
	}
	unattended, _ := body["unattended"].(string)
	concurrency, _ := body["concurrency"].(string)
	tr, err := s.app.CreateTriggerRecord(r.Context(), r.PathValue("id"), kind, mapFrom(body["config"]),
		mapFrom(body["inputs"]), enabled, unattended, concurrency)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, tr)
}

func (s *Server) handleUpdateTrigger(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	tr, err := s.app.UpdateTriggerRecord(r.Context(), r.PathValue("id"), body)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, tr)
}

func (s *Server) handleDeleteTrigger(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteTriggerRecord(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		token = r.Header.Get("X-MyAgent-Token")
	}
	run, err := s.app.TriggerWebhook(r.Context(), r.PathValue("id"), token, body)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"run_id": run.ID, "status": run.Status})
}

func intFrom(v any, def int) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	}
	return def
}

func mapFrom(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
