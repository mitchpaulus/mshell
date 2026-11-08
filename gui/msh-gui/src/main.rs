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
//!   Alt-S            split: a new pane in this tab
//!   Alt-R            reorder: swap the pane with the first
//!   Alt-Space        next layout: everything, columns, right halves,
//!                    right thirds
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
use pane::{color, Canvas, Pane, ACCENT, BG, DIM};
use render::{Glyphs, Quad, Renderer};
use std::sync::mpsc::Sender;
use std::sync::Arc;
use std::time::Instant;
use winit::application::ApplicationHandler;
use winit::event::{ElementState, MouseScrollDelta, WindowEvent};
use winit::event_loop::{ActiveEventLoop, ControlFlow, EventLoop, EventLoopProxy};
use winit::keyboard::{Key, ModifiersState};
use winit::platform::modifier_supplement::KeyEventExtModifierSupplement;
use winit::window::{Window, WindowId};

enum UserEvent {
    /// An event from the engine of the pane with this id.
    Engine(u32, Event),
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
    font_size: f32,
    cols: usize,
    rows: usize,
    mods: ModifiersState,
    start: Instant,
    quads: Vec<Quad>,
    glyph_quads: Vec<Quad>,
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

    /// A pane with an engine of its own, which loads init.msh as it starts.
    fn new_pane(&mut self) -> Pane {
        let id = self.next_pane;
        self.next_pane += 1;
        let sink = self.sink.clone();
        let engine = Engine::spawn(&self.msh, move |e| sink.send(UserEvent::Engine(id, e)))
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

    /// The panes' area: the window, below the tab bar when there are tabs.
    fn area(&self) -> Rect {
        let bar = (self.tabs.len() > 1) as usize;
        Rect { x: 0, y: bar, w: self.cols, h: self.rows.saturating_sub(bar) }
    }

    fn rects(&self) -> Vec<Option<Rect>> {
        let tab = &self.tabs[self.tab];
        layout::rects(tab.layout, tab.panes.len(), tab.focus, self.area())
    }

    /// Gives each shown pane of the current tab its size.
    fn layout(&mut self) {
        let Some(glyphs) = &self.glyphs else { return };
        let cell = (glyphs.cell.w, glyphs.cell.h);
        if self.tabs.is_empty() {
            return;
        }
        let rects = self.rects();
        for (p, r) in self.tabs[self.tab].panes.iter_mut().zip(rects) {
            if let Some(r) = r {
                p.resize(r.w, r.h, cell);
            }
        }
    }

    fn resize(&mut self, width: u32, height: u32) {
        let Some(glyphs) = &self.glyphs else { return };
        self.cols = (width / glyphs.cell.w).max(1) as usize;
        self.rows = (height / glyphs.cell.h).max(1) as usize;
        if let Some(r) = &mut self.renderer {
            r.resize(width, height);
        }
        self.layout();
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
                let rects = layout::rects(tab.layout, n, tab.focus, self.area());
                self.tabs[self.tab].focus = layout::neighbor(&rects, self.tabs[self.tab].focus, dir);
            }
            's' => {
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
            ' ' => tab.layout = tab.layout.next(),
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
        let (Some(renderer), Some(glyphs)) = (&mut self.renderer, &mut self.glyphs) else { return };
        renderer.draw(glyphs, &self.quads, BG.map(|v| v as f64 / 255.0));
    }

    /// Lays out the frame as quads in `self.quads`: solid ones first, then
    /// glyphs.
    fn compose(&mut self) {
        self.quads.clear();
        self.glyph_quads.clear();
        if self.tabs.is_empty() {
            return;
        }
        let rects = self.rects();
        let Some(glyphs) = &mut self.glyphs else { return };
        // The tab bar, when there are tabs: each tab's title or number.
        if self.tabs.len() > 1 {
            let mut cv = Canvas { solid: &mut self.quads, glyph: &mut self.glyph_quads, x0: 0, y0: 0, cols: self.cols, rows: 1 };
            let mut x = 0;
            for (i, tab) in self.tabs.iter().enumerate() {
                let pane = &tab.panes[tab.focus];
                let title = pane.title.clone().unwrap_or_else(|| format!("{}", i + 1));
                let label = format!(" {}: {} ", i + 1, title);
                let w = label.chars().count();
                if i == self.tab {
                    cv.fill(x, 0, w, color([0x2c, 0x22, 0x36]));
                }
                cv.text(glyphs, x, 0, &label, color(if i == self.tab { ACCENT } else { DIM }));
                x += w + 1;
            }
            let layout = self.tabs[self.tab].layout.name();
            cv.text(glyphs, self.cols.saturating_sub(layout.len() + 1), 0, layout, color(DIM));
        }
        let tab = &mut self.tabs[self.tab];
        let focus = tab.focus;
        for (i, (pane, rect)) in tab.panes.iter_mut().zip(rects).enumerate() {
            let Some(r) = rect else { continue };
            let mut cv = Canvas { solid: &mut self.quads, glyph: &mut self.glyph_quads, x0: r.x, y0: r.y, cols: r.w, rows: r.h };
            pane.compose(&mut cv, glyphs, i == focus);
        }
        self.quads.extend_from_slice(&self.glyph_quads);
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
        let UserEvent::Engine(id, e) = event;
        if self.engine_event(id, e) {
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
            WindowEvent::MouseWheel { delta, .. } => {
                let cell_h = self.glyphs.as_ref().map_or(16.0, |g| g.cell.h as f32);
                let lines = match delta {
                    MouseScrollDelta::LineDelta(_, y) => y * 3.0,
                    MouseScrollDelta::PixelDelta(p) => p.y as f32 / cell_h,
                };
                if let Some(p) = self.focused() {
                    p.scroll = (p.scroll as i64 + lines.round() as i64).max(0) as usize;
                }
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
    let sink = match &event_loop {
        Some(el) => Sink::Window(el.create_proxy()),
        None => Sink::Script(tx),
    };
    let font_path = std::env::var("MSH_GUI_FONT").unwrap_or_else(|_| "/usr/share/fonts/noto/NotoSansMono-Regular.ttf".into());
    let font = std::fs::read(&font_path).unwrap_or_else(|e| panic!("reading font {font_path}: {e}"));
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
        font_size,
        cols: 80,
        rows: 24,
        mods: ModifiersState::empty(),
        start: Instant::now(),
        quads: Vec::new(),
        glyph_quads: Vec::new(),
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
