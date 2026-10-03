package uniseg

import (
	"errors"
	"unicode/utf8"
)

// ErrIncrementalFinished is returned by [IncrementalGraphemes.Feed] or
// [IncrementalGraphemes.Finish] when they are called after the stream has been
// terminated with [IncrementalGraphemes.Finish]. Call
// [IncrementalGraphemes.Reset] to reuse the parser for a new stream.
var ErrIncrementalFinished = errors.New("uniseg: incremental grapheme parser was already finished")

// GraphemeCluster is a single grapheme cluster produced by
// [IncrementalGraphemes].
//
// Bytes are the raw bytes of the cluster. Invalid UTF-8 input is passed
// through byte for byte exactly as in [FirstGraphemeCluster]; no bytes are
// repaired or replaced. The slice is owned by the caller and is not modified
// by subsequent [IncrementalGraphemes.Feed] or [IncrementalGraphemes.Reset]
// calls.
//
// From and To are the cluster's absolute byte positions in the concatenation
// of all data passed to [IncrementalGraphemes.Feed]: From indexes the first
// byte of the cluster and To is the position immediately after its last byte,
// so Bytes equals the concatenated input in the interval [From, To).
//
// Width is the monospace width of the cluster, using the same calculation as
// [FirstGraphemeCluster].
type GraphemeCluster struct {
	// Bytes contains the raw bytes of the grapheme cluster.
	Bytes []byte

	// From is the absolute byte offset of the cluster's first byte.
	From int

	// To is the absolute byte offset immediately after the cluster's last
	// byte.
	To int

	// Width is the monospace width of the cluster.
	Width int
}

// IncrementalGraphemes is an incremental (streaming) parser for grapheme
// clusters, as defined in [Unicode Standard Annex #29, Grapheme Cluster
// Boundaries]. Unlike repeatedly calling [FirstGraphemeCluster] on individual
// network blocks, it carries the parser state across blocks, so a grapheme
// cluster (or a UTF-8 encoding) split by a block boundary is not broken.
//
// Data is handed to the parser with [IncrementalGraphemes.Feed]. Feed only
// returns clusters that are closed, i.e. clusters whose boundaries cannot
// change when more data arrives. The final, still open cluster is held back
// (as are trailing bytes that do not yet make up a complete UTF-8 encoding)
// until the stream is closed with [IncrementalGraphemes.Finish]. The
// boundaries preserved across blocks include paired Regional Indicators
// (flags), Extend and SpacingMark sequences, Prepend characters, and emoji
// ZWJ sequences.
//
// For any input, the clusters, their byte ranges, and their widths are
// identical to splitting the concatenation of all blocks in one go with
// [FirstGraphemeCluster]. Invalid UTF-8 is handled the same way as in the
// byte-based functions and is never modified.
//
// The parser does not retain the full input. It only keeps the bytes of the
// one currently open cluster (which may be arbitrarily long) and at most
// three trailing bytes of a possibly incomplete UTF-8 encoding.
//
// The zero value is ready to use. A parser must not be copied while in use.
//
// [Unicode Standard Annex #29, Grapheme Cluster Boundaries]: http://unicode.org/reports/tr29/#Grapheme_Cluster_Boundaries
type IncrementalGraphemes struct {
	// cluster holds the bytes of the currently open cluster, all of which
	// decode to complete runes.
	cluster []byte

	// processed is the number of bytes in cluster already consumed by the
	// state machine. It equals len(cluster) between calls.
	processed int

	// tail holds 0 to 3 trailing bytes which may be the beginning of a
	// UTF-8 encoding completed by a later call.
	tail []byte

	// state is the current grapheme parser state (one of the gr* values).
	state int

	// firstProp is the grapheme property of the first rune of the open
	// cluster.
	firstProp int

	// width is the accumulated width of the open cluster.
	width int

	// base is the absolute byte offset of cluster[0].
	base int

	// finished is true after Finish was called.
	finished bool
}

// NewIncrementalGraphemes returns a new [IncrementalGraphemes] parser. The zero
// value is also ready to use.
func NewIncrementalGraphemes() *IncrementalGraphemes {
	return &IncrementalGraphemes{state: grAny}
}

// Feed hands a block of input data to the parser and returns all grapheme
// clusters that are closed at the end of the block, in order. A cluster is
// only returned once no future data can extend or invalidate it: the last,
// still open cluster and any trailing bytes that do not yet constitute a
// complete UTF-8 character are retained. If no cluster can be closed, nil is
// returned.
//
// The returned byte slices are independent copies; retaining them across
// later Feed calls is safe. The data slice is not retained after Feed
// returns.
//
// After [IncrementalGraphemes.Finish] has been called, Feed returns
// [ErrIncrementalFinished] without changing the parser.
func (g *IncrementalGraphemes) Feed(data []byte) ([]GraphemeCluster, error) {
	if g.finished {
		return nil, ErrIncrementalFinished
	}

	// Prefix any previously held partial bytes: logically they precede the
	// new block.
	chunk := make([]byte, 0, len(g.tail)+len(data))
	chunk = append(chunk, g.tail...)
	chunk = append(chunk, data...)
	g.tail = nil

	// Hold back a possibly truncated final UTF-8 encoding.
	if n := trailingPartialUTF8(chunk); n > 0 {
		tail := make([]byte, n)
		copy(tail, chunk[len(chunk)-n:])
		g.tail = tail
		chunk = chunk[:len(chunk)-n]
	}

	g.cluster = append(g.cluster, chunk...)

	return g.parse(), nil
}

// Finish marks the end of the stream. Any retained open cluster, including
// previously held trailing bytes, is closed at the true end of the text and
// returned. After Finish, the parser cannot accept more data until
// [IncrementalGraphemes.Reset] is called. Calling Finish on an empty stream
// returns no clusters. A second call returns [ErrIncrementalFinished].
func (g *IncrementalGraphemes) Finish() ([]GraphemeCluster, error) {
	if g.finished {
		return nil, ErrIncrementalFinished
	}
	g.finished = true

	// At the true end of the text, every held byte is decoded as-is, in the
	// same way FirstGraphemeCluster would decode it.
	g.cluster = append(g.cluster, g.tail...)
	g.tail = nil

	out := g.parse()

	// Close the final open cluster.
	if len(g.cluster) > 0 {
		out = append(out, GraphemeCluster{
			Bytes: copyBytes(g.cluster),
			From:  g.base,
			To:    g.base + len(g.cluster),
			Width: g.width,
		})
		g.base += len(g.cluster)
		g.cluster = nil
		g.processed = 0
	}

	return out, nil
}

// Reset returns the parser to its initial state so it can be reused for a new
// stream. Clusters already returned to the caller are not affected.
func (g *IncrementalGraphemes) Reset() {
	*g = IncrementalGraphemes{state: grAny}
}

// parse advances the state machine over the complete runes appended to
// cluster, emitting every cluster that ends at a confirmed boundary. The
// bytes following the last confirmed boundary remain in cluster.
func (g *IncrementalGraphemes) parse() []GraphemeCluster {
	var out []GraphemeCluster

	b := g.cluster
	p := g.processed
	for p < len(b) {
		r, size := utf8.DecodeRune(b[p:])
		newState, prop, boundary := transitionGraphemeState(g.state, r)

		if p == 0 {
			// First rune of a cluster (the start of the stream or the rune
			// following a boundary). The reported boundary is implied.
			g.state = newState
			g.firstProp = prop
			g.width = runeWidth(r, prop)
		} else if boundary {
			// The cluster b[:p] is final. Copy it so it can never be touched
			// by later operations.
			out = append(out, GraphemeCluster{
				Bytes: copyBytes(b[:p]),
				From:  g.base,
				To:    g.base + p,
				Width: g.width,
			})

			// Compact the buffer to the new open cluster, using a fresh
			// backing array so emitted clusters keep their storage.
			rest := b[p:]
			next := make([]byte, len(rest))
			copy(next, rest)
			g.base += p
			b = next
			g.cluster = next
			p = 0

			g.state = newState
			g.firstProp = prop
			g.width = runeWidth(r, prop)
			p += size
			g.processed = p
			continue
		} else {
			if g.firstProp == prExtendedPictographic {
				if r == vs15 {
					g.width = 1
				} else if r == vs16 {
					g.width = 2
				}
			} else if g.firstProp != prRegionalIndicator && g.firstProp != prL {
				g.width += runeWidth(r, prop)
			}
			g.state = newState
		}

		p += size
		g.processed = p
	}

	g.cluster = b
	g.processed = p

	return out
}

// trailingPartialUTF8 returns the number of bytes at the end of b which may be
// the beginning of an incomplete UTF-8 encoding and which must therefore be
// retained until more bytes are available. It returns 0 if the final encoding
// is complete, including encodings that are invalid: invalid bytes decode to
// utf8.RuneError of size 1 regardless of what follows and must not be held.
func trailingPartialUTF8(b []byte) int {
	n := len(b)
	if n == 0 {
		return 0
	}

	// Walk back over continuation bytes (at most 3) to the leading byte of
	// the final encoding.
	k := n - 1
	limit := n - utf8.UTFMax
	if limit < 0 {
		limit = 0
	}
	for k > limit && !utf8.RuneStart(b[k]) {
		k--
	}

	if utf8.FullRune(b[k:]) {
		return 0
	}
	return n - k
}

// copyBytes returns an independent copy of b.
func copyBytes(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
