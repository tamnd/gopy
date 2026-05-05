//go:build !windows

package gil

import (
	"syscall"
	"testing"
	"time"
)

func TestSignalBridgeRealSignal(t *testing.T) {
	var b Breaker
	var p Pending
	bridge := NewSignalBridge(&b, &p)
	defer bridge.Stop()

	got := make(chan struct{}, 1)
	bridge.Handle(syscall.SIGUSR1, func() error {
		got <- struct{}{}
		return nil
	})

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatalf("kill: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for !b.IsSet(BreakerCallsPending) {
		select {
		case <-deadline:
			t.Fatal("breaker bit never set")
		case <-time.After(5 * time.Millisecond):
		}
	}

	if err := p.Drain(); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("handler did not run after Drain")
	}
}
