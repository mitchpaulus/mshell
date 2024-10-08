Grammar:

```
file : (token | definition)* ;
token : simple | list | quote ;
simple : DOUBLE_QUOTE_STRING | SINGLE_QUOTE_STRING | INTEGER | LITERAL | BOOLEAN | STRING ;
list : '[' token* ']' ;
quote : '(' token* ')' ;
definition : 'def' literal file 'end' ;
```
