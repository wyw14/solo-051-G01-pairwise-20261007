package recipe

import (
	"errors"
	"testing"

	"github.com/wyw14/cry-161/internal/model"
)

func layer(n int) model.Layer {
	return model.Layer{Number: n, Material: "SiO2", TargetNanometers: 100, RateNanometers: 2}
}

func TestCompileRejectsMissingLayers(t *testing.T) {
	recipe := &model.Recipe{ID: model.NewID(), Name: "gappy", Revision: 1, Layers: map[int]model.Layer{
		1: layer(1),
		// layer 2 missing
		3: layer(3),
	}}
	_, err := Compile(recipe)
	if !errors.Is(err, ErrCompile) {
		t.Fatalf("expected ErrCompile for missing layer, got %v", err)
	}
}

func TestCompileRejectsIllegalOrder(t *testing.T) {
	// Map slots are contiguous but Layer.Number disagrees with the slot.
	recipe := &model.Recipe{ID: model.NewID(), Name: "swapped", Revision: 1, Layers: map[int]model.Layer{
		1: layer(2),
		2: layer(1),
	}}
	if _, err := Compile(recipe); !errors.Is(err, ErrCompile) {
		t.Fatalf("expected ErrCompile for illegal order, got %v", err)
	}
}

func TestCompileRejectsEmptyAndInvalidValues(t *testing.T) {
	if _, err := Compile(nil); !errors.Is(err, ErrCompile) {
		t.Fatalf("nil recipe should fail compile, got %v", err)
	}
	empty := &model.Recipe{ID: model.NewID(), Layers: map[int]model.Layer{}}
	if _, err := Compile(empty); !errors.Is(err, ErrCompile) {
		t.Fatalf("empty recipe should fail compile, got %v", err)
	}
	bad := layer(1)
	bad.TargetNanometers = 0
	if err := ValidateCompiled([]model.Layer{bad}); err == nil {
		t.Fatal("invalid layer values should be rejected")
	}
}

func TestCompileAcceptsContiguousLayers(t *testing.T) {
	recipe := &model.Recipe{ID: model.NewID(), Name: "ok", Revision: 1, Layers: map[int]model.Layer{
		2: layer(2), 1: layer(1), 3: layer(3),
	}}
	plan, err := Compile(recipe)
	if err != nil {
		t.Fatalf("valid recipe failed compile: %v", err)
	}
	if len(plan.Layers) != 3 || plan.Layers[0].Number != 1 || plan.Layers[2].Number != 3 {
		t.Fatalf("compiled layers out of order: %+v", plan.Layers)
	}
}
