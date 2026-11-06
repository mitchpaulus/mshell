package main

import (
	"unicode"
	"unicode/utf8"
)

// matchRank is how well a candidate fits the typed text. Lower is better.
// Tab offers only the candidates that share the best rank found.
type matchRank uint8

const (
	// The candidate starts with the typed text. Typing in lowercase ignores
	// case, so "mak" puts "Makefile" here too.
	rankPrefix matchRank = iota
	// The candidate starts with the typed text only when case is ignored,
	// and the typed text has an uppercase letter.
	rankPrefixIgnoreCase
	// The typed text appears after the start of the candidate.
	rankSubstring
	// The typed text appears after the start of the candidate only when
	// case is ignored, and the typed text has an uppercase letter.
	rankSubstringIgnoreCase
	// No match.
	rankNone
)

// typedText is the text Tab completes, checked once so that matching each
// candidate does not repeat the work.
type typedText struct {
	text string
	// hasUpper is set when the text has an uppercase letter. Only then does
	// a case difference make a match rank lower.
	hasUpper bool
	// ascii is set when every byte of the text is ASCII. A match then
	// covers at least len(text) bytes of the candidate.
	ascii bool
	// prefixOnly is set for options: text starting with '-' only matches
	// the start of a candidate.
	prefixOnly bool
	// first is the lowercase first byte of an ASCII text.
	first byte
}

func newTypedText(text string) typedText {
	t := typedText{text: text, ascii: true}
	for i := 0; i < len(text); i++ {
		if text[i] >= utf8.RuneSelf {
			t.ascii = false
			break
		}
	}
	for _, r := range text {
		if unicode.IsUpper(r) {
			t.hasUpper = true
			break
		}
	}
	if len(text) > 0 {
		t.prefixOnly = text[0] == '-'
		t.first = asciiLower[text[0]]
	}
	return t
}

// rank reports how well candidate fits the text. It returns rankNone when
// candidate does not fit or fits worse than cutoff, and skips the substring
// search when cutoff is already a prefix rank.
func (t *typedText) rank(candidate string, cutoff matchRank) matchRank {
	if ok, exact := foldedPrefix(candidate, t.text); ok {
		r := rankPrefix
		if !exact && t.hasUpper {
			r = rankPrefixIgnoreCase
		}
		if r > cutoff {
			return rankNone
		}
		return r
	}
	if t.prefixOnly || cutoff < rankSubstring {
		return rankNone
	}
	r := t.substringRank(candidate)
	if r > cutoff {
		return rankNone
	}
	return r
}

// substringRank looks for the text after the start of candidate. The start
// was already checked by rank.
func (t *typedText) substringRank(candidate string) matchRank {
	last := len(candidate) - 1
	if t.ascii {
		last = len(candidate) - len(t.text)
	}
	foundIgnoringCase := false
	for i := 1; i <= last; i++ {
		c := candidate[i]
		if c&0xC0 == 0x80 {
			// The middle of a multi-byte character.
			continue
		}
		if t.ascii && c < utf8.RuneSelf && asciiLower[c] != t.first {
			continue
		}
		ok, exact := foldedPrefix(candidate[i:], t.text)
		if !ok {
			continue
		}
		if exact || !t.hasUpper {
			return rankSubstring
		}
		// Keep looking for a place where the case matches too.
		foundIgnoringCase = true
	}
	if foundIgnoringCase {
		return rankSubstringIgnoreCase
	}
	return rankNone
}

// foldedPrefix reports whether s starts with prefix when case is ignored,
// and whether s also starts with prefix exactly. ASCII compares byte by
// byte; other characters use Unicode simple case folding.
func foldedPrefix(s, prefix string) (match bool, exact bool) {
	exact = true
	i, j := 0, 0
	for j < len(prefix) {
		if i >= len(s) {
			return false, false
		}
		a, b := s[i], prefix[j]
		if a < utf8.RuneSelf && b < utf8.RuneSelf {
			if a != b {
				if asciiLower[a] != asciiLower[b] {
					return false, false
				}
				exact = false
			}
			i++
			j++
			continue
		}
		ra, na := utf8.DecodeRuneInString(s[i:])
		rb, nb := utf8.DecodeRuneInString(prefix[j:])
		if ra == utf8.RuneError || rb == utf8.RuneError {
			// Invalid UTF-8 only matches the same byte.
			if a != b {
				return false, false
			}
			i++
			j++
			continue
		}
		if ra != rb {
			if !equalFoldRune(ra, rb) {
				return false, false
			}
			exact = false
		}
		i += na
		j += nb
	}
	return true, exact
}

// equalFoldRune reports whether two runes are equal under Unicode simple
// case folding, the same test strings.EqualFold makes.
func equalFoldRune(a, b rune) bool {
	if a == b {
		return true
	}
	if b < a {
		a, b = b, a
	}
	// SimpleFold returns the next rune in a's fold set, wrapping around to
	// the smallest.
	r := unicode.SimpleFold(a)
	for r != a && r < b {
		r = unicode.SimpleFold(r)
	}
	return r == b
}

// asciiLower maps each ASCII byte to its lowercase form. Bytes at or above
// 0x80 map to themselves.
var asciiLower = func() (table [256]byte) {
	for i := range table {
		c := byte(i)
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		table[i] = c
	}
	return table
}()

// completionCollector gathers the completions that share the best rank
// found so far. A better rank drops what was gathered before it.
type completionCollector struct {
	best    matchRank
	matches []TabMatch
}

func newCompletionCollector() completionCollector {
	return completionCollector{best: rankNone}
}

// useRank drops the gathered matches when r beats them.
func (c *completionCollector) useRank(r matchRank) {
	if r < c.best {
		c.matches = c.matches[:0]
		c.best = r
	}
}

// add gathers a match of rank r, unless r is worse than the best so far.
func (c *completionCollector) add(kind TabMatchType, text string, r matchRank) {
	if r == rankNone || r > c.best {
		return
	}
	c.useRank(r)
	c.matches = append(c.matches, TabMatch{kind, text})
}
