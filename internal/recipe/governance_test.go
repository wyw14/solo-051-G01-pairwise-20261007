package recipe

import (
	"errors"
	"testing"

	"github.com/wyw14/cry-161/internal/model"
)

type fakeGuard struct{ active map[model.ID]bool }

func (f fakeGuard) HasActiveRun(id model.ID) bool { return f.active[id] }

func newTestRecipe(t *testing.T, revision int, layers map[int]model.Layer) *model.Recipe {
	t.Helper()
	return &model.Recipe{ID: model.NewID(), Name: "test", Revision: revision, Layers: layers}
}

func twoLayers() map[int]model.Layer {
	return map[int]model.Layer{
		1: {Number: 1, Material: "SiO2", TargetNanometers: 100, RateNanometers: 2},
		2: {Number: 2, Material: "TiO2", TargetNanometers: 60, RateNanometers: 3},
	}
}

func approve(t *testing.T, g *ReleaseRegistry, recipeID model.ID, rev Revision, role ApprovalRole, reviewer string) {
	t.Helper()
	if _, err := g.RecordApproval(recipeID, rev.ID, Approval{Role: role, Reviewer: reviewer, EvidenceID: "evidence-" + string(role)}); err != nil {
		t.Fatalf("approval %s failed: %v", role, err)
	}
}

func approveAndPublish(t *testing.T, g *ReleaseRegistry, guard ActiveRunGuard, recipeID model.ID, rev Revision) {
	t.Helper()
	approve(t, g, recipeID, rev, RoleProcessEngineer, "engineer-1")
	approve(t, g, recipeID, rev, RoleQuality, "qa-1")
	if _, err := g.Publish(recipeID, rev.ID, "tester", guard); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
}

func TestFreezeRejectsGappyRecipe(t *testing.T) {
	store := NewRevisionStore()
	bad := newTestRecipe(t, 1, map[int]model.Layer{1: {Number: 1, Material: "SiO2", TargetNanometers: 100, RateNanometers: 2}, 3: {Number: 3, Material: "TiO2", TargetNanometers: 60, RateNanometers: 3}})
	if _, err := store.Freeze(bad); !errors.Is(err, ErrCompile) {
		t.Fatalf("freeze should wrap ErrCompile, got %v", err)
	}
}

func TestReviewPublishFlow(t *testing.T) {
	g := NewReleaseRegistry()
	store := NewRevisionStore()
	rcp := newTestRecipe(t, 1, twoLayers())
	rev, err := store.Freeze(rcp)
	if err != nil {
		t.Fatal(err)
	}
	record, err := g.OpenReview(rev, "author")
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != ReviewInReview {
		t.Fatalf("new record status = %s", record.Status)
	}
	// Same revision cannot enter governance twice.
	if _, err := g.OpenReview(rev, "author"); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate review should fail with ErrAlreadyExists, got %v", err)
	}
	guard := fakeGuard{active: map[model.ID]bool{}}
	approveAndPublish(t, g, guard, rcp.ID, rev)
	active, ok := g.ActiveRelease(rcp.ID)
	if !ok || active.Status != ReleaseActive || active.RevisionNumber != 1 {
		t.Fatalf("expected revision 1 active, got %+v ok=%v", active, ok)
	}
}

func TestPublishRequiresSufficientEvidence(t *testing.T) {
	g := NewReleaseRegistry()
	store := NewRevisionStore()
	rcp := newTestRecipe(t, 1, twoLayers())
	rev, _ := store.Freeze(rcp)
	if _, err := g.OpenReview(rev, "author"); err != nil {
		t.Fatal(err)
	}
	guard := fakeGuard{active: map[model.ID]bool{}}

	// No approvals at all.
	if _, err := g.Publish(rcp.ID, rev.ID, "tester", guard); !errors.Is(err, ErrApproval) {
		t.Fatalf("publish without approvals: want ErrApproval, got %v", err)
	}

	// Only one role.
	approve(t, g, rcp.ID, rev, RoleProcessEngineer, "engineer-1")
	if _, err := g.Publish(rcp.ID, rev.ID, "tester", guard); !errors.Is(err, ErrApproval) {
		t.Fatalf("publish with one role: want ErrApproval, got %v", err)
	}

	// Same reviewer covering both roles is accepted as evidence input but
	// cannot satisfy segregation of duties at publish time.
	if _, err := g.RecordApproval(rcp.ID, rev.ID, Approval{Role: RoleQuality, Reviewer: "engineer-1", EvidenceID: "ev"}); err != nil {
		t.Fatalf("recording second-role evidence failed: %v", err)
	}
	if _, err := g.Publish(rcp.ID, rev.ID, "tester", guard); !errors.Is(err, ErrApproval) {
		t.Fatalf("dual-role reviewer: want ErrApproval on publish, got %v", err)
	}

	// Approval without evidence id is rejected when recorded.
	if _, err := g.RecordApproval(rcp.ID, rev.ID, Approval{Role: RoleQuality, Reviewer: "qa-1"}); !errors.Is(err, ErrApproval) {
		t.Fatalf("missing evidence id: want ErrApproval, got %v", err)
	}
}

func TestApprovalOnlyAcceptedInReview(t *testing.T) {
	g := NewReleaseRegistry()
	store := NewRevisionStore()
	rcp := newTestRecipe(t, 1, twoLayers())
	rev, _ := store.Freeze(rcp)
	if _, err := g.OpenReview(rev, "author"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Reject(rcp.ID, rev.ID, "tester", "bad"); err != nil {
		t.Fatal(err)
	}
	_, err := g.RecordApproval(rcp.ID, rev.ID, Approval{Role: RoleQuality, Reviewer: "qa-1", EvidenceID: "ev"})
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("approval after reject: want ErrInvalidState, got %v", err)
	}
	if _, err := g.Publish(rcp.ID, rev.ID, "tester", fakeGuard{map[model.ID]bool{}}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("publish rejected revision: want ErrInvalidState, got %v", err)
	}
}

func TestInFlightRunBlocksPublishButKeepsOldRelease(t *testing.T) {
	g := NewReleaseRegistry()
	store := NewRevisionStore()
	rcp := newTestRecipe(t, 1, twoLayers())
	rev1, _ := store.Freeze(rcp)
	if _, err := g.OpenReview(rev1, "author"); err != nil {
		t.Fatal(err)
	}
	idle := fakeGuard{active: map[model.ID]bool{}}
	busy := fakeGuard{active: map[model.ID]bool{rcp.ID: true}}
	approve(t, g, rcp.ID, rev1, RoleProcessEngineer, "engineer-1")
	approve(t, g, rcp.ID, rev1, RoleQuality, "qa-1")
	if _, err := g.Publish(rcp.ID, rev1.ID, "tester", idle); err != nil {
		t.Fatalf("first publish failed: %v", err)
	}

	// Freeze revision 2 while a run on revision 1 is in flight.
	rcp.Revision = 2
	rev2, err := store.Freeze(rcp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.OpenReview(rev2, "author"); err != nil {
		t.Fatal(err)
	}
	approve(t, g, rcp.ID, rev2, RoleProcessEngineer, "engineer-2")
	approve(t, g, rcp.ID, rev2, RoleQuality, "qa-2")
	if _, err := g.Publish(rcp.ID, rev2.ID, "tester", busy); !errors.Is(err, ErrActiveRun) {
		t.Fatalf("publish over in-flight run: want ErrActiveRun, got %v", err)
	}
	active, _ := g.ActiveRelease(rcp.ID)
	if active.RevisionNumber != 1 {
		t.Fatalf("in-flight run must keep revision 1 active, got %d", active.RevisionNumber)
	}
	history := g.List(rcp.ID)
	if history[0].Status != ReviewInReview { // newest first = rev2
		t.Fatalf("rev2 must remain in_review after blocked publish, got %s", history[0].Status)
	}

	// Once the run finishes, publish succeeds and supersedes revision 1.
	if _, err := g.Publish(rcp.ID, rev2.ID, "tester", idle); err != nil {
		t.Fatal(err)
	}
	active, _ = g.ActiveRelease(rcp.ID)
	if active.RevisionNumber != 2 {
		t.Fatalf("expected revision 2 active, got %d", active.RevisionNumber)
	}
	for _, record := range g.List(rcp.ID) {
		if record.RevisionNumber == 1 && record.Status != ReleaseSuperseded {
			t.Fatalf("revision 1 should be superseded, got %s", record.Status)
		}
	}
}

func TestWithdrawAndRollback(t *testing.T) {
	g := NewReleaseRegistry()
	store := NewRevisionStore()
	rcp := newTestRecipe(t, 1, twoLayers())
	rev1, _ := store.Freeze(rcp)
	if _, err := g.OpenReview(rev1, "author"); err != nil {
		t.Fatal(err)
	}
	idle := fakeGuard{active: map[model.ID]bool{}}
	approveAndPublish(t, g, idle, rcp.ID, rev1)

	// A run binds revision 1, then the release is withdrawn: the bound
	// snapshot survives but no new run can bind.
	runID := model.NewID()
	if _, err := g.Bind(runID, rcp.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Withdraw(rcp.ID, "tester", "defect"); err != nil {
		t.Fatal(err)
	}
	if _, ok := g.ActiveRelease(rcp.ID); ok {
		t.Fatal("withdrawn recipe must have no active release")
	}
	if got, ok := g.Binding(runID); !ok || got.RevisionNumber != 1 || len(got.Layers) != 2 {
		t.Fatalf("bound snapshot must survive withdrawal, got %+v ok=%v", got, ok)
	}
	if _, err := g.Bind(model.NewID(), rcp.ID); !errors.Is(err, ErrNoActiveRelease) {
		t.Fatalf("bind after withdraw: want ErrNoActiveRelease, got %v", err)
	}

	// Rollback restores revision 1 without new approval evidence.
	if _, err := g.Rollback(rcp.ID, rev1.ID, "tester", idle); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
	active, _ := g.ActiveRelease(rcp.ID)
	if active.RevisionNumber != 1 || active.Status != ReleaseActive {
		t.Fatalf("rollback did not restore revision 1: %+v", active)
	}

	// Rollback is blocked while a run is in flight.
	if _, err := g.Withdraw(rcp.ID, "tester", "again"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Rollback(rcp.ID, rev1.ID, "tester", fakeGuard{map[model.ID]bool{rcp.ID: true}}); !errors.Is(err, ErrActiveRun) {
		t.Fatalf("rollback over in-flight run: want ErrActiveRun, got %v", err)
	}
}

func TestRollbackRequiresPriorRelease(t *testing.T) {
	g := NewReleaseRegistry()
	store := NewRevisionStore()
	rcp := newTestRecipe(t, 1, twoLayers())
	rev1, _ := store.Freeze(rcp)
	if _, err := g.OpenReview(rev1, "author"); err != nil {
		t.Fatal(err)
	}
	// An in_review revision cannot be rolled back.
	if _, err := g.Rollback(rcp.ID, rev1.ID, "tester", fakeGuard{map[model.ID]bool{}}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("rollback of in_review revision: want ErrInvalidState, got %v", err)
	}
}

func TestBoundSnapshotIsImmutable(t *testing.T) {
	g := NewReleaseRegistry()
	store := NewRevisionStore()
	rcp := newTestRecipe(t, 1, twoLayers())
	rev1, _ := store.Freeze(rcp)
	if _, err := g.OpenReview(rev1, "author"); err != nil {
		t.Fatal(err)
	}
	approveAndPublish(t, g, fakeGuard{map[model.ID]bool{}}, rcp.ID, rev1)
	firstRun := model.NewID()
	if _, err := g.Bind(firstRun, rcp.ID); err != nil {
		t.Fatal(err)
	}
	bound, err := g.Bind(model.NewID(), rcp.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Mutating the returned slice must not corrupt registry state.
	bound.Layers[0].Material = "TAMPERED"
	stored, ok := g.Binding(firstRun)
	if !ok {
		t.Fatal("bound run missing")
	}
	if stored.Layers[0].Material == "TAMPERED" {
		t.Fatal("bound snapshot mutation leaked into registry")
	}
	fresh, err := g.Bind(model.NewID(), rcp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Layers[0].Material == "TAMPERED" {
		t.Fatal("bound snapshot mutation leaked into new bindings")
	}
}
