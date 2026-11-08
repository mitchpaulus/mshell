//! The mshell GUI (ai/gui-terminal.typ). Not a terminal emulator: each
//! command is a card with its output, which alone runs in a terminal
//! emulator of its own, and the prompt is a box of its own at the bottom,
//! edited locally by the Rust port of msh's editor. The engine (`msh
//! server`) runs the lines, renders the prompt, completes, and answers what
//! only it can.
//!
//! Usage: msh-gui [MSH]   (MSH: the msh binary; default $MSH_BIN, then the
//!                         repository's mshell/mshell, then msh on PATH)
//! MSH_GUI_FONT and MSH_GUI_FONT_SIZE choose the font.
//!
//! Not yet: history, selection, mouse reporting; a cell shows its first code
//! point, one cell wide.

mod keys;
mod render;
mod script;
mod vt;

use msh_client::{Engine, EngineHost, Event};
use msh_editor::edit::{EditOp, Line};
use msh_editor::graphemes::clusters;
use msh_editor::keys::KeyCode;
use msh_editor::resolver::{Config, Editor, Mode};
use render::{rgba, Glyphs, Quad, Renderer, SOLID};
use std::cell::{Cell as Flag, RefCell};
use std::sync::Arc;
use std::time::Instant;
use winit::application::ApplicationHandler;
use winit::event::{ElementState, MouseScrollDelta, WindowEvent};
use winit::event_loop::{ActiveEventLoop, ControlFlow, EventLoop, EventLoopProxy};
use winit::keyboard::{Key, ModifiersState};
use winit::platform::modifier_supplement::KeyEventExtModifierSupplement;
use winit::window::{Window, WindowId};

enum UserEvent {
    Engine(Event),
}

// The theme.
const BG: [u8; 3] = [0x10, 0x10, 0x13];
const CARD_BG: [u8; 3] = [0x17, 0x17, 0x1b];
const HEADER_BG: [u8; 3] = [0x1e, 0x1e, 0x24];
const PROMPT_BG: [u8; 3] = [0x22, 0x1e, 0x2a];
const MENU_BG: [u8; 3] = [0x26, 0x26, 0x2e];
const SELECTED_BG: [u8; 3] = [0x4a, 0x30, 0x5a];
const FG: [u8; 3] = [0xd8, 0xd8, 0xd8];
const DIM: [u8; 3] = [0x78, 0x78, 0x84];
const COMMAND: [u8; 3] = [0x8a, 0xb4, 0xf8];
const OK: [u8; 3] = [0x7e, 0xc6, 0x99];
const FAILED: [u8; 3] = [0xe0, 0x6c, 0x75];

/// Columns between a card's edge and its output; rows between cards.
const MARGIN: usize = 1;
const GAP: usize = 1;
/// The prompt box's inner left edge.
const PROMPT_INSET: usize = 2;
/// The most rows the completion menu shows.
const MENU_ROWS: usize = 8;

fn color([r, g, b]: [u8; 3]) -> u32 {
    rgba(r, g, b, 0xff)
}

fn cursor_color() -> u32 {
    rgba(FG[0], FG[1], FG[2], 0x90)
}

/// A command and its output.
struct Block {
    id: u64,
    /// The line as it ran; empty for output outside any block (startup
    /// messages).
    command: String,
    term: vt::Term,
    /// Exit code and duration, once finished.
    finished: Option<(i32, u64)>,
}

impl Block {
    /// Rows on screen: the header, the output, and the gap after it.
    fn rows(&self) -> usize {
        1 + self.term.content_rows() + GAP
    }
}

/// Completion candidates while a cycle is on.
#[derive(Default)]
struct Menu {
    candidates: Vec<String>,
    selected: Option<usize>,
}

struct App {
    engine: Engine,
    window: Option<Arc<Window>>,
    renderer: Option<Renderer>,
    glyphs: Option<Glyphs>,
    font: Vec<u8>,
    font_size: f32,
    /// The editor config; keys wait for the first.
    cfg: Option<Config>,
    editor: Editor,
    queued: Vec<(KeyCode, i64)>,
    blocks: Vec<Block>,
    running: Option<usize>,
    next_block: u64,
    cols: u16,
    rows: u16,
    /// The prompt as the engine rendered it, in an emulator of its own.
    prompt: Option<vt::Term>,
    prompt_text: Vec<u8>,
    menu: Menu,
    /// Rows scrolled up from the newest output.
    scroll: usize,
    mods: ModifiersState,
    start: Instant,
    message: Option<String>,
    quads: Vec<Quad>,
    glyph_quads: Vec<Quad>,
    key_bytes: Vec<u8>,
}

impl App {
    fn now(&self) -> i64 {
        self.start.elapsed().as_nanos() as i64
    }

    fn redraw(&self) {
        if let Some(w) = &self.window {
            w.request_redraw();
        }
    }

    /// Columns a card's output has: the PTY's width.
    fn output_cols(&self) -> u16 {
        (self.cols as usize).saturating_sub(2 * MARGIN).max(10) as u16
    }

    /// Columns the prompt box's text has.
    fn prompt_cols(&self) -> u16 {
        (self.cols as usize).saturating_sub(2 * PROMPT_INSET).max(10) as u16
    }

    /// The grid for the window's size. Programs get the cards' width and
    /// the window's height (a program on the alternate screen has it all).
    fn resize(&mut self, width: u32, height: u32) {
        let Some(glyphs) = &self.glyphs else { return };
        let (cw, ch) = (glyphs.cell.w, glyphs.cell.h);
        self.cols = (width / cw).max(1) as u16;
        self.rows = (height / ch).max(1) as u16;
        if let Some(r) = &mut self.renderer {
            r.resize(width, height);
        }
        let (oc, rows) = (self.output_cols(), self.rows);
        let _ = self.engine.resize(oc, rows, oc as u16 * cw as u16, rows * ch as u16);
        if let Some(i) = self.running {
            self.blocks[i].term.resize(oc, rows, cw, ch);
        }
        self.set_prompt();
    }

    /// Lays out the prompt text in its emulator, at the box's width.
    fn set_prompt(&mut self) {
        if self.prompt_text.is_empty() {
            return;
        }
        let mut t = vt::Term::new(self.prompt_cols(), 16);
        t.write(&self.prompt_text);
        self.prompt = Some(t);
    }

    /// Handles an event from the engine; returns whether the app should end.
    fn engine_event(&mut self, event: Event) -> bool {
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
                if let (Some(t), Some(w)) = (title, &self.window) {
                    w.set_title(&t);
                }
            }
            Event::PtyOutput { id, bytes } => {
                let i = match self.blocks.iter().position(|b| b.id == id) {
                    Some(i) => i,
                    None => {
                        // Output outside any block: startup messages.
                        self.blocks.push(Block {
                            id,
                            command: String::new(),
                            term: vt::Term::new(self.output_cols(), self.rows),
                            finished: Some((0, 0)),
                        });
                        self.blocks.len() - 1
                    }
                };
                self.blocks[i].term.write(&bytes);
            }
            Event::BlockFinished { id, exit_code, duration_ns } => {
                if let Some(b) = self.blocks.iter_mut().find(|b| b.id == id) {
                    b.finished = Some((exit_code, duration_ns));
                }
                self.running = None;
                if let Some(cfg) = self.engine.take_config() {
                    self.cfg = Some(cfg);
                }
            }
            Event::Exit { .. } | Event::Closed { .. } => return true,
            Event::Hello { .. } | Event::BlockStarted { .. } => {}
        }
        self.redraw();
        false
    }

    /// A key at the prompt, through the editor; returns whether the app
    /// should end.
    fn editor_key(&mut self, code: KeyCode, now: i64) -> bool {
        if let Some(cfg) = self.engine.take_config() {
            self.cfg = Some(cfg);
        }
        let Some(cfg) = &self.cfg else {
            self.queued.push((code, now));
            return false;
        };
        let (accept, exit, clear) = (Flag::new(false), Flag::new(false), Flag::new(false));
        let message = RefCell::new(None);
        // A completion op's result: whether the cycle is on, and the menu.
        let completed: RefCell<Option<(bool, Menu)>> = RefCell::new(None);
        let effect = |engine: &mut Engine, op: &EditOp, line: &mut Line| match op {
            EditOp::Accept => {
                accept.set(true);
                true
            }
            EditOp::Exit => {
                exit.set(true);
                true
            }
            EditOp::Message(m) => {
                *message.borrow_mut() = Some(m.clone());
                false
            }
            EditOp::ClearScreen => {
                clear.set(true);
                false
            }
            EditOp::Complete
            | EditOp::CompleteBack
            | EditOp::CompleteNext
            | EditOp::CompletePrev
            | EditOp::CompleteLeft
            | EditOp::CompleteRight
            | EditOp::CompleteAccept => {
                if let Ok(e) = engine.edit(op, &line.text, line.cursor) {
                    line.text = e.line;
                    line.cursor = e.cursor;
                    let menu = Menu { candidates: e.candidates, selected: e.selected };
                    *completed.borrow_mut() = Some((e.cycling, menu));
                }
                false
            }
            // Not yet: history, editRun.
            _ => false,
        };
        let mut host = EngineHost { engine: &mut self.engine, effect, problem: None };
        self.editor.key(cfg, code, None, now, &mut host);
        if accept.get() {
            self.editor.expand_on_enter(cfg, &mut host);
        }
        let problem = host.problem.take();
        drop(host);
        self.message = message.into_inner().or(problem);
        // The editor leaves complete mode on a key complete mode doesn't
        // bind; the engine ends its cycle too.
        if self.editor.left_complete {
            let line = &self.editor.line;
            let _ = self.engine.edit(&EditOp::CompleteAccept, &line.text.clone(), line.cursor);
            self.menu = Menu::default();
        }
        if let Some((cycling, menu)) = completed.into_inner() {
            self.editor.mode = if cycling { Mode::Complete } else { Mode::Insert };
            self.menu = if cycling { menu } else { Menu::default() };
        }
        if let Some(cfg) = self.engine.take_config() {
            self.cfg = Some(cfg);
        }
        if clear.get() {
            self.blocks.clear();
        }
        self.scroll = 0;
        if exit.get() {
            return true;
        }
        if accept.get() {
            self.run_line();
        }
        false
    }

    fn run_line(&mut self) {
        let line = std::mem::take(&mut self.editor.line.text);
        self.editor.line.cursor = 0;
        self.editor.mode = Mode::Insert;
        self.menu = Menu::default();
        if line.trim().is_empty() {
            return;
        }
        let id = self.next_block;
        self.next_block += 1;
        let term = vt::Term::new(self.output_cols(), self.rows);
        self.blocks.push(Block { id, command: line.clone(), term, finished: None });
        self.running = Some(self.blocks.len() - 1);
        let _ = self.engine.run(id, &line);
    }

    /// A key while a block runs: to its program, encoded as its terminal's
    /// modes say.
    fn program_key(&mut self, i: usize, event: &winit::event::KeyEvent) {
        let text = event.text.as_ref().map_or("", |t| t.as_str());
        let unshifted = match event.key_without_modifiers() {
            Key::Character(s) => s.chars().next(),
            _ => None,
        };
        let key = keys::ghostty_key(event);
        let mods = keys::ghostty_mods(self.mods);
        self.blocks[i].term.encode_key(key, mods, text, unshifted, &mut self.key_bytes);
        if !self.key_bytes.is_empty() {
            let _ = self.engine.input(&self.key_bytes);
        }
        self.scroll = 0;
    }

    fn frame(&mut self) {
        self.compose();
        let (Some(renderer), Some(glyphs)) = (&mut self.renderer, &mut self.glyphs) else { return };
        renderer.draw(glyphs, &self.quads, BG.map(|v| v as f64 / 255.0));
    }

    /// Lays out the frame as quads in `self.quads`: solid ones first, then
    /// glyphs.
    fn compose(&mut self) {
        let prompt_cols = self.prompt_cols() as usize;
        let Some(glyphs) = &mut self.glyphs else { return };
        let (cols, rows) = (self.cols as usize, self.rows as usize);
        self.quads.clear();
        self.glyph_quads.clear();
        let mut cv = Canvas { solid: &mut self.quads, glyph: &mut self.glyph_quads, cols, rows };

        // A program on the alternate screen has the whole window.
        if let Some(i) = self.running.filter(|&i| self.blocks[i].term.alternate_screen()) {
            let term = &mut self.blocks[i].term;
            term.scroll_to_bottom();
            let cursor = term.visit(0, rows, |r, c, cell| cv.cell(glyphs, MARGIN + c, r, cell, BG));
            if let Some((x, y)) = cursor {
                cv.glyph.push(Quad::solid(MARGIN + x as usize, y as usize, 1, cursor_color()));
            }
            cv.solid.extend_from_slice(cv.glyph);
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

        // The cards, newest at the bottom of the area above the prompt.
        let total: usize = self.blocks.iter().map(Block::rows).sum();
        self.scroll = self.scroll.min(total.saturating_sub(area));
        let top = total.saturating_sub(area + self.scroll);
        let mut y = 0usize;
        for (bi, b) in self.blocks.iter_mut().enumerate() {
            let height = b.rows();
            if y + height <= top || y >= top + area {
                y += height;
                continue;
            }
            let shown = |row: usize| row >= top && row < top + area;
            // The header: status, command, time.
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
            // The output, inset, on the card's background.
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
                if Some(bi) == self.running {
                    if let Some((x, vy)) = cursor {
                        let row = content_y + (from - off) + vy as usize;
                        if shown(row) {
                            cv.glyph.push(Quad::solid(MARGIN + x as usize, row - top, 1, cursor_color()));
                        }
                    }
                }
            }
            y += height;
        }

        // The completion menu, just above the prompt box.
        if menu_rows > 0 {
            let first_row = menu.selected.map_or(0, |s| (s % menu.candidates.len().div_ceil(menu_cols)).saturating_sub(menu_rows - 1));
            let all_rows = menu.candidates.len().div_ceil(menu_cols);
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
        for r in box_top..rows {
            cv.fill(0, r, cols, color(PROMPT_BG));
        }
        let text_top = box_top + 1;
        if let Some(i) = self.running {
            cv.text(glyphs, PROMPT_INSET, text_top, "running:", color(DIM));
            let command: String = self.blocks[i].command.chars().filter(|c| *c != '\n').collect();
            cv.text(glyphs, PROMPT_INSET + 9, text_top, &command, color(COMMAND));
        } else {
            if let Some(p) = &mut self.prompt {
                p.visit(0, prompt_y + 1, |r, c, cell| cv.cell(glyphs, PROMPT_INSET + c, text_top + r, cell, PROMPT_BG));
            }
            let at = |cell: usize| (PROMPT_INSET + (prompt_x + cell) % prompt_cols, text_top + prompt_y + (prompt_x + cell) / prompt_cols);
            for (i, (s, _)) in clusters(&line.text).enumerate() {
                let (x, row) = at(i);
                cv.put(glyphs, x, row, line.text[s..].chars().next().unwrap_or(' '), color(FG));
            }
            let (cx, cy) = at(cursor_cells);
            cv.glyph.push(Quad::solid(cx, cy, 1, cursor_color()));
            if let Some(m) = &self.message {
                let text: String = m.trim_end().chars().filter(|c| *c != '\n').collect();
                cv.text(glyphs, PROMPT_INSET, text_top + text_rows, &text, color(DIM));
            }
        }
        cv.solid.extend_from_slice(cv.glyph);
    }
}

impl Quad {
    fn solid(col: usize, row: usize, width: usize, color: u32) -> Quad {
        Quad { col: col as u32, row: row as u32, width: width as u32, height: 1, glyph: SOLID, color }
    }
}

/// The frame's quads: solid ones (backgrounds, the cursor) first, then
/// glyphs over them.
struct Canvas<'a> {
    solid: &'a mut Vec<Quad>,
    glyph: &'a mut Vec<Quad>,
    cols: usize,
    rows: usize,
}

impl Canvas<'_> {
    fn put(&mut self, glyphs: &mut Glyphs, col: usize, row: usize, c: char, fg: u32) {
        if c != ' ' && col < self.cols && row < self.rows {
            let slot = glyphs.slot(c);
            self.glyph.push(Quad { col: col as u32, row: row as u32, width: 1, height: 1, glyph: slot, color: fg });
        }
    }

    fn text(&mut self, glyphs: &mut Glyphs, col: usize, row: usize, s: &str, fg: u32) {
        for (i, c) in s.chars().enumerate() {
            self.put(glyphs, col + i, row, c, fg);
        }
    }

    fn fill(&mut self, col: usize, row: usize, width: usize, bg: u32) {
        if row < self.rows {
            self.solid.push(Quad::solid(col, row, width, bg));
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

impl ApplicationHandler<UserEvent> for App {
    fn resumed(&mut self, el: &ActiveEventLoop) {
        if self.window.is_some() {
            return;
        }
        let attrs = Window::default_attributes().with_title("msh").with_inner_size(winit::dpi::LogicalSize::new(960.0, 640.0));
        let window = Arc::new(el.create_window(attrs).expect("create_window"));
        let glyphs = Glyphs::new(std::mem::take(&mut self.font), self.font_size * window.scale_factor() as f32);
        let renderer = Renderer::new(window.clone(), &glyphs);
        self.glyphs = Some(glyphs);
        self.renderer = Some(renderer);
        let size = window.inner_size();
        self.window = Some(window);
        self.resize(size.width, size.height);
        self.redraw();
    }

    fn user_event(&mut self, el: &ActiveEventLoop, event: UserEvent) {
        let UserEvent::Engine(e) = event;
        if self.engine_event(e) {
            el.exit();
        }
    }

    fn window_event(&mut self, el: &ActiveEventLoop, _: WindowId, event: WindowEvent) {
        match event {
            WindowEvent::CloseRequested => el.exit(),
            WindowEvent::Resized(size) => {
                self.resize(size.width, size.height);
                self.redraw();
            }
            WindowEvent::ModifiersChanged(m) => self.mods = m.state(),
            WindowEvent::KeyboardInput { event, .. } if event.state == ElementState::Pressed => {
                if let Some(i) = self.running {
                    self.program_key(i, &event);
                } else if let Some(code) = keys::editor_key(&event, self.mods) {
                    let now = self.now();
                    if self.editor_key(code, now) {
                        el.exit();
                    }
                }
                self.redraw();
            }
            WindowEvent::MouseWheel { delta, .. } => {
                let lines = match delta {
                    MouseScrollDelta::LineDelta(_, y) => y * 3.0,
                    MouseScrollDelta::PixelDelta(p) => p.y as f32 / self.glyphs.as_ref().map_or(16.0, |g| g.cell.h as f32),
                };
                self.scroll = (self.scroll as i64 + lines.round() as i64).max(0) as usize;
                self.redraw();
            }
            WindowEvent::RedrawRequested => self.frame(),
            _ => {}
        }
    }
}

fn msh_binary(args: &[String]) -> String {
    if let Some(a) = args.iter().find(|a| !a.starts_with("--")) {
        return a.clone();
    }
    if let Ok(m) = std::env::var("MSH_BIN") {
        return m;
    }
    let repo = concat!(env!("CARGO_MANIFEST_DIR"), "/../../mshell/mshell");
    if std::path::Path::new(repo).exists() {
        return repo.to_string();
    }
    "msh".to_string()
}

fn main() {
    // --script SCRIPT --dump OUT [--size COLSxROWS]: no window (script.rs).
    let mut args: Vec<String> = std::env::args().skip(1).collect();
    let mut opt = |name: &str| {
        let i = args.iter().position(|a| a == name)?;
        let v = args.get(i + 1).cloned();
        args.drain(i..(i + 2).min(args.len()));
        v
    };
    let (script, dump, size) = (opt("--script"), opt("--dump"), opt("--size"));
    let msh = msh_binary(&args);
    let event_loop = (script.is_none()).then(|| {
        let el = EventLoop::<UserEvent>::with_user_event().build().expect("event loop");
        el.set_control_flow(ControlFlow::Wait);
        el
    });
    let (tx, rx) = std::sync::mpsc::channel();
    let proxy: Option<EventLoopProxy<UserEvent>> = event_loop.as_ref().map(|el| el.create_proxy());
    // The engine starts first: it loads init.msh while the window opens.
    let engine = Engine::spawn(&msh, move |e| match &proxy {
        Some(p) => {
            let _ = p.send_event(UserEvent::Engine(e));
        }
        None => {
            let _ = tx.send(UserEvent::Engine(e));
        }
    })
    .unwrap_or_else(|e| panic!("starting {msh} server: {e}"));
    let font_path = std::env::var("MSH_GUI_FONT").unwrap_or_else(|_| "/usr/share/fonts/noto/NotoSansMono-Regular.ttf".into());
    let font = std::fs::read(&font_path).unwrap_or_else(|e| panic!("reading font {font_path}: {e}"));
    let font_size = std::env::var("MSH_GUI_FONT_SIZE").ok().and_then(|s| s.parse().ok()).unwrap_or(15.0);
    let mut app = App {
        engine,
        window: None,
        renderer: None,
        glyphs: None,
        font,
        font_size,
        cfg: None,
        editor: Editor::default(),
        queued: Vec::new(),
        blocks: Vec::new(),
        running: None,
        next_block: 1,
        cols: 80,
        rows: 24,
        prompt: None,
        prompt_text: Vec::new(),
        menu: Menu::default(),
        scroll: 0,
        mods: ModifiersState::empty(),
        start: Instant::now(),
        message: None,
        quads: Vec::new(),
        glyph_quads: Vec::new(),
        key_bytes: Vec::new(),
    };
    if let Some(script) = script {
        let (cols, rows) = size
            .and_then(|s| s.split_once('x').and_then(|(c, r)| Some((c.parse().ok()?, r.parse().ok()?))))
            .unwrap_or((100, 30));
        let dump = dump.expect("--script needs --dump OUT.ppm");
        if let Err(e) = script::run(app, rx, &script, &dump, cols, rows) {
            eprintln!("msh-gui: {e}");
            std::process::exit(1);
        }
        return;
    }
    event_loop.unwrap().run_app(&mut app).expect("event loop");
}
