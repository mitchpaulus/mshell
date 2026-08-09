# Native Windows validation

The ConPTY APIs cannot be exercised by a Linux cross-build.
Run these checks from PowerShell or Windows Terminal with a real console
attached.

## Automated lifecycle check

```powershell
cd mshell
$env:MSHSTDLIB = (Resolve-Path ..\lib\std.msh)
go test -run '^TestWindowsConPTYLifecycle$' -v -timeout 30s
go test ./...
go build -o msh-terminal-control.exe
```

The lifecycle test launches its ConPTY case in a helper process.
The parent has a 15-second deadline and kills the helper if it hangs.
The helper's kill-on-close Job Object then terminates the nested command tree.

## Interactive checks

Run `msh-terminal-control.exe`, then verify each of these:

1. Start `nvim` with no redirected streams.
   Type text, use arrow/function keys, save, and exit.
   Confirm mshell accepts input only after Neovim exits.
2. Resize Windows Terminal while Neovim is open.
   Confirm its screen redraws at the new width and height.
3. Press Ctrl+C in a foreground `cmd.exe`, PowerShell, and a long-running native
   program.
   Confirm the child receives it and mshell survives.
4. Run the original `brename` piped-input reproducer.
   Confirm the editor receives terminal input while its data stream remains the
   pipeline, and the temporary file is read after the editor exits.
5. Exit a full-screen program after it changes console modes.
   Confirm echo, line editing, cursor visibility, and colors are restored.
6. Force-close a foreground program that has spawned a child process.
   Confirm neither process remains after the mshell command returns.

Record the Windows version (`winver`), terminal host, and Neovim version with
the result.
