package recipe

import (
	"fmt"
	"github.com/wyw14/cry-161/internal/model"
	"sort"
	"sync"
	"time"
)

type Revision struct {
	ID        model.ID      `json:"id"`
	RecipeID  model.ID      `json:"recipe_id"`
	Name      string        `json:"name"`
	Number    int           `json:"number"`
	CreatedAt time.Time     `json:"created_at"`
	Digest    string        `json:"digest"`
	Layers    []model.Layer `json:"layers"`
}

type RevisionStore struct {
	mu     sync.RWMutex
	values map[model.ID][]Revision
}

func NewRevisionStore() *RevisionStore {
	return &RevisionStore{values: map[model.ID][]Revision{}}
}

func (s *RevisionStore) Freeze(recipe *model.Recipe) (Revision, error) {
	plan, err := Compile(recipe)
	if err != nil {
		return Revision{}, err
	}
	revision := Revision{ID: model.NewID(), RecipeID: plan.RecipeID, Name: recipe.Name, Number: plan.Revision, CreatedAt: time.Now().UTC(), Digest: ContentDigest(plan.Layers), Layers: append([]model.Layer(nil), plan.Layers...)}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing := s.values[recipe.ID]
	for _, value := range existing {
		if value.Number == revision.Number {
			return Revision{}, fmt.Errorf("revision already frozen")
		}
	}
	s.values[recipe.ID] = append(existing, revision)
	return revision, nil
}

func (s *RevisionStore) Get(recipeID model.ID, number int) (Revision, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, value := range s.values[recipeID] {
		if value.Number == number {
			return cloneRevision(value), true
		}
	}
	return Revision{}, false
}

// Latest returns the highest-numbered frozen revision for a recipe.
func (s *RevisionStore) Latest(recipeID model.ID) (Revision, bool) {
	revisions := s.List(recipeID)
	if len(revisions) == 0 {
		return Revision{}, false
	}
	return revisions[len(revisions)-1], true
}

func (s *RevisionStore) List(recipeID model.ID) []Revision {
	s.mu.RLock()
	result := append([]Revision(nil), s.values[recipeID]...)
	s.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].Number < result[j].Number })
	for index := range result {
		result[index] = cloneRevision(result[index])
	}
	return result
}

func cloneRevision(in Revision) Revision {
	in.Layers = append([]model.Layer(nil), in.Layers...)
	return in
}
