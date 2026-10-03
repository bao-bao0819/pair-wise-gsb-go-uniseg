package uniseg

import (
	"bytes"
	"testing"
)

// referenceClusters parses the whole input at once with FirstGraphemeCluster,
// the existing byte interface. This is the ground truth every incremental
// result is compared against.
func referenceClusters(t *testing.T, input []byte) []GraphemeCluster {
	t.Helper()
	rest := input
	state := -1
	offset := 0
	var clusters []GraphemeCluster
	for len(rest) > 0 {
		var c []byte
		var w int
		c, rest, w, state = FirstGraphemeCluster(rest, state)
		clusters = append(clusters, GraphemeCluster{
			Bytes: append([]byte(nil), c...),
			From:  offset,
			To:    offset + len(c),
			Width: w,
		})
		offset += len(c)
	}
	return clusters
}

// equalClusters compares two cluster lists by content, absolute byte range, and
// width.
func equalClusters(got, want []GraphemeCluster) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if !bytes.Equal(got[i].Bytes, want[i].Bytes) ||
			got[i].From != want[i].From ||
			got[i].To != want[i].To ||
			got[i].Width != want[i].Width {
			return false
		}
	}
	return true
}

// splitFeed runs one parser over input delivered in chunks according to the
// given cut positions (each cut k feeds input[prev:k]), calls Finish, and
// returns everything delivered.
func splitFeed(t *testing.T, input []byte, cuts ...int) []GraphemeCluster {
	t.Helper()
	g := NewIncrementalGraphemes()
	var got []GraphemeCluster
	prev := 0
	for _, cut := range cuts {
		if cut > prev {
			cs, err := g.Feed(input[prev:cut])
			if err != nil {
				t.Fatalf("unexpected Feed error: %v", err)
			}
			got = append(got, cs...)
		}
		prev = cut
	}
	if prev < len(input) {
		cs, err := g.Feed(input[prev:])
		if err != nil {
			t.Fatalf("unexpected Feed error: %v", err)
		}
		got = append(got, cs...)
	}
	cs, err := g.Finish()
	if err != nil {
		t.Fatalf("unexpected Finish error: %v", err)
	}
	got = append(got, cs...)
	return got
}

// TestIncrementalAllCasesEveryByteCut splits every existing grapheme test case
// at every possible byte position, feeds the two pieces separately, and
// compares the result against a single-shot parse.
func TestIncrementalAllCasesEveryByteCut(t *testing.T) {
	allCases := append(append([]testCase{}, testCases...), graphemeBreakTestCases...)
	for testNum, tc := range allCases {
		input := []byte(tc.original)
		want := referenceClusters(t, input)
		for cut := 0; cut <= len(input); cut++ {
			got := splitFeed(t, input, cut)
			if !equalClusters(got, want) {
				t.Fatalf("test case %d %q cut at %d: got %#v, want %#v",
					testNum, tc.original, cut, got, want)
			}
		}
	}
}

// TestIncrementalSingleByteFeeds delivers every byte in its own Feed call,
// which exercises absolute offsets and combining marks delivered one byte at a
// time.
func TestIncrementalSingleByteFeeds(t *testing.T) {
	allCases := append(append([]testCase{}, testCases...), graphemeBreakTestCases...)
	for testNum, tc := range allCases {
		input := []byte(tc.original)
		want := referenceClusters(t, input)
		g := NewIncrementalGraphemes()
		var got []GraphemeCluster
		for i := range input {
			cs, err := g.Feed(input[i : i+1])
			if err != nil {
				t.Fatalf("test case %d: unexpected Feed error: %v", testNum, err)
			}
			got = append(got, cs...)
		}
		cs, err := g.Finish()
		if err != nil {
			t.Fatalf("test case %d: unexpected Finish error: %v", testNum, err)
		}
		got = append(got, cs...)
		if !equalClusters(got, want) {
			t.Fatalf("test case %d %q single-byte feeds: got %#v, want %#v",
				testNum, tc.original, got, want)
		}
	}
}

// TestIncrementalFeedEmitsNothing demonstrates that a Feed ending inside a
// multi-byte rune delivers nothing and that Finish delivers the held cluster in
// a single result.
func TestIncrementalFeedEmitsNothing(t *testing.T) {
	input := []byte("😀")
	g := NewIncrementalGraphemes()

	cs, err := g.Feed(input[:1])
	if err != nil {
		t.Fatalf("unexpected Feed error: %v", err)
	}
	if len(cs) != 0 {
		t.Fatalf("expected no cluster after first byte, got %#v", cs)
	}

	cs, err = g.Feed(input[1:3])
	if err != nil {
		t.Fatalf("unexpected Feed error: %v", err)
	}
	if len(cs) != 0 {
		t.Fatalf("expected no cluster inside the rune, got %#v", cs)
	}

	cs, err = g.Feed(input[3:])
	if err != nil {
		t.Fatalf("unexpected Feed error: %v", err)
	}
	if len(cs) != 0 {
		t.Fatalf("expected last cluster to be held until Finish, got %#v", cs)
	}

	cs, err = g.Finish()
	if err != nil {
		t.Fatalf("unexpected Finish error: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("expected exactly one final cluster, got %#v", cs)
	}
	if !bytes.Equal(cs[0].Bytes, input) || cs[0].From != 0 || cs[0].To != 4 || cs[0].Width != 2 {
		t.Fatalf("unexpected final cluster: %#v", cs[0])
	}

	// A second Finish is an error.
	if _, err := g.Finish(); err != ErrGraphemesFinished {
		t.Fatalf("expected ErrGraphemesFinished, got %v", err)
	}
}

// TestIncrementalLongCombiningChain feeds a base plus a long chain of combining
// marks one byte at a time and checks offsets and the single resulting cluster.
func TestIncrementalLongCombiningChain(t *testing.T) {
	input := []byte("a")
	for i := 0; i < 200; i++ {
		input = append(input, []byte("\u0308")...) // combining diaeresis
	}
	g := NewIncrementalGraphemes()
	var delivered int
	for i := range input {
		cs, err := g.Feed(input[i : i+1])
		if err != nil {
			t.Fatalf("unexpected Feed error: %v", err)
		}
		delivered += len(cs)
	}
	if delivered != 0 {
		t.Fatalf("expected the combining chain to stay open, %d clusters delivered early", delivered)
	}
	cs, err := g.Finish()
	if err != nil {
		t.Fatalf("unexpected Finish error: %v", err)
	}
	want := referenceClusters(t, input)
	if !equalClusters(cs, want) {
		t.Fatalf("long combining chain mismatch: got %#v, want %#v", cs, want)
	}
}

// TestIncrementalRegionalIndicators checks RI pairing across chunk boundaries,
// including odd/even runs of three and four RI code points.
func TestIncrementalRegionalIndicators(t *testing.T) {
	for _, s := range []string{
		"🇩🇪",   // two RI: one flag
		"🇦🇧🇨",  // three RI: flag + lone RI
		"🇦🇧🇨🇩", // four RI: two flags
	} {
		input := []byte(s)
		want := referenceClusters(t, input)
		for cut := 0; cut <= len(input); cut++ {
			if got := splitFeed(t, input, cut); !equalClusters(got, want) {
				t.Fatalf("RI %q cut at %d: got %#v, want %#v", s, cut, got, want)
			}
		}
	}
}

// TestIncrementalEmojiZWJ verifies an emoji ZWJ sequence split at every byte
// position is not broken apart.
func TestIncrementalEmojiZWJ(t *testing.T) {
	input := []byte("👩‍❤️‍💋‍👩")
	want := referenceClusters(t, input)
	if len(want) != 1 {
		t.Fatalf("sanity: expected one cluster, got %d", len(want))
	}
	for cut := 0; cut <= len(input); cut++ {
		if got := splitFeed(t, input, cut); !equalClusters(got, want) {
			t.Fatalf("emoji ZWJ cut at %d: got %#v, want %#v", cut, got, want)
		}
	}
}

// TestIncrementalPrepend verifies Prepend code points keep absorbing following
// content across chunk boundaries (GB9b).
func TestIncrementalPrepend(t *testing.T) {
	input := []byte("\u0600a\u0308")
	want := referenceClusters(t, input)
	for cut := 0; cut <= len(input); cut++ {
		if got := splitFeed(t, input, cut); !equalClusters(got, want) {
			t.Fatalf("prepend cut at %d: got %#v, want %#v", cut, got, want)
		}
	}
}

// TestIncrementalCRLF joins a CR and an LF delivered in different chunks.
func TestIncrementalCRLF(t *testing.T) {
	input := []byte("\r\n")
	g := NewIncrementalGraphemes()
	cs, err := g.Feed(input[:1])
	if err != nil {
		t.Fatalf("unexpected Feed error: %v", err)
	}
	if len(cs) != 0 {
		t.Fatalf("CR must be held pending a possible LF, got %#v", cs)
	}
	cs, err = g.Feed(input[1:])
	if err != nil {
		t.Fatalf("unexpected Feed error: %v", err)
	}
	if len(cs) != 1 || len(cs[0].Bytes) != 2 {
		t.Fatalf("CRLF must be delivered once LF arrives, got %#v", cs)
	}
	want := referenceClusters(t, input)
	if !equalClusters(cs, want) {
		t.Fatalf("CRLF early delivery mismatch: got %#v, want %#v", cs, want)
	}
	cs, err = g.Finish()
	if err != nil {
		t.Fatalf("unexpected Finish error: %v", err)
	}
	if len(cs) != 0 {
		t.Fatalf("Finish must not redeliver CRLF, got %#v", cs)
	}
}

// TestIncrementalControlEager checks that a control character closes a cluster
// immediately (GB4/GB5), even mid-Feed.
func TestIncrementalControlEager(t *testing.T) {
	g := NewIncrementalGraphemes()
	cs, err := g.Feed([]byte("a\tb"))
	if err != nil {
		t.Fatalf("unexpected Feed error: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("expected a and tab delivered eagerly, got %#v", cs)
	}
	if string(cs[0].Bytes) != "a" || string(cs[1].Bytes) != "\t" {
		t.Fatalf("unexpected eager clusters: %q %q", cs[0].Bytes, cs[1].Bytes)
	}
	if cs[0].From != 0 || cs[0].To != 1 || cs[1].From != 1 || cs[1].To != 2 {
		t.Fatalf("unexpected eager offsets: %#v", cs)
	}
	cs, err = g.Finish()
	if err != nil {
		t.Fatalf("unexpected Finish error: %v", err)
	}
	if len(cs) != 1 || string(cs[0].Bytes) != "b" {
		t.Fatalf("expected final b, got %#v", cs)
	}
}

// TestIncrementalInvalidUTF8 ensures invalid and truncated bytes are preserved
// verbatim and segmented identically to a single-shot parse.
func TestIncrementalInvalidUTF8(t *testing.T) {
	cases := [][]byte{
		{0xff},                   // invalid leading byte
		{0x80},                   // stray continuation byte
		{'a', 0xff, 'b'},         // error in the middle
		{0xe0, 0xff, 0x80, 0x80}, // lead that can never be completed
		{0xe0},                   // truncated lead at the real end
		{'a', 0x80},              // stray continuation after a base
		{0xf0, 0x9f},             // truncated, potentially valid prefix
		{0xed, 0xa0, 0x80},       // surrogate code point: three one-byte errors
		{0xf4, 0x90, 0x80, 0x80}, // above U+10FFFF
		{0xf0, 0x80, 0x80, 0x80}, // overlong four-byte encoding
		{0xc0, 0x80},             // overlong two-byte NUL
	}
	for ci, input := range cases {
		want := referenceClusters(t, input)
		// Single byte at a time.
		g := NewIncrementalGraphemes()
		var got []GraphemeCluster
		for i := range input {
			cs, err := g.Feed(input[i : i+1])
			if err != nil {
				t.Fatalf("case %d: unexpected Feed error: %v", ci, err)
			}
			got = append(got, cs...)
		}
		cs, err := g.Finish()
		if err != nil {
			t.Fatalf("case %d: unexpected Finish error: %v", ci, err)
		}
		got = append(got, cs...)
		if !equalClusters(got, want) {
			t.Fatalf("case %d % x: got %#v, want %#v", ci, input, got, want)
		}

		// Also cut at every byte position.
		for cut := 0; cut <= len(input); cut++ {
			if got := splitFeed(t, input, cut); !equalClusters(got, want) {
				t.Fatalf("case %d % x cut at %d: got %#v, want %#v", ci, input, cut, got, want)
			}
		}

		// Raw bytes must survive unchanged.
		var reassembled []byte
		for _, c := range got {
			reassembled = append(reassembled, c.Bytes...)
		}
		if !bytes.Equal(reassembled, input) {
			t.Fatalf("case %d: bytes were modified: got % x, want % x", ci, reassembled, input)
		}
	}
}

// TestIncrementalFeedAfterFinish returns a clear error and Reset reuses the
// parser.
func TestIncrementalFeedAfterFinishAndReset(t *testing.T) {
	g := NewIncrementalGraphemes()
	if _, err := g.Feed([]byte("ab")); err != nil {
		t.Fatalf("unexpected Feed error: %v", err)
	}
	if _, err := g.Finish(); err != nil {
		t.Fatalf("unexpected Finish error: %v", err)
	}
	if _, err := g.Feed([]byte("x")); err != ErrGraphemesFinished {
		t.Fatalf("expected ErrGraphemesFinished on Feed, got %v", err)
	}
	if _, err := g.Finish(); err != ErrGraphemesFinished {
		t.Fatalf("expected ErrGraphemesFinished on Finish, got %v", err)
	}

	// Reset makes the object reusable for a fresh stream.
	g.Reset()
	input := []byte("möp😀")
	got := splitFeed(t, input, 1)
	want := referenceClusters(t, input)
	if !equalClusters(got, want) {
		t.Fatalf("after Reset: got %#v, want %#v", got, want)
	}
}

// TestIncrementalZeroValue ensures a zero-valued parser works and that an empty
// stream Finish returns nothing.
func TestIncrementalZeroValue(t *testing.T) {
	var g IncrementalGraphemes
	cs, err := g.Feed([]byte("a"))
	if err != nil {
		t.Fatalf("unexpected Feed error: %v", err)
	}
	if len(cs) != 0 {
		t.Fatalf("expected single cluster held, got %#v", cs)
	}
	cs, err = g.Finish()
	if err != nil {
		t.Fatalf("unexpected Finish error: %v", err)
	}
	if len(cs) != 1 || string(cs[0].Bytes) != "a" {
		t.Fatalf("unexpected zero-value result: %#v", cs)
	}

	var empty IncrementalGraphemes
	cs, err = empty.Finish()
	if err != nil {
		t.Fatalf("unexpected Finish error: %v", err)
	}
	if len(cs) != 0 {
		t.Fatalf("expected no clusters for empty input, got %#v", cs)
	}
}

// TestIncrementalDeliveredBytesImmutable mutates the caller's input and earlier
// results and verifies already delivered clusters are not changed.
func TestIncrementalDeliveredBytesImmutable(t *testing.T) {
	g := NewIncrementalGraphemes()
	chunk := []byte("a\t")
	first, err := g.Feed(chunk)
	if err != nil {
		t.Fatalf("unexpected Feed error: %v", err)
	}
	if len(first) != 2 || string(first[0].Bytes) != "a" || string(first[1].Bytes) != "\t" {
		t.Fatalf("unexpected initial clusters: %#v", first)
	}
	// Deliver more data and force internal buffer reuse/compaction.
	var later []GraphemeCluster
	for i := 0; i < 10; i++ {
		cs, err := g.Feed([]byte("xy\t"))
		if err != nil {
			t.Fatalf("unexpected Feed error: %v", err)
		}
		later = append(later, cs...)
	}
	if _, err := g.Finish(); err != nil {
		t.Fatalf("unexpected Finish error: %v", err)
	}
	// Earlier results must be untouched.
	// Mutating the caller's original chunk and all previously returned bytes
	// must not corrupt anything still held by the parser.
	for i := range chunk {
		chunk[i] = 0
	}
	for _, c := range later {
		for i := range c.Bytes {
			c.Bytes[i] = 0
		}
	}
	if string(first[0].Bytes) != "a" || string(first[1].Bytes) != "\t" {
		t.Fatalf("delivered clusters changed after mutation: %#v", first)
	}
}

// TestIncrementalRandomChunking feeds a complex string using a few fixed chunk
// schedules to complement the single-byte and two-way-cut coverage.
func TestIncrementalRandomChunking(t *testing.T) {
	input := []byte("a🇩🇪b\r\nx👩‍❤️‍💋‍👩y möp 🇦🇧🇨\t!")
	want := referenceClusters(t, input)
	schedules := [][]int{
		{1, 1, 2, 3, 1, 5, 2, 4},
		{3, 3, 3, 3, 3, 3},
		{1, len(input) - 1},
	}
	for si, sched := range schedules {
		g := NewIncrementalGraphemes()
		var got []GraphemeCluster
		pos := 0
		for _, n := range sched {
			end := pos + n
			if end > len(input) {
				end = len(input)
			}
			cs, err := g.Feed(input[pos:end])
			if err != nil {
				t.Fatalf("schedule %d: unexpected Feed error: %v", si, err)
			}
			got = append(got, cs...)
			pos = end
		}
		if pos < len(input) {
			cs, err := g.Feed(input[pos:])
			if err != nil {
				t.Fatalf("schedule %d: unexpected Feed error: %v", si, err)
			}
			got = append(got, cs...)
		}
		cs, err := g.Finish()
		if err != nil {
			t.Fatalf("schedule %d: unexpected Finish error: %v", si, err)
		}
		got = append(got, cs...)
		if !equalClusters(got, want) {
			t.Fatalf("schedule %d: got %#v, want %#v", si, got, want)
		}
	}
}

// TestIncrementalFuzzCuts deterministically builds many inputs (including
// invalid UTF-8) and feeds them through random multi-chunk schedules, comparing
// against a single-shot parse.
func TestIncrementalFuzzCuts(t *testing.T) {
	alphabet := []rune("a\t\r\nmöp🏳️‍🌈🇩🇪👩‍❤️‍💋‍👩\u0308\u0600\U0001F1E6")
	raw := []byte{0xff, 0x80, 0xe0, 0xf0, 0x9f}
	seed := uint32(0x12345678)
	next := func() uint32 {
		seed = seed*1664525 + 1013904223
		return seed
	}
	for iter := 0; iter < 400; iter++ {
		var input []byte
		for n := 0; n < 12; n++ {
			if next()%5 == 0 {
				input = append(input, raw[next()%uint32(len(raw))])
			} else {
				input = append(input, []byte(string(alphabet[next()%uint32(len(alphabet))]))...)
			}
		}
		want := referenceClusters(t, input)

		g := NewIncrementalGraphemes()
		var got []GraphemeCluster
		pos := 0
		for pos < len(input) {
			n := int(next()%4) + 1
			end := pos + n
			if end > len(input) {
				end = len(input)
			}
			cs, err := g.Feed(input[pos:end])
			if err != nil {
				t.Fatalf("iter %d: unexpected Feed error: %v", iter, err)
			}
			got = append(got, cs...)
			pos = end
		}
		cs, err := g.Finish()
		if err != nil {
			t.Fatalf("iter %d: unexpected Finish error: %v", iter, err)
		}
		got = append(got, cs...)
		if !equalClusters(got, want) {
			t.Fatalf("iter %d input % x: got %#v, want %#v", iter, input, got, want)
		}
	}
}
