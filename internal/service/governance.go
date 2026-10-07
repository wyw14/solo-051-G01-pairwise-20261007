package service

import (
	"fmt"
	"github.com/wyw14/cry-161/internal/journal"
	"github.com/wyw14/cry-161/internal/model"
	"github.com/wyw14/cry-161/internal/recipe"
)

func layerInRevision(revision recipe.Revision, number int) (model.Layer, bool) {
	for _, layer := range revision.Layers {
		if layer.Number == number {
			return layer, true
		}
	}
	return model.Layer{}, false
}

// StartDraft copies the currently published recipe into a new editable draft.
func (s *Service) StartDraft(recipeID model.ID) (*model.Recipe, error) {
	active, err := s.Recipes.Active(recipeID)
	if err != nil {
		return nil, err
	}
	return s.Drafts.Save(active), nil
}

func (s *Service) ListRevisions(recipeID model.ID) []recipe.Revision {
	return s.Revisions.List(recipeID)
}

func (s *Service) ListReleases(recipeID model.ID) []recipe.RecipeRelease {
	return s.Governance.ListReleases(recipeID)
}

func (s *Service) GetRelease(id model.ID) (recipe.RecipeRelease, bool) {
	return s.Governance.GetRelease(id)
}

// SubmitDraft freezes a draft snapshot and opens a review for the immutable
// revision. Compilation rejects missing layers and illegal ordering.
func (s *Service) SubmitDraft(draftID model.ID) (recipe.RecipeRelease, recipe.Revision, error) {
	draft, ok := s.Drafts.Get(draftID)
	if !ok {
		return recipe.RecipeRelease{}, recipe.Revision{}, fmt.Errorf("draft not found")
	}
	release, revision, err := s.Governance.SubmitRecipe(draft)
	if err != nil {
		return recipe.RecipeRelease{}, recipe.Revision{}, err
	}
	_ = s.Journal.Append(journal.RecipeSubmitted(release))
	return release, revision, nil
}

func (s *Service) ReviewRelease(releaseID model.ID, role model.ApprovalRole, reviewer, comment string) (recipe.RecipeRelease, error) {
	return s.Governance.Review(releaseID, role, reviewer, comment)
}

func (s *Service) PublishRelease(releaseID model.ID) (recipe.RecipeRelease, error) {
	release, err := s.Governance.Publish(releaseID)
	if err != nil {
		return recipe.RecipeRelease{}, err
	}
	_ = s.Journal.Append(journal.RecipePublished(release))
	return release, nil
}

func (s *Service) WithdrawRelease(releaseID model.ID, reason string) (recipe.RecipeRelease, error) {
	release, err := s.Governance.Withdraw(releaseID, reason)
	if err != nil {
		return recipe.RecipeRelease{}, err
	}
	_ = s.Journal.Append(journal.RecipeWithdrawn(release))
	return release, nil
}

func (s *Service) RollbackRecipe(recipeID model.ID, revision int, reason string) (recipe.RecipeRelease, error) {
	release, err := s.Governance.Rollback(recipeID, revision, reason)
	if err != nil {
		return recipe.RecipeRelease{}, err
	}
	_ = s.Journal.Append(journal.RecipeRolledBack(release))
	return release, nil
}
