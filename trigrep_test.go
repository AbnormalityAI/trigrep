package trigrep

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) *Index {
	root := t.TempDir()
	write(t, root, "a.go", "package a\nfunc Hello() {}\n// TODO fix\n")
	write(t, root, "b/b.go", "package b\nfunc World() {}\n")
	write(t, root, "bin.dat", "\x00\x01\x02Hello")
	write(t, root, ".git/x", "Hello")
	ix, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func TestBuildSkipsBinaryAndGit(t *testing.T) {
	ix := fixture(t)
	if !reflect.DeepEqual(ix.Files, []string{"a.go", "b/b.go"}) {
		t.Fatalf("files = %v", ix.Files)
	}
}

func TestSearchUsesIndex(t *testing.T) {
	ix := fixture(t)
	ms, st, err := ix.Search("Hello", true, true)
	if err != nil || len(ms) != 1 || ms[0].File != "a.go" || ms[0].Line != 2 {
		t.Fatalf("%v %v", ms, err)
	}
	if !st.UsedIndex || st.Candidates != 1 || st.Scanned != 1 {
		t.Fatalf("stats %+v", st)
	}
}

func TestRegexRequiredLiterals(t *testing.T) {
	cases := map[string][]string{
		`foo\d+bar`:    {"foo", "bar"},
		`(?i)hello`:    nil,
		`ab`:           nil, // shorter than a trigram
		`func (Hello)`: {"func ", "Hello"},
		`a|bcdef`:      nil,
	}
	for pat, want := range cases {
		q, err := Compile(pat, false)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(q.Required, want) {
			t.Errorf("%q: got %v want %v", pat, q.Required, want)
		}
	}
}

func TestIndexedEqualsScan(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateCorpus(dir, 60, 3); err != nil {
		t.Fatal(err)
	}
	ix, _ := Build(dir)
	for _, p := range []string{`NEEDLE_\w+`, `socket buffer`, `ErrConnection\w+`, `(?i)needle`, `zzzzzz`, `r.llback`} {
		a, _, err := ix.Search(p, false, true)
		b, _, _ := ix.Search(p, false, false)
		if err != nil || !reflect.DeepEqual(a, b) {
			t.Fatalf("%q: indexed %d vs scan %d (%v)", p, len(a), len(b), err)
		}
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	ix := fixture(t)
	p := filepath.Join(t.TempDir(), "idx")
	if err := ix.Save(p); err != nil {
		t.Fatal(err)
	}
	l, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(l.Files, ix.Files) || !reflect.DeepEqual(l.postings, ix.postings) {
		t.Fatal("round trip mismatch")
	}
	os.WriteFile(p, []byte("garbage"), 0o644)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error on garbage")
	}
}
