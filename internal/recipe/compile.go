package recipe

import (
	"crypto/sha256"
	"encoding/hex"
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
		return Plan{}, fmt.Errorf("recipe is required")
	}
	if recipe.ID == "" {
		return Plan{}, fmt.Errorf("recipe id is required")
	}
	if len(recipe.Layers) == 0 {
		return Plan{}, fmt.Errorf("recipe has no layers")
	}
	keys := make([]int, 0, len(recipe.Layers))
	for k := range recipe.Layers {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	layers := make([]model.Layer, 0, len(keys))
	for position, k := range keys {
		if expected := position + 1; k != expected {
			return Plan{}, fmt.Errorf("layer sequence is invalid: expected layer %d, found %d", expected, k)
		}
		layer := recipe.Layers[k]
		if layer.Number != k {
			return Plan{}, fmt.Errorf("layer %d number does not match its position", k)
		}
		if layer.Material == "" {
			return Plan{}, fmt.Errorf("layer %d material is required", k)
		}
		if layer.TargetNanometers <= 0 {
			return Plan{}, fmt.Errorf("layer %d target thickness must be positive", k)
		}
		if layer.RateNanometers <= 0 {
			return Plan{}, fmt.Errorf("layer %d deposition rate must be positive", k)
		}
		layers = append(layers, layer)
	}
	return Plan{RecipeID: recipe.ID, Revision: recipe.Revision, Layers: layers}, nil
}

// ContentDigest returns the immutable content digest of a compiled layer
// sequence. Approval evidence is bound to this digest, so a frozen revision
// cannot be tampered with after review.
func ContentDigest(layers []model.Layer) string {
	hash := sha256.New()
	for _, layer := range layers {
		fmt.Fprintf(hash, "%d|%s|%.12f|%.12f\n", layer.Number, layer.Material, layer.TargetNanometers, layer.RateNanometers)
	}
	return hex.EncodeToString(hash.Sum(nil))
}
