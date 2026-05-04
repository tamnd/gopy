package state_test

import (
	"testing"

	"github.com/tamnd/gopy/state"
)

type fakeExc struct{ msg string }

func (fakeExc) IsException() {}

func TestSetCurrentSwapClear(t *testing.T) {
	ts := state.NewThread()
	if ts.CurrentException() != nil {
		t.Fatal("fresh thread has no exception")
	}
	e := fakeExc{msg: "boom"}
	ts.SetException(e)
	if ts.CurrentException() != e {
		t.Fatal("CurrentException must return what SetException stored")
	}
	old := ts.SwapException(nil)
	if old != e {
		t.Fatal("SwapException must return the prior value")
	}
	if ts.CurrentException() != nil {
		t.Fatal("Swap to nil must clear")
	}
}
