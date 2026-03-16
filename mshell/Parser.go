package main

// Main parsing entry point ParseFile

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

// MShellIndexerList {{{
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

// }}}

// MShellVarstoreList {{{
type MShellVarstoreList struct {
	VarStores []Token
}

func (varstoreList MShellVarstoreList) DebugString() string {
	builder := strings.Builder{}
	builder.WriteString(varstoreList.VarStores[0].DebugString())
	for i := 1; i < len(varstoreList.VarStores); i++ {
		builder.WriteString(", ")
		builder.WriteString(varstoreList.VarStores[i].DebugString())
	}
	return builder.String()
}

func (varstoreList MShellVarstoreList) ToJson() string {
	// Basically return comma separated list of the tokens
	if len(varstoreList.VarStores) == 1 {
		return fmt.Sprintf("[\"%s\"]", varstoreList.VarStores[0].Lexeme)
	}

	builder := strings.Builder{}
	builder.WriteString("[\"")
	builder.WriteString(varstoreList.VarStores[0].Lexeme)
	for i := 1; i < len(varstoreList.VarStores); i++ {
		builder.WriteString("\", \"")
		builder.WriteString(varstoreList.VarStores[i].Lexeme)
	}
	builder.WriteString("\"]")
	return builder.String()
}

func (varstoreList MShellVarstoreList) GetStartToken() Token {
	// We should never have an empty list
	return varstoreList.VarStores[0]
}

func (varstoreList MShellVarstoreList) GetEndToken() Token {
	// We should never have an empty list
	return varstoreList.VarStores[len(varstoreList.VarStores)-1]
}
// }}}

// MShellGetter {{{
type MShellGetter struct {
	Token Token
	String string
}

func (getter *MShellGetter) ToJson() string {
	return fmt.Sprintf("{\"getter\": %s}", getter.Token.ToJson())
}

func (getter *MShellGetter) DebugString() string {
	return fmt.Sprintf(":%s", getter.Token.Lexeme)
}

func (getter *MShellGetter) GetStartToken() Token {
	return getter.Token
}

func (getter *MShellGetter) GetEndToken() Token {
	return getter.Token
}
// }}}


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

// MShellParsePrefixQuote represents a prefix quote: .functionName ... end
type MShellParsePrefixQuote struct {
	Items      []MShellParseItem // Items between function and 'end'
	StartToken Token             // The PREFIXQUOTE token (.filter)
	EndToken   Token             // The 'end' token
}

func (pq *MShellParsePrefixQuote) GetStartToken() Token {
	return pq.StartToken
}

func (pq *MShellParsePrefixQuote) GetEndToken() Token {
	return pq.EndToken
}

func (pq *MShellParsePrefixQuote) ToJson() string {
	lexeme := pq.StartToken.Lexeme
	funcName := lexeme[:len(lexeme)-1] // Strip trailing '.'
	return fmt.Sprintf("{\"prefix_quote\": {\"function\": \"%s\", \"items\": %s}}", funcName, ToJson(pq.Items))
}

func (pq *MShellParsePrefixQuote) DebugString() string {
	builder := strings.Builder{}
	builder.WriteString(pq.StartToken.Lexeme)
	builder.WriteString(" ")
	for _, item := range pq.Items {
		builder.WriteString(item.DebugString())
		builder.WriteString(" ")
	}
	builder.WriteString("end")
	return builder.String()
}

// MShellParseElseIf represents an else-if branch with its condition and body
type MShellParseElseIf struct {
	Condition []MShellParseItem
	Body      []MShellParseItem
}

// MShellParseIfBlock represents an if/else-if/else/end block
type MShellParseIfBlock struct {
	IfBody     []MShellParseItem
	ElseIfs    []MShellParseElseIf
	ElseBody   []MShellParseItem
	StartToken Token
	EndToken   Token
}

func (ifBlock *MShellParseIfBlock) GetStartToken() Token {
	return ifBlock.StartToken
}

func (ifBlock *MShellParseIfBlock) GetEndToken() Token {
	return ifBlock.EndToken
}

func (ifBlock *MShellParseIfBlock) ToJson() string {
	builder := strings.Builder{}
	builder.WriteString("{\"if_body\": ")
	builder.WriteString(ToJson(ifBlock.IfBody))
	builder.WriteString(", \"else_ifs\": [")
	for i, elseIf := range ifBlock.ElseIfs {
		if i > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString("{\"condition\": ")
		builder.WriteString(ToJson(elseIf.Condition))
		builder.WriteString(", \"body\": ")
		builder.WriteString(ToJson(elseIf.Body))
		builder.WriteString("}")
	}
	builder.WriteString("], \"else_body\": ")
	builder.WriteString(ToJson(ifBlock.ElseBody))
	builder.WriteString("}")
	return builder.String()
}

func (ifBlock *MShellParseIfBlock) DebugString() string {
	builder := strings.Builder{}
	builder.WriteString("if ")
	for _, item := range ifBlock.IfBody {
		builder.WriteString(item.DebugString())
		builder.WriteString(" ")
	}
	for _, elseIf := range ifBlock.ElseIfs {
		builder.WriteString("else* ")
		for _, item := range elseIf.Condition {
			builder.WriteString(item.DebugString())
			builder.WriteString(" ")
		}
		builder.WriteString("*if ")
		for _, item := range elseIf.Body {
			builder.WriteString(item.DebugString())
			builder.WriteString(" ")
		}
	}
	if len(ifBlock.ElseBody) > 0 {
		builder.WriteString("else ")
		for _, item := range ifBlock.ElseBody {
			builder.WriteString(item.DebugString())
			builder.WriteString(" ")
		}
	}
	builder.WriteString("end")
	return builder.String()
}

type MShellDefinition struct {
	Name      string
	NameToken Token
	Items     []MShellParseItem
	TypeDef   TypeDefinition
	Metadata  MShellParseDict
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
	initialized bool
}

type parserPanic struct {
	err error
}

func NewMShellParser(lexer *Lexer) *MShellParser {
	return &MShellParser{lexer: lexer}
}

func (parser *MShellParser) ResetInput(input string) {
	parser.lexer.resetInput(input)
	parser.initialized = false
}

func (parser *MShellParser) recoverPanic(err *error) {
	recovered := recover()
	if recovered == nil {
		return
	}

	asParserPanic, ok := recovered.(parserPanic)
	if ok {
		*err = asParserPanic.err
		return
	}

	panic(recovered)
}

func (parser *MShellParser) scanToken() Token {
	token := parser.lexer.scanToken()
	if token.Type == ERROR {
		panic(parserPanic{err: errors.New(token.Lexeme)})
	}
	return token
}

func (parser *MShellParser) scanTokenAll() Token {
	token := parser.lexer.scanTokenAll()
	if token.Type == ERROR {
		panic(parserPanic{err: errors.New(token.Lexeme)})
	}
	return token
}

func (parser *MShellParser) ensureInitialized() {
	if !parser.initialized {
		parser.NextToken()
		parser.initialized = true
	}
}

func (parser *MShellParser) NextToken() {
	parser.curr = parser.scanToken()
	parser.initialized = true
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

func (parser *MShellParser) ParseFile() (file *MShellFile, err error) {
	file = &MShellFile{}
	defer parser.recoverPanic(&err)
	parser.ensureInitialized()

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
		case IF:
			ifBlock, err := parser.ParseIfBlock()
			if err != nil {
				return file, err
			}
			file.Items = append(file.Items, ifBlock)
		case PREFIXQUOTE:
			pq, err := parser.ParsePrefixQuote()
			if err != nil {
				return file, err
			}
			file.Items = append(file.Items, pq)
	case DEF:
		_ = parser.Match(parser.curr, DEF)
		if parser.curr.Type != LITERAL {
			return file, fmt.Errorf("%d: %d: Expected a name for the definition, got %s", parser.curr.Line, parser.curr.Column, parser.curr.Type)
		}

		nameToken := parser.curr
		def := MShellDefinition{Name: parser.curr.Lexeme, NameToken: nameToken, Items: []MShellParseItem{}, TypeDef: TypeDefinition{}}
		_ = parser.Match(parser.curr, LITERAL)

		if parser.curr.Type == LEFT_CURLY {
			metadata, err := parser.ParseStaticDict()
			if err != nil {
				return file, err
			}
			def.Metadata = *metadata
		}

		typeDef, err := parser.ParseTypeDefinition()
		if err != nil {
			return file, err
		}
		def.TypeDef = *typeDef

			for {
				if parser.curr.Type == END {
					break
				} else if parser.curr.Type == EOF {
					return file, fmt.Errorf("Unexpected EOF while parsing definition %s", def.Name)
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

func (parser *MShellParser) ParseVarstoreList() (MShellVarstoreList) {
	varStoreList := MShellVarstoreList{}
	varStoreList.VarStores = []Token{}
	varStoreList.VarStores = append(varStoreList.VarStores, parser.curr)
	parser.NextToken()

	for {
		if parser.curr.Type == COMMA {
			parser.NextToken()
			if parser.curr.Type == VARSTORE {
				varStoreList.VarStores = append(varStoreList.VarStores, parser.curr)
				parser.NextToken()
			} else {
				// No error here, just a trailing comma which is fine.
				break
			}
		} else {
			break
		}
	}

	return varStoreList
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

// TypeDictionary {{{
type TypeDictionary struct {
	TypeMap      map[string]MShellType
	WildCardType MShellType
}

func (dict *TypeDictionary) Bind(otherType MShellType) ([]BoundType, error) {
	if dict == nil {
		return []BoundType{}, errors.New("Cannot bind a nil dictionary type")
	}

	otherDict, ok := otherType.(*TypeDictionary)
	if !ok || otherDict == nil {
		return []BoundType{}, errors.New("Cannot bind a dictionary to a non-dictionary type")
	}

	bound := make([]BoundType, 0)
	for key, expectedType := range dict.TypeMap {
		actualType, exists := otherDict.TypeMap[key]
		if !exists {
			if otherDict.WildCardType != nil {
				actualType = otherDict.WildCardType
			} else {
				return bound, fmt.Errorf("Missing key '%s' when binding dictionary type", key)
			}
		}
		b, err := expectedType.Bind(actualType)
		if err != nil {
			return bound, err
		}
		bound = append(bound, b...)
	}

	if dict.WildCardType != nil {
		if otherDict.WildCardType != nil {
			b, err := dict.WildCardType.Bind(otherDict.WildCardType)
			if err != nil {
				return bound, err
			}
			bound = append(bound, b...)
		}
		for key, actualType := range otherDict.TypeMap {
			if _, exists := dict.TypeMap[key]; exists {
				continue
			}
			b, err := dict.WildCardType.Bind(actualType)
			if err != nil {
				return bound, err
			}
			bound = append(bound, b...)
		}
	} else {
		if otherDict.WildCardType != nil {
			return bound, errors.New("Cannot bind dictionary without wildcard to dictionary with wildcard type")
		}
		for key := range otherDict.TypeMap {
			if _, exists := dict.TypeMap[key]; !exists {
				return bound, fmt.Errorf("Unexpected key '%s' when binding dictionary type", key)
			}
		}
	}

	return bound, nil
}

func (dict *TypeDictionary) Replace(boundTypes []BoundType) MShellType {
	if dict == nil {
		return nil
	}

	newMap := make(map[string]MShellType, len(dict.TypeMap))
	for key, value := range dict.TypeMap {
		newMap[key] = value.Replace(boundTypes)
	}

	var newWildCard MShellType
	if dict.WildCardType != nil {
		newWildCard = dict.WildCardType.Replace(boundTypes)
	}

	return &TypeDictionary{
		TypeMap:      newMap,
		WildCardType: newWildCard,
	}
}

func (dict *TypeDictionary) ToMshell() string {
	if dict == nil {
		return "{}"
	}

	keys := make([]string, 0, len(dict.TypeMap))
	for key := range dict.TypeMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys)+1)
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s: %s", key, dict.TypeMap[key].ToMshell()))
	}
	if dict.WildCardType != nil {
		parts = append(parts, fmt.Sprintf("*: %s", dict.WildCardType.ToMshell()))
	}

	return fmt.Sprintf("{%s}", strings.Join(parts, ", "))
}

func (dict *TypeDictionary) ToJson() string {
	if dict == nil {
		return "{\"dict\": {}}"
	}

	keys := make([]string, 0, len(dict.TypeMap))
	for key := range dict.TypeMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	entries := make([]string, 0, len(keys)+1)
	for _, key := range keys {
		entries = append(entries, fmt.Sprintf("\"%s\": %s", key, dict.TypeMap[key].ToJson()))
	}
	if dict.WildCardType != nil {
		entries = append(entries, fmt.Sprintf("\"*\": %s", dict.WildCardType.ToJson()))
	}

	return fmt.Sprintf("{\"dict\": {%s}}", strings.Join(entries, ", "))
}

func (dict *TypeDictionary) Equals(other MShellType) bool {
	if dict == nil {
		return other == nil
	}

	otherDict, ok := other.(*TypeDictionary)
	if !ok || otherDict == nil {
		return false
	}

	if (dict.WildCardType == nil) != (otherDict.WildCardType == nil) {
		return false
	}
	if dict.WildCardType != nil && !dict.WildCardType.Equals(otherDict.WildCardType) {
		return false
	}

	if len(dict.TypeMap) != len(otherDict.TypeMap) {
		return false
	}

	for key, expectedType := range dict.TypeMap {
		actualType, exists := otherDict.TypeMap[key]
		if !exists || !expectedType.Equals(actualType) {
			return false
		}
	}
	return true
}

func (dict *TypeDictionary) String() string {
	if dict == nil {
		return "{}"
	}
	return dict.ToMshell()
}

// }}}

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

type TypeBinary struct{}

func (t TypeBinary) Bind(otherType MShellType) ([]BoundType, error) {
	return make([]BoundType, 0), nil
}

func (t TypeBinary) Replace(boundTypes []BoundType) MShellType {
	return t
}

func (t TypeBinary) ToMshell() string {
	return "binary"
}

func (t TypeBinary) ToJson() string {
	return "\"binary\""
}

func (t TypeBinary) Equals(other MShellType) bool {
	_, ok := other.(TypeBinary)
	return ok
}

func (t TypeBinary) String() string {
	return "binary"
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
}

type TypeHomogeneousList struct {
	ListType       MShellType
	Count          int // This is < 0 if the Count is not known
	StdoutBehavior StdoutBehavior
}

type TypeHeterogenousList struct {
	ListTypes      []MShellType
	StdoutBehavior StdoutBehavior
}

func (list *TypeHomogeneousList) Bind(otherType MShellType) ([]BoundType, error) {
	asListType, ok := otherType.(*TypeHomogeneousList)
	if !ok {
		return []BoundType{}, errors.New("Cannot bind a list to a non-list type")
	}
	// Recursively select all the bound types in the list.
	return list.ListType.Bind(asListType.ListType)
}

func (list *TypeHomogeneousList) Replace(boundTypes []BoundType) MShellType {
	// Replace the list type with the bound type
	newListType := list.ListType.Replace(boundTypes)
	return &TypeHomogeneousList{ListType: newListType, Count: list.Count, StdoutBehavior: list.StdoutBehavior}
}

func (list *TypeHomogeneousList) ToMshell() string {
	return fmt.Sprintf("[%s]", list.ListType.ToMshell())
}

func (list *TypeHomogeneousList) ToJson() string {
	return fmt.Sprintf("{\"list\": %s}", list.ListType.ToJson())
}

func (list *TypeHomogeneousList) Equals(other MShellType) bool {
	if otherList, ok := other.(*TypeHomogeneousList); ok {
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

func (list *TypeHomogeneousList) String() string {
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
	if otherList, ok := other.(*TypeHomogeneousList); ok {
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
		case LEFT_CURLY:
			typeDict, err := parser.ParseTypeDictionary()
			if err != nil {
				return nil, err
			}
			types = append(types, typeDict)
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

func (parser *MShellParser) ParseTypeList() (*TypeHomogeneousList, error) {
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
	case LEFT_CURLY:
		// Parse dictionary
		typeDict, err := parser.ParseTypeDictionary()
		if err != nil {
			return nil, err
		}
		listType = typeDict
	case LITERAL:
		// Parse generic
		genericType := TypeGeneric{Name: parser.curr.Lexeme}
		listType = genericType
		parser.NextToken()
	default:
		return nil, fmt.Errorf("Unexpected token %s while parsing type list", parser.curr.Type)
	}

	err = parser.Match(parser.curr, RIGHT_SQUARE_BRACKET)
	if err != nil {
		return nil, err
	}

	typeList := TypeHomogeneousList{ListType: listType}
	return &typeList, nil
}

func (parser *MShellParser) ParseTypeDictionary() (*TypeDictionary, error) {
	startToken := parser.curr
	err := parser.Match(parser.curr, LEFT_CURLY)
	if err != nil {
		return nil, err
	}

	dictType := &TypeDictionary{
		TypeMap:      make(map[string]MShellType),
		WildCardType: nil,
	}

	if parser.curr.Type == RIGHT_CURLY {
		parser.NextToken()
		return dictType, nil
	}

	isKeyValue := parser.curr.Type == STRING || parser.curr.Type == SINGLEQUOTESTRING || parser.curr.Type == ASTERISK

	if isKeyValue {
		for {
			if parser.curr.Type == ASTERISK {
				if dictType.WildCardType != nil {
					return nil, fmt.Errorf("Duplicate wildcard dictionary type at line %d, column %d.", parser.curr.Line, parser.curr.Column)
				}
				parser.NextToken()
				err = parser.MatchWithMessage(parser.curr, COLON, "Expected ':' after '*' in dictionary type definition.")
				if err != nil {
					return nil, err
				}
				valueType, err := parser.parseSingleType("dictionary wildcard value")
				if err != nil {
					return nil, err
				}
				dictType.WildCardType = valueType
			} else if parser.curr.Type == STRING || parser.curr.Type == SINGLEQUOTESTRING {
				key, keyToken, err := parser.parseTypeDictionaryKey()
				if err != nil {
					return nil, err
				}

				err = parser.MatchWithMessage(parser.curr, COLON, fmt.Sprintf("Expected ':' after dictionary key %s.", key))
				if err != nil {
					return nil, err
				}

				valueType, err := parser.parseSingleType(fmt.Sprintf("dictionary value for key %s", key))
				if err != nil {
					return nil, err
				}

				if _, exists := dictType.TypeMap[key]; exists {
					return nil, fmt.Errorf("Duplicate dictionary key '%s' in type definition at line %d, column %d.", key, keyToken.Line, keyToken.Column)
				}

				dictType.TypeMap[key] = valueType
			} else {
				return nil, fmt.Errorf("Expected dictionary key or '*' in type definition at line %d, column %d.", parser.curr.Line, parser.curr.Column)
			}

			if parser.curr.Type == COMMA {
				parser.NextToken()
				if parser.curr.Type == RIGHT_CURLY {
					break
				}
				continue
			}

			break
		}
	} else {
		valueType, err := parser.parseSingleType("dictionary value")
		if err != nil {
			return nil, err
		}
		dictType.WildCardType = valueType
	}

	err = parser.Match(parser.curr, RIGHT_CURLY)
	if err != nil {
		return nil, err
	}

	// If no key/value pairs or wildcard were provided, and the dictionary was empty ({}),
	// we already returned above. At this point, we either have specific keys, a wildcard, or both.
	// Ensure that an entirely empty dictionary type (with no wildcard and no keys) is not allowed
	// unless it was explicitly {}.
	if len(dictType.TypeMap) == 0 && dictType.WildCardType == nil {
		return nil, fmt.Errorf("Dictionary type starting at line %d, column %d must specify a value type.", startToken.Line, startToken.Column)
	}

	return dictType, nil
}

func (parser *MShellParser) parseTypeDictionaryKey() (string, Token, error) {
	token := parser.curr
	switch token.Type {
	case STRING:
		key, err := ParseRawString(token.Lexeme)
		if err != nil {
			return "", token, fmt.Errorf("Error parsing dictionary key at line %d, column %d: %s.", token.Line, token.Column, err.Error())
		}
		parser.NextToken()
		return key, token, nil
	case SINGLEQUOTESTRING:
		lexeme := token.Lexeme
		if len(lexeme) < 2 {
			return "", token, fmt.Errorf("Invalid single quoted dictionary key at line %d, column %d.", token.Line, token.Column)
		}
		key := lexeme[1 : len(lexeme)-1]
		parser.NextToken()
		return key, token, nil
	default:
		return "", token, fmt.Errorf("Expected string key in dictionary type at line %d, column %d, found %s.", token.Line, token.Column, token.Type)
	}
}

func (parser *MShellParser) parseSingleType(context string) (MShellType, error) {
	startToken := parser.curr
	types, err := parser.ParseTypeItems()
	if err != nil {
		return nil, err
	}

	if len(types) == 0 {
		return nil, fmt.Errorf("Expected %s but found no type at line %d, column %d.", context, startToken.Line, startToken.Column)
	}

	if len(types) > 1 {
		return nil, fmt.Errorf("Expected single %s but parsed %d types starting at line %d, column %d.", context, len(types), startToken.Line, startToken.Column)
	}

	return types[0], nil
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
			return dict, fmt.Errorf("Did not find closing '}' for dict beginning at line %d, column %d.", dict.StartToken.Line, dict.StartToken.Column)
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
			return dict, fmt.Errorf("Duplicate key '%s' found in dict at line %d, column %d.", keys[i], dict.StartToken.Line, dict.StartToken.Column)
		}
	}

	return dict, nil
}

func (parser *MShellParser) ParseStaticDict() (*MShellParseDict, error) {
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
			return dict, fmt.Errorf("Did not find closing '}' for metadata dict beginning at line %d, column %d.", dict.StartToken.Line, dict.StartToken.Column)
		} else {
			keyValue, err := parser.parseStaticDictKeyValue()
			if err != nil {
				return dict, err
			}
			dict.Items = append(dict.Items, keyValue)
		}
	}

	dict.EndToken = parser.curr
	err = parser.Match(parser.curr, RIGHT_CURLY)
	if err != nil {
		return dict, err
	}

	keys := make([]string, 0, len(dict.Items))
	for _, item := range dict.Items {
		keys = append(keys, item.Key)
	}
	sort.Strings(keys)
	for i := 1; i < len(keys); i++ {
		if keys[i] == keys[i-1] {
			return dict, fmt.Errorf("Duplicate key '%s' found in metadata dict at line %d, column %d.", keys[i], dict.StartToken.Line, dict.StartToken.Column)
		}
	}

	return dict, nil
}

func (parser *MShellParser) parseDictKeyValue() (MShellParseDictKeyValue, error) {
	// Else expect a literal or string key.
	keyToken := parser.curr.Type
	if keyToken != LITERAL && keyToken != STRING && keyToken != SINGLEQUOTESTRING && keyToken != INTEGER && keyToken != STARTINDEXER {
		return MShellParseDictKeyValue{}, fmt.Errorf("Expected a key for dict, got %s at line %d, column %d.", keyToken, parser.curr.Line, parser.curr.Column)
	}

	// key := parser.curr
	var key string
	var err error
	if keyToken == STRING {
		key, err = ParseRawString(parser.curr.Lexeme)
		if err != nil {
			return MShellParseDictKeyValue{}, fmt.Errorf("Error parsing string key: %s at line %d, column %d.", err.Error(), parser.curr.Line, parser.curr.Column)
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
		return MShellParseDictKeyValue{}, fmt.Errorf("Expected a key for dict, got %s at line %d, column %d.", keyToken, parser.curr.Line, parser.curr.Column)
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
			return MShellParseDictKeyValue{}, fmt.Errorf("Unexpected EOF while parsing dict at line %d, column %d.", parser.curr.Line, parser.curr.Column)
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

func (parser *MShellParser) parseStaticDictKeyValue() (MShellParseDictKeyValue, error) {
	keyToken := parser.curr.Type
	if keyToken != LITERAL && keyToken != STRING && keyToken != SINGLEQUOTESTRING && keyToken != INTEGER && keyToken != STARTINDEXER {
		return MShellParseDictKeyValue{}, fmt.Errorf("Expected a key for metadata dict, got %s at line %d, column %d.", keyToken, parser.curr.Line, parser.curr.Column)
	}

	var key string
	var err error
	if keyToken == STRING {
		key, err = ParseRawString(parser.curr.Lexeme)
		if err != nil {
			return MShellParseDictKeyValue{}, fmt.Errorf("Error parsing metadata dict string key: %s at line %d, column %d.", err.Error(), parser.curr.Line, parser.curr.Column)
		}
	} else if keyToken == SINGLEQUOTESTRING {
		key = parser.curr.Lexeme[1 : len(parser.curr.Lexeme)-1]
	} else if keyToken == LITERAL {
		key = parser.curr.Lexeme
	} else if keyToken == INTEGER {
		intVal, _ := strconv.Atoi(parser.curr.Lexeme)
		key = strconv.Itoa(intVal)
	} else if keyToken == STARTINDEXER {
		indexStr := parser.curr.Lexeme[:len(parser.curr.Lexeme)-1]
		intVal, _ := strconv.Atoi(indexStr)
		key = strconv.Itoa(intVal)
	} else {
		return MShellParseDictKeyValue{}, fmt.Errorf("Expected a key for metadata dict, got %s at line %d, column %d.", keyToken, parser.curr.Line, parser.curr.Column)
	}

	parser.NextToken()
	if keyToken != STARTINDEXER {
		err = parser.MatchWithMessage(parser.curr, COLON, fmt.Sprintf("Expected ':' after key %s at line %d, column %d.", key, parser.curr.Line, parser.curr.Column))
		if err != nil {
			return MShellParseDictKeyValue{}, err
		}
	}

	var valueItems []MShellParseItem
	for {
		if parser.curr.Type == COMMA {
			parser.NextToken()
			break
		} else if parser.curr.Type == RIGHT_CURLY {
			break
		} else if parser.curr.Type == EOF {
			return MShellParseDictKeyValue{}, fmt.Errorf("Unexpected EOF while parsing metadata dict at line %d, column %d.", parser.curr.Line, parser.curr.Column)
		} else {
			item, err := parser.ParseStaticItem()
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
			return list, fmt.Errorf("Did not find closing ']' for list beginning at line %d, column %d.", list.StartToken.Line, list.StartToken.Column)
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

func (parser *MShellParser) ParseStaticList() (*MShellParseList, error) {
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
			return list, fmt.Errorf("Did not find closing ']' for metadata list beginning at line %d, column %d.", list.StartToken.Line, list.StartToken.Column)
		} else {
			item, err := parser.ParseStaticItem()
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
	parser.ensureInitialized()

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
	case VARSTORE:
		return parser.ParseVarstoreList(), nil
	case EOF:
		return nil, errors.New("Unexpected EOF while parsing item")
	case COLON:
		return parser.ParseGetter()
	case IF:
		return parser.ParseIfBlock()
	case PREFIXQUOTE:
		return parser.ParsePrefixQuote()
	default:
		return parser.ParseSimple(), nil
	}
}

func (parser *MShellParser) ParseStaticItem() (MShellParseItem, error) {
	switch parser.curr.Type {
	case LEFT_SQUARE_BRACKET:
		return parser.ParseStaticList()
	case LEFT_CURLY:
		return parser.ParseStaticDict()
	case STRING, SINGLEQUOTESTRING, INTEGER, FLOAT, TRUE, FALSE:
		return parser.ParseSimple(), nil
	case FORMATSTRING:
		return nil, fmt.Errorf("Interpolated strings are not allowed in metadata at line %d, column %d.", parser.curr.Line, parser.curr.Column)
	default:
		return nil, fmt.Errorf("Expected static metadata value at line %d, column %d, got %s.", parser.curr.Line, parser.curr.Column, parser.curr.Type)
	}
}

func (parser *MShellParser) ParseGetter() (MShellParseItem, error) {
	parser.NextToken()
	t := parser.curr

	switch t.Type {
	case STRING:
		parsedStr, err := ParseRawString(t.Lexeme)
		if err != nil {
			return nil, fmt.Errorf("Error parsing getter string: %s at line %d, column %d.", err.Error(), t.Line, t.Column)
		}
		parser.NextToken()
		return &MShellGetter{Token: t, String: parsedStr}, nil
	case SINGLEQUOTESTRING:
		if len(t.Lexeme) < 2 {
			return nil, fmt.Errorf("Error parsing getter string: empty single-quoted string at line %d, column %d.", t.Line, t.Column)
		}
		parsedStr := t.Lexeme[1 : len(t.Lexeme)-1]
		parser.NextToken()
		return &MShellGetter{Token: t, String: parsedStr}, nil
	case LITERAL:
		parser.NextToken()
		return &MShellGetter{Token: t, String: t.Lexeme}, nil
	default:
		return nil, fmt.Errorf("Expected a string or literal after ':' at line %d, column %d.", t.Line, t.Column)
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
			return quote, fmt.Errorf("Did not find closing ')' for quote beginning at line %d, column %d.", quote.StartToken.Line, quote.StartToken.Column)
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

func (parser *MShellParser) ParsePrefixQuote() (*MShellParsePrefixQuote, error) {
	pq := &MShellParsePrefixQuote{StartToken: parser.curr}
	parser.NextToken() // consume the PREFIXQUOTE token

	// Parse items until we see END
	for {
		if parser.curr.Type == END {
			break
		} else if parser.curr.Type == EOF {
			return pq, fmt.Errorf("Did not find 'end' for prefix quote %s beginning at line %d, column %d.", pq.StartToken.Lexeme, pq.StartToken.Line, pq.StartToken.Column)
		} else {
			item, err := parser.ParseItem()
			if err != nil {
				return pq, err
			}
			pq.Items = append(pq.Items, item)
		}
	}

	pq.EndToken = parser.curr
	err := parser.Match(parser.curr, END)
	if err != nil {
		return pq, err
	}

	return pq, nil
}

func (parser *MShellParser) ParseIfBlock() (*MShellParseIfBlock, error) {
	ifBlock := &MShellParseIfBlock{StartToken: parser.curr}
	err := parser.Match(parser.curr, IF)
	if err != nil {
		return ifBlock, err
	}

	// Parse the if body until we see ELSESTAR, ELSE, or END
	for {
		switch parser.curr.Type {
		case ELSESTAR, ELSE, END:
			goto DoneIfBody
		case EOF:
			return ifBlock, fmt.Errorf("Did not find 'end' for if block beginning at line %d, column %d.", ifBlock.StartToken.Line, ifBlock.StartToken.Column)
		default:
			item, err := parser.ParseItem()
			if err != nil {
				return ifBlock, err
			}
			ifBlock.IfBody = append(ifBlock.IfBody, item)
		}
	}
DoneIfBody:

	// Parse else-if branches
	for parser.curr.Type == ELSESTAR {
		parser.NextToken() // consume else*

		elseIf := MShellParseElseIf{}

		// Parse condition items until *if
		for {
			if parser.curr.Type == STARIF {
				break
			} else if parser.curr.Type == EOF {
				return ifBlock, fmt.Errorf("Did not find '*if' for 'else*' in if block beginning at line %d, column %d.", ifBlock.StartToken.Line, ifBlock.StartToken.Column)
			} else {
				item, err := parser.ParseItem()
				if err != nil {
					return ifBlock, err
				}
				elseIf.Condition = append(elseIf.Condition, item)
			}
		}
		parser.NextToken() // consume *if

		// Parse else-if body until ELSESTAR, ELSE, or END
		for {
			switch parser.curr.Type {
			case ELSESTAR, ELSE, END:
				goto DoneElseIfBody
			case EOF:
				return ifBlock, fmt.Errorf("Did not find 'end' for if block beginning at line %d, column %d.", ifBlock.StartToken.Line, ifBlock.StartToken.Column)
			default:
				item, err := parser.ParseItem()
				if err != nil {
					return ifBlock, err
				}
				elseIf.Body = append(elseIf.Body, item)
			}
		}
	DoneElseIfBody:
		ifBlock.ElseIfs = append(ifBlock.ElseIfs, elseIf)
	}

	// Parse optional else body
	if parser.curr.Type == ELSE {
		parser.NextToken() // consume else

		for {
			if parser.curr.Type == END {
				break
			} else if parser.curr.Type == EOF {
				return ifBlock, fmt.Errorf("Did not find 'end' for if block beginning at line %d, column %d.", ifBlock.StartToken.Line, ifBlock.StartToken.Column)
			} else {
				item, err := parser.ParseItem()
				if err != nil {
					return ifBlock, err
				}
				ifBlock.ElseBody = append(ifBlock.ElseBody, item)
			}
		}
	}

	// Match the closing 'end'
	ifBlock.EndToken = parser.curr
	err = parser.Match(parser.curr, END)
	if err != nil {
		return ifBlock, err
	}

	return ifBlock, nil
}
