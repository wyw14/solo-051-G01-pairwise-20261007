package run

import (
	"context"
	"fmt"
	"github.com/wyw14/cry-161/internal/chamber"
	"github.com/wyw14/cry-161/internal/journal"
	"github.com/wyw14/cry-161/internal/model"
	"github.com/wyw14/cry-161/internal/recipe"
	"github.com/wyw14/cry-161/internal/substrate"
	"github.com/wyw14/cry-161/internal/vacuum"
	"time"
)

type Coordinator struct {
	Registry *Registry
	Recipes  *recipe.Store
	Releases *recipe.ReleaseRegistry
	Carrier  *substrate.Carrier
	Chamber  *chamber.Chamber
	Vacuum   *vacuum.Controller
	Journal  *journal.Store
}

func (c *Coordinator) Start(ctx context.Context, batch string, recipeID model.ID) (*model.Run, error) {
	if c.Releases == nil {
		return nil, fmt.Errorf("release governance is not configured")
	}
	runID := model.NewID()
	if err := c.Carrier.Load(batch, runID); err != nil {
		return nil, err
	}
	// Governance binding is the sole admission gate: every run pins
	// itself to the immutable snapshot of the active release. A withdrawn
	// or never published recipe is rejected here, and later publications,
	// withdrawals or rollbacks cannot change what this run executes.
	bound, err := c.Releases.Bind(runID, recipeID)
	if err != nil {
		c.Carrier.Unload(batch)
		return nil, err
	}
	run := &model.Run{ID: runID, Batch: batch, RecipeID: recipeID, RecipeRevision: bound.RevisionNumber, RecipeReleaseID: bound.ReleaseID, State: model.Loading, CreatedAt: timeNow(), UpdatedAt: timeNow()}
	_ = c.Journal.Append(journal.RunBound(runID, recipeID, bound.RevisionNumber, bound.ReleaseID))
	c.Registry.Add(run)
	if !c.Registry.Transition(run.ID, model.Pumping) {
		return nil, fmt.Errorf("state transition failed")
	}
	if err := c.Vacuum.Start(ctx); err != nil {
		c.Registry.UpdateError(run.ID, err)
		return nil, err
	}
	c.Chamber.SetVacuum(c.Vacuum.Pressure(), c.Vacuum.Stable())
	c.Chamber.SetInterlock("vacuum", true)
	_ = c.Journal.Append(journal.RunStarted(*run))
	value, _ := c.Registry.Get(run.ID)
	return value, nil
}
func timeNow() time.Time { return time.Now().UTC() }
