package gc

import "testing"

// TestQSBR_AdvanceBumpsByIncr confirms Advance returns wrSeq + QSBRIncr
// and that subsequent calls keep climbing.
func TestQSBR_AdvanceBumpsByIncr(t *testing.T) {
	var s QSBRShared
	s.Init()
	got := s.Advance()
	if got != QSBRInitial+QSBRIncr {
		t.Errorf("first Advance got %d, want %d", got, QSBRInitial+QSBRIncr)
	}
	got = s.Advance()
	if got != QSBRInitial+2*QSBRIncr {
		t.Errorf("second Advance got %d, want %d", got, QSBRInitial+2*QSBRIncr)
	}
	if next := s.SharedNext(); next != got+QSBRIncr {
		t.Errorf("SharedNext got %d, want %d", next, got+QSBRIncr)
	}
}

// TestQSBR_ReserveRegisterUnregister walks the full Reserve / Register
// / Unregister cycle: the reserved index must round-trip through
// Register, the freelist must replenish on Unregister, and a second
// Reserve must reuse the released slot before growing.
func TestQSBR_ReserveRegisterUnregister(t *testing.T) {
	var s QSBRShared

	idx := s.Reserve()
	if idx < 0 {
		t.Fatalf("Reserve returned %d, want >= 0", idx)
	}
	qsbr := s.Register("tstate-1", idx)
	if qsbr == nil || qsbr.TState != "tstate-1" {
		t.Errorf("Register did not bind tstate to slot %d", idx)
	}
	if !qsbr.Allocated {
		t.Errorf("reserved slot must be Allocated=true")
	}

	s.Unregister(qsbr)
	if qsbr.Allocated {
		t.Errorf("Unregister must clear Allocated")
	}

	idx2 := s.Reserve()
	if idx2 != idx {
		t.Errorf("second Reserve got %d, want %d (LIFO reuse)", idx2, idx)
	}
}

// TestQSBR_ReserveGrowsAcrossMin confirms the array doubles past
// minQSBRArraySize when we ask for more slots than fit.
func TestQSBR_ReserveGrowsAcrossMin(t *testing.T) {
	var s QSBRShared
	const n = minQSBRArraySize*3 + 1
	seen := map[int]bool{}
	for i := 0; i < n; i++ {
		idx := s.Reserve()
		if idx < 0 {
			t.Fatalf("Reserve #%d returned -1", i)
		}
		if seen[idx] {
			t.Errorf("Reserve handed out duplicate index %d", idx)
		}
		seen[idx] = true
	}
	if s.size < n {
		t.Errorf("array size %d, want >= %d", s.size, n)
	}
}

// TestQSBR_PollReachesGoalAfterAllPublish confirms Poll(goal) returns
// false until every attached thread has published a sequence at or
// after the goal, and true once they have.
func TestQSBR_PollReachesGoalAfterAllPublish(t *testing.T) {
	var s QSBRShared
	s.Init()

	idxA := s.Reserve()
	idxB := s.Reserve()
	a := s.Register("a", idxA)
	b := s.Register("b", idxB)
	Attach(a)
	Attach(b)

	// Bump the writer past the readers' attached seq.
	goal := s.Advance()

	if Poll(a, goal) {
		t.Errorf("Poll must be false while readers are behind goal")
	}

	// One reader publishes the new seq; the other is still behind.
	s.QuiescentState(a)
	if Poll(a, goal) {
		t.Errorf("Poll must be false while one reader still trails")
	}

	// Detached threads do not block the goal.
	Detach(b)
	if !Poll(a, goal) {
		t.Errorf("Poll must succeed once all attached readers are caught up")
	}
}

// TestQSBR_FiniClears confirms Fini drops the array and zeros size.
func TestQSBR_FiniClears(t *testing.T) {
	var s QSBRShared
	s.Reserve()
	s.Fini()
	if s.size != 0 {
		t.Errorf("Fini should zero size, got %d", s.size)
	}
	if s.array != nil {
		t.Errorf("Fini should drop array")
	}
}
