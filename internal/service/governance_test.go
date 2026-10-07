package service

import (
	"context"
	"errors"
	"testing"

	"github.com/wyw14/cry-161/internal/model"
	"github.com/wyw14/cry-161/internal/recipe"
)

func seedRecipeID(s *Service) model.ID {
	recipes := s.RecipesList()
	if len(recipes) != 1 {
		panic("expected one seeded recipe")
	}
	return recipes[0].ID
}

func approveService(t *testing.T, s *Service, recipeID, revisionID model.ID, role recipe.ApprovalRole, reviewer string) {
	t.Helper()
	if _, err := s.RecordApproval(recipeID, revisionID, recipe.Approval{Role: role, Reviewer: reviewer, EvidenceID: "evidence-" + string(role)}); err != nil {
		t.Fatalf("approval %s: %v", role, err)
	}
}

func finishRun(s *Service, runID model.ID) {
	value, ok := s.Runs.Get(runID)
	if !ok {
		panic("run missing")
	}
	value.State = model.Released
	s.Runs.Add(value)
}

func TestStartBindsImmutableRelease(t *testing.T) {
	s := New()
	recipeID := seedRecipeID(s)
	started, err := s.Start(context.Background(), "batch-1", recipeID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if started.RecipeRevision != 1 || started.RecipeReleaseID == "" {
		t.Fatalf("run not bound to immutable release: %+v", started)
	}
	bound, ok := s.Releases.Binding(started.ID)
	if !ok || bound.RevisionNumber != 1 || len(bound.Layers) != 2 {
		t.Fatalf("binding missing or wrong content: %+v ok=%v", bound, ok)
	}
}

func TestStartRejectedWhenNoActiveRelease(t *testing.T) {
	s := New()
	recipeID := seedRecipeID(s)
	if _, err := s.Withdraw(recipeID, "tester", "hold"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(context.Background(), "batch-x", recipeID); !errors.Is(err, recipe.ErrNoActiveRelease) {
		t.Fatalf("start after withdraw: want ErrNoActiveRelease, got %v", err)
	}
}

func TestPublishBlockedByInFlightRunThenRollbackRestores(t *testing.T) {
	s := New()
	recipeID := seedRecipeID(s)
	first, err := s.Start(context.Background(), "batch-1", recipeID)
	if err != nil {
		t.Fatal(err)
	}

	// Prepare revision 2 from a draft.
	if _, err := s.SaveDraft(recipeID); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDraft(recipeID, model.Layer{Number: 1, Material: "Ta2O5", TargetNanometers: 120, RateNanometers: 2}); err != nil {
		t.Fatal(err)
	}
	rev2, err := s.FreezeDraft(recipeID)
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	if rev2.Number != 2 {
		t.Fatalf("new revision number = %d, want 2", rev2.Number)
	}
	if _, err := s.OpenReview(recipeID, rev2.ID, "author"); err != nil {
		t.Fatal(err)
	}
	approveService(t, s, recipeID, rev2.ID, recipe.RoleProcessEngineer, "engineer-2")
	approveService(t, s, recipeID, rev2.ID, recipe.RoleQuality, "qa-2")

	// Run still in flight: publish refused and the run keeps revision 1.
	if _, err := s.Publish(recipeID, rev2.ID, "tester"); !errors.Is(err, recipe.ErrActiveRun) {
		t.Fatalf("publish during run: want ErrActiveRun, got %v", err)
	}
	stored, _ := s.Runs.Get(first.ID)
	if stored.RecipeRevision != 1 {
		t.Fatalf("in-flight run rebound to %d", stored.RecipeRevision)
	}
	active, _ := s.Releases.ActiveRelease(recipeID)
	if active.RevisionNumber != 1 {
		t.Fatalf("active release changed during in-flight run: %d", active.RevisionNumber)
	}

	// Run finishes; publishing now succeeds and supersedes revision 1.
	finishRun(s, first.ID)
	s.Carrier.Unload("batch-1")
	if _, err := s.Publish(recipeID, rev2.ID, "tester"); err != nil {
		t.Fatalf("publish after run: %v", err)
	}
	second, err := s.Start(context.Background(), "batch-2", recipeID)
	if err != nil {
		t.Fatal(err)
	}
	if second.RecipeRevision != 2 {
		t.Fatalf("new run bound to revision %d, want 2", second.RecipeRevision)
	}
	if layers := s.FrozenRevisions(recipeID); len(layers) != 2 {
		t.Fatalf("expected two frozen revisions, got %d", len(layers))
	}

	// Withdraw revision 2, then roll back to revision 1: no new review
	// round, and a new run binds revision 1 again.
	finishRun(s, second.ID)
	s.Carrier.Unload("batch-2")
	if _, err := s.Withdraw(recipeID, "tester", "defect"); err != nil {
		t.Fatal(err)
	}
	rev1 := s.FrozenRevisions(recipeID)[0]
	if _, err := s.Rollback(recipeID, rev1.ID, "tester"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	third, err := s.Start(context.Background(), "batch-3", recipeID)
	if err != nil {
		t.Fatal(err)
	}
	if third.RecipeRevision != 1 {
		t.Fatalf("run after rollback bound to revision %d, want 1", third.RecipeRevision)
	}
}

func TestFreezeDraftRejectsInvalidCompile(t *testing.T) {
	s := New()
	recipeID := seedRecipeID(s)
	if _, err := s.SaveDraft(recipeID); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDraft(recipeID, model.Layer{Number: 1, Material: "", TargetNanometers: 100, RateNanometers: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FreezeDraft(recipeID); !errors.Is(err, recipe.ErrCompile) {
		t.Fatalf("freeze invalid draft: want ErrCompile, got %v", err)
	}
	// No revision created on failed compile.
	if len(s.FrozenRevisions(recipeID)) != 1 {
		t.Fatal("failed compile must not create a revision")
	}
}

func TestPublishWithoutEvidenceRejected(t *testing.T) {
	s := New()
	recipeID := seedRecipeID(s)
	if _, err := s.SaveDraft(recipeID); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDraft(recipeID, model.Layer{Number: 1, Material: "Ta2O5", TargetNanometers: 90, RateNanometers: 2}); err != nil {
		t.Fatal(err)
	}
	rev2, err := s.FreezeDraft(recipeID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenReview(recipeID, rev2.ID, "author"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish(recipeID, rev2.ID, "tester"); !errors.Is(err, recipe.ErrApproval) {
		t.Fatalf("publish without evidence: want ErrApproval, got %v", err)
	}
}
