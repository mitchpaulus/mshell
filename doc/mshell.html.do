#!/usr/bin/env mshell

[pandoc --from=markdown --to=html -o $3 `mshell.md`]!
