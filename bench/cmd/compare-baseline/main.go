// compare-baseline gates a fresh gopy bench run against the frozen
// baseline_v0124.json. It exits non-zero if any bench got materially
// slower (>TOLERANCE percent on mean_ms) or regressed from an ok status
// to a failure status.
//
// Improvements (faster than baseline) are reported but never fail.
// New benches in the current run that the baseline doesn't know about
// are reported as "new" and don't fail either.
//
// Usage:
//
//	go run ./bench/cmd/compare-baseline \
//	    -current bench/raw_gopy.json \
//	    -baseline bench/baseline_v0124.json \
//	    [-tolerance 0.10]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
)

type entry struct {
	Status string  `json:"status"`
	MeanMs float64 `json:"mean_ms"`
	MinMs  float64 `json:"min_ms"`
}

type file struct {
	Interpreter string           `json:"interpreter"`
	Benchmarks  map[string]entry `json:"benchmarks"`
}

func load(path string) (*file, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Benchmarks == nil {
		return nil, fmt.Errorf("%s: no benchmarks", path)
	}
	return &f, nil
}

func main() {
	current := flag.String("current", "bench/raw_gopy.json", "current run JSON")
	baseline := flag.String("baseline", "bench/baseline_v0124.json", "frozen baseline JSON")
	tolerance := flag.Float64("tolerance", 0.10, "max allowed mean_ms slowdown ratio (0.10 = +10%)")
	flag.Parse()

	cur, err := load(*current)
	if err != nil {
		die(err)
	}
	base, err := load(*baseline)
	if err != nil {
		die(err)
	}

	names := map[string]struct{}{}
	for n := range cur.Benchmarks {
		names[n] = struct{}{}
	}
	for n := range base.Benchmarks {
		names[n] = struct{}{}
	}
	ordered := make([]string, 0, len(names))
	for n := range names {
		ordered = append(ordered, n)
	}
	sort.Strings(ordered)

	var regressions, improvements, news, drops, missing []string
	for _, n := range ordered {
		c, cok := cur.Benchmarks[n]
		b, bok := base.Benchmarks[n]
		switch {
		case bok && !cok:
			missing = append(missing, fmt.Sprintf("%s: dropped from current run", n))
		case !bok && cok:
			news = append(news, fmt.Sprintf("%s: new (mean=%.2fms, status=%s)", n, c.MeanMs, c.Status))
		case b.Status == "ok" && c.Status != "ok":
			drops = append(drops, fmt.Sprintf("%s: ok -> %s", n, c.Status))
		case b.Status != "ok" && c.Status == "ok":
			improvements = append(improvements, fmt.Sprintf("%s: %s -> ok (mean=%.2fms)", n, b.Status, c.MeanMs))
		case b.Status == "ok" && c.Status == "ok" && b.MeanMs > 0:
			delta := (c.MeanMs - b.MeanMs) / b.MeanMs
			switch {
			case delta > *tolerance:
				regressions = append(regressions,
					fmt.Sprintf("%s: %.2fms -> %.2fms (+%.1f%%)", n, b.MeanMs, c.MeanMs, delta*100))
			case delta < -*tolerance:
				improvements = append(improvements,
					fmt.Sprintf("%s: %.2fms -> %.2fms (%.1f%%)", n, b.MeanMs, c.MeanMs, delta*100))
			}
		}
	}

	report("Improvements", improvements)
	report("New benches", news)
	report("Missing benches", missing)
	report("Regressions", regressions)
	report("Status drops", drops)

	if len(regressions) > 0 || len(drops) > 0 || len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "\ncompare-baseline: FAIL (%d regressions, %d drops, %d missing)\n",
			len(regressions), len(drops), len(missing))
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "\ncompare-baseline: OK")
}

func report(title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Println("##", title)
	for _, l := range lines {
		fmt.Println("- " + l)
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "compare-baseline:", err)
	os.Exit(2)
}
