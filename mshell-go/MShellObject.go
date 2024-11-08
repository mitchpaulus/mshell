package main

import (
    "strconv"
    "strings"
    "fmt"
)

type Jsonable interface {
    ToJson() string
}

type MShellObject interface {
    TypeName() string
    IsCommandLineable() bool
    IsNumeric() bool
    FloatNumeric() float64
    CommandLine() string
    DebugString() string
    Index(index int) (MShellObject, error)
    SliceStart(start int) (MShellObject, error)
    SliceEnd(end int) (MShellObject, error)
    Slice(startInc int, endExc int) (MShellObject, error)
    ToJson() string
}

type MShellSimple struct {
    Token Token
}

type MShellLiteral struct {
    LiteralText string
}

type MShellBool struct {
    Value bool
}

type MShellQuotation struct {
    Tokens []Token
    StandardInputFile string
    StandardOutputFile string
    StandardErrorFile string
}

type MShellQuotation2 struct {
    Objects []MShellParseItem
    StandardInputFile string
    StandardOutputFile string
    StandardErrorFile string
}

type StdoutBehavior int

const (
    STDOUT_NONE = iota
    STDOUT_LINES
    STDOUT_STRIPPED
    STDOUT_COMPLETE
)

type MShellList struct {
    Items []MShellObject
    StandardInputFile string
    StandardOutputFile string
    StandardErrorFile string
    // This sets how stdout is handled, whether it's broken up into lines, stripped of trailing newline, or left as is
    StdoutBehavior StdoutBehavior
}

type MShellString struct {
    Content string
}

type MShellPipe struct {
    List MShellList
}

type MShellInt struct {
    Value int
}

// TypeNames
func (obj *MShellLiteral) TypeName() string {
    return "Literal"
}

func (obj *MShellBool) TypeName() string {
    return "Boolean"
}

func (obj *MShellQuotation) TypeName() string {
    return "Quotation"
}

func (obj *MShellQuotation2) TypeName() string {
    return "Quotation"
}

func (obj *MShellList) TypeName() string {
    return "List"
}

func (obj *MShellString) TypeName() string {
    return "String"
}
    
func (obj *MShellPipe) TypeName() string {
    return "Pipe"
}

func (obj *MShellInt) TypeName() string {
    return "Integer"
}

func (obj *MShellSimple) TypeName() string {
    return obj.Token.Type.String()
}

// IsCommandLineable

func (obj *MShellLiteral) IsCommandLineable() bool {
    return true
}

func (obj *MShellBool) IsCommandLineable() bool {
    return false
}

func (obj *MShellQuotation) IsCommandLineable() bool {
    return false
}

func (obj *MShellQuotation2) IsCommandLineable() bool { return false }

func (obj *MShellList) IsCommandLineable() bool {
    return false
}

func (obj *MShellString) IsCommandLineable() bool {
    return true
}

func (obj *MShellPipe) IsCommandLineable() bool {
    return false
}

func (obj *MShellInt) IsCommandLineable() bool {
    return true
}

func (obj *MShellSimple) IsCommandLineable() bool {
    return false
}

// IsNumeric
func (obj *MShellLiteral) IsNumeric() bool {
    return false
}

func (obj *MShellBool) IsNumeric() bool {
    return false
}

func (obj *MShellQuotation) IsNumeric() bool {
    return false
}

func (obj *MShellQuotation2) IsNumeric() bool { return false }

func (obj *MShellList) IsNumeric() bool {
    return false
}

func (obj *MShellString) IsNumeric() bool {
    return false
}

func (obj *MShellPipe) IsNumeric() bool {
    return false
}

func (obj *MShellInt) IsNumeric() bool {
    return true
}

func (obj *MShellSimple) IsNumeric() bool {
    return false
}

// FloatNumeric
func (obj *MShellLiteral) FloatNumeric() float64 {
    return 0 
}

func (obj *MShellBool) FloatNumeric() float64 {
    return 0
}

func (obj *MShellQuotation) FloatNumeric() float64 {
    return 0
}

func (obj *MShellQuotation2) FloatNumeric() float64 { return 0 }

func (obj *MShellList) FloatNumeric() float64 {
    return 0
}

func (obj *MShellString) FloatNumeric() float64 {
    return 0
}

func (obj *MShellPipe) FloatNumeric() float64 {
    return 0
}

func (obj *MShellInt) FloatNumeric() float64 {
    return float64(obj.Value)
}

func (obj *MShellSimple) FloatNumeric() float64 {
    return 0
}

// CommandLine
func (obj *MShellLiteral) CommandLine() string {
    return obj.LiteralText
}

func (obj *MShellBool) CommandLine() string {
    return "" 
}

func (obj *MShellQuotation) CommandLine() string {
    return ""
}

func (obj *MShellQuotation2) CommandLine() string { return "" }

func (obj *MShellList) CommandLine() string {
    return "" 
}

func (obj *MShellString) CommandLine() string {
    return obj.Content
}

func (obj *MShellPipe) CommandLine() string {
    return ""
}

func (obj *MShellInt) CommandLine() string {
    return strconv.Itoa(obj.Value)
}

func (obj *MShellSimple) CommandLine() string {
    return ""
}

// DebugString

func DebugStrs(objs []MShellObject) []string {
    debugStrs := make([]string, len(objs))
    for i, obj := range objs {
        debugStrs[i] = obj.DebugString()
    }
    return debugStrs
}


func (obj *MShellLiteral) DebugString() string {
    return obj.LiteralText
}

func (obj *MShellBool) DebugString() string {
    return strconv.FormatBool(obj.Value)
}

func (obj *MShellQuotation) DebugString() string {
    // Join the tokens with a space, surrounded by '(' and ')'
    debugStrs := make([]string, len(obj.Tokens))
    for i, token := range obj.Tokens {
        debugStrs[i] = token.Lexeme
    }

    message := "(" + strings.Join(debugStrs, " ") + ")"
    if obj.StandardInputFile != "" {
        message += " < " + obj.StandardInputFile
    }

    if obj.StandardOutputFile != "" {
        message += " > " + obj.StandardOutputFile
    }

    return message
}

func (obj *MShellQuotation2) DebugString() string {
    // Join the tokens with a space, surrounded by '(' and ')'
    debugStrs := make([]string, len(obj.Objects))
    for i, item := range obj.Objects {
        debugStrs[i] = item.DebugString()
    }

    message := "(" + strings.Join(debugStrs, " ") + ")"
    if obj.StandardInputFile != "" {
        message += " < " + obj.StandardInputFile
    }

    if obj.StandardOutputFile != "" {
        message += " > " + obj.StandardOutputFile
    }

    return message
}

func (obj *MShellList) DebugString() string {
    // Join the tokens with a space, surrounded by '[' and ']'
    return "[" + strings.Join(DebugStrs(obj.Items), " ") + "]"
}

func (obj *MShellString) DebugString() string {
    // Surround the string with double quotes
    return "\"" + obj.Content + "\""
}

func (obj *MShellPipe) DebugString() string {
    // Join each item with a ' | '
    return strings.Join(DebugStrs(obj.List.Items), " | ")
}

func (obj *MShellInt) DebugString() string {
    return strconv.Itoa(obj.Value)
}

func (obj *MShellSimple) DebugString() string {
    return obj.Token.Lexeme
}

// IsIndexable

func CheckRangeInclusive(index int, length int, obj MShellObject, toReturn MShellObject) (MShellObject, error) {
    if index < 0 || index >= length {
        return nil, fmt.Errorf("Index %d out of range for %s with length %d.\n", index, obj.TypeName(), length)
    } else { return toReturn, nil }
}
func CheckRangeExclusive(index int, length int, obj MShellObject, toReturn MShellObject) (MShellObject, error) {
    if index < 0 || index > length {
        return nil, fmt.Errorf("Index %d out of range for %s with length %d.\n", index, obj.TypeName(), length)
    } else { return toReturn, nil }
}

// Index
func (obj *MShellLiteral) Index(index int) (MShellObject, error) {
    return CheckRangeInclusive(index, len(obj.LiteralText), obj, &MShellLiteral{LiteralText: string(obj.LiteralText[index])})
}

func (obj *MShellBool) Index(index int) (MShellObject, error) { return nil, fmt.Errorf("Cannot index into a boolean.\n") }

func (obj *MShellQuotation) Index(index int) (MShellObject, error) {
    return CheckRangeInclusive(index, len(obj.Tokens), obj, &MShellQuotation{Tokens: []Token{obj.Tokens[index]}})
}

func (obj *MShellQuotation2) Index(index int) (MShellObject, error) {
    return nil, fmt.Errorf("Cannot index into a quotation.\n")
}

func (obj *MShellList) Index(index int) (MShellObject, error) {
    return CheckRangeInclusive(index, len(obj.Items), obj, obj.Items[index])
}

func (obj *MShellString) Index(index int) (MShellObject, error) {
    return CheckRangeInclusive(index, len(obj.Content), obj, &MShellString{Content: string(obj.Content[index])})
}

func (obj *MShellPipe) Index(index int) (MShellObject, error) {
    return CheckRangeInclusive(index, len(obj.List.Items), obj, obj.List.Items[index])
}

func (obj *MShellInt) Index(index int) (MShellObject, error) { return nil, fmt.Errorf("Cannot index into an integer.\n") }

func (obj *MShellSimple) Index(index int) (MShellObject, error) { return nil, fmt.Errorf("Cannot index into a simple token.\n") }

// SliceStart
func (obj *MShellLiteral) SliceStart(start int) (MShellObject, error) {
    return CheckRangeInclusive(start, len(obj.LiteralText), obj, &MShellLiteral{LiteralText: obj.LiteralText[start:]})
}

func (obj *MShellBool) SliceStart(start int) (MShellObject, error) { return nil, fmt.Errorf("Cannot slice a boolean.\n") }

func (obj *MShellQuotation) SliceStart(start int) (MShellObject, error) {
    return CheckRangeInclusive(start, len(obj.Tokens), obj, &MShellQuotation{Tokens: obj.Tokens[start:]})
}

func (obj *MShellQuotation2) SliceStart(start int) (MShellObject, error) {
    return CheckRangeInclusive(start, len(obj.Objects), obj, &MShellQuotation2{Objects: obj.Objects[start:]})
}

func (obj *MShellList) SliceStart(start int) (MShellObject, error) {
    return CheckRangeInclusive(start, len(obj.Items), obj, &MShellList{Items: obj.Items[start:]})
}

func (obj *MShellString) SliceStart(start int) (MShellObject, error) {
    return CheckRangeInclusive(start, len(obj.Content), obj, &MShellString{Content: obj.Content[start:]})
}

func (obj *MShellPipe) SliceStart(start int) (MShellObject, error) {
    return CheckRangeInclusive(start, len(obj.List.Items), obj, &MShellPipe{List: MShellList{Items: obj.List.Items[start:]}})
}

func (obj *MShellInt) SliceStart(start int) (MShellObject, error) { return nil, fmt.Errorf("cannot slice an integer.\n") }

func (obj *MShellSimple) SliceStart(start int) (MShellObject, error) { return nil, fmt.Errorf("cannot slice a simple token.\n") }

// SliceEnd
func (obj *MShellLiteral) SliceEnd(end int) (MShellObject, error) {
    return CheckRangeExclusive(end, len(obj.LiteralText), obj, &MShellLiteral{LiteralText: obj.LiteralText[:end]})
}

func (obj *MShellBool) SliceEnd(end int) (MShellObject, error) { return nil, fmt.Errorf("cannot slice a boolean.\n") }

func (obj *MShellQuotation) SliceEnd(end int) (MShellObject, error) {
    return CheckRangeExclusive(end, len(obj.Tokens), obj, &MShellQuotation{Tokens: obj.Tokens[:end]})
}

func (obj *MShellQuotation2) SliceEnd(end int) (MShellObject, error) {
    return CheckRangeExclusive(end, len(obj.Objects), obj, &MShellQuotation2{Objects: obj.Objects[:end]})
}

func (obj *MShellList) SliceEnd(end int) (MShellObject, error) {
    return CheckRangeExclusive(end, len(obj.Items), obj, &MShellList{Items: obj.Items[:end]})
}

func (obj *MShellString) SliceEnd(end int) (MShellObject, error) {
    return CheckRangeExclusive(end, len(obj.Content), obj, &MShellString{Content: obj.Content[:end]})
}

func (obj *MShellPipe) SliceEnd(end int) (MShellObject, error) {
    return CheckRangeExclusive(end, len(obj.List.Items), obj, &MShellPipe{List: MShellList{Items: obj.List.Items[:end]}})
}

func (obj *MShellInt) SliceEnd(end int) (MShellObject, error) { return nil, fmt.Errorf("Cannot slice an integer.\n") }

func (obj *MShellSimple) SliceEnd(end int) (MShellObject, error) { return nil, fmt.Errorf("Cannot slice a simple token.\n") }

// Slice

func (obj *MShellLiteral) Slice(startInc int, endExc int) (MShellObject, error) {
    return CheckRangeExclusive(startInc, len(obj.LiteralText), obj, &MShellLiteral{LiteralText: obj.LiteralText[startInc:endExc]})
}

func (obj *MShellBool) Slice(startInc int, endExc int) (MShellObject, error) { return nil, fmt.Errorf("Cannot slice a boolean.\n") }

func (obj *MShellQuotation) Slice(startInc int, endExc int) (MShellObject, error) {
    return CheckRangeExclusive(startInc, len(obj.Tokens), obj, &MShellQuotation{Tokens: obj.Tokens[startInc:endExc]})
}

func (obj *MShellQuotation2) Slice(startInc int, endExc int) (MShellObject, error) {
    return CheckRangeExclusive(startInc, len(obj.Objects), obj, &MShellQuotation2{Objects: obj.Objects[startInc:endExc]})
}

func (obj *MShellList) Slice(startInc int, endExc int) (MShellObject, error) {
    return CheckRangeExclusive(startInc, len(obj.Items), obj, &MShellList{Items: obj.Items[startInc:endExc]})
}

func (obj *MShellString) Slice(startInc int, endExc int) (MShellObject, error) {
    return CheckRangeExclusive(startInc, len(obj.Content), obj, &MShellString{Content: obj.Content[startInc:endExc]})
}

func (obj *MShellPipe) Slice(startInc int, endExc int) (MShellObject, error) {
    return CheckRangeExclusive(startInc, len(obj.List.Items), obj, &MShellPipe{List: MShellList{Items: obj.List.Items[startInc:endExc]}})
}

func (obj *MShellInt) Slice(startInc int, endExc int) (MShellObject, error) { return nil, fmt.Errorf("Cannot slice an integer.\n") }

func (obj *MShellSimple) Slice(startInc int, endExc int) (MShellObject, error) { return nil, fmt.Errorf("Cannot slice a simple token.\n") }


// ToJson
func (obj *MShellLiteral) ToJson() string {
    return fmt.Sprintf("{\"type\": \"Literal\", \"value\": \"%s\"}", obj.LiteralText)
}

func (obj *MShellBool) ToJson() string {
    return fmt.Sprintf("{\"type\": \"Boolean\", \"value\": %t}", obj.Value)
}

func (obj *MShellQuotation) ToJson() string {
    builder := strings.Builder{}
    builder.WriteString("{\"type\": \"Quotation\", \"tokens\": [")
    if len(obj.Tokens) > 0 {
        builder.WriteString(obj.Tokens[0].ToJson())
        for _, token := range obj.Tokens[1:] {
            builder.WriteString(", ")
            builder.WriteString(token.ToJson())
        }
    }
    builder.WriteString("]}")
    return builder.String()
}

func (obj *MShellQuotation2) ToJson() string {
    builder := strings.Builder{}
    builder.WriteString("{\"type\": \"Quotation\", \"objects\": [")
    if len(obj.Objects) > 0 {
        builder.WriteString(obj.Objects[0].ToJson())
        for _, item := range obj.Objects[1:] {
            builder.WriteString(", ")
            builder.WriteString(item.ToJson())
        }
    }
    builder.WriteString("]}")
    return builder.String()
}

func (obj *MShellList) ToJson() string {
    builder := strings.Builder{}
    builder.WriteString("{\"type\": \"List\", \"items\": [")
    if len(obj.Items) > 0 {
        builder.WriteString(obj.Items[0].ToJson())
        for _, item := range obj.Items[1:] {
            builder.WriteString(", ")
            builder.WriteString(item.ToJson())
        }
    }
    builder.WriteString("]}")
    return builder.String()
}

func (obj *MShellString) ToJson() string {
    return fmt.Sprintf("{\"type\": \"String\", \"content\": \"%s\"}", obj.Content)
}

func (obj *MShellPipe) ToJson() string {
    return fmt.Sprintf("{\"type\": \"Pipe\", \"list\": %s}", obj.List.ToJson())
}

func (obj *MShellInt) ToJson() string {
    return fmt.Sprintf("{\"type\": \"Integer\", \"value\": %d}", obj.Value)
}

func (obj *MShellSimple) ToJson() string {
    return fmt.Sprintf("{\"type\": \"Simple\", \"token\": %s}", obj.Token.ToJson())
}

func ParseRawString(inputString string) (string, error) {
    // Purpose of this function is to remove outer quotes, handle escape characters
    if len(inputString) < 2 {
        return "", fmt.Errorf("input string should have a minimum length of 2 for surrounding double quotes.\n")
    }

    var b strings.Builder
	index := 1
	inEscape := false

	for index < len(inputString) - 1 {
		c := inputString[index]

		if inEscape {
			switch c {
			case 'n':
				b.WriteRune('\n')
			case 't':
				b.WriteRune('\t')
			case 'r':
				b.WriteRune('\r')
			case '\\':
				b.WriteRune('\\')
			case '"':
				b.WriteRune('"')
			default:
				return "", fmt.Errorf("invalid escape character '%c'", c)
			}
			inEscape = false
		} else {
			if c == '\\' {
				inEscape = true
			} else {
				b.WriteRune(rune(c))
			}
		}

		index++
	}

	return b.String(), nil
}
