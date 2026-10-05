# Checking the startup files' top-level code

Decided with Mitchell, 2026-10-04.

## What runs, and what is checked

The startup files are std.msh and init.msh, in that order.
Each has definitions, declarations and top-level code.

- Every definition body in them is checked on every run (the lazy base is gone).
  A broken definition is a warning for a script; a call to it is refused.
- Their top-level code is checked as the first lines of a type-checking session (`CoreSession`), one line per file, in load order, before it runs.
  A file whose top level does not check is not run, as a REPL line that does not check is not run.
  Its definitions stay; its errors are printed.
  A script then fails; the interactive shell starts without that file's top level.
- The script is checked as the next line of the same session: it starts from the stack and variables the startup files' top level left, with their types.
  The interactive shell's lines continue the same session.
  The language server checks a document the same way, after the startup files before it.
- `--type-check-only` checks the startup files' top level but does not run it.

## Why no new typing rule

The session's soundness argument (`TypeCoreSession.go`, and `formal-ver/Repl.v` for a line that stops with a runtime error) is that running lines one after another is running their concatenation, each checked from the state the lines before it left.
The runtime already runs std.msh's top level, then init.msh's, then the script, on one stack and one set of variables.
Checking them as consecutive session lines is that premise exactly; skipping a file whose top level does not check is the REPL's rule for a line that does not check.
So `formal-ver/` is unchanged.

One mismatch had to go: std.msh's top level ran with only std.msh's definitions, while the checker's base holds every startup file's.
The runtime now registers every startup file's definitions and declarations before it runs any top level, so both see the same names.
The shipped std.msh has no top-level code, so this only matters for an `MSHSTDLIB` that has some.
