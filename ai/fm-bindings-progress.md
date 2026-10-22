# File manager key bindings: implementation progress

Branch `fm-bindings`. Design: `ai/fm-bindings.typ`. Implemented 2026-10-22.

## Where things are

- `mshell/KeyResolver.go`: the resolver, factored out of `Bindings.go` and generic over the snapshot, the modes and the binding type:
  pending keys, snapshots, tries, timing, the conflict rule (`recomputeInert`, `addToKeymap`), `fireLongest`, `used`.
  The shell's tests pass unchanged on it.
- `mshell/FmBindings.go`: `FmOp` kinds, op parsing, mode tracking (`checkFmProgram`), `fmBindingEntry`, the default keymap, `fmBindingAdd/Erase/List`, `keyShort`, `fmOpsSource`.
- `mshell/FileManagerInput.go`: decode (the shell's lexer over `fm.input`), resolve, ops, the prompt line, pastes, questions, popup, help, terminal hand-over, `fmRun*`.
- `mshell/FileManager.go`: constructors (`RunFileManager` for `msh fm`, which loads the startup files; `RunFileManagerBuiltin(ev, context, definitions, dir)`), render, selection-aware clipboard, trash and yank.
  The three hand-written decoders, `lastKey`, `renderPrefixOverlay` and `RunFileManagerInteractive` are gone.
- `mshell/Main.go`: the Escape rule of section 10.2 in `InteractiveLexer`; `HandleToken` resolves Escape and then the key when Alt and the key is unbound;
  `StdinReaderState.read` lets a reader be fed other than from stdin (the file manager reads `os.Stdin.Read` without the shell's input gate, as before);
  CSI parameters are bounded at 256 bytes. The `ESC b`/`d`/`f`/`o`/`R`/`.` special cases went through the general rule.
- `mshell/FileManagerImage.go`: `detectTerminal` puts `CSI ? u` before the sixel queries.
- Tests: `FmBindings_test.go` (enum order, defaults in force, inert reasons, conflicts, every default key, popup, search, rename, selection, pastes, help, rollback, prompts, quit, trash, and `TestFmResolveMatchesDefinition`, the oracle over the prompt line, mutation-checked); lexer read-boundary tests in `Main_test.go`.
- Docs: "File manager" section in `doc/getting-started.inc.html`; the Escape paragraph of "Keys" rewritten; `functions.inc.html`, `type_system.inc.html`, `mshell.md`, `CHANGELOG.md`.
  `ai/cli_bindings.typ` section 10 and the legacy-bytes table record the new Escape rule.

## Choices the design left open

- A message in insert mode is shown on a row above the prompt line, so the line stays visible.
- A question that gets Escape and a key in one read takes it as Escape (no); the key is dropped.
- The quote of `fmPromptAsk` gets an `FmState` with empty `keys`.
- `fmCd` to something that is not a directory says `Not a directory: <path>`; a failed rename says why.
- The popup's title is the pending keys (` y `), not today's ` Yank `.
- Selected entries are marked with a magenta `*` in the space before the name; the top row shows `SELECTED n`.
- The help screen's bottom row shows `Help: lines a-b of n`, or a message.

## Checked by hand, in a pty (scratchpad harness)

`msh fm` with an init file: popup with user bindings, `g s`, search for `é`, Delete in rename, Space, a refused paste, help with an inert binding, `fmRunWait`, `Q` printing the start directory;
kitty protocol pushed and popped, `CSI 27 u` cancelling the line, `CSI 100;5 u`; `e` under `cd "$(msh fm)"` writes to the terminal, not the pipe;
Ctrl-O from the interactive shell, `q` changing the shell's directory; a lone Escape in the shell, then `x` typed; `exit 3` in `fmRun` ending `msh fm` with status 3.

Not checked: Windows (the deferred split-ESC question, section 15), tmux, a real sixel terminal.

## Remaining

- Use it in real terminals before merging; Windows console behaviour of a split ESC is deferred by decision.
- gofmt was not run (not allowed without asking).
