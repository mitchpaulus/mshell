package main

import (
	"math/rand"
	"testing"
)

// TestSubstitutionVersionsMatchCopies drives the versioned substitution
// with random allocations, writes, checkpoints, and rollbacks to any
// earlier checkpoint, in any order, and compares it after every step with
// a model that checkpoints by copying the whole slice.
func TestSubstitutionVersionsMatchCopies(t *testing.T) {
	arena := NewTypeArena()
	values := []TypeId{TidNothing, TidInt, TidStr, TidBool}
	for seed := int64(0); seed < 300; seed++ {
		rng := rand.New(rand.NewSource(seed))
		var s Substitution
		var model []TypeId
		type saved struct {
			cp   SubstCheckpoint
			copy []TypeId
		}
		var checkpoints []saved

		for step := 0; step < 400; step++ {
			switch op := rng.Intn(10); {
			case op < 2 || len(model) == 0:
				s.FreshVar(arena)
				model = append(model, TidNothing)
			case op < 6:
				v := TypeVarId(rng.Intn(len(model)))
				val := values[rng.Intn(len(values))]
				s.set(v, val)
				model[v] = val
			case op < 8:
				checkpoints = append(checkpoints, saved{s.Checkpoint(), append([]TypeId(nil), model...)})
			default:
				if len(checkpoints) == 0 {
					continue
				}
				c := checkpoints[rng.Intn(len(checkpoints))]
				s.Rollback(c.cp)
				// Slots allocated after the checkpoint stay, unbound.
				for i := range model {
					if i < len(c.copy) {
						model[i] = c.copy[i]
					} else {
						model[i] = TidNothing
					}
				}
			}
			if len(s.bound) != len(model) {
				t.Fatalf("seed %d step %d: len %d, model %d", seed, step, len(s.bound), len(model))
			}
			for i := range model {
				if s.bound[i] != model[i] {
					t.Fatalf("seed %d step %d: slot %d is %d, model %d", seed, step, i, s.bound[i], model[i])
				}
			}
		}
	}
}
