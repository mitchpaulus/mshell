//! Abbreviations: which word the editor tries to expand, and where it is,
//! ported from `mshell/Abbr.go` (ai/abbr.typ). Expanding is
//! `Editor::expand_abbr`.
//!
//! The engine sends the abbreviations that can expand, newest last. What
//! only the engine can decide goes through the host: whether a regex
//! matches (Go's and Rust's regex syntax differ), what an abbrFunc returns,
//! and whether a word names a program (which depends on the engine's PATH).

use crate::edit::{grapheme_ceil, grapheme_floor};
use crate::lexer::{is_space, Token, TokenKind};

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum AbbrMatch {
    Word(String),
    /// The engine matches it.
    Regex(String),
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum AbbrPosition {
    Command,
    Anywhere,
    ArgOf(Vec<String>),
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum AbbrExpansion {
    Text(String),
    /// An abbrFunc: the engine's id for its quotation.
    Func(u32),
}

/// An abbreviation that can expand.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Abbr {
    /// The engine's id.
    pub id: u32,
    pub matcher: AbbrMatch,
    pub position: AbbrPosition,
    pub expansion: AbbrExpansion,
    /// The cursor marker, or "" for none.
    pub marker: String,
}

/// What an abbrFunc returned.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum AbbrAnswer {
    Text(String),
    /// None: the word is left to the next abbreviation.
    Decline,
    /// The quotation failed: the word stays, and no other is tried.
    Fail,
}

/// Go's isAbbrSeparator: ends the word an abbreviation is tried on.
pub fn is_separator(c: char) -> bool {
    matches!(c, '[' | ']' | '(' | ')' | '{' | '}' | '|' | ';' | '<' | '>') || is_space(c)
}

/// Go's abbrWordAt: the word to try, from the nearest separator before the
/// cursor to the cursor, or with `to_word_end` (Enter) to the next
/// separator. None when there is no word, or it doesn't start and end on
/// cluster boundaries.
pub fn word_at(line: &str, cursor: usize, to_word_end: bool) -> Option<(usize, usize)> {
    let mut start = cursor;
    for c in line[..cursor].chars().rev() {
        if is_separator(c) {
            break;
        }
        start -= c.len_utf8();
    }
    let mut end = cursor;
    if to_word_end {
        for c in line[cursor..].chars() {
            if is_separator(c) {
                break;
            }
            end += c.len_utf8();
        }
    }
    if start == end || grapheme_floor(line, start) != start || grapheme_ceil(line, end) != end {
        return None;
    }
    Some((start, end))
}

/// Go's abbrSite: the word and its position.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Site {
    pub start: usize,
    pub end: usize,
    /// A command word: at the start of the line, at the head of a list, or
    /// after '|' on a command line.
    pub command_position: bool,
    /// The byte range of the command the word is an argument of.
    pub command: Option<(usize, usize)>,
}

/// Go's isAbbrTextToken: a word there is data and never expands.
fn is_text_token(k: TokenKind) -> bool {
    use TokenKind::*;
    matches!(
        k,
        String
            | SingleQuoteString
            | FormatString
            | FormatStringStart
            | FormatStringMid
            | FormatStringEnd
            | UnfinishedString
            | UnfinishedFormatString
            | UnfinishedSingleQuoteString
            | Path
            | UnfinishedPath
            | LineComment
            | Rune
            | UnfinishedRune
    )
}

fn next_token(tokens: &[Token], i: usize, last: usize) -> Option<usize> {
    (i + 1..=last).find(|&j| !matches!(tokens[j].kind, TokenKind::Whitespace | TokenKind::LineComment))
}

/// Go's findAbbrSite: the position of the word start..end, from the line's
/// tokens (the editor's lexer settings; an error token, if any, last). None
/// when the word may not expand: it doesn't start a token, is in a string,
/// path or comment, or the line doesn't lex up to its end. `open` is a
/// buffer, kept to avoid allocating.
pub fn find_site(
    line: &str,
    tokens: &[Token],
    errored: bool,
    start: usize,
    end: usize,
    open: &mut Vec<usize>,
    is_program: &mut dyn FnMut(&str) -> bool,
) -> Option<Site> {
    let tokens = if errored {
        let err_at = tokens.last().map_or(0, |t| t.byte_start as usize);
        if err_at < end {
            return None;
        }
        &tokens[..tokens.len() - 1]
    } else {
        tokens
    };
    open.clear();
    let (mut first, mut last, mut last_pipe) = (None::<usize>, None::<usize>, None::<usize>);
    let mut found = false;
    for (i, t) in tokens.iter().enumerate() {
        if t.kind == TokenKind::Eof {
            break;
        }
        let (token_start, token_end) = (t.byte_start as usize, t.byte_end as usize);
        if token_start >= start {
            found = token_start == start && !is_text_token(t.kind);
            break;
        }
        if token_end > start {
            break;
        }
        use TokenKind::*;
        match t.kind {
            Whitespace | LineComment => continue,
            LeftSquareBracket | LeftParen | LeftCurly | GridOpen => open.push(i),
            RightSquareBracket | RightParen | RightCurly | GridClose => {
                open.pop();
            }
            Pipe => {
                if open.is_empty() {
                    last_pipe = Some(i);
                }
            }
            _ => {}
        }
        if first.is_none() {
            first = Some(i);
        }
        last = Some(i);
    }
    if !found {
        return None;
    }
    let mut site = Site { start, end, command_position: false, command: None };
    let (Some(first), Some(last)) = (first, last) else {
        site.command_position = true;
        return Some(site);
    };
    let command_line = tokens[first].kind == TokenKind::Literal && is_program(tokens[first].text(line));
    let prev = tokens[last].kind;
    if prev == TokenKind::LeftSquareBracket || (prev == TokenKind::Pipe && command_line) {
        site.command_position = true;
        return Some(site);
    }
    let head = if let Some(&i) = open.last() {
        if tokens[i].kind == TokenKind::LeftSquareBracket { next_token(tokens, i, last) } else { None }
    } else if command_line && prev != TokenKind::LessThan && prev != TokenKind::GreaterThan {
        // A word after a redirect is a file, not an argument.
        match last_pipe {
            Some(p) => next_token(tokens, p, last),
            None => Some(first),
        }
    } else {
        None
    };
    if let Some(h) = head {
        if tokens[h].kind == TokenKind::Literal {
            site.command = Some((tokens[h].byte_start as usize, tokens[h].byte_end as usize));
        }
    }
    Some(site)
}

impl Abbr {
    /// Go's appliesAt.
    pub fn applies_at(&self, site: &Site, line: &str) -> bool {
        match &self.position {
            AbbrPosition::Command => site.command_position,
            AbbrPosition::Anywhere => true,
            AbbrPosition::ArgOf(commands) => {
                site.command.is_some_and(|(s, e)| commands.iter().any(|c| c == &line[s..e]))
            }
        }
    }
}
