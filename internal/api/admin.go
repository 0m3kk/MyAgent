package api

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"myagent/internal/model"
)

func (s *Server) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	list, err := s.app.PendingApprovals(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": list})
}

func (s *Server) handleApprovalDecision(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	decision, _ := body["decision"].(string)
	comment, _ := body["comment"].(string)
	ap, err := s.app.ResolveApproval(r.Context(), r.PathValue("id"), decision, comment)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, ap)
}

func (s *Server) handleListSecrets(w http.ResponseWriter, r *http.Request) {
	list, err := s.app.Secrets.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(
		w,
		http.StatusOK,
		map[string]any{
			"secrets":         list,
			"key_kind":        s.app.KeyKind(),
			"key_description": s.app.KeyDescription(),
		},
	)
}

func (s *Server) handlePutSecret(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	name, _ := body["name"].(string)
	kind, _ := body["kind"].(string)
	description, _ := body["description"].(string)
	value, _ := body["value"].(string)
	expires := int64From(body["expires_at"], 0)
	ask, _ := body["ask_each_run"].(bool)
	meta, err := s.app.PutSecret(r.Context(), name, kind, description, value, expires, ask)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (s *Server) handleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Secrets.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleKeyInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(
		w,
		http.StatusOK,
		map[string]any{"kind": s.app.KeyKind(), "description": s.app.KeyDescription()},
	)
}

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	list, err := s.app.Store.ListModelProfiles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": list})
}

func (s *Server) handleSaveModel(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	id, _ := body["id"].(string)
	var existing model.ModelProfile
	if id != "" {
		existing, _ = s.app.Store.GetModelProfile(r.Context(), id)
	}
	name, _ := body["name"].(string)
	baseURL, _ := body["base_url"].(string)
	modelName, _ := body["model"].(string)
	role, _ := body["role"].(string)
	apiKey, _ := body["api_key"].(string)
	if baseURL == "" || modelName == "" {
		writeError(w, http.StatusBadRequest, "base_url and model are required", nil)
		return
	}
	paramsRaw := existing.Params
	if p, ok := body["params"].(map[string]any); ok {
		paramsRaw, _ = json.Marshal(p)
	}
	caps := existing.Capabilities
	if c, ok := body["capabilities"]; ok {
		caps, _ = json.Marshal(c)
	}
	p, err := s.app.SaveModelProfile(r.Context(), model.ModelProfile{
		ID:           existing.ID,
		Name:         name,
		BaseURL:      baseURL,
		Model:        modelName,
		Role:         role,
		Params:       paramsRaw,
		Capabilities: caps,
		SecretID:     existing.SecretID,
		CreatedAt:    existing.CreatedAt,
	}, apiKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteModelProfile(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleProbeModel(w http.ResponseWriter, r *http.Request) {
	caps, err := s.app.ProbeModel(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, caps)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.app.GetSettings())
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	settings, err := s.app.UpdateSettings(r.Context(), body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleEnvironment(w http.ResponseWriter, r *http.Request) {
	profiles, _ := s.app.Store.ListModelProfiles(r.Context())
	bins := map[string]string{}
	for _, b := range []string{"python3", "node", "git", "rg", "jq"} {
		if p, err := exec.LookPath(b); err == nil {
			bins[b] = p
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workspace": s.app.Workspace, "os": runtime.GOOS, "arch": runtime.GOARCH,
		"date": time.Now().Format(time.RFC3339), "runtimes": bins, "models": profiles,
		"key_kind": s.app.KeyKind(), "key_description": s.app.KeyDescription(),
	})
}

func int64From(v any, def int64) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	}
	return def
}
