package uniseg

import (
	"runtime"
	"sync"
	"testing"
)

// widthThreeWays reports the width of str computed via the whole-string
// helper, the grapheme iterator, and the per-cluster parser. All three must
// always agree because they share one implementation.
func widthThreeWays(t *testing.T, s WidthStrategy, str string) (total int, clusters int) {
	t.Helper()

	stringWidth := s.StringWidth(str)

	var iteratorWidth int
	g := s.NewGraphemes(str)
	for g.Next() {
		iteratorWidth += g.Width()
		clusters++
	}

	var parserWidth int
	state := -1
	rest := str
	for len(rest) > 0 {
		var w int
		_, rest, w, state = s.FirstGraphemeClusterInString(rest, state)
		parserWidth += w
	}

	// Also exercise the byte-slice parser; it must stay consistent.
	var bytesWidth int
	state = -1
	b := []byte(str)
	for len(b) > 0 {
		var w int
		_, b, w, state = s.FirstGraphemeCluster(b, state)
		bytesWidth += w
	}

	if stringWidth != iteratorWidth || iteratorWidth != parserWidth || parserWidth != bytesWidth {
		t.Errorf("inconsistent widths for %q with strategy(A=%d, E=%d): StringWidth=%d iterator=%d parser=%d bytes=%d",
			str, s.AmbiguousWidth(), s.EmojiPresentationWidth(),
			stringWidth, iteratorWidth, parserWidth, bytesWidth)
	}

	return stringWidth, clusters
}

// TestWidthStrategyParallelReports generates two reports for the same input
// concurrently, over and over: one terminal counts East-Asian Ambiguous and
// emoji presentation as one cell, the other counts them as two cells. Each
// report must observe a stable width regardless of how the goroutines
// interleave, even while the global EastAsianAmbiguousWidth is deliberately set
// to a third value to prove strategies never consult it.
func TestWidthStrategyParallelReports(t *testing.T) {
	// "ⓐ©☺️⌛😀!": Ambiguous, text-presentation emoji, emoji-presentation
	// emoji, VS15 text presentation, a wide emoji, and plain text.
	input := "\U0001F100\u00a9\u263a\ufe0f\u231b\ufe0e\U0001F60A!"

	narrow := NewWidthStrategy(1, 1) // Ambiguous and emoji: 1 cell.
	wide := NewWidthStrategy(2, 2)   // Ambiguous and emoji: 2 cells.

	const iterations = 500
	const (
		narrowExpected = 6 // 1+1+1+1+1+1
		wideExpected   = 9 // 2(A)+1(copyright)+2+1(VS15)+2+1
	)

	// Deliberately perturb the global; immutable strategies must ignore it.
	oldGlobal := EastAsianAmbiguousWidth
	EastAsianAmbiguousWidth = 2
	t.Cleanup(func() { EastAsianAmbiguousWidth = oldGlobal })

	generate := func(wg *sync.WaitGroup, strategy WidthStrategy, expected int) {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			w, clusters := widthThreeWays(t, strategy, input)
			if w != expected {
				t.Errorf("strategy(A=%d,E=%d) iteration %d: width %d, expected %d",
					strategy.AmbiguousWidth(), strategy.EmojiPresentationWidth(), i, w, expected)
				return
			}
			if clusters != 6 {
				t.Errorf("strategy(A=%d,E=%d) iteration %d: %d clusters, expected 6",
					strategy.AmbiguousWidth(), strategy.EmojiPresentationWidth(), i, clusters)
				return
			}
			runtime.Gosched() // Encourage aggressive interleaving.
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go generate(&wg, narrow, narrowExpected)
	go generate(&wg, wide, wideExpected)
	wg.Wait()
}

// TestWidthStrategyClusterSum walks a string containing a text-presentation
// variant, a rainbow flag (ZWJ emoji sequence), and an isolated combining mark
// cluster by cluster, checking each cluster's width and the total. It exercises
// every combination of ambiguous and emoji presentation widths.
func TestWidthStrategyClusterSum(t *testing.T) {
	// Clusters:
	//  1. combining grave (U+0300) leading the string: isolated combining mark
	//  2. U+263A + VS16: emoji presentation
	//  3. U+231B + VS15: text presentation
	//  4. white flag + VS16 + ZWJ + rainbow: one ZWJ presentation unit
	//  5. plain "a"
	str := "\u0300\u263a\ufe0f\u231b\ufe0e\U0001f3f3\ufe0f\u200d\U0001f308a"

	cases := []struct {
		ambiguous, emoji int
		clusterWidths    []int
		total            int
	}{
		{1, 1, []int{0, 1, 1, 1, 1}, 4},
		{1, 2, []int{0, 2, 1, 2, 1}, 6},
		{2, 1, []int{0, 1, 1, 1, 1}, 4},
		{2, 2, []int{0, 2, 1, 2, 1}, 6},
	}

	for _, tc := range cases {
		s := NewWidthStrategy(tc.ambiguous, tc.emoji)

		var (
			gotWidths []int
			sum       int
			state     = -1
			rest      = str
		)
		for len(rest) > 0 {
			var w int
			_, rest, w, state = s.FirstGraphemeClusterInString(rest, state)
			gotWidths = append(gotWidths, w)
			sum += w
		}

		if len(gotWidths) != len(tc.clusterWidths) {
			t.Fatalf("strategy(A=%d,E=%d): got %d clusters %v, expected %d",
				tc.ambiguous, tc.emoji, len(gotWidths), gotWidths, len(tc.clusterWidths))
		}
		for i, w := range gotWidths {
			if w != tc.clusterWidths[i] {
				t.Errorf("strategy(A=%d,E=%d) cluster %d: width %d, expected %d",
					tc.ambiguous, tc.emoji, i, w, tc.clusterWidths[i])
			}
		}
		if sum != tc.total {
			t.Errorf("strategy(A=%d,E=%d): summed width %d, expected %d",
				tc.ambiguous, tc.emoji, sum, tc.total)
		}

		// The whole-string helper and the iterator must yield the same total.
		if total, clusters := widthThreeWays(t, s, str); total != tc.total || clusters != len(tc.clusterWidths) {
			t.Errorf("strategy(A=%d,E=%d): total=%d clusters=%d, expected total=%d clusters=%d",
				tc.ambiguous, tc.emoji, total, clusters, tc.total, len(tc.clusterWidths))
		}
	}
}

// TestWidthStrategyPresentationUnits verifies that emoji ZWJ sequences, skin
// tone modifiers, Regional Indicator flags, and keycap sequences each form a
// single presentation unit, and that the strategy only changes the two declared
// width classes (the keycap sequence keeps its legacy width of 1).
func TestWidthStrategyPresentationUnits(t *testing.T) {
	units := []struct {
		name string
		str  string
		// emojiWidth is the unit's width under emoji presentation width 1 and 2.
		emoji1, emoji2 int
	}{
		{"skin tone", "\U0001F44D\U0001F3FD", 1, 2}, // 👍🏽
		{"zwj family", "\U0001F468\u200d\U0001F469\u200d\U0001F467\u200d\U0001F466", 1, 2},
		{"ri flag", "\U0001F1E9\U0001F1EA", 1, 2}, // 🇩🇪
		{"rainbow flag", "\U0001f3f3\ufe0f\u200d\U0001f308", 1, 2},
		{"keycap", "1\ufe0f\u20e3", 1, 1}, // 1️⃣ keeps its legacy width.
	}

	for _, u := range units {
		if n := GraphemeClusterCount(u.str); n != 1 {
			t.Errorf("%s: got %d grapheme clusters, expected 1", u.name, n)
		}
		for _, tc := range []struct{ emoji, expected int }{{1, u.emoji1}, {2, u.emoji2}} {
			s := NewWidthStrategy(1, tc.emoji)
			if w, clusters := widthThreeWays(t, s, u.str); w != tc.expected || clusters != 1 {
				t.Errorf("%s with emoji width %d: width=%d clusters=%d, expected width=%d clusters=1",
					u.name, tc.emoji, w, clusters, tc.expected)
			}
		}
	}
}

// TestWidthStrategyFixedRules verifies that widths the strategy does not claim
// to control remain identical to the package rules for every strategy.
func TestWidthStrategyFixedRules(t *testing.T) {
	fixed := []struct {
		name string
		str  string
		want int
	}{
		{"wide rune", "\uff01", 2},       // Fullwidth exclamation mark.
		{"combining mark", "a\u0300", 1}, // Zero-width Extend.
		{"isolated combining", "\u0300", 0},
		{"control", "\x00", 0},
		{"two-em dash", "\u2e3a", 3},
		{"three-em dash", "\u2e3b", 4},
		{"neutral", "x", 1},
	}
	for _, ambiguous := range []int{1, 2} {
		for _, emoji := range []int{1, 2} {
			s := NewWidthStrategy(ambiguous, emoji)
			for _, c := range fixed {
				if w := s.StringWidth(c.str); w != c.want {
					t.Errorf("%s with (A=%d,E=%d): width %d, expected %d",
						c.name, ambiguous, emoji, w, c.want)
				}
			}
		}
	}
}

// TestNewWidthStrategyRejectsInvalidValues ensures only 1 and 2 are accepted.
func TestNewWidthStrategyRejectsInvalidValues(t *testing.T) {
	for _, values := range [][2]int{{0, 1}, {3, 1}, {1, 0}, {1, 3}, {-1, 2}, {2, 4}} {
		func(a, e int) {
			defer func() {
				if recover() == nil {
					t.Errorf("NewWidthStrategy(%d, %d) should have panicked", a, e)
				}
			}()
			NewWidthStrategy(a, e)
		}(values[0], values[1])
	}

	s := NewWidthStrategy(2, 1)
	if s.AmbiguousWidth() != 2 || s.EmojiPresentationWidth() != 1 {
		t.Errorf("unexpected strategy getters: A=%d E=%d", s.AmbiguousWidth(), s.EmojiPresentationWidth())
	}
}

// TestWidthStrategyDefaultCompatibility ensures the immutable default strategy
// (ambiguous width 1, emoji presentation width 2) reproduces the legacy
// option-less API exactly for all existing width test cases.
func TestWidthStrategyDefaultCompatibility(t *testing.T) {
	s := NewWidthStrategy(EastAsianAmbiguousWidth, 2)
	for index, tc := range widthTestCases {
		if w := s.StringWidth(tc.original); w != tc.expected {
			t.Errorf("default strategy StringWidth(%q) is %d, expected %d (test case %d)",
				tc.original, w, tc.expected, index)
		}
		g := s.NewGraphemes(tc.original)
		var sum int
		for g.Next() {
			sum += g.Width()
		}
		if sum != tc.expected {
			t.Errorf("default strategy iterator width of %q is %d, expected %d (test case %d)",
				tc.original, sum, tc.expected, index)
		}
	}
}

// TestLegacyGlobalAmbiguousWidthUnchanged confirms the option-less API still
// honors the mutable global and that strategies built alongside it are
// unaffected by later global changes.
func TestLegacyGlobalAmbiguousWidthUnchanged(t *testing.T) {
	old := EastAsianAmbiguousWidth
	defer func() { EastAsianAmbiguousWidth = old }()

	const ambiguous = "\U0001F100"
	EastAsianAmbiguousWidth = 1
	if w := StringWidth(ambiguous); w != 1 {
		t.Fatalf("legacy width with global=1: got %d, expected 1", w)
	}
	EastAsianAmbiguousWidth = 2
	if w := StringWidth(ambiguous); w != 2 {
		t.Fatalf("legacy width with global=2: got %d, expected 2", w)
	}

	// A strategy captured at ambiguous width 1 must stay at 1 even though the
	// global now reads 2.
	narrow := NewWidthStrategy(1, 2)
	wide := NewWidthStrategy(2, 2)
	if narrow.StringWidth(ambiguous) != 1 || wide.StringWidth(ambiguous) != 2 {
		t.Fatalf("strategies drifted: narrow=%d wide=%d",
			narrow.StringWidth(ambiguous), wide.StringWidth(ambiguous))
	}
}

// TestWidthStrategyBoundariesUnchanged verifies the policy never alters
// grapheme, word, sentence, or line segmentation.
func TestWidthStrategyBoundariesUnchanged(t *testing.T) {
	str := "Hello, \U0001F468\u200d\U0001F469 world!"
	for _, s := range []WidthStrategy{NewWidthStrategy(1, 1), NewWidthStrategy(2, 2)} {
		legacy := NewGraphemes(str)
		policy := s.NewGraphemes(str)
		for {
			ln := legacy.Next()
			lp := policy.Next()
			if ln != lp {
				t.Fatalf("iterator termination differs for strategy %+v", s)
			}
			if !ln {
				break
			}
			if legacy.Str() != policy.Str() ||
				legacy.IsWordBoundary() != policy.IsWordBoundary() ||
				legacy.IsSentenceBoundary() != policy.IsSentenceBoundary() ||
				legacy.LineBreak() != policy.LineBreak() {
				t.Fatalf("boundary mismatch for cluster %q under strategy %+v", legacy.Str(), s)
			}
		}
	}
}
