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
	Runs      *run.Registry
	Recipes   *recipe.Store
	Drafts    *recipe.DraftBook
	Revisions *recipe.RevisionStore
	Releases  *recipe.ReleaseRegistry
	Carrier   *substrate.Carrier
	Chamber   *chamber.Chamber
	Vacuum    *vacuum.Controller
	Source    *source.Controller
	Rotation  *rotation.Controller
	Journal   *journal.Store
}

func New() *Service {
	runs := run.NewRegistry()
	recipes := recipe.NewStore()
	id := model.NewID()
	seed := &model.Recipe{ID: id, Name: "AR coating", Revision: 1, Layers: map[int]model.Layer{1: {Number: 1, Material: "SiO2", TargetNanometers: 100, RateNanometers: 2}, 2: {Number: 2, Material: "TiO2", TargetNanometers: 60, RateNanometers: 3}}}
	recipes.Publish(seed)
	revisions := recipe.NewRevisionStore()
	releases := recipe.NewReleaseRegistry()
	bootstrapRelease(revisions, releases, runs, seed)
	dir := os.Getenv("OPTICCOAT_DATA")
	if dir == "" {
		dir = "data"
	}
	return &Service{Runs: runs, Recipes: recipes, Drafts: recipe.NewDraftBook(), Revisions: revisions, Releases: releases, Carrier: substrate.NewCarrier(), Chamber: chamber.New("coat-01"), Vacuum: vacuum.NewController(), Source: source.NewController(100), Rotation: rotation.NewController(), Journal: journal.New(dir)}
}

// bootstrapRelease takes the seeded production recipe through the same
// freeze/review/approval/publish gates every later release passes, so the
// system starts with exactly one immutable active release.
func bootstrapRelease(revisions *recipe.RevisionStore, releases *recipe.ReleaseRegistry, guard recipe.ActiveRunGuard, seed *model.Recipe) {
	rev, err := revisions.Freeze(seed)
	if err != nil {
		panic(fmt.Sprintf("bootstrap freeze failed: %v", err))
	}
	record, err := releases.OpenReview(rev, "system-bootstrap")
	if err != nil {
		panic(fmt.Sprintf("bootstrap review failed: %v", err))
	}
	record, err = releases.RecordApproval(seed.ID, rev.ID, recipe.Approval{Role: recipe.RoleProcessEngineer, Reviewer: "bootstrap-process", EvidenceID: "bootstrap-process-evidence"})
	if err != nil {
		panic(fmt.Sprintf("bootstrap process approval failed: %v", err))
	}
	if _, err = releases.RecordApproval(seed.ID, rev.ID, recipe.Approval{Role: recipe.RoleQuality, Reviewer: "bootstrap-quality", EvidenceID: "bootstrap-quality-evidence"}); err != nil {
		panic(fmt.Sprintf("bootstrap quality approval failed: %v", err))
	}
	if _, err = releases.Publish(seed.ID, rev.ID, "system-bootstrap", guard); err != nil {
		panic(fmt.Sprintf("bootstrap publish failed: %v", err))
	}
	_ = record
}

func (s *Service) RecipesList() []*model.Recipe { return s.Recipes.List() }
func (s *Service) RunsList() []*model.Run       { return s.Runs.List() }
func (s *Service) Start(ctx context.Context, batch string, recipeID model.ID) (*model.Run, error) {
	coordinator := &run.Coordinator{Registry: s.Runs, Recipes: s.Recipes, Releases: s.Releases, Carrier: s.Carrier, Chamber: s.Chamber, Vacuum: s.Vacuum, Journal: s.Journal}
	return coordinator.Start(ctx, batch, recipeID)
}
func (s *Service) UpdateDraft(id model.ID, layer model.Layer) error {
	if !s.Drafts.UpdateLayer(id, layer) {
		return fmt.Errorf("draft not found")
	}
	return nil
}

// SaveDraft opens an editable copy of the active recipe. Editing a draft
// never touches the frozen release in-flight runs are bound to.
func (s *Service) SaveDraft(recipeID model.ID) (*model.Recipe, error) {
	active, err := s.Recipes.Active(recipeID)
	if err != nil {
		return nil, err
	}
	return s.Drafts.Save(active), nil
}
