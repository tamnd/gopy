// ContextVar type and its Get / Set / Reset operations. Ports the
// PyContextVar half of cpython/Python/context.c, including the
// per-ContextVar tsid+version cache that short-circuits Get on
// repeated lookups against the same thread + context.
//
// CPython: Python/context.c:773 ContextVar

package contextvar

import (
	"sync"
	"unsafe"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/hash"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// ContextVar is the Python-level context variable. The cache fields
// match CPython's var_cached / var_cached_tsid / var_cached_tsver.
// hasDefault distinguishes "no default" from "default=None"; CPython
// uses NULL on the C side for the same purpose. cacheMu guards the
// cache slot: CPython relies on the GIL to serialize writes, but gopy
// runs goroutines truly in parallel so we need an explicit lock.
//
// CPython: Python/context.c:861 contextvar_new
type ContextVar struct {
	objects.Header
	name        string
	defaultVal  objects.Object
	hasDefault  bool
	hash        int64
	cacheMu     sync.Mutex
	cachedTSID  uint64
	cachedVer   uint64
	cachedVal   objects.Object
	cachedValid bool
}

// NewContextVar creates a ContextVar named name with an optional
// default. Pass hasDefault=false to create a variable with no
// default; in that case Get raises LookupError when the variable is
// unset.
//
// CPython: Python/context.c:861 contextvar_new / 261 PyContextVar_New
func NewContextVar(name string, defaultVal objects.Object, hasDefault bool) *ContextVar {
	cv := &ContextVar{
		name:       name,
		defaultVal: defaultVal,
		hasDefault: hasDefault,
	}
	cv.Init(ContextVarType)
	cv.hash = generateHash(unsafe.Pointer(cv), name)
	return cv
}

// Name returns the variable's name as a Go string.
func (cv *ContextVar) Name() string { return cv.name }

// Default returns the (defaultVal, hasDefault) pair the variable was
// constructed with.
func (cv *ContextVar) Default() (objects.Object, bool) {
	return cv.defaultVal, cv.hasDefault
}

// generateHash mirrors contextvar_generate_hash: XOR the name's hash
// with the pointer's hash, then remap -1 to -2.
//
// CPython: Python/context.c:833 contextvar_generate_hash
func generateHash(addr unsafe.Pointer, name string) int64 {
	nameHash, err := objects.Hash(objects.NewStr(name))
	if err != nil {
		// strStub.Hash never errors; the err branch is here for
		// shape parity with the C version's PyObject_Hash failure.
		nameHash = 0
	}
	res := hash.Pointer(addr) ^ nameHash
	if res == -1 {
		return -2
	}
	return res
}

// Get returns the value bound to cv in the thread's current
// context. If the variable is not bound, returns the default the
// ContextVar was constructed with. Raises LookupError when there is
// no default. The (cachedTSID, cachedVer) cache short-circuits the
// HAMT walk on repeat lookups.
//
// CPython: Python/context.c:274 PyContextVar_Get
func (cv *ContextVar) Get(ts *state.Thread) (objects.Object, error) {
	cur, _ := ts.Context().(*Context)
	if cur == nil {
		return cv.notFound(ts)
	}

	if val, ok := cv.cacheLookup(ts); ok {
		return val, nil
	}

	val, found, err := cur.vars.Find(cv)
	if err != nil {
		return nil, err
	}
	if found {
		cv.cacheStore(ts, val)
		return val, nil
	}
	return cv.notFound(ts)
}

// GetWithDefault matches PyContextVar_Get's `def != NULL` branch:
// the supplied default overrides the ContextVar's stored default and
// suppresses the LookupError path.
//
// CPython: Python/context.c:325 PyContextVar_Get (def != NULL branch)
func (cv *ContextVar) GetWithDefault(ts *state.Thread, def objects.Object) (objects.Object, error) {
	cur, _ := ts.Context().(*Context)
	if cur != nil {
		if val, ok := cv.cacheLookup(ts); ok {
			return val, nil
		}
		val, found, err := cur.vars.Find(cv)
		if err != nil {
			return nil, err
		}
		if found {
			cv.cacheStore(ts, val)
			return val, nil
		}
	}
	return def, nil
}

// notFound implements the "not found" branch of PyContextVar_Get
// when no override default is supplied: return the constructor
// default if there is one, else raise LookupError(cv).
//
// CPython: Python/context.c:315 PyContextVar_Get (not_found label)
func (cv *ContextVar) notFound(ts *state.Thread) (objects.Object, error) {
	if cv.hasDefault {
		return cv.defaultVal, nil
	}
	errors.SetString(ts, errors.PyExc_LookupError, cv.name)
	return nil, errLookup
}

// Set binds cv to val in the thread's current context (allocating an
// empty context first if the thread has none) and returns a Token
// that Reset can use to restore the prior binding.
//
// CPython: Python/context.c:340 PyContextVar_Set / 776 contextvar_set
func (cv *ContextVar) Set(ts *state.Thread, val objects.Object) (*Token, error) {
	ctx := contextGet(ts)

	oldVal, found, err := ctx.vars.Find(cv)
	if err != nil {
		return nil, err
	}

	tok := newToken(ctx, cv, oldVal, found)

	newVars, err := ctx.vars.Assoc(cv, val)
	if err != nil {
		return nil, err
	}
	ctx.vars = newVars

	cv.cacheStore(ts, val)
	return tok, nil
}

// Reset undoes the cv.Set call that produced tok. The token must be
// from the same context and must not have been used before.
//
// CPython: Python/context.c:371 PyContextVar_Reset
func (cv *ContextVar) Reset(ts *state.Thread, tok *Token) error {
	if tok.used {
		errors.SetString(ts, errors.PyExc_RuntimeError,
			tokenRepr(tok)+" has already been used once")
		return errTokenReused
	}
	if cv != tok.cv {
		errors.SetString(ts, errors.PyExc_ValueError,
			tokenRepr(tok)+" was created by a different ContextVar")
		return errTokenWrongVar
	}
	ctx := contextGet(ts)
	if ctx != tok.ctx {
		errors.SetString(ts, errors.PyExc_ValueError,
			tokenRepr(tok)+" was created in a different Context")
		return errTokenWrongCtx
	}

	tok.used = true
	cv.cacheInvalidate()

	if !tok.hadOld {
		newVars, removed, err := ctx.vars.Without(cv)
		if err != nil {
			return err
		}
		if !removed {
			errors.SetString(ts, errors.PyExc_LookupError, cv.name)
			return errLookup
		}
		ctx.vars = newVars
		return nil
	}
	newVars, err := ctx.vars.Assoc(cv, tok.oldVal)
	if err != nil {
		return err
	}
	ctx.vars = newVars
	return nil
}

// cacheLookup returns the cached value if it matches ts's id and
// context version. Holds cacheMu so concurrent Set/Reset calls from
// other threads cannot tear the slot.
//
// CPython: Python/context.c:298 PyContextVar_Get (cache hit branch)
func (cv *ContextVar) cacheLookup(ts *state.Thread) (objects.Object, bool) {
	cv.cacheMu.Lock()
	defer cv.cacheMu.Unlock()
	if cv.cachedValid &&
		cv.cachedTSID == ts.ID() &&
		cv.cachedVer == ts.ContextVersion() {
		return cv.cachedVal, true
	}
	return nil, false
}

// cacheStore overwrites the cache slot.
//
// CPython: Python/context.c:309 PyContextVar_Get (cache fill branch)
func (cv *ContextVar) cacheStore(ts *state.Thread, val objects.Object) {
	cv.cacheMu.Lock()
	defer cv.cacheMu.Unlock()
	cv.cachedVal = val
	cv.cachedTSID = ts.ID()
	cv.cachedVer = ts.ContextVersion()
	cv.cachedValid = true
}

// cacheInvalidate marks the cache slot stale.
//
// CPython: Python/context.c:393 PyContextVar_Reset (cache invalidate)
func (cv *ContextVar) cacheInvalidate() {
	cv.cacheMu.Lock()
	defer cv.cacheMu.Unlock()
	cv.cachedValid = false
}

var (
	errLookup        = errSentinel("contextvar: lookup error")
	errTokenReused   = errSentinel("contextvar: token already used")
	errTokenWrongVar = errSentinel("contextvar: token from different ContextVar")
	errTokenWrongCtx = errSentinel("contextvar: token from different Context")
)
