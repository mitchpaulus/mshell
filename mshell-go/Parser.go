package main

import (
    "errors"
    "fmt"
    "strings"
    "os"
)

type MShellItem interface { }

type MShellFile struct {
    definitions []MShellDefinition
    items []MShellObject
}

type MShellDefinition struct {
    name string
    items []MShellObject
}

func (def *MShellDefinition) ToJson() string {
    return fmt.Sprintf("{\"name\": \"%s\", \"items\": %s}", def.name, ToJson(def.items))
}

func ToJson(objList []MShellObject) string {
    builder := strings.Builder{}
    builder.WriteString("[")
    if len(objList) > 0 {
        builder.WriteString(objList[0].ToJson())
        for i := 1; i < len(objList); i++ {
            builder.WriteString(", ")
            builder.WriteString(objList[i].ToJson())
        }
    }
    builder.WriteString("]")
    return builder.String()
}

func (file *MShellFile) ToJson() string {
    // Start builder for definitions
    definitions := strings.Builder{}
    definitions.WriteString("[")
    for i, def := range file.definitions {
        definitions.WriteString(def.ToJson())
        if i != len(file.definitions) - 1 {
            definitions.WriteString(", ")
        }
    }
    definitions.WriteString("]")

    // Start builder for items
    items := strings.Builder{}
    items.WriteString("[")
    for i, item := range file.items {
        items.WriteString(item.ToJson())
        if i != len(file.items) - 1 {
            items.WriteString(", ")
        }
    }
    items.WriteString("]")

    return fmt.Sprintf("{\"definitions\": %s, \"items\": %s}", definitions.String(), items.String())
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

func (parser *MShellParser) ParseFile() (*MShellFile, error) {
    file := &MShellFile{}

    for parser.curr.Type != EOF && parser.curr.Type != END {
        switch parser.curr.Type {
        case RIGHT_SQUARE_BRACKET, RIGHT_PAREN:
            message := fmt.Sprintf("Unexpected token %s while parsing file", parser.curr.Type)
            return file, errors.New(message)
        case LEFT_SQUARE_BRACKET:
            list, err := parser.ParseList()
            if err != nil {
                return file, err
            }
            fmt.Fprintf(os.Stderr, "List: %s\n", list.ToJson())
            file.items = append(file.items, list)
        case DEF:
            _ = parser.Match(parser.curr, DEF)
            if parser.curr.Type != LITERAL {
                return file, errors.New(fmt.Sprintf("Expected LITERAL, got %s", parser.curr.Type))
            }

            def := MShellDefinition{name: parser.curr.Lexeme, items: []MShellObject{}}
            _ = parser.Match(parser.curr, LITERAL)

            for {
                if parser.curr.Type == END {
                    break
                } else if parser.curr.Type == EOF {
                    return file, errors.New(fmt.Sprintf("Unexpected EOF while parsing definition %s", def.name))
                } else {
                    item, err := parser.ParseItem()
                    if err != nil {
                        return file, err
                    }
                    def.items = append(def.items, item)
                }
            }

            file.definitions = append(file.definitions, def)
            _ = parser.Match(parser.curr, END)
            // return file, errors.New("DEF Not implemented")
            // parser.ParseDefinition()
        // case 
            // parser.ParseItem()
        default:
            item, err := parser.ParseItem()
            if err != nil {
                return file, err
            }
            file.items = append(file.items, item)
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
        return parser.ParseSimple(), nil
    }
}

func (parser *MShellParser) ParseSimple() (*MShellSimple) {
    s := &MShellSimple { Token: parser.curr }
    parser.NextToken()
    return s
}

func (parser *MShellParser) ParseQuote() (*MShellQuotation2, error) {
    quote := &MShellQuotation2{}
    err := parser.Match(parser.curr, LEFT_PAREN)
    if err != nil {
        return quote, err
    }
    for parser.curr.Type != RIGHT_PAREN {
        item, err := parser.ParseItem()
        if err != nil {
            return quote, err
        }
        quote.Objects = append(quote.Objects, item)
    }
    err = parser.Match(parser.curr, RIGHT_PAREN)
    if err != nil {
        return quote, err
    }
    return quote, nil
}
