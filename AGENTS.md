# AGENTS.md

## General

This repository is for a concatenative shell language.
It is implemented in golang, with the go code in `mshell/`.
User documentation is in `doc/`.
The standard library for the language is in `lib/std.msh`.
Always work in a separate feature branch.

## Building

This project uses `redo` as the build system.
In the `mshell` directory, you can run `redo` to rebuild the executables.

## New Functions

New built in functions are in `mshell/Evaluator.go`.
If it is a simple combination of other existing functions, it belongs in the standard library.
Always make sure to update the documentation appropriately.

In the `CHANGELOG.md` file, add the new function as a line under the ## Unreleased (create if necessary), ### Added heading.
If there are multiple functions, group under a `- Functions` bullet.

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
