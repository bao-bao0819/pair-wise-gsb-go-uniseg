package uniseg

// WidthStrategy is an immutable, reusable monospace width policy. It differs
// from the package-wide [EastAsianAmbiguousWidth] variable in that a strategy
// is bound to its own two widths, so independent requests (for example a
// report rendered for a terminal counting East Asian Ambiguous characters as
// one column and another report for a terminal counting them as two columns)
// can be served in parallel without interfering with each other and without
// temporarily mutating any global state.
//
// Only two categories of widths are configurable, and each is either 1 or 2:
//
//   - The width of code points with the East-Asian Width property "Ambiguous"
//     (via [WithAmbiguousWidth]).
//   - The width of emoji presentation units, that is grapheme clusters which
//     present as emoji: emoji-default Extended Pictographic code points,
//     clusters forced to emoji presentation by Variation Selector-16
//     (U+FE0F), and Regional Indicator flag pairs (via [WithEmojiWidth]).
//
// All other rules are inherited from the package: Control/CR/LF/Extend/ZWJ
// code points have a width of 0, Fullwidth/Wide code points have a width of
// 2, U+2E3A has a width of 3, U+2E3B has a width of 4, and everything else
// has a width of 1. Variation Selector-15 (U+FE0E) always forces text
// presentation (width 1) and Variation Selector-16 (U+FE0F) always forces
// emoji presentation. Width is decided for the complete grapheme cluster, so
// emoji ZWJ sequences, emoji skin tone modifiers, Regional Indicator flags,
// and keycap sequences each occupy the width of one presentation unit.
//
// Widths do not change grapheme cluster boundaries: the segmentation results
// are identical to those produced by the option-less functions.
//
// A WidthStrategy is safe for concurrent use because its fields cannot change
// after construction.
type WidthStrategy struct {
	// ambiguousWidth is the width assigned to East-Asian Ambiguous code
	// points. It is either 1 or 2.
	ambiguousWidth int

	// emojiWidth is the width assigned to emoji presentation units. It is
	// either 1 or 2.
	emojiWidth int
}

// WidthOption configures a [WidthStrategy]. Options are evaluated when the
// strategy is constructed; afterwards the strategy is immutable.
type WidthOption func(*WidthStrategy)

// WithAmbiguousWidth sets the monospace width of code points classified as
// East-Asian "Ambiguous". Only the values 1 and 2 are accepted. If this
// option is not provided, the default is 1, matching the package-wide
// [EastAsianAmbiguousWidth] default.
func WithAmbiguousWidth(width int) WidthOption {
	return func(strategy *WidthStrategy) {
		strategy.ambiguousWidth = width
	}
}

// WithEmojiWidth sets the monospace width of emoji presentation units
// (emoji-default Extended Pictographic code points, clusters forced to emoji
// presentation by Variation Selector-16, and Regional Indicator flag pairs).
// Only the values 1 and 2 are accepted. If this option is not provided, the
// default is 2, matching the option-less API.
func WithEmojiWidth(width int) WidthOption {
	return func(strategy *WidthStrategy) {
		strategy.emojiWidth = width
	}
}

// NewWidthStrategy returns a new immutable [WidthStrategy]. The default
// widths are 1 for East-Asian Ambiguous code points and 2 for emoji
// presentation units; the provided options override these defaults. Widths
// other than 1 or 2 cause a panic, which lets invalid configurations fail at
// construction time rather than producing invalid cell counts.
func NewWidthStrategy(options ...WidthOption) *WidthStrategy {
	strategy := &WidthStrategy{
		ambiguousWidth: 1,
		emojiWidth:     defaultEmojiPresentationWidth,
	}
	for _, option := range options {
		option(strategy)
	}
	if strategy.ambiguousWidth != 1 && strategy.ambiguousWidth != 2 {
		panic("uniseg: ambiguous width must be 1 or 2")
	}
	if strategy.emojiWidth != 1 && strategy.emojiWidth != 2 {
		panic("uniseg: emoji presentation width must be 1 or 2")
	}
	return strategy
}

// AmbiguousWidth returns the width this strategy assigns to East-Asian
// Ambiguous code points. The result is either 1 or 2.
func (s *WidthStrategy) AmbiguousWidth() int {
	return s.ambiguousWidth
}

// EmojiWidth returns the width this strategy assigns to emoji presentation
// units. The result is either 1 or 2.
func (s *WidthStrategy) EmojiWidth() int {
	return s.emojiWidth
}

// StringWidth returns the monospace width of the given string under this
// strategy, that is, the number of same-size cells occupied by the string.
// It is the strategy-aware counterpart of [StringWidth]. The calculation is
// the same one used by [WidthStrategy.NewGraphemes],
// [WidthStrategy.FirstGraphemeCluster], and
// [WidthStrategy.FirstGraphemeClusterInString], so summing the per-cluster
// widths returned by any of them yields exactly this result.
func (s *WidthStrategy) StringWidth(str string) (width int) {
	state := -1
	for len(str) > 0 {
		var clusterWidth int
		_, str, clusterWidth, state = s.FirstGraphemeClusterInString(str, state)
		width += clusterWidth
	}
	return
}

// NewGraphemes returns a grapheme cluster iterator over the given string
// which reports widths according to this strategy. It is the strategy-aware
// counterpart of [NewGraphemes]. Grapheme cluster boundaries are not affected
// by the strategy.
func (s *WidthStrategy) NewGraphemes(str string) *Graphemes {
	return NewGraphemesWithWidthStrategy(str, s)
}

// FirstGraphemeCluster returns the first grapheme cluster found in the given
// byte slice according to the rules of Unicode Standard Annex #29, along with
// the cluster's monospace width under this strategy. It is the
// strategy-aware counterpart of [FirstGraphemeCluster] and is called
// repeatedly in the same way (passing -1 for the first call and the returned
// state afterwards).
func (s *WidthStrategy) FirstGraphemeCluster(b []byte, state int) (cluster, rest []byte, width, newState int) {
	return firstGraphemeCluster(b, state, s.ambiguousWidth, s.emojiWidth)
}

// FirstGraphemeClusterInString is like [WidthStrategy.FirstGraphemeCluster]
// but its input and outputs are strings. It is the strategy-aware
// counterpart of [FirstGraphemeClusterInString].
func (s *WidthStrategy) FirstGraphemeClusterInString(str string, state int) (cluster, rest string, width, newState int) {
	return firstGraphemeClusterInString(str, state, s.ambiguousWidth, s.emojiWidth)
}
