# Parallel Execution Design Notes

## Current State

- Lists represent external commands.
- Quotations are executable with `x`.
- `;`, `!`, and `?` are synchronous execution points today.
- `|` already creates a pipeline object, and pipeline stages already run concurrently in the evaluator.
- Redirection and stdout/stderr capture are configured on executable values before they are executed.
- I did not find a dedicated user-facing documentation section for `|`.
- Current pipeline behavior is mostly inferred from `tests/pipeline.msh` and `mshell/Evaluator.go`.

## Problem Statement

We need a way to run many independent commands in parallel.
This is different from a pipeline.
In a pipeline, data flows left to right through stdin/stdout.
In the new feature, the commands are siblings that happen at the same time.

## Goals

- Keep the postfix and stack-oriented style.
- Make the wait point explicit.
- Preserve deterministic result ordering.
- Avoid shared-stack and shared-variable races.
- Support bounded concurrency, not only "start everything at once".
- Stay portable across Linux, macOS, and Windows.

## Non-Goals For V1

- Full shell job control.
- Long-lived background jobs that survive interpreter exit.
- Concurrent execution of arbitrary quotations that mutate shared mshell state.
- Rich streaming mux behavior for combined stdout/stderr.

## Syntax Direction

We are using a word-based operator: `parallel`.

Basic form:

```msh
[
    [git fetch --all]
    [go test ./...]
    [./test.sh]
] parallel !
```

Bounded form:

```msh
[
    [cmd1]
    [cmd2]
    [cmd3]
    [cmd4]
] 4 parallel !
```

Recommended stack signature:

- `parallel ([executable] -- parallel-executable)`
- `parallel ([executable] int:maxJobs -- parallel-executable)`

This keeps the primary object first and lets the optional limit behave like other postfix functions that consume a trailing integer.

## Recommended Direction

The best v1 is a wait-all parallel group, not a full job system.

The new piece is only "convert this list of executables into a parallel executable".
Execution still happens through `;`, `!`, and `?`.

## Recommended Semantic Choices

### 1. Unit Of Concurrency

Recommendation:

- V1 should allow external-command lists and pipelines as children.
- Do not run quotations in parallel yet.

Reasoning:

- Quotations can touch the current stack, variables, and interpreter state.
- External commands and pipelines already have a much cleaner isolation boundary.
- A pipeline is already an executable object in the current evaluator, so allowing it as a child is conceptually natural.
- We can revisit quotations later with a clearer story for isolated stacks and copied variables.

### 2. Result Ordering

Recommendation:

- Preserve input order in all returned results.
- Do not return completion order by default.

Reasoning:

- Input order is deterministic.
- Completion order is harder to use in scripts.
- If we later want "first finished wins", that should be a separate primitive like `race` or `waitAny`.

### 3. Execution Suffixes

Recommendation:

- `parallel ;` means wait for all, ignore exit codes.
- `parallel !` means wait for all, then fail if any child exited non-zero.
- `parallel ?` means wait for all and push exit results in input order.

Example:

```msh
[
    [true]
    [false]
    [sh -c 'exit 7']
] parallel ?
```

Result:

```msh
[0 1 7]
```

This mirrors the existing `;`, `!`, and `?` model closely enough to be predictable.

### 4. Failure Behavior

Recommendation:

- V1 should be wait-all, not fail-fast.
- On `!`, all started children are still waited on before returning failure.
- For bounded concurrency, once a failure is seen, it is reasonable to stop launching queued work, but already-running work should still be waited on.

Reasoning:

- Wait-all is simpler and avoids orphaned subprocesses.
- It is easier to explain.
- Fail-fast cancellation is useful, but it is a separate design problem.

### 5. Concurrency Limit

Recommendation:

- Support a bounded form from the start.
- `parallel` should accept an optional positive integer limit.
- If no limit is given, the default should be the number of logical CPU threads, i.e. Go `runtime.NumCPU()`.

Reasoning:

- "Many processes in parallel" often really means "many, but not all at once".
- Unlimited fan-out is risky for CPU, memory, open files, and network requests.
- Retrofitting bounded scheduling later will change semantics more than adding it early.
- Using logical CPU threads is a portable default and aligns fairly well with how GNU Parallel documents its default.

GNU Parallel reference docs currently say `--jobs` defaults to `100%`, and define `100%` as one job per CPU thread.
The tutorial currently phrases that as the number of CPU cores.
The reference manual is more specific and also notes options to switch to sockets or physical cores instead.
For mshell, `runtime.NumCPU()` seems like the cleanest default.

### 6. Stdin Semantics

Recommendation:

- Do not implicitly share live parent stdin across parallel children.
- If a child needs stdin, it should set its own `<`.
- If we allow a group-level `<` later, it should only accept finite buffered inputs like string, binary, or path, and it should broadcast the same content to each child.

Reasoning:

- Multiple processes reading from the same terminal or stream is surprising and race-prone.
- Pipelines have a natural stdin/stdout topology.
- Parallel siblings do not.

This is one of the most important semantic choices.

### 7. Stdout/Stderr Semantics

Recommendation:

- Default behavior should inherit the parent's stdout/stderr, with the clear expectation that output may interleave.
- Per-child file redirection should work exactly as it does today.
- Group-level stdout/stderr capture should reuse the public `*`, `*b`, `^`, and `^b` operators and return lists in input order.

Reasoning:

- Inherited output is easy and useful for interactive progress.
- mshell already has a public capture model for stdout and stderr; parallel execution should not invent a second output model.
- Returning lists in input order preserves determinism and matches the existing stack contract well.

Proposed rule:

- `*` on a parallel executable captures each child stdout as a string, returning `[str]` in input order.
- `*b` on a parallel executable captures each child stdout as binary, returning `[binary]` in input order.
- `^` on a parallel executable captures each child stderr as a string, returning `[str]` in input order.
- `^b` on a parallel executable captures each child stderr as binary, returning `[binary]` in input order.
- If both stdout and stderr are captured, stdout is pushed first and stderr second, matching the existing documented rule.
- `?` still pushes the exit code list last.

This means the result shape stays parallel to the single-executable case:

- single executable: captured stream values are scalars, then optional exit code
- parallel executable: captured stream values are lists, then optional exit code list

There is no public line-splitting capture operator in the current docs, so parallel execution should not add one implicitly.

Examples:

```msh
[
    [echo "a"]
    [echo "b"]
] parallel * ?
```

Result:

```msh
["a\n" "b\n"] [0 0]
```

```msh
[
    [sh -c 'printf out1; printf err1 >&2']
    [sh -c 'printf out2; printf err2 >&2']
] parallel * ^ ?
```

Result:

```msh
["out1" "out2"] ["err1" "err2"] [0 0]
```

### 8. Redirection On The Group Itself

Decision:

- Group-level redirection should be allowed.

Proposed semantics:

- `>` and `>>` on the parallel executable redirect merged stdout from all children.
- `2>` and `2>>` redirect merged stderr from all children.
- `&>` and `&>>` redirect both to a shared file descriptor, just like a single executable.
- Output ordering is nondeterministic across children unless we add a future ordered-buffering mode.

This should be documented very clearly because it differs from the deterministic list ordering used for stack captures.

### 9. Nested Executables

Decision:

- A parallel group should allow any child executable that is already process-isolated enough for v1.
- In practice that means plain command lists and pipelines.
- Quotations are still excluded in v1.

That would make this possible:

```msh
[
    [[printf "a\nb\n"] [grep a]] |
    [[printf "x\ny\n"] [grep y]] |
] parallel !
```

## Minimal V1 Proposal

Syntax:

```msh
[ [cmd1] [cmd2] [cmd3] ] parallel !
[ [cmd1] [cmd2] [cmd3] ] parallel ?
[ [cmd1] [cmd2] [cmd3] ] 4 parallel !
```

Semantics:

- Inputs are executable external-command values, including pipelines.
- All results are reported in input order.
- `;`, `!`, and `?` keep their familiar meanings.
- `?` returns `[int]` exit codes when there are no capture operators.
- `*`, `*b`, `^`, and `^b` on a parallel executable return lists in input order, with element types determined by the selected capture mode.
- No quotation parallelism yet.
- No shared live stdin.
- Group-level redirection is allowed and merges child output streams.
- Per-child redirections still work.
- Default max concurrency is logical CPU threads.

## Likely Phase 2

- Rich per-child result objects.
- Fail-fast mode with cancellation.
- `waitAny` or `race`.
- Async handles from `spawn`/`wait`.
- Ordered or line-buffered merged output modes.
- Possibly isolated quotation parallelism if there is a convincing state model.

## Questions To Decide Together

1. Should the optional max-jobs limit be `list int parallel`, or should we prefer a dictionary/options object later?
2. For group-level redirection, do we want bare interleaving only, or also a future ordered buffering mode similar to GNU Parallel `-k`?
3. Do we want the default concurrency to be logical CPU threads exactly, or leave room for a future "physical cores" mode?
