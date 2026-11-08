//! Grapheme clusters, ported from github.com/rivo/uniseg (the version mshell
//! builds with), so the editor moves the cursor over exactly the clusters
//! msh's editor does. Its tables are generated from uniseg's own
//! (tools/gengraphemes.py); this is its state machine (grapheme.go,
//! graphemerules.go), without the width calculation.
//!
//! The constants are uniseg's numbers. uniseg's prXX is 0 and prAny is 1: a
//! code point missing from the table gets 0, which behaves as prAny through
//! the fallback rules.

use crate::grapheme_tables::GRAPHEME_PROPERTIES;

const PR_ANY: i32 = 1;
const PR_PREPEND: i32 = 2;
const PR_CR: i32 = 3;
const PR_LF: i32 = 4;
const PR_CONTROL: i32 = 5;
const PR_EXTEND: i32 = 6;
const PR_REGIONAL_INDICATOR: i32 = 7;
const PR_SPACING_MARK: i32 = 8;
const PR_L: i32 = 9;
const PR_V: i32 = 10;
const PR_T: i32 = 11;
const PR_LV: i32 = 12;
const PR_LVT: i32 = 13;
const PR_ZWJ: i32 = 14;
const PR_EXTENDED_PICTOGRAPHIC: i32 = 15;

const GR_ANY: i32 = 0;
const GR_CR: i32 = 1;
const GR_CONTROL_LF: i32 = 2;
const GR_L: i32 = 3;
const GR_LVV: i32 = 4;
const GR_LVTT: i32 = 5;
const GR_PREPEND: i32 = 6;
const GR_EXTENDED_PICTOGRAPHIC: i32 = 7;
const GR_EXTENDED_PICTOGRAPHIC_ZWJ: i32 = 8;
const GR_RI_ODD: i32 = 9;
const GR_RI_EVEN: i32 = 10;

const SHIFT_PROP_STATE: i32 = 4;
const MASK_STATE: i32 = 0xf;

/// uniseg's propertyGraphemes.
fn property(r: char) -> i32 {
    let c = r as u32;
    if (0x20..=0x7e).contains(&c) {
        return PR_ANY;
    }
    if c == 0x0a {
        return PR_LF;
    }
    if c == 0x0d {
        return PR_CR;
    }
    if c <= 0x1f || c == 0x7f {
        return PR_CONTROL;
    }
    // A binary search, as uniseg's propertySearch; 0 when not found.
    let (mut from, mut to) = (0, GRAPHEME_PROPERTIES.len());
    while to > from {
        let middle = (from + to) / 2;
        let (lo, hi, p) = GRAPHEME_PROPERTIES[middle];
        if c < lo {
            to = middle;
        } else if c > hi {
            from = middle + 1;
        } else {
            return p as i32;
        }
    }
    0
}

/// uniseg's grTransitions: the new state, whether there is a boundary
/// before the code point, and the rule's number; None if no rule matches.
fn transitions(state: i32, prop: i32) -> Option<(i32, bool, i32)> {
    Some(match (state, prop) {
        // GB5
        (GR_ANY, PR_CR) => (GR_CR, true, 50),
        (GR_ANY, PR_LF) => (GR_CONTROL_LF, true, 50),
        (GR_ANY, PR_CONTROL) => (GR_CONTROL_LF, true, 50),
        // GB4
        (GR_CR, PR_ANY) => (GR_ANY, true, 40),
        (GR_CONTROL_LF, PR_ANY) => (GR_ANY, true, 40),
        // GB3
        (GR_CR, PR_LF) => (GR_CONTROL_LF, false, 30),
        // GB6
        (GR_ANY, PR_L) => (GR_L, true, 9990),
        (GR_L, PR_L) => (GR_L, false, 60),
        (GR_L, PR_V) => (GR_LVV, false, 60),
        (GR_L, PR_LV) => (GR_LVV, false, 60),
        (GR_L, PR_LVT) => (GR_LVTT, false, 60),
        // GB7
        (GR_ANY, PR_LV) => (GR_LVV, true, 9990),
        (GR_ANY, PR_V) => (GR_LVV, true, 9990),
        (GR_LVV, PR_V) => (GR_LVV, false, 70),
        (GR_LVV, PR_T) => (GR_LVTT, false, 70),
        // GB8
        (GR_ANY, PR_LVT) => (GR_LVTT, true, 9990),
        (GR_ANY, PR_T) => (GR_LVTT, true, 9990),
        (GR_LVTT, PR_T) => (GR_LVTT, false, 80),
        // GB9
        (GR_ANY, PR_EXTEND) => (GR_ANY, false, 90),
        (GR_ANY, PR_ZWJ) => (GR_ANY, false, 90),
        // GB9a
        (GR_ANY, PR_SPACING_MARK) => (GR_ANY, false, 91),
        // GB9b
        (GR_ANY, PR_PREPEND) => (GR_PREPEND, true, 9990),
        (GR_PREPEND, PR_ANY) => (GR_ANY, false, 92),
        // GB11
        (GR_ANY, PR_EXTENDED_PICTOGRAPHIC) => (GR_EXTENDED_PICTOGRAPHIC, true, 9990),
        (GR_EXTENDED_PICTOGRAPHIC, PR_EXTEND) => (GR_EXTENDED_PICTOGRAPHIC, false, 110),
        (GR_EXTENDED_PICTOGRAPHIC, PR_ZWJ) => (GR_EXTENDED_PICTOGRAPHIC_ZWJ, false, 110),
        (GR_EXTENDED_PICTOGRAPHIC_ZWJ, PR_EXTENDED_PICTOGRAPHIC) => (GR_EXTENDED_PICTOGRAPHIC, false, 110),
        // GB12 / GB13
        (GR_ANY, PR_REGIONAL_INDICATOR) => (GR_RI_ODD, true, 9990),
        (GR_RI_ODD, PR_REGIONAL_INDICATOR) => (GR_RI_EVEN, false, 120),
        (GR_RI_EVEN, PR_REGIONAL_INDICATOR) => (GR_RI_ODD, true, 120),
        _ => return None,
    })
}

/// uniseg's transitionGraphemeState: the new state, the code point's
/// property, and whether there is a boundary before it.
fn transition(state: i32, r: char) -> (i32, i32, bool) {
    let prop = property(r);
    if let Some((next, boundary, _)) = transitions(state, prop) {
        return (next, prop, boundary);
    }
    let any_prop = transitions(state, PR_ANY);
    let any_state = transitions(GR_ANY, prop);
    match (any_prop, any_state) {
        (Some((_, prop_boundary, prop_rule)), Some((state_state, state_boundary, state_rule))) => {
            let boundary = if prop_rule < state_rule { prop_boundary } else { state_boundary };
            (state_state, prop, boundary)
        }
        (Some((s, b, _)), None) => (s, prop, b),
        (None, Some((s, b, _))) => (s, prop, b),
        // GB999: Any ÷ Any.
        (None, None) => (GR_ANY, prop, true),
    }
}

/// The length in bytes of the first grapheme cluster of `s`, and the state
/// to pass for the rest: uniseg's FirstGraphemeClusterInString. Start with
/// state -1. Returns 0 for an empty string.
pub fn first_cluster(s: &str, state: i32) -> (usize, i32) {
    let mut chars = s.char_indices();
    let Some((_, r)) = chars.next() else { return (0, state) };
    let mut length = r.len_utf8();
    if s.len() <= length {
        let prop = if state < 0 { property(r) } else { state >> SHIFT_PROP_STATE };
        return (s.len(), GR_ANY | (prop << SHIFT_PROP_STATE));
    }
    let mut state = if state < 0 { transition(state, r).0 } else { state };
    for (_, r) in chars {
        let (next, prop, boundary) = transition(state & MASK_STATE, r);
        state = next;
        if boundary {
            return (length, state | (prop << SHIFT_PROP_STATE));
        }
        length += r.len_utf8();
        if s.len() <= length {
            return (s.len(), GR_ANY | (prop << SHIFT_PROP_STATE));
        }
    }
    unreachable!("the loop returns at the end of the string")
}

/// Iterates over the grapheme clusters of a string as byte ranges.
pub struct Clusters<'a> {
    rest: &'a str,
    offset: usize,
    state: i32,
}

pub fn clusters(s: &str) -> Clusters<'_> {
    Clusters { rest: s, offset: 0, state: -1 }
}

impl Iterator for Clusters<'_> {
    type Item = (usize, usize);
    fn next(&mut self) -> Option<(usize, usize)> {
        if self.rest.is_empty() {
            return None;
        }
        let (n, state) = first_cluster(self.rest, self.state);
        self.state = state;
        let start = self.offset;
        self.offset += n;
        self.rest = &self.rest[n..];
        Some((start, self.offset))
    }
}
