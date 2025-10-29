Grammar:

```
file : (item | definition)* ;
item : simple | list | quote ;
list : '[' item* ']' ;
quote : '(' item* ')' ;
definition : 'def' literal file 'end' ;
simple
    | INTEGER
    | LITERAL
    | BOOLEAN
    | STRING
    | EXECUTE ('x')
    | PIPE ('|')
    | QUESTION ('?')
    | POSITIONAL
    | STRING
    | SINGLEQUOTESTRING
    | MINUS ('-')
    | PLUS ('+')
    | EQUALS ('=')
    | INTERPRET
    | IF ('if')
    | LOOP ('loop')
    | READ ('read')
    | STR ('str')
    | BREAK ('break')
    | NOT ('not')
    | AND ('and')
    | OR ('or')
    | GREATERTHANOREQUAL
    | LESSTHANOREQUAL
    | LESSTHAN
    | GREATERTHAN
    | TRUE ('true')
    | FALSE ('false')
    | VARRETRIEVE
    | VARSTORE
    | INTEGER
    | DOUBLE
    | LITERAL
    | INDEXER
    | ENDINDEXER
    | STARTINDEXER
    | SLICEINDEXER
    | STDOUTLINES
    | STDOUTSTRIPPED
    | STDOUTCOMPLETE
    | EXPORT
    | TILDEEXPANSION
    | STOP_ON_ERROR
    | DEF
    | END
    | STDERRREDIRECT
    ;

type : typeItem ('|' typeItem)* ':' identifier;

typeItem
    : 'int'
    | 'float'
    | 'str'
    | 'bool'
    | 'binary'
    | 'path'
    | typeList
    | typeQuote
    | typeMaybe
    | genericType
    | typeDict
    ;

typeQuote : '(' type* -- type* ')' ;
typeDict : '{' keyPair (',' keyPair )* '}' | '{' type '}'
typeMaybe : 'Maybe[' type ']'

keyPair : string ':' type
        | '*" ':' type

typeList : '[' type+  ']'  # If just one item assume all in homogeneous list. Otherwise, assume tuple of exact length.

```

Key Types:

```
MShellObject
    MShellSimple
    MShellLiteral
    MShellBool
    MShellQuotation
    MShellString
    MShellFloat
```

## References

[fish shell built in](https://github.com/fish-shell/fish-shell/tree/master/src/builtins)
[Stroustrop's Rule](https://buttondown.com/hillelwayne/archive/stroustrops-rule/)
[WebAssembly Type Checking](https://binji.github.io/posts/webassembly-type-checking/)
[Carapace](https://carapace.sh/): Completion library we may be able to reference?
