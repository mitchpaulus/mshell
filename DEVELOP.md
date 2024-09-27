Grammar:

```
file : (token | definition)* ;
token : simple | list | quote ;
simple : DOUBLE_QUOTE_STRING | SINGLE_QUOTE_STRING | LITERAL ;
list : '[' token* ']' ;
quote : '(' token* ')' ;
definition : 'def' file 'end' ;
```
