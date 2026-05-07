// Disable / Missing sentinels. CPython exposes both as bare PyObject
// instances of object: tools compare returned values for identity and
// the runtime treats them as opaque markers.
//
// CPython: Python/instrumentation.c:65 _PyInstrumentation_DISABLE

package monitor

import "github.com/tamnd/gopy/objects"

// sentinelObject is the per-singleton struct backing Disable and
// Missing. Both are bare object instances with no payload; identity
// comparison is the only meaningful operation.
type sentinelObject struct {
	objects.Header
}

var sentinelType = objects.NewType("InstrumentationSentinel", []*objects.Type{objects.ObjectType()})

func newSentinel() objects.Object {
	o := &sentinelObject{}
	o.Init(sentinelType)
	return o
}

// Disable is the singleton callbacks return to ask the runtime to
// stop firing this event for this code object. The fire-event entry
// points compare returned values for identity.
//
// CPython: Python/instrumentation.c:70 _PyInstrumentation_DISABLE
var Disable = newSentinel()

// Missing is the singleton handed to a callback when the requested
// value is not available (for example, a return value during PY_THROW).
//
// CPython: Python/instrumentation.c:72 _PyInstrumentation_MISSING
var Missing = newSentinel()

// IsDisable reports whether o is the Disable sentinel.
func IsDisable(o objects.Object) bool { return o == Disable }

// IsMissing reports whether o is the Missing sentinel.
func IsMissing(o objects.Object) bool { return o == Missing }
