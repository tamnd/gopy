// Pre-allocated stackrefs for the singleton objects the eval loop
// references most often: None, True, False. Wrapping them once at
// init lets LOAD_CONST None / True / False skip the allocation step.
//
// CPython: Include/internal/pycore_stackref.h PyStackRef_None / True / False

package stackref

import "github.com/tamnd/gopy/objects"

// None wraps the None singleton.
//
// CPython: Include/internal/pycore_stackref.h PyStackRef_None
var None = Ref{o: objects.None()}

// True wraps the True singleton.
//
// CPython: Include/internal/pycore_stackref.h PyStackRef_True
var True = Ref{o: objects.True()}

// False wraps the False singleton.
//
// CPython: Include/internal/pycore_stackref.h PyStackRef_False
var False = Ref{o: objects.False()}
