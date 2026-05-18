// optimize_load_fast and the uninitialized-locals scanner ported onto
// the cfgBuilder graph. Stackdepth lives in
// flowgraph_cfg_stackdepth.go; the flat-sequence shim in
// flowgraph_stackdepth.go stays until phase 6 of spec 1715 retires it.
//
// CPython: Python/flowgraph.c:2637 ref_stack_push / pop / swap_top / at / clear
// CPython: Python/flowgraph.c:2707 kill_local
// CPython: Python/flowgraph.c:2719 store_local
// CPython: Python/flowgraph.c:2728 load_fast_push_block
// CPython: Python/flowgraph.c:2776 optimize_load_fast
// CPython: Python/flowgraph.c:3062 maybe_push
// CPython: Python/flowgraph.c:3080 scan_block_for_locals
// CPython: Python/flowgraph.c:3127 fast_scan_many_locals
// CPython: Python/flowgraph.c:3273 add_checks_for_loads_of_uninitialized_variables

package compile

import "fmt"

// notLocal mirrors NOT_LOCAL (no local slot).
//
// CPython: Python/flowgraph.c:2619 NOT_LOCAL
const notLocal = -1

// dummyInstr mirrors DUMMY_INSTR (no producing instruction).
//
// CPython: Python/flowgraph.c:2620 DUMMY_INSTR
const dummyInstr = -1

// ref is one element of the abstract-reference stack maintained by
// optimize_load_fast. Instr is the instruction index that produced the
// reference (or dummyInstr); Local is the frame slot it shadows (or
// notLocal).
//
// CPython: Python/flowgraph.c:2622 ref
type ref struct {
	Instr int
	Local int
}

// refStack is the abstract reference stack used by optimize_load_fast.
//
// CPython: Python/flowgraph.c:2630 ref_stack
type refStack struct {
	Refs []ref
}

// push appends r.
//
// CPython: Python/flowgraph.c:2637 ref_stack_push
func (s *refStack) push(r ref) { s.Refs = append(s.Refs, r) }

// pop removes and returns the top of the stack. Mirrors the assert in
// the C version by panicking on underflow; CPython relies on the
// invariant that stackdepth keeps the stack non-empty when pop fires.
//
// CPython: Python/flowgraph.c:2654 ref_stack_pop
func (s *refStack) pop() ref {
	n := len(s.Refs)
	r := s.Refs[n-1]
	s.Refs = s.Refs[:n-1]
	return r
}

// swapTop swaps the top of the stack with the entry off slots below.
// CPython's helper indexes from size: idx = size - off; swap idx with
// size - 1.
//
// CPython: Python/flowgraph.c:2663 ref_stack_swap_top
func (s *refStack) swapTop(off int) {
	n := len(s.Refs)
	idx := n - off
	s.Refs[idx], s.Refs[n-1] = s.Refs[n-1], s.Refs[idx]
}

// at returns the reference at absolute index idx.
//
// CPython: Python/flowgraph.c:2673 ref_stack_at
func (s *refStack) at(idx int) ref { return s.Refs[idx] }

// clear empties the stack without releasing capacity.
//
// CPython: Python/flowgraph.c:2680 ref_stack_clear
func (s *refStack) clear() { s.Refs = s.Refs[:0] }

// size returns the current depth.
func (s *refStack) size() int { return len(s.Refs) }

// loadFastInstrFlag bits. Matches the LoadFastInstrFlag enum.
//
// CPython: Python/flowgraph.c:2697 LoadFastInstrFlag
const (
	supportKilled uint8 = 1
	storedAsLocal uint8 = 2
	refUnconsumed uint8 = 4
)

// killLocal walks the ref stack and marks every reference whose Local
// is local as supportKilled. Once a local is overwritten the borrowed
// references on the stack that came from it cannot be downgraded.
//
// CPython: Python/flowgraph.c:2707 kill_local
func killLocal(instrFlags []uint8, refs *refStack, local int) {
	for i := 0; i < refs.size(); i++ {
		r := refs.at(i)
		if r.Local == local {
			instrFlags[r.Instr] |= supportKilled
		}
	}
}

// storeLocal kills any borrowed references that shadow local, then
// records that the reference r escaped into a frame slot.
//
// CPython: Python/flowgraph.c:2719 store_local
func storeLocal(instrFlags []uint8, refs *refStack, local int, r ref) {
	killLocal(instrFlags, refs, local)
	if r.Instr != dummyInstr {
		instrFlags[r.Instr] |= storedAsLocal
	}
}

// loadFastPushBlock enqueues target onto the worklist once and stamps
// its StartDepth. Mirrors the same-named C helper, including the
// assert that an already-visited target has a matching depth.
//
// CPython: Python/flowgraph.c:2728 load_fast_push_block
func loadFastPushBlock(stack []*basicblock, target *basicblock, startDepth int) []*basicblock {
	if target.StartDepth < 0 {
		target.StartDepth = startDepth
	}
	if !target.Visited {
		target.Visited = true
		stack = append(stack, target)
	}
	return stack
}

// optimizeLoadFast rewrites LOAD_FAST{,_LOAD_FAST} into the BORROW
// variants when the borrowed reference is provably consumed before the
// supporting local is killed. The algorithm is the CPython routine of
// the same name, ported function-for-function.
//
// CPython: Python/flowgraph.c:2776 optimize_load_fast
//
//nolint:gocognit,gocyclo // direct port; CPython returns ERROR on PyMem failures, gopy's append cannot.
func optimizeLoadFast(g *cfgBuilder) error {
	refs := refStack{}
	maxInstrs := 0
	entry := g.EntryBlock
	for b := entry; b != nil; b = b.Next {
		if len(b.Instr) > maxInstrs {
			maxInstrs = len(b.Instr)
		}
	}
	instrFlags := make([]uint8, maxInstrs)
	blocks := cfgMakeTraversalStack(entry)
	blocks = append(blocks, entry)
	entry.StartDepth = 0
	entry.Visited = true

	pushRef := func(instr, local int) {
		refs.push(ref{Instr: instr, Local: local})
	}

	for len(blocks) > 0 {
		block := blocks[len(blocks)-1]
		blocks = blocks[:len(blocks)-1]
		for i := range instrFlags[:len(block.Instr)] {
			instrFlags[i] = 0
		}
		refs.clear()
		for i := 0; i < block.StartDepth; i++ {
			pushRef(dummyInstr, notLocal)
		}

		for i := range block.Instr {
			instr := &block.Instr[i]
			op := instr.Op
			oparg := int(instr.Oparg)
			switch op {
			case DELETE_FAST:
				killLocal(instrFlags, &refs, oparg)
			case LOAD_FAST:
				pushRef(i, oparg)
			case LOAD_FAST_AND_CLEAR:
				killLocal(instrFlags, &refs, oparg)
				pushRef(i, oparg)
			case LOAD_FAST_LOAD_FAST:
				pushRef(i, oparg>>4)
				pushRef(i, oparg&15)
			case STORE_FAST:
				r := refs.pop()
				storeLocal(instrFlags, &refs, oparg, r)
			case STORE_FAST_LOAD_FAST:
				r := refs.pop()
				storeLocal(instrFlags, &refs, oparg>>4, r)
				pushRef(i, oparg&15)
			case STORE_FAST_STORE_FAST:
				r := refs.pop()
				storeLocal(instrFlags, &refs, oparg>>4, r)
				r = refs.pop()
				storeLocal(instrFlags, &refs, oparg&15, r)
			case COPY:
				idx := refs.size() - oparg
				r := refs.at(idx)
				pushRef(r.Instr, r.Local)
			case SWAP:
				refs.swapTop(oparg)
			case FORMAT_SIMPLE, GET_ANEXT, GET_LEN, GET_YIELD_FROM_ITER,
				IMPORT_FROM, MATCH_KEYS, MATCH_MAPPING, MATCH_SEQUENCE,
				WITH_EXCEPT_START:
				numPopped := numPopped(op, instr.Oparg)
				numPushed := numPushed(op, instr.Oparg)
				net := numPushed - numPopped
				for range net {
					pushRef(i, notLocal)
				}
			case DICT_MERGE, DICT_UPDATE, LIST_APPEND, LIST_EXTEND,
				MAP_ADD, RERAISE, SET_ADD, SET_UPDATE:
				numPopped := numPopped(op, instr.Oparg)
				numPushed := numPushed(op, instr.Oparg)
				net := numPopped - numPushed
				for range net {
					refs.pop()
				}
			case END_SEND, SET_FUNCTION_ATTRIBUTE:
				tos := refs.pop()
				refs.pop()
				pushRef(tos.Instr, tos.Local)
			case CHECK_EXC_MATCH:
				refs.pop()
				pushRef(i, notLocal)
			case FOR_ITER:
				if instr.Target != nil {
					blocks = loadFastPushBlock(blocks, instr.Target, refs.size()+1)
				}
				pushRef(i, notLocal)
			case LOAD_ATTR, LOAD_SUPER_ATTR:
				self := refs.pop()
				if op == LOAD_SUPER_ATTR {
					refs.pop()
					refs.pop()
				}
				pushRef(i, notLocal)
				if oparg&1 != 0 {
					pushRef(self.Instr, self.Local)
				}
			case LOAD_SPECIAL, PUSH_EXC_INFO:
				tos := refs.pop()
				pushRef(i, notLocal)
				pushRef(tos.Instr, tos.Local)
			case SEND:
				if instr.Target != nil {
					blocks = loadFastPushBlock(blocks, instr.Target, refs.size())
				}
				refs.pop()
				pushRef(i, notLocal)
			default:
				numPopped := numPopped(op, instr.Oparg)
				numPushed := numPushed(op, instr.Oparg)
				if HasTarget(op) && instr.Target != nil {
					blocks = loadFastPushBlock(blocks, instr.Target,
						refs.size()-numPopped+numPushed)
				}
				if !isBlockPushOpcode(op) {
					for range numPopped {
						refs.pop()
					}
					for range numPushed {
						pushRef(i, notLocal)
					}
				}
			}
		}

		term := block.lastInstr()
		if term != nil && block.Next != nil &&
			!isUnconditionalJump(term.Op) && !isScopeExitOpcode(term.Op) {
			blocks = loadFastPushBlock(blocks, block.Next, refs.size())
		}

		for i := 0; i < refs.size(); i++ {
			r := refs.at(i)
			if r.Instr != -1 {
				instrFlags[r.Instr] |= refUnconsumed
			}
		}

		for i := range block.Instr {
			if instrFlags[i] == 0 {
				switch block.Instr[i].Op {
				case LOAD_FAST:
					block.Instr[i].Op = LOAD_FAST_BORROW
				case LOAD_FAST_LOAD_FAST:
					block.Instr[i].Op = LOAD_FAST_BORROW_LOAD_FAST_BORROW
				}
			}
		}
	}
	return nil
}

// maybePush enqueues b when extending its UnsafeLocalsMask reveals new
// uninitialized-local bits. Visited tracks "currently on the stack" to
// keep the worklist bounded.
//
// CPython: Python/flowgraph.c:3062 maybe_push
func maybePush(b *basicblock, unsafeMask uint64, stack []*basicblock) []*basicblock {
	both := b.UnsafeLocalsMask | unsafeMask
	if b.UnsafeLocalsMask != both {
		b.UnsafeLocalsMask = both
		if !b.Visited {
			stack = append(stack, b)
			b.Visited = true
		}
	}
	return stack
}

// scanBlockForLocals walks b and propagates the uninitialized-local
// bitmask. A LOAD_FAST against an unsafe local becomes LOAD_FAST_CHECK;
// the mask is then routed to fallthrough, jump-target, and except
// successors via maybePush.
//
// CPython: Python/flowgraph.c:3080 scan_block_for_locals
func scanBlockForLocals(b *basicblock, stack []*basicblock) []*basicblock {
	unsafeMask := b.UnsafeLocalsMask
	for i := range b.Instr {
		instr := &b.Instr[i]
		if instr.Except != nil {
			stack = maybePush(instr.Except, unsafeMask, stack)
		}
		if instr.Oparg >= 64 {
			continue
		}
		bit := uint64(1) << uint(instr.Oparg)
		switch instr.Op {
		case DELETE_FAST, LOAD_FAST_AND_CLEAR, STORE_FAST_MAYBE_NULL:
			unsafeMask |= bit
		case STORE_FAST:
			unsafeMask &^= bit
		case LOAD_FAST_CHECK:
			unsafeMask &^= bit
		case LOAD_FAST:
			if unsafeMask&bit != 0 {
				instr.Op = LOAD_FAST_CHECK
			}
			unsafeMask &^= bit
		}
	}
	if b.Next != nil && !b.nofallthrough() {
		stack = maybePush(b.Next, unsafeMask, stack)
	}
	last := b.lastInstr()
	if last != nil && isJumpOpcode(last.Op) && last.Target != nil {
		stack = maybePush(last.Target, unsafeMask, stack)
	}
	return stack
}

// fastScanManyLocals handles the > 64 locals case. To avoid O(n^2)
// behavior, initialization info is not propagated between blocks;
// instead each block is treated independently.
//
// CPython: Python/flowgraph.c:3127 fast_scan_many_locals
func fastScanManyLocals(entry *basicblock, nlocals int) error {
	if nlocals <= 64 {
		return fmt.Errorf("compile: fast_scan_many_locals called with nlocals=%d", nlocals)
	}
	states := make([]int, nlocals-64)
	blocknum := 0
	for b := entry; b != nil; b = b.Next {
		blocknum++
		for i := range b.Instr {
			instr := &b.Instr[i]
			arg := int(instr.Oparg)
			if arg < 64 {
				continue
			}
			switch instr.Op {
			case DELETE_FAST, LOAD_FAST_AND_CLEAR, STORE_FAST_MAYBE_NULL:
				states[arg-64] = blocknum - 1
			case STORE_FAST:
				states[arg-64] = blocknum
			case LOAD_FAST:
				if states[arg-64] != blocknum {
					instr.Op = LOAD_FAST_CHECK
				}
				states[arg-64] = blocknum
			}
		}
	}
	return nil
}

// addChecksForLoadsOfUninitializedVariables flips LOAD_FAST to
// LOAD_FAST_CHECK whenever the local could be undefined. The first
// uninitialized origin is "non-parameter locals on entry"; the second
// is DELETE_FAST somewhere downstream.
//
// CPython: Python/flowgraph.c:3273 add_checks_for_loads_of_uninitialized_variables
func addChecksForLoadsOfUninitializedVariables(entry *basicblock, nlocals, nparams int) error {
	if nlocals == 0 {
		return nil
	}
	if nlocals > 64 {
		if err := fastScanManyLocals(entry, nlocals); err != nil {
			return err
		}
		nlocals = 64
	}
	stack := cfgMakeTraversalStack(entry)
	var startMask uint64
	for i := nparams; i < nlocals; i++ {
		startMask |= uint64(1) << uint(i)
	}
	stack = maybePush(entry, startMask, stack)
	for b := entry; b != nil; b = b.Next {
		stack = scanBlockForLocals(b, stack)
	}
	for len(stack) > 0 {
		b := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		b.Visited = false
		stack = scanBlockForLocals(b, stack)
	}
	return nil
}
