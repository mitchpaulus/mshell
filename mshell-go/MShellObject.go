package main

import (
    "strconv"
    "strings"
    "fmt"
)

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
}

type MShellList struct {
    Items []MShellObject
    StandardInputFile string
    StandardOutputFile string
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

// SliceStart
func (obj *MShellLiteral) SliceStart(start int) (MShellObject, error) {
    return CheckRangeInclusive(start, len(obj.LiteralText), obj, &MShellLiteral{LiteralText: obj.LiteralText[start:]})
}

func (obj *MShellBool) SliceStart(start int) (MShellObject, error) { return nil, fmt.Errorf("Cannot slice a boolean.\n") }

func (obj *MShellQuotation) SliceStart(start int) (MShellObject, error) {
    return CheckRangeInclusive(start, len(obj.Tokens), obj, &MShellQuotation{Tokens: obj.Tokens[start:]})
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

// SliceEnd
func (obj *MShellLiteral) SliceEnd(end int) (MShellObject, error) {
    return CheckRangeExclusive(end, len(obj.LiteralText), obj, &MShellLiteral{LiteralText: obj.LiteralText[:end]})
}

func (obj *MShellBool) SliceEnd(end int) (MShellObject, error) { return nil, fmt.Errorf("cannot slice a boolean.\n") }

func (obj *MShellQuotation) SliceEnd(end int) (MShellObject, error) {
    return CheckRangeExclusive(end, len(obj.Tokens), obj, &MShellQuotation{Tokens: obj.Tokens[:end]})
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
