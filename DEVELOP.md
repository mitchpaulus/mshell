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

type : typeItem ('|' typeItem)* ;

typeItem
    : 'int'
    | 'float'
    | 'string'
    | 'bool'
    | typeList
    | typeQuote
    | genericType
    ;

typeQuote : '(' type* -- type* ')' ;
typeList : homogeneousList | heterogeneousList ;
homogeneousList : '[' type  ']' ;
heterogeneousList : '&' '[' type* ']' ;
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
