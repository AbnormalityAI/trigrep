package trigrep

import (
	"regexp"
	"regexp/syntax"
)

// Query is a compiled search with the literals every match must contain.
type Query struct {
	re       *regexp.Regexp
	Required []string
}

// Match reports whether line matches.
func (q *Query) Match(line []byte) bool { return q.re.Match(line) }

// Compile parses pattern and extracts required literal substrings usable for
// trigram narrowing. Case-insensitive or alternation-only patterns yield none
// and fall back to scanning every file, which stays correct but slower.
func Compile(pattern string, literal bool) (*Query, error) {
	if literal {
		pattern = regexp.QuoteMeta(pattern)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	parsed, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil, err
	}
	return &Query{re: re, Required: required(parsed.Simplify())}, nil
}

// required returns literals that must appear in any match of r.
func required(r *syntax.Regexp) []string {
	switch r.Op {
	case syntax.OpLiteral:
		if r.Flags&syntax.FoldCase != 0 {
			return nil
		}
		if s := string(r.Rune); len(s) >= 3 {
			return []string{s}
		}
	case syntax.OpConcat:
		var out []string
		// adjacent literals fuse, e.g. foo(bar)baz is not fused, but
		// each piece is still required on its own.
		for _, sub := range r.Sub {
			out = append(out, required(sub)...)
		}
		return out
	case syntax.OpCapture, syntax.OpPlus:
		return required(r.Sub[0])
	case syntax.OpRepeat:
		if r.Min >= 1 {
			return required(r.Sub[0])
		}
	}
	return nil
}
