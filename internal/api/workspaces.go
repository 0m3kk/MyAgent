package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"myagent/internal/workspaces"
)

func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	reg := s.app.Workspaces
	if reg == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": false, "active": "", "workspaces": []workspaces.Profile{},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": true, "active": reg.Active(), "workspaces": reg.Profiles(),
	})
}

func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	reg := s.app.Workspaces
	if reg == nil {
		writeError(w, http.StatusBadRequest, "workspace profiles are not available", nil)
		return
	}
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	name, _ := body["name"].(string)
	root, _ := body["root_path"].(string)
	p, err := reg.Create(name, root)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	reg := s.app.Workspaces
	if reg == nil {
		writeError(w, http.StatusBadRequest, "workspace profiles are not available", nil)
		return
	}
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	id := r.PathValue("id")
	name, _ := body["name"].(string)
	root, _ := body["root_path"].(string)
	if id == reg.Active() && root != "" && s.app.HasActiveWork() {
		writeError(
			w,
			http.StatusConflict,
			"stop active chats and runs before changing the workspace folder",
			nil,
		)
		return
	}
	p, err := reg.Update(id, name, root)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	if id == reg.Active() {
		if err := s.app.UpdateWorkspaceMetadata(r.Context(), p.Name, p.RootPath); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error(), nil)
			return
		}
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	reg := s.app.Workspaces
	if reg == nil {
		writeError(w, http.StatusBadRequest, "workspace profiles are not available", nil)
		return
	}
	if err := reg.Delete(r.PathValue("id")); err != nil {
		writeWorkspaceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleActivateWorkspace(w http.ResponseWriter, r *http.Request) {
	reg := s.app.Workspaces
	if reg == nil {
		writeError(w, http.StatusBadRequest, "workspace profiles are not available", nil)
		return
	}
	current := reg.Active()
	id := r.PathValue("id")
	p, err := reg.Activate(id)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	switched := current != id
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "switched": switched, "workspace": p,
	})
	if switched {
		go func() {
			time.Sleep(500 * time.Millisecond)
			s.app.RequestRestart()
		}()
	}
}

func (s *Server) handleExportWorkspace(w http.ResponseWriter, r *http.Request) {
	reg := s.app.Workspaces
	if reg == nil {
		writeError(w, http.StatusBadRequest, "workspace profiles are not available", nil)
		return
	}
	id := r.PathValue("id")
	p, err := reg.Get(id)
	if err != nil {
		writeWorkspaceError(w, err)
		return
	}
	if id == reg.Active() {
		// The running profile is authoritative for the active workspace.
		p.DataDir = s.app.Config().DataDir
		p.Name = s.app.Workspace.Name
		p.RootPath = s.app.Workspace.RootPath
	}

	tmp, err := os.CreateTemp("", "myagent-export-*.myagentws")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	if err := workspaces.Export(tmp, p); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	info, err := tmp.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().
		Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", workspaces.ArchiveFilename(p)))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	_, _ = io.Copy(w, tmp)
}

func (s *Server) handleImportWorkspace(w http.ResponseWriter, r *http.Request) {
	reg := s.app.Workspaces
	if reg == nil {
		writeError(w, http.StatusBadRequest, "workspace profiles are not available", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<30)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "workspace archive is too large or invalid", nil)
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file", nil)
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "myagent-import-*.myagentws")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	if _, err := io.Copy(tmp, file); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	if err := tmp.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	root := r.FormValue("root_path")
	p, err := workspaces.Import(tmp.Name(), reg.Home(), root)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	if err := reg.Reload(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func writeWorkspaceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspaces.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, workspaces.ErrDuplicate),
		errors.Is(err, workspaces.ErrActive),
		errors.Is(err, workspaces.ErrLast):
		writeError(w, http.StatusConflict, err.Error(), nil)
	default:
		writeError(w, http.StatusBadRequest, err.Error(), nil)
	}
}
