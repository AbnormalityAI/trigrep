// Package trigrep builds a trigram inverted index over a directory tree and
// uses it to narrow regex and literal searches to a few candidate files.
package trigrep

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const magic = "TRGR2\n"

// Index maps each trigram to the sorted list of file IDs that contain it.
type Index struct {
	Root     string
	Files    []string
	Meta     []FileMeta // size and mtime at index time, parallel to Files
	postings map[uint32][]uint32
}

// FileMeta records what a file looked like when it was indexed, so searches
// can notice files that changed since.
type FileMeta struct {
	Size    int64
	ModTime int64 // unix nanoseconds
}

func tri(a, b, c byte) uint32 { return uint32(a)<<16 | uint32(b)<<8 | uint32(c) }

// IsText reports whether data looks like text (no NUL in the first 8 KiB).
func IsText(data []byte) bool {
	n := len(data)
	if n > 8192 {
		n = 8192
	}
	return bytes.IndexByte(data[:n], 0) < 0
}

var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, ".trigrep": true}

// Build walks root and indexes every text file under 8 MiB.
func Build(root string) (*Index, error) {
	ix := &Index{Root: root, postings: map[uint32][]uint32{}}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 8<<20 {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil || !IsText(data) {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		id := uint32(len(ix.Files))
		ix.Files = append(ix.Files, filepath.ToSlash(rel))
		ix.Meta = append(ix.Meta, FileMeta{info.Size(), info.ModTime().UnixNano()})
		seen := map[uint32]struct{}{}
		for i := 0; i+2 < len(data); i++ {
			seen[tri(data[i], data[i+1], data[i+2])] = struct{}{}
		}
		for t := range seen {
			ix.postings[t] = append(ix.postings[t], id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ix, nil
}

// Candidates returns file IDs that contain every trigram of every literal.
// With no usable literal it returns all files and ok=false.
func (ix *Index) Candidates(literals []string) (ids []uint32, ok bool) {
	var lists [][]uint32
	for _, lit := range literals {
		for i := 0; i+2 < len(lit); i++ {
			l, found := ix.postings[tri(lit[i], lit[i+1], lit[i+2])]
			if !found {
				return nil, true
			}
			lists = append(lists, l)
		}
	}
	if len(lists) == 0 {
		all := make([]uint32, len(ix.Files))
		for i := range all {
			all[i] = uint32(i)
		}
		return all, false
	}
	sort.Slice(lists, func(i, j int) bool { return len(lists[i]) < len(lists[j]) })
	cur := lists[0]
	for _, l := range lists[1:] {
		cur = intersect(cur, l)
		if len(cur) == 0 {
			break
		}
	}
	return cur, true
}

func intersect(a, b []uint32) []uint32 {
	var out []uint32
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, a[i])
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return out
}

// TrigramCount returns the number of distinct trigrams indexed.
func (ix *Index) TrigramCount() int { return len(ix.postings) }

// Save writes the index: magic, file table, then delta-varint postings.
func (ix *Index) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	w.WriteString(magic)
	var buf [binary.MaxVarintLen64]byte
	put := func(v uint64) { w.Write(buf[:binary.PutUvarint(buf[:], v)]) }
	put(uint64(len(ix.Root)))
	w.WriteString(ix.Root)
	put(uint64(len(ix.Files)))
	for _, p := range ix.Files {
		put(uint64(len(p)))
		w.WriteString(p)
	}
	for _, m := range ix.Meta {
		put(uint64(m.Size))
		put(uint64(m.ModTime))
	}
	keys := make([]uint32, 0, len(ix.postings))
	for k := range ix.postings {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	put(uint64(len(keys)))
	for _, k := range keys {
		put(uint64(k))
		l := ix.postings[k]
		put(uint64(len(l)))
		prev := uint32(0)
		for _, id := range l {
			put(uint64(id - prev))
			prev = id
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Limits applied while loading, so a corrupt or hostile index file cannot
// cause huge allocations or out-of-range file IDs.
const (
	maxFiles    = 1 << 24
	maxPathLen  = 4096
	maxTrigrams = 1 << 24
)

var errCorruptIndex = errors.New("corrupt trigrep index")

func safeRel(p string) bool {
	if p == "" || filepath.IsAbs(p) || strings.Contains(p, "\\") || strings.ContainsRune(p, 0) {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// Load reads and validates an index written by Save. Every posting list must
// be strictly increasing with IDs below the file count, and every path must be
// a clean relative path.
func Load(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	head := make([]byte, len(magic))
	if _, err := io.ReadFull(r, head); err != nil || string(head) != magic {
		return nil, errors.New("not a trigrep index (or an older format: re-run trigrep index)")
	}
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", errCorruptIndex, fmt.Sprintf(format, a...))
	}
	rd := func() (uint64, error) {
		v, err := binary.ReadUvarint(r)
		if err != nil {
			return 0, bad("truncated or malformed varint")
		}
		return v, nil
	}
	str := func(limit uint64) (string, error) {
		n, err := rd()
		if err != nil {
			return "", err
		}
		if n > limit {
			return "", bad("string length %d over limit", n)
		}
		b := make([]byte, n)
		if _, err := io.ReadFull(r, b); err != nil {
			return "", bad("truncated string")
		}
		return string(b), nil
	}
	ix := &Index{postings: map[uint32][]uint32{}}
	if ix.Root, err = str(maxPathLen); err != nil {
		return nil, err
	}
	nf, err := rd()
	if err != nil {
		return nil, err
	}
	if nf > maxFiles {
		return nil, bad("file count %d over limit", nf)
	}
	for i := uint64(0); i < nf; i++ {
		p, err := str(maxPathLen)
		if err != nil {
			return nil, err
		}
		if !safeRel(p) {
			return nil, bad("unsafe file path %q", p)
		}
		ix.Files = append(ix.Files, p)
	}
	for i := uint64(0); i < nf; i++ {
		size, err1 := rd()
		mt, err2 := rd()
		if err1 != nil || err2 != nil {
			return nil, bad("truncated file metadata")
		}
		ix.Meta = append(ix.Meta, FileMeta{int64(size), int64(mt)})
	}
	nk, err := rd()
	if err != nil {
		return nil, err
	}
	if nk > maxTrigrams {
		return nil, bad("trigram count %d over limit", nk)
	}
	lastKey := int64(-1)
	for i := uint64(0); i < nk; i++ {
		k, err1 := rd()
		n, err2 := rd()
		if err1 != nil || err2 != nil {
			return nil, bad("truncated posting header")
		}
		if k >= 1<<24 || int64(k) <= lastKey {
			return nil, bad("trigram keys out of order or out of range")
		}
		lastKey = int64(k)
		if n == 0 || n > nf {
			return nil, bad("posting length %d invalid for %d files", n, nf)
		}
		l := make([]uint32, 0, n)
		prev := int64(-1)
		for j := uint64(0); j < n; j++ {
			d, err := rd()
			if err != nil {
				return nil, err
			}
			id := prev + int64(d)
			if j == 0 {
				id = int64(d)
			}
			if d > nf || id >= int64(nf) || (j > 0 && id <= prev) {
				return nil, bad("posting id out of range or not increasing")
			}
			prev = id
			l = append(l, uint32(id))
		}
		ix.postings[uint32(k)] = l
	}
	return ix, nil
}

// Match is one matching line.
type Match struct {
	File string
	Line int
	Text string
}

// Stats describes how a search was executed.
type Stats struct {
	Indexed, Candidates, Scanned int
	Stale                        int // indexed files changed since indexing, always scanned
	UsedIndex                    bool
}

// Search runs pattern (a Go regexp; set literal to treat it as plain text).
func (ix *Index) Search(pattern string, literal bool, useIndex bool) ([]Match, Stats, error) {
	q, err := Compile(pattern, literal)
	if err != nil {
		return nil, Stats{}, err
	}
	st := Stats{Indexed: len(ix.Files)}
	var ids []uint32
	if useIndex {
		ids, st.UsedIndex = ix.Candidates(q.Required)
	} else {
		ids, _ = ix.Candidates(nil)
	}
	if useIndex {
		var stale []uint32
		stale = ix.staleIDs()
		st.Stale = len(stale)
		if st.UsedIndex && len(stale) > 0 {
			ids = unionSorted(ids, stale)
		}
	}
	st.Candidates = len(ids)
	var out []Match
	for _, id := range ids {
		if int(id) >= len(ix.Files) {
			return nil, st, fmt.Errorf("%w: file id %d out of range", errCorruptIndex, id)
		}
		data, err := os.ReadFile(filepath.Join(ix.Root, ix.Files[id]))
		if err != nil {
			continue
		}
		st.Scanned++
		line := 1
		for len(data) > 0 {
			end := bytes.IndexByte(data, '\n')
			var l []byte
			if end < 0 {
				l, data = data, nil
			} else {
				l, data = data[:end], data[end+1:]
			}
			if q.Match(l) {
				out = append(out, Match{ix.Files[id], line, strings.TrimRight(string(l), "\r")})
			}
			line++
		}
	}
	return out, st, nil
}

// staleIDs lists indexed files whose size or mtime differs from index time.
// Their trigrams may be out of date, so they must be scanned regardless of
// what the posting lists say. Files created after indexing are not detected.
func (ix *Index) staleIDs() []uint32 {
	var out []uint32
	if len(ix.Meta) != len(ix.Files) {
		return nil
	}
	for i, p := range ix.Files {
		info, err := os.Stat(filepath.Join(ix.Root, p))
		if err != nil {
			continue // deleted: the read in Search skips it
		}
		if info.Size() != ix.Meta[i].Size || info.ModTime().UnixNano() != ix.Meta[i].ModTime {
			out = append(out, uint32(i))
		}
	}
	return out
}

func unionSorted(a, b []uint32) []uint32 {
	seen := make(map[uint32]struct{}, len(a)+len(b))
	var out []uint32
	for _, l := range [][]uint32{a, b} {
		for _, v := range l {
			if _, ok := seen[v]; !ok {
				seen[v] = struct{}{}
				out = append(out, v)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
