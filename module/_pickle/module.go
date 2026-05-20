// Package _pickle is the gopy port of CPython's _pickle built-in
// module. Phase 1 lands the scaffolding: the wire-protocol opcode
// table, the protocol-version constants, and the three exception
// classes (PickleError, PicklingError, UnpicklingError). The
// Pickler / Unpickler / PickleBuffer types and the dump / dumps /
// load / loads entry points are deferred to later phases.
//
// pickle.py imports the C-accelerated names inside a try/except
// ImportError, so the module is allowed to expose only a subset
// during the staged port: the second `from _pickle import (...)`
// statement fails on the missing Pickler symbol, the except branch
// fires, and pickle.py keeps using its pure-Python _Pickler /
// _Unpickler classes. The early `from _pickle import PickleBuffer`
// also falls back cleanly.
//
// CPython: Modules/_pickle.c:1 _pickle module
package _pickle

import (
	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

func init() {
	_ = imp.AppendInittab("_pickle", buildModule)
}

// ---------------------------------------------------------------------------
// Protocol version.
//
// HIGHEST_PROTOCOL is the newest protocol the implementation understands;
// DEFAULT_PROTOCOL is what dumps() / Pickler() pick when no protocol
// argument is supplied. Both are exposed as module attributes and are
// read by pickle.py to advertise its own ceiling.
//
// CPython: Modules/_pickle.c:43 HIGHEST_PROTOCOL / DEFAULT_PROTOCOL
// ---------------------------------------------------------------------------

const (
	highestProtocol = 5
	defaultProtocol = 5
)

// ---------------------------------------------------------------------------
// Pickle opcodes. The one-byte tags the wire format uses to dispatch on
// type. Kept in the same order as the CPython enum so a reader can map
// each Go constant onto its C origin by line number.
//
// CPython: Modules/_pickle.c:63 enum opcode
// ---------------------------------------------------------------------------

const (
	opMark           byte = '('
	opStop           byte = '.'
	opPop            byte = '0'
	opPopMark        byte = '1'
	opDup            byte = '2'
	opFloat          byte = 'F'
	opInt            byte = 'I'
	opBinint         byte = 'J'
	opBinint1        byte = 'K'
	opLong           byte = 'L'
	opBinint2        byte = 'M'
	opNone           byte = 'N'
	opPersid         byte = 'P'
	opBinpersid      byte = 'Q'
	opReduce         byte = 'R'
	opString         byte = 'S'
	opBinstring      byte = 'T'
	opShortBinstring byte = 'U'
	opUnicode        byte = 'V'
	opBinunicode     byte = 'X'
	opAppend         byte = 'a'
	opBuild          byte = 'b'
	opGlobal         byte = 'c'
	opDict           byte = 'd'
	opEmptyDict      byte = '}'
	opAppends        byte = 'e'
	opGet            byte = 'g'
	opBinget         byte = 'h'
	opInst           byte = 'i'
	opLongBinget     byte = 'j'
	opList           byte = 'l'
	opEmptyList      byte = ']'
	opObj            byte = 'o'
	opPut            byte = 'p'
	opBinput         byte = 'q'
	opLongBinput     byte = 'r'
	opSetitem        byte = 's'
	opTuple          byte = 't'
	opEmptyTuple     byte = ')'
	opSetitems       byte = 'u'
	opBinfloat       byte = 'G'
)

// Protocol 2.
// CPython: Modules/_pickle.c:106 protocol 2 opcodes
const (
	opProto    byte = '\x80'
	opNewobj   byte = '\x81'
	opExt1     byte = '\x82'
	opExt2     byte = '\x83'
	opExt4     byte = '\x84'
	opTuple1   byte = '\x85'
	opTuple2   byte = '\x86'
	opTuple3   byte = '\x87'
	opNewtrue  byte = '\x88'
	opNewfalse byte = '\x89'
	opLong1    byte = '\x8a'
	opLong4    byte = '\x8b'
)

// Protocol 3.
// CPython: Modules/_pickle.c:120 protocol 3 opcodes
const (
	opBinbytes      byte = 'B'
	opShortBinbytes byte = 'C'
)

// Protocol 4.
// CPython: Modules/_pickle.c:124 protocol 4 opcodes
const (
	opShortBinunicode byte = '\x8c'
	opBinunicode8     byte = '\x8d'
	opBinbytes8       byte = '\x8e'
	opEmptySet        byte = '\x8f'
	opAdditems        byte = '\x90'
	opFrozenset       byte = '\x91'
	opNewobjEx        byte = '\x92'
	opStackGlobal     byte = '\x93'
	opMemoize         byte = '\x94'
	opFrame           byte = '\x95'
)

// Protocol 5.
// CPython: Modules/_pickle.c:136 protocol 5 opcodes
const (
	opBytearray8     byte = '\x96'
	opNextBuffer     byte = '\x97'
	opReadonlyBuffer byte = '\x98'
)

// ---------------------------------------------------------------------------
// Sizing knobs. None are Python-visible in CPython; they're internal
// tuning constants the Pickler / Unpickler reach for. Kept here so the
// later phases can reference them without re-deriving the numbers.
//
// CPython: Modules/_pickle.c:142 enum (BATCHSIZE / WRITE_BUF_SIZE / ...)
// ---------------------------------------------------------------------------

const (
	batchSize        = 1000
	fastNestingLimit = 50
	writeBufSize     = 4096
	prefetch         = 8192 * 16
	frameSizeMin     = 4
	frameSizeTarget  = 64 * 1024
	frameHeaderSize  = 9
)

// ---------------------------------------------------------------------------
// Exception classes. CPython publishes three: PickleError is the common
// base, PicklingError is raised by the Pickler, UnpicklingError by the
// Unpickler. All three need to be present on the module so the staged
// `from _pickle import (...)` in pickle.py at least lets PickleError /
// PicklingError / UnpicklingError resolve before the missing Pickler
// symbol trips the ImportError.
//
// CPython: Modules/_pickle.c:168 PickleState.PickleError
// ---------------------------------------------------------------------------

var (
	pickleErrorType     *objects.Type
	picklingErrorType   *objects.Type
	unpicklingErrorType *objects.Type
)

func init() {
	pickleErrorType = objects.NewType("PickleError", []*objects.Type{objects.ObjectType()})
	picklingErrorType = objects.NewType("PicklingError", []*objects.Type{pickleErrorType})
	unpicklingErrorType = objects.NewType("UnpicklingError", []*objects.Type{pickleErrorType})
}

// buildModule constructs the _pickle module dict.
//
// CPython: Modules/_pickle.c:7700 _pickle_exec (module init body)
func buildModule() (*objects.Module, error) {
	m := objects.NewModule("_pickle")
	d := m.Dict()

	// Protocol version constants.
	// CPython: Modules/_pickle.c:7711 _PyModule_Add (HIGHEST_PROTOCOL / DEFAULT_PROTOCOL)
	if err := d.SetItem(objects.NewStr("HIGHEST_PROTOCOL"), objects.NewInt(highestProtocol)); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("DEFAULT_PROTOCOL"), objects.NewInt(defaultProtocol)); err != nil {
		return nil, err
	}

	// Exception classes.
	// CPython: Modules/_pickle.c:7720 PyModule_AddType (PickleError / PicklingError / UnpicklingError)
	if err := d.SetItem(objects.NewStr("PickleError"), pickleErrorType); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("PicklingError"), picklingErrorType); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("UnpicklingError"), unpicklingErrorType); err != nil {
		return nil, err
	}

	return m, nil
}
