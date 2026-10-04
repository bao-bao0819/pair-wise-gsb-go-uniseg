package uniseg

// EastAsianAmbiguousWidth specifies the monospace width for East Asian
// characters classified as Ambiguous. The default is 1 but some rare fonts
// render them with a width of 2.
var EastAsianAmbiguousWidth = 1

// defaultEmojiPresentationWidth is the width used for code points presented as
// emoji by the legacy (option-less) API. It mirrors the hard-coded value used
// before [WidthStrategy] was introduced.
const defaultEmojiPresentationWidth = 2

// runeWidthWith returns the monospace width for the given rune. The provided
// grapheme property is a value mapped by the [graphemeCodePoints] table. It is
// the single source of truth for all parsers, both for the option-less API
// (which passes [EastAsianAmbiguousWidth] and [defaultEmojiPresentationWidth])
// and for [WidthStrategy].
//
// Every rune has a width of 1, except for runes with the following properties
// (evaluated in this order):
//
//   - Control, CR, LF, Extend, ZWJ: Width of 0
//   - \u2e3a, TWO-EM DASH: Width of 3
//   - \u2e3b, THREE-EM DASH: Width of 4
//   - East-Asian width Fullwidth and Wide: Width of 2 (Neutral has a width
//     of 1; Ambiguous gets ambiguousWidth)
//   - Regional Indicator: Width of emojiWidth
//   - Extended Pictographic: Width of emojiWidth when the Emoji Presentation
//     property is "Yes", otherwise a width of 1
func runeWidthWith(r rune, graphemeProperty int, ambiguousWidth, emojiWidth int) int {
	switch graphemeProperty {
	case prControl, prCR, prLF, prExtend, prZWJ:
		return 0
	case prRegionalIndicator:
		return emojiWidth
	case prExtendedPictographic:
		if property(emojiPresentation, r) == prEmojiPresentation {
			return emojiWidth
		}
		return 1
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
		return ambiguousWidth
	}

	return 1
}

// updateClusterWidth folds a non-first rune of a grapheme cluster into the
// cluster's running width. Width is decided on the whole grapheme cluster, so
// this mirrors the special handling previously inlined by the parsers:
//
//   - Clusters starting with an Extended Pictographic only react to Variation
//     Selector-15 (U+FE0E, forces text presentation, width 1) and Variation
//     Selector-16 (U+FE0F, forces emoji presentation, width emojiWidth). ZWJ
//     sequences, emoji modifiers and keycap combining marks contribute 0, so
//     the whole presentation unit keeps one width.
//   - Hangul (prL) and Regional Indicator (flags) clusters contribute 0 for
//     every rune after the first one.
//   - All other non-first runes add their own rune width.
func updateClusterWidth(width int, firstProperty, property int, r rune, ambiguousWidth, emojiWidth int) int {
	if firstProperty == prExtendedPictographic {
		switch r {
		case vs15:
			return 1
		case vs16:
			return emojiWidth
		}
	} else if firstProperty != prRegionalIndicator && firstProperty != prL {
		return width + runeWidthWith(r, property, ambiguousWidth, emojiWidth)
	}
	return width
}

// StringWidth returns the monospace width for the given string, that is, the
// number of same-size cells to be occupied by the string.
func StringWidth(s string) (width int) {
	state := -1
	for len(s) > 0 {
		var w int
		_, s, w, state = firstGraphemeClusterInString(s, state, EastAsianAmbiguousWidth, defaultEmojiPresentationWidth)
		width += w
	}
	return
}
