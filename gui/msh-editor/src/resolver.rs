//! The key resolver and the editor's use of it, ported from
//! `mshell/KeyResolver.go` and `Bindings.go` (ai/cli_bindings.typ section 6,
//! ai/key-bindings/KeyResolution.tla). The Go code is the reference:
//! `msh --resolver-cases` records cases, and `examples/resolvercheck.rs`
//! replays them here (tools/check-resolver.sh).
//!
//! The keymap is data from the engine: only the bindings in force (the
//! engine works out conflicts), each with the engine's id. A `bindingFunc`
//! can only run in the engine, so the editor asks through a callback, which
//! answers with the binding's ops or None when it declines. Effects other
//! than `editMessage` go to another callback, in order with the pure ops.
//!
//! No allocation on the keystroke path once warm: pending keys and their
//! snapshots live in fixed arrays, and snapshots reuse their strings.

use crate::edit::{EditOp, Line};
use crate::keys::KeyCode;

/// The most keys a binding has, and so the most ever pending.
pub const MAX_BINDING_KEYS: usize = 8;

/// The editor's modes (Go's EditMode): the bindings of insert mode, or of
/// complete mode while Tab cycles through completions.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
#[repr(u8)]
pub enum Mode {
    #[default]
    Insert = 0,
    Complete = 1,
}

const MODE_COUNT: usize = 2;

#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Action {
    Ops(Vec<EditOp>),
    /// A bindingFunc: the engine's id for its quotation.
    Func(u32),
}

/// A binding in force.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Binding {
    /// The engine's id, given back when the binding fires.
    pub id: u32,
    pub keys: Vec<KeyCode>,
    pub mode: Mode,
    /// Most nanoseconds between keys, or 0 for no limit.
    pub within: i64,
    /// Least nanoseconds of quiet before the first key, or 0.
    pub idle_before: i64,
    pub action: Action,
}

impl Binding {
    /// Changes only the line, so restoring it undoes the binding. A
    /// bindingFunc's result isn't known until it runs, so it never is.
    pub fn is_pure(&self) -> bool {
        match &self.action {
            Action::Func(_) => false,
            Action::Ops(ops) => ops.iter().all(EditOp::is_pure),
        }
    }
}

struct TrieNode {
    next: Vec<(KeyCode, u32)>,
    entry: Option<u32>,
    // For a node with bindings below it, what their timing allows: the
    // longest within (or none at all), and the shortest idleBefore.
    any_within: bool,
    max_within: i64,
    min_idle_before: i64, // -1: none yet
}

impl TrieNode {
    fn new() -> TrieNode {
        TrieNode { next: Vec::new(), entry: None, any_within: false, max_within: 0, min_idle_before: -1 }
    }

    fn child(&self, k: KeyCode) -> Option<u32> {
        self.next.iter().find(|(key, _)| *key == k).map(|&(_, n)| n)
    }
}

#[derive(Default)]
struct Trie {
    nodes: Vec<TrieNode>,
}

impl Trie {
    fn build(&mut self, bindings: &[Binding], mode: Mode) {
        self.nodes.clear();
        self.nodes.push(TrieNode::new());
        for (i, b) in bindings.iter().enumerate() {
            if b.mode != mode {
                continue;
            }
            let mut n = 0u32;
            for &k in &b.keys {
                let node = &mut self.nodes[n as usize];
                if b.within == 0 {
                    node.any_within = true;
                }
                node.max_within = node.max_within.max(b.within);
                if node.min_idle_before < 0 || b.idle_before < node.min_idle_before {
                    node.min_idle_before = b.idle_before;
                }
                n = match node.child(k) {
                    Some(next) => next,
                    None => {
                        let next = self.nodes.len() as u32;
                        self.nodes[n as usize].next.push((k, next));
                        self.nodes.push(TrieNode::new());
                        next
                    }
                };
            }
            self.nodes[n as usize].entry = Some(i as u32);
        }
    }

    /// The node `keys` lead to from the root.
    fn walk(&self, keys: &[Pending]) -> Option<u32> {
        if self.nodes.is_empty() {
            return None;
        }
        let mut n = 0;
        for k in keys {
            n = self.nodes[n as usize].child(k.code)?;
        }
        Some(n)
    }

    /// Whether some binding has the keys of w as a proper prefix, with its
    /// times so far.
    fn grows(&self, w: &[Pending]) -> bool {
        let Some(n) = self.walk(w) else { return false };
        let node = &self.nodes[n as usize];
        if node.next.is_empty() {
            return false;
        }
        if !node.any_within && w[1..].iter().any(|k| k.gap > node.max_within) {
            return false;
        }
        node.min_idle_before <= 0 || w[0].first || w[0].gap >= node.min_idle_before
    }
}

/// The bindings in force and each mode's trie of them.
pub struct Keymap {
    bindings: Vec<Binding>,
    tries: [Trie; MODE_COUNT],
}

impl Keymap {
    /// The engine sends the bindings in force, with no two of one mode
    /// having the same keys.
    pub fn new(bindings: Vec<Binding>) -> Keymap {
        let mut tries: [Trie; MODE_COUNT] = Default::default();
        tries[Mode::Insert as usize].build(&bindings, Mode::Insert);
        tries[Mode::Complete as usize].build(&bindings, Mode::Complete);
        Keymap { bindings, tries }
    }

    pub fn bindings(&self) -> &[Binding] {
        &self.bindings
    }
}

/// Whether the keys of w satisfy b's within and idleBefore.
fn timed(b: &Binding, w: &[Pending]) -> bool {
    if b.within > 0 && w[1..].iter().any(|k| k.gap > b.within) {
        return false;
    }
    b.idle_before == 0 || w[0].first || w[0].gap >= b.idle_before
}

/// A key that may still become part of a binding.
#[derive(Clone, Copy, Default, Debug)]
struct Pending {
    code: KeyCode,
    /// The key's number since the resolver started; its snapshot is at
    /// num % SNAPSHOTS.
    num: usize,
    /// Nanoseconds since the event before it.
    gap: i64,
    /// The first key the resolver read, with no gap before it.
    first: bool,
}

/// A pure binding in force among the pending keys: keys start to end.
#[derive(Clone, Copy, Default)]
struct Span {
    start: usize,
    end: usize,
}

/// Snapshots are indexed by key number: the window holds at most
/// MAX_BINDING_KEYS consecutive numbers, so this many slots never collide.
const SNAPSHOTS: usize = 2 * MAX_BINDING_KEYS;

/// What the resolver remembers between keys (Go's keyResolver).
#[derive(Default)]
struct Resolver {
    pending: [Pending; MAX_BINDING_KEYS],
    n: usize,
    window: [Pending; MAX_BINDING_KEYS],
    in_force: [Span; MAX_BINDING_KEYS],
    n_force: usize,
    mode: Mode,
    next: usize,
    last_at: i64,
    has_last: bool,
}

impl Resolver {
    fn clear(&mut self) {
        self.n = 0;
        self.n_force = 0;
    }

    fn event(&mut self, now: i64) {
        self.clear();
        self.last_at = now;
        self.has_last = true;
    }

    /// Adds a key and gives the keys to resolve, as the window's length;
    /// the caller has taken the key's snapshot at num `self.next`.
    fn push(&mut self, code: KeyCode, now: i64) -> usize {
        let (gap, first) = (now - self.last_at, !self.has_last);
        self.last_at = now;
        self.has_last = true;
        if self.n == MAX_BINDING_KEYS {
            self.clear();
        }
        self.pending[self.n] = Pending { code, num: self.next, gap, first };
        self.next += 1;
        let n = self.n + 1;
        self.window[..n].copy_from_slice(&self.pending[..n]);
        n
    }

    /// Whether a binding may start at key num: not inside one in force.
    fn fits(&self, num: usize) -> bool {
        self.in_force[..self.n_force].iter().all(|s| !(num > s.start && num <= s.end))
    }

    fn can_grow(&self, t: &Trie, w: &[Pending]) -> bool {
        (0..w.len()).any(|p| self.fits(w[p].num) && t.grows(&w[p..]))
    }

    /// Keeps as pending the keys of w from the first that can still grow
    /// into a binding, and forgets the bindings in force that start before.
    fn keep(&mut self, t: &Trie, w: &[Pending]) {
        for p in 0..w.len() {
            if self.fits(w[p].num) && t.grows(&w[p..]) {
                let kept = w.len() - p;
                self.pending[..kept].copy_from_slice(&w[p..]);
                self.n = kept;
                let start = w[p].num;
                let mut k = 0;
                for i in 0..self.n_force {
                    if self.in_force[i].start >= start {
                        self.in_force[k] = self.in_force[i];
                        k += 1;
                    }
                }
                self.n_force = k;
                return;
            }
        }
        self.clear();
    }

    fn used(&self, t: &Trie, key: KeyCode) -> bool {
        let mut w = [Pending::default(); MAX_BINDING_KEYS];
        let n = self.n.min(MAX_BINDING_KEYS - 1);
        w[..n].copy_from_slice(&self.pending[..n]);
        w[n] = Pending { code: key, ..Pending::default() };
        (0..=n).any(|p| t.walk(&w[p..=n]).is_some())
    }
}

/// What a bindingFunc is given (Go's EditState): the line around the cursor
/// as it was before the binding's keys, the keys, and the mode.
pub struct EditState<'a> {
    pub before: &'a str,
    pub after: &'a str,
    pub keys: &'a [KeyCode],
    pub mode: Mode,
}

/// What the frontend provides: the engine's answers and the effects.
pub trait Host {
    /// Runs a bindingFunc in the engine: its ops, or None when it declines
    /// (or fails).
    fn call(&mut self, binding: &Binding, state: &EditState) -> Option<Vec<EditOp>>;
    /// Carries out an op that isn't pure (or `editExpandAbbr`), in order
    /// with the others, and reports whether the shell should end.
    fn effect(&mut self, op: &EditOp, line: &mut Line) -> bool;
}

/// The editor's line and its key resolver.
#[derive(Default)]
pub struct Editor {
    pub line: Line,
    pub mode: Mode,
    resolver: Resolver,
    snapshots: [Line; SNAPSHOTS],
    keys_buf: [KeyCode; MAX_BINDING_KEYS],
    /// The ids of the bindings that fired for the last event, in order.
    pub fired: Vec<u32>,
    /// Set when a key left complete mode, so the frontend ends the
    /// completion cycle.
    pub left_complete: bool,
}

impl Editor {
    pub fn new(text: &str, cursor: usize) -> Editor {
        Editor { line: Line { text: text.to_string(), cursor }, ..Editor::default() }
    }

    /// The keys pending.
    pub fn pending(&self) -> impl Iterator<Item = KeyCode> + '_ {
        self.resolver.pending[..self.resolver.n].iter().map(|k| k.code)
    }

    /// A paste: inserted as it is, never through the bindings.
    pub fn paste(&mut self, text: &str, now: i64) {
        self.fired.clear();
        // A paste ends a completion cycle, as any edit does.
        self.left_complete = self.mode == Mode::Complete;
        self.mode = Mode::Insert;
        self.resolver.event(now);
        if !text.is_empty() {
            self.line.insert(text);
        }
    }

    /// A key from the terminal or the window, at `now` (nanoseconds, any
    /// epoch). `esc_of` is set when the legacy encoding sent it as ESC and
    /// that key in one read (Go's handleTokenAt). Returns whether the shell
    /// should end.
    pub fn key(&mut self, km: &Keymap, code: KeyCode, esc_of: Option<KeyCode>, now: i64, host: &mut impl Host) -> bool {
        self.fired.clear();
        self.left_complete = false;
        let mut code = code;
        if let Some(esc_of) = esc_of {
            if !self.resolver.used(&km.tries[self.mode as usize], code) {
                if self.resolve(km, KeyCode::ESCAPE, now, host) {
                    return true;
                }
                code = esc_of;
            }
        }
        self.resolve(km, code, now, host)
    }

    /// Go's resolveKey: runs the longest binding the key completes, or does
    /// the key's own action, and keeps the keys that may still become part
    /// of a binding.
    fn resolve(&mut self, km: &Keymap, code: KeyCode, now: i64, host: &mut impl Host) -> bool {
        let mut mode = self.mode;
        if mode != self.resolver.mode {
            self.resolver.clear();
            self.resolver.mode = mode;
        }
        let slot = self.resolver.next % SNAPSHOTS;
        self.snapshots[slot].clone_from(&self.line);
        let len = self.resolver.push(code, now);
        let mut from = 0; // the window is window[from..len]
        loop {
            let trie = &km.tries[mode as usize];
            let w: [Pending; MAX_BINDING_KEYS] = self.resolver.window;
            let (fired, end) = self.fire_longest(km, trie, &w[from..len], host);
            if fired {
                return end;
            }
            // A key complete mode doesn't bind leaves it, and acts as in
            // insert mode, on its own.
            if mode == Mode::Complete && !self.resolver.can_grow(trie, &w[from..len]) {
                self.left_complete = true;
                mode = Mode::Insert;
                self.mode = Mode::Insert;
                self.resolver.mode = Mode::Insert;
                self.resolver.clear();
                from = len - 1;
                continue;
            }
            if let Some(c) = code.text() {
                let mut buf = [0u8; 4];
                self.line.insert(c.encode_utf8(&mut buf));
            }
            self.resolver.keep(trie, &w[from..len]);
            return false;
        }
    }

    /// Go's fireLongest with fireBinding: runs the longest binding that ends
    /// the keys of w, whose times fit and which doesn't decline. Returns
    /// whether one ran, and whether the shell should end.
    fn fire_longest(&mut self, km: &Keymap, t: &Trie, w: &[Pending], host: &mut impl Host) -> (bool, bool) {
        for p in 0..w.len() {
            let Some(n) = t.walk(&w[p..]) else { continue };
            let Some(index) = t.nodes[n as usize].entry else { continue };
            let b = &km.bindings[index as usize];
            if !timed(b, &w[p..]) || !self.resolver.fits(w[p].num) {
                continue;
            }
            let snap = w[p].num % SNAPSHOTS;
            let asked;
            let program: &[EditOp] = match &b.action {
                Action::Ops(ops) => ops,
                Action::Func(_) => {
                    let keys = &mut self.keys_buf[..w.len() - p];
                    for (k, pk) in keys.iter_mut().zip(&w[p..]) {
                        *k = pk.code;
                    }
                    let line = &self.snapshots[snap];
                    let state = EditState {
                        before: &line.text[..line.cursor],
                        after: &line.text[line.cursor..],
                        keys,
                        mode: self.resolver.mode,
                    };
                    match host.call(b, &state) {
                        Some(mut ops) => {
                            // Ops after one that ends are not run.
                            if let Some(i) = ops.iter().position(EditOp::ends) {
                                ops.truncate(i + 1);
                            }
                            asked = ops;
                            &asked
                        }
                        None => continue,
                    }
                }
            };
            self.fired.push(b.id);
            self.line.clone_from(&self.snapshots[snap]);
            let end = run_ops(&mut self.line, program, w[w.len() - 1].code, host);
            if b.is_pure() {
                let r = &mut self.resolver;
                if r.n_force < MAX_BINDING_KEYS {
                    let mut k = 0;
                    for i in 0..r.n_force {
                        if r.in_force[i].start < w[p].num {
                            r.in_force[k] = r.in_force[i];
                            k += 1;
                        }
                    }
                    r.in_force[k] = Span { start: w[p].num, end: w[w.len() - 1].num };
                    r.n_force = k + 1;
                }
                r.keep(t, w);
            } else {
                self.resolver.clear();
            }
            return (true, end);
        }
        (false, false)
    }
}

/// Go's runEditOps: runs a program on the line. `key` is the last key of the
/// sequence that ran it, which editSelfInsert types.
pub fn run_ops(line: &mut Line, program: &[EditOp], key: KeyCode, host: &mut impl Host) -> bool {
    for op in program {
        if line.move_or_delete(op) {
            continue;
        }
        let end = match op {
            EditOp::Insert(text) => {
                line.insert(text);
                false
            }
            EditOp::SelfInsert => {
                if let Some(c) = key.text() {
                    let mut buf = [0u8; 4];
                    line.insert(c.encode_utf8(&mut buf));
                }
                false
            }
            EditOp::SetLine(before, after) => {
                line.set(before, after);
                false
            }
            other => host.effect(other, line),
        };
        if end {
            return true;
        }
    }
    false
}
