# AGENTS.md

## General

This repository is for a concatenative shell language.
It is implemented in golang, with the go code in `mshell/`.
User documentation is in `doc/`.
The standard library for the language is in `lib/std.msh`.
Always work in a separate feature branch.
Do NOT run `gofmt` without my permission. I will tell you when it's allowed.

You do not have much training data on this language. Please read the HTML documentation in `doc/` to get a full understanding of the language.
If you don't understand something, or don't find something in the docs, you MUST tell me, so it can be made clear for you and everyone else.

## Building

In the `mshell` directory, there are several very simple build scripts that are one line `go build` commands.

- `build.sh`
- `build_win.sh`
- `build_mshw_win.sh`

In general, `go build -o <executable>` should work.
You may need to build with cache within the repo if you don't have permissions for the golang cache outside.

## New Functions

New built in functions are in `mshell/Evaluator.go`.
If it is a simple combination of other existing functions, it belongs in the standard library.
Always make sure to update the documentation appropriately.

In the `CHANGELOG.md` file, add the new function as a line under the ## Unreleased (create if necessary), ### Added heading.
If there are multiple functions, group under a `- Functions` bullet.

Right now, the functions are defined in dumb way, requiring us to track all the known built-ins in a separate map at `mshell/BuiltInList.go`.
If something is added or removed, make sure to update `mshell/BuiltInList.go`.

## CLI

The CLI code is at `mshell/Main.go`.
If you make a change to the CLI interface, make sure to update the shell completions.

### Completions

Completions for common commands are in the standard library `lib/std.msh` at the bottom.
Wrap in a Vim fold, and make the definition following the pattern of the others.
Completions definitions have key 'complete' in the definition meta data, where the value is the list of commands it is used for.

## Testing instructions

All test cases are in `tests`.

- cd to `tests/` and run `./test.sh`.
- cd to `mshell/` and run `go test`.

## Documentation

You build the documentation using:

```
cd doc
msh build.msh
```

Files in `doc/build` are build artifacts. Do not edit these.
There is a main base template at `doc/base.html`, which has most of the general purpose CSS and styles for code.
You should rebuild the docs when you make edits.
