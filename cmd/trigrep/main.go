// Command trigrep indexes a tree and searches it with a trigram index.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AbnormalityAI/trigrep"
)

const usage = `usage:
  trigrep index [-root DIR]            build DIR/.trigrep/index
  trigrep search [-root DIR] [-F] [-stats] PATTERN
  trigrep bench [-files N] [-runs N]   benchmark on a generated corpus`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	root := fs.String("root", ".", "tree root")
	lit := fs.Bool("F", false, "treat pattern as a fixed string")
	stats := fs.Bool("stats", false, "print index statistics to stderr")
	nfiles := fs.Int("files", 2000, "bench: corpus files")
	runs := fs.Int("runs", 7, "bench: runs per query")
	fs.Parse(args)
	idxPath := func(r string) string { return filepath.Join(r, ".trigrep", "index") }
	switch cmd {
	case "index":
		t := time.Now()
		ix, err := trigrep.Build(*root)
		check(err)
		check(os.MkdirAll(filepath.Join(*root, ".trigrep"), 0o755))
		check(ix.Save(idxPath(*root)))
		fmt.Printf("indexed %d files, %d trigrams in %s\n", len(ix.Files), ix.TrigramCount(), time.Since(t).Round(time.Millisecond))
	case "search":
		if fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, usage)
			os.Exit(2)
		}
		ix, err := trigrep.Load(idxPath(*root))
		check(err)
		ix.Root = *root
		ms, st, err := ix.Search(fs.Arg(0), *lit, true)
		check(err)
		for _, m := range ms {
			fmt.Printf("%s:%d:%s\n", m.File, m.Line, m.Text)
		}
		if *stats {
			fmt.Fprintf(os.Stderr, "files=%d candidates=%d scanned=%d stale=%d used_index=%v\n", st.Indexed, st.Candidates, st.Scanned, st.Stale, st.UsedIndex)
		}
		if len(ms) == 0 {
			os.Exit(1)
		}
	case "bench":
		dir, err := os.MkdirTemp("", "trigrep-bench")
		check(err)
		defer os.RemoveAll(dir)
		check(trigrep.GenerateCorpus(dir, *nfiles, 1))
		ix, err := trigrep.Build(dir)
		check(err)
		pats := []string{`NEEDLE_rare_identifier_42`, `ErrConnection\w+`, `rollback replica leader`, `(?i)needle_rare`, `socket buffer`}
		res, err := trigrep.Bench(ix, pats, *runs)
		check(err)
		fmt.Printf("| pattern | matches | candidates/files | indexed | full scan | speedup |\n|---|---|---|---|---|---|\n")
		for _, r := range res {
			fmt.Printf("| `%s` | %d | %d/%d | %s | %s | %.1fx |\n", r.Pattern, r.Matches, r.Candidates, r.Files,
				r.IndexMedian.Round(time.Microsecond), r.ScanMedian.Round(time.Microsecond), float64(r.ScanMedian)/float64(r.IndexMedian))
		}
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
