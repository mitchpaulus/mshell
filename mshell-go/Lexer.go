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
    SINGLEQUOTESTRING
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
    INDEXER
    ENDINDEXER
    STARTINDEXER
    SLICEINDEXER
    STDOUTLINES
    STDOUTSTRIPPED
    STDOUTCOMPLETE
    EXPORT
    TILDEEXPANSION
    STOP_ON_ERROR
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
    case SINGLEQUOTESTRING:
        return "SINGLEQUOTESTRING"
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
    case INDEXER:
        return "INDEXER"
    case ENDINDEXER:
        return "ENDINDEXER"
    case STARTINDEXER:
        return "STARTINDEXER"
    case SLICEINDEXER:
        return "SLICEINDEXER"
    case STDOUTLINES:
        return "STDOUTLINES"
    case STDOUTSTRIPPED:
        return "STDOUTSTRIPPED"
    case STDOUTCOMPLETE:
        return "STDOUTCOMPLETE"
    case EXPORT:
        return "EXPORT"
    default:
        return "UNKNOWN"
    }
}

type Token struct {
    Line      int
    Column    int
    Start     int
    Lexeme    string
    Type TokenType
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
        Type: tokenType,
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

var notAllowedLiteralChars = map[rune]bool{
    '[': true,
    ']': true,
    '(': true,
    ')': true,
    '<': true,
    '>': true,
    ';': true,
    '?': true,
}

func isAllowedLiteral(r rune) bool {
    if unicode.IsSpace(r) { return false }
    _, ok := notAllowedLiteralChars[r]
    return !ok
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

    if unicode.IsDigit(c) { return l.parseNumberOrStartIndexer() }

    switch c {
    case '\'':
        return l.parseSingleQuoteString()
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
    case ':':
        return l.parseIndexerOrLiteral()
    case 'o':
        peek := l.peek()
        if peek == 's' {
            l.advance()
            if isAllowedLiteral(l.peek()) {
                return l.consumeLiteral()
            } else {
                return l.makeToken(STDOUTSTRIPPED)
            }
        } else if peek == 'c' {
            l.advance()
            if isAllowedLiteral(l.peek()) {
                return l.consumeLiteral()
            } else {
                return l.makeToken(STDOUTCOMPLETE)
            }
        } else if peek == 'r' {
            l.advance()
            if isAllowedLiteral(l.peek()) {
                return l.consumeLiteral()
            } else {
                return l.makeToken(OR)
            }
        } else if isAllowedLiteral(peek) {
            return l.consumeLiteral()
        } else {
            return l.makeToken(STDOUTLINES)
        }
    default:
        return l.parseLiteralOrNumber()
    }
}

func (l *Lexer) parseSingleQuoteString() Token {
    // When this is called, we've already consumed a single quote.
    for {
        if l.atEnd() {
            fmt.Fprintf(os.Stderr, "%d:%d: Unterminated string.\n", l.line, l.col)
            return l.makeToken(ERROR)
        }

        c := l.advance()
        if c == '\'' {
            break
        }
    }

    return l.makeToken(SINGLEQUOTESTRING)
}

func (l *Lexer) consumeLiteral() Token {
    for {
        if l.atEnd() {
            break
        }
        c := l.peek()
        if isAllowedLiteral(c) {
            l.advance()
        } else {
            break
        }
    }

    return l.makeToken(LITERAL)
}

func (l *Lexer) parseNumberOrStartIndexer() Token {
    // Read all the digits
    for {
        if l.atEnd() { break }
        if !unicode.IsDigit(l.peek()) { break }
        l.advance()
    }

    peek := l.peek()
    if peek == ':' {
        l.advance()

        c := l.peek()
        if unicode.IsDigit(c) {
            // Read all the digits
            for {
                if l.atEnd() { break }
                if !unicode.IsDigit(l.peek()) { break } 
                l.advance()
            }
            return l.makeToken(SLICEINDEXER)
        } else {
            return l.makeToken(STARTINDEXER)
        }
    }

    if !isAllowedLiteral(peek) {
        return l.makeToken(INTEGER)
    } else {
        return l.consumeLiteral()
    }
}

func (l *Lexer) parseIndexerOrLiteral() Token {
    c := l.advance()

    // Return literal if at end
    if l.atEnd() {
        return l.makeToken(LITERAL)
    }

    if unicode.IsDigit(c) {
        // Read all the digits
        for {
            if l.atEnd() { break }
            if !unicode.IsDigit(l.peek()) { break }
            c = l.advance()
        }
    } else {
        return l.consumeLiteral()
    }

    if l.peek() == ':' {
        l.advance()
        return l.makeToken(INDEXER)
    } else {
        return l.makeToken(ENDINDEXER)
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
        if isAllowedLiteral(c) {
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
    case "export":
        return l.makeToken(EXPORT)
    case "if":
        return l.makeToken(IF)
    case "loop":
        return l.makeToken(LOOP)
    case "read":
        return l.makeToken(READ)
    case "str":
        return l.makeToken(STR)
    case "soe":
        return l.makeToken(STOP_ON_ERROR)
    case "break":
        return l.makeToken(BREAK)
    case "not":
        return l.makeToken(NOT)
    case "and":
        return l.makeToken(AND)
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
        if t.Type == ERROR || t.Type == EOF {
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
