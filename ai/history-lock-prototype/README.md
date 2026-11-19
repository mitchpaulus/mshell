# History lock prototype

A throwaway test of the lock protocol in `ai/history-storage.typ`.
It is its own Go module and is not part of the `msh` build.

It starts several processes against one directory:

- appenders, which each add numbered records under the exclusive lock;
- readers, which follow the log the way a shell would at each prompt (one try for the shared lock, tail reads, full reloads);
- a rewriter, which replaces the log through a temporary file and a rename;
- intruders, which hold the log open with no lock, the way an antivirus or indexer might.

At the end every record must be present exactly once, in the file and in every reader.

## Build

```
go build -o lockproto .
GOOS=windows GOARCH=amd64 go build -o lockproto.exe .
```

## Run

```
lockproto run DIR APPENDERS RECORDS READERS REWRITES INTRUDERS
```

`DIR` must exist and be empty. For example:

```
mkdir t1
./lockproto run t1 8 1500 3 150 1
```

On Windows, the runs worth doing are with no intruder, to see whether any rename needs a retry,
and with one or two, to see how often the two second retry budget runs out:

```
mkdir t1 t2
lockproto.exe run t1 6 600 3 60 0
lockproto.exe run t2 6 600 3 60 2
```

The run prints `OK` and exits 0 when no record was lost or duplicated.
A failed rename is reported as `rewrite FAILED`; the log must still be complete after it.
