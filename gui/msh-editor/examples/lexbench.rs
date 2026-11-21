//! Lexing speed on the keystroke path: each case of a lexcases.py file,
//! lexed with the editor's settings into a reused Vec, many times.
//! Usage: cargo run --release --example lexbench CASES

use msh_editor::lexer::Lexer;
use std::time::Instant;

fn main() {
    let path = std::env::args().nth(1).expect("CASES");
    let data = std::fs::read(path).unwrap();
    let mut cases = Vec::new();
    let mut rest = &data[..];
    while !rest.is_empty() {
        let n = u32::from_le_bytes(rest[..4].try_into().unwrap()) as usize;
        cases.push(std::str::from_utf8(&rest[4..4 + n]).unwrap());
        rest = &rest[4 + n..];
    }
    // Lines as typed: single lines of at most 200 bytes.
    let lines: Vec<&str> = cases.iter().copied().filter(|c| !c.contains('\n') && c.len() <= 200).collect();
    let bytes: usize = lines.iter().map(|l| l.len()).sum();
    let mut lexer = Lexer::editor();
    let mut tokens = Vec::with_capacity(256);
    let mut best = f64::MAX;
    for _ in 0..5 {
        let t = Instant::now();
        let mut n = 0;
        for line in &lines {
            lexer.tokenize_into(line, &mut tokens);
            n += tokens.len();
        }
        std::hint::black_box(n);
        best = best.min(t.elapsed().as_secs_f64());
    }
    println!(
        "{} lines, mean {:.0} bytes: {:.0} ns per line, {:.0} MB/s (best of 5)",
        lines.len(),
        bytes as f64 / lines.len() as f64,
        best / lines.len() as f64 * 1e9,
        bytes as f64 / best / 1e6
    );
}
