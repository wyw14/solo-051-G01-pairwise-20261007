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
	Number    int           `json:"number"`
	Name      string        `json:"name"`
	CreatedAt time.Time     `json:"created_at"`
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
	revision := Revision{ID: model.NewID(), RecipeID: plan.RecipeID, Number: plan.Revision, Name: recipe.Name, CreatedAt: time.Now().UTC(), Layers: append([]model.Layer(nil), plan.Layers...)}
	s.mu.Lock()
	existing := s.values[recipe.ID]
	for _, value := range existing {
		if value.Number == revision.Number {
			s.mu.Unlock()
			return Revision{}, fmt.Errorf("revision already frozen")
		}
	}
	s.values[recipe.ID] = append(existing, revision)
	s.mu.Unlock()
	return revision, nil
}

func (s *RevisionStore) List(recipeID model.ID) []Revision {
	s.mu.RLock()
	result := append([]Revision(nil), s.values[recipeID]...)
	s.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].Number < result[j].Number })
	return result
}

// NextNumber returns the revision number the next freeze for the recipe
// must carry.
func (s *RevisionStore) NextNumber(recipeID model.ID) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	next := 1
	for _, rev := range s.values[recipeID] {
		if rev.Number >= next {
			next = rev.Number + 1
		}
	}
	return next
}

// Get returns a single frozen revision.
func (s *RevisionStore) Get(recipeID, revisionID model.ID) (Revision, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, rev := range s.values[recipeID] {
		if rev.ID == revisionID {
			rev.Layers = append([]model.Layer(nil), rev.Layers...)
			return rev, true
		}
	}
	return Revision{}, false
}
