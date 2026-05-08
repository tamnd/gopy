// CLI entry point for the uops generator. Wires the IDs parser, the
// metadata parser, and the drift check together. Each subcommand maps
// to one generated artifact under optimizer/ so the tool is composable
// in CI.
//
// CPython: Tools/cases_generator/uop_id_generator.py +
// Tools/cases_generator/uop_metadata_generator.py

package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"os"
	"strings"
)

func main() {
	var (
		src          = flag.String("src", "", "path to input header (pycore_uop_ids.h or pycore_uop_metadata.h) or DSL (bytecodes.c)")
		src2         = flag.String("src2", "", "second DSL input for optimizer-cases (optimizer_bytecodes.c)")
		out          = flag.String("out", "", "output Go file")
		pkg          = flag.String("pkg", "", "target Go package")
		mode         = flag.String("mode", "", "ids|meta|cases|optimizer-cases|check-drift")
		againstHash  = flag.String("hash", "", "expected sha256 (for check-drift)")
		fromAnalysis = flag.Bool("from-analysis", false, "drive ids/meta from analyzer AST over -src bytecodes.c instead of from the header")
		projectRoot  = flag.String("project-root", "", "CPython project root for header citations in cases / optimizer-cases output")
	)
	flag.Parse()

	if *src == "" || *mode == "" {
		flag.Usage()
		os.Exit(2)
	}

	switch *mode {
	case "check-drift":
		if err := runCheckDrift(*src, *againstHash); err != nil {
			fmt.Fprintln(os.Stderr, "drift:", err)
			os.Exit(1)
		}
	case "ids", "meta":
		if *out == "" || *pkg == "" {
			fmt.Fprintln(os.Stderr, "ids/meta require -out and -pkg")
			os.Exit(2)
		}
		if *fromAnalysis {
			if err := runEmitFromAnalysis(*src, *out, *pkg, *mode); err != nil {
				fmt.Fprintln(os.Stderr, *mode+":", err)
				os.Exit(1)
			}
			return
		}
		if err := runEmit(*src, *out, *pkg, *mode); err != nil {
			fmt.Fprintln(os.Stderr, *mode+":", err)
			os.Exit(1)
		}
	case "cases":
		if *out == "" {
			fmt.Fprintln(os.Stderr, "cases requires -out")
			os.Exit(2)
		}
		if err := runEmitCases(*src, *out, *projectRoot); err != nil {
			fmt.Fprintln(os.Stderr, "cases:", err)
			os.Exit(1)
		}
	case "optimizer-cases":
		if *out == "" || *src2 == "" {
			fmt.Fprintln(os.Stderr, "optimizer-cases requires -out, -src (bytecodes.c), and -src2 (optimizer_bytecodes.c)")
			os.Exit(2)
		}
		if err := runEmitOptimizerCases(*src, *src2, *out, *projectRoot); err != nil {
			fmt.Fprintln(os.Stderr, "optimizer-cases:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown mode:", *mode)
		os.Exit(2)
	}
}

func runCheckDrift(src, expect string) error {
	got, err := HashFile(src)
	if err != nil {
		return err
	}
	if expect != "" && got != expect {
		return fmt.Errorf("hash mismatch: file=%s recorded=%s", got, expect)
	}
	fmt.Println(got)
	return nil
}

func runEmit(src, out, pkg, mode string) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	hash, err := HashFile(src)
	if err != nil {
		return err
	}

	var rendered string
	switch mode {
	case "ids":
		ids, maxID, err := ParseUopIDs(string(body))
		if err != nil {
			return err
		}
		rendered = EmitIDsFile(pkg, hash, ids, maxID)
	case "meta":
		meta, err := ParseUopMetadata(string(body))
		if err != nil {
			return err
		}
		rendered = EmitMetaFile(pkg, hash, meta)
	}
	return os.WriteFile(out, []byte(rendered), 0o644)
}

// runEmitFromAnalysis runs the ids/meta emitter over the analyzer AST
// of bytecodes.c (passed via -src). It is opt-in via --from-analysis;
// the default path still reads the upstream pycore_*.h headers.
//
// CPython: Tools/cases_generator/uop_id_generator.py +
// Tools/cases_generator/uop_metadata_generator.py
func runEmitFromAnalysis(src, outPath, pkg, mode string) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	hash, err := HashFile(src)
	if err != nil {
		return err
	}
	p, err := NewParser(string(body), src)
	if err != nil {
		return err
	}
	var forest []Node
	for {
		n, err := p.Definition()
		if err != nil {
			return err
		}
		if n == nil {
			break
		}
		forest = append(forest, n)
	}
	analysis, err := AnalyzeForest(forest)
	if err != nil {
		return err
	}

	var rendered string
	switch mode {
	case "ids":
		ids, maxID, err := BuildUopIDsFromAnalysis(analysis, false)
		if err != nil {
			return err
		}
		rendered = EmitIDsFile(pkg, hash, ids, maxID)
	case "meta":
		meta, err := BuildUopMetaFromAnalysis(analysis)
		if err != nil {
			return err
		}
		rendered = EmitMetaFile(pkg, hash, meta)
	}
	return os.WriteFile(outPath, []byte(rendered), 0o644)
}

// sliceBytecodesSection returns the body between the
// // BEGIN BYTECODES // and // END BYTECODES // markers when both are
// present, or the original body otherwise. optimizer_bytecodes.c has
// no such markers, so the unfenced fall-through is the common path
// for that file.
//
// CPython: Tools/cases_generator/parser.py prepare_for_parsing
func sliceBytecodesSection(text string) string {
	lines := strings.Split(text, "\n")
	bi, ei := -1, -1
	for i, l := range lines {
		switch strings.TrimRight(l, "\r") {
		case "// BEGIN BYTECODES //":
			bi = i
		case "// END BYTECODES //":
			ei = i
		}
	}
	if bi < 0 || ei < 0 || ei <= bi {
		return text
	}
	return strings.Join(lines[bi+1:ei], "\n")
}

// parseDSL reads a bytecodes-flavored DSL file and returns the analyzer
// forest produced by Parser.Definition. The file is sliced between the
// // BEGIN BYTECODES // / // END BYTECODES // markers when present, to
// match the slice the upstream parser feeds the lexer.
//
// CPython: Tools/cases_generator/parser.py parse_files
func parseDSL(src string) ([]Node, error) {
	body, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}
	text := sliceBytecodesSection(string(body))
	p, err := NewParser(text, src)
	if err != nil {
		return nil, err
	}
	var forest []Node
	for {
		n, err := p.Definition()
		if err != nil {
			return nil, err
		}
		if n == nil {
			break
		}
		forest = append(forest, n)
	}
	return forest, nil
}

// runEmitCases drives GenerateTier2 over Python/bytecodes.c.
//
// CPython: Tools/cases_generator/tier2_generator.py generate_tier2
func runEmitCases(src, out, projectRoot string) error {
	forest, err := parseDSL(src)
	if err != nil {
		return err
	}
	analysis, err := AnalyzeForest(forest)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := GenerateTier2(analysis, projectRoot, &buf); err != nil {
		return err
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("gofmt cases output: %w", err)
	}
	return os.WriteFile(out, formatted, 0o644)
}

// runEmitOptimizerCases drives GenerateOptimizer over the base
// bytecodes.c plus the optimizer_bytecodes.c override.
//
// CPython: Tools/cases_generator/optimizer_generator.py generate_tier2_abstract_from_files
func runEmitOptimizerCases(src, src2, out, projectRoot string) error {
	baseForest, err := parseDSL(src)
	if err != nil {
		return err
	}
	overrideForest, err := parseDSL(src2)
	if err != nil {
		return err
	}
	base, err := AnalyzeForest(baseForest)
	if err != nil {
		return err
	}
	override, err := AnalyzeForest(overrideForest)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := GenerateOptimizer(base, override, projectRoot, &buf); err != nil {
		return err
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("gofmt optimizer-cases output: %w", err)
	}
	return os.WriteFile(out, formatted, 0o644)
}
