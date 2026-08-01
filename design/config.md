# Configuration

In shells, configuration is nearly always done at *runtime* and is dynamic.
Things like 'aliases' are typically added using a built in command.

Things to exist in configuration:

```
'
{
  'prompt' : ( -- str),
  'abbreviations' : [

  ]
'
```

# Executable Lookups

Two steps:

- Find the absolute path to file
- Understand how to transform CLI to run file

## Linux/Unix like:

Finding:

- Explicitly set, and has executable
- PATH: all files with an executable bit

Running:

- If it has shebang -> Run directly, let OS handle interpreter setup.
- No shebang -> Can check for extension/explicit pattern configuration.

## Windows:

Finding:

- Explicit set
- PATH: All files with specific extension: Default .exe, .bat, .cmd, .msh.
  Have to get others from configuration.
  Here we also add extensions in order to try to match.
- What about trying to get many files in directory that don't have extension, because they are done like linux?
  For example, my script directory? A special comment string?
  - Grab anything with a SHEBANG

Running:

- Special pattern by extension/explicit configuration.
- Else:
  - Check for SHEBANG. If we have a shebang, then we know it was a script file, and is text.
  - We also know that it must essentially be text.
    - Then can check for a shebang map configuration. Full string -> new CLI list

Example:


Map exact name -> [ 'asdf' 'asdfasdf'  ]

## Shebang info

Short answer: Linux does a raw byte check for #!, parses only the first line into interpreter + at most one optional-arg string,
  then rebuilds argv as interpreter [optional-arg] script_path argv[1...] and re-execs the interpreter. It does not split the rest of
  the line into multiple args. (sources.debian.org (https://sources.debian.org/src/linux/6.12.8-1/fs/binfmt_script.c))

  More exact flow (from kernel source + execve man page):

  - Detection: the binfmt_script handler checks bprm->buf[0] == '#' and bprm->buf[1] == '!'; if not, it returns -ENOEXEC. That’s a
    literal first‑two‑bytes test. (sources.debian.org (https://sources.debian.org/src/linux/6.12.8-1/fs/binfmt_script.c))
  - Parsing: it looks for a newline (or buffer end), trims trailing spaces/tabs, skips leading spaces/tabs after #!, then takes the
    interpreter path up to the first space/tab/NUL. If anything remains, it is treated as one optional argument string (Linux passes
    the entire remainder as a single argument, even if it contains spaces). (sources.debian.org (https://sources.debian.org/src/
    linux/6.12.8-1/fs/binfmt_script.c))
  - Argv construction: it removes the original argv[0] and splices in interpreter, optional-arg (if any), then the script path (the
    pathname passed to execve), followed by the original argv[1...]. The original argv[0] is not recoverable. (sources.debian.org
    (https://sources.debian.org/src/linux/6.12.8-1/fs/binfmt_script.c))
  - Exec: it opens the interpreter path and restarts execution using that interpreter. (sources.debian.org (https://
    sources.debian.org/src/linux/6.12.8-1/fs/binfmt_script.c))
  - Length limit: Linux caps the text after #! (before newline) to 127 chars pre‑5.1 and 255 chars since 5.1; excess is ignored.
    (man7.org (https://man7.org/linux/man-pages/man2/execve.2.html?utm_source=openai))

  So your “splits on whitespace and adds the filepath as the last argument” is close but not quite: it splits only once (interpreter
  vs optional-arg), and the script path is inserted before the original argv[1...], not always last (only last if no extra args were
  supplied). (man7.org (https://man7.org/linux/man-pages/man2/execve.2.html?utm_source=openai))
