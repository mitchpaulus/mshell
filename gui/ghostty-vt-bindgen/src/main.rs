//! Writes `ghostty-vt-sys/src/bindings.rs` from libghostty-vt's `ghostty/vt.h`.
//!
//! The bindings are committed, so building the GUI needs no libclang.
//! Run this only when the libghostty-vt version changes:
//!
//! ```text
//! cargo run -p ghostty-vt-bindgen -- [--check] [DIR]
//! ```
//!
//! `DIR` holds `include/ghostty/vt.h`: an install prefix, or the unpacked
//! source tarball (`ghostty-vt-sys/fetch.sh --source` prints its path).
//! It defaults to `gui/third_party/libghostty-vt-1.0.0`, where `fetch.sh`
//! installs. Running this needs libclang with its built-in headers
//! (on Windows, LLVM in `C:\Program Files\LLVM` or `LIBCLANG_PATH`).
//!
//! One file serves every platform. The headers include only freestanding C
//! headers, and the bindings come out the same for every 64-bit target but
//! one way: MSVC gives C enums the type `int` where others give
//! `unsigned int` (when no value is negative). Both are 32 bits holding the
//! same non-negative values, passed and stored identically, so the file is
//! generated for x86_64 Linux on every host, and `--check` confirms the
//! MSVC output differs only that way.
//!
//! `--check` writes nothing. It fails if `bindings.rs` isn't what this
//! would write, or if the MSVC bindings differ in anything but enum types.

use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::ExitCode;

const TARGET: &str = "x86_64-unknown-linux-gnu";
const MSVC: &str = "x86_64-pc-windows-msvc";

fn main() -> ExitCode {
    let gui = Path::new(env!("CARGO_MANIFEST_DIR")).parent().unwrap().to_path_buf();
    let mut check = false;
    let mut dir = None;
    for arg in env::args_os().skip(1) {
        if arg == "--check" {
            check = true;
        } else if dir.is_none() {
            dir = Some(PathBuf::from(arg));
        } else {
            eprintln!("usage: ghostty-vt-bindgen [--check] [DIR]");
            return ExitCode::FAILURE;
        }
    }
    let dir = dir.unwrap_or_else(|| gui.join("third_party/libghostty-vt-1.0.0"));
    let include = dir.join("include");
    if !include.join("ghostty/vt.h").exists() {
        eprintln!("ghostty-vt-bindgen: {} not found", include.join("ghostty/vt.h").display());
        return ExitCode::FAILURE;
    }
    let out = gui.join("ghostty-vt-sys/src/bindings.rs");
    let bindings = generate(&include, TARGET);

    if !check {
        fs::write(&out, &bindings).expect("writing bindings");
        println!("wrote {}", out.display());
        return ExitCode::SUCCESS;
    }

    let mut ok = true;
    let committed = fs::read_to_string(&out).unwrap_or_default();
    if committed != bindings {
        eprintln!("{} is out of date: run `cargo run -p ghostty-vt-bindgen` and commit it", out.display());
        ok = false;
    }
    let msvc = generate(&include, MSVC);
    if let Some((line, ours, theirs)) = first_difference(&enum_types_unsigned(&bindings), &enum_types_unsigned(&msvc)) {
        eprintln!("the {MSVC} bindings differ in more than C enum types, so one file no longer serves Windows.");
        eprintln!("line {line}:\n  {TARGET}: {ours}\n  {MSVC}: {theirs}");
        ok = false;
    }
    if ok {
        println!("bindings.rs is up to date and serves {MSVC}");
        ExitCode::SUCCESS
    } else {
        ExitCode::FAILURE
    }
}

fn generate(include: &Path, target: &str) -> String {
    bindgen::Builder::default()
        .header(include.join("ghostty/vt.h").to_str().unwrap())
        .clang_arg(format!("-I{}", include.display()))
        // The same output on any host: a fixed target, and clang's own
        // headers rather than the host C library's.
        .clang_arg(format!("--target={target}"))
        .clang_arg("-ffreestanding")
        .allowlist_function("ghostty_.*")
        .allowlist_type("Ghostty.*")
        .allowlist_var("GHOSTTY_.*")
        // C enums as plain constants: GHOSTTY_SUCCESS, not GhosttyResult_GHOSTTY_SUCCESS.
        .default_enum_style(bindgen::EnumVariation::Consts)
        .prepend_enum_name(false)
        .derive_default(true)
        .layout_tests(false)
        .generate()
        .unwrap_or_else(|e| panic!("bindgen on ghostty/vt.h for {target}: {e}"))
        .to_string()
}

/// The bindings with every `pub type X = c_int;` written as `c_uint`, so
/// the two targets' enum types compare equal.
fn enum_types_unsigned(bindings: &str) -> Vec<String> {
    bindings
        .lines()
        .map(|l| {
            if l.starts_with("pub type ") && l.ends_with(" = ::std::os::raw::c_int;") {
                l.replace("c_int;", "c_uint;")
            } else {
                l.to_string()
            }
        })
        .collect()
}

fn first_difference(a: &[String], b: &[String]) -> Option<(usize, String, String)> {
    let n = a.len().max(b.len());
    (0..n).find_map(|i| {
        let (x, y) = (a.get(i).map_or("", |s| s), b.get(i).map_or("", |s| s));
        (x != y).then(|| (i + 1, x.to_string(), y.to_string()))
    })
}
