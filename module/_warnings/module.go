// Package _warnings is the gopy port of CPython's _warnings C
// accelerator. It backs `import _warnings` plus the C-level
// PyErr_WarnEx / PyErr_WarnFormat / PyErr_WarnExplicit family that
// the rest of the runtime calls. The vendored Lib/warnings.py is the
// Python facade; this module supplies the fast filter / lock /
// registry plumbing underneath it.
//
// CPython: Python/_warnings.c
package _warnings

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// MODULE_NAME mirrors the C constant.
//
// CPython: Python/_warnings.c:17 MODULE_NAME
const moduleName = "_warnings"

// state mirrors WarningsState (declared in pycore_warnings.h) plus the
// per-interp lock. gopy currently exposes a single interpreter, so
// the state is package-level. The lock is a sync.Mutex rather than a
// recursive mutex because gopy builtins do not re-enter the lock.
//
// CPython: Include/internal/pycore_warnings.h _warnings_runtime_state
type warningsState struct {
	mu             sync.Mutex
	filters        *objects.List
	onceRegistry   *objects.Dict
	defaultAction  objects.Object // a *objects.Unicode str singleton
	context        objects.Object // PEP 567 ContextVar; gopy stores the var instance.
	filtersVersion int64
	lockHeld       bool
}

var state = func() *warningsState {
	s := &warningsState{}
	initState(s)
	return s
}()

func init() {
	_ = imp.AppendInittab(moduleName, buildModule)
}

// buildModule materializes the _warnings module dict.
//
// CPython: Python/_warnings.c:1637 warnings_module_exec
func buildModule() (*objects.Module, error) {
	m := objects.NewModule(moduleName)
	d := m.Dict()
	entries := []struct {
		name string
		val  objects.Object
	}{
		{"warn", objects.NewBuiltinFunction("warn", warnBuiltin)},
		{"warn_explicit", objects.NewBuiltinFunction("warn_explicit", warnExplicitBuiltin)},
		{"_filters_mutated_lock_held", objects.NewBuiltinFunction("_filters_mutated_lock_held", filtersMutatedLockHeldBuiltin)},
		{"_acquire_lock", objects.NewBuiltinFunction("_acquire_lock", acquireLockBuiltin)},
		{"_release_lock", objects.NewBuiltinFunction("_release_lock", releaseLockBuiltin)},
		{"filters", state.filters},
		{"_onceregistry", state.onceRegistry},
		{"_defaultaction", state.defaultAction},
	}
	if state.context != nil {
		entries = append(entries, struct {
			name string
			val  objects.Object
		}{"_warnings_context", state.context})
	}
	for _, e := range entries {
		if err := d.SetItem(objects.NewStr(e.name), e.val); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// createFilter constructs one (action, message, category, modname, lineno)
// tuple. Mirrors create_filter.
//
// CPython: Python/_warnings.c:76 create_filter
func createFilter(category *objects.Type, action string, modname string) *objects.Tuple {
	modObj := objects.None()
	if modname != "" {
		modObj = objects.NewStr(modname)
	}
	return objects.NewTuple([]objects.Object{
		objects.NewStr(action),
		objects.None(),
		category,
		modObj,
		objects.NewInt(0),
	})
}

// initFilters builds the default 5-row filter list. Mirrors
// init_filters; gopy is always a release build, never Py_DEBUG.
//
// CPython: Python/_warnings.c:99 init_filters
func initFilters() *objects.List {
	items := []objects.Object{
		createFilter(errors.PyExc_DeprecationWarning, "default", "__main__"),
		createFilter(errors.PyExc_DeprecationWarning, "ignore", ""),
		createFilter(errors.PyExc_PendingDeprecationWarning, "ignore", ""),
		createFilter(errors.PyExc_ImportWarning, "ignore", ""),
		createFilter(errors.PyExc_ResourceWarning, "ignore", ""),
	}
	return objects.NewList(items)
}

// initState is the gopy analog of _PyWarnings_InitState: stamp the
// per-interp warnings state with its empty defaults.
//
// CPython: Python/_warnings.c:134 _PyWarnings_InitState
func initState(st *warningsState) {
	if st.filters == nil {
		st.filters = initFilters()
	}
	if st.onceRegistry == nil {
		st.onceRegistry = objects.NewDict()
	}
	if st.defaultAction == nil {
		st.defaultAction = objects.NewStr("default")
	}
	// PEP 567 ContextVar is not exposed from builtin context yet, so
	// the slot is left nil. Lib/warnings.py treats a missing
	// _warnings_context as "no context filters", which matches the C
	// path through get_warnings_context returning None.
	st.filtersVersion = 0
}

// -----------------------------------------------------------------------------
// Lock helpers.
// -----------------------------------------------------------------------------

// warningsLock acquires the warnings state lock. Mirrors warnings_lock.
//
// CPython: Python/_warnings.c:245 warnings_lock
func warningsLock(st *warningsState) {
	st.mu.Lock()
	st.lockHeld = true
}

// warningsUnlock releases the warnings state lock. Mirrors
// warnings_unlock. Returns false when the caller did not hold it.
//
// CPython: Python/_warnings.c:253 warnings_unlock
func warningsUnlock(st *warningsState) bool {
	if !st.lockHeld {
		return false
	}
	st.lockHeld = false
	st.mu.Unlock()
	return true
}

// warningsLockHeld reports whether the warnings lock is currently
// owned. Mirrors warnings_lock_held.
//
// CPython: Python/_warnings.c:261 warnings_lock_held
func warningsLockHeld(st *warningsState) bool {
	return st.lockHeld
}

// acquireLockBuiltin is the _acquire_lock method.
//
// CPython: Python/_warnings.c:334 warnings_acquire_lock_impl
func acquireLockBuiltin(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	warningsLock(state)
	return objects.None(), nil
}

// releaseLockBuiltin is the _release_lock method.
//
// CPython: Python/_warnings.c:351 warnings_release_lock_impl
func releaseLockBuiltin(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if !warningsUnlock(state) {
		return nil, fmt.Errorf("RuntimeError: cannot release un-acquired lock")
	}
	return objects.None(), nil
}

// filtersMutatedLockHeldBuiltin is _filters_mutated_lock_held: bumps
// the version counter that already_warned tracks per registry.
//
// CPython: Python/_warnings.c:1335 warnings_filters_mutated_lock_held_impl
func filtersMutatedLockHeldBuiltin(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if !warningsLockHeld(state) {
		return nil, fmt.Errorf("RuntimeError: warnings lock is not held")
	}
	state.filtersVersion++
	return objects.None(), nil
}

// -----------------------------------------------------------------------------
// Filter lookup.
// -----------------------------------------------------------------------------

// checkMatched checks whether filter slot obj matches arg. A None
// filter always matches; a plain-str filter matches by equality; any
// other object is treated as a regex and queried via .match(arg).
//
// CPython: Python/_warnings.c:174 check_matched
func checkMatched(obj objects.Object, arg objects.Object) (int, error) {
	if objects.IsNone(obj) {
		return 1, nil
	}
	if _, ok := obj.(*objects.Unicode); ok {
		argStr, err := objects.Str(arg)
		if err != nil {
			return -1, err
		}
		ownStr, _ := objects.Str(obj)
		if ownStr == argStr {
			return 1, nil
		}
		return 0, nil
	}
	// Regex filter: call obj.match(arg). gopy does not yet expose a
	// universal .match shortcut from builtin context, so route the
	// call through PyObject_CallMethod via attribute lookup.
	method, err := objects.GetAttr(obj, objects.NewStr("match"))
	if err != nil {
		return -1, err
	}
	res, err := objects.Call(method, objects.NewTuple([]objects.Object{arg}), nil)
	if err != nil {
		return -1, err
	}
	if objects.IsNone(res) {
		return 0, nil
	}
	ok, err := objects.IsTruthy(res)
	if err != nil {
		return -1, err
	}
	if ok {
		return 1, nil
	}
	return 0, nil
}

// getWarningsAttr looks up a name on the warnings module. Mirrors
// get_warnings_attr. tryImport asks gopy to import warnings; in our
// stack the module may not yet be installed at builtin call time, so
// the lookup is tolerant of missing modules.
//
// CPython: Python/_warnings.c:210 get_warnings_attr
func getWarningsAttr(name string, tryImport bool) (objects.Object, error) { //nolint:unparam // error slot mirrors CPython, kept for future plumbing.
	mod, ok := imp.GetModule("warnings")
	if !ok {
		if !tryImport {
			return nil, nil
		}
		// Late finalization or warnings was never imported: bail with
		// no error, matching the C version's "fallback to C impl" arm.
		return nil, nil
	}
	d := mod.Dict()
	v, lookupErr := d.GetItem(objects.NewStr(name))
	if lookupErr != nil {
		// Missing attribute mirrors PyObject_GetOptionalAttr's
		// (NULL, no-error) result, not a hard failure.
		return nil, nil //nolint:nilerr // GetItem returns ErrKey on miss, which we map to absent.
	}
	return v, nil
}

// getWarningsContext returns the active warnings context. gopy does
// not surface PyContextVar.get from builtin scope, so this always
// reports "no context".
//
// CPython: Python/_warnings.c:267 get_warnings_context
func getWarningsContext() (objects.Object, error) { //nolint:unparam // error slot mirrors CPython, kept for future plumbing.
	return objects.None(), nil
}

// getWarningsContextFilters reads the _filters list off the active
// context. With no context wired in, returns None unconditionally.
//
// CPython: Python/_warnings.c:282 get_warnings_context_filters
func getWarningsContextFilters() (objects.Object, error) {
	ctx, err := getWarningsContext()
	if err != nil {
		return nil, err
	}
	if objects.IsNone(ctx) {
		return objects.None(), nil
	}
	cf, err := objects.GetAttr(ctx, objects.NewStr("_filters"))
	if err != nil {
		return nil, err
	}
	if _, ok := cf.(*objects.List); !ok {
		return nil, fmt.Errorf("ValueError: _filters of warnings._warnings_context must be a list")
	}
	return cf, nil
}

// getWarningsFilters returns the warnings filter list. If
// warnings.filters has been replaced from Python, picks that up.
//
// CPython: Python/_warnings.c:307 get_warnings_filters
func getWarningsFilters() (*objects.List, error) {
	if v, _ := getWarningsAttr("filters", false); v != nil {
		l, ok := v.(*objects.List)
		if !ok {
			return nil, fmt.Errorf("ValueError: _warnings.filters must be a list")
		}
		state.filters = l
	}
	if state.filters == nil {
		return nil, fmt.Errorf("ValueError: _warnings.filters must be a list")
	}
	return state.filters, nil
}

// getOnceRegistry returns the once-registry dict for the active
// interpreter, picking up warnings.onceregistry if the Python facade
// has overridden it.
//
// CPython: Python/_warnings.c:366 get_once_registry
func getOnceRegistry() (*objects.Dict, error) {
	if v, _ := getWarningsAttr("onceregistry", false); v != nil {
		d, ok := v.(*objects.Dict)
		if !ok {
			return nil, fmt.Errorf("TypeError: _warnings.onceregistry must be a dict, not '%s'", v.Type().Name)
		}
		state.onceRegistry = d
	}
	return state.onceRegistry, nil
}

// getDefaultAction returns the default action string, picking up
// warnings.defaultaction overrides on each call.
//
// CPython: Python/_warnings.c:394 get_default_action
func getDefaultAction() (objects.Object, error) {
	if v, _ := getWarningsAttr("defaultaction", false); v != nil {
		if _, ok := v.(*objects.Unicode); !ok {
			return nil, fmt.Errorf("TypeError: _warnings.defaultaction must be a string, not '%s'", v.Type().Name)
		}
		state.defaultAction = v
	}
	return state.defaultAction, nil
}

// filterSearch walks one filter list looking for a row that matches
// (category, text, lineno, module). Mirrors filter_search.
//
// CPython: Python/_warnings.c:424 filter_search
func filterSearch(category *objects.Type, text objects.Object, lineno int64, module objects.Object, listName string, filters *objects.List) (matched *objects.Tuple, action objects.Object, err error) {
	for i := 0; i < filters.Len(); i++ {
		item := filters.Item(i)
		tup, ok := item.(*objects.Tuple)
		if !ok || tup.Len() != 5 {
			return nil, nil, fmt.Errorf("ValueError: warnings.%s item %d isn't a 5-tuple", listName, i)
		}
		act := tup.Item(0)
		msg := tup.Item(1)
		cat := tup.Item(2)
		mod := tup.Item(3)
		lnObj := tup.Item(4)

		if _, ok := act.(*objects.Unicode); !ok {
			return nil, nil, fmt.Errorf("TypeError: action must be a string, not '%s'", act.Type().Name)
		}

		gm, err := checkMatched(msg, text)
		if err != nil {
			return nil, nil, err
		}
		gmod, err := checkMatched(mod, module)
		if err != nil {
			return nil, nil, err
		}

		catType, ok := cat.(*objects.Type)
		if !ok {
			return nil, nil, fmt.Errorf("TypeError: filter category must be a class")
		}
		isSub := objects.IsSubtype(category, catType)

		lnInt, ok := lnObj.(*objects.Int)
		if !ok {
			return nil, nil, fmt.Errorf("TypeError: filter lineno must be int")
		}
		ln, _ := lnInt.Int64()

		if gm == 1 && isSub && gmod == 1 && (ln == 0 || lineno == ln) {
			return tup, act, nil
		}
	}
	return nil, nil, nil
}

// getFilter looks up the (action, item) pair for a (category, text,
// lineno, module) tuple. Walks context filters first, then global
// filters, then falls back to the default action.
//
// CPython: Python/_warnings.c:505 get_filter
func getFilter(category *objects.Type, text objects.Object, lineno int64, module objects.Object) (action objects.Object, item objects.Object, err error) {
	ctxFilters, err := getWarningsContextFilters()
	if err != nil {
		return nil, nil, err
	}
	useGlobal := false
	if objects.IsNone(ctxFilters) {
		useGlobal = true
	} else if l, ok := ctxFilters.(*objects.List); ok {
		mat, act, err := filterSearch(category, text, lineno, module, "_warnings_context _filters", l)
		if err != nil {
			return nil, nil, err
		}
		if act != nil {
			return act, mat, nil
		}
	}
	if useGlobal {
		filters, err := getWarningsFilters()
		if err != nil {
			return nil, nil, err
		}
		mat, act, err := filterSearch(category, text, lineno, module, "filters", filters)
		if err != nil {
			return nil, nil, err
		}
		if act != nil {
			return act, mat, nil
		}
	}
	def, err := getDefaultAction()
	if err != nil {
		return nil, nil, err
	}
	return def, objects.None(), nil
}

// alreadyWarned looks up key in registry. If the stored filters
// version mismatches, the registry is cleared first. With shouldSet,
// the key is stamped True before returning.
//
// CPython: Python/_warnings.c:564 already_warned
func alreadyWarned(registry *objects.Dict, key *objects.Tuple, shouldSet bool) (int, error) {
	if key == nil {
		return -1, fmt.Errorf("RuntimeError: already_warned: nil key")
	}
	versionKey := objects.NewStr("version")
	verObj, _ := registry.GetItem(versionKey)
	needsBump := true
	if v, ok := verObj.(*objects.Int); ok {
		n, _ := v.Int64()
		if n == state.filtersVersion {
			needsBump = false
		}
	}
	if needsBump {
		for _, k := range registry.Keys() {
			_ = registry.DelItem(k)
		}
		if err := registry.SetItem(versionKey, objects.NewInt(state.filtersVersion)); err != nil {
			return -1, err
		}
	} else {
		if existing, err := registry.GetItem(key); err == nil {
			ok, err := objects.IsTruthy(existing)
			if err != nil {
				return -1, err
			}
			if ok {
				return 1, nil
			}
		}
	}
	if shouldSet {
		if err := registry.SetItem(key, objects.True()); err != nil {
			return -1, err
		}
		return 0, nil
	}
	return 0, nil
}

// normalizeModule strips a trailing .py from a filename, or returns
// "<unknown>" for empty input. Mirrors normalize_module.
//
// CPython: Python/_warnings.c:617 normalize_module
func normalizeModule(filename objects.Object) (objects.Object, error) {
	s, err := objects.Str(filename)
	if err != nil {
		return nil, err
	}
	if s == "" {
		return objects.NewStr("<unknown>"), nil
	}
	if strings.HasSuffix(s, ".py") {
		return objects.NewStr(s[:len(s)-3]), nil
	}
	return filename, nil
}

// updateRegistry stamps (text, category[, 0]) True in registry.
//
// CPython: Python/_warnings.c:649 update_registry
func updateRegistry(registry *objects.Dict, text objects.Object, category objects.Object, addZero bool) (int, error) {
	var altkey *objects.Tuple
	if addZero {
		altkey = objects.NewTuple([]objects.Object{text, category, objects.NewInt(0)})
	} else {
		altkey = objects.NewTuple([]objects.Object{text, category})
	}
	return alreadyWarned(registry, altkey, true)
}

// showWarning is the fallback formatter when no _showwarnmsg hook is
// installed. Mirrors show_warning: writes
// "filename:lineno: category: text\n" to sys.stderr, then the source
// line if any.
//
// CPython: Python/_warnings.c:666 show_warning
func showWarning(filename objects.Object, lineno int64, text objects.Object, category *objects.Type, sourceline objects.Object) {
	stderr := getStderr()
	fnStr, _ := objects.Str(filename)
	txStr, _ := objects.Str(text)
	name := "Warning"
	if category != nil {
		name = category.Name
	}
	line := fmt.Sprintf("%s:%d: %s: %s\n", fnStr, lineno, name, txStr)
	writeToFile(stderr, line)
	if sourceline != nil && !objects.IsNone(sourceline) {
		if s, err := objects.Str(sourceline); err == nil {
			trimmed := strings.TrimLeft(s, " \t\f")
			writeToFile(stderr, trimmed+"\n")
		}
	}
}

// getStderr returns sys.stderr if accessible, falling back to the
// host stderr file descriptor. Used by showWarning.
//
// CPython: Python/_warnings.c:681 _PySys_GetOptionalAttr(stderr)
func getStderr() objects.Object {
	mod, ok := imp.GetModule("sys")
	if !ok {
		return nil
	}
	v, _ := mod.Dict().GetItem(objects.NewStr("stderr"))
	return v
}

// writeToFile sends s to either an objects.File or, as a last
// resort, the host stderr file.
//
// CPython: Python/_warnings.c:687 PyFile_WriteObject(... f_stderr ...)
func writeToFile(f objects.Object, s string) {
	if fi, ok := f.(*objects.File); ok {
		_, _ = fi.Write(objects.NewStr(s))
		return
	}
	// Fallback: print directly so the warning is never silently lost.
	fmt.Fprint(os.Stderr, s)
}

// callShowWarning hands off to warnings._showwarnmsg if present, or
// falls back to showWarning. Mirrors call_show_warning.
//
// CPython: Python/_warnings.c:736 call_show_warning
func callShowWarning(category *objects.Type, text, message, filename objects.Object, lineno int64, sourceline, source objects.Object) error {
	showFn, _ := getWarningsAttr("_showwarnmsg", true)
	if showFn == nil {
		showWarning(filename, lineno, text, category, sourceline)
		return nil
	}
	warnmsgCls, _ := getWarningsAttr("WarningMessage", false)
	if warnmsgCls == nil {
		return fmt.Errorf("RuntimeError: unable to get warnings.WarningMessage")
	}
	if source == nil {
		source = objects.None()
	}
	args := objects.NewTuple([]objects.Object{message, category, filename, objects.NewInt(lineno), objects.None(), objects.None(), source})
	msg, err := objects.Call(warnmsgCls, args, nil)
	if err != nil {
		return err
	}
	_, err = objects.Call(showFn, objects.NewTuple([]objects.Object{msg}), nil)
	return err
}

// normalizeMessage covers the C code's "rc = PyObject_IsInstance(
// message, PyExc_Warning)" branch in warn_explicit: if message is a
// Warning instance, lift its str()/type as text/category; otherwise
// build an instance from category(message). Returns (text, catType,
// possibly-rewritten message, err).
//
// CPython: Python/_warnings.c:826 warn_explicit (rc = PyObject_IsInstance)
func normalizeMessage(message objects.Object, category objects.Object) (objects.Object, *objects.Type, objects.Object, error) {
	if e, ok := message.(*errors.Exception); ok && errors.IsSubtype(e.ExcType, errors.PyExc_Warning) {
		return objects.NewStr(e.Message()), e.ExcType, message, nil
	}
	catCls, ok := category.(*objects.Type)
	if !ok {
		return nil, nil, nil, fmt.Errorf("TypeError: category must be a Warning subclass")
	}
	inst, err := objects.Call(catCls, objects.NewTuple([]objects.Object{message}), nil)
	if err != nil {
		return nil, nil, nil, err
	}
	return message, catCls, inst, nil
}

// applyAction handles the action dispatch tail of warn_explicit:
// returns (stopShort, rc, err). stopShort==true means "do not call
// show_warning". rc==1 means "already_warned for this module".
//
// CPython: Python/_warnings.c:870 warn_explicit (action dispatch)
func applyAction(actStr string, registry objects.Object, key *objects.Tuple, text objects.Object, catType *objects.Type) (bool, int, error) {
	switch actStr {
	case "error":
		return true, 0, nil
	case "ignore":
		return true, 0, nil
	}
	if actStr == "always" || actStr == "all" {
		return false, 0, nil
	}
	if reg, ok := registry.(*objects.Dict); ok && !objects.IsNone(registry) {
		if err := reg.SetItem(key, objects.True()); err != nil {
			return false, 0, err
		}
	}
	switch actStr {
	case "once":
		reg, ok := registry.(*objects.Dict)
		if !ok || objects.IsNone(registry) {
			or, err := getOnceRegistry()
			if err != nil {
				return false, 0, err
			}
			reg = or
		}
		r, err := updateRegistry(reg, text, catType, false)
		return false, r, err
	case "module":
		if reg, ok := registry.(*objects.Dict); ok && !objects.IsNone(registry) {
			r, err := updateRegistry(reg, text, catType, false)
			return false, r, err
		}
		return false, 0, nil
	case "default":
		return false, 0, nil
	}
	return false, 0, fmt.Errorf("RuntimeError: Unrecognized action (%s) in warnings.filters", actStr)
}

// warnExplicit is the workhorse: given a fully resolved (category,
// message, filename, lineno, module, registry) tuple, decide whether
// to ignore, error, or display the warning. Mirrors warn_explicit.
//
// CPython: Python/_warnings.c:792 warn_explicit
func warnExplicit(category objects.Object, message objects.Object, filename objects.Object, lineno int64, module objects.Object, registry objects.Object, sourceline objects.Object, source objects.Object) (objects.Object, error) {
	if objects.IsNone(module) {
		return objects.None(), nil
	}
	if registry != nil && !objects.IsNone(registry) {
		if _, ok := registry.(*objects.Dict); !ok {
			return nil, fmt.Errorf("TypeError: 'registry' must be a dict or None")
		}
	}
	if module == nil {
		m, err := normalizeModule(filename)
		if err != nil {
			return nil, err
		}
		module = m
	}

	text, catType, message, err := normalizeMessage(message, category)
	if err != nil {
		return nil, err
	}

	linenoObj := objects.NewInt(lineno)
	if objects.IsNone(source) {
		source = nil
	}

	key := objects.NewTuple([]objects.Object{text, catType, linenoObj})
	if reg, ok := registry.(*objects.Dict); ok {
		rc, err := alreadyWarned(reg, key, false)
		if err != nil {
			return nil, err
		}
		if rc == 1 {
			return objects.None(), nil
		}
	}

	action, _, err := getFilter(catType, text, lineno, module)
	if err != nil {
		return nil, err
	}
	actStr, err := objects.Str(action)
	if err != nil {
		return nil, err
	}

	stopShort, rc, err := applyAction(actStr, registry, key, text, catType)
	if err != nil {
		return nil, err
	}
	if stopShort {
		if actStr == "error" {
			msgText, _ := objects.Str(text)
			return nil, fmt.Errorf("%s: %s", catType.Name, msgText)
		}
		return objects.None(), nil
	}
	if rc == 1 {
		return objects.None(), nil
	}
	if err := callShowWarning(catType, text, message, filename, lineno, sourceline, source); err != nil {
		return nil, err
	}
	return objects.None(), nil
}

// setupContext approximates the C version's frame walk. gopy does
// not expose the current frame from builtin context, so the
// filename / lineno default to "<unknown>" / 0 and module to
// "<string>". Once the eval-loop walk lands, this function is the
// place to plumb sys._getframe(stack_level) in.
//
// CPython: Python/_warnings.c:1024 setup_context
func setupContext(stackLevel int64) (filename objects.Object, lineno int64, module objects.Object, registry objects.Object, err error) { //nolint:unparam // error slot mirrors CPython once frame walking lands.
	_ = stackLevel
	return objects.NewStr("<unknown>"), 0, objects.NewStr("<string>"), objects.NewDict(), nil
}

// getCategory normalizes (message, category) into a Warning subclass.
//
// CPython: Python/_warnings.c:1125 get_category
func getCategory(message objects.Object, category objects.Object) (*objects.Type, error) {
	if e, ok := message.(*errors.Exception); ok && errors.IsSubtype(e.ExcType, errors.PyExc_Warning) {
		return e.ExcType, nil
	}
	if category == nil || objects.IsNone(category) {
		return errors.PyExc_UserWarning, nil
	}
	catType, ok := category.(*objects.Type)
	if !ok {
		return nil, fmt.Errorf("TypeError: category must be a Warning subclass, not '%s'", category.Type().Name)
	}
	if !errors.IsSubtype(catType, errors.PyExc_Warning) {
		return nil, fmt.Errorf("TypeError: category must be a Warning subclass, not '%s'", catType.Name)
	}
	return catType, nil
}

// doWarn is the kernel shared by warn() and the public PyErr_WarnEx
// path. Mirrors do_warn.
//
// CPython: Python/_warnings.c:1154 do_warn
func doWarn(message objects.Object, category *objects.Type, stackLevel int64, source objects.Object, skipFilePrefixes *objects.Tuple) (objects.Object, error) {
	_ = skipFilePrefixes
	filename, lineno, module, registry, err := setupContext(stackLevel)
	if err != nil {
		return nil, err
	}
	warningsLock(state)
	res, werr := warnExplicit(category, message, filename, lineno, module, registry, nil, source)
	warningsUnlock(state)
	return res, werr
}

// warnBuiltin is the _warnings.warn entry point.
//
// CPython: Python/_warnings.c:1200 warnings_warn_impl
func warnBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	var (
		message  = objects.None()
		category = objects.None()
		stackLvl = int64(1)
		source   = objects.None()
		skip     *objects.Tuple
	)
	if len(args) >= 1 {
		message = args[0]
	}
	if len(args) >= 2 {
		category = args[1]
	}
	if len(args) >= 3 {
		if n, ok := args[2].(*objects.Int); ok {
			v, _ := n.Int64()
			stackLvl = v
		}
	}
	if len(args) >= 4 {
		source = args[3]
	}
	if v, ok := kwargs["stacklevel"]; ok {
		if n, ok := v.(*objects.Int); ok {
			x, _ := n.Int64()
			stackLvl = x
		}
	}
	if v, ok := kwargs["source"]; ok {
		source = v
	}
	if v, ok := kwargs["skip_file_prefixes"]; ok {
		if t, ok := v.(*objects.Tuple); ok {
			skip = t
		}
	}
	cat, err := getCategory(message, category)
	if err != nil {
		return nil, err
	}
	if skip != nil && skip.Len() > 0 && stackLvl < 2 {
		stackLvl = 2
	}
	return doWarn(message, cat, stackLvl, source, skip)
}

// warnExplicitBuiltin is _warnings.warn_explicit.
//
// CPython: Python/_warnings.c:1293 warnings_warn_explicit_impl
func warnExplicitBuiltin(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 4 {
		return nil, fmt.Errorf("TypeError: warn_explicit() takes at least 4 arguments (%d given)", len(args))
	}
	message := args[0]
	category := args[1]
	filename := args[2]
	lineno := int64(0)
	if n, ok := args[3].(*objects.Int); ok {
		v, _ := n.Int64()
		lineno = v
	}
	var module objects.Object
	registry := objects.None()
	moduleGlobals := objects.None()
	source := objects.None()
	if len(args) >= 5 {
		module = args[4]
	}
	if len(args) >= 6 {
		registry = args[5]
	}
	if len(args) >= 7 {
		moduleGlobals = args[6]
	}
	if len(args) >= 8 {
		source = args[7]
	}
	if v, ok := kwargs["module"]; ok {
		module = v
	}
	if v, ok := kwargs["registry"]; ok {
		registry = v
	}
	if v, ok := kwargs["module_globals"]; ok {
		moduleGlobals = v
	}
	if v, ok := kwargs["source"]; ok {
		source = v
	}

	var sourceLine objects.Object
	if moduleGlobals != nil && !objects.IsNone(moduleGlobals) {
		mg, ok := moduleGlobals.(*objects.Dict)
		if !ok {
			return nil, fmt.Errorf("TypeError: module_globals must be a dict, not '%s'", moduleGlobals.Type().Name)
		}
		sl, err := getSourceLine(mg, lineno)
		if err != nil {
			return nil, err
		}
		sourceLine = sl
	}

	warningsLock(state)
	res, err := warnExplicit(category, message, filename, lineno, module, registry, sourceLine, source)
	warningsUnlock(state)
	return res, err
}

// getSourceLine asks the module's loader for line `lineno` of the
// source. gopy does not yet implement importlib loaders end-to-end,
// so this returns nil unless module_globals carries a "__source__"
// hint (used by tests).
//
// CPython: Python/_warnings.c:1222 get_source_line
func getSourceLine(moduleGlobals *objects.Dict, lineno int64) (objects.Object, error) { //nolint:unparam // error slot mirrors CPython once loaders land.
	// Test/runtime hook: skip the loader walk when not wired up.
	hint, lookupErr := moduleGlobals.GetItem(objects.NewStr("__source__"))
	if lookupErr != nil || hint == nil {
		return nil, nil //nolint:nilerr // missing hint is not a hard error.
	}
	s, strErr := objects.Str(hint)
	if strErr != nil {
		return nil, nil //nolint:nilerr // bad hint shape gets the loader-miss path.
	}
	lines := strings.Split(s, "\n")
	if lineno < 1 || int(lineno) > len(lines) {
		return nil, nil
	}
	return objects.NewStr(lines[lineno-1]), nil
}

// -----------------------------------------------------------------------------
// C-level PyErr_Warn family. Other gopy packages call these to emit
// warnings without dropping back into Python.
// -----------------------------------------------------------------------------

// WarnUnicode is the workhorse used by PyErr_Warn*. Mirrors
// warn_unicode.
//
// CPython: Python/_warnings.c:1360 warn_unicode
func WarnUnicode(category *objects.Type, message string, stackLevel int64, source objects.Object) error {
	if category == nil {
		category = errors.PyExc_RuntimeWarning
	}
	if source == nil {
		source = objects.None()
	}
	msg := objects.NewStr(message)
	_, err := doWarn(msg, category, stackLevel, source, nil)
	return err
}

// WarnFormat ports PyErr_WarnFormat: format args via fmt.Sprintf, then
// hand off to WarnUnicode.
//
// CPython: Python/_warnings.c:1394 PyErr_WarnFormat
func WarnFormat(category *objects.Type, stackLevel int64, format string, a ...interface{}) error {
	return WarnUnicode(category, fmt.Sprintf(format, a...), stackLevel, nil)
}

// WarnFormatSource ports the static _PyErr_WarnFormat. Same as
// WarnFormat but threads a source object through to ResourceWarning
// finalization handlers.
//
// CPython: Python/_warnings.c:1407 _PyErr_WarnFormat
func WarnFormatSource(source objects.Object, category *objects.Type, stackLevel int64, format string, a ...interface{}) error {
	return WarnUnicode(category, fmt.Sprintf(format, a...), stackLevel, source)
}

// ResourceWarning ports PyErr_ResourceWarning.
//
// CPython: Python/_warnings.c:1420 PyErr_ResourceWarning
func ResourceWarning(source objects.Object, stackLevel int64, format string, a ...interface{}) error {
	return WarnFormatSource(source, errors.PyExc_ResourceWarning, stackLevel, format, a...)
}

// WarnEx ports PyErr_WarnEx.
//
// CPython: Python/_warnings.c:1435 PyErr_WarnEx
func WarnEx(category *objects.Type, text string, stackLevel int64) error {
	return WarnUnicode(category, text, stackLevel, nil)
}

// Warn ports the legacy PyErr_Warn (stack_level==1).
//
// CPython: Python/_warnings.c:1452 PyErr_Warn
func Warn(category *objects.Type, text string) error {
	return WarnEx(category, text, 1)
}

// WarnExplicitObject ports PyErr_WarnExplicitObject.
//
// CPython: Python/_warnings.c:1459 PyErr_WarnExplicitObject
func WarnExplicitObject(category *objects.Type, message, filename objects.Object, lineno int64, module objects.Object, registry objects.Object) error {
	if category == nil {
		category = errors.PyExc_RuntimeWarning
	}
	warningsLock(state)
	_, err := warnExplicit(category, message, filename, lineno, module, registry, nil, nil)
	warningsUnlock(state)
	return err
}

// WarnExplicit ports PyErr_WarnExplicit.
//
// CPython: Python/_warnings.c:1482 PyErr_WarnExplicit
func WarnExplicit(category *objects.Type, text, filenameStr string, lineno int64, moduleStr string, registry objects.Object) error {
	msg := objects.NewStr(text)
	fn := objects.NewStr(filenameStr)
	var module objects.Object
	if moduleStr != "" {
		module = objects.NewStr(moduleStr)
	}
	return WarnExplicitObject(category, msg, fn, lineno, module, registry)
}

// WarnExplicitFormat ports PyErr_WarnExplicitFormat.
//
// CPython: Python/_warnings.c:1514 PyErr_WarnExplicitFormat
func WarnExplicitFormat(category *objects.Type, filenameStr string, lineno int64, moduleStr string, registry objects.Object, format string, a ...interface{}) error {
	return WarnExplicit(category, fmt.Sprintf(format, a...), filenameStr, lineno, moduleStr, registry)
}

// WarnUnawaitedAgenMethod ports _PyErr_WarnUnawaitedAgenMethod. gopy
// does not yet ship async generators; the entry point is exposed so
// callers can wire it once Modules/_asyncio lands.
//
// CPython: Python/_warnings.c:1558 _PyErr_WarnUnawaitedAgenMethod
func WarnUnawaitedAgenMethod(agen objects.Object, method objects.Object) {
	_ = WarnFormatSource(agen, errors.PyExc_RuntimeWarning, 1, "coroutine method %v of %v was never awaited", method, agen)
}

// WarnUnawaitedCoroutine ports _PyErr_WarnUnawaitedCoroutine.
//
// CPython: Python/_warnings.c:1573 _PyErr_WarnUnawaitedCoroutine
func WarnUnawaitedCoroutine(coro objects.Object) {
	fn, _ := getWarningsAttr("_warn_unawaited_coroutine", true)
	warned := false
	if fn != nil {
		if _, err := objects.Call(fn, objects.NewTuple([]objects.Object{coro}), nil); err == nil {
			warned = true
		}
	}
	if !warned {
		_ = WarnFormatSource(coro, errors.PyExc_RuntimeWarning, 1, "coroutine '%v' was never awaited", coro)
	}
}

// WarningsFini drops every entry of the active state, mirroring
// _PyWarnings_Fini.
//
// CPython: Python/_warnings.c:1687 _PyWarnings_Fini
func WarningsFini() {
	state.filters = objects.NewList(nil)
	state.onceRegistry = objects.NewDict()
}

// FiltersList exposes the live filter list for callers (tests,
// Lib/warnings.py overrides). Returning the same *List so mutations
// propagate to subsequent warn() calls.
func FiltersList() *objects.List { return state.filters }

// ResetFilters drops every row, the gopy equivalent of
// warnings.resetwarnings().
func ResetFilters() {
	state.filters.SetSlice(0, state.filters.Len(), nil)
	state.filtersVersion++
}

// SimpleFilter clears the filter list and inserts one row of the
// given action against every Warning. Mirrors the Python
// simplefilter() behavior at the C state level.
func SimpleFilter(action string) {
	state.filters.SetSlice(0, state.filters.Len(), nil)
	state.filters.Append(createFilter(errors.PyExc_Warning, action, ""))
	state.filtersVersion++
}

// FilterWarnings prepends an (action, message, category, module,
// lineno) row to the filter list. Mirrors warnings.filterwarnings.
func FilterWarnings(action string, category *objects.Type, module string, lineno int64) {
	if category == nil {
		category = errors.PyExc_Warning
	}
	tup := createFilter(category, action, module)
	// prepend
	all := []objects.Object{tup}
	for i := 0; i < state.filters.Len(); i++ {
		all = append(all, state.filters.Item(i))
	}
	state.filters.SetSlice(0, state.filters.Len(), all)
	state.filtersVersion++
}
