# Compile NAME.v to NAME.vo.
# The files it requires come from rocq dep, so changing NAME.v or any proof
# file it requires rebuilds NAME.vo.
# Rocq names its output after the source, and a .vo records the library name
# of the directory it is written to. So it compiles into .build/NAME, mapped
# to MshellCore, and the .vo is moved to $3. Mapping . skips .build, because
# its name starts with a dot.
$2 base!
$"{@base}.v" src!

# rocq dep prints "NAME.vo NAME.glob ...: NAME.v ./A.vo B.vo /stdlib/...".
# Keep the proof's own .vo files: those with a relative path.
['msh' 'rocq.msh' 'rocq' 'dep' '-Q' '.' 'MshellCore' @src] * ! lines :0: ": " splitOnce ? :1: wsplit
(d! @d ".vo" endsWith @d "/" startsWith not and @d ":" in not and) filter deps!
['redo-ifchange' @src @deps]!

`.build` @base toPath / out!
@out mkdirp
@out $"{@base}.vo" toPath / vo!
['msh' 'rocq.msh' 'rocq' 'c' '-Q' '.' 'MshellCore' '-Q' @out 'MshellCore' '-o' @vo @src]!
@vo $3 mv
