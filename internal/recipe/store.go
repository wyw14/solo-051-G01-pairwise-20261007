package recipe

import (
	"fmt"
	"github.com/wyw14/cry-161/internal/model"
	"sync"
)

type Store struct {
	mu     sync.RWMutex
	active map[model.ID]*model.Recipe
	drafts map[model.ID]*model.Recipe
}

func NewStore() *Store {
	return &Store{active: map[model.ID]*model.Recipe{}, drafts: map[model.ID]*model.Recipe{}}
}
func (s *Store) Publish(recipe *model.Recipe) {
	s.mu.Lock()
	defer s.mu.Unlock()
	recipe.Published = true
	s.active[recipe.ID] = cloneRecipe(recipe)
}

// Replace installs a recipe snapshot as the active projection. It is used by
// governance to point production at an immutable frozen revision.
func (s *Store) Replace(recipe *model.Recipe) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := cloneRecipe(recipe)
	copy.Published = true
	s.active[copy.ID] = copy
}

// Retract removes the active projection for a recipe.
func (s *Store) Retract(id model.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.active, id)
}
func (s *Store) Active(id model.ID) (*model.Recipe, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recipe, ok := s.active[id]
	if !ok {
		return nil, fmt.Errorf("recipe not found")
	}
	return cloneRecipe(recipe), nil
}
func (s *Store) List() []*model.Recipe {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*model.Recipe, 0, len(s.active))
	for _, r := range s.active {
		result = append(result, cloneRecipe(r))
	}
	return result
}
func cloneRecipe(in *model.Recipe) *model.Recipe {
	out := *in
	out.Layers = make(map[int]model.Layer, len(in.Layers))
	for k, v := range in.Layers {
		out.Layers[k] = v
	}
	return &out
}
