# Compile the proof, then print the assumptions the main theorems depend on.
# Each should end with "Closed under the global context".
['redo-ifchange' 'all']!

[
    "From MshellCore Require Import Soundness Decide Escape Repl."
    "Print Assumptions soundness."
    "Print Assumptions if_join_alg."
    "Print Assumptions if_join2_alg."
    "Print Assumptions subq_set_sound."
    "Print Assumptions rsubq_set_sound."
    "Print Assumptions kind_list_once."
    "Print Assumptions kind_enum_once."
    "Print Assumptions repl_error."
    "Print Assumptions repl_error_commit."
] unlines theorems!

# Compiled in .build/check. Its output goes to stderr: stdout would become a
# file named check.
`.build` `check` / out!
@out mkdirp
@out `Assumptions_check.v` / src!
@theorems @src writeFile
@out `Assumptions_check.vo` / vo!
['msh' 'rocq.msh' 'rocq' 'c' '-Q' '.' 'MshellCore' '-o' @vo @src] 1>&2 !
