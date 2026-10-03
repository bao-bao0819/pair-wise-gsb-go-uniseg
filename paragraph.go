package uniseg

import (
	"unicode/utf8"
	"unsafe"
)

// A LaidOutLine is a single line produced by [LayoutTextString] (or
// [LayoutText]). Its byte range refers to the original text; no bytes are
// copied, trimmed, or modified.
//
// The bytes of a mandatory break sequence (LF, CR, CRLF, NEL, VT, FF, U+0085)
// are not part of any line: consecutive lines are separated either by the
// original break sequence, which remains in the text at the gap between two
// line ranges, or by a break inserted at a position listed in
// [TextLayout.SoftBreakPositions].
type LaidOutLine struct {
	// From is the byte offset of the first byte of this line in the original
	// text.
	From int

	// To is the byte offset immediately after the last byte of this line.
	To int

	// Width is the monospace width of the line as it appears in the original
	// text. Mandatory break sequences and other zero-width control characters
	// contribute nothing. Spaces attached to the end of a line (an inserted
	// break goes after the space, per UAX #14) remain part of the line and are
	// counted.
	Width int

	// Overflow is true if the line's width exceeds the requested maximum
	// column width. This only happens for a line consisting of a single
	// unbreakable fragment (a grapheme cluster with no legal break
	// opportunity before it) which is wider than the maximum width. Such a
	// fragment is never split into runes.
	Overflow bool
}

// A Paragraph is a section of the original text delimited by mandatory line
// breaks. Each paragraph is optimized independently. An empty text produces no
// paragraphs. A terminating mandatory break does not produce an additional
// empty paragraph.
type Paragraph struct {
	// From is the byte offset of the first byte of this paragraph.
	From int

	// To is the byte offset immediately after the last byte of this
	// paragraph. If the paragraph ends with a mandatory break sequence, that
	// sequence is included.
	To int

	// HardBreakLen is the length in bytes of the mandatory break sequence
	// terminating this paragraph, or 0 if the paragraph ends at the end of the
	// text without a break sequence. CRLF is a single mandatory break of
	// length 2.
	HardBreakLen int

	// Lines are the laid out lines of this paragraph, in order. The last line
	// ends before the mandatory break sequence when HardBreakLen is not 0.
	Lines []LaidOutLine
}

// TextLayout is the result of laying out a text with [LayoutTextString] or
// [LayoutText].
type TextLayout struct {
	// Paragraphs are the paragraphs of the text, in order. Each paragraph was
	// optimized independently.
	Paragraphs []Paragraph

	// SoftBreakPositions are the byte offsets at which a line break character
	// (such as '\n') must be inserted to reproduce the laid out text. They are
	// sorted in ascending order. A position is the byte offset immediately
	// after the last byte of the line preceding the inserted break; inserting
	// the line break character at that offset separates the two lines.
	// Mandatory break positions are not included because the original break
	// sequence is already part of the text.
	SoftBreakPositions []int
}

// LayoutTextString lays out the given text into lines of at most maxWidth
// monospace columns, following the line breaking rules of [UAX #14] and the
// monospace widths of this package.
//
// Among all legal line break selections, this function minimizes the sum of
// (maxWidth-lineWidth)^2 over all non-final lines of every paragraph.
// Positions reported as [LineMustBreak] are always broken; positions reported
// as [LineDontBreak] are never break candidates; [LineCanBreak] positions are
// optional. The last line of each paragraph contributes nothing to the cost.
// If multiple solutions have the same cost, the lexicographically smallest
// sequence of inserted break positions is chosen.
//
// Mandatory break sequences (LF, CR, CRLF, NEL, VT, FF, U+0085) terminate a
// paragraph, and CRLF is treated as a single mandatory break; no additional
// break is inserted around it. If an unbreakable fragment is wider than
// maxWidth, it is placed on a line by itself and that line is marked as
// overflowing; fragments are never split into runes. Thus grapheme clusters
// (including ZWJ emoji sequences), no-break spaces, and word joiners are
// handled according to the segmentation results of [StepString], and CJK text
// without spaces is broken exactly at the opportunities reported by the UAX
// #14 rules.
//
// The original text is not modified, trimmed, or normalized in any way. The
// result refers back to the original text using byte ranges. A maxWidth below
// 1 is treated as 0. The text is segmented exactly once; see the Complexity
// section of the package documentation. ANSI escape sequences and hyphenation
// dictionaries are not handled.
//
// [UAX #14]: https://unicode.org/reports/tr14/
func LayoutTextString(str string, maxWidth int) TextLayout {
	if maxWidth < 0 {
		maxWidth = 0
	}

	var (
		layout   TextLayout
		pending  []layoutUnit // Units of the paragraph currently being parsed.
		pendings []pendingParagraph

		pos       int
		pendingCR bool
		state     = -1
	)

	// flush closes the current paragraph. hardLen > 0 means it ends with a
	// mandatory break sequence of that many bytes; the break is included in
	// the paragraph's byte range but not in its layout units.
	flush := func(hardLen int) {
		start := 0
		if n := len(pendings); n > 0 {
			start = pendings[n-1].end
		}
		pendings = append(pendings, pendingParagraph{
			start:   start,
			end:     pos + hardLen,
			hardLen: hardLen,
			units:   pending,
		})
		pending = nil
	}

	for len(str) > 0 {
		cluster, rest, boundaries, newState := StepString(str, state)
		length := len(str) - len(rest)
		lineBreak := boundaries & MaskLine

		r, _ := utf8.DecodeRuneInString(cluster)
		prop, _ := propertyLineBreak(r)
		isHardRune := prop == prBK || prop == prCR || prop == prLF || prop == prNL

		switch {
		case pendingCR && prop == prLF:
			// LB5: CRLF is a single mandatory break. The CR already
			// terminated the paragraph; absorb the LF without opening or
			// closing another one.
			pp := &pendings[len(pendings)-1]
			pp.end = pos + length
			pp.hardLen = 2
			pendingCR = false
		case isHardRune:
			flush(length)
			pendingCR = prop == prCR
		default:
			pending = append(pending, layoutUnit{
				start:    pos,
				end:      pos + length,
				width:    boundaries >> ShiftWidth,
				canBreak: lineBreak == LineCanBreak,
			})
			pendingCR = false
		}

		pos += length
		str = rest
		state = newState
	}

	// A final paragraph without a trailing mandatory break. Empty input
	// produces no paragraphs at all.
	if len(pending) > 0 {
		flush(0)
	}

	for pi := range pendings {
		layout.Paragraphs = append(layout.Paragraphs,
			layoutParagraph(&pendings[pi], maxWidth, &layout.SoftBreakPositions))
	}

	return layout
}

// LayoutText is like [LayoutTextString] but for byte slices. The returned byte
// ranges index into b.
func LayoutText(b []byte, maxWidth int) TextLayout {
	if len(b) == 0 {
		return LayoutTextString("", maxWidth)
	}
	// Zero-copy conversion; the pre-Go 1.20 equivalent of
	// unsafe.String(unsafe.SliceData(b), len(b)). The result is only passed to
	// LayoutTextString, which never mutates or retains it beyond the call.
	str := *(*string)(unsafe.Pointer(&b))
	return LayoutTextString(str, maxWidth)
}

// layoutUnit is the information of one grapheme cluster within the paragraph
// currently being optimized.
type layoutUnit struct {
	start, end int
	width      int
	canBreak   bool // An inserted break is legal after this unit.
}

// pendingParagraph is a paragraph collected during the single segmentation
// pass, awaiting optimization.
type pendingParagraph struct {
	start   int
	end     int
	hardLen int
	units   []layoutUnit
}

// layoutParagraph runs the dynamic optimization for one paragraph and appends
// the resulting lines. softBreaks accumulates inserted break positions across
// paragraphs. The parser is not invoked here; the segmentation result was
// captured in p.units during the single pass over the whole text.
func layoutParagraph(p *pendingParagraph, maxWidth int, softBreaks *[]int) Paragraph {
	units := p.units
	n := len(units)

	// Prefix widths of the units for O(1) range width queries.
	prefix := make([]int, n+1)
	for i, u := range units {
		prefix[i+1] = prefix[i] + u.width
	}

	// Build atoms: maximal runs of units joined by LineDontBreak boundaries.
	// An atom is the smallest piece that can start a line. A break candidate
	// exists between two adjacent atoms.
	type atom struct {
		endUnit int // Exclusive unit index: units[start:endUnit].
		width   int
	}
	var atoms []atom
	for i := 0; i < n; {
		j := i + 1
		for j < n && !units[j-1].canBreak {
			j++
		}
		atoms = append(atoms, atom{
			endUnit: j,
			width:   prefix[j] - prefix[i],
		})
		i = j
	}
	m := len(atoms)

	// Prefix widths over atoms.
	atomPrefix := make([]int, m+1)
	for i, a := range atoms {
		atomPrefix[i+1] = atomPrefix[i] + a.width
	}

	// Backward dynamic programming on atoms. cost[i] is the minimal cost of
	// laying out atoms[i:m]; nxt[i] is the exclusive end atom index of the
	// first line.
	cost := make([]int64, m+1)
	nxt := make([]int, m+1)
	cost[m] = 0
	for i := m - 1; i >= 0; i-- {
		best := int64(1) << 62
		bestEnd := i + 1
		for end := i + 1; end <= m; end++ {
			width := atomPrefix[end] - atomPrefix[i]
			if end > i+1 && width > maxWidth {
				// Only a single-atom line may overflow; atoms themselves are
				// indivisible and are never split into runes.
				break
			}
			c := cost[end]
			if end < m {
				gap := int64(maxWidth - width)
				c += gap * gap
			}
			// The first inserted break offset is the end position of the
			// chosen first line, so among equal-cost solutions the smallest
			// end gives the lexicographically smallest break sequence. end is
			// visited in ascending order; by induction (the suffix is already
			// optimal), strict comparison retains exactly that solution.
			if c < best {
				best = c
				bestEnd = end
			}
		}
		cost[i], nxt[i] = best, bestEnd
	}

	// Walk the chosen breaks forward to produce lines.
	result := Paragraph{From: p.start, To: p.end, HardBreakLen: p.hardLen}
	i := 0 // Atom index.
	lineStart := p.start
	for i < m {
		end := nxt[i]
		width := atomPrefix[end] - atomPrefix[i]
		lastUnit := atoms[end-1].endUnit - 1
		line := LaidOutLine{
			From:     lineStart,
			Width:    width,
			Overflow: width > maxWidth,
		}
		if end < m {
			// Inserted break: the line ends after its last unit.
			line.To = units[lastUnit].end
			*softBreaks = append(*softBreaks, line.To)
			lineStart = line.To
		} else {
			// Final line: it ends right before the mandatory break sequence
			// (if any), which occupies its own zero-width bytes between this
			// paragraph and the next one.
			line.To = p.end - p.hardLen
		}
		result.Lines = append(result.Lines, line)
		i = end
	}

	// An empty paragraph (a mandatory break sequence with no preceding
	// content) consists of a single empty zero-width line.
	if n == 0 {
		result.Lines = append(result.Lines, LaidOutLine{
			From:  p.end - p.hardLen,
			To:    p.end - p.hardLen,
			Width: 0,
		})
	}

	return result
}
