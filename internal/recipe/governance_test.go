package recipe

import (
	"fmt"
	"testing"

	"github.com/wyw14/cry-161/internal/model"
)

type fakeLiveness struct {
	counts map[string]int
}

func (f *fakeLiveness) RunningCountForRevision(recipeID model.ID, revision int) int {
	return f.counts[fmt.Sprintf("%s:%d", recipeID, revision)]
}

func (f *fakeLiveness) RunningCount(recipeID model.ID) int {
	prefix := fmt.Sprintf("%s:", recipeID)
	total := 0
	for key, count := range f.counts {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			total += count
		}
	}
	return total
}

func (f *fakeLiveness) set(recipeID model.ID, revision, count int) {
	f.counts[fmt.Sprintf("%s:%d", recipeID, revision)] = count
}

func testRecipe(number int) *model.Recipe {
	return &model.Recipe{ID: model.NewID(), Name: "test", Revision: number, Layers: map[int]model.Layer{
		1: {Number: 1, Material: "SiO2", TargetNanometers: 100, RateNanometers: 2},
		2: {Number: 2, Material: "TiO2", TargetNanometers: 60, RateNanometers: 3},
	}}
}

func newHarness() (*Governance, *RevisionStore, *Store, *fakeLiveness) {
	liveness := &fakeLiveness{counts: map[string]int{}}
	revisions := NewRevisionStore()
	store := NewStore()
	return NewGovernance(revisions, store, liveness), revisions, store, liveness
}

// approveAndPublish runs the full dual-role review and publication flow.
func approveAndPublish(t *testing.T, g *Governance, release RecipeRelease) RecipeRelease {
	t.Helper()
	var err error
	release, err = g.Review(release.ID, model.ApprovalProcess, "alice", "process ok")
	if err != nil {
		t.Fatalf("process review: %v", err)
	}
	release, err = g.Review(release.ID, model.ApprovalQuality, "bob", "quality ok")
	if err != nil {
		t.Fatalf("quality review: %v", err)
	}
	release, err = g.Publish(release.ID)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	return release
}

func TestCompileRejectsMissingLayers(t *testing.T) {
	if _, err := Compile(nil); err == nil {
		t.Fatal("nil recipe should not compile")
	}
	empty := testRecipe(1)
	empty.Layers = map[int]model.Layer{}
	if _, err := Compile(empty); err == nil {
		t.Fatal("recipe without layers should not compile")
	}
	gapped := testRecipe(1)
	delete(gapped.Layers, 1)
	gapped.Layers[3] = model.Layer{Number: 3, Material: "Al2O3", TargetNanometers: 10, RateNanometers: 1}
	if _, err := Compile(gapped); err == nil {
		t.Fatal("recipe with a missing layer (1,2,3 gap) should not compile")
	}
	invalid := testRecipe(1)
	layer := invalid.Layers[1]
	layer.Material = ""
	invalid.Layers[1] = layer
	if _, err := Compile(invalid); err == nil {
		t.Fatal("layer without material should not compile")
	}
}

func TestSubmitRejectsIllegalOrder(t *testing.T) {
	g, _, _, _ := newHarness()
	illegal := testRecipe(1)
	illegal.Layers[1] = model.Layer{Number: 2, Material: "SiO2", TargetNanometers: 100, RateNanometers: 2}
	if _, _, err := g.SubmitRecipe(illegal); err == nil {
		t.Fatal("frozen revision with illegal layer ordering must be rejected")
	}
}

func TestPublishRequiresSufficientEvidence(t *testing.T) {
	g, _, _, _ := newHarness()
	release, _, err := g.SubmitRecipe(testRecipe(1))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := g.Publish(release.ID); err == nil {
		t.Fatal("publishing without any approval evidence must be rejected")
	}
	release, _ = g.Review(release.ID, model.ApprovalProcess, "alice", "")
	if _, err := g.Publish(release.ID); err == nil {
		t.Fatal("publishing with only one approval role must be rejected")
	}
	if _, err := g.Review(release.ID, model.ApprovalQuality, "alice", ""); err == nil {
		t.Fatal("the same reviewer must not sign both approval roles")
	}
}

func TestPublishPinsImmutableRevision(t *testing.T) {
	g, revisions, store, liveness := newHarness()
	first := testRecipe(1)
	r1 := approveAndPublish(t, g, mustSubmit(t, g, first))

	active, err := store.Active(first.ID)
	if err != nil || active.Revision != 1 {
		t.Fatalf("active projection should be revision 1, got %v (%v)", active, err)
	}
	pinned, err := g.Bind(first.ID)
	if err != nil || pinned.Number != 1 {
		t.Fatalf("bind should pin revision 1, got %d (%v)", pinned.Number, err)
	}
	liveness.set(first.ID, 1, 1)

	// A second revision goes through review and publication; the already
	// started run must keep pinning revision 1.
	second := testRecipe(2)
	second.ID = first.ID
	second.Layers[1] = model.Layer{Number: 1, Material: "SiO2", TargetNanometers: 120, RateNanometers: 2}
	approveAndPublish(t, g, mustSubmit(t, g, second))

	if active, _ := store.Active(first.ID); active.Revision != 2 {
		t.Fatalf("new runs should resolve revision 2, got %d", active.Revision)
	}
	bound, _ := revisions.Get(first.ID, 1)
	if bound.Layers[0].TargetNanometers != 100 {
		t.Fatal("frozen revision 1 content must not be overwritten by the new publication")
	}
	if pinned.Number != 1 || pinned.Layers[0].TargetNanometers != 100 {
		t.Fatalf("running run must stay pinned to revision 1, got %+v", pinned)
	}

	// The revision 1 release is marked superseded, but its frozen content and
	// the running run's pin remain intact.
	var release1 RecipeRelease
	for _, candidate := range g.ListReleases(first.ID) {
		if candidate.Revision == 1 {
			release1 = candidate
		}
	}
	if release1.State != ReleaseSuperseded {
		t.Fatalf("revision 1 release should be superseded, got %s", release1.State)
	}
	if r1.State != ReleasePublished {
		t.Fatal("original release object should remain untouched")
	}
}

func TestWithdrawAndRollback(t *testing.T) {
	g, revisions, store, liveness := newHarness()
	recipe := testRecipe(1)
	approveAndPublish(t, g, mustSubmit(t, g, recipe))

	second := testRecipe(2)
	second.ID = recipe.ID
	release2 := approveAndPublish(t, g, mustSubmit(t, g, second))

	// Rollback refused while a run on the current revision is in progress.
	liveness.set(recipe.ID, 2, 1)
	if _, err := g.Rollback(recipe.ID, 1, "regression"); err == nil {
		t.Fatal("rollback must be refused while the current revision has running runs")
	}
	// A run pinned to the rollback TARGET also blocks rollback: the active
	// release for that revision must never move under a started run.
	liveness.set(recipe.ID, 2, 0)
	liveness.set(recipe.ID, 1, 1)
	if _, err := g.Rollback(recipe.ID, 1, "regression"); err == nil {
		t.Fatal("rollback must be refused while a run pinned to the target revision is in progress")
	}
	liveness.set(recipe.ID, 1, 0)

	// A revision that was frozen but never published cannot be rolled back to.
	third := testRecipe(3)
	third.ID = recipe.ID
	if _, err := revisions.Freeze(third); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	if _, err := g.Rollback(recipe.ID, 3, ""); err == nil {
		t.Fatal("rollback to a never-published revision must be refused")
	}

	rolledBack, err := g.Rollback(recipe.ID, 1, "coating regression")
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rolledBack.Revision != 1 || rolledBack.State != ReleasePublished {
		t.Fatalf("rollback should republish revision 1, got %+v", rolledBack)
	}
	if active, _ := store.Active(recipe.ID); active.Revision != 1 {
		t.Fatalf("active projection should be revision 1, got %d", active.Revision)
	}
	refreshed, ok := g.GetRelease(release2.ID)
	if !ok || refreshed.State != ReleaseSuperseded {
		t.Fatalf("revision 2 release should be superseded after rollback, got %+v ok=%t", refreshed, ok)
	}
	if len(rolledBack.Approvals) != 2 {
		t.Fatal("rollback must reuse the immutable dual-role approval evidence")
	}
}

func TestWithdrawLifecycle(t *testing.T) {
	g, _, store, liveness := newHarness()
	recipe := testRecipe(1)
	release := approveAndPublish(t, g, mustSubmit(t, g, recipe))

	liveness.set(recipe.ID, 1, 2)
	if _, err := g.Withdraw(release.ID, ""); err == nil {
		t.Fatal("withdraw must be refused with runs in progress")
	}
	if _, err := store.Active(recipe.ID); err != nil {
		t.Fatal("active projection must remain while withdrawal is refused")
	}
	liveness.set(recipe.ID, 1, 0)
	withdrawn, err := g.Withdraw(release.ID, "quality escape")
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if withdrawn.State != ReleaseWithdrawn {
		t.Fatalf("release should be withdrawn, got %s", withdrawn.State)
	}
	if _, err := store.Active(recipe.ID); err == nil {
		t.Fatal("active projection must be removed after withdrawal")
	}
	if _, err := g.Bind(recipe.ID); err == nil {
		t.Fatal("no run may start against a withdrawn recipe")
	}
}

func TestEvidenceDigestTamperRejected(t *testing.T) {
	g, _, _, _ := newHarness()
	recipe := testRecipe(1)
	release := mustSubmit(t, g, recipe)
	release, _ = g.Review(release.ID, model.ApprovalProcess, "alice", "")
	release, _ = g.Review(release.ID, model.ApprovalQuality, "bob", "")

	// Simulate tampering with the release's content binding after review.
	g.mu.Lock()
	g.releases[release.ID].ContentDigest = "tampered"
	g.mu.Unlock()
	if _, err := g.Publish(release.ID); err == nil {
		t.Fatal("publishing evidence bound to different content must be rejected")
	}
}

func mustSubmit(t *testing.T, g *Governance, recipe *model.Recipe) RecipeRelease {
	t.Helper()
	release, _, err := g.SubmitRecipe(recipe)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	return release
}
