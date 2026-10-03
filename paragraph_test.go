package uniseg

import (
	"math/rand"
	"strings"
	"testing"
)

// bruteForceRaggedness enumerates every legal partition of the fragments and
// returns the minimum raggedness and the lexicographically smallest sequence
// of break fragment indices achieving it. It is used to verify
// optimizeRaggedness on small inputs.
func bruteForceRaggedness(fragments []breakFragment, maxWidth int) (bestBreaks []int, bestCost int) {
	m := len(fragments)
	bestCost = -1

	// legal reports whether fragments[i:j] may occupy a line together.
	legal := func(i, j int) bool {
		if j-i == 1 {
			return true // A single fragment always occupies its line.
		}
		width := 0
		for k := i; k < j; k++ {
			width += fragments[k].width
		}
		return width <= maxWidth
	}

	lineCost := func(i, j int) int {
		width := 0
		for k := i; k < j; k++ {
			width += fragments[k].width
		}
		slack := maxWidth - width
		return slack * slack
	}

	var rec func(start int, breaks []int, cost int)
	rec = func(start int, breaks []int, cost int) {
		if start == m {
			// The final line is free, so cost already excludes it.
			if bestCost == -1 || cost < bestCost ||
				(cost == bestCost && lexBreaksLess(breaks, bestBreaks)) {
				bestCost = cost
				bestBreaks = append([]int(nil), breaks...)
			}
			return
		}
		for end := start + 1; end <= m; end++ {
			if !legal(start, end) {
				if end-start >= 2 {
					break
				}
				continue
			}
			var nextCost int
			if end < m {
				nextCost = cost + lineCost(start, end)
			} else {
				nextCost = cost // Final line is free.
			}
			if end < m {
				rec(end, append(breaks, end), nextCost)
			} else {
				rec(end, breaks, nextCost)
			}
		}
	}
	rec(0, nil, 0)
	return bestBreaks, bestCost
}

func lexBreaksLess(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func makeFragments(widths []int) []breakFragment {
	offset := 0
	fragments := make([]breakFragment, len(widths))
	for i, w := range widths {
		fragments[i] = breakFragment{offset: offset, width: w}
		offset += w // Offsets only need to be strictly increasing here.
	}
	return fragments
}

// TestOptimizeRaggednessExhaustive checks the dynamic program against a full
// enumeration of all legal partitions for short inputs, including the
// lexicographical tie-break and overflowing fragments.
func TestOptimizeRaggednessExhaustive(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 20000; trial++ {
		m := 1 + rng.Intn(8)
		widths := make([]int, m)
		for i := range widths {
			widths[i] = 1 + rng.Intn(7) // Includes fragments wider than W.
		}
		maxWidth := 1 + rng.Intn(9)
		fragments := makeFragments(widths)

		wantBreaks, wantCost := bruteForceRaggedness(fragments, maxWidth)
		gotBreaks, gotCost := optimizeRaggedness(fragments, maxWidth)

		if gotCost != wantCost {
			t.Fatalf("widths=%v W=%d: cost=%d, want %d", widths, maxWidth, gotCost, wantCost)
		}
		if len(gotBreaks) != len(wantBreaks) {
			t.Fatalf("widths=%v W=%d: breaks=%v, want %v (cost %d)",
				widths, maxWidth, gotBreaks, wantBreaks, gotCost)
		}
		for i := range gotBreaks {
			if gotBreaks[i] != wantBreaks[i] {
				t.Fatalf("widths=%v W=%d: breaks=%v, want %v (cost %d)",
					widths, maxWidth, gotBreaks, wantBreaks, gotCost)
			}
		}
	}
}

// TestOptimizeRaggednessFixed covers specific scenarios: a case where greedy
// packing is strictly worse, and a tie resolved by the lexicographically
// smaller break sequence.
func TestOptimizeRaggednessFixed(t *testing.T) {
	// Widths [1 2 1 3] at W=3: breaks [1 3] and [2 3] both cost 4. The
	// lexicographically smaller break sequence wins.
	tied := makeFragments([]int{1, 2, 1, 3})
	breaks, cost := optimizeRaggedness(tied, 3)
	if cost != 4 {
		t.Fatalf("tied cost=%d, want 4", cost)
	}
	want := []int{1, 3} // Lexicographically smaller than [2 3].
	if len(breaks) != len(want) {
		t.Fatalf("tied breaks=%v, want %v", breaks, want)
	}
	for i := range want {
		if breaks[i] != want[i] {
			t.Fatalf("tied breaks=%v, want %v", breaks, want)
		}
	}

	// Exact fits prefer no break (the shorter sequence is lex-smaller).
	exact := makeFragments([]int{2, 2, 2})
	breaks, cost = optimizeRaggedness(exact, 6)
	if cost != 0 || len(breaks) != 0 {
		t.Fatalf("exact: breaks=%v cost=%d, want none/0", breaks, cost)
	}

	// Overflow: a single over-wide fragment on its own line carries the
	// squared excess, and multi-fragment lines that would overflow are illegal.
	over := makeFragments([]int{3, 12, 3})
	breaks, cost = optimizeRaggedness(over, 10)
	if len(breaks) != 2 || breaks[0] != 1 || breaks[1] != 2 {
		t.Fatalf("overflow breaks=%v, want [1 2]", breaks)
	}
	// Line 0 width 3 -> 49, line 1 width 12 -> 4, final line free.
	if cost != 53 {
		t.Fatalf("overflow cost=%d, want 53", cost)
	}
}

// TestLayoutSimple verifies end-to-end ranges, widths and reconstruction.
func TestLayoutSimple(t *testing.T) {
	str := "It is my house today!"
	// Words with trailing spaces: It(3) is(3) my(3) house(6) today!(6)
	layout := LayoutText(str, 10)

	// Ranges cover the entire input exactly.
	pos := 0
	for _, line := range layout.Lines {
		if line.Start != pos {
			t.Fatalf("line start=%d, want %d", line.Start, pos)
		}
		pos = line.End
	}
	if pos != len(str) {
		t.Fatalf("lines cover %d bytes, want %d", pos, len(str))
	}

	// Optimal: "It is " + "my house " on the first two lines (3+3=6, 3+6=9),
	// which beats the greedy packing "It is my " (9) + "house " (5).
	if got := layout.Apply(str); got != "It is \nmy house \ntoday!" {
		t.Fatalf("Apply = %q", got)
	}

	// Soft break positions are the starts of "my" and "today!".
	wantBreaks := []int{6, 15}
	if len(layout.SoftBreakPositions) != len(wantBreaks) {
		t.Fatalf("soft breaks=%v, want %v", layout.SoftBreakPositions, wantBreaks)
	}
	for i := range wantBreaks {
		if layout.SoftBreakPositions[i] != wantBreaks[i] {
			t.Fatalf("soft breaks=%v, want %v", layout.SoftBreakPositions, wantBreaks)
		}
	}
	if layout.Raggedness != 17 {
		t.Fatalf("raggedness=%d, want 17", layout.Raggedness)
	}
}

// TestLayoutHardBreaks covers LF and CRLF which must remain a single hard
// line break without any duplicate inserted newline.
func TestLayoutHardBreaks(t *testing.T) {
	str := "ab\r\ncd\nef"
	layout := LayoutText(str, 100)
	if len(layout.Lines) != 3 {
		t.Fatalf("got %d lines: %+v", len(layout.Lines), layout.Lines)
	}
	wantRanges := [][2]int{{0, 4}, {4, 7}, {7, 9}}
	for i, line := range layout.Lines {
		if line.Start != wantRanges[i][0] || line.End != wantRanges[i][1] {
			t.Fatalf("line %d range=%d:%d, want %d:%d",
				i, line.Start, line.End, wantRanges[i][0], wantRanges[i][1])
		}
		if i < 2 && !line.HardBreak {
			t.Fatalf("line %d should be a hard break", i)
		}
	}
	if layout.Lines[2].HardBreak {
		t.Fatal("last line must not be a hard break")
	}
	if len(layout.SoftBreakPositions) != 0 {
		t.Fatalf("unexpected soft breaks %v", layout.SoftBreakPositions)
	}
	if got := layout.Apply(str); got != str {
		t.Fatalf("Apply changed input: %q", got)
	}

	// An empty middle paragraph and a trailing newline.
	empty := LayoutText("a\n\nb\n", 100)
	if len(empty.Lines) != 3 {
		t.Fatalf("got %d lines: %+v", len(empty.Lines), empty.Lines)
	}
	// The middle empty paragraph is the single '\n' that terminates it:
	// range [2,3) with no text width.
	if empty.Lines[1].Start != 2 || empty.Lines[1].End != 3 || empty.Lines[1].Width != 0 {
		t.Fatalf("middle paragraph wrong: %+v", empty.Lines[1])
	}
	if empty.Apply("a\n\nb\n") != "a\n\nb\n" {
		t.Fatal("input with empty paragraphs must be preserved")
	}
}

// TestLayoutNoBreakOpportunities verifies that NBSP, Word Joiner and ZWJ
// clusters are never split, that CJK breaks between ideographs, and that an
// unbreakable over-wide fragment is reported as overflow.
func TestLayoutNoBreakOpportunities(t *testing.T) {
	// "a\u00a0b c": the NBSP group is one fragment; only the space breaks.
	layout := LayoutText("a\u00a0b c", 1)
	if len(layout.Lines) != 2 {
		t.Fatalf("NBSP: got %d lines: %+v", len(layout.Lines), layout.Lines)
	}
	if got := layout.Apply("a\u00a0b c"); got != "a\u00a0b \nc" {
		t.Fatalf("NBSP Apply = %q", got)
	}

	// Word Joiner keeps "a" and "b" together.
	wj := LayoutText("a\u2060b c", 1)
	if wj.Apply("a\u2060b c") != "a\u2060b \nc" {
		t.Fatalf("WJ Apply = %q", wj.Apply("a\u2060b c"))
	}

	// A ZWJ cluster stays atomic and overflows on its own line.
	zwj := LayoutText("x👨\u200d👩 y", 1)
	var overflow int
	for _, line := range zwj.Lines {
		if line.Overflow {
			overflow++
		}
	}
	if overflow == 0 {
		t.Fatal("expected an overflow line for the ZWJ cluster")
	}

	// CJK offers breaks between every ideograph.
	cjk := LayoutText("中文", 2)
	if cjk.Apply("中文") != "中\n文" {
		t.Fatalf("CJK Apply = %q", cjk.Apply("中文"))
	}
}

// TestLayoutPreservesText ensures no trimming or normalization happens and
// that the line ranges always reconstruct the input.
func TestLayoutPreservesText(t *testing.T) {
	inputs := []string{
		"",
		" ",
		"   ",
		"hello   world",
		"trailing spaces   ",
		"\n\n\n",
		"line one\r\nline two\r\n",
		"混合 mixed 文本\tTab",
	}
	for _, str := range inputs {
		layout := LayoutText(str, 5)
		var rebuilt []byte
		prev := 0
		for _, pos := range layout.SoftBreakPositions {
			rebuilt = append(rebuilt, str[prev:pos]...)
			rebuilt = append(rebuilt, '\n')
			prev = pos
		}
		rebuilt = append(rebuilt, str[prev:]...)
		if string(rebuilt) != layout.Apply(str) {
			t.Fatalf("manual reconstruction mismatch for %q", str)
		}

		// Ranges are contiguous and cover the whole string.
		pos := 0
		for _, line := range layout.Lines {
			if line.Start != pos {
				t.Fatalf("%q: non-contiguous line at %d (want %d)", str, line.Start, pos)
			}
			pos = line.End
		}
		if pos != len(str) {
			t.Fatalf("%q: ranges cover %d of %d bytes", str, pos, len(str))
		}
	}
}

// TestLayoutOverflowAndEdgeCases covers over-wide unbreakable fragments,
// zero maximum width, empty input, and a single hard line break.
func TestLayoutOverflowAndEdgeCases(t *testing.T) {
	// A 20-cell word plus trailing space is one fragment; it overflows.
	long := LayoutText("supercalifragilistic short", 8)
	if len(long.Lines) != 2 {
		t.Fatalf("got %d lines: %+v", len(long.Lines), long.Lines)
	}
	if !long.Lines[0].Overflow || long.Lines[0].Width != 21 {
		t.Fatalf("first line should overflow at width 21: %+v", long.Lines[0])
	}
	// (21-8)^2 = 169 for the non-final line.
	if long.Raggedness != 169 {
		t.Fatalf("raggedness=%d, want 169", long.Raggedness)
	}

	// Every fragment overflows when maxWidth is 0, but nothing is split.
	zero := LayoutText("ab cd", 0)
	if zero.Apply("ab cd") != "ab \ncd" {
		t.Fatalf("W=0 Apply = %q", zero.Apply("ab cd"))
	}
	for _, line := range zero.Lines {
		if !line.Overflow {
			t.Fatalf("line %q must overflow at W=0", "")
		}
	}

	if got := LayoutText("", 10); len(got.Lines) != 0 || got.Raggedness != 0 {
		t.Fatalf("empty input: %+v", got)
	}

	singleNL := LayoutText("\n", 10)
	if len(singleNL.Lines) != 1 || !singleNL.Lines[0].HardBreak {
		t.Fatalf("single newline: %+v", singleNL.Lines)
	}
	if singleNL.Apply("\n") != "\n" {
		t.Fatal("single newline must not gain a duplicate break")
	}
}

func BenchmarkLayoutText(b *testing.B) {
	text := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 50)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		LayoutText(text, 80)
	}
}

// FuzzLayoutTextRoundTrip verifies invariants for arbitrary valid UTF-8-ish
// inputs: line ranges are contiguous and cover the input exactly, Apply
// reproduces soft breaks, and soft breaks never duplicate hard breaks.
func FuzzLayoutTextRoundTrip(f *testing.F) {
	f.Add("hello world", 8)
	f.Add("a\r\nb\u00a0c\u200bd 中文\u200bx", 5)
	f.Add("", 3)
	f.Add("\n\n", 1)
	f.Fuzz(func(t *testing.T, str string, maxWidth int) {
		if maxWidth < 0 || maxWidth > 40 {
			return
		}
		layout := LayoutText(str, maxWidth)

		var rebuilt []byte
		prev := 0
		for _, p := range layout.SoftBreakPositions {
			rebuilt = append(rebuilt, str[prev:p]...)
			rebuilt = append(rebuilt, '\n')
			prev = p
		}
		rebuilt = append(rebuilt, str[prev:]...)
		if layout.Apply(str) != string(rebuilt) {
			t.Fatalf("Apply mismatch for %q", str)
		}

		pos := 0
		for _, line := range layout.Lines {
			if line.Start != pos {
				t.Fatalf("non-contiguous lines for %q: %+v", str, layout.Lines)
			}
			pos = line.End
			if line.HardBreak && (line.End == 0 || !endsWithHardBreak(str[:line.End])) {
				t.Fatalf("HardBreak line without trailing break: %+v", line)
			}
		}
		if pos != len(str) {
			t.Fatalf("lines cover %d of %d bytes for %q", pos, len(str), str)
		}

		prevPos := -1
		for _, sb := range layout.SoftBreakPositions {
			if sb <= prevPos || sb <= 0 || sb > len(str) {
				t.Fatalf("soft breaks not strictly increasing: %v", layout.SoftBreakPositions)
			}
			if str[sb-1] == '\n' || str[sb-1] == '\r' {
				t.Fatalf("soft break duplicates a hard break at %d for %q", sb, str)
			}
			prevPos = sb
		}
	})
}

func endsWithHardBreak(s string) bool {
	return HasTrailingLineBreakInString(s)
}
