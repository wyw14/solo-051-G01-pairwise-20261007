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
	Registry   *Registry
	Governance *recipe.Governance
	Carrier    *substrate.Carrier
	Chamber    *chamber.Chamber
	Vacuum     *vacuum.Controller
	Journal    *journal.Store
}

func (c *Coordinator) Start(ctx context.Context, batch string, recipeID model.ID) (*model.Run, error) {
	// Every run must bind an immutable, published frozen revision. The pinned
	// revision never changes, so a later publication cannot affect this run.
	pinned, err := c.Governance.Bind(recipeID)
	if err != nil {
		return nil, err
	}
	run := &model.Run{ID: model.NewID(), Batch: batch, RecipeID: recipeID, RecipeRevision: pinned.Number, State: model.Loading, CreatedAt: timeNow(), UpdatedAt: timeNow()}
	if err := c.Carrier.Load(batch, run.ID); err != nil {
		return nil, err
	}
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
