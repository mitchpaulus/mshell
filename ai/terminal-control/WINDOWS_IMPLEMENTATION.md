# Windows terminal-process implementation

Last reviewed against Microsoft documentation: 2026-08-09.

## Foreground terminal profile

When stdin, stdout, and stderr all resolve to console handles, mshell creates a
fresh ConPTY for the command.
The launch transaction is:

1. reserve shell input and capture every affected console mode;
2. create synchronous ConPTY input and output channels;
3. create a kill-on-close Job Object;
4. create the child with `CREATE_SUSPENDED` and the
   `PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE` attribute;
5. assign the suspended child to the Job Object;
6. put the outer console into raw VT relay mode;
7. start independent input, output, and resize relays;
8. resume the child;
9. wait for the child, cancel and join the input relay, close the ConPTY while
   output continues draining, close the Job Object, restore console modes, and
   release shell input.

The relay does not interpret child escape sequences.
It passes the ConPTY UTF-8/VT byte stream to the surrounding terminal.
The only input transformation is Windows' documented console-to-VT conversion
provided by `ENABLE_VIRTUAL_TERMINAL_INPUT`.
The outer console input and output code pages are temporarily set to UTF-8 and
restored as part of the same terminal snapshot.

## Compatibility profile

ConPTY exposes one input channel and one merged output channel.
It therefore cannot preserve arbitrary standard-stream topology.
If any stream is redirected, captured, or connected to a pipeline, mshell uses
ordinary `os/exec` handles and the direct-console foreground transaction.

This distinction is intentional:

- full-terminal programs receive isolation, resize events, VT transport, and
  race-free process-tree ownership;
- pipelines and redirections retain exact byte streams and separate stdout and
  stderr;
- the shell input gate still prevents mshell from reading concurrently with a
  foreground direct-console child.

## Future full job control

The long-term requirement remains full foreground/background job control.
Before user-facing `jobs`, `fg`, and `bg` can ship on Windows, every process in a
pipeline or background job must be represented by a durable mshell job record
and contained by a Job Object.
Foreground ConPTY ownership must move with that record rather than being scoped
to one synchronous evaluator call.
Background jobs must never have an active relay from the shell's console input.

Windows does not provide a faithful equivalent of POSIX `SIGTSTP`/`SIGCONT` for
an arbitrary process tree.
Future stop/resume behavior must therefore either define a Windows-specific
cooperative contract or clearly report that operation as unsupported; it must
not use undocumented process suspension as though it were POSIX job control.
