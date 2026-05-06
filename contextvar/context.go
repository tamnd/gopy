// Context type and operations. Ports the PyContext half of
// cpython/Python/context.c: tp_new, copy, run, enter / exit, the
// thread-state context_get fallback, and richcompare via _PyHamt_Eq.
//
// CPython: Python/context.c:408 PyContext

package contextvar

import (
	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/hamt"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/state"
)

// Context is the immutable PEP 567 mapping from ContextVar to value.
// vars is the persistent HAMT; prev links the pre-Run context the
// thread was running in, so Exit can restore it. entered guards
// against double-entry, matching ctx_entered in CPython.
//
// CPython: Python/context.c:408 PyContext
type Context struct {
	objects.Header
	vars    *hamt.Hamt
	prev    *Context
	entered bool
}

// NewContext returns a fresh empty Context.
//
// CPython: Python/context.c:439 context_new_empty
func NewContext() *Context {
	c := &Context{vars: hamt.New()}
	c.Init(ContextType)
	return c
}

// newContextFromVars wraps vars in a freshly-allocated Context. Used
// by Copy and CopyCurrent.
//
// CPython: Python/context.c:458 context_new_from_vars
func newContextFromVars(vars *hamt.Hamt) *Context {
	c := &Context{vars: vars}
	c.Init(ContextType)
	return c
}

// Copy returns a shallow snapshot of c. The new Context shares the
// HAMT with c (the HAMT is immutable, so sharing is safe) but starts
// with prev=nil and entered=false.
//
// CPython: Python/context.c:84 PyContext_Copy
func (c *Context) Copy() *Context {
	return newContextFromVars(c.vars)
}

// CopyCurrent snapshots the thread's current context. If the thread
// has no context yet, a fresh empty one is installed first, matching
// context_get's lazy initialisation.
//
// CPython: Python/context.c:91 PyContext_CopyCurrent
func CopyCurrent(ts *state.Thread) *Context {
	cur := contextGet(ts)
	return newContextFromVars(cur.vars)
}

// contextGet returns the thread's current context, allocating one on
// demand. Matches CPython's context_get which lazily seeds
// ts->context with an empty Context the first time it is observed
// nil.
//
// CPython: Python/context.c:473 context_get
func contextGet(ts *state.Thread) *Context {
	if cur, ok := ts.Context().(*Context); ok && cur != nil {
		return cur
	}
	c := NewContext()
	ts.SetContext(c)
	return c
}

// Enter pushes c onto the thread's context stack. Returns an error
// (RuntimeError) if c is already entered.
//
// CPython: Python/context.c:194 _PyContext_Enter
func (c *Context) Enter(ts *state.Thread) error {
	if c.entered {
		errors.SetString(ts, errors.PyExc_RuntimeError,
			"cannot enter context: "+contextRepr(c)+" is already entered")
		return errAlreadyEntered
	}
	prev, _ := ts.Context().(*Context)
	c.prev = prev
	c.entered = true
	ts.SetContext(c)
	return nil
}

// Exit pops c from the thread's context stack. Returns an error if c
// has not been entered or if the thread state currently references a
// different context.
//
// CPython: Python/context.c:223 _PyContext_Exit
func (c *Context) Exit(ts *state.Thread) error {
	if !c.entered {
		errors.SetString(ts, errors.PyExc_RuntimeError,
			"cannot exit context: "+contextRepr(c)+" has not been entered")
		return errNotEntered
	}
	cur, _ := ts.Context().(*Context)
	if cur != c {
		errors.SetString(ts, errors.PyExc_RuntimeError,
			"cannot exit context: thread state references a different context object")
		return errExitMismatch
	}
	ts.SetContext(c.prev)
	c.prev = nil
	c.entered = false
	return nil
}

// Run enters c, calls fn(args...), and exits c, even if fn raises.
// Mirrors PyContext.run from the Python side.
//
// CPython: Python/context.c:706 context_run
func (c *Context) Run(ts *state.Thread, fn func() (objects.Object, error)) (objects.Object, error) {
	if err := c.Enter(ts); err != nil {
		return nil, err
	}
	out, callErr := fn()
	exitErr := c.Exit(ts)
	if callErr != nil {
		return nil, callErr
	}
	if exitErr != nil {
		return nil, exitErr
	}
	return out, nil
}

// Get reads cv from c without going through any cache. found==false
// means the variable is not bound in this context.
//
// CPython: Python/context.c:584 context_tp_subscript (lookup half)
func (c *Context) Get(cv *ContextVar) (objects.Object, bool, error) {
	return c.vars.Find(cv)
}

// Len reports the number of bound variables.
//
// CPython: Python/context.c:577 context_tp_len
func (c *Context) Len() int { return c.vars.Len() }

// Eq reports whether two contexts hold the same bindings. Defers to
// hamt.Eq.
//
// CPython: Python/context.c:550 context_tp_richcompare
func (c *Context) Eq(other *Context) (bool, error) {
	return c.vars.Eq(other.vars)
}

// contextRepr returns the "<Context object at %p>" string CPython's
// %R formatter would emit. ctx is enough; the address tag is what
// the error messages need to disambiguate identical-shape contexts.
func contextRepr(c *Context) string {
	r, _ := objects.Repr(c)
	return r
}

// Sentinels returned by Enter / Exit alongside the SetException
// call. The VM-facing surface uses errors.Occurred to recover the
// real Python exception; these Go values just signal "an exception
// is set" so the caller can return early. Pinned as package-level
// vars so callers can compare with errors.Is.
var (
	errAlreadyEntered = errSentinel("contextvar: already entered")
	errNotEntered     = errSentinel("contextvar: not entered")
	errExitMismatch   = errSentinel("contextvar: exit mismatch")
)

type errSentinel string

func (e errSentinel) Error() string { return string(e) }
