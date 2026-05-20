// Tier-2 trace mirror. Emits the same "T2 <event> k=v ..." lines the
// CPython side prints when its build is configured with
// -DGOPY_TIER2_TRACE=1. Diffing the two streams gives a line-by-line
// oracle for the body translator (spec 1714 phase 8): the projector,
// dispatcher, and exit paths log identical events under identical
// inputs.
//
// Activation:
//   GOPY_TIER2_TRACE_FILE=path -> append-mode file (line-buffered)
//   GOPY_TIER2_TRACE=1         -> stderr
//   (neither set)              -> no-op fast path
//
// CPython peer: Include/internal/pycore_gopy_tier2_trace.h
// CPython peer: Python/gopy_tier2_trace.c

package optimizer

import (
	"fmt"
	"io"
	"os"
	"sync"
)

var (
	tier2LogOnce sync.Once
	tier2LogSink io.Writer
)

func resolveTier2LogSink() {
	if path := os.Getenv("GOPY_TIER2_TRACE_FILE"); path != "" {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err == nil {
			tier2LogSink = f
			return
		}
	}
	if os.Getenv("GOPY_TIER2_TRACE") != "" {
		tier2LogSink = os.Stderr
		return
	}
	tier2LogSink = nil
}

func tier2LogEnabled() bool {
	tier2LogOnce.Do(resolveTier2LogSink)
	return tier2LogSink != nil
}

func tier2Log(event, format string, args ...any) {
	if !tier2LogEnabled() {
		return
	}
	fmt.Fprintf(tier2LogSink, "T2 %s ", event)
	fmt.Fprintf(tier2LogSink, format, args...)
	fmt.Fprintln(tier2LogSink)
}

func tier2UopName(op uint16) string {
	if name, ok := UopNames[op]; ok && name != "" {
		return name
	}
	return "?"
}
