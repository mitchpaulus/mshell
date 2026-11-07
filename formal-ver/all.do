# Compile every file of the proof listed in _CoqProject.
`_CoqProject` readFile lines (".v" endsWith) filter (v! $"{@v}o") map vos!
['redo-ifchange' '_CoqProject' @vos]!
