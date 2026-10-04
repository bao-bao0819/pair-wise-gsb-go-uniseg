package uniseg

import (
	"errors"
	"sort"
	"unicode/utf8"
)

// Errors returned by [NewGraphemeIndex] and [GraphemeIndex.Replace].
var (
	// ErrInvalidOffset is returned when an edit range does not fall entirely
	// inside the text or its boundaries are not aligned with UTF-8 rune
	// boundaries.
	ErrInvalidOffset = errors.New("uniseg: byte offset is out of range or not on a UTF-8 rune boundary")

	// ErrInvalidUTF8 is returned when a text or a replacement string is not
	// valid UTF-8.
	ErrInvalidUTF8 = errors.New("uniseg: string is not valid UTF-8")
)

// GraphemeIndex holds a piece of text together with its grapheme cluster
// boundaries (as defined by [UAX #29]). It supports local, byte-range edits
// while reusing boundary information from unchanged prefixes and suffixes
// instead of rebuilding the entire boundary table.
//
// The index is safe for concurrent reads as long as no write is in progress.
// It does not support concurrent writers; [GraphemeIndex.Replace] mutates the
// index in place. Readers always observe either the state before or after a
// complete Replace call, never a partially updated one.
//
// Boundary byte offsets stored in this index, and the offsets accepted by its
// methods, are always aligned with UTF-8 rune boundaries. Edit boundaries may
// fall inside a grapheme cluster (for example between a base letter and a
// combining mark); the clusters touching the edit are simply recomputed.
//
// [UAX #29]: https://unicode.org/reports/tr29/
type GraphemeIndex struct {
	// The current text.
	text string

	// Grapheme cluster boundary byte offsets into text. It starts with 0 and
	// ends with len(text); consecutive entries delimit one grapheme cluster.
	bounds []int

	// For each entry in bounds, the grapheme parser state immediately before
	// the first rune at that boundary. A rescan resumed from such a checkpoint
	// re-processes that first rune and then continues, which exactly
	// reproduces the segmentation [StepString] produces from scratch. These
	// are the checkpoints from which a rescan after an edit is resumed. The
	// first entry is grAny.
	states []int
}

// NewGraphemeIndex creates a new grapheme cluster index for the given text.
// The text must be valid UTF-8; otherwise ErrInvalidUTF8 is returned.
func NewGraphemeIndex(text string) (*GraphemeIndex, error) {
	if !utf8.ValidString(text) {
		return nil, ErrInvalidUTF8
	}
	bounds, states := buildGraphemes(text)
	return &GraphemeIndex{
		text:   text,
		bounds: bounds,
		states: states,
	}, nil
}

// Text returns the current text held by the index.
func (g *GraphemeIndex) Text() string {
	return g.text
}

// Len returns the length of the current text in bytes.
func (g *GraphemeIndex) Len() int {
	return len(g.text)
}

// NumGraphemes returns the number of grapheme clusters in the current text.
func (g *GraphemeIndex) NumGraphemes() int {
	return len(g.bounds) - 1
}

// GraphemeBounds returns the half-open byte interval [lo, hi) of the grapheme
// cluster at the given zero-based grapheme index, so that g.Text()[lo:hi] is
// that cluster. Indices outside [0, NumGraphemes) are clamped to the nearest
// valid value.
func (g *GraphemeIndex) GraphemeBounds(index int) (lo, hi int) {
	n := g.NumGraphemes()
	if n == 0 {
		return 0, 0
	}
	if index < 0 {
		index = 0
	} else if index >= n {
		index = n - 1
	}
	return g.bounds[index], g.bounds[index+1]
}

// GraphemeAt returns the zero-based index of the grapheme cluster containing
// the given byte offset. A byte offset on a cluster boundary is considered to
// belong to the cluster starting at that boundary. An offset equal to Len (at
// the end of the text) returns NumGraphemes. Offsets are clamped to the text.
// The offset is not required to a rune boundary; any byte inside a cluster
// maps to that cluster.
func (g *GraphemeIndex) GraphemeAt(byteOffset int) int {
	if byteOffset <= 0 {
		return 0
	}
	if byteOffset >= len(g.text) {
		return g.NumGraphemes()
	}
	// The first entry strictly greater than byteOffset, minus one.
	index := sort.Search(len(g.bounds), func(i int) bool {
		return g.bounds[i] > byteOffset
	}) - 1
	if index < 0 {
		return 0
	}
	if index > g.NumGraphemes() {
		return g.NumGraphemes()
	}
	return index
}

// Slice returns the substring of the text covered by grapheme clusters in the
// half-open grapheme index range [from, to). from and to are clamped to
// [0, NumGraphemes]; if from > to after clamping, they are swapped.
func (g *GraphemeIndex) Slice(from, to int) string {
	n := g.NumGraphemes()
	from = clampIndex(from, n)
	to = clampIndex(to, n)
	if from > to {
		from, to = to, from
	}
	return g.text[g.bounds[from]:g.bounds[to]]
}

// Replace replaces text[lo:hi] with replacement. Both lo and hi must be byte
// offsets on UTF-8 rune boundaries and must satisfy 0 <= lo <= hi <= Len; they
// are allowed to fall inside a grapheme cluster. The replacement must be valid
// UTF-8. If any of these conditions is violated, the edit is rejected
// entirely and the index is left unchanged.
//
// On success it returns the byte ranges affected by the edit: oldLo and oldHi
// delimit the half-open byte range in the previous text, newLo and newHi the
// corresponding range in the updated text. All grapheme boundaries that
// changed are contained in these ranges (they are reported conservatively);
// the boundary at each endpoint is unchanged. Boundaries before oldLo keep
// their byte offsets, and boundaries at or after oldHi keep their relative
// offsets, shifted by (newHi-newLo)-(oldHi-oldLo). Upper layers can use these
// ranges to invalidate and refresh caches.
//
// Only the region that could possibly have changed is rescanned. The rescan
// starts at a valid checkpoint strictly before the edit (the beginning of the
// cluster preceding the first touched cluster) and continues through the
// unchanged suffix until the parser state converges, at which point the
// remaining old boundaries are reused and shifted.
func (g *GraphemeIndex) Replace(lo, hi int, replacement string) (oldLo, oldHi, newLo, newHi int, err error) {
	oldLo, oldHi, newLo, newHi, _, err = g.replace(lo, hi, replacement)
	return
}

// replace implements Replace and additionally reports the number of bytes the
// two rescanners consumed (old text plus new text). This is used in tests to
// verify that large stable prefixes and suffixes are reused.
func (g *GraphemeIndex) replace(lo, hi int, replacement string) (oldLo, oldHi, newLo, newHi, rescanned int, err error) {
	// Validate the edit. Illegal edits are rejected before any state changes.
	if lo < 0 || hi < lo || hi > len(g.text) {
		return 0, 0, 0, 0, 0, ErrInvalidOffset
	}
	if (lo > 0 && lo < len(g.text) && !utf8.RuneStart(g.text[lo])) ||
		(hi < len(g.text) && !utf8.RuneStart(g.text[hi])) {
		return 0, 0, 0, 0, 0, ErrInvalidOffset
	}
	if !utf8.ValidString(replacement) {
		return 0, 0, 0, 0, 0, ErrInvalidUTF8
	}

	oldText := g.text
	oldBounds := g.bounds
	oldStates := g.states

	// Choose the rescan checkpoint: the cluster boundary at the start of the
	// cluster immediately preceding the first cluster that the edit touches.
	// Backing off by one whole cluster (rather than a fixed number of bytes)
	// guarantees the checkpoint is a stable cluster boundary whose parser
	// state remains valid after the edit. This matters, for example, when a
	// ZWJ is inserted at a boundary: it attaches to the preceding cluster.
	restartIdx := sort.SearchInts(oldBounds, lo) - 1
	if restartIdx < 0 {
		restartIdx = 0
	}
	restart := oldBounds[restartIdx]
	restartState := oldStates[restartIdx]

	newText := oldText[:lo] + replacement + oldText[hi:]
	delta := len(replacement) - (hi - lo)
	newEditEnd := lo + len(replacement)

	// Rescan from the checkpoint with two independent scanners over the old
	// and new text. Once newPos-oldPos == delta they refer to the same suffix
	// byte. We merge-walk both scanners until their parser states converge at
	// a boundary inside the shared, unchanged suffix.
	oldPos, newPos := restart, restart
	oldState, newState := restartState, restartState

	// New boundaries (and checkpoints) strictly after the restart boundary.
	freshBounds := make([]int, 0, 8)
	freshStates := make([]int, 0, 8)

	oldSteps := 0
	converged := false
loop:
	for {
		// Both positions describe the same suffix byte, both are past the
		// edited regions, and the parser states agree: the segmentation of
		// everything that follows is identical, so the old suffix can be
		// reused and shifted.
		if newPos-oldPos == delta && oldPos >= hi && newPos >= newEditEnd && oldState == newState {
			converged = true
			break
		}

		oldAtEnd := oldPos >= len(oldText)
		newAtEnd := newPos >= len(newText)
		switch {
		case oldAtEnd && newAtEnd:
			// Both texts exhausted without converging. Rebuild everything.
			break loop
		case oldAtEnd:
			newPos, newState = graphemeStep(newText, newPos, newState)
			freshBounds = append(freshBounds, newPos)
			freshStates = append(freshStates, newState)
		case newAtEnd:
			oldPos, oldState = graphemeStep(oldText, oldPos, oldState)
			oldSteps++
		case newPos-delta < oldPos:
			// The new scanner lags in shared-suffix coordinates; catch up.
			newPos, newState = graphemeStep(newText, newPos, newState)
			freshBounds = append(freshBounds, newPos)
			freshStates = append(freshStates, newState)
		case newPos-delta > oldPos:
			// The old scanner lags; catch up.
			oldPos, oldState = graphemeStep(oldText, oldPos, oldState)
			oldSteps++
		default:
			// Same suffix byte but differing parser states. Advance both
			// scanners independently and try to converge again.
			newPos, newState = graphemeStep(newText, newPos, newState)
			freshBounds = append(freshBounds, newPos)
			freshStates = append(freshStates, newState)
			oldPos, oldState = graphemeStep(oldText, oldPos, oldState)
			oldSteps++
		}
	}

	rescanned = (oldPos - restart) + (newPos - restart)

	// Assemble the new boundary table: unchanged prefix, freshly scanned
	// region, then the shifted old suffix.
	prefixEnd := restartIdx + 1
	size := prefixEnd + len(freshBounds)
	convOldIdx := restartIdx + oldSteps
	if converged {
		size += len(oldBounds) - (convOldIdx + 1)
	}
	bounds := make([]int, 0, size)
	states := make([]int, 0, size)
	bounds = append(bounds, oldBounds[:prefixEnd]...)
	states = append(states, oldStates[:prefixEnd]...)
	bounds = append(bounds, freshBounds...)
	states = append(states, freshStates...)
	if converged {
		for k := convOldIdx + 1; k < len(oldBounds); k++ {
			bounds = append(bounds, oldBounds[k]+delta)
			states = append(states, oldStates[k])
		}
	}

	// Report the affected byte ranges, conservatively.
	oldLo = restart
	newLo = restart
	if converged {
		oldHi = oldPos
		newHi = newPos
	} else {
		oldHi = len(oldText)
		newHi = len(newText)
	}

	// Commit the complete update atomically.
	g.text = newText
	g.bounds = bounds
	g.states = states

	return oldLo, oldHi, newLo, newHi, rescanned, nil
}

// buildGraphemes computes all grapheme cluster boundary byte offsets of a valid
// UTF-8 string together with the parser checkpoint state at each boundary.
func buildGraphemes(str string) (bounds, states []int) {
	bounds = []int{0}
	states = []int{grAny}
	pos, state := 0, grAny
	for pos < len(str) {
		pos, state = graphemeStep(str, pos, state)
		bounds = append(bounds, pos)
		states = append(states, state)
	}
	return
}

// graphemeStep consumes a single grapheme cluster starting at byte offset pos,
// using state as the parser state immediately before the rune at pos. It
// returns the byte offset of the next cluster boundary and the parser state
// immediately before the first rune at that boundary. pos must be a rune
// boundary and str[pos:] must be non-empty.
func graphemeStep(str string, pos int, state int) (boundary, preState int) {
	r, length := utf8.DecodeRuneInString(str[pos:])
	state, _, _ = transitionGraphemeState(state, r)
	for pos+length < len(str) {
		r, l := utf8.DecodeRuneInString(str[pos+length:])
		previous := state
		nextState, _, breaks := transitionGraphemeState(state, r)
		if breaks {
			// The boundary is before r. The checkpoint state is the state
			// after the preceding rune; re-processing r from it reproduces
			// the same boundary StepString finds.
			return pos + length, previous
		}
		state = nextState
		length += l
	}
	return len(str), state
}

// clampIndex clamps a grapheme index to [0, n].
func clampIndex(index, n int) int {
	if index < 0 {
		return 0
	}
	if index > n {
		return n
	}
	return index
}
