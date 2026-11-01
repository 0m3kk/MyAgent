package api

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"myagent/internal/app"
)

type Server struct {
	app   *app.App
	token string
	uiDir string
	mux   *http.ServeMux
}

func New(a *app.App, token, uiDir string) *Server {
	s := &Server{app: a, token: token, uiDir: uiDir, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.recoverMW(s.securityMW(s.authMW(s.mux)))
}

func (s *Server) authMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/api/health" || p == "/api/dev-token" || strings.HasPrefix(p, "/api/hooks/") ||
			!strings.HasPrefix(p, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		tok := ""
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			tok = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
		if tok == "" {
			tok = r.URL.Query().Get("token")
		}
		if tok == "" {
			if c, err := r.Cookie("myagent_session"); err == nil {
				tok = c.Value
			}
		}
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v", rec)
				writeError(w, http.StatusInternalServerError, "internal error", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/dev-token", s.handleDevToken)
	s.mux.HandleFunc("GET /api/bootstrap", s.handleBootstrap)
	s.mux.HandleFunc("GET /api/events", s.handleEvents)
	s.mux.HandleFunc("GET /api/events/replay", s.handleEventsReplay)

	s.mux.HandleFunc("GET /api/workspaces", s.handleListWorkspaces)
	s.mux.HandleFunc("POST /api/workspaces", s.handleCreateWorkspace)
	s.mux.HandleFunc("POST /api/workspaces/import", s.handleImportWorkspace)
	s.mux.HandleFunc("PATCH /api/workspaces/{id}", s.handleUpdateWorkspace)
	s.mux.HandleFunc("DELETE /api/workspaces/{id}", s.handleDeleteWorkspace)
	s.mux.HandleFunc("POST /api/workspaces/{id}/activate", s.handleActivateWorkspace)
	s.mux.HandleFunc("GET /api/workspaces/{id}/export", s.handleExportWorkspace)

	s.mux.HandleFunc("GET /api/conversations", s.handleListConversations)
	s.mux.HandleFunc("GET /api/conversations/active", s.handleActiveConversations)
	s.mux.HandleFunc("POST /api/conversations", s.handleCreateConversation)
	s.mux.HandleFunc("GET /api/conversations/{id}", s.handleGetConversation)
	s.mux.HandleFunc("PATCH /api/conversations/{id}", s.handleRenameConversation)
	s.mux.HandleFunc("POST /api/conversations/{id}/title", s.handleGenerateConversationTitle)
	s.mux.HandleFunc("DELETE /api/conversations/{id}", s.handleDeleteConversation)
	s.mux.HandleFunc("POST /api/conversations/{id}/messages", s.handleSendMessage)
	s.mux.HandleFunc("POST /api/conversations/{id}/stop", s.handleStopChat)
	s.mux.HandleFunc("POST /api/conversations/{id}/save-workflow", s.handleSaveAsWorkflow)
	s.mux.HandleFunc("POST /api/conversations/{id}/run-workflow", s.handleRunWorkflowInChat)
	s.mux.HandleFunc("POST /api/chat", s.handleChat)

	s.mux.HandleFunc("GET /api/workflows", s.handleListWorkflows)
	s.mux.HandleFunc("POST /api/workflows/validate", s.handleValidateWorkflow)
	s.mux.HandleFunc("POST /api/workflows", s.handleSaveWorkflow)
	s.mux.HandleFunc("GET /api/workflows/{id}", s.handleGetWorkflow)
	s.mux.HandleFunc("PATCH /api/workflows/{id}", s.handleUpdateWorkflowOrganization)
	s.mux.HandleFunc("DELETE /api/workflows/{id}", s.handleDeleteWorkflow)
	s.mux.HandleFunc("POST /api/workflows/{id}/edit-chat", s.handleWorkflowEditChat)
	s.mux.HandleFunc("POST /api/workflows/{id}/versions/{vid}/approve", s.handleApproveWorkflow)
	s.mux.HandleFunc("POST /api/workflows/{id}/run", s.handleRunWorkflow)
	s.mux.HandleFunc("GET /api/workflows/{id}/triggers", s.handleListTriggers)
	s.mux.HandleFunc("POST /api/workflows/{id}/triggers", s.handleCreateTrigger)
	s.mux.HandleFunc("PATCH /api/triggers/{id}", s.handleUpdateTrigger)
	s.mux.HandleFunc("DELETE /api/triggers/{id}", s.handleDeleteTrigger)
	s.mux.HandleFunc("POST /api/hooks/{id}", s.handleWebhook)

	s.mux.HandleFunc("GET /api/runs", s.handleListRuns)
	s.mux.HandleFunc("GET /api/runs/{id}", s.handleGetRun)
	s.mux.HandleFunc("GET /api/runs/{id}/events", s.handleRunEvents)
	s.mux.HandleFunc("POST /api/runs/{id}/pause", s.handlePauseRun)
	s.mux.HandleFunc("POST /api/runs/{id}/resume", s.handleResumeRun)
	s.mux.HandleFunc("POST /api/runs/{id}/cancel", s.handleCancelRun)
	s.mux.HandleFunc("POST /api/runs/{id}/rerun", s.handleRerun)
	s.mux.HandleFunc("POST /api/runs/{id}/analyze", s.handleAnalyzeRun)

	s.mux.HandleFunc("GET /api/tools", s.handleListTools)
	s.mux.HandleFunc("POST /api/tools", s.handleCreateTool)
	s.mux.HandleFunc("GET /api/tools/{id}", s.handleGetTool)
	s.mux.HandleFunc("PATCH /api/tools/{id}", s.handleUpdateToolOrganization)
	s.mux.HandleFunc("DELETE /api/tools/{id}", s.handleDeleteTool)
	s.mux.HandleFunc("PUT /api/tools/{id}/versions/{vid}/files", s.handleWriteToolFile)
	s.mux.HandleFunc("GET /api/tools/{id}/versions/{vid}/files", s.handleReadToolFile)
	s.mux.HandleFunc("POST /api/tools/{id}/versions/{vid}/test", s.handleTestTool)
	s.mux.HandleFunc("POST /api/tools/{id}/versions/{vid}/approve", s.handleApproveTool)
	s.mux.HandleFunc("POST /api/tools/{id}/run", s.handleRunTool)
	s.mux.HandleFunc("POST /api/tools/{id}/secrets", s.handleBindToolSecret)

	s.mux.HandleFunc("GET /api/folders", s.handleListFolders)
	s.mux.HandleFunc("POST /api/folders", s.handleCreateFolder)
	s.mux.HandleFunc("PATCH /api/folders/{id}", s.handleUpdateFolder)
	s.mux.HandleFunc("DELETE /api/folders/{id}", s.handleDeleteFolder)
	s.mux.HandleFunc("POST /api/library/organize", s.handleOrganizeLibrary)

	s.mux.HandleFunc("GET /api/approvals", s.handleListApprovals)
	s.mux.HandleFunc("POST /api/approvals/{id}/decision", s.handleApprovalDecision)

	s.mux.HandleFunc("GET /api/secrets", s.handleListSecrets)
	s.mux.HandleFunc("POST /api/secrets", s.handlePutSecret)
	s.mux.HandleFunc("DELETE /api/secrets/{id}", s.handleDeleteSecret)
	s.mux.HandleFunc("GET /api/secrets/key-info", s.handleKeyInfo)

	s.mux.HandleFunc("GET /api/models", s.handleListModels)
	s.mux.HandleFunc("POST /api/models", s.handleSaveModel)
	s.mux.HandleFunc("DELETE /api/models/{id}", s.handleDeleteModel)
	s.mux.HandleFunc("POST /api/models/{id}/probe", s.handleProbeModel)

	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("PATCH /api/settings", s.handleUpdateSettings)
	s.mux.HandleFunc("GET /api/environment", s.handleEnvironment)

	s.mux.HandleFunc("GET /", s.serveUI)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(
		w,
		http.StatusOK,
		map[string]any{
			"ok":      true,
			"version": s.app.Config().Version,
			"time":    time.Now().UnixMilli(),
		},
	)
}

func (s *Server) handleDevToken(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("MYAGENT_DEV") != "1" {
		writeError(w, http.StatusNotFound, "not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": s.token})
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	b, err := s.app.Bootstrap(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) serveUI(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, "not found", nil)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", nil)
		return
	}
	clean := filepath.Clean(r.URL.Path)
	if clean == "/" || clean == "." {
		clean = "/index.html"
	}
	full := filepath.Join(s.uiDir, clean)
	if !strings.HasPrefix(full, filepath.Clean(s.uiDir)+string(os.PathSeparator)) &&
		full != filepath.Join(filepath.Clean(s.uiDir), "index.html") {
		writeError(w, http.StatusBadRequest, "invalid path", nil)
		return
	}
	if info, err := os.Stat(full); err == nil && !info.IsDir() {
		http.ServeFile(w, r, full)
		return
	}
	index := filepath.Join(s.uiDir, "index.html")
	if _, err := os.Stat(index); err != nil {
		writeError(
			w,
			http.StatusNotFound,
			"UI is not built. Run `make ui` or use the Vite dev server.",
			nil,
		)
		return
	}
	// The local UI gets a SameSite session cookie so reloads keep working. The
	// token query parameter is still accepted for CLI tools and SSE clients.
	http.SetCookie(
		w,
		&http.Cookie{
			Name:     "myagent_session",
			Value:    s.token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   30 * 24 * 3600,
		},
	)
	http.ServeFile(w, r, index)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string, details any) {
	out := map[string]any{"error": msg}
	if details != nil {
		out["details"] = details
	}
	writeJSON(w, status, out)
}

func readJSON(w http.ResponseWriter, r *http.Request, limit int64) (map[string]any, bool) {
	if limit <= 0 {
		limit = 2 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "cannot read request body", nil)
		return nil, false
	}
	if int64(len(raw)) > limit {
		writeError(w, http.StatusRequestEntityTooLarge, "request body is too large", nil)
		return nil, false
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]any{}, true
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error(), nil)
		return nil, false
	}
	return m, true
}
