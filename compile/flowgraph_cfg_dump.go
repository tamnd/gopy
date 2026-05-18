// Deterministic dump of a cfgBuilder, used by per-phase compat tests
// that diff gopy's pipeline against CPython's _testinternalcapi dump
// at every phase boundary inside _PyCfg_OptimizeCodeUnit /
// _PyCfg_OptimizedCfgToInstructionSequence / _PyAssemble_MakeCodeObject.
//
// The format is documented in website/docs/specs/1700/1716_full_compile_pipeline_port.md
// section "Dump format". Blocks come out in fallthrough order, jump
// targets and exception handlers print as block indices into that
// order, so two dumps compare with `cmp` even though the underlying
// pointer addresses differ.

package compile

import (
	"fmt"
	"strings"
)

// CfgDumpHeader is the per-unit metadata that prefixes every dump.
// Carries the inputs CPython's _PyCfg_OptimizeCodeUnit takes by
// argument, so a diff catches mismatches in the caller as well as in
// the graph itself.
type CfgDumpHeader struct {
	Phase       string
	FirstLineno int
	NLocals     int
	NParams     int
	CodeFlags   uint32
}

// DumpCfg renders g plus header as a deterministic string.
//
// CPython: Python/flowgraph.c:321 _PyCfgBuilder_DumpGraph (debug-only
// dump used by the same compat tests we mirror here)
func DumpCfg(g *cfgBuilder, header CfgDumpHeader) string {
	if g == nil {
		return fmt.Sprintf("# cfg dump: %s\n(nil)\n", header.Phase)
	}
	blocks := g.blocks()
	index := make(map[*basicblock]int, len(blocks))
	for i, b := range blocks {
		index[b] = i
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "# cfg dump: %s\n", header.Phase)
	fmt.Fprintf(&sb, "firstlineno: %d\n", header.FirstLineno)
	fmt.Fprintf(&sb, "nlocals: %d\n", header.NLocals)
	fmt.Fprintf(&sb, "nparams: %d\n", header.NParams)
	fmt.Fprintf(&sb, "codeflags: 0x%x\n", header.CodeFlags)
	sb.WriteString("\n")
	for i, b := range blocks {
		fmt.Fprintf(&sb, "block %d (label=%d preds=%d startdepth=%d warm=%t cold=%t):\n",
			i, b.Label.id, b.Predecessors, b.StartDepth, b.Warm, b.Cold)
		for j := range b.Instr {
			ins := &b.Instr[j]
			fmt.Fprintf(&sb, "  %d %s(%d) loc=%s",
				j, ins.Op.Name(), ins.Oparg, formatLoc(ins))
			if ins.Target != nil {
				fmt.Fprintf(&sb, " target=block%d", blockIndex(index, ins.Target))
			}
			if ins.Except != nil {
				fmt.Fprintf(&sb, " except=block%d", blockIndex(index, ins.Except))
			}
			sb.WriteString("\n")
		}
		if b.Next != nil {
			fmt.Fprintf(&sb, "  -> next=block%d\n", blockIndex(index, b.Next))
		} else {
			sb.WriteString("  -> next=nil\n")
		}
	}
	return sb.String()
}

func formatLoc(ins *cfgInstr) string {
	return fmt.Sprintf("%d:%d-%d:%d",
		ins.Loc.Lineno, ins.Loc.ColOffset,
		ins.Loc.EndLineno, ins.Loc.EndColOffset)
}

func blockIndex(index map[*basicblock]int, b *basicblock) int {
	if i, ok := index[b]; ok {
		return i
	}
	return -1
}
