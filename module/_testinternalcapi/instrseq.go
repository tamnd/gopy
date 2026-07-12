// InstructionSequence object and the compiler-pipeline helpers
// _testinternalcapi exposes to the test suite: new_instruction_sequence,
// compiler_codegen, optimize_cfg, and assemble_code_object. These drive
// gopy's codegen / cfg / assemble passes directly so test_peepholer,
// test_compiler_assemble, test_compiler_codegen, and the vendored
// test.support.bytecode_helper harness run against the real pipeline.
//
// CPython: Python/instruction_sequence.c InstructionSequenceType
// CPython: Modules/_testinternalcapi.c:713 new_instruction_sequence ff.
package testinternalcapi

import (
	"fmt"
	"math/big"
	"sort"

	"github.com/tamnd/gopy/ast"
	"github.com/tamnd/gopy/builtins"
	"github.com/tamnd/gopy/compile"
	"github.com/tamnd/gopy/objects"
)

// instructionSequence wraps a compile.Sequence as the Python-visible
// InstructionSequence object.
//
// CPython: Python/instruction_sequence.c _PyInstructionSequence
type instructionSequence struct {
	objects.Header
	seq *compile.Sequence
}

// instructionSequenceType is the Python type for InstructionSequence.
//
// CPython: Python/instruction_sequence.c:460 _PyInstructionSequence_Type
var instructionSequenceType = objects.NewType("InstructionSequence", []*objects.Type{objects.ObjectType()})

func init() {
	t := instructionSequenceType
	methods := []struct {
		name string
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
	}{
		{"addop", instrSeqAddop},
		{"use_label", instrSeqUseLabel},
		{"new_label", instrSeqNewLabel},
		{"add_nested", instrSeqAddNested},
		{"get_nested", instrSeqGetNested},
		{"get_instructions", instrSeqGetInstructions},
	}
	for _, m := range methods {
		objects.SetTypeDescr(t, m.name, objects.NewMethodDescr(t, m.name, m.fn))
	}
}

// newInstrSeq wraps seq in a fresh InstructionSequence object.
func newInstrSeq(seq *compile.Sequence) *instructionSequence {
	o := &instructionSequence{seq: seq}
	o.Init(instructionSequenceType)
	return o
}

// asInt coerces a Python int-like argument to a Go int via __index__.
func asInt(o objects.Object) (int, error) {
	idx, err := objects.NumberIndex(o)
	if err != nil {
		return 0, err
	}
	i, ok := idx.(*objects.Int)
	if !ok {
		return 0, fmt.Errorf("TypeError: expected an integer")
	}
	v, ok := i.Int64()
	if !ok {
		return 0, fmt.Errorf("OverflowError: Python int too large to convert to C int")
	}
	return int(v), nil
}

// instrSeqAddop implements InstructionSequence.addop(opcode, oparg,
// lineno, col_offset, end_lineno, end_col_offset).
//
// CPython: Python/instruction_sequence.c:282 InstructionSequenceType_addop_impl
func instrSeqAddop(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	self, ok := args[0].(*instructionSequence)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'addop' requires an InstructionSequence")
	}
	if len(args) != 7 {
		return nil, fmt.Errorf("TypeError: addop() takes exactly 6 arguments (%d given)", len(args)-1)
	}
	vals := make([]int, 6)
	for i := 0; i < 6; i++ {
		v, err := asInt(args[i+1])
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	opcode, oparg, lineno, colOffset, endLineno, endColOffset := vals[0], vals[1], vals[2], vals[3], vals[4], vals[5]
	if err := self.seq.AddopRaw(compile.Opcode(opcode), int32(oparg), lineno, colOffset, endLineno, endColOffset); err != nil {
		return nil, fmt.Errorf("ValueError: %s", err.Error())
	}
	return objects.None(), nil
}

// instrSeqUseLabel implements InstructionSequence.use_label(label).
//
// CPython: Python/instruction_sequence.c:257 InstructionSequenceType_use_label_impl
func instrSeqUseLabel(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	self, ok := args[0].(*instructionSequence)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'use_label' requires an InstructionSequence")
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: use_label() takes exactly one argument (%d given)", len(args)-1)
	}
	label, err := asInt(args[1])
	if err != nil {
		return nil, err
	}
	self.seq.UseLabelID(label)
	return objects.None(), nil
}

// instrSeqNewLabel implements InstructionSequence.new_label() -> int.
//
// CPython: Python/instruction_sequence.c:301 InstructionSequenceType_new_label_impl
func instrSeqNewLabel(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	self, ok := args[0].(*instructionSequence)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'new_label' requires an InstructionSequence")
	}
	return objects.NewInt(int64(self.seq.NewLabelID())), nil
}

// instrSeqAddNested implements InstructionSequence.add_nested(seq).
//
// CPython: Python/instruction_sequence.c:317 InstructionSequenceType_add_nested_impl
func instrSeqAddNested(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	self, ok := args[0].(*instructionSequence)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'add_nested' requires an InstructionSequence")
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: add_nested() takes exactly one argument (%d given)", len(args)-1)
	}
	nested, ok := args[1].(*instructionSequence)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected an instruction sequence, not %s", args[1].Type().Name)
	}
	self.seq.AddNested(nested.seq)
	return objects.None(), nil
}

// instrSeqGetNested implements InstructionSequence.get_nested() -> list.
// Each nested compile.Sequence is wrapped in a fresh InstructionSequence.
//
// CPython: Python/instruction_sequence.c:340 InstructionSequenceType_get_nested_impl
func instrSeqGetNested(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	self, ok := args[0].(*instructionSequence)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'get_nested' requires an InstructionSequence")
	}
	items := make([]objects.Object, len(self.seq.Nested))
	for i, child := range self.seq.Nested {
		items[i] = newInstrSeq(child)
	}
	return objects.NewList(items), nil
}

// instrSeqGetInstructions implements InstructionSequence.get_instructions().
// Returns one (opcode, oparg|None, lineno, end_lineno, col_offset,
// end_col_offset) tuple per instruction, after resolving jump labels.
//
// CPython: Python/instruction_sequence.c:356 InstructionSequenceType_get_instructions_impl
func instrSeqGetInstructions(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	self, ok := args[0].(*instructionSequence)
	if !ok {
		return nil, fmt.Errorf("TypeError: descriptor 'get_instructions' requires an InstructionSequence")
	}
	self.seq.ApplyJumpLabels()
	items := make([]objects.Object, len(self.seq.Instrs))
	for i := range self.seq.Instrs {
		ins := self.seq.Instrs[i]
		var arg objects.Object
		if ins.Op.HasArg() {
			arg = objects.NewInt(int64(ins.Oparg))
		} else {
			arg = objects.None()
		}
		loc := ins.Loc
		items[i] = objects.NewTuple([]objects.Object{
			objects.NewInt(int64(ins.Op)),
			arg,
			objects.NewInt(int64(loc.Lineno)),
			objects.NewInt(int64(loc.EndLineno)),
			objects.NewInt(int64(loc.ColOffset)),
			objects.NewInt(int64(loc.EndColOffset)),
		})
	}
	return objects.NewList(items), nil
}

// newInstructionSequence implements _testinternalcapi.new_instruction_sequence().
//
// CPython: Modules/_testinternalcapi.c:713 new_instruction_sequence
func newInstructionSequence(_ []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	return newInstrSeq(compile.NewSequence()), nil
}

// compilerCodegen implements compiler_codegen(ast, filename, optimize,
// compile_mode=0) -> (InstructionSequence, metadata). metadata maps
// argcount / posonlyargcount / kwonlyargcount to ints and consts to a
// list in LOAD_CONST index order.
//
// CPython: Modules/_testinternalcapi.c:742 compiler_codegen +
// Python/compile.c:1607 _PyCompile_CodeGen
func compilerCodegen(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("TypeError: compiler_codegen() missing required arguments")
	}
	mod, ok, err := builtins.PyASTObjectToMod(args[0])
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("TypeError: expected an AST")
	}
	filename, ok := args[1].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: compiler_codegen() filename must be str, not %s", args[1].Type().Name)
	}
	optimize, err := asInt(args[2])
	if err != nil {
		return nil, err
	}
	unit, err := compile.CodeGenForTest(mod, filename.Value(), optimize)
	if err != nil {
		return nil, err
	}
	unit.Seq.ApplyJumpLabels()

	metadata := objects.NewDict()
	if err := metadata.SetItem(objects.NewStr("argcount"), objects.NewInt(int64(unit.Argcount))); err != nil {
		return nil, err
	}
	if err := metadata.SetItem(objects.NewStr("posonlyargcount"), objects.NewInt(int64(unit.PosOnlyArgCount))); err != nil {
		return nil, err
	}
	if err := metadata.SetItem(objects.NewStr("kwonlyargcount"), objects.NewInt(int64(unit.KwOnlyArgCount))); err != nil {
		return nil, err
	}
	constItems := make([]objects.Object, len(unit.Consts))
	for i, c := range unit.Consts {
		o, err := constToObj(c)
		if err != nil {
			return nil, err
		}
		constItems[i] = o
	}
	if err := metadata.SetItem(objects.NewStr("consts"), objects.NewList(constItems)); err != nil {
		return nil, err
	}
	return objects.NewTuple([]objects.Object{newInstrSeq(unit.Seq), metadata}), nil
}

// optimizeCfg implements optimize_cfg(instructions, consts, nlocals).
// instructions is an InstructionSequence, consts a list aligned with
// LOAD_CONST opargs. The consts list is mutated in place to reflect the
// optimized const table, and a new optimized InstructionSequence is
// returned.
//
// CPython: Modules/_testinternalcapi.c:767 optimize_cfg +
// Python/flowgraph.c:4126 _PyCompile_OptimizeCfg
func optimizeCfg(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: optimize_cfg() takes exactly 3 arguments (%d given)", len(args))
	}
	seq, ok := args[0].(*instructionSequence)
	if !ok {
		return nil, fmt.Errorf("ValueError: expected an instruction sequence")
	}
	constsList, ok := args[1].(*objects.List)
	if !ok {
		return nil, fmt.Errorf("TypeError: consts must be a list")
	}
	nlocals, err := asInt(args[2])
	if err != nil {
		return nil, err
	}
	consts := make([]any, constsList.Len())
	for i := 0; i < constsList.Len(); i++ {
		c, err := objToConst(constsList.Item(i))
		if err != nil {
			return nil, err
		}
		consts[i] = c
	}
	out, err := compile.OptimizeCfgForTest(seq.seq, &consts, nlocals)
	if err != nil {
		return nil, err
	}
	newConsts := make([]objects.Object, len(consts))
	for i, c := range consts {
		o, cerr := constToObj(c)
		if cerr != nil {
			return nil, cerr
		}
		newConsts[i] = o
	}
	constsList.SetSlice(0, constsList.Len(), newConsts)
	return newInstrSeq(out), nil
}

// assembleCodeObject implements assemble_code_object(filename,
// instructions, metadata) -> code. metadata mirrors the C
// _PyCompile_CodeUnitMetadata: name/qualname strings, consts/names/
// varnames/cellvars/freevars/fasthidden dicts keyed by index, and the
// argcount/posonlyargcount/kwonlyargcount/firstlineno ints.
//
// CPython: Modules/_testinternalcapi.c:795 assemble_code_object +
// Python/compile.c:1657 _PyCompile_Assemble
func assembleCodeObject(args []objects.Object, _ map[string]objects.Object) (objects.Object, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("TypeError: assemble_code_object() takes exactly 3 arguments (%d given)", len(args))
	}
	filename, ok := args[0].(*objects.Unicode)
	if !ok {
		return nil, fmt.Errorf("TypeError: assemble_code_object() filename must be str, not %s", args[0].Type().Name)
	}
	seq, ok := args[1].(*instructionSequence)
	if !ok {
		return nil, fmt.Errorf("TypeError: expected an instruction sequence")
	}
	meta, ok := args[2].(*objects.Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: metadata must be a dict")
	}
	unit, err := unitFromMetadata(meta)
	if err != nil {
		return nil, err
	}
	co, err := compile.AssembleForTest(unit, seq.seq, filename.Value())
	if err != nil {
		return nil, err
	}
	return builtins.LiftCompileCode(co), nil
}

// unitFromMetadata builds a compile.Unit from the assemble_code_object
// metadata dict.
//
// CPython: Modules/_testinternalcapi.c:802 assemble_code_object_impl
func unitFromMetadata(meta *objects.Dict) (*compile.Unit, error) {
	getStr := func(key string) (string, error) {
		v, err := meta.GetItem(objects.NewStr(key))
		if err != nil {
			return "", err
		}
		s, ok := v.(*objects.Unicode)
		if !ok {
			return "", fmt.Errorf("TypeError: metadata[%q] must be str", key)
		}
		return s.Value(), nil
	}
	getInt := func(key string) (int, error) {
		v, err := meta.GetItem(objects.NewStr(key))
		if err != nil {
			return 0, err
		}
		return asInt(v)
	}

	name, err := getStr("name")
	if err != nil {
		return nil, err
	}
	qualname, err := getStr("qualname")
	if err != nil {
		return nil, err
	}
	unit := &compile.Unit{Name: name, Qualname: qualname}

	if unit.Argcount, err = getInt("argcount"); err != nil {
		return nil, err
	}
	if unit.PosOnlyArgCount, err = getInt("posonlyargcount"); err != nil {
		return nil, err
	}
	if unit.KwOnlyArgCount, err = getInt("kwonlyargcount"); err != nil {
		return nil, err
	}
	if unit.FirstLineno, err = getInt("firstlineno"); err != nil {
		return nil, err
	}

	consts, err := constsFromIndexDict(meta, "consts")
	if err != nil {
		return nil, err
	}
	unit.Consts = consts

	for _, f := range []struct {
		key string
		dst *[]string
	}{
		{"names", &unit.Names},
		{"varnames", &unit.VarNames},
		{"cellvars", &unit.CellVars},
		{"freevars", &unit.FreeVars},
	} {
		names, nerr := namesFromIndexDict(meta, f.key)
		if nerr != nil {
			return nil, nerr
		}
		*f.dst = names
	}

	fasthidden, err := fasthiddenFromDict(meta)
	if err != nil {
		return nil, err
	}
	unit.FastHidden = fasthidden
	return unit, nil
}

// constsFromIndexDict reads a {const: index} dict into a []any ordered
// by index. Mirrors consts_dict_keys_inorder's inverse.
//
// CPython: Python/compile.c consts_dict_keys_inorder
func constsFromIndexDict(meta *objects.Dict, key string) ([]any, error) {
	v, err := meta.GetItem(objects.NewStr(key))
	if err != nil {
		return nil, err
	}
	d, ok := v.(*objects.Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: metadata[%q] must be a dict", key)
	}
	type kv struct {
		val any
		idx int
	}
	keys := d.Keys()
	pairs := make([]kv, len(keys))
	for i, k := range keys {
		idxObj, gerr := d.GetItem(k)
		if gerr != nil {
			return nil, gerr
		}
		idx, ierr := asInt(idxObj)
		if ierr != nil {
			return nil, ierr
		}
		c, cerr := objToConst(k)
		if cerr != nil {
			return nil, cerr
		}
		pairs[i] = kv{val: c, idx: idx}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].idx < pairs[j].idx })
	out := make([]any, len(pairs))
	for i, p := range pairs {
		out[i] = p.val
	}
	return out, nil
}

// namesFromIndexDict reads a {name: index} dict into a []string ordered
// by index.
func namesFromIndexDict(meta *objects.Dict, key string) ([]string, error) {
	v, err := meta.GetItem(objects.NewStr(key))
	if err != nil {
		return nil, err
	}
	d, ok := v.(*objects.Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: metadata[%q] must be a dict", key)
	}
	type kv struct {
		name string
		idx  int
	}
	keys := d.Keys()
	pairs := make([]kv, len(keys))
	for i, k := range keys {
		s, ok := k.(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: metadata[%q] keys must be str", key)
		}
		idxObj, gerr := d.GetItem(k)
		if gerr != nil {
			return nil, gerr
		}
		idx, ierr := asInt(idxObj)
		if ierr != nil {
			return nil, ierr
		}
		pairs[i] = kv{name: s.Value(), idx: idx}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].idx < pairs[j].idx })
	out := make([]string, len(pairs))
	for i, p := range pairs {
		out[i] = p.name
	}
	return out, nil
}

// fasthiddenFromDict reads the {name: bool/index} fasthidden dict into a
// map[string]bool.
func fasthiddenFromDict(meta *objects.Dict) (map[string]bool, error) {
	v, err := meta.GetItem(objects.NewStr("fasthidden"))
	if err != nil {
		return nil, err
	}
	d, ok := v.(*objects.Dict)
	if !ok {
		return nil, fmt.Errorf("TypeError: metadata[\"fasthidden\"] must be a dict")
	}
	out := make(map[string]bool)
	for _, k := range d.Keys() {
		s, ok := k.(*objects.Unicode)
		if !ok {
			return nil, fmt.Errorf("TypeError: metadata[\"fasthidden\"] keys must be str")
		}
		val, gerr := d.GetItem(k)
		if gerr != nil {
			return nil, gerr
		}
		truthy, terr := objects.IsTruthy(val)
		if terr != nil {
			return nil, terr
		}
		out[s.Value()] = truthy
	}
	return out, nil
}

// objToConst converts a Python constant object into the native const
// value gopy's cfg / assemble passes consume.
func objToConst(o objects.Object) (any, error) {
	switch x := o.(type) {
	case nil:
		return nil, nil
	case *objects.Bool:
		v, _ := x.Int64()
		return v != 0, nil
	case *objects.Int:
		if v, ok := x.Int64(); ok {
			return v, nil
		}
		return x.BigInt(), nil
	case *objects.Float:
		return x.Float64(), nil
	case *objects.Complex:
		return x.Complex128(), nil
	case *objects.Unicode:
		return x.Value(), nil
	case *objects.Bytes:
		return x.Bytes(), nil
	case *objects.Tuple:
		vals := make([]any, x.Len())
		for i := 0; i < x.Len(); i++ {
			c, err := objToConst(x.Item(i))
			if err != nil {
				return nil, err
			}
			vals[i] = c
		}
		return &compile.ConstTuple{Values: vals}, nil
	case *objects.Set:
		if o.Type() == objects.FrozensetType {
			items := x.Items()
			vals := make(ast.FrozenSet, len(items))
			for i, it := range items {
				c, err := objToConst(it)
				if err != nil {
					return nil, err
				}
				vals[i] = c
			}
			return vals, nil
		}
	case *objects.Code:
		// A nested code object const round-trips through the assembler
		// unchanged: liftCompileConst passes any non-compile value back
		// out, so co_consts keeps the same code object.
		return x, nil
	}
	if objects.IsNone(o) {
		return nil, nil
	}
	if o == objects.Ellipsis() {
		return ast.EllipsisType{}, nil
	}
	return nil, fmt.Errorf("ValueError: unsupported constant of type %s", o.Type().Name)
}

// constToObj is the inverse of objToConst: it turns a native const value
// back into the Python object the test harness inspects.
func constToObj(c any) (objects.Object, error) {
	switch x := c.(type) {
	case nil:
		return objects.None(), nil
	case bool:
		return objects.NewBool(x), nil
	case int:
		return objects.NewInt(int64(x)), nil
	case int32:
		return objects.NewInt(int64(x)), nil
	case int64:
		return objects.NewInt(x), nil
	case *big.Int:
		return objects.NewIntFromBig(x), nil
	case float64:
		return objects.NewFloat(x), nil
	case complex128:
		return objects.NewComplex(real(x), imag(x)), nil
	case string:
		return objects.NewStr(x), nil
	case []byte:
		return objects.NewBytes(x), nil
	case *compile.ConstTuple:
		items := make([]objects.Object, len(x.Values))
		for i, v := range x.Values {
			o, err := constToObj(v)
			if err != nil {
				return nil, err
			}
			items[i] = o
		}
		return objects.NewTuple(items), nil
	case []any:
		items := make([]objects.Object, len(x))
		for i, v := range x {
			o, err := constToObj(v)
			if err != nil {
				return nil, err
			}
			items[i] = o
		}
		return objects.NewTuple(items), nil
	case ast.FrozenSet:
		items := make([]objects.Object, len(x))
		for i, v := range x {
			o, err := constToObj(v)
			if err != nil {
				return nil, err
			}
			items[i] = o
		}
		return objects.NewFrozenset(items)
	case ast.EllipsisType:
		return objects.Ellipsis(), nil
	case *compile.Code:
		return builtins.LiftCompileCode(x), nil
	case objects.Object:
		return x, nil
	default:
		return nil, fmt.Errorf("ValueError: unsupported constant of type %T", c)
	}
}
