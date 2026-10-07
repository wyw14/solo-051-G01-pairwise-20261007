package journal

import "github.com/wyw14/cry-161/internal/model"

func RunStarted(run model.Run) model.Event {
	return model.NewEvent("run.started", run.ID, map[string]any{"batch": run.Batch, "state": run.State})
}
func RunStateChanged(run model.Run, previous model.RunState) model.Event {
	return model.NewEvent("run.state", run.ID, map[string]any{"from": previous, "to": run.State})
}
func IncidentRaised(incident model.Incident) model.Event {
	return model.NewEvent("incident", incident.RunID, map[string]any{"severity": incident.Severity, "message": incident.Message})
}

func RevisionFrozen(recipeID model.ID, revision int, revisionID model.ID) model.Event {
	return model.NewEvent("recipe.revision_frozen", recipeID, map[string]any{"revision": revision, "revision_id": revisionID})
}
func ReviewOpened(recipeID model.ID, revision int, releaseID model.ID) model.Event {
	return model.NewEvent("recipe.review_opened", recipeID, map[string]any{"revision": revision, "release_id": releaseID})
}
func ApprovalRecorded(recipeID model.ID, revision int, role, reviewer, evidenceID string) model.Event {
	return model.NewEvent("recipe.approval_recorded", recipeID, map[string]any{"revision": revision, "role": role, "reviewer": reviewer, "evidence_id": evidenceID})
}
func RecipePublished(recipeID model.ID, revision int, releaseID model.ID, actor string) model.Event {
	return model.NewEvent("recipe.published", recipeID, map[string]any{"revision": revision, "release_id": releaseID, "actor": actor})
}
func RecipeWithdrawn(recipeID model.ID, revision int, actor, reason string) model.Event {
	return model.NewEvent("recipe.withdrawn", recipeID, map[string]any{"revision": revision, "actor": actor, "reason": reason})
}
func RecipeRolledBack(recipeID model.ID, revision int, releaseID model.ID, actor string) model.Event {
	return model.NewEvent("recipe.rolled_back", recipeID, map[string]any{"revision": revision, "release_id": releaseID, "actor": actor})
}
func RunBound(runID, recipeID model.ID, revision int, releaseID model.ID) model.Event {
	return model.NewEvent("run.bound_release", runID, map[string]any{"recipe_id": recipeID, "revision": revision, "release_id": releaseID})
}
