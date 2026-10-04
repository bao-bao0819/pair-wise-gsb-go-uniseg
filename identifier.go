package uniseg

import "unicode"

// IdentifierTokenClass identifies the role of an [IdentifierToken].
type IdentifierTokenClass int

const (
	// IdentifierTokenOther is a token outside an identifier region, segmented
	// according to the standard Unicode word boundary rules.
	IdentifierTokenOther IdentifierTokenClass = iota

	// IdentifierTokenSubword is a letter/number part inside an identifier.
	IdentifierTokenSubword

	// IdentifierTokenSeparator is a maximal run of underscore grapheme
	// clusters.
	IdentifierTokenSeparator
)

// IdentifierTokenReason describes why a new identifier token was started.
type IdentifierTokenReason int

const (
	// IdentifierTokenStandardWord applies to non-identifier text produced by
	// the standard Unicode word boundary rules.
	IdentifierTokenStandardWord IdentifierTokenReason = iota

	// IdentifierTokenStart marks the first subword in an identifier region.
	IdentifierTokenStart

	// IdentifierTokenAfterUnderscore marks a subword beginning after an
	// underscore separator.
	IdentifierTokenAfterUnderscore

	// IdentifierTokenLetterToDigit marks a transition from a letter to a
	// decimal digit.
	IdentifierTokenLetterToDigit

	// IdentifierTokenDigitToLetter marks a transition from a decimal digit to
	// a letter.
	IdentifierTokenDigitToLetter

	// IdentifierTokenLowerToUpper marks a transition from a lowercase letter to
	// an uppercase letter.
	IdentifierTokenLowerToUpper

	// IdentifierTokenUppercaseSequence marks the boundary before the final
	// uppercase letter in a sequence of at least two uppercase letters followed
	// by a lowercase letter, as in "XMLReader" -> "XML", "Reader".
	IdentifierTokenUppercaseSequence

	// IdentifierTokenUnderscore applies to an underscore separator token.
	IdentifierTokenUnderscore
)

// IdentifierToken is a segment returned by identifier tokenization. Byte
// offsets are half-open positions in the original input: the token text is
// input[Start:End]. The original bytes are never normalized or changed to
// lowercase.
type IdentifierToken struct {
	// Start is the inclusive byte offset at which the token starts.
	Start int

	// End is the exclusive byte offset at which the token ends.
	End int

	// Class is the token's role.
	Class IdentifierTokenClass

	// Reason describes the segmentation rule that produced the token.
	Reason IdentifierTokenReason
}

type identifierClusterKind int

const (
	identifierClusterNonIdentifier identifierClusterKind = iota
	identifierClusterLetter
	identifierClusterNumber
	identifierClusterMark
	identifierClusterUnderscore
)

const (
	identifierNoCase = iota
	identifierUpperCase
	identifierLowerCase
)

// TokenizeIdentifiers splits b into identifier-oriented tokens. Maximal
// regions made from Unicode letters, decimal digits, combining marks, and
// underscores are split into identifier subwords and underscore separators.
// Text outside those regions retains the segmentation produced by [FirstWord]
// and is marked as [IdentifierTokenOther].
//
// Grapheme clusters are never split. A combining mark attached to a letter
// remains with that letter, while a mark attached to an emoji or another
// non-identifier base remains part of the non-identifier cluster.
func TokenizeIdentifiers(b []byte) []IdentifierToken {
	return tokenizeIdentifiers(string(b))
}

// TokenizeIdentifiersInString is like [TokenizeIdentifiers] but accepts a
// string. The returned byte offsets are relative to str and match those
// returned for the equivalent byte-slice input.
func TokenizeIdentifiersInString(str string) []IdentifierToken {
	return tokenizeIdentifiers(str)
}

func tokenizeIdentifiers(str string) []IdentifierToken {
	var tokens []IdentifierToken

	pos := 0
	inIdentifier := false
	nonIdentifierStart := 0
	wordActive := false
	wordStart := 0
	wordReason := IdentifierTokenStart
	lastKind := identifierClusterNonIdentifier
	lastCasing := identifierNoCase
	lastClusterStart := 0
	lastTokenIndex := -1
	uppercaseRun := 0

	flushWord := func(end int) {
		if wordActive {
			tokens = append(tokens, IdentifierToken{
				Start:  wordStart,
				End:    end,
				Class:  IdentifierTokenSubword,
				Reason: wordReason,
			})
			wordActive = false
		}
	}

	for pos < len(str) {
		cluster, _, _, _ := FirstGraphemeClusterInString(str[pos:], -1)
		clusterStart := pos
		clusterEnd := pos + len(cluster)
		kind, casing := identifierClusterInfo(cluster)
		isIdentifier := kind != identifierClusterNonIdentifier

		if !inIdentifier {
			if isIdentifier {
				tokens = appendStandardWordTokens(tokens, str, nonIdentifierStart, clusterStart)
				inIdentifier = true
				if kind == identifierClusterUnderscore {
					tokens = appendUnderscoreToken(tokens, clusterStart, clusterEnd)
					lastTokenIndex = len(tokens) - 1
					wordActive = false
					lastKind = identifierClusterUnderscore
					lastCasing = identifierNoCase
					uppercaseRun = 0
				} else {
					wordActive = true
					wordStart = clusterStart
					wordReason = IdentifierTokenStart
					lastKind = kind
					lastCasing = casing
					if casing == identifierUpperCase {
						uppercaseRun = 1
					} else {
						uppercaseRun = 0
					}
				}
				lastClusterStart = clusterStart
			}
		} else if !isIdentifier {
			flushWord(clusterStart)
			inIdentifier = false
			nonIdentifierStart = clusterStart
		} else {
			switch kind {
			case identifierClusterUnderscore:
				flushWord(clusterStart)
				if lastKind == identifierClusterUnderscore {
					tokens[lastTokenIndex].End = clusterEnd
				} else {
					tokens = appendUnderscoreToken(tokens, clusterStart, clusterEnd)
					lastTokenIndex = len(tokens) - 1
				}
				lastKind = identifierClusterUnderscore
				lastCasing = identifierNoCase
				uppercaseRun = 0
			case identifierClusterMark:
				if !wordActive {
					wordActive = true
					wordStart = clusterStart
					if lastKind == identifierClusterUnderscore {
						wordReason = IdentifierTokenAfterUnderscore
					} else {
						wordReason = IdentifierTokenStart
					}
				}
			case identifierClusterLetter, identifierClusterNumber:
				if !wordActive {
					wordActive = true
					wordStart = clusterStart
					if lastKind == identifierClusterUnderscore {
						wordReason = IdentifierTokenAfterUnderscore
					} else {
						wordReason = IdentifierTokenStart
					}
					lastKind = kind
					lastCasing = casing
					uppercaseRun = 0
					if casing == identifierUpperCase {
						uppercaseRun = 1
					}
					break
				}

				hasBoundary := false
				boundary := 0
				reason := IdentifierTokenStandardWord
				if lastKind == identifierClusterLetter && kind == identifierClusterNumber {
					hasBoundary = true
					boundary = clusterStart
					reason = IdentifierTokenLetterToDigit
				} else if lastKind == identifierClusterNumber && kind == identifierClusterLetter {
					hasBoundary = true
					boundary = clusterStart
					reason = IdentifierTokenDigitToLetter
				} else if lastKind == identifierClusterLetter && kind == identifierClusterLetter {
					if lastCasing == identifierLowerCase && casing == identifierUpperCase {
						hasBoundary = true
						boundary = clusterStart
						reason = IdentifierTokenLowerToUpper
					} else if lastCasing == identifierUpperCase && casing == identifierLowerCase && uppercaseRun >= 2 {
						hasBoundary = true
						boundary = lastClusterStart
						reason = IdentifierTokenUppercaseSequence
					}
				}

				if hasBoundary {
					tokens = append(tokens, IdentifierToken{
						Start:  wordStart,
						End:    boundary,
						Class:  IdentifierTokenSubword,
						Reason: wordReason,
					})
					wordStart = boundary
					wordReason = reason
				}

				if kind == identifierClusterLetter {
					if casing == identifierUpperCase {
						if lastKind != identifierClusterLetter || lastCasing != identifierUpperCase ||
							reason == IdentifierTokenLowerToUpper {
							uppercaseRun = 1
						} else if !hasBoundary && lastCasing == identifierUpperCase {
							uppercaseRun++
						}
						lastCasing = identifierUpperCase
					} else {
						if casing == identifierLowerCase {
							lastCasing = identifierLowerCase
						} else {
							lastCasing = identifierNoCase
						}
						uppercaseRun = 0
					}
					lastKind = identifierClusterLetter
				} else {
					lastKind = identifierClusterNumber
					lastCasing = identifierNoCase
					uppercaseRun = 0
				}
			}
			if kind != identifierClusterMark {
				lastClusterStart = clusterStart
			}
		}

		pos = clusterEnd
	}

	if inIdentifier {
		flushWord(len(str))
	} else {
		tokens = appendStandardWordTokens(tokens, str, nonIdentifierStart, len(str))
	}

	return tokens
}

func identifierClusterInfo(cluster string) (identifierClusterKind, int) {
	allIdentifierCharacters := true
	firstKind := identifierClusterNonIdentifier
	casing := identifierNoCase

	for _, r := range cluster {
		var kind identifierClusterKind
		switch {
		case r == '_':
			kind = identifierClusterUnderscore
		case unicode.IsLetter(r):
			kind = identifierClusterLetter
		case unicode.IsDigit(r):
			kind = identifierClusterNumber
		case unicode.IsMark(r):
			kind = identifierClusterMark
		default:
			allIdentifierCharacters = false
		}

		if firstKind == identifierClusterNonIdentifier &&
			kind != identifierClusterMark {
			firstKind = kind
		}
		if casing == identifierNoCase && kind == identifierClusterLetter {
			switch {
			case unicode.IsUpper(r) || unicode.IsTitle(r):
				casing = identifierUpperCase
			case unicode.IsLower(r):
				casing = identifierLowerCase
			}
		}
	}

	if !allIdentifierCharacters {
		return identifierClusterNonIdentifier, identifierNoCase
	}
	if firstKind == identifierClusterNonIdentifier {
		firstKind = identifierClusterMark
	}
	return firstKind, casing
}

func appendUnderscoreToken(tokens []IdentifierToken, start, end int) []IdentifierToken {
	return append(tokens, IdentifierToken{
		Start:  start,
		End:    end,
		Class:  IdentifierTokenSeparator,
		Reason: IdentifierTokenUnderscore,
	})
}

func appendStandardWordTokens(tokens []IdentifierToken, str string, start, end int) []IdentifierToken {
	if start >= end {
		return tokens
	}

	remaining := str[start:end]
	offset := start
	state := -1
	for len(remaining) > 0 {
		wordStart := offset
		word, rest, newState := FirstWordInString(remaining, state)
		tokens = append(tokens, IdentifierToken{
			Start:  wordStart,
			End:    offset + len(word),
			Class:  IdentifierTokenOther,
			Reason: IdentifierTokenStandardWord,
		})
		offset += len(word)
		remaining = rest
		state = newState
	}
	return tokens
}
