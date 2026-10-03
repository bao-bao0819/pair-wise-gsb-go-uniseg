package uniseg_test

import (
	"fmt"

	"github.com/rivo/uniseg"
)

// ExampleLayoutText_demo compares the raggedness-minimizing layout with the
// classic greedy ("pack as much as fits") strategy. Both only break at legal
// UAX #14 opportunities, but the optimized layout balances the non-final
// lines and achieves a smaller sum of squared slack.
func ExampleLayoutText_demo() {
	const maxWidth = 8
	text := "She is so waited"

	// Optimal layout.
	layout := uniseg.LayoutText(text, maxWidth)
	fmt.Println("optimal:")
	for _, line := range layout.Lines {
		fmt.Printf("%-*s width=%d\n", maxWidth, text[line.Start:line.End], line.Width)
	}
	fmt.Println("raggedness:", layout.Raggedness)

	// Collect the same unbreakable fragments (maximal runs between break
	// opportunities) that the optimizer uses.
	type fragment struct {
		text  string
		width int
	}
	var fragments []fragment
	state := -1
	rest := text
	var fragText string
	var fragWidth int
	flush := func() {
		if fragText != "" {
			fragments = append(fragments, fragment{fragText, fragWidth})
			fragText = ""
			fragWidth = 0
		}
	}
	for len(rest) > 0 {
		var cluster string
		var boundaries int
		cluster, rest, boundaries, state = uniseg.StepString(rest, state)
		fragText += cluster
		fragWidth += boundaries >> uniseg.ShiftWidth
		if boundaries&uniseg.MaskLine == uniseg.LineCanBreak {
			flush()
		}
	}
	flush()

	// Greedy packing. A single over-wide fragment stays on its own line.
	fmt.Println("greedy:")
	lineText := fragments[0].text
	lineWidth := fragments[0].width
	greedyRaggedness := 0
	for i := 1; i < len(fragments); i++ {
		f := fragments[i]
		if lineWidth+f.width <= maxWidth || lineText == "" {
			lineText += f.text
			lineWidth += f.width
			continue
		}
		fmt.Printf("%-*s width=%d\n", maxWidth, lineText, lineWidth)
		slack := maxWidth - lineWidth
		greedyRaggedness += slack * slack
		lineText = f.text
		lineWidth = f.width
	}
	fmt.Printf("%-*s width=%d\n", maxWidth, lineText, lineWidth)
	fmt.Println("raggedness:", greedyRaggedness)

	// Apply reports the inserted newlines as byte positions. Spaces are kept
	// (no trimming), shown here with %q.
	fmt.Printf("%q\n", layout.Apply(text))

	// Output:
	// optimal:
	// She      width=4
	// is so    width=6
	// waited   width=6
	// raggedness: 20
	// greedy:
	// She is   width=7
	// so       width=3
	// waited   width=6
	// raggedness: 26
	// "She \nis so \nwaited"
}
