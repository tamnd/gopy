// Package _uuid is the gopy port of CPython's _uuid C module
// (Modules/_uuidmodule.c). It backs Lib/uuid.py with thread-safe UUID
// byte generation via crypto/rand and exposes the safety-level constants.
//
// CPython: Modules/_uuidmodule.c

package _uuid

import (
	"crypto/rand"
	"fmt"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_uuid", buildModule)
}

// Safety-level constants mirror the uuid_rc_t / int values that CPython
// returns as the second element of the generate_time_safe tuple.
//
// CPython: Modules/_uuidmodule.c:52 _uuid_generate_time_safe_impl
const (
	uuidSafetyUnknown        = 0
	uuidSafeMultiprocessing  = 1
	uuidSafeThread           = 2
)

// generateTimeSafe returns a 2-tuple (bytes_16, safety_int) where bytes_16
// is 16 cryptographically random bytes and safety_int is UUID_SAFE_THREAD
// (2) because crypto/rand is goroutine-safe and process-safe.
//
// CPython: Modules/_uuidmodule.c:52 _uuid_generate_time_safe_impl
func generateTimeSafe(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return nil, fmt.Errorf("OSError: crypto/rand failed: %w", err)
	}
	b := objects.NewBytes(buf[:])
	safety := objects.NewInt(uuidSafeThread)
	return objects.NewTuple([]objects.Object{b, safety}), nil
}

// buildModule materializes the _uuid module dict.
//
// CPython: Modules/_uuidmodule.c uuid_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_uuid")
	d := m.Dict()

	entries := []struct {
		name string
		val  objects.Object
	}{
		{"generate_time_safe", objects.NewBuiltinFunction("generate_time_safe", generateTimeSafe)},
		{"UUID_SAFETY_UNKNOWN", objects.NewInt(uuidSafetyUnknown)},
		{"UUID_SAFE_MULTIPROCESSING", objects.NewInt(uuidSafeMultiprocessing)},
		{"UUID_SAFE_THREAD", objects.NewInt(uuidSafeThread)},
		// has_uuid_generate_time_safe: always 1 because crypto/rand is always
		// available on all Go targets.
		{"has_uuid_generate_time_safe", objects.NewInt(1)},
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}
