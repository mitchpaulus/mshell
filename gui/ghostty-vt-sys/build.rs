//! Links libghostty-vt statically and generates bindings from its headers.
//!
//! The library comes from `GHOSTTY_VT_PREFIX` (an install prefix holding
//! `include/ghostty/vt.h` and `lib/libghostty-vt.a`), or by default from
//! `gui/third_party/libghostty-vt-1.0.0`, which `fetch.sh` builds.

use std::env;
use std::path::PathBuf;

fn main() {
    println!("cargo:rerun-if-env-changed=GHOSTTY_VT_PREFIX");
    let prefix = env::var_os("GHOSTTY_VT_PREFIX").map(PathBuf::from).unwrap_or_else(|| {
        PathBuf::from(env::var("CARGO_MANIFEST_DIR").unwrap()).join("../third_party/libghostty-vt-1.0.0")
    });
    let include = prefix.join("include");
    let lib = prefix.join("lib");
    let header = include.join("ghostty/vt.h");
    let archive = lib.join("libghostty-vt.a");
    if !header.exists() || !archive.exists() {
        panic!(
            "libghostty-vt not found in {}: run gui/ghostty-vt-sys/fetch.sh, or set GHOSTTY_VT_PREFIX",
            prefix.display()
        );
    }
    println!("cargo:rerun-if-changed={}", archive.display());
    println!("cargo:rerun-if-changed={}", include.join("ghostty").display());

    // Static, so the GUI stays one binary and nothing is dlopen'd at startup.
    println!("cargo:rustc-link-search=native={}", lib.display());
    println!("cargo:rustc-link-lib=static=ghostty-vt");

    let bindings = bindgen::Builder::default()
        .header(header.to_str().unwrap())
        .clang_arg(format!("-I{}", include.display()))
        .allowlist_function("ghostty_.*")
        .allowlist_type("Ghostty.*")
        .allowlist_var("GHOSTTY_.*")
        // C enums as plain constants: GHOSTTY_SUCCESS, not GhosttyResult_GHOSTTY_SUCCESS.
        .default_enum_style(bindgen::EnumVariation::Consts)
        .prepend_enum_name(false)
        .derive_default(true)
        .layout_tests(false)
        .generate()
        .expect("bindgen on ghostty/vt.h");
    let out = PathBuf::from(env::var("OUT_DIR").unwrap()).join("bindings.rs");
    bindings.write_to_file(out).expect("writing bindings");
}
