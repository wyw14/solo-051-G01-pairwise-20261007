package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wyw14/cry-161/internal/service"
)

func setupRouter(t *testing.T) (http.Handler, map[string]string) {
	t.Helper()
	t.Setenv("OPTICCOAT_DATA", t.TempDir())
	router := NewRouter(service.New())
	ids := map[string]string{}

	var recipes []map[string]any
	getJSON(t, router, http.MethodGet, "/api/recipes", nil, &recipes)
	ids["recipe"] = recipes[0]["id"].(string)

	draft := map[string]any{}
	getJSON(t, router, http.MethodPost, "/api/recipes/"+ids["recipe"]+"/drafts", nil, &draft)
	ids["draft"] = draft["id"].(string)

	body := map[string]any{"number": 2, "material": "Ta2O5", "target_nm": 70, "rate_nm_s": 3}
	status := doJSON(t, router, http.MethodPut, "/api/drafts/"+ids["draft"], body)
	if status != http.StatusNoContent {
		t.Fatalf("draft update status = %d", status)
	}

	submitted := map[string]any{}
	getJSON(t, router, http.MethodPost, "/api/drafts/"+ids["draft"]+"/submit", nil, &submitted)
	release := submitted["release"].(map[string]any)
	ids["release"] = release["id"].(string)
	return router, ids
}

func TestPublishWithoutEvidenceRejected(t *testing.T) {
	router, ids := setupRouter(t)
	status := doJSON(t, router, http.MethodPost, "/api/releases/"+ids["release"]+"/publish", nil)
	if status != http.StatusBadRequest && status != http.StatusConflict {
		t.Fatalf("publish without evidence status = %d, want 4xx", status)
	}
}

func TestReviewPublishWithdrawFlow(t *testing.T) {
	router, ids := setupRouter(t)

	review := func(role string, statusWant int) {
		t.Helper()
		body := map[string]any{"role": role, "reviewer": "alice", "comment": "ok"}
		status := doJSON(t, router, http.MethodPost, "/api/releases/"+ids["release"]+"/reviews", body)
		if status != statusWant {
			t.Fatalf("review %s status = %d, want %d", role, status, statusWant)
		}
	}
	review("process_engineer", http.StatusOK)
	// Segregation of duties: the same reviewer cannot also sign quality.
	review("quality_reviewer", http.StatusConflict)
	quality := map[string]any{"role": "quality_reviewer", "reviewer": "bob", "comment": "ok"}
	if status := doJSON(t, router, http.MethodPost, "/api/releases/"+ids["release"]+"/reviews", quality); status != http.StatusOK {
		t.Fatalf("quality review status = %d", status)
	}

	published := map[string]any{}
	getJSON(t, router, http.MethodPost, "/api/releases/"+ids["release"]+"/publish", nil, &published)
	if published["state"] != "published" {
		t.Fatalf("release state = %v, want published", published["state"])
	}

	var recipes []map[string]any
	getJSON(t, router, http.MethodGet, "/api/recipes", nil, &recipes)
	var active map[string]any
	for _, r := range recipes {
		if r["id"] == ids["recipe"] {
			active = r
		}
	}
	if active["revision"].(float64) != 2 {
		t.Fatalf("active revision = %v, want 2", active["revision"])
	}

	var revisions []map[string]any
	getJSON(t, router, http.MethodGet, "/api/recipes/"+ids["recipe"]+"/revisions", nil, &revisions)
	if len(revisions) != 2 {
		t.Fatalf("frozen revisions = %d, want 2", len(revisions))
	}

	var releases []map[string]any
	getJSON(t, router, http.MethodGet, "/api/recipes/"+ids["recipe"]+"/releases", nil, &releases)
	if len(releases) < 2 {
		t.Fatalf("releases = %d, want at least 2", len(releases))
	}
}

func TestReviewRejectsUnknownRole(t *testing.T) {
	router, ids := setupRouter(t)
	body := map[string]any{"role": "manager", "reviewer": "alice"}
	if status := doJSON(t, router, http.MethodPost, "/api/releases/"+ids["release"]+"/reviews", body); status != http.StatusBadRequest {
		t.Fatalf("unknown role status = %d, want 400", status)
	}
}

func TestInvalidLayerDraftRejectedAtSubmit(t *testing.T) {
	router, ids := setupRouter(t)
	draft := map[string]any{}
	getJSON(t, router, http.MethodPost, "/api/recipes/"+ids["recipe"]+"/drafts", nil, &draft)
	draftID := draft["id"].(string)
	// Overwrite layer 1 with an illegal layer (missing material).
	_ = doJSON(t, router, http.MethodPut, "/api/drafts/"+draftID, map[string]any{"number": 1, "material": "", "target_nm": 10, "rate_nm_s": 1})
	if status := doJSON(t, router, http.MethodPost, "/api/drafts/"+draftID+"/submit", nil); status != http.StatusBadRequest {
		t.Fatalf("submit with illegal layer status = %d, want 400", status)
	}
}

func doJSON(t *testing.T, router http.Handler, method, path string, body any) int {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code
}

func getJSON(t *testing.T, router http.Handler, method, path string, body any, target any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code >= 400 {
		t.Fatalf("%s %s -> %d: %s", method, path, rec.Code, rec.Body.String())
	}
	if target != nil && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), target); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
}
