package api

import (
	"net/http"

	"myagent/internal/model"
)

func (s *Server) handleListTuning(w http.ResponseWriter, r *http.Request) {
	proposals, err := s.app.Store.ListTuningProposals(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	suites, _ := s.app.EvalSuiteList(r.Context())
	failed, _ := s.app.Store.ListRuns(
		r.Context(),
		model.RunFilter{Status: model.RunFailed, Limit: 20},
	)
	writeJSON(
		w,
		http.StatusOK,
		map[string]any{"proposals": proposals, "suites": suites, "failed_runs": failed},
	)
}

func (s *Server) handleAnalyzeRun(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	runID, _ := body["run_id"].(string)
	p, err := s.app.AnalyzeRun(r.Context(), runID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleProposalDecision(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	action, _ := body["action"].(string)
	comment, _ := body["comment"].(string)
	p, err := s.app.DecideProposal(r.Context(), r.PathValue("id"), action, comment)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleListEvalSuites(w http.ResponseWriter, r *http.Request) {
	suites, err := s.app.EvalSuiteList(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"suites": suites})
}

func (s *Server) handleRunEval(w http.ResponseWriter, r *http.Request) {
	body, ok := readJSON(w, r, 1<<20)
	if !ok {
		return
	}
	target, _ := body["target_version"].(string)
	run, err := s.app.RunEvalSuite(r.Context(), r.PathValue("id"), target)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleGetEvalRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.app.Store.GetEvalRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "eval run not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, run)
}
