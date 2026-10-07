package recipe

import (
	"fmt"
	"github.com/wyw14/cry-161/internal/model"
	"sort"
	"sync"
	"time"
)

// ReleaseState is the lifecycle state of a recipe release.
type ReleaseState string

const (
	ReleaseInReview   ReleaseState = "in_review"
	ReleasePublished  ReleaseState = "published"
	ReleaseSuperseded ReleaseState = "superseded"
	ReleaseWithdrawn  ReleaseState = "withdrawn"
)

// RecipeRelease binds a frozen, immutable revision to its review evidence and
// publication lifecycle.
type RecipeRelease struct {
	ID            model.ID                 `json:"id"`
	RecipeID      model.ID                 `json:"recipe_id"`
	RevisionID    model.ID                 `json:"revision_id"`
	Revision      int                      `json:"revision"`
	ContentDigest string                   `json:"content_digest"`
	State         ReleaseState             `json:"state"`
	Approvals     []model.ApprovalEvidence `json:"approvals"`
	CreatedAt     time.Time                `json:"created_at"`
	PublishedAt   time.Time                `json:"published_at,omitempty"`
	WithdrawnAt   time.Time                `json:"withdrawn_at,omitempty"`
	Reason        string                   `json:"reason,omitempty"`
}

// RunLiveness reports runs that are still in progress. Governance uses it to
// freeze production topology changes while any run is in progress: started
// runs pin their own immutable revision, and withdrawal/rollback must never
// reshuffle releases around them.
type RunLiveness interface {
	RunningCount(recipeID model.ID) int
	RunningCountForRevision(recipeID model.ID, revision int) int
}

// Governance owns the review, publish, withdraw and rollback workflow around
// drafts, compiled plans and frozen revisions.
type Governance struct {
	mu         sync.Mutex
	revisions  *RevisionStore
	projection *Store
	liveness   RunLiveness
	releases   map[model.ID]*RecipeRelease
	order      []model.ID
}

func NewGovernance(revisions *RevisionStore, projection *Store, liveness RunLiveness) *Governance {
	return &Governance{revisions: revisions, projection: projection, liveness: liveness, releases: map[model.ID]*RecipeRelease{}}
}

func (g *Governance) running(recipeID model.ID, revision int) int {
	if g.liveness == nil {
		return 0
	}
	return g.liveness.RunningCountForRevision(recipeID, revision)
}

func (g *Governance) runningAny(recipeID model.ID) int {
	if g.liveness == nil {
		return 0
	}
	return g.liveness.RunningCount(recipeID)
}

// SubmitRecipe compiles and freezes a draft snapshot, then opens a review for
// the frozen revision. Compilation rejects missing layers and illegal layer
// ordering before any review can start.
func (g *Governance) SubmitRecipe(recipe *model.Recipe) (RecipeRelease, Revision, error) {
	revision, err := g.revisions.Freeze(recipe)
	if err != nil {
		return RecipeRelease{}, Revision{}, err
	}
	release, err := g.Submit(recipe.ID, revision.Number)
	return release, revision, err
}

// Submit opens a review for an already frozen revision.
func (g *Governance) Submit(recipeID model.ID, number int) (RecipeRelease, error) {
	revision, ok := g.revisions.Get(recipeID, number)
	if !ok {
		return RecipeRelease{}, fmt.Errorf("revision %d is not frozen", number)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, id := range g.order {
		existing := g.releases[id]
		if existing.RevisionID == revision.ID && existing.State == ReleaseInReview {
			return RecipeRelease{}, fmt.Errorf("revision %d is already under review", number)
		}
	}
	release := &RecipeRelease{ID: model.NewID(), RecipeID: recipeID, RevisionID: revision.ID, Revision: revision.Number, ContentDigest: revision.Digest, State: ReleaseInReview, Approvals: []model.ApprovalEvidence{}, CreatedAt: time.Now().UTC()}
	g.releases[release.ID] = release
	g.order = append(g.order, release.ID)
	return cloneRelease(release), nil
}

// Review attaches approval evidence from a named reviewer for a required
// role. Evidence is bound to the frozen content digest. The two mandatory
// roles must be signed by different reviewers (segregation of duties).
func (g *Governance) Review(releaseID model.ID, role model.ApprovalRole, reviewer, comment string) (RecipeRelease, error) {
	if reviewer == "" {
		return RecipeRelease{}, fmt.Errorf("reviewer is required")
	}
	if !role.Valid() {
		return RecipeRelease{}, fmt.Errorf("approval role %q is invalid", role)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	release, ok := g.releases[releaseID]
	if !ok {
		return RecipeRelease{}, fmt.Errorf("release not found")
	}
	if release.State != ReleaseInReview {
		return RecipeRelease{}, fmt.Errorf("release is %s, not under review", release.State)
	}
	for _, approval := range release.Approvals {
		if approval.Role != role && approval.Reviewer == reviewer {
			return RecipeRelease{}, fmt.Errorf("reviewer %q cannot sign two approval roles", reviewer)
		}
	}
	evidence := model.ApprovalEvidence{RevisionID: release.RevisionID, Role: role, Reviewer: reviewer, ContentDigest: release.ContentDigest, Comment: comment, At: time.Now().UTC()}
	if err := evidence.Validate(); err != nil {
		return RecipeRelease{}, err
	}
	replaced := false
	for index, existing := range release.Approvals {
		if existing.Role == role {
			release.Approvals[index] = evidence
			replaced = true
			break
		}
	}
	if !replaced {
		release.Approvals = append(release.Approvals, evidence)
	}
	sort.SliceStable(release.Approvals, func(i, j int) bool { return release.Approvals[i].Role < release.Approvals[j].Role })
	return cloneRelease(release), nil
}

// Publish moves a reviewed release into production. It refuses when approval
// evidence is incomplete or bound to different content, and atomically moves
// the active projection to the immutable frozen revision. Runs already
// started keep pinning their own revision and are never overwritten.
func (g *Governance) Publish(releaseID model.ID) (RecipeRelease, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	release, ok := g.releases[releaseID]
	if !ok {
		return RecipeRelease{}, fmt.Errorf("release not found")
	}
	if release.State != ReleaseInReview {
		return RecipeRelease{}, fmt.Errorf("release is %s, not under review", release.State)
	}
	revision, ok := g.revisions.Get(release.RecipeID, release.Revision)
	if !ok {
		return RecipeRelease{}, fmt.Errorf("frozen revision %d is missing", release.Revision)
	}
	if err := validateEvidence(release, revision); err != nil {
		return RecipeRelease{}, err
	}
	if active := g.activeLocked(release.RecipeID); active != nil {
		active.State = ReleaseSuperseded
		active.WithdrawnAt = time.Now().UTC()
		active.Reason = fmt.Sprintf("superseded by revision %d", release.Revision)
	}
	release.State = ReleasePublished
	release.PublishedAt = time.Now().UTC()
	g.projection.Replace(recipeFromRevision(revision))
	return cloneRelease(release), nil
}

// Withdraw pulls the currently published revision out of production. It is
// refused while runs are in progress on that revision.
func (g *Governance) Withdraw(releaseID model.ID, reason string) (RecipeRelease, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	release, ok := g.releases[releaseID]
	if !ok {
		return RecipeRelease{}, fmt.Errorf("release not found")
	}
	if release.State != ReleasePublished {
		return RecipeRelease{}, fmt.Errorf("release is %s, not published", release.State)
	}
	if running := g.running(release.RecipeID, release.Revision); running > 0 {
		return RecipeRelease{}, fmt.Errorf("cannot withdraw revision %d: %d run(s) still in progress", release.Revision, running)
	}
	release.State = ReleaseWithdrawn
	release.WithdrawnAt = time.Now().UTC()
	release.Reason = reason
	g.projection.Retract(release.RecipeID)
	return cloneRelease(release), nil
}

// Rollback republishes a previously published revision. The revision and its
// approval evidence are immutable, so evidence is reused. A rollback is
// refused while any run of the recipe is still in progress.
func (g *Governance) Rollback(recipeID model.ID, targetRevision int, reason string) (RecipeRelease, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	target, ok := g.revisions.Get(recipeID, targetRevision)
	if !ok {
		return RecipeRelease{}, fmt.Errorf("revision %d is not frozen", targetRevision)
	}
	source := g.releaseForRevisionLocked(target.ID)
	if source == nil || source.State == ReleaseInReview {
		return RecipeRelease{}, fmt.Errorf("revision %d has no prior publication to roll back to", targetRevision)
	}
	if err := validateEvidence(source, target); err != nil {
		return RecipeRelease{}, fmt.Errorf("revision %d evidence is insufficient: %w", targetRevision, err)
	}
	if active := g.activeLocked(recipeID); active != nil {
		// Production topology is frozen while ANY run of this recipe is in
		// progress, including runs pinned to the rollback target itself.
		if running := g.runningAny(recipeID); running > 0 {
			return RecipeRelease{}, fmt.Errorf("cannot roll back from revision %d: %d run(s) still in progress", active.Revision, running)
		}
		active.State = ReleaseSuperseded
		active.WithdrawnAt = time.Now().UTC()
		active.Reason = fmt.Sprintf("rolled back to revision %d: %s", targetRevision, reason)
	}
	release := &RecipeRelease{ID: model.NewID(), RecipeID: recipeID, RevisionID: target.ID, Revision: target.Number, ContentDigest: target.Digest, State: ReleasePublished, Approvals: append([]model.ApprovalEvidence(nil), source.Approvals...), CreatedAt: time.Now().UTC(), PublishedAt: time.Now().UTC(), Reason: fmt.Sprintf("rollback to revision %d: %s", targetRevision, reason)}
	g.releases[release.ID] = release
	g.order = append(g.order, release.ID)
	g.projection.Replace(recipeFromRevision(target))
	return cloneRelease(release), nil
}

// Bind resolves the immutable revision a new run must pin. Starting a run is
// only possible against a published release.
func (g *Governance) Bind(recipeID model.ID) (Revision, error) {
	g.mu.Lock()
	active := g.activeLocked(recipeID)
	g.mu.Unlock()
	if active == nil {
		return Revision{}, fmt.Errorf("recipe has no published release")
	}
	revision, ok := g.revisions.Get(recipeID, active.Revision)
	if !ok {
		return Revision{}, fmt.Errorf("published revision %d is missing", active.Revision)
	}
	return revision, nil
}

func (g *Governance) GetRelease(id model.ID) (RecipeRelease, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	release, ok := g.releases[id]
	if !ok {
		return RecipeRelease{}, false
	}
	return cloneRelease(release), true
}

func (g *Governance) ListReleases(recipeID model.ID) []RecipeRelease {
	g.mu.Lock()
	defer g.mu.Unlock()
	result := make([]RecipeRelease, 0)
	for _, id := range g.order {
		release := g.releases[id]
		if release.RecipeID == recipeID {
			result = append(result, cloneRelease(release))
		}
	}
	return result
}

func (g *Governance) ActiveRelease(recipeID model.ID) (RecipeRelease, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	release := g.activeLocked(recipeID)
	if release == nil {
		return RecipeRelease{}, false
	}
	return cloneRelease(release), true
}

func (g *Governance) activeLocked(recipeID model.ID) *RecipeRelease {
	for index := len(g.order) - 1; index >= 0; index-- {
		release := g.releases[g.order[index]]
		if release.RecipeID == recipeID && release.State == ReleasePublished {
			return release
		}
	}
	return nil
}

func (g *Governance) releaseForRevisionLocked(revisionID model.ID) *RecipeRelease {
	for index := len(g.order) - 1; index >= 0; index-- {
		release := g.releases[g.order[index]]
		if release.RevisionID == revisionID {
			return release
		}
	}
	return nil
}

func validateEvidence(release *RecipeRelease, revision Revision) error {
	if ContentDigest(revision.Layers) != revision.Digest {
		return fmt.Errorf("frozen revision %d content has been tampered with", revision.Number)
	}
	if release.ContentDigest != revision.Digest {
		return fmt.Errorf("release is bound to different content than revision %d", revision.Number)
	}
	byRole := map[model.ApprovalRole]model.ApprovalEvidence{}
	for _, approval := range release.Approvals {
		if err := approval.Validate(); err != nil {
			return fmt.Errorf("approval evidence invalid: %w", err)
		}
		if approval.ContentDigest != revision.Digest {
			return fmt.Errorf("approval by %q is bound to different content", approval.Reviewer)
		}
		byRole[approval.Role] = approval
	}
	for _, role := range model.RequiredApprovalRoles {
		if _, ok := byRole[role]; !ok {
			return fmt.Errorf("approval evidence insufficient: missing %s", role)
		}
	}
	return nil
}

func recipeFromRevision(revision Revision) *model.Recipe {
	layers := make(map[int]model.Layer, len(revision.Layers))
	for _, layer := range revision.Layers {
		layers[layer.Number] = layer
	}
	return &model.Recipe{ID: revision.RecipeID, Name: revision.Name, Revision: revision.Number, Layers: layers, Published: true}
}

func cloneRelease(in *RecipeRelease) RecipeRelease {
	out := *in
	out.Approvals = append([]model.ApprovalEvidence(nil), in.Approvals...)
	return out
}
