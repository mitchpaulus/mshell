# Key bindings and runes: implementation progress

Branch `cli-bindings-design`. Design: `ai/cli_bindings.typ` (bindings), `ai/runes.typ` (runes).
Everything stays on this branch, including the full implementation (Mitchell, 2026-10-18).

## Decisions made along the way (all recorded in the design docs)

- Keys are typed: `type Key = rune | NamedKey | ModifiedKey`. F keys are enumerated `keyF1`..`keyF35`; no runtime errors on key values.
- Runes: `r'a'` and `U+XXXX`; `r'…'` holds one visible code point (controls, `'`, marks via `U+`). Space prints as `r' '`. No `runesStr`.
- Shift on a non-letter rune (`r';' keyShift`) is valid and never fires; `msh keys` is the debugging tool.
- Two fixed modes, `modeInsert` and `modeComplete` (an enum). No user modes, no vi mode: `editInEditor` (Ctrl-G) hands the line to `$EDITOR`.
- `editRun ( -- )` keeps the line; `editRunOps (EditState -- Maybe[[EditOp]])` runs the returned ops.
- Ctrl-C, Ctrl-D, Alt-O exit; Ctrl-O file manager; Alt-D is personal (not a default); no default timing.
- Conflict rule (found by the TLA+ model): an effect binding that *ends a proper prefix* of another binding conflicts with it (newer wins). No overlap waiting.
- **No timers, polls, goroutines or signals** (Mitchell, 2026-10-18): `idleAfter` was removed; nothing happens unless a key arrives.
  Escape stays a blocking read of the next byte: ESC+key is Alt+key when that is bound, otherwise the key alone (`KeyToken.EscOf`).
  A lone Escape is a key only under the kitty protocol (`CSI 27 u`). Kitty detection blocks until the DA1 reply, like the cursor query.

## Done and committed

1. Runes: Rocq model (`formal-ver`, `make check` closed), lexer, type, builtins (`runes`, `toRune`, `toInt`), docs, grammars, tests.
2. TLA+ model `ai/key-bindings/KeyResolution.tla` + `check.sh` (design passes; NoConflicts and NoSnapshot variants fail as required). `idleAfter` removed from it.
3. Go implementation (this commit):
   - `mshell/Keys.go`: `KeyCode` (21-bit base + modifier bits 24..27), normalization, Key value <-> code, `keySource`, legacy token mapping, `decodeCsiKey` (modified legacy keys and kitty `CSI … u`).
   - `mshell/Bindings.go`: `EditOp` kinds, `bindingEntry`, `parseBinding`, conflict rule + `recomputeBindings` (inert entries warn via `AbbrWarnings`), per-mode tries, `bindingAdd/Erase/List`, default keymap (Go table), resolver `resolveKey` (pending window, snapshots, pure bindings in force, `within`/`idleBefore`), op runner, `editRun`/`editRunOps` (`runBindingQuote`).
   - `Main.go`: `HandleToken` is now paste-or-resolve; old branches became ops (`historyPrev/Next`, `completeAtCursor`). Lexer decodes Alt+any key, modified keys, F1/F2/F4 (`CSI P/Q/S`), kitty keys.
   - `TerminalMode.go`: kitty protocol detection (`detectKittyKeys`, `MSH_KITTY_KEYS=0` turns it off), push `CSI > 5 u` in `enterRawMode`, pop in `leaveRawMode`.
   - `KeysCommand.go`: `msh keys`.
   - `lib/std.msh`: `editInEditor`.
   - Types in `builtinDeclSource` (Prompt.go): NamedKey, ModifiedKey, Key, EditMode, EditState, EditOp, BindingAction, Binding.
   - Tests: `Bindings_test.go` (enum order, defaults in force, key round trip, CSI decoding, ESC+unbound key, and `TestResolveMatchesDefinition`, the random oracle; mutation-checked), `tests/success/bindings.msh`, `tests/typecheck_fail/binding_*.msh`.
   - Verified by hand in a pty (`/tmp/.../scratchpad/fake_term.py` style harness): `jf` accept with rollback, `;r` editRun keeping the line, Ctrl-X prefix display, `msh keys` in legacy and kitty modes.

## State of the tests

- `tests/test.sh`: all pass. `tests/typecheck_test.sh`: 501 pass.
- `go test`: all pass **except `TestPromptBuiltinsDocumented`**, which needs the user docs (next step).
  It requires `doc/mshell.md` to name each new enum in backticks and `doc/getting-started.inc.html` to show every member in `<code>member</code>` (all 49 NamedKey members, all EditOp members, etc.).

## Remaining work

1. **User docs** (makes the failing test pass):
   - `doc/getting-started.inc.html`: a "Key bindings" section after "Abbreviations": the Binding record, Key values, every EditOp, EditMode, NamedKey (all F keys), ModifiedKey, the default keymap table, `msh keys`, `MSH_KITTY_KEYS`, the conflict rule, `within`/`idleBefore`, `editInEditor`. Generate code blocks with `msh --html`.
   - `doc/functions.inc.html`: `bindingAdd`, `bindingErase`, `bindingList`, `editInEditor`.
   - `doc/type_system.inc.html`: the new type names; `doc/mshell.md`: name the enums (agent doc, keep interactive detail minimal); replace the key list in `mshell.md` (~line 430) with a pointer.
   - Rebuild: `cd doc && msh build.msh`.
2. **CHANGELOG** (Unreleased/Added): key bindings, `msh keys`, kitty protocol; Functions: `bindingAdd`, `bindingErase`, `bindingList`, `editInEditor`. Changed: the hard-coded `jf`, `vl`, `qq`, `;r`, `;j`, F1, Alt-D bindings are gone (move to init.msh); Escape followed by an unbound key no longer loses... (it acts as the key, as before).
3. **Shell completions** for `msh keys`: add `'keys'` to `subcommands` in `__mshCompletion` (lib/std.msh), and to the fish/bash/nushell/elvish generators if `runCompletionsCommand` lists subcommands.
4. **Mitchell's init.msh** (outside the repo, `~/dotfiles/.config/msh/init.msh`): tell him to add the snippet from `ai/cli_bindings.typ` section 11 (jf, vl, qq, ;r, ;j, F1 → lf, Alt-D → date). Do not edit his dotfiles without asking.
5. **Design doc sync**: section 10 still says the push/pop "belong in the ai/terminal-control model"; it is tied to `enterRawMode`/`leaveRawMode` instead. Decide whether to extend that TLA+ model or just state the pairing.
6. Optional: VS Code grammar for the `key…`/`edit…` names is not needed (they are ordinary words).
7. Review: `go vet` is clean apart from the pre-existing `UnreadByte` signature warning (Main.go). `TestKeyPathAllocations` passes (typing stays allocation-free).

## Useful commands

- Build: `cd mshell && ./build.sh` (tests use `mshell/mshell`).
- Tests: `cd tests && ./test.sh && ./typecheck_test.sh`; `cd mshell && go test -count=1 -timeout 300s .`
- Model: `ai/key-bindings/check.sh`; Rocq: `cd formal-ver && make check`.
- Remember: ntfy at the end of every turn; no gofmt without permission; commit only when asked (Mitchell asked for commits on this branch).
