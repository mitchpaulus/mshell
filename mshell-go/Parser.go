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
