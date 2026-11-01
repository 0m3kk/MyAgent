package api

import (
	"errors"
	"net/http"
	"strings"
)

func (s *Server) handleListFolders(w http.ResponseWriter, r *http.Request) {
	folders, err := s.app.FolderList(r.Context(), r.URL.Query().Get("kind"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders})
}

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	kind, _ := body["kind"].(string)
	name, _ := body["name"].(string)
	parentID, _ := body["parent_id"].(string)
	folder, err := s.app.CreateFolder(r.Context(), kind, name, strings.TrimSpace(parentID))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, folder)
}

func (s *Server) handleUpdateFolder(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	name, hasName, err := optionalString(body, "name")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	parentID, hasParent, err := optionalString(body, "parent_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	if !hasName && !hasParent {
		writeError(w, http.StatusUnprocessableEntity, "nothing to update", nil)
		return
	}
	folder, err := s.app.UpdateFolder(r.Context(), r.PathValue("id"), name, parentID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, folder)
}

func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	if err := s.app.DeleteFolder(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleUpdateToolOrganization(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	folderID, pinned, err := organizationPatch(body)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}
	if err := s.app.SetToolOrganization(
		r.Context(),
		r.PathValue("id"),
		folderID,
		pinned,
	); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleUpdateWorkflowOrganization(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	folderID, pinned, err := organizationPatch(body)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}
	if err := s.app.SetWorkflowOrganization(
		r.Context(),
		r.PathValue("id"),
		folderID,
		pinned,
	); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func organizationPatch(body map[string]any) (*string, *bool, error) {
	folderID, hasFolder, err := optionalString(body, "folder_id")
	if err != nil {
		return nil, nil, err
	}
	var pinned *bool
	rawPinned, hasPinned := body["pinned"]
	if hasPinned {
		value, ok := rawPinned.(bool)
		if !ok {
			return nil, nil, errors.New("pinned must be a boolean")
		}
		pinned = &value
	}
	if !hasFolder && !hasPinned {
		return nil, nil, errors.New("nothing to update")
	}
	return folderID, pinned, nil
}

func optionalString(body map[string]any, key string) (*string, bool, error) {
	raw, ok := body[key]
	if !ok {
		return nil, false, nil
	}
	if raw == nil {
		value := ""
		return &value, true, nil
	}
	value, ok := raw.(string)
	if !ok {
		return nil, true, errors.New(key + " must be a string")
	}
	return &value, true, nil
}
