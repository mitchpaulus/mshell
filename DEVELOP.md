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
```

Key Types:

```
MShellObject
    MShellSimple
    MShellLiteral
    MShellBool
    MShellQuotation
    MShellString

```

