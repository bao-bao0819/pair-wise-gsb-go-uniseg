package uniseg

import (
	"unicode"
	"unicode/utf8"
)

// Identifier token classes as returned by [IdentifierToken.Class].
const (
	// IdentifierWord is an identifier sub-word: a maximal run of letters and
	// decimal digits within an identifier, after camelCase, acronym, and
	// letter/digit boundaries have been applied. The bytes are returned
	// verbatim, without normalization or case folding.
	IdentifierWord = iota

	// IdentifierSeparator is a maximal run of underscore characters ("_")
	// inside an identifier.
	IdentifierSeparator

	// IdentifierNonWord is a segment of text outside of an identifier. The
	// non-identifier text is segmented with the standard UAX #29 word
	// boundaries (see [FirstWord]); this default Unicode behavior is not
	// modified.
	IdentifierNonWord
)

// Identifier split reasons as returned by [IdentifierToken.Reason]. A reason
// describes the boundary immediately preceding the segment, i.e. the boundary
// between the segment and the segment before it.
const (
	// IdentifierStart is the reason of the first segment, which has no
	// boundary preceding it.
	IdentifierStart = iota

	// IdentifierCamelCase is a boundary between a lower case letter and an
	// upper case letter, as in "camelCase" ("camel", "Case").
	IdentifierCamelCase

	// IdentifierAcronym is a boundary between a run of upper case letters and
	// a following upper/lower case pair. It is placed before the last upper
	// case letter, as in "XMLReader" ("XML", "Reader") and "HTTPServer"
	// ("HTTP", "Server").
	IdentifierAcronym

	// IdentifierLetterDigit is a boundary between a letter and a decimal
	// digit, in either direction, as in "utf8" ("utf", "8") and "v2" ("v",
	// "2").
	IdentifierLetterDigit

	// IdentifierUnderscore is a boundary adjacent to a maximal run of
	// underscore characters ("_"). The underscores themselves are returned as
	// [IdentifierSeparator] segments.
	IdentifierUnderscore

	// IdentifierClassChange is a boundary between identifier characters (a
	// letter, decimal digit, combining mark, or underscore) and characters
	// that do not participate in identifiers, in either direction.
	IdentifierClassChange

	// IdentifierWordBoundary is a boundary inside non-identifier text that is
	// a standard UAX #29 word boundary.
	IdentifierWordBoundary
)

// IdentifierToken is one segment produced by [IdentifierTokens]. Consecutive
// tokens cover the entire input in order and without gaps: for two consecutive
// tokens t1 and t2, t1.End() == t2.Offset.
type IdentifierToken struct {
	// Offset is the byte offset of the segment's first byte relative to the
	// start of the original input.
	Offset int

	// length is the segment's length in bytes.
	length int

	// Class is one of [IdentifierWord], [IdentifierSeparator], or
	// [IdentifierNonWord].
	Class int

	// Reason is the reason for the boundary preceding the segment. It is one
	// of the Identifier* constants. The first segment's reason is
	// [IdentifierStart].
	Reason int
}

// ByteLength returns the length of the segment in bytes.
func (t IdentifierToken) ByteLength() int {
	return t.length
}

// End returns the byte offset immediately after the segment. The segment's
// bytes are input[t.Offset:t.End()].
func (t IdentifierToken) End() int {
	return t.Offset + t.length
}

// IdentifierTokens is an iterator over identifier-oriented text segments. It
// provides a tokenization mode that is independent of [FirstWord]; the
// standard UAX #29 word segmentation rules are not modified and remain
// available unchanged.
//
// An identifier is a maximal run of "identifier characters": Unicode letters,
// decimal digits (general category Nd), combining marks (general categories
// Mn, Mc, Me), and underscores ("_"). Grapheme clusters are used as the
// atomic unit, so combining marks always stay attached to the cluster they
// belong to; an isolated combining mark counts as an identifier character as
// well.
//
// Within an identifier, maximal runs of underscores are returned as
// [IdentifierSeparator] segments, and the remaining letters and digits are
// split into [IdentifierWord] sub-words at the following boundaries:
//
//   - between a letter and a decimal digit in either direction;
//   - between a lower case letter and an upper case letter;
//   - between a run of upper case letters and a following upper/lower case
//     pair, with the split placed before the last upper case letter, so
//     "XMLReader" splits into "XML" and "Reader".
//
// The case of a grapheme cluster is determined by its first letter with a
// case property (upper, lower, or title case); combining marks attached to it
// never affect it. Scripts without case (such as CJK) do not create
// boundaries on their own, so "中文123abc" splits into "中文", "123", "abc",
// while cased scripts such as Greek follow the same lower/upper rules as
// Latin, so "αβγΔ" splits into "αβγ" and "Δ".
//
// Text outside of identifiers is segmented into [IdentifierNonWord] segments
// using the standard UAX #29 word boundaries. No normalization or case
// folding is applied; all segments retain their original bytes.
//
// Construct an iterator with [NewIdentifierTokens] or
// [NewIdentifierTokensFromBytes], then call [IdentifierTokens.Next] in a loop.
// The byte and the string entry points produce identical tokens (same byte
// offsets, classes, and reasons) for identical text.
type IdentifierTokens struct {
	// The input; exactly one of the two is set.
	str string
	b   []byte

	// The grapheme clusters of the input, produced by a single lazy streaming
	// pre-scan on the first call to [IdentifierTokens.Next].
	clusters []idCluster
	scanned  bool

	// Index of the next cluster to turn into segments.
	index int

	// Segments produced ahead of time while draining one region.
	queue []IdentifierToken
	head  int
}

// NewIdentifierTokens returns a new [IdentifierTokens] iterator over the given
// string. Byte offsets in the produced tokens are relative to "str".
func NewIdentifierTokens(str string) *IdentifierTokens {
	return &IdentifierTokens{str: str}
}

// NewIdentifierTokensFromBytes returns a new [IdentifierTokens] iterator over
// the given byte slice. Byte offsets in the produced tokens are relative to
// "b". The produced tokens are identical (same offsets, classes, and reasons)
// to those produced by [NewIdentifierTokens] for the same text.
func NewIdentifierTokensFromBytes(b []byte) *IdentifierTokens {
	return &IdentifierTokens{b: b}
}

// Reset restores the iterator to its initial state, so the input can be
// iterated over again.
func (it *IdentifierTokens) Reset() {
	it.clusters = nil
	it.scanned = false
	it.index = 0
	it.queue = it.queue[:0]
	it.head = 0
}

// Next advances the iterator to the next segment and returns false when all
// segments have been consumed. This function must be called before the first
// segment is accessed.
func (it *IdentifierTokens) Next() bool {
	if it.head < len(it.queue) {
		it.head++
		if it.head < len(it.queue) {
			return true
		}
	}
	it.queue = it.queue[:0]
	it.head = 0

	if !it.scanned {
		it.scan()
	}
	if it.index >= len(it.clusters) {
		return false
	}

	if it.clusters[it.index].kind != idKindOther {
		it.drainIdentifierRegion()
	} else {
		it.drainNonIdentifierRegion()
	}
	return len(it.queue) > 0
}

// Token returns the current segment. It is only valid after a successful call
// to [IdentifierTokens.Next].
func (it *IdentifierTokens) Token() IdentifierToken {
	if it.head >= len(it.queue) {
		return IdentifierToken{}
	}
	return it.queue[it.head]
}

// Str returns the current segment as a string.
func (it *IdentifierTokens) Str() string {
	if it.head >= len(it.queue) {
		return ""
	}
	t := it.queue[it.head]
	if it.b != nil {
		return string(it.b[t.Offset:t.End()])
	}
	return it.str[t.Offset:t.End()]
}

// Bytes returns the current segment as a byte slice. For iterators created
// with [NewIdentifierTokensFromBytes], the returned slice shares the backing
// array of the original input.
func (it *IdentifierTokens) Bytes() []byte {
	if it.head >= len(it.queue) {
		return nil
	}
	t := it.queue[it.head]
	if it.b != nil {
		return it.b[t.Offset:t.End()]
	}
	return []byte(it.str[t.Offset:t.End()])
}

// Positions returns the byte interval of the current segment relative to the
// original input: the first value indexes the first byte, the second is the
// first byte after the segment.
func (it *IdentifierTokens) Positions() (int, int) {
	if it.head >= len(it.queue) {
		return 0, 0
	}
	t := it.queue[it.head]
	return t.Offset, t.End()
}

// Grapheme cluster classification.
const (
	idKindOther = iota
	idKindUnderscore
	idKindDigit
	idKindLetter
)

// Case attributes of a grapheme cluster.
const (
	idCaseNone  = -1
	idCaseLower = 0
	idCaseUpper = 1
	idCaseTitle = 2
)

// idCluster describes one grapheme cluster of the input.
type idCluster struct {
	start, end int  // byte range relative to the original input
	kind       int  // one of idKind*
	cased      int  // one of idCase*
	wordBreak  bool // UAX #29 word boundary between this cluster and the next one
}

func (it *IdentifierTokens) inputLength() int {
	if it.b != nil {
		return len(it.b)
	}
	return len(it.str)
}

// enqueue appends a segment spanning [start, end).
func (it *IdentifierTokens) enqueue(start, end, class, reason int) {
	it.queue = append(it.queue, IdentifierToken{
		Offset: start,
		length: end - start,
		Class:  class,
		Reason: reason,
	})
}

// scan performs a single streaming pass over the input, mirroring the
// [FirstGraphemeCluster]/[FirstWord] parsers, and records every grapheme
// cluster with its classification and the UAX #29 word boundary between it
// and the following cluster.
//
// The word state machine is advanced once per rune (so its lookahead for
// apostrophes, decimal points, and similar rules is identical to [FirstWord]),
// but a word boundary only takes effect at a grapheme cluster boundary, in
// accordance with UAX #29's interaction with grapheme clusters.
func (it *IdentifierTokens) scan() {
	it.scanned = true
	n := it.inputLength()
	if n == 0 {
		return
	}

	wordState := -1
	graphemeState := -1

	pos := 0
	r, size := it.decodeRuneAt(pos)
	pos = size
	cluster := idCluster{
		start: 0,
		end:   pos,
		kind:  runeIdentifierKind(r),
		cased: runeCased(r),
	}
	wordState, _ = transitionWordBreakState(wordState, r, it.bytesAt(pos), it.stringAt(pos))
	graphemeState, _, _ = transitionGraphemeState(graphemeState, r)

	for pos < n {
		nextR, nextSize := it.decodeRuneAt(pos)
		nextKind := runeIdentifierKind(nextR)
		grState, _, graphemeBoundary := transitionGraphemeState(graphemeState&maskGraphemeState, nextR)
		after := pos + nextSize
		var wordBoundary bool
		wordState, wordBoundary = transitionWordBreakState(wordState, nextR, it.bytesAt(after), it.stringAt(after))

		if graphemeBoundary {
			// The boundary reported by the new cluster's first rune is the
			// boundary between the cluster being closed and the new one.
			cluster.wordBreak = wordBoundary
			cluster.end = pos
			it.clusters = append(it.clusters, cluster)

			cluster = idCluster{
				start: pos,
				end:   after,
				kind:  nextKind,
				cased: runeCased(nextR),
			}
			graphemeState = grState
			pos = after
			continue
		}

		pos = after
		graphemeState = grState
		cluster.end = pos
		// The cluster's identifier kind is fixed by its first rune; attached
		// marks never change it.
		if cluster.cased == idCaseNone {
			cluster.cased = runeCased(nextR)
		}
	}

	// The final cluster is always followed by an end-of-input boundary.
	cluster.wordBreak = true
	cluster.end = n
	it.clusters = append(it.clusters, cluster)
}
func (it *IdentifierTokens) decodeRuneAt(pos int) (r rune, size int) {
	if it.b != nil {
		return utf8.DecodeRune(it.b[pos:])
	}
	return utf8.DecodeRuneInString(it.str[pos:])
}

func (it *IdentifierTokens) bytesAt(pos int) []byte {
	if it.b == nil {
		return nil
	}
	return it.b[pos:]
}

func (it *IdentifierTokens) stringAt(pos int) string {
	if it.b != nil {
		return ""
	}
	return it.str[pos:]
}

// drainIdentifier consumes one maximal identifier region starting at it.index
// and enqueues its word and separator segments.
// drainIdentifierWord splits one standard word unit that consists entirely of
// identifier characters into [IdentifierWord] and [IdentifierSeparator]
// segments, and enqueues them.
// drainIdentifierRegion consumes one maximal run of identifier clusters
// starting at it.index and enqueues its [IdentifierWord] and
// [IdentifierSeparator] segments.
func (it *IdentifierTokens) drainIdentifierRegion() {
	clusters := it.clusters
	startIndex := it.index

	endIndex := startIndex
	for endIndex < len(clusters) && clusters[endIndex].kind != idKindOther {
		endIndex++
	}
	it.index = endIndex

	firstReason := IdentifierStart
	if startIndex > 0 {
		firstReason = IdentifierClassChange
	}

	segStart := clusters[startIndex].start
	segClass := classForKind(clusters[startIndex].kind)
	segReason := firstReason

	flush := func(end int) {
		it.enqueue(segStart, end, segClass, segReason)
	}

	for i := startIndex + 1; i < endIndex; i++ {
		prev := clusters[i-1]
		cur := clusters[i]

		boundary := false
		reason := 0
		switch {
		case prev.kind == idKindUnderscore && cur.kind == idKindUnderscore:
			// Consecutive underscores form one separator segment.
		case prev.kind == idKindUnderscore || cur.kind == idKindUnderscore:
			boundary = true
			reason = IdentifierUnderscore
		case prev.kind != cur.kind:
			// Letter/digit switch in either direction.
			boundary = true
			reason = IdentifierLetterDigit
		default:
			if r := caseBoundaryReason(clusters, i, endIndex); r >= 0 {
				boundary = true
				reason = r
			}
		}

		if !boundary {
			continue
		}
		flush(prev.end)
		segStart = cur.start
		segClass = classForKind(cur.kind)
		segReason = reason
	}

	flush(clusters[endIndex-1].end)
}

// drainNonIdentifierRegion consumes non-identifier clusters up to the next
// identifier character and splits them into [IdentifierNonWord] segments at
// standard UAX #29 word boundaries. The word boundary between two clusters is
// read from the earlier cluster.
func (it *IdentifierTokens) drainNonIdentifierRegion() {
	clusters := it.clusters
	startIndex := it.index

	endIndex := startIndex
	for endIndex < len(clusters) && clusters[endIndex].kind == idKindOther {
		endIndex++
	}
	it.index = endIndex

	segStart := clusters[startIndex].start
	segReason := IdentifierStart
	if startIndex > 0 {
		segReason = IdentifierClassChange
	}

	for i := startIndex; i < endIndex; i++ {
		if i > startIndex && clusters[i-1].wordBreak {
			it.enqueue(segStart, clusters[i].start, IdentifierNonWord, segReason)
			segStart = clusters[i].start
			segReason = IdentifierWordBoundary
		}
	}
	it.enqueue(segStart, clusters[endIndex-1].end, IdentifierNonWord, segReason)
}

func classForKind(kind int) int {
	if kind == idKindUnderscore {
		return IdentifierSeparator
	}
	return IdentifierWord
}

// caseBoundaryReason returns the reason for a boundary between clusters[i-1]
// and clusters[i], or -1 if no case boundary applies. Both clusters must be
// letter clusters; digit clusters are handled by the caller.
func caseBoundaryReason(clusters []idCluster, i, endIndex int) int {
	prev := clusters[i-1]
	cur := clusters[i]
	if prev.kind != idKindLetter || cur.kind != idKindLetter {
		return -1
	}

	prevUpper := prev.cased == idCaseUpper || prev.cased == idCaseTitle
	prevLower := prev.cased == idCaseLower
	curUpper := cur.cased == idCaseUpper || cur.cased == idCaseTitle

	switch {
	case prevLower && curUpper:
		// lower -> upper: camelCase.
		return IdentifierCamelCase
	case prevUpper && curUpper && i+1 < endIndex:
		// A run of upper case followed by an upper/lower case pair: split
		// before the last upper case letter: XML|Reader, HTTP|Server.
		next := clusters[i+1]
		if next.kind == idKindLetter && next.cased == idCaseLower {
			return IdentifierAcronym
		}
	}
	return -1
}

// runeIdentifierKind classifies a rune for the purposes of identifier
// detection.
func runeIdentifierKind(r rune) int {
	switch {
	case r == '_':
		return idKindUnderscore
	case unicode.IsDigit(r):
		return idKindDigit
	case unicode.IsLetter(r) || unicode.IsMark(r):
		return idKindLetter
	default:
		return idKindOther
	}
}

// runeCased returns the case property of a letter: idCaseUpper for upper
// case, idCaseTitle for title case, idCaseLower for lower case, and
// idCaseNone if the rune has no case property.
func runeCased(r rune) int {
	switch {
	case unicode.IsUpper(r):
		return idCaseUpper
	case unicode.IsTitle(r):
		return idCaseTitle
	case unicode.IsLower(r):
		return idCaseLower
	default:
		return idCaseNone
	}
}
