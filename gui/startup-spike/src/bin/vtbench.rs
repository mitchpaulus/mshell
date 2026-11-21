//! libghostty-vt costs the design doc asks about (ai/gui-terminal.typ, "Costs and gaps"):
//! creating a terminal, memory per idle terminal, and parse throughput.
//!
//! Usage: vtbench [FILE]   (FILE: bytes to parse, escape sequences included;
//!                          default: lib/std.msh, with \n sent as \r\n)

use ghostty_vt_sys as g;
use std::ptr;
use std::time::Instant;

const COLS: u16 = 100;
const ROWS: u16 = 30;

fn new_term() -> g::GhosttyTerminal {
    let mut t = ptr::null_mut();
    // SAFETY: valid out-pointer; NULL allocator = default.
    let r = unsafe { g::ghostty_terminal_new(ptr::null(), &mut t, COLS, ROWS) };
    assert!(r == g::GHOSTTY_SUCCESS, "terminal_new: {r}");
    t
}

fn write(t: g::GhosttyTerminal, b: &[u8]) {
    // SAFETY: t is live, b is a valid slice.
    unsafe { g::ghostty_terminal_vt_write(t, b.as_ptr(), b.len()) }
}

fn free(t: g::GhosttyTerminal) {
    // SAFETY: t was created by new_term and is freed once.
    unsafe { g::ghostty_terminal_free(t) }
}

fn rss_kb() -> u64 {
    let s = std::fs::read_to_string("/proc/self/status").unwrap();
    let line = s.lines().find(|l| l.starts_with("VmRSS:")).unwrap();
    line.split_whitespace().nth(1).unwrap().parse().unwrap()
}

fn median(mut v: Vec<f64>) -> f64 {
    v.sort_by(f64::total_cmp);
    v[v.len() / 2]
}

fn main() {
    let path = std::env::args().nth(1).unwrap_or_else(|| concat!(env!("CARGO_MANIFEST_DIR"), "/../../lib/std.msh").into());
    let raw = std::fs::read(&path).unwrap_or_else(|e| panic!("{path}: {e}"));
    let mut bytes = Vec::with_capacity(raw.len() * 2);
    for &b in &raw {
        if b == b'\n' {
            bytes.push(b'\r');
        }
        bytes.push(b);
    }
    // One screenful: what a finished short command leaves behind.
    let screen: Vec<u8> = bytes.split_inclusive(|&b| b == b'\n').take(ROWS as usize - 1).flatten().copied().collect();

    // 1. The first terminal in the process (cold: first allocations, page faults).
    let t0 = Instant::now();
    let first = new_term();
    let first_ns = t0.elapsed().as_nanos();
    free(first);

    // 2. Creating and freeing a terminal, warm.
    let mut create = Vec::new();
    let mut destroy = Vec::new();
    for _ in 0..2000 {
        let t0 = Instant::now();
        let t = new_term();
        create.push(t0.elapsed().as_nanos() as f64);
        let t0 = Instant::now();
        free(t);
        destroy.push(t0.elapsed().as_nanos() as f64);
    }
    println!("first terminal_new          {:8.1} us", first_ns as f64 / 1e3);
    println!("terminal_new (warm, median) {:8.1} us", median(create) / 1e3);
    println!("terminal_free (median)      {:8.1} us", median(destroy) / 1e3);

    // 3. Memory per idle terminal holding one screen of output.
    for n in [100usize, 1000] {
        let before = rss_kb();
        let terms: Vec<_> = (0..n)
            .map(|_| {
                let t = new_term();
                write(t, &screen);
                t
            })
            .collect();
        let after = rss_kb();
        println!(
            "RSS per terminal, {n:5} terminals with one {}x{} screen: {:7.1} KB",
            COLS,
            ROWS,
            (after - before) as f64 / n as f64
        );
        terms.into_iter().for_each(free);
    }

    // 4. Parse throughput: the input repeated to >= 64 MB, written in 64 KB chunks
    // (a typical PTY read), into one terminal with the default scrollback.
    let mut big = Vec::with_capacity(64 << 20);
    while big.len() < 64 << 20 {
        big.extend_from_slice(&bytes);
    }
    // The default scrollback is 10,000 bytes (one ~400 KB page in practice);
    // also run with room for everything, as a block keeping its whole output would.
    for (label, limit) in [("default scrollback", None), ("unlimited scrollback", Some(usize::MAX))] {
        let mut runs = Vec::new();
        let mut rss = 0;
        for _ in 0..5 {
            let t = new_term();
            if let Some(mut l) = limit {
                // SAFETY: the option takes a size_t*.
                let r = unsafe {
                    g::ghostty_terminal_set(t, g::GHOSTTY_TERMINAL_OPT_SCROLLBACK_MAX_BYTES, (&mut l as *mut usize).cast())
                };
                assert!(r == g::GHOSTTY_SUCCESS, "scrollback: {r}");
            }
            let before = rss_kb();
            let t0 = Instant::now();
            for chunk in big.chunks(64 << 10) {
                write(t, chunk);
            }
            runs.push(big.len() as f64 / t0.elapsed().as_secs_f64() / 1e6);
            rss = rss_kb().saturating_sub(before);
            free(t);
        }
        println!(
            "throughput, {label} ({:.0} MB of {}): median {:.0} MB/s, max {:.0}; RSS grew {:.0} MB",
            big.len() as f64 / 1e6,
            std::path::Path::new(&path).file_name().unwrap().to_string_lossy(),
            median(runs.clone()),
            runs.iter().cloned().fold(0.0, f64::max),
            rss as f64 / 1024.0
        );
    }
}
