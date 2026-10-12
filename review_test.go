package trigrep

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModifiedFileIsStillFound(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "a.txt")
	os.WriteFile(p, []byte("old"), 0o600)
	ix, _ := Build(d)
	os.WriteFile(p, []byte("needle"), 0o600)
	a, st, _ := ix.Search("needle", true, true)
	b, _, _ := ix.Search("needle", true, false)
	if len(a) != len(b) || len(a) != 1 {
		t.Fatalf("modified file missed: indexed %d full scan %d", len(a), len(b))
	}
	if st.Stale != 1 {
		t.Fatalf("stale count = %d", st.Stale)
	}
}

func TestZeroRunsRejected(t *testing.T) {
	ix := fixture(t)
	for _, n := range []int{0, -3} {
		if _, err := Bench(ix, []string{"Hello"}, n); err == nil {
			t.Fatalf("runs=%d not rejected", n)
		}
	}
}

func TestCorruptPostingRejectedOnLoad(t *testing.T) {
	ix := fixture(t)
	for _, k := range []uint32{tri('H', 'e', 'l'), tri('e', 'l', 'l'), tri('l', 'l', 'o')} {
		ix.postings[k] = []uint32{999}
	}
	p := filepath.Join(t.TempDir(), "bad-index")
	if err := ix.Save(p); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("out-of-range posting accepted")
	}
}

func TestLoadRejectsUnsafeAndMalformed(t *testing.T) {
	good := fixture(t)
	dir := t.TempDir()
	save := func(ix *Index) string {
		p := filepath.Join(dir, "i")
		if err := ix.Save(p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, path := range []string{"../etc/passwd", "/abs", "a//b", ""} {
		ix := &Index{Root: "x", Files: []string{path}, Meta: []FileMeta{{}}, postings: map[uint32][]uint32{}}
		if _, err := Load(save(ix)); err == nil {
			t.Errorf("unsafe path %q accepted", path)
		}
	}
	p := save(good)
	b, _ := os.ReadFile(p)
	for cut := 1; cut < len(b); cut += 7 { // every truncation must error, never panic
		os.WriteFile(p, b[:cut], 0o600)
		if _, err := Load(p); err == nil {
			t.Fatalf("truncated index (%d bytes) accepted", cut)
		}
	}
	dup := &Index{Root: "x", Files: []string{"a", "b"}, Meta: []FileMeta{{}, {}},
		postings: map[uint32][]uint32{1: {1, 1}}}
	if _, err := Load(save(dup)); err == nil {
		t.Error("non-increasing posting accepted")
	}
}

func FuzzLoad(f *testing.F) {
	ix, _ := Build(".")
	p := filepath.Join(f.TempDir(), "seed")
	ix.Save(p)
	seed, _ := os.ReadFile(p)
	f.Add(seed)
	f.Add([]byte("TRGR2\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		p := filepath.Join(t.TempDir(), "i")
		os.WriteFile(p, data, 0o600)
		if l, err := Load(p); err == nil {
			l.Search("a.b", false, true) // must not panic on anything Load accepts
		}
	})
}
