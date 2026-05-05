// Signal bridge from os/signal to the eval breaker. SIGINT etc.
// arrive on a goroutine-safe channel; the bridge sets the
// SIGNALS_PENDING bit and (for SIGINT) queues a pending call that
// raises KeyboardInterrupt. The eval loop discovers the bit at the
// next poll point and drains the pending queue.
//
// CPython: Modules/signalmodule.c trip_signal
// CPython: Python/ceval_gil.c handle_signals

package gil

import (
	"os"
	"os/signal"
	"sync"
)

// SignalBridge wires os/signal delivery to a Breaker and a Pending
// queue. One per interpreter.
type SignalBridge struct {
	breaker *Breaker
	pending *Pending

	mu       sync.Mutex
	handlers map[os.Signal]PendingFunc
	ch       chan os.Signal
	stopCh   chan struct{}
	running  bool
}

// NewSignalBridge constructs an unstarted bridge.
func NewSignalBridge(b *Breaker, p *Pending) *SignalBridge {
	return &SignalBridge{
		breaker:  b,
		pending:  p,
		handlers: make(map[os.Signal]PendingFunc),
	}
}

// Handle registers fn to run (under the GIL, at the next poll point)
// when sig arrives. Replaces any previous handler for sig.
//
// CPython: Modules/signalmodule.c signal_signal_impl
func (s *SignalBridge) Handle(sig os.Signal, fn PendingFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[sig] = fn
	signal.Notify(s.signalChan(), sig)
}

// Unhandle stops receiving sig.
func (s *SignalBridge) Unhandle(sig os.Signal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.handlers, sig)
	signal.Reset(sig)
}

// Stop tears the bridge down. Any in-flight signal that has been
// dispatched to the channel still triggers its handler.
func (s *SignalBridge) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	stop := s.stopCh
	signal.Stop(s.ch)
	s.mu.Unlock()
	close(stop)
}

func (s *SignalBridge) signalChan() chan os.Signal {
	if !s.running {
		s.ch = make(chan os.Signal, 16)
		s.stopCh = make(chan struct{})
		s.running = true
		go s.loop(s.ch, s.stopCh)
	}
	return s.ch
}

func (s *SignalBridge) loop(ch chan os.Signal, stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case sig, ok := <-ch:
			if !ok {
				return
			}
			s.deliver(sig)
		}
	}
}

func (s *SignalBridge) deliver(sig os.Signal) {
	s.mu.Lock()
	fn := s.handlers[sig]
	s.mu.Unlock()
	if fn == nil {
		return
	}
	s.breaker.Set(BreakerSignalsPending)
	if err := s.pending.Add(fn); err == nil {
		s.breaker.Set(BreakerCallsPending)
	}
	// On overflow, the SIGNALS_PENDING bit alone tells the eval
	// loop to re-check; CPython behaves the same way.
}
