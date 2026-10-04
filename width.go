package uniseg

// EastAsianAmbiguousWidth specifies the monospace width for East Asian
// characters classified as Ambiguous. The default is 1 but some rare fonts
// render them with a width of 2.
var EastAsianAmbiguousWidth = 1

// runeWidth returns the monospace width for the given rune. The provided
// grapheme property is a value mapped by the [graphemeCodePoints] table.
//
// Every rune has a width of 1, except for runes with the following properties
// (evaluated in this order):
//
//   - Control, CR, LF, Extend, ZWJ: Width of 0
//   - \u2e3a, TWO-EM DASH: Width of 3
//   - \u2e3b, THREE-EM DASH: Width of 4
//   - East-Asian width Fullwidth and Wide: Width of 2 (Ambiguous and Neutral
//     have a width of 1)
//   - Regional Indicator: Width of 2
//   - Extended Pictographic: Width of 2, unless Emoji Presentation is "No".
//
// This is retained for backwards compatibility within the package and simply
// delegates to the shared [WidthStrategy] logic using the package-default
// policy (which reads the global [EastAsianAmbiguousWidth]).
func runeWidth(r rune, graphemeProperty int) int {
	return (*WidthStrategy)(nil).firstRuneWidth(r, graphemeProperty)
}

// StringWidth returns the monospace width for the given string, that is, the
// number of same-size cells to be occupied by the string.
func StringWidth(s string) (width int) {
	state := -1
	for len(s) > 0 {
		var w int
		_, s, w, state = FirstGraphemeClusterInString(s, state)
		width += w
	}
	return
}
