package api

import (
	"net/http"

	"myagent/internal/model"
)

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	list, err := s.app.Store.ListConversations(r.Context(), 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": list})
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	title, _ := body["title"].(string)
	if title == "" {
		title = "New chat"
	}
	c, err := s.app.Store.CreateConversation(r.Context(), s.app.Workspace.ID, title)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	conv, err := s.app.Store.GetConversation(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "conversation not found", nil)
		return
	}
	msgs, err := s.app.ConversationMessages(r.Context(), conv.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	var progress any
	if p, ok := s.app.ChatProgress(conv.ID); ok {
		progress = p
	} else if p, ok := s.app.ChatLastProgress(r.Context(), conv.ID); ok {
		progress = p
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"conversation": conv,
		"messages":     msgs,
		"active":       s.app.ChatActive(conv.ID),
		"progress":     progress,
	})
}

func (s *Server) handleRenameConversation(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	title, _ := body["title"].(string)
	conv, err := s.app.RenameConversation(r.Context(), r.PathValue("id"), title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, conv)
}

func (s *Server) handleGenerateConversationTitle(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	mode, _ := body["mode"].(string)
	if mode == "" {
		mode = "auto"
	}
	var (
		conv model.Conversation
		err  error
	)
	switch mode {
	case "auto":
		conv, err = s.app.GenerateConversationTitle(r.Context(), r.PathValue("id"))
	case "reset":
		conv, err = s.app.ResetConversationTitle(r.Context(), r.PathValue("id"))
	default:
		writeError(w, http.StatusBadRequest, "unknown title mode", nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, conv)
}

func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Store.DeleteConversation(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	text, _ := body["text"].(string)
	reply, err := s.app.SendChat(r.Context(), r.PathValue("id"), text)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, reply)
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	text, _ := body["text"].(string)
	convID, _ := body["conversation_id"].(string)
	reply, err := s.app.SendChat(r.Context(), convID, text)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, reply)
}

func (s *Server) handleStopChat(w http.ResponseWriter, r *http.Request) {
	if err := s.app.StopChat(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSaveAsWorkflow(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	name, _ := body["name"].(string)
	res, err := s.app.SaveChatAsWorkflow(r.Context(), r.PathValue("id"), name)
	if err != nil {
		writeError(
			w,
			http.StatusBadRequest,
			err.Error(),
			map[string]any{"validation": res.Validation},
		)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
