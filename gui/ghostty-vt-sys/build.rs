//! Links libghostty-vt statically.
//!
//! The library comes from `GHOSTTY_VT_PREFIX` (an install prefix holding
//! `lib/libghostty-vt.a`, or `lib/ghostty-vt-static.lib` on Windows), or by
//! default from `gui/third_party/libghostty-vt-1.0.0-{target os}`, which
//! `fetch.msh` builds. (On Windows `ghostty-vt.lib` is the DLL's import
//! library, not the static one.)
//! The bindings are committed in `src/bindings.rs`, written by
//! `ghostty-vt-bindgen`, so building needs no libclang.

use std::env;
use std::path::PathBuf;

fn main() {
    println!("cargo:rerun-if-env-changed=GHOSTTY_VT_PREFIX");
    let os = env::var("CARGO_CFG_TARGET_OS").unwrap();
    let prefix = env::var_os("GHOSTTY_VT_PREFIX").map(PathBuf::from).unwrap_or_else(|| {
        PathBuf::from(env::var("CARGO_MANIFEST_DIR").unwrap()).join(format!("../third_party/libghostty-vt-1.0.0-{os}"))
    });
    let lib = prefix.join("lib");
    let (name, file) = if os == "windows" {
        ("ghostty-vt-static", "ghostty-vt-static.lib")
    } else {
        ("ghostty-vt", "libghostty-vt.a")
    };
    let archive = lib.join(file);
    if !archive.exists() {
        panic!(
            "libghostty-vt not found at {}: run `msh ghostty-vt-sys/fetch.msh` in gui/, or set GHOSTTY_VT_PREFIX",
            archive.display()
        );
    }
    println!("cargo:rerun-if-changed={}", archive.display());

    // Static, so the GUI stays one binary and nothing is dlopen'd at startup.
    println!("cargo:rustc-link-search=native={}", lib.display());
    println!("cargo:rustc-link-lib=static={name}");
}
