//! The line being edited and the edit operations that need nothing but the
//! line, ported from `mshell/CommandEditing.go`, `Bindings.go` and
//! `Main.go` (replaceText). Positions are byte offsets, and moves go by
//! grapheme cluster, as in the Go editor.

use crate::graphemes::clusters;

/// An `EditOp`. The first group, up to `ExpandAbbr`, is pure: it changes
/// only the line and the cursor. The rest are effects, which the frontend or
/// the engine carries out.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum EditOp {
    Insert(String),
    SelfInsert,
    SetLine(String, String),
    Left,
    Right,
    WordLeft,
    WordRight,
    LineStart,
    LineEnd,
    Backspace,
    Delete,
    DeleteWordLeft,
    DeleteToStart,
    DeleteToEnd,
    ExpandAbbr,
    Accept,
    Exit,
    /// `editRun` with the engine's id of its quotation.
    Run(u32),
    /// `editRunOps` with the engine's id of its quotation.
    RunOps(u32),
    Forward,
    AcceptSuggestion,
    HistoryPrev,
    HistoryNext,
    HistorySearchBack,
    HistorySearchForward,
    LastArg,
    Complete,
    CompleteBack,
    CompleteNext,
    CompletePrev,
    CompleteLeft,
    CompleteRight,
    CompleteAccept,
    ClearScreen,
    Remeasure,
    Message(String),
}

impl EditOp {
    /// Changes only the line and the cursor, so restoring them undoes it.
    pub fn is_pure(&self) -> bool {
        use EditOp::*;
        matches!(
            self,
            Insert(_)
                | SelfInsert
                | SetLine(..)
                | Left
                | Right
                | WordLeft
                | WordRight
                | LineStart
                | LineEnd
                | Backspace
                | Delete
                | DeleteWordLeft
                | DeleteToStart
                | DeleteToEnd
                | ExpandAbbr
        )
    }

    /// Starts something the program can't follow: a new line, or no shell.
    pub fn ends(&self) -> bool {
        matches!(self, EditOp::Accept | EditOp::Exit)
    }

    /// The op for an `EditOp` member name with its str payloads, as the
    /// engine names them; None for an unknown member or the wrong payload.
    pub fn from_member(member: &str, a: Option<&str>, b: Option<&str>) -> Option<EditOp> {
        use EditOp::*;
        Some(match (member, a, b) {
            ("editInsert", Some(a), None) => Insert(a.to_string()),
            ("editSelfInsert", None, None) => SelfInsert,
            ("editSetLine", Some(a), Some(b)) => SetLine(a.to_string(), b.to_string()),
            ("editLeft", None, None) => Left,
            ("editRight", None, None) => Right,
            ("editWordLeft", None, None) => WordLeft,
            ("editWordRight", None, None) => WordRight,
            ("editLineStart", None, None) => LineStart,
            ("editLineEnd", None, None) => LineEnd,
            ("editBackspace", None, None) => Backspace,
            ("editDelete", None, None) => Delete,
            ("editDeleteWordLeft", None, None) => DeleteWordLeft,
            ("editDeleteToStart", None, None) => DeleteToStart,
            ("editDeleteToEnd", None, None) => DeleteToEnd,
            ("editExpandAbbr", None, None) => ExpandAbbr,
            ("editAccept", None, None) => Accept,
            ("editExit", None, None) => Exit,
            ("editForward", None, None) => Forward,
            ("editAcceptSuggestion", None, None) => AcceptSuggestion,
            ("editHistoryPrev", None, None) => HistoryPrev,
            ("editHistoryNext", None, None) => HistoryNext,
            ("editHistorySearchBack", None, None) => HistorySearchBack,
            ("editHistorySearchForward", None, None) => HistorySearchForward,
            ("editLastArg", None, None) => LastArg,
            ("editComplete", None, None) => Complete,
            ("editCompleteBack", None, None) => CompleteBack,
            ("editCompleteNext", None, None) => CompleteNext,
            ("editCompletePrev", None, None) => CompletePrev,
            ("editCompleteLeft", None, None) => CompleteLeft,
            ("editCompleteRight", None, None) => CompleteRight,
            ("editCompleteAccept", None, None) => CompleteAccept,
            ("editClearScreen", None, None) => ClearScreen,
            ("editRemeasure", None, None) => Remeasure,
            ("editMessage", Some(a), None) => Message(a.to_string()),
            _ => return None,
        })
    }
}

/// Go's isAllPrintableAscii: then every byte is its own cluster.
fn all_printable_ascii(s: &str) -> bool {
    s.bytes().all(|b| (0x20..=0x7e).contains(&b))
}

/// Go's graphemeGap: the cluster boundaries around `cursor`, both `cursor`
/// when it is on one. The whole line is segmented, since what comes before
/// can move boundaries (regional indicators pair up).
pub fn grapheme_gap(s: &str, cursor: usize) -> (usize, usize) {
    let cursor = cursor.min(s.len());
    if cursor == 0 || cursor == s.len() || all_printable_ascii(s) {
        return (cursor, cursor);
    }
    for (start, end) in clusters(s) {
        if cursor == end {
            return (end, end);
        }
        if cursor < end {
            return (start, end);
        }
    }
    (cursor, cursor)
}

pub fn grapheme_floor(s: &str, cursor: usize) -> usize {
    grapheme_gap(s, cursor).0
}

pub fn grapheme_ceil(s: &str, cursor: usize) -> usize {
    grapheme_gap(s, cursor).1
}

pub fn grapheme_previous(s: &str, cursor: usize) -> usize {
    grapheme_floor(s, cursor.min(s.len()).saturating_sub(1))
}

pub fn grapheme_next(s: &str, cursor: usize) -> usize {
    grapheme_ceil(s, s.len().min(cursor + 1))
}

/// A word ends at a cluster whose first byte is an ASCII space.
fn is_delim(s: &str, start: usize) -> bool {
    s.as_bytes()[start] == b' '
}

pub fn word_left(s: &str, cursor: usize) -> usize {
    let cursor = grapheme_ceil(s, cursor);
    let mut word_start = 0;
    let mut previous_space = true;
    for (start, _) in clusters(s) {
        if start >= cursor {
            break;
        }
        let space = is_delim(s, start);
        if !space && previous_space {
            word_start = start;
        }
        previous_space = space;
    }
    word_start
}

pub fn word_right(s: &str, cursor: usize) -> usize {
    let cursor = grapheme_floor(s, cursor);
    let mut in_word = false;
    for (start, _) in clusters(s) {
        if start >= cursor {
            let space = is_delim(s, start);
            if space && in_word {
                return start;
            }
            in_word = in_word || !space;
        }
    }
    s.len()
}

/// Go's lineMoveOrDelete: what a move or delete does, as the bytes to delete
/// (start to end) with the cursor left at start; a move gives start == end.
/// None for any other op.
pub fn line_move_or_delete(op: &EditOp, s: &str, cursor: usize) -> Option<(usize, usize)> {
    use EditOp::*;
    let start = match op {
        Left => grapheme_previous(s, cursor),
        Right => grapheme_next(s, cursor),
        WordLeft => word_left(s, cursor),
        WordRight => word_right(s, cursor),
        LineStart => 0,
        LineEnd => s.len(),
        Backspace => return Some((grapheme_previous(s, cursor), cursor)),
        Delete => return Some((grapheme_floor(s, cursor), grapheme_next(s, cursor))),
        DeleteWordLeft => return Some((word_left(s, cursor), cursor)),
        DeleteToStart => return Some((0, cursor)),
        DeleteToEnd => return Some((cursor, s.len())),
        _ => return None,
    };
    Some((start, start))
}

/// The line and the cursor, a byte offset on a cluster boundary.
#[derive(Debug, Default, PartialEq, Eq)]
pub struct Line {
    pub text: String,
    pub cursor: usize,
}

// By hand, because a derived clone_from allocates a new String; the
// resolver's snapshots reuse theirs on every key.
impl Clone for Line {
    fn clone(&self) -> Line {
        Line { text: self.text.clone(), cursor: self.cursor }
    }
    fn clone_from(&mut self, source: &Line) {
        self.text.clone_from(&source.text);
        self.cursor = source.cursor;
    }
}

impl Line {
    /// Go's replaceText: replaces start to end with `new`, widened to whole
    /// clusters, and leaves the cursor after it, moved forward if the text
    /// joins into a cluster with what follows.
    pub fn replace(&mut self, new: &str, start: usize, end: usize) {
        let len = self.text.len();
        let end = end.min(len);
        let mut start = start.min(len).min(end);
        let end = if start == end {
            start = grapheme_ceil(&self.text, start);
            start
        } else {
            let e = grapheme_ceil(&self.text, end);
            start = grapheme_floor(&self.text, start);
            e
        };
        self.text.replace_range(start..end, new);
        self.cursor = grapheme_ceil(&self.text, start + new.len());
    }

    pub fn insert(&mut self, text: &str) {
        self.replace(text, self.cursor, self.cursor);
    }

    pub fn set(&mut self, before: &str, after: &str) {
        self.text.clear();
        self.text.push_str(before);
        self.text.push_str(after);
        self.cursor = grapheme_ceil(&self.text, before.len());
    }

    /// Runs a move or delete; false for any other op.
    pub fn move_or_delete(&mut self, op: &EditOp) -> bool {
        let Some((start, end)) = line_move_or_delete(op, &self.text, self.cursor) else { return false };
        if start < end {
            self.replace("", start, end);
        } else {
            self.cursor = start;
        }
        true
    }
}
