#!/usr/bin/env mshell
$"../{$2 stem basename}.inc.html" f!

[redo-ifchange `../base.html` @f]!

`../base.html` readFile lines (
    l!
    @l "CONTENTS" in
    (@f readFile w)
    (@l wl)
    iff
) each
