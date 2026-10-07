package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/wyw14/cry-161/internal/model"
	"github.com/wyw14/cry-161/internal/recipe"
	"github.com/wyw14/cry-161/internal/service"
)

type draftLayerRequest struct {
	Number           int     `json:"number"`
	Material         string  `json:"material"`
	TargetNanometers float64 `json:"target_nm"`
	RateNanometers   float64 `json:"rate_nm_s"`
}

type approvalRequest struct {
	Role       string `json:"role"`
	Reviewer   string `json:"reviewer"`
	EvidenceID string `json:"evidence_id"`
}

type rejectRequest struct {
	Reason string `json:"reason"`
}

type rollbackRequest struct {
	RevisionID string `json:"revision_id"`
}

// writeGovernanceError maps the recipe package sentinel errors onto stable
// HTTP status codes.
func writeGovernanceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, recipe.ErrReviewNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, recipe.ErrCompile), errors.Is(err, recipe.ErrApproval):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	case errors.Is(err, recipe.ErrActiveRun), errors.Is(err, recipe.ErrInvalidState), errors.Is(err, recipe.ErrAlreadyExists), errors.Is(err, recipe.ErrNoActiveRelease):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		if err.Error() == "recipe not found" || err.Error() == "draft not found" {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func actor(r *http.Request) string {
	if value := r.Header.Get("X-Operator"); value != "" {
		return value
	}
	return "local-operator"
}

// saveDraft copies the active recipe into the draft book for editing.
func saveDraft(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID, ok := model.ParseID(chi.URLParam(r, "recipeID"))
		if !ok {
			http.Error(w, "invalid recipe id", http.StatusBadRequest)
			return
		}
		draft, err := svc.SaveDraft(recipeID)
		if err != nil {
			writeGovernanceError(w, err)
			return
		}
		writeJSON(w, draft)
	}
}

func updateDraftLayer(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID, ok := model.ParseID(chi.URLParam(r, "recipeID"))
		if !ok {
			http.Error(w, "invalid recipe id", http.StatusBadRequest)
			return
		}
		var input draftLayerRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		layer := model.Layer{Number: input.Number, Material: input.Material, TargetNanometers: input.TargetNanometers, RateNanometers: input.RateNanometers}
		if err := svc.UpdateDraft(recipeID, layer); err != nil {
			writeGovernanceError(w, err)
			return
		}
		draft, _ := svc.Drafts.Get(recipeID)
		writeJSON(w, draft)
	}
}

func freezeRecipe(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID, ok := model.ParseID(chi.URLParam(r, "recipeID"))
		if !ok {
			http.Error(w, "invalid recipe id", http.StatusBadRequest)
			return
		}
		rev, err := svc.FreezeDraft(recipeID)
		if err != nil {
			writeGovernanceError(w, err)
			return
		}
		writeJSON(w, rev)
	}
}

func listRevisions(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID, ok := model.ParseID(chi.URLParam(r, "recipeID"))
		if !ok {
			http.Error(w, "invalid recipe id", http.StatusBadRequest)
			return
		}
		writeJSON(w, svc.FrozenRevisions(recipeID))
	}
}

func openReview(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID, revisionID, ok := parseRevisionParams(w, r)
		if !ok {
			return
		}
		record, err := svc.OpenReview(recipeID, revisionID, actor(r))
		if err != nil {
			writeGovernanceError(w, err)
			return
		}
		writeJSON(w, record)
	}
}

func recordApproval(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID, revisionID, ok := parseRevisionParams(w, r)
		if !ok {
			return
		}
		var input approvalRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		record, err := svc.RecordApproval(recipeID, revisionID, recipe.Approval{Role: recipe.ApprovalRole(input.Role), Reviewer: input.Reviewer, EvidenceID: input.EvidenceID})
		if err != nil {
			writeGovernanceError(w, err)
			return
		}
		writeJSON(w, record)
	}
}

func publishRevision(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID, revisionID, ok := parseRevisionParams(w, r)
		if !ok {
			return
		}
		record, err := svc.Publish(recipeID, revisionID, actor(r))
		if err != nil {
			writeGovernanceError(w, err)
			return
		}
		writeJSON(w, record)
	}
}

func rejectRevision(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID, revisionID, ok := parseRevisionParams(w, r)
		if !ok {
			return
		}
		var input rejectRequest
		_ = json.NewDecoder(r.Body).Decode(&input)
		record, err := svc.Reject(recipeID, revisionID, actor(r), input.Reason)
		if err != nil {
			writeGovernanceError(w, err)
			return
		}
		writeJSON(w, record)
	}
}

func withdrawRecipe(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID, ok := model.ParseID(chi.URLParam(r, "recipeID"))
		if !ok {
			http.Error(w, "invalid recipe id", http.StatusBadRequest)
			return
		}
		var input rejectRequest
		_ = json.NewDecoder(r.Body).Decode(&input)
		record, err := svc.Withdraw(recipeID, actor(r), input.Reason)
		if err != nil {
			writeGovernanceError(w, err)
			return
		}
		writeJSON(w, record)
	}
}

func rollbackRecipe(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID, ok := model.ParseID(chi.URLParam(r, "recipeID"))
		if !ok {
			http.Error(w, "invalid recipe id", http.StatusBadRequest)
			return
		}
		var input rollbackRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		revisionID, ok := model.ParseID(input.RevisionID)
		if !ok {
			http.Error(w, "invalid revision id", http.StatusBadRequest)
			return
		}
		record, err := svc.Rollback(recipeID, revisionID, actor(r))
		if err != nil {
			writeGovernanceError(w, err)
			return
		}
		writeJSON(w, record)
	}
}

func listReleases(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID, ok := model.ParseID(chi.URLParam(r, "recipeID"))
		if !ok {
			http.Error(w, "invalid recipe id", http.StatusBadRequest)
			return
		}
		writeJSON(w, svc.ReleaseHistory(recipeID))
	}
}

func parseRevisionParams(w http.ResponseWriter, r *http.Request) (model.ID, model.ID, bool) {
	recipeID, ok := model.ParseID(chi.URLParam(r, "recipeID"))
	if !ok {
		http.Error(w, "invalid recipe id", http.StatusBadRequest)
		return "", "", false
	}
	revisionID, ok := model.ParseID(chi.URLParam(r, "revisionID"))
	if !ok {
		http.Error(w, "invalid revision id", http.StatusBadRequest)
		return "", "", false
	}
	return recipeID, revisionID, true
}
