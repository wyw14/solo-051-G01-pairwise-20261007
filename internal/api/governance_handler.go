package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/wyw14/cry-161/internal/model"
	"github.com/wyw14/cry-161/internal/service"
)

func governanceError(w http.ResponseWriter, err error) {
	status := http.StatusConflict
	message := err.Error()
	switch {
	case strings.Contains(message, "not found"), strings.Contains(message, "is not frozen"), strings.Contains(message, "no published release"), strings.Contains(message, "no prior publication"):
		status = http.StatusNotFound
	case strings.Contains(message, "invalid"), strings.Contains(message, "required"), strings.Contains(message, "missing"):
		status = http.StatusBadRequest
	}
	http.Error(w, message, status)
}

func listRevisions(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID, ok := model.ParseID(chi.URLParam(r, "recipeID"))
		if !ok {
			http.Error(w, "invalid recipe id", http.StatusBadRequest)
			return
		}
		writeJSON(w, svc.ListRevisions(recipeID))
	}
}

func listReleases(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID, ok := model.ParseID(chi.URLParam(r, "recipeID"))
		if !ok {
			http.Error(w, "invalid recipe id", http.StatusBadRequest)
			return
		}
		writeJSON(w, svc.ListReleases(recipeID))
	}
}

func startDraft(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID, ok := model.ParseID(chi.URLParam(r, "recipeID"))
		if !ok {
			http.Error(w, "invalid recipe id", http.StatusBadRequest)
			return
		}
		draft, err := svc.StartDraft(recipeID)
		if err != nil {
			governanceError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, draft)
	}
}

func updateDraft(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		draftID, ok := model.ParseID(chi.URLParam(r, "draftID"))
		if !ok {
			http.Error(w, "invalid draft id", http.StatusBadRequest)
			return
		}
		var input struct {
			Number           int     `json:"number"`
			Material         string  `json:"material"`
			TargetNanometers float64 `json:"target_nm"`
			RateNanometers   float64 `json:"rate_nm_s"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		layer := model.Layer{Number: input.Number, Material: input.Material, TargetNanometers: input.TargetNanometers, RateNanometers: input.RateNanometers}
		if err := svc.UpdateDraft(draftID, layer); err != nil {
			governanceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func submitDraft(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		draftID, ok := model.ParseID(chi.URLParam(r, "draftID"))
		if !ok {
			http.Error(w, "invalid draft id", http.StatusBadRequest)
			return
		}
		release, revision, err := svc.SubmitDraft(draftID)
		if err != nil {
			governanceError(w, err)
			return
		}
		writeJSON(w, map[string]any{"release": release, "revision": revision})
	}
}

func reviewRelease(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		releaseID, ok := model.ParseID(chi.URLParam(r, "releaseID"))
		if !ok {
			http.Error(w, "invalid release id", http.StatusBadRequest)
			return
		}
		var input struct {
			Role     string `json:"role"`
			Reviewer string `json:"reviewer"`
			Comment  string `json:"comment"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		role := model.ApprovalRole(input.Role)
		if !role.Valid() {
			http.Error(w, "invalid approval role", http.StatusBadRequest)
			return
		}
		release, err := svc.ReviewRelease(releaseID, role, input.Reviewer, input.Comment)
		if err != nil {
			governanceError(w, err)
			return
		}
		writeJSON(w, release)
	}
}

func publishRelease(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		releaseID, ok := model.ParseID(chi.URLParam(r, "releaseID"))
		if !ok {
			http.Error(w, "invalid release id", http.StatusBadRequest)
			return
		}
		release, err := svc.PublishRelease(releaseID)
		if err != nil {
			governanceError(w, err)
			return
		}
		writeJSON(w, release)
	}
}

func withdrawRelease(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		releaseID, ok := model.ParseID(chi.URLParam(r, "releaseID"))
		if !ok {
			http.Error(w, "invalid release id", http.StatusBadRequest)
			return
		}
		var input struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&input)
		release, err := svc.WithdrawRelease(releaseID, input.Reason)
		if err != nil {
			governanceError(w, err)
			return
		}
		writeJSON(w, release)
	}
}

func rollbackRecipe(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		recipeID, ok := model.ParseID(chi.URLParam(r, "recipeID"))
		if !ok {
			http.Error(w, "invalid recipe id", http.StatusBadRequest)
			return
		}
		var input struct {
			Revision int    `json:"revision"`
			Reason   string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		release, err := svc.RollbackRecipe(recipeID, input.Revision, input.Reason)
		if err != nil {
			governanceError(w, err)
			return
		}
		writeJSON(w, release)
	}
}
