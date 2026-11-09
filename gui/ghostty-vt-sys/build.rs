//! Links libghostty-vt statically.
//!
//! The library comes from `GHOSTTY_VT_PREFIX` (an install prefix holding
//! `lib/libghostty-vt.a`), or by default from
//! `gui/third_party/libghostty-vt-1.0.0`, which `fetch.sh` builds.
//! The bindings are committed in `src/bindings.rs`, written by
//! `ghostty-vt-bindgen`, so building needs no libclang.

use std::env;
use std::path::PathBuf;

fn main() {
    println!("cargo:rerun-if-env-changed=GHOSTTY_VT_PREFIX");
    let prefix = env::var_os("GHOSTTY_VT_PREFIX").map(PathBuf::from).unwrap_or_else(|| {
        PathBuf::from(env::var("CARGO_MANIFEST_DIR").unwrap()).join("../third_party/libghostty-vt-1.0.0")
    });
    let lib = prefix.join("lib");
    let archive = lib.join("libghostty-vt.a");
    if !archive.exists() {
        panic!(
            "libghostty-vt not found at {}: run gui/ghostty-vt-sys/fetch.sh, or set GHOSTTY_VT_PREFIX",
            archive.display()
        );
    }
    println!("cargo:rerun-if-changed={}", archive.display());

    // Static, so the GUI stays one binary and nothing is dlopen'd at startup.
    println!("cargo:rustc-link-search=native={}", lib.display());
    println!("cargo:rustc-link-lib=static=ghostty-vt");
}
