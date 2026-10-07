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
func (s *Store) Active(id model.ID) (*model.Recipe, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recipe, ok := s.active[id]
	if !ok {
		return nil, fmt.Errorf("recipe not found")
	}
	return cloneRecipe(recipe), nil
}

// Deactivate pulls a recipe out of the active set (release withdrawal).
func (s *Store) Deactivate(id model.ID) {
	s.mu.Lock()
	delete(s.active, id)
	s.mu.Unlock()
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
