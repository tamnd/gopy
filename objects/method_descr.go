// MethodDescr is the type-level descriptor for built-in methods.
// CPython generates one per PyMethodDef row when the type table is
// loaded and stores it in tp_dict; gopy wires it explicitly via
// SetTypeDescr until tp_dict lands. Fetching the descriptor through
// an instance (`obj.method`) returns a BoundMethod that prepends the
// instance to the call, mirroring method_get + cfunction_call.
//
// CPython: Objects/descrobject.c:1480 PyMethodDescr_Type

package objects

import "fmt"

// MethodDescr wraps a Go function whose first positional argument is
// the receiver. The fn signature matches BuiltinFunction so the same
// closure can serve as the unbound call target after BoundMethod
// prepends self.
//
// Conv is the METH_* calling-convention tag inherited from the
// PyMethodDef row. specialize_method_descriptor reads it to pick
// CALL_METHOD_DESCRIPTOR_NOARGS / _O / _FAST / _FAST_WITH_KEYWORDS.
// Defaults to MethVarargs|MethKeywords so legacy NewMethodDescr
// callers keep matching the closure shape they always passed.
//
// CPython: Include/cpython/descrobject.h PyMethodDescrObject
type MethodDescr struct {
	Header
	name    string
	doc     string
	textSig string
	conv    MethFlag
	fn      func(args []Object, kwargs map[string]Object) (Object, error)
	owner   *Type

	// fnOrdered, when non-nil, supersedes fn for order-sensitive builtins
	// (dict(), dict.update()) whose CPython impl walks the keyword dict in
	// caller order. The vectorcall arm rebuilds an insertion-ordered *Dict
	// from the kwnames tuple and hands it here instead of flattening to the
	// unordered map. kwargs is nil when no keywords were passed.
	//
	// CPython: Objects/dictobject.c:3795 dict_update_common (merges the
	// ordered kwds dict, so dict(a=1, b=2) preserves call-site order)
	fnOrdered func(args []Object, kwargs *Dict) (Object, error)

	// kwParams, when non-nil, names every keyword the underlying impl
	// accepts. methodDescrVectorcall walks the kwnames tuple in caller
	// order and rejects the first keyword absent from this list, exactly
	// where Argument Clinic's _PyArg_UnpackKeywords scans for extraneous
	// keywords before the impl runs. kwFname is the bare function name
	// the message reports ("split", "ImportError"); kwCountLimit > 0
	// switches to the vgetargskeywords "takes at most N keyword
	// arguments" surplus check that fires ahead of the per-key scan.
	//
	// CPython: Python/getargs.c:1489 _PyArg_UnpackKeywords (per-key scan)
	// CPython: Python/getargs.c:1638 vgetargskeywords (surplus count)
	kwParams     []string
	kwFname      string
	kwCountLimit int

	// slotWrapper marks a descriptor that stands in for a CPython
	// slot_wrapper (wrapper_descriptor) rather than a PyMethodDef
	// method_descriptor. gopy collapses several tp-slot dunders
	// (object.__init__, the descr_get/set/iter/call wrappers) onto
	// MethodDescr; binding such a descriptor must yield a method-wrapper
	// (gopy's BoundMethod), not a PyCMethod, so type(object().__init__)
	// stays a "method" the way CPython reports "method-wrapper".
	//
	// CPython: Objects/descrobject.c:451 wrapperdescr_get (method-wrapper)
	slotWrapper bool
}

// AsSlotWrapper marks the descriptor as a slot_wrapper stand-in so its
// bound form is a method-wrapper rather than a PyCMethod. Returns the
// receiver for chaining at registration time.
func (d *MethodDescr) AsSlotWrapper() *MethodDescr {
	d.slotWrapper = true
	return d
}

// MethodDescrType is the type singleton for method descriptors.
//
// CPython: Objects/descrobject.c:1480 PyMethodDescr_Type
var MethodDescrType = NewType("method_descriptor", []*Type{objectType})

func init() {
	// PyMethodDescr_Type carries Py_TPFLAGS_METHOD_DESCRIPTOR and
	// Py_TPFLAGS_HAVE_VECTORCALL (tp_vectorcall_offset = d_vectorcall).
	//
	// CPython: Objects/descrobject.c:1480 PyMethodDescr_Type tp_flags
	MethodDescrType.TpFlags |= TpFlagMethodDescriptor | TpFlagHaveVectorcall
	MethodDescrType.Repr = methodDescrRepr
	MethodDescrType.Str = methodDescrRepr
	MethodDescrType.DescrGet = methodDescrGet
	MethodDescrType.Call = methodDescrCall
	MethodDescrType.Vectorcall = methodDescrVectorcall
	// Identity hash so method descriptors are hashable.
	MethodDescrType.Hash = identityHash
	// Install a faithful method_get as __get__ before the generic descriptor
	// wrapper so the METH_METHOD branch sees the raw type argument. The
	// generic wrap_descr_get collapses a non-type arg 2 to nil, which would
	// hide the "needs a type, not '...', as arg 2" error.
	//
	// CPython: Objects/descrobject.c:230 method_get
	// method_get carries the Argument Clinic signature "($self, instance,
	// owner=None, /)"; inspect strips the bound $self, leaving
	// (instance, owner=None) so inspect.signature(x.__get__) resolves.
	//
	// CPython: Objects/descrobject.c:230 method_get (descr_get clinic input)
	SetTypeDescr(MethodDescrType, "__get__", NewMethodDescr(MethodDescrType, "__get__", methodDescrDunderGet).
		WithTextSignature("($self, instance, owner=None, /)"))
	addDescriptorSlotWrappers(MethodDescrType)
	AddCallSlotWrapper(MethodDescrType)
	addDescrIntrospectionDescriptors(MethodDescrType)
	// __text_signature__: Argument Clinic signature string used by
	// inspect._signature_from_builtin. Returns None when not set.
	//
	// CPython: Objects/methodobject.c:286 meth_getsets __text_signature__
	SetTypeDescr(MethodDescrType, "__text_signature__", NewGetSetDescr("__text_signature__",
		func(o Object) (Object, error) {
			if d, ok := o.(*MethodDescr); ok && d.textSig != "" {
				return NewStr(d.textSig), nil
			}
			return None(), nil
		},
		nil,
	))
}

// Owner returns the type this method descriptor is registered on.
func (d *MethodDescr) Owner() *Type { return d.owner }

// NewMethodDescr builds a method descriptor that exposes name on
// owner. fn receives the receiver as args[0]. Conv defaults to
// MethVarargs|MethKeywords.
//
// CPython: Objects/descrobject.c:1100 PyDescr_NewMethod
func NewMethodDescr(owner *Type, name string, fn func(args []Object, kwargs map[string]Object) (Object, error)) *MethodDescr {
	d := &MethodDescr{name: name, conv: MethVarargs | MethKeywords, fn: fn, owner: owner}
	d.init(MethodDescrType)
	return d
}

// NewMethodDescrKwOrdered builds a method descriptor whose impl needs the
// keyword arguments in caller order. The vectorcall arm passes them as an
// insertion-ordered *Dict (nil when none) rather than the unordered map, so
// constructors like dict() observe the same key order CPython does.
//
// CPython: Objects/descrobject.c:1100 PyDescr_NewMethod
func NewMethodDescrKwOrdered(owner *Type, name string, fn func(args []Object, kwargs *Dict) (Object, error)) *MethodDescr {
	d := &MethodDescr{name: name, conv: MethVarargs | MethKeywords, fnOrdered: fn, owner: owner}
	d.init(MethodDescrType)
	return d
}

// NewMethodDescrConv builds a method descriptor with an explicit
// METH_* tag. Mirrors PyDescr_NewMethod when the PyMethodDef row
// declares METH_NOARGS / METH_O / METH_FASTCALL / etc.
//
// CPython: Objects/descrobject.c:1100 PyDescr_NewMethod
func NewMethodDescrConv(owner *Type, name string, conv MethFlag, fn func(args []Object, kwargs map[string]Object) (Object, error)) *MethodDescr {
	d := &MethodDescr{name: name, conv: conv, fn: fn, owner: owner}
	d.init(MethodDescrType)
	return d
}

// WithKwParams declares the keyword names the impl accepts so the
// descriptor's vectorcall arm rejects extraneous keywords in caller
// order, matching the Argument Clinic / getargs keyword scan. fname is
// the bare name printed in the TypeError ("split"); a positive
// countLimit enables the vgetargskeywords surplus check ("takes at most
// N keyword arguments") ahead of the per-key scan, as exceptions like
// ImportError do through PyArg_ParseTupleAndKeywords.
//
// CPython: Python/getargs.c:1489 _PyArg_UnpackKeywords
func (d *MethodDescr) WithKwParams(fname string, params []string, countLimit int) *MethodDescr {
	d.kwFname = fname
	d.kwParams = params
	d.kwCountLimit = countLimit
	return d
}

// Name returns the attribute name this descriptor binds to.
func (d *MethodDescr) Name() string { return d.name }

// Doc returns the documentation string for this descriptor, or "".
func (d *MethodDescr) Doc() string { return d.doc }

// WithDoc attaches a docstring to the descriptor and returns it. Mirrors
// the ml_doc field set by PyMethodDef rows in CPython.
//
// CPython: Objects/descrobject.c:642 descr_members ml_doc
func (d *MethodDescr) WithDoc(doc string) *MethodDescr {
	d.doc = doc
	return d
}

// WithTextSignature attaches an Argument Clinic text signature and returns
// the descriptor. inspect._signature_from_builtin reads __text_signature__
// to build a Signature for method descriptors that have no code object.
//
// CPython: Objects/methodobject.c:286 meth_getsets __text_signature__
func (d *MethodDescr) WithTextSignature(sig string) *MethodDescr {
	d.textSig = sig
	return d
}

// TextSig returns the text signature string, or "" if none is set.
func (d *MethodDescr) TextSig() string { return d.textSig }

// Conv returns the METH_* calling-convention tag the specializer
// uses to pick the descriptor-specific CALL fast arm.
func (d *MethodDescr) Conv() MethFlag { return d.conv }

func methodDescrRepr(o Object) (string, error) {
	d := o.(*MethodDescr)
	return "<method '" + d.name + "' of '" + d.owner.Name + "' objects>", nil
}

// methodDescrGet binds the descriptor to an instance. Class-level
// access (owner==nil) returns the descriptor unchanged so attribute
// lookup on the type itself sees the descriptor object.
//
// CPython: Objects/descrobject.c:230 method_get
func methodDescrGet(descr Object, owner Object, _ *Type) (Object, error) {
	if owner == nil {
		return descr, nil
	}
	// descr_check rejects binding to an object whose type is not a
	// subtype of the descriptor's owner, e.g. set.add.__get__(0).
	//
	// CPython: Objects/descrobject.c:147 descr_check
	d := descr.(*MethodDescr)
	if !IsSubtype(owner.Type(), d.owner) {
		return nil, fmt.Errorf("TypeError: descriptor '%s' for '%s' objects doesn't apply to a '%s' object", d.name, d.owner.Name, owner.Type().Name)
	}
	// A slot_wrapper stand-in binds to a method-wrapper (BoundMethod);
	// only a genuine method_descriptor mints a PyCMethod.
	if d.slotWrapper {
		return NewBoundMethod(descr, owner), nil
	}
	return newBoundCMethod(d, owner), nil
}

// newBoundCMethod binds a method descriptor to an instance the way
// method_get does: PyCMethod_New produces a builtin_function_or_method
// whose m_self is the receiver, not a separate bound-method object. So
// `[].append` is a builtin_function_or_method, type([].append) is that
// type, and repr reads "<built-in method append of list object at ...>".
// The Fn closure prepends the receiver and runs the descriptor's own
// call machinery (subtype + arity checks, then ml_meth), mirroring
// cfunction_call dispatching ml_meth(m_self, args).
//
// CPython: Objects/descrobject.c:230 method_get (PyCMethod_New)
// CPython: Objects/methodobject.c:544 cfunction_call
func newBoundCMethod(d *MethodDescr, self Object) *BuiltinFunction {
	// PyCMethod_New keeps its own reference to m_self; without the Incref
	// a bound method off a temporary ([].append, sorted(x).pop) sees its
	// receiver decref'd to zero by the LOAD_ATTR owner-release before the
	// call runs.
	//
	// CPython: Objects/classobject.c PyCMethod_New (Py_XNewRef(self))
	Incref(self)
	bf := &BuiltinFunction{
		Name:     d.name,
		Conv:     d.conv,
		Self:     self,
		ownsSelf: true,
		Doc:      d.doc,
		// boundDescr drives the order-preserving vectorcall path
		// (builtinFunctionVectorcall forwards kwnames straight to
		// methodDescrVectorcall). The Fn closure is the slow tp_call
		// fallback: a Go map has already lost keyword order by the time it
		// arrives, but CALL_KW always routes through vectorcall, so the
		// order-sensitive callers never reach here.
		boundDescr: d,
		Fn: func(args []Object, kwargs map[string]Object) (Object, error) {
			full := make([]Object, 0, len(args)+1)
			full = append(full, self)
			full = append(full, args...)
			return methodDescrCall(d, full, kwargs)
		},
	}
	bf.TextSignature = d.textSig
	bf.init(BuiltinFunctionType)
	return bf
}

// methodDescrDunderGet is the Python-level __get__(self, obj, type) for
// method descriptors. Unlike the generic descriptor wrapper it keeps the
// raw type argument so the METH_METHOD branch can reject a non-type arg 2
// with the exact message method_get produces. descr_check in 3.14 only
// short-circuits the obj==NULL (class) access; it no longer type-checks the
// instance, so binding to an unrelated instance is deferred to call time.
//
// CPython: Objects/descrobject.c:230 method_get
func methodDescrDunderGet(args []Object, _ map[string]Object) (Object, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("TypeError: expected 1 or 2 arguments, got %d", len(args)-1)
	}
	d, ok := args[0].(*MethodDescr)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor '__get__' requires a 'method_descriptor' object")
	}
	obj := args[1]
	if IsNone(obj) {
		obj = nil
	}
	var typ Object
	if len(args) == 3 {
		typ = args[2]
	}
	if obj == nil && (typ == nil || IsNone(typ)) {
		return nil, fmt.Errorf("TypeError: __get__(None, None) is invalid")
	}
	// descr_check: class access returns the descriptor itself; otherwise the
	// instance must be an instance of the descriptor's owning type.
	if obj == nil {
		return d, nil
	}
	if !IsSubtype(obj.Type(), d.owner) {
		return nil, fmt.Errorf("TypeError: descriptor '%s' for '%s' objects doesn't apply to a '%s' object", d.name, d.owner.Name, obj.Type().Name)
	}
	if d.conv&MethMethod != 0 {
		if typ != nil {
			if _, isType := typ.(*Type); !isType {
				return nil, fmt.Errorf("TypeError: descriptor '%s' needs a type, not '%s', as arg 2", d.name, typ.Type().Name)
			}
		}
	}
	if d.slotWrapper {
		return NewBoundMethod(d, obj), nil
	}
	return newBoundCMethod(d, obj), nil
}

// methodDescrCall is the unbound call: the first positional argument
// is the receiver, mirroring how a class-level method descriptor is
// dispatched in CPython.
//
// CPython: Objects/descrobject.c:296 method_call
func methodDescrCall(o Object, args []Object, kwargs map[string]Object) (Object, error) {
	d := o.(*MethodDescr)
	if len(args) == 0 {
		return nil, fmt.Errorf("TypeError: unbound method %s.%s() needs an argument", d.owner.Name, d.name)
	}
	if !IsSubtype(args[0].Type(), d.owner) {
		return nil, fmt.Errorf("TypeError: descriptor '%s' for '%s' objects doesn't apply to a '%s' object", d.name, d.owner.Name, args[0].Type().Name)
	}
	if err := methodDescrCheckArity(d, len(args)-1, len(kwargs)); err != nil {
		return nil, err
	}
	if d.fnOrdered != nil {
		// The legacy tp_call path already flattened keywords to an unordered
		// map upstream, so order cannot be recovered here; rebuild a *Dict
		// best-effort. Callers needing order use the vectorcall arm.
		var kwd *Dict
		if len(kwargs) > 0 {
			kwd = NewDict()
			for k, v := range kwargs {
				if err := kwd.SetItem(NewStr(k), v); err != nil {
					return nil, err
				}
			}
		}
		return d.fnOrdered(args, kwd)
	}
	return d.fn(args, kwargs)
}

// methodDescrVectorcall converts the (args, nargsf, kwnames) shape
// into the (positional, kwargs) closure the descriptor stores.
// Mirrors method_vectorcall + cfunction_vectorcall_FASTCALL_KEYWORDS.
//
// CPython: Objects/descrobject.c:330 method_vectorcall_FASTCALL_KEYWORDS
func methodDescrVectorcall(callable Object, args []Object, nargsf uint, kwnames *Tuple) (Object, error) {
	d := callable.(*MethodDescr)
	nargs := VectorcallNargs(nargsf)
	if nargs == 0 {
		return nil, fmt.Errorf("TypeError: unbound method %s.%s() needs an argument", d.owner.Name, d.name)
	}
	if !IsSubtype(args[0].Type(), d.owner) {
		return nil, fmt.Errorf("TypeError: descriptor '%s' for '%s' objects doesn't apply to a '%s' object", d.name, d.owner.Name, args[0].Type().Name)
	}
	nkw := 0
	if kwnames != nil {
		nkw = kwnames.Len()
	}
	if err := methodDescrCheckArity(d, nargs-1, nkw); err != nil {
		return nil, err
	}
	pos := make([]Object, nargs)
	copy(pos, args[:nargs])
	// Order-sensitive impls take the keywords as an insertion-ordered *Dict
	// built straight from the kwnames tuple, bypassing the unordered map.
	if d.fnOrdered != nil {
		var kwd *Dict
		if nkw > 0 {
			kwd = NewDict()
			order := make([]string, 0, nkw)
			for i := 0; i < nkw; i++ {
				name, err := Str(kwnames.Item(i))
				if err != nil {
					return nil, err
				}
				if err := kwd.SetItem(NewStr(name), args[nargs+i]); err != nil {
					return nil, err
				}
				order = append(order, name)
			}
			if err := d.checkKwParams(order); err != nil {
				return nil, err
			}
		}
		return d.fnOrdered(pos, kwd)
	}
	var kwargs map[string]Object
	if nkw > 0 {
		kwargs = make(map[string]Object, nkw)
		order := make([]string, 0, nkw)
		for i := 0; i < nkw; i++ {
			name, err := Str(kwnames.Item(i))
			if err != nil {
				return nil, err
			}
			kwargs[name] = args[nargs+i]
			order = append(order, name)
		}
		if err := d.checkKwParams(order); err != nil {
			return nil, err
		}
	}
	return d.fn(pos, kwargs)
}

// checkKwParams runs the extraneous-keyword scan declared by
// WithKwParams over the keyword names in caller order. The surplus
// count check fires first when kwCountLimit is set (vgetargskeywords),
// then the first keyword missing from kwParams raises the
// unexpected-keyword TypeError with the suggestion tail.
//
// CPython: Python/getargs.c:1489 _PyArg_UnpackKeywords
func (d *MethodDescr) checkKwParams(order []string) error {
	if d.kwParams == nil {
		return nil
	}
	if d.kwCountLimit > 0 && len(order) > d.kwCountLimit {
		return CheckKeywordCount(d.kwFname, 0, len(order), d.kwCountLimit)
	}
	for _, k := range order {
		known := false
		for _, p := range d.kwParams {
			if k == p {
				known = true
				break
			}
		}
		if !known {
			return UnexpectedKeywordError(d.kwFname, k, d.kwParams)
		}
	}
	return nil
}

// methodDescrCheckArity enforces the METH_NOARGS / METH_O calling
// conventions: NOARGS rejects any extra positional arguments and
// any keyword arguments, while METH_O requires exactly one
// positional argument (and zero keyword arguments). nargs counts
// only user-supplied positional arguments (receiver already
// excluded). Mirrors method_check_args + method_vectorcall_NOARGS /
// method_vectorcall_O.
//
// CPython: Objects/descrobject.c:330 method_vectorcall_NOARGS
// CPython: Objects/descrobject.c:360 method_vectorcall_O
func methodDescrCheckArity(d *MethodDescr, nargs, nkw int) error {
	noKeyword := func() error {
		funcstr, err := FunctionStr(d)
		if err != nil {
			return err
		}
		return fmt.Errorf("TypeError: %s takes no keyword arguments", funcstr)
	}
	switch d.conv & (MethVarargs | MethKeywords | MethNoArgs | MethO | MethFastcall | MethMethod) {
	case MethVarargs:
		// METH_VARARGS without METH_KEYWORDS: method_vectorcall_VARARGS
		// routes through method_check_args, which rejects any keyword
		// arguments. The METH_KEYWORDS variant skips this check.
		//
		// CPython: Objects/descrobject.c:266 method_check_args
		if nkw > 0 {
			return noKeyword()
		}
	case MethNoArgs:
		if nkw > 0 {
			return noKeyword()
		}
		if nargs != 0 {
			funcstr, err := FunctionStr(d)
			if err != nil {
				return err
			}
			return fmt.Errorf("TypeError: %s takes no arguments (%d given)", funcstr, nargs)
		}
	case MethO:
		if nkw > 0 {
			return noKeyword()
		}
		if nargs != 1 {
			funcstr, err := FunctionStr(d)
			if err != nil {
				return err
			}
			return fmt.Errorf("TypeError: %s takes exactly one argument (%d given)", funcstr, nargs)
		}
	case MethFastcall:
		if nkw > 0 {
			return noKeyword()
		}
	}
	return nil
}
