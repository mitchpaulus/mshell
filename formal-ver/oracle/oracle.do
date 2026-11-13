# Extract the decision procedures from the proof (Extract.v) to OCaml and
# build the oracle program.
# The extracted code and OCaml's intermediate files go in .build.

# rocq dep prints "Extract.vo ...: Extract.v ../A.vo ../B.vo /stdlib/...".
# Keep the proof's own .vo files: those with a relative path.
['msh' '../rocq.msh' 'rocq' 'dep' '-Q' '..' 'MshellCore' 'Extract.v'] * ! lines :0: ": " splitOnce ? :1: wsplit
(d! @d ".vo" endsWith @d "/" startsWith not and @d ":" in not and) filter deps!
['redo-ifchange' 'Extract.v' 'oracle.ml' @deps]!

`.build` out!
@out mkdirp
[`decide.mli` `decide.ml` `oracle.ml`] (f! @out @f /) map mls!
`oracle.ml` @mls :2: cp
@out `Extract.vo` / vo!
['msh' '../rocq.msh' 'rocq' 'c' '-Q' '..' 'MshellCore' '-output-directory' @out '-o' @vo 'Extract.v']!
['msh' '../rocq.msh' 'ocamlfind' 'ocamlopt' '-package' 'str' '-w' '-a' '-I' @out @mls '-o' $3]!
