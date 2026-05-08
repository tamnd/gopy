// CLI entry point for the uops generator. Wires the IDs parser, the
// metadata parser, and the drift check together. Each subcommand maps
// to one generated artifact under optimizer/ so the tool is composable
// in CI.
//
// CPython: Tools/cases_generator/uop_id_generator.py +
// Tools/cases_generator/uop_metadata_generator.py

package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	var (
		src          = flag.String("src", "", "path to input header (pycore_uop_ids.h or pycore_uop_metadata.h)")
		out          = flag.String("out", "", "output Go file")
		pkg          = flag.String("pkg", "", "target Go package")
		mode         = flag.String("mode", "", "ids|meta|check-drift")
		againstHash  = flag.String("hash", "", "expected sha256 (for check-drift)")
		fromAnalysis = flag.Bool("from-analysis", false, "drive ids/meta from analyzer AST over -src bytecodes.c instead of from the header")
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
