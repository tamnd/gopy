// Deterministic dump of a cfgBuilder, used by per-phase compat tests
// that diff gopy's pipeline against the patched CPython _testinternalcapi
// hook at every phase boundary inside _PyCfg_OptimizeCodeUnit.
//
// The output is byte-identical to CPython's cfg_dump_to_string: just the
// block listing, with no header. Callers (the harness) wrap it with the
// phase name they fed the hook.

package compile

import (
	"fmt"
	"strings"
)

// DumpCfg renders g as a deterministic block listing.
//
// CPython: Python/flowgraph.c cfg_dump_to_string (added by the gopy
// phase-dump patch under test/cpython/patches/0001-cfg-phase-dump.patch).
func DumpCfg(g *cfgBuilder) string {
	if g == nil {
		return ""
	}
	var sb strings.Builder
	for _, b := range g.blocks() {
		dumpBasicblock(&sb, b)
	}
	return sb.String()
}

func dumpBasicblock(sb *strings.Builder, b *basicblock) {
	bReturn := ""
	if basicblockReturns(b) {
		bReturn = "return "
	}
	fmt.Fprintf(sb,
		"B%d: [EH=%d CLD=%d WRM=%d NO_FT=%d] used: %d, depth: %d, preds: %d %s\n",
		b.Label.id, boolToInt(b.ExceptHandler), boolToInt(b.Cold),
		boolToInt(b.Warm), boolToInt(b.nofallthrough()),
		len(b.Instr), b.StartDepth, b.Predecessors, bReturn)
	for i := range b.Instr {
		fmt.Fprintf(sb, "  [%02d] ", i)
		dumpInstr(sb, &b.Instr[i])
	}
}

func dumpInstr(sb *strings.Builder, ins *cfgInstr) {
	fmt.Fprintf(sb, "line: %d, %s (%d) ", ins.Loc.Lineno, ins.Op.Name(), int(ins.Op))
	switch {
	case hasJumpTarget(ins.Op):
		tgtID := -1
		if ins.Target != nil {
			tgtID = ins.Target.Label.id
		}
		fmt.Fprintf(sb, "target: B%d [%d] ", tgtID, ins.Oparg)
	case ins.Op.HasArg():
		fmt.Fprintf(sb, "arg: %d ", ins.Oparg)
	}
	if isJumpOpcode(ins.Op) {
		sb.WriteString("jump ")
	}
	sb.WriteByte('\n')
}

func basicblockReturns(b *basicblock) bool {
	last := b.lastInstr()
	return last != nil && last.Op == RETURN_VALUE
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
