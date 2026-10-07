package service

import (
	"context"
	"fmt"
	"github.com/wyw14/cry-161/internal/chamber"
	"github.com/wyw14/cry-161/internal/journal"
	"github.com/wyw14/cry-161/internal/model"
	"github.com/wyw14/cry-161/internal/recipe"
	"github.com/wyw14/cry-161/internal/rotation"
	"github.com/wyw14/cry-161/internal/run"
	"github.com/wyw14/cry-161/internal/source"
	"github.com/wyw14/cry-161/internal/substrate"
	"github.com/wyw14/cry-161/internal/vacuum"
	"os"
)

type Service struct {
	Runs       *run.Registry
	Recipes    *recipe.Store
	Revisions  *recipe.RevisionStore
	Governance *recipe.Governance
	Drafts     *recipe.DraftBook
	Carrier    *substrate.Carrier
	Chamber    *chamber.Chamber
	Vacuum     *vacuum.Controller
	Source     *source.Controller
	Rotation   *rotation.Controller
	Journal    *journal.Store
}

func New() *Service {
	runs := run.NewRegistry()
	revisions := recipe.NewRevisionStore()
	recipes := recipe.NewStore()
	governance := recipe.NewGovernance(revisions, recipes, runs)
	dir := os.Getenv("OPTICCOAT_DATA")
	if dir == "" {
		dir = "data"
	}
	svc := &Service{Runs: runs, Recipes: recipes, Revisions: revisions, Governance: governance, Drafts: recipe.NewDraftBook(), Carrier: substrate.NewCarrier(), Chamber: chamber.New("coat-01"), Vacuum: vacuum.NewController(), Source: source.NewController(100), Rotation: rotation.NewController(), Journal: journal.New(dir)}
	svc.seed()
	return svc
}

// seed publishes the default recipe through the full governance workflow, so
// production always starts from a frozen, dual-approved immutable revision.
func (s *Service) seed() {
	seed := &model.Recipe{ID: model.NewID(), Name: "AR coating", Revision: 1, Layers: map[int]model.Layer{1: {Number: 1, Material: "SiO2", TargetNanometers: 100, RateNanometers: 2}, 2: {Number: 2, Material: "TiO2", TargetNanometers: 60, RateNanometers: 3}}}
	release, _, err := s.Governance.SubmitRecipe(seed)
	if err != nil {
		panic(fmt.Sprintf("seed recipe cannot be frozen: %v", err))
	}
	release, err = s.Governance.Review(release.ID, model.ApprovalProcess, "bootstrap-process", "seed process approval")
	if err != nil {
		panic(fmt.Sprintf("seed recipe process review failed: %v", err))
	}
	release, err = s.Governance.Review(release.ID, model.ApprovalQuality, "bootstrap-quality", "seed quality approval")
	if err != nil {
		panic(fmt.Sprintf("seed recipe quality review failed: %v", err))
	}
	if _, err := s.Governance.Publish(release.ID); err != nil {
		panic(fmt.Sprintf("seed recipe publication failed: %v", err))
	}
}

func (s *Service) RecipesList() []*model.Recipe { return s.Recipes.List() }
func (s *Service) RunsList() []*model.Run       { return s.Runs.List() }
func (s *Service) Start(ctx context.Context, batch string, recipeID model.ID) (*model.Run, error) {
	coordinator := &run.Coordinator{Registry: s.Runs, Governance: s.Governance, Carrier: s.Carrier, Chamber: s.Chamber, Vacuum: s.Vacuum, Journal: s.Journal}
	return coordinator.Start(ctx, batch, recipeID)
}
func (s *Service) UpdateDraft(id model.ID, layer model.Layer) error {
	if !s.Drafts.UpdateLayer(id, layer) {
		return fmt.Errorf("draft not found")
	}
	return nil
}
