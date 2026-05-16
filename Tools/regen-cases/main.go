// regen-cases drives the vendored CPython Tools/cases_generator/
// pipeline. It is the spec 1714 driver: every generator under
// Tools/cases_generator/ is invoked from here. The default mode
// emits CPython's own C/Python outputs into a scratch directory
// for inspection; --check-upstream additionally proves the
// vendored generator and inputs are unchanged from CPython 3.14.5
// by diffing tree files, inputs, and output bodies (with the
// ROOT-dependent header stripped).
//
// Inputs mirror CPython source layout exactly so the port is
// obvious at a glance:
//
//	Tools/cases_generator/inputs/Python/bytecodes.c
//	Tools/cases_generator/inputs/Python/optimizer_bytecodes.c
//	Tools/cases_generator/inputs/Include/internal/pycore_code.h
//
// Later phases of 1714 add gopy-targeted emitters; this binary
// gains a --target=go flag at that point.
//
// Usage:
//
//	go run ./Tools/regen-cases                         # emit upstream C output into /tmp
//	go run ./Tools/regen-cases --check-upstream        # plus byte-diff vs $CPYTHON_ROOT
//	go run ./Tools/regen-cases --out /tmp/x            # pin scratch dir
//	go run ./Tools/regen-cases --python python3.14     # override python binary
//
// Spec: website/docs/specs/1700/1714_bytecodes_dsl_codegen.md
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// generator describes one upstream Tools/cases_generator entry
// point. Inputs are CPython-relative paths (e.g. Python/bytecodes.c);
// they are resolved against Tools/cases_generator/inputs/, which
// mirrors the upstream tree, and against CPYTHON_ROOT for the
// upstream-parity check.
type generator struct {
	name    string   // python file under Tools/cases_generator/
	out     string   // basename of the output file
	inputs  []string // CPython-relative paths (e.g. "Python/bytecodes.c")
	upPath  string   // path inside CPYTHON_ROOT that this output mirrors
	comment string   // "//" for C, "#" for Python; for header stripping
}

var upstreamGenerators = []generator{
	{name: "tier1_generator.py", out: "generated_cases.c.h",
		inputs: []string{"Python/bytecodes.c"},
		upPath: "Python/generated_cases.c.h", comment: "//"},
	{name: "tier2_generator.py", out: "executor_cases.c.h",
		inputs: []string{"Python/bytecodes.c"},
		upPath: "Python/executor_cases.c.h", comment: "//"},
	{name: "optimizer_generator.py", out: "optimizer_cases.c.h",
		inputs: []string{"Python/optimizer_bytecodes.c", "Python/bytecodes.c"},
		upPath: "Python/optimizer_cases.c.h", comment: "//"},
	{name: "opcode_id_generator.py", out: "opcode_ids.h",
		inputs: []string{"Python/bytecodes.c"},
		upPath: "Include/opcode_ids.h", comment: "//"},
	{name: "opcode_metadata_generator.py", out: "pycore_opcode_metadata.h",
		inputs: []string{"Python/bytecodes.c"},
		upPath: "Include/internal/pycore_opcode_metadata.h", comment: "//"},
	{name: "uop_id_generator.py", out: "pycore_uop_ids.h",
		inputs: []string{"Python/bytecodes.c"},
		upPath: "Include/internal/pycore_uop_ids.h", comment: "//"},
	{name: "uop_metadata_generator.py", out: "pycore_uop_metadata.h",
		inputs: []string{"Python/bytecodes.c"},
		upPath: "Include/internal/pycore_uop_metadata.h", comment: "//"},
	{name: "target_generator.py", out: "opcode_targets.h",
		inputs: []string{"Python/bytecodes.c"},
		upPath: "Python/opcode_targets.h", comment: "//"},
	{name: "py_metadata_generator.py", out: "_opcode_metadata.py",
		inputs: []string{"Python/bytecodes.c"},
		upPath: "Lib/_opcode_metadata.py", comment: "#"},
}

// vendoredGenerator lists the files we expect to find inside
// Tools/cases_generator/ as a verbatim mirror of upstream
// Tools/cases_generator/. --check-upstream diffs each one.
var vendoredGenerator = []string{
	"_typing_backports.py", "analyzer.py", "cwriter.py",
	"generators_common.py", "lexer.py", "opcode_id_generator.py",
	"opcode_metadata_generator.py", "optimizer_generator.py",
	"parser.py", "parsing.py", "plexer.py",
	"py_metadata_generator.py", "stack.py", "target_generator.py",
	"tier1_generator.py", "tier2_generator.py",
	"uop_id_generator.py", "uop_metadata_generator.py",
	"interpreter_definition.md", "mypy.ini",
}

// vendoredInputs lists CPython-relative paths that must equal
// their upstream counterparts byte for byte.
var vendoredInputs = []string{
	"Python/bytecodes.c",
	"Python/optimizer_bytecodes.c",
	"Include/internal/pycore_code.h",
}

type options struct {
	repoRoot      string
	cpythonRoot   string
	outDir        string
	python        string
	checkUpstream bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "regen-cases:", err)
		os.Exit(1)
	}
}

func run() error {
	opts, err := parseFlags(os.Args[1:])
	if err != nil {
		return err
	}
	cleanup, err := ensureOutDir(&opts)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := emitUpstream(opts); err != nil {
		return fmt.Errorf("emit upstream outputs: %w", err)
	}
	if opts.checkUpstream {
		if err := checkUpstream(opts); err != nil {
			return fmt.Errorf("upstream-parity check: %w", err)
		}
		fmt.Println("==> upstream parity OK (generator verbatim, inputs identical, output bodies identical)")
	}
	return nil
}

func parseFlags(argv []string) (options, error) {
	fs := flag.NewFlagSet("regen-cases", flag.ContinueOnError)
	opts := options{
		python:      defaultPython(),
		cpythonRoot: os.Getenv("CPYTHON_ROOT"),
	}
	if opts.cpythonRoot == "" {
		home, _ := os.UserHomeDir()
		opts.cpythonRoot = filepath.Join(home, "cpython-314")
	}
	fs.StringVar(&opts.outDir, "out", "", "output scratch dir (default: fresh mktemp)")
	fs.StringVar(&opts.python, "python", opts.python, "python interpreter")
	fs.StringVar(&opts.cpythonRoot, "cpython-root", opts.cpythonRoot, "pinned CPython 3.14.5 source tree for --check-upstream")
	fs.BoolVar(&opts.checkUpstream, "check-upstream", false, "verify vendored generator+inputs+outputs vs CPython 3.14.5")
	if err := fs.Parse(argv); err != nil {
		return opts, err
	}
	root, err := repoRoot()
	if err != nil {
		return opts, err
	}
	opts.repoRoot = root
	return opts, nil
}

func defaultPython() string {
	if env := os.Getenv("PYTHON"); env != "" {
		return env
	}
	return "python3"
}

func repoRoot() (string, error) {
	// Find the gopy repo root by walking up from this source
	// file's directory looking for go.mod.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for range 6 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// Fallback: cwd.
	return os.Getwd()
}

func ensureOutDir(opts *options) (func(), error) {
	if opts.outDir != "" {
		if err := os.MkdirAll(opts.outDir, 0o755); err != nil {
			return func() {}, err
		}
		return func() {}, nil
	}
	dir, err := os.MkdirTemp("", "gopy-regen-cases-")
	if err != nil {
		return func() {}, err
	}
	opts.outDir = dir
	return func() { /* leave it for inspection */ }, nil
}

func emitUpstream(opts options) error {
	genDir := filepath.Join(opts.repoRoot, "Tools", "cases_generator")
	inputsDir := filepath.Join(genDir, "inputs")
	for _, g := range upstreamGenerators {
		args := []string{filepath.Join(genDir, g.name), "-o", filepath.Join(opts.outDir, g.out)}
		for _, in := range g.inputs {
			args = append(args, filepath.Join(inputsDir, filepath.FromSlash(in)))
		}
		fmt.Printf("==> %s -> %s\n", g.name, g.out)
		cmd := exec.Command(opts.python, args...)
		cmd.Dir = genDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s: %w", g.name, err)
		}
	}
	return nil
}

func checkUpstream(opts options) error {
	if _, err := os.Stat(opts.cpythonRoot); err != nil {
		return fmt.Errorf("CPYTHON_ROOT=%s not found: %w", opts.cpythonRoot, err)
	}
	genDir := filepath.Join(opts.repoRoot, "Tools", "cases_generator")
	inputsDir := filepath.Join(genDir, "inputs")
	upstreamGen := filepath.Join(opts.cpythonRoot, "Tools", "cases_generator")

	var drift []string

	fmt.Printf("==> vendored generator tree vs %s\n", upstreamGen)
	for _, f := range vendoredGenerator {
		got := filepath.Join(genDir, f)
		want := filepath.Join(upstreamGen, f)
		if !filesEqual(got, want) {
			drift = append(drift, fmt.Sprintf("GEN-FILE: Tools/cases_generator/%s != upstream", f))
		}
	}

	fmt.Printf("==> vendored inputs vs %s\n", opts.cpythonRoot)
	for _, upRel := range vendoredInputs {
		got := filepath.Join(inputsDir, filepath.FromSlash(upRel))
		want := filepath.Join(opts.cpythonRoot, filepath.FromSlash(upRel))
		if !filesEqual(got, want) {
			drift = append(drift, fmt.Sprintf("INPUT: Tools/cases_generator/inputs/%s != %s", upRel, upRel))
		}
	}

	fmt.Printf("==> generator output bodies vs %s\n", opts.cpythonRoot)
	for _, g := range upstreamGenerators {
		got := filepath.Join(opts.outDir, g.out)
		want := filepath.Join(opts.cpythonRoot, g.upPath)
		eq, err := bodiesEqual(got, want, g.comment)
		if err != nil {
			return fmt.Errorf("body-compare %s: %w", g.out, err)
		}
		if !eq {
			drift = append(drift, fmt.Sprintf("OUTPUT: %s body differs from %s", g.out, g.upPath))
		}
	}

	if len(drift) > 0 {
		for _, d := range drift {
			fmt.Fprintln(os.Stderr, "DRIFT:", d)
		}
		return fmt.Errorf("%d divergence(s)", len(drift))
	}
	return nil
}

// filesEqual returns true iff the two files have identical bytes.
// Missing files are unequal.
func filesEqual(a, b string) bool {
	ab, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	bb, err := os.ReadFile(b)
	if err != nil {
		return false
	}
	return bytes.Equal(ab, bb)
}

// bodiesEqual compares two generator outputs ignoring the header
// block. Upstream's generators_common.write_header() emits four
// lines starting with the comment marker, the last carrying
// "Do not edit!"; the lines name the generator and its inputs
// using a ROOT-relative path, and ROOT differs between the gopy
// checkout and a CPython checkout. The bodies after that line
// must match exactly. A few upstream outputs (notably
// opcode_targets.h) skip write_header() entirely; for those we
// compare the file as-is.
func bodiesEqual(a, b, comment string) (bool, error) {
	ab, err := stripHeader(a, comment)
	if err != nil {
		return false, err
	}
	bb, err := stripHeader(b, comment)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ab, bb), nil
}

// headerScanLines bounds how far stripHeader will look for the
// "Do not edit!" marker. write_header() emits exactly 4 lines, so
// 16 is a generous ceiling; past that we assume the file has no
// header block.
const headerScanLines = 16

func stripHeader(path, comment string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	marker := comment + " Do not edit!"
	rest := data
	for range headerScanLines {
		nl := bytes.IndexByte(rest, '\n')
		var line []byte
		if nl < 0 {
			line = rest
		} else {
			line = rest[:nl]
		}
		if bytes.HasPrefix(line, []byte(marker)) {
			if nl < 0 {
				return nil, nil
			}
			return rest[nl+1:], nil
		}
		if nl < 0 {
			break
		}
		rest = rest[nl+1:]
	}
	// No header block: compare the whole file.
	return data, nil
}
