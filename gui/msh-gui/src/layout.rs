//! A tab's panes, laid out as tiling window managers do: one of a few known
//! layouts, so the only keys are split, reorder, and change the layout.

/// A rectangle of cells.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Rect {
    pub x: usize,
    pub y: usize,
    pub w: usize,
    pub h: usize,
}

impl Rect {
    fn center(&self) -> (f64, f64) {
        (self.x as f64 + self.w as f64 / 2.0, self.y as f64 + self.h as f64 / 2.0)
    }
}


#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Layout {
    /// The focused pane has the whole tab.
    Everything,
    /// The first pane on the left; the others stacked on the right, each a
    /// row. What a split shows.
    Stack,
    /// Every pane a column, side by side.
    Columns,
    /// The first pane on the left; the right side in halves, for the next
    /// two.
    RightHalves,
    /// The first pane on the left; the right side in thirds, for the next
    /// three.
    RightThirds,
}

impl Layout {
    pub fn next(self) -> Layout {
        match self {
            Layout::Everything => Layout::Stack,
            Layout::Stack => Layout::Columns,
            Layout::Columns => Layout::RightHalves,
            Layout::RightHalves => Layout::RightThirds,
            Layout::RightThirds => Layout::Everything,
        }
    }

    pub fn name(self) -> &'static str {
        match self {
            Layout::Everything => "everything",
            Layout::Stack => "stack",
            Layout::Columns => "columns",
            Layout::RightHalves => "right halves",
            Layout::RightThirds => "right thirds",
        }
    }
}

/// `n` lengths that fill `total` with `gap` between them.
fn split(total: usize, n: usize, gap: usize) -> Vec<(usize, usize)> {
    let free = total.saturating_sub(gap * (n - 1));
    let mut at = 0;
    (0..n)
        .map(|i| {
            let len = free / n + (i < free % n) as usize;
            let span = (at, len);
            at += len + gap;
            span
        })
        .collect()
}

/// Where each of `n` panes goes in `area`, `gap` apart, or None for a pane
/// the layout doesn't show (the others in Everything; those past the right
/// side's slots).
pub fn rects(layout: Layout, n: usize, focus: usize, area: Rect, gap: usize) -> Vec<Option<Rect>> {
    let mut out = vec![None; n];
    if n == 0 {
        return out;
    }
    match layout {
        Layout::Everything => out[focus.min(n - 1)] = Some(area),
        _ if n == 1 => out[0] = Some(area),
        Layout::Columns => {
            for (i, (x, w)) in split(area.w, n, gap).into_iter().enumerate() {
                out[i] = Some(Rect { x: area.x + x, w, ..area });
            }
        }
        Layout::Stack | Layout::RightHalves | Layout::RightThirds => {
            let slots = match layout {
                Layout::RightHalves => 2,
                Layout::RightThirds => 3,
                _ => n - 1,
            };
            let cols = split(area.w, 2, gap);
            out[0] = Some(Rect { x: area.x + cols[0].0, w: cols[0].1, ..area });
            let k = (n - 1).min(slots);
            for (i, (y, h)) in split(area.h, k, gap).into_iter().enumerate() {
                out[1 + i] = Some(Rect { x: area.x + cols[1].0, y: area.y + y, w: cols[1].1, h });
            }
        }
    }
    out
}

#[derive(Clone, Copy, Debug)]
pub enum Direction {
    Left,
    Down,
    Up,
    Right,
}

/// The pane nearest the focused one in a direction, among those shown; or,
/// when none is there (or only one pane is shown), the next pane (Down,
/// Right) or the previous one (Up, Left), shown or not.
pub fn neighbor(rects: &[Option<Rect>], focus: usize, dir: Direction) -> usize {
    let n = rects.len();
    let step = |d: isize| ((focus as isize + d).rem_euclid(n as isize)) as usize;
    let fallback = match dir {
        Direction::Down | Direction::Right => step(1),
        Direction::Up | Direction::Left => step(-1),
    };
    let Some(from) = rects[focus] else { return fallback };
    let (fx, fy) = from.center();
    let mut best: Option<(f64, usize)> = None;
    for (i, r) in rects.iter().enumerate() {
        let Some(r) = r else { continue };
        if i == focus {
            continue;
        }
        let (x, y) = r.center();
        let ahead = match dir {
            Direction::Left => r.x + r.w <= from.x,
            Direction::Right => r.x >= from.x + from.w,
            Direction::Up => r.y + r.h <= from.y,
            Direction::Down => r.y >= from.y + from.h,
        };
        if !ahead {
            continue;
        }
        let d = (x - fx).powi(2) + (y - fy).powi(2);
        if best.is_none_or(|(b, _)| d < b) {
            best = Some((d, i));
        }
    }
    best.map_or(fallback, |(_, i)| i)
}

#[cfg(test)]
mod tests {
    use super::*;

    const AREA: Rect = Rect { x: 0, y: 1, w: 101, h: 31 };
    const GAP: usize = 1;

    #[test]
    fn layouts() {
        assert_eq!(rects(Layout::Everything, 3, 1, AREA, GAP), [None, Some(AREA), None]);
        let cols = rects(Layout::Columns, 3, 0, AREA, GAP);
        assert_eq!(cols.iter().map(|r| r.unwrap().w).sum::<usize>() + 2 * GAP, AREA.w);
        let halves = rects(Layout::RightHalves, 4, 0, AREA, GAP);
        assert_eq!(halves[0], Some(Rect { x: 0, y: 1, w: 50, h: 31 }));
        assert_eq!(halves[1], Some(Rect { x: 51, y: 1, w: 50, h: 15 }));
        assert_eq!(halves[2], Some(Rect { x: 51, y: 17, w: 50, h: 15 }));
        assert_eq!(halves[3], None);
        let thirds = rects(Layout::RightThirds, 4, 0, AREA, GAP);
        assert_eq!(thirds.iter().skip(1).map(|r| r.unwrap().h).sum::<usize>() + 2 * GAP, AREA.h);
        // The stack shows every pane: the first on the left, the rest in
        // rows on the right.
        let stack = rects(Layout::Stack, 5, 0, AREA, GAP);
        assert_eq!(stack[0], Some(Rect { x: 0, y: 1, w: 50, h: 31 }));
        assert!(stack[1..].iter().all(|r| r.is_some_and(|r| r.x == 51 && r.w == 50)));
        assert_eq!(stack[1..].iter().map(|r| r.unwrap().h).sum::<usize>() + 3 * GAP, AREA.h);
        assert_eq!(rects(Layout::Stack, 2, 0, AREA, GAP), rects(Layout::RightHalves, 2, 0, AREA, GAP));
    }

    #[test]
    fn neighbors() {
        let r = rects(Layout::RightHalves, 3, 0, AREA, GAP);
        assert_eq!(neighbor(&r, 0, Direction::Right), 1);
        assert_eq!(neighbor(&r, 1, Direction::Down), 2);
        assert_eq!(neighbor(&r, 2, Direction::Up), 1);
        assert_eq!(neighbor(&r, 2, Direction::Left), 0);
        // Nothing to the left of the first: the previous pane.
        assert_eq!(neighbor(&r, 0, Direction::Left), 2);
        let e = rects(Layout::Everything, 3, 1, AREA, GAP);
        assert_eq!(neighbor(&e, 1, Direction::Right), 2);
    }
}
