package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wyw14/cry-161/internal/model"
	"github.com/wyw14/cry-161/internal/recipe"
	"github.com/wyw14/cry-161/internal/service"
)

type releaseView struct {
	ID             model.ID             `json:"id"`
	RevisionID     model.ID             `json:"revision_id"`
	RevisionNumber int                  `json:"revision_number"`
	Status         recipe.ReleaseStatus `json:"status"`
}

func setupRouter(t *testing.T) (http.Handler, *service.Service, model.ID) {
	t.Helper()
	svc := service.New()
	recipes := svc.RecipesList()
	if len(recipes) != 1 {
		t.Fatalf("expected seeded recipe, got %d", len(recipes))
	}
	return NewRouter(svc), svc, recipes[0].ID
}

func do(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, reader)
	req.Header.Set("X-Operator", "tester")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v (body=%q)", rec.Body.String(), err, rec.Body.String())
	}
	return out
}

func TestFullGovernanceFlowOverHTTP(t *testing.T) {
	h, svc, recipeID := setupRouter(t)
	base := "/api/recipes/" + string(recipeID)

	// Open and edit a draft, then freeze revision 2.
	if rec := do(t, h, http.MethodPost, base+"/draft", ""); rec.Code != http.StatusOK {
		t.Fatalf("save draft: %d %s", rec.Code, rec.Body.String())
	}
	layerBody := `{"number":1,"material":"Ta2O5","target_nm":120,"rate_nm_s":2}`
	if rec := do(t, h, http.MethodPut, base+"/draft/layers", layerBody); rec.Code != http.StatusOK {
		t.Fatalf("update draft: %d %s", rec.Code, rec.Body.String())
	}
	rec := do(t, h, http.MethodPost, base+"/freeze", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("freeze: %d %s", rec.Code, rec.Body.String())
	}
	rev2 := decode[recipe.Revision](t, rec)

	// Open review, then publishing without approval evidence is rejected: 422.
	reviewURL := base + "/revisions/" + string(rev2.ID) + "/review"
	if rec := do(t, h, http.MethodPost, reviewURL, ""); rec.Code != http.StatusOK {
		t.Fatalf("open review: %d %s", rec.Code, rec.Body.String())
	}
	publishURL := base + "/revisions/" + string(rev2.ID) + "/publish"
	if rec := do(t, h, http.MethodPost, publishURL, ""); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("publish without evidence: code=%d body=%s", rec.Code, rec.Body.String())
	}

	// Record both approvals.
	approvalURL := base + "/revisions/" + string(rev2.ID) + "/approvals"
	for _, body := range []string{
		`{"role":"process_engineer","reviewer":"eng-2","evidence_id":"ev-pe"}`,
		`{"role":"quality","reviewer":"qa-2","evidence_id":"ev-qa"}`,
	} {
		if rec := do(t, h, http.MethodPost, approvalURL, body); rec.Code != http.StatusOK {
			t.Fatalf("approval: %d %s", rec.Code, rec.Body.String())
		}
	}

	// Start a run on revision 1, then publishing revision 2 must be 409.
	startRec := do(t, h, http.MethodPost, "/api/runs", `{"batch":"b1","recipe_id":"`+string(recipeID)+`"}`)
	if startRec.Code != http.StatusOK {
		t.Fatalf("start run: %d %s", startRec.Code, startRec.Body.String())
	}
	run := decode[model.Run](t, startRec)
	if run.RecipeRevision != 1 {
		t.Fatalf("run bound to revision %d, want 1", run.RecipeRevision)
	}
	if rec := do(t, h, http.MethodPost, publishURL, ""); rec.Code != http.StatusConflict {
		t.Fatalf("publish during in-flight run: code=%d body=%s", rec.Code, rec.Body.String())
	}

	// Finish the run through the service so the registry guard clears,
	// then publish succeeds.
	finished, ok := svc.Runs.Get(run.ID)
	if !ok {
		t.Fatal("run missing")
	}
	finished.State = model.Released
	svc.Runs.Add(finished)
	svc.Carrier.Unload("b1")
	publishRec := do(t, h, http.MethodPost, publishURL, "")
	if publishRec.Code != http.StatusOK {
		t.Fatalf("publish after run: %d %s", publishRec.Code, publishRec.Body.String())
	}
	published := decode[releaseView](t, publishRec)
	if published.Status != recipe.ReleaseActive || published.RevisionNumber != 2 {
		t.Fatalf("unexpected published record: %+v", published)
	}

	// Withdraw: active release list becomes empty, new runs get 409.
	if rec := do(t, h, http.MethodPost, base+"/releases/withdraw", `{"reason":"defect"}`); rec.Code != http.StatusOK {
		t.Fatalf("withdraw: %d %s", rec.Code, rec.Body.String())
	}
	startRec = do(t, h, http.MethodPost, "/api/runs", `{"batch":"b2","recipe_id":"`+string(recipeID)+`"}`)
	if startRec.Code != http.StatusConflict {
		t.Fatalf("start after withdraw: code=%d body=%s", startRec.Code, startRec.Body.String())
	}

	// Roll back to revision 2, then new runs bind it again.
	rollback := do(t, h, http.MethodPost, base+"/releases/rollback", `{"revision_id":"`+string(rev2.ID)+`"}`)
	if rollback.Code != http.StatusOK {
		t.Fatalf("rollback: %d %s", rollback.Code, rollback.Body.String())
	}
	startRec = do(t, h, http.MethodPost, "/api/runs", `{"batch":"b3","recipe_id":"`+string(recipeID)+`"}`)
	if startRec.Code != http.StatusOK {
		t.Fatalf("start after rollback: %d %s", startRec.Code, startRec.Body.String())
	}
	run3 := decode[model.Run](t, startRec)
	if run3.RecipeRevision != 2 {
		t.Fatalf("run after rollback bound to %d, want 2", run3.RecipeRevision)
	}

	// Release history is exposed.
	historyRec := do(t, h, http.MethodGet, base+"/releases", "")
	if historyRec.Code != http.StatusOK {
		t.Fatalf("history: %d", historyRec.Code)
	}
	history := decode[[]releaseView](t, historyRec)
	if len(history) != 2 {
		t.Fatalf("expected two governance records (rev1 superseded, rev2 rolled back), got %d", len(history))
	}
	if !strings.Contains(historyRec.Body.String(), "release.rolled_back") {
		t.Fatalf("history missing rollback audit event: %s", historyRec.Body.String())
	}
}

func TestFreezeMissingLayerReturns422(t *testing.T) {
	h, _, recipeID := setupRouter(t)
	base := "/api/recipes/" + string(recipeID)
	do(t, h, http.MethodPost, base+"/draft", "")
	// Write a layer numbered 5, creating a gap from 1.
	body := `{"number":5,"material":"SiO2","target_nm":100,"rate_nm_s":2}`
	if rec := do(t, h, http.MethodPut, base+"/draft/layers", body); rec.Code != http.StatusOK {
		t.Fatalf("update draft: %d", rec.Code)
	}
	rec := do(t, h, http.MethodPost, base+"/freeze", "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("freeze with missing layer: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUnknownRevisionReturns404(t *testing.T) {
	h, _, recipeID := setupRouter(t)
	missing := model.NewID()
	rec := do(t, h, http.MethodPost, "/api/recipes/"+string(recipeID)+"/revisions/"+string(missing)+"/review", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing revision: code=%d body=%s", rec.Code, rec.Body.String())
	}
}
