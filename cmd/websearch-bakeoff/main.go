// Command websearch-bakeoff runs the same queries through every keyed web search backend and
// writes the results side by side as Markdown, so the production default is chosen on What Iff
// traffic rather than published benchmarks (ADR 0x021).
//
//	PARALLEL_API_KEY=... BRAVE_SEARCH_API_KEY=... \
//	  go run ./cmd/websearch-bakeoff -queries scripts/websearch-bakeoff-queries.txt > bakeoff.md
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/websearch"
)

func main() {
	queriesPath := flag.String("queries", "scripts/websearch-bakeoff-queries.txt", "file with one query per line (# comments allowed)")
	maxResults := flag.Int("n", 5, "results per query")
	flag.Parse()

	queries, err := readQueries(*queriesPath)
	if err != nil {
		fail(err)
	}
	backends := keyedBackends()
	if len(backends) == 0 {
		fail(fmt.Errorf("set PARALLEL_API_KEY and/or BRAVE_SEARCH_API_KEY"))
	}

	fmt.Printf("# Web search bake-off\n\n%s · %d queries · backends: %s\n", time.Now().Format(time.RFC3339), len(queries), names(backends))
	totals := make(map[string]time.Duration)
	for _, q := range queries {
		fmt.Printf("\n## %s\n", q)
		for _, b := range backends {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			start := time.Now()
			results, err := b.Search(ctx, websearch.Query{Query: q, MaxResults: *maxResults})
			elapsed := time.Since(start)
			cancel()
			totals[b.Name()] += elapsed
			fmt.Printf("\n**%s** (%dms)\n\n", b.Name(), elapsed.Milliseconds())
			if err != nil {
				fmt.Printf("- error: %v\n", err)
				continue
			}
			if len(results) == 0 {
				fmt.Println("- (no results)")
			}
			for i, r := range results {
				fmt.Printf("%d. [%s](%s)%s — %s\n", i+1, oneLine(r.Title, 100), r.URL, dated(r.PublishedAt), oneLine(r.Snippet, 200))
			}
		}
	}
	fmt.Printf("\n## Mean latency\n\n")
	for _, b := range backends {
		fmt.Printf("- %s: %dms\n", b.Name(), (totals[b.Name()] / time.Duration(len(queries))).Milliseconds())
	}
}

// keyedBackends returns each configured backend on its own (no fallback), for comparison.
func keyedBackends() []websearch.Backend {
	client := &http.Client{Timeout: 30 * time.Second}
	var out []websearch.Backend
	if key := strings.TrimSpace(os.Getenv("PARALLEL_API_KEY")); key != "" {
		out = append(out, websearch.NewParallel(key, os.Getenv("PARALLEL_SEARCH_MODE"), client))
	}
	if key := strings.TrimSpace(os.Getenv("BRAVE_SEARCH_API_KEY")); key != "" {
		out = append(out, websearch.NewBrave(key, client))
	}
	return out
}

func readQueries(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s has no queries", path)
	}
	return out, nil
}

func names(backends []websearch.Backend) string {
	parts := make([]string, len(backends))
	for i, b := range backends {
		parts[i] = b.Name()
	}
	return strings.Join(parts, ", ")
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

func dated(published string) string {
	if published == "" {
		return ""
	}
	return " (" + published + ")"
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "websearch-bakeoff:", err)
	os.Exit(1)
}
