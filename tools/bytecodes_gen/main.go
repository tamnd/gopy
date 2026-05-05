// CLI entry point for the bytecodes generator. Wires the parser, the
// stack-effect analyzer, the metadata emitter, the Tier-1 emitter, and
// the drift check together. Each subcommand maps to one generated
// artifact so the tool is composable in CI.

package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	var (
		src         = flag.String("src", "", "path to Python/bytecodes.c")
		out         = flag.String("out", "", "output Go file")
		pkg         = flag.String("pkg", "", "target Go package")
		mode        = flag.String("mode", "", "metadata|tier1|check-drift")
		againstHash = flag.String("hash", "", "expected sha256 (for check-drift)")
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
	case "metadata", "tier1":
		if *out == "" || *pkg == "" {
			fmt.Fprintln(os.Stderr, "metadata/tier1 require -out and -pkg")
			os.Exit(2)
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
	defs, err := ParseBytecodes(string(body))
	if err != nil {
		return err
	}

	var rendered string
	switch mode {
	case "metadata":
		rendered = EmitMetadataFile(pkg, hash, CollectMetadata(defs.Order))
	case "tier1":
		analyses, err := AnalyzeAll(defs.Order)
		if err != nil {
			return err
		}
		rendered = EmitTier1File(pkg, hash, analyses)
	}
	return os.WriteFile(out, []byte(rendered), 0o644)
}
