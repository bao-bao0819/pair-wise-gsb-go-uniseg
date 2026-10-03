package uniseg

import (
	"errors"
	"unicode/utf8"
)

// ErrGraphemesFinished is returned by [IncrementalGraphemes.Feed] and
// [IncrementalGraphemes.Finish] when they are called after the stream has been
// terminated with [IncrementalGraphemes.Finish]. Call [IncrementalGraphemes.Reset]
// to reuse the parser for a new stream.
var ErrGraphemesFinished = errors.New("uniseg: incremental grapheme parser already finished")

// GraphemeCluster is a single grapheme cluster produced by
// [IncrementalGraphemes].
type GraphemeCluster struct {
	// Bytes is the raw byte content of the grapheme cluster. It is a copy of
	// the corresponding bytes fed into the parser, so it remains valid even
	// after subsequent Feed or Reset calls. Invalid UTF-8 bytes are passed
	// through unchanged; they are never repaired.
	Bytes []byte

	// From is the absolute byte offset of the cluster's first byte relative to
	// the start of the stream (the first byte ever fed in is at offset 0).
	From int

	// To is the absolute byte offset one past the cluster's last byte. To-From
	// is always len(Bytes).
	To int

	// Width is the monospace width of the grapheme cluster, as returned by
	// [FirstGraphemeCluster].
	Width int
}

// IncrementalGraphemes is an incremental, streaming parser for Unicode
// grapheme clusters. Unlike repeatedly calling [FirstGraphemeCluster] on
// consecutive network chunks, it correctly joins clusters that span chunk
// boundaries: the UTF-8 continuation bytes of a split code point, Regional
// Indicator pairs, Extend/ZWJ sequences (including emoji ZWJ sequences), and
// Prepend characters are all carried across chunks.
//
// Bytes are delivered in arbitrary chunks via [IncrementalGraphemes.Feed].
// Feed only returns clusters whose boundary can no longer be changed by future
// input. The final, potentially unclosed cluster (and any bytes of an
// incomplete trailing UTF-8 character) is retained and is only delivered by
// [IncrementalGraphemes.Finish], which marks the real end of the text.
//
// The concatenation of the clusters produced by a sequence of Feed calls
// followed by a single Finish call is identical (content, order, byte ranges,
// and widths) to the result of parsing the entire concatenated input at once
// with [FirstGraphemeCluster].
//
// A zero value is ready to use. A parser must not be copied after first use.
type IncrementalGraphemes struct {
	// pending holds the bytes of the last, not yet closed grapheme cluster
	// plus any bytes of an incomplete trailing UTF-8 character. It does not
	// retain any bytes of clusters that have already been delivered.
	pending []byte

	// pos is the absolute stream offset of pending's first retained byte.
	pos int

	// state is the grapheme parser state after the last fully decoded code
	// point of the pending cluster. It is -1 before the first code point of a
	// stream.
	state int

	// decoded is the number of leading bytes of pending whose code points have
	// already been fed into the state machine. Bytes after them are either new
	// chunk bytes or bytes of an incomplete trailing UTF-8 character.
	decoded int

	// firstProp is the grapheme property of the pending cluster's first code
	// point. It is used to compute the cluster width.
	firstProp int

	// width is the accumulated width of the pending cluster.
	width int

	// finished is true after Finish was called.
	finished bool

	// started is false only for a zero value that has not received any input
	// yet.
	started bool
}

// NewIncrementalGraphemes returns a new [IncrementalGraphemes] parser. A zero
// value may be used directly instead.
func NewIncrementalGraphemes() *IncrementalGraphemes {
	return &IncrementalGraphemes{state: -1, started: true}
}

// Feed appends a chunk of bytes to the stream and returns the grapheme clusters
// that are now known to be complete. It returns an empty (or nil) slice when no
// cluster can be closed yet; the unclosed trailing cluster and the bytes of an
// incomplete trailing UTF-8 character are retained for later calls (or for
// [IncrementalGraphemes.Finish]).
//
// The returned clusters are copies and are not modified by any subsequent call
// to Feed, Finish, or Reset. Calling Feed after Finish returns
// [ErrGraphemesFinished]; call [IncrementalGraphemes.Reset] first.
func (g *IncrementalGraphemes) Feed(chunk []byte) ([]GraphemeCluster, error) {
	if g.finished {
		return nil, ErrGraphemesFinished
	}
	return g.consume(chunk, false), nil
}

// Finish marks the end of the stream and returns all remaining clusters. Any
// bytes of an incomplete trailing UTF-8 character are decoded the same way
// [FirstGraphemeCluster] decodes invalid UTF-8 (each invalid byte becomes a
// RuneError); the raw bytes are preserved in the returned clusters.
//
// After Finish, the parser is unusable until [IncrementalGraphemes.Reset] is
// called; further calls to Finish or Feed return [ErrGraphemesFinished].
func (g *IncrementalGraphemes) Finish() ([]GraphemeCluster, error) {
	if g.finished {
		return nil, ErrGraphemesFinished
	}
	g.finished = true
	clusters := g.consume(nil, true)

	// Release retained memory.
	g.pending = nil

	return clusters, nil
}

// Reset puts the parser back into its initial state so it can be reused for a
// new stream. Clusters returned by earlier Feed or Finish calls are copies and
// are not affected.
func (g *IncrementalGraphemes) Reset() {
	g.pending = nil
	g.decoded = 0
	g.pos = 0
	g.state = -1
	g.firstProp = 0
	g.width = 0
	g.finished = false
	g.started = true
}

// consume appends chunk to the pending bytes and extracts complete clusters. If
// atEnd is true, an incomplete trailing UTF-8 character is force-decoded and
// the final cluster (if any) is delivered; otherwise such bytes are retained.
func (g *IncrementalGraphemes) consume(chunk []byte, atEnd bool) []GraphemeCluster {
	if !g.started {
		g.state = -1
		g.started = true
	}
	if len(chunk) > 0 {
		g.pending = append(g.pending, chunk...)
	}

	var clusters []GraphemeCluster

	// i is the position up to which pending has been decoded into state.
	// clusterStart is the position of the first byte of the pending cluster;
	// bytes before it belong to a cluster delivered during this call.
	i := g.decoded
	clusterStart := 0

	// emit closes the pending cluster ending at byte index end.
	emit := func(end int) {
		from := g.pos + clusterStart
		content := make([]byte, end-clusterStart)
		copy(content, g.pending[clusterStart:end])
		clusters = append(clusters, GraphemeCluster{
			Bytes: content,
			From:  from,
			To:    from + len(content),
			Width: g.width,
		})
		clusterStart = end
		g.width = 0
		g.firstProp = 0
		g.state = -1
	}

	// Decode code points. The trailing bytes of an incomplete but potentially
	// valid UTF-8 character are retained for future chunks; a prefix that can
	// no longer become valid is decoded byte by byte as RuneError, exactly like
	// [FirstGraphemeCluster] does. At the real end of the text, retained bytes
	// are force-decoded the same way.
	for {
		if len(g.pending[i:]) == 0 {
			break
		}
		var (
			r    rune
			size int
		)
		if atEnd {
			// At the real end of the text every byte is consumed, exactly as
			// FirstGraphemeCluster does with utf8.DecodeRune.
			r, size = utf8.DecodeRune(g.pending[i:])
		} else {
			var incomplete bool
			r, size, incomplete = decodePendingRune(g.pending[i:])
			if incomplete {
				break
			}
		}

		if g.state == -1 {
			// First code point of a new cluster.
			g.state, g.firstProp, _ = transitionGraphemeState(-1, r)
			g.width = runeWidth(r, g.firstProp)
		} else {
			var (
				prop     int
				boundary bool
			)
			g.state, prop, boundary = transitionGraphemeState(g.state&maskGraphemeState, r)
			if boundary {
				// The pending cluster ends before this code point. Deliver it.
				emit(i)

				// Start a new cluster with this code point.
				g.state, g.firstProp, _ = transitionGraphemeState(-1, r)
				g.width = runeWidth(r, g.firstProp)
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
			}
		}

		i += size

		if !atEnd {
			// A cluster ending after a CR/LF/control code point can never be
			// extended by future input (GB4/GB5). Deliver it eagerly so callers
			// do not have to wait for the next chunk.
			if g.state&maskGraphemeState == grControlLF {
				emit(i)
			}
		}
	}

	// Discard bytes of clusters already delivered while retaining the pending
	// cluster and any bytes of an incomplete trailing UTF-8 character.
	if clusterStart > 0 {
		retained := copy(g.pending, g.pending[clusterStart:])
		g.pending = g.pending[:retained]
		g.pos += clusterStart
		i -= clusterStart
		clusterStart = 0
	}

	if atEnd {
		// GB2: everything still retained is the final cluster.
		if len(g.pending) > 0 {
			end := len(g.pending)
			emit(end)
			g.pos += end
		}
		g.pending = g.pending[:0]
		i = 0
	}

	g.decoded = i

	return clusters
}

// decodePendingRune decodes the first UTF-8 encoding in p. For a complete
// encoding it behaves exactly like utf8.DecodeRune (invalid encodings yield
// utf8.RuneError with size 1). It additionally returns incomplete=true when p
// contains only a proper, still-extendable prefix of a valid encoding; in that
// case size is the number of prefix bytes that must be retained until more
// input arrives. A prefix that can no longer become valid is reported as a
// one-byte error rather than retained.
func decodePendingRune(p []byte) (r rune, size int, incomplete bool) {
	n := len(p)
	if n == 0 {
		return utf8.RuneError, 0, false
	}

	// ASCII fast path. (Bytes 0x80-0xBF are stray continuation bytes and are
	// rejected as invalid leading bytes below.)
	if p[0] < 0x80 {
		return rune(p[0]), 1, false
	}

	// Determine the expected length from the leading byte, as in the standard
	// library's decodeRune.
	first := p[0]
	var accept struct{ lo, hi byte }
	switch {
	case first < 0xC2:
		// 0x80-0xBF are stray continuation bytes; 0xC0/0xC1 are overlong.
		return utf8.RuneError, 1, false
	case first <= 0xDF:
		size, accept.lo, accept.hi = 2, 0x80, 0xBF
	case first <= 0xEF:
		size = 3
		if first == 0xE0 {
			accept.lo, accept.hi = 0xA0, 0xBF
		} else if first == 0xED {
			accept.lo, accept.hi = 0x80, 0x9F
		} else {
			accept.lo, accept.hi = 0x80, 0xBF
		}
	case first <= 0xF4:
		size = 4
		if first == 0xF0 {
			accept.lo, accept.hi = 0x90, 0xBF
		} else if first == 0xF4 {
			accept.lo, accept.hi = 0x80, 0x8F
		} else {
			accept.lo, accept.hi = 0x80, 0xBF
		}
	default:
		return utf8.RuneError, 1, false
	}

	// A proper, still-extendable prefix: retain it unless the bytes already
	// present rule out a valid completion.
	if n < size {
		if n > 1 && (p[1] < accept.lo || accept.hi < p[1]) {
			return utf8.RuneError, 1, false
		}
		for k := 2; k < n; k++ {
			if p[k] < 0x80 || 0xBF < p[k] {
				return utf8.RuneError, 1, false
			}
		}
		return utf8.RuneError, n, true
	}

	// All bytes are available; delegate to the standard decoder so complete
	// encodings behave identically to FirstGraphemeCluster.
	r, s := utf8.DecodeRune(p)
	return r, s, false
}
