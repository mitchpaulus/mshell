package main

import (
    "errors"
    "fmt"
)

type MShellItem interface { }

type MShellFile struct {
    definitions []MShellDefinition
    items []MShellItem
}

type MShellDefinition struct {
    name string
    file MShellFile
}

type MShellParser struct {
    lexer *Lexer
    curr Token
}

func (parser *MShellParser) NextToken() {
    parser.curr = parser.lexer.scanToken()
}

func (parser *MShellParser) Match(token Token, tokenType TokenType) error {
    if token.Type != tokenType {
        message := fmt.Sprintf("Expected %s, got %s", tokenType, token.Type)
        return errors.New(message)
    }
    parser.NextToken()
    return nil
}

func (parser *MShellParser) ParseFile() (MShellFile, error) {
    file := MShellFile{}

    for parser.curr.Type != EOF {
        switch parser.curr.Type {
        case RIGHT_SQUARE_BRACKET, RIGHT_PAREN, END :
            message := fmt.Sprintf("Unexpected token %s while parsing file", parser.curr.Type)
            return file, errors.New(message)
        case LEFT_SQUARE_BRACKET:
            list, error := parser.ParseList()
            if error != nil {
                return file, error
            }
            file.items = append(file.items, list)
        case DEF:
            // parser.ParseDefinition()
        // case 
            // parser.ParseItem()
        default:
            message := fmt.Sprintf("Unexpected token %s while parsing file", parser.curr.Type)
            return file, errors.New(message)
        }
    }
    return file, nil
}

func (parser *MShellParser) ParseList() (*MShellList, error) {
    list := &MShellList{}
    err := parser.Match(parser.curr, LEFT_SQUARE_BRACKET)
    if err != nil {
        return list, err
    }
    for parser.curr.Type != RIGHT_SQUARE_BRACKET {
        item, err := parser.ParseItem()
        if err != nil {
            return list, err
        }
        list.Items = append(list.Items, item)
    }
    err = parser.Match(parser.curr, RIGHT_SQUARE_BRACKET)
    if err != nil {
        return list, err
    }
    return list, nil
}

func (parser *MShellParser) ParseItem() (MShellObject, error) {
    switch parser.curr.Type {
    case LEFT_SQUARE_BRACKET:
        return parser.ParseList()
    case LEFT_PAREN:
        return parser.ParseQuote()
    default:
        return parser.ParseLiteral()
    }
}

func (parser *MShellParser) ParseLiteral() (*MShellLiteral, error) {
    literal := &MShellLiteral{LiteralText: parser.curr.Lexeme}
    parser.NextToken()
    return literal, nil
}

func (parser *MShellParser) ParseQuote() (*MShellQuotation, error) {
    quote := &MShellQuotation{}
    err := parser.Match(parser.curr, LEFT_PAREN)
    if err != nil {
        return quote, err
    }
    for parser.curr.Type != RIGHT_PAREN {
        quote.Tokens = append(quote.Tokens, parser.curr)
    }
    err = parser.Match(parser.curr, RIGHT_PAREN)
    if err != nil {
        return quote, err
    }
    return quote, nil
}
