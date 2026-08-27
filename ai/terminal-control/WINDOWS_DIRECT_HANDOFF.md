# Windows foreground handoff

Last reviewed: 2026-08-09.

## Chosen architecture

Synchronous foreground commands use direct console inheritance.
After standard streams and redirections have been resolved, mshell starts the
child with those exact handles, stops all of its own terminal reads, waits for
the child, restores the captured console modes, and only then resumes shell
input.

The surrounding console host remains directly connected to the child.
It—not mshell—handles keyboard input, output, escape sequences, Unicode, cursor
state, and terminal resize events.
There are no mshell input/output relays and no resize-forwarding worker.

## Why foreground ConPTY proxying was rejected

A nested ConPTY was prototyped for foreground commands.
That design placed mshell between the existing terminal host and every child:

```text
terminal host -> mshell console -> mshell relays -> nested ConPTY -> child
```

It required mshell to proxy input, output, encoding, resize events, cancellation,
and teardown.
Those responsibilities added failure modes without solving a foreground problem:
the synchronous shell can simply stop reading while the child uses the inherited
console directly.

## Future job control

Full job control remains the long-term requirement.
Windows has no POSIX foreground process-group gate, so an interactive background
job sharing the console could compete with mshell for input.
A per-job ConPTY and Job Object may be appropriate for that future isolation
case, but it must be introduced with durable job records and explicit `jobs`,
`fg`, and `bg` semantics.
It must not complicate the direct foreground path merely in anticipation of
those features.
