package uniseg

import (
	"strings"
	"testing"
)

func BenchmarkLayoutTextString(b *testing.B) {
	word := strings.Repeat("word ", 200)
	text := strings.Repeat(word+"\n", 10)
	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	for i := 0; i < b.N; i++ {
		LayoutTextString(text, 80)
	}
}
