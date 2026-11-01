package api

import "net/http"

func (s *Server) handleOrganizeLibrary(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	kind, _ := body["kind"].(string)
	result, err := s.app.OrganizeLibrary(r.Context(), kind)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
