// Decoder primitives. Phase 5 of P14.1 ports the proto-5 binary read
// path: the wire format the Phase 2/3 encoder writes goes back through
// here and reconstructs the original object graph. The dispatch loop
// mirrors `load` in Modules/_pickle.c:6950 case by case.
//
// The decoder works against an in-memory []byte, mirroring CPython's
// _Unpickler_ReadFromFile fast path that copies the file payload into
// a memory buffer first. File / iterator-based input lands later.
//
// CPython: Modules/_pickle.c:6950 load
// CPython: Modules/_pickle.c:5281 load_none ... 5856 load_list

package _pickle

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// unpickler holds the in-progress read state for a single load cycle.
// Mirrors the subset of CPython's UnpicklerObject that's reachable
// from the proto-5 binary read path.
//
// CPython: Modules/_pickle.c:790 UnpicklerObject
type unpickler struct {
	buf        []byte
	pos        int
	stack      []objects.Object
	marks      []int
	memo       []objects.Object
	proto      int
	fixImports bool
}

func newUnpickler(buf []byte) *unpickler {
	// fix_imports defaults to True; it only takes effect for proto < 3
	// where it maps Python 2 module/global names onto their Python 3
	// equivalents via _compat_pickle.
	//
	// CPython: Modules/_pickle.c:1669 Unpickler.__init__ (fix_imports=1)
	return &unpickler{buf: buf, fixImports: true}
}

// read returns the next n bytes from the stream and advances the
// cursor. The slice aliases the underlying buffer; callers must not
// mutate it (matching `_Unpickler_Read` which hands back a pointer
// into the internal read buffer).
//
// CPython: Modules/_pickle.c:1543 _Unpickler_Read
func (u *unpickler) read(n int) ([]byte, error) {
	if u.pos+n > len(u.buf) {
		return nil, errors.New("UnpicklingError: pickle data was truncated")
	}
	out := u.buf[u.pos : u.pos+n]
	u.pos += n
	return out, nil
}

func (u *unpickler) readByte() (byte, error) {
	if u.pos >= len(u.buf) {
		return 0, errors.New("UnpicklingError: pickle data was truncated")
	}
	b := u.buf[u.pos]
	u.pos++
	return b, nil
}

// push appends an object to the value stack.
//
// CPython: Modules/_pickle.c:296 PDATA_PUSH
func (u *unpickler) push(o objects.Object) {
	u.stack = append(u.stack, o)
}

// pop removes and returns the top of the value stack.
//
// CPython: Modules/_pickle.c:328 PDATA_POP
func (u *unpickler) pop() (objects.Object, error) {
	if len(u.stack) == 0 {
		return nil, errors.New("UnpicklingError: stack underflow")
	}
	top := u.stack[len(u.stack)-1]
	u.stack = u.stack[:len(u.stack)-1]
	return top, nil
}

// pushMark records the current stack height. MARK is later popped by
// the closing opcode (APPENDS / SETITEMS / ADDITEMS / TUPLE / DICT /
// LIST / FROZENSET) to slice the items off the stack.
//
// CPython: Modules/_pickle.c:6852 load_mark
func (u *unpickler) pushMark() {
	u.marks = append(u.marks, len(u.stack))
}

// popMark consumes the most recent mark and returns the stack index
// where the mark was placed (so stack[idx:] are the items the closing
// opcode should grab).
//
// CPython: Modules/_pickle.c:266 marker
func (u *unpickler) popMark() (int, error) {
	if len(u.marks) == 0 {
		return 0, errors.New("UnpicklingError: could not find MARK")
	}
	idx := u.marks[len(u.marks)-1]
	u.marks = u.marks[:len(u.marks)-1]
	return idx, nil
}

// memoPut stores obj at the given index in the memo table. The memo
// grows as needed; gaps stay nil and trip an UnpicklingError if a
// later BINGET / LONG_BINGET tries to fetch them.
//
// CPython: Modules/_pickle.c:1908 _Unpickler_MemoPut
func (u *unpickler) memoPutAt(idx int, obj objects.Object) error {
	if idx < 0 {
		return errors.New("UnpicklingError: negative memo index")
	}
	for len(u.memo) <= idx {
		u.memo = append(u.memo, nil)
	}
	u.memo[idx] = obj
	return nil
}

func (u *unpickler) memoGet(idx int) (objects.Object, error) {
	if idx < 0 || idx >= len(u.memo) || u.memo[idx] == nil {
		return nil, fmt.Errorf("UnpicklingError: bad memo key %d", idx)
	}
	return u.memo[idx], nil
}

// load is the dispatch loop. Reads one opcode at a time and runs the
// matching handler until STOP, at which point the top of the stack is
// returned.
//
// CPython: Modules/_pickle.c:6950 load
func (u *unpickler) load() (objects.Object, error) {
	for {
		op, err := u.readByte()
		if err != nil {
			return nil, err
		}
		switch op {
		case opProto:
			if err := u.loadProto(); err != nil {
				return nil, err
			}
		case opFrame:
			if err := u.loadFrame(); err != nil {
				return nil, err
			}
		case opStop:
			return u.pop()
		case opNone:
			u.push(objects.None())
		case opNewtrue:
			u.push(objects.True())
		case opNewfalse:
			u.push(objects.False())
		case opInt:
			if err := u.loadTextInt(); err != nil {
				return nil, err
			}
		case opFloat:
			if err := u.loadTextFloat(); err != nil {
				return nil, err
			}
		case opLong:
			if err := u.loadTextLong(); err != nil {
				return nil, err
			}
		case opString:
			if err := u.loadTextString(); err != nil {
				return nil, err
			}
		case opUnicode:
			if err := u.loadTextUnicode(); err != nil {
				return nil, err
			}
		case opDict:
			if err := u.loadDictFromMark(); err != nil {
				return nil, err
			}
		case opList:
			if err := u.loadListFromMark(); err != nil {
				return nil, err
			}
		case opBinint:
			if err := u.loadBinintX(4, true); err != nil {
				return nil, err
			}
		case opBinint1:
			if err := u.loadBinintX(1, false); err != nil {
				return nil, err
			}
		case opBinint2:
			if err := u.loadBinintX(2, false); err != nil {
				return nil, err
			}
		case opLong1:
			if err := u.loadCountedLong(1); err != nil {
				return nil, err
			}
		case opLong4:
			if err := u.loadCountedLong(4); err != nil {
				return nil, err
			}
		case opBinfloat:
			if err := u.loadBinfloat(); err != nil {
				return nil, err
			}
		case opShortBinbytes:
			if err := u.loadCountedBinbytes(1); err != nil {
				return nil, err
			}
		case opBinbytes:
			if err := u.loadCountedBinbytes(4); err != nil {
				return nil, err
			}
		case opBinbytes8:
			if err := u.loadCountedBinbytes(8); err != nil {
				return nil, err
			}
		case opShortBinunicode:
			if err := u.loadCountedBinunicode(1); err != nil {
				return nil, err
			}
		case opBinunicode:
			if err := u.loadCountedBinunicode(4); err != nil {
				return nil, err
			}
		case opBinunicode8:
			if err := u.loadCountedBinunicode(8); err != nil {
				return nil, err
			}
		case opEmptyTuple:
			u.push(objects.NewTuple(nil))
		case opTuple1:
			if err := u.loadCountedTuple(1); err != nil {
				return nil, err
			}
		case opTuple2:
			if err := u.loadCountedTuple(2); err != nil {
				return nil, err
			}
		case opTuple3:
			if err := u.loadCountedTuple(3); err != nil {
				return nil, err
			}
		case opTuple:
			if err := u.loadTuple(); err != nil {
				return nil, err
			}
		case opEmptyList:
			u.push(objects.NewList(nil))
		case opEmptyDict:
			u.push(objects.NewDict())
		case opEmptySet:
			u.push(objects.NewSet())
		case opMark:
			u.pushMark()
		case opAppend:
			if err := u.loadAppend(); err != nil {
				return nil, err
			}
		case opAppends:
			if err := u.loadAppends(); err != nil {
				return nil, err
			}
		case opSetitem:
			if err := u.loadSetitem(); err != nil {
				return nil, err
			}
		case opSetitems:
			if err := u.loadSetitems(); err != nil {
				return nil, err
			}
		case opAdditems:
			if err := u.loadAdditems(); err != nil {
				return nil, err
			}
		case opFrozenset:
			if err := u.loadFrozenset(); err != nil {
				return nil, err
			}
		case opMemoize:
			if err := u.loadMemoize(); err != nil {
				return nil, err
			}
		case opBinget:
			if err := u.loadBinget(); err != nil {
				return nil, err
			}
		case opLongBinget:
			if err := u.loadLongBinget(); err != nil {
				return nil, err
			}
		case opBinput:
			if err := u.loadBinput(); err != nil {
				return nil, err
			}
		case opLongBinput:
			if err := u.loadLongBinput(); err != nil {
				return nil, err
			}
		case opPut:
			if err := u.loadPut(); err != nil {
				return nil, err
			}
		case opGet:
			if err := u.loadGet(); err != nil {
				return nil, err
			}
		case opPop:
			if _, err := u.pop(); err != nil {
				return nil, err
			}
		case opPopMark:
			if err := u.loadPopMark(); err != nil {
				return nil, err
			}
		case opGlobal:
			if err := u.loadGlobal(); err != nil {
				return nil, err
			}
		case opStackGlobal:
			if err := u.loadStackGlobal(); err != nil {
				return nil, err
			}
		case opReduce:
			if err := u.loadReduce(); err != nil {
				return nil, err
			}
		case opBuild:
			if err := u.loadBuild(); err != nil {
				return nil, err
			}
		case opNewobj:
			if err := u.loadNewobj(); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("UnpicklingError: unknown opcode 0x%02x", op)
		}
	}
}

// findClass resolves a "module" + "name" pickle GLOBAL reference. For
// proto < 3 with fix_imports enabled (the default) the old Python 2.x
// names are mapped to their Python 3 equivalents via _compat_pickle
// before the module is imported, so legacy streams that reference
// __builtin__.xrange or cPickle.* still load. The module is imported
// (not merely fetched from sys.modules) and dotted name components are
// walked for nested attributes (e.g. "os.path" then "join").
//
// CPython: Modules/_pickle.c:7172 _pickle_Unpickler_find_class_impl
func (u *unpickler) findClass(module, name string) (objects.Object, error) {
	if u.proto < 3 && u.fixImports {
		var err error
		module, name, err = fixImportsMapping(module, name)
		if err != nil {
			return nil, err
		}
	}
	mod, err := importModule(module)
	if err != nil {
		return nil, err
	}
	// Walk dotted name components.
	var obj objects.Object = mod
	for _, part := range strings.Split(name, ".") {
		v, gErr := objects.GetAttr(obj, objects.NewStr(part))
		if gErr != nil {
			return nil, fmt.Errorf("AttributeError: %s: %q", module, part)
		}
		obj = v
	}
	return obj, nil
}

// importModule imports a module by name, using the ImportModuleHook
// installed by the vm package when present (so modules not yet in
// sys.modules are imported through the finder chain) and falling back
// to a sys.modules lookup otherwise.
//
// CPython: Modules/_pickle.c:7240 PyImport_Import in find_class
func importModule(module string) (objects.Object, error) {
	if objects.ImportModuleHook != nil {
		mod, err := objects.ImportModuleHook(module)
		if err != nil {
			return nil, fmt.Errorf("ModuleNotFoundError: No module named %q", module)
		}
		return mod, nil
	}
	mod, ok := imp.GetModule(module)
	if !ok {
		return nil, fmt.Errorf("ModuleNotFoundError: No module named %q", module)
	}
	return mod, nil
}

// fixImportsMapping applies the Python 2 to 3 rename tables from
// _compat_pickle: NAME_MAPPING keyed on (module, name) takes priority,
// otherwise IMPORT_MAPPING renames the module alone. A reference with
// no mapping is returned unchanged.
//
// CPython: Modules/_pickle.c:7185 _pickle_Unpickler_find_class_impl (fix_imports block)
func fixImportsMapping(module, name string) (string, string, error) {
	compat, err := importModule("_compat_pickle")
	if err != nil {
		// Without the compat tables there is nothing to remap; fall back
		// to the names as written.
		return module, name, nil
	}
	nameMapping, err := objects.GetAttr(compat, objects.NewStr("NAME_MAPPING"))
	if err != nil {
		return module, name, nil
	}
	key := objects.NewTuple([]objects.Object{objects.NewStr(module), objects.NewStr(name)})
	item, found, err := objects.MappingGetOptionalItem(nameMapping, key)
	if err != nil {
		return "", "", err
	}
	if found {
		pair, ok := item.(*objects.Tuple)
		if !ok || pair.Len() != 2 {
			return "", "", fmt.Errorf("RuntimeError: _compat_pickle.NAME_MAPPING values should be 2-tuples, not %.200s", item.Type().Name)
		}
		newMod, ok1 := pair.Item(0).(*objects.Unicode)
		newName, ok2 := pair.Item(1).(*objects.Unicode)
		if !ok1 || !ok2 {
			return "", "", fmt.Errorf("RuntimeError: _compat_pickle.NAME_MAPPING values should be pairs of str, not (%.200s, %.200s)", pair.Item(0).Type().Name, pair.Item(1).Type().Name)
		}
		return newMod.Value(), newName.Value(), nil
	}
	importMapping, err := objects.GetAttr(compat, objects.NewStr("IMPORT_MAPPING"))
	if err != nil {
		return module, name, nil
	}
	modItem, found, err := objects.MappingGetOptionalItem(importMapping, objects.NewStr(module))
	if err != nil {
		return "", "", err
	}
	if found {
		newMod, ok := modItem.(*objects.Unicode)
		if !ok {
			return "", "", fmt.Errorf("RuntimeError: _compat_pickle.IMPORT_MAPPING values should be strings, not %.200s", modItem.Type().Name)
		}
		module = newMod.Value()
	}
	return module, name, nil
}

// loadGlobal reads two newline-terminated strings (module and name)
// and pushes the resolved global onto the stack.
//
// CPython: Modules/_pickle.c:5671 load_global
func (u *unpickler) loadGlobal() error {
	// Proto < 4: two NL-terminated ASCII lines.
	module := u.readLine()
	name := u.readLine()
	obj, err := u.findClass(module, name)
	if err != nil {
		return err
	}
	u.push(obj)
	return nil
}

// readLine reads bytes until '\n', returning the content without the newline.
func (u *unpickler) readLine() string {
	start := u.pos
	for u.pos < len(u.buf) && u.buf[u.pos] != '\n' {
		u.pos++
	}
	line := string(u.buf[start:u.pos])
	if u.pos < len(u.buf) {
		u.pos++ // consume '\n'
	}
	return line
}

// loadStackGlobal pops (name, module) from the stack and pushes the
// resolved global. Proto >= 4.
//
// CPython: Modules/_pickle.c:5707 load_stack_global
func (u *unpickler) loadStackGlobal() error {
	nameObj, err := u.pop()
	if err != nil {
		return err
	}
	moduleObj, err := u.pop()
	if err != nil {
		return err
	}
	modStr, ok1 := moduleObj.(*objects.Unicode)
	nameStr, ok2 := nameObj.(*objects.Unicode)
	if !ok1 || !ok2 {
		return errors.New("UnpicklingError: STACK_GLOBAL requires two str objects")
	}
	obj, err := u.findClass(modStr.Value(), nameStr.Value())
	if err != nil {
		return err
	}
	u.push(obj)
	return nil
}

// loadReduce pops (callable, args) from the stack, calls
// callable(*args), and pushes the result.
//
// CPython: Modules/_pickle.c:5734 load_reduce
func (u *unpickler) loadReduce() error {
	args, err := u.pop()
	if err != nil {
		return err
	}
	callable, err := u.pop()
	if err != nil {
		return err
	}
	argTuple, ok := args.(*objects.Tuple)
	if !ok {
		return errors.New("UnpicklingError: REDUCE requires a tuple as args")
	}
	result, err := objects.Call(callable, argTuple, nil)
	if err != nil {
		return fmt.Errorf("UnpicklingError: REDUCE call failed: %w", err)
	}
	u.push(result)
	return nil
}

// loadBuild applies state to the top of the stack. When the instance
// defines __setstate__ that method takes full responsibility; otherwise
// the default protocol applies: a proto-2 state may be a (dict, slots)
// pair, the dict half updates the instance __dict__, and the slots half
// is restored attribute-by-attribute via setattr.
//
// CPython: Modules/_pickle.c:5752 load_build
func (u *unpickler) loadBuild() error {
	state, err := u.pop()
	if err != nil {
		return err
	}
	if len(u.stack) == 0 {
		return errors.New("UnpicklingError: BUILD on empty stack")
	}
	obj := u.stack[len(u.stack)-1]

	// An explicit __setstate__ owns the whole restore.
	// CPython: Modules/_pickle.c:5772 load_build (setstate branch)
	setstate, err := objects.LookupAttrString(obj, "__setstate__")
	if err != nil {
		return err
	}
	if setstate != nil {
		_, err = objects.CallOneArg(setstate, state)
		return err
	}

	// A default __setstate__: state may carry a slot-state dict too (a
	// protocol 2 addition), as the 2-tuple (state, slotstate).
	// CPython: Modules/_pickle.c:5789 load_build (tuple split)
	var slotstate objects.Object
	if t, ok := state.(*objects.Tuple); ok && t.Len() == 2 {
		state = t.Item(0)
		slotstate = t.Item(1)
	}

	// Set inst.__dict__ from the state dict (if any).
	// CPython: Modules/_pickle.c:5804 load_build (state dict branch)
	if state != nil && state != objects.None() {
		d, ok := state.(*objects.Dict)
		if !ok {
			return errors.New("UnpicklingError: state is not a dictionary")
		}
		instDict, err := objects.GetAttr(obj, objects.NewStr("__dict__"))
		if err != nil {
			return err
		}
		dict, ok := instDict.(*objects.Dict)
		if !ok {
			return errors.New("UnpicklingError: instance __dict__ is not a dictionary")
		}
		for _, k := range d.Keys() {
			v, err := d.GetItem(k)
			if err != nil || v == nil {
				continue
			}
			// Instance-attribute keys are normally interned; mirror that.
			// CPython: Modules/_pickle.c:5826 PyUnicode_InternInPlace
			if _, ok := k.(*objects.Unicode); ok {
				objects.InternInPlace(&k)
			}
			if err := dict.SetItem(k, v); err != nil {
				return err
			}
		}
	}

	// Restore instance attributes from the slot-state dict (if any).
	// CPython: Modules/_pickle.c:5841 load_build (slotstate branch)
	if slotstate != nil && slotstate != objects.None() {
		sd, ok := slotstate.(*objects.Dict)
		if !ok {
			return errors.New("UnpicklingError: slot state is not a dictionary")
		}
		for _, k := range sd.Keys() {
			v, err := sd.GetItem(k)
			if err != nil || v == nil {
				continue
			}
			if err := objects.SetAttr(obj, k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

// loadNewobj implements NEWOBJ: pops (cls, args), calls cls(*args),
// pushes the result.
//
// CPython: Modules/_pickle.c:5760 load_newobj
func (u *unpickler) loadNewobj() error {
	args, err := u.pop()
	if err != nil {
		return err
	}
	cls, err := u.pop()
	if err != nil {
		return err
	}
	argTuple, ok := args.(*objects.Tuple)
	if !ok {
		return errors.New("UnpicklingError: NEWOBJ requires a tuple as args")
	}
	result, err := objects.Call(cls, argTuple, nil)
	if err != nil {
		return fmt.Errorf("UnpicklingError: NEWOBJ call failed: %w", err)
	}
	u.push(result)
	return nil
}

// loadProto consumes the protocol byte and stashes it.
//
// CPython: Modules/_pickle.c:6906 load_proto
func (u *unpickler) loadProto() error {
	b, err := u.readByte()
	if err != nil {
		return err
	}
	if b > highestProtocol {
		return fmt.Errorf("UnpicklingError: unsupported pickle protocol: %d", b)
	}
	u.proto = int(b)
	return nil
}

// loadFrame consumes the 8-byte length and otherwise no-ops. Framing
// is purely a streaming optimization; once the body is in our buffer
// we don't care about frame boundaries.
//
// CPython: Modules/_pickle.c:6925 load_frame
func (u *unpickler) loadFrame() error {
	_, err := u.read(8)
	return err
}

// loadBinintX reads `size` LE bytes. BININT (size 4) is signed,
// BININT1 and BININT2 are unsigned.
//
// CPython: Modules/_pickle.c:5396 load_binintx
func (u *unpickler) loadBinintX(size int, signed bool) error {
	s, err := u.read(size)
	if err != nil {
		return err
	}
	var x int64
	for i := 0; i < size; i++ {
		x |= int64(s[i]) << (8 * i)
	}
	if signed && size == 4 && x&(1<<31) != 0 {
		x |= -(1 << 31)
	}
	u.push(objects.NewInt(x))
	return nil
}

// loadCountedLong reads `size` (1 or 4) LE bytes as the unsigned
// payload length, then `len` LE bytes as a two's-complement integer.
//
// CPython: Modules/_pickle.c:5470 load_counted_long
func (u *unpickler) loadCountedLong(size int) error {
	hdr, err := u.read(size)
	if err != nil {
		return err
	}
	var n int64
	for i := 0; i < size; i++ {
		n |= int64(hdr[i]) << (8 * i)
	}
	if size == 4 && n&(1<<31) != 0 {
		// Negative payload length is a corrupt stream.
		return errors.New("UnpicklingError: LONG pickle has negative byte count")
	}
	if n == 0 {
		u.push(objects.NewInt(0))
		return nil
	}
	payload, err := u.read(int(n))
	if err != nil {
		return err
	}
	be := make([]byte, len(payload))
	for i, b := range payload {
		be[len(payload)-1-i] = b
	}
	negative := be[0]&0x80 != 0
	val := new(big.Int).SetBytes(be)
	if negative {
		// Two's complement: result = SetBytes(be) - 2^(8*len).
		mod := new(big.Int).Lsh(big.NewInt(1), uint(len(be)*8))
		val.Sub(val, mod)
	}
	u.push(objects.NewIntFromBig(val))
	return nil
}

// loadBinfloat reads 8 big-endian IEEE 754 bytes.
//
// CPython: Modules/_pickle.c:5533 load_binfloat
func (u *unpickler) loadBinfloat() error {
	s, err := u.read(8)
	if err != nil {
		return err
	}
	bits := binary.BigEndian.Uint64(s)
	u.push(objects.NewFloat(math.Float64frombits(bits)))
	return nil
}

// loadCountedBinbytes reads `nbytes` LE bytes as the unsigned payload
// length, then the payload itself, and pushes a bytes object.
//
// CPython: Modules/_pickle.c:5637 load_counted_binbytes
func (u *unpickler) loadCountedBinbytes(nbytes int) error {
	hdr, err := u.read(nbytes)
	if err != nil {
		return err
	}
	size, err := calcBinsize(hdr, nbytes)
	if err != nil {
		return err
	}
	payload, err := u.read(size)
	if err != nil {
		return err
	}
	u.push(objects.NewBytes(payload))
	return nil
}

// loadCountedBinunicode reads `nbytes` LE bytes as the unsigned
// payload length, then `size` bytes of UTF-8, and pushes a str.
//
// CPython: Modules/_pickle.c:5768 load_counted_binunicode
func (u *unpickler) loadCountedBinunicode(nbytes int) error {
	hdr, err := u.read(nbytes)
	if err != nil {
		return err
	}
	size, err := calcBinsize(hdr, nbytes)
	if err != nil {
		return err
	}
	payload, err := u.read(size)
	if err != nil {
		return err
	}
	u.push(objects.NewStr(string(payload)))
	return nil
}

// calcBinsize reads an unsigned little-endian integer of `nbytes`
// bytes. CPython folds in an overflow check for BINBYTES8 /
// BINUNICODE8 on 32-bit platforms; we map straight to int.
//
// CPython: Modules/_pickle.c:5341 calc_binsize
func calcBinsize(b []byte, nbytes int) (int, error) {
	var x uint64
	if nbytes > 8 {
		nbytes = 8
	}
	for i := 0; i < nbytes; i++ {
		x |= uint64(b[i]) << (8 * i)
	}
	if x > math.MaxInt {
		return 0, errors.New("OverflowError: pickle payload exceeds max int")
	}
	return int(x), nil
}

// loadCountedTuple slices `n` items off the top of the stack and
// replaces them with a single tuple. Empty tuple is handled inline in
// the dispatch loop (EMPTY_TUPLE pushes the singleton).
//
// CPython: Modules/_pickle.c:5797 load_counted_tuple
func (u *unpickler) loadCountedTuple(n int) error {
	if len(u.stack) < n {
		return errors.New("UnpicklingError: TUPLE underflow")
	}
	items := make([]objects.Object, n)
	copy(items, u.stack[len(u.stack)-n:])
	u.stack = u.stack[:len(u.stack)-n]
	u.push(objects.NewTuple(items))
	return nil
}

// loadTuple consumes the MARK and pops the items between MARK and the
// stack top into a tuple.
//
// CPython: Modules/_pickle.c:5811 load_tuple
func (u *unpickler) loadTuple() error {
	mark, err := u.popMark()
	if err != nil {
		return err
	}
	return u.loadCountedTuple(len(u.stack) - mark)
}

// loadAppend grabs the single item above the list and appends it. The
// single-item form skips MARK so the underlying list is at stack
// height - 2.
//
// CPython: Modules/_pickle.c:6615 load_append + do_append
func (u *unpickler) loadAppend() error {
	if len(u.stack) < 2 {
		return errors.New("UnpicklingError: APPEND underflow")
	}
	item, _ := u.pop()
	list := u.stack[len(u.stack)-1]
	l, ok := list.(*objects.List)
	if !ok {
		return errors.New("UnpicklingError: APPEND target is not a list")
	}
	l.Append(item)
	return nil
}

// loadAppends pops the MARK and extends the list with the items
// between the MARK and the stack top.
//
// CPython: Modules/_pickle.c:6623 load_appends + do_append
func (u *unpickler) loadAppends() error {
	mark, err := u.popMark()
	if err != nil {
		return err
	}
	if mark == 0 || mark > len(u.stack) {
		return errors.New("UnpicklingError: APPENDS underflow")
	}
	items := u.stack[mark:]
	list := u.stack[mark-1]
	l, ok := list.(*objects.List)
	if !ok {
		return errors.New("UnpicklingError: APPENDS target is not a list")
	}
	for _, it := range items {
		l.Append(it)
	}
	u.stack = u.stack[:mark]
	return nil
}

// loadSetitem implements the single-pair SETITEM form. Stack is
// `... dict key value`; pops k+v, sets dict[k] = v.
//
// CPython: Modules/_pickle.c:6669 load_setitem + do_setitems
func (u *unpickler) loadSetitem() error {
	if len(u.stack) < 3 {
		return errors.New("UnpicklingError: SETITEM underflow")
	}
	v, _ := u.pop()
	k, _ := u.pop()
	d := u.stack[len(u.stack)-1]
	dict, ok := d.(*objects.Dict)
	if !ok {
		return errors.New("UnpicklingError: SETITEM target is not a dict")
	}
	return dict.SetItem(k, v)
}

// loadSetitems pops the MARK and stuffs all (k, v) pairs between the
// MARK and the top into the dict that sits just below the MARK.
//
// CPython: Modules/_pickle.c:6675 load_setitems + do_setitems
func (u *unpickler) loadSetitems() error {
	mark, err := u.popMark()
	if err != nil {
		return err
	}
	if mark == 0 || mark > len(u.stack) {
		return errors.New("UnpicklingError: SETITEMS underflow")
	}
	if (len(u.stack)-mark)%2 != 0 {
		return errors.New("UnpicklingError: odd number of items for SETITEMS")
	}
	d := u.stack[mark-1]
	dict, ok := d.(*objects.Dict)
	if !ok {
		return errors.New("UnpicklingError: SETITEMS target is not a dict")
	}
	for i := mark; i < len(u.stack); i += 2 {
		if err := dict.SetItem(u.stack[i], u.stack[i+1]); err != nil {
			return err
		}
	}
	u.stack = u.stack[:mark]
	return nil
}

// loadAdditems pops the MARK and adds the items between the MARK and
// the top into the set that sits just below the MARK.
//
// CPython: Modules/_pickle.c:6685 load_additems
func (u *unpickler) loadAdditems() error {
	mark, err := u.popMark()
	if err != nil {
		return err
	}
	if mark == 0 || mark > len(u.stack) {
		return errors.New("UnpicklingError: ADDITEMS underflow")
	}
	s := u.stack[mark-1]
	set, ok := s.(*objects.Set)
	if !ok {
		return errors.New("UnpicklingError: ADDITEMS target is not a set")
	}
	for i := mark; i < len(u.stack); i++ {
		if err := set.Add(u.stack[i]); err != nil {
			return err
		}
	}
	u.stack = u.stack[:mark]
	return nil
}

// loadFrozenset pops the MARK, collects the items above it into a
// frozenset, and replaces them with the new set.
//
// CPython: Modules/_pickle.c:5903 load_frozenset
func (u *unpickler) loadFrozenset() error {
	mark, err := u.popMark()
	if err != nil {
		return err
	}
	if mark > len(u.stack) {
		return errors.New("UnpicklingError: FROZENSET underflow")
	}
	items := make([]objects.Object, len(u.stack)-mark)
	copy(items, u.stack[mark:])
	u.stack = u.stack[:mark]
	fs, err := objects.NewFrozenset(items)
	if err != nil {
		return err
	}
	u.push(fs)
	return nil
}

// loadMemoize records the top of the stack at the next free memo
// slot (proto >= 4 only). Earlier protocols use BINPUT / LONG_BINPUT
// with an explicit index.
//
// CPython: Modules/_pickle.c:6529 load_memoize
func (u *unpickler) loadMemoize() error {
	if len(u.stack) == 0 {
		return errors.New("UnpicklingError: MEMOIZE on empty stack")
	}
	return u.memoPutAt(len(u.memo), u.stack[len(u.stack)-1])
}

// loadBinget reads one byte as the memo index and pushes the stored
// object.
//
// CPython: Modules/_pickle.c:6314 load_binget
func (u *unpickler) loadBinget() error {
	b, err := u.readByte()
	if err != nil {
		return err
	}
	obj, err := u.memoGet(int(b))
	if err != nil {
		return err
	}
	u.push(obj)
	return nil
}

// loadLongBinget reads 4 LE bytes as the memo index and pushes the
// stored object.
//
// CPython: Modules/_pickle.c:6340 load_long_binget
func (u *unpickler) loadLongBinget() error {
	s, err := u.read(4)
	if err != nil {
		return err
	}
	idx := int(binary.LittleEndian.Uint32(s))
	obj, err := u.memoGet(idx)
	if err != nil {
		return err
	}
	u.push(obj)
	return nil
}

// loadBinput reads one byte as the memo index and records the stack
// top under that index. Used by proto 2 / 3 streams.
//
// CPython: Modules/_pickle.c:6486 load_binput
func (u *unpickler) loadBinput() error {
	b, err := u.readByte()
	if err != nil {
		return err
	}
	if len(u.stack) == 0 {
		return errors.New("UnpicklingError: BINPUT on empty stack")
	}
	return u.memoPutAt(int(b), u.stack[len(u.stack)-1])
}

// loadLongBinput reads 4 LE bytes as the memo index and records the
// stack top under that index.
//
// CPython: Modules/_pickle.c:6505 load_long_binput
func (u *unpickler) loadLongBinput() error {
	s, err := u.read(4)
	if err != nil {
		return err
	}
	idx := int(binary.LittleEndian.Uint32(s))
	if len(u.stack) == 0 {
		return errors.New("UnpicklingError: LONG_BINPUT on empty stack")
	}
	return u.memoPutAt(idx, u.stack[len(u.stack)-1])
}

// loadPut reads a decimal integer followed by '\n' and stores the
// stack top at that memo index. Proto 0 text-mode form.
//
// CPython: Modules/_pickle.c:6471 load_put
func (u *unpickler) loadPut() error {
	line := u.readLine()
	var idx int
	if _, err := fmt.Sscanf(line, "%d", &idx); err != nil {
		return fmt.Errorf("UnpicklingError: PUT index not an integer: %q", line)
	}
	if len(u.stack) == 0 {
		return errors.New("UnpicklingError: PUT on empty stack")
	}
	return u.memoPutAt(idx, u.stack[len(u.stack)-1])
}

// loadGet reads a decimal integer followed by '\n' and pushes the
// object stored at that memo index. Proto 0 text-mode form.
//
// CPython: Modules/_pickle.c:6301 load_get
func (u *unpickler) loadGet() error {
	line := u.readLine()
	var idx int
	if _, err := fmt.Sscanf(line, "%d", &idx); err != nil {
		return fmt.Errorf("UnpicklingError: GET index not an integer: %q", line)
	}
	obj, err := u.memoGet(idx)
	if err != nil {
		return err
	}
	u.push(obj)
	return nil
}

// loadPopMark drops everything above the current MARK and the MARK
// itself.
//
// CPython: Modules/_pickle.c:6253 load_pop_mark
func (u *unpickler) loadPopMark() error {
	mark, err := u.popMark()
	if err != nil {
		return err
	}
	u.stack = u.stack[:mark]
	return nil
}

// loadTextInt reads the decimal-text INT opcode (proto 0).
// "01" and "00" are the canonical True / False encodings.
//
// CPython: Modules/_pickle.c:5396 load_int
func (u *unpickler) loadTextInt() error {
	line := u.readLine()
	switch line {
	case "01":
		u.push(objects.True())
	case "00":
		u.push(objects.False())
	default:
		v, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return fmt.Errorf("UnpicklingError: invalid INT data: %q", line)
		}
		u.push(objects.NewInt(v))
	}
	return nil
}

// loadTextFloat reads the decimal-text FLOAT opcode (proto 0).
//
// CPython: Modules/_pickle.c:5545 load_float
func (u *unpickler) loadTextFloat() error {
	line := u.readLine()
	v, err := strconv.ParseFloat(line, 64)
	if err != nil {
		return fmt.Errorf("UnpicklingError: invalid FLOAT data: %q", line)
	}
	u.push(objects.NewFloat(v))
	return nil
}

// loadTextLong reads the decimal-text LONG opcode (proto 0).
// The line may have a trailing 'L' (Python 2 legacy).
//
// CPython: Modules/_pickle.c:5418 load_long
func (u *unpickler) loadTextLong() error {
	line := u.readLine()
	line = strings.TrimRight(line, "L")
	b := new(big.Int)
	if _, ok := b.SetString(line, 10); !ok {
		return fmt.Errorf("UnpicklingError: invalid LONG data: %q", line)
	}
	u.push(objects.NewIntFromBig(b))
	return nil
}

// loadTextString reads the STRING opcode (proto 0 bytes). The line is
// a Python-repr-escaped string literal enclosed in single or double
// quotes.
//
// CPython: Modules/_pickle.c:5563 load_string
func (u *unpickler) loadTextString() error {
	line := u.readLine()
	if len(line) < 2 {
		return fmt.Errorf("UnpicklingError: invalid STRING data: %q", line)
	}
	q := line[0]
	if (q != '\'' && q != '"') || line[len(line)-1] != q {
		return fmt.Errorf("UnpicklingError: STRING not quoted: %q", line)
	}
	inner := line[1 : len(line)-1]
	decoded, err := unescapePickleString(inner)
	if err != nil {
		return fmt.Errorf("UnpicklingError: STRING decode error: %v", err)
	}
	u.push(objects.NewBytes(decoded))
	return nil
}

// loadTextUnicode reads the UNICODE opcode (proto 0 str).
// The line uses \u, \U, \x, and \\ escapes.
//
// CPython: Modules/_pickle.c:5608 load_unicode
func (u *unpickler) loadTextUnicode() error {
	line := u.readLine()
	decoded, err := unescapePickleUnicode(line)
	if err != nil {
		return fmt.Errorf("UnpicklingError: UNICODE decode error: %v", err)
	}
	u.push(objects.NewStr(decoded))
	return nil
}

// loadDictFromMark pops all items above the MARK and builds a dict.
// Items must be interleaved key/value pairs.
//
// CPython: Modules/_pickle.c:5843 load_dict
func (u *unpickler) loadDictFromMark() error {
	mark, err := u.popMark()
	if err != nil {
		return err
	}
	n := len(u.stack) - mark
	if n%2 != 0 {
		return errors.New("UnpicklingError: odd number of items for DICT")
	}
	d := objects.NewDict()
	for i := mark; i < len(u.stack); i += 2 {
		if err := d.SetItem(u.stack[i], u.stack[i+1]); err != nil {
			return err
		}
	}
	u.stack = u.stack[:mark]
	u.push(d)
	return nil
}

// loadListFromMark pops all items above the MARK and builds a list.
//
// CPython: Modules/_pickle.c:5856 load_list
func (u *unpickler) loadListFromMark() error {
	mark, err := u.popMark()
	if err != nil {
		return err
	}
	items := make([]objects.Object, len(u.stack)-mark)
	copy(items, u.stack[mark:])
	u.stack = u.stack[:mark]
	u.push(objects.NewList(items))
	return nil
}

// unescapePickleString decodes the C-style backslash escapes used by
// the proto 0 STRING opcode.
//
// CPython: Modules/_pickle.c:5563 load_string (uses PyBytes_DecodeEscape)
func unescapePickleString(s string) ([]byte, error) {
	var out []byte
	for i := 0; i < len(s); {
		if s[i] != '\\' {
			out = append(out, s[i])
			i++
			continue
		}
		if i+1 >= len(s) {
			return nil, errors.New("trailing backslash")
		}
		switch s[i+1] {
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case '\\':
			out = append(out, '\\')
		case '\'':
			out = append(out, '\'')
		case '"':
			out = append(out, '"')
		case 'x':
			if i+3 >= len(s) {
				return nil, errors.New("short \\x escape")
			}
			v, err := strconv.ParseUint(s[i+2:i+4], 16, 8)
			if err != nil {
				return nil, err
			}
			out = append(out, byte(v))
			i += 4
			continue
		default:
			out = append(out, s[i+1])
		}
		i += 2
	}
	return out, nil
}

// unescapePickleUnicode decodes the \uXXXX / \UXXXXXXXX / \xXX / \\
// escapes used by the proto 0 UNICODE opcode.
//
// CPython: Modules/_pickle.c:5608 load_unicode (uses PyUnicode_DecodeRawUnicodeEscape)
func unescapePickleUnicode(s string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\\' {
			out.WriteByte(s[i])
			i++
			continue
		}
		if i+1 >= len(s) {
			return "", errors.New("trailing backslash")
		}
		switch s[i+1] {
		case 'u':
			if i+5 > len(s) {
				return "", errors.New("short \\u escape")
			}
			v, err := strconv.ParseUint(s[i+2:i+6], 16, 16)
			if err != nil {
				return "", err
			}
			out.WriteRune(rune(v))
			i += 6
			continue
		case 'U':
			if i+9 > len(s) {
				return "", errors.New("short \\U escape")
			}
			v, err := strconv.ParseUint(s[i+2:i+10], 16, 32)
			if err != nil {
				return "", err
			}
			out.WriteRune(rune(v))
			i += 10
			continue
		case 'x':
			if i+3 > len(s) {
				return "", errors.New("short \\x escape")
			}
			v, err := strconv.ParseUint(s[i+2:i+4], 16, 8)
			if err != nil {
				return "", err
			}
			out.WriteRune(rune(v))
			i += 4
			continue
		case '\\':
			out.WriteByte('\\')
		default:
			out.WriteByte('\\')
			out.WriteByte(s[i+1])
		}
		i += 2
	}
	return out.String(), nil
}

// loadsAtom is the round-trip Phase 5 driver: reads the byte slice
// straight through the dispatch loop and returns the top of the stack
// when STOP fires.
//
// CPython: Modules/_pickle.c:8002 _pickle_loads_impl (data path)
func loadsAtom(buf []byte) (objects.Object, error) {
	u := newUnpickler(buf)
	return u.load()
}
