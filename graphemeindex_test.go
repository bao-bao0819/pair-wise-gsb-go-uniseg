package uniseg

import (
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"
)

// refBounds computes the grapheme cluster boundaries of s with the
// authoritative NewGraphemes iterator. The result always starts with 0 and
// ends with len(s).
func refBounds(s string) []int {
	bounds := []int{0}
	g := NewGraphemes(s)
	for g.Next() {
		_, to := g.Positions()
		bounds = append(bounds, to)
	}
	return bounds
}

// assertWellFormed checks the structural invariants of an index.
func assertWellFormed(t *testing.T, idx *GraphemeIndex) {
	t.Helper()
	if len(idx.bounds) < 1 || idx.bounds[0] != 0 || idx.bounds[len(idx.bounds)-1] != len(idx.text) {
		t.Fatalf("bounds must start at 0 and end at %d, got %v", len(idx.text), idx.bounds)
	}
	if len(idx.bounds) != len(idx.states) {
		t.Fatalf("bounds/states length mismatch: %d vs %d", len(idx.bounds), len(idx.states))
	}
	for i := 1; i < len(idx.bounds); i++ {
		if idx.bounds[i] <= idx.bounds[i-1] {
			t.Fatalf("bounds not strictly increasing: %v", idx.bounds)
		}
	}
	if !utf8.ValidString(idx.text) {
		t.Fatalf("index text is not valid UTF-8: %q", idx.text)
	}
}

// assertMatches compares the index against a full NewGraphemes segmentation
// and independently verifies that every stored checkpoint can resume the
// parser and reproduce that exact suffix.
func assertMatches(t *testing.T, idx *GraphemeIndex) {
	t.Helper()
	assertWellFormed(t, idx)

	want := refBounds(idx.text)
	if len(want) != len(idx.bounds) {
		t.Fatalf("%q: boundary count %d, want %d (%v vs %v)", idx.text, len(idx.bounds), len(want), idx.bounds, want)
	}
	for i := range want {
		if want[i] != idx.bounds[i] {
			t.Fatalf("%q: boundaries %v, want %v", idx.text, idx.bounds, want)
		}
	}

	// Every checkpoint must reproduce the suffix segmentation exactly.
	for i := 0; i < len(idx.bounds); i++ {
		pos, state := idx.bounds[i], idx.states[i]
		var got []int
		for pos < len(idx.text) {
			pos, state = graphemeStep(idx.text, pos, state)
			got = append(got, pos)
		}
		rest := idx.bounds[i+1:]
		if len(got) != len(rest) {
			t.Fatalf("%q: checkpoint %d resumes to %v, want %v", idx.text, idx.bounds[i], got, rest)
		}
		for j := range got {
			if got[j] != rest[j] {
				t.Fatalf("%q: checkpoint %d resumes to %v, want %v", idx.text, idx.bounds[i], got, rest)
			}
		}
	}
}

// assertRanges verifies that the affected ranges cover every changed
// boundary: the prefix is byte-identical and the suffix survives by shifting.
func assertRanges(t *testing.T, oldText string, oldBounds []int, oldLo, oldHi int,
	newText string, newBounds []int, newLo, newHi int) {
	t.Helper()
	delta := newHi - oldHi

	// Prefix: everything before oldLo/newLo is byte-identical.
	if oldLo != newLo || oldText[:oldLo] != newText[:newLo] {
		t.Fatalf("affected range does not start on the shared prefix: %d vs %d", oldLo, newLo)
	}
	for i := 0; i < len(oldBounds) && oldBounds[i] < oldLo; i++ {
		if i >= len(newBounds) || newBounds[i] != oldBounds[i] {
			t.Fatalf("prefix boundary changed outside affected range: old %v new %v (oldLo=%d)", oldBounds, newBounds, oldLo)
		}
	}

	// Suffix: every old boundary at or after oldHi survives shifted by delta.
	have := map[int]bool{}
	for _, b := range newBounds {
		have[b] = true
	}
	for _, b := range oldBounds {
		if b >= oldHi && !have[b+delta] {
			t.Fatalf("suffix boundary %d (shifted to %d) missing after update; old=%v new=%v range=[%d,%d)",
				b, b+delta, oldBounds, newBounds, oldLo, oldHi)
		}
	}
	if len(newText)-len(oldText) != delta {
		t.Fatalf("inconsistent shift delta")
	}
}

func TestNewGraphemeIndex(t *testing.T) {
	for _, tc := range testCases {
		idx, err := NewGraphemeIndex(tc.original)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tc.original, err)
		}
		assertMatches(t, idx)
		if idx.NumGraphemes() != len(tc.expected) {
			t.Fatalf("%q: NumGraphemes=%d want %d", tc.original, idx.NumGraphemes(), len(tc.expected))
		}
	}

	if _, err := NewGraphemeIndex("a\xffb"); err != ErrInvalidUTF8 {
		t.Fatalf("expected ErrInvalidUTF8, got %v", err)
	}

	empty, err := NewGraphemeIndex("")
	if err != nil {
		t.Fatal(err)
	}
	assertMatches(t, empty)
}

func TestReplaceInvalid(t *testing.T) {
	idx, err := NewGraphemeIndex("abcdef")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := append([]int(nil), idx.bounds...)

	// "é" is a 2-byte rune; offset 1 lands in its middle.
	multi := "a\u00e9z"
	mi, err := NewGraphemeIndex(multi)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name        string
		lo, hi      int
		replacement string
		idx         *GraphemeIndex
	}{
		{"lo negative", -1, 1, "x", idx},
		{"hi beyond end", 0, 7, "x", idx},
		{"hi less than lo", 3, 2, "x", idx},
		{"lo in rune", 1, 2, "x", mi},
		{"hi in rune", 1, 2, "x", mi},
		{"invalid replacement", 0, 1, "\xff", idx},
	}
	for _, tc := range cases {
		_, _, _, _, err := tc.idx.Replace(tc.lo, tc.hi, tc.replacement)
		if err == nil {
			t.Fatalf("%s: expected error, got nil", tc.name)
		}
	}

	// The ASCII index must be entirely untouched after all rejected edits.
	if idx.Text() != "abcdef" || len(idx.bounds) != len(snapshot) {
		t.Fatalf("index mutated by rejected edits: %q %v", idx.Text(), idx.bounds)
	}
	for i := range snapshot {
		if idx.bounds[i] != snapshot[i] {
			t.Fatalf("index mutated by rejected edits: %v", idx.bounds)
		}
	}

	// Edits are allowed inside a grapheme as long as they hit rune edges.
	if _, _, _, _, err := mi.Replace(1, 3, "X"); err != nil {
		t.Fatalf("rune-aligned edit inside cluster should be allowed: %v", err)
	}
}

func TestReplaceQueries(t *testing.T) {
	idx, err := NewGraphemeIndex("a\u0300bc") // clusters: a+(grave), b, c
	if err != nil {
		t.Fatal(err)
	}
	assertMatches(t, idx)

	if n := idx.NumGraphemes(); n != 3 {
		t.Fatalf("NumGraphemes=%d want 3", n)
	}
	if lo, hi := idx.GraphemeBounds(0); lo != 0 || hi != 3 {
		t.Fatalf("GraphemeBounds(0)=%d,%d want 0,3", lo, hi)
	}
	if lo, hi := idx.GraphemeBounds(2); lo != 4 || hi != 5 {
		t.Fatalf("GraphemeBounds(2)=%d,%d want 4,5", lo, hi)
	}
	// Clamping.
	if lo, hi := idx.GraphemeBounds(-9); lo != 0 || hi != 3 {
		t.Fatalf("GraphemeBounds(-9)=%d,%d want 0,3", lo, hi)
	}
	if lo, hi := idx.GraphemeBounds(99); lo != 4 || hi != 5 {
		t.Fatalf("GraphemeBounds(99)=%d,%d want 4,5", lo, hi)
	}

	// Byte offsets map to the containing cluster, including intra-cluster
	// bytes and the end sentinel.
	cases := map[int]int{1: 0, 2: 0, 3: 1, 4: 2, 5: 3, -4: 0}
	for off, want := range cases {
		if got := idx.GraphemeAt(off); got != want {
			t.Fatalf("GraphemeAt(%d)=%d want %d", off, got, want)
		}
	}

	if s := idx.Slice(0, 2); s != "a\u0300b" {
		t.Fatalf("Slice(0,2)=%q want %q", s, "a\u0300b")
	}
	if s := idx.Slice(1, 1); s != "" {
		t.Fatalf("Slice(1,1)=%q want empty", s)
	}
	// Reversed and out-of-range indices are normalized.
	if s := idx.Slice(9, 0); s != "a\u0300bc" {
		t.Fatalf("Slice(9,0)=%q want full", s)
	}
}

// runReplace performs an edit and validates both the segmentation and the
// reported affected ranges. It returns the number of bytes rescanned.
func runReplace(t *testing.T, idx *GraphemeIndex, lo, hi int, repl string) int {
	t.Helper()
	oldText := idx.Text()
	oldBounds := append([]int(nil), idx.bounds...)

	oldLo, oldHi, newLo, newHi, rescanned, err := idx.replace(lo, hi, repl)
	if err != nil {
		t.Fatalf("Replace(%d,%d,%q) on %q: %v", lo, hi, repl, oldText, err)
	}
	wantText := oldText[:lo] + repl + oldText[hi:]
	if idx.Text() != wantText {
		t.Fatalf("text=%q want %q", idx.Text(), wantText)
	}
	assertMatches(t, idx)
	assertRanges(t, oldText, oldBounds, oldLo, oldHi, idx.Text(), idx.bounds, newLo, newHi)
	return rescanned
}

func TestReplaceDeleteZWJ(t *testing.T) {
	// "a ZWJ b" is two clusters: [a ZWJ] and [b]. Deleting the ZWJ leaves
	// three single-rune clusters.
	idx, _ := NewGraphemeIndex("a\u200db")
	zwj := len("a")
	runReplace(t, idx, zwj, zwj+3, "")
	if idx.Text() != "ab" || idx.NumGraphemes() != 2 {
		t.Fatalf("after ZWJ deletion: %q clusters=%d", idx.Text(), idx.NumGraphemes())
	}

	// Deleting a ZWJ inside a long emoji sequence splits / re-clusters it.
	s := "x👩\u200d❤️y"
	idx2, _ := NewGraphemeIndex(s)
	before := idx2.NumGraphemes()
	// Locate the ZWJ rune and remove it.
	pos := -1
	for i := 0; i < len(s); {
		r, sz := utf8.DecodeRuneInString(s[i:])
		if r == 0x200d {
			pos = i
			break
		}
		i += sz
	}
	if pos < 0 {
		t.Fatal("ZWJ not found")
	}
	runReplace(t, idx2, pos, pos+3, "")
	if idx2.NumGraphemes() <= before {
		t.Fatalf("deleting ZWJ expected to change clustering, before=%d after=%d", before, idx2.NumGraphemes())
	}
}

func TestReplaceInsertCombining(t *testing.T) {
	// Inserting a combining mark after "a" merges it into the previous
	// cluster and shifts subsequent boundaries.
	idx, _ := NewGraphemeIndex("abcd")
	rescanned := runReplace(t, idx, 1, 1, "\u0300") // grave accent
	if idx.Text() != "a\u0300bcd" || idx.NumGraphemes() != 4 {
		t.Fatalf("after combining insert: %q clusters=%d", idx.Text(), idx.NumGraphemes())
	}
	if lo, hi := idx.GraphemeBounds(0); lo != 0 || hi != 3 {
		t.Fatalf("first cluster=%d,%d want 0,3", lo, hi)
	}
	// The tiny text offers little room for reuse; we only require that the
	// scan did not walk the whole string. The large-text reuse guarantee is
	// asserted separately in TestASCIIRescanReuse.
	if rescanned >= len("abcd")*2 {
		t.Fatalf("small edit rescanned %d bytes; unexpectedly large", rescanned)
	}
}

func TestReplaceCRLF(t *testing.T) {
	type edit struct {
		lo, hi int
		repl   string
	}
	cases := []struct {
		name string
		text string
		edit edit
	}{
		// Insert between CR and LF: the CRLF cluster must break apart.
		{"insert inside CRLF", "a\r\nb", edit{2, 2, "X"}},
		// Delete spanning the CR/LF boundary.
		{"delete across CRLF", "x\r\nyz", edit{1, 3, ""}},
		// Replace the LF with an LF (stable CRLF cluster).
		{"replace LF", "a\r\nb", edit{2, 3, "\n"}},
		// Edit starting right before a CRLF and consuming the CR.
		{"delete CR", "a\r\nb", edit{1, 2, ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx, err := NewGraphemeIndex(tc.text)
			if err != nil {
				t.Fatal(err)
			}
			runReplace(t, idx, tc.edit.lo, tc.edit.hi, tc.edit.repl)
		})
	}
}

func TestReplaceRIParity(t *testing.T) {
	ri := "🇩🇪" // one regional indicator pair is two RI runes (4 bytes each)

	// A pure run of RIs: inserting one RI flips pairing all the way to the
	// end. There is no resetting cluster, so this degenerates to a full
	// rescan and must still be exactly correct.
	long := strings.Repeat(ri, 5)
	idx, _ := NewGraphemeIndex(long)
	oldBounds := append([]int(nil), idx.bounds...)
	oldLo, oldHi, newLo, newHi, rescanned, err := idx.replace(4, 4, "🇩") // insert one RI rune
	if err != nil {
		t.Fatal(err)
	}
	assertMatches(t, idx)
	assertRanges(t, long, oldBounds, oldLo, oldHi, idx.Text(), idx.bounds, newLo, newHi)
	// With no resetting rune the parity never re-converges, so the scanner
	// must walk all the way to EOF: this is the correct full-scan fallback.
	if oldHi != len(long) || newHi != len(idx.Text()) {
		t.Fatalf("pure RI parity ripple should only converge at EOF, got oldHi=%d newHi=%d", oldHi, newHi)
	}
	if rescanned < len(long) {
		t.Fatalf("pure RI edit should rescan through EOF, rescanned=%d of %d", rescanned, len(long))
	}

	// Same long RI run but followed by a resetting cluster "x". After the
	// pairing ripple reaches "x" the state becomes grAny and the long stable
	// suffix after it is reused.
	withReset := long + "x" + "tail-unchanged-zone"
	idx2, _ := NewGraphemeIndex(withReset)
	total := len(withReset)
	tail := "tail-unchanged-zone"
	tailStart := total - len(tail)
	oldBounds2 := append([]int(nil), idx2.bounds...)
	oldLo2, oldHi2, newLo2, newHi2, rescanned2, err := idx2.replace(4, 4, "🇩")
	if err != nil {
		t.Fatal(err)
	}
	assertMatches(t, idx2)
	assertRanges(t, withReset, oldBounds2, oldLo2, oldHi2, idx2.Text(), idx2.bounds, newLo2, newHi2)
	// With a pre-boundary checkpoint, the scanner confirms convergence one
	// cluster after the resetting "x"; it must never traverse more than the
	// first cluster of the stable tail, so most of the tail is reused.
	if newHi2 > tailStart+len("tail") {
		t.Fatalf("rescan traversed the stable tail (newHi=%d, tail starts at %d); suffix not reused", newHi2, tailStart)
	}
	if idx2.Text()[len(idx2.Text())-len("tail-unchanged-zone"):] != "tail-unchanged-zone" {
		t.Fatalf("suffix not preserved: %q", idx2.Text())
	}
	t.Logf("RI parity ripple converged at newHi=%d; tail of %d bytes reused (total %d, rescanned %d)",
		newHi2, len(tail), total, rescanned2)

	// Inserting an RI at the very front also flips the whole run.
	idx3, _ := NewGraphemeIndex(long)
	runReplace(t, idx3, 0, 0, "🇩")
}

func TestASCIIRescanReuse(t *testing.T) {
	text := "The quick brown fox jumps over the lazy dog. " +
		"This second sentence is intentionally long and entirely ASCII " +
		"so that a tiny local edit only needs to rescan a couple of bytes."
	idx, _ := NewGraphemeIndex(text)

	// Insert one ASCII character near the start of a word.
	at := len("The quick ")
	ins := runReplace(t, idx, at, at, "Z")
	t.Logf("ASCII 1-byte insert rescanned %d bytes (text length %d)", ins, len(text))
	if ins > 8 {
		t.Fatalf("1-byte insert rescanned %d bytes, expected the stable prefix/suffix to be reused (≤8)", ins)
	}

	// Delete one byte.
	del := runReplace(t, idx, at, at+1, "")
	t.Logf("ASCII 1-byte delete rescanned %d bytes (text length %d)", del, len(text))
	if del > 8 {
		t.Fatalf("1-byte delete rescanned %d bytes, expected reuse (≤8)", del)
	}
}

// graphemeFuzzAtoms is a corpus that exercises all interesting boundary
// states: combining marks, ZWJ, emoji, RI runes, CR/LF, prepend, Hangul.
var graphemeFuzzAtoms = []string{
	"a", "b", " ", ".",
	"\r", "\n", "\t",
	"é", "🙂", "🇩", "🇪",
	"\u0300",      // combining grave (Extend)
	"\u200d",      // ZWJ
	"🏳", "\ufe0f", // white flag + variation selector
	"🌈",
	"ᄀ", "ᅡ", "ᆨ", // Hangul L, V, T
	"\u0600", // prepend
}

func TestReplaceRandomized(t *testing.T) {
	for _, seed := range []int64{1, 2, 7, 42, 99} {
		t.Run("", func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))

			for iter := 0; iter < 200; iter++ {
				// Build a random initial text.
				var s string
				for i := 0; i < 40; i++ {
					s += graphemeFuzzAtoms[rng.Intn(len(graphemeFuzzAtoms))]
				}
				idx, err := NewGraphemeIndex(s)
				if err != nil {
					t.Fatal(err)
				}
				assertMatches(t, idx)

				for step := 0; step < 60; step++ {
					old := s
					// Collect valid rune boundaries (plus the end).
					starts := []int{0}
					for i := 0; i < len(s); {
						_, sz := utf8.DecodeRuneInString(s[i:])
						i += sz
						starts = append(starts, i)
					}
					lo := starts[rng.Intn(len(starts))]
					hi := starts[rng.Intn(len(starts))]
					if lo > hi {
						lo, hi = hi, lo
					}
					var repl string
					n := rng.Intn(4)
					for i := 0; i < n; i++ {
						repl += graphemeFuzzAtoms[rng.Intn(len(graphemeFuzzAtoms))]
					}

					oldBounds := append([]int(nil), idx.bounds...)
					oldLo, oldHi, newLo, newHi, _, err := idx.replace(lo, hi, repl)
					if err != nil {
						t.Fatalf("seed %d iter %d step %d: valid edit rejected: %v", seed, iter, step, err)
					}
					s = old[:lo] + repl + old[hi:]
					if idx.Text() != s {
						t.Fatalf("seed %d iter %d step %d: text mismatch", seed, iter, step)
					}
					assertMatches(t, idx)
					assertRanges(t, old, oldBounds, oldLo, oldHi, idx.Text(), idx.bounds, newLo, newHi)
				}
			}
		})
	}
}
