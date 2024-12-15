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
	TypeDef TypeDefinition
}

func (def *MShellDefinition) ToJson() string {
	return fmt.Sprintf("{\"name\": \"%s\", \"items\": %s, \"type\": %s }", def.Name, ToJson(def.Items), def.TypeDef.ToJson())
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

func TypeListToJson(typeList []MShellType) string {
	if len(typeList) == 0 {
		return "[]"
	}

	builder := strings.Builder{}
	builder.WriteString("[")
	builder.WriteString(typeList[0].ToJson())
	for i := 1; i < len(typeList); i++ {
		builder.WriteString(", ")
		builder.WriteString(typeList[i].ToJson())
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
		message := fmt.Sprintf("%d:%d: Expected %s, got %s", token.Line, token.Column, tokenType, token.Type)
		return errors.New(message)
	}
	parser.NextToken()
	return nil
}

func (parser *MShellParser) MatchWithMessage(token Token, tokenType TokenType, message string) error {
	if token.Type != tokenType {
		message := fmt.Sprintf("%d:%d: %s", token.Line, token.Column, message)
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
				return file, errors.New(fmt.Sprintf("Expected a name for the definition, got %s", parser.curr.Type))
			}

			def := MShellDefinition{Name: parser.curr.Lexeme, Items: []MShellParseItem{}, TypeDef: TypeDefinition{}}
			_ = parser.Match(parser.curr, LITERAL)

			typeDef, err := parser.ParseTypeDefinition()
			if err != nil {
				return file, err
			}
			def.TypeDef = *typeDef

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

type MShellType interface {
	ToJson() string
}

type TypeDefinition struct {
	InputTypes []MShellType
	OutputTypes []MShellType
}

func (def *TypeDefinition) ToJson() string {
	return fmt.Sprintf("{\"input\": %s, \"output\": %s}", TypeListToJson(def.InputTypes), TypeListToJson(def.OutputTypes))
}

type TypeGeneric struct {
	Name string
}

func (generic TypeGeneric) ToJson() string {
	return fmt.Sprintf("{ \"generic\": \"%s\" }", generic.Name)
}

type TypeInt struct { }

func (t TypeInt) ToJson() string {
	return "\"int\""
}

type TypeFloat struct { }

func (t TypeFloat) ToJson() string {
	return "\"float\""
}

type TypeString struct { }

func (t TypeString) ToJson() string {
	return "\"string\""
}

type TypeBool struct { }

func (t TypeBool) ToJson() string {
	return "\"bool\""
}

type TypeList struct {
	ListType MShellType
}

func (list *TypeList) ToJson() string {
	return fmt.Sprintf("{\"list\": %s}", list.ListType.ToJson())
}

type TypeTuple struct {
	Types []MShellType
}

func (tuple *TypeTuple) ToJson() string {
	return fmt.Sprintf("{\"tuple\": %s}", TypeListToJson(tuple.Types))
}

type TypeQuote struct {
	InputTypes []MShellType
	OutputTypes []MShellType
}

func (quote *TypeQuote) ToJson() string {
	return fmt.Sprintf("{\"input\": %s, \"output\": %s}", TypeListToJson(quote.InputTypes), TypeListToJson(quote.OutputTypes))
}

func (parser *MShellParser) ParseTypeDefinition() (*TypeDefinition, error) {
	err := parser.MatchWithMessage(parser.curr, LEFT_PAREN, "Expected '(' to start type definition.")
	if err != nil {
		return nil, err
	}

	// Parse first type
	inputTypes, err := parser.ParseTypeItems()
	if err != nil {
		return nil, err
	}

	err = parser.Match(parser.curr, DOUBLEDASH)
	if err != nil {
		return nil, err
	}

	outputTypes, err := parser.ParseTypeItems()
	if err != nil {
		return nil, err
	}

	err = parser.Match(parser.curr, RIGHT_PAREN)

	typeDef := TypeDefinition{InputTypes: inputTypes, OutputTypes: outputTypes}
	return &typeDef, nil
}

func (parser *MShellParser) ParseTypeItems() ([]MShellType, error) {
	types := []MShellType{}

	forLoop:
	for {
		switch parser.curr.Type {
		case TYPEINT:
			types = append(types, TypeInt{})
			parser.NextToken()
		case TYPEFLOAT:
			types = append(types, TypeInt{})
			parser.NextToken()
		case STR:
			types = append(types, TypeString{})
			parser.NextToken()
		case TYPEBOOL:
			types = append(types, TypeBool{})
			parser.NextToken()
		case AMPERSAND:
			// Parse tuple/heterogeneous list
			typeTuple, err := parser.ParseTypeTuple()
			if err != nil {
				return nil, err
			}
			types = append(types, typeTuple)
		case LEFT_SQUARE_BRACKET:
			// Parse list
			typeList, err := parser.ParseTypeList()
			if err != nil {
				return nil, err
			}
			types = append(types, typeList)
		case LEFT_PAREN:
			// Parse quote
			typeQuote, err := parser.ParseTypeQuote()
			if err != nil {
				return nil, err
			}
			types = append(types, typeQuote)
		case LITERAL:
			// Parse generic
			genericType := TypeGeneric{Name: parser.curr.Lexeme}
			types = append(types, genericType)
			parser.NextToken()
		default:
			break forLoop
		}
	}

	return types, nil
}

func (parser *MShellParser) ParseTypeTuple() (*TypeTuple, error) {

	err := parser.Match(parser.curr, AMPERSAND)
	if err != nil {
		return nil, err
	}

	err = parser.Match(parser.curr, LEFT_SQUARE_BRACKET)
	if err != nil {
		return nil, err
	}

	types := []MShellType{}
	for parser.curr.Type != RIGHT_SQUARE_BRACKET {
		// Parse type
	}

	parser.Match(parser.curr, RIGHT_SQUARE_BRACKET)
	typeTuple := TypeTuple{Types: types}
	return &typeTuple, nil
}

func (parser *MShellParser) ParseTypeQuote() (*TypeQuote, error) {
	err := parser.Match(parser.curr, LEFT_PAREN)
	if err != nil {
		return nil, err
	}

	// Parse input types
	inputTypes, err := parser.ParseTypeItems()
	if err != nil {
		return nil, err
	}

	err = parser.Match(parser.curr, DOUBLEDASH)

	// Parse output types
	outputTypes, err := parser.ParseTypeItems()
	if err != nil {
		return nil, err
	}

	err = parser.Match(parser.curr, RIGHT_PAREN)

	typeQuote := TypeQuote{InputTypes: inputTypes, OutputTypes: outputTypes}
	return &typeQuote, nil
}

func (parser *MShellParser) ParseTypeList() (*TypeList, error) {
	err := parser.Match(parser.curr, LEFT_SQUARE_BRACKET)
	if err != nil {
		return nil, err
	}

	// Single type list
	var listType MShellType

	// Parse type
	switch parser.curr.Type {
	case TYPEINT:
		listType = TypeInt{}
		parser.NextToken()
	case TYPEFLOAT:
		listType = TypeInt{}
		parser.NextToken()
	case STR:
		listType = TypeString{}
		parser.NextToken()
	case TYPEBOOL:
		listType = TypeBool{}
		parser.NextToken()
	case AMPERSAND:
		// Parse tuple/heterogeneous list
		typeTuple, err := parser.ParseTypeTuple()
		if err != nil {
			return nil, err
		}
		listType = typeTuple
	case LEFT_SQUARE_BRACKET:
		// Parse list
		typeList, err := parser.ParseTypeList()
		if err != nil {
			return nil, err
		}
		listType = typeList
	case LEFT_PAREN:
		// Parse quote
		typeQuote, err := parser.ParseTypeQuote()
		if err != nil {
			return nil, err
		}
		listType = typeQuote
	case LITERAL:
		// Parse generic
		genericType := TypeGeneric{Name: parser.curr.Lexeme}
		listType = genericType
		parser.NextToken()
	default:
		return nil, errors.New(fmt.Sprintf("Unexpected token %s while parsing type list", parser.curr.Type))
	}

	err = parser.Match(parser.curr, RIGHT_SQUARE_BRACKET)
	if err != nil {
		return nil, err
	}

	typeList := TypeList{ListType: listType}
	return &typeList, nil
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
