package specialize

import "testing"

// TestFamilyDeoptParentConsistency asserts that the two generated
// tables agree with each other: every variant listed under a parent
// in Family must round-trip through DeoptParent to the same parent.
// The cases_generator emits both tables from the same family() AST,
// so a divergence here is a generator bug.
func TestFamilyDeoptParentConsistency(t *testing.T) {
	for parent, variants := range Family {
		for _, v := range variants {
			if got := DeoptParent[v]; got != parent {
				t.Errorf("Family[%s] contains %s but DeoptParent[%s]=%s",
					parent.Name(), v.Name(), v.Name(), got.Name())
			}
		}
	}
	// Inverse: every entry in DeoptParent must appear in Family[parent].
	for variant, parent := range DeoptParent {
		found := false
		for _, v := range Family[parent] {
			if v == variant {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("DeoptParent[%s]=%s but %s is not in Family[%s]",
				variant.Name(), parent.Name(), variant.Name(), parent.Name())
		}
	}
}

// TestDeoptFixedPoint asserts that an unspecialized opcode deopts to
// itself. Family parents are listed in Family (so they can be reached
// by the specializer) but absent from DeoptParent.
func TestDeoptFixedPoint(t *testing.T) {
	for parent := range Family {
		if got := Deopt(parent); got != parent {
			t.Errorf("Deopt(%s)=%s, want %s (fixed point)",
				parent.Name(), got.Name(), parent.Name())
		}
	}
}

// TestDeoptSpecializedVariant asserts that every variant in Family
// deopts to its parent.
func TestDeoptSpecializedVariant(t *testing.T) {
	for parent, variants := range Family {
		for _, v := range variants {
			if got := Deopt(v); got != parent {
				t.Errorf("Deopt(%s)=%s, want %s",
					v.Name(), got.Name(), parent.Name())
			}
		}
	}
}
