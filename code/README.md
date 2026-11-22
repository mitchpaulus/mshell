# `mshell` VS Code Extension

This is the official extension for the concatenative shell-like programming language [`mshell`](https://github.com/mitchpaulus/mshell).

## Features

- Syntax highlighting for `.msh` and `.mshell` files
- Language Server Protocol (LSP) support:
  - Hover documentation for built-in functions
  - Variable name completion (triggered by `@`)
  - Variable rename support
  - Code actions to quote the bare literals in the list containing the cursor: only those that name nothing (not definitions or builtins), which are errors until quoted, or all of them
- Run the active mshell file from the editor title bar in a VS Code terminal
- Run the active mshell file with F5 via a minimal debug adapter

## Requirements

For LSP features, the `msh` executable must be installed and available in your PATH, or configured via the `mshell.lspPath` setting.

## Extension Settings

- `mshell.lspPath`: Path to the msh executable for the language server (default: `"msh"`)
- `mshell.mshPath`: Path to the msh executable for running scripts (default: `"msh"`)
