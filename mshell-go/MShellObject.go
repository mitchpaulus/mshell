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

func ParseRawString(inputString string) (string, error) {
    // Purpose of this function is to remove outer quotes, handle escape characters
    if len(inputString) < 2 {
        return "", fmt.Errorf("input string should have a minimum length of 2 for surrounding double quotes")
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
