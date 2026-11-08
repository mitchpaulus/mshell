//! The mshell GUI (ai/gui-terminal.typ): one pane of blocks, each command's
//! output in its own terminal emulator, and the prompt edited locally by the
//! Rust port of msh's editor, with the engine (`msh server`) running the
//! lines and answering what only it can.
//!
//! Usage: msh-gui [MSH]   (MSH: the msh binary; default $MSH_BIN, then the
//!                         repository's mshell/mshell, then msh on PATH)
//! MSH_GUI_FONT and MSH_GUI_FONT_SIZE choose the font.
//!
//! First version: no history, completion, selection or mouse reporting; a
//! cell shows its first code point, one cell wide.

mod keys;
mod render;
mod script;
mod vt;

use msh_client::{Engine, EngineHost, Event};
use msh_editor::edit::{EditOp, Line};
use msh_editor::graphemes::clusters;
use msh_editor::keys::KeyCode;
use msh_editor::resolver::{Config, Editor};
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
const BG: [u8; 3] = [0x12, 0x12, 0x14];
const FG: [u8; 3] = [0xd8, 0xd8, 0xd8];
const DIM: [u8; 3] = [0x70, 0x70, 0x7a];
const ACCENT: [u8; 3] = [0xc6, 0x78, 0xdd];
const COMMAND: [u8; 3] = [0x8a, 0xb4, 0xf8];
const OK: [u8; 3] = [0x7e, 0xc6, 0x99];
const FAILED: [u8; 3] = [0xe0, 0x6c, 0x75];
const HEADER_BG: [u8; 3] = [0x1a, 0x1a, 0x1e];

fn color([r, g, b]: [u8; 3]) -> u32 {
    rgba(r, g, b, 0xff)
}

/// A command and its output.
struct Block {
    id: u64,
    /// The line as it ran; empty for output outside any block (startup
    /// messages), which has no header.
    command: String,
    term: vt::Term,
    /// Exit code and duration, once finished.
    finished: Option<(i32, u64)>,
}

impl Block {
    fn header_rows(&self) -> usize {
        !self.command.is_empty() as usize
    }

    fn rows(&self) -> usize {
        self.header_rows() + self.term.content_rows()
    }
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
    /// Rows scrolled up from the bottom.
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

    /// The pane's grid for the window's size, given to the engine's PTY and
    /// the running block's emulator.
    fn resize(&mut self, width: u32, height: u32) {
        let Some(glyphs) = &self.glyphs else { return };
        let (cw, ch) = (glyphs.cell.w, glyphs.cell.h);
        self.cols = (width / cw).max(1) as u16;
        self.rows = (height / ch).max(1) as u16;
        if let Some(r) = &mut self.renderer {
            r.resize(width, height);
        }
        let _ = self.engine.resize(self.cols, self.rows, width as u16, height as u16);
        if let Some(i) = self.running {
            self.blocks[i].term.resize(self.cols, self.rows, cw, ch);
        }
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
            Event::PtyOutput { id, bytes } => {
                let i = match self.blocks.iter().position(|b| b.id == id) {
                    Some(i) => i,
                    None => {
                        // Output outside any block: startup messages.
                        self.blocks.push(Block {
                            id,
                            command: String::new(),
                            term: vt::Term::new(self.cols, self.rows),
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
        let effect = |op: &EditOp, _: &mut Line| match op {
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
            // Not in the first version: history, completion, editRun.
            _ => false,
        };
        let mut host = EngineHost { engine: &mut self.engine, effect, problem: None };
        self.editor.key(cfg, code, None, now, &mut host);
        if accept.get() {
            self.editor.expand_on_enter(cfg, &mut host);
        }
        let problem = host.problem.take();
        self.message = message.into_inner().or(problem);
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
        if line.trim().is_empty() {
            return;
        }
        let id = self.next_block;
        self.next_block += 1;
        self.blocks.push(Block { id, command: line.clone(), term: vt::Term::new(self.cols, self.rows), finished: None });
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
        let Some(glyphs) = &mut self.glyphs else { return };
        let (cols, rows) = (self.cols as usize, self.rows as usize);
        self.quads.clear();
        self.glyph_quads.clear();
        let mut cv = Canvas { solid: &mut self.quads, glyph: &mut self.glyph_quads, cols, rows };
        let cell_color = |rgb: vt::Rgb, default: [u8; 3]| color(rgb.unwrap_or(default));

        // A program on the alternate screen has the whole pane.
        if let Some(i) = self.running.filter(|&i| self.blocks[i].term.alternate_screen()) {
            let term = &mut self.blocks[i].term;
            term.scroll_to_bottom();
            let cursor = term.visit(0, rows, |r, c, cell| {
                let (fg, bg) = if cell.inverse_default && cell.fg.is_none() && cell.bg.is_none() {
                    (color(BG), Some(color(FG)))
                } else {
                    (cell_color(cell.fg, FG), cell.bg.map(|b| color(b)))
                };
                if let Some(bg) = bg {
                    cv.fill(c, r, 1, bg);
                }
                cv.put(glyphs, c, r, cell.ch, fg);
            });
            if let Some((x, y)) = cursor {
                cv.glyph.push(Quad { col: x as u32, row: y as u32, width: 1, height: 1, glyph: SOLID, color: rgba(FG[0], FG[1], FG[2], 0x90) });
            }
            cv.solid.extend_from_slice(cv.glyph);
            return;
        }

        // The prompt: "> " and the line, wrapped; nothing while a block runs.
        let line = &self.editor.line;
        let line_cells = clusters(&line.text).count();
        let cursor_cells = clusters(&line.text[..line.cursor]).count();
        let prompt_rows =
            if self.running.is_some() { 0 } else { (2 + line_cells + 1).div_ceil(cols) + self.message.is_some() as usize };
        let total: usize = self.blocks.iter().map(Block::rows).sum::<usize>() + prompt_rows;
        let max_scroll = total.saturating_sub(rows);
        self.scroll = self.scroll.min(max_scroll);
        let top = total.saturating_sub(rows + self.scroll);

        // Blocks, from the top.
        let mut y = 0usize;
        for (bi, b) in self.blocks.iter_mut().enumerate() {
            let height = b.rows();
            if y + height <= top || y >= top + rows {
                y += height;
                continue;
            }
            if b.header_rows() == 1 && y >= top {
                let wy = y - top;
                cv.fill(0, wy, cols, color(HEADER_BG));
                let (mark, mark_color) = match b.finished {
                    None => ('…', DIM),
                    Some((0, _)) => ('●', OK),
                    Some(_) => ('●', FAILED),
                };
                cv.put(glyphs, 0, wy, mark, color(mark_color));
                for (i, c) in b.command.chars().filter(|c| *c != '\n').take(cols.saturating_sub(2)).enumerate() {
                    cv.put(glyphs, 2 + i, wy, c, color(COMMAND));
                }
                if let Some((code, ns)) = b.finished {
                    let status = if code == 0 { format!("{:.1} s", ns as f64 / 1e9) } else { format!("exit {code}  {:.1} s", ns as f64 / 1e9) };
                    let x = cols.saturating_sub(status.chars().count() + 1);
                    for (i, c) in status.chars().enumerate() {
                        cv.put(glyphs, x + i, wy, c, color(if code == 0 { DIM } else { FAILED }));
                    }
                }
            }
            let content_y = y + b.header_rows();
            let content = b.term.content_rows();
            let from = top.max(content_y) - content_y;
            let to = (top + rows).min(content_y + content).saturating_sub(content_y);
            if from < to {
                let off = b.term.scroll_to(from);
                let base = content_y + from;
                let cursor = b.term.visit(off, off + (to - from), |r, c, cell| {
                    let wy = base + (r - off) - top;
                    let (fg, bg) = if cell.inverse_default && cell.fg.is_none() && cell.bg.is_none() {
                        (color(BG), Some(color(FG)))
                    } else {
                        (cell_color(cell.fg, FG), cell.bg.map(|b| color(b)))
                    };
                    if let Some(bg) = bg {
                        cv.fill(c, wy, 1, bg);
                    }
                    cv.put(glyphs, c, wy, cell.ch, fg);
                });
                b.term.scroll_to_bottom();
                // The running program's cursor.
                if Some(bi) == self.running {
                    if let Some((x, vy)) = cursor {
                        let row = content_y + (from - off) + vy as usize;
                        if row >= top && row < top + rows {
                            cv.glyph.push(Quad {
                                col: x as u32,
                                row: (row - top) as u32,
                                width: 1,
                                height: 1,
                                glyph: SOLID,
                                color: rgba(FG[0], FG[1], FG[2], 0x90),
                            });
                        }
                    }
                }
            }
            y += height;
        }

        // The prompt and the line being edited.
        if self.running.is_none() {
            let py = total - prompt_rows;
            let at = |cell: usize| (cell % cols, py + cell / cols);
            let put_at = |cv: &mut Canvas, glyphs: &mut Glyphs, cell: usize, c: char, fg: u32| {
                let (x, row) = at(cell);
                if row >= top {
                    cv.put(glyphs, x, row - top, c, fg);
                }
            };
            put_at(&mut cv, glyphs, 0, '>', color(ACCENT));
            for (i, (s, _)) in clusters(&line.text).enumerate() {
                let c = line.text[s..].chars().next().unwrap_or(' ');
                put_at(&mut cv, glyphs, 2 + i, c, color(FG));
            }
            let (cx, cy) = at(2 + cursor_cells);
            if cy >= top && cy < top + rows {
                cv.glyph.push(Quad {
                    col: cx as u32,
                    row: (cy - top) as u32,
                    width: 1,
                    height: 1,
                    glyph: SOLID,
                    color: rgba(FG[0], FG[1], FG[2], 0x90),
                });
            }
            if let Some(m) = &self.message {
                let my = py + (2 + line_cells + 1).div_ceil(cols);
                if my >= top && my < top + rows {
                    for (i, c) in m.trim_end().chars().filter(|c| *c != '\n').take(cols).enumerate() {
                        cv.put(glyphs, i, my - top, c, color(DIM));
                    }
                }
            }
        }
        cv.solid.extend_from_slice(cv.glyph);
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

    fn fill(&mut self, col: usize, row: usize, width: usize, bg: u32) {
        if row < self.rows {
            self.solid.push(Quad { col: col as u32, row: row as u32, width: width as u32, height: 1, glyph: SOLID, color: bg });
        }
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
