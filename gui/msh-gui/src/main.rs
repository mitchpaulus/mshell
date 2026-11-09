//! The mshell GUI (ai/gui-terminal.typ). Not a terminal emulator: each
//! command is a card with its output, which alone runs in a terminal
//! emulator of its own, and the prompt is a box of its own at the bottom of
//! its pane, edited locally by the Rust port of msh's editor.
//!
//! Tabs hold panes, laid out as a tiling window manager does; each pane has
//! its own engine (`msh server`), which runs its lines, renders its prompt,
//! completes, and answers what only it can.
//!
//! Keys of the window (before the editor's or a program's):
//!   Alt-Enter        new tab
//!   Alt-N / Alt-P    next / previous tab
//!   Alt-H/J/K/L      focus the pane left / below / above / right
//!   Alt-;            split: a new pane in this tab
//!   Alt-R            reorder: swap the pane with the first
//!   Alt-'            next layout: everything, columns, right halves,
//!                    right thirds
//! A click on a tab shows it, on a pane focuses it; the wheel scrolls the
//! pane under the pointer. In the file manager's dialog, presses act at
//! once (Pane::fm_press), the wheel scrolls the listing, and the Back
//! button goes to the parent directory.
//!
//! Usage: msh-gui [MSH]   (MSH: the msh binary; default $MSH_BIN, then the
//!                         repository's mshell/mshell, then msh on PATH)
//! MSH_GUI_FONT and MSH_GUI_FONT_SIZE choose the font.

mod keys;
mod layout;
mod pane;
mod render;
mod script;
mod vt;

use layout::{Direction, Layout, Rect};
use msh_client::{Engine, Event};
use msh_editor::keys::{KeyCode, MOD_ALT};
use pane::{color, Canvas, Pane, ACCENT, DIM};
use render::{Glyphs, Quad, Renderer};
use std::sync::mpsc::Sender;
use std::sync::Arc;
use std::time::Instant;
use winit::application::ApplicationHandler;
use winit::event::{ElementState, MouseButton, MouseScrollDelta, WindowEvent};
use winit::event_loop::{ActiveEventLoop, ControlFlow, EventLoop, EventLoopProxy};
use winit::keyboard::{Key, ModifiersState};
use winit::platform::modifier_supplement::KeyEventExtModifierSupplement;
use winit::window::{Window, WindowId};

// Gaps around and between panes, as a tiling window manager's, and the
// focused pane's border, in pixels.
const GAP: usize = 8;
const BORDER: u32 = 2;
/// Behind the panes: what shows in the gaps.
pub const BG: [u8; 3] = [0x0b, 0x0b, 0x0e];
const PANE_BG: [u8; 3] = pane::BG;
const TAB_SELECTED_BG: [u8; 3] = [0x2c, 0x22, 0x36];
const TAB_PAD: f32 = 10.0;

enum UserEvent {
    /// An engine has events waiting (Engine::next_event).
    Wake,
}

/// Where engines' events go: the window's event loop, or the script's
/// channel.
#[derive(Clone)]
enum Sink {
    Window(EventLoopProxy<UserEvent>),
    Script(Sender<UserEvent>),
}

impl Sink {
    fn send(&self, e: UserEvent) {
        match self {
            Sink::Window(p) => {
                let _ = p.send_event(e);
            }
            Sink::Script(s) => {
                let _ = s.send(e);
            }
        }
    }
}

struct Tab {
    panes: Vec<Pane>,
    focus: usize,
    layout: Layout,
}

struct App {
    msh: String,
    sink: Sink,
    next_pane: u32,
    tabs: Vec<Tab>,
    tab: usize,
    window: Option<Arc<Window>>,
    renderer: Option<Renderer>,
    glyphs: Option<Glyphs>,
    font: Vec<u8>,
    /// The font of the app's own text: proportional.
    ui_font: Vec<u8>,
    font_size: f32,
    /// The window's size in pixels.
    width: usize,
    height: usize,
    /// The pointer, in pixels.
    pointer: (f64, f64),
    mods: ModifiersState,
    start: Instant,
    quads: Vec<Quad>,
    glyph_quads: Vec<Quad>,
    top_quads: Vec<Quad>,
    /// Engines' events were left waiting when the last drain's time ran
    /// out: the next frame drains more, once it is drawn.
    backlog: bool,
}

/// How long a drain handles engines' events before the frame is drawn: a
/// flood of output (cat /dev/random) shows as it comes, and keys still
/// reach the program.
const DRAIN_BUDGET: std::time::Duration = std::time::Duration::from_millis(10);

impl App {
    fn now(&self) -> i64 {
        self.start.elapsed().as_nanos() as i64
    }

    fn redraw(&self) {
        if let Some(w) = &self.window {
            w.request_redraw();
        }
    }

    /// A pane with an engine of its own, which loads init.msh as it starts.
    fn new_pane(&mut self) -> Pane {
        let id = self.next_pane;
        self.next_pane += 1;
        let sink = self.sink.clone();
        // Programs see a terminal like xterm, with 24-bit colour; started
        // from a desktop menu, the GUI may have no TERM of its own.
        let mut cmd = std::process::Command::new(&self.msh);
        cmd.arg("server").env("TERM", "xterm-256color").env("COLORTERM", "truecolor").env("TERM_PROGRAM", "msh-gui");
        let engine = Engine::spawn_command(cmd, move || sink.send(UserEvent::Wake))
            .unwrap_or_else(|e| panic!("starting {} server: {e}", self.msh));
        Pane::new(id, engine)
    }

    fn new_tab(&mut self) {
        let pane = self.new_pane();
        self.tabs.push(Tab { panes: vec![pane], focus: 0, layout: Layout::Everything });
        self.tab = self.tabs.len() - 1;
        self.layout();
    }

    fn focused_pane(&self) -> Option<&Pane> {
        let tab = self.tabs.get(self.tab)?;
        tab.panes.get(tab.focus)
    }

    fn focused(&mut self) -> Option<&mut Pane> {
        let tab = self.tabs.get_mut(self.tab)?;
        tab.panes.get_mut(tab.focus)
    }

    fn cell(&self) -> (u32, u32) {
        self.glyphs.as_ref().map_or((8, 16), |g| (g.cell.w, g.cell.h))
    }

    /// The tab bar's height in pixels: a row, when there are tabs.
    fn bar_height(&self) -> usize {
        if self.tabs.len() > 1 { self.cell().1 as usize } else { 0 }
    }

    /// The panes' area in pixels: the window, inside the outer gap, below
    /// the tab bar.
    fn area(&self) -> Rect {
        let top = self.bar_height();
        Rect { x: GAP, y: top + GAP, w: self.width.saturating_sub(2 * GAP), h: self.height.saturating_sub(top + 2 * GAP) }
    }

    fn rects(&self) -> Vec<Option<Rect>> {
        let tab = &self.tabs[self.tab];
        layout::rects(tab.layout, tab.panes.len(), tab.focus, self.area(), GAP)
    }

    /// Gives each shown pane of the current tab its size in cells.
    fn layout(&mut self) {
        if self.glyphs.is_none() || self.tabs.is_empty() {
            return;
        }
        let cell = self.cell();
        let rects = self.rects();
        for (p, r) in self.tabs[self.tab].panes.iter_mut().zip(rects) {
            if let Some(r) = r {
                p.resize((r.w / cell.0 as usize).max(1), (r.h / cell.1 as usize).max(1), cell);
            }
        }
    }

    fn resize(&mut self, width: u32, height: u32) {
        (self.width, self.height) = (width as usize, height as usize);
        if let Some(r) = &mut self.renderer {
            r.resize(width, height);
        }
        self.layout();
    }

    /// Each tab's label in the tab bar, with where it starts and its width,
    /// in pixels.
    fn tab_labels(&mut self) -> Vec<(f32, f32, String)> {
        let mut x = 0.0;
        let labels: Vec<String> = self
            .tabs
            .iter()
            .enumerate()
            .map(|(i, tab)| format!("{}  {}", i + 1, tab.panes[tab.focus].title.clone().unwrap_or_default()).trim().to_string())
            .collect();
        let Some(glyphs) = &mut self.glyphs else { return Vec::new() };
        labels
            .into_iter()
            .map(|label| {
                let w = glyphs.ui_width(&label) + 2.0 * TAB_PAD;
                let at = x;
                x += w + 2.0;
                (at, w, label)
            })
            .collect()
    }

    /// A click: on a tab, shows it; on a pane, focuses it, and in its file
    /// manager's dialog acts on what it is on. `now` in ns.
    fn click(&mut self, now: i64) {
        let (px, py) = (self.pointer.0 as usize, self.pointer.1 as usize);
        if py < self.bar_height() {
            let x = px as f32;
            if let Some(i) = self.tab_labels().iter().position(|(at, w, _)| x >= *at && x < at + w) {
                self.tab = i;
                self.layout();
            }
            return;
        }
        if let Some(i) = self.pane_at(px, py) {
            self.tabs[self.tab].focus = i;
            self.layout();
            let (x, y) = self.in_pane(i);
            let ctrl = self.mods.control_key();
            let pane = &mut self.tabs[self.tab].panes[i];
            if pane.fm.is_some() {
                pane.fm_press(x, y, now, ctrl);
            }
        }
    }

    /// The Back button: in a file manager's dialog, the parent directory.
    fn back(&mut self) {
        let (px, py) = (self.pointer.0 as usize, self.pointer.1 as usize);
        if let Some(i) = self.pane_at(px, py) {
            let pane = &mut self.tabs[self.tab].panes[i];
            if pane.fm.is_some() {
                let _ = pane.engine.fm_mouse(msh_client::FmMouse::Parent, 0, "", "");
            }
        }
    }

    /// The wheel over the pane under the pointer: it scrolls back through
    /// the blocks, or its file manager's listing. Positive is up.
    fn wheel(&mut self, delta: MouseScrollDelta) {
        let (px, py) = (self.pointer.0 as usize, self.pointer.1 as usize);
        let Some(i) = self.pane_at(px, py) else { return };
        let cell_h = self.cell().1 as f32;
        let p = &mut self.tabs[self.tab].panes[i];
        if p.fm.is_some() {
            let rows = match delta {
                MouseScrollDelta::LineDelta(_, y) => y * 3.0,
                MouseScrollDelta::PixelDelta(d) => d.y as f32 / p.fm_row(),
            };
            p.fm_wheel(-rows);
            return;
        }
        let lines = match delta {
            MouseScrollDelta::LineDelta(_, y) => y * 3.0,
            MouseScrollDelta::PixelDelta(d) => d.y as f32 / cell_h,
        };
        p.scroll = (p.scroll as i64 + lines.round() as i64).max(0) as usize;
    }

    /// The pointer in the pixels of a pane of the tab shown.
    fn in_pane(&self, i: usize) -> (f32, f32) {
        let r = self.rects()[i].expect("a pane shown");
        ((self.pointer.0 - r.x as f64) as f32, (self.pointer.1 - r.y as f64) as f32)
    }

    /// The shown pane under a pixel.
    fn pane_at(&self, x: usize, y: usize) -> Option<usize> {
        if self.tabs.is_empty() {
            return None;
        }
        self.rects().iter().position(|r| r.is_some_and(|r| x >= r.x && x < r.x + r.w && y >= r.y && y < r.y + r.h))
    }

    /// Handles the engines' waiting events, each pane's in order, for up to
    /// `budget`; sets `backlog` when some are left. Returns whether the app
    /// ends.
    fn drain(&mut self, budget: std::time::Duration) -> bool {
        let start = Instant::now();
        self.backlog = false;
        let ids: Vec<u32> = self.tabs.iter().flat_map(|t| t.panes.iter().map(|p| p.id)).collect();
        for id in ids {
            loop {
                if start.elapsed() >= budget {
                    // The frame drawn next drains more.
                    self.backlog = true;
                    self.redraw();
                    return false;
                }
                let Some(pane) = self.tabs.iter_mut().flat_map(|t| t.panes.iter_mut()).find(|p| p.id == id) else { break };
                let Some(event) = pane.engine.next_event() else { break };
                if self.engine_event(id, event) {
                    return true;
                }
            }
        }
        false
    }

    /// An engine's event for its pane; returns whether the app ends.
    fn engine_event(&mut self, id: u32, event: Event) -> bool {
        let Some((t, p)) = self
            .tabs
            .iter()
            .enumerate()
            .find_map(|(t, tab)| tab.panes.iter().position(|p| p.id == id).map(|p| (t, p)))
        else {
            return false;
        };
        if self.tabs[t].panes[p].engine_event(event) {
            return self.close(t, p);
        }
        self.redraw();
        false
    }

    /// Closes a pane, and its tab when it was the last; returns whether no
    /// tab is left.
    fn close(&mut self, t: usize, p: usize) -> bool {
        let tab = &mut self.tabs[t];
        drop(tab.panes.remove(p));
        if tab.panes.is_empty() {
            self.tabs.remove(t);
            if self.tabs.is_empty() {
                return true;
            }
            self.tab = self.tab.min(self.tabs.len() - 1);
        } else {
            tab.focus = tab.focus.min(tab.panes.len() - 1);
        }
        self.layout();
        self.redraw();
        false
    }

    /// The window's own keys; returns whether `code` was one.
    fn window_key(&mut self, code: KeyCode) -> bool {
        if code.mods() != MOD_ALT {
            return false;
        }
        let tab = &mut self.tabs[self.tab];
        if code.base() == KeyCode::ENTER.base() {
            self.new_tab();
            return true;
        }
        let Some(c) = char::from_u32(code.base()) else { return false };
        let n = tab.panes.len();
        match c {
            'n' => self.tab = (self.tab + 1) % self.tabs.len(),
            'p' => self.tab = (self.tab + self.tabs.len() - 1) % self.tabs.len(),
            'h' | 'j' | 'k' | 'l' => {
                let dir = match c {
                    'h' => Direction::Left,
                    'j' => Direction::Down,
                    'k' => Direction::Up,
                    _ => Direction::Right,
                };
                let rects = layout::rects(tab.layout, n, tab.focus, self.area(), GAP);
                self.tabs[self.tab].focus = layout::neighbor(&rects, self.tabs[self.tab].focus, dir);
            }
            ';' => {
                let pane = self.new_pane();
                let tab = &mut self.tabs[self.tab];
                // A split shows: from everything, the columns.
                if tab.layout == Layout::Everything {
                    tab.layout = Layout::Columns;
                }
                tab.panes.insert(tab.focus + 1, pane);
                tab.focus += 1;
            }
            'r' if n > 1 => {
                // The focused pane takes the first place; the first, from
                // there, takes the second.
                let other = if tab.focus == 0 { 1 } else { 0 };
                tab.panes.swap(tab.focus, other);
                tab.focus = other;
            }
            '\'' => tab.layout = tab.layout.next(),
            _ => return false,
        }
        self.layout();
        true
    }

    /// A key as the editor names it: the window's, or the focused pane's
    /// editor's (programs get keys as winit events, in key_event). Returns
    /// whether the app ends.
    fn key(&mut self, code: KeyCode, now: i64) -> bool {
        if self.window_key(code) {
            return false;
        }
        let (t, p) = (self.tab, self.tabs[self.tab].focus);
        let pane = &mut self.tabs[t].panes[p];
        if pane.fm.is_some() {
            let _ = pane.engine.fm_key(code, None);
            return false;
        }
        if pane.running.is_some() {
            return false;
        }
        if pane.editor_key(code, now) {
            return self.close(t, p);
        }
        false
    }

    fn key_event(&mut self, event: &winit::event::KeyEvent) -> bool {
        let code = keys::editor_key(event, self.mods);
        // The file manager's dialog takes keys, after the window's.
        if self.focused().is_some_and(|p| p.fm.is_some()) {
            if let Some(c) = code {
                if !self.window_key(c) {
                    if let Some(p) = self.focused() {
                        let _ = p.engine.fm_key(c, None);
                    }
                }
            }
            return false;
        }
        let running = self.focused().is_some_and(|p| p.running.is_some());
        if running {
            if code.is_some_and(|c| self.window_key(c)) {
                return false;
            }
            let text = event.text.as_ref().map_or("", |t| t.as_str());
            let unshifted = match event.key_without_modifiers() {
                Key::Character(s) => s.chars().next(),
                _ => None,
            };
            let (key, mods) = (keys::ghostty_key(event), keys::ghostty_mods(self.mods));
            if let Some(p) = self.focused() {
                p.program_key(key, mods, text, unshifted);
            }
            return false;
        }
        match code {
            Some(c) => {
                let now = self.now();
                self.key(c, now)
            }
            None => false,
        }
    }

    fn frame(&mut self) {
        self.compose();
        // The atlas filled and was cleared as the frame was laid out: lay it
        // out again, with fresh regions.
        if self.glyphs.as_ref().is_some_and(|g| g.reset) {
            self.glyphs.as_mut().unwrap().reset = false;
            self.compose();
        }
        let (Some(renderer), Some(glyphs)) = (&mut self.renderer, &mut self.glyphs) else { return };
        renderer.draw(glyphs, &self.quads, BG.map(|v| v as f64 / 255.0));
    }

    /// Lays out the frame as quads in `self.quads`: solid ones first, then
    /// glyphs.
    fn compose(&mut self) {
        self.quads.clear();
        self.glyph_quads.clear();
        self.top_quads.clear();
        if self.tabs.is_empty() || self.glyphs.is_none() {
            return;
        }
        let rects = self.rects();
        let labels = self.tab_labels();
        let cell = self.cell();
        let cols = (self.width / cell.0 as usize).max(1);
        let Some(glyphs) = &mut self.glyphs else { return };
        // The tab bar, when there are tabs: each tab's title or number.
        if self.tabs.len() > 1 {
            let mut cv = Canvas { solid: &mut self.quads, glyph: &mut self.glyph_quads, top: &mut self.top_quads, on_top: false, x0: 0, y0: 0, cols, rows: 1, cell };
            let h = cell.1 as f32;
            for (i, (x, w, label)) in labels.iter().enumerate() {
                if i == self.tab {
                    cv.fill_px(*x, 0.0, *w, h, color(TAB_SELECTED_BG));
                }
                cv.ui_text(glyphs, x + TAB_PAD, 0.0, h, label, color(if i == self.tab { ACCENT } else { DIM }), *w);
            }
            let layout = self.tabs[self.tab].layout.name();
            let lw = glyphs.ui_width(layout);
            cv.ui_text(glyphs, self.width as f32 - lw - TAB_PAD, 0.0, h, layout, color(DIM), lw + 1.0);
        }
        let focus = self.tabs[self.tab].focus;
        let shown = tab_count_shown(&self.tabs[self.tab]);
        for (i, rect) in rects.into_iter().enumerate() {
            let Some(r) = rect else { continue };
            let (x, y, w, h) = (r.x as u32, r.y as u32, r.w as u32, r.h as u32);
            // The pane's own background, and the focused one's border in
            // the gap around it.
            if i == focus && shown > 1 {
                let b = BORDER;
                for (bx, by, bw, bh) in [(x - b, y - b, w + 2 * b, b), (x - b, y + h, w + 2 * b, b), (x - b, y, b, h), (x + w, y, b, h)] {
                    self.quads.push(Quad::solid(bx, by, bw, bh, color(ACCENT)));
                }
            }
            self.quads.push(Quad::solid(x, y, w, h, color(PANE_BG)));
            let pane = &mut self.tabs[self.tab].panes[i];
            let mut cv = Canvas { solid: &mut self.quads, glyph: &mut self.glyph_quads, top: &mut self.top_quads, on_top: false, x0: x, y0: y, cols: pane.cols, rows: pane.rows, cell };
            pane.compose(&mut cv, glyphs, i == focus);
        }
        self.quads.extend_from_slice(&self.glyph_quads);
        self.quads.extend_from_slice(&self.top_quads);
    }
}

/// The panes a tab shows: its layout's, at most all of them.
fn tab_count_shown(tab: &Tab) -> usize {
    if tab.layout == Layout::Everything { 1 } else { tab.panes.len() }
}

impl ApplicationHandler<UserEvent> for App {
    fn resumed(&mut self, el: &ActiveEventLoop) {
        if self.window.is_some() {
            return;
        }
        let attrs = Window::default_attributes().with_title("msh").with_inner_size(winit::dpi::LogicalSize::new(960.0, 640.0));
        let window = Arc::new(el.create_window(attrs).expect("create_window"));
        let glyphs = Glyphs::new(std::mem::take(&mut self.font), std::mem::take(&mut self.ui_font), self.font_size * window.scale_factor() as f32);
        let renderer = Renderer::new(window.clone(), &glyphs);
        self.glyphs = Some(glyphs);
        self.renderer = Some(renderer);
        let size = window.inner_size();
        self.window = Some(window);
        self.resize(size.width, size.height);
        self.redraw();
    }

    fn user_event(&mut self, el: &ActiveEventLoop, event: UserEvent) {
        let UserEvent::Wake = event;
        if self.drain(DRAIN_BUDGET) {
            el.exit();
        }
        let title = self.focused().and_then(|p| p.title.clone());
        if let (Some(t), Some(w)) = (title, &self.window) {
            w.set_title(&t);
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
                if self.key_event(&event) {
                    el.exit();
                }
                self.redraw();
            }
            WindowEvent::CursorMoved { position, .. } => self.pointer = (position.x, position.y),
            WindowEvent::MouseInput { state: ElementState::Pressed, button: MouseButton::Left, .. } => {
                let now = self.now();
                self.click(now);
                self.redraw();
            }
            WindowEvent::MouseInput { state: ElementState::Pressed, button: MouseButton::Back, .. } => {
                self.back();
                self.redraw();
            }
            WindowEvent::MouseWheel { delta, .. } => {
                self.wheel(delta);
                self.redraw();
            }
            WindowEvent::RedrawRequested => {
                self.frame();
                // Output left waiting: more of it, then the next frame.
                if self.backlog && self.drain(DRAIN_BUDGET) {
                    el.exit();
                }
            }
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
    let sink = match &event_loop {
        Some(el) => Sink::Window(el.create_proxy()),
        None => Sink::Script(tx),
    };
    let font_path = std::env::var("MSH_GUI_FONT").unwrap_or_else(|_| "/usr/share/fonts/noto/NotoSansMono-Regular.ttf".into());
    let font = std::fs::read(&font_path).unwrap_or_else(|e| panic!("reading font {font_path}: {e}"));
    let ui_font_path = std::env::var("MSH_GUI_UI_FONT").unwrap_or_else(|_| "/usr/share/fonts/noto/NotoSans-Regular.ttf".into());
    let ui_font = std::fs::read(&ui_font_path).unwrap_or_else(|e| panic!("reading font {ui_font_path}: {e}"));
    let font_size = std::env::var("MSH_GUI_FONT_SIZE").ok().and_then(|s| s.parse().ok()).unwrap_or(15.0);
    let mut app = App {
        msh,
        sink,
        next_pane: 1,
        tabs: Vec::new(),
        tab: 0,
        window: None,
        renderer: None,
        glyphs: None,
        font,
        ui_font,
        font_size,
        width: 800,
        height: 600,
        pointer: (0.0, 0.0),
        mods: ModifiersState::empty(),
        start: Instant::now(),
        quads: Vec::new(),
        glyph_quads: Vec::new(),
        top_quads: Vec::new(),
        backlog: false,
    };
    // The first pane's engine starts first: it loads init.msh while the
    // window opens.
    app.new_tab();
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
