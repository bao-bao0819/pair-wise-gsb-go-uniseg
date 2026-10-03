package uniseg

import (
	"bytes"
	"strings"
	"testing"
)

// incrementalTestCases are all the strings segmented once with
// FirstGraphemeCluster and incrementally in every possible way.
func incrementalTestCases() []string {
	cases := make([]string, 0, len(testCases)+len(graphemeBreakTestCases)+len(invalidUTF8Cases))
	for _, tc := range testCases {
		cases = append(cases, tc.original)
	}
	for _, tc := range graphemeBreakTestCases {
		cases = append(cases, tc.original)
	}
	cases = append(cases, invalidUTF8Cases...)
	return cases
}

// Invalid UTF-8 and truncated encodings. The incremental parser must handle
// these byte for byte exactly like FirstGraphemeCluster, without repairing
// anything.
var invalidUTF8Cases = []string{
	"\x80",
	"\xff",
	"\xc0\x80",
	"\xe0\x80\x80",
	"\xf4\x90\x80\x80",
	"a\xffb",
	"a\x80b",
	"a\xe0\x80b",
	"a\xf0\x90b",
	"a\xf0\x90\x80b",
	"\xc3\xa9",         // valid: é
	"a\xc3\xa9b",       // valid é in context
	"\xc3",             // truncated 2-byte
	"a\xc3",            // truncated 2-byte at the end
	"\xe0",             // truncated 3-byte
	"\xe0\xa0",         // truncated 3-byte
	"\xf0",             // truncated 4-byte
	"\xf0\x9f",         // truncated 4-byte
	"\xf0\x9f\x99",     // truncated 4-byte
	"\xf0\x9f\x99\xc3", // valid start, truncated next
	"🙂\xf0",            // valid emoji, truncated next
	"🙂\xf0\x9f",
	"\xf0🙂",        // truncated, then complete rune
	"🙂caf\xc3\xa9", // mixed
	"\xed\xa0\x80", // UTF-16 surrogate half, invalid
	"\xf4\x90",     // out of range, truncated
}

// referenceClusters segments b with the existing byte interface and returns
// the expected clusters with absolute ranges and widths.
func referenceClusters(b []byte) []GraphemeCluster {
	var want []GraphemeCluster
	state := -1
	pos := 0
	for len(b) > 0 {
		cluster, rest, width, newState := FirstGraphemeCluster(b, state)
		want = append(want, GraphemeCluster{
			Bytes: append([]byte(nil), cluster...),
			From:  pos,
			To:    pos + len(cluster),
			Width: width,
		})
		pos += len(cluster)
		b = rest
		state = newState
	}
	return want
}

// feedSegments drives g with the given blocks and returns every emitted
// cluster, including those produced by Finish.
func feedSegments(t *testing.T, g *IncrementalGraphemes, blocks ...[]byte) []GraphemeCluster {
	t.Helper()
	var got []GraphemeCluster
	for _, block := range blocks {
		clusters, err := g.Feed(block)
		if err != nil {
			t.Fatalf("Feed(%v) returned unexpected error: %v", block, err)
		}
		got = append(got, clusters...)
	}
	clusters, err := g.Finish()
	if err != nil {
		t.Fatalf("Finish returned unexpected error: %v", err)
	}
	got = append(got, clusters...)
	return got
}

// assertClusters checks that got matches the reference segmentation of input.
func assertClusters(t *testing.T, input []byte, got []GraphemeCluster, label string) {
	t.Helper()
	want := referenceClusters(input)

	if len(got) != len(want) {
		t.Fatalf("%s: got %d clusters, want %d (got %+v, want %+v)",
			label, len(got), len(want), got, want)
	}

	consumed := 0
	for i := range want {
		if !bytes.Equal(got[i].Bytes, want[i].Bytes) {
			t.Errorf("%s: cluster %d bytes = %v (%q), want %v (%q)",
				label, i, got[i].Bytes, got[i].Bytes, want[i].Bytes, want[i].Bytes)
		}
		if got[i].From != want[i].From || got[i].To != want[i].To {
			t.Errorf("%s: cluster %d range = [%d,%d), want [%d,%d)",
				label, i, got[i].From, got[i].To, want[i].From, want[i].To)
		}
		if got[i].Width != want[i].Width {
			t.Errorf("%s: cluster %d (%q) width = %d, want %d",
				label, i, got[i].Bytes, got[i].Width, want[i].Width)
		}
		if got[i].To-got[i].From != len(got[i].Bytes) {
			t.Errorf("%s: cluster %d range length %d does not match %d bytes",
				label, i, got[i].To-got[i].From, len(got[i].Bytes))
		}
		consumed += len(got[i].Bytes)
	}

	if consumed != len(input) {
		t.Errorf("%s: emitted clusters cover %d bytes, input has %d",
			label, consumed, len(input))
	}
	if len(input) > 0 && got[len(got)-1].To != len(input) {
		t.Errorf("%s: last cluster ends at %d, want %d",
			label, got[len(got)-1].To, len(input))
	}
}

// TestIncrementalSplitAtEveryByte cuts each existing grapheme test case at
// every byte position and feeds the two resulting blocks incrementally, then
// compares clusters, absolute byte ranges, and widths against a single
// segmentation of the whole input.
func TestIncrementalSplitAtEveryByte(t *testing.T) {
	for _, s := range incrementalTestCases() {
		input := []byte(s)

		// Also feed the whole input at once.
		t.Run("whole/"+shortName(s), func(t *testing.T) {
			g := NewIncrementalGraphemes()
			got := feedSegments(t, g, input)
			assertClusters(t, input, got, "whole")
		})

		for cut := 0; cut <= len(input); cut++ {
			cut := cut
			t.Run("cut", func(t *testing.T) {
				g := NewIncrementalGraphemes()
				got := feedSegments(t, g, input[:cut], input[cut:])
				assertClusters(t, input, got, "split")
			})
		}
	}
}

// TestIncrementalSingleByte feeds every byte in its own Feed call, verifying
// absolute offsets and widths even for long combining chains.
func TestIncrementalSingleByte(t *testing.T) {
	for _, s := range incrementalTestCases() {
		input := []byte(s)
		g := NewIncrementalGraphemes()
		var got []GraphemeCluster
		for _, b := range input {
			clusters, err := g.Feed([]byte{b})
			if err != nil {
				t.Fatalf("%q: Feed returned unexpected error: %v", s, err)
			}
			got = append(got, clusters...)
		}
		clusters, err := g.Finish()
		if err != nil {
			t.Fatalf("%q: Finish returned unexpected error: %v", s, err)
		}
		got = append(got, clusters...)
		assertClusters(t, input, got, "single-byte")
	}
}

// TestIncrementalFeedHoldsOpenCluster explicitly checks that Feed returns no
// cluster while a cluster could still be extended, and that Finish delivers
// the held tail cluster in one go.
func TestIncrementalFeedHoldsOpenCluster(t *testing.T) {
	// One multi-codepoint cluster only: every Feed must return nothing and
	// Finish must deliver the whole cluster.
	input := []byte("🏳️‍🌈")
	g := NewIncrementalGraphemes()
	for i := 0; i < len(input); i++ {
		clusters, err := g.Feed(input[i : i+1])
		if err != nil {
			t.Fatalf("Feed %d returned unexpected error: %v", i, err)
		}
		if len(clusters) != 0 {
			t.Fatalf("Feed %d emitted %d clusters prematurely: %+v",
				i, len(clusters), clusters)
		}
	}
	clusters, err := g.Finish()
	if err != nil {
		t.Fatalf("Finish returned unexpected error: %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("Finish emitted %d clusters, want 1: %+v", len(clusters), clusters)
	}
	if !bytes.Equal(clusters[0].Bytes, input) {
		t.Errorf("Finish cluster = %q, want %q", clusters[0].Bytes, input)
	}
	if clusters[0].From != 0 || clusters[0].To != len(input) {
		t.Errorf("Finish range = [%d,%d), want [0,%d)",
			clusters[0].From, clusters[0].To, len(input))
	}
	if clusters[0].Width != 2 {
		t.Errorf("Finish width = %d, want 2", clusters[0].Width)
	}

	// "a" followed byte by byte by a combining chain. The cluster is open
	// until Finish; earlier clusters (the preceding "x") are already out.
	long := "x" + "a" + strings.Repeat("\u0308", 50)
	data := []byte(long)
	g2 := NewIncrementalGraphemes()
	var early []GraphemeCluster
	for _, b := range data {
		clusters, err := g2.Feed([]byte{b})
		if err != nil {
			t.Fatalf("Feed returned unexpected error: %v", err)
		}
		early = append(early, clusters...)
	}
	if len(early) != 1 || string(early[0].Bytes) != "x" {
		t.Fatalf("before Finish got %d clusters, want only %q: %+v",
			len(early), "x", early)
	}
	final, err := g2.Finish()
	if err != nil {
		t.Fatalf("Finish returned unexpected error: %v", err)
	}
	if len(final) != 1 || string(final[0].Bytes) != "a"+strings.Repeat("\u0308", 50) {
		t.Fatalf("Finish got %+v, want the long combining chain", final)
	}
	if final[0].From != 1 || final[0].To != len(data) {
		t.Errorf("long chain range = [%d,%d), want [1,%d)",
			final[0].From, final[0].To, len(data))
	}
	assertClusters(t, data, append(early, final...), "long-chain")
}

// TestIncrementalRegionalIndicators checks that the Regional Indicator pairing
// (odd/even) context is preserved across blocks.
func TestIncrementalRegionalIndicators(t *testing.T) {
	// Three RIs: first two form one flag, the third closes alone at EOF.
	input := []byte("🇩🇪🇽")
	for cut := 0; cut <= len(input); cut++ {
		g := NewIncrementalGraphemes()
		got := feedSegments(t, g, input[:cut], input[cut:])
		assertClusters(t, input, got, "RI split")
	}

	// Four RIs: two flags.
	input4 := []byte("🇩🇪🇦🇨")
	g := NewIncrementalGraphemes()
	got := feedSegments(t, g, input4[:2], input4[2:6], input4[6:])
	assertClusters(t, input4, got, "RI four")

	// A single RI split between the two Feeds stays open until Finish.
	ri := []byte("🇽")
	g2 := NewIncrementalGraphemes()
	first, err := g2.Feed(ri[:2])
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 0 {
		t.Fatalf("partial RI emitted a cluster: %+v", first)
	}
	next, err := g2.Feed(ri[2:])
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 0 {
		t.Fatalf("completed RI emitted a cluster before Finish: %+v", next)
	}
	final, err := g2.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if len(final) != 1 || !bytes.Equal(final[0].Bytes, ri) {
		t.Fatalf("partial RI Finish = %+v, want %q", final, ri)
	}
}

// TestIncrementalZWJEmoji checks emoji ZWJ sequences split by block
// boundaries stay a single cluster.
func TestIncrementalZWJEmoji(t *testing.T) {
	input := []byte("👩‍❤️‍💋‍👩")
	// Split directly around the ZWJ and around the FE0F.
	for _, cut := range []int{
		bytes.Index(input, []byte{0xe2, 0x80, 0x8d}),
		bytes.Index(input, []byte{0xef, 0xb8, 0x8f}),
		len(input) - 4,
		1,
		len(input) / 2,
	} {
		g := NewIncrementalGraphemes()
		got := feedSegments(t, g, input[:cut], input[cut:])
		assertClusters(t, input, got, "ZWJ split")
	}

	// EP ZWJ where the following EP arrives in the next block must not close
	// after the ZWJ: "🙂‍🙂" then "a".
	joined := []byte("🙂\u200d🙂a")
	for cut := 0; cut <= len(joined); cut++ {
		g := NewIncrementalGraphemes()
		got := feedSegments(t, g, joined[:cut], joined[cut:])
		assertClusters(t, joined, got, "EP-ZWJ split")
	}
}

// TestIncrementalPrependAndExtend checks Prepend and Extend context across
// blocks, including a Prepend held open at the end of a block and CRLF.
func TestIncrementalPrependAndExtend(t *testing.T) {
	cases := [][]byte{
		[]byte("\u0600\u0600a"),  // two Prepends then base
		[]byte("\u0600b\u0308"),  // Prepend, base, Extend
		[]byte("a\u0308\u0300b"), // extend chain then new cluster
		[]byte("\r\nx"),          // CR LF cluster
		[]byte("\rx"),            // CR followed by a base: two clusters
		[]byte("x\ry"),           // CR in the middle
	}
	for _, input := range cases {
		for cut := 0; cut <= len(input); cut++ {
			g := NewIncrementalGraphemes()
			got := feedSegments(t, g, input[:cut], input[cut:])
			assertClusters(t, input, got, "prepend/extend split")
		}
	}

	// Prepend alone at EOF is its own cluster; Prepend plus arriving base
	// merge.
	g := NewIncrementalGraphemes()
	if c, err := g.Feed([]byte("\u0600")); err != nil || len(c) != 0 {
		t.Fatalf("Prepend Feed: clusters=%+v err=%v", c, err)
	}
	c, err := g.Feed([]byte("a"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c) != 0 {
		t.Fatalf("Prepend+base emitted a cluster before Finish: %+v", c)
	}
	final, err := g.Finish()
	if err != nil || len(final) != 1 || string(final[0].Bytes) != "\u0600a" {
		t.Fatalf("Prepend Finish = %+v err=%v", final, err)
	}
}

// TestIncrementalInvalidUTF8 verifies invalid and truncated bytes pass through
// unchanged and match the byte interface, including a truncated encoding
// completed or made invalid by the next block.
func TestIncrementalInvalidUTF8(t *testing.T) {
	for _, s := range invalidUTF8Cases {
		input := []byte(s)
		// Single byte feeding is the strongest check.
		g := NewIncrementalGraphemes()
		var got []GraphemeCluster
		for _, b := range input {
			c, err := g.Feed([]byte{b})
			if err != nil {
				t.Fatalf("% x: Feed error: %v", input, err)
			}
			got = append(got, c...)
		}
		c, err := g.Finish()
		if err != nil {
			t.Fatalf("% x: Finish error: %v", input, err)
		}
		got = append(got, c...)
		assertClusters(t, input, got, "invalid utf8")

		// Also split at each byte position.
		for cut := 0; cut <= len(input); cut++ {
			g2 := NewIncrementalGraphemes()
			got2 := feedSegments(t, g2, input[:cut], input[cut:])
			assertClusters(t, input, got2, "invalid utf8 split")
		}
	}

	// A truncated 3-byte start that becomes invalid once a complete encoding
	// is available must not lose or "repair" bytes.
	input := []byte{'a', 0xe0, 0x80, 'b'}
	g := NewIncrementalGraphemes()
	got := feedSegments(t, g, input[:2], input[2:])
	assertClusters(t, input, got, "truncated becomes invalid")
	if merged := bytes.Join(func() [][]byte {
		out := make([][]byte, len(got))
		for i := range got {
			out[i] = got[i].Bytes
		}
		return out
	}(), nil); !bytes.Equal(merged, input) {
		t.Errorf("emitted bytes % x, want % x", merged, input)
	}
}

// TestIncrementalEmittedImmutable verifies clusters handed out earlier are not
// modified by later Feed, Finish, or Reset calls.
func TestIncrementalEmittedImmutable(t *testing.T) {
	input := []byte("möp🏳️‍🌈x")
	g := NewIncrementalGraphemes()

	// Force early emissions by feeding enough for several clusters.
	early, err := g.Feed(input[:4]) // "möp" -> "m", "ö" emitted, "p" open
	if err != nil {
		t.Fatal(err)
	}
	if len(early) < 2 {
		t.Fatalf("expected at least 2 early clusters, got %+v", early)
	}
	snapshots := make([][]byte, len(early))
	for i := range early {
		snapshots[i] = append([]byte(nil), early[i].Bytes...)
	}

	if _, err := g.Feed(input[4:]); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Finish(); err != nil {
		t.Fatal(err)
	}
	for i, c := range early {
		if !bytes.Equal(c.Bytes, snapshots[i]) {
			t.Errorf("early cluster %d changed to %x, want %x", i, c.Bytes, snapshots[i])
		}
	}

	// Reset must not touch retained results either.
	g.Reset()
	for i, c := range early {
		if !bytes.Equal(c.Bytes, snapshots[i]) {
			t.Errorf("early cluster %d changed after Reset to %x, want %x",
				i, c.Bytes, snapshots[i])
		}
	}
}

// TestIncrementalDoesNotRetainInput verifies the parser does not keep the
// caller's data slices after Feed returns: mutating them must not corrupt
// later output.
func TestIncrementalDoesNotRetainInput(t *testing.T) {
	g := NewIncrementalGraphemes()
	block := []byte("ab")
	early, err := g.Feed(block)
	if err != nil {
		t.Fatal(err)
	}
	if len(early) != 1 || string(early[0].Bytes) != "a" {
		t.Fatalf("early clusters = %+v, want just %q", early, "a")
	}
	for i := range block {
		block[i] = 0xff
	}
	rest, err := g.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 1 || string(rest[0].Bytes) != "b" {
		t.Fatalf("Finish clusters = %+v, want just %q", rest, "b")
	}
	if string(early[0].Bytes) != "a" {
		t.Errorf("emitted cluster corrupted after input mutation: %q", early[0].Bytes)
	}
}

// TestIncrementalLifecycle covers Finish errors, Reset reuse, zero values, and
// empty streams.
func TestIncrementalLifecycle(t *testing.T) {
	g := NewIncrementalGraphemes()

	// Empty stream.
	c, err := g.Finish()
	if err != nil {
		t.Fatalf("empty Finish error: %v", err)
	}
	if len(c) != 0 {
		t.Fatalf("empty Finish emitted clusters: %+v", c)
	}

	// Feed after Finish is an explicit error and changes nothing.
	if _, err := g.Feed([]byte("x")); err != ErrIncrementalFinished {
		t.Errorf("Feed after Finish error = %v, want ErrIncrementalFinished", err)
	}
	if _, err := g.Finish(); err != ErrIncrementalFinished {
		t.Errorf("second Finish error = %v, want ErrIncrementalFinished", err)
	}

	// Reset reuses the object.
	g.Reset()
	got := feedSegments(t, g, []byte("möp"))
	assertClusters(t, []byte("möp"), got, "after reset")

	// The zero value is usable.
	var zero IncrementalGraphemes
	got = feedSegments(t, &zero, []byte("🙂🙂"))
	assertClusters(t, []byte("🙂🙂"), got, "zero value")

	// Reset after Finish works as well.
	g.Reset()
	if c, err := g.Feed([]byte("a")); err != nil || len(c) != 0 {
		t.Fatalf("post-reset Feed = %+v err=%v", c, err)
	}
	c, err = g.Finish()
	if err != nil || len(c) != 1 || string(c[0].Bytes) != "a" {
		t.Fatalf("post-reset Finish = %+v err=%v", c, err)
	}
}

// TestIncrementalMultipleBlocksOffsets feeds several arbitrary blocks and
// checks the absolute ranges tile the whole input.
func TestIncrementalMultipleBlocksOffsets(t *testing.T) {
	input := []byte("a🇩🇪möp🏳️‍🌈x🙂")
	cuts := []int{0, 1, 2, 9, 10, 14, 15, len(input)}
	g := NewIncrementalGraphemes()
	var got []GraphemeCluster
	for i := 1; i < len(cuts); i++ {
		c, err := g.Feed(input[cuts[i-1]:cuts[i]])
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, c...)
	}
	c, err := g.Finish()
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, c...)
	assertClusters(t, input, got, "multi-block offsets")

	// Offsets must be strictly contiguous.
	pos := 0
	for _, cl := range got {
		if cl.From != pos {
			t.Errorf("cluster %q starts at %d, want %d", cl.Bytes, cl.From, pos)
		}
		pos = cl.To
	}
}

// shortName produces a stable, short subtest label.
func shortName(s string) string {
	r := []rune(s)
	if len(r) <= 6 {
		return s
	}
	return string(r[:6]) + "…"
}
