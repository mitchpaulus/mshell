Here are the design goals of modules with respect to mshell:

- Large, complete, set of built ins and standard library such that most one-off scripts do not need any module statements.
- Modules are *content-addressed* by hash. This is for de-duping, caching, and security.
- The *File* is the unit of identity.
- The hash is *required* to run.


┌────────────────────────┬─────────────────────────────────────────────────────┬──────────────────────────────────────────────────────┐
│    Unit of identity    │                      Examples                       │                     Granularity                      │
├────────────────────────┼─────────────────────────────────────────────────────┼──────────────────────────────────────────────────────┤
│ Definition             │ Unison                                              │ finest — every function is its own addressable thing │
├────────────────────────┼─────────────────────────────────────────────────────┼──────────────────────────────────────────────────────┤
│ File ← your choice     │ Deno, Node ESM, Zig, Lua, OCaml (.ml = a structure) │ natural human-sized unit                             │
├────────────────────────┼─────────────────────────────────────────────────────┼──────────────────────────────────────────────────────┤
│ Directory / collection │ Go, Rust crate, Odin, Python package                │ coarsest — many files, one namespace                 │
└────────────────────────┴─────────────────────────────────────────────────────┴──────────────────────────────────────────────────────┘

To get a hash, we will provide CLI tools.
Because it is so simple, we should also empower our users to understand they can just copy/paste and hash the file if desired.

```
msh add "gh:mitchpaulus/mylibrary" myscript.msh # Updates the myscript.msh in place.
msh lookup "gh:mitchpaulus/mylibrary" # Prints out the line to stdout
```

So mshell will closely align with Deno, where a library is a `mod.ts` file that re-exports.
