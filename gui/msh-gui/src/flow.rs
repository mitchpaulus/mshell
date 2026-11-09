//! Text laid out as a GUI lays it out: in pixels, by its glyphs' measured
//! advances, wrapping between words (within a word only when it alone is
//! wider than the line). The prompt box's prompt, line and history
//! suggestion are one flow. In a monospace font every advance is the same,
//! so the result is what cells would give.

use crate::render::{Glyphs, Shaped, ShapedGlyph};

/// How a run of text is drawn: its colour, its background, its face
/// (render::BOLD, ITALIC), and its lines.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Style {
    pub color: u32,
    pub bg: Option<u32>,
    pub face: u8,
    pub underline: bool,
    pub strike: bool,
}

impl Style {
    pub fn plain(color: u32) -> Style {
        Style { color, bg: None, face: 0, underline: false, strike: false }
    }
}

/// A glyph where it goes: its cluster's x (pixels from the flow's left)
/// and row.
pub struct Placed {
    pub glyph: ShapedGlyph,
    pub x: f32,
    pub row: usize,
    pub color: u32,
}

/// A rectangle with text: a background under it, or a line under or
/// through it.
pub struct Deco {
    pub x: f32,
    pub w: f32,
    pub row: usize,
    pub kind: DecoKind,
    pub color: u32,
}

#[derive(Clone, Copy, PartialEq, Eq)]
pub enum DecoKind {
    Background,
    Underline,
    Strike,
}

/// Where a cluster of marked text starts (its byte, x and row) and how
/// wide it is: for the cursor. The text's end has one too, of no width.
#[derive(Clone, Copy, Debug)]
pub struct Mark {
    pub byte: usize,
    pub x: f32,
    pub row: usize,
    pub w: f32,
}

/// Where a row may break: after a space. What was placed after it moves to
/// the next row when a word doesn't fit.
struct Break {
    placed: usize,
    decos: usize,
    marks: usize,
    x: f32,
}

#[derive(Default)]
pub struct Flow {
    pub width: f32,
    /// Where the next cluster goes.
    pub x: f32,
    pub row: usize,
    pub placed: Vec<Placed>,
    pub decos: Vec<Deco>,
    pub marks: Vec<Mark>,
    marking: bool,
    brk: Option<Break>,
    shaped: Shaped,
}

impl Flow {
    /// Starts again, with nothing placed, for rows `width` pixels wide. The
    /// buffers are kept.
    pub fn start(&mut self, width: f32) {
        self.width = width.max(1.0);
        (self.x, self.row) = (0.0, 0);
        self.placed.clear();
        self.decos.clear();
        self.marks.clear();
        self.marking = false;
        self.brk = None;
    }

    /// Ends the row: what follows starts the next.
    pub fn newline(&mut self) {
        self.row += 1;
        self.x = 0.0;
        self.brk = None;
    }

    /// While on, each cluster added is marked.
    pub fn marking(&mut self, on: bool) {
        self.marking = on;
    }

    /// Marks where the text added so far ends, as `byte`.
    pub fn mark_end(&mut self, byte: usize) {
        self.marks.push(Mark { byte, x: self.x, row: self.row, w: 0.0 });
    }

    /// Adds text in a family, in runs: each run's first byte and style, in
    /// order, the first at 0.
    pub fn add(&mut self, glyphs: &mut Glyphs, slot: u8, text: &str, runs: &[(usize, Style)]) {
        let mut shaped = std::mem::take(&mut self.shaped);
        for (k, &(start, style)) in runs.iter().enumerate() {
            let end = runs.get(k + 1).map_or(text.len(), |r| r.0).min(text.len());
            if start >= end {
                continue;
            }
            let piece = &text[start..end];
            glyphs.shape(slot, style.face, piece, &mut shaped);
            for c in &shaped.clusters {
                let space = piece[c.start..c.end].chars().all(char::is_whitespace);
                // A space may hang past the edge; a word that doesn't fit
                // goes to the next row.
                if !space && self.x + c.advance > self.width && self.x > 0.0 {
                    self.wrap();
                }
                if self.marking {
                    self.marks.push(Mark { byte: start + c.start, x: self.x, row: self.row, w: c.advance });
                }
                for &g in &shaped.glyphs[c.first..c.last] {
                    self.placed.push(Placed { glyph: g, x: self.x, row: self.row, color: style.color });
                }
                if let Some(bg) = style.bg {
                    self.deco(DecoKind::Background, bg, c.advance);
                }
                if style.underline {
                    self.deco(DecoKind::Underline, style.color, c.advance);
                }
                if style.strike {
                    self.deco(DecoKind::Strike, style.color, c.advance);
                }
                self.x += c.advance;
                if space {
                    self.brk = Some(Break { placed: self.placed.len(), decos: self.decos.len(), marks: self.marks.len(), x: self.x });
                }
            }
        }
        self.shaped = shaped;
    }

    /// A decoration under the cluster at x, `w` wide: the one before it
    /// grows instead when it is the same and ends here, so a run is one
    /// rectangle, without seams between its clusters.
    fn deco(&mut self, kind: DecoKind, color: u32, w: f32) {
        let start = self.brk.as_ref().map_or(0, |b| b.decos);
        if let Some(d) = self.decos[start..].iter_mut().rev().find(|d| d.kind == kind) {
            if d.row == self.row && d.color == color && (d.x + d.w - self.x).abs() < 0.01 {
                d.w += w;
                return;
            }
        }
        self.decos.push(Deco { x: self.x, w, row: self.row, kind, color });
    }

    /// Starts the next row for a word that doesn't fit: the word, from the
    /// last space on, moves to it, unless the word is all the row has.
    fn wrap(&mut self) {
        match self.brk.take() {
            Some(b) if b.x > 0.0 && b.x < self.x => {
                for p in &mut self.placed[b.placed..] {
                    (p.x, p.row) = (p.x - b.x, p.row + 1);
                }
                for d in &mut self.decos[b.decos..] {
                    (d.x, d.row) = (d.x - b.x, d.row + 1);
                }
                for m in &mut self.marks[b.marks..] {
                    (m.x, m.row) = (m.x - b.x, m.row + 1);
                }
                self.x -= b.x;
                self.row += 1;
            }
            _ => self.newline(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::render::{Family, MONO};

    fn glyphs() -> Option<Glyphs> {
        let ui = Family::load("/usr/share/fonts/noto/NotoSans-Regular.ttf", "TEST_NO_VAR").ok()?;
        let mono = Family::load("/usr/share/fonts/noto/NotoSansMono-Regular.ttf", "TEST_NO_VAR").ok()?;
        Some(Glyphs::new(ui, mono, None, 15.0))
    }

    /// The marks' rows and xs, rounded.
    fn marks(f: &Flow) -> Vec<(usize, usize, i32)> {
        f.marks.iter().map(|m| (m.byte, m.row, m.x.round() as i32)).collect()
    }

    #[test]
    fn monospace_is_cells() {
        let Some(mut g) = glyphs() else { return };
        let cw = g.cell.w as f32;
        let mut f = Flow::default();
        // Ten cells wide: "abc defgh" fits, "ijk" goes to the next row.
        f.start(10.0 * cw);
        f.marking(true);
        f.add(&mut g, MONO, "abc defgh ijk", &[(0, Style::plain(0))]);
        f.mark_end(13);
        let m = marks(&f);
        assert_eq!(m[4], (4, 0, (4.0 * cw).round() as i32), "{m:?}");
        assert_eq!(m[10], (10, 1, 0), "the word after the last space wraps: {m:?}");
        assert_eq!(*m.last().unwrap(), (13, 1, (3.0 * cw).round() as i32));
    }

    #[test]
    fn a_word_wider_than_the_row_breaks_inside() {
        let Some(mut g) = glyphs() else { return };
        let cw = g.cell.w as f32;
        let mut f = Flow::default();
        f.start(4.0 * cw);
        f.marking(true);
        f.add(&mut g, MONO, "abcdefg", &[(0, Style::plain(0))]);
        let rows: Vec<usize> = f.marks.iter().map(|m| m.row).collect();
        assert_eq!(rows, [0, 0, 0, 0, 1, 1, 1]);
    }

    #[test]
    fn proportional_measures() {
        let Some(mut g) = glyphs() else { return };
        let mut f = Flow::default();
        f.start(1000.0);
        f.marking(true);
        f.add(&mut g, crate::render::UI, "iiWW", &[(0, Style::plain(0))]);
        // An i is narrower than a W.
        let w: Vec<f32> = f.marks.iter().map(|m| m.w).collect();
        assert!(w[0] < w[2] / 2.0, "{w:?}");
        assert!((f.marks[2].x - (w[0] + w[1])).abs() < 0.01);
    }
}
