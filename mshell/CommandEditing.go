package main

import (
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

func (state *TermState) commandEnd() ByteOffset {
	return ByteOffset(len(state.currentCommand))
}

// The old painter still positions by rune count. Keep that conversion here
// until it is replaced by cell positions from resolved layout.
func (state *TermState) legacyCursorColumn() int {
	return utf8.RuneCountInString(string(state.currentCommand[:state.index]))
}

// The lexer uses rune offsets; editing and layout use source byte offsets.
func sourceByteOffset(source SourceText, runeOffset int) ByteOffset {
	for offset := range string(source) {
		if runeOffset <= 0 {
			return ByteOffset(offset)
		}
		runeOffset--
	}
	return ByteOffset(len(source))
}

// Segment the complete source, since prefixes can change grapheme boundaries
// (for example the pairing of regional indicators). Invalid bytes stay intact.
func graphemeGap(source SourceText, cursor ByteOffset) (ByteOffset, ByteOffset) {
	cursor = max(0, min(cursor, ByteOffset(len(source))))
	if cursor == 0 || cursor == ByteOffset(len(source)) || isAllPrintableAscii(source) {
		return cursor, cursor
	}
	rest := string(source)
	segmentState := -1
	start := ByteOffset(0)
	for len(rest) > 0 {
		var cluster string
		cluster, rest, _, segmentState = uniseg.FirstGraphemeClusterInString(rest, segmentState)
		end := start + ByteOffset(len(cluster))
		if cursor == end {
			return end, end
		}
		if cursor < end {
			return start, end
		}
		start = end
	}
	return cursor, cursor
}

func graphemeFloor(source SourceText, cursor ByteOffset) ByteOffset {
	floor, _ := graphemeGap(source, cursor)
	return floor
}

func graphemeCeil(source SourceText, cursor ByteOffset) ByteOffset {
	_, ceil := graphemeGap(source, cursor)
	return ceil
}

func graphemePrevious(source SourceText, cursor ByteOffset) ByteOffset {
	return graphemeFloor(source, max(0, min(cursor, ByteOffset(len(source)))-1))
}

func graphemeNext(source SourceText, cursor ByteOffset) ByteOffset {
	return graphemeCeil(source, min(ByteOffset(len(source)), max(0, cursor)+1))
}

// Word commands retain the existing ASCII-space delimiter convention.
// A space with combining marks is one whole delimiter grapheme.
func wordLeft(source SourceText, cursor ByteOffset) ByteOffset {
	cursor = graphemeCeil(source, cursor)
	rest := string(source)
	segmentState := -1
	offset := ByteOffset(0)
	wordStart := ByteOffset(0)
	previousSpace := true
	for len(rest) > 0 && offset < cursor {
		var cluster string
		cluster, rest, _, segmentState = uniseg.FirstGraphemeClusterInString(rest, segmentState)
		space := cluster[0] == ' '
		if !space && previousSpace {
			wordStart = offset
		}
		previousSpace = space
		offset += ByteOffset(len(cluster))
	}
	return wordStart
}

func wordRight(source SourceText, cursor ByteOffset) ByteOffset {
	cursor = graphemeFloor(source, cursor)
	rest := string(source)
	segmentState := -1
	offset := ByteOffset(0)
	inWord := false
	for len(rest) > 0 {
		var cluster string
		cluster, rest, _, segmentState = uniseg.FirstGraphemeClusterInString(rest, segmentState)
		if offset >= cursor {
			space := cluster[0] == ' '
			if space && inWord {
				return offset
			}
			inWord = inWord || !space
		}
		offset += ByteOffset(len(cluster))
	}
	return offset
}

func (state *TermState) deletePreviousWord() {
	if state.index > 0 {
		state.replaceText("", wordLeft(state.currentCommand, state.index), state.index)
	}
}

// Token.Start is a whole-input rune offset, whereas Token.Column restarts on
// each line. Convert only at this lexer boundary, including for multiline input.
func completionTokenPrefix(source SourceText, cursor ByteOffset, token Token) (string, ByteOffset) {
	start := sourceByteOffset(source, token.Start)
	if cursor > start + ByteOffset(len(token.Lexeme)) || token.Type == EOF {
		return "", cursor
	}
	prefixStart := start
	if token.Type == UNFINISHEDSTRING || token.Type == UNFINISHEDSINGLEQUOTESTRING || token.Type == UNFINISHEDPATH {
		prefixStart++
	}
	return string(source[prefixStart:cursor]), start
}

// A shared byte prefix can end inside a UTF-8 codepoint or grapheme. Only insert
// the prefix shared at a complete grapheme boundary in every candidate.
func completionGraphemePrefix(matches []string) string {
	if len(matches) == 0 {
		return ""
	}
	prefix := matches[0]
	for _, match := range matches[1:] {
		length := min(len(prefix), len(match))
		i := 0
		for i < length && prefix[i] == match[i] {
			i++
		}
		prefix = prefix[:i]
	}
	end := ByteOffset(len(prefix))
	for {
		previous := end
		for _, match := range matches {
			end = graphemeFloor(SourceText(match), end)
		}
		if end == previous {
			return prefix[:end]
		}
	}
}
