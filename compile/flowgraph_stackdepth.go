// Stack-depth analysis for the flowgraph. Forward dataflow over the
// instruction stream, propagating the post-instruction stack height
// to the fall-through successor and to each jump target. The maximum
// across all reachable points becomes co_stacksize.
//
// CPython runs the dataflow on the CFG (basicblock list); gopy walks
// the flat instruction sequence with a worklist of instruction
// indices. Each block-entry index plays the role of a basicblock,
// so the algorithm is the same up to the substitution
// "block[i]->b_startdepth" with "startDepth[i]".

package compile

import "fmt"

// stackEffects mirrors the C struct returned by get_stack_effects. The
// C version has room for additional columns (max effect, etc) that the
// minimal port does not need yet.
//
// CPython: Python/flowgraph.c:762 stack_effects
type stackEffects struct {
	// Net is the post-instruction stack height delta (push - pop).
	Net int
}

// getStackEffects returns the stack effect of one opcode/oparg pair.
// jump selects the jump-taken column for branching opcodes (FOR_ITER,
// SEND). Returns an error for opcodes outside the metadata table.
//
// CPython: Python/flowgraph.c:767 get_stack_effects
func getStackEffects(op Opcode, oparg int32, jump bool, effects *stackEffects) error {
	if op < 0 {
		return fmt.Errorf("compile: invalid opcode %d", op)
	}
	// Specialized instructions are not supported. CPython gates on
	// `opcode <= MAX_REAL_OPCODE && _PyOpcode_Deopt[opcode] != opcode`
	// to reject specialized variants. gopy's codegen never emits
	// specialized opcodes (the specializer is a runtime concern), so
	// the check is structurally a no-op here; the comment preserves
	// the invariant.
	popped := numPopped(op, oparg)
	pushed := numPushed(op, oparg)
	if popped < 0 || pushed < 0 {
		return fmt.Errorf("compile: unknown stack effect for opcode %s arg %d", op.Name(), oparg)
	}
	if isBlockPushOpcode(op) && !jump {
		effects.Net = 0
		return nil
	}
	effects.Net = pushed - popped
	return nil
}

// isBlockPushOpcode mirrors IS_BLOCK_PUSH_OPCODE: SETUP_FINALLY,
// SETUP_WITH, SETUP_CLEANUP push a try/with handler onto the block
// stack and contribute a non-zero effect only when the implicit
// exception edge is taken.
//
// CPython: Include/internal/pycore_opcode_utils.h:17 IS_BLOCK_PUSH_OPCODE
func isBlockPushOpcode(op Opcode) bool {
	switch op {
	case SETUP_FINALLY, SETUP_WITH, SETUP_CLEANUP:
		return true
	}
	return false
}

// isScopeExitOpcode mirrors IS_SCOPE_EXIT_OPCODE.
//
// CPython: Include/internal/pycore_opcode_utils.h:52 IS_SCOPE_EXIT_OPCODE
func isScopeExitOpcode(op Opcode) bool {
	switch op {
	case RETURN_VALUE, RAISE_VARARGS, RERAISE:
		return true
	}
	return false
}

// numPopped returns the number of operand-stack values opcode
// consumes for the given oparg. -1 if the opcode is unknown.
//
// CPython: Python/Python-ast.c via _PyOpcode_num_popped (generated from
// Python/bytecodes.c). gopy reads the same data from a hand-maintained
// table keyed by opcode; the table values mirror the bytecodes.c
// "inputs" arity.
func numPopped(op Opcode, oparg int32) int {
	if e, ok := opcodeArity[op]; ok {
		if e.poppedFunc != nil {
			return e.poppedFunc(oparg)
		}
		return e.popped
	}
	return -1
}

// numPushed returns the number of operand-stack values opcode
// produces for the given oparg. -1 if the opcode is unknown.
//
// CPython: _PyOpcode_num_pushed.
func numPushed(op Opcode, oparg int32) int {
	if e, ok := opcodeArity[op]; ok {
		if e.pushedFunc != nil {
			return e.pushedFunc(oparg)
		}
		return e.pushed
	}
	return -1
}

// arityEntry stores the popped/pushed counts for one opcode. Either
// the constant fields or the oparg-dependent function fields are used,
// matching the two shapes of CPython's bytecodes.c "inputs"/"outputs"
// declarations.
type arityEntry struct {
	popped, pushed         int
	poppedFunc, pushedFunc func(int32) int
}

func arity(p, u int) arityEntry { return arityEntry{popped: p, pushed: u} }

// arityFunc is the generic constructor for opcodes whose stack effect
// depends on oparg.
func arityFunc(pop, push func(int32) int) arityEntry {
	return arityEntry{poppedFunc: pop, pushedFunc: push}
}

// constI returns a constant-int func used by arityFunc when only one
// of (pop, push) is oparg-dependent.
func constI(n int) func(int32) int { return func(int32) int { return n } }

// opcodeArity is the per-opcode (popped, pushed) table. Mirrors the
// "inputs" / "outputs" arity fields generated into
// pycore_opcode_metadata.h. Only the rows codegen actually emits are
// populated; opcodes outside the table are reported as -1 and surface
// an error from getStackEffects.
//
// CPython: Include/internal/pycore_opcode_metadata.h
// _PyOpcode_num_popped / _PyOpcode_num_pushed switches.
var opcodeArity = map[Opcode]arityEntry{
	NOP:                        arity(0, 0),
	RESUME:                     arity(0, 0),
	POP_TOP:                    arity(1, 0),
	POP_ITER:                   arity(1, 0),
	PUSH_NULL:                  arity(0, 1),
	END_FOR:                    arity(1, 0),
	END_SEND:                   arity(2, 1),
	UNARY_NEGATIVE:             arity(1, 1),
	UNARY_NOT:                  arity(1, 1),
	UNARY_INVERT:               arity(1, 1),
	GET_ITER:                   arity(1, 1),
	GET_LEN:                    arity(1, 2),
	GET_AITER:                  arity(1, 1),
	GET_ANEXT:                  arity(1, 2),
	GET_AWAITABLE:              arity(1, 1),
	GET_YIELD_FROM_ITER:        arity(1, 1),
	BINARY_OP:                  arity(2, 1),
	BINARY_SLICE:               arity(3, 1),
	STORE_SLICE:                arity(4, 0),
	STORE_SUBSCR:               arity(3, 0),
	DELETE_SUBSCR:              arity(2, 0),
	BUILD_SLICE:                arityFunc(func(a int32) int { return int(a) }, constI(1)),
	BUILD_LIST:                 arityFunc(func(a int32) int { return int(a) }, constI(1)),
	BUILD_SET:                  arityFunc(func(a int32) int { return int(a) }, constI(1)),
	BUILD_TUPLE:                arityFunc(func(a int32) int { return int(a) }, constI(1)),
	BUILD_MAP:                  arityFunc(func(a int32) int { return 2 * int(a) }, constI(1)),
	BUILD_STRING:               arityFunc(func(a int32) int { return int(a) }, constI(1)),
	LIST_APPEND:                arity(1, 0),
	SET_ADD:                    arity(1, 0),
	MAP_ADD:                    arity(2, 0),
	LIST_EXTEND:                arity(1, 0),
	SET_UPDATE:                 arity(1, 0),
	DICT_UPDATE:                arity(1, 0),
	DICT_MERGE:                 arity(1, 0),
	LOAD_FAST:                         arity(0, 1),
	LOAD_FAST_BORROW:                  arity(0, 1),
	LOAD_FAST_CHECK:                   arity(0, 1),
	LOAD_FAST_AND_CLEAR:               arity(0, 1),
	LOAD_FAST_LOAD_FAST:               arity(0, 2),
	LOAD_FAST_BORROW_LOAD_FAST_BORROW: arity(0, 2),
	STORE_FAST_LOAD_FAST:              arity(1, 1),
	STORE_FAST_STORE_FAST:             arity(2, 0),
	LOAD_NAME:                  arity(0, 1),
	LOAD_GLOBAL:                arityFunc(constI(0), func(a int32) int { return 1 + int(a&1) }),
	LOAD_DEREF:                 arity(0, 1),
	LOAD_FROM_DICT_OR_DEREF:    arity(1, 1),
	LOAD_CLOSURE:               arity(0, 1),
	LOAD_CONST:                 arity(0, 1),
	LOAD_SMALL_INT:             arity(0, 1),
	LOAD_COMMON_CONSTANT:       arity(0, 1),
	LOAD_LOCALS:                arity(0, 1),
	LOAD_BUILD_CLASS:           arity(0, 1),
	LOAD_ATTR:                  arityFunc(constI(1), func(a int32) int { return 1 + int(a&1) }),
	LOAD_SUPER_ATTR:            arityFunc(constI(3), func(a int32) int { return 2 + int(a&1) }),
	LOAD_SPECIAL:               arity(1, 2),
	LOAD_FROM_DICT_OR_GLOBALS:  arity(1, 1),
	STORE_FAST:                 arity(1, 0),
	STORE_FAST_MAYBE_NULL:      arity(1, 0),
	STORE_NAME:                 arity(1, 0),
	STORE_GLOBAL:               arity(1, 0),
	STORE_DEREF:                arity(1, 0),
	STORE_ATTR:                 arity(2, 0),
	DELETE_FAST:                arity(0, 0),
	DELETE_NAME:                arity(0, 0),
	DELETE_GLOBAL:              arity(0, 0),
	DELETE_DEREF:               arity(0, 0),
	DELETE_ATTR:                arity(1, 0),
	COMPARE_OP:                 arity(2, 1),
	IS_OP:                      arity(2, 1),
	CONTAINS_OP:                arity(2, 1),
	CHECK_EXC_MATCH:            arity(2, 2),
	CHECK_EG_MATCH:             arity(2, 2),
	IMPORT_NAME:                arity(2, 1),
	IMPORT_FROM:                arity(1, 2),
	JUMP:                       arity(0, 0),
	JUMP_NO_INTERRUPT:          arity(0, 0),
	JUMP_BACKWARD:              arity(0, 0),
	JUMP_BACKWARD_NO_INTERRUPT: arity(0, 0),
	JUMP_FORWARD:               arity(0, 0),
	POP_JUMP_IF_FALSE:          arity(1, 0),
	POP_JUMP_IF_TRUE:           arity(1, 0),
	POP_JUMP_IF_NONE:           arity(1, 0),
	POP_JUMP_IF_NOT_NONE:       arity(1, 0),
	FOR_ITER:                   arity(1, 2),
	SEND:                       arity(2, 2),
	YIELD_VALUE:                arity(1, 1),
	RESERVED:                   arity(0, 0),
	RETURN_VALUE:               arity(1, 0),
	RETURN_GENERATOR:           arity(0, 1),
	RAISE_VARARGS:              arityFunc(func(a int32) int { return int(a) }, constI(0)),
	RERAISE:                    arity(1, 0),
	INTERPRETER_EXIT:           arity(1, 0),
	END_ASYNC_FOR:              arity(2, 0),
	CLEANUP_THROW:              arity(3, 1),
	PUSH_EXC_INFO:              arity(1, 2),
	POP_EXCEPT:                 arity(1, 0),
	WITH_EXCEPT_START:          arity(4, 5),
	SETUP_WITH:                 arity(0, 1),
	SETUP_FINALLY:              arity(0, 1),
	SETUP_CLEANUP:              arity(0, 2),
	POP_BLOCK:                  arity(0, 0),
	CALL:                       arityFunc(func(a int32) int { return 2 + int(a) }, constI(1)),
	CALL_KW:                    arityFunc(func(a int32) int { return 3 + int(a) }, constI(1)),
	CALL_FUNCTION_EX:           arityFunc(func(a int32) int { return 3 + int(a&1) }, constI(1)),
	CALL_INTRINSIC_1:           arity(1, 1),
	CALL_INTRINSIC_2:           arity(2, 1),
	MAKE_FUNCTION:              arity(1, 1),
	SET_FUNCTION_ATTRIBUTE:     arity(2, 1),
	MAKE_CELL:                  arity(0, 0),
	COPY:                       arity(0, 1),
	COPY_FREE_VARS:             arity(0, 0),
	SWAP:                       arity(0, 0),
	UNPACK_SEQUENCE:            arityFunc(constI(1), func(a int32) int { return int(a) }),
	UNPACK_EX:                  arityFunc(constI(1), func(a int32) int { return int(a&0xff) + 1 + int(a>>8) }),
	FORMAT_SIMPLE:              arity(1, 1),
	FORMAT_WITH_SPEC:           arity(2, 1),
	CONVERT_VALUE:              arity(1, 1),
	BUILD_INTERPOLATION:        arityFunc(func(a int32) int { return 2 + int(a&1) }, constI(1)),
	BUILD_TEMPLATE:             arity(2, 1),
	MATCH_MAPPING:              arity(1, 2),
	MATCH_SEQUENCE:             arity(1, 2),
	MATCH_KEYS:                 arity(2, 3),
	MATCH_CLASS:                arity(3, 1),
	TO_BOOL:                    arity(1, 1),
	NOT_TAKEN:                  arity(0, 0),
	SETUP_ANNOTATIONS:          arity(0, 0),
	EXIT_INIT_CHECK:            arity(1, 0),
}

// stackdepthMin is the sentinel CPython sets as b_startdepth before
// the dataflow visits a block. -1 says the block has not yet been
// reached; any first push wins.
//
// CPython: Python/flowgraph.c:813 INT_MIN init
const stackdepthMin = -1

// stackdepthPush pushes idx onto the worklist when the proposed depth
// is greater than the current startDepth[idx]. Mirrors the CPython
// helper of the same name. The CPython version asserts startDepth is
// either uninitialised or matches the new value; gopy reports the
// inconsistency through the error return.
//
// CPython: Python/flowgraph.c:790 stackdepth_push
func stackdepthPush(stack []int, sp int, startDepth []int, idx, depth int) (int, error) {
	if startDepth[idx] >= 0 && startDepth[idx] != depth {
		return sp, fmt.Errorf("compile: invalid CFG, inconsistent stackdepth at %d (have %d, got %d)",
			idx, startDepth[idx], depth)
	}
	if startDepth[idx] < depth && startDepth[idx] < 100 {
		startDepth[idx] = depth
		stack[sp] = idx
		sp++
	}
	return sp, nil
}

// calculateStackdepth performs the forward dataflow over seq and
// returns the maximum running stack height plus the per-instruction
// entry depth. Mirrors CPython's CFG-based `calculate_stackdepth` with
// the basicblock list flattened to a worklist of instruction indices.
//
// CPython visits the exception edge from each block_push opcode with
// the jump-taken stack effect (push the exception, plus the lasti for
// SETUP_WITH / SETUP_CLEANUP). gopy does the same when it walks past
// a SETUP_X, so the handler block's entry depth ends up in the
// returned slice and stampHandlerStartDepths can read it from there.
//
// CPython: Python/flowgraph.c:808 calculate_stackdepth
func calculateStackdepth(seq *Sequence) (int, []int, error) {
	if len(seq.Instrs) == 0 {
		return 0, nil, nil
	}
	startDepth := make([]int, len(seq.Instrs))
	for i := range startDepth {
		startDepth[i] = stackdepthMin
	}
	stack := make([]int, len(seq.Instrs))
	sp := 0
	maxdepth := 0
	var err error
	sp, err = stackdepthPush(stack, sp, startDepth, 0, 0)
	if err != nil {
		return 0, nil, err
	}
	for sp > 0 {
		sp--
		i := stack[sp]
		depth := startDepth[i]
		if depth < 0 {
			continue
		}
		j := i
		for j < len(seq.Instrs) {
			ins := &seq.Instrs[j]
			var effects stackEffects
			if err := getStackEffects(ins.Op, ins.Oparg, false, &effects); err != nil {
				return 0, nil, fmt.Errorf("compile: invalid stack effect for opcode=%s arg=%d: %w",
					ins.Op.Name(), ins.Oparg, err)
			}
			newDepth := depth + effects.Net
			if newDepth < 0 {
				return 0, nil, fmt.Errorf("compile: invalid CFG, stack underflow at %d (%s)",
					j, ins.Op.Name())
			}
			if depth > maxdepth {
				maxdepth = depth
			}
			// Pseudo JUMP / JUMP_NO_INTERRUPT do not carry the HAS_JUMP
			// flag in gopy's opcode metadata (they get lowered to
			// JUMP_FORWARD / JUMP_BACKWARD later by
			// resolveUnconditionalJumps), but they ARE unconditional
			// branches with absolute-index opargs. The dataflow has to
			// propagate to their target the same way it does for
			// HAS_JUMP opcodes, otherwise blocks only reachable through
			// a pseudo JUMP get startDepth == -1.
			hasJumpTarget := HasTarget(ins.Op) && ins.Op != END_ASYNC_FOR
			pseudoJump := ins.Op == JUMP || ins.Op == JUMP_NO_INTERRUPT
			if hasJumpTarget || pseudoJump {
				if err := getStackEffects(ins.Op, ins.Oparg, true, &effects); err != nil {
					return 0, nil, fmt.Errorf("compile: invalid stack effect for opcode=%s arg=%d (jump): %w",
						ins.Op.Name(), ins.Oparg, err)
				}
				targetDepth := depth + effects.Net
				if targetDepth < 0 {
					return 0, nil, fmt.Errorf("compile: invalid CFG, target stackdepth %d at %d (%s)",
						targetDepth, j, ins.Op.Name())
				}
				if depth > maxdepth {
					maxdepth = depth
				}
				targetIdx := int(ins.Oparg)
				if targetIdx >= 0 && targetIdx < len(seq.Instrs) {
					sp, err = stackdepthPush(stack, sp, startDepth, targetIdx, targetDepth)
					if err != nil {
						return 0, nil, err
					}
				}
			}
			// Exception-edge propagation for the SETUP_X pseudo-ops.
			// SETUP_FINALLY: handler entry pushes exc (net +1 from
			// SETUP_FINALLY's running depth). SETUP_WITH: handler entry
			// restores the stack to before __enter__'s result and pushes
			// (lasti, exc), so net +1 (drop enter_result, push 2).
			// SETUP_CLEANUP: handler entry pushes (lasti, exc) on top
			// of the current stack, net +2. gopy's opcode metadata
			// flags don't carry the jump bit for these pseudo-ops, so
			// the propagation is open-coded here.
			//
			// CPython: Python/flowgraph.c:782 get_stack_effects branch
			// for IS_BLOCK_PUSH_OPCODE
			// CPython: Python/bytecodes.c:3568 pseudo SETUP_WITH
			if isBlockPushOpcode(ins.Op) {
				bonus := 1
				if ins.Op == SETUP_CLEANUP {
					bonus = 2
				}
				targetDepth := depth + bonus
				targetIdx := int(ins.Oparg)
				if targetIdx >= 0 && targetIdx < len(seq.Instrs) {
					sp, err = stackdepthPush(stack, sp, startDepth, targetIdx, targetDepth)
					if err != nil {
						return 0, nil, err
					}
				}
			}
			depth = newDepth
			if isUnconditionalJump(ins.Op) || isScopeExitOpcode(ins.Op) {
				// remaining code is dead
				j = len(seq.Instrs)
				break
			}
			j++
		}
		if j < len(seq.Instrs) {
			sp, err = stackdepthPush(stack, sp, startDepth, j, depth)
			if err != nil {
				return 0, nil, err
			}
		}
	}
	return maxdepth, startDepth, nil
}
