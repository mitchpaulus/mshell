package main

import (
	"errors"
	"fmt"
	"strings"
	// "os"
	"sort"
	"strconv"
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
	Items      []MShellParseItem
	StartToken Token
	EndToken   Token
}

type MShellParseDictKeyValue struct {
	Key   string
	Value []MShellParseItem
}

func (kv *MShellParseDictKeyValue) ToJson() string {
	if len(kv.Value) == 0 {
		return fmt.Sprintf("\"%s\": []", kv.Key)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\"%s\": [", kv.Key))
	sb.WriteString(kv.Value[0].ToJson())

	for i := 1; i < len(kv.Value); i++ {
		sb.WriteString(", ")
		sb.WriteString(kv.Value[i].ToJson())
	}
	sb.WriteString("]")
	return sb.String()
}

func (kv *MShellParseDictKeyValue) DebugString() string {
	return kv.ToJson()
}

type MShellParseDict struct {
	Items      []MShellParseDictKeyValue
	StartToken Token
	EndToken   Token
}

func (d *MShellParseDict) DebugString() string {
	if len(d.Items) == 0 {
		return "{}"
	}

	builder := strings.Builder{}
	builder.WriteString("{")

	builder.WriteString(d.Items[0].DebugString())

	for i := 1; i < len(d.Items); i++ {
		builder.WriteString(fmt.Sprintf(", %s", d.Items[i].DebugString()))
	}
	builder.WriteString("}")
	return builder.String()
}

func (d *MShellParseDict) ToJson() string {
	if len(d.Items) == 0 {
		return "{}"
	}

	var sb strings.Builder
	sb.WriteString("{")
	sb.WriteString(d.Items[0].ToJson())

	for i := 1; i < len(d.Items); i++ {
		sb.WriteString(", ")
		sb.WriteString(d.Items[i].ToJson())
	}
	sb.WriteString("}")
	return sb.String()
}

func (d *MShellParseDict) GetStartToken() Token {
	return d.StartToken
}

func (d *MShellParseDict) GetEndToken() Token {
	return d.EndToken
}

// This is a comma separated list of indexers, which can be used to index into a list or dict.
type MShellIndexerList struct {
	Indexers []MShellParseItem
}

func (indexerList *MShellIndexerList) DebugString() string {
	if len(indexerList.Indexers) == 0 {
		return ""
	}

	builder := strings.Builder{}
	builder.WriteString(indexerList.Indexers[0].DebugString())
	for i := 1; i < len(indexerList.Indexers); i++ {
		builder.WriteString(", ")
		builder.WriteString(indexerList.Indexers[i].DebugString())
	}
	return builder.String()
}

func (indexerList *MShellIndexerList) ToJson() string {
	return ToJson(indexerList.Indexers)
}

func (indexerList *MShellIndexerList) GetStartToken() Token {
	return indexerList.Indexers[0].GetStartToken()
}

func (indexerList *MShellIndexerList) GetEndToken() Token {
	return indexerList.Indexers[len(indexerList.Indexers)-1].GetEndToken()
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
	Items      []MShellParseItem
	StartToken Token
	EndToken   Token
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
	Name      string
	NameToken Token
	Items     []MShellParseItem
	TypeDef   TypeDefinition
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

// Checks for the desired match, and then advances the parser.
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

	for parser.curr.Type != EOF {
		switch parser.curr.Type {
		case RIGHT_SQUARE_BRACKET, RIGHT_PAREN:
			message := fmt.Sprintf("Unexpected token %s while parsing file", parser.curr.Type)
			return file, errors.New(message)
		case RIGHT_CURLY:
			return file, fmt.Errorf("Unexpected '}' while parsing file at line %d, column %d", parser.curr.Line, parser.curr.Column)
		case LEFT_SQUARE_BRACKET:
			list, err := parser.ParseList()
			if err != nil {
				return file, err
			}
			// fmt.Fprintf(os.Stderr, "List: %s\n", list.ToJson())
			file.Items = append(file.Items, list)
		case LEFT_CURLY:
			dict, err := parser.ParseDict()
			if err != nil {
				return file, err
			}
			file.Items = append(file.Items, dict)
		case INDEXER, ENDINDEXER, STARTINDEXER, SLICEINDEXER:
			indexerList := parser.ParseIndexer()
			file.Items = append(file.Items, indexerList)
		case DEF:
			_ = parser.Match(parser.curr, DEF)
			if parser.curr.Type != LITERAL {
				return file, errors.New(fmt.Sprintf("%d: %d: Expected a name for the definition, got %s", parser.curr.Line, parser.curr.Column, parser.curr.Type))
			}

			nameToken := parser.curr
			def := MShellDefinition{Name: parser.curr.Lexeme, NameToken: nameToken, Items: []MShellParseItem{}, TypeDef: TypeDefinition{}}
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

func (parser *MShellParser) ParseIndexer() *MShellIndexerList {
	indexerList := &MShellIndexerList{}
	indexerList.Indexers = []MShellParseItem{}
	indexerList.Indexers = append(indexerList.Indexers, parser.curr)
	parser.NextToken()

	for {
		if parser.curr.Type == COMMA {
			parser.NextToken()
			if parser.curr.Type == ENDINDEXER || parser.curr.Type == STARTINDEXER || parser.curr.Type == INDEXER || parser.curr.Type == SLICEINDEXER {
				indexerList.Indexers = append(indexerList.Indexers, parser.curr)
				parser.NextToken()
			} else {
				// No error here, just a trailing comma which is fine.
				break
			}
		} else {
			break
		}
	}

	return indexerList
}

type MShellType interface {
	ToJson() string
	Equals(other MShellType) bool
	String() string
	ToMshell() string
	Bind(otherType MShellType) ([]BoundType, error)
	Replace(boundTypes []BoundType) MShellType
}

type TypeDefinition struct {
	InputTypes  []MShellType
	OutputTypes []MShellType
}

type BoundType struct {
	GenericName string
	Type        MShellType
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
	Name  string
	Count int // -1 if the generic is variadic.
}

func (generic TypeGeneric) Bind(otherType MShellType) ([]BoundType, error) {
	return []BoundType{{GenericName: generic.Name, Type: otherType}}, nil
}

func (generic TypeGeneric) Replace(boundTypes []BoundType) MShellType {
	for _, bound := range boundTypes {
		if bound.GenericName == generic.Name {
			return bound.Type
		}
	}
	return generic
}

func (generic TypeGeneric) ToMshell() string {
	return generic.Name
}

func (generic TypeGeneric) ToJson() string {
	return fmt.Sprintf("{ \"generic\": \"%s\" }", generic.Name)
}

func (generic TypeGeneric) Equals(other MShellType) bool {
	return true
	// if otherGeneric, ok := other.(TypeGeneric); ok {
	// return generic.Name == otherGeneric.Name
	// }
	// return false
}

func (generic TypeGeneric) String() string {
	return generic.Name
}

type TypeInt struct{}

func (t TypeInt) Bind(otherType MShellType) ([]BoundType, error) {
	return make([]BoundType, 0), nil
}

func (t TypeInt) Replace(boundTypes []BoundType) MShellType {
	return t
}

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

type TypeFloat struct{}

func (t TypeFloat) Bind(otherType MShellType) ([]BoundType, error) {
	return make([]BoundType, 0), nil
}

func (t TypeFloat) Replace(boundTypes []BoundType) MShellType {
	return t
}

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

type TypeString struct{}

func (t TypeString) Bind(otherType MShellType) ([]BoundType, error) {
	return make([]BoundType, 0), nil
}

func (t TypeString) Replace(boundTypes []BoundType) MShellType {
	return t
}

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

type TypeBool struct{}

func (t TypeBool) Bind(otherType MShellType) ([]BoundType, error) {
	return make([]BoundType, 0), nil
}

func (t TypeBool) Replace(boundTypes []BoundType) MShellType {
	return t
}

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
	ListType       MShellType
	Count          int // This is < 0 if the Count is not known
	StdoutBehavior StdoutBehavior
}

func (list *TypeList) Bind(otherType MShellType) ([]BoundType, error) {
	asListType, ok := otherType.(*TypeList)
	if !ok {
		return []BoundType{}, errors.New("Cannot bind a list to a non-list type")
	}
	// Recursively select all the bound types in the list.
	return list.ListType.Bind(asListType.ListType)
}

func (list *TypeList) Replace(boundTypes []BoundType) MShellType {
	// Replace the list type with the bound type
	newListType := list.ListType.Replace(boundTypes)
	return &TypeList{ListType: newListType, Count: list.Count, StdoutBehavior: list.StdoutBehavior}
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
	Types          []MShellType
	StdoutBehavior StdoutBehavior
}

func (tuple *TypeTuple) Bind(otherType MShellType) ([]BoundType, error) {
	asTuple, ok := otherType.(*TypeTuple)
	if !ok {
		return []BoundType{}, errors.New("Cannot bind a tuple to a non-tuple type")
	}

	if len(tuple.Types) != len(asTuple.Types) {
		return []BoundType{}, errors.New("Cannot bind tuples of different lengths")
	}

	boundTypes := make([]BoundType, 0)
	for i, t := range tuple.Types {
		bound, err := t.Bind(asTuple.Types[i])
		if err != nil {
			return boundTypes, err
		}
		boundTypes = append(boundTypes, bound...)
	}
	return boundTypes, nil
}

func (tuple *TypeTuple) Replace(boundTypes []BoundType) MShellType {
	newTypes := make([]MShellType, len(tuple.Types))
	for i, t := range tuple.Types {
		newTypes[i] = t.Replace(boundTypes)
	}
	return &TypeTuple{Types: newTypes, StdoutBehavior: tuple.StdoutBehavior}
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
	InputTypes  []MShellType
	OutputTypes []MShellType
}

func (quote *TypeQuote) Bind(otherType MShellType) ([]BoundType, error) {
	asQuote, ok := otherType.(*TypeQuote)
	if !ok {
		return []BoundType{}, errors.New("Cannot bind a quote to a non-quote type")
	}

	if len(quote.InputTypes) != len(asQuote.InputTypes) || len(quote.OutputTypes) != len(asQuote.OutputTypes) {
		return []BoundType{}, errors.New("Cannot bind quotes of different lengths")
	}

	boundTypes := make([]BoundType, 0)
	for i, t := range quote.InputTypes {
		bound, err := t.Bind(asQuote.InputTypes[i])
		if err != nil {
			return boundTypes, err
		}
		boundTypes = append(boundTypes, bound...)
	}

	for i, t := range quote.OutputTypes {
		bound, err := t.Bind(asQuote.OutputTypes[i])
		if err != nil {
			return boundTypes, err
		}
		boundTypes = append(boundTypes, bound...)
	}

	return boundTypes, nil
}

func (quote *TypeQuote) Replace(boundTypes []BoundType) MShellType {
	newInputTypes := make([]MShellType, len(quote.InputTypes))
	for i, t := range quote.InputTypes {
		newInputTypes[i] = t.Replace(boundTypes)
	}

	newOutputTypes := make([]MShellType, len(quote.OutputTypes))
	for i, t := range quote.OutputTypes {
		newOutputTypes[i] = t.Replace(boundTypes)
	}

	return &TypeQuote{InputTypes: newInputTypes, OutputTypes: newOutputTypes}
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
	typeTuple := TypeTuple{Types: types, StdoutBehavior: STDOUT_NONE}
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

func (parser *MShellParser) ParseDict() (*MShellParseDict, error) {
	dict := &MShellParseDict{}
	dict.StartToken = parser.curr
	err := parser.Match(parser.curr, LEFT_CURLY)
	if err != nil {
		return dict, err
	}

	dict.Items = []MShellParseDictKeyValue{}
	for {
		if parser.curr.Type == RIGHT_CURLY {
			break
		} else if parser.curr.Type == EOF {
			return dict, errors.New(fmt.Sprintf("Did not find closing '}' for dict beginning at line %d, column %d.", dict.StartToken.Line, dict.StartToken.Column))
		} else {
			keyValue, err := parser.parseDictKeyValue()
			if err != nil {
				return dict, err
			}

			// Add the key-value pair to the dict. TBD: Error on duplicate keys?
			dict.Items = append(dict.Items, keyValue)
		}
	}

	// Match the closing curly brace.
	dict.EndToken = parser.curr
	err = parser.Match(parser.curr, RIGHT_CURLY)
	if err != nil {
		return dict, err
	}

	// Check for dups, use sorted list
	keys := make([]string, 0, len(dict.Items))
	for _, item := range dict.Items {
		keys = append(keys, item.Key)
	}
	sort.Strings(keys)
	for i := 1; i < len(keys); i++ {
		if keys[i] == keys[i-1] {
			return dict, errors.New(fmt.Sprintf("Duplicate key '%s' found in dict at line %d, column %d.", keys[i], dict.StartToken.Line, dict.StartToken.Column))
		}
	}

	return dict, nil
}

func (parser *MShellParser) parseDictKeyValue() (MShellParseDictKeyValue, error) {
	// Else expect a literal or string key.
	keyToken := parser.curr.Type
	if keyToken != LITERAL && keyToken != STRING && keyToken != SINGLEQUOTESTRING && keyToken != INTEGER && keyToken != STARTINDEXER {
		return MShellParseDictKeyValue{}, errors.New(fmt.Sprintf("Expected a key for dict, got %s at line %d, column %d.", keyToken, parser.curr.Line, parser.curr.Column))
	}

	// key := parser.curr
	var key string
	var err error
	if keyToken == STRING {
		key, err = ParseRawString(parser.curr.Lexeme)
		if err != nil {
			return MShellParseDictKeyValue{}, errors.New(fmt.Sprintf("Error parsing string key: %s at line %d, column %d.", err.Error(), parser.curr.Line, parser.curr.Column))
		}
	} else if keyToken == SINGLEQUOTESTRING {
		key = parser.curr.Lexeme[1 : len(parser.curr.Lexeme)-1]
	} else if keyToken == LITERAL {
		key = parser.curr.Lexeme
	} else if keyToken == INTEGER {
		intVal, _ := strconv.Atoi(parser.curr.Lexeme) // This normalizes the integer to not have leading 0's etc.
		key = strconv.Itoa(intVal)
	} else if keyToken == STARTINDEXER {
		indexStr := parser.curr.Lexeme[:len(parser.curr.Lexeme)-1]
		intVal, _ := strconv.Atoi(indexStr) // This normalizes the integer to not have leading 0's etc.
		key = strconv.Itoa(intVal)
	} else {
		return MShellParseDictKeyValue{}, errors.New(fmt.Sprintf("Expected a key for dict, got %s at line %d, column %d.", keyToken, parser.curr.Line, parser.curr.Column))
	}

	parser.NextToken()
	// Handle colon or skip if STARTINDEXER
	if keyToken != STARTINDEXER {
		err = parser.MatchWithMessage(parser.curr, COLON, fmt.Sprintf("Expected ':' after key %s at line %d, column %d.", key, parser.curr.Line, parser.curr.Column))
		if err != nil {
			return MShellParseDictKeyValue{}, err
		}
	}

	// Parse the value for the key.
	var valueItems []MShellParseItem
	for {
		if parser.curr.Type == COMMA {
			parser.NextToken()
			break
		} else if parser.curr.Type == RIGHT_CURLY {
			break
		} else if parser.curr.Type == EOF {
			return MShellParseDictKeyValue{}, errors.New(fmt.Sprintf("Unexpected EOF while parsing dict at line %d, column %d.", parser.curr.Line, parser.curr.Column))
		} else {
			item, err := parser.ParseItem()
			if err != nil {
				return MShellParseDictKeyValue{}, err
			}

			valueItems = append(valueItems, item)
		}
	}

	return MShellParseDictKeyValue{Key: key, Value: valueItems}, nil
}

func (parser *MShellParser) ParseList() (*MShellParseList, error) {
	list := &MShellParseList{}
	list.StartToken = parser.curr
	err := parser.Match(parser.curr, LEFT_SQUARE_BRACKET)
	if err != nil {
		return list, err
	}
	for {
		if parser.curr.Type == RIGHT_SQUARE_BRACKET {
			break
		} else if parser.curr.Type == EOF {
			return list, errors.New(fmt.Sprintf("Did not find closing ']' for list beginning at line %d, column %d.", list.StartToken.Line, list.StartToken.Column))
		} else {
			item, err := parser.ParseItem()
			if err != nil {
				return list, err
			}
			list.Items = append(list.Items, item)
		}
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
	case LEFT_CURLY:
		dict, err := parser.ParseDict()
		if err != nil {
			return nil, err
		}
		return dict, nil
	case INDEXER, ENDINDEXER, STARTINDEXER, SLICEINDEXER:
		return parser.ParseIndexer(), nil
	case EOF:
		return nil, errors.New("Unexpected EOF while parsing item")
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
	quote := &MShellParseQuote{StartToken: parser.curr}
	err := parser.Match(parser.curr, LEFT_PAREN)
	if err != nil {
		return quote, err
	}

	for {
		if parser.curr.Type == RIGHT_PAREN {
			break
		} else if parser.curr.Type == RIGHT_CURLY {
			return quote, fmt.Errorf("Unexpected '}' while parsing file at line %d, column %d", parser.curr.Line, parser.curr.Column)
		} else if parser.curr.Type == LEFT_CURLY {
			dict, err := parser.ParseDict()
			if err != nil {
				return quote, err
			}
			quote.Items = append(quote.Items, dict)
		} else if parser.curr.Type == EOF {
			return quote, errors.New(fmt.Sprintf("Did not find closing ')' for quote beginning at line %d, column %d.", quote.StartToken.Line, quote.StartToken.Column))
		} else {
			item, err := parser.ParseItem()
			if err != nil {
				return quote, err
			}
			quote.Items = append(quote.Items, item)
		}
	}
	quote.EndToken = parser.curr
	err = parser.Match(parser.curr, RIGHT_PAREN)

	if err != nil {
		return quote, err
	}
	return quote, nil
}
