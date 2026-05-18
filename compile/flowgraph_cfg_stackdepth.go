// Stackdepth dataflow over cfgBuilder. Mirrors the section of
// Python/flowgraph.c that runs the forward stack-effects walk and
// stamps b_startdepth on every reachable block.
//
// CPython: Python/flowgraph.c:740 make_cfg_traversal_stack
// CPython: Python/flowgraph.c:790 stackdepth_push
// CPython: Python/flowgraph.c:809 calculate_stackdepth

package compile

import "fmt"

// cfgMakeTraversalStack returns a slice sized to hold every block on
// the entry chain and zeroes Visited as a side effect. CPython
// allocates room for nblocks pointers; gopy returns the matching slice.
// Shared by calculate_stackdepth, optimize_load_fast, and
// add_checks_for_loads_of_uninitialized_variables.
//
// CPython: Python/flowgraph.c:740 make_cfg_traversal_stack
func cfgMakeTraversalStack(entry *basicblock) []*basicblock {
	n := 0
	for b := entry; b != nil; b = b.Next {
		b.Visited = false
		n++
	}
	return make([]*basicblock, 0, n)
}

// cfgStackdepthPush stamps depth on b when it improves on the current
// startDepth and adds b to the worklist. Mirrors CPython's
// stackdepth_push with the basicblock pointer playing the role of the
// **sp double pointer.
//
// CPython: Python/flowgraph.c:790 stackdepth_push
func cfgStackdepthPush(stack []*basicblock, b *basicblock, depth int) ([]*basicblock, error) {
	if b.StartDepth >= 0 && b.StartDepth != depth {
		return stack, fmt.Errorf("compile: invalid CFG, inconsistent stackdepth")
	}
	if b.StartDepth < depth && b.StartDepth < 100 {
		b.StartDepth = depth
		stack = append(stack, b)
	}
	return stack, nil
}

// cfgCalculateStackdepth runs the forward dataflow over g and returns
// the maximum running stack height. Every reachable block's StartDepth
// ends up populated; unreachable blocks retain stackdepthMin.
//
// CPython: Python/flowgraph.c:809 calculate_stackdepth
//
//nolint:gocognit // direct port of CPython's monolithic dataflow loop
func cfgCalculateStackdepth(g *cfgBuilder) (int, error) {
	entry := g.EntryBlock
	for b := entry; b != nil; b = b.Next {
		b.StartDepth = stackdepthMin
	}
	stack := cfgMakeTraversalStack(entry)
	maxdepth := 0
	var err error
	stack, err = cfgStackdepthPush(stack, entry, 0)
	if err != nil {
		return 0, err
	}
	for len(stack) > 0 {
		b := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		depth := b.StartDepth
		next := b.Next
		for i := range b.Instr {
			ins := &b.Instr[i]
			var effects stackEffects
			if err := getStackEffects(ins.Op, ins.Oparg, false, &effects); err != nil {
				return 0, fmt.Errorf("compile: invalid stack effect for opcode=%s arg=%d: %w",
					ins.Op.Name(), ins.Oparg, err)
			}
			newDepth := depth + effects.Net
			if newDepth < 0 {
				return 0, fmt.Errorf("compile: invalid CFG, stack underflow at %s", ins.Op.Name())
			}
			if depth > maxdepth {
				maxdepth = depth
			}
			if HasTarget(ins.Op) && ins.Op != END_ASYNC_FOR {
				if err := getStackEffects(ins.Op, ins.Oparg, true, &effects); err != nil {
					return 0, fmt.Errorf("compile: invalid stack effect for opcode=%s arg=%d (jump): %w",
						ins.Op.Name(), ins.Oparg, err)
				}
				targetDepth := depth + effects.Net
				if targetDepth < 0 {
					return 0, fmt.Errorf("compile: invalid CFG, target stackdepth %d at %s",
						targetDepth, ins.Op.Name())
				}
				if ins.Target != nil {
					stack, err = cfgStackdepthPush(stack, ins.Target, targetDepth)
					if err != nil {
						return 0, err
					}
				}
			}
			depth = newDepth
			if isUnconditionalJump(ins.Op) || isScopeExitOpcode(ins.Op) {
				next = nil
				break
			}
		}
		if next != nil {
			stack, err = cfgStackdepthPush(stack, next, depth)
			if err != nil {
				return 0, err
			}
		}
	}
	return maxdepth, nil
}
