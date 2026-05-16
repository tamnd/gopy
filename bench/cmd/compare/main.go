// compare reads pyperformance-shaped JSON from cpython, PyPy, and the
// gopy shim, then writes a markdown table that the 1712 spec renders.
//
// Input shape (all three files):
//
//	{
//	  "interpreter": "cpython|pypy|gopy",
//	  "version":     "...",
//	  "benchmarks":  { "<name>": {<entry>} }
//	}
//
// gopy entry: see bench/run_gopy.sh ("status", "mean_ms", "min_ms", "runs").
//
// pyperformance entry: the runner writes a richer object; we accept
// either the top-level pyperformance file (a "benchmarks" array of
// {"metadata": {"name": ...}, "runs": [{"values":[...]}]} entries) or
// the normalized {"benchmarks": {"name": {"mean_ms": ...}}} shape if
// the caller pre-processed it. The reader handles both.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

type benchEntry struct {
	Status string  `json:"status"`
	MeanMs float64 `json:"mean_ms"`
	MinMs  float64 `json:"min_ms"`
}

type runFile struct {
	Interpreter string                `json:"interpreter"`
	Version     string                `json:"version"`
	Benchmarks  map[string]benchEntry `json:"benchmarks"`
}

// pyperformanceFile is the raw output of `pyperformance run`. We only
// pull the fields we need; the rest is ignored.
type pyperfFile struct {
	Benchmarks []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Runs []struct {
			Values []float64 `json:"values"`
		} `json:"runs"`
	} `json:"benchmarks"`
}

func loadRun(path string) (*runFile, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // CLI tool reads paths supplied on argv.
	if err != nil {
		return nil, err
	}
	// Try gopy shim shape first.
	var r runFile
	if err := json.Unmarshal(raw, &r); err == nil && r.Benchmarks != nil {
		return &r, nil
	}
	// Fall back to pyperformance raw.
	var p pyperfFile
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out := &runFile{Benchmarks: map[string]benchEntry{}}
	for _, b := range p.Benchmarks {
		var all []float64
		for _, run := range b.Runs {
			all = append(all, run.Values...)
		}
		if len(all) == 0 {
			out.Benchmarks[b.Metadata.Name] = benchEntry{Status: "no_samples"}
			continue
		}
		sort.Float64s(all)
		var sum float64
		for _, v := range all {
			sum += v
		}
		out.Benchmarks[b.Metadata.Name] = benchEntry{
			Status: "ok",
			MinMs:  all[0] * 1000.0,
			MeanMs: (sum / float64(len(all))) * 1000.0,
		}
	}
	return out, nil
}

func cell(e benchEntry, ok bool) string {
	if !ok {
		return "N/A"
	}
	if e.Status != "ok" {
		return "N/A"
	}
	return fmt.Sprintf("%.2f", e.MeanMs)
}

func ratio(num, denom benchEntry, numOK, denomOK bool) string {
	if !numOK || !denomOK || num.Status != "ok" || denom.Status != "ok" || denom.MeanMs == 0 {
		return "N/A"
	}
	return fmt.Sprintf("%.2fx", num.MeanMs/denom.MeanMs)
}

func geomean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var sumLog float64
	for _, v := range vals {
		sumLog += math.Log(v)
	}
	return math.Exp(sumLog / float64(len(vals)))
}

//nolint:gocyclo // glue script: argument parsing + report assembly fan out by design.
func main() {
	cpy := flag.String("cpy", "", "cpython JSON")
	pypy := flag.String("pypy", "", "pypy JSON")
	gopy := flag.String("gopy", "", "gopy JSON")
	out := flag.String("out", "", "output markdown path (stdout if empty)")
	flag.Parse()

	if *cpy == "" || *pypy == "" || *gopy == "" {
		fmt.Fprintln(os.Stderr, "compare: -cpy, -pypy, -gopy are all required")
		os.Exit(2)
	}

	rcpy, err := loadRun(*cpy)
	if err != nil {
		die(err)
	}
	rpypy, err := loadRun(*pypy)
	if err != nil {
		die(err)
	}
	rgopy, err := loadRun(*gopy)
	if err != nil {
		die(err)
	}

	names := map[string]struct{}{}
	for n := range rcpy.Benchmarks {
		names[n] = struct{}{}
	}
	for n := range rpypy.Benchmarks {
		names[n] = struct{}{}
	}
	for n := range rgopy.Benchmarks {
		names[n] = struct{}{}
	}
	ordered := make([]string, 0, len(names))
	for n := range names {
		ordered = append(ordered, n)
	}
	sort.Strings(ordered)

	var b strings.Builder
	fmt.Fprintln(&b, "| Benchmark | cpython 3.14 (ms) | PyPy 3.11 (ms) | gopy (ms) | gopy / cpython | gopy / PyPy |")
	fmt.Fprintln(&b, "|---|---:|---:|---:|---:|---:|")

	var cpyMeans, pypyMeans, gopyMeans []float64
	for _, n := range ordered {
		c, cok := rcpy.Benchmarks[n]
		p, pok := rpypy.Benchmarks[n]
		g, gok := rgopy.Benchmarks[n]
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s |\n",
			n,
			cell(c, cok),
			cell(p, pok),
			cell(g, gok),
			ratio(g, c, gok, cok),
			ratio(g, p, gok, pok),
		)
		if cok && c.Status == "ok" {
			cpyMeans = append(cpyMeans, c.MeanMs)
		}
		if pok && p.Status == "ok" {
			pypyMeans = append(pypyMeans, p.MeanMs)
		}
		if gok && g.Status == "ok" {
			gopyMeans = append(gopyMeans, g.MeanMs)
		}
	}

	gc, gp, gg := geomean(cpyMeans), geomean(pypyMeans), geomean(gopyMeans)
	gcOver := "N/A"
	if gc > 0 && gg > 0 {
		gcOver = fmt.Sprintf("%.2fx", gg/gc)
	}
	gpOver := "N/A"
	if gp > 0 && gg > 0 {
		gpOver = fmt.Sprintf("%.2fx", gg/gp)
	}
	fmt.Fprintf(&b, "| **geomean** | %.2f | %.2f | %.2f | %s | %s |\n", gc, gp, gg, gcOver, gpOver)

	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "cpython: `%s`  PyPy: `%s`  gopy: `%s`\n",
		safe(rcpy.Version), safe(rpypy.Version), safe(rgopy.Version))

	if *out == "" {
		_, _ = os.Stdout.WriteString(b.String())
	} else if err := os.WriteFile(*out, []byte(b.String()), 0o644); err != nil { //nolint:gosec // CLI emits a human-readable report.
		die(err)
	}
}

func safe(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "compare:", err)
	os.Exit(1)
}
