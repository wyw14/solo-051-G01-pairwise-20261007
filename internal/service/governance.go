package service

import (
	"fmt"

	"github.com/wyw14/cry-161/internal/journal"
	"github.com/wyw14/cry-161/internal/model"
	"github.com/wyw14/cry-161/internal/recipe"
)

// FreezeDraft compiles a saved draft and freezes it as an immutable
// revision candidate. Missing layers, illegal layer order or unusable
// process values make the compile fail, so no revision is created.
func (s *Service) FreezeDraft(recipeID model.ID) (recipe.Revision, error) {
	draft, ok := s.Drafts.Get(recipeID)
	if !ok {
		return recipe.Revision{}, fmt.Errorf("draft not found")
	}
	// Revision numbering is governed by the revision store, not by draft
	// edit counters, so numbering stays contiguous.
	draft.Revision = s.Revisions.NextNumber(recipeID)
	rev, err := s.Revisions.Freeze(draft)
	if err != nil {
		return recipe.Revision{}, err
	}
	_ = s.Journal.Append(journal.RevisionFrozen(recipeID, rev.Number, rev.ID))
	return rev, nil
}

// OpenReview moves a freshly frozen revision into the review queue.
func (s *Service) OpenReview(recipeID, revisionID model.ID, actor string) (recipe.ReleaseRecord, error) {
	rev, ok := s.Revisions.Get(recipeID, revisionID)
	if !ok {
		return recipe.ReleaseRecord{}, fmt.Errorf("%w: revision %s", recipe.ErrReviewNotFound, revisionID)
	}
	record, err := s.Releases.OpenReview(rev, actor)
	if err != nil {
		return recipe.ReleaseRecord{}, err
	}
	_ = s.Journal.Append(journal.ReviewOpened(recipeID, rev.Number, record.ID))
	return record, nil
}

// RecordApproval attaches one piece of review evidence.
func (s *Service) RecordApproval(recipeID, revisionID model.ID, approval recipe.Approval) (recipe.ReleaseRecord, error) {
	record, err := s.Releases.RecordApproval(recipeID, revisionID, approval)
	if err != nil {
		return recipe.ReleaseRecord{}, err
	}
	_ = s.Journal.Append(journal.ApprovalRecorded(recipeID, record.RevisionNumber, string(approval.Role), approval.Reviewer, approval.EvidenceID))
	return record, nil
}

// Publish releases an approved, fully-evidenced revision. It refuses while
// a started run is still in flight so an in-flight run can never be
// covered by a new publication. The recipe store follows the registry
// decision.
func (s *Service) Publish(recipeID, revisionID model.ID, actor string) (recipe.ReleaseRecord, error) {
	record, err := s.Releases.Publish(recipeID, revisionID, actor, s.Runs)
	if err != nil {
		return recipe.ReleaseRecord{}, err
	}
	rev, ok := s.Revisions.Get(recipeID, revisionID)
	if !ok {
		return recipe.ReleaseRecord{}, fmt.Errorf("%w: revision %s", recipe.ErrReviewNotFound, revisionID)
	}
	s.Recipes.Publish(&model.Recipe{ID: recipeID, Name: rev.Name, Revision: rev.Number, Layers: layerSliceToMap(rev.Layers)})
	_ = s.Journal.Append(journal.RecipePublished(recipeID, rev.Number, record.ID, actor))
	return record, nil
}

// Withdraw pulls the active release out of service. Bound, in-flight runs
// keep executing their immutable snapshot; new runs cannot start.
func (s *Service) Withdraw(recipeID model.ID, actor, reason string) (recipe.ReleaseRecord, error) {
	record, err := s.Releases.Withdraw(recipeID, actor, reason)
	if err != nil {
		return recipe.ReleaseRecord{}, err
	}
	s.Recipes.Deactivate(recipeID)
	_ = s.Journal.Append(journal.RecipeWithdrawn(recipeID, record.RevisionNumber, actor, reason))
	return record, nil
}

// Rollback restores a prior superseded or withdrawn release to active
// without a new freeze or review: its revision content and approval
// evidence are already immutable on record.
func (s *Service) Rollback(recipeID, revisionID model.ID, actor string) (recipe.ReleaseRecord, error) {
	record, err := s.Releases.Rollback(recipeID, revisionID, actor, s.Runs)
	if err != nil {
		return recipe.ReleaseRecord{}, err
	}
	rev, ok := s.Revisions.Get(recipeID, revisionID)
	if !ok {
		return recipe.ReleaseRecord{}, fmt.Errorf("%w: revision %s", recipe.ErrReviewNotFound, revisionID)
	}
	s.Recipes.Publish(&model.Recipe{ID: recipeID, Name: rev.Name, Revision: rev.Number, Layers: layerSliceToMap(rev.Layers)})
	_ = s.Journal.Append(journal.RecipeRolledBack(recipeID, rev.Number, record.ID, actor))
	return record, nil
}

// Reject closes a review that cannot pass.
func (s *Service) Reject(recipeID, revisionID model.ID, actor, reason string) (recipe.ReleaseRecord, error) {
	return s.Releases.Reject(recipeID, revisionID, actor, reason)
}

// ReleaseHistory lists governance records for a recipe.
func (s *Service) ReleaseHistory(recipeID model.ID) []recipe.ReleaseRecord {
	return s.Releases.List(recipeID)
}

// FrozenRevisions lists frozen revisions for a recipe.
func (s *Service) FrozenRevisions(recipeID model.ID) []recipe.Revision {
	return s.Revisions.List(recipeID)
}

func layerSliceToMap(layers []model.Layer) map[int]model.Layer {
	out := make(map[int]model.Layer, len(layers))
	for _, layer := range layers {
		out[layer.Number] = layer
	}
	return out
}
