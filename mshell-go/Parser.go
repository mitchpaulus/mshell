package main

import (
	"errors"
	"fmt"
	"strings"
	// "os"
)

type JsonableList []Jsonable

type MShellParseItem interface {
	ToJson() string
	DebugString() string
}

type MShellFile struct {
	Definitions []MShellDefinition
	Items       []MShellParseItem
}

type MShellParseList struct {
	Items []MShellParseItem
}

func (list *MShellParseList) ToJson() string {
	return ToJson(list.Items)
}

func (list *MShellParseList) DebugString() string {
	builder := strings.Builder{}
	builder.WriteString("[")
	if len(list.Items) > 0 {
		builder.WriteString(list.Items[0].DebugString())
		for i := 1; i < len(list.Items); i++ {
			builder.WriteString(", ")
			builder.WriteString(list.Items[i].DebugString())
		}
	}
	builder.WriteString("]")
	return builder.String()
}

type MShellParseQuote struct {
	Items []MShellParseItem
}

func (quote *MShellParseQuote) ToJson() string {
	return ToJson(quote.Items)
}

func (quote *MShellParseQuote) DebugString() string {
	builder := strings.Builder{}
	builder.WriteString("(")
	if len(quote.Items) > 0 {
		builder.WriteString(quote.Items[0].DebugString())
		for i := 1; i < len(quote.Items); i++ {
			builder.WriteString(", ")
			builder.WriteString(quote.Items[i].DebugString())
		}
	}
	builder.WriteString(")")
	return builder.String()
}

type MShellDefinition struct {
	Name  string
	Items []MShellParseItem
}

func (def *MShellDefinition) ToJson() string {
	return fmt.Sprintf("{\"name\": \"%s\", \"items\": %s}", def.Name, ToJson(def.Items))
}

func ToJson(objList []MShellParseItem) string {
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
	for i, def := range file.Definitions {
		definitions.WriteString(def.ToJson())
		if i != len(file.Definitions)-1 {
			definitions.WriteString(", ")
		}
	}
	definitions.WriteString("]")

	// Start builder for items
	items := strings.Builder{}
	items.WriteString("[")
	for i, item := range file.Items {
		items.WriteString(item.ToJson())
		if i != len(file.Items)-1 {
			items.WriteString(", ")
		}
	}
	items.WriteString("]")

	return fmt.Sprintf("{\"definitions\": %s, \"items\": %s}", definitions.String(), items.String())
}

type MShellParser struct {
	lexer *Lexer
	curr  Token
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
			// fmt.Fprintf(os.Stderr, "List: %s\n", list.ToJson())
			file.Items = append(file.Items, list)
		case DEF:
			_ = parser.Match(parser.curr, DEF)
			if parser.curr.Type != LITERAL {
				return file, errors.New(fmt.Sprintf("Expected LITERAL, got %s", parser.curr.Type))
			}

			def := MShellDefinition{Name: parser.curr.Lexeme, Items: []MShellParseItem{}}
			_ = parser.Match(parser.curr, LITERAL)

			for {
				if parser.curr.Type == END {
					break
				} else if parser.curr.Type == EOF {
					return file, errors.New(fmt.Sprintf("Unexpected EOF while parsing definition %s", def.Name))
				} else {
					item, err := parser.ParseItem()
					if err != nil {
						return file, err
					}
					def.Items = append(def.Items, item)
				}
			}

			file.Definitions = append(file.Definitions, def)
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
			file.Items = append(file.Items, item)
		}
	}
	return file, nil
}

func (parser *MShellParser) ParseList() (*MShellParseList, error) {
	list := &MShellParseList{}
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

func (parser *MShellParser) ParseItem() (MShellParseItem, error) {
	switch parser.curr.Type {
	case LEFT_SQUARE_BRACKET:
		return parser.ParseList()
	case LEFT_PAREN:
		return parser.ParseQuote()
	default:
		return parser.ParseSimple(), nil
	}
}

func (parser *MShellParser) ParseSimple() Token {
	s := parser.curr
	parser.NextToken()
	return s
}

func (parser *MShellParser) ParseQuote() (*MShellParseQuote, error) {
	quote := &MShellParseQuote{}
	err := parser.Match(parser.curr, LEFT_PAREN)
	if err != nil {
		return quote, err
	}
	for parser.curr.Type != RIGHT_PAREN {
		item, err := parser.ParseItem()
		if err != nil {
			return quote, err
		}
		quote.Items = append(quote.Items, item)
	}
	err = parser.Match(parser.curr, RIGHT_PAREN)
	if err != nil {
		return quote, err
	}
	return quote, nil
}
