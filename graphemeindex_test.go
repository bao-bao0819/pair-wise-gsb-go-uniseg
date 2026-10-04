package uniseg

import (
	"math/rand"
	"strings"
	"testing"
)

// fullBounds returns all grapheme cluster boundary offsets of s, computed
// from scratch using the Graphemes class.
func fullBounds(s string) []int {
	bounds := []int{0}
	gr := NewGraphemes(s)
	for gr.Next() {
		_, to := gr.Positions()
		bounds = append(bounds, to)
	}
	return bounds
}

// equalInts reports whether a and b are equal.
func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// checkIndex verifies that the index boundaries match a full re-computation
// and that the byte-offset queries are consistent.
func checkIndex(t *testing.T, g *GraphemeIndex) {
	t.Helper()
	want := fullBounds(g.String())
	got := make([]int, g.GraphemeCount()+1)
	for i := range got {
		got[i] = g.GraphemeOffset(i)
	}
	if !equalInts(got, want) {
		t.Fatalf("boundaries mismatch for %q:\n got %v\nwant %v", g.String(), got, want)
	}
	// Check GraphemeAt for every byte offset.
	for offset := 0; offset <= g.Len(); offset++ {
		i := g.GraphemeAt(offset)
		if i < 0 || i > g.GraphemeCount() {
			t.Fatalf("GraphemeAt(%d) = %d, out of range", offset, i)
		}
		if offset == g.Len() {
			if i != g.GraphemeCount() {
				t.Fatalf("GraphemeAt(%d) = %d, want %d", offset, i, g.GraphemeCount())
			}
		} else if from, to := g.GraphemeRange(i); !(from <= offset && offset < to) {
			t.Fatalf("GraphemeAt(%d) = %d with range [%d, %d)", offset, i, from, to)
		}
	}
}

// checkAffected verifies the contract of the affected ranges returned by
// Replace: boundaries outside the returned ranges must be unchanged (prefix)
// or merely shifted by the edit's byte delta (suffix).
func checkAffected(t *testing.T, oldBounds, newBounds []int, oldFrom, oldTo, newFrom, newTo, delta int) {
	t.Helper()
	if oldFrom != newFrom {
		t.Fatalf("oldFrom = %d, newFrom = %d, want them equal", oldFrom, newFrom)
	}
	contains := func(bounds []int, x int) bool {
		for _, b := range bounds {
			if b == x {
				return true
			}
		}
		return false
	}
	for _, b := range oldBounds {
		if b <= oldFrom && !contains(newBounds, b) {
			t.Fatalf("old boundary %d (<= oldFrom %d) missing in new boundaries", b, oldFrom)
		}
		if b >= oldTo && !contains(newBounds, b+delta) {
			t.Fatalf("old boundary %d (>= oldTo %d) missing in new boundaries at %d", b, oldTo, b+delta)
		}
	}
	for _, b := range newBounds {
		if b <= newFrom && !contains(oldBounds, b) {
			t.Fatalf("new boundary %d (<= newFrom %d) missing in old boundaries", b, newFrom)
		}
		if b >= newTo && !contains(oldBounds, b-delta) {
			t.Fatalf("new boundary %d (>= newTo %d) missing in old boundaries at %d", b, newTo, b-delta)
		}
	}
}

// runeBoundaryOffsets returns all rune boundary offsets of s.
func runeBoundaryOffsets(s string) []int {
	var offsets []int
	for i := range s {
		offsets = append(offsets, i)
	}
	return append(offsets, len(s))
}

// TestGraphemeIndexRandomEdits applies a long sequence of random edits and
// compares the index against a full re-computation after every edit. The
// alphabet of text pieces includes combining marks, ZWJ, CR/LF, Regional
// Indicators, emoji, Hangul jamo, and prepend characters, so edits naturally
// cover deleting ZWJ, inserting combining marks, splitting CRLF pairs, and
// changing Regional Indicator parity.
func TestGraphemeIndexRandomEdits(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	pieces := []string{
		"a", "b", "z", " ", ".", "\t",
		"\r", "\n", "\r\n",
		"́", "̀", // combining marks
		"‍",      // ZWJ
		"🇦", "🇧", // Regional Indicators
		"😀", "🧑", "️", // emoji + VS16
		"ᄀ", "ᅡ", "ᆨ", "가", // Hangul
		"؀", "्", // prepend, spacing mark
		"é", "ツ",
	}
	randomPiece := func() string { return pieces[rng.Intn(len(pieces))] }
	randomText := func(n int) string {
		var sb strings.Builder
		for i := 0; i < n; i++ {
			sb.WriteString(randomPiece())
		}
		return sb.String()
	}

	g, err := NewGraphemeIndex(randomText(20))
	if err != nil {
		t.Fatal(err)
	}
	for iter := 0; iter < 2000; iter++ {
		text := g.String()
		oldBounds := fullBounds(text)

		offsets := runeBoundaryOffsets(text)
		from := offsets[rng.Intn(len(offsets))]
		to := offsets[rng.Intn(len(offsets))]
		if from > to {
			from, to = to, from
		}
		repl := randomText(rng.Intn(4))

		oldFrom, oldTo, newFrom, newTo, err := g.Replace(from, to, repl)
		if err != nil {
			t.Fatalf("iteration %d: Replace(%d, %d, %q) failed: %v", iter, from, to, repl, err)
		}
		checkIndex(t, g)
		checkAffected(t, oldBounds, fullBounds(g.String()), oldFrom, oldTo, newFrom, newTo, len(repl)-(to-from))
	}
}

// TestGraphemeIndexTargetedEdits covers specific tricky edits.
func TestGraphemeIndexTargetedEdits(t *testing.T) {
	cases := []struct {
		name     string
		initial  string
		from, to int
		repl     string
	}{
		{"delete ZWJ splits emoji sequence", "x👩‍👩y", 5, 8, ""},
		{"insert ZWJ joins emoji sequence", "x👩👩y", 5, 5, "‍"},
		{"insert combining mark merges clusters", "xay", 2, 2, "́"},
		{"delete combining mark splits cluster", "xáy", 2, 4, ""},
		{"delete LF of CRLF", "a\r\nb", 2, 3, ""},
		{"delete CR of CRLF", "a\r\nb", 1, 2, ""},
		{"insert LF after CR creates CRLF", "a\rb", 2, 2, "\n"},
		{"insert CR before LF creates CRLF", "a\nb", 1, 1, "\r"},
		{"replace across CRLF boundary", "a\r\nb", 1, 3, "c"},
		{"insert RI flips parity", "🇦🇦🇦🇦", 0, 0, "🇦"},
		{"delete RI flips parity", "🇦🇦🇦🇦🇦", 0, 4, ""},
		{"insert RI in the middle", "a🇦🇦🇦🇦b", 5, 5, "🇦"},
		{"replace inside grapheme cluster", "👩‍👩", 4, 7, "x"},
		{"append at end", "abc", 3, 3, "́"},
		{"delete everything", "áb🇦🇦", 0, 8, ""},
		{"insert into empty text", "", 0, 0, "á"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g, err := NewGraphemeIndex(tc.initial)
			if err != nil {
				t.Fatal(err)
			}
			oldBounds := fullBounds(tc.initial)
			oldFrom, oldTo, newFrom, newTo, err := g.Replace(tc.from, tc.to, tc.repl)
			if err != nil {
				t.Fatalf("Replace failed: %v", err)
			}
			checkIndex(t, g)
			checkAffected(t, oldBounds, fullBounds(g.String()), oldFrom, oldTo, newFrom, newTo,
				len(tc.repl)-(tc.to-tc.from))
		})
	}
}

// TestGraphemeIndexInvalidEdits ensures invalid edits are rejected as a whole
// and leave the index unchanged.
func TestGraphemeIndexInvalidEdits(t *testing.T) {
	initial := "áb🇦🇦"
	g, err := NewGraphemeIndex(initial)
	if err != nil {
		t.Fatal(err)
	}
	before := fullBounds(initial)

	invalid := []struct {
		from, to int
		repl     string
	}{
		{-1, 0, ""},               // from out of range
		{0, len(initial) + 1, ""}, // to out of range
		{3, 1, ""},                // from > to
		{2, 3, ""},                // from not on rune boundary (inside ́)
		{0, 2, ""},                // to not on rune boundary (inside ́)
		{5, 6, ""},                // inside Regional Indicator
		{0, 0, "\xff"},            // invalid UTF-8 replacement
		{0, 1, "a\x80b"},          // invalid UTF-8 replacement
	}
	for i, tc := range invalid {
		if _, _, _, _, err := g.Replace(tc.from, tc.to, tc.repl); err == nil {
			t.Fatalf("case %d: Replace(%d, %d, %q) succeeded, want error", i, tc.from, tc.to, tc.repl)
		}
		if g.String() != initial {
			t.Fatalf("case %d: text changed to %q after rejected edit", i, g.String())
		}
		got := make([]int, g.GraphemeCount()+1)
		for j := range got {
			got[j] = g.GraphemeOffset(j)
		}
		if !equalInts(got, before) {
			t.Fatalf("case %d: boundaries changed after rejected edit", i)
		}
	}

	// The constructor rejects invalid UTF-8.
	if _, err := NewGraphemeIndex("a\xffb"); err != ErrInvalidUTF8 {
		t.Fatalf("NewGraphemeIndex with invalid UTF-8: got %v, want ErrInvalidUTF8", err)
	}
}

// TestGraphemeIndexRescanReuse verifies that a small ASCII edit in a large
// text only re-scans a small number of bytes, i.e. the stable prefix and
// suffix are reused.
func TestGraphemeIndexRescanReuse(t *testing.T) {
	text := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 1000) // 45,000 bytes
	g, err := NewGraphemeIndex(text)
	if err != nil {
		t.Fatal(err)
	}
	if g.RescannedBytes() != len(text) {
		t.Fatalf("construction re-scanned %d bytes, want %d", g.RescannedBytes(), len(text))
	}

	// A small edit in the middle.
	mid := len(text) / 2
	oldFrom, oldTo, newFrom, newTo, err := g.Replace(mid, mid+1, "X")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("small ASCII edit in %d-byte text: re-scanned %d bytes, affected old [%d, %d), new [%d, %d)",
		len(text), g.RescannedBytes(), oldFrom, oldTo, newFrom, newTo)
	if g.RescannedBytes() > 100 {
		t.Fatalf("re-scanned %d bytes for a 1-byte edit, want prefix/suffix reuse", g.RescannedBytes())
	}
	checkIndex(t, g)
}

// TestGraphemeIndexRIParityFullRescan verifies that inserting a Regional
// Indicator in front of a long run of Regional Indicators correctly
// re-pairs the entire run (degrading to a full scan) and still produces the
// correct boundaries.
func TestGraphemeIndexRIParityFullRescan(t *testing.T) {
	text := strings.Repeat("🇦", 1000) // 4,000 bytes, 500 flag clusters
	g, err := NewGraphemeIndex(text)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := g.Replace(0, 0, "🇦"); err != nil {
		t.Fatal(err)
	}
	checkIndex(t, g)
	if g.GraphemeCount() != 501 {
		t.Fatalf("got %d grapheme clusters, want 501", g.GraphemeCount())
	}
	// The parity of the whole run changed, so (almost) the whole text must
	// have been re-scanned.
	if g.RescannedBytes() < len(text) {
		t.Fatalf("re-scanned %d bytes, want at least %d (full run)", g.RescannedBytes(), len(text))
	}
}
