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
	GetStartToken() Token
	GetEndToken() Token
}

type MShellFile struct {
	Definitions []MShellDefinition
	Items       []MShellParseItem
}

type MShellParseList struct {
	Items []MShellParseItem
	StartToken Token
	EndToken Token
}

func (list *MShellParseList) GetStartToken() Token {
	return list.StartToken
}

func (list *MShellParseList) GetEndToken() Token {
	return list.EndToken
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
	StartToken Token
	EndToken Token
}

func (quote *MShellParseQuote) GetStartToken() Token {
	return quote.StartToken
}

func (quote *MShellParseQuote) GetEndToken() Token {
	return quote.EndToken
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

	for parser.curr.Type != EOF  {
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
	Equals(other MShellType) bool
	String() string
	ToMshell() string
}

type TypeDefinition struct {
	InputTypes []MShellType
	OutputTypes []MShellType
}

func (def *TypeDefinition) ToMshell() string {
	inputMshellCode := make([]string, len(def.InputTypes))
	for i, t := range def.InputTypes {
		inputMshellCode[i] = t.ToMshell()
	}
	outputMshellCode := make([]string, len(def.OutputTypes))
	for i, t := range def.OutputTypes {
		outputMshellCode[i] = t.ToMshell()
	}
	return fmt.Sprintf("%s -- %s", strings.Join(inputMshellCode, " "), strings.Join(outputMshellCode, " "))
}

func (def *TypeDefinition) ToJson() string {
	return fmt.Sprintf("{\"input\": %s, \"output\": %s}", TypeListToJson(def.InputTypes), TypeListToJson(def.OutputTypes))
}

type TypeGeneric struct {
	Name string
}

func (generic TypeGeneric) ToMshell() string {
	return generic.Name
}

func (generic TypeGeneric) ToJson() string {
	return fmt.Sprintf("{ \"generic\": \"%s\" }", generic.Name)
}

func (generic TypeGeneric) Equals(other MShellType) bool {
	if otherGeneric, ok := other.(TypeGeneric); ok {
		return generic.Name == otherGeneric.Name
	}
	return false
}

func (generic TypeGeneric) String() string {
	return generic.Name
}

type TypeInt struct { }

func (t TypeInt) ToMshell() string {
	return "int"
}

func (t TypeInt) ToJson() string {
	return "\"int\""
}

func (t TypeInt) Equals(other MShellType) bool {
	_, ok := other.(TypeInt)
	return ok
}

func (t TypeInt) String() string {
	return "int"
}

type TypeFloat struct { }

func (t TypeFloat) ToMshell() string {
	return "float"
}

func (t TypeFloat) ToJson() string {
	return "\"float\""
}

func (t TypeFloat) Equals(other MShellType) bool {
	_, ok := other.(TypeFloat)
	return ok
}

func (t TypeFloat) String() string {
	return "float"
}


type TypeString struct { }

func (t TypeString) ToMshell() string {
	return "str"
}

func (t TypeString) ToJson() string {
	return "\"string\""
}

func (t TypeString) Equals(other MShellType) bool {
	_, ok := other.(TypeString)
	return ok
}

func (t TypeString) String() string {
	return "string"
}

type TypeBool struct { }

func (t TypeBool) ToMshell() string {
	return "bool"
}

func (t TypeBool) ToJson() string {
	return "\"bool\""
}

func (t TypeBool) Equals(other MShellType) bool {
	_, ok := other.(TypeBool)
	return ok
}

func (t TypeBool) String() string {
	return "bool"
}

type TypeList struct {
	ListType MShellType
	Count int // This is < 0 if the Count is not known
}

func (list *TypeList) ToMshell() string {
	return fmt.Sprintf("[%s]", list.ListType.ToMshell())
}

func (list *TypeList) ToJson() string {
	return fmt.Sprintf("{\"list\": %s}", list.ListType.ToJson())
}

func (list *TypeList) Equals(other MShellType) bool {
	if otherList, ok := other.(*TypeList); ok {
		return list.ListType.Equals(otherList.ListType)
	}

	// If other is a TypeTuple, and all elements are of the same type as list.ListType,
	// then we can consider them equal
	if otherTuple, ok := other.(*TypeTuple); ok {
		for _, t := range otherTuple.Types {
			if !list.ListType.Equals(t) {
				return false
			}
		}
		return true
	}

	return false
}

func (list *TypeList) String() string {
	return fmt.Sprintf("[%s]", list.ListType.String())
}

type TypeTuple struct {
	Types []MShellType
}

func (tuple *TypeTuple) ToMshell() string {
	builder := strings.Builder{}
	builder.WriteString("[")
	builder.WriteString(tuple.Types[0].ToMshell())
	for i := 1; i < len(tuple.Types); i++ {
		builder.WriteString(" ")
		builder.WriteString(tuple.Types[i].ToMshell())
	}
	builder.WriteString("]")
	return builder.String()
}

func (tuple *TypeTuple) ToJson() string {
	return fmt.Sprintf("{\"tuple\": %s}", TypeListToJson(tuple.Types))
}

func (tuple *TypeTuple) Equals(other MShellType) bool {
	if otherTuple, ok := other.(*TypeTuple); ok {
		if len(tuple.Types) != len(otherTuple.Types) {
			return false
		}
		for i, t := range tuple.Types {
			if !t.Equals(otherTuple.Types[i]) {
				return false
			}
		}
		return true
	}

	// If the other is a TypeList, and all elements are of the same type as tuple.Types,
	// then we can consider them equal
	// If we are empty, then we can consider them equal
	if otherList, ok := other.(*TypeList); ok {
		for _, t := range tuple.Types {
			if !t.Equals(otherList.ListType) {
				return false
			}
		}
		return true
	}
	return false
}

func (tuple *TypeTuple) String() string {
	if len(tuple.Types) == 0 {
		return "[]"
	}
	builder := strings.Builder{}
	builder.WriteString("[")
	builder.WriteString(tuple.Types[0].String())
	for i := 1; i < len(tuple.Types); i++ {
		builder.WriteString(", ")
		builder.WriteString(tuple.Types[i].String())
	}
	builder.WriteString("]")
	return builder.String()
}

type TypeQuote struct {
	InputTypes []MShellType
	OutputTypes []MShellType
}

func (quote *TypeQuote) ToMshell() string {
	builder := strings.Builder{}
	builder.WriteString("(")

	inputTypes := []string{}
	for _, t := range quote.InputTypes {
		inputTypes = append(inputTypes, t.ToMshell())
	}

	outputTypes := []string{}
	for _, t := range quote.OutputTypes {
		outputTypes = append(outputTypes, t.ToMshell())
	}

	// Write input types space separated
	builder.WriteString(strings.Join(inputTypes, " "))
	builder.WriteString(" -- ")
	// Write output types space separated
	builder.WriteString(strings.Join(outputTypes, " "))
	builder.WriteString(")")
	return builder.String()
}

func (quote *TypeQuote) ToJson() string {
	return fmt.Sprintf("{\"input\": %s, \"output\": %s}", TypeListToJson(quote.InputTypes), TypeListToJson(quote.OutputTypes))
}

func (quote *TypeQuote) Equals(other MShellType) bool {
	if otherQuote, ok := other.(*TypeQuote); ok {
		if len(quote.InputTypes) != len(otherQuote.InputTypes) || len(quote.OutputTypes) != len(otherQuote.OutputTypes) {
			return false
		}
		for i, t := range quote.InputTypes {
			if !t.Equals(otherQuote.InputTypes[i]) {
				return false
			}
		}

		for i, t := range quote.OutputTypes {
			if !t.Equals(otherQuote.OutputTypes[i]) {
				return false
			}
		}
		return true
	}
	return false
}

func (quote *TypeQuote) String() string {
	builder := strings.Builder{}
	builder.WriteString("(")

	inputTypes := []string{}
	for _, t := range quote.InputTypes {
		inputTypes = append(inputTypes, t.String())
	}

	outputTypes := []string{}
	for _, t := range quote.OutputTypes {
		outputTypes = append(outputTypes, t.String())
	}

	// Write input types space separated
	builder.WriteString(strings.Join(inputTypes, " "))
	builder.WriteString(" -- ")
	// Write output types space separated
	builder.WriteString(strings.Join(outputTypes, " "))
	builder.WriteString(")")
	return builder.String()
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
	list.StartToken = parser.curr
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
	list.EndToken = parser.curr
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
	quote := &MShellParseQuote{ StartToken: parser.curr }
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
	quote.EndToken = parser.curr
	err = parser.Match(parser.curr, RIGHT_PAREN)

	if err != nil {
		return quote, err
	}
	return quote, nil
}
