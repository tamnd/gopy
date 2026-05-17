// Tier-1 emitter (B3). Renders one switch arm per real `inst` from
// the analyzed signatures. Body translation is the action translator's
// job (B6); until that lands every arm emits a panic-stub call so the
// generated file always compiles.
//
// CPython: Tools/cases_generator/tier1_generator.py.

package main

import (
	"embed"
	"fmt"
	"go/format"
	"sort"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var tier1Templates embed.FS

func mustParseTemplate(name string) *template.Template {
	t := template.New(name).Funcs(template.FuncMap{
		"bodyLines": func(s string) []string {
			s = strings.TrimRight(s, "\n")
			if s == "" {
				return nil
			}
			return strings.Split(s, "\n")
		},
	})
	data, err := tier1Templates.ReadFile("templates/" + name)
	if err != nil {
		panic(fmt.Sprintf("read template %q: %v", name, err))
	}
	return template.Must(t.Parse(string(data)))
}

// armView is the template input for one switch arm. It mirrors
// CPython's tier1_generator stack model: each named scalar input is
// bound with a peek at its initial depth (no pop); after the body
// runs, passthrough scalars are committed back via setPeek, the
// non-passthrough input region is dropped, and new outputs are
// pushed onto the resulting top.
//
// CPython: Tools/cases_generator/tier1_generator.py Emitter.emit_save_stack
// and Tools/cases_generator/stack.py Stack.flush.
type armView struct {
	Name        string
	Peeks       []peekStmt   // peek-and-bind for named scalar inputs
	SizedPeeks  []sizedPeek  // slice-bind for named sized inputs
	Caches      []cacheView  // cache slots
	Bail        bool         // body translator bailed; emit panic-stub
	Note        string       // bail explanation
	BailOutputs []string     // pretty-printed output descriptors for the bail comment
	OutputDecls []outputDecl // declarations for new (non-passthrough) outputs
	Body        string       // translated body lines (no surrounding braces)
	Terminates  bool         // body emits its own return
	Writebacks  []peekStmt   // setPeek for passthrough scalar inputs
	DropExpr    string       // Go expression for the post-body stack shrink (empty = no drop)
	Pushes      []pushStmt   // new outputs pushed onto the resulting top
}

// peekStmt renders as either `name := e.peek(<depth>); _ = name` (read)
// or `e.setPeek(<depth>, name)` (write), depending on where it appears.
type peekStmt struct {
	Name      string
	DepthExpr string
}

// sizedPeek binds a sized input (e.g. `args[oparg]`) to a Go slice.
// The slice is ordered bottom-first to match CPython's
// `_PyStackRef *args` semantics: index 0 is the deepest stack slot.
type sizedPeek struct {
	Name      string
	TopOffset string // depth-from-TOS of the topmost slot of this region
	SizeExpr  string // C size expression, used as the slice length
}

// outputDecl declares a local for a non-passthrough output before the
// body runs.
type outputDecl struct {
	Name     string
	Sized    bool
	SizeExpr string
}

// pushStmt renders the post-body push for a non-passthrough output.
type pushStmt struct {
	Name     string
	Sized    bool
	SizeExpr string
}

type cacheView struct {
	Name   string
	Size   int
	Offset int
}

// armTemplate prints one switch arm. The reverse-order input pop
// matches CPython's stack-bottom-first signature. The body lives in
// templates/tier1_arm.tmpl so the format is easy to read without
// fighting Go's string-escape rules.
var armTemplate = mustParseTemplate("tier1_arm.tmpl")

// EmitTier1Arm renders the `case opcode.NAME:` block for one analyzed
// instruction. The shape mirrors CPython's generated_cases.c.h: each
// named scalar input is bound by peeking the initial stack at the
// computed depth, the translated body runs against those locals,
// passthrough scalars are committed back, the consumed input region
// is dropped, and new outputs are pushed.
func EmitTier1Arm(a *SignatureAnalysis) string {
	v := armView{Name: a.Name}

	// Bind each named scalar input. Sized inputs and "unused" slots
	// stay in place on the value stack without a Go-side binding, the
	// same way CPython leaves the underlying stack_pointer slice
	// untouched for slots the body never reads.
	for i, in := range a.Inputs {
		if in.Name == "" || in.Name == "unused" {
			continue
		}
		if in.Sized {
			v.SizedPeeks = append(v.SizedPeeks, sizedPeek{
				Name:      bindName(in.Name, "in", in.Index),
				TopOffset: stackDepthExpr(a.Inputs, i),
				SizeExpr:  in.SizeExpr,
			})
			continue
		}
		v.Peeks = append(v.Peeks, peekStmt{
			Name:      bindName(in.Name, "in", in.Index),
			DepthExpr: stackDepthExpr(a.Inputs, i),
		})
	}
	for _, c := range a.Caches {
		v.Caches = append(v.Caches, cacheView{Name: c.Name, Size: c.Size, Offset: c.Offset})
	}

	body, terminates, ok, note := TranslateBody(a.Body, a)
	if !ok {
		v.Bail = true
		v.Note = note
		for _, o := range a.Outputs {
			s := bindName(o.Name, "out", o.Index)
			if o.Sized {
				s += "[" + o.SizeExpr + "]"
			}
			if o.Passthrough {
				s += "*"
			}
			v.BailOutputs = append(v.BailOutputs, s)
		}
		return renderArm(v)
	}
	v.Body = body
	v.Terminates = terminates

	// Non-passthrough outputs are declared as locals before the body
	// so the translator can assign into them. Passthrough outputs
	// already have a binding via the matching input peek.
	for _, o := range a.Outputs {
		if o.Passthrough {
			continue
		}
		v.OutputDecls = append(v.OutputDecls, outputDecl{
			Name:     bindName(o.Name, "out", o.Index),
			Sized:    o.Sized,
			SizeExpr: o.SizeExpr,
		})
	}

	// Passthrough scalars are committed back to their slot in case the
	// body reassigned them (e.g. SWAP). Write-back happens *before*
	// the SP drop so depths still match the read-side peeks.
	for i, in := range a.Inputs {
		if !in.Passthrough || in.Sized {
			continue
		}
		if in.Name == "" || in.Name == "unused" {
			continue
		}
		v.Writebacks = append(v.Writebacks, peekStmt{
			Name:      bindName(in.Name, "in", in.Index),
			DepthExpr: stackDepthExpr(a.Inputs, i),
		})
	}

	v.DropExpr = dropExpr(a.Inputs)

	for _, o := range a.Outputs {
		if o.Passthrough {
			continue
		}
		v.Pushes = append(v.Pushes, pushStmt{
			Name:     bindName(o.Name, "out", o.Index),
			Sized:    o.Sized,
			SizeExpr: o.SizeExpr,
		})
	}
	return renderArm(v)
}

func renderArm(v armView) string {
	var b strings.Builder
	if err := armTemplate.Execute(&b, v); err != nil {
		panic(fmt.Sprintf("arm template: %v", err))
	}
	return b.String()
}

// stackDepthExpr returns a Go integer expression for the initial
// depth-from-TOS of the *topmost* slot of input[i]. Mirrors CPython's
// Tools/cases_generator/stack.py Stack.depth computation: sum the
// sizes of inputs that sit above index i on the value stack.
func stackDepthExpr(inputs []StackBinding, i int) string {
	var parts []string
	for j := i + 1; j < len(inputs); j++ {
		parts = append(parts, sizeTerm(inputs[j]))
	}
	if len(parts) == 0 {
		return "0"
	}
	return strings.Join(parts, " + ")
}

// dropExpr returns a Go integer expression for the post-body
// STACK_SHRINK count: the sum of sizes of every non-passthrough input.
// Empty string means no shrink is needed.
func dropExpr(inputs []StackBinding) string {
	var parts []string
	for _, in := range inputs {
		if in.Passthrough {
			continue
		}
		parts = append(parts, sizeTerm(in))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " + ")
}

func sizeTerm(b StackBinding) string {
	if b.Sized {
		return "int(" + b.SizeExpr + ")"
	}
	return "1"
}

// bindName picks a stable Go identifier for a slot. Empty/unused names
// get a synthetic `_in0` / `_out1` so collisions across slots can't
// happen; Go keywords clash with the language and get a `_v` suffix;
// otherwise we use the source name directly.
func bindName(name, dir string, idx int) string {
	if name == "" || name == "unused" {
		return fmt.Sprintf("_%s%d", dir, idx)
	}
	if goKeywords[name] {
		return name + "_v"
	}
	return name
}

// goKeywords are the reserved identifiers we have to rename around.
// CPython names like `type`, `func`, `default`, `range` collide with
// Go's grammar and would not parse as variable names.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true,
	"continue": true, "default": true, "defer": true, "else": true,
	"fallthrough": true, "for": true, "func": true, "go": true,
	"goto": true, "if": true, "import": true, "interface": true,
	"map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true,
	"var": true,
}

// fileTemplate wraps the per-arm output into a complete Go source
// file. The marker comment up top is what the drift check reads. The
// body lives in templates/tier1_file.tmpl.
var fileTemplate = mustParseTemplate("tier1_file.tmpl")

// EmitTier1File renders a Go source file containing dispatchGen, which
// the hand-written dispatch falls back to once codegen takes over.
// `pkg` is the target package, `hash` is the bytecodes.c sha256 stamped
// in the drift marker. Adaptive variants in fm are skipped, since v0.6
// has no specializer; their base instruction handles dispatch and the
// adaptive opcode constants don't exist in compile/ yet.
func EmitTier1File(pkg, hash string, analyses []*SignatureAnalysis, fm FamilyMap) string {
	sorted := make([]*SignatureAnalysis, 0, len(analyses))
	for _, a := range analyses {
		if _, isVariant := fm[a.Name]; isVariant {
			continue
		}
		sorted = append(sorted, a)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	arms := make([]string, 0, len(sorted))
	for _, a := range sorted {
		arms = append(arms, strings.TrimRight(EmitTier1Arm(a), "\n"))
	}

	var b strings.Builder
	if err := fileTemplate.Execute(&b, struct {
		Pkg    string
		Marker string
		Arms   []string
	}{Pkg: pkg, Marker: strings.TrimRight(MarkerLine(hash), "\n"), Arms: arms}); err != nil {
		panic(fmt.Sprintf("file template: %v", err))
	}
	// Run gofmt so the on-disk file matches what `gofmt -l` expects;
	// CPython's cases_generator runs clang-format on its tier-1 output
	// for the same reason.
	raw := b.String()
	formatted, err := format.Source([]byte(raw))
	if err != nil {
		// Templates produced something Go can't parse; surface the raw
		// text so the user can see exactly what failed.
		panic(fmt.Sprintf("gofmt on generated source: %v\n--- source ---\n%s", err, raw))
	}
	return string(formatted)
}
