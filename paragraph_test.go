package uniseg

import (
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// parseForLayout is a test-only mirror of the unit collection used by
// LayoutTextString. It segments one paragraph (which must not contain
// mandatory breaks) and returns the units.
func parseForLayout(t *testing.T, str string) []layoutUnit {
	t.Helper()
	var units []layoutUnit
	state := -1
	pos := 0
	for len(str) > 0 {
		_, rest, boundaries, newState := StepString(str, state)
		length := len(str) - len(rest)
		lineBreak := boundaries & MaskLine
		if lineBreak == LineMustBreak && len(rest) > 0 {
			t.Fatalf("unexpected mandatory break in paragraph %q", str)
		}
		units = append(units, layoutUnit{
			start:    pos,
			end:      pos + length,
			width:    boundaries >> ShiftWidth,
			canBreak: lineBreak == LineCanBreak,
		})
		pos += length
		str = rest
		state = newState
	}
	return units
}

// optimalByEnumeration brute-forces every subset of the legal break
// opportunities of one paragraph and returns the minimal cost together with
// the lexicographically smallest sequence of inserted break byte offsets.
// This independent implementation is used to validate the dynamic program.
func optimalByEnumeration(units []layoutUnit, maxWidth int) (int64, []int) {
	n := len(units)
	if n == 0 {
		return 0, nil
	}

	// Build atoms (maximal runs joined by LineDontBreak) exactly like the
	// production code, so the brute force evaluates the same legal layouts:
	// breaks only exist between atoms, and an overflowing multi-atom line is
	// illegal while a single atom may overflow.
	type atom struct {
		start, end int
		width      int
		lastEnd    int // Byte offset after the atom's last unit.
	}
	var atoms []atom
	for i := 0; i < n; {
		j := i + 1
		for j < n && !units[j-1].canBreak {
			j++
		}
		w := 0
		for u := i; u < j; u++ {
			w += units[u].width
		}
		atoms = append(atoms, atom{start: i, end: j, width: w, lastEnd: units[j-1].end})
		i = j
	}
	m := len(atoms)

	// Candidate break boundaries between adjacent atoms.
	var cands []int
	for k := 1; k < m; k++ {
		cands = append(cands, k)
	}

	widthOf := func(from, toExclusive int) int {
		w := 0
		for a := from; a < toExclusive; a++ {
			w += atoms[a].width
		}
		return w
	}

	bestCost := int64(math.MaxInt64)
	var bestBreaks []int

	maskCount := 1 << len(cands)
	for mask := 0; mask < maskCount; mask++ {
		// Build line boundaries (unit indices), always ending at n.
		bounds := []int{0}
		for ci, k := range cands {
			if mask&(1<<ci) != 0 {
				bounds = append(bounds, k)
			}
		}
		bounds = append(bounds, m)

		// Validate widths: a line with more than one atom must fit; a single
		// atom may overflow.
		legal := true
		var cost int64
		for li := 0; li < len(bounds)-1; li++ {
			from, to := bounds[li], bounds[li+1]
			w := widthOf(from, to)
			isLast := to == m
			if to-from > 1 && w > maxWidth {
				legal = false
				break
			}
			if !isLast {
				gap := int64(maxWidth - w)
				cost += gap * gap
			}
		}
		if !legal {
			continue
		}

		// Inserted byte offsets: end of the last unit of each non-final line.
		var breaks []int
		for _, k := range bounds[1 : len(bounds)-1] {
			breaks = append(breaks, atoms[k-1].lastEnd)
		}

		if cost < bestCost || (cost == bestCost && lexLess(breaks, bestBreaks)) {
			bestCost = cost
			bestBreaks = breaks
		}
	}
	return bestCost, bestBreaks
}

func lexLess(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func TestLayoutExhaustiveShortInputs(t *testing.T) {
	// A pool of building blocks exercising the various break classes:
	// plain letters, spaces, NBSP, WJ, CJK, emoji ZWJ clusters, punctuation.
	pool := []string{
		"a", "b", "c", " ", "e", "f",
		"\u00a0", // NBSP: no break after
		"\u2060", // WORD JOINER: no break around
		"中", "文", // CJK: breaks between ideographs
		"🏳️‍🌈", // ZWJ cluster, width 2
		",", ".", "-",
	}

	rng := rand.New(rand.NewSource(1))
	const iterations = 4000
	for iter := 0; iter < iterations; iter++ {
		length := 1 + rng.Intn(9)
		b := make([]byte, 0, length*4)
		for j := 0; j < length; j++ {
			b = append(b, pool[rng.Intn(len(pool))]...)
		}
		str := string(b)
		if hasMandatoryBreak(str) {
			continue
		}
		units := parseForLayout(t, str)

		for _, maxWidth := range []int{0, 1, 2, 3, 5, 8, 100} {
			wantCost, wantBreaks := optimalByEnumeration(units, maxWidth)

			l := LayoutTextString(str, maxWidth)
			if len(l.Paragraphs) != 1 {
				t.Fatalf("input %q: got %d paragraphs, want 1", str, len(l.Paragraphs))
			}
			if !reflect.DeepEqual(l.SoftBreakPositions, wantBreaks) {
				t.Fatalf("input %q width=%d:\n  got breaks %v\n  want %v\n  (want cost %d)",
					str, maxWidth, l.SoftBreakPositions, wantBreaks, wantCost)
			}

			// Verify reported lines and overflow flags are consistent with
			// the optimal break set.
			par := l.Paragraphs[0]
			cost := checkLines(t, str, par, maxWidth)
			if cost != wantCost {
				t.Fatalf("input %q width=%d: line cost %d, want %d",
					str, maxWidth, cost, wantCost)
			}
		}
	}
}

// TestLayoutTieBreakExhaustive enumerates all subsets of legal breaks for
// short structured inputs with several equal-cost solutions and verifies the
// lexicographically smallest break offset sequence is chosen.
func TestLayoutTieBreakExhaustive(t *testing.T) {
	// Inputs composed only of 'x' (width 1) separated by spaces (' '), so the
	// atoms are exactly "x " (width 2), with the final "x" (width 1). All
	// distributions of n atoms into lines of capacity W are available.
	for n := 2; n <= 12; n++ {
		str := strings.Repeat("x ", n-1) + "x"
		units := parseForLayout(t, str)
		for _, W := range []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 100} {
			wantCost, wantBreaks := optimalByEnumeration(units, W)
			l := LayoutTextString(str, W)
			if !reflect.DeepEqual(l.SoftBreakPositions, wantBreaks) {
				t.Fatalf("tie-break n=%d W=%d: got %v, want %v (cost %d)",
					n, W, l.SoftBreakPositions, wantBreaks, wantCost)
			}
		}
	}
}

// checkLines validates the line ranges of a paragraph and recomputes its
// non-final-line raggedness cost.
func checkLines(t *testing.T, str string, p Paragraph, maxWidth int) int64 {
	t.Helper()
	if p.HardBreakLen != 0 {
		t.Fatalf("unexpected hard break for %q", str)
	}
	var cost int64
	pos := p.From
	for li, ln := range p.Lines {
		if ln.From != pos {
			t.Fatalf("line %d starts at %d, expected %d", li, ln.From, pos)
		}
		if ln.To < ln.From {
			t.Fatalf("line %d has negative range", li)
		}
		w := StringWidth(str[ln.From:ln.To])
		if w != ln.Width {
			t.Fatalf("line %d width %d, computed %d (%q)",
				li, ln.Width, w, str[ln.From:ln.To])
		}
		overflow := w > maxWidth
		if overflow != ln.Overflow {
			t.Fatalf("line %d overflow flag %v, want %v (%q)",
				li, ln.Overflow, overflow, str[ln.From:ln.To])
		}
		isLast := li == len(p.Lines)-1
		if !isLast {
			if len(p.Lines) > 1 {
				gap := int64(maxWidth - w)
				cost += gap * gap
			}
		}
		pos = ln.To
	}
	if pos != p.To {
		t.Fatalf("paragraph %q lines end at %d, expected %d", str, pos, p.To)
	}
	return cost
}

func hasMandatoryBreak(str string) bool {
	state := -1
	for len(str) > 0 {
		_, rest, b, ns := StepString(str, state)
		if b&MaskLine == LineMustBreak && len(rest) > 0 {
			return true
		}
		str = rest
		state = ns
	}
	return false
}
