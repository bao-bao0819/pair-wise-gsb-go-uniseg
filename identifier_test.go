package uniseg

import (
	"testing"
)

type expectedIdentifierToken struct {
	text   string
	class  IdentifierTokenClass
	reason IdentifierTokenReason
}

func TestTokenizeIdentifiers(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []expectedIdentifierToken
	}{
		{
			name: "HTTPServer_v2",
			text: "HTTPServer_v2",
			want: []expectedIdentifierToken{
				{"HTTP", IdentifierTokenSubword, IdentifierTokenStart},
				{"Server", IdentifierTokenSubword, IdentifierTokenUppercaseSequence},
				{"_", IdentifierTokenSeparator, IdentifierTokenUnderscore},
				{"v", IdentifierTokenSubword, IdentifierTokenAfterUnderscore},
				{"2", IdentifierTokenSubword, IdentifierTokenLetterToDigit},
			},
		},
		{
			name: "uppercase acronym",
			text: "XMLReader",
			want: []expectedIdentifierToken{
				{"XML", IdentifierTokenSubword, IdentifierTokenStart},
				{"Reader", IdentifierTokenSubword, IdentifierTokenUppercaseSequence},
			},
		},
		{
			name: "combining accents",
			text: "cafe\u0301HTTP\u0391\u0301Reader",
			want: []expectedIdentifierToken{
				{"cafe\u0301", IdentifierTokenSubword, IdentifierTokenStart},
				{"HTTP\u0391\u0301", IdentifierTokenSubword, IdentifierTokenLowerToUpper},
				{"Reader", IdentifierTokenSubword, IdentifierTokenUppercaseSequence},
			},
		},
		{
			name: "combining mark preserves acronym state",
			text: "XM\u039b\u0301e",
			want: []expectedIdentifierToken{
				{"XM", IdentifierTokenSubword, IdentifierTokenStart},
				{"\u039b\u0301e", IdentifierTokenSubword, IdentifierTokenUppercaseSequence},
			},
		},
		{
			name: "greek camel case",
			text: "ΌνομαΜέγα",
			want: []expectedIdentifierToken{
				{"Όνομα", IdentifierTokenSubword, IdentifierTokenStart},
				{"Μέγα", IdentifierTokenSubword, IdentifierTokenLowerToUpper},
			},
		},
		{
			name: "chinese with digits",
			text: "价格100元20",
			want: []expectedIdentifierToken{
				{"价格", IdentifierTokenSubword, IdentifierTokenStart},
				{"100", IdentifierTokenSubword, IdentifierTokenLetterToDigit},
				{"元", IdentifierTokenSubword, IdentifierTokenDigitToLetter},
				{"20", IdentifierTokenSubword, IdentifierTokenLetterToDigit},
			},
		},
		{
			name: "standalone combining mark",
			text: "a_\u0301b",
			want: []expectedIdentifierToken{
				{"a", IdentifierTokenSubword, IdentifierTokenStart},
				{"_\u0301", IdentifierTokenSeparator, IdentifierTokenUnderscore},
				{"b", IdentifierTokenSubword, IdentifierTokenAfterUnderscore},
			},
		},
		{
			name: "digits with combining mark",
			text: "a1\u0301b",
			want: []expectedIdentifierToken{
				{"a", IdentifierTokenSubword, IdentifierTokenStart},
				{"1\u0301", IdentifierTokenSubword, IdentifierTokenLetterToDigit},
				{"b", IdentifierTokenSubword, IdentifierTokenDigitToLetter},
			},
		},
		{
			name: "consecutive underscores",
			text: "_a__b_",
			want: []expectedIdentifierToken{
				{"_", IdentifierTokenSeparator, IdentifierTokenUnderscore},
				{"a", IdentifierTokenSubword, IdentifierTokenAfterUnderscore},
				{"__", IdentifierTokenSeparator, IdentifierTokenUnderscore},
				{"b", IdentifierTokenSubword, IdentifierTokenAfterUnderscore},
				{"_", IdentifierTokenSeparator, IdentifierTokenUnderscore},
			},
		},
		{
			name: "emoji gap",
			text: "id😀🎉x",
			want: []expectedIdentifierToken{
				{"id", IdentifierTokenSubword, IdentifierTokenStart},
				{"😀", IdentifierTokenOther, IdentifierTokenStandardWord},
				{"🎉", IdentifierTokenOther, IdentifierTokenStandardWord},
				{"x", IdentifierTokenSubword, IdentifierTokenStart},
			},
		},
		{
			name: "combining mark attached to emoji",
			text: "a😀\u0301b",
			want: []expectedIdentifierToken{
				{"a", IdentifierTokenSubword, IdentifierTokenStart},
				{"😀\u0301", IdentifierTokenOther, IdentifierTokenStandardWord},
				{"b", IdentifierTokenSubword, IdentifierTokenStart},
			},
		},
		{
			name: "standard punctuation gap",
			text: "a, b",
			want: []expectedIdentifierToken{
				{"a", IdentifierTokenSubword, IdentifierTokenStart},
				{",", IdentifierTokenOther, IdentifierTokenStandardWord},
				{" ", IdentifierTokenOther, IdentifierTokenStandardWord},
				{"b", IdentifierTokenSubword, IdentifierTokenStart},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertIdentifierTokens(t, test.text, TokenizeIdentifiersInString(test.text), test.want)

			b := []byte(test.text)
			assertIdentifierTokens(t, test.text, TokenizeIdentifiers(b), test.want)

			stringTokens := TokenizeIdentifiersInString(test.text)
			byteTokens := TokenizeIdentifiers(b)
			if len(stringTokens) != len(byteTokens) {
				t.Fatalf("string and byte token counts differ: %d != %d", len(stringTokens), len(byteTokens))
			}
			for i := range stringTokens {
				if stringTokens[i] != byteTokens[i] {
					t.Fatalf("token %d differs between string and byte entries: %+v != %+v", i, stringTokens[i], byteTokens[i])
				}
			}
		})
	}
}

func TestTokenizeIdentifiersCoverage(t *testing.T) {
	text := "a1 🇩🇪 x\u0301y cafe\u0301"
	tokens := TokenizeIdentifiersInString(text)

	if len(tokens) == 0 {
		t.Fatal("expected tokens")
	}
	if tokens[0].Start != 0 {
		t.Fatalf("first token starts at %d, want 0", tokens[0].Start)
	}
	for i, token := range tokens {
		if token.Start >= token.End {
			t.Fatalf("token %d has empty range [%d,%d)", i, token.Start, token.End)
		}
		if i > 0 && token.Start != tokens[i-1].End {
			t.Fatalf("token %d leaves a gap: previous ends %d, current starts %d", i, tokens[i-1].End, token.Start)
		}
		if text[token.Start:token.End] == "" {
			t.Fatalf("token %d has empty text", i)
		}
	}
	if last := tokens[len(tokens)-1]; last.End != len(text) {
		t.Fatalf("last token ends at %d, want %d", last.End, len(text))
	}
}

func TestFirstWordRemainsStandardUnicode(t *testing.T) {
	text := "HTTPServer_v2"
	word, rest, state := FirstWordInString(text, -1)
	if word != text || rest != "" || state == -1 {
		t.Fatalf("FirstWordInString(%q) = %q, %q, %d, want the whole text", text, word, rest, state)
	}
}

func assertIdentifierTokens(t *testing.T, text string, got []IdentifierToken, want []expectedIdentifierToken) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %d tokens %+v, want %d", len(got), got, len(want))
	}

	offset := 0
	for i, expected := range want {
		token := got[i]
		if token.Start != offset {
			t.Fatalf("token %d starts at %d, want %d", i, token.Start, offset)
		}
		if text[token.Start:token.End] != expected.text {
			t.Fatalf("token %d text = %q, want %q", i, text[token.Start:token.End], expected.text)
		}
		if token.Class != expected.class {
			t.Fatalf("token %d class = %d, want %d", i, token.Class, expected.class)
		}
		if token.Reason != expected.reason {
			t.Fatalf("token %d reason = %d, want %d", i, token.Reason, expected.reason)
		}
		offset = token.End
	}

	if offset != len(text) {
		t.Fatalf("tokens end at %d, want %d", offset, len(text))
	}
}
