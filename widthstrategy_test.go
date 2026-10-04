package uniseg

import (
	"sync"
	"testing"
)

// widthStrategyCases are the grapheme clusters of widthStrategyMixed, in
// order. The string covers, in order: an isolated combining mark, a
// text-presentation variant selector, an emoji-presentation variant
// selector, an emoji ZWJ sequence (rainbow flag), an East-Asian Ambiguous
// code point, a Regional Indicator flag pair, an emoji-default pictograph,
// a text-default pictograph (copyright sign), a Wide CJK character, and a
// neutral letter.
var widthStrategyClusters = []string{
	"\u0300",                           // Isolated combining grave accent (leads the string)
	"\u263a\ufe0e",                     // White smiling face + VS15 (text)
	"\u263a\ufe0f",                     // White smiling face + VS16 (emoji)
	"\U0001f3f3\ufe0f\u200d\U0001f308", // Rainbow flag (ZWJ sequence)
	"\u00a1",                           // INVERTED EXCLAMATION MARK (Ambiguous)
	"\U0001f1e9\U0001f1ea",             // German flag (Regional Indicators)
	"\U0001F60A",                       // Smiling face with smiling eyes
	"\u00a9",                           // COPYRIGHT SIGN (pictograph, text default)
	"\u4e2d",                           // CJK ideograph (Wide)
	"A",                                // Neutral letter
}

var widthStrategyMixed = func() string {
	var s string
	for _, cluster := range widthStrategyClusters {
		s += cluster
	}
	return s
}()

// expectedClusterWidths returns the width of every cluster in
// widthStrategyClusters under the given (ambiguousWidth, emojiWidth) policy.
func expectedClusterWidths(ambiguousWidth, emojiWidth int) []int {
	return []int{
		0,          // Isolated combining mark
		1,          // VS15 forces text presentation
		emojiWidth, // VS16 forces emoji presentation
		emojiWidth, // Rainbow flag is one emoji presentation unit
		ambiguousWidth,
		emojiWidth, // RI flag pair
		emojiWidth, // Emoji-default pictograph
		1,          // Text-default pictograph
		2,          // Wide CJK, always 2
		1,          // Neutral letter
	}
}

func sum(widths []int) (total int) {
	for _, width := range widths {
		total += width
	}
	return
}

// All cluster-parsing entry points must agree with WidthStrategy.StringWidth
// and with each other: there is only one width calculation.
func TestWidthStrategyConsistency(t *testing.T) {
	policies := []struct {
		ambiguous, emoji int
	}{
		{1, 1},
		{1, 2},
		{2, 1},
		{2, 2},
	}

	for _, policy := range policies {
		strategy := NewWidthStrategy(
			WithAmbiguousWidth(policy.ambiguous),
			WithEmojiWidth(policy.emoji),
		)

		if got := strategy.AmbiguousWidth(); got != policy.ambiguous {
			t.Errorf("AmbiguousWidth() = %d, expected %d", got, policy.ambiguous)
		}
		if got := strategy.EmojiWidth(); got != policy.emoji {
			t.Errorf("EmojiWidth() = %d, expected %d", got, policy.emoji)
		}

		expected := expectedClusterWidths(policy.ambiguous, policy.emoji)
		expectedTotal := sum(expected)

		// Whole-string width.
		if got := strategy.StringWidth(widthStrategyMixed); got != expectedTotal {
			t.Errorf("ambiguous=%d emoji=%d: StringWidth = %d, expected %d",
				policy.ambiguous, policy.emoji, got, expectedTotal)
		}

		// Grapheme iterator.
		graphemes := strategy.NewGraphemes(widthStrategyMixed)
		var iteratorTotal int
		var index int
		for graphemes.Next() {
			if got := graphemes.Width(); got != expected[index] {
				t.Errorf("ambiguous=%d emoji=%d: iterator cluster %d (%q) width = %d, expected %d",
					policy.ambiguous, policy.emoji, index, graphemes.Str(), got, expected[index])
			}
			iteratorTotal += graphemes.Width()
			index++
		}
		if iteratorTotal != expectedTotal {
			t.Errorf("ambiguous=%d emoji=%d: iterator total = %d, expected %d",
				policy.ambiguous, policy.emoji, iteratorTotal, expectedTotal)
		}

		// Cluster-by-cluster parsing on strings.
		var parsedClusters []string
		state := -1
		rest := widthStrategyMixed
		var stringTotal int
		for len(rest) > 0 {
			var cluster string
			var width int
			cluster, rest, width, state = strategy.FirstGraphemeClusterInString(rest, state)
			parsedClusters = append(parsedClusters, cluster)
			stringTotal += width
		}
		if stringTotal != expectedTotal {
			t.Errorf("ambiguous=%d emoji=%d: FirstGraphemeClusterInString total = %d, expected %d",
				policy.ambiguous, policy.emoji, stringTotal, expectedTotal)
		}

		// Cluster-by-cluster parsing on byte slices.
		state = -1
		bytes := []byte(widthStrategyMixed)
		var bytesTotal int
		for len(bytes) > 0 {
			var width int
			_, bytes, width, state = strategy.FirstGraphemeCluster(bytes, state)
			bytesTotal += width
		}
		if bytesTotal != expectedTotal {
			t.Errorf("ambiguous=%d emoji=%d: FirstGraphemeCluster total = %d, expected %d",
				policy.ambiguous, policy.emoji, bytesTotal, expectedTotal)
		}

		// Width policies never change segmentation boundaries.
		var legacyClusters []string
		state = -1
		legacyRest := widthStrategyMixed
		for len(legacyRest) > 0 {
			var cluster string
			cluster, legacyRest, _, state = FirstGraphemeClusterInString(legacyRest, state)
			legacyClusters = append(legacyClusters, cluster)
		}
		if len(parsedClusters) != len(legacyClusters) {
			t.Fatalf("ambiguous=%d emoji=%d: got %d clusters, legacy parser produced %d",
				policy.ambiguous, policy.emoji, len(parsedClusters), len(legacyClusters))
		}
		for index := range parsedClusters {
			if parsedClusters[index] != legacyClusters[index] {
				t.Errorf("ambiguous=%d emoji=%d: cluster %d is %q, legacy parser produced %q",
					policy.ambiguous, policy.emoji, index, parsedClusters[index], legacyClusters[index])
			}
		}
	}
}

// Per-cluster summation of a string containing a text variant, a rainbow
// flag, and an isolated combining mark, using the default strategy.
func TestWidthStrategyClusterSum(t *testing.T) {
	strategy := NewWidthStrategy()
	expected := expectedClusterWidths(1, 2)

	state := -1
	rest := widthStrategyMixed
	var total int
	var index int
	for len(rest) > 0 {
		var cluster string
		var width int
		cluster, rest, width, state = strategy.FirstGraphemeClusterInString(rest, state)
		if width != expected[index] {
			t.Errorf("cluster %d %q width = %d, expected %d",
				index, cluster, width, expected[index])
		}
		total += width
		index++
	}

	if total != sum(expected) {
		t.Errorf("summed width = %d, expected %d", total, sum(expected))
	}
	if total != strategy.StringWidth(widthStrategyMixed) {
		t.Errorf("summed width = %d does not match StringWidth = %d",
			total, strategy.StringWidth(widthStrategyMixed))
	}
}

// ZWJ sequences, skin tone modifiers, and keycap combining marks form one
// presentation unit; VS15 always forces width 1 while VS16 yields the
// strategy's emoji width.
func TestWidthStrategyPresentationUnits(t *testing.T) {
	cases := []struct {
		name    string
		s       string
		narrowW int // ambiguous=1, emoji=1
		wideW   int // ambiguous=2, emoji=2
	}{
		{"skin tone", "\U0001F44B\U0001F3FD", 1, 2},
		{"keycap", "1\ufe0f\u20e3", 1, 1},
		{"family ZWJ", "\U0001f468\u200d\U0001f469\u200d\U0001f467\u200d\U0001f466", 1, 2},
		{"hourglass VS15", "\u231b\ufe0e", 1, 1},
		{"hourglass default", "\u231b", 1, 2},
	}
	narrow := NewWidthStrategy(WithAmbiguousWidth(1), WithEmojiWidth(1))
	wide := NewWidthStrategy(WithAmbiguousWidth(2), WithEmojiWidth(2))
	for _, c := range cases {
		if got := narrow.StringWidth(c.s); got != c.narrowW {
			t.Errorf("%s: narrow StringWidth(%q) = %d, want %d", c.name, c.s, got, c.narrowW)
		}
		if got := wide.StringWidth(c.s); got != c.wideW {
			t.Errorf("%s: wide StringWidth(%q) = %d, want %d", c.name, c.s, got, c.wideW)
		}
	}
}

// The default strategy must reproduce the option-less API, even while the
// global EastAsianAmbiguousWidth is changed.
func TestWidthStrategyDefaultMatchesLegacy(t *testing.T) {
	original := EastAsianAmbiguousWidth
	defer func() { EastAsianAmbiguousWidth = original }()

	strategy := NewWidthStrategy()
	for _, global := range []int{1, 2} {
		EastAsianAmbiguousWidth = global
		for index, testCase := range widthTestCases {
			got := strategy.StringWidth(testCase.original)
			if global == 1 {
				// With the default global, the strategy matches the legacy API.
				if legacy := StringWidth(testCase.original); got != legacy {
					t.Errorf("global=1 case %d %q: strategy width %d differs from legacy width %d",
						index, testCase.original, got, legacy)
				}
			}
			// The strategy is independent of the global: it always reproduces
			// the baseline results recorded in widthTestCases (which assume
			// the default ambiguous width of 1).
			if got != testCase.expected {
				t.Errorf("global=%d case %d %q: strategy width %d, expected baseline %d",
					global, index, testCase.original, got, testCase.expected)
			}
		}
	}
}

// reportInput is the shared input rendered for two different terminals.
var reportInput = "\u00a1\U0001F60A\u4e2dA\u263a\ufe0f"

const (
	narrowReportWidth = 6 // ambiguous=1, emoji=1
	wideReportWidth   = 9 // ambiguous=2, emoji=2
	reportIterations  = 200
)

// Two reports are generated in parallel for two terminals. The same input is
// processed in strictly interleaved turns while the global
// EastAsianAmbiguousWidth is deliberately set to 2; each strategy must keep
// reporting its own stable width without affecting the other.
func TestWidthStrategyParallelReports(t *testing.T) {
	original := EastAsianAmbiguousWidth
	EastAsianAmbiguousWidth = 2
	t.Cleanup(func() { EastAsianAmbiguousWidth = original })

	narrow := NewWidthStrategy(WithAmbiguousWidth(1), WithEmojiWidth(1))
	wide := NewWidthStrategy(WithAmbiguousWidth(2), WithEmojiWidth(2))

	widthViaIterator := func(strategy *WidthStrategy) int {
		graphemes := strategy.NewGraphemes(reportInput)
		var total int
		for graphemes.Next() {
			total += graphemes.Width()
		}
		return total
	}

	narrowResults := make([]int, reportIterations)
	wideResults := make([]int, reportIterations)

	narrowTurn := make(chan struct{})
	wideTurn := make(chan struct{})
	var waiter sync.WaitGroup
	waiter.Add(2)

	go func() {
		defer waiter.Done()
		for index := 0; index < reportIterations; index++ {
			<-narrowTurn
			narrowResults[index] = narrow.StringWidth(reportInput)
			if widthViaIterator(narrow) != narrowReportWidth {
				narrowResults[index] = -1
			}
			wideTurn <- struct{}{}
		}
	}()

	go func() {
		defer waiter.Done()
		for index := 0; index < reportIterations; index++ {
			<-wideTurn
			wideResults[index] = wide.StringWidth(reportInput)
			if widthViaIterator(wide) != wideReportWidth {
				wideResults[index] = -1
			}
			if index+1 < reportIterations {
				narrowTurn <- struct{}{}
			}
		}
	}()

	narrowTurn <- struct{}{}
	waiter.Wait()

	for index, width := range narrowResults {
		if width != narrowReportWidth {
			t.Fatalf("narrow report iteration %d: width = %d, expected %d",
				index, width, narrowReportWidth)
		}
	}
	for index, width := range wideResults {
		if width != wideReportWidth {
			t.Fatalf("wide report iteration %d: width = %d, expected %d",
				index, width, wideReportWidth)
		}
	}
}

func TestNewWidthStrategyInvalidOptions(t *testing.T) {
	if !panics(func() { NewWidthStrategy(WithAmbiguousWidth(0)) }) {
		t.Error("WithAmbiguousWidth(0) did not panic")
	}
	if !panics(func() { NewWidthStrategy(WithAmbiguousWidth(3)) }) {
		t.Error("WithAmbiguousWidth(3) did not panic")
	}
	if !panics(func() { NewWidthStrategy(WithEmojiWidth(0)) }) {
		t.Error("WithEmojiWidth(0) did not panic")
	}
	if !panics(func() { NewWidthStrategy(WithEmojiWidth(3)) }) {
		t.Error("WithEmojiWidth(3) did not panic")
	}
}

func panics(function func()) (didPanic bool) {
	defer func() {
		if recover() != nil {
			didPanic = true
		}
	}()
	function()
	return false
}
