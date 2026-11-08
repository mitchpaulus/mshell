//! The keystroke path allocates nothing once warm (the Go editor's rule,
//! kept in the port): lexing the line, resolving keys, running pure ops.

use msh_editor::abbr::{Abbr, AbbrExpansion, AbbrMatch, AbbrPosition};
use msh_editor::edit::{EditOp, Line};
use msh_editor::keys::{KeyCode, MOD_CTRL};
use msh_editor::lexer::Lexer;
use msh_editor::resolver::{Action, Binding, Config, EditState, Editor, Host, Keymap, Mode};
use std::alloc::{GlobalAlloc, Layout, System};
use std::sync::atomic::{AtomicUsize, Ordering};

struct Counting;
static ALLOCS: AtomicUsize = AtomicUsize::new(0);

unsafe impl GlobalAlloc for Counting {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        ALLOCS.fetch_add(1, Ordering::Relaxed);
        unsafe { System.alloc(layout) }
    }
    unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
        unsafe { System.dealloc(ptr, layout) }
    }
    unsafe fn realloc(&self, ptr: *mut u8, layout: Layout, new_size: usize) -> *mut u8 {
        ALLOCS.fetch_add(1, Ordering::Relaxed);
        unsafe { System.realloc(ptr, layout, new_size) }
    }
}

#[global_allocator]
static GLOBAL: Counting = Counting;

struct NoEngine;
impl Host for NoEngine {
    fn call(&mut self, _: &Binding, _: &EditState) -> Option<Vec<EditOp>> {
        None
    }
    fn effect(&mut self, _: &EditOp, _: &mut Line) -> bool {
        false
    }
}

fn bind(id: u32, keys: &[KeyCode], within_ms: i64, ops: Vec<EditOp>) -> Binding {
    Binding { id, keys: keys.to_vec(), mode: Mode::Insert, within: within_ms * 1_000_000, idle_before: 0, action: Action::Ops(ops) }
}

#[test]
fn keystroke_path_does_not_allocate() {
    let c = KeyCode::char;
    // gs expands in command position, and ls's -l anywhere after it, with a
    // cursor marker.
    let abbrs = vec![
        Abbr {
            id: 0,
            matcher: AbbrMatch::Word("gs".into()),
            position: AbbrPosition::Command,
            expansion: AbbrExpansion::Text("git status".into()),
            marker: String::new(),
        },
        Abbr {
            id: 1,
            matcher: AbbrMatch::Word("-l".into()),
            position: AbbrPosition::Anywhere,
            expansion: AbbrExpansion::Text("-la %".into()),
            marker: "%".into(),
        },
    ];
    let cfg = Config { abbrs, keymap: Keymap::new(vec![
        // A chord, a pure multi-key binding, and moves and deletes.
        bind(0, &[c('j'), c('k')], 200, vec![EditOp::Backspace, EditOp::Insert("→".into())]),
        bind(1, &[c('w').with(MOD_CTRL)], 0, vec![EditOp::DeleteWordLeft]),
        bind(2, &[KeyCode::LEFT], 0, vec![EditOp::Left]),
        bind(3, &[c('a').with(MOD_CTRL)], 0, vec![EditOp::LineStart, EditOp::WordRight]),
        bind(4, &[c(' ')], 0, vec![EditOp::ExpandAbbr, EditOp::SelfInsert]),
    ]) };
    let mut ed = Editor::new("", 0);
    let mut lexer = Lexer::editor();
    let mut tokens = Vec::new();
    let keys: Vec<KeyCode> = "gs ; echo $\"a{@x}b\" 'é世' jk |  ls -l jj kk 👍 "
        .chars()
        .map(c)
        .chain([KeyCode::LEFT, c('w').with(MOD_CTRL), c('a').with(MOD_CTRL)])
        .collect();
    let mut type_all = |ed: &mut Editor, now: &mut i64| {
        for &k in &keys {
            *now += 50_000_000;
            ed.key(&cfg, k, None, *now, &mut NoEngine);
            lexer.tokenize_into(&ed.line.text, &mut tokens);
        }
    };
    let mut now = 0;
    // Warm: strings and vectors reach their sizes.
    for _ in 0..3 {
        ed.line.text.clear();
        ed.line.cursor = 0;
        type_all(&mut ed, &mut now);
    }
    ed.line.text.clear();
    ed.line.cursor = 0;
    let before = ALLOCS.load(Ordering::Relaxed);
    type_all(&mut ed, &mut now);
    let allocs = ALLOCS.load(Ordering::Relaxed) - before;
    assert!(ed.line.text.contains('→'), "the chord fired: {:?}", ed.line.text);
    assert!(ed.line.text.contains("git status"), "gs expanded: {:?}", ed.line.text);
    assert!(ed.line.text.contains("-la "), "-l expanded: {:?}", ed.line.text);
    assert_eq!(allocs, 0, "allocations on the keystroke path");
}
