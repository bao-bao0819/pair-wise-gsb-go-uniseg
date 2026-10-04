package uniseg_test

import (
	"fmt"

	"github.com/rivo/uniseg"
)

func ExampleGraphemeClusterCount() {
	n := uniseg.GraphemeClusterCount("🇩🇪🏳️‍🌈")
	fmt.Println(n)
	// Output: 2
}

func ExampleFirstGraphemeCluster() {
	b := []byte("🇩🇪🏳️‍🌈!")
	state := -1
	var c []byte
	for len(b) > 0 {
		var width int
		c, b, width, state = uniseg.FirstGraphemeCluster(b, state)
		fmt.Println(string(c), width)
	}
	// Output: 🇩🇪 2
	//🏳️‍🌈 2
	//! 1
}

func ExampleFirstGraphemeClusterInString() {
	str := "🇩🇪🏳️‍🌈!"
	state := -1
	var c string
	for len(str) > 0 {
		var width int
		c, str, width, state = uniseg.FirstGraphemeClusterInString(str, state)
		fmt.Println(c, width)
	}
	// Output: 🇩🇪 2
	//🏳️‍🌈 2
	//! 1
}

func ExampleFirstWord() {
	b := []byte("Hello, world!")
	state := -1
	var c []byte
	for len(b) > 0 {
		c, b, state = uniseg.FirstWord(b, state)
		fmt.Printf("(%s)\n", string(c))
	}
	// Output: (Hello)
	//(,)
	//( )
	//(world)
	//(!)
}

func ExampleFirstWordInString() {
	str := "Hello, world!"
	state := -1
	var c string
	for len(str) > 0 {
		c, str, state = uniseg.FirstWordInString(str, state)
		fmt.Printf("(%s)\n", c)
	}
	// Output: (Hello)
	//(,)
	//( )
	//(world)
	//(!)
}

func ExampleFirstSentence() {
	b := []byte("This is sentence 1.0. And this is sentence two.")
	state := -1
	var c []byte
	for len(b) > 0 {
		c, b, state = uniseg.FirstSentence(b, state)
		fmt.Printf("(%s)\n", string(c))
	}
	// Output: (This is sentence 1.0. )
	//(And this is sentence two.)
}

func ExampleFirstSentenceInString() {
	str := "This is sentence 1.0. And this is sentence two."
	state := -1
	var c string
	for len(str) > 0 {
		c, str, state = uniseg.FirstSentenceInString(str, state)
		fmt.Printf("(%s)\n", c)
	}
	// Output: (This is sentence 1.0. )
	//(And this is sentence two.)
}

func ExampleFirstLineSegment() {
	b := []byte("First line.\nSecond line.")
	state := -1
	var (
		c         []byte
		mustBreak bool
	)
	for len(b) > 0 {
		c, b, mustBreak, state = uniseg.FirstLineSegment(b, state)
		fmt.Printf("(%s)", string(c))
		if mustBreak {
			fmt.Print("!")
		}
	}
	// Output: (First )(line.
	//)!(Second )(line.)!
}

func ExampleFirstLineSegmentInString() {
	str := "First line.\nSecond line."
	state := -1
	var (
		c         string
		mustBreak bool
	)
	for len(str) > 0 {
		c, str, mustBreak, state = uniseg.FirstLineSegmentInString(str, state)
		fmt.Printf("(%s)", c)
		if mustBreak {
			fmt.Println(" < must break")
		} else {
			fmt.Println(" < may break")
		}
	}
	// Output: (First ) < may break
	//(line.
	//) < must break
	//(Second ) < may break
	//(line.) < must break
}

func ExampleStep_graphemes() {
	b := []byte("🇩🇪🏳️‍🌈!")
	state := -1
	var c []byte
	for len(b) > 0 {
		var boundaries int
		c, b, boundaries, state = uniseg.Step(b, state)
		fmt.Println(string(c), boundaries>>uniseg.ShiftWidth)
	}
	// Output: 🇩🇪 2
	//🏳️‍🌈 2
	//! 1
}

func ExampleStepString_graphemes() {
	str := "🇩🇪🏳️‍🌈!"
	state := -1
	var c string
	for len(str) > 0 {
		var boundaries int
		c, str, boundaries, state = uniseg.StepString(str, state)
		fmt.Println(c, boundaries>>uniseg.ShiftWidth)
	}
	// Output: 🇩🇪 2
	//🏳️‍🌈 2
	//! 1
}

func ExampleStep_word() {
	b := []byte("Hello, world!")
	state := -1
	var (
		c          []byte
		boundaries int
	)
	for len(b) > 0 {
		c, b, boundaries, state = uniseg.Step(b, state)
		fmt.Print(string(c))
		if boundaries&uniseg.MaskWord != 0 {
			fmt.Print("|")
		}
	}
	// Output: Hello|,| |world|!|
}

func ExampleStepString_word() {
	str := "Hello, world!"
	state := -1
	var (
		c          string
		boundaries int
	)
	for len(str) > 0 {
		c, str, boundaries, state = uniseg.StepString(str, state)
		fmt.Print(c)
		if boundaries&uniseg.MaskWord != 0 {
			fmt.Print("|")
		}
	}
	// Output: Hello|,| |world|!|
}

func ExampleStep_sentence() {
	b := []byte("This is sentence 1.0. And this is sentence two.")
	state := -1
	var (
		c          []byte
		boundaries int
	)
	for len(b) > 0 {
		c, b, boundaries, state = uniseg.Step(b, state)
		fmt.Print(string(c))
		if boundaries&uniseg.MaskSentence != 0 {
			fmt.Print("|")
		}
	}
	// Output: This is sentence 1.0. |And this is sentence two.|
}

func ExampleStepString_sentence() {
	str := "This is sentence 1.0. And this is sentence two."
	state := -1
	var (
		c          string
		boundaries int
	)
	for len(str) > 0 {
		c, str, boundaries, state = uniseg.StepString(str, state)
		fmt.Print(c)
		if boundaries&uniseg.MaskSentence != 0 {
			fmt.Print("|")
		}
	}
	// Output: This is sentence 1.0. |And this is sentence two.|
}

func ExampleStep_lineBreaking() {
	b := []byte("First line.\nSecond line.")
	state := -1
	var (
		c          []byte
		boundaries int
	)
	for len(b) > 0 {
		c, b, boundaries, state = uniseg.Step(b, state)
		fmt.Print(string(c))
		if boundaries&uniseg.MaskLine == uniseg.LineCanBreak {
			fmt.Print("|")
		} else if boundaries&uniseg.MaskLine == uniseg.LineMustBreak {
			fmt.Print("‖")
		}
	}
	// Output: First |line.
	//‖Second |line.‖
}

func ExampleStepString_lineBreaking() {
	str := "First line.\nSecond line."
	state := -1
	var (
		c          string
		boundaries int
	)
	for len(str) > 0 {
		c, str, boundaries, state = uniseg.StepString(str, state)
		fmt.Print(c)
		if boundaries&uniseg.MaskLine == uniseg.LineCanBreak {
			fmt.Print("|")
		} else if boundaries&uniseg.MaskLine == uniseg.LineMustBreak {
			fmt.Print("‖")
		}
	}
	// Output: First |line.
	//‖Second |line.‖
}

func ExampleGraphemes_graphemes() {
	g := uniseg.NewGraphemes("🇩🇪🏳️‍🌈")
	for g.Next() {
		fmt.Println(g.Str())
	}
	// Output: 🇩🇪
	//🏳️‍🌈
}

func ExampleGraphemes_word() {
	g := uniseg.NewGraphemes("Hello, world!")
	for g.Next() {
		fmt.Print(g.Str())
		if g.IsWordBoundary() {
			fmt.Print("|")
		}
	}
	// Output: Hello|,| |world|!|
}

func ExampleGraphemes_sentence() {
	g := uniseg.NewGraphemes("This is sentence 1.0. And this is sentence two.")
	for g.Next() {
		fmt.Print(g.Str())
		if g.IsSentenceBoundary() {
			fmt.Print("|")
		}
	}
	// Output: This is sentence 1.0. |And this is sentence two.|
}

func ExampleGraphemes_lineBreaking() {
	g := uniseg.NewGraphemes("First line.\nSecond line.")
	for g.Next() {
		fmt.Print(g.Str())
		if g.LineBreak() == uniseg.LineCanBreak {
			fmt.Print("|")
		} else if g.LineBreak() == uniseg.LineMustBreak {
			fmt.Print("‖")
		}
	}
	if g.LineBreak() == uniseg.LineMustBreak {
		fmt.Print("\nNo clusters left. LineMustBreak")
	}
	g.Reset()
	if g.LineBreak() == uniseg.LineDontBreak {
		fmt.Print("\nIterator has been reset. LineDontBreak")
	}
	// Output: First |line.
	//‖Second |line.‖
	//No clusters left. LineMustBreak
	//Iterator has been reset. LineDontBreak
}

func ExampleStringWidth() {
	fmt.Println(uniseg.StringWidth("Hello, 世界"))
	// Output: 11
}

func ExampleNewIdentifierTokens() {
	// Build a simple search index by extracting only identifier sub-words.
	documents := []string{
		"HTTPServer_v2",
		"XMLReader",
	}
	index := map[string][]int{}
	for docID, doc := range documents {
		it := uniseg.NewIdentifierTokens(doc)
		for it.Next() {
			tok := it.Token()
			if tok.Class != uniseg.IdentifierWord {
				continue // Skip separators and non-identifier text.
			}
			subword := it.Str() // Original bytes; no normalization is applied.
			index[subword] = append(index[subword], docID)
		}
	}
	for _, term := range []string{"HTTP", "Server", "v", "2", "XML", "Reader"} {
		fmt.Printf("%s -> document %v\n", term, index[term])
	}
	// Output:
	// HTTP -> document [0]
	// Server -> document [0]
	// v -> document [0]
	// 2 -> document [0]
	// XML -> document [1]
	// Reader -> document [1]
}

func ExampleNewIdentifierTokensFromBytes() {
	// The byte and string entry points return identical byte ranges.
	b := []byte("HTTPServer_v2, XMLReader")
	it := uniseg.NewIdentifierTokensFromBytes(b)
	for it.Next() {
		tok := it.Token()
		from, to := it.Positions()
		class := map[int]string{
			uniseg.IdentifierWord:      "word",
			uniseg.IdentifierSeparator: "sep",
			uniseg.IdentifierNonWord:   "other",
		}[tok.Class]
		fmt.Printf("%d:%d %-5s %q\n", from, to, class, string(b[from:to]))
	}
	// Output:
	// 0:4 word  "HTTP"
	// 4:10 word  "Server"
	// 10:11 sep   "_"
	// 11:12 word  "v"
	// 12:13 word  "2"
	// 13:14 other ","
	// 14:15 other " "
	// 15:18 word  "XML"
	// 18:24 word  "Reader"
}
