package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wyw14/cry-161/internal/model"
	"github.com/wyw14/cry-161/internal/recipe"
	"github.com/wyw14/cry-161/internal/service"
)

func newTestService(t *testing.T) *service.Service {
	t.Helper()
	t.Setenv("OPTICCOAT_DATA", t.TempDir())
	return service.New()
}

func approve(t *testing.T, svc *service.Service, releaseID model.ID) {
	t.Helper()
	if _, err := svc.ReviewRelease(releaseID, model.ApprovalProcess, "alice", "process ok"); err != nil {
		t.Fatalf("process review: %v", err)
	}
	if _, err := svc.ReviewRelease(releaseID, model.ApprovalQuality, "bob", "quality ok"); err != nil {
		t.Fatalf("quality review: %v", err)
	}
}

func TestRunBindsImmutablePublishedRevision(t *testing.T) {
	svc := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	active := svc.RecipesList()
	if len(active) != 1 {
		t.Fatalf("expected one seeded published recipe, got %d", len(active))
	}
	recipeID := active[0].ID

	// A started run pins the immutable revision recorded on the run itself.
	run1, err := svc.Start(ctx, "batch-1", recipeID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if run1.RecipeRevision != 1 {
		t.Fatalf("run should bind revision 1, got %d", run1.RecipeRevision)
	}

	// Evolve the draft through freeze -> dual-role review -> publication.
	draft, err := svc.StartDraft(recipeID)
	if err != nil {
		t.Fatalf("start draft: %v", err)
	}
	if err := svc.UpdateDraft(draft.ID, model.Layer{Number: 2, Material: "Ta2O5", TargetNanometers: 70, RateNanometers: 3}); err != nil {
		t.Fatalf("update draft: %v", err)
	}
	release, _, err := svc.SubmitDraft(draft.ID)
	if err != nil {
		t.Fatalf("submit draft: %v", err)
	}
	if _, err := svc.PublishRelease(release.ID); err == nil {
		t.Fatal("publication without sufficient approval evidence must be rejected")
	}
	approve(t, svc, release.ID)
	release, err = svc.PublishRelease(release.ID)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	// The already started run must not be overwritten by the new publication.
	pinned, _ := svc.Runs.Get(run1.ID)
	if pinned.RecipeRevision != 1 {
		t.Fatalf("started run must stay on revision 1, got %d", pinned.RecipeRevision)
	}

	// A new run binds the new revision.
	svc.Carrier.Unload("batch-1")
	run2, err := svc.Start(ctx, "batch-2", recipeID)
	if err != nil || run2.RecipeRevision != 2 {
		t.Fatalf("new run should bind revision 2, got %+v err=%v", run2, err)
	}

	// Withdrawal and rollback are refused while the current revision runs.
	if _, err := svc.WithdrawRelease(release.ID, "defect"); err == nil {
		t.Fatal("withdraw must be refused while a run is in progress")
	}
	if _, err := svc.RollbackRecipe(recipeID, 1, "regression"); err == nil {
		t.Fatal("rollback must be refused while the current revision runs")
	}

	// Finished runs release the guard: rollback republishing revision 1 works.
	for _, value := range svc.RunsList() {
		if !value.State.IsTerminal() {
			svc.Runs.UpdateError(value.ID, errors.New("test complete"))
		}
	}
	rolledBack, err := svc.RollbackRecipe(recipeID, 1, "coating regression")
	if err != nil {
		t.Fatalf("rollback after run finishes: %v", err)
	}
	if rolledBack.Revision != 1 {
		t.Fatalf("rollback should pin revision 1, got %d", rolledBack.Revision)
	}
	if active, _ := svc.Recipes.Active(recipeID); active.Revision != 1 || active.Layers[2].Material != "TiO2" {
		t.Fatalf("active projection should be immutable revision 1 content, got %+v", active)
	}
}

func TestRunLayersMustMatchPinnedRevision(t *testing.T) {
	svc := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	runtime := service.NewRuntime()

	recipeID := svc.RecipesList()[0].ID
	value, err := svc.Start(ctx, "batch-layers", recipeID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := svc.RunLayer(ctx, runtime, value.ID, model.Layer{Number: 99, Material: "SiO2", TargetNanometers: 1, RateNanometers: 1}); err == nil {
		t.Fatal("executing a layer outside the pinned revision must be rejected")
	}
	if err := svc.RunLayer(ctx, runtime, value.ID, model.Layer{Number: 2, Material: "Ta2O5", TargetNanometers: 60, RateNanometers: 3}); err == nil {
		t.Fatal("executing layer content that differs from the pinned revision must be rejected")
	}
	if err := svc.RunLayer(ctx, runtime, value.ID, model.Layer{Number: 1, Material: "SiO2", TargetNanometers: 100, RateNanometers: 2}); err != nil {
		t.Fatalf("pinned layer should execute: %v", err)
	}
}

func TestWithdrawStopsNewRuns(t *testing.T) {
	svc := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	recipeID := svc.RecipesList()[0].ID
	active, ok := svc.Governance.ActiveRelease(recipeID)
	if !ok {
		t.Fatal("seed recipe should have an active release")
	}
	if _, err := svc.Start(ctx, "batch-w", recipeID); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := svc.WithdrawRelease(active.ID, "quality escape"); err == nil {
		t.Fatal("withdraw must be refused while a run is in progress")
	}

	for _, value := range svc.RunsList() {
		if value.State != model.Released && value.State != model.Failed {
			svc.Runs.UpdateError(value.ID, errors.New("test complete"))
		}
	}
	if _, err := svc.WithdrawRelease(active.ID, "quality escape"); err != nil {
		t.Fatalf("withdraw after runs finish: %v", err)
	}
	if _, err := svc.Start(ctx, "batch-w2", recipeID); err == nil {
		t.Fatal("no run may start after the only release is withdrawn")
	}
}

func TestCompileAndEvidenceGuardsAtServiceBoundary(t *testing.T) {
	svc := newTestService(t)
	recipeID := svc.RecipesList()[0].ID

	// Missing layers / illegal ordering are rejected at freeze time.
	bad := &model.Recipe{ID: recipeID, Name: "broken", Revision: 9, Layers: map[int]model.Layer{
		1: {Number: 1, Material: "SiO2", TargetNanometers: 100, RateNanometers: 2},
		3: {Number: 3, Material: "TiO2", TargetNanometers: 60, RateNanometers: 3},
	}}
	if _, _, err := svc.Governance.SubmitRecipe(bad); err == nil {
		t.Fatal("freezing a recipe with a missing layer must be rejected")
	}

	// A frozen-but-unreviewed revision can neither be published nor bound.
	good := &model.Recipe{ID: recipeID, Name: "next", Revision: 10, Layers: map[int]model.Layer{
		1: {Number: 1, Material: "SiO2", TargetNanometers: 50, RateNanometers: 2},
	}}
	release, revision, err := svc.Governance.SubmitRecipe(good)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if revision.Digest == "" {
		t.Fatal("frozen revision must carry a content digest")
	}
	if len(svc.ListReleases(recipeID)) == 0 {
		t.Fatal("releases should be queryable")
	}
	if got, ok := svc.GetRelease(release.ID); !ok || got.State != recipe.ReleaseInReview {
		t.Fatalf("release should be in review, got %+v ok=%t", got, ok)
	}
}
