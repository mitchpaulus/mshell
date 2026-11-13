# `mshell`

> [!WARNING]
> This is still very much an experiment.
> I still am changing significant parts of the syntax and semantics.
> At this point, this repository has fewer than 10 stars, so I will be operating as if I am the only user.
> If you are using it, have feedback, or just want to say hello, you can email [mshell-feedback@mitchellt.com](mailto:mshell-feedback@mitchellt.com) or use the GitHub discussions.

`mshell` is my personal scripting language, meant to replace short shell scripts (< ~100 LOC) and other shell one-liners.
A concatenative language is a good fit for this purpose, as it allows for easy composition of simple functions and pipelines.

The goal is to provide most of the simple Gnu utilities as part of the language,
while making calls to external programs and pipelines simple and easy.

Future goals are to even add some type safety.

# Documentation

The beginnings of some documentation are for now located [here](https://mitchellt.com/msh/basics.html).

# Installation

`mshell` is compiled to a single executable binary, so installation is straightforward.
First, download the executable from the [GitHub releases page](https://github.com/mitchpaulus/mshell/releases/latest).
The assets are tar.gz files, so you would unpack them with:

```sh
tar -xvf linux_amd64.tar.gz
```

Put that file in a directory that is in your `$PATH` and make sure it is marked as executable on Linux.

The standard library is built into the binary, so there is no other file to install.

With Go installed, you can instead build and install from source.
This installs the binary as `mshell`:

```sh
go install github.com/mitchpaulus/mshell/mshell@latest
```

Your own startup code goes in an init file.
For `msh v0.13.0`, create `${XDG_CONFIG_HOME:-$HOME/.config}/msh/v0.13.0/init.msh` on Linux/macOS
or `%LOCALAPPDATA%\msh\v0.13.0\init.msh` on Windows.
That init file is required for scripts that declare `VER "v0.13.0"`, but it is optional for interactive use and scripts without `VER`.
For one-off overrides, `MSHINIT` can point to an exact init file for interactive use and scripts without `VER`.
Scripts with `VER "vX.Y.Z"` always use the init file for `vX.Y.Z`.

An example install script is at [install.sh](https://github.com/mitchpaulus/mshell/blob/main/install.sh) in this repository.

# Examples

Best way to understand purpose and syntax of `mshell` is to see it in action.
I have ported over many of personal scripts that used to be done with `sh` or Python in my personal dotfiles.
Take a look through this [script directory](https://github.com/mitchpaulus/dotfiles/tree/master/scripts) and you'll find many real life examples.

Otherwise, there are some other examples [here](https://mitchellt.com/msh/examples.html)

Here are some examples.

*Better Awk One-liners*. Examples from the `awk` book, translated to `mshell`. You can run these examples like:

```sh
msh file_with_contents.msh < input_file_to_process.txt
awk -f file_with_contents.awk < input_file_to_process.txt
# OR (using 1st example)
msh -c 'sl len wl' < input_file_to_process.txt
awk 'END { print NR }' < input_file_to_process.txt
```


*Simpler execution of common shell idioms*

| Objective | `sh` | `mshell` |
|-----------|-----|----------|
| Print the number of files in the current directory | `ls \| wc -l`                                                | `"*" glob len wl` |
| `find`/`xargs`                                     |  `find . -t x -name '*.sh' -print0 \|  xargs -0 mycommand`   | `[mycommand [find . -t x -name "*.sh" ]*; lines];` |
| `head` | `head -n 10` | `sl :10 uw` |
| `tail` | `tail -n 10` | `sl :-10 uw` |
| `wc` | `wc -l` | `sl len wl` |
| `grep` | `grep 'pattern'` | `sl ("pattern" in) filter uw` |
| `cut` | `cut -d ';' -f 2` | `sl (";" split :1: wl) each` |

# Editor Support

- VS Code extension is available on the Marketplace: [`MitchellPaulus.mshell`](https://marketplace.visualstudio.com/items?itemName=MitchellPaulus.mshell).
- Sublime Text syntax highlighting is available in [`sublime/msh.sublime-syntax`](https://github.com/mitchpaulus/mshell/tree/main/sublime/msh.sublime-syntax).
- Notepad++ light and dark user-defined language files are available in [`Notepad++/`](https://github.com/mitchpaulus/mshell/tree/main/Notepad++).
- Vim/Neovim syntax highlighting is available via [`mshell-vim`](https://github.com/mitchpaulus/mshell-vim).
- A language server is bundled with the CLI, providing builtin hover information, completion on `@` variables, and scope-aware variable renaming.


# TODO

- Job control. We don't have CTRL-z or `bg`/`fg` functionality right now.
- Improved error messages.

# References/Inspirations

- [Nushell](https://www.nushell.sh/)
- [Elvish](https://elv.sh/)
- [fish](https://fishshell.com/)
- [Porth](https://gitlab.com/tsoding/porth)
- [Factor](https://factorcode.org/)
- [`dt`](https://dt.plumbing/)
