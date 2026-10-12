package trigrep

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

// BenchResult is one measured query.
type BenchResult struct {
	Pattern                 string
	Matches                 int
	IndexMedian, ScanMedian time.Duration
	Candidates, Files       int
}

var vocab = strings.Fields(`alpha beta gamma delta lambda socket buffer parse token render
 config handler stream cursor mutex queue packet signal vector matrix kernel module
 select insert update commit rollback cache evict shard replica leader follower`)

// GenerateCorpus writes a deterministic synthetic tree: nFiles files of about
// 200 lines each, drawn from a fixed vocabulary, plus a few planted needles.
func GenerateCorpus(dir string, nFiles int, seed int64) error {
	r := rand.New(rand.NewSource(seed))
	for i := 0; i < nFiles; i++ {
		sub := filepath.Join(dir, fmt.Sprintf("pkg%02d", i%20))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return err
		}
		var b strings.Builder
		for l := 0; l < 200; l++ {
			for w := 0; w < 8; w++ {
				b.WriteString(vocab[r.Intn(len(vocab))])
				b.WriteByte(' ')
			}
			if i%97 == 0 && l == 100 {
				b.WriteString("NEEDLE_rare_identifier_42")
			}
			if i%13 == 0 && l == 50 {
				b.WriteString("ErrConnectionReset")
			}
			b.WriteByte('\n')
		}
		if err := os.WriteFile(filepath.Join(sub, fmt.Sprintf("f%04d.txt", i)), []byte(b.String()), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func median(f func(), runs int) time.Duration {
	ds := make([]time.Duration, runs)
	for i := range ds {
		t := time.Now()
		f()
		ds[i] = time.Since(t)
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	return ds[len(ds)/2]
}

// Bench compares indexed search to a full scan on the same index, with a
// warm-up run, median of runs, and a correctness check that both paths agree.
func Bench(ix *Index, patterns []string, runs int) ([]BenchResult, error) {
	if runs < 1 {
		return nil, fmt.Errorf("runs must be at least 1, got %d", runs)
	}
	var out []BenchResult
	for _, p := range patterns {
		a, st, err := ix.Search(p, false, true)
		if err != nil {
			return nil, err
		}
		b, _, _ := ix.Search(p, false, false)
		if !reflect.DeepEqual(a, b) {
			return nil, fmt.Errorf("pattern %q: indexed and full-scan matches differ (%d vs %d)", p, len(a), len(b))
		}
		out = append(out, BenchResult{
			Pattern: p, Matches: len(a), Candidates: st.Candidates, Files: st.Indexed,
			IndexMedian: median(func() { ix.Search(p, false, true) }, runs),
			ScanMedian:  median(func() { ix.Search(p, false, false) }, runs),
		})
	}
	return out, nil
}
