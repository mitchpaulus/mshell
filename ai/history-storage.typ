// Design: storage for interactive command history.
// Build: typst compile ai/history-storage.typ

#set document(title: "History storage in mshell", author: "Claude (draft for Mitchell)")
#set page(paper: "us-letter", margin: (x: 1in, y: 1in), numbering: "1")
#set text(font: "Libertinus Serif", size: 11pt)
#set par(justify: true, leading: 0.62em)
#show table: set par(justify: false)
#show table: set text(size: 9.5pt)
#set heading(numbering: "1.1")
#show heading.where(level: 1): it => { v(0.8em); it; v(0.3em) }
#show raw: set text(font: "DejaVu Sans Mono", size: 0.86em)
#show raw.where(block: true): it => block(
  fill: luma(245), inset: 8pt, radius: 3pt, width: 100%, it,
)
#show link: underline

#let callout(kind, body) = block(
  width: 100%, inset: (left: 10pt, rest: 8pt), stroke: (left: 2pt + luma(120)),
  fill: luma(250),
  [*#kind.* #body],
)
#let decision(body) = callout("Decision", body)
#let note(body) = callout("Note", body)
#let open(body) = callout("Open", body)
#let rule(id, body) = block(
  width: 100%, inset: (left: 10pt, rest: 6pt), stroke: (left: 2pt + luma(60)),
  [*#id.* #body],
)

#align(center)[
  #text(size: 20pt, weight: "bold")[History storage in mshell]
  #v(0.3em)
  #text(size: 11pt)[Design draft, 2026-11-15, revised the same day (locking and Windows), branch `history-design`]
  #v(0.2em)
  #text(size: 10pt, style: "italic")[Written by Claude from the code on `main` (a3774d7), aggregate counts
  from Mitchell's own history files, the Go 1.27 runtime source, and a prototype of the lock protocol in `ai/history-lock-prototype`.
  No command text was read out of the history files.]
]

#v(1em)

= Summary

The interactive shell records every submitted line with its time and directory, but it stores them in three files,
writes them only at exit, and never reads the directory back.
This design replaces the three files with *one append-only log*.

- Each record is self-contained and goes to disk in a single `write()`, at the moment the line is submitted.
- One goroutine in each shell does all history file work, so a slow or stuck disk can never hold up the prompt.
- Every access to the log happens under one advisory file lock, so there is one writer at a time without a server process.
  The lock is never waited on without a limit, and every way of failing to get it has a defined result (@locking).
- A record carries the time, session, directory, command, the file arguments that existed, and later the exit status and duration.
- Each shell keeps an in-memory index and picks up other sessions' records by reading the new bytes at each prompt.
- Deleting a command rewrites the file; nothing else ever needs to.

At the measured rate of 18.5 commands a day the log grows by about 0.6 MB a year,
so there is no index cache, no size cap and no automatic compaction in this design.

The stored directory and paths are what make fish-style suggestions possible: prefer a command that was run in this directory,
and do not suggest a `cd` or a file argument that does not exist here (@using).

The lock protocol was built as a prototype and run with many processes on Linux (tmpfs and ext4) and, as a Windows build, under Wine.
It has not been run on Windows itself. @windows says what is expected there and why, and @checked says what is still unproven.

= What exists today

== Capture and storage

`ExecuteCurrentCommand` (`mshell/Main.go`) trims the line and appends a `HistoryItem` of time, command and `os.Getwd()` to `historyToSave`.
The directory is read before the line runs, so `cd foo` is recorded with the directory it was typed in.

`saveHistory` runs once, deferred to shell exit, with a two second timeout. It appends to three files in the history directory
(`$XDG_DATA_HOME/msh` or `~/.local/share/msh`; `%LOCALAPPDATA%\msh` on Windows):

#table(
  columns: (auto, 1fr),
  inset: 4pt,
  table.header([*File*], [*Contents*]),
  [`msh_history`], [24-byte records: time, xxHash64 of the command, xxHash64 of the directory, big-endian],
  [`msh_commands`], [one command per line, a newline written as `\n`, appended every time],
  [`msh_dirs`], [one directory per line, appended every time],
)

`ReadHistory` (`mshell/History.go`) loads all three at startup and joins them by hash.

== Use

The ghost suggestion is the most recent command that starts with the typed text (`SearchHistory`).
Up, Down, Ctrl-P and Ctrl-N walk a plain list of command strings.
The directory is stored and loaded, and nothing reads it.
No suggestion is checked against the file system.

== Measurements <measurements>

From the files in `~/.local/share/msh` on 2026-11-15:

#table(
  columns: (1fr, auto),
  inset: 4pt,
  table.header([*Quantity*], [*Value*]),
  [Records in `msh_history`], [9,332],
  [Lines in `msh_commands` / `msh_dirs`], [9,326 / 9,320],
  [Unique commands / unique directories], [1,470 / 316],
  [`msh_commands` size, total / unique], [121 KB / 36 KB],
  [`msh_dirs` size, total / unique], [344 KB / 15 KB],
  [Mean command length / mean directory length], [13 bytes / 37 bytes],
  [Time span], [2025-06-28 to 2026-11-15, 504 days, 18.5 commands a day],
  [Adjacent records with a decreasing timestamp], [278],
  [`cd` commands / with a relative target], [340 / 312],
  [Unique commands only ever run in one directory], [1,244 of 1,470],
  [Records whose command or directory fails the hash lookup], [0],
)

== Problems <problems>

+ *One record is three writes.* The three files disagree in length (9,332, 9,326 and 9,320).
  Nothing was lost only because every missing line also appears elsewhere in the same file.
+ *History is written only at exit.* A crash or a killed terminal loses the session, and sessions never see each other's commands.
+ *File order is the order sessions exited.* That is the cause of the 278 out-of-order timestamps, and "most recent" means file order.
+ *Lines with a newline are lost on reload.* The save hashes the real command and writes it escaped; the load hashes the escaped line.
  A pasted multi-line command reaches this path. There are none in the measured files.
+ *A failed lookup leaves a blank entry.* `ReadHistory` skips the record but keeps its zero value in the list.
+ *Nothing is deduplicated.* `msh_dirs` is 23 times the size of its unique content, and all of it is hashed at startup.
+ *A long line can cut the load short.* `ReadHashFile` uses a default `bufio.Scanner` (64 KB a line) and does not check its error.
+ *Dead code.* `WriteToHistory` in `Main.go` is an older SHA-256 form with no callers.

There are no tests for the read or save path, and no locking of any kind.

= Prior art

#note[This table is from memory. It was not rechecked against the sources for this draft, so treat the details as approximate.]

#table(
  columns: (auto, 1fr, 1fr, 1fr),
  inset: 4pt,
  table.header([], [*Store*], [*Per record*], [*Many sessions*]),
  [bash], [text file], [command, optional time], [written at exit; without `histappend` the last shell to exit wins],
  [zsh], [text file], [command; start time and duration with `EXTENDED_HISTORY`], [`INC_APPEND_HISTORY` and `SHARE_HISTORY`, under a lock file],
  [fish], [one text file of small records], [command, time, the paths that were valid], [appends, and rewrites under a file lock; other sessions are seen after `history merge`],
  [Nushell], [text file or SQLite], [command; more with SQLite], [SQLite does the locking],
  [Atuin], [SQLite, optional daemon and sync server], [command, directory, exit, duration, session, host], [SQLite; the server is for syncing machines],
)

Fish is the model for suggestions: it does not suggest a `cd` to a directory that does not exist from here,
and it does not suggest a command whose recorded paths are gone.
Fish does not store the directory a command ran in; mshell already does.

= Principles

- *One record, one write.* A record never depends on a second write having happened.
- *The log is the only truth.* Everything else is derived from it and can be thrown away.
- *No process to look after.* Nothing to start, stop, upgrade or find a socket for.
- *The prompt never waits on the disk.* History files may sit on a slow or stuck mount.
- *Nothing waits without a limit.* Every wait for the lock has a budget and a defined result when it runs out.
- *Every limit degrades.* A full queue, an oversized line, a busy lock or a torn record costs that record or that operation, not the history.
- *Corruption is local.* A bad record is skipped; the records after it are still read.
- *One code path.* Linux, macOS and Windows run the same steps. Only the two lock calls and the list of retryable errors differ.

= Why not a server

A history server would give one writer, and with it atomic writes. The log gets the same from the kernel and a lock:

- A `write()` to a file opened for appending goes to the end of the file as one step.
  On a local file system two processes' records do not interleave.
- An advisory lock held for the length of that write makes it true on network mounts too, and keeps writers out during a rewrite.
  The kernel releases the lock when a process dies, so there is no stale lock to clean up.

A server would add what the principles rule out: starting and stopping it, a stale socket, a running server that is older than the `msh` binary,
different IPC on Windows, and a socket to secure.
It would not remove the direct path, because a shell must still save history when the server is down.

#decision[No server. A server becomes worth its cost only for syncing history between machines, which is out of scope here.]

= The design <design>

== Files

#table(
  columns: (auto, 1fr),
  inset: 4pt,
  table.header([*File*], [*Purpose*]),
  [`msh_history.v2`], [The log. A 16-byte header and then records.],
  [`msh_history.v2.lock`], [The lock file. Always empty. Never deleted, renamed or written.],
  [`msh_history.v2.tmp`], [Exists only during a rewrite, and after a crash in the middle of one. Never read.],
)

All three are created through the existing `openHistoryFile`, which on Linux and macOS keeps the owner, symlink and hard-link checks and mode 0600,
and on Windows relies on the access list inherited from `%LOCALAPPDATA%`.
The three old files are left where they are (@migration).

The header is the magic `MSHH`, a `u16` format version, two reserved bytes, and a `u64` *generation*: a random number chosen each time the file is created or rewritten.
A file whose first 16 bytes are not a header is read from its first byte, with generation 0.

== Record frame

All integers are little-endian.

#table(
  columns: (auto, auto, 1fr),
  inset: 4pt,
  table.header([*Field*], [*Bytes*], [*Meaning*]),
  [sync], [2], [`FF FE`. Neither byte occurs in UTF-8 text, so a reader can look for the next record after a bad one.],
  [kind], [1], [Record kind. A reader skips a kind it does not know.],
  [length], [4], [Payload length. More than 1 MiB is treated as corruption.],
  [payload], [length], [Depends on the kind.],
  [crc], [4], [CRC-32C of kind, length and payload.],
)

Strings are stored raw with a length in front. Nothing is escaped, so a command with a newline needs no special case.

== Record kinds

#table(
  columns: (auto, 1fr, auto),
  inset: 4pt,
  table.header([*Kind*], [*Payload*], [*Written*]),
  [`dir`], [The directory path. Its key is the xxHash64 of the path, as today.], [In the same write as its first use],
  [`start`], [Time (`i64`, Unix milliseconds), session (`u64`), sequence (`u32`), directory key (`u64`), command, and up to 16 paths.], [When the line is submitted],
  [`end`], [Session, sequence, flags (`u8`), exit code (`i32`), duration (`u32` milliseconds, saturating).], [When the line finishes],
)

- *Session* is a random `u64` chosen when the shell starts. *Sequence* counts the session's lines. Together they tie an `end` to its `start`.
  A reader ignores a `start` whose session and sequence it has already seen, so a record written twice is harmless.
- *Flags* say whether the line finished without an error and whether an exit code is present.
  These are what `finishLine` already gives the prompt: `lastLineOk`, `lastLineExit`, `lastLineExitSet` and `lastLineDuration`.
- *Paths* are the arguments of the line that named an existing file or directory when it was submitted, stored as typed.
  They are found by lexing the line and checking the literal, string and path tokens after the command word.

#decision[Directories are interned and commands are stored inline.
A directory averages 37 bytes and repeats about 30 times. A command averages 13 bytes, so an 8-byte key in its place saves almost nothing
and would make every record depend on another.]

#decision[`start` and `end` are separate records. The command is on disk before it runs, so a crash or a closed terminal does not lose it,
and other sessions can see it while it is still running. A `start` with no `end` has an unknown status.]

== Reading a log

The same parser serves a first load, a read of new bytes, and a file that was written with no lock at all.

+ At the current position, if there is a whole frame with a good CRC, take it and move past it.
+ Otherwise search forward, starting one byte later, for sync bytes that begin a whole frame with a good CRC.
  If one is found, skip to it. The skipped bytes are a torn or damaged record.
+ If none is found, stop and stay at the current position. The next read tries again from here.

So a reader never moves past bytes it could not parse until later bytes do parse.
An unfinished record at the end of the file is retried later; a torn record in the middle is skipped as soon as a good one follows it.
The search starts one byte later, not one frame later, because the length field of a damaged frame cannot be trusted.

== The history goroutine <goroutine>

Each shell process has one goroutine that does all history file work: every `stat`, open, lock, read, write and rename.
The prompt goroutine never touches the files. It sends requests and never waits for an answer, with the two exceptions below.

#table(
  columns: (auto, 1fr, auto),
  inset: 4pt,
  table.header([*Request*], [*What the history goroutine does*], [*Prompt waits?*]),
  [submit], [Queue a `start` record, then try to write the queue.], [No],
  [finish], [Queue an `end` record, then try to write the queue.], [No],
  [refresh], [Look for records from other sessions. Sent when a prompt is printed.], [No],
  [rewrite], [Delete or compact (@rewrite).], [Yes, with a limit],
  [flush], [Write what is queued. Sent at exit.], [Yes, the existing 2 s],
)

New entries from other sessions come back as finished batches.
The prompt goroutine collects any that are ready when it prints a prompt, without waiting, and adds them to the index it owns.
Nothing is merged during a key press.

This arrangement has three effects:

- A file system that hangs blocks only this goroutine. The prompt, the editor and the commands carry on, and the existing shutdown watchdog covers exit.
- The lock is only ever held by one goroutine in a process, so differences between systems in who owns a lock (the handle, or the process) cannot matter (@lock-properties).
- The index has one owner and needs no mutex on the key path.

At startup the shell shows its first prompt without waiting for the load. History appears when the first batch arrives.

*The queue.* Records that could not be written yet wait in a queue of fixed size.
They are written, in order, the next time any request is handled.
When the queue is full the oldest record is dropped and counted, and the count is reported once at exit.
A line whose record would pass 1 MiB is never queued. In both cases the line stays in the session's in-memory history.

= Locking <locking>

== What the lock is for <invariants>

The lock exists to make three things true. Each rule in @rules is there for one of them.

#rule("L1")[*One writer.* At any moment at most one process is appending to the log or replacing it.]
#rule("L2")[*Nobody has the log open while it is replaced.* During the rename in a rewrite, no `msh` process holds a handle to the log.
On Linux and macOS this is tidiness. On Windows the rename fails without it (@win-replace).]
#rule("L3")[*A reader that holds the lock sees whole records.* It cannot observe a record half written.]

The lock is *not* what keeps existing records safe. That comes from the log being append-only and from the CRC on every frame (@layers).
If the lock cannot be had, records can still be written and read; only replacing the log is refused.

== The lock <lock-properties>

It is an advisory lock on the lock file, in two modes: *shared*, which any number of processes may hold together,
and *exclusive*, which excludes everything else.

#table(
  columns: (auto, 1fr, 1fr),
  inset: 4pt,
  table.header([], [*Linux and macOS*], [*Windows*]),
  [Call], [`flock(fd, LOCK_SH or LOCK_EX, with LOCK_NB)`], [`LockFileEx(handle, flags, byte 0, length 1)` with `LOCKFILE_FAIL_IMMEDIATELY`, and `LOCKFILE_EXCLUSIVE_LOCK` for exclusive],
  [Busy result], [`EWOULDBLOCK`], [`ERROR_LOCK_VIOLATION`],
  [Release], [`flock(fd, LOCK_UN)`], [`UnlockFileEx`],
  [Owned by], [The open file description. On NFS, Linux turns `flock` into a byte-range lock owned by the process.], [The handle],
  [If the process dies], [The kernel drops the lock when the last descriptor closes.], [The system drops it. The `LockFileEx` notes say this can take a moment.],
  [Enforced on reads and writes], [No. Advisory only.], [Yes, for the locked bytes. The lock file is empty and is never read or written, so this never applies.],
)

Why a separate lock file, and not a lock on the log:

- The log is replaced by a rewrite. A lock on a file that has just been replaced protects nothing.
- On Windows a byte-range lock stops other handles reading or writing those bytes. A lock on the log would get in the way of the reads it is meant to coordinate.

Why `flock` and not `fcntl` record locks: an `fcntl` lock belongs to the process, and closing _any_ descriptor for the file drops it.
`flock` belongs to one descriptor, which matches Windows.

== The rules <rules>

#rule("R1")[*One goroutine.* Only the history goroutine touches history files (@goroutine).]
#rule("R2")[*Lock first, open second.* The log is opened only while the lock is held, and closed before the lock is released.
No handle to the log outlives an operation. This gives L2.]
#rule("R3")[*Exclusive to change, shared to read.* Appending and rewriting take the exclusive lock; reading takes the shared lock. This gives L1 and L3.]
#rule("R4")[*No upgrade, no nesting.* An operation takes one lock in one mode, once. It never holds the lock while asking for it again.]
#rule("R5")[*Never wait without a limit.* The lock is only ever _tried_. Waiting is a loop of tries with a budget (@acquire).]
#rule("R6")[*Unlock, then close.* The lock is released with an explicit unlock call before the descriptor is closed.]
#rule("R7")[*Only file work under the lock.* Nothing is read from the terminal, no user code runs, and no path is checked while the lock is held.
A record is built, and its paths are checked, before the lock is taken.]
#rule("R8")[*The lock file is permanent.* Nothing deletes, renames, truncates or writes it.]
#rule("R9")[*Check the lock file is still the lock file.* On Linux and macOS, after the lock is granted, compare the descriptor with the path (device and inode).
If they differ, someone removed the lock file; release and start again.]

Two exceptions, both deliberate:

- The `stat` of the log at each prompt takes no lock and opens nothing. It only decides whether a locked read is worth doing.
- When the file system has no locks at all, appends and reads go ahead without one (@nolock).

Why each of the less obvious rules:

- *R5.* A blocking lock call cannot be cancelled in Go; the goroutine would be stuck in the kernel for as long as the holder likes.
  A holder can stay for a long time without being dead: a shell stopped with `SIGSTOP`, a process frozen in a debugger, a suspended process on Windows.
- *R6.* On Linux a child process holds a copy of every descriptor between `fork` and `exec`.
  History files are opened close-on-exec, so the copy goes away at `exec`, but for that instant a plain `close` in the shell would leave the lock held by the copy.
  An explicit unlock releases it for every copy. On Windows the notes on `LockFileEx` say the release after a close can lag. Go's own lock package makes the same demand.
- *R9.* If the lock file is deleted while a process holds it, that process keeps a lock on a file with no name,
  and the next process creates a new lock file and locks that. Two holders.
  On Windows this cannot happen: a file that is open without delete-sharing cannot be deleted, and Go opens files that way.

== Getting the lock <acquire>

```
acquire(mode, budget):
    f = open the lock file, read and write, create if missing
    wait = 1 ms
    repeat:
        result = try_lock(f, mode)            # returns at once, always
        if result is OK:
            if f is no longer the file at the path:      # R9
                unlock(f); close(f); start acquire again
            return f
        if result is NOT SUPPORTED:  close(f); return NO_LOCKS
        if result is another error:  close(f); return that error
        if the budget is used up:    close(f); return BUSY
        sleep(wait + random(0 .. wait))
        wait = min(2 * wait, 16 ms)

release(f):
    unlock(f)          # R6
    close(f)
```

A budget of zero means one try.
The random part of the sleep stops two waiting processes from trying in step.

There is no queue of waiters, so the lock is not fair.
That is acceptable here because every hold is short: a shared hold is one read, an exclusive hold is one write or one rewrite of a file of a few megabytes.
In the prototype a rewriter completed 150 rewrites while eight processes appended without pause and three read (@checked).

== Operations <operations>

The budgets are starting values to be tuned on real use.

#table(
  columns: (auto, auto, auto, 1fr),
  inset: 4pt,
  table.header([*Operation*], [*Mode*], [*Budget*], [*If the lock is busy when the budget ends*]),
  [Append (`start`, `end`)], [exclusive], [2 s], [The records stay queued and are tried again with the next request.],
  [Refresh at a prompt], [shared], [one try], [Skipped. The next prompt tries again.],
  [First load], [shared], [250 ms], [The shell starts with no history yet. Each prompt tries again, once, until it works.],
  [Rewrite], [exclusive], [5 s], [Nothing is changed. The user is told the history is busy.],
  [Flush at exit], [exclusive], [what is left of 2 s], [The queued records are lost, and the shell says how many.],
)

=== Append

+ _Before the lock._ Build the record. For a `start`, check which arguments are existing paths.
+ Acquire exclusive.
+ `stat` the log. If it does not exist, create it (@migration) and continue.
+ Open the log read-only, read the 16-byte header, close it.
  A higher format version: write nothing, warn once, drop the queue.
  A generation other than the one last seen: forget which directories are believed to be defined.
+ Open the log write-only in append mode, without create.
+ Write everything queued with one `write()`: for each `start`, its `dir` record first if the directory is not known to be defined.
+ Close the log. Release.

On any error in steps 3 to 6 the lock is released and the records stay queued.
A record that was written but is tried again, because the error came after the write, is ignored by readers as a repeat.

The header is read through its own handle so that the append handle has no read access.
On Windows that keeps the handle to exactly the access that makes appends atomic (@win-append).

The writer never calls `fsync`. A power loss can lose the last few records; that is the right trade for a history.
It also never truncates the log after a failed write. A partial record is skipped by every reader, and a second way of repairing it is not worth a second code path.

=== Refresh

+ `stat` the log by path, with no lock. If the size and modification time are what they were after the last read, stop.
+ Acquire shared, one try. If busy, stop.
+ Open the log read-only. Read its size and its header.
+ If the generation is unchanged and the size is not smaller than the position already read, read from that position to the end.
  Otherwise read the whole file and start the index again.
+ Close the log. Release.
+ _After the lock._ Parse the bytes and hand the batch to the prompt goroutine.

After a read of new bytes, records of this session are skipped; they are already in the index.
After a read of the whole file, this session's records come from the file like any others, and any that are still queued are added after them.

The first load is the same with nothing read yet.

=== Rewrite <rewrite>

Used to delete a command, for example a password typed at the prompt, and for a manual compaction.

+ Acquire exclusive. With no locks available, refuse (@nolock).
+ Read the whole log: open, read, close.
+ Build the new contents in memory: a header with a new generation, one `dir` record for each directory still in use,
  then every surviving `start` and its `end`, in order.
+ Open `msh_history.v2.tmp` with create and truncate. Write the contents, `fsync`, close.
+ Rename the temporary file over the log. Retry for up to 2 s on the errors that mean "another program has it open" (@win-replace).
+ On Linux and macOS, `fsync` the directory.
+ Release.

If step 5 fails for good, the temporary file is removed and the log is exactly as it was. The user is told, and can try again.
A crash between steps 4 and 5 leaves a temporary file that the next rewrite overwrites. Its fixed name is safe because only the lock holder writes it.

Other sessions see a new generation at their next refresh and load the whole file again, which drops the deleted command from their memory too.

There is no tombstone record. A deletion that left the text on disk until some later cleanup would not be a deletion.
Nothing runs a rewrite automatically.

== Who waits for whom

#table(
  columns: (auto, 1fr, 1fr),
  inset: 4pt,
  table.header([*Holder*], [*Another process wants shared*], [*Another process wants exclusive*]),
  [nobody], [granted], [granted],
  [shared (a read)], [granted], [tries again until its budget ends],
  [exclusive (an append or a rewrite)], [a refresh is skipped; a first load tries for 250 ms], [tries again until its budget ends],
)

*No deadlock.* There is one lock. No operation holds it while asking for it (R4), and the holder waits for nothing except its own file calls (R7).
So no process ever waits for a process that is waiting for it.

*No lost record from a busy lock.* An append that cannot get the lock keeps its records queued. Only a full queue or the exit limit drops any, and both are counted.

== When the holder dies, stops, or the disk hangs

#table(
  columns: (1fr, 1.5fr),
  inset: 4pt,
  table.header([*What happens*], [*Result*]),
  [The holder is killed], [The system releases the lock. Others get it on a later try. If the holder was in the middle of a write, the log has a torn record, which readers skip.],
  [The holder is killed during a rewrite], [The log is whole: either the old file or the new one, never a mix. A temporary file may remain.],
  [The holder is stopped or suspended while holding], [The lock stays held. Appends queue, refreshes are skipped, a rewrite reports busy. Everything catches up when it resumes. Holds last microseconds, so this takes a stop at exactly the wrong moment.],
  [The file system hangs under the holder], [The same as stopped, for every process using that directory. Each shell's prompt keeps working (@goroutine).],
  [The machine loses power], [The lock is gone with the machine. The end of the log may be missing, zeros, or garbage; readers skip it.],
)

== When the file system has no locks <nolock>

Some file systems refuse lock calls: some network and FUSE mounts, and Windows drives seen from WSL 2 (@wsl).
The lock call then fails with `ENOLCK`, `ENOTSUP` or `ENOSYS`, which is different from busy.

- *Append and refresh go ahead without the lock.* They rely on the other two layers (@layers).
- *Rewrite is refused,* with a message that says why. Without the lock a rewrite could drop a record that another session appended while it ran.

A file system that accepts lock calls and silently ignores them cannot be told apart from one that works. Then the design runs on the other two layers without knowing it, and a rewrite is not safe. This is not detected.

== Three layers <layers>

#table(
  columns: (auto, 1fr, 1fr),
  inset: 4pt,
  table.header([*Layer*], [*What it gives*], [*What it needs*]),
  [1. The lock], [One writer. Nobody has the log open during a rename. Readers see whole records. A rewrite loses nothing.], [A file system with working locks],
  [2. Append mode], [A record lands after the others, never on top of one.], [A local file system, or all writers under one kernel],
  [3. Sync bytes and CRC], [A torn, overwritten or interleaved record is skipped, and the records after it are read.], [Nothing],
)

Layers 2 and 3 are why a missing lock degrades the history and does not destroy it.

= Windows <windows>

Nothing in this section has been run on Windows. It comes from the Go 1.27 source (read directly), Microsoft's documentation and other projects' experience (see @sources),
and a run of the Windows build under Wine, which follows the Windows API but is not Windows.

== What Go does on Windows

Read from the Go 1.27 source in `syscall`, `os` and `internal/syscall/windows`:

#table(
  columns: (auto, 1fr),
  inset: 4pt,
  table.header([*Go call*], [*On Windows*]),
  [`os.OpenFile`], [`CreateFile` with sharing for read and write, and *not* for delete. The handle is not inherited by child processes. I/O is synchronous.],
  [`O_APPEND`], [Access is `FILE_APPEND_DATA` in place of `FILE_WRITE_DATA`. Every write goes to the end of the file.],
  [`os.Stat`], [`GetFileAttributesEx` first. It reads size and time without leaving a handle open.],
  [`os.Rename`], [`MoveFileEx` with `MOVEFILE_REPLACE_EXISTING`.],
  [`os.Root.Rename`], [A rename with POSIX semantics (`FILE_RENAME_POSIX_SEMANTICS`), falling back to the plain kind where the file system does not support it.],
  [`File.Sync`], [`FlushFileBuffers`.],
)

Three consequences:

- Two `msh` processes can have the log open together, for reading or appending. Go's sharing mode allows it.
- While any `msh` process has a file open, no process can delete or rename that file, because the handle does not share delete. This is what makes R9 unnecessary on Windows, and what makes R2 necessary.
- The existing Windows `openHistoryFile` (an `Lstat` and an `OpenFile`) is enough. It needs no change.

== The lock on Windows

- `LockFileEx` needs a handle with read or write access. The lock file is opened for both.
- The locked range is byte 0, length 1. The file is empty; Windows allows locking past the end of a file.
- `LOCKFILE_FAIL_IMMEDIATELY` makes the call return at once with `ERROR_LOCK_VIOLATION` when the lock is busy. That is the "busy" of @acquire.
  The handle is synchronous, so without that flag the call would block, which R5 forbids.
- The lock belongs to the handle. A second handle in the same process would conflict with the first; R1 means there is never a second one.
- When a process ends, by any means, the system releases its locks. The `LockFileEx` notes warn that this can take a moment, and recommend unlocking explicitly; R6 does.

== Appending on Windows <win-append>

A handle with `FILE_APPEND_DATA` and without `FILE_WRITE_DATA` appends atomically: the system moves to the end and writes as one step.
Git for Windows relies on exactly this for its own append mode.

Reports disagree on two details: whether adding read access to the handle weakens the guarantee, and whether it holds for writes of many kilobytes.
The design avoids both questions. The append handle is write-only, and the header is read through a separate handle.
And under the exclusive lock there is only one writer, so the guarantee is needed only when the lock is missing.

== Replacing the log on Windows <win-replace>

This is the one place where Windows is truly different.
On Linux a file can be renamed over while others have it open. On Windows, replacing a file that anything has open fails with `ERROR_ACCESS_DENIED` or `ERROR_SHARING_VIOLATION`.

*Our own processes.* R2 means no `msh` process has the log open during a rename: to open it they need the lock, and the rewriter holds it exclusively.
Under Wine, with six appenders and three readers running flat out and no other program involved, none of 60 renames needed a retry.

*Other programs.* An antivirus scanner, the search indexer, a backup tool, or a viewer the user has pointed at the file can have it open at the wrong moment.
Scanners in particular open a file just after it is written, which is when the temporary file is renamed.
The design does three things:

+ It renames with `os.Root.Rename`. On NTFS that is a rename with POSIX semantics, which can replace a file that other programs have open, as long as they opened it sharing delete.
  Whether a given scanner or indexer does was not checked.
+ It retries for up to 2 s on `ERROR_ACCESS_DENIED` and `ERROR_SHARING_VIOLATION`, with growing sleeps. The Go toolchain and Git both do this for the same reason.
+ If that fails, it gives up cleanly: the temporary file is removed, the log is untouched, and the user is told that another program has the history file open.

Under Wine, with one program holding the log open most of the time, 22 of 30 renames needed retries and all succeeded.
With two such programs one rename ran out of budget and failed; the log was still complete, and every record was still present once.

The same retry wraps every open of the log and the temporary file, since a scanner can also be in the way of an open.

#note[A design that never replaces a file was considered: logs named with a number, the highest number winning, and a rewrite creating the next number and removing the old file.
It would turn a failed delete into a delayed cleanup. It costs a second `stat` at every prompt and a rule for finding the current file, on every system.
It is the fallback if step 3 turns out to happen in practice.]

== How a shell ends on Windows

- *Exit, or the exit timeout.* The flush runs as on any system.
- *The console window is closed.* Windows gives the process a few seconds and then ends it. Whatever is queued and unwritten is lost; what was written is safe, because records are written at submit and not at exit.
  This is a large improvement on today, where closing the window loses the whole session.
- *The process is ended from Task Manager.* The same, with no grace period.

In every case the system releases the lock.

== Where the files live

`%LOCALAPPDATA%` is local to the machine and is not roamed. It is also not synced by OneDrive by default.
A history directory inside a synced folder would have a sync client opening the files constantly; that is not supported.

An elevated shell uses the same directory and the same files as a normal one. Whether a file created by the elevated shell is always writable by the normal one has not been checked.

== WSL <wsl>

#table(
  columns: (1fr, 1.6fr),
  inset: 4pt,
  table.header([*Case*], [*Expectation*]),
  [History in the Linux file system (the default, `~/.local/share/msh`)], [Ordinary Linux. Fully supported.],
  [History on a Windows drive through `/mnt/c`], [The WSL 2 file server is reported not to implement locks. The design then runs without the lock (@nolock): appends and reads work, a rewrite is refused. This is also the slow-disk case the exit timeout already exists for.],
  [A Linux `msh` and a Windows `msh.exe` sharing one directory], [Not supported. The two kinds of lock do not see each other there, and append mode is not atomic across the boundary. Layer 3 keeps the records that survive readable.],
)

= Using the data <using>

This section is what the storage is for. It is a sketch; the suggestion rules deserve their own review.

*Ranking.* For typed text _P_ in directory _D_, the suggestion is the most recent valid entry that starts with _P_ and ran in _D_,
and otherwise the most recent valid entry that starts with _P_ anywhere.

*Validity.* An entry is valid when:

- it is a `cd` and its target exists from _D_;
- otherwise, each of its recorded paths exists from _D_.

*Up and Down.* They walk this session's lines first and then older history.

*Scripts.* With one well-defined log, a builtin could return history as a grid, so that questions about history are written in mshell and not in SQL.
That builtin is not designed here.

= Cost

== Size

A `start` with no paths is about 58 bytes for the mean command and an `end` is 32, so a line costs about 90 bytes plus its paths.
At 18.5 lines a day that is about 0.6 MB a year. The measured history would be about 0.85 MB, against 0.69 MB in the three files today.

#decision[No index cache file and no size cap. They can be added later without changing the log, if a measured startup calls for them.]

== System calls

#table(
  columns: (auto, 1fr),
  inset: 4pt,
  table.header([*When*], [*Calls, all on the history goroutine*]),
  [A prompt, nothing new], [One `stat`.],
  [A prompt, new records], [`stat`; open, lock, unlock and close the lock file; open, `fstat`, two reads and close the log.],
  [A submitted line], [Open, lock, unlock and close the lock file; `stat`; open, read and close for the header; open, write and close for the append.],
)

None of these is on the key path, and none delays the prompt.

== A key press <keypress>

The suggestion search must not allocate, and its system calls count.

- *Search.* Scan the entries of _D_ backward, then all entries backward, comparing bytes in the arena.
- *Typing forward.* If the last suggestion still starts with the longer text it is still the answer.
  If not, the scan continues from where it stopped, because a newer entry that did not match the shorter text cannot match the longer.
  A whole line typed left to right costs one pass over the history, not one pass a key.
- *Validity.* Each check is a `stat`. At most a fixed number of candidates are checked for a key, and a verdict is remembered until the next prompt.
  When the budget runs out there is no suggestion for that key.

None of this has been measured yet. It has to be, with the syscalls included, before the rules are fixed.

= Migration <migration>

The log is created by whichever shell first holds the exclusive lock and finds that it does not exist.
It builds the first contents, writes them to the temporary file, and renames it into place, exactly as a rewrite does:

- a header;
- if the three old files exist, their records, sorted by timestamp, which repairs the session-exit order.
  Each becomes a `start` with session 0, its position as the sequence, no paths and no `end`.

The old files are not changed or removed. All 9,332 measured records resolve, so none is dropped.
An older `msh` keeps working with the old files, and the two histories then diverge.

Two consequences to know about:

- Without locks, two shells could both import. The repeats have the same session and sequence, so readers ignore them.
- Deleting `msh_history.v2` by hand brings the old history back at the next start, for as long as the old files and the import code exist.

= Testing <testing>

- *Frame.* Encode and decode every kind, including a command with newlines and one at the size limit.
- *Torn records.* Cut a small log at every byte offset, append good records, and check the reader returns every intact record.
- *Garbage.* A fuzz test of the reader on arbitrary bytes: no panic, and no allocation above the record limit.
- *Many processes.* The prototype's test, kept as a test of the real code: appenders, readers, a rewriter and a program that holds the log open,
  with every record required to be present exactly once at the end. Fixed counts, a temporary directory, a timeout, and a check for leftover processes.
- *Killed writers.* The same with some appenders killed part way. Survivors must be complete and nothing may stay locked.
- *A held lock.* A helper process takes the lock and keeps it. Appends must queue, a refresh must be skipped, a rewrite must report busy, and the prompt must not be delayed.
- *Windows.* The many-process test on a real Windows machine, with and without a scanner-like holder, and once with real-time antivirus scanning on.
- *Migration.* A fixture of the three old files, including out-of-order timestamps and a missing line.

A TLA+ model of the lock rules, in the manner of `ai/terminal-control`, would check the orderings a stress test only samples.
It is not written.

= Open decisions

#open[*Do Up and Down see other sessions' lines?* Proposed: this session first, then older history, with suggestions using everything.
Fish keeps them session-local until a merge; zsh's shared history mixes them.]

#open[*Are failed commands suggested?* Proposed: skip entries that exited 127 (command not found) and keep the rest.]

#open[*Is a line that starts with a space kept out of history?* bash and fish do this. Today the line is trimmed and always saved.]

#open[*Validity checks on the key path.* Proposed: synchronous, bounded and cached, as in @keypress.
A `stat` on a stuck mount would still freeze a key press. The history goroutine could do these checks too, with the suggestion painted when the answer arrives.]

#open[*The budgets.* 2 s for an append, 250 ms for a first load, 5 s and 2 s for a rewrite are guesses.]

#open[*The user-facing words.* Deleting, compacting and querying need builtins or `msh` subcommands. Neither is designed here.]

#open[*Names.* `msh_history.v2` is a placeholder.]

= Implementation plan

+ Frame, record kinds and the parser, with the frame, torn-record and garbage tests. Nothing uses them yet.
+ The lock: `acquire` and `release` for Linux, macOS and Windows, the retry helper, and the many-process, killed-writer and held-lock tests.
  Run the many-process test on Windows before going further.
+ The history goroutine, creation of the log with the import, and the switch: write at submit, `end` at finish, load at startup.
  Suggestions and Up/Down behave as before. This step alone fixes problems 1 to 7 of @problems.
+ The refresh at each prompt.
+ Rewrite and delete.
+ Directory ranking, then `cd` validity, then recorded paths, each measured on the key path.
+ Remove `WriteToHistory` and the three-file code once the migration has shipped.

= What was and was not checked <checked>

*Checked.*

- The description of today's code, by reading `History.go`, `Main.go`, `CommandPaint.go` and the three `History_*.go` files at a3774d7.
- The measurements in @measurements, by a script that printed counts only.
- What Go does on Windows, by reading the Go 1.27 source: `syscall.Open`, `os.Stat`, `os.Rename`, `os.Root.Rename`,
  and the lock and retry packages the Go command uses itself.
- The lock protocol, with the prototype in `ai/history-lock-prototype`. It implements @acquire, the three operations of @operations, and the parser, and builds for Linux, macOS and Windows.
  It differs from the design in three ways: it creates the log with the first append and not through the import, it has no queue and no history goroutine, and it parses while still holding the lock.

#table(
  columns: (auto, 1fr, 1fr),
  inset: 4pt,
  table.header([*Where*], [*Run*], [*Result*]),
  [Linux, tmpfs], [8 appenders of 1,500 records, 3 readers, 150 rewrites, 1 program holding the log open], [All 12,000 records present once, in the file and in every reader],
  [Linux, ext4], [The same], [The same],
  [Linux, ext4], [6 appenders of 3,000 records and 40 rewrites; 3 appenders killed with `SIGKILL` part way], [Survivors complete. Each killed writer's records an unbroken run from its first. Nothing left locked; an append and a rewrite afterwards both worked],
  [Wine 11.19, Windows build], [6 appenders of 600, 3 readers, 60 rewrites, no other program], [All 3,600 present once. No rename needed a retry],
  [Wine 11.19, Windows build], [4 appenders of 300, 2 readers, 30 rewrites, 1 program holding the log open], [All 1,200 present once. 22 renames needed a retry; all succeeded],
  [Wine 11.19, Windows build], [6 appenders of 600, 3 readers, 60 rewrites, 2 programs holding the log open], [One rename failed after 2 s. The log was complete, with every record present once],
)

*Not checked.*

- Anything on Windows itself: the lock calls, the append guarantee, both kinds of rename, and how real antivirus and indexing behave. Wine is evidence about the code, not about Windows.
- macOS, NFS, SMB and WSL mounts. The WSL statement rests on two issue reports.
- The prototype used `os.Rename`. `os.Root.Rename` was read, not run.
- A lock holder that is stopped or suspended, and a file system that hangs. The design's answer to both is the budget, which the prototype has, and the queue, which it does not. Neither case was staged.
- The prior-art table.
- Any timing: load, search, `stat`, time under the lock.
- The rule for finding path arguments. It is written from the token names in `Lexer.go` and has not been checked against the language documentation or the simple command parser.

= Sources <sources>

- Go 1.27 source, read locally: `syscall/syscall_windows.go`, `os/file_windows.go`, `os/stat_windows.go`, `internal/syscall/windows/at_windows.go`,
  `cmd/go/internal/lockedfile/internal/filelock`, `cmd/internal/robustio`.
- Microsoft, #link("https://learn.microsoft.com/windows/win32/api/winbase/nf-winbase-movefileexa")[MoveFileEx].
- Microsoft, #link("https://learn.microsoft.com/windows/win32/fileio/locking-and-unlocking-byte-ranges-in-files")[Locking and Unlocking Byte Ranges in Files].
  The statement that locks are released when a process ends is from the `LockFileEx` notes as remembered; the search for this draft did not return that page.
- Git, #link("https://googlers.googlesource.com/jrn/git/+/d641097589160eb795127d8dbcb14c877c217b60%5E%21/compat/mingw.c")[mingw: enable atomic O_APPEND], on `FILE_APPEND_DATA`.
- Git, #link("https://git.lorimer.id.au/gitweb.git/diff_plain/6ac6f878")[retrying a rename that fails with access denied].
- Python issue tracker, #link("https://bugs.python.org/msg382784")[two ways to append atomically on Windows].
- NLog, #link("https://github.com/NLog/NLog/pull/1756")[discussion of `FILE_APPEND_DATA` and write size].
- WSL, #link("https://github.com/microsoft/WSL/issues/5762")[issue 5762], and Go, #link("https://github.com/golang/go/issues/37461")[issue 37461], on file locks over the WSL 2 file server.
