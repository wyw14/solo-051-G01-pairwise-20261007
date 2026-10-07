package journal

import (
	"github.com/wyw14/cry-161/internal/model"
	"github.com/wyw14/cry-161/internal/recipe"
)

func RecipeSubmitted(release recipe.RecipeRelease) model.Event {
	return model.NewEvent("recipe.submitted", release.RecipeID, map[string]any{"release_id": release.ID, "revision": release.Revision, "digest": release.ContentDigest})
}

func RecipePublished(release recipe.RecipeRelease) model.Event {
	return model.NewEvent("recipe.published", release.RecipeID, map[string]any{"release_id": release.ID, "revision": release.Revision, "digest": release.ContentDigest})
}

func RecipeWithdrawn(release recipe.RecipeRelease) model.Event {
	return model.NewEvent("recipe.withdrawn", release.RecipeID, map[string]any{"release_id": release.ID, "revision": release.Revision, "reason": release.Reason})
}

func RecipeRolledBack(release recipe.RecipeRelease) model.Event {
	return model.NewEvent("recipe.rolled_back", release.RecipeID, map[string]any{"release_id": release.ID, "revision": release.Revision, "reason": release.Reason})
}
