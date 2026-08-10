# Implementation blueprint

## Current failure

The current execution path resolves a child `cmd.Stdin`, but foreground control
later tests and operates on `os.Stdin`.  This fails when mshell is reading a pipe
and a child receives an explicitly opened terminal, as in `brename`:

1. the editor receives a terminal handle as its effective stdin;
2. mshell sees that its own inherited stdin is not a terminal and skips the
   foreground transfer;
3. the editor emits a terminal query (the observed OSC color sequence is an
   example) and exits or fails to consume the response correctly;
4. the response remains in the terminal input queue; and
5. mshell resumes its interactive parser and treats response bytes such as `;r`
   as user input, invoking the file-manager binding.

The escape parser is not the correct primary fix.  While an editor owns the
terminal, mshell must not read or interpret its input.  Parser hardening remains
defense in depth for input received while mshell genuinely owns the terminal.

The current code also discards errors from foreground transfer and restoration.
That makes recovery unverifiable: the in-memory control flow can claim a handoff
that the kernel rejected.

## Proposed architecture

Introduce one serialized `JobController`; do not spread process creation,
terminal modes, input reading, waiting, and recovery across `RunProcess`,
`RunPipeline`, the prompt, and platform helpers.

```text
resolved command and endpoints
            |
            v
       JobController
       /     |      \
 reader gate   process launcher   terminal backend
       |             |             /            \
 prompt lexer    process/job table       POSIX       Windows
```

Core types should make the proof state visible:

- `ResolvedEndpoint`: stream direction, concrete reader/writer, optional terminal
  identity, ownership/lifetime, and child inheritance information.
- `ResolvedStdio`: three endpoints plus aliases used by stream merging.
- `Job`: stable ID, process records, pipeline topology, state, placement,
  platform group/isolation identity, saved terminal mode, and completion state.
- `ControlTransaction`: the one job currently acquiring or releasing terminal
  control.  TLC found that this serialization cannot safely be implicit.
- `InputGate`: starts, quiesces, and resumes the shell input reader and confirms
  quiescence before a job is activated.
- `TerminalBackend`: transactional acquire/reclaim/save/restore operations with
  typed errors.  A failed acquire must run an explicit rollback.

Every transition should return an error that identifies both the failed action
and the recovery result.  No terminal-control error should be discarded.

## Endpoint resolution

Resolve all three child streams before process creation.  Terminal control is
based on the resolved controlling-terminal identity, not on file-descriptor
number 0 and not just on `os.Stdin`.

For the original case, an `*os.File` opened from `/dev/tty` is both the child's
stdin endpoint and a candidate controlling-terminal descriptor for
`tcgetpgrp`/`tcsetpgrp`.  If the child has terminal output but redirected input,
the job can still require foreground control for signals and output policy, so
the decision is a job property rather than a single `stdinIsTerminal` test.

Abstract `io.Reader`/`io.Writer` values without a descriptor are never guessed to
be terminals.  Wrappers that intentionally preserve terminal identity should
implement a private endpoint interface instead of relying on incidental Go type
assertions throughout the evaluator.

## POSIX backend

1. Establish whether mshell has a controlling terminal independently from its
   standard streams.  Interactive job control also requires mshell to be in its
   own process group and in the terminal foreground before reading a prompt.
2. Quiesce the shell reader and restore the shell's cooked baseline.
3. Create one process group per job.  Use child-side `Setpgid` before exec and a
   parent-side `setpgid`/verification where useful to close the documented race.
4. Launch all pipeline members into that group.  Track start failure per member;
   do not destroy the group when its leader exits early.
5. Save shell modes, call `tcsetpgrp` with a descriptor for the controlling
   terminal, check the result, restore the job's saved modes when continuing, and
   send `SIGCONT` only after foregrounding a stopped job.
6. Wait with stopped/continued status enabled and aggregate process state into
   job state.
7. On stop, exit, or error: save the job's modes if applicable, reclaim the
   foreground group, restore shell modes, then resume the shell reader.

Signal disposition must follow shell rules: the shell protects itself while it
manipulates the terminal, children receive default job-control dispositions, and
the protection is scoped and restored.

## Windows backends

Windows needs two explicit profiles because documented APIs cannot make a shared
console enforce exclusive input access for background jobs.

### Direct attached-console compatibility profile

- Quiesce the shell reader before process creation/activation and do not resume
  it until the child has stopped or exited.
- Save and restore console input/output modes because they are shared buffer
  state.
- Treat `CREATE_NEW_PROCESS_GROUP` only as a control-event routing mechanism.
  It is not terminal ownership, and it disables Ctrl+C for the created group.
- Document Ctrl+C versus Ctrl+Break behavior exactly; targeted Ctrl+C via
  `GenerateConsoleCtrlEvent` is not available.
- Do not claim enforceable background-input isolation.  A background process
  retaining the shared console input handle can consume the queue.

This profile can correctly fix the foreground editor handoff, but it is not the
foundation for full Windows job control.

### Isolated ConPTY job-control profile

- Give every terminal-using job its own ConPTY.  The child reads only its ConPTY
  input; mshell is the sole reader of the real console and forwards input only to
  the foreground job.
- Service ConPTY input and output on independent workers with bounded queues and
  cancellation.  Continue draining output through teardown as Microsoft
  requires.
- Restrict inherited handles with the extended startup handle list.  Close host
  copies of child-only channel ends immediately after creation on success and on
  every rollback path.
- Combine the ConPTY with a Windows Job Object for process-tree accounting and
  cleanup.  A Job Object is not the same concept as a shell job, so wrap it behind
  the platform backend.
- Define stop/continue semantics explicitly.  Windows exposes no documented
  equivalent of POSIX `SIGTSTP`/`SIGCONT` for arbitrary console trees; the first
  implementation may need to report stop/continue as unsupported rather than use
  undocumented process-suspension APIs and falsely promise correctness.

ConPTY changes presentation: mshell becomes a terminal relay and must forward
virtual-terminal bytes transparently.  It should recognize only the terminal
queries that mshell itself must answer as host; it must not reinterpret child
output as shell input.

## Testing strategy

- Unit-test every controller transition and injected backend failure.
- Generate transition traces from the TLA+ state graph and replay them against a
  fake backend.
- On POSIX, use a fresh session and pseudo-terminal for integration tests.  Cover
  piped shell stdin plus `/dev/tty`, early pipeline-leader exit, stop/continue,
  nested shells, terminal loss, and failed `tcsetpgrp`/mode restoration.
- On Windows, test both classic Console Host and Windows Terminal, redirected
  handles, Ctrl+C/Ctrl+Break, ConPTY resize, EOF, child-created descendants, and
  teardown with a full output buffer.
- Every interactive test must have a deadline and retain process/job handles so a
  hung tree can be terminated and diagnosed.  Never rely only on killing the
  immediate child.
- Add a byte-stream corpus for parser defense in depth, but keep it separate from
  ownership tests.  Escape-sequence coverage cannot prove terminal handoff.
