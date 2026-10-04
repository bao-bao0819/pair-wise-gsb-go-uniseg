package uniseg

import (
	"reflect"
	"testing"
)

type idToken struct {
	from, to, class, reason int
}

func tokenize(s string) []idToken {
	it := NewIdentifierTokens(s)
	var out []idToken
	for it.Next() {
		from, to := it.Positions()
		out = append(out, idToken{from, to, it.Token().Class, it.Token().Reason})
	}
	return out
}

func tokenizeBytes(s string) []idToken {
	b := []byte(s)
	it := NewIdentifierTokensFromBytes(b)
	var out []idToken
	for it.Next() {
		from, to := it.Positions()
		out = append(out, idToken{from, to, it.Token().Class, it.Token().Reason})
	}
	return out
}

func segmentTexts(s string, toks []idToken) []string {
	var out []string
	for _, tk := range toks {
		out = append(out, s[tk.from:tk.to])
	}
	return out
}

// w builds an IdentifierWord token, p an IdentifierSeparator, o an
// IdentifierNonWord token.
func w(f, t, r int) idToken { return idToken{f, t, IdentifierWord, r} }
func p(f, t, r int) idToken { return idToken{f, t, IdentifierSeparator, r} }
func o(f, t, r int) idToken { return idToken{f, t, IdentifierNonWord, r} }

func TestIdentifierBasic(t *testing.T) {
	tests := []struct {
		input string
		want  []idToken
	}{
		{
			"HTTPServer_v2",
			[]idToken{
				w(0, 4, IdentifierStart),    // HTTP
				w(4, 10, IdentifierAcronym), // Server
				p(10, 11, IdentifierUnderscore),
				w(11, 12, IdentifierUnderscore), // v
				w(12, 13, IdentifierLetterDigit),
			},
		},
		{
			"XMLReader",
			[]idToken{
				w(0, 3, IdentifierStart),   // XML
				w(3, 9, IdentifierAcronym), // Reader
			},
		},
		{
			"utf8",
			[]idToken{
				w(0, 3, IdentifierStart),
				w(3, 4, IdentifierLetterDigit),
			},
		},
		{
			"v2",
			[]idToken{
				w(0, 1, IdentifierStart),
				w(1, 2, IdentifierLetterDigit),
			},
		},
		{
			"camelCase",
			[]idToken{
				w(0, 5, IdentifierStart),
				w(5, 9, IdentifierCamelCase),
			},
		},
		{
			"getHTTPResponse",
			[]idToken{
				w(0, 3, IdentifierStart),
				w(3, 7, IdentifierCamelCase),
				w(7, 15, IdentifierAcronym),
			},
		},
		{"ABC", []idToken{w(0, 3, IdentifierStart)}},
		{"AB", []idToken{w(0, 2, IdentifierStart)}},
		{"A", []idToken{w(0, 1, IdentifierStart)}},
	}
	for _, tt := range tests {
		got := tokenize(tt.input)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%q:\n got=%v\nwant=%v\ntexts=%v", tt.input, got, tt.want, segmentTexts(tt.input, got))
		}
	}
}

func TestIdentifierUnderscores(t *testing.T) {
	got := tokenize("a__b___c")
	want := []idToken{
		w(0, 1, IdentifierStart),
		p(1, 3, IdentifierUnderscore),
		w(3, 4, IdentifierUnderscore),
		p(4, 7, IdentifierUnderscore),
		w(7, 8, IdentifierUnderscore),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got=%v want=%v texts=%v", got, want, segmentTexts("a__b___c", got))
	}
}

func TestIdentifierGreekAndCJK(t *testing.T) {
	// Greek is cased and follows lower -> upper rules.
	got := tokenize("αβγΔ")
	want := []idToken{
		w(0, 6, IdentifierStart),
		w(6, 8, IdentifierCamelCase),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("αβγΔ got=%v", got)
	}

	// CJK (uncased) with digits and Latin.
	got = tokenize("中文123abc")
	want = []idToken{
		w(0, 6, IdentifierStart),
		w(6, 9, IdentifierLetterDigit),
		w(9, 12, IdentifierLetterDigit),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("中文123abc got=%v texts=%v", got, segmentTexts("中文123abc", got))
	}
}

func TestIdentifierCombiningMarks(t *testing.T) {
	// "e" + combining acute accent stays attached; lower -> upper still splits.
	s := "cafe\u0301Case"
	got := tokenize(s)
	want := []idToken{
		w(0, 6, IdentifierStart),
		w(6, 10, IdentifierCamelCase),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got=%v texts=%v", got, segmentTexts(s, got))
	}

	// Upper case letter with combining mark remains its own cluster and is
	// treated as upper case. "B" + diaeresis followed by lower case "c" is an
	// upper/lower pair (as in "Reader"), so no boundary is inserted.
	s2 := "AB\u0308c"
	got = tokenize(s2)
	texts := segmentTexts(s2, got)
	wantTexts := []string{"A", "B\u0308c"}
	if !reflect.DeepEqual(texts, wantTexts) {
		t.Errorf("got texts=%v want=%v", texts, wantTexts)
	}
}

func TestIdentifierEmoji(t *testing.T) {
	s := "a😀b"
	got := tokenize(s)
	want := []idToken{
		w(0, 1, IdentifierStart),
		o(1, 5, IdentifierClassChange),
		w(5, 6, IdentifierClassChange),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got=%v texts=%v", got, segmentTexts(s, got))
	}

	// An emoji separates two identifier regions; the emoji itself is a single
	// non-identifier segment while the adjacent letters remain words.
	s2 := "foo😀bar"
	got = tokenize(s2)
	want = []idToken{
		w(0, 3, IdentifierStart),
		o(3, 7, IdentifierClassChange),
		w(7, 10, IdentifierClassChange),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("foo😀bar got=%v texts=%v", got, segmentTexts(s2, got))
	}

	// A ZWJ emoji sequence (including its variation selector and combining
	// marks) stays one grapheme cluster and is a single non-identifier
	// segment; it does not get promoted to a letter by its attached marks.
	s3 := "x\U0001f3f3\ufe0f\u200d\U0001f308y"
	got = tokenize(s3)
	want = []idToken{
		w(0, 1, IdentifierStart),
		o(1, 15, IdentifierClassChange),
		w(15, 16, IdentifierClassChange),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("zwj emoji got=%v texts=%v", got, segmentTexts(s3, got))
	}
}

func TestIdentifierCoverage(t *testing.T) {
	cases := []string{
		"",
		" ",
		"HTTPServer_v2",
		"Hello, world! αβγΔ 中文123abc a😀b",
		"  spaced  ",
		"can't 3.14",
	}
	for _, s := range cases {
		toks := tokenize(s)
		if s == "" {
			if len(toks) != 0 {
				t.Errorf("empty input produced %v", toks)
			}
			continue
		}
		pos := 0
		for i, tk := range toks {
			if tk.from != pos {
				t.Errorf("%q: gap/overlap at token %d: %d != %d", s, i, tk.from, pos)
			}
			if tk.to <= tk.from {
				t.Errorf("%q: empty token %v", s, tk)
			}
			pos = tk.to
		}
		if pos != len(s) {
			t.Errorf("%q: coverage ended at %d, want %d", s, pos, len(s))
		}
	}
}

func TestIdentifierByteAndStringAgree(t *testing.T) {
	cases := []string{
		"HTTPServer_v2",
		"XMLReader",
		"中文123abc",
		"Hello, world! αβγΔ 🇩🇪🏳️‍🌈 x9",
		"a__b___c",
		"cafe\u0301Case A\u0308b foo😀bar",
	}
	for _, s := range cases {
		ts := tokenize(s)
		tb := tokenizeBytes(s)
		if !reflect.DeepEqual(ts, tb) {
			t.Errorf("byte/string mismatch for %q:\nstr=%v\nbyt=%v", s, ts, tb)
		}
	}
}

func TestIdentifierReset(t *testing.T) {
	it := NewIdentifierTokens("v2")
	first := tokenize("v2")
	var again []idToken
	it.Reset()
	for it.Next() {
		from, to := it.Positions()
		again = append(again, idToken{from, to, it.Token().Class, it.Token().Reason})
	}
	if !reflect.DeepEqual(again, first) {
		t.Errorf("after Reset got=%v want=%v", again, first)
	}
}
