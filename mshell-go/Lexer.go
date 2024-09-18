package main

import (
    "fmt"
    "os"
    "strconv"
    "strings"
    "unicode"
)

type TokenType int

const (
    EOF TokenType = iota
    ERROR
    LEFT_SQUARE_BRACKET
    RIGHT_SQUARE_BRACKET
    LEFT_PAREN
    RIGHT_PAREN
    EXECUTE
    PIPE
    QUESTION
    POSITIONAL
    STRING
    MINUS
    PLUS
    EQUALS
    INTERPRET
    IF
    LOOP
    READ
    STR
    BREAK
    NOT
    AND
    OR
    GREATERTHANOREQUAL
    LESSTHANOREQUAL
    LESSTHAN
    GREATERTHAN
    TRUE
    FALSE
    VARRETRIEVE
    VARSTORE
    INTEGER
    DOUBLE
    LITERAL
)

func (t TokenType) String() string {
    switch t {
    case EOF:
        return "EOF"
    case ERROR:
        return "ERROR"
    case LEFT_SQUARE_BRACKET:
        return "LEFT_SQUARE_BRACKET"
    case RIGHT_SQUARE_BRACKET:
        return "RIGHT_SQUARE_BRACKET"
    case LEFT_PAREN:
        return "LEFT_PAREN"
    case RIGHT_PAREN:
        return "RIGHT_PAREN"
    case EXECUTE:
        return "EXECUTE"
    case PIPE:
        return "PIPE"
    case QUESTION:
        return "QUESTION"
    case POSITIONAL:
        return "POSITIONAL"
    case STRING:
        return "STRING"
    case MINUS:
        return "MINUS"
    case PLUS:
        return "PLUS"
    case EQUALS:
        return "EQUALS"
    case INTERPRET:
        return "INTERPRET"
    case IF:
        return "IF"
    case LOOP:
        return "LOOP"
    case READ:
        return "READ"
    case STR:
        return "STR"
    case BREAK:
        return "BREAK"
    case NOT:
        return "NOT"
    case AND:
        return "AND"
    case OR:
        return "OR"
    case GREATERTHANOREQUAL:
        return "GREATERTHANOREQUAL"
    case LESSTHANOREQUAL:
        return "LESSTHANOREQUAL"
    case LESSTHAN:
        return "LESSTHAN"
    case GREATERTHAN:
        return "GREATERTHAN"
    case TRUE:
        return "TRUE"
    case FALSE:
        return "FALSE"
    case VARRETRIEVE:
        return "VARRETRIEVE"
    case VARSTORE:
        return "VARSTORE"
    case INTEGER:
        return "INTEGER"
    case DOUBLE:
        return "DOUBLE"
    case LITERAL:
        return "LITERAL"
    default:
        return "UNKNOWN"
    }
}

type Token struct {
    Line      int
    Column    int
    Start     int
    Lexeme    string
    TokenType TokenType
}

type Lexer struct {
    start   int
    current int
    col     int
    line    int
    input   []rune
}

func NewLexer(input string) *Lexer {
    return &Lexer{
        input: []rune(input),
        line:  1,
    }
}

func (l *Lexer) atEnd() bool {
    return l.current >= len(l.input)
}

func (l *Lexer) makeToken(tokenType TokenType) Token {
    lexeme := string(l.input[l.start:l.current])
    return Token{
        Line:      l.line,
        Column:    l.col,
        Start:     l.start,
        Lexeme:    lexeme,
        TokenType: tokenType,
    }
}

func (l *Lexer) advance() rune {
    c := l.input[l.current]
    l.current++
    l.col++
    return c
}

func (l *Lexer) peek() rune {
    if l.atEnd() {
        return 0
    }
    return l.input[l.current]
}

func (l *Lexer) peekNext() rune {
    if l.current+1 >= len(l.input) {
        return 0
    }
    return l.input[l.current+1]
}

func (l *Lexer) scanToken() Token {
    l.eatWhitespace()
    l.start = l.current
    if l.atEnd() {
        return l.makeToken(EOF)
    }

    c := l.advance()

    if c == '"' {
        return l.parseString()
    }

    switch c {
    case '[':
        return l.makeToken(LEFT_SQUARE_BRACKET)
    case ']':
        return l.makeToken(RIGHT_SQUARE_BRACKET)
    case '(':
        return l.makeToken(LEFT_PAREN)
    case ')':
        return l.makeToken(RIGHT_PAREN)
    case ';':
        return l.makeToken(EXECUTE)
    case '|':
        return l.makeToken(PIPE)
    case '?':
        return l.makeToken(QUESTION)
    case '$':
        if unicode.IsDigit(l.peek()) {
            return l.parsePositional()
        }
        return l.parseLiteralOrNumber()
    default:
        return l.parseLiteralOrNumber()
    }
}

func (l *Lexer) parsePositional() Token {
    for {
        if l.atEnd() {
            break
        }
        if !unicode.IsDigit(l.peek()) {
            break
        }
        l.advance()
    }
    return l.makeToken(POSITIONAL)
}

func (l *Lexer) parseString() Token {
    // When this is called, we've already consumed a single double quote.
    inEscape := false
    for {
        if l.atEnd() {
            fmt.Fprintf(os.Stderr, "%d:%d: Unterminated string.\n", l.line, l.col)
            return l.makeToken(ERROR)
        }
        c := l.advance()
        if inEscape {
            if c != 'n' && c != 't' && c != 'r' && c != '\\' && c != '"' {
                fmt.Fprintf(os.Stderr, "%d:%d: Invalid escape character '%c'.\n", l.line, l.col, c)
                return l.makeToken(ERROR)
            }
            inEscape = false
        } else {
            if c == '"' {
                break
            }
            if c == '\\' {
                inEscape = true
            }
        }
    }
    return l.makeToken(STRING)
}

func (l *Lexer) parseLiteralOrNumber() Token {
    for {
        if l.atEnd() {
            break
        }
        c := l.peek()
        if !unicode.IsSpace(c) && c != ']' && c != ')' && c != '<' && c != '>' && c != ';' && c != '?' {
            l.advance()
        } else {
            break
        }
    }

    literal := string(l.input[l.start:l.current])

    switch literal {
    case "-":
        return l.makeToken(MINUS)
    case "+":
        return l.makeToken(PLUS)
    case "=":
        return l.makeToken(EQUALS)
    case "x":
        return l.makeToken(INTERPRET)
    case "if":
        return l.makeToken(IF)
    case "loop":
        return l.makeToken(LOOP)
    case "read":
        return l.makeToken(READ)
    case "str":
        return l.makeToken(STR)
    case "break":
        return l.makeToken(BREAK)
    case "not":
        return l.makeToken(NOT)
    case "and":
        return l.makeToken(AND)
    case "or":
        return l.makeToken(OR)
    case ">=":
        return l.makeToken(GREATERTHANOREQUAL)
    case "<=":
        return l.makeToken(LESSTHANOREQUAL)
    case "<":
        return l.makeToken(LESSTHAN)
    case ">":
        return l.makeToken(GREATERTHAN)
    case "true":
        return l.makeToken(TRUE)
    case "false":
        return l.makeToken(FALSE)
    default:
        if strings.HasSuffix(literal, "!") {
            return l.makeToken(VARRETRIEVE)
        }
        if strings.HasPrefix(literal, "@") {
            return l.makeToken(VARSTORE)
        }
        if _, err := strconv.Atoi(literal); err == nil {
            return l.makeToken(INTEGER)
        }
        if _, err := strconv.ParseFloat(literal, 64); err == nil {
            return l.makeToken(DOUBLE)
        }
        return l.makeToken(LITERAL)
    }
}

func (l *Lexer) Tokenize() []Token {
    var tokens []Token
    for {
        t := l.scanToken()
        tokens = append(tokens, t)
        if t.TokenType == ERROR || t.TokenType == EOF {
            break
        }
    }
    return tokens
}

func (l *Lexer) eatWhitespace() {
    for {
        if l.atEnd() {
            return
        }
        c := l.peek()
        switch c {
        case ' ', '\t', '\r':
            l.advance()
        case '#':
            for !l.atEnd() && l.peek() != '\n' {
                l.advance()
            }
        case '\n':
            l.line++
            l.col = 0
            l.advance()
        default:
            return
        }
    }
}
