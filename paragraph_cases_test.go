package uniseg

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// greedyLayout wraps one paragraph by always taking as many atoms as fit.
func greedyAtoms(units []layoutUnit, maxWidth int) (cost int64, breaks []int) {
	type atom struct {
		width   int
		lastEnd int
	}
	var atoms []atom
	for i := 0; i < len(units); {
		j := i + 1
		for j < len(units) && !units[j-1].canBreak {
			j++
		}
		w := 0
		for u := i; u < j; u++ {
			w += units[u].width
		}
		atoms = append(atoms, atom{width: w, lastEnd: units[j-1].end})
		i = j
	}

	start := 0
	for start < len(atoms) {
		end := start + 1
		width := atoms[start].width
		for end < len(atoms) && width+atoms[end].width <= maxWidth {
			width += atoms[end].width
			end++
		}
		if end < len(atoms) {
			gap := int64(maxWidth - width)
			cost += gap * gap
			breaks = append(breaks, atoms[end-1].lastEnd)
		}
		start = end
	}
	return
}

// TestLayoutGreedyWorse demonstrates a paragraph where line-by-line greedy
// filling is strictly worse than the global optimum.
//
// With maxWidth 10, the atom widths (a break opportunity follows each space)
// are:
//
//	7, 2, 4, 8, 6, 7
//
// Greedy fills the first line with 7+2=9:
//
//	[7,2] [4] [8] [6] [7] -> 1^2 + 6^2 + 2^2 + 4^2 = 57
//
// The optimum takes an earlier break:
//
//	[7] [2,4] [8] [6] [7] -> 3^2 + 4^2 + 2^2 + 4^2 = 45
func TestLayoutGreedyWorse(t *testing.T) {
	const str = "omaccp g fef klomkmp igbpa dbhchcc"
	const maxWidth = 10

	units := parseForLayout(t, str)
	greedyCost, _ := greedyAtoms(units, maxWidth)

	l := LayoutTextString(str, maxWidth)
	wantLines := []string{"omaccp ", "g fef ", "klomkmp ", "igbpa ", "dbhchcc"}
	gotLines := linesOf(str, l)
	if strings.Join(gotLines, "|") != strings.Join(wantLines, "|") {
		t.Fatalf("optimal lines = %v, want %v", gotLines, wantLines)
	}

	optCost := int64(0)
	for i := 0; i < len(l.Paragraphs[0].Lines)-1; i++ {
		ln := l.Paragraphs[0].Lines[i]
		gap := int64(maxWidth - ln.Width)
		optCost += gap * gap
	}
	if optCost != 45 || greedyCost != 57 || !(optCost < greedyCost) {
		t.Fatalf("costs: opt=%d greedy=%d", optCost, greedyCost)
	}
}

func linesOf(str string, l TextLayout) []string {
	var out []string
	for _, p := range l.Paragraphs {
		for _, ln := range p.Lines {
			out = append(out, str[ln.From:ln.To])
		}
	}
	return out
}

func TestLayoutCRLFIsSingleHardBreak(t *testing.T) {
	str := "a\r\nb\r\n"
	l := LayoutTextString(str, 10)
	if len(l.Paragraphs) != 2 {
		t.Fatalf("got %d paragraphs, want 2", len(l.Paragraphs))
	}
	if l.Paragraphs[0].HardBreakLen != 2 || l.Paragraphs[1].HardBreakLen != 2 {
		t.Fatalf("hard break lengths = %d,%d, want 2,2",
			l.Paragraphs[0].HardBreakLen, l.Paragraphs[1].HardBreakLen)
	}
	if len(l.SoftBreakPositions) != 0 {
		t.Fatalf("inserted breaks = %v, want none", l.SoftBreakPositions)
	}
	if got := linesOf(str, l); strings.Join(got, "|") != "a|b" {
		t.Fatalf("lines = %v", got)
	}
	if recon, want := reconstructText(str, l), insertAll(str, l.SoftBreakPositions); recon != want {
		t.Fatalf("reconstruction = %q, want %q", recon, want)
	}
}

func TestLayoutHardBreakVariants(t *testing.T) {
	cases := []struct {
		str       string
		paragraph int
		hardLen   int
	}{
		{"\r", 1, 1},
		{"a\rb", 2, 1},
		{"a\r\nb", 2, 2},
		{"\r\n", 1, 2},
		{"\r\n\r\n", 2, 2},
		{"x\u0085y", 2, 2}, // NEL
		{"x\u000by", 2, 1}, // VT (BK)
		{"x\u000cy", 2, 1}, // FF (BK)
	}
	for _, tc := range cases {
		l := LayoutTextString(tc.str, 80)
		if len(l.Paragraphs) != tc.paragraph {
			t.Fatalf("%q: got %d paragraphs, want %d",
				tc.str, len(l.Paragraphs), tc.paragraph)
		}
		if l.Paragraphs[0].HardBreakLen != tc.hardLen {
			t.Fatalf("%q: first paragraph hardLen = %d, want %d",
				tc.str, l.Paragraphs[0].HardBreakLen, tc.hardLen)
		}
		if got, want := reconstructText(tc.str, l), insertAll(tc.str, l.SoftBreakPositions); got != want {
			t.Fatalf("%q reconstruction = %q, want %q", tc.str, got, want)
		}
	}
}

func TestLayoutParagraphsIndependent(t *testing.T) {
	str := "aaa bbbbbbbb\ncc dddddddd"
	l := LayoutTextString(str, 5)
	if len(l.Paragraphs) != 2 {
		t.Fatalf("got %d paragraphs, want 2", len(l.Paragraphs))
	}
	want := []string{"aaa ", "bbbbbbbb", "cc ", "dddddddd"}
	if got := linesOf(str, l); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lines = %v, want %v", got, want)
	}
	overflow := []bool{false, true, false, true}
	i := 0
	for _, p := range l.Paragraphs {
		for _, ln := range p.Lines {
			if ln.Overflow != overflow[i] {
				t.Fatalf("line %d overflow = %v, want %v", i, ln.Overflow, overflow[i])
			}
			i++
		}
	}
	if recon, want := reconstructText(str, l), insertAll(str, l.SoftBreakPositions); recon != want {
		t.Fatalf("reconstruction = %q, want %q", recon, want)
	}
}

func TestLayoutOverflowNotSplitIntoRunes(t *testing.T) {
	str := "bbbbbbbb"
	l := LayoutTextString(str, 5)
	lines := l.Paragraphs[0].Lines
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if !lines[0].Overflow || lines[0].Width != 8 {
		t.Fatalf("line = %+v", lines[0])
	}

	// A wide grapheme cluster (emoji, width 2) that cannot fit at width 1
	// occupies a line by itself and is not broken apart.
	str = "🏳️‍🌈"
	l = LayoutTextString(str, 1)
	lines = l.Paragraphs[0].Lines
	if len(lines) != 1 || !lines[0].Overflow || lines[0].Width != 2 {
		t.Fatalf("emoji line = %+v", lines)
	}

	// Width-2 CJK at maxWidth 1 likewise overflows as one intact cluster.
	str = "中文"
	l = LayoutTextString(str, 1)
	lines = l.Paragraphs[0].Lines
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	for _, ln := range lines {
		if !ln.Overflow || ln.Width != 2 {
			t.Fatalf("cjk overflow line = %+v", ln)
		}
	}
}

func TestLayoutNBSPWordJoinerZWJ(t *testing.T) {
	// NBSP glues "a" and "b"; Word Joiner glues "c" and "d". A space is a
	// regular break. ZWJ emoji stays one cluster.
	str := "a\u00a0b c\u2060d e"
	l := LayoutTextString(str, 4)
	want := []string{"a\u00a0b ", "c\u2060d e"}
	if got := linesOf(str, l); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lines = %v, want %v", got, want)
	}

	// The WJ-glued run can be forced onto an overflow line but is not split.
	l = LayoutTextString(str, 3)
	var sawWJ bool
	for _, p := range l.Paragraphs {
		for _, ln := range p.Lines {
			seg := str[ln.From:ln.To]
			if strings.Contains(seg, "\u2060") {
				sawWJ = true
				if !strings.Contains(seg, "c\u2060d") {
					t.Fatalf("WJ run split: %q", seg)
				}
			}
		}
	}
	if !sawWJ {
		t.Fatal("WJ run not found in layout")
	}

	str = "x 🏳️‍🌈"
	l = LayoutTextString(str, 2)
	if got := linesOf(str, l); strings.Join(got, "|") != "x |🏳️‍🌈" {
		t.Fatalf("emoji lines = %v", got)
	}
}

func TestLayoutCJK(t *testing.T) {
	str := "中文测试文字排版"
	l := LayoutTextString(str, 6)
	want := []string{"中文测", "试文字", "排版"}
	if got := linesOf(str, l); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lines = %v, want %v", got, want)
	}
	if recon, want := reconstructText(str, l), insertAll(str, l.SoftBreakPositions); recon != want {
		t.Fatalf("reconstruction = %q, want %q", recon, want)
	}
}

func TestLayoutEmptyAndLeadingBreaks(t *testing.T) {
	if l := LayoutTextString("", 10); len(l.Paragraphs) != 0 {
		t.Fatalf("empty input produced %d paragraphs", len(l.Paragraphs))
	}

	str := "\n\nx"
	l := LayoutTextString(str, 10)
	if len(l.Paragraphs) != 3 {
		t.Fatalf("got %d paragraphs, want 3", len(l.Paragraphs))
	}
	want := []string{"", "", "x"}
	if got := linesOf(str, l); strings.Join(got, ";") != strings.Join(want, ";") {
		t.Fatalf("lines = %v, want %v", got, want)
	}
	if recon := reconstructText(str, l); recon != str {
		t.Fatalf("reconstruction = %q, want %q", recon, str)
	}

	for _, s := range []string{"a\nb", "a\rb", "a\u000bb", "a\u000cb", "a\u0085b"} {
		l := LayoutTextString(s, 10)
		if len(l.Paragraphs) != 2 {
			t.Fatalf("%q: got %d paragraphs", s, len(l.Paragraphs))
		}
		if recon, want := reconstructText(s, l), insertAll(s, l.SoftBreakPositions); recon != want {
			t.Fatalf("%q reconstruction = %q, want %q", s, recon, want)
		}
	}
}

func TestLayoutBytesVersion(t *testing.T) {
	str := []byte("hello hello! hello!")
	l := LayoutText(str, 7)
	if len(l.Paragraphs) != 1 || len(l.Paragraphs[0].Lines) != 3 {
		t.Fatalf("unexpected layout: %+v", l)
	}
	if string(str[l.Paragraphs[0].Lines[0].From:l.Paragraphs[0].Lines[0].To]) != "hello " {
		t.Fatal("byte offsets wrong")
	}

	ls := LayoutTextString(string(str), 7)
	if !reflect.DeepEqual(ls, l) {
		t.Fatal("LayoutText and LayoutTextString differ")
	}
}

func TestLayoutSoftBreaksSortedAcrossParagraphs(t *testing.T) {
	str := "aaa bbb ccc\nddd eee fff\nggg hhh iii"
	l := LayoutTextString(str, 5)
	for i := 1; i < len(l.SoftBreakPositions); i++ {
		if l.SoftBreakPositions[i-1] >= l.SoftBreakPositions[i] {
			t.Fatalf("soft breaks not strictly increasing: %v", l.SoftBreakPositions)
		}
	}
	if got, want := reconstructText(str, l), insertAll(str, l.SoftBreakPositions); got != want {
		t.Fatalf("reconstruction = %q, want %q", got, want)
	}
}

func TestLayoutRandomRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(123))
	alphabet := []string{"a", "b", " ", "\t", "中", "\u00a0", "\u2060", "🏳️‍🌈",
		"\n", "\r", "\r\n", ",", "."}
	for iter := 0; iter < 2000; iter++ {
		var b strings.Builder
		n := 1 + rng.Intn(20)
		for j := 0; j < n; j++ {
			b.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		str := b.String()
		for _, W := range []int{0, 1, 2, 4, 8, 30} {
			l := LayoutTextString(str, W)
			got := reconstructText(str, l)
			want := insertAll(str, l.SoftBreakPositions)
			if got != want {
				t.Fatalf("iter=%d W=%d str=%q\n got=%q\nwant=%q",
					iter, W, str, got, want)
			}
			for _, p := range l.Paragraphs {
				for _, ln := range p.Lines {
					if ln.Overflow && ln.Width <= W {
						t.Fatalf("false overflow: %q W=%d", str[ln.From:ln.To], W)
					}
				}
			}
		}
	}
}

func TestLayoutNoNormalization(t *testing.T) {
	// Spaces, tabs, and multiple blanks are kept verbatim.
	str := "a  b\t c"
	l := LayoutTextString(str, 2)
	rebuilt := reconstructText(str, l)
	want := insertAll(str, l.SoftBreakPositions)
	if rebuilt != want {
		t.Fatalf("rebuilt %q, want %q", rebuilt, want)
	}
	// The non-newline bytes of the rebuilt text are exactly the original.
	if stripped := strings.ReplaceAll(rebuilt, "\n", ""); stripped != str {
		t.Fatalf("content normalized: %q", stripped)
	}
}

func insertAll(str string, pos []int) string {
	var b strings.Builder
	prev := 0
	for _, p := range pos {
		b.WriteString(str[prev:p])
		b.WriteByte('\n')
		prev = p
	}
	b.WriteString(str[prev:])
	return b.String()
}

// reconstructText rebuilds the laid out text by concatenating every line in
// order, preserving mandatory break bytes between lines and inserting '\n' at
// the reported soft break positions.
func reconstructText(str string, l TextLayout) string {
	var b strings.Builder
	pos := 0
	for _, p := range l.Paragraphs {
		for li, ln := range p.Lines {
			b.WriteString(str[pos:ln.From])
			b.WriteString(str[ln.From:ln.To])
			isLast := li == len(p.Lines)-1
			if !isLast && containsInt(l.SoftBreakPositions, ln.To) {
				b.WriteByte('\n')
			}
			pos = ln.To
		}
	}
	b.WriteString(str[pos:])
	return b.String()
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
