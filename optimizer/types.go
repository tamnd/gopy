// Package optimizer hosts the Tier-2 trace optimizer and uop interpreter.
//
// CPython 3.14 splits the runtime into two tiers. Tier-1 is the
// adaptive specializer that runs over conventional bytecode (lives in
// vm/ and specialize/). Tier-2 is a linear trace of micro-ops that the
// runtime projects out of warmed-up Tier-1 code, runs through an
// abstract interpreter, and dispatches via a separate uop loop. This
// package owns the trace + uop side. The matching jit.c path is
// explicitly out of scope; gopy stays interpreter-only.
//
// CPython: Include/internal/pycore_optimizer.h
package optimizer

import (
	"github.com/tamnd/gopy/objects"
)

// UOPMaxTraceLength caps the number of uops in a single projected
// trace. Trace projection stops when the buffer fills up.
//
// CPython: pycore_optimizer.h:121 UOP_MAX_TRACE_LENGTH
const UOPMaxTraceLength = 800

// TraceStackSize bounds the depth of inlined frames the projection
// will follow through PUSH_FRAME.
//
// CPython: pycore_optimizer.h:123 TRACE_STACK_SIZE
const TraceStackSize = 5

// MaxAbstractInterpSize bounds the locals + stack + consts buffer the
// abstract interpreter walks during analysis.
//
// CPython: pycore_optimizer.h:154 MAX_ABSTRACT_INTERP_SIZE
const MaxAbstractInterpSize = 4096

// TyArenaSize caps the per-trace symbolic-type arena.
//
// CPython: pycore_optimizer.h:156 TY_ARENA_SIZE
const TyArenaSize = UOPMaxTraceLength * 5

// MaxAbstractFrameDepth bounds the abstract-interpreter frame stack.
// One slot for the root frame, one for the overflow frame, the rest
// for inlined PUSH_FRAME calls.
//
// CPython: pycore_optimizer.h:159 MAX_ABSTRACT_FRAME_DEPTH
const MaxAbstractFrameDepth = TraceStackSize + 2

// MaxChainDepth bounds the number of side exits a trace tree may take
// before requiring forward progress through a fresh ENTER_EXECUTOR.
//
// CPython: pycore_optimizer.h:165 MAX_CHAIN_DEPTH
const MaxChainDepth = 4

// JITCleanupThreshold is the trace-run-counter threshold past which
// the runtime invalidates cold executors.
//
// CPython: pycore_optimizer.h:118 JIT_CLEANUP_THRESHOLD
const JITCleanupThreshold = 100000

// ExecutorDeleteListMax caps the pending-deletion list before a sweep
// runs. A separate list keeps us from running tp_dealloc while another
// thread is executing the trace.
//
// CPython: pycore_optimizer.h:90 EXECUTOR_DELETE_LIST_MAX
const ExecutorDeleteListMax = 100

// MaxAllowedBuiltinsModifications and MaxAllowedGlobalsModifications
// cap how many times the runtime tolerates builtins/globals mutating
// before it stops trying to specialize globals loads.
//
// CPython: pycore_optimizer.h:101-102
const (
	MaxAllowedBuiltinsModifications = 3
	MaxAllowedGlobalsModifications  = 6
)

// UOPFormatTarget and UOPFormatJump pick which interpretation of the
// 32-bit field between oparg and operand applies to a given uop.
//
// CPython: pycore_optimizer.h:132-133
const (
	UOPFormatTarget = 0
	UOPFormatJump   = 1
)

// BloomFilterWords is the bloom filter's width in 32-bit words. A
// width of 8 gives m = 256 bits; trace projection adds every guarded
// type / dict / code pointer, the invalidation walk hashes a mutated
// pointer once and asks every executor whether it might be in scope.
//
// CPython: pycore_optimizer.h:24 _Py_BLOOM_FILTER_WORDS
const BloomFilterWords = 8

// BloomFilter packs eight 32-bit words. False positives are tolerable;
// false negatives would skip a needed invalidation.
//
// CPython: pycore_optimizer.h:26-28 _PyBloomFilter
type BloomFilter struct {
	Bits [BloomFilterWords]uint32
}

// UOPInstruction is one row of an executor's trace. The opcode is a
// uint16 from the uop ID table (see uop_ids_gen.go). The format bit
// distinguishes plain Tier-1-offset targets from split jump/error
// targets used by exit-arm uops. operand0 carries cache entries and
// constant pointers; operand1 is reserved for the second-cache-entry
// uops the upstream JIT uses (gopy stays interpreter-only but mirrors
// the field for trace-shape parity).
//
// CPython: pycore_optimizer.h:51-67 _PyUOpInstruction
type UOPInstruction struct {
	// Opcode holds the uop ID in the low 15 bits and the format bit
	// in the high bit. Use Opcode() and Format() to read them.
	OpcodeAndFormat uint16
	Oparg           uint16
	// Target is the Tier-1 bytecode offset of the source opcode when
	// Format == UOPFormatTarget, or jump_target | (error_target<<16)
	// when Format == UOPFormatJump.
	Target   uint32
	Operand0 uint64
	Operand1 uint64
}

// Opcode returns the uop ID stored in OpcodeAndFormat's low 15 bits.
//
// CPython: pycore_optimizer.h:52 opcode:15
func (u *UOPInstruction) Opcode() uint16 {
	return u.OpcodeAndFormat & 0x7FFF
}

// SetOpcode writes the uop ID, preserving the format bit.
func (u *UOPInstruction) SetOpcode(op uint16) {
	u.OpcodeAndFormat = (u.OpcodeAndFormat & 0x8000) | (op & 0x7FFF)
}

// Format returns the format bit (UOPFormatTarget or UOPFormatJump).
//
// CPython: pycore_optimizer.h:53 format:1
func (u *UOPInstruction) Format() uint16 {
	return (u.OpcodeAndFormat >> 15) & 1
}

// SetFormat writes the format bit, preserving the opcode.
func (u *UOPInstruction) SetFormat(fmt uint16) {
	u.OpcodeAndFormat = (u.OpcodeAndFormat & 0x7FFF) | ((fmt & 1) << 15)
}

// GetTarget returns the Tier-1 bytecode offset for a plain target uop.
//
// CPython: pycore_optimizer.h:135-139 uop_get_target
func (u *UOPInstruction) GetTarget() uint32 {
	return u.Target
}

// GetJumpTarget returns the in-trace jump target for a JUMP-format uop.
//
// CPython: pycore_optimizer.h:141-145 uop_get_jump_target
func (u *UOPInstruction) GetJumpTarget() uint16 {
	return uint16(u.Target & 0xFFFF)
}

// GetErrorTarget returns the in-trace error-handler target for a
// JUMP-format uop.
//
// CPython: pycore_optimizer.h:147-151 uop_get_error_target
func (u *UOPInstruction) GetErrorTarget() uint16 {
	return uint16((u.Target >> 16) & 0xFFFF)
}

// ExecutorLinkListNode holds the doubly-linked thread-list pointers
// the per-interpreter executor list uses.
//
// CPython: pycore_optimizer.h:16-19 _PyExecutorLinkListNode
type ExecutorLinkListNode struct {
	Next *Executor
	Prev *Executor
}

// VMData is the slice of an executor the VM mutates: pointers back to
// the bytecode site that installed the executor, the bloom filter the
// invalidation path reads, and the link-list node.
//
// CPython: pycore_optimizer.h:30-41 _PyVMData
type VMData struct {
	// Opcode and Oparg hold the original Tier-1 bytecode at the
	// install site so deopt can restore them.
	Opcode uint8
	Oparg  uint8
	// Valid is cleared when the executor is invalidated.
	Valid bool
	// Linked is set while the executor is on the per-interpreter
	// list.
	Linked bool
	// ChainDepth tracks how far this trace is from the root through
	// chained side exits (max MaxChainDepth - 1).
	ChainDepth uint8
	// Warm is set once the trace has run enough times to be worth
	// keeping cold-pruning at bay.
	Warm bool
	// Index is the slot in Code.Executors that holds this executor.
	Index int
	// Bloom is the filter over types/dicts/code objects this trace
	// guards against.
	Bloom BloomFilter
	// Links connects the executor to the per-interpreter list.
	Links ExecutorLinkListNode
	// Code is a weak back-pointer to the install site; nil if no
	// ENTER_EXECUTOR currently routes here.
	Code *objects.Code
}

// ExitData holds one trace-exit slot. Each guard that might bail to
// Tier-1 owns one of these. Temperature drives whether the exit's own
// trace gets compiled when the exit fires often enough.
//
// CPython: pycore_optimizer.h:69-73 _PyExitData
type ExitData struct {
	// Target is the Tier-1 bytecode offset to resume at.
	Target uint32
	// Temperature is the side-exit warmup counter.
	Temperature BackoffCounter
	// Executor is the side-exit's own trace, populated lazily when
	// the exit warms up.
	Executor *Executor
}

// BackoffCounter is the same value-and-backoff scheme the adaptive
// specializer uses for its per-instruction warmup counters. Exposed
// here so trace exits can share the shape.
//
// CPython: Include/internal/pycore_backoff.h _Py_BackoffCounter
type BackoffCounter struct {
	ValueAndBackoff uint16
}

// Executor is the v0.12 unit of work. One executor owns one linear
// trace of uops projected out of a warm Tier-1 region. The Python
// object surface (lives in pyobject.go) makes Executor round-trip
// through dis.dis and the debugger.
//
// CPython: pycore_optimizer.h:75-85 _PyExecutorObject
type Executor struct {
	objects.VarHeader
	// Trace is the post-analysis uop buffer. Length is in VarHeader.
	Trace []UOPInstruction
	// VMData is the slice the dispatch loop mutates.
	VMData VMData
	// ExitCount is len(Exits); the JIT path uses this for chain
	// bookkeeping.
	ExitCount uint32
	// CodeSize is len(Trace) at allocation time.
	CodeSize uint32
	// Exits holds one ExitData per side-exit guard. Variable-length;
	// the slot count is fixed at allocation.
	Exits []ExitData
}

// ExecutorArray is the side table that hangs off Code. Each entry
// carries one ENTER_EXECUTOR routing for a given bytecode offset; the
// dispatch loop reads code.Executors[oparg] when it hits
// ENTER_EXECUTOR.
//
// CPython: optimizer.c:34-193 (the side-table helpers)
type ExecutorArray struct {
	Capacity int
	Size     int
	Entries  []*Executor
}

// JitSymType enumerates the symbolic-state tags the abstract
// interpreter operates on. The values match CPython byte for byte so
// gate fixtures that assert on tag values stay portable.
//
// CPython: pycore_optimizer.h:171-181 _JitSymType
type JitSymType uint8

const (
	JitSymUnknown     JitSymType = 1
	JitSymNull        JitSymType = 2
	JitSymNonNull     JitSymType = 3
	JitSymBottom      JitSymType = 4
	JitSymTypeVersion JitSymType = 5
	JitSymKnownClass  JitSymType = 6
	JitSymKnownValue  JitSymType = 7
	JitSymTuple       JitSymType = 8
	JitSymTruthiness  JitSymType = 9
)

// MaxSymbolicTupleSize bounds the length of a tuple the symbolic
// interpreter will track elementwise. Past this length the tuple
// degrades to type-only.
//
// CPython: pycore_optimizer.h:199 MAX_SYMBOLIC_TUPLE_SIZE
const MaxSymbolicTupleSize = 7

// JitOptKnownClass is the "definitely an instance of T, possibly with
// version v pinned" symbol payload.
//
// CPython: pycore_optimizer.h:183-187 _jit_opt_known_class
type JitOptKnownClass struct {
	Tag     uint8
	Version uint32
	Type    *objects.Type
}

// JitOptKnownVersion is the "type version pinned but type identity
// unknown" symbol payload. Appears after _GUARD_TYPE_VERSION fires.
//
// CPython: pycore_optimizer.h:189-192 _jit_opt_known_version
type JitOptKnownVersion struct {
	Tag     uint8
	Version uint32
}

// JitOptKnownValue is the "constant" symbol payload, holding the
// concrete object the analyzer proved is on the stack.
//
// CPython: pycore_optimizer.h:194-197 _jit_opt_known_value
type JitOptKnownValue struct {
	Tag   uint8
	Value objects.Object
}

// JitOptTuple holds element-wise symbol indices for a small tuple.
//
// CPython: pycore_optimizer.h:201-205 _jit_opt_tuple
type JitOptTuple struct {
	Tag    uint8
	Length uint8
	Items  [MaxSymbolicTupleSize]uint16
}

// JitOptTruthiness encodes "this symbol's truthiness equals (maybe
// inverted) some other symbol's truthiness".
//
// CPython: pycore_optimizer.h:207-211 (anonymous struct)
type JitOptTruthiness struct {
	Tag    uint8
	Invert bool
	Value  uint16
}

// JitOptSymbol is the tagged-union one-of for the symbolic lattice.
// CPython packs this as a C union; Go's lack of unions forces a
// struct that holds every payload variant. The Tag field picks which
// payload is live.
//
// CPython: pycore_optimizer.h:213-220 _jit_opt_symbol
type JitOptSymbol struct {
	Tag        uint8
	Class      JitOptKnownClass
	Value      JitOptKnownValue
	Version    JitOptKnownVersion
	Tuple      JitOptTuple
	Truthiness JitOptTruthiness
}

// AbstractFrame mirrors one stack frame inside the abstract
// interpreter. Locals and stack point into the JitOptContext's
// per-trace pool.
//
// CPython: pycore_optimizer.h:224-232 _Py_UOpsAbstractFrame
type AbstractFrame struct {
	StackLen     int
	LocalsLen    int
	StackPointer int // index into Ctx.LocalsAndStack
	Stack        int // index into Ctx.LocalsAndStack
	Locals       int // index into Ctx.LocalsAndStack
}

// TyArena is the per-trace bump arena that backs every JitOptSymbol.
//
// CPython: pycore_optimizer.h:236-240 ty_arena
type TyArena struct {
	CurrNumber int
	MaxNumber  int
	Arena      [TyArenaSize]JitOptSymbol
}

// JitOptContext threads through the analysis pass. Holds the symbol
// arena, the abstract-frame stack, and the live locals+stack window.
//
// CPython: pycore_optimizer.h:242-257 _JitOptContext
type JitOptContext struct {
	Done           bool
	OutOfSpace     bool
	Contradiction  bool
	Frame          *AbstractFrame
	Frames         [MaxAbstractFrameDepth]AbstractFrame
	CurrFrameDepth int
	TArena         TyArena
	NConsumed      int // index into LocalsAndStack
	Limit          int // index into LocalsAndStack
	LocalsAndStack [MaxAbstractInterpSize]*JitOptSymbol
}

// IsTerminator reports whether u closes a projected trace. The trace
// driver stops dispatch on these.
//
// CPython: pycore_optimizer.h:301-308 is_terminator
func (u *UOPInstruction) IsTerminator() bool {
	op := u.Opcode()
	return op == UopExitTrace || op == UopJumpToTop
}
