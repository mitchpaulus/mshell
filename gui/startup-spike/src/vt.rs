//! The cell grid from libghostty-vt: bytes go into a terminal, and the
//! render state gives one `CellInstance` per non-blank cell.

use ghostty_vt_sys as g;
use std::ptr;

use crate::{trace, CellInstance, FIRST_GLYPH, GLYPH_COUNT};

/// A terminal and its render state. The real frontend keeps one per block;
/// the spike keeps it alive for the window's lifetime.
pub struct Vt {
    term: g::GhosttyTerminal,
    state: g::GhosttyRenderState,
    rows: g::GhosttyRenderStateRowIterator,
    cells: g::GhosttyRenderStateRowCells,
}

fn ok(r: g::GhosttyResult, what: &str) {
    assert!(r == g::GHOSTTY_SUCCESS, "{what}: GhosttyResult {r}");
}

impl Vt {
    pub fn new(cols: u16, rows: u16) -> Vt {
        let mut v = Vt { term: ptr::null_mut(), state: ptr::null_mut(), rows: ptr::null_mut(), cells: ptr::null_mut() };
        // SAFETY: each call gets a valid out-pointer; NULL allocator = default.
        unsafe {
            ok(g::ghostty_terminal_new(ptr::null(), &mut v.term, cols, rows), "terminal_new");
            ok(g::ghostty_render_state_new(ptr::null(), &mut v.state), "render_state_new");
            ok(g::ghostty_render_state_row_iterator_new(ptr::null(), &mut v.rows), "row_iterator_new");
            ok(g::ghostty_render_state_row_cells_new(ptr::null(), &mut v.cells), "row_cells_new");
        }
        v
    }

    pub fn write(&mut self, bytes: &[u8]) {
        // SAFETY: term is live; bytes is a valid slice.
        unsafe { g::ghostty_terminal_vt_write(self.term, bytes.as_ptr(), bytes.len()) }
    }

    /// Snapshot the terminal and append its non-blank cells to `out`.
    /// Cells without a colour of their own get `default_fg`.
    pub fn cells(&mut self, default_fg: u32, out: &mut Vec<CellInstance>) {
        // SAFETY: all handles are live and owned by self; every out-pointer
        // points at a value of the type the data kind documents.
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
            let mut row = 0u32;
            while g::ghostty_render_state_row_iterator_next(self.rows) {
                ok(
                    g::ghostty_render_state_row_get(
                        self.rows,
                        g::GHOSTTY_RENDER_STATE_ROW_DATA_CELLS,
                        (&mut self.cells as *mut g::GhosttyRenderStateRowCells).cast(),
                    ),
                    "row cells",
                );
                let mut col = 0u32;
                while g::ghostty_render_state_row_cells_next(self.cells) {
                    let mut len = 0u32;
                    g::ghostty_render_state_row_cells_get(
                        self.cells,
                        g::GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_LEN,
                        (&mut len as *mut u32).cast(),
                    );
                    if len > 0 {
                        let mut cp = [0u32; 1];
                        let mut buf = g::GhosttyCodepointBuffer { ptr: cp.as_mut_ptr(), cap: 1, len: 0 };
                        g::ghostty_render_state_row_cells_get(
                            self.cells,
                            g::GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_GRAPHEMES_BUF,
                            (&mut buf as *mut g::GhosttyCodepointBuffer).cast(),
                        );
                        let mut rgb = g::GhosttyColorRgb::default();
                        let fg = match g::ghostty_render_state_row_cells_get(
                            self.cells,
                            g::GHOSTTY_RENDER_STATE_ROW_CELLS_DATA_FG_COLOR,
                            (&mut rgb as *mut g::GhosttyColorRgb).cast(),
                        ) {
                            g::GHOSTTY_SUCCESS => 0xff00_0000 | (rgb.b as u32) << 16 | (rgb.g as u32) << 8 | rgb.r as u32,
                            _ => default_fg,
                        };
                        // Only the ASCII atlas exists yet: anything else is '?'.
                        let glyph = match cp[0].checked_sub(FIRST_GLYPH as u32) {
                            Some(i) if i < GLYPH_COUNT => i,
                            _ => (b'?' - FIRST_GLYPH) as u32,
                        };
                        if cp[0] != ' ' as u32 {
                            out.push(CellInstance { col, row, glyph, fg });
                        }
                    }
                    col += 1;
                }
                row += 1;
            }
            ok(g::ghostty_render_state_clean(self.state), "render_state_clean");
        }
        trace("render state read, cells built");
    }
}

impl Drop for Vt {
    fn drop(&mut self) {
        // SAFETY: handles were created in new() and are freed once.
        unsafe {
            g::ghostty_render_state_row_cells_free(self.cells);
            g::ghostty_render_state_row_iterator_free(self.rows);
            g::ghostty_render_state_free(self.state);
            g::ghostty_terminal_free(self.term);
        }
    }
}

/// A file's bytes as a PTY would deliver them: `\n` becomes `\r\n` (onlcr).
pub fn as_tty_output(bytes: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(bytes.len() + bytes.len() / 16);
    for &b in bytes {
        if b == b'\n' {
            out.push(b'\r');
        }
        out.push(b);
    }
    out
}
