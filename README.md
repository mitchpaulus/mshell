# `mshell`

> [!WARNING]
> This is still very much an experiment.
> I still am changing significant parts of the syntax and semantics.

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

The other file to copy is the standard library, which is at `lib/std.msh` in this repo.
Download that, put it somewhere. Then set the environment variable `$MSHSTDLIB` to point to that file location.

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

Note that you'll also need the environment variable `MSHSTDLIB` pointing to the file at `lib/std.msh`.


*Simpler execution of common shell idioms*

| Objective | `sh` | `mshell` |
|-----------|-----|----------|
| Print the number of files in the current directory | `ls \| wc -l`                                                | `"*" glob len wl` |
| `find`/`xargs`                                     |  `find . -t x -name '*.sh' -print0 \|  xargs -0 mycommand`   | `[mycommand [find . -t x -name "*.sh"]]o;` |
| `head` | `head -n 10` | `sl :10 uw` |
| `tail` | `tail -n 10` | `sl :-10 uw` |
| `wc` | `wc -l` | `sl len wl` |
| `grep` | `grep 'pattern'` | `sl ("pattern" in) filter uw` |
| `cut` | `cut -d ';' -f 2` | `sl (";" split :1: wl) each` |

# Editor Support

- Sublime Text syntax highlighting is available in [`sublime/msh.sublime-syntax`](https://github.com/mitchpaulus/mshell/tree/main/sublime/msh.sublime-syntax).
- Notepad++ light and dark user-defined language files are available in [`Notepad++/`](https://github.com/mitchpaulus/mshell/tree/main/Notepad++).
- Vim/Neovim syntax highlighting is available via [`mshell-vim`](https://github.com/mitchpaulus/mshell-vim).


# TODO

- Job control. Right now, if you CTRL-c a long running process, it kills the entire shell.
- Type checking.
- Improved error messages.
- Built in file manager (like [Elvish](https://elv.sh/)).

# References/Inspirations

- [Porth](https://gitlab.com/tsoding/porth)
- [Factor](https://factorcode.org/)
- [`dt`](https://dt.plumbing/)
