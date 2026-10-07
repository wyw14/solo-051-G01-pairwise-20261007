package recipe

import (
	"fmt"
	"github.com/wyw14/cry-161/internal/model"
	"sort"
)

type Plan struct {
	RecipeID model.ID
	Revision int
	Layers   []model.Layer
}

func Compile(recipe *model.Recipe) (Plan, error) {
	if recipe == nil {
		return Plan{}, fmt.Errorf("%w: recipe is nil", ErrCompile)
	}
	if len(recipe.Layers) == 0 {
		return Plan{}, fmt.Errorf("%w: recipe %s has no layers", ErrCompile, recipe.ID)
	}
	keys := make([]int, 0, len(recipe.Layers))
	for k := range recipe.Layers {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	layers := make([]model.Layer, 0, len(keys))
	for position, key := range keys {
		expected := position + 1
		if key != expected {
			return Plan{}, fmt.Errorf("%w: missing layer %d: layer numbers must be contiguous from 1", ErrCompile, expected)
		}
		layer := recipe.Layers[key]
		if layer.Number != key {
			return Plan{}, fmt.Errorf("%w: illegal layer order: slot %d carries layer number %d", ErrCompile, key, layer.Number)
		}
		layers = append(layers, layer)
	}
	if err := ValidateCompiled(layers); err != nil {
		return Plan{}, fmt.Errorf("%w: %v", ErrCompile, err)
	}
	return Plan{RecipeID: recipe.ID, Revision: recipe.Revision, Layers: layers}, nil
}

// ValidateCompiled enforces the immutable layer contract every frozen
// revision must satisfy: layer numbers are strictly contiguous from 1
// (no missing layers), each Layer.Number matches its execution position
// (no illegal ordering) and every layer carries usable process values.
func ValidateCompiled(layers []model.Layer) error {
	if len(layers) == 0 {
		return fmt.Errorf("compiled plan has no layers")
	}
	for position, layer := range layers {
		expected := position + 1
		switch {
		case layer.Number == expected:
		case layer.Number < 1:
			return fmt.Errorf("missing layer %d: layer number is required", expected)
		case layer.Number < expected:
			return fmt.Errorf("illegal layer order: duplicate layer %d at position %d", layer.Number, expected)
		default:
			return fmt.Errorf("missing layer %d: layer numbers must be contiguous from 1", expected)
		}
		if layer.Material == "" {
			return fmt.Errorf("layer %d is missing material", expected)
		}
		if layer.TargetNanometers <= 0 {
			return fmt.Errorf("layer %d has non-positive target thickness", expected)
		}
		if layer.RateNanometers <= 0 {
			return fmt.Errorf("layer %d has non-positive deposition rate", expected)
		}
	}
	return nil
}
