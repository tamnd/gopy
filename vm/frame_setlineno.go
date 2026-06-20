// Settable frame.f_lineno: the debugger line-jump feature. A trace
// function assigns frame.f_lineno to relocate the running frame's
// instruction pointer to a chosen source line. The jump is only legal
// when the evaluation stack at the target offset is compatible with the
// stack at the current offset, so the bulk of this file models the
// abstract stack across the whole code object (mark_stacks) and then
// finds the deepest compatible target on the requested line.
//
// CPython: Objects/frameobject.c:1640 frame_lineno_set_impl and the
// helper block above it (mark_stacks, compatible_stack, marklines,
// first_line_not_before).

package vm

import (
	"fmt"

	warnings "github.com/tamnd/gopy/module/_warnings"

	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/errors"
	"github.com/tamnd/gopy/frame"
	"github.com/tamnd/gopy/monitor"
	"github.com/tamnd/gopy/objects"
	"github.com/tamnd/gopy/stackref"
	"github.com/tamnd/gopy/state"
)

// Abstract stack-entry kinds. The evaluation stack is modelled as a
// 64-bit integer of 3-bit blocks, one per live value, so jump
// compatibility reduces to integer comparison.
//
// CPython: Objects/frameobject.c:1166 typedef enum kind
type stackKind int64

const (
	kindIterator stackKind = 1
	kindExcept   stackKind = 2
	kindObject   stackKind = 3
	kindNull     stackKind = 4
	kindLasti    stackKind = 5
)

const (
	bitsPerBlock    = 3
	stackMask       = (1 << bitsPerBlock) - 1
	maxStackEntries = 63 / bitsPerBlock
	willOverflow    = int64(1) << uint((maxStackEntries-1)*bitsPerBlock)

	stackUninitialized int64 = -2
	stackOverflowed    int64 = -1
	stackEmpty         int64 = 0
)

// compatibleKind reports whether a value of kind from can satisfy a
// slot expecting kind to.
//
// CPython: Objects/frameobject.c:1175 compatible_kind
func compatibleKind(from, to stackKind) bool {
	if to == 0 {
		return false
	}
	if to == kindObject {
		return from != kindNull
	}
	if to == kindNull {
		return true
	}
	return from == to
}

// CPython: Objects/frameobject.c:1199 push_value
func pushValue(stack int64, kind stackKind) int64 {
	if uint64(stack) >= uint64(willOverflow) {
		return stackOverflowed
	}
	return (stack << bitsPerBlock) | int64(kind)
}

// CPython: Objects/frameobject.c:1209 pop_value (arithmetic right shift)
func popValue(stack int64) int64 {
	return stack >> bitsPerBlock
}

// CPython: Objects/frameobject.c:1217 top_of_stack
func topOfStack(stack int64) stackKind {
	return stackKind(stack & stackMask)
}

// CPython: Objects/frameobject.c:1223 peek
func peekStack(stack int64, n int) stackKind {
	return stackKind((stack >> uint(bitsPerBlock*(n-1))) & stackMask)
}

// CPython: Objects/frameobject.c:1230 stack_swap
func stackSwap(stack int64, n int) int64 {
	toSwap := peekStack(stack, n)
	top := topOfStack(stack)
	shift := uint(bitsPerBlock * (n - 1))
	replacedLow := (stack &^ (int64(stackMask) << shift)) | (int64(top) << shift)
	replacedTop := (replacedLow &^ int64(stackMask)) | int64(toSwap)
	return replacedTop
}

// CPython: Objects/frameobject.c:1243 pop_to_level
func popToLevel(stack int64, level int) int64 {
	if level == 0 {
		return stackEmpty
	}
	maxItem := int64((1 << bitsPerBlock) - 1)
	levelMaxStack := maxItem << uint((level-1)*bitsPerBlock)
	for stack > levelMaxStack {
		stack = popValue(stack)
	}
	return stack
}

// markStacks models the abstract evaluation stack at every code-unit
// offset of code, returning a slice of length len+1 (one extra slot
// for the fall-through past the last instruction). Each entry is either
// a packed stack, stackUninitialized (unreachable), or stackOverflowed.
//
// CPython: Objects/frameobject.c:1308 mark_stacks
func markStacks(code *objects.Code, length int) []int64 {
	stacks := make([]int64, length+1)
	for i := 1; i <= length; i++ {
		stacks[i] = stackUninitialized
	}
	stacks[0] = stackEmpty

	cacheFor := func(op compile.Opcode) int { return compile.CacheCount(op) }

	todo := true
	for todo {
		todo = false
		// Scan instructions.
		for i := 0; i < length; {
			nextStack := stacks[i]
			opcode := monitor.GetBaseCodeUnit(code, i)
			oparg := 0
			// Consume any EXTENDED_ARG prefix run.
			for opcode == compile.EXTENDED_ARG {
				oparg = (oparg << 8) | int(code.Code[2*i+1])
				i++
				opcode = monitor.GetBaseCodeUnit(code, i)
				stacks[i] = nextStack
			}
			oparg = (oparg << 8) | int(code.Code[2*i+1])
			nextI := i + cacheFor(opcode) + 1
			if nextStack == stackUninitialized {
				i = nextI
				continue
			}
			switch opcode {
			case compile.POP_JUMP_IF_FALSE, compile.POP_JUMP_IF_TRUE,
				compile.POP_JUMP_IF_NONE, compile.POP_JUMP_IF_NOT_NONE:
				j := nextI + oparg
				nextStack = popValue(nextStack)
				stacks[j] = nextStack
				stacks[nextI] = nextStack
			case compile.SEND:
				j := oparg + i + cacheFor(compile.SEND) + 1
				stacks[j] = nextStack
				stacks[nextI] = nextStack
			case compile.JUMP_FORWARD:
				j := oparg + i + 1
				stacks[j] = nextStack
			case compile.JUMP_BACKWARD, compile.JUMP_BACKWARD_NO_INTERRUPT:
				j := nextI - oparg
				if stacks[j] == stackUninitialized && j < i {
					todo = true
				}
				stacks[j] = nextStack
			case compile.GET_ITER, compile.GET_AITER:
				nextStack = pushValue(popValue(nextStack), kindIterator)
				stacks[nextI] = nextStack
			case compile.FOR_ITER:
				targetStack := pushValue(nextStack, kindObject)
				stacks[nextI] = targetStack
				j := oparg + 1 + cacheFor(compile.FOR_ITER) + i
				stacks[j] = targetStack
			case compile.END_ASYNC_FOR:
				nextStack = popValue(popValue(nextStack))
				stacks[nextI] = nextStack
			case compile.PUSH_EXC_INFO:
				nextStack = pushValue(nextStack, kindExcept)
				stacks[nextI] = nextStack
			case compile.POP_EXCEPT:
				nextStack = popValue(nextStack)
				stacks[nextI] = nextStack
			case compile.RETURN_VALUE:
				// End of block.
			case compile.RAISE_VARARGS:
				// End of block.
			case compile.RERAISE:
				// End of block.
			case compile.PUSH_NULL:
				nextStack = pushValue(nextStack, kindNull)
				stacks[nextI] = nextStack
			case compile.LOAD_GLOBAL:
				nextStack = pushValue(nextStack, kindObject)
				if oparg&1 != 0 {
					nextStack = pushValue(nextStack, kindNull)
				}
				stacks[nextI] = nextStack
			case compile.LOAD_ATTR:
				if oparg&1 != 0 {
					nextStack = popValue(nextStack)
					nextStack = pushValue(nextStack, kindObject)
					nextStack = pushValue(nextStack, kindNull)
				}
				stacks[nextI] = nextStack
			case compile.SWAP:
				nextStack = stackSwap(nextStack, oparg)
				stacks[nextI] = nextStack
			case compile.COPY:
				nextStack = pushValue(nextStack, peekStack(nextStack, oparg))
				stacks[nextI] = nextStack
			default:
				delta := compile.OpcodeStackEffectWithJump(int(opcode), int32(oparg), 0)
				for delta < 0 {
					nextStack = popValue(nextStack)
					delta++
				}
				for delta > 0 {
					nextStack = pushValue(nextStack, kindObject)
					delta--
				}
				stacks[nextI] = nextStack
			}
			i = nextI
		}
		// Scan the exception table: a reachable protected region seeds
		// its handler's stack with the unwound level plus the pushed
		// Lasti / Except entries.
		tab := code.ExceptionTable
		pos := 0
		for pos < len(tab) {
			if tab[pos]&excEntryStartBit == 0 {
				pos++
				continue
			}
			startOffset, n, ok := readExcVarint(tab, pos)
			if !ok {
				break
			}
			pos += n
			_, n, ok = readExcVarint(tab, pos) // size, unused here
			if !ok {
				break
			}
			pos += n
			handler, n, ok := readExcVarint(tab, pos)
			if !ok {
				break
			}
			pos += n
			depthLasti, n, ok := readExcVarint(tab, pos)
			if !ok {
				break
			}
			pos += n
			level := depthLasti >> 1
			lasti := depthLasti & 1
			if startOffset < len(stacks) && stacks[startOffset] != stackUninitialized {
				if handler < len(stacks) && stacks[handler] == stackUninitialized {
					todo = true
					targetStack := popToLevel(stacks[startOffset], level)
					if lasti != 0 {
						targetStack = pushValue(targetStack, kindLasti)
					}
					targetStack = pushValue(targetStack, kindExcept)
					stacks[handler] = targetStack
				}
			}
		}
	}
	return stacks
}

// compatibleStack reports whether a jump from fromStack to toStack is
// legal: the source stack must reduce to the target stack with every
// surviving slot kind-compatible.
//
// CPython: Objects/frameobject.c:1522 compatible_stack
func compatibleStack(fromStack, toStack int64) bool {
	if fromStack < 0 || toStack < 0 {
		return false
	}
	for fromStack > toStack {
		fromStack = popValue(fromStack)
	}
	for fromStack != 0 {
		fromTop := topOfStack(fromStack)
		toTop := topOfStack(toStack)
		if !compatibleKind(fromTop, toTop) {
			return false
		}
		fromStack = popValue(fromStack)
		toStack = popValue(toStack)
	}
	return toStack == 0
}

// explainIncompatibleStack returns the diagnostic for an illegal jump
// target.
//
// CPython: Objects/frameobject.c:1543 explain_incompatible_stack
func explainIncompatibleStack(toStack int64) string {
	if toStack == stackOverflowed {
		return "stack is too deep to analyze"
	}
	if toStack == stackUninitialized {
		return "can't jump into an exception handler, or code may be unreachable"
	}
	switch topOfStack(toStack) {
	case kindExcept:
		return "can't jump into an 'except' block as there's no exception"
	case kindLasti:
		return "can't jump into a re-raising block as there's no location"
	case kindObject, kindNull:
		return "incompatible stacks"
	case kindIterator:
		return "can't jump into the body of a for loop"
	default:
		return "incompatible stacks"
	}
}

// marklines returns, for each code-unit offset, the source line that
// starts there, or -1. Mirrors CPython walking the line table and
// recording a line only at the first offset where it changes.
//
// CPython: Objects/frameobject.c:1569 marklines
func marklines(code *objects.Code, length int) []int {
	linestarts := make([]int, length)
	for i := range linestarts {
		linestarts[i] = -1
	}
	last := -1
	for _, e := range objects.CoLines(code) {
		idx := e.Start / 2
		if e.Line != last && e.Line != -1 && idx >= 0 && idx < length {
			linestarts[idx] = e.Line
			last = e.Line
		}
	}
	return linestarts
}

// CPython: Objects/frameobject.c:1595 first_line_not_before
func firstLineNotBefore(lines []int, line int) int {
	const intMax = int(^uint(0) >> 1)
	result := intMax
	for _, l := range lines {
		if l < result && l >= line {
			result = l
		}
	}
	if result == intMax {
		return -1
	}
	return result
}

// frameLinenoSetHook backs objects.SetLinenoHook. It extracts the
// activation record from the wrapper and runs frameLinenoSet.
func frameLinenoSetHook(w *objects.Frame, value objects.Object) error {
	if w == nil {
		return fmt.Errorf("RuntimeError: lost frame")
	}
	f, ok := w.Interp().(*frame.Frame)
	if !ok || f == nil {
		return fmt.Errorf("RuntimeError: frame has no activation record")
	}
	return frameLinenoSet(f, value)
}

// frameLinenoSet implements the f_lineno setter for a live frame.
//
// CPython: Objects/frameobject.c:1640 frame_lineno_set_impl
//
//nolint:gocognit,gocyclo // mirrors frame_lineno_set_impl: event gate + stack-analysis pop loop
func frameLinenoSet(f *frame.Frame, value objects.Object) error {
	ts := currentThread()
	code := f.Code
	if value == nil {
		return fmt.Errorf("ValueError: can't delete numeric/char attribute")
	}
	iv, ok := value.(*objects.Int)
	if !ok {
		return fmt.Errorf("ValueError: lineno must be an integer")
	}

	// Jumps are forbidden outside a trace callback and from any event
	// other than the handful that leave the eval loop re-enterable.
	whatEvent := -1
	if ts != nil {
		whatEvent = ts.WhatEvent
	}
	if whatEvent < 0 {
		return fmt.Errorf("ValueError: f_lineno can only be set in a trace function")
	}
	switch monitor.Event(whatEvent) {
	case monitor.EventPyResume, monitor.EventJump, monitor.EventBranch,
		monitor.EventBranchLeft, monitor.EventBranchRight,
		monitor.EventLine, monitor.EventPyYield:
		// Allowed.
	case monitor.EventPyStart:
		return fmt.Errorf("ValueError: can't jump from the 'call' trace event of a new frame")
	case monitor.EventCall, monitor.EventCReturn:
		return fmt.Errorf("ValueError: can't jump during a call")
	case monitor.EventPyReturn, monitor.EventPyUnwind, monitor.EventPyThrow,
		monitor.EventRaise, monitor.EventCRaise, monitor.EventInstruction,
		monitor.EventExceptionHandled:
		return fmt.Errorf("ValueError: can only jump from a 'line' trace event")
	default:
		return fmt.Errorf("SystemError: unexpected event type")
	}

	l, ok := iv.Int64()
	const intMax = int64(int32(^uint32(0) >> 1))
	if !ok || l > intMax || l < -intMax-1 {
		return fmt.Errorf("ValueError: lineno out of range")
	}
	newLineno := int(l)

	if newLineno < code.Firstlineno {
		return fmt.Errorf("ValueError: line %d comes before the current code block", newLineno)
	}

	length := len(code.Code) / 2
	lines := marklines(code, length)
	newLineno = firstLineNotBefore(lines, newLineno)
	if newLineno < 0 {
		return fmt.Errorf("ValueError: line %d comes after the current code block", int(l))
	}

	stacks := markStacks(code, length)

	bestStack := stackOverflowed
	bestAddr := -1
	lasti := f.InstrPtr / 2
	startStack := stacks[lasti]
	found := false
	msg := "cannot find bytecode for specified line"
	for i := 0; i < length; i++ {
		if lines[i] != newLineno {
			continue
		}
		targetStack := stacks[i]
		if compatibleStack(startStack, targetStack) {
			found = true
			if targetStack > bestStack {
				bestStack = targetStack
				bestAddr = i
			}
		} else if !found {
			switch {
			case startStack == stackOverflowed:
				msg = "stack to deep to analyze"
			case startStack == stackUninitialized:
				msg = "can't jump from unreachable code"
			default:
				msg = explainIncompatibleStack(targetStack)
			}
		}
	}
	if bestAddr < 0 {
		return fmt.Errorf("ValueError: %s", msg)
	}

	// Bind any locals the compiler proved live at the target but that
	// are currently NULL: rather than crash or rewrite co_code, assign
	// None (after a RuntimeWarning).
	//
	// CPython: Objects/frameobject.c:1789 (PyErr_WarnFormat block)
	nlocalsplus := frame.NLocalsPlusOf(code)
	unbound := 0
	for i := 0; i < nlocalsplus; i++ {
		if f.LocalsPlus[i].IsNull() {
			unbound++
		}
	}
	if unbound != 0 {
		s := ""
		if unbound != 1 {
			s = "s"
		}
		if err := warnings.WarnFormat(errors.PyExc_RuntimeWarning, 0,
			"assigning None to %d unbound local%s", unbound, s); err != nil {
			return err
		}
		// Second pass so that warnings-as-errors raises before any
		// slot is written.
		for i := 0; i < nlocalsplus; i++ {
			if f.LocalsPlus[i].IsNull() {
				f.LocalsPlus[i] = stackref.FromObjectImmortal(objects.None())
			}
		}
	}

	if frameIsSuspended(f) {
		// Account for the value popped by yield.
		startStack = popValue(startStack)
	}

	for startStack > bestStack {
		popped := f.PopStack()
		if topOfStack(startStack) == kindExcept {
			obj := popped.AsObjectSteal()
			if obj == nil || obj == objects.None() {
				ts.SetHandledException(nil)
			} else if exc, ok := obj.(state.Exception); ok {
				ts.SetHandledException(exc)
			} else {
				ts.SetHandledException(nil)
			}
		} else {
			popped.Close()
		}
		startStack = popValue(startStack)
	}

	f.Lineno = 0
	f.InstrPtr = bestAddr * 2
	f.LinenoJumped = true
	return nil
}

// frameIsSuspended reports whether f is a generator/coroutine/async-gen
// activation record that is currently suspended (its driver goroutine is
// not running it). A jump in that state must account for the value the
// yield popped.
//
// CPython: Objects/frameobject.c:1610 frame_is_suspended
func frameIsSuspended(f *frame.Frame) bool {
	if f.Owner != frame.OwnedByGenerator {
		return false
	}
	switch g := f.GenOwner.(type) {
	case *objects.Generator:
		return g.Running.Load() == 0
	case *objects.Coroutine:
		return g.Running.Load() == 0
	case *objects.AsyncGenerator:
		return g.Running.Load() == 0
	}
	return false
}
