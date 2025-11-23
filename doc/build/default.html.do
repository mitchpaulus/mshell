#!/usr/bin/env mshell
pwd startDir!

$"../{$2 stem basename}.inc.html" f!

[redo-ifchange `../base.html` @f]!

@f dirname cd
[msh templateEval.msh] @f toPath < * ! stdout!
@startDir cd

# In base.html, there should be one location for CONTENTS, replace with the file.
`../base.html` readFile lines (
    l!
    @l "CONTENTS" in
    # (@f readFile w)
    (@stdout)
    (@l wl)
    iff
) each
