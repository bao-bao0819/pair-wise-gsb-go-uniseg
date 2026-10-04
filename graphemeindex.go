package uniseg

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

// Errors returned by [NewGraphemeIndex] and [GraphemeIndex.Replace].
var (
	// ErrInvalidUTF8 is returned if the initial text or the replacement text
	// is not valid UTF-8.
	ErrInvalidUTF8 = errors.New("uniseg: text must be valid UTF-8")

	// ErrNotRuneBoundary is returned if the start or end position of an edit
	// does not lie on a UTF-8 rune boundary of the current text.
	ErrNotRuneBoundary = errors.New("uniseg: edit range must lie on UTF-8 rune boundaries")

	// ErrOutOfRange is returned if an edit range lies outside the current
	// text or if its start position is greater than its end position.
	ErrOutOfRange = errors.New("uniseg: edit range out of bounds")
)

// GraphemeIndex holds a text together with the byte positions of its grapheme
// cluster boundaries. It supports efficient incremental updates: when a byte
// range of the text is replaced via [GraphemeIndex.Replace], only the region
// around the edit is re-scanned while the boundaries of the unchanged prefix
// and suffix are reused.
//
// Re-scanning starts at the last grapheme cluster boundary before the edit
// (a checkpoint whose parser state is known to be unaffected by the edit) and
// continues past the edit until the parser state converges with the state
// recorded at a boundary of the unchanged suffix. Only then are the remaining
// old boundaries reused (shifted by the edit's byte delta). No fixed number of
// look-back or look-ahead bytes is used: for example, inserting a Regional
// Indicator in front of a long run of Regional Indicators may change the
// pairing of the entire run, in which case the re-scan correctly extends over
// the whole run.
//
// A GraphemeIndex is not safe for concurrent writes. Any read observes the
// state after a complete update.
type GraphemeIndex struct {
	// The current text.
	text string

	// The byte offsets of all grapheme cluster boundaries, in ascending
	// order. bounds[0] is always 0 and bounds[len(bounds)-1] is always
	// len(text). The i-th grapheme cluster is text[bounds[i]:bounds[i+1]].
	bounds []int

	// The grapheme cluster parser state at each boundary, i.e. states[i] is
	// the state with which parsing of text[bounds[i]:] continues. states[0]
	// is -1 (the initial state).
	states []int

	// The number of bytes of the new text that were actually re-scanned
	// during the last call to Replace (or during construction).
	rescanned int
}

// NewGraphemeIndex returns a new grapheme cluster index for the given string.
// The string must be valid UTF-8, otherwise ErrInvalidUTF8 is returned.
func NewGraphemeIndex(s string) (*GraphemeIndex, error) {
	if !utf8.ValidString(s) {
		return nil, ErrInvalidUTF8
	}
	g := &GraphemeIndex{
		text:   s,
		bounds: []int{0},
		states: []int{-1},
	}
	state := -1
	rest := s
	pos := 0
	for len(rest) > 0 {
		var cluster string
		cluster, rest, _, state = FirstGraphemeClusterInString(rest, state)
		pos += len(cluster)
		g.bounds = append(g.bounds, pos)
		g.states = append(g.states, state)
	}
	g.rescanned = len(s)
	return g, nil
}

// isRuneBoundary reports whether position i lies on a rune boundary of s,
// assuming s is valid UTF-8 and 0 <= i <= len(s).
func isRuneBoundary(s string, i int) bool {
	return i <= 0 || i >= len(s) || s[i]&0xc0 != 0x80
}

// Replace substitutes the byte range text[from:to] of the current text with
// the given replacement string and updates the grapheme cluster boundaries
// incrementally.
//
// The positions "from" and "to" must lie on UTF-8 rune boundaries of the
// current text (they may lie inside a grapheme cluster) and the replacement
// string must be valid UTF-8. If any of these conditions is violated, the
// entire edit is rejected: an error is returned and the index remains
// unchanged.
//
// The four return values describe the affected byte range in the old text
// (oldFrom, oldTo) and in the new text (newFrom, newTo), which is useful for
// invalidating caches. The ranges may be larger than strictly necessary but
// are guaranteed to contain all boundary changes. Specifically, grapheme
// cluster boundaries up to oldFrom (== newFrom) are identical in the old and
// the new text, and boundaries from oldTo (== newTo shifted by the edit's
// byte delta) onward are the same boundaries, merely shifted by the delta.
// Note that if the edit changes the length of the text, the absolute
// positions of all boundaries in the unchanged suffix shift accordingly.
func (g *GraphemeIndex) Replace(from, to int, repl string) (oldFrom, oldTo, newFrom, newTo int, err error) {
	if from < 0 || to < from || to > len(g.text) {
		return 0, 0, 0, 0, ErrOutOfRange
	}
	if !isRuneBoundary(g.text, from) || !isRuneBoundary(g.text, to) {
		return 0, 0, 0, 0, ErrNotRuneBoundary
	}
	if !utf8.ValidString(repl) {
		return 0, 0, 0, 0, ErrInvalidUTF8
	}

	oldLen := len(g.text)
	delta := len(repl) - (to - from)

	// Determine the re-scan start: the last boundary strictly before "from"
	// (or the start of the text). The parser state at this checkpoint is
	// unaffected by the edit. We cannot start at a boundary exactly at
	// "from" because the state stored there encodes the property of the old
	// rune at "from", which the edit may change.
	i := sort.Search(len(g.bounds), func(k int) bool { return g.bounds[k] >= from })
	if i > 0 {
		i--
	}
	start := g.bounds[i]
	state := g.states[i]

	// Assemble the new text.
	var sb strings.Builder
	sb.Grow(oldLen + delta)
	sb.WriteString(g.text[:from])
	sb.WriteString(repl)
	sb.WriteString(g.text[to:])
	newText := sb.String()

	// The boundaries before the re-scan start are reused as they are.
	bounds := make([]int, i+1, len(g.bounds)+1)
	copy(bounds, g.bounds[:i+1])
	states := make([]int, i+1, len(g.states)+1)
	copy(states, g.states[:i+1])

	// Re-scan from the checkpoint until the parser state converges with the
	// state recorded at a boundary of the unchanged suffix. Because the
	// parser is deterministic, equal states in front of identical text imply
	// identical boundaries from that point on, so the remaining old
	// boundaries can be reused (shifted by delta).
	pos := start
	rest := newText[start:]
	converged := false
	convOld := 0
	for len(rest) > 0 {
		var cluster string
		cluster, rest, _, state = FirstGraphemeClusterInString(rest, state)
		pos += len(cluster)
		bounds = append(bounds, pos)
		states = append(states, state)

		c := pos - delta // Position of the new boundary in old coordinates.
		if c >= to {
			j := sort.Search(len(g.bounds), func(k int) bool { return g.bounds[k] >= c })
			if j < len(g.bounds) && g.bounds[j] == c && g.states[j] == state {
				converged = true
				convOld = c
				for k := j + 1; k < len(g.bounds); k++ {
					bounds = append(bounds, g.bounds[k]+delta)
					states = append(states, g.states[k])
				}
				break
			}
		}
	}

	g.text = newText
	g.bounds = bounds
	g.states = states
	g.rescanned = pos - start

	oldFrom, newFrom = start, start
	if converged {
		oldTo, newTo = convOld, pos
	} else {
		oldTo, newTo = oldLen, pos
	}
	return oldFrom, oldTo, newFrom, newTo, nil
}

// String returns the current text.
func (g *GraphemeIndex) String() string {
	return g.text
}

// Len returns the length of the current text in bytes.
func (g *GraphemeIndex) Len() int {
	return len(g.text)
}

// GraphemeCount returns the number of grapheme clusters in the current text.
func (g *GraphemeIndex) GraphemeCount() int {
	return len(g.bounds) - 1
}

// GraphemeOffset returns the byte offset of the start of the i-th grapheme
// cluster. For i == GraphemeCount(), it returns the length of the text in
// bytes. It panics if i is out of range.
func (g *GraphemeIndex) GraphemeOffset(i int) int {
	return g.bounds[i]
}

// GraphemeRange returns the byte offsets of the start and the end of the i-th
// grapheme cluster, i.e. the cluster is text[from:to]. It panics if i is out
// of range.
func (g *GraphemeIndex) GraphemeRange(i int) (from, to int) {
	return g.bounds[i], g.bounds[i+1]
}

// GraphemeAt returns the index of the grapheme cluster containing the given
// byte offset, i.e. the index i for which GraphemeOffset(i) <= offset <
// GraphemeOffset(i+1). For offset == Len(), it returns GraphemeCount(). For
// offsets outside the text, it returns -1.
func (g *GraphemeIndex) GraphemeAt(offset int) int {
	if offset < 0 || offset > len(g.text) {
		return -1
	}
	return sort.Search(len(g.bounds), func(k int) bool { return g.bounds[k] > offset }) - 1
}

// RescannedBytes returns the number of bytes of the new text that were
// actually re-scanned during the last call to Replace (or during the
// construction of the index). For small edits in large texts this is
// typically much smaller than the text length, demonstrating that the
// unchanged prefix and suffix were reused. For pathological edits (such as
// changing the Regional Indicator parity of a long flag sequence) it may
// cover the entire text.
func (g *GraphemeIndex) RescannedBytes() int {
	return g.rescanned
}
