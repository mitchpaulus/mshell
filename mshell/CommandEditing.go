package main

import (
	"strings"

	"github.com/rivo/uniseg"
)

func (state *TermState) commandEnd() ByteOffset {
	return ByteOffset(len(state.currentCommand))
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

// isSpaceByte ends a word in the shell's command line: an ASCII space. A
// space with combining marks is one whole delimiter grapheme.
func isSpaceByte(b byte) bool { return b == ' ' }

// wordLeft and wordRight move by words that end at a grapheme whose first
// byte delim accepts.
func wordLeft(source SourceText, cursor ByteOffset, delim func(byte) bool) ByteOffset {
	cursor = graphemeCeil(source, cursor)
	rest := string(source)
	segmentState := -1
	offset := ByteOffset(0)
	wordStart := ByteOffset(0)
	previousSpace := true
	for len(rest) > 0 && offset < cursor {
		var cluster string
		cluster, rest, _, segmentState = uniseg.FirstGraphemeClusterInString(rest, segmentState)
		space := delim(cluster[0])
		if !space && previousSpace {
			wordStart = offset
		}
		previousSpace = space
		offset += ByteOffset(len(cluster))
	}
	return wordStart
}

func wordRight(source SourceText, cursor ByteOffset, delim func(byte) bool) ByteOffset {
	cursor = graphemeFloor(source, cursor)
	rest := string(source)
	segmentState := -1
	offset := ByteOffset(0)
	inWord := false
	for len(rest) > 0 {
		var cluster string
		cluster, rest, _, segmentState = uniseg.FirstGraphemeClusterInString(rest, segmentState)
		if offset >= cursor {
			space := delim(cluster[0])
			if space && inWord {
				return offset
			}
			inWord = inWord || !space
		}
		offset += ByteOffset(len(cluster))
	}
	return offset
}

// lineMoveOrDelete is what a line edit that moves the cursor or deletes
// does to a line, in the shell and in the file manager's prompt line: it
// deletes the bytes start to end and leaves the cursor at start, and a move
// gives start == end. delim says which bytes end a word. ok is false for
// any other op.
func lineMoveOrDelete(kind editOpKind, line SourceText, cursor ByteOffset, delim func(byte) bool) (start, end ByteOffset, ok bool) {
	lineEnd := ByteOffset(len(line))
	switch kind {
	case opLeft:
		start = graphemePrevious(line, cursor)
	case opRight:
		start = graphemeNext(line, cursor)
	case opWordLeft:
		start = wordLeft(line, cursor, delim)
	case opWordRight:
		start = wordRight(line, cursor, delim)
	case opLineStart:
		start = 0
	case opLineEnd:
		start = lineEnd
	case opBackspace:
		return graphemePrevious(line, cursor), cursor, true
	case opDelete:
		return graphemeFloor(line, cursor), graphemeNext(line, cursor), true
	case opDeleteWordLeft:
		return wordLeft(line, cursor, delim), cursor, true
	case opDeleteToStart:
		return 0, cursor, true
	case opDeleteToEnd:
		return cursor, lineEnd, true
	default:
		return 0, 0, false
	}
	return start, start, true
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
	} else if token.Type == UNFINISHEDFORMATSTRING {
		// Completion text goes inside the string, after its opener: the
		// '$"' that starts it, or the '}' that ends an interpolation.
		if strings.HasPrefix(token.Lexeme, "$\"") {
			prefixStart += 2
		} else {
			prefixStart++
		}
		return string(source[prefixStart:cursor]), prefixStart
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
