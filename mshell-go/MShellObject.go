package main

import (
    "io"
    "strconv"
    "strings"
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
    StandardInputFile io.Reader
    StandardOutputFile io.Writer
}

type MShellList struct {
    Items []MShellObject
    StandardInputFile io.Reader
    StandardOutputFile io.Writer
}

type MShellString struct {
    Content string
}

type MShellPipe struct {
    List MShellList
}

type MShellInt struct {
    Value int64
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
    return strconv.FormatInt(obj.Value, 10)
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

    return "(" + strings.Join(debugStrs, " ") + ")"
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

