// Analyzer for parsed bytecode definitions: classifies uops, computes properties, escape analysis, opcode assignment.
//
// CPython: Tools/cases_generator/analyzer.py
package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// CodeDef is the union of InstDef and LabelDef, mirroring parser.CodeDef.
//
// CPython: Tools/cases_generator/parsing.py CodeDef alias
type CodeDef interface {
	Node
	codeDefName() string
}

func (i *InstDef) codeDefName() string  { return i.Name }
func (l *LabelDef) codeDefName() string { return l.Name }

// EscapingCall records a token that triggers an escaping API call,
// optionally pinned to an identifier it kills (PyStackRef_CLOSE).
//
// CPython: Tools/cases_generator/analyzer.py:10-14 EscapingCall
type EscapingCall struct {
	Stmt  *SimpleStmt
	Call  Token
	Kills *Token
}

// Properties is the bag of booleans summarizing one uop or label.
//
// CPython: Tools/cases_generator/analyzer.py:16-39 Properties
type Properties struct {
	EscapingCalls   map[*SimpleStmt]EscapingCall
	Escapes         bool
	ErrorWithPop    bool
	ErrorWithoutPop bool
	Deopts          bool
	Oparg           bool
	Jumps           bool
	EvalBreaker     bool
	NeedsThis       bool
	AlwaysExits     bool
	StoresSp        bool
	UsesCoConsts    bool
	UsesCoNames     bool
	UsesLocals      bool
	HasFree         bool
	SideExit        bool
	Pure            bool
	UsesOpcode      bool
	Tier            int // 0 == unset
	ConstOparg      int // -1 default
	NeedsPrev       bool
	NoSaveIP        bool
}

// Infallible mirrors Properties.infallible.
//
// CPython: Tools/cases_generator/analyzer.py:78-80 Properties.infallible
func (p *Properties) Infallible() bool {
	return !p.ErrorWithPop && !p.ErrorWithoutPop
}

// PropertiesFromList aggregates a slice of Properties using the same
// any/all reductions as the upstream from_list classmethod.
//
// CPython: Tools/cases_generator/analyzer.py:50-76 Properties.from_list
func PropertiesFromList(props []*Properties) *Properties {
	out := &Properties{
		EscapingCalls: map[*SimpleStmt]EscapingCall{},
		Pure:          true,
		NoSaveIP:      true,
		ConstOparg:    -1,
	}
	if len(props) == 0 {
		out.Pure = false
		out.NoSaveIP = false
		return out
	}
	for _, p := range props {
		for k, v := range p.EscapingCalls {
			out.EscapingCalls[k] = v
		}
		out.Escapes = out.Escapes || p.Escapes
		out.ErrorWithPop = out.ErrorWithPop || p.ErrorWithPop
		out.ErrorWithoutPop = out.ErrorWithoutPop || p.ErrorWithoutPop
		out.Deopts = out.Deopts || p.Deopts
		out.Oparg = out.Oparg || p.Oparg
		out.Jumps = out.Jumps || p.Jumps
		out.EvalBreaker = out.EvalBreaker || p.EvalBreaker
		out.NeedsThis = out.NeedsThis || p.NeedsThis
		out.AlwaysExits = out.AlwaysExits || p.AlwaysExits
		out.StoresSp = out.StoresSp || p.StoresSp
		out.UsesCoConsts = out.UsesCoConsts || p.UsesCoConsts
		out.UsesCoNames = out.UsesCoNames || p.UsesCoNames
		out.UsesLocals = out.UsesLocals || p.UsesLocals
		out.UsesOpcode = out.UsesOpcode || p.UsesOpcode
		out.HasFree = out.HasFree || p.HasFree
		out.SideExit = out.SideExit || p.SideExit
		out.Pure = out.Pure && p.Pure
		out.NeedsPrev = out.NeedsPrev || p.NeedsPrev
		out.NoSaveIP = out.NoSaveIP && p.NoSaveIP
	}
	return out
}

// SkipProperties is the all-zero Properties used by Skip/Flush parts.
//
// CPython: Tools/cases_generator/analyzer.py:82-102 SKIP_PROPERTIES
func SkipProperties() *Properties {
	return &Properties{
		EscapingCalls: map[*SimpleStmt]EscapingCall{},
		Pure:          true,
		ConstOparg:    -1,
	}
}

// StackItem is the analyzer-side stack slot, adding peek/used flags
// that the parser-side StackEffect lacks.
//
// CPython: Tools/cases_generator/analyzer.py:135-152 StackItem
type StackItem struct {
	Name string
	Type string
	Size string
	Peek bool
	Used bool
}

// IsArray reports whether the slot has a [size] suffix.
//
// CPython: Tools/cases_generator/analyzer.py:148-149 StackItem.is_array
func (s *StackItem) IsArray() bool { return s.Size != "" }

// GetSize returns Size if set, "1" otherwise.
//
// CPython: Tools/cases_generator/analyzer.py:151-152 StackItem.get_size
func (s *StackItem) GetSize() string {
	if s.Size != "" {
		return s.Size
	}
	return "1"
}

// AnalyzedStackEffect is the (inputs, outputs) pair attached to a Uop.
//
// CPython: Tools/cases_generator/analyzer.py:155-161 StackEffect
type AnalyzedStackEffect struct {
	Inputs  []*StackItem
	Outputs []*StackItem
}

// CacheEntry is one analyzed cache slot.
//
// CPython: Tools/cases_generator/analyzer.py:164-170 CacheEntry
type CacheEntry struct {
	Name string
	Size int
}

// Skip is an unused cache entry.
//
// CPython: Tools/cases_generator/analyzer.py:105-116 Skip
type Skip struct {
	SizeVal int
}

func (s *Skip) PartName() string            { return fmt.Sprintf("unused/%d", s.SizeVal) }
func (s *Skip) PartSize() int               { return s.SizeVal }
func (s *Skip) PartProperties() *Properties { return SkipProperties() }
func (*Skip) isPart()                       {}

// Flush is an explicit "flush" placeholder appearing inside macros.
//
// CPython: Tools/cases_generator/analyzer.py:119-130 Flush
type Flush struct{}

func (*Flush) PartName() string            { return "flush" }
func (*Flush) PartSize() int               { return 0 }
func (*Flush) PartProperties() *Properties { return SkipProperties() }
func (*Flush) isPart()                     {}

// Part is the union of Uop, Skip, Flush appearing inside an Instruction.
//
// CPython: Tools/cases_generator/analyzer.py:246 Part
type Part interface {
	PartName() string
	PartSize() int
	PartProperties() *Properties
	isPart()
}

// Uop is the central per-uop analyzed record.
//
// CPython: Tools/cases_generator/analyzer.py:173-227 Uop
type Uop struct {
	Name              string
	UopContext        *Context
	Annotations       []string
	Stack             AnalyzedStackEffect
	Caches            []CacheEntry
	LocalStores       []Token
	Body              *BlockStmt
	Properties        *Properties
	cachedSize        int // negative until computed
	ImplicitlyCreated bool
	Replicated        int
	Replicates        *Uop
	InstructionSize   *int
}

func (*Uop) isPart() {}

// PartName returns the uop name, satisfying Part.
func (u *Uop) PartName() string { return u.Name }

// PartSize returns the cached cache-byte total.
func (u *Uop) PartSize() int { return u.Size() }

// PartProperties returns the uop's properties.
func (u *Uop) PartProperties() *Properties { return u.Properties }

// Size lazily sums the cache-entry sizes (analyzer.py caches via _size).
//
// CPython: Tools/cases_generator/analyzer.py:197-201 Uop.size
func (u *Uop) Size() int {
	if u.cachedSize < 0 {
		s := 0
		for _, c := range u.Caches {
			s += c.Size
		}
		u.cachedSize = s
	}
	return u.cachedSize
}

// WhyNotViable returns a non-empty reason when the uop is not Tier-2 viable.
//
// CPython: Tools/cases_generator/analyzer.py:203-218 Uop.why_not_viable
func (u *Uop) WhyNotViable() string {
	if u.Name == "_SAVE_RETURN_OFFSET" {
		return ""
	}
	if strings.Contains(u.Name, "INSTRUMENTED") {
		return "is instrumented"
	}
	for _, a := range u.Annotations {
		if a == "replaced" {
			return "is replaced"
		}
	}
	if u.Name == "INTERPRETER_EXIT" || u.Name == "JUMP_BACKWARD" {
		return "has tier 1 control flow"
	}
	if u.Properties.NeedsThis {
		return "uses the 'this_instr' variable"
	}
	used := 0
	for _, c := range u.Caches {
		if c.Name != "unused" {
			used++
		}
	}
	if used > 2 {
		return "has too many cache entries"
	}
	if u.Properties.ErrorWithPop && u.Properties.ErrorWithoutPop {
		return "has both popping and not-popping errors"
	}
	return ""
}

// IsViable reports WhyNotViable() == "".
//
// CPython: Tools/cases_generator/analyzer.py:220-221 Uop.is_viable
func (u *Uop) IsViable() bool { return u.WhyNotViable() == "" }

// IsSuper reports whether the body references oparg1 (a super-instruction).
//
// CPython: Tools/cases_generator/analyzer.py:223-227 Uop.is_super
func (u *Uop) IsSuper() bool {
	for _, t := range blockTokens(u.Body) {
		if t.Kind == TokIdentifier && t.Text == "oparg1" {
			return true
		}
	}
	return false
}

// Label is one [spilled] label() definition with computed properties.
//
// CPython: Tools/cases_generator/analyzer.py:230-243 Label
type Label struct {
	Name            string
	Spilled         bool
	Body            *BlockStmt
	Properties      *Properties
	SizeVal         int
	LocalStores     []Token
	InstructionSize *int
}

// Instruction is a top-level inst()/macro() entry: a name plus an
// ordered list of Parts.
//
// CPython: Tools/cases_generator/analyzer.py:250-285 Instruction
type Instruction struct {
	Where      Token
	Name       string
	Parts      []Part
	properties *Properties
	IsTarget   bool
	Family     *AnalyzedFamily
	Opcode     int
}

// Properties returns the lazily-computed aggregated properties.
//
// CPython: Tools/cases_generator/analyzer.py:260-267 Instruction.properties
func (i *Instruction) Props() *Properties {
	if i.properties == nil {
		ps := make([]*Properties, len(i.Parts))
		for j, p := range i.Parts {
			ps[j] = p.PartProperties()
		}
		i.properties = PropertiesFromList(ps)
	}
	return i.properties
}

// Size is 1 + sum of part sizes.
//
// CPython: Tools/cases_generator/analyzer.py:273-275 Instruction.size
func (i *Instruction) Size() int {
	s := 1
	for _, p := range i.Parts {
		s += p.PartSize()
	}
	return s
}

// IsSuper reports whether this instruction has a single super-uop part.
//
// CPython: Tools/cases_generator/analyzer.py:277-284 Instruction.is_super
func (i *Instruction) IsSuper() bool {
	if len(i.Parts) != 1 {
		return false
	}
	if u, ok := i.Parts[0].(*Uop); ok {
		return u.IsSuper()
	}
	return false
}

// PseudoInstruction is one pseudo() declaration, post-analysis.
//
// CPython: Tools/cases_generator/analyzer.py:287-301 PseudoInstruction
type PseudoInstruction struct {
	Name       string
	Stack      AnalyzedStackEffect
	Targets    []*Instruction
	AsSequence bool
	Flags      []string
	Opcode     int
}

// Props aggregates the targets' properties.
//
// CPython: Tools/cases_generator/analyzer.py:299-301 PseudoInstruction.properties
func (p *PseudoInstruction) Props() *Properties {
	ps := make([]*Properties, len(p.Targets))
	for i, t := range p.Targets {
		ps[i] = t.Props()
	}
	return PropertiesFromList(ps)
}

// AnalyzedFamily is one family() declaration with resolved members.
//
// CPython: Tools/cases_generator/analyzer.py:304-311 Family
type AnalyzedFamily struct {
	Name    string
	SizeStr string
	Members []*Instruction
}

// Analysis is the top-level forest of analyzed declarations.
//
// CPython: Tools/cases_generator/analyzer.py:314-323 Analysis
type Analysis struct {
	Instructions    map[string]*Instruction
	Uops            map[string]*Uop
	Families        map[string]*AnalyzedFamily
	Pseudos         map[string]*PseudoInstruction
	Labels          map[string]*Label
	Opmap           map[string]int
	HaveArg         int
	MinInstrumented int
}

// analysisError builds a SyntaxError tied to tkn.
//
// CPython: Tools/cases_generator/analyzer.py:326-329 analysis_error
func analysisError(message string, tkn Token) *SyntaxError {
	return &SyntaxError{
		Message:  message,
		Filename: tkn.Filename,
		Line:     tkn.Begin.Line,
		Column:   tkn.Begin.Col,
	}
}

// overrideError reports a duplicate definition.
//
// CPython: Tools/cases_generator/analyzer.py:332-342 override_error
func overrideError(name string, ctx, prev *Context, tkn Token) *SyntaxError {
	return analysisError(
		fmt.Sprintf("Duplicate definition of '%s' @ %v previous definition @ %v", name, ctx, prev),
		tkn,
	)
}

// convertStackItem maps a parser StackEffect to an analyzer StackItem.
//
// CPython: Tools/cases_generator/analyzer.py:345-348 convert_stack_item
func convertStackItem(item *StackEffect) *StackItem {
	return &StackItem{Name: item.Name, Type: item.Type, Size: item.Size}
}

// inputName is the display name of an input effect for diagnostics.
func inputFirstToken(in InputEffect) Token {
	switch v := in.(type) {
	case *StackEffect:
		return v.FirstToken()
	case *CacheEffect:
		return v.FirstToken()
	}
	return Token{}
}

func inputName(in InputEffect) string {
	switch v := in.(type) {
	case *StackEffect:
		return v.Name
	case *CacheEffect:
		return v.Name
	}
	return ""
}

// checkUnused enforces "unused inputs cannot sit above used non-peek items".
//
// CPython: Tools/cases_generator/analyzer.py:350-359 check_unused
func checkUnused(stack []*StackItem, inputNames map[string]Token) error {
	seenUnused := false
	for i := len(stack) - 1; i >= 0; i-- {
		item := stack[i]
		switch {
		case item.Name == "unused":
			seenUnused = true
		case item.Peek:
			return nil
		case seenUnused:
			return analysisError(
				fmt.Sprintf("Cannot have used input '%s' below an unused value on the stack", item.Name),
				inputNames[item.Name],
			)
		}
	}
	return nil
}

// analyzeStack converts an InstDef or Pseudo's parser-side inputs/outputs
// into the analyzer's StackEffect, marking peek and used flags.
//
// CPython: Tools/cases_generator/analyzer.py:362-406 analyze_stack
func analyzeStack(op Node) (AnalyzedStackEffect, error) {
	var (
		inputs    []*StackItem
		outputs   []*StackItem
		opInputs  []InputEffect
		opOutputs []OutputEffect
		isInst    bool
		instDef   *InstDef
	)
	switch v := op.(type) {
	case *InstDef:
		opInputs = v.Inputs
		opOutputs = v.Outputs
		isInst = true
		instDef = v
	case *Pseudo:
		opInputs = v.Inputs
		opOutputs = v.Outputs
	default:
		return AnalyzedStackEffect{}, fmt.Errorf("analyzeStack: unsupported %T", op)
	}
	for _, in := range opInputs {
		if se, ok := in.(*StackEffect); ok {
			inputs = append(inputs, convertStackItem(se))
		}
	}
	for _, out := range opOutputs {
		if se, ok := out.(*StackEffect); ok {
			outputs = append(outputs, convertStackItem(se))
		}
	}
	inputNames := map[string]Token{}
	for _, in := range opInputs {
		n := inputName(in)
		if n != "" && n != "unused" {
			inputNames[n] = inputFirstToken(in)
		}
	}
	modified := false
	maxLen := len(inputs)
	if len(outputs) > maxLen {
		maxLen = len(outputs)
	}
	for i := 0; i < maxLen; i++ {
		var inItem, outItem *StackItem
		if i < len(inputs) {
			inItem = inputs[i]
		}
		if i < len(outputs) {
			outItem = outputs[i]
		}
		switch {
		case outItem == nil:
			// no-op
		case inItem == nil:
			if _, ok := inputNames[outItem.Name]; ok {
				return AnalyzedStackEffect{}, analysisError(
					fmt.Sprintf("Reuse of variable '%s' at different stack location", outItem.Name),
					inputNames[outItem.Name],
				)
			}
		case inItem.Name == outItem.Name:
			if !modified {
				inItem.Peek = true
				outItem.Peek = true
			}
		default:
			modified = true
			if _, ok := inputNames[outItem.Name]; ok {
				return AnalyzedStackEffect{}, analysisError(
					fmt.Sprintf("Reuse of variable '%s' at different stack location", outItem.Name),
					inputNames[outItem.Name],
				)
			}
		}
	}
	if isInst {
		outNames := map[string]struct{}{}
		for _, o := range outputs {
			outNames[o.Name] = struct{}{}
		}
		for _, in := range inputs {
			_, inOut := outNames[in.Name]
			if variableUsed(instDef, in.Name) ||
				variableUsed(instDef, "DECREF_INPUTS") ||
				(!in.Peek && inOut) {
				in.Used = true
			}
		}
		for _, out := range outputs {
			if variableUsed(instDef, out.Name) {
				out.Used = true
			}
		}
	}
	if err := checkUnused(inputs, inputNames); err != nil {
		return AnalyzedStackEffect{}, err
	}
	return AnalyzedStackEffect{Inputs: inputs, Outputs: outputs}, nil
}

// analyzeCaches collects CacheEntry slots and rejects unused first/last.
//
// CPython: Tools/cases_generator/analyzer.py:409-421 analyze_caches
func analyzeCaches(inputs []InputEffect) ([]CacheEntry, error) {
	var caches []*CacheEffect
	for _, in := range inputs {
		if c, ok := in.(*CacheEffect); ok {
			caches = append(caches, c)
		}
	}
	if len(caches) > 0 {
		for _, idx := range []int{0, len(caches) - 1} {
			c := caches[idx]
			if c.Name == "unused" {
				position := "First"
				if idx != 0 {
					position = "Last"
				}
				tok := c.FirstToken()
				return nil, analysisError(
					fmt.Sprintf("%s cache entry in op is unused. Move to enclosing macro.", position),
					tok,
				)
			}
		}
	}
	out := make([]CacheEntry, 0, len(caches))
	for _, c := range caches {
		out = append(out, CacheEntry{Name: c.Name, Size: c.Size})
	}
	return out, nil
}

// findVariableStores collects identifier tokens that are stored to
// (LHS of '=', or address-taken with '&') matching an input or output.
//
// CPython: Tools/cases_generator/analyzer.py:424-455 find_variable_stores
func findVariableStores(node *InstDef) ([]Token, error) {
	var res []Token
	outNames := map[string]struct{}{}
	inNames := map[string]struct{}{}
	for _, o := range node.Outputs {
		if se, ok := o.(*StackEffect); ok {
			outNames[se.Name] = struct{}{}
		}
	}
	for _, in := range node.Inputs {
		if se, ok := in.(*StackEffect); ok {
			inNames[se.Name] = struct{}{}
		}
	}

	findStores := func(tokens []Token, callback func(Token) error) error {
		for len(tokens) > 0 && tokens[0].Kind == TokComment {
			tokens = tokens[1:]
		}
		if len(tokens) < 4 {
			return nil
		}
		if tokens[1].Kind == TokEquals && tokens[0].Kind == TokIdentifier {
			name := tokens[0].Text
			if _, ok := outNames[name]; ok {
				if err := callback(tokens[0]); err != nil {
					return err
				}
			} else if _, ok := inNames[name]; ok {
				if err := callback(tokens[0]); err != nil {
					return err
				}
			}
		}
		for idx, tkn := range tokens {
			if tkn.Kind == TokAnd && idx+1 < len(tokens) {
				nameTkn := tokens[idx+1]
				if _, ok := outNames[nameTkn.Text]; ok {
					if err := callback(nameTkn); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}

	var visitErr error
	visitor := func(stmt Stmt) {
		if visitErr != nil {
			return
		}
		switch s := stmt.(type) {
		case *IfStmt:
			err := findStores(s.Condition, func(tkn Token) error {
				return analysisError("Cannot define variable in 'if' condition", tkn)
			})
			if err != nil {
				visitErr = err
			}
		case *SimpleStmt:
			err := findStores(s.Contents, func(tkn Token) error {
				res = append(res, tkn)
				return nil
			})
			if err != nil {
				visitErr = err
			}
		}
	}
	acceptBlock(node.Block, visitor)
	if visitErr != nil {
		return nil, visitErr
	}
	return res, nil
}

// acceptStmt applies visitor to stmt and its descendants matching CPython's accept().
//
// CPython: Tools/cases_generator/parsing.py accept methods
func acceptStmt(stmt Stmt, visitor func(Stmt)) {
	if stmt == nil {
		return
	}
	visitor(stmt)
	switch s := stmt.(type) {
	case *IfStmt:
		acceptStmt(s.Body, visitor)
		acceptStmt(s.ElseBody, visitor)
	case *ForStmt:
		acceptStmt(s.Body, visitor)
	case *WhileStmt:
		acceptStmt(s.Body, visitor)
	case *MacroIfStmt:
		for _, b := range s.Body {
			acceptStmt(b, visitor)
		}
		for _, b := range s.ElseBody {
			acceptStmt(b, visitor)
		}
	case *BlockStmt:
		for _, b := range s.Body {
			acceptStmt(b, visitor)
		}
	}
}

func acceptBlock(b *BlockStmt, visitor func(Stmt)) {
	if b == nil {
		return
	}
	acceptStmt(b, visitor)
}

// blockTokens yields every token reachable from a BlockStmt, mirroring
// the upstream BlockStmt.tokens() iterator.
//
// CPython: Tools/cases_generator/parsing.py:224-228 BlockStmt.tokens
func blockTokens(b *BlockStmt) []Token {
	if b == nil {
		return nil
	}
	var out []Token
	collectStmtTokens(b, &out)
	return out
}

func collectStmtTokens(stmt Stmt, out *[]Token) {
	switch s := stmt.(type) {
	case *BlockStmt:
		*out = append(*out, s.Open)
		for _, c := range s.Body {
			collectStmtTokens(c, out)
		}
		*out = append(*out, s.Close)
	case *IfStmt:
		*out = append(*out, s.If)
		*out = append(*out, s.Condition...)
		collectStmtTokens(s.Body, out)
		if s.Else != nil {
			*out = append(*out, *s.Else)
		}
		if s.ElseBody != nil {
			collectStmtTokens(s.ElseBody, out)
		}
	case *ForStmt:
		*out = append(*out, s.For)
		*out = append(*out, s.Header...)
		collectStmtTokens(s.Body, out)
	case *WhileStmt:
		*out = append(*out, s.While)
		*out = append(*out, s.Condition...)
		collectStmtTokens(s.Body, out)
	case *MacroIfStmt:
		*out = append(*out, s.Condition)
		for _, c := range s.Body {
			collectStmtTokens(c, out)
		}
		for _, c := range s.ElseBody {
			collectStmtTokens(c, out)
		}
	case *SimpleStmt:
		*out = append(*out, s.Contents...)
	}
}

// codeDefBlock returns the block for an InstDef or LabelDef.
func codeDefBlock(node CodeDef) *BlockStmt {
	switch v := node.(type) {
	case *InstDef:
		return v.Block
	case *LabelDef:
		return v.Block
	}
	return nil
}

func codeDefTokens(node CodeDef) []Token {
	switch v := node.(type) {
	case *InstDef:
		return v.Tokens()
	case *LabelDef:
		return v.Tokens()
	}
	return nil
}

func codeDefAnnotations(node CodeDef) []string {
	if v, ok := node.(*InstDef); ok {
		return v.Annotations
	}
	return nil
}

// variableUsed reports whether an IDENTIFIER token with the given name
// appears anywhere in node.block.
//
// CPython: Tools/cases_generator/analyzer.py:516-520 variable_used
func variableUsed(node CodeDef, name string) bool {
	for _, t := range blockTokens(codeDefBlock(node)) {
		if t.Kind == TokIdentifier && t.Text == name {
			return true
		}
	}
	return false
}

// opargUsed reports whether the bare identifier "oparg" appears anywhere in node.tokens.
//
// CPython: Tools/cases_generator/analyzer.py:523-527 oparg_used
func opargUsed(node CodeDef) bool {
	for _, t := range codeDefTokens(node) {
		if t.Kind == TokIdentifier && t.Text == "oparg" {
			return true
		}
	}
	return false
}

var tierAnnoRE = regexp.MustCompile(`^tier\d$`)

// tierVariable returns 1 for "specializing", N for "tierN", or 0/-1 sentinel for none.
//
// CPython: Tools/cases_generator/analyzer.py:530-540 tier_variable
func tierVariable(node CodeDef) int {
	if _, ok := node.(*LabelDef); ok {
		return 0
	}
	for _, t := range codeDefTokens(node) {
		if t.Kind == TokAnnotation {
			if t.Text == "specializing" {
				return 1
			}
			if tierAnnoRE.MatchString(t.Text) {
				return int(t.Text[len(t.Text)-1] - '0')
			}
		}
	}
	return 0
}

// hasErrorWithPop reports whether the body uses ERROR_IF / exception_unwind.
//
// CPython: Tools/cases_generator/analyzer.py:543-547 has_error_with_pop
func hasErrorWithPop(op CodeDef) bool {
	return variableUsed(op, "ERROR_IF") || variableUsed(op, "exception_unwind")
}

// hasErrorWithoutPop reports whether the body uses ERROR_NO_POP / exception_unwind.
//
// CPython: Tools/cases_generator/analyzer.py:550-554 has_error_without_pop
func hasErrorWithoutPop(op CodeDef) bool {
	return variableUsed(op, "ERROR_NO_POP") || variableUsed(op, "exception_unwind")
}

// nonEscapingFunctions is the carve-out set of names that do NOT count as
// escaping API calls when invoked inside a uop body.
//
// CPython: Tools/cases_generator/analyzer.py:557-682 NON_ESCAPING_FUNCTIONS
var nonEscapingFunctions = map[string]struct{}{}

func init() {
	names := []string{
		"PyCFunction_GET_FLAGS", "PyCFunction_GET_FUNCTION", "PyCFunction_GET_SELF",
		"PyCell_GetRef", "PyCell_New", "PyCell_SwapTakeRef",
		"PyExceptionInstance_Class", "PyException_GetCause", "PyException_GetContext",
		"PyException_GetTraceback", "PyFloat_AS_DOUBLE", "PyFloat_FromDouble",
		"PyFunction_GET_CODE", "PyFunction_GET_GLOBALS", "PyList_GET_ITEM",
		"PyList_GET_SIZE", "PyList_SET_ITEM", "PyLong_AsLong", "PyLong_FromLong",
		"PyLong_FromSsize_t", "PySlice_New", "PyStackRef_AsPyObjectBorrow",
		"PyStackRef_AsPyObjectNew", "PyStackRef_FromPyObjectNewMortal",
		"PyStackRef_AsPyObjectSteal", "PyStackRef_Borrow", "PyStackRef_CLEAR",
		"PyStackRef_CLOSE_SPECIALIZED", "PyStackRef_DUP", "PyStackRef_False",
		"PyStackRef_FromPyObjectImmortal", "PyStackRef_FromPyObjectNew",
		"PyStackRef_FromPyObjectSteal", "PyStackRef_IsExactly",
		"PyStackRef_FromPyObjectStealMortal", "PyStackRef_IsNone", "PyStackRef_Is",
		"PyStackRef_IsHeapSafe", "PyStackRef_IsTrue", "PyStackRef_IsFalse",
		"PyStackRef_IsNull", "PyStackRef_MakeHeapSafe", "PyStackRef_None",
		"PyStackRef_TYPE", "PyStackRef_True", "PyTuple_GET_ITEM", "PyTuple_GET_SIZE",
		"PyType_HasFeature", "PyUnicode_Concat", "PyUnicode_GET_LENGTH",
		"PyUnicode_READ_CHAR", "Py_ARRAY_LENGTH", "Py_FatalError", "Py_INCREF",
		"Py_IS_TYPE", "Py_NewRef", "Py_REFCNT", "Py_SIZE", "Py_TYPE",
		"Py_UNREACHABLE", "Py_Unicode_GET_LENGTH",
		"_PyCode_CODE", "_PyDictValues_AddToInsertionOrder", "_PyErr_Occurred",
		"_PyFloat_FromDouble_ConsumeInputs", "_PyFrame_GetBytecode",
		"_PyFrame_GetCode", "_PyFrame_IsIncomplete", "_PyFrame_PushUnchecked",
		"_PyFrame_SetStackPointer", "_PyFrame_StackPush", "_PyFunction_SetVersion",
		"_PyGen_GetGeneratorFromFrame", "_PyInterpreterState_GET",
		"_PyList_AppendTakeRef", "_PyList_ITEMS", "_PyLong_CompactValue",
		"_PyLong_DigitCount", "_PyLong_IsCompact", "_PyLong_IsNegative",
		"_PyLong_IsNonNegativeCompact", "_PyLong_IsZero",
		"_PyManagedDictPointer_IsValues", "_PyObject_GC_IS_SHARED",
		"_PyObject_GC_IS_TRACKED", "_PyObject_GC_MAY_BE_TRACKED",
		"_PyObject_GC_TRACK", "_PyObject_GetManagedDict", "_PyObject_InlineValues",
		"_PyObject_IsUniquelyReferenced", "_PyObject_ManagedDictPointer",
		"_PyThreadState_HasStackSpace", "_PyTuple_FromStackRefStealOnSuccess",
		"_PyTuple_ITEMS", "_PyType_HasFeature", "_PyType_NewManagedObject",
		"_PyUnicode_Equal", "_PyUnicode_JoinArray",
		"_Py_CHECK_EMSCRIPTEN_SIGNALS_PERIODICALLY", "_Py_DECREF_NO_DEALLOC",
		"_Py_ID", "_Py_IsImmortal", "_Py_IsOwnedByCurrentThread",
		"_Py_LeaveRecursiveCallPy", "_Py_LeaveRecursiveCallTstate", "_Py_NewRef",
		"_Py_SINGLETON", "_Py_STR", "_Py_TryIncrefCompare",
		"_Py_TryIncrefCompareStackRef", "_Py_atomic_compare_exchange_uint8",
		"_Py_atomic_load_ptr_acquire", "_Py_atomic_load_uintptr_relaxed",
		"_Py_set_eval_breaker_bit", "advance_backoff_counter", "assert",
		"backoff_counter_triggers", "initial_temperature_backoff_counter",
		"JUMP_TO_LABEL", "restart_backoff_counter", "_Py_ReachedRecursionLimit",
		"PyStackRef_IsTaggedInt", "PyStackRef_TagInt", "PyStackRef_UntagInt",
	}
	for _, n := range names {
		nonEscapingFunctions[n] = struct{}{}
	}
}

// checkEscapingCalls rejects any escaping call that lives inside an
// if/while condition (escapes must not happen in guards).
//
// CPython: Tools/cases_generator/analyzer.py:684-713 check_escaping_calls
func checkEscapingCalls(instr CodeDef, escapes map[*SimpleStmt]EscapingCall) error {
	calls := map[Token]struct{}{}
	for _, e := range escapes {
		calls[e.Call] = struct{}{}
	}
	var errTok *Token
	visitor := func(stmt Stmt) {
		if errTok != nil {
			return
		}
		switch s := stmt.(type) {
		case *IfStmt:
			for _, t := range s.Condition {
				if _, ok := calls[t]; ok {
					tt := t
					errTok = &tt
				}
			}
		case *WhileStmt:
			for _, t := range s.Condition {
				if _, ok := calls[t]; ok {
					tt := t
					errTok = &tt
				}
			}
		case *SimpleStmt:
			inIf := 0
			i := 0
			for i < len(s.Contents) {
				t := s.Contents[i]
				switch {
				case t.Kind == TokIdentifier && (t.Text == "DEOPT_IF" || t.Text == "ERROR_IF" || t.Text == "EXIT_IF"):
					inIf = 1
					if i+1 < len(s.Contents) {
						i++
					}
				case t.Kind == TokLParen:
					if inIf > 0 {
						inIf++
					}
				case t.Kind == TokRParen:
					if inIf > 0 {
						inIf--
					}
				default:
					if _, ok := calls[t]; ok && inIf > 0 {
						tt := t
						errTok = &tt
					}
				}
				i++
			}
		}
	}
	acceptBlock(codeDefBlock(instr), visitor)
	if errTok != nil {
		return analysisError(fmt.Sprintf("Escaping call '%s in condition", errTok.Text), *errTok)
	}
	return nil
}

// findEscapingAPICalls scans every SimpleStmt for calls of the form
// `IDENT(...)` that are not in the non-escaping carve-out.
//
// CPython: Tools/cases_generator/analyzer.py:715-765 find_escaping_api_calls
func findEscapingAPICalls(instr CodeDef) (map[*SimpleStmt]EscapingCall, error) {
	result := map[*SimpleStmt]EscapingCall{}
	var visitErr error
	visitor := func(stmt Stmt) {
		if visitErr != nil {
			return
		}
		s, ok := stmt.(*SimpleStmt)
		if !ok {
			return
		}
		tokens := s.Contents
		for idx := 0; idx < len(tokens); idx++ {
			if idx+1 >= len(tokens) {
				break
			}
			tkn := tokens[idx]
			nextTkn := tokens[idx+1]
			if nextTkn.Kind != TokLParen {
				continue
			}
			switch tkn.Kind {
			case TokIdentifier:
				if strings.ToUpper(tkn.Text) == tkn.Text {
					continue
				}
				if strings.HasPrefix(tkn.Text, "sym_") || strings.HasPrefix(tkn.Text, "optimize_") {
					continue
				}
				if strings.HasSuffix(tkn.Text, "Check") {
					continue
				}
				if strings.HasPrefix(tkn.Text, "Py_Is") {
					continue
				}
				if strings.HasSuffix(tkn.Text, "CheckExact") {
					continue
				}
				if _, found := nonEscapingFunctions[tkn.Text]; found {
					continue
				}
			case TokRParen:
				if idx == 0 {
					continue
				}
				prev := tokens[idx-1]
				if strings.HasSuffix(prev.Text, "_t") || prev.Text == "*" || prev.Text == "int" {
					continue
				}
			default:
				if tkn.Kind != TokRBracket {
					continue
				}
			}
			var kills *Token
			if tkn.Text == "PyStackRef_CLOSE" || tkn.Text == "PyStackRef_XCLOSE" {
				if len(tokens) <= idx+2 {
					visitErr = analysisError("Unexpected end of file", nextTkn)
					return
				}
				k := tokens[idx+2]
				if k.Kind != TokIdentifier {
					visitErr = analysisError(
						fmt.Sprintf("Expected identifier, got '%s'", k.Text), k,
					)
					return
				}
				kills = &k
			}
			result[s] = EscapingCall{Stmt: s, Call: tkn, Kills: kills}
		}
	}
	acceptBlock(codeDefBlock(instr), visitor)
	if visitErr != nil {
		return nil, visitErr
	}
	if err := checkEscapingCalls(instr, result); err != nil {
		return nil, err
	}
	return result, nil
}

var exitsSet = map[string]struct{}{
	"DISPATCH":         {},
	"Py_UNREACHABLE":   {},
	"DISPATCH_INLINED": {},
	"DISPATCH_GOTO":    {},
}

// alwaysExits reports whether control unconditionally leaves the body.
//
// CPython: Tools/cases_generator/analyzer.py:776-799 always_exits
func alwaysExits(op CodeDef) bool {
	depth := 0
	tokens := codeDefTokens(op)
	for i := 0; i < len(tokens); i++ {
		tkn := tokens[i]
		switch tkn.Kind {
		case TokLBrace:
			depth++
			continue
		case TokRBrace:
			depth--
			continue
		}
		if depth > 1 {
			continue
		}
		switch tkn.Kind {
		case TokGoto, TokReturn:
			return true
		case "KEYWORD":
			if _, ok := exitsSet[tkn.Text]; ok {
				return true
			}
		case TokIdentifier:
			if _, ok := exitsSet[tkn.Text]; ok {
				return true
			}
			if tkn.Text == "DEOPT_IF" || tkn.Text == "ERROR_IF" {
				if i+2 < len(tokens) {
					i++ // '('
					t := tokens[i+1]
					if t.Text == "true" || t.Text == "1" {
						return true
					}
				}
			}
		}
	}
	return false
}

// computeProperties is the master per-uop / per-label property classifier.
//
// CPython: Tools/cases_generator/analyzer.py:814-860 compute_properties
func computeProperties(op CodeDef) (*Properties, error) {
	escaping, err := findEscapingAPICalls(op)
	if err != nil {
		return nil, err
	}
	hasFree := variableUsed(op, "PyCell_New") ||
		variableUsed(op, "PyCell_GetRef") ||
		variableUsed(op, "PyCell_SetTakeRef") ||
		variableUsed(op, "PyCell_SwapTakeRef")
	deopts := variableUsed(op, "DEOPT_IF")
	exits := variableUsed(op, "EXIT_IF")
	if deopts && exits {
		toks := codeDefTokens(op)
		if len(toks) > 0 {
			return nil, analysisError("Op cannot contain both EXIT_IF and DEOPT_IF", toks[0])
		}
		return nil, fmt.Errorf("op cannot contain both EXIT_IF and DEOPT_IF")
	}
	pure := false
	noSaveIP := false
	if _, isLabel := op.(*LabelDef); !isLabel {
		for _, a := range codeDefAnnotations(op) {
			if a == "pure" {
				pure = true
			}
			if a == "no_save_ip" {
				noSaveIP = true
			}
		}
	}
	name := ""
	switch v := op.(type) {
	case *InstDef:
		name = v.Name
	case *LabelDef:
		name = v.Name
	}
	return &Properties{
		EscapingCalls:   escaping,
		Escapes:         len(escaping) > 0,
		ErrorWithPop:    hasErrorWithPop(op),
		ErrorWithoutPop: hasErrorWithoutPop(op),
		Deopts:          deopts,
		SideExit:        exits,
		Oparg:           opargUsed(op),
		Jumps:           variableUsed(op, "JUMPBY"),
		EvalBreaker:     strings.Contains(name, "CHECK_PERIODIC"),
		NeedsThis:       variableUsed(op, "this_instr"),
		AlwaysExits:     alwaysExits(op),
		StoresSp:        variableUsed(op, "SYNC_SP"),
		UsesCoConsts:    variableUsed(op, "FRAME_CO_CONSTS"),
		UsesCoNames:     variableUsed(op, "FRAME_CO_NAMES"),
		UsesLocals:      variableUsed(op, "GETLOCAL") && !hasFree,
		UsesOpcode:      variableUsed(op, "opcode"),
		HasFree:         hasFree,
		Pure:            pure,
		NoSaveIP:        noSaveIP,
		Tier:            tierVariable(op),
		NeedsPrev:       variableUsed(op, "prev_instr"),
		ConstOparg:      -1,
	}, nil
}

// makeUop builds one Uop, plus replicas if a `replicate(N)` annotation appears.
//
// CPython: Tools/cases_generator/analyzer.py:863-903 make_uop
func makeUop(name string, op *InstDef, inputs []InputEffect, uops map[string]*Uop) (*Uop, error) {
	stack, err := analyzeStack(op)
	if err != nil {
		return nil, err
	}
	caches, err := analyzeCaches(inputs)
	if err != nil {
		return nil, err
	}
	stores, err := findVariableStores(op)
	if err != nil {
		return nil, err
	}
	props, err := computeProperties(op)
	if err != nil {
		return nil, err
	}
	result := &Uop{
		Name:        name,
		UopContext:  op.GetContext(),
		Annotations: op.Annotations,
		Stack:       stack,
		Caches:      caches,
		LocalStores: stores,
		Body:        op.Block,
		Properties:  props,
		cachedSize:  -1,
	}
	replicated := 0
	for _, anno := range op.Annotations {
		if strings.HasPrefix(anno, "replicate") {
			// Format: replicate(N)
			inner := anno[len("replicate"):]
			if len(inner) >= 2 && inner[0] == '(' && inner[len(inner)-1] == ')' {
				num := inner[1 : len(inner)-1]
				n := 0
				if _, err := fmt.Sscanf(num, "%d", &n); err == nil {
					replicated = n
				}
			}
			break
		}
	}
	result.Replicated = replicated
	if replicated == 0 {
		return result, nil
	}
	for oparg := 0; oparg < replicated; oparg++ {
		nameX := fmt.Sprintf("%s_%d", name, oparg)
		st, err := analyzeStack(op)
		if err != nil {
			return nil, err
		}
		ca, err := analyzeCaches(inputs)
		if err != nil {
			return nil, err
		}
		st2, err := findVariableStores(op)
		if err != nil {
			return nil, err
		}
		pr, err := computeProperties(op)
		if err != nil {
			return nil, err
		}
		pr.Oparg = false
		pr.ConstOparg = oparg
		rep := &Uop{
			Name:        nameX,
			UopContext:  op.GetContext(),
			Annotations: op.Annotations,
			Stack:       st,
			Caches:      ca,
			LocalStores: st2,
			Body:        op.Block,
			Properties:  pr,
			cachedSize:  -1,
			Replicates:  result,
		}
		uops[nameX] = rep
	}
	return result, nil
}

// addOp registers an op() definition into the uops map, honoring
// the `override` annotation.
//
// CPython: Tools/cases_generator/analyzer.py:906-913 add_op
func addOp(op *InstDef, uops map[string]*Uop) error {
	if op.Kind != "op" {
		return fmt.Errorf("addOp called with kind=%q", op.Kind)
	}
	if prev, ok := uops[op.Name]; ok {
		hasOverride := false
		for _, a := range op.Annotations {
			if a == "override" {
				hasOverride = true
				break
			}
		}
		if !hasOverride {
			toks := op.Tokens()
			var first Token
			if len(toks) > 0 {
				first = toks[0]
			}
			return overrideError(op.Name, op.GetContext(), prev.UopContext, first)
		}
	}
	u, err := makeUop(op.Name, op, op.Inputs, uops)
	if err != nil {
		return err
	}
	uops[op.Name] = u
	return nil
}

// addInstruction registers an Instruction (synthetic or macro) into instructions.
//
// CPython: Tools/cases_generator/analyzer.py:916-922 add_instruction
func addInstruction(where Token, name string, parts []Part, instructions map[string]*Instruction) {
	instructions[name] = &Instruction{Where: where, Name: name, Parts: parts, Opcode: -1}
}

// desugarInst lowers an inst() into one synthetic uop plus surrounding Skip parts.
//
// CPython: Tools/cases_generator/analyzer.py:925-950 desugar_inst
func desugarInst(inst *InstDef, instructions map[string]*Instruction, uops map[string]*Uop) error {
	if inst.Kind != "inst" {
		return fmt.Errorf("desugarInst called with kind=%q", inst.Kind)
	}
	var opInputs []InputEffect
	var parts []Part
	uopIndex := -1
	for _, in := range inst.Inputs {
		if ce, ok := in.(*CacheEffect); ok && ce.Name == "unused" {
			parts = append(parts, &Skip{SizeVal: ce.Size})
		} else {
			opInputs = append(opInputs, in)
			if uopIndex < 0 {
				uopIndex = len(parts)
				parts = append(parts, &Skip{SizeVal: 0})
			}
		}
	}
	uop, err := makeUop("_"+inst.Name, inst, opInputs, uops)
	if err != nil {
		return err
	}
	uop.ImplicitlyCreated = true
	uops[inst.Name] = uop
	if uopIndex < 0 {
		parts = append(parts, uop)
	} else {
		parts[uopIndex] = uop
	}
	first := Token{}
	toks := inst.Tokens()
	if len(toks) > 0 {
		first = toks[0]
	}
	addInstruction(first, inst.Name, parts, instructions)
	return nil
}

// addMacro resolves a macro() definition into Instruction parts.
//
// CPython: Tools/cases_generator/analyzer.py:953-973 add_macro
func addMacro(macro *Macro, instructions map[string]*Instruction, uops map[string]*Uop) error {
	var parts []Part
	for _, part := range macro.UOps {
		switch p := part.(type) {
		case *OpName:
			if p.Name == "flush" {
				parts = append(parts, &Flush{})
			} else {
				u, ok := uops[p.Name]
				if !ok {
					toks := macro.Tokens()
					first := Token{}
					if len(toks) > 0 {
						first = toks[0]
					}
					return analysisError(fmt.Sprintf("No Uop named %s", p.Name), first)
				}
				parts = append(parts, u)
			}
		case *CacheEffect:
			parts = append(parts, &Skip{SizeVal: p.Size})
		default:
			return fmt.Errorf("addMacro: unexpected uop %T", part)
		}
	}
	if len(parts) == 0 {
		return fmt.Errorf("addMacro: empty parts for %s", macro.Name)
	}
	first := Token{}
	toks := macro.Tokens()
	if len(toks) > 0 {
		first = toks[0]
	}
	addInstruction(first, macro.Name, parts, instructions)
	return nil
}

// addFamily resolves family() members and back-links each to its Family.
//
// CPython: Tools/cases_generator/analyzer.py:976-990 add_family
func addFamily(pf *Family, instructions map[string]*Instruction, families map[string]*AnalyzedFamily) error {
	members := make([]*Instruction, 0, len(pf.Members))
	for _, m := range pf.Members {
		inst, ok := instructions[m]
		if !ok {
			return fmt.Errorf("family %s: unknown member %s", pf.Name, m)
		}
		members = append(members, inst)
	}
	fam := &AnalyzedFamily{Name: pf.Name, SizeStr: pf.Size, Members: members}
	for _, m := range fam.Members {
		m.Family = fam
	}
	if head, ok := instructions[fam.Name]; ok {
		head.IsTarget = true
	}
	families[fam.Name] = fam
	return nil
}

// addPseudo registers a pseudo() definition as a PseudoInstruction.
//
// CPython: Tools/cases_generator/analyzer.py:993-1004 add_pseudo
func addPseudo(p *Pseudo, instructions map[string]*Instruction, pseudos map[string]*PseudoInstruction) error {
	st, err := analyzeStack(p)
	if err != nil {
		return err
	}
	targets := make([]*Instruction, 0, len(p.Targets))
	for _, t := range p.Targets {
		inst, ok := instructions[t]
		if !ok {
			return fmt.Errorf("pseudo %s: unknown target %s", p.Name, t)
		}
		targets = append(targets, inst)
	}
	pseudos[p.Name] = &PseudoInstruction{
		Name:       p.Name,
		Stack:      st,
		Targets:    targets,
		AsSequence: p.AsSequence,
		Flags:      p.Flags,
		Opcode:     -1,
	}
	return nil
}

// addLabel registers a label() definition with computed properties.
//
// CPython: Tools/cases_generator/analyzer.py:1007-1012 add_label
func addLabel(label *LabelDef, labels map[string]*Label) error {
	props, err := computeProperties(label)
	if err != nil {
		return err
	}
	labels[label.Name] = &Label{
		Name:       label.Name,
		Spilled:    label.Spilled,
		Body:       label.Block,
		Properties: props,
	}
	return nil
}

// assignOpcodes reproduces the upstream opcode-assignment algorithm.
//
// CPython: Tools/cases_generator/analyzer.py:1015-1096 assign_opcodes
func assignOpcodes(
	instructions map[string]*Instruction,
	families map[string]*AnalyzedFamily,
	pseudos map[string]*PseudoInstruction,
) (map[string]int, int, int) {
	instmap := map[string]int{}
	instmap["CACHE"] = 0
	instmap["RESERVED"] = 17
	instmap["RESUME"] = 128
	instmap["BINARY_OP_INPLACE_ADD_UNICODE"] = 3
	instmap["INSTRUMENTED_LINE"] = 254
	instmap["ENTER_EXECUTOR"] = 255

	var instrumented []string
	for name := range instructions {
		if strings.HasPrefix(name, "INSTRUMENTED") {
			instrumented = append(instrumented, name)
		}
	}
	specialized := map[string]struct{}{}
	for _, fam := range families {
		for _, inst := range fam.Members {
			specialized[inst.Name] = struct{}{}
		}
	}
	instrSet := map[string]struct{}{}
	for _, n := range instrumented {
		instrSet[n] = struct{}{}
	}

	var noArg, hasArg []string
	for _, inst := range instructions {
		if _, ok := specialized[inst.Name]; ok {
			continue
		}
		if _, ok := instrSet[inst.Name]; ok {
			continue
		}
		if inst.Props().Oparg {
			hasArg = append(hasArg, inst.Name)
		} else {
			noArg = append(noArg, inst.Name)
		}
	}
	minInternal := instmap["RESUME"] + 1
	minInstrumented := 254 - (len(instrumented) - 1)
	_ = minInternal

	nextOpcode := 1
	used := map[int]struct{}{}
	for _, v := range instmap {
		used[v] = struct{}{}
	}
	addOne := func(name string) {
		if _, ok := instmap[name]; ok {
			return
		}
		for {
			if _, ok := used[nextOpcode]; !ok {
				break
			}
			nextOpcode++
		}
		instmap[name] = nextOpcode
		used[nextOpcode] = struct{}{}
		nextOpcode++
	}
	sort.Strings(noArg)
	for _, n := range noArg {
		addOne(n)
	}
	sort.Strings(hasArg)
	for _, n := range hasArg {
		addOne(n)
	}
	nextOpcode = minInternal
	specSorted := make([]string, 0, len(specialized))
	for n := range specialized {
		specSorted = append(specSorted, n)
	}
	sort.Strings(specSorted)
	for _, n := range specSorted {
		addOne(n)
	}
	nextOpcode = minInstrumented
	for _, n := range instrumented {
		addOne(n)
	}
	for name, inst := range instructions {
		if op, ok := instmap[name]; ok {
			inst.Opcode = op
		}
	}
	psNames := make([]string, 0, len(pseudos))
	for n := range pseudos {
		psNames = append(psNames, n)
	}
	sort.Strings(psNames)
	for i, n := range psNames {
		op := 256 + i
		instmap[n] = op
		pseudos[n].Opcode = op
	}
	return instmap, len(noArg), minInstrumented
}

// getInstructionSizeForUop returns the constant size of the instruction
// containing uop when it uses the INSTRUCTION_SIZE macro, else nil.
//
// CPython: Tools/cases_generator/analyzer.py:1099-1125 get_instruction_size_for_uop
func getInstructionSizeForUop(instructions map[string]*Instruction, uop *Uop) (*int, error) {
	var hit Token
	found := false
	for _, t := range blockTokens(uop.Body) {
		if t.Text == "INSTRUCTION_SIZE" {
			hit = t
			found = true
			break
		}
	}
	if !found {
		return nil, nil
	}
	var size *int
	for _, inst := range instructions {
		contains := false
		for _, p := range inst.Parts {
			if pu, ok := p.(*Uop); ok && pu == uop {
				contains = true
				break
			}
		}
		if !contains {
			continue
		}
		s := inst.Size()
		if size == nil {
			size = &s
		} else if *size != s {
			return nil, analysisError(
				fmt.Sprintf("All instructions containing a uop with the `INSTRUCTION_SIZE` macro must have the same size: %d != %d", *size, s),
				hit,
			)
		}
	}
	if size == nil {
		return nil, analysisError(
			fmt.Sprintf("No instruction containing the uop '%s' was found", uop.Name),
			hit,
		)
	}
	return size, nil
}

// AnalyzeForest walks the parsed forest and returns an Analysis.
//
// CPython: Tools/cases_generator/analyzer.py:1128-1178 analyze_forest
func AnalyzeForest(forest []Node) (*Analysis, error) {
	instructions := map[string]*Instruction{}
	uops := map[string]*Uop{}
	families := map[string]*AnalyzedFamily{}
	pseudos := map[string]*PseudoInstruction{}
	labels := map[string]*Label{}

	for _, n := range forest {
		switch v := n.(type) {
		case *InstDef:
			switch v.Kind {
			case "inst":
				if err := desugarInst(v, instructions, uops); err != nil {
					return nil, err
				}
			case "op":
				if err := addOp(v, uops); err != nil {
					return nil, err
				}
			default:
				return nil, fmt.Errorf("unknown InstDef kind %q", v.Kind)
			}
		case *Macro, *Family, *Pseudo, *LabelDef:
			// handled in subsequent passes
		default:
			return nil, fmt.Errorf("AnalyzeForest: unexpected node %T", n)
		}
	}
	for _, n := range forest {
		if m, ok := n.(*Macro); ok {
			if err := addMacro(m, instructions, uops); err != nil {
				return nil, err
			}
		}
	}
	for _, n := range forest {
		switch v := n.(type) {
		case *Family:
			if err := addFamily(v, instructions, families); err != nil {
				return nil, err
			}
		case *Pseudo:
			if err := addPseudo(v, instructions, pseudos); err != nil {
				return nil, err
			}
		case *LabelDef:
			if err := addLabel(v, labels); err != nil {
				return nil, err
			}
		}
	}
	for _, u := range uops {
		s, err := getInstructionSizeForUop(instructions, u)
		if err != nil {
			return nil, err
		}
		u.InstructionSize = s
	}
	if inst, ok := instructions["BINARY_OP_INPLACE_ADD_UNICODE"]; ok {
		if fam, ok := families["BINARY_OP"]; ok {
			inst.Family = fam
			fam.Members = append(fam.Members, inst)
		}
	}
	opmap, firstArg, minInstrumented := assignOpcodes(instructions, families, pseudos)
	return &Analysis{
		Instructions:    instructions,
		Uops:            uops,
		Families:        families,
		Pseudos:         pseudos,
		Labels:          labels,
		Opmap:           opmap,
		HaveArg:         firstArg,
		MinInstrumented: minInstrumented,
	}, nil
}
