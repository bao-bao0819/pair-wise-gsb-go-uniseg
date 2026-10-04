package uniseg

import "unicode/utf8"

// WidthStrategy is an immutable, reusable policy for calculating the monospace
// display width of text. Unlike the package-level [EastAsianAmbiguousWidth]
// variable, a WidthStrategy is safe for concurrent use: it never reads from or
// writes to any global state, so parallel requests can use different strategies
// without interfering with each other.
//
// A WidthStrategy only changes two kinds of widths. All other width rules of
// this package remain unchanged:
//
//   - Code points with grapheme cluster break properties Control, CR, LF,
//     Extend, and ZWJ have a width of 0.
//   - U+2E3A (Two-Em Dash) has a width of 3 and U+2E3B (Three-Em Dash) has a
//     width of 4.
//   - East-Asian Width properties "Fullwidth" (F) and "Wide" (W) have a width
//     of 2 and "Neutral" (N) has a width of 1.
//
// Width is always decided at the grapheme cluster level: an emoji ZWJ sequence,
// a sequence with an emoji skin tone modifier, a Regional Indicator pair (flag),
// and a keycap sequence are each a single presentation unit. A Variation
// Selector-15 (U+FE0E) forces text presentation while a Variation Selector-16
// (U+FE0F) forces emoji presentation, based on the Unicode emoji presentation
// table.
//
// The zero value is not a valid WidthStrategy; use [NewWidthStrategy] to
// construct one.
type WidthStrategy struct {
	// Width of East-Asian "Ambiguous" (A) code points. Always 1 or 2.
	ambiguousWidth int

	// Width of a grapheme cluster rendered with emoji presentation. Always 1
	// or 2.
	emojiPresentationWidth int
}

// NewWidthStrategy returns a new immutable [WidthStrategy]. ambiguousWidth is
// the monospace width of East-Asian Ambiguous code points and
// emojiPresentationWidth is the monospace width of a grapheme cluster rendered
// with emoji presentation. Both values must be either 1 or 2; any other value
// causes a panic.
func NewWidthStrategy(ambiguousWidth, emojiPresentationWidth int) WidthStrategy {
	if ambiguousWidth != 1 && ambiguousWidth != 2 {
		panic("uniseg: ambiguous width must be 1 or 2")
	}
	if emojiPresentationWidth != 1 && emojiPresentationWidth != 2 {
		panic("uniseg: emoji presentation width must be 1 or 2")
	}
	return WidthStrategy{
		ambiguousWidth:         ambiguousWidth,
		emojiPresentationWidth: emojiPresentationWidth,
	}
}

// AmbiguousWidth returns the configured monospace width for East-Asian
// Ambiguous code points. It is always either 1 or 2.
func (s WidthStrategy) AmbiguousWidth() int {
	return s.ambiguousWidth
}

// EmojiPresentationWidth returns the configured monospace width for a grapheme
// cluster rendered with emoji presentation. It is always either 1 or 2.
func (s WidthStrategy) EmojiPresentationWidth() int {
	return s.emojiPresentationWidth
}

// ambiguousWidthFor returns the effective East-Asian Ambiguous width for the
// receiver. A nil strategy means "use the package default", i.e. the value of
// the global [EastAsianAmbiguousWidth] variable, preserving the behavior of the
// strategy-less functions.
func (s *WidthStrategy) ambiguousWidthFor() int {
	if s == nil {
		return EastAsianAmbiguousWidth
	}
	return s.ambiguousWidth
}

// emojiWidthFor returns the effective width of a grapheme cluster with emoji
// presentation for the receiver. A nil strategy means "use the package
// default", which is 2.
func (s *WidthStrategy) emojiWidthFor() int {
	if s == nil {
		return 2
	}
	return s.emojiPresentationWidth
}

// firstRuneWidth returns the width of the first rune of a grapheme cluster. The
// provided graphemeProperty is a value mapped by the [graphemeCodePoints] table.
// It is the single source of truth for the initial cluster width, shared by the
// whole-string, iterator, and per-cluster code paths.
func (s *WidthStrategy) firstRuneWidth(r rune, graphemeProperty int) int {
	switch graphemeProperty {
	case prControl, prCR, prLF, prExtend, prZWJ:
		return 0
	case prRegionalIndicator:
		return s.emojiWidthFor()
	case prExtendedPictographic:
		if property(emojiPresentation, r) == prEmojiPresentation {
			return s.emojiWidthFor()
		}
		return 1 // Emoji Presentation "No" means text presentation: 1 cell.
	}

	switch r {
	case 0x2e3a:
		return 3
	case 0x2e3b:
		return 4
	}

	switch propertyEastAsianWidth(r) {
	case prW, prF:
		return 2
	case prA:
		return s.ambiguousWidthFor()
	}

	return 1
}

// addRuneWidth folds the width of a subsequent (non-first) rune into the width
// of the grapheme cluster being built. firstProperty is the grapheme property
// of the cluster's first rune and property is the current rune's grapheme
// property. This mirrors the package's grouping rules:
//
//   - For a cluster starting with an Extended Pictographic, VS15 forces text
//     presentation and VS16 forces emoji presentation; any other continuation
//     rune does not change the width.
//   - For Hangul (property L) and Regional Indicator clusters, continuation
//     runes add nothing (the cluster is a single unit).
//   - Every other continuation rune adds its own rune width.
//
// Grapheme cluster boundaries are determined independently of widths, so this
// function never affects segmentation.
func (s *WidthStrategy) addRuneWidth(width int, firstProperty, property int, r rune) int {
	if firstProperty == prExtendedPictographic {
		if r == vs15 {
			return 1 // VS15 forces text presentation.
		}
		if r == vs16 {
			return s.emojiWidthFor()
		}
		return width
	}
	if firstProperty == prRegionalIndicator || firstProperty == prL {
		return width
	}
	return width + s.firstRuneWidth(r, property)
}

// StringWidth returns the monospace width for the given string, that is, the
// number of same-size cells occupied by the string, using the receiver's width
// policy.
func (s WidthStrategy) StringWidth(str string) (width int) {
	state := -1
	for len(str) > 0 {
		var w int
		_, str, w, state = s.FirstGraphemeClusterInString(str, state)
		width += w
	}
	return
}

// NewGraphemes returns a new [Graphemes] iterator over str whose reported
// grapheme cluster widths follow the receiver's policy. Grapheme cluster, word,
// sentence, and line boundaries are unaffected by the policy.
func (s WidthStrategy) NewGraphemes(str string) *Graphemes {
	return &Graphemes{
		original:      str,
		remaining:     str,
		state:         -1,
		widthStrategy: &s,
	}
}

// FirstGraphemeCluster is like the package-level [FirstGraphemeCluster]
// function but it calculates the cluster width using the receiver's policy. The
// state parameter and the returned cluster, rest, and newState values are
// identical to the package-level function; segmentation does not change with
// the policy.
func (s WidthStrategy) FirstGraphemeCluster(b []byte, state int) (cluster, rest []byte, width, newState int) {
	return firstGraphemeCluster(b, state, &s)
}

// FirstGraphemeClusterInString is like the package-level
// [FirstGraphemeClusterInString] function but it calculates the cluster width
// using the receiver's policy. The state parameter and the returned cluster,
// rest, and newState values are identical to the package-level function;
// segmentation does not change with the policy.
func (s WidthStrategy) FirstGraphemeClusterInString(str string, state int) (cluster, rest string, width, newState int) {
	return firstGraphemeClusterInString(str, state, &s)
}

// firstGraphemeCluster is the single implementation backing
// [FirstGraphemeCluster] and [WidthStrategy.FirstGraphemeCluster]. A nil
// strategy reproduces the package-default behavior (reading
// [EastAsianAmbiguousWidth]).
func firstGraphemeCluster(b []byte, state int, strategy *WidthStrategy) (cluster, rest []byte, width, newState int) {
	// An empty byte slice returns nothing.
	if len(b) == 0 {
		return
	}

	// Extract the first rune.
	r, length := utf8.DecodeRune(b)
	if len(b) <= length { // If we're already past the end, there is nothing else to parse.
		var prop int
		if state < 0 {
			prop = propertyGraphemes(r)
		} else {
			prop = state >> shiftGraphemePropState
		}
		return b, nil, strategy.firstRuneWidth(r, prop), grAny | (prop << shiftGraphemePropState)
	}

	// If we don't know the state, determine it now.
	var firstProp int
	if state < 0 {
		state, firstProp, _ = transitionGraphemeState(state, r)
	} else {
		firstProp = state >> shiftGraphemePropState
	}
	width += strategy.firstRuneWidth(r, firstProp)

	// Transition until we find a boundary.
	for {
		var (
			prop     int
			boundary bool
		)

		r, l := utf8.DecodeRune(b[length:])
		state, prop, boundary = transitionGraphemeState(state&maskGraphemeState, r)

		if boundary {
			return b[:length], b[length:], width, state | (prop << shiftGraphemePropState)
		}

		width = strategy.addRuneWidth(width, firstProp, prop, r)

		length += l
		if len(b) <= length {
			return b, nil, width, grAny | (prop << shiftGraphemePropState)
		}
	}
}

// firstGraphemeClusterInString is the single implementation backing
// [FirstGraphemeClusterInString] and
// [WidthStrategy.FirstGraphemeClusterInString]. A nil strategy reproduces the
// package-default behavior (reading [EastAsianAmbiguousWidth]).
func firstGraphemeClusterInString(str string, state int, strategy *WidthStrategy) (cluster, rest string, width, newState int) {
	// An empty string returns nothing.
	if len(str) == 0 {
		return
	}

	// Extract the first rune.
	r, length := utf8.DecodeRuneInString(str)
	if len(str) <= length { // If we're already past the end, there is nothing else to parse.
		var prop int
		if state < 0 {
			prop = propertyGraphemes(r)
		} else {
			prop = state >> shiftGraphemePropState
		}
		return str, "", strategy.firstRuneWidth(r, prop), grAny | (prop << shiftGraphemePropState)
	}

	// If we don't know the state, determine it now.
	var firstProp int
	if state < 0 {
		state, firstProp, _ = transitionGraphemeState(state, r)
	} else {
		firstProp = state >> shiftGraphemePropState
	}
	width += strategy.firstRuneWidth(r, firstProp)

	// Transition until we find a boundary.
	for {
		var (
			prop     int
			boundary bool
		)

		r, l := utf8.DecodeRuneInString(str[length:])
		state, prop, boundary = transitionGraphemeState(state&maskGraphemeState, r)

		if boundary {
			return str[:length], str[length:], width, state | (prop << shiftGraphemePropState)
		}

		width = strategy.addRuneWidth(width, firstProp, prop, r)

		length += l
		if len(str) <= length {
			return str, "", width, grAny | (prop << shiftGraphemePropState)
		}
	}
}
