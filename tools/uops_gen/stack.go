// Stack model walked by the case-body emitter to render PyStackRef pushes, pops, peeks, and spill/reload edges.
//
// CPython: Tools/cases_generator/stack.py
package main

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// PrintStacks toggles voluminous comment dumps mirroring the upstream
// PRINT_STACKS module flag.
//
// CPython: Tools/cases_generator/stack.py:9-10 PRINT_STACKS
var PrintStacks = false

// unusedNames holds the {"unused"} constant set used as a sentinel.
//
// CPython: Tools/cases_generator/stack.py:7 UNUSED
var unusedNames = map[string]struct{}{"unused": {}}

// reSimpleSym matches a symbol that does not need parentheses.
//
// CPython: Tools/cases_generator/stack.py:21 maybe_parenthesize
var reSimpleSym = regexp.MustCompile(`^[\s\w*]+$`)

// maybeParenthesize wraps sym in parens unless it is already wrapped or
// is a simple identifier-with-`*` expression.
//
// CPython: Tools/cases_generator/stack.py:12-24 maybe_parenthesize
func maybeParenthesize(sym string) string {
	if strings.HasPrefix(sym, "(") && strings.HasSuffix(sym, ")") {
		return sym
	}
	if reSimpleSym.MatchString(sym) {
		return sym
	}
	return "(" + sym + ")"
}

// varSize returns the symbolic size of a StackItem or "1" for a scalar.
//
// CPython: Tools/cases_generator/stack.py:27-31 var_size
func varSize(v *StackItem) string {
	if v.Size != "" {
		return v.Size
	}
	return "1"
}

// PointerOffset is a signed integer offset plus a multiset of positive
// and negative symbol terms, modeling the algebraic distance between
// two stack pointers.
//
// CPython: Tools/cases_generator/stack.py:34-44 PointerOffset
type PointerOffset struct {
	Numeric  int
	Positive []string
	Negative []string
}

// ZeroOffset returns a fresh zero PointerOffset.
//
// CPython: Tools/cases_generator/stack.py:45-47 PointerOffset.zero
func ZeroOffset() PointerOffset { return PointerOffset{} }

// Pop returns self - PointerOffset.from_item(item).
//
// CPython: Tools/cases_generator/stack.py:49-50 PointerOffset.pop
func (o PointerOffset) Pop(item *StackItem) PointerOffset {
	return o.Sub(OffsetFromItem(item))
}

// Push returns self + PointerOffset.from_item(item).
//
// CPython: Tools/cases_generator/stack.py:52-53 PointerOffset.push
func (o PointerOffset) Push(item *StackItem) PointerOffset {
	return o.Add(OffsetFromItem(item))
}

// OffsetFromItem returns the per-item offset contribution. Scalars
// produce a numeric +1, symbolic sizes parse the leading sign.
//
// CPython: Tools/cases_generator/stack.py:55-72 PointerOffset.from_item
func OffsetFromItem(item *StackItem) PointerOffset {
	if item.Size == "" {
		return PointerOffset{Numeric: 1}
	}
	txt := strings.TrimSpace(item.Size)
	var (
		n []string
		p []string
	)
	if i, err := atoiStrict(txt); err == nil {
		return PointerOffset{Numeric: i}
	}
	if len(txt) > 0 && txt[0] == '+' {
		txt = txt[1:]
	}
	if len(txt) > 0 && txt[0] == '-' {
		n = []string{txt[1:]}
	} else {
		p = []string{txt}
	}
	return PointerOffset{Numeric: 0, Positive: p, Negative: n}
}

// atoiStrict mirrors Python's int(txt) for an integer-only string.
func atoiStrict(s string) (int, error) {
	if s == "" {
		return 0, errors.New("empty")
	}
	n := 0
	sign := 1
	i := 0
	switch s[0] {
	case '+':
		i = 1
	case '-':
		sign = -1
		i = 1
	}
	if i == len(s) {
		return 0, errors.New("no digits")
	}
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, errors.New("not a digit")
		}
		n = n*10 + int(c-'0')
	}
	return sign * n, nil
}

// CreateOffset constructs a PointerOffset, simplifying matched terms.
//
// CPython: Tools/cases_generator/stack.py:74-77 PointerOffset.create
func CreateOffset(numeric int, positive, negative []string) PointerOffset {
	p, n := simplifyOffsetTerms(positive, negative)
	return PointerOffset{Numeric: numeric, Positive: p, Negative: n}
}

// Sub returns self - other.
//
// CPython: Tools/cases_generator/stack.py:79-84 PointerOffset.__sub__
func (o PointerOffset) Sub(other PointerOffset) PointerOffset {
	return CreateOffset(
		o.Numeric-other.Numeric,
		concatStrs(o.Positive, other.Negative),
		concatStrs(o.Negative, other.Positive),
	)
}

// Add returns self + other.
//
// CPython: Tools/cases_generator/stack.py:86-91 PointerOffset.__add__
func (o PointerOffset) Add(other PointerOffset) PointerOffset {
	return CreateOffset(
		o.Numeric+other.Numeric,
		concatStrs(o.Positive, other.Positive),
		concatStrs(o.Negative, other.Negative),
	)
}

// Neg returns -self.
//
// CPython: Tools/cases_generator/stack.py:93-94 PointerOffset.__neg__
func (o PointerOffset) Neg() PointerOffset {
	return PointerOffset{Numeric: -o.Numeric, Positive: o.Negative, Negative: o.Positive}
}

func concatStrs(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}

// simplifyOffsetTerms cancels matching positive and negative terms.
//
// CPython: Tools/cases_generator/stack.py:96-113 PointerOffset._simplify
func simplifyOffsetTerms(positive, negative []string) ([]string, []string) {
	pOrig := append([]string(nil), positive...)
	nOrig := append([]string(nil), negative...)
	sort.Strings(pOrig)
	sort.Strings(nOrig)
	var pUniq, nUniq []string
	for len(pOrig) > 0 && len(nOrig) > 0 {
		pItem := pOrig[len(pOrig)-1]
		pOrig = pOrig[:len(pOrig)-1]
		nItem := nOrig[len(nOrig)-1]
		nOrig = nOrig[:len(nOrig)-1]
		switch {
		case pItem > nItem:
			pUniq = append(pUniq, pItem)
			nOrig = append(nOrig, nItem)
		case pItem < nItem:
			nUniq = append(nUniq, nItem)
			pOrig = append(pOrig, pItem)
		}
	}
	return concatStrs(pOrig, pUniq), concatStrs(nOrig, nUniq)
}

// ToC renders the offset as a C expression.
//
// CPython: Tools/cases_generator/stack.py:115-129 PointerOffset.to_c
func (o PointerOffset) ToC() string {
	var b strings.Builder
	for _, item := range o.Negative {
		b.WriteString(" - ")
		b.WriteString(maybeParenthesize(item))
	}
	for _, item := range o.Positive {
		b.WriteString(" + ")
		b.WriteString(maybeParenthesize(item))
	}
	sym := b.String()
	var res string
	if sym != "" && o.Numeric == 0 {
		res = sym
	} else {
		res = fmt.Sprintf("%d%s", o.Numeric, sym)
	}
	res = strings.TrimPrefix(res, " + ")
	if strings.HasPrefix(res, " - ") {
		res = "-" + res[3:]
	}
	return res
}

// AsInt returns the numeric value when there are no symbolic terms.
//
// CPython: Tools/cases_generator/stack.py:131-134 PointerOffset.as_int
func (o PointerOffset) AsInt() (int, bool) {
	if len(o.Positive) > 0 || len(o.Negative) > 0 {
		return 0, false
	}
	return o.Numeric, true
}

// Equal reports structural equality.
func (o PointerOffset) Equal(other PointerOffset) bool {
	if o.Numeric != other.Numeric {
		return false
	}
	return strSliceEq(o.Positive, other.Positive) && strSliceEq(o.Negative, other.Negative)
}

func strSliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func (o PointerOffset) String() string { return o.ToC() }

// Local is a stack-resident slot bound to a StackItem with optional
// memory backing and an "in local variable" flag.
//
// CPython: Tools/cases_generator/stack.py:142-149 Local
type Local struct {
	Item         *StackItem
	MemoryOffset *PointerOffset
	InLocal      bool
}

// CompactStr renders a debug-friendly tag string for the local.
//
// CPython: Tools/cases_generator/stack.py:151-155 Local.compact_str
func (l *Local) CompactStr() string {
	mtag := ""
	if l.MemoryOffset != nil {
		mtag = "M"
	}
	dtag := ""
	if l.InLocal {
		dtag = "L"
	}
	atag := ""
	if l.IsArray() {
		atag = "A"
	}
	return fmt.Sprintf("'%s'%s%s%s", l.Item.Name, mtag, dtag, atag)
}

// LocalUnused builds an unused (non-live) Local at a known offset.
//
// CPython: Tools/cases_generator/stack.py:157-159 Local.unused
func LocalUnused(defn *StackItem, offset *PointerOffset) *Local {
	return &Local{Item: defn, MemoryOffset: offset, InLocal: false}
}

// LocalUndefined builds a Local with no backing.
//
// CPython: Tools/cases_generator/stack.py:161-163 Local.undefined
func LocalUndefined(defn *StackItem) *Local {
	return &Local{Item: defn, MemoryOffset: nil, InLocal: false}
}

// LocalFromMemory builds a Local pointing at memory.
//
// CPython: Tools/cases_generator/stack.py:165-167 Local.from_memory
func LocalFromMemory(defn *StackItem, offset PointerOffset) *Local {
	o := offset
	return &Local{Item: defn, MemoryOffset: &o, InLocal: true}
}

// LocalRegister builds a register-only Local with a synthetic StackItem.
//
// CPython: Tools/cases_generator/stack.py:169-172 Local.register
func LocalRegister(name string) *Local {
	item := &StackItem{Name: name, Used: true}
	return &Local{Item: item, InLocal: true}
}

// Kill drops the local from both stack and memory.
//
// CPython: Tools/cases_generator/stack.py:174-176 Local.kill
func (l *Local) Kill() {
	l.InLocal = false
	l.MemoryOffset = nil
}

// InMemory reports whether the local is reachable through memory.
//
// CPython: Tools/cases_generator/stack.py:178-179 Local.in_memory
func (l *Local) InMemory() bool {
	return l.MemoryOffset != nil || l.IsArray()
}

// IsDead reports whether the local has no backing.
//
// CPython: Tools/cases_generator/stack.py:181-182 Local.is_dead
func (l *Local) IsDead() bool {
	return !l.InLocal && l.MemoryOffset == nil
}

// Copy returns a shallow clone of the local.
//
// CPython: Tools/cases_generator/stack.py:184-189 Local.copy
func (l *Local) Copy() *Local {
	c := &Local{Item: l.Item, InLocal: l.InLocal}
	if l.MemoryOffset != nil {
		off := *l.MemoryOffset
		c.MemoryOffset = &off
	}
	return c
}

// Size returns the underlying StackItem's size string.
//
// CPython: Tools/cases_generator/stack.py:191-193 Local.size
func (l *Local) Size() string { return l.Item.Size }

// Name returns the underlying StackItem's name.
//
// CPython: Tools/cases_generator/stack.py:195-197 Local.name
func (l *Local) Name() string { return l.Item.Name }

// IsArray reports whether the underlying StackItem is an array.
//
// CPython: Tools/cases_generator/stack.py:199-200 Local.is_array
func (l *Local) IsArray() bool { return l.Item.IsArray() }

// Equal compares two Locals as the upstream __eq__ does.
//
// CPython: Tools/cases_generator/stack.py:202-209 Local.__eq__
func (l *Local) Equal(other *Local) bool {
	if l.Item != other.Item || l.InLocal != other.InLocal {
		return false
	}
	if (l.MemoryOffset == nil) != (other.MemoryOffset == nil) {
		return false
	}
	if l.MemoryOffset != nil && !l.MemoryOffset.Equal(*other.MemoryOffset) {
		return false
	}
	return true
}

// StackError is the typed error class raised on bad stack state.
//
// CPython: Tools/cases_generator/stack.py:212-213 StackError
type StackError struct{ Msg string }

func (e *StackError) Error() string { return e.Msg }

func newStackError(format string, args ...any) error {
	return &StackError{Msg: fmt.Sprintf(format, args...)}
}

// Stack models the abstract operand stack: a logical pointer plus a
// stash of unsaved Locals stacked above the physical pointer.
//
// CPython: Tools/cases_generator/stack.py:218-225 Stack
type Stack struct {
	BaseOffset  PointerOffset
	PhysicalSp  PointerOffset
	LogicalSp   PointerOffset
	Variables   []*Local
	ExtractBits bool
	CastType    string
}

// NewStack returns a freshly initialized Stack with default cast type.
//
// CPython: Tools/cases_generator/stack.py:219-225 Stack.__init__
func NewStack() *Stack { return NewStackOpt(true, "uintptr_t") }

// NewStackOpt mirrors Stack(extract_bits, cast_type).
//
// CPython: Tools/cases_generator/stack.py:219 Stack.__init__
func NewStackOpt(extractBits bool, castType string) *Stack {
	return &Stack{ExtractBits: extractBits, CastType: castType}
}

// Drop pops a value, requiring it to be dead unless check_liveness=false.
//
// CPython: Tools/cases_generator/stack.py:227-234 Stack.drop
func (s *Stack) Drop(v *StackItem, checkLiveness bool) error {
	s.LogicalSp = s.LogicalSp.Pop(v)
	if len(s.Variables) > 0 {
		popped := s.Variables[len(s.Variables)-1]
		s.Variables = s.Variables[:len(s.Variables)-1]
		if popped.IsDead() || !v.Used {
			return nil
		}
	}
	if checkLiveness {
		return newStackError("Dropping live value '%s'", v.Name)
	}
	return nil
}

// Pop emits assignments to bind v to the top of the stack and returns
// the resulting Local.
//
// CPython: Tools/cases_generator/stack.py:236-277 Stack.pop
func (s *Stack) Pop(v *StackItem, out *CWriter) (*Local, error) {
	if len(s.Variables) > 0 {
		top := s.Variables[len(s.Variables)-1]
		if v.IsArray() != top.IsArray() || top.Item.Size != v.Size {
			s.Clear(out)
		}
	}
	s.LogicalSp = s.LogicalSp.Pop(v)
	indirect := ""
	if v.IsArray() {
		indirect = "&"
	}
	if len(s.Variables) > 0 {
		popped := s.Variables[len(s.Variables)-1]
		s.Variables = s.Variables[:len(s.Variables)-1]
		if v.IsArray() != popped.IsArray() || popped.Item.Size != v.Size {
			return nil, newStackError("Stack mismatch popping '%s'", v.Name)
		}
		if !v.Used {
			return popped, nil
		}
		var rename string
		if popped.Name() != v.Name {
			rename = fmt.Sprintf("%s = %s;\n", v.Name, popped.Name())
			popped.Item = v
		}
		var defn string
		if !popped.InLocal {
			if popped.MemoryOffset == nil {
				off := s.LogicalSp
				popped.MemoryOffset = &off
			}
			offset := popped.MemoryOffset.Sub(s.PhysicalSp)
			if v.IsArray() {
				defn = fmt.Sprintf("%s = &stack_pointer[%s];\n", v.Name, offset.ToC())
			} else {
				defn = fmt.Sprintf("%s = stack_pointer[%s];\n", v.Name, offset.ToC())
				popped.InLocal = true
			}
		} else {
			defn = rename
		}
		out.Emit(defn)
		return popped, nil
	}
	s.BaseOffset = s.LogicalSp
	if _, isUnused := unusedNames[v.Name]; isUnused || !v.Used {
		base := s.BaseOffset
		return LocalUnused(v, &base), nil
	}
	cast := ""
	if indirect == "" && v.Type != "" {
		cast = "(" + v.Type + ")"
	}
	bits := ""
	if cast != "" && s.ExtractBits {
		bits = ".bits"
	}
	cOffset := s.BaseOffset.Sub(s.PhysicalSp).ToC()
	assign := fmt.Sprintf("%s = %s%sstack_pointer[%s]%s;\n", v.Name, cast, indirect, cOffset, bits)
	out.Emit(assign)
	s.printComment(out)
	return LocalFromMemory(v, s.BaseOffset), nil
}

// Clear flushes the stash to memory and resets the stash list.
//
// CPython: Tools/cases_generator/stack.py:279-283 Stack.clear
func (s *Stack) Clear(out *CWriter) {
	s.Flush(out)
	s.Variables = nil
	s.BaseOffset = s.LogicalSp
}

// Push pushes a Local onto the stash, advancing logical_sp.
//
// CPython: Tools/cases_generator/stack.py:285-288 Stack.push
func (s *Stack) Push(v *Local) {
	s.Variables = append(s.Variables, v)
	s.LogicalSp = s.LogicalSp.Push(v.Item)
}

// doEmit writes one stack_pointer[off] = (cast)name; line.
//
// CPython: Tools/cases_generator/stack.py:290-300 Stack._do_emit
func doEmit(out *CWriter, v *StackItem, off PointerOffset, castType string, extractBits bool) {
	cast := ""
	if v.Type != "" {
		cast = "(" + castType + ")"
	}
	bits := ""
	if cast != "" && extractBits {
		bits = ".bits"
	}
	out.Emit(fmt.Sprintf("stack_pointer[%s]%s = %s%s;\n", off.ToC(), bits, cast, v.Name))
}

// savePhysicalSp emits a stack_pointer += diff bump if needed.
//
// CPython: Tools/cases_generator/stack.py:302-309 Stack._save_physical_sp
func (s *Stack) savePhysicalSp(out *CWriter) {
	if !s.PhysicalSp.Equal(s.LogicalSp) {
		diff := s.LogicalSp.Sub(s.PhysicalSp)
		out.StartLine()
		out.Emit(fmt.Sprintf("stack_pointer += %s;\n", diff.ToC()))
		out.Emit("assert(WITHIN_STACK_BOUNDS());\n")
		s.PhysicalSp = s.LogicalSp
		s.printComment(out)
	}
}

// SaveVariables flushes any local that lives only in a register.
//
// CPython: Tools/cases_generator/stack.py:311-325 Stack.save_variables
func (s *Stack) SaveVariables(out *CWriter) {
	out.StartLine()
	varOffset := s.BaseOffset
	for _, v := range s.Variables {
		if v.InLocal && v.MemoryOffset == nil && !v.IsArray() {
			s.printComment(out)
			off := varOffset
			v.MemoryOffset = &off
			stackOffset := varOffset.Sub(s.PhysicalSp)
			doEmit(out, v.Item, stackOffset, s.CastType, s.ExtractBits)
			s.printComment(out)
		}
		varOffset = varOffset.Push(v.Item)
	}
}

// Flush saves variables, bumps the physical pointer, and starts a new line.
//
// CPython: Tools/cases_generator/stack.py:327-331 Stack.flush
func (s *Stack) Flush(out *CWriter) {
	s.printComment(out)
	s.SaveVariables(out)
	s.savePhysicalSp(out)
	out.StartLine()
}

// IsFlushed reports whether nothing is dirty.
//
// CPython: Tools/cases_generator/stack.py:333-337 Stack.is_flushed
func (s *Stack) IsFlushed() bool {
	for _, v := range s.Variables {
		if !v.InMemory() {
			return false
		}
	}
	return s.PhysicalSp.Equal(s.LogicalSp)
}

// SpOffset returns physical_sp - logical_sp as a C expression.
//
// CPython: Tools/cases_generator/stack.py:339-340 Stack.sp_offset
func (s *Stack) SpOffset() string {
	return s.PhysicalSp.Sub(s.LogicalSp).ToC()
}

// AsComment renders a debug comment line.
//
// CPython: Tools/cases_generator/stack.py:342-346 Stack.as_comment
func (s *Stack) AsComment() string {
	parts := make([]string, 0, len(s.Variables))
	for _, v := range s.Variables {
		parts = append(parts, v.CompactStr())
	}
	return fmt.Sprintf(
		"/* Variables=[%s]; base=%s; sp=%s; logical_sp=%s */",
		strings.Join(parts, ", "),
		s.BaseOffset.ToC(),
		s.PhysicalSp.ToC(),
		s.LogicalSp.ToC(),
	)
}

func (s *Stack) printComment(out *CWriter) {
	if PrintStacks {
		out.Emit(s.AsComment() + "\n")
	}
}

// Copy returns a deep clone of the stack.
//
// CPython: Tools/cases_generator/stack.py:352-358 Stack.copy
func (s *Stack) Copy() *Stack {
	other := NewStackOpt(s.ExtractBits, s.CastType)
	other.BaseOffset = s.BaseOffset
	other.PhysicalSp = s.PhysicalSp
	other.LogicalSp = s.LogicalSp
	other.Variables = make([]*Local, len(s.Variables))
	for i, v := range s.Variables {
		other.Variables[i] = v.Copy()
	}
	return other
}

// Equal compares two stacks.
//
// CPython: Tools/cases_generator/stack.py:360-368 Stack.__eq__
func (s *Stack) Equal(other *Stack) bool {
	if !s.PhysicalSp.Equal(other.PhysicalSp) ||
		!s.LogicalSp.Equal(other.LogicalSp) ||
		!s.BaseOffset.Equal(other.BaseOffset) {
		return false
	}
	if len(s.Variables) != len(other.Variables) {
		return false
	}
	for i, v := range s.Variables {
		if !v.Equal(other.Variables[i]) {
			return false
		}
	}
	return true
}

// Align bumps the physical pointer to match other's.
//
// CPython: Tools/cases_generator/stack.py:370-378 Stack.align
func (s *Stack) Align(other *Stack, out *CWriter) error {
	if !s.LogicalSp.Equal(other.LogicalSp) {
		return newStackError("Cannot align stacks: differing logical top")
	}
	if s.PhysicalSp.Equal(other.PhysicalSp) {
		return nil
	}
	diff := other.PhysicalSp.Sub(s.PhysicalSp)
	out.StartLine()
	out.Emit(fmt.Sprintf("stack_pointer += %s;\n", diff.ToC()))
	s.PhysicalSp = other.PhysicalSp
	return nil
}

// Merge unifies self with other across a control-flow join.
//
// CPython: Tools/cases_generator/stack.py:380-395 Stack.merge
func (s *Stack) Merge(other *Stack, out *CWriter) error {
	if len(s.Variables) != len(other.Variables) {
		return newStackError("Cannot merge stacks: differing variables")
	}
	for i, sv := range s.Variables {
		ov := other.Variables[i]
		if sv.Name() != ov.Name() {
			return newStackError("Mismatched variables on stack: %s and %s", sv.Name(), ov.Name())
		}
		sv.InLocal = sv.InLocal && ov.InLocal
		if ov.MemoryOffset == nil {
			sv.MemoryOffset = nil
		}
	}
	if err := s.Align(other, out); err != nil {
		return err
	}
	for i, sv := range s.Variables {
		ov := other.Variables[i]
		switch {
		case sv.MemoryOffset != nil:
			if ov.MemoryOffset == nil || !sv.MemoryOffset.Equal(*ov.MemoryOffset) {
				return newStackError(
					"Mismatched stack depths for %s",
					sv.Name(),
				)
			}
		case ov.MemoryOffset == nil:
			sv.MemoryOffset = nil
		}
	}
	return nil
}

// stacksOf yields the analyzed stack effects for an Instruction or
// PseudoInstruction.
//
// CPython: Tools/cases_generator/stack.py:398-405 stacks
func stacksOf(inst any) []AnalyzedStackEffect {
	switch v := inst.(type) {
	case *Instruction:
		var out []AnalyzedStackEffect
		for _, p := range v.Parts {
			if u, ok := p.(*Uop); ok {
				out = append(out, u.Stack)
			}
		}
		return out
	case *PseudoInstruction:
		return []AnalyzedStackEffect{v.Stack}
	}
	return nil
}

// applyStackEffect threads one (inputs, outputs) pair through a stack
// without emitting any C.
//
// CPython: Tools/cases_generator/stack.py:408-420 apply_stack_effect
func applyStackEffect(stack *Stack, effect AnalyzedStackEffect) error {
	null := NullCWriter()
	locals := map[string]*Local{}
	for i := len(effect.Inputs) - 1; i >= 0; i-- {
		v := effect.Inputs[i]
		local, err := stack.Pop(v, null)
		if err != nil {
			return err
		}
		if v.Name != "unused" {
			locals[local.Name()] = local
		}
	}
	for _, v := range effect.Outputs {
		var local *Local
		if l, ok := locals[v.Name]; ok {
			local = l
		} else {
			local = LocalUnused(v, nil)
		}
		stack.Push(local)
	}
	return nil
}

// GetStackEffect runs every per-uop effect through a fresh Stack and
// returns the resulting state.
//
// CPython: Tools/cases_generator/stack.py:423-427 get_stack_effect
func GetStackEffect(inst any) (*Stack, error) {
	stack := NewStack()
	for _, eff := range stacksOf(inst) {
		if err := applyStackEffect(stack, eff); err != nil {
			return nil, err
		}
	}
	return stack, nil
}

// Storage wraps a Stack with the higher-level inputs/outputs lists and
// the spill counter that the case-body emitter walks.
//
// CPython: Tools/cases_generator/stack.py:430-438 Storage
type Storage struct {
	Stack         *Stack
	Inputs        []*Local
	Outputs       []*Local
	Peeks         int
	CheckLiveness bool
	Spilled       int
}

// storageNeedsDefining reports whether var is an output that must be
// declared before use.
//
// CPython: Tools/cases_generator/stack.py:440-447 Storage.needs_defining
func storageNeedsDefining(v *Local) bool {
	return !v.Item.Peek && !v.InLocal && !v.IsArray() && v.Name() != "unused"
}

// storageIsLive reports whether v is a live local at the case boundary.
//
// CPython: Tools/cases_generator/stack.py:449-457 Storage.is_live
func storageIsLive(v *Local) bool {
	if v.Name() == "unused" {
		return false
	}
	return v.InLocal || v.MemoryOffset != nil
}

// ClearInputs pops every input above peeks, requiring them to be dead.
//
// CPython: Tools/cases_generator/stack.py:459-466 Storage.clear_inputs
func (s *Storage) ClearInputs(reason string) error {
	for len(s.Inputs) > s.Peeks {
		tos := s.Inputs[len(s.Inputs)-1]
		s.Inputs = s.Inputs[:len(s.Inputs)-1]
		if storageIsLive(tos) && s.CheckLiveness {
			return newStackError("Input '%s' is still live %s", tos.Name(), reason)
		}
		if err := s.Stack.Drop(tos.Item, s.CheckLiveness); err != nil {
			return err
		}
	}
	return nil
}

// ClearDeadInputs pops dead inputs from the top, then verifies that
// no live input sits below a dead one.
//
// CPython: Tools/cases_generator/stack.py:468-481 Storage.clear_dead_inputs
func (s *Storage) ClearDeadInputs() error {
	live := ""
	for len(s.Inputs) > s.Peeks {
		tos := s.Inputs[len(s.Inputs)-1]
		if storageIsLive(tos) {
			live = tos.Name()
			break
		}
		s.Inputs = s.Inputs[:len(s.Inputs)-1]
		if err := s.Stack.Drop(tos.Item, s.CheckLiveness); err != nil {
			return err
		}
	}
	for _, v := range s.Inputs[s.Peeks:] {
		if !storageIsLive(v) {
			return newStackError("Input '%s' is not live, but '%s' is", v.Name(), live)
		}
	}
	return nil
}

// pushDefinedOutputs flushes any output whose register is already
// defined onto the underlying stack stash.
//
// CPython: Tools/cases_generator/stack.py:483-501 Storage._push_defined_outputs
func (s *Storage) pushDefinedOutputs() error {
	definedOutput := ""
	for _, o := range s.Outputs {
		if o.InLocal && o.MemoryOffset == nil {
			definedOutput = o.Name()
		}
	}
	if definedOutput == "" {
		return nil
	}
	if err := s.ClearInputs(fmt.Sprintf("when output '%s' is defined", definedOutput)); err != nil {
		return err
	}
	for len(s.Outputs) > s.Peeks && !storageNeedsDefining(s.Outputs[s.Peeks]) {
		out := s.Outputs[s.Peeks]
		s.Outputs = append(s.Outputs[:s.Peeks], s.Outputs[s.Peeks+1:]...)
		s.Stack.Push(out)
	}
	return nil
}

// LocalsCached reports whether any output sits in a register.
//
// CPython: Tools/cases_generator/stack.py:503-507 Storage.locals_cached
func (s *Storage) LocalsCached() bool {
	for _, o := range s.Outputs {
		if o.InLocal {
			return true
		}
	}
	return false
}

// Flush commits dead inputs, defined outputs, and the underlying stack.
//
// CPython: Tools/cases_generator/stack.py:509-512 Storage.flush
func (s *Storage) Flush(out *CWriter) error {
	if err := s.ClearDeadInputs(); err != nil {
		return err
	}
	if err := s.pushDefinedOutputs(); err != nil {
		return err
	}
	s.Stack.Flush(out)
	return nil
}

// Save records the start of a spill region.
//
// CPython: Tools/cases_generator/stack.py:514-519 Storage.save
func (s *Storage) Save(out *CWriter) {
	if s.Spilled == 0 {
		out.StartLine()
		out.EmitSpill()
	}
	s.Spilled++
}

// SaveInputs flushes the stack and arms a spill edge in one step.
//
// CPython: Tools/cases_generator/stack.py:521-528 Storage.save_inputs
func (s *Storage) SaveInputs(out *CWriter) error {
	if s.Spilled == 0 {
		if err := s.ClearDeadInputs(); err != nil {
			return err
		}
		s.Stack.Flush(out)
		out.StartLine()
		out.EmitSpill()
	}
	s.Spilled++
	return nil
}

// Reload pops a spill region; emits a reload edge when balanced.
//
// CPython: Tools/cases_generator/stack.py:530-537 Storage.reload
func (s *Storage) Reload(out *CWriter) error {
	if s.Spilled == 0 {
		return newStackError("Cannot reload stack as it hasn't been saved")
	}
	s.Spilled--
	if s.Spilled == 0 {
		out.StartLine()
		out.EmitReload()
	}
	return nil
}

// StorageForUop builds a Storage capturing one uop's effect.
//
// CPython: Tools/cases_generator/stack.py:539-559 Storage.for_uop
func StorageForUop(stack *Stack, uop *Uop, out *CWriter, checkLiveness bool) (*Storage, error) {
	var inputs []*Local
	var peeks []*Local
	ins := uop.Stack.Inputs
	for i := len(ins) - 1; i >= 0; i-- {
		in := ins[i]
		local, err := stack.Pop(in, out)
		if err != nil {
			return nil, err
		}
		if in.Peek {
			peeks = append(peeks, local)
		}
		inputs = append(inputs, local)
	}
	// reverse
	for i, j := 0, len(inputs)-1; i < j; i, j = i+1, j-1 {
		inputs[i], inputs[j] = inputs[j], inputs[i]
	}
	for i, j := 0, len(peeks)-1; i < j; i, j = i+1, j-1 {
		peeks[i], peeks[j] = peeks[j], peeks[i]
	}
	offset := stack.LogicalSp.Sub(stack.PhysicalSp)
	for _, output := range uop.Stack.Outputs {
		if output.IsArray() && output.Used && !output.Peek {
			out.Emit(fmt.Sprintf("%s = &stack_pointer[%s];\n", output.Name, offset.ToC()))
		}
		offset = offset.Push(output)
	}
	for _, v := range inputs {
		stack.Push(v)
	}
	outputs := make([]*Local, 0, len(peeks)+len(uop.Stack.Outputs))
	outputs = append(outputs, peeks...)
	for _, v := range uop.Stack.Outputs {
		if !v.Peek {
			outputs = append(outputs, LocalUndefined(v))
		}
	}
	return &Storage{
		Stack:         stack,
		Inputs:        inputs,
		Outputs:       outputs,
		Peeks:         len(peeks),
		CheckLiveness: checkLiveness,
	}, nil
}

// copyLocalList clones a list of Locals.
//
// CPython: Tools/cases_generator/stack.py:561-563 Storage.copy_list
func copyLocalList(arg []*Local) []*Local {
	out := make([]*Local, len(arg))
	for i, v := range arg {
		out[i] = v.Copy()
	}
	return out
}

// Copy clones the storage; inputs are remapped to point at the new
// stack's variables (matching the upstream identity check).
//
// CPython: Tools/cases_generator/stack.py:565-573 Storage.copy
func (s *Storage) Copy() *Storage {
	newStack := s.Stack.Copy()
	byName := map[string]*Local{}
	for _, v := range newStack.Variables {
		byName[v.Name()] = v
	}
	inputs := make([]*Local, 0, len(s.Inputs))
	for _, v := range s.Inputs {
		if l, ok := byName[v.Name()]; ok {
			inputs = append(inputs, l)
		} else {
			inputs = append(inputs, v.Copy())
		}
	}
	return &Storage{
		Stack:         newStack,
		Inputs:        inputs,
		Outputs:       copyLocalList(s.Outputs),
		Peeks:         s.Peeks,
		CheckLiveness: s.CheckLiveness,
		Spilled:       s.Spilled,
	}
}

// checkNames asserts no duplicate non-"unused" names appear.
//
// CPython: Tools/cases_generator/stack.py:575-583 Storage.check_names
func checkNames(locals []*Local) error {
	names := map[string]struct{}{}
	for _, v := range locals {
		if v.Name() == "unused" {
			continue
		}
		if _, dup := names[v.Name()]; dup {
			return newStackError("Duplicate name %s", v.Name())
		}
		names[v.Name()] = struct{}{}
	}
	return nil
}

// SanityCheck runs checkNames over all three name sets.
//
// CPython: Tools/cases_generator/stack.py:585-588 Storage.sanity_check
func (s *Storage) SanityCheck() error {
	if err := checkNames(s.Inputs); err != nil {
		return err
	}
	if err := checkNames(s.Outputs); err != nil {
		return err
	}
	return checkNames(s.Stack.Variables)
}

// IsFlushed reports whether stack and outputs are all in memory.
//
// CPython: Tools/cases_generator/stack.py:590-594 Storage.is_flushed
func (s *Storage) IsFlushed() bool {
	for _, v := range s.Outputs {
		if v.InLocal && v.MemoryOffset == nil {
			return false
		}
	}
	return s.Stack.IsFlushed()
}

// Merge unifies two storages across a join.
//
// CPython: Tools/cases_generator/stack.py:596-621 Storage.merge
func (s *Storage) Merge(other *Storage, out *CWriter) error {
	if err := s.SanityCheck(); err != nil {
		return err
	}
	if len(s.Inputs) != len(other.Inputs) {
		if err := s.ClearDeadInputs(); err != nil {
			return err
		}
		if err := other.ClearDeadInputs(); err != nil {
			return err
		}
	}
	if len(s.Inputs) != len(other.Inputs) && s.CheckLiveness {
		var diff *Local
		if len(s.Inputs) > len(other.Inputs) {
			diff = s.Inputs[len(s.Inputs)-1]
		} else {
			diff = other.Inputs[len(other.Inputs)-1]
		}
		s.printComment(out)
		other.printComment(out)
		return newStackError("Unmergeable inputs. Differing state of '%s'", diff.Name())
	}
	for i, v := range s.Inputs {
		ov := other.Inputs[i]
		if v.InLocal != ov.InLocal {
			return newStackError("'%s' is cleared on some paths, but not all", v.Name())
		}
	}
	if len(s.Outputs) != len(other.Outputs) {
		if err := s.pushDefinedOutputs(); err != nil {
			return err
		}
		if err := other.pushDefinedOutputs(); err != nil {
			return err
		}
	}
	if len(s.Outputs) != len(other.Outputs) {
		var v *Local
		if len(s.Outputs) > len(other.Outputs) {
			v = s.Outputs[0]
		} else {
			v = other.Outputs[0]
		}
		return newStackError("'%s' is set on some paths, but not all", v.Name())
	}
	for i, v := range s.Outputs {
		ov := other.Outputs[i]
		switch {
		case v.MemoryOffset == nil:
			ov.MemoryOffset = nil
		case ov.MemoryOffset == nil:
			v.MemoryOffset = nil
		}
	}
	if err := s.Stack.Merge(other.Stack, out); err != nil {
		return err
	}
	return s.SanityCheck()
}

// PushOutputs flushes any remaining outputs onto the stack stash and
// asserts there are no dangling spills.
//
// CPython: Tools/cases_generator/stack.py:623-635 Storage.push_outputs
func (s *Storage) PushOutputs() error {
	if s.Spilled != 0 {
		return newStackError("Unbalanced stack spills")
	}
	if err := s.ClearInputs("at the end of the micro-op"); err != nil {
		return err
	}
	if len(s.Inputs) > s.Peeks && s.CheckLiveness {
		return newStackError("Input variable '%s' is still live", s.Inputs[len(s.Inputs)-1].Name())
	}
	if err := s.pushDefinedOutputs(); err != nil {
		return err
	}
	if len(s.Outputs) > 0 {
		for _, o := range s.Outputs[s.Peeks:] {
			if storageNeedsDefining(o) {
				return newStackError("Output variable '%s' is not defined", s.Outputs[0].Name())
			}
			s.Stack.Push(o)
		}
		s.Outputs = nil
	}
	return nil
}

// AsComment renders a debug-friendly summary line.
//
// CPython: Tools/cases_generator/stack.py:637-642 Storage.as_comment
func (s *Storage) AsComment() string {
	stackComment := s.Stack.AsComment()
	nextLine := "\n                  "
	ins := make([]string, 0, len(s.Inputs))
	for _, v := range s.Inputs {
		ins = append(ins, v.CompactStr())
	}
	outs := make([]string, 0, len(s.Outputs))
	for _, v := range s.Outputs {
		outs = append(outs, v.CompactStr())
	}
	return fmt.Sprintf(
		"%s%sinputs: %s outputs: %s*/",
		stackComment[:len(stackComment)-2],
		nextLine,
		strings.Join(ins, ", "),
		strings.Join(outs, ", "),
	)
}

func (s *Storage) printComment(out *CWriter) {
	if PrintStacks {
		out.Emit(s.AsComment() + "\n")
	}
}

// closeNamedHelper bundles the inner closure used by CloseInputs.
type closeNamedHelper struct {
	storage    *Storage
	out        *CWriter
	tmpDefined bool
}

func (h *closeNamedHelper) closeNamed(closeMacro, name, overwrite string) {
	if overwrite != "" {
		if !h.tmpDefined {
			h.out.Emit("_PyStackRef ")
			h.tmpDefined = true
		}
		h.out.Emit(fmt.Sprintf("tmp = %s;\n", name))
		h.out.Emit(fmt.Sprintf("%s = %s;\n", name, overwrite))
		h.storage.Stack.SaveVariables(h.out)
		h.out.Emit(fmt.Sprintf("%s(tmp);\n", closeMacro))
	} else {
		h.out.Emit(fmt.Sprintf("%s(%s);\n", closeMacro, name))
	}
}

func (h *closeNamedHelper) closeVariable(v *Local, overwrite string) error {
	closeMacro := "PyStackRef_CLOSE"
	if strings.Contains(v.Name(), "null") {
		closeMacro = "PyStackRef_XCLOSE"
	}
	v.MemoryOffset = nil
	h.storage.Save(h.out)
	h.out.StartLine()
	switch {
	case v.Size() == "":
		h.closeNamed(closeMacro, v.Name(), overwrite)
	case v.Size() == "1":
		h.closeNamed(closeMacro, fmt.Sprintf("%s[0]", v.Name()), overwrite)
	default:
		if overwrite != "" && !h.tmpDefined {
			h.out.Emit("_PyStackRef tmp;\n")
			h.tmpDefined = true
		}
		h.out.Emit(fmt.Sprintf("for (int _i = %s; --_i >= 0;) {\n", v.Size()))
		h.closeNamed(closeMacro, fmt.Sprintf("%s[_i]", v.Name()), overwrite)
		h.out.Emit("}\n")
	}
	return h.storage.Reload(h.out)
}

// CloseInputs walks the inputs and emits PyStackRef_CLOSE calls,
// preserving live outputs as required by DECREF_INPUTS semantics.
//
// CPython: Tools/cases_generator/stack.py:648-737 Storage.close_inputs
func (s *Storage) CloseInputs(out *CWriter) error {
	helper := &closeNamedHelper{storage: s, out: out}
	if err := s.ClearDeadInputs(); err != nil {
		return err
	}
	if len(s.Inputs) == 0 {
		return nil
	}
	lowest := s.Inputs[0]
	var output *Local
	for _, v := range s.Outputs {
		switch {
		case v.IsArray():
			if len(s.Inputs) > 1 {
				return newStackError("Cannot call DECREF_INPUTS with array output and more than one input")
			}
			output = v
		case v.InLocal:
			if output != nil {
				return newStackError("Cannot call DECREF_INPUTS with more than one live output")
			}
			output = v
		}
	}
	if output != nil {
		if output.IsArray() {
			if err := s.Stack.Drop(s.Inputs[0].Item, false); err != nil {
				return err
			}
			s.Stack.Push(output)
			s.Stack.Flush(out)
			if err := helper.closeVariable(s.Inputs[0], ""); err != nil {
				return err
			}
			if err := s.Stack.Drop(output.Item, s.CheckLiveness); err != nil {
				return err
			}
			s.Inputs = nil
			return nil
		}
		if varSize(lowest.Item) != varSize(output.Item) {
			return newStackError("Cannot call DECREF_INPUTS with live output not matching first input size")
		}
		s.Stack.Flush(out)
		lowest.InLocal = true
		if err := helper.closeVariable(lowest, output.Name()); err != nil {
			return err
		}
		if lowest.MemoryOffset == nil {
			return newStackError("internal: lowest.memory_offset is nil after closeVariable")
		}
	}
	for i := len(s.Inputs) - 1; i >= 1; i-- {
		if err := helper.closeVariable(s.Inputs[i], "PyStackRef_NULL"); err != nil {
			return err
		}
	}
	if output == nil {
		if err := helper.closeVariable(s.Inputs[0], "PyStackRef_NULL"); err != nil {
			return err
		}
	}
	for i := len(s.Inputs) - 1; i >= 1; i-- {
		s.Inputs[i].Kill()
		if err := s.Stack.Drop(s.Inputs[i].Item, s.CheckLiveness); err != nil {
			return err
		}
	}
	if output == nil {
		s.Inputs[0].Kill()
	}
	if err := s.Stack.Drop(s.Inputs[0].Item, false); err != nil {
		return err
	}
	outputInPlace := len(s.Outputs) > 0 && output != nil && output == s.Outputs[0] && lowest.MemoryOffset != nil
	if outputInPlace {
		off := *lowest.MemoryOffset
		output.MemoryOffset = &off
	} else {
		s.Stack.Flush(out)
	}
	if output != nil {
		s.Stack.Push(output)
	}
	s.Inputs = nil
	if outputInPlace {
		s.Stack.Flush(out)
	}
	if output != nil {
		var err error
		output, err = s.Stack.Pop(output.Item, out)
		if err != nil {
			return err
		}
		_ = output
	}
	return nil
}
