//! A block's terminal emulator: libghostty-vt (ai/gui-terminal.typ, "VT
//! emulation"). Each block that runs gets a fresh one, fed the PTY output
//! tagged with its id; finished blocks keep theirs, to draw from.

use ghostty_vt_sys as g;
use std::ptr;

fn ok(r: g::GhosttyResult, what: &str) {
    assert!(r == g::GHOSTTY_SUCCESS, "{what}: GhosttyResult {r}");
}

/// A colour as the cells give it: RGB, or None for the default.
pub type Rgb = Option<[u8; 3]>;

/// One cell as drawn: its first code point, and its colours with inverse
/// video applied.
pub struct Cell {
    pub ch: char,
    pub fg: Rgb,
    pub bg: Rgb,
    /// With inverse video and no colours of its own: the default colours
    /// swapped.
    pub inverse_default: bool,
}

const STYLE_FLAG_INVERSE: u16 = 1 << 4;

pub struct Term {
    term: g::GhosttyTerminal,
    state: g::GhosttyRenderState,
    rows: g::GhosttyRenderStateRowIterator,
    cells: g::GhosttyRenderStateRowCells,
    encoder: g::GhosttyKeyEncoder,
    event: g::GhosttyKeyEvent,
}

// SAFETY: the handles are used from one thread at a time (the UI thread).
unsafe impl Send for Term {}

impl Term {
    pub fn new(cols: u16, rows: u16) -> Term {
        let mut t = Term {
            term: ptr::null_mut(),
            state: ptr::null_mut(),
            rows: ptr::null_mut(),
            cells: ptr::null_mut(),
            encoder: ptr::null_mut(),
            event: ptr::null_mut(),
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
        }
        t
    }

    pub fn write(&mut self, bytes: &[u8]) {
        // SAFETY: the terminal is live; bytes is a valid slice.
        unsafe { g::ghostty_terminal_vt_write(self.term, bytes.as_ptr(), bytes.len()) }
    }

    pub fn resize(&mut self, cols: u16, rows: u16, cell_w: u32, cell_h: u32) {
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
    /// (column, row of the viewport) when it is visible there.
    pub fn visit(&mut self, from: usize, to: usize, mut f: impl FnMut(usize, usize, &Cell)) -> Option<(u16, u16)> {
        // SAFETY: all handles are live and owned by self; each out-pointer
        // points at the type its data kind documents.
        unsafe {
            ok(g::ghostty_render_state_update(self.state, self.term), "render_state_update");
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
                        let cell = Cell {
                            ch: char::from_u32(cp[0]).filter(|_| len > 0).unwrap_or(' '),
                            fg,
                            bg,
                            inverse_default: inverse,
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
            (cursor.visible && cursor.viewport_has_value).then_some((cursor.viewport_x, cursor.viewport_y))
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
