//! A pane: one engine (`msh server`) and its session, its cards, and its
//! prompt box (ai/gui-terminal.typ: one engine and one PTY per pane).

use crate::keys::key_label;
use crate::render::{rgba, Glyphs, Quad, SOLID};
use crate::vt;
use msh_client::{Engine, EngineHost, Event, FmView};
use msh_editor::edit::{EditOp, Line};
use msh_editor::graphemes::clusters;
use msh_editor::keys::KeyCode;
use msh_editor::resolver::{Config, Editor, Mode};
use std::cell::RefCell;
use std::collections::HashMap;

// The theme.
pub const BG: [u8; 3] = [0x10, 0x10, 0x13];
const CARD_BG: [u8; 3] = [0x17, 0x17, 0x1b];
const HEADER_BG: [u8; 3] = [0x1e, 0x1e, 0x24];
const PROMPT_BG: [u8; 3] = [0x22, 0x1e, 0x2a];
const PROMPT_BG_UNFOCUSED: [u8; 3] = [0x1a, 0x19, 0x1e];
const MENU_BG: [u8; 3] = [0x26, 0x26, 0x2e];
const SELECTED_BG: [u8; 3] = [0x4a, 0x30, 0x5a];
pub const FG: [u8; 3] = [0xd8, 0xd8, 0xd8];
pub const DIM: [u8; 3] = [0x78, 0x78, 0x84];
pub const ACCENT: [u8; 3] = [0xc6, 0x78, 0xdd];
const COMMAND: [u8; 3] = [0x8a, 0xb4, 0xf8];
const OK: [u8; 3] = [0x7e, 0xc6, 0x99];
const FAILED: [u8; 3] = [0xe0, 0x6c, 0x75];

/// Columns between a card's edge and its output; rows between cards.
const MARGIN: usize = 1;
const CARD_GAP: usize = 1;
/// The prompt box's inner left edge.
const PROMPT_INSET: usize = 2;
/// The most rows the completion menu shows.
const MENU_ROWS: usize = 8;

pub fn color([r, g, b]: [u8; 3]) -> u32 {
    rgba(r, g, b, 0xff)
}

fn cursor_color() -> u32 {
    rgba(FG[0], FG[1], FG[2], 0x90)
}

/// A command and its output.
struct Block {
    id: u64,
    /// The line as it ran, or the keys of the binding whose quote it is;
    /// empty for output outside any block (startup messages).
    command: String,
    term: vt::Term,
    /// Exit code and duration, once finished.
    finished: Option<(i32, u64)>,
}

impl Block {
    /// Rows on screen: the header, the output, and the gap after it.
    fn rows(&self) -> usize {
        1 + self.term.content_rows() + CARD_GAP
    }
}

/// Completion candidates while a cycle is on.
#[derive(Default)]
struct Menu {
    candidates: Vec<String>,
    selected: Option<usize>,
}

/// What the editor's effects asked for while it ran a program.
#[derive(Default)]
struct Effects {
    accept: bool,
    exit: bool,
    clear: bool,
    message: Option<String>,
    /// A completion op's result: whether the cycle is on, and the menu.
    completed: Option<(bool, Menu)>,
    /// An editRun (false) or editRunOps (true) quote to run as a block.
    run: Option<(u32, bool)>,
}

fn effect(log: &RefCell<Effects>, engine: &mut Engine, op: &EditOp, line: &mut Line) -> bool {
    let mut e = log.borrow_mut();
    match op {
        EditOp::Accept => {
            e.accept = true;
            true
        }
        EditOp::Exit => {
            e.exit = true;
            true
        }
        EditOp::Message(m) => {
            e.message = Some(m.clone());
            false
        }
        EditOp::ClearScreen => {
            e.clear = true;
            false
        }
        EditOp::Run(q) => {
            e.run = Some((*q, false));
            false
        }
        EditOp::RunOps(q) => {
            e.run = Some((*q, true));
            false
        }
        EditOp::Complete
        | EditOp::CompleteBack
        | EditOp::CompleteNext
        | EditOp::CompletePrev
        | EditOp::CompleteLeft
        | EditOp::CompleteRight
        | EditOp::CompleteAccept => {
            if let Ok(r) = engine.edit(op, &line.text, line.cursor) {
                line.text = r.line;
                line.cursor = r.cursor;
                e.completed = Some((r.cycling, Menu { candidates: r.candidates, selected: r.selected }));
            }
            false
        }
        // Not yet: history.
        _ => false,
    }
}

pub struct Pane {
    pub id: u32,
    pub engine: Engine,
    /// The editor config; keys wait for the first.
    pub cfg: Option<Config>,
    editor: Editor,
    queued: Vec<(KeyCode, i64)>,
    blocks: Vec<Block>,
    pub running: Option<usize>,
    next_block: u64,
    /// The pane's size in cells.
    pub cols: usize,
    pub rows: usize,
    cell: (u32, u32),
    /// The prompt as the engine rendered it, in an emulator of its own.
    prompt: Option<vt::Term>,
    prompt_text: Vec<u8>,
    pub title: Option<String>,
    menu: Menu,
    /// Rows scrolled up from the newest output.
    pub scroll: usize,
    /// Where the screen was last cleared: the rows of the cards before it,
    /// which are above the view until scrolled back to.
    clear_rows: usize,
    message: Option<String>,
    /// The ops editRunOps quotes returned, by block, run when it finishes.
    pending_ops: HashMap<u64, Vec<EditOp>>,
    /// The file manager's view while it shows as a dialog, and the size
    /// last given it.
    pub fm: Option<FmView>,
    fm_size: (u16, u16),
    key_bytes: Vec<u8>,
}

impl Pane {
    pub fn new(id: u32, engine: Engine) -> Pane {
        Pane {
            id,
            engine,
            cfg: None,
            editor: Editor::default(),
            queued: Vec::new(),
            blocks: Vec::new(),
            running: None,
            next_block: 1,
            cols: 80,
            rows: 24,
            cell: (8, 16),
            prompt: None,
            prompt_text: Vec::new(),
            title: None,
            menu: Menu::default(),
            scroll: 0,
            clear_rows: 0,
            message: None,
            pending_ops: HashMap::new(),
            fm: None,
            fm_size: (0, 0),
            key_bytes: Vec::new(),
        }
    }

    /// Columns a card's output has: the PTY's width.
    fn output_cols(&self) -> u16 {
        self.cols.saturating_sub(2 * MARGIN).max(10) as u16
    }

    fn prompt_cols(&self) -> usize {
        self.cols.saturating_sub(2 * PROMPT_INSET).max(10)
    }

    /// The pane's size. Programs get the cards' width and the pane's height
    /// (a program on the alternate screen has the whole pane).
    pub fn resize(&mut self, cols: usize, rows: usize, cell: (u32, u32)) {
        if (cols, rows, cell) == (self.cols, self.rows, self.cell) {
            return;
        }
        (self.cols, self.rows, self.cell) = (cols, rows, cell);
        let oc = self.output_cols();
        let (cw, ch) = cell;
        let _ = self.engine.resize(oc, rows as u16, (oc as u32 * cw) as u16, (rows as u32 * ch) as u16);
        if let Some(i) = self.running {
            self.blocks[i].term.resize(oc, rows as u16, cw, ch);
        }
        self.set_prompt();
        if self.fm.is_some() {
            self.size_fm();
        }
    }

    /// The file manager dialog's place in the pane, in cells.
    fn fm_rect(&self) -> (usize, usize, usize, usize) {
        let (x, y) = (2.min(self.cols / 10), 1.min(self.rows / 10));
        (x, y, self.cols - 2 * x, self.rows - 2 * y)
    }

    /// Tells the file manager the dialog's size, when it changed.
    fn size_fm(&mut self) {
        let (_, _, w, h) = self.fm_rect();
        let size = (w as u16, h as u16);
        if size != self.fm_size {
            self.fm_size = size;
            let _ = self.engine.fm_resize(size.0, size.1);
        }
    }

    /// Lays out the prompt text in its emulator, at the box's width.
    fn set_prompt(&mut self) {
        if self.prompt_text.is_empty() {
            return;
        }
        let mut t = vt::Term::new(self.prompt_cols() as u16, 16);
        t.write(&self.prompt_text);
        self.prompt = Some(t);
    }

    /// Handles an event from the engine; returns whether the pane closes.
    pub fn engine_event(&mut self, event: Event) -> bool {
        match event {
            Event::EditorConfig { .. } => {
                if let Some(cfg) = self.engine.take_config() {
                    self.cfg = Some(cfg);
                }
                for (code, at) in std::mem::take(&mut self.queued) {
                    if self.editor_key(code, at) {
                        return true;
                    }
                }
            }
            Event::Prompt { title, text } => {
                self.prompt_text = text;
                self.set_prompt();
                if title.is_some() {
                    self.title = title;
                }
            }
            Event::PtyOutput { id, bytes } => {
                let i = match self.blocks.iter().position(|b| b.id == id) {
                    Some(i) => i,
                    None => {
                        // Output outside any block: startup messages.
                        let term = vt::Term::new(self.output_cols(), self.rows as u16);
                        self.blocks.push(Block { id, command: String::new(), term, finished: Some((0, 0)) });
                        self.blocks.len() - 1
                    }
                };
                self.blocks[i].term.write(&bytes);
            }
            Event::FileManager(view) => {
                if view.shown {
                    if self.fm.is_none() {
                        self.fm_size = (0, 0);
                    }
                    self.fm = Some(view);
                    self.size_fm();
                } else {
                    self.fm = None;
                }
            }
            Event::Ops { id, ops } => {
                self.pending_ops.insert(id, ops);
            }
            Event::BlockFinished { id, exit_code, duration_ns } => {
                self.fm = None;
                if let Some(b) = self.blocks.iter_mut().find(|b| b.id == id) {
                    b.finished = Some((exit_code, duration_ns));
                }
                self.running = None;
                if let Some(cfg) = self.engine.take_config() {
                    self.cfg = Some(cfg);
                }
                if let Some(ops) = self.pending_ops.remove(&id) {
                    if self.run_ops(&ops) {
                        return true;
                    }
                }
            }
            Event::Exit { .. } | Event::Closed { .. } => return true,
            Event::Hello { .. } | Event::BlockStarted { .. } => {}
        }
        false
    }

    /// A key at the prompt, through the editor; returns whether the pane
    /// closes.
    pub fn editor_key(&mut self, code: KeyCode, now: i64) -> bool {
        if let Some(cfg) = self.engine.take_config() {
            self.cfg = Some(cfg);
        }
        let Some(cfg) = &self.cfg else {
            self.queued.push((code, now));
            return false;
        };
        let log = RefCell::new(Effects::default());
        let mut host = EngineHost { engine: &mut self.engine, effect: |e: &mut Engine, op: &EditOp, l: &mut Line| effect(&log, e, op, l), problem: None };
        self.editor.key(cfg, code, None, now, &mut host);
        if log.borrow().accept {
            self.editor.expand_on_enter(cfg, &mut host);
        }
        let problem = host.problem.take();
        drop(host);
        // The keys of the binding that fired, to name an editRun's card.
        let label = self.editor.fired.last().and_then(|&id| cfg.keymap.bindings().get(id as usize)).map(|b| {
            b.keys.iter().map(|&k| key_label(k)).collect::<Vec<_>>().join(" ")
        });
        self.after(log.into_inner(), problem, label)
    }

    /// Runs the ops an editRunOps quote returned on the line.
    fn run_ops(&mut self, ops: &[EditOp]) -> bool {
        let Some(cfg) = &self.cfg else { return false };
        let log = RefCell::new(Effects::default());
        let mut host = EngineHost { engine: &mut self.engine, effect: |e: &mut Engine, op: &EditOp, l: &mut Line| effect(&log, e, op, l), problem: None };
        self.editor.run_ops(cfg, ops, KeyCode::NONE, &mut host);
        let problem = host.problem.take();
        drop(host);
        self.after(log.into_inner(), problem, None)
    }

    /// Carries out what the effects asked for; returns whether the pane
    /// closes.
    fn after(&mut self, e: Effects, problem: Option<String>, label: Option<String>) -> bool {
        self.message = e.message.or(problem);
        // The editor leaves complete mode on a key complete mode doesn't
        // bind; the engine ends its cycle too.
        if self.editor.left_complete {
            let (text, cursor) = (self.editor.line.text.clone(), self.editor.line.cursor);
            let _ = self.engine.edit(&EditOp::CompleteAccept, &text, cursor);
            self.menu = Menu::default();
        }
        if let Some((cycling, menu)) = e.completed {
            self.editor.mode = if cycling { Mode::Complete } else { Mode::Insert };
            self.menu = if cycling { menu } else { Menu::default() };
        }
        if let Some(cfg) = self.engine.take_config() {
            self.cfg = Some(cfg);
        }
        if e.clear {
            // As a terminal's clear: the cards so far go above the view.
            self.clear_rows = self.blocks.iter().map(Block::rows).sum();
        }
        self.scroll = 0;
        if e.exit {
            return true;
        }
        if e.accept {
            self.run_line();
        } else if let Some((quote, with_state)) = e.run {
            self.run_quote(quote, with_state, label.unwrap_or_else(|| "editRun".into()));
        }
        false
    }

    fn new_block(&mut self, command: String) -> u64 {
        let id = self.next_block;
        self.next_block += 1;
        let term = vt::Term::new(self.output_cols(), self.rows as u16);
        self.blocks.push(Block { id, command, term, finished: None });
        self.running = Some(self.blocks.len() - 1);
        self.menu = Menu::default();
        self.editor.mode = Mode::Insert;
        id
    }

    fn run_line(&mut self) {
        let line = std::mem::take(&mut self.editor.line.text);
        self.editor.line.cursor = 0;
        if line.trim().is_empty() {
            return;
        }
        let id = self.new_block(line.clone());
        let _ = self.engine.run(id, &line);
    }

    /// An editRun or editRunOps quote, as a block; the line stays.
    fn run_quote(&mut self, quote: u32, with_state: bool, label: String) {
        let id = self.new_block(label);
        let line = &self.editor.line;
        let (before, after) = line.text.split_at(line.cursor);
        let _ = self.engine.run_quote(id, quote, with_state, before, after);
    }

    /// A key while a block runs: to its program, encoded as its terminal's
    /// modes say.
    pub fn program_key(&mut self, key: ghostty_vt_sys::GhosttyKey, mods: ghostty_vt_sys::GhosttyMods, text: &str, unshifted: Option<char>) {
        let Some(i) = self.running else { return };
        self.blocks[i].term.encode_key(key, mods, text, unshifted, &mut self.key_bytes);
        if !self.key_bytes.is_empty() {
            let _ = self.engine.input(&self.key_bytes);
        }
        self.scroll = 0;
    }

    /// Draws the pane into `cv`, whose origin and size are the pane's.
    pub fn compose(&mut self, cv: &mut Canvas, glyphs: &mut Glyphs, focused: bool) {
        if self.fm.is_some() {
            self.compose_fm(cv, glyphs, focused);
            return;
        }
        let (cols, rows) = (self.cols, self.rows);
        let prompt_cols = self.prompt_cols();

        // A program on the alternate screen has the whole pane.
        if let Some(i) = self.running.filter(|&i| self.blocks[i].term.alternate_screen()) {
            let term = &mut self.blocks[i].term;
            term.scroll_to_bottom();
            let cursor = term.visit(0, rows, |r, c, cell| cv.cell(glyphs, MARGIN + c, r, cell, BG));
            if let Some((x, y)) = cursor.filter(|_| focused) {
                cv.cursor(MARGIN + x as usize, y as usize);
            }
            return;
        }

        // The prompt box, at the bottom: the rendered prompt, then the line.
        // While a command runs, it says so; keys go to the program.
        let line = &self.editor.line;
        let (prompt_x, prompt_y) = self.prompt.as_ref().map_or((0, 0), |p| {
            let (x, y) = p.cursor();
            (x as usize, y as usize)
        });
        let line_cells = clusters(&line.text).count();
        let cursor_cells = clusters(&line.text[..line.cursor]).count();
        let text_rows = if self.running.is_some() { 1 } else { prompt_y + (prompt_x + line_cells + 1).div_ceil(prompt_cols) };
        let box_rows = 2 + text_rows + self.message.is_some() as usize;
        let box_top = rows.saturating_sub(box_rows);
        let menu = &self.menu;
        let menu_width = menu.candidates.iter().map(|c| c.chars().count()).max().unwrap_or(0) + 2;
        let menu_cols = (prompt_cols / menu_width.max(1)).max(1);
        let menu_rows = menu.candidates.len().div_ceil(menu_cols).min(MENU_ROWS);
        let area = box_top.saturating_sub(menu_rows);

        // The cards. Cleared ones are above the view until scrolled to.
        let total: usize = self.blocks.iter().map(Block::rows).sum();
        let bottom_top = self.clear_rows.max(total.saturating_sub(area));
        self.scroll = self.scroll.min(bottom_top);
        let top = bottom_top - self.scroll;
        let mut y = 0usize;
        for (bi, b) in self.blocks.iter_mut().enumerate() {
            let height = b.rows();
            if y + height <= top || y >= top + area {
                y += height;
                continue;
            }
            let shown = |row: usize| row >= top && row < top + area;
            if shown(y) {
                let wy = y - top;
                cv.fill(0, wy, cols, color(HEADER_BG));
                let (mark, mark_color) = match b.finished {
                    None => ('…', DIM),
                    Some((0, _)) => ('●', OK),
                    Some(_) => ('●', FAILED),
                };
                if b.command.is_empty() {
                    cv.text(glyphs, MARGIN, wy, "startup", color(DIM));
                } else {
                    cv.put(glyphs, MARGIN, wy, mark, color(mark_color));
                    let command: String = b.command.chars().filter(|c| *c != '\n').take(cols.saturating_sub(4)).collect();
                    cv.text(glyphs, MARGIN + 2, wy, &command, color(COMMAND));
                }
                if let Some((code, ns)) = b.finished.filter(|_| !b.command.is_empty()) {
                    let secs = ns as f64 / 1e9;
                    let status = if code == 0 { format!("{secs:.1} s") } else { format!("exit {code}  {secs:.1} s") };
                    let x = cols.saturating_sub(status.chars().count() + MARGIN);
                    cv.text(glyphs, x, wy, &status, color(if code == 0 { DIM } else { FAILED }));
                }
            }
            let content_y = y + 1;
            let content = b.term.content_rows();
            for row in content_y..content_y + content {
                if shown(row) {
                    cv.fill(0, row - top, cols, color(CARD_BG));
                }
            }
            let from = top.max(content_y) - content_y;
            let to = (top + area).min(content_y + content).saturating_sub(content_y);
            if from < to {
                let off = b.term.scroll_to(from);
                let base = content_y + from;
                let cursor = b.term.visit(off, off + (to - from), |r, c, cell| {
                    cv.cell(glyphs, MARGIN + c, base + (r - off) - top, cell, CARD_BG);
                });
                b.term.scroll_to_bottom();
                if Some(bi) == self.running && focused {
                    if let Some((x, vy)) = cursor {
                        let row = content_y + (from - off) + vy as usize;
                        if shown(row) {
                            cv.cursor(MARGIN + x as usize, row - top);
                        }
                    }
                }
            }
            y += height;
        }

        // The completion menu, just above the prompt box.
        if menu_rows > 0 {
            let all_rows = menu.candidates.len().div_ceil(menu_cols);
            let first_row = menu.selected.map_or(0, |s| (s % all_rows).saturating_sub(menu_rows - 1));
            for r in 0..menu_rows {
                cv.fill(0, area + r, cols, color(MENU_BG));
            }
            for (i, c) in menu.candidates.iter().enumerate() {
                let (col, row) = (i / all_rows, i % all_rows);
                if row < first_row || row >= first_row + menu_rows {
                    continue;
                }
                let (x, wy) = (PROMPT_INSET + col * menu_width, area + row - first_row);
                if Some(i) == menu.selected {
                    cv.fill(x.saturating_sub(1), wy, menu_width, color(SELECTED_BG));
                }
                cv.text(glyphs, x, wy, c, color(FG));
            }
        }

        // The prompt box.
        let box_bg = if focused { PROMPT_BG } else { PROMPT_BG_UNFOCUSED };
        for r in box_top..rows {
            cv.fill(0, r, cols, color(box_bg));
        }
        let text_top = box_top + 1;
        if let Some(i) = self.running {
            cv.text(glyphs, PROMPT_INSET, text_top, "running:", color(DIM));
            let command: String = self.blocks[i].command.chars().filter(|c| *c != '\n').collect();
            cv.text(glyphs, PROMPT_INSET + 9, text_top, &command, color(COMMAND));
            return;
        }
        if let Some(p) = &mut self.prompt {
            p.visit(0, prompt_y + 1, |r, c, cell| cv.cell(glyphs, PROMPT_INSET + c, text_top + r, cell, box_bg));
        }
        let at = |cell: usize| (PROMPT_INSET + (prompt_x + cell) % prompt_cols, text_top + prompt_y + (prompt_x + cell) / prompt_cols);
        for (i, (s, _)) in clusters(&line.text).enumerate() {
            let (x, row) = at(i);
            cv.put(glyphs, x, row, line.text[s..].chars().next().unwrap_or(' '), color(FG));
        }
        if focused {
            let (cx, cy) = at(cursor_cells);
            cv.cursor(cx, cy);
        }
        if let Some(m) = &self.message {
            let text: String = m.trim_end().chars().filter(|c| *c != '\n').collect();
            cv.text(glyphs, PROMPT_INSET, text_top + text_rows, &text, color(DIM));
        }
    }
}

impl Pane {
    /// The file manager, as a dialog over the pane: the directory, its
    /// entries and the preview side by side, the status or prompt line,
    /// and the key popup or a question in a box over them.
    fn compose_fm(&mut self, cv: &mut Canvas, glyphs: &mut Glyphs, focused: bool) {
        let (x0, y0, w, h) = self.fm_rect();
        let Some(v) = &self.fm else { return };
        for r in y0..y0 + h {
            cv.fill(x0, r, w, color(FM_BG));
        }
        // The header: where, and what is cut, copied and selected.
        cv.fill(x0, y0, w, color(FM_HEADER_BG));
        let clip_w = v.clipboard.chars().count();
        let header: String = v.header.chars().take(w.saturating_sub(clip_w + 2)).collect();
        cv.text(glyphs, x0 + 1, y0, &header, color(FG));
        cv.text(glyphs, (x0 + w).saturating_sub(clip_w + 1), y0, &v.clipboard, color(CLIP));
        let body = y0 + 1;
        let list_rows = h.saturating_sub(2);
        if v.help {
            for (i, line) in v.preview.iter().take(list_rows).enumerate() {
                cv.text(glyphs, x0 + 1, body + i, &truncate(line, w - 2), color(FG));
            }
        } else {
            let left = (w * 2 / 5).clamp(20.min(w), w);
            let marker_w = v.entries.iter().map(|e| e.marker.chars().count()).max().unwrap_or(0);
            let size_w = v.entries.iter().map(|e| e.size.chars().count()).max().unwrap_or(0);
            for (i, e) in v.entries.iter().take(list_rows).enumerate() {
                let row = body + i;
                if e.cursor {
                    cv.fill(x0, row, left, color(if focused { SELECTED_BG } else { MENU_BG }));
                }
                let mut x = x0 + 1;
                if marker_w > 0 {
                    cv.text(glyphs, x, row, &e.marker, color(DIM));
                    x += marker_w + 1;
                }
                if e.selected {
                    cv.put(glyphs, x, row, '*', color(ACCENT));
                }
                x += 1;
                let name_color = if e.cut {
                    FAILED
                } else if e.copied {
                    OK
                } else if e.dir {
                    COMMAND
                } else {
                    FG
                };
                let name = if e.dir { format!("{}/", e.name) } else { e.name.clone() };
                let room = (x0 + left).saturating_sub(x + size_w + 2);
                cv.text(glyphs, x, row, &truncate(&name, room), color(name_color));
                if !e.size.is_empty() {
                    let sx = (x0 + left).saturating_sub(e.size.chars().count() + 1);
                    cv.text(glyphs, sx, row, &e.size, color(DIM));
                }
            }
            // The preview, past a rule.
            let px = x0 + left + 1;
            for r in 0..list_rows {
                cv.put(glyphs, x0 + left, body + r, '│', color(RULE));
            }
            let room = (x0 + w).saturating_sub(px + 1);
            for (i, line) in v.preview.iter().take(list_rows).enumerate() {
                let mut x = px + 1;
                for (text, fg) in sgr_spans(line) {
                    let n = text.chars().count().min((px + 1 + room).saturating_sub(x));
                    cv.text(glyphs, x, body + i, &truncate(&text, n), color(fg.unwrap_or(PREVIEW)));
                    x += n;
                }
            }
        }
        // The bottom: the prompt line, or a message, or the summary.
        let bottom = y0 + h - 1;
        if let Some((label, text, cursor)) = &v.line {
            cv.text(glyphs, x0 + 1, bottom, label, color(ACCENT));
            let lx = x0 + 1 + label.chars().count();
            cv.text(glyphs, lx, bottom, text, color(FG));
            if focused {
                let c = text[..(*cursor).min(text.len())].chars().count();
                cv.cursor(lx + c, bottom);
            }
        } else if v.status_kind != 0 {
            let c = if v.status_kind == 2 { MESSAGE } else { DIM };
            cv.text(glyphs, x0 + 1, bottom, &truncate(&v.status, w - 2), color(c));
        }
        // The key popup or a question, in a box over the rest.
        if !v.overlay.is_empty() {
            cv.on_top = true;
            let bw = v.overlay.iter().map(|l| l.chars().count()).max().unwrap_or(0).min(w - 4) + 2;
            let bh = v.overlay.len().min(h - 2);
            let (bx, by) = (x0 + (w - bw) / 2, y0 + (h - bh) / 2);
            for r in 0..bh {
                cv.fill(bx, by + r, bw, color(OVERLAY_BG));
            }
            for (i, line) in v.overlay.iter().take(bh).enumerate() {
                cv.text(glyphs, bx + 1, by + i, &truncate(line, bw - 2), color(if i == 0 { ACCENT } else { FG }));
            }
            cv.on_top = false;
        }
    }
}

/// A line's text split at its SGR sequences, each piece with the
/// foreground colour set before it (None: the default). Other attributes
/// are dropped.
fn sgr_spans(line: &str) -> Vec<(String, Option<[u8; 3]>)> {
    let mut spans = Vec::new();
    let mut fg = None;
    let mut rest = line;
    while let Some(i) = rest.find("\x1b[") {
        if i > 0 {
            spans.push((rest[..i].to_string(), fg));
        }
        let Some(m) = rest[i + 2..].find('m') else {
            rest = &rest[i + 2..];
            continue;
        };
        let params: Vec<u32> = rest[i + 2..i + 2 + m].split(';').map(|p| p.parse().unwrap_or(0)).collect();
        let mut k = 0;
        while k < params.len() {
            match params[k] {
                0 | 39 => fg = None,
                n @ 30..=37 => fg = Some(ANSI[(n - 30) as usize]),
                n @ 90..=97 => fg = Some(ANSI[(n - 90 + 8) as usize]),
                38 if params.get(k + 1) == Some(&5) => {
                    fg = params.get(k + 2).map(|&n| xterm256(n as u8));
                    k += 2;
                }
                38 if params.get(k + 1) == Some(&2) => {
                    if let (Some(&r), Some(&g), Some(&b)) = (params.get(k + 2), params.get(k + 3), params.get(k + 4)) {
                        fg = Some([r as u8, g as u8, b as u8]);
                    }
                    k += 4;
                }
                _ => {}
            }
            k += 1;
        }
        rest = &rest[i + 2 + m + 1..];
    }
    if !rest.is_empty() {
        spans.push((rest.to_string(), fg));
    }
    spans
}

/// The 16 basic colours.
const ANSI: [[u8; 3]; 16] = [
    [0x40, 0x40, 0x48], [0xe0, 0x6c, 0x75], [0x98, 0xc3, 0x79], [0xe5, 0xc0, 0x7b],
    [0x61, 0xaf, 0xef], [0xc6, 0x78, 0xdd], [0x56, 0xb6, 0xc2], [0xd8, 0xd8, 0xd8],
    [0x70, 0x70, 0x7a], [0xf0, 0x80, 0x88], [0xa8, 0xd3, 0x89], [0xf0, 0xd0, 0x8b],
    [0x81, 0xc0, 0xff], [0xd6, 0x98, 0xed], [0x66, 0xc6, 0xd2], [0xff, 0xff, 0xff],
];

/// xterm's 256-colour palette.
fn xterm256(n: u8) -> [u8; 3] {
    match n {
        0..=15 => ANSI[n as usize],
        16..=231 => {
            let i = n - 16;
            let level = |v: u8| if v == 0 { 0 } else { 55 + v * 40 };
            [level(i / 36), level(i / 6 % 6), level(i % 6)]
        }
        _ => {
            let g = 8 + (n - 232) * 10;
            [g, g, g]
        }
    }
}

/// At most `n` characters of `s`.
fn truncate(s: &str, n: usize) -> String {
    s.chars().take(n).collect()
}

const FM_BG: [u8; 3] = [0x16, 0x15, 0x1b];
const FM_HEADER_BG: [u8; 3] = [0x2c, 0x22, 0x36];
const OVERLAY_BG: [u8; 3] = [0x30, 0x2a, 0x3a];
const CLIP: [u8; 3] = [0x6c, 0xc8, 0xd0];
const RULE: [u8; 3] = [0x3a, 0x3a, 0x44];
const PREVIEW: [u8; 3] = [0xb0, 0xb0, 0xb8];
const MESSAGE: [u8; 3] = [0xe5, 0xc0, 0x7b];

/// Quads for part of the window: solid ones (backgrounds) first, then
/// glyphs and cursors over them. Positions are cells from the origin, a
/// pixel position, clipped to the size in cells.
pub struct Canvas<'a> {
    pub solid: &'a mut Vec<Quad>,
    pub glyph: &'a mut Vec<Quad>,
    /// Quads over everything else (a dialog's popup), in drawing order.
    pub top: &'a mut Vec<Quad>,
    /// While set, quads go to `top`.
    pub on_top: bool,
    pub x0: u32,
    pub y0: u32,
    pub cols: usize,
    pub rows: usize,
    /// The cell's size in pixels.
    pub cell: (u32, u32),
}

impl Canvas<'_> {
    fn at(&self, col: usize, row: usize, width: usize, glyph: u32, color: u32) -> Quad {
        let (cw, ch) = self.cell;
        Quad { x: self.x0 + col as u32 * cw, y: self.y0 + row as u32 * ch, w: width as u32 * cw, h: ch, glyph, color }
    }

    pub fn put(&mut self, glyphs: &mut Glyphs, col: usize, row: usize, c: char, fg: u32) {
        if c != ' ' && col < self.cols && row < self.rows {
            let slot = glyphs.slot(c);
            let q = self.at(col, row, 1, slot, fg);
            if self.on_top { self.top.push(q) } else { self.glyph.push(q) }
        }
    }

    pub fn text(&mut self, glyphs: &mut Glyphs, col: usize, row: usize, s: &str, fg: u32) {
        for (i, c) in s.chars().enumerate() {
            self.put(glyphs, col + i, row, c, fg);
        }
    }

    pub fn fill(&mut self, col: usize, row: usize, width: usize, bg: u32) {
        if row < self.rows && col < self.cols {
            let q = self.at(col, row, width.min(self.cols - col), SOLID, bg);
            if self.on_top { self.top.push(q) } else { self.solid.push(q) }
        }
    }

    fn cursor(&mut self, col: usize, row: usize) {
        if col < self.cols && row < self.rows {
            let q = self.at(col, row, 1, SOLID, cursor_color());
            self.glyph.push(q);
        }
    }

    /// A cell of an emulator, over `bg` where it has no background of its
    /// own.
    fn cell(&mut self, glyphs: &mut Glyphs, col: usize, row: usize, cell: &vt::Cell, bg: [u8; 3]) {
        let (fg, cell_bg) = if cell.inverse_default && cell.fg.is_none() && cell.bg.is_none() {
            (color(bg), Some(color(FG)))
        } else {
            (color(cell.fg.unwrap_or(FG)), cell.bg.map(color))
        };
        if let Some(b) = cell_bg {
            self.fill(col, row, 1, b);
        }
        self.put(glyphs, col, row, cell.ch, fg);
    }
}
