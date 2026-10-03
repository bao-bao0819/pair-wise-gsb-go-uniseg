package uniseg

// A TextLine describes one line of a typeset text as returned by
// [LayoutText]. All byte positions are positions in the original input string.
// Lines never overlap and, in order, cover the entire input.
type TextLine struct {
	// Start and End are the half-open byte range of the original text
	// covered by this line, str[Start:End]. No characters are removed or
	// re-ordered, so joining the line ranges in order reproduces the input
	// exactly. Spaces and hard line break characters are never trimmed.
	Start, End int

	// Width is the monospace width of the line as calculated by [StepString],
	// including any trailing spaces and excluding the hard line break
	// characters (which have a width of 0 anyway).
	Width int

	// HardBreak is true if the line ends with a mandatory line break (a
	// newline character or another code point classified as BK, LF, NL, or a
	// CR that is not followed by LF). The line range includes that line
	// break. CRLF is treated as a single hard line break, so no additional
	// line break is inserted for it.
	HardBreak bool

	// Overflow is true if the line is occupied by a single unbreakable
	// fragment that is wider than the maximum line width. Such a fragment
	// cannot legally be split (it is not broken into grapheme clusters or
	// runes), so it occupies the line on its own and the excess width is
	// reported here.
	Overflow bool
}

// TextLayout is the result of typesetting a text with [LayoutText].
type TextLayout struct {
	// Lines are the typeset lines, in order.
	Lines []TextLine

	// SoftBreakPositions are the byte positions in the original string at
	// which a line break was inserted at an optional line break opportunity
	// (LineCanBreak). A line break character ('\n') must be inserted at each
	// of these positions, before str[pos], to render the layout. Positions
	// are sorted in ascending order. Positions at mandatory line breaks are
	// not included because the original text already contains the line break
	// characters there.
	SoftBreakPositions []int

	// Raggedness is the sum of (maxWidth-Width)^2 over all non-final lines
	// of each paragraph. The final line of every paragraph is free. Lines
	// marked as Overflow contribute (Width-maxWidth)^2 like any other
	// non-final line.
	Raggedness int
}

// Apply inserts the soft line breaks of the layout into the original string
// and returns the result. Mandatory line breaks are part of the original
// string already, so they are not duplicated. The string is not otherwise
// modified: no trimming and no normalization is performed.
func (l TextLayout) Apply(str string) string {
	if len(l.SoftBreakPositions) == 0 {
		return str
	}
	result := make([]byte, 0, len(str)+len(l.SoftBreakPositions))
	prev := 0
	for _, pos := range l.SoftBreakPositions {
		result = append(result, str[prev:pos]...)
		result = append(result, '\n')
		prev = pos
	}
	result = append(result, str[prev:]...)
	return string(result)
}

// A breakFragment is a maximal run of grapheme clusters between which the
// line break algorithm does not permit a break. It is the atomic unit of the
// paragraph optimization.
type breakFragment struct {
	offset int // Byte offset of the fragment within the full input.
	width  int // Sum of the monospace widths of the fragment's clusters.
}

// LayoutText typesets the given plain text for the given maximum line width
// (in monospace character cells). Line break opportunities are determined
// according to [UAX #14] as exposed by [StepString]: boundaries marked
// LineMustBreak terminate a paragraph, boundaries marked LineDontBreak never
// offer a break, and boundaries marked LineCanBreak are the only positions at
// which a line break may be inserted.
//
// Each paragraph (the text between mandatory line breaks) is optimized
// independently. A layout is chosen which minimizes the sum of
// (maxWidth-Width)^2 over all lines except the final line of each paragraph.
// If several layouts have the same cost, the one with the lexicographically
// smallest sequence of soft break positions is chosen (a sequence which is a
// prefix of another is considered smaller).
//
// A fragment which is wider than maxWidth cannot be split further, so it
// occupies a line on its own and that line is marked as overflowing.
//
// The original text is never modified. No trimming or space normalization is
// performed. A CR followed by LF (CRLF) is a single mandatory line break and
// does not cause a duplicate line break. This function only handles plain
// text: it does not interpret ANSI control sequences or perform dictionary
// hyphenation.
//
// The text is segmented exactly once. The optimization runs in O(n^2) time
// and O(n) additional space in the worst case, where n is the number of
// unbreakable fragments (typically the number of words or, for scripts such
// as Chinese, close to the number of characters); paragraphs ending at
// mandatory line breaks are optimized without being segmented again.
//
// [UAX #14]: https://unicode.org/reports/tr14/
func LayoutText(str string, maxWidth int) TextLayout {
	// Segment the entire text once with StepString. Each mandatory line break
	// closes a paragraph; the paragraph is optimized immediately (without
	// being segmented again) and emitted as lines.
	var (
		paragraph []breakFragment // Fragments of the current paragraph.
		lines     []TextLine

		cursor      = 0 // Byte offset of the current cluster.
		fragStart   = 0 // Byte offset of the fragment currently being built.
		fragWidth   = 0 // Width of the fragment currently being built.
		fragStarted = false
		paraStart   = 0 // Start of the current paragraph within the text.
		totalCost   = 0
		softBreaks  []int
	)

	flushFragment := func() {
		if !fragStarted {
			return
		}
		paragraph = append(paragraph, breakFragment{offset: fragStart, width: fragWidth})
		fragStarted = false
		fragWidth = 0
	}

	// emitParagraph optimizes the fragments collected since the previous
	// mandatory line break and appends the resulting lines. paragraphEnd is
	// the end of the paragraph's text (including the hard line break if any),
	// hard indicates whether the paragraph is terminated by a real line break
	// code point.
	emitParagraph := func(paragraphEnd int, hard bool) {
		flushFragment()

		if len(paragraph) == 0 {
			// An empty paragraph between two hard line breaks. The line break
			// that terminates it belongs to this line, so consecutive hard
			// line breaks produce consecutive, non-overlapping line ranges.
			if hard {
				lines = append(lines, TextLine{Start: paraStart, End: paragraphEnd, HardBreak: true})
			}
			paragraph = paragraph[:0]
			paraStart = paragraphEnd
			return
		}

		breaks, cost := optimizeRaggedness(paragraph, maxWidth)
		totalCost += cost

		// Emit the non-final lines, each ending at the next break fragment's
		// start offset.
		lineStart := 0
		for _, fragIdx := range breaks {
			line := TextLine{Start: paragraph[lineStart].offset, End: paragraph[fragIdx].offset}
			for k := lineStart; k < fragIdx; k++ {
				line.Width += paragraph[k].width
			}
			if fragIdx-lineStart == 1 && line.Width > maxWidth {
				line.Overflow = true
			}
			lines = append(lines, line)
			softBreaks = append(softBreaks, line.End)
			lineStart = fragIdx
		}

		// The final line of the paragraph is free of raggedness cost.
		last := TextLine{
			Start:     paragraph[lineStart].offset,
			End:       paragraphEnd,
			HardBreak: hard,
		}
		for k := lineStart; k < len(paragraph); k++ {
			last.Width += paragraph[k].width
		}
		if len(paragraph)-lineStart == 1 && last.Width > maxWidth {
			last.Overflow = true
		}
		lines = append(lines, last)

		paragraph = paragraph[:0]
		paraStart = paragraphEnd
	}

	state := -1
	rest := str
	for len(rest) > 0 {
		var cluster string
		var boundaries int
		cluster, rest, boundaries, state = StepString(rest, state)
		width := boundaries >> ShiftWidth
		lineBreak := boundaries & MaskLine
		clusterEnd := cursor + len(cluster)

		switch lineBreak {
		case LineMustBreak:
			// UAX #14 LB3 emits a mandatory break after the final cluster of
			// any text. It is a real hard line break only when the final
			// cluster is a line break code point.
			if len(rest) == 0 && !HasTrailingLineBreakInString(cluster) {
				// End of text without a trailing line break: the cluster is
				// part of the final paragraph.
				if !fragStarted {
					fragStart = cursor
					fragStarted = true
				}
				fragWidth += width
				break
			}
			// The mandatory break closes the current fragment. The line break
			// cluster itself has a width of 0 and is part of the line's range.
			flushFragment()
			emitParagraph(clusterEnd, true)
		case LineCanBreak:
			// Extend the current fragment with this cluster (e.g. a trailing
			// space or ZWSP), then close it: the next cluster starts a new
			// candidate fragment.
			if !fragStarted {
				fragStart = cursor
				fragStarted = true
			}
			fragWidth += width
			flushFragment()
		default: // LineDontBreak
			if !fragStarted {
				fragStart = cursor
				fragStarted = true
			}
			fragWidth += width
		}

		cursor = clusterEnd
	}

	// Emit the final paragraph if the text does not end with a hard line
	// break. (If it does, the final mandatory break already emitted it.)
	if fragStarted || len(paragraph) > 0 {
		emitParagraph(len(str), false)
	}

	return TextLayout{
		Lines:              lines,
		SoftBreakPositions: softBreaks,
		Raggedness:         totalCost,
	}
}

// optimizeRaggedness finds the partition of the given fragments into lines
// which minimizes the sum of (maxWidth-lineWidth)^2 over all lines except the
// last one. A line made up of more than one fragment must not exceed
// maxWidth; a single fragment may exceed it, in which case the line is
// implicitly overflowing. The returned indices are the fragment indices at
// which a soft line break is inserted (each starting a new line); they are
// lexicographically smallest among all optimal solutions. The returned cost
// is the minimum raggedness.
//
// The optimization is a suffix dynamic program in O(m^2) time and O(m)
// space. Choosing the first (smallest) next break index when costs tie yields
// the lexicographically smallest break sequence because all remaining
// suffixes are themselves optimal.
func optimizeRaggedness(fragments []breakFragment, maxWidth int) (breaks []int, cost int) {
	m := len(fragments)
	if m <= 1 {
		return nil, 0
	}

	const infinity = int(^uint(0) >> 1)
	dp := make([]int, m+1)     // dp[i] = optimal cost from fragment i.
	choice := make([]int, m+1) // choice[i] = first line end (exclusive) from i.

	for i := m - 1; i >= 0; i-- {
		best := infinity
		bestEnd := m
		width := 0
		for j := i + 1; j <= m; j++ {
			width += fragments[j-1].width
			// Lines containing more than one fragment must fit. A single
			// fragment is always allowed and may overflow.
			if width > maxWidth && j-i >= 2 {
				break
			}

			lineCost := 0
			if j < m {
				slack := maxWidth - width
				lineCost = slack*slack + dp[j]
			}
			if lineCost < best {
				best = lineCost
				bestEnd = j
			}
		}
		dp[i] = best
		choice[i] = bestEnd
	}

	for i := 0; choice[i] < m; {
		i = choice[i]
		breaks = append(breaks, i)
	}
	return breaks, dp[0]
}
