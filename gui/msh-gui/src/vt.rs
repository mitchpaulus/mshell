//! A block's terminal emulator: libghostty-vt (ai/gui-terminal.typ, "VT
//! emulation"). Each block that runs gets a fresh one, fed the PTY output
//! tagged with its id; finished blocks keep theirs, to draw from.

use ghostty_vt_sys as g;
use std::ptr;
use std::time::{Duration, Instant};

/// How long a program may hold the screen (synchronized output, mode
/// 2026) before it is drawn anyway, as Ghostty does.
const HOLD_MAX: Duration = Duration::from_secs(1);

fn ok(r: g::GhosttyResult, what: &str) {
    assert!(r == g::GHOSTTY_SUCCESS, "{what}: GhosttyResult {r}");
}

/// A colour as the cells give it: RGB, or None for the default.
pub type Rgb = Option<[u8; 3]>;

/// One cell as drawn: its first code point, its colours with inverse
/// video applied, and the lines through and under it.
pub struct Cell {
    pub ch: char,
    pub fg: Rgb,
    pub bg: Rgb,
    /// With inverse video and no colours of its own: the default colours
    /// swapped.
    pub inverse_default: bool,
    pub underline: Underline,
    /// The underline's own colour (SGR 58); None draws it in the text's.
    pub underline_color: Rgb,
    pub strikethrough: bool,
    pub overline: bool,
    pub bold: bool,
    pub italic: bool,
    /// Dim (SGR 2).
    pub faint: bool,
    /// Blinking text (SGR 5).
    pub blink: bool,
    /// Concealed (SGR 8): only the background shows.
    pub invisible: bool,
}

/// An underline's style (SGR 4, and 4:1 to 4:5).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Underline {
    None,
    Single,
    Double,
    Curly,
    Dotted,
    Dashed,
}

/// The cursor's shape, as the program set it (DECSCUSR, CSI Ps SP q).
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum CursorShape {
    Block,
    HollowBlock,
    Bar,
    Underline,
}

/// The cursor where it is drawn: column and row of the viewport, its
/// shape, whether it blinks (DECSCUSR's odd shapes, mode 12), and its
/// colour (OSC 12) when the program set one.
#[derive(Clone, Copy, Debug)]
pub struct Cursor {
    pub x: u16,
    pub y: u16,
    pub shape: CursorShape,
    pub blinking: bool,
    pub color: Rgb,
}

const STYLE_FLAG_BOLD: u16 = 1 << 0;
const STYLE_FLAG_ITALIC: u16 = 1 << 1;
const STYLE_FLAG_FAINT: u16 = 1 << 2;
const STYLE_FLAG_BLINK: u16 = 1 << 3;
const STYLE_FLAG_INVERSE: u16 = 1 << 4;
const STYLE_FLAG_INVISIBLE: u16 = 1 << 5;
const STYLE_FLAG_STRIKETHROUGH: u16 = 1 << 6;
const STYLE_FLAG_OVERLINE: u16 = 1 << 7;
const STYLE_UNDERLINE_MASK: u16 = 0b111 << g::GHOSTTY_STYLE_UNDERLINE_SHIFT;

/// What the terminal's callbacks use, at a fixed address (their userdata).
struct Shared {
    /// Replies to the program's queries (device attributes, status
    /// reports, colours), in order, for the frontend to send to its PTY.
    replies: Vec<u8>,
    size: g::GhosttySizeReportSize,
    /// Since when the program holds the screen (synchronized output).
    held_since: Option<Instant>,
    /// A BEL came since the last take_bell.
    bell: bool,
}

unsafe extern "C" fn write_pty(_: g::GhosttyTerminal, userdata: *mut std::ffi::c_void, data: *const u8, len: usize) {
    // SAFETY: userdata is the Term's Shared, and data holds len bytes.
    unsafe { (*(userdata as *mut Shared)).replies.extend_from_slice(std::slice::from_raw_parts(data, len)) }
}

unsafe extern "C" fn report_size(_: g::GhosttyTerminal, userdata: *mut std::ffi::c_void, out: *mut g::GhosttySizeReportSize) -> bool {
    // SAFETY: userdata is the Term's Shared; out is valid.
    unsafe { *out = (*(userdata as *mut Shared)).size };
    true
}

unsafe extern "C" fn render_hold(_: g::GhosttyTerminal, userdata: *mut std::ffi::c_void, held: bool) {
    // SAFETY: userdata is the Term's Shared.
    let shared = unsafe { &mut *(userdata as *mut Shared) };
    shared.held_since = held.then(Instant::now);
}

unsafe extern "C" fn bell(_: g::GhosttyTerminal, userdata: *mut std::ffi::c_void) {
    // SAFETY: userdata is the Term's Shared.
    unsafe { (*(userdata as *mut Shared)).bell = true };
}

unsafe extern "C" fn color_scheme(_: g::GhosttyTerminal, _: *mut std::ffi::c_void, out: *mut g::GhosttyColorScheme) -> bool {
    // SAFETY: out is valid. The GUI is dark.
    unsafe { *out = g::GHOSTTY_COLOR_SCHEME_DARK };
    true
}

/// The colours a program is told when it asks (OSC 10, 11, 12): the GUI's.
pub struct Colors {
    pub fg: [u8; 3],
    pub bg: [u8; 3],
    pub cursor: [u8; 3],
}

pub struct Term {
    shared: Box<Shared>,
    term: g::GhosttyTerminal,
    state: g::GhosttyRenderState,
    rows: g::GhosttyRenderStateRowIterator,
    cells: g::GhosttyRenderStateRowCells,
    encoder: g::GhosttyKeyEncoder,
    event: g::GhosttyKeyEvent,
    /// The palette, for underline colours given as an index; read once a
    /// visit, when one is.
    palette: [g::GhosttyColorRgb; 256],
}

// SAFETY: the handles are used from one thread at a time (the UI thread).
unsafe impl Send for Term {}

impl Term {
    pub fn new(cols: u16, rows: u16, colors: &Colors) -> Term {
        let mut t = Term {
            shared: Box::new(Shared {
                replies: Vec::new(),
                size: g::GhosttySizeReportSize { rows, columns: cols, cell_width: 0, cell_height: 0 },
                held_since: None,
                bell: false,
            }),
            term: ptr::null_mut(),
            state: ptr::null_mut(),
            rows: ptr::null_mut(),
            cells: ptr::null_mut(),
            encoder: ptr::null_mut(),
            event: ptr::null_mut(),
            palette: [g::GhosttyColorRgb::default(); 256],
        };
        // SAFETY: valid out-pointers; NULL allocator = the default.
        unsafe {
            ok(g::ghostty_terminal_new(ptr::null(), &mut t.term, cols, rows), "terminal_new");
            ok(g::ghostty_render_state_new(ptr::null(), &mut t.state), "render_state_new");
            ok(g::ghostty_render_state_row_iterator_new(ptr::null(), &mut t.rows), "row_iterator_new");
            ok(g::ghostty_render_state_row_cells_new(ptr::null(), &mut t.cells), "row_cells_new");
            ok(g::ghostty_key_encoder_new(ptr::null(), &mut t.encoder), "key_encoder_new");
            ok(g::ghostty_key_event_new(ptr::null(), &mut t.event), "key_event_new");
            // A block keeps up to 16 MB of history; the default keeps one
            // page (ai/gui-terminal.typ, "Costs and gaps").
            let mut limit: usize = 16 << 20;
            ok(
                g::ghostty_terminal_set(t.term, g::GHOSTTY_TERMINAL_OPT_SCROLLBACK_MAX_BYTES, (&mut limit as *mut usize).cast()),
                "scrollback limit",
            );
            // Answers to the program's queries: without them a program
            // waits for each (Neovim: E1568, and a second's wait to quit).
            let shared: *mut Shared = &mut *t.shared;
            let set = |opt, value: *const std::ffi::c_void| ok(g::ghostty_terminal_set(t.term, opt, value), "terminal option");
            set(g::GHOSTTY_TERMINAL_OPT_USERDATA, shared as *const std::ffi::c_void);
            set(g::GHOSTTY_TERMINAL_OPT_WRITE_PTY, write_pty as *const std::ffi::c_void);
            set(g::GHOSTTY_TERMINAL_OPT_SIZE, report_size as *const std::ffi::c_void);
            set(g::GHOSTTY_TERMINAL_OPT_COLOR_SCHEME, color_scheme as *const std::ffi::c_void);
            set(g::GHOSTTY_TERMINAL_OPT_RENDER_HOLD, render_hold as *const std::ffi::c_void);
            set(g::GHOSTTY_TERMINAL_OPT_BELL, bell as *const std::ffi::c_void);
            for (opt, [r, g_, b]) in [
                (g::GHOSTTY_TERMINAL_OPT_COLOR_FOREGROUND, colors.fg),
                (g::GHOSTTY_TERMINAL_OPT_COLOR_BACKGROUND, colors.bg),
                (g::GHOSTTY_TERMINAL_OPT_COLOR_CURSOR, colors.cursor),
            ] {
                let rgb = g::GhosttyColorRgb { r, g: g_, b };
                set(opt, (&rgb as *const g::GhosttyColorRgb).cast());
            }
        }
        t
    }

    /// Replies the terminal made to the program since the last call, to
    /// write to its PTY.
    pub fn take_replies(&mut self) -> Vec<u8> {
        std::mem::take(&mut self.shared.replies)
    }

    /// Whether a BEL came since the last call.
    pub fn take_bell(&mut self) -> bool {
        std::mem::take(&mut self.shared.bell)
    }

    /// While the program holds the screen (synchronized output, mode
    /// 2026), when the hold runs out and the screen is drawn anyway.
    pub fn hold_ends(&self) -> Option<Instant> {
        self.shared.held_since.map(|t| t + HOLD_MAX).filter(|&end| end > Instant::now())
    }

    pub fn write(&mut self, bytes: &[u8]) {
        // SAFETY: the terminal is live; bytes is a valid slice.
        unsafe { g::ghostty_terminal_vt_write(self.term, bytes.as_ptr(), bytes.len()) }
    }

    pub fn resize(&mut self, cols: u16, rows: u16, cell_w: u32, cell_h: u32) {
        self.shared.size = g::GhosttySizeReportSize { rows, columns: cols, cell_width: cell_w, cell_height: cell_h };
        // SAFETY: the terminal is live.
        unsafe { ok(g::ghostty_terminal_resize(self.term, cols, rows, cell_w, cell_h), "resize") }
    }

    fn get<T: Default>(&self, data: g::GhosttyTerminalData) -> T {
        let mut v = T::default();
        // SAFETY: T is the output type the data kind documents.
        unsafe { ok(g::ghostty_terminal_get(self.term, data, (&mut v as *mut T).cast()), "terminal_get") };
        v
    }

    pub fn alternate_screen(&self) -> bool {
        self.get::<g::GhosttyTerminalScreen>(g::GHOSTTY_TERMINAL_DATA_ACTIVE_SCREEN) == g::GHOSTTY_TERMINAL_SCREEN_ALTERNATE
    }

    pub fn scrollback_rows(&self) -> usize {
        self.get(g::GHOSTTY_TERMINAL_DATA_SCROLLBACK_ROWS)
    }

    /// The cursor: column and row of the active area.
    pub fn cursor(&self) -> (u16, u16) {
        (self.get(g::GHOSTTY_TERMINAL_DATA_CURSOR_X), self.get(g::GHOSTTY_TERMINAL_DATA_CURSOR_Y))
    }

    /// The rows of output so far: the history, and the active area down to
    /// the cursor's row (counted only once something is on it).
    pub fn content_rows(&self) -> usize {
        let (x, y): (u16, u16) = (self.get(g::GHOSTTY_TERMINAL_DATA_CURSOR_X), self.get(g::GHOSTTY_TERMINAL_DATA_CURSOR_Y));
        self.scrollback_rows() + y as usize + (x > 0) as usize
    }

    /// Scrolls the viewport so `row` of the history (0 is its top) is its
    /// first row, as far as the active area allows; returns the row of the
    /// viewport where `row` is.
    pub fn scroll_to(&mut self, row: usize) -> usize {
        let top = row.min(self.scrollback_rows());
        let sv = g::GhosttyTerminalScrollViewport {
            tag: g::GHOSTTY_TERMINAL_SCROLL_VIEWPORT_ROW,
            value: g::GhosttyTerminalScrollViewportValue { row: top },
        };
        // SAFETY: the terminal is live.
        unsafe { g::ghostty_terminal_scroll_viewport(self.term, sv) };
        row - top
    }

    pub fn scroll_to_bottom(&mut self) {
        let sv = g::GhosttyTerminalScrollViewport {
            tag: g::GHOSTTY_TERMINAL_SCROLL_VIEWPORT_BOTTOM,
            value: g::GhosttyTerminalScrollViewportValue::default(),
        };
        // SAFETY: the terminal is live.
        unsafe { g::ghostty_terminal_scroll_viewport(self.term, sv) };
    }

    /// Visits the viewport's cells, rows `from..to`, and returns the cursor
    /// when it is visible there.
    pub fn visit(&mut self, from: usize, to: usize, mut f: impl FnMut(usize, usize, &Cell)) -> Option<Cursor> {
        let mut palette_read = false;
        // While the program holds the screen, the last snapshot is drawn
        // again: a half-made frame of Neovim's never shows.
        let held = self.hold_ends().is_some();
        // SAFETY: all handles are live and owned by self; each out-pointer
        // points at the type its data kind documents.
        unsafe {
            if !held {
                ok(g::ghostty_render_state_update(self.state, self.term), "render_state_update");
            }
            ok(
                g::ghostty_render_state_get(
                    self.state,
                    g::GHOSTTY_RENDER_STATE_DATA_ROW_ITERATOR,
                    (&mut self.rows as *mut g::GhosttyRenderStateRowIterator).cast(),
                ),
                "row iterator",
            );
            let mut row = 0;
            while g::ghostty_render_state_row_iterator_next(self.rows) {
                if row >= to {
                    break;
                }
                if row >= from {
                    ok(
                        g::ghostty_render_state_row_get(
                            self.rows,
                            g::GHOSTTY_RENDER_STATE_ROW_DATA_CELLS,
                            (&mut self.cells as *mut g::GhosttyRenderStateRowCells).cast(),
                        ),
                        "row cells",
                    );
                    let mut col = 0;
                    while g::ghostty_render_state_row_cells_next(self.cells) {
                        let mut len = 0u32;
                        g::ghostty_render_state_row_cells_get(
                            self.cells,
                            g::GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_LEN,
                            (&mut len as *mut u32).cast(),
                        );
                        let mut cp = [0u32; 1];
                        if len > 0 {
                            let mut buf = g::GhosttyCodepointBuffer { ptr: cp.as_mut_ptr(), cap: 1, len: 0 };
                            g::ghostty_render_state_row_cells_get(
                                self.cells,
                                g::GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_BUF,
                                (&mut buf as *mut g::GhosttyCodepointBuffer).cast(),
                            );
                        }
                        let color = |data| {
                            let mut rgb = g::GhosttyColorRgb::default();
                            let r = g::ghostty_render_state_row_cells_get(
                                self.cells,
                                data,
                                (&mut rgb as *mut g::GhosttyColorRgb).cast(),
                            );
                            (r == g::GHOSTTY_SUCCESS).then_some([rgb.r, rgb.g, rgb.b])
                        };
                        let (mut fg, mut bg) = (
                            color(g::GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_FG_COLOR),
                            color(g::GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_BG_COLOR),
                        );
                        let mut style = g::GhosttyStyle::default();
                        g::ghostty_render_state_row_cells_get(
                            self.cells,
                            g::GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_STYLE,
                            (&mut style as *mut g::GhosttyStyle).cast(),
                        );
                        let inverse = style.flags & STYLE_FLAG_INVERSE != 0;
                        if inverse {
                            std::mem::swap(&mut fg, &mut bg);
                        }
                        let underline = match (style.flags & STYLE_UNDERLINE_MASK) >> g::GHOSTTY_STYLE_UNDERLINE_SHIFT {
                            0 => Underline::None,
                            1 => Underline::Single,
                            2 => Underline::Double,
                            3 => Underline::Curly,
                            4 => Underline::Dotted,
                            _ => Underline::Dashed,
                        };
                        let uc = style.underline_color;
                        let underline_color = match uc.tag as g::GhosttyStyleColorTag {
                            g::GHOSTTY_STYLE_COLOR_RGB => Some([uc.value.rgb.r, uc.value.rgb.g, uc.value.rgb.b]),
                            g::GHOSTTY_STYLE_COLOR_PALETTE if underline != Underline::None => {
                                if !palette_read {
                                    palette_read = true;
                                    ok(
                                        g::ghostty_render_state_get(
                                            self.state,
                                            g::GHOSTTY_RENDER_STATE_DATA_COLOR_PALETTE,
                                            self.palette.as_mut_ptr().cast(),
                                        ),
                                        "palette",
                                    );
                                }
                                let c = self.palette[uc.value.palette as usize];
                                Some([c.r, c.g, c.b])
                            }
                            _ => None,
                        };
                        let cell = Cell {
                            ch: char::from_u32(cp[0]).filter(|_| len > 0).unwrap_or(' '),
                            fg,
                            bg,
                            inverse_default: inverse,
                            underline,
                            underline_color,
                            strikethrough: style.flags & STYLE_FLAG_STRIKETHROUGH != 0,
                            overline: style.flags & STYLE_FLAG_OVERLINE != 0,
                            bold: style.flags & STYLE_FLAG_BOLD != 0,
                            italic: style.flags & STYLE_FLAG_ITALIC != 0,
                            faint: style.flags & STYLE_FLAG_FAINT != 0,
                            blink: style.flags & STYLE_FLAG_BLINK != 0,
                            invisible: style.flags & STYLE_FLAG_INVISIBLE != 0,
                        };
                        f(row, col, &cell);
                        col += 1;
                    }
                }
                row += 1;
            }
            let mut cursor: g::GhosttyRenderStateCursor = ghostty_vt_sys::sized();
            ok(
                g::ghostty_render_state_get(
                    self.state,
                    g::GHOSTTY_RENDER_STATE_DATA_CURSOR,
                    (&mut cursor as *mut g::GhosttyRenderStateCursor).cast(),
                ),
                "cursor",
            );
            let shape = match cursor.visual_style {
                g::GHOSTTY_CURSOR_STYLE_BAR => CursorShape::Bar,
                g::GHOSTTY_CURSOR_STYLE_UNDERLINE => CursorShape::Underline,
                g::GHOSTTY_CURSOR_STYLE_BLOCK_HOLLOW => CursorShape::HollowBlock,
                _ => CursorShape::Block,
            };
            if !(cursor.visible && cursor.viewport_has_value) {
                return None;
            }
            let mut has_color = false;
            let mut rgb = g::GhosttyColorRgb::default();
            g::ghostty_render_state_get(
                self.state,
                g::GHOSTTY_RENDER_STATE_DATA_COLOR_CURSOR_HAS_VALUE,
                (&mut has_color as *mut bool).cast(),
            );
            if has_color {
                g::ghostty_render_state_get(self.state, g::GHOSTTY_RENDER_STATE_DATA_COLOR_CURSOR, (&mut rgb as *mut g::GhosttyColorRgb).cast());
            }
            Some(Cursor {
                x: cursor.viewport_x,
                y: cursor.viewport_y,
                shape,
                blinking: cursor.blinking,
                color: has_color.then_some([rgb.r, rgb.g, rgb.b]),
            })
        }
    }

    /// Encodes a key for the program, as the terminal's modes say (cursor
    /// key mode, the kitty keyboard protocol). Empty when the key sends
    /// nothing.
    pub fn encode_key(&mut self, key: g::GhosttyKey, mods: g::GhosttyMods, text: &str, unshifted: Option<char>, out: &mut Vec<u8>) {
        out.clear();
        // SAFETY: the handles are live; text outlives the encode call.
        unsafe {
            g::ghostty_key_encoder_set_from_terminal(self.encoder, self.term);
            g::ghostty_key_event_set_action(self.event, g::GHOSTTY_KEY_ACTION_PRESS);
            g::ghostty_key_event_set_key(self.event, key);
            g::ghostty_key_event_set_mods(self.event, mods);
            g::ghostty_key_event_set_utf8(self.event, g::GhosttyString { ptr: text.as_ptr(), len: text.len() });
            g::ghostty_key_event_set_unshifted_codepoint(self.event, unshifted.map_or(0, |c| c as u32));
            let mut buf = [0u8; 128];
            let mut written = 0usize;
            if g::ghostty_key_encoder_encode(self.encoder, self.event, buf.as_mut_ptr(), buf.len(), &mut written)
                == g::GHOSTTY_SUCCESS
            {
                out.extend_from_slice(&buf[..written]);
            }
        }
    }
}

impl Drop for Term {
    fn drop(&mut self) {
        // SAFETY: each handle was created in new() and is freed once.
        unsafe {
            g::ghostty_key_event_free(self.event);
            g::ghostty_key_encoder_free(self.encoder);
            g::ghostty_render_state_row_cells_free(self.cells);
            g::ghostty_render_state_row_iterator_free(self.rows);
            g::ghostty_render_state_free(self.state);
            g::ghostty_terminal_free(self.term);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const COLORS: Colors = Colors { fg: [200, 200, 200], bg: [0, 0, 0], cursor: [200, 200, 200] };

    /// The cells of the first row, and the cursor.
    fn row(t: &mut Term) -> (Vec<(char, Underline, bool, bool, bool, bool, bool, bool, bool, Rgb)>, Option<Cursor>) {
        let mut cells = Vec::new();
        let cursor = t.visit(0, 1, |_, _, c| {
            cells.push((c.ch, c.underline, c.bold, c.italic, c.faint, c.blink, c.invisible, c.overline, c.strikethrough, c.underline_color))
        });
        (cells, cursor)
    }

    #[test]
    fn attributes() {
        let mut t = Term::new(20, 3, &COLORS);
        t.write(b"\x1b[1ma\x1b[0;3mb\x1b[0;2mc\x1b[0;5md\x1b[0;8me\x1b[0;53mf\x1b[0;9mg\x1b[0;4:3;58;2;1;2;3mh\x1b[0;4;58;5;1mi\x1b[0m");
        let (cells, _) = row(&mut t);
        let c = |i: usize| cells[i];
        assert!(c(0).2 && !c(0).3, "bold");
        assert!(c(1).3 && !c(1).2, "italic");
        assert!(c(2).4, "faint");
        assert!(c(3).5, "blink");
        assert!(c(4).6, "invisible");
        assert!(c(5).7, "overline");
        assert!(c(6).8, "strikethrough");
        assert_eq!((c(7).1, c(7).9), (Underline::Curly, Some([1, 2, 3])));
        // A palette underline colour: index 1, red.
        assert_eq!(c(8).1, Underline::Single);
        assert!(c(8).9.is_some_and(|[r, g, b]| r > g && r > b), "palette underline colour {:?}", c(8).9);
        assert_eq!(c(9).1, Underline::None);
    }

    #[test]
    fn cursor_shape_and_blink() {
        let mut t = Term::new(20, 3, &COLORS);
        for (seq, shape, blinking) in [
            (&b"\x1b[2 q"[..], CursorShape::Block, false),
            (b"\x1b[5 q", CursorShape::Bar, true),
            (b"\x1b[6 q", CursorShape::Bar, false),
            (b"\x1b[3 q", CursorShape::Underline, true),
        ] {
            t.write(seq);
            let k = row(&mut t).1.expect("a cursor");
            assert_eq!((k.shape, k.blinking), (shape, blinking), "{seq:?}");
        }
        // A cursor colour the program sets (OSC 12).
        t.write(b"\x1b]12;rgb:ff/00/00\x1b\\");
        assert_eq!(row(&mut t).1.unwrap().color, Some([255, 0, 0]));
    }

    #[test]
    fn synchronized_output_holds_the_frame() {
        let mut t = Term::new(20, 3, &COLORS);
        t.write(b"one");
        assert_eq!(row(&mut t).0[0].0, 'o');
        // Held: what is written meanwhile doesn't show until the hold ends.
        t.write(b"\x1b[?2026h\r\x1b[2Ktwo");
        assert!(t.hold_ends().is_some());
        assert_eq!(row(&mut t).0[0].0, 'o');
        t.write(b"\x1b[?2026l");
        assert!(t.hold_ends().is_none());
        assert_eq!(row(&mut t).0[0].0, 't');
    }

    #[test]
    fn bell() {
        let mut t = Term::new(20, 3, &COLORS);
        assert!(!t.take_bell());
        t.write(b"\x07");
        assert!(t.take_bell());
        assert!(!t.take_bell());
    }
}
