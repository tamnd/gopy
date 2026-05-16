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
		"reverse": func(in []inputView) []inputView {
			out := make([]inputView, len(in))
			for i, v := range in {
				out[len(in)-1-i] = v
			}
			return out
		},
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

// armView is the template input for one switch arm.
type armView struct {
	Name        string       // opcode constant suffix
	Inputs      []inputView  // popped in source order (template reverses)
	Caches      []cacheView  // cache slots
	Bail        bool         // body translator bailed; emit panic-stub
	Note        string       // bail explanation
	BailOutputs []outputView // outputs (rendered as comment when bailing)
	Outputs     []outputView // output locals (declared, then pushed)
	Body        string       // translated body lines (no surrounding braces)
	Terminates  bool         // body emits its own return
}

type inputView struct {
	Name     string
	Sized    bool
	SizeExpr string
}

type cacheView struct {
	Name   string
	Size   int
	Offset int
}

type outputView struct {
	Name     string
	Sized    bool
	SizeExpr string
}

// armTemplate prints one switch arm. The reverse-order input pop
// matches CPython's stack-bottom-first signature. The body lives in
// templates/tier1_arm.tmpl so the format is easy to read without
// fighting Go's string-escape rules.
var armTemplate = mustParseTemplate("tier1_arm.tmpl")

// EmitTier1Arm renders the `case opcode.NAME:` block for one analyzed
// instruction.
func EmitTier1Arm(a *SignatureAnalysis) string {
	v := armView{Name: a.Name}
	for _, in := range a.Inputs {
		v.Inputs = append(v.Inputs, inputView{
			Name:     bindName(in.Name, "in", in.Index),
			Sized:    in.Sized,
			SizeExpr: in.SizeExpr,
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
			v.BailOutputs = append(v.BailOutputs, outputView{
				Name:     bindName(o.Name, "out", o.Index),
				Sized:    o.Sized,
				SizeExpr: o.SizeExpr,
			})
		}
	} else {
		v.Body = body
		v.Terminates = terminates
		for _, o := range a.Outputs {
			v.Outputs = append(v.Outputs, outputView{
				Name:     bindName(o.Name, "out", o.Index),
				Sized:    o.Sized,
				SizeExpr: o.SizeExpr,
			})
		}
	}
	var b strings.Builder
	if err := armTemplate.Execute(&b, v); err != nil {
		panic(fmt.Sprintf("arm template: %v", err))
	}
	return b.String()
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
