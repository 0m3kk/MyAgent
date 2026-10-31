package api

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleListTools(w http.ResponseWriter, r *http.Request) {
	list, err := s.app.ToolList(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": list})
}

func (s *Server) handleGetTool(w http.ResponseWriter, r *http.Request) {
	detail, err := s.app.ToolDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "tool not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleCreateTool(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 2<<20)
	if !ok {
		return
	}
	manifest, _ := body["manifest_yaml"].(string)
	if manifest == "" {
		manifest, _ = body["manifest"].(string)
	}
	createdBy, _ := body["created_by"].(string)
	base := intFrom(body["base_version"], 0)
	res, err := s.app.CreateToolDraft(r.Context(), manifest, createdBy, base)
	if err != nil {
		writeError(
			w,
			http.StatusUnprocessableEntity,
			err.Error(),
			map[string]any{"issues": res.Issues, "version": res.Version},
		)
		return
	}
	if files, ok := body["files"].(map[string]any); ok {
		for path, content := range files {
			if _, err := s.app.WriteToolFile(
				r.Context(),
				res.Tool.ID,
				res.Version.ID,
				path,
				contentString(content),
			); err != nil {
				writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
				return
			}
		}
	}
	detail, _ := s.app.ToolDetail(r.Context(), res.Tool.ID)
	writeJSON(w, http.StatusOK, map[string]any{"result": res, "detail": detail})
}

func (s *Server) handleDeleteTool(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Store.DeleteTool(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleWriteToolFile(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 2<<20)
	if !ok {
		return
	}
	path, _ := body["path"].(string)
	content, _ := body["content"].(string)
	tv, err := s.app.WriteToolFile(
		r.Context(),
		r.PathValue("id"),
		r.PathValue("vid"),
		path,
		content,
	)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, tv)
}

func (s *Server) handleReadToolFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	content, err := s.app.ReadToolFile(r.Context(), r.PathValue("id"), r.PathValue("vid"), path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "content": content})
}

func (s *Server) handleTestTool(w http.ResponseWriter, r *http.Request) {
	results, err := s.app.ToolTest(r.Context(), r.PathValue("id"), r.PathValue("vid"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}
	passed := len(results) > 0
	for _, res := range results {
		if !res.Passed {
			passed = false
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"passed": passed, "tests": results})
}

func (s *Server) handleApproveTool(w http.ResponseWriter, r *http.Request) {
	if err := s.app.ApproveToolVersion(
		r.Context(),
		r.PathValue("id"),
		r.PathValue("vid"),
	); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRunTool(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 2<<20)
	if !ok {
		return
	}
	versionID, _ := body["version_id"].(string)
	inputs := mapFrom(body["inputs"])
	run, err := s.app.RunToolDirect(r.Context(), r.PathValue("id"), versionID, inputs)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": run})
}

func (s *Server) handleBindToolSecret(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	logical, _ := body["logical_name"].(string)
	secretID, _ := body["secret_id"].(string)
	if err := s.app.BindToolSecret(r.Context(), r.PathValue("id"), logical, secretID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func contentString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}
