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
//!   Alt-'            next layout: everything, stack, columns, right
//!                    halves, right thirds
//! The bar at the top shows the tabs, and the layout while the tab has
//! more than one pane.
//! A click on a tab shows it, on a pane focuses it; the wheel scrolls the
//! pane under the pointer. In the file manager's dialog, presses act at
//! once (Pane::fm_press), the wheel scrolls the listing, and the Back
//! button goes to the parent directory.
//!
//! Usage: msh-gui [MSH]   (MSH: the msh binary; default $MSH_BIN, then the
//!                         repository's mshell/mshell (mshell/msh.exe on
//!                         Windows), then msh on PATH)
//! On Windows msh server needs Microsoft's ConPTY: MSH_CONPTY, else the
//! conpty.dll beside msh-gui.exe (with OpenConsole.exe), else the copy
//! msh-gui/fetch-conpty.msh unpacks in the repository (conpty_dll).
//! MSH_GUI_FONT and MSH_GUI_FONT_SIZE choose the monospace font (program
//! output), MSH_GUI_UI_FONT the app's own, and MSH_GUI_PROMPT_FONT the
//! prompt box's (else the UI font); {VAR}_BOLD, _ITALIC and _BOLD_ITALIC
//! their styles (else the files beside them).

mod flow;
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
use render::{Family, Glyphs, Quad, Renderer};
use std::sync::mpsc::Sender;
use std::sync::Arc;
use std::time::{Duration, Instant};
use winit::application::ApplicationHandler;
use winit::event::{ElementState, MouseButton, MouseScrollDelta, StartCause, WindowEvent};
use winit::event_loop::{ActiveEventLoop, ControlFlow, EventLoop, EventLoopProxy};
use winit::keyboard::{Key, ModifiersState};
use winit::platform::modifier_supplement::KeyEventExtModifierSupplement;
use winit::window::{UserAttentionType, Window, WindowId};

// Gaps around and between panes, as a tiling window manager's, and the
// focused pane's border, in pixels.
const GAP: usize = 8;
const BORDER: u32 = 2;
/// The smallest a split may leave a pane, in cells: room for the prompt box
/// and a few lines of output.
const MIN_PANE: (usize, usize) = (20, 8);
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
    /// The font families (UI, monospace, the prompt's), until the window
    /// (or the script) takes them.
    fonts: Option<(Family, Family, Option<Family>)>,
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
    /// Blinking counts from here: a key starts it again, so the cursor
    /// shows while you type.
    blink_epoch: Instant,
    /// The script's phase of blinking, fixed; the window's follows the
    /// clock.
    blink_fixed: Option<bool>,
    window_focused: bool,
    /// When to draw again though nothing comes (the next phase of
    /// blinking, a bell's flash ending, a hold running out).
    wake_at: Option<Instant>,
}

/// Each phase of blinking, shown or hidden.
const BLINK: Duration = Duration::from_millis(600);

/// How long a drain handles engines' events before the frame is drawn: a
/// flood of output (cat /dev/random) shows as it comes, and keys still
/// reach the program.
const DRAIN_BUDGET: std::time::Duration = std::time::Duration::from_millis(10);

impl App {
    fn now(&self) -> i64 {
        self.start.elapsed().as_nanos() as i64
    }

    /// Whether blinking things show now.
    fn blink_on(&self) -> bool {
        match self.blink_fixed {
            Some(on) => on,
            None => !self.window_focused || (self.blink_epoch.elapsed().as_millis() / BLINK.as_millis()) % 2 == 0,
        }
    }

    /// When the phase of blinking next changes.
    fn next_blink(&self) -> Instant {
        let phases = self.blink_epoch.elapsed().as_millis() / BLINK.as_millis() + 1;
        self.blink_epoch + BLINK * phases as u32
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
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            // msh.exe is a console program: without this Windows opens a
            // console window for it. The engine moves to its pseudoconsole.
            const CREATE_NO_WINDOW: u32 = 0x0800_0000;
            cmd.creation_flags(CREATE_NO_WINDOW);
            if let Some(dll) = conpty_dll() {
                cmd.env("MSH_CONPTY", dll);
            }
        }
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

    /// Whether the bar at the top shows: for the tabs, or for the layout
    /// of a tab with more than one pane.
    fn bar_shown(&self) -> bool {
        self.tabs.len() > 1 || self.tabs.get(self.tab).is_some_and(|t| t.panes.len() > 1)
    }

    /// The bar's height in pixels: a row, when it shows.
    fn bar_height(&self) -> usize {
        if self.bar_shown() { self.cell().1 as usize } else { 0 }
    }

    /// The panes' area in pixels: the window, inside the outer gap, below
    /// the tab bar.
    fn area(&self) -> Rect {
        self.area_with_bar(self.bar_shown())
    }

    fn area_with_bar(&self, bar: bool) -> Rect {
        let top = if bar { self.cell().1 as usize } else { 0 };
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
                p.resize((r.w / cell.0 as usize).max(1), (r.h / cell.1 as usize).max(1), cell, (r.w as u32, r.h as u32));
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
        let pane = &mut self.tabs[t].panes[p];
        if pane.engine_event(event) {
            return self.close(t, p);
        }
        // A bell while the window hasn't the keyboard: the desktop is
        // asked to draw attention to it.
        if std::mem::take(&mut pane.bell_unheard) && !self.window_focused {
            if let Some(w) = &self.window {
                w.request_user_attention(Some(UserAttentionType::Informational));
            }
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
                // A split shows: from everything, the stack. One that would
                // leave a pane too small to use is refused.
                let (layout, focus) = (if tab.layout == Layout::Everything { Layout::Stack } else { tab.layout }, tab.focus);
                let cell = self.cell();
                let rects = layout::rects(layout, n + 1, focus + 1, self.area_with_bar(true), GAP);
                let small = |r: &Rect| r.w / (cell.0 as usize) < MIN_PANE.0 || r.h / (cell.1 as usize) < MIN_PANE.1;
                if rects.iter().flatten().any(small) {
                    self.tabs[self.tab].panes[focus].message = Some("No room for another pane".into());
                    return true;
                }
                let pane = self.new_pane();
                let tab = &mut self.tabs[self.tab];
                tab.layout = layout;
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
        let bar = self.bar_height() > 0;
        let (blink_on, window_focused) = (self.blink_on(), self.window_focused);
        let mut blinks = false;
        let Some(glyphs) = &mut self.glyphs else { return };
        // The bar: each tab's title or number, when there are tabs; and
        // the layout, when the tab has more than one pane.
        if bar {
            let mut cv = Canvas { solid: &mut self.quads, glyph: &mut self.glyph_quads, top: &mut self.top_quads, on_top: false, blink_on, window_focused, blinks: false, x0: 0, y0: 0, cols, rows: 1, cell };
            let h = cell.1 as f32;
            if self.tabs.len() > 1 {
                for (i, (x, w, label)) in labels.iter().enumerate() {
                    if i == self.tab {
                        cv.fill_px(*x, 0.0, *w, h, color(TAB_SELECTED_BG));
                    }
                    cv.ui_text(glyphs, x + TAB_PAD, 0.0, h, label, color(if i == self.tab { ACCENT } else { DIM }), *w);
                }
            }
            let tab = &self.tabs[self.tab];
            if tab.panes.len() > 1 {
                // Its name (in everything, which pane of how many), after a
                // map of it: the panes shown, the focused one in the
                // accent colour.
                let name = match tab.layout {
                    Layout::Everything => format!("everything {}/{}", tab.focus + 1, tab.panes.len()),
                    l => l.name().to_string(),
                };
                let lw = glyphs.ui_width(&name);
                let x = self.width as f32 - lw - TAB_PAD;
                cv.ui_text(glyphs, x, 0.0, h, &name, color(DIM), lw + 1.0);
                let mh = (h * 0.6).round() as usize;
                let mw = mh * 3 / 2;
                let (mx, my) = (x as usize - 8 - mw, ((h as usize) - mh) / 2);
                let map = Rect { x: mx, y: my, w: mw, h: mh };
                for (i, r) in layout::rects(tab.layout, tab.panes.len(), tab.focus, map, 1).into_iter().enumerate() {
                    if let Some(r) = r {
                        let c = if i == tab.focus { ACCENT } else { DIM };
                        cv.fill_px(r.x as f32, r.y as f32, r.w as f32, r.h as f32, color(c));
                    }
                }
            }
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
            let mut cv = Canvas { solid: &mut self.quads, glyph: &mut self.glyph_quads, top: &mut self.top_quads, on_top: false, blink_on, window_focused, blinks: false, x0: x, y0: y, cols: pane.cols, rows: pane.rows, cell };
            pane.compose(&mut cv, glyphs, i == focus);
            blinks |= cv.blinks;
            // The bell: the pane flashes.
            if pane.flashing() {
                self.top_quads.push(Quad::solid(x, y, w, h, pane::color_alpha(ACCENT, 0x30)));
            }
        }
        self.quads.extend_from_slice(&self.glyph_quads);
        self.quads.extend_from_slice(&self.top_quads);
        // When to draw again by itself: the next phase of what blinks
        // (only while the window has the keyboard), a flash ending, a hold
        // running out.
        let blink = (blinks && self.window_focused && self.blink_fixed.is_none()).then(|| self.next_blink());
        let panes = self.tabs[self.tab].panes.iter().filter_map(|p| p.wake_at());
        self.wake_at = blink.into_iter().chain(panes).min();
    }
}

/// How many panes a tab shows: its layout's, at most all of them.
fn tab_count_shown(tab: &Tab) -> usize {
    match tab.layout {
        Layout::Everything => 1,
        Layout::RightHalves => tab.panes.len().min(3),
        Layout::RightThirds => tab.panes.len().min(4),
        Layout::Stack | Layout::Columns => tab.panes.len(),
    }
}

impl ApplicationHandler<UserEvent> for App {
    fn resumed(&mut self, el: &ActiveEventLoop) {
        if self.window.is_some() {
            return;
        }
        let attrs = Window::default_attributes().with_title("msh").with_inner_size(winit::dpi::LogicalSize::new(960.0, 640.0));
        let window = Arc::new(el.create_window(attrs).expect("create_window"));
        let (ui, mono, prompt) = self.fonts.take().expect("fonts");
        let glyphs = Glyphs::new(ui, mono, prompt, self.font_size * window.scale_factor() as f32);
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

    /// Woken at wake_at: the frame is drawn again.
    fn new_events(&mut self, _: &ActiveEventLoop, cause: StartCause) {
        if let StartCause::ResumeTimeReached { .. } = cause {
            self.redraw();
        }
    }

    fn about_to_wait(&mut self, el: &ActiveEventLoop) {
        el.set_control_flow(match self.wake_at {
            Some(t) => ControlFlow::WaitUntil(t),
            None => ControlFlow::Wait,
        });
    }

    fn window_event(&mut self, el: &ActiveEventLoop, _: WindowId, event: WindowEvent) {
        match event {
            WindowEvent::CloseRequested => el.exit(),
            WindowEvent::Resized(size) => {
                self.resize(size.width, size.height);
                self.redraw();
            }
            WindowEvent::ModifiersChanged(m) => self.mods = m.state(),
            WindowEvent::Focused(focused) => {
                self.window_focused = focused;
                self.blink_epoch = Instant::now();
                self.redraw();
            }
            WindowEvent::KeyboardInput { event, .. } if event.state == ElementState::Pressed => {
                self.blink_epoch = Instant::now();
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

enum FontKind {
    Mono,
    Ui,
}

/// A platform's default font: the regular file, and its bold, italic and
/// bold italic when they aren't named as Noto's are (beside it, -Regular
/// swapped for -Bold and so on).
struct DefaultFont {
    regular: String,
    styles: Option<[String; 3]>,
}

/// Noto Sans Mono and Noto Sans; on Windows Consolas and Segoe UI, which
/// every Windows has, with real bold and italic files.
fn default_font(kind: FontKind) -> DefaultFont {
    if cfg!(windows) {
        let fonts = std::path::Path::new(&std::env::var("WINDIR").unwrap_or_else(|_| r"C:\Windows".into())).join("Fonts");
        let file = |name: &str| fonts.join(name).to_string_lossy().into_owned();
        let [regular, bold, italic, bold_italic] = match kind {
            FontKind::Mono => ["consola.ttf", "consolab.ttf", "consolai.ttf", "consolaz.ttf"],
            FontKind::Ui => ["segoeui.ttf", "segoeuib.ttf", "segoeuii.ttf", "segoeuiz.ttf"],
        };
        return DefaultFont { regular: file(regular), styles: Some([file(bold), file(italic), file(bold_italic)]) };
    }
    let regular = match kind {
        FontKind::Mono => "/usr/share/fonts/noto/NotoSansMono-Regular.ttf",
        FontKind::Ui => "/usr/share/fonts/noto/NotoSans-Regular.ttf",
    };
    DefaultFont { regular: regular.into(), styles: None }
}

fn msh_binary(args: &[String]) -> String {
    if let Some(a) = args.iter().find(|a| !a.starts_with("--")) {
        return a.clone();
    }
    if let Ok(m) = std::env::var("MSH_BIN") {
        return m;
    }
    let repo = if cfg!(windows) {
        concat!(env!("CARGO_MANIFEST_DIR"), "/../../mshell/msh.exe")
    } else {
        concat!(env!("CARGO_MANIFEST_DIR"), "/../../mshell/mshell")
    };
    if std::path::Path::new(repo).exists() {
        return repo.to_string();
    }
    "msh".to_string()
}

/// The ConPTY package's conpty.dll for msh server on Windows: MSH_CONPTY if
/// set, else beside msh-gui.exe, as it ships, else the copy
/// msh-gui/fetch-conpty.msh unpacks in the repository. None leaves msh server
/// to say what it needs.
#[cfg(windows)]
fn conpty_dll() -> Option<std::path::PathBuf> {
    if let Some(p) = std::env::var_os("MSH_CONPTY") {
        return Some(p.into());
    }
    let beside = std::env::current_exe().ok()?.with_file_name("conpty.dll");
    if beside.exists() {
        return Some(beside);
    }
    let arch = if cfg!(target_arch = "aarch64") { "arm64" } else { "x64" };
    let repo = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../third_party/conpty-1.25.260930003")
        .join(arch)
        .join("conpty.dll");
    repo.exists().then_some(repo)
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
    // The fonts: each a family, its bold and italic beside it or named by
    // {VAR}_BOLD, _ITALIC, _BOLD_ITALIC. The prompt box's is the UI font
    // unless MSH_GUI_PROMPT_FONT names one (a monospace one lays it out
    // as cells would).
    let family = |var: &str, default: Option<DefaultFont>| {
        let loaded = match (std::env::var(var), default) {
            (Ok(path), _) => Family::load(&path, var, None),
            (Err(_), Some(d)) => Family::load(&d.regular, var, d.styles),
            (Err(_), None) => Family::load("", var, None),
        };
        loaded.unwrap_or_else(|e| panic!("{e}"))
    };
    let mono = family("MSH_GUI_FONT", Some(default_font(FontKind::Mono)));
    let ui = family("MSH_GUI_UI_FONT", Some(default_font(FontKind::Ui)));
    let prompt = std::env::var("MSH_GUI_PROMPT_FONT").ok().map(|_| family("MSH_GUI_PROMPT_FONT", None));
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
        fonts: Some((ui, mono, prompt)),
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
        blink_epoch: Instant::now(),
        blink_fixed: None,
        window_focused: true,
        wake_at: None,
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
