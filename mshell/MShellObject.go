package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"os"
	"time"
)

// TruncateMiddle truncates a string to a maximum length, adding "..." in the middle if necessary.
func TruncateMiddle(s string, maxLen int) string {
	if len(s) <= maxLen || maxLen <= 3 {
		// No truncation needed or maxLen too small to fit "..."
		return s
	}

	// Calculate lengths of the parts around "..."
	half := (maxLen - 3) / 2
	remainder := (maxLen - 3) % 2
	start := s[:half]                 // First part of the string
	end := s[len(s)-half-remainder:]  // Last part of the string

	return start + "..." + end
}

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
	ToString() string
	IndexErrStr() string
	Concat(other MShellObject) (MShellObject, error)
}

type MShellSimple struct {
	Token Token
}

type MShellDateTime struct {
	// TODO: replace with my own simpler int64 based on the Calendrical Calculation book.
	Time time.Time
	Token Token
}

func (obj *MShellDateTime) TypeName() string {
	return "DateTime"
}

func (obj *MShellDateTime) IsCommandLineable() bool {
	return true
}

func (obj *MShellDateTime) IsNumeric() bool {
	return false
}

func (obj *MShellDateTime) FloatNumeric() float64 {
	return 0
}

func (obj *MShellDateTime) CommandLine() string {
	return obj.Token.Lexeme
}

func (obj *MShellDateTime) DebugString() string {
	return obj.Token.Lexeme
}

func (obj *MShellDateTime) Index(index int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot index into a DateTime.\n")
}

func (obj *MShellDateTime) SliceStart(start int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a DateTime.\n")
}

func (obj *MShellDateTime) SliceEnd(end int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a DateTime.\n")
}

func (obj *MShellDateTime) Slice(startInc int, endExc int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a DateTime.\n")
}

func (obj *MShellDateTime) ToJson() string {
	return fmt.Sprintf("{\"type\": \"DateTime\", \"value\": \"%s\"}", obj.Token.Lexeme)
}

func (obj *MShellDateTime) ToString() string {
	return obj.Token.Lexeme
}

func (obj *MShellDateTime) IndexErrStr() string {
	return ""
}

func (obj *MShellDateTime) Concat(other MShellObject) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot concatenate a DateTime.\n")
}

type MShellLiteral struct {
	LiteralText string
}

type MShellBool struct {
	Value bool
}

type MShellQuotation struct {
	MShellParseQuote      *MShellParseQuote
	Tokens                []MShellParseItem
	StdinBehavior         StdinBehavior
	StandardInputContents string
	StandardInputFile     string
	StandardOutputFile    string
	StandardErrorFile     string
	Variables             map[string]MShellObject
}

func (q *MShellQuotation) GetStartToken() Token {
	return q.MShellParseQuote.StartToken
}

func (q *MShellQuotation) GetEndToken() Token {
	return q.MShellParseQuote.EndToken
}

func (q *MShellQuotation) BuildExecutionContext(context *ExecuteContext) (*ExecuteContext, error) {
	quoteContext := ExecuteContext{
		StandardInput:  nil,
		StandardOutput: nil,
		Variables: q.Variables,
		ShouldCloseInput: false,
		ShouldCloseOutput: false,
	}

	if q.StdinBehavior != STDIN_NONE {
		if q.StdinBehavior == STDIN_CONTENT {
			quoteContext.StandardInput = strings.NewReader(q.StandardInputContents)
		} else if q.StdinBehavior == STDIN_FILE {
			file, err := os.Open(q.StandardInputFile)
			if err != nil {
				t := q.GetStartToken()
				return nil, fmt.Errorf("%d:%d: Error opening file %s for reading: %s\n", t.Line, t.Column, q.StandardInputFile, err.Error())
			}
			quoteContext.StandardInput = file
			quoteContext.ShouldCloseInput = true
			// defer file.Close()
		}
	} else if context.StandardInput != nil {
		quoteContext.StandardInput = context.StandardInput
	} else {
		// Default to stdin of this process itself
		quoteContext.StandardInput = os.Stdin
	}

	if q.StandardOutputFile != "" {
		file, err := os.Create(q.StandardOutputFile)
		if err != nil {
			t := q.GetStartToken()
			return nil, fmt.Errorf("%d:%d: Error opening file %s for writing: %s\n", t.Line, t.Column, q.StandardOutputFile, err.Error())
		}
		quoteContext.StandardOutput = file
		quoteContext.ShouldCloseOutput = true
		// defer file.Close()
	} else if context.StandardOutput != nil {
		quoteContext.StandardOutput = context.StandardOutput
	} else {
		// Default to stdout of this process itself
		quoteContext.StandardOutput = os.Stdout
	}

	return &quoteContext, nil
}

// type MShellQuotation2 struct {
// Objects []MShellParseItem
// StandardInputFile string
// StandardOutputFile string
// StandardErrorFile string
// }

type StdoutBehavior int

const (
	STDOUT_NONE StdoutBehavior = iota
	STDOUT_LINES
	STDOUT_STRIPPED
	STDOUT_COMPLETE
)

type StdinBehavior int

const (
	STDIN_NONE StdinBehavior = iota
	STDIN_FILE
	STDIN_CONTENT
)

type MShellList struct {
	Items                 []MShellObject
	StdinBehavior         StdinBehavior
	StandardInputContents string
	StandardInputFile     string
	StandardOutputFile    string
	StandardErrorFile     string
	// This sets how stdout is handled, whether it's broken up into lines, stripped of trailing newline, or left as is
	StdoutBehavior StdoutBehavior
}

func NewList(initLength int) *MShellList {
	if initLength < 0 {
		// panic
		panic("Cannot create a list with a negative length.")
	}
	return &MShellList{
		Items:                 make([]MShellObject, initLength),
		StdinBehavior:         STDIN_NONE,
		StandardInputContents: "",
		StandardInputFile:     "",
		StandardOutputFile:    "",
		StandardErrorFile:     "",
		StdoutBehavior:        STDOUT_NONE,
	}
}

type MShellString struct {
	Content string
}

type MShellPath struct {
	Path string
}

type MShellPipe struct {
	List           MShellList
	StdoutBehavior StdoutBehavior
}

type MShellInt struct {
	Value int
}

type MShellFloat struct {
	Value float64
}

// ToString
func (obj *MShellLiteral) ToString() string {
	return obj.LiteralText
}

func (obj *MShellBool) ToString() string {
	return strconv.FormatBool(obj.Value)
}

func (obj *MShellQuotation) ToString() string {
	return obj.DebugString()
}

func (obj *MShellList) ToString() string {
	return obj.DebugString()
}

func (obj *MShellString) ToString() string {
	return obj.Content
}

func (obj *MShellPath) ToString() string {
	return obj.Path
}

func (obj *MShellPipe) ToString() string {
	return obj.DebugString()
}

func (obj *MShellInt) ToString() string {
	return strconv.Itoa(obj.Value)
}

func (obj *MShellFloat) ToString() string {
	return strconv.FormatFloat(obj.Value, 'f', -1, 64)
}

func (obj *MShellSimple) ToString() string {
	return obj.Token.Lexeme
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

func (obj *MShellPath) TypeName() string {
	return "Path"
}

func (obj *MShellPipe) TypeName() string {
	return "Pipe"
}

func (obj *MShellInt) TypeName() string {
	return "Integer"
}

func (obj *MShellFloat) TypeName() string {
	return "Float"
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

func (obj *MShellList) IsCommandLineable() bool {
	return false
}

func (obj *MShellString) IsCommandLineable() bool {
	return true
}

func (obj *MShellPath) IsCommandLineable() bool {
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

func (obj *MShellFloat) IsCommandLineable() bool {
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

func (obj *MShellPath) IsNumeric() bool {
	return false
}

func (obj *MShellPipe) IsNumeric() bool {
	return false
}

func (obj *MShellInt) IsNumeric() bool {
	return true
}

func (obj *MShellFloat) IsNumeric() bool {
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

func (obj *MShellList) FloatNumeric() float64 {
	return 0
}

func (obj *MShellString) FloatNumeric() float64 {
	return 0
}

func (obj *MShellPath) FloatNumeric() float64 {
	return 0
}

func (obj *MShellPipe) FloatNumeric() float64 {
	return 0
}

func (obj *MShellInt) FloatNumeric() float64 {
	return float64(obj.Value)
}

func (obj *MShellFloat) FloatNumeric() float64 {
	return obj.Value
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

func (obj *MShellList) CommandLine() string {
	return ""
}

func (obj *MShellString) CommandLine() string {
	return obj.Content
}

func (obj *MShellPath) CommandLine() string {
	return obj.Path
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

func (obj *MShellFloat) CommandLine() string {
	return strconv.FormatFloat(obj.Value, 'f', -1, 64)
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
		debugStrs[i] = token.DebugString()
	}

	message := "(" + strings.Join(debugStrs, " ") + ")"

	if obj.StdinBehavior == STDIN_CONTENT {
		message += " < " + TruncateMiddle(obj.StandardInputContents, 30)
	} else if obj.StdinBehavior == STDIN_FILE {
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
	// Surround the string with double quotes, keep just the first 15 and last 15 characters
	if len(obj.Content) > 30 {
		return "\"" + obj.Content[:15] + "..." + obj.Content[len(obj.Content)-15:] + "\""
	}

	return "\"" + obj.Content + "\""
}

func (obj *MShellPath) DebugString() string {
	if len(obj.Path) > 30 {
		return "`" + obj.Path[:15] + "..." + obj.Path[len(obj.Path)-15:] + "`"
	}

	return "`" + obj.Path + "`"
}

func (obj *MShellPipe) DebugString() string {
	// Join each item with a ' | '
	return strings.Join(DebugStrs(obj.List.Items), " | ")
}

func (obj *MShellInt) DebugString() string {
	return strconv.Itoa(obj.Value)
}

func (obj *MShellFloat) DebugString() string {
	return strconv.FormatFloat(obj.Value, 'f', -1, 64)
}

func (obj *MShellSimple) DebugString() string {
	return obj.Token.Lexeme
}

func (obj *MShellLiteral) IndexErrStr() string {
	return fmt.Sprintf(" (%s)", obj.LiteralText)
}

func (obj *MShellBool) IndexErrStr() string {
	return ""
}

func (obj *MShellQuotation) IndexErrStr() string {
	if len(obj.Tokens) == 0 {
		return ""
	}
	return fmt.Sprintf(" Last item: %s", obj.Tokens[len(obj.Tokens)-1].DebugString())
}

func (obj *MShellList) IndexErrStr() string {
	if len(obj.Items) == 0 {
		return ""
	}
	return fmt.Sprintf(" Last item: %s", obj.Items[len(obj.Items)-1].DebugString())
}

func (obj *MShellString) IndexErrStr() string {
	return fmt.Sprintf(" '%s'", obj.Content)
}

func (obj *MShellPath) IndexErrStr() string {
	return fmt.Sprintf(" `%s`", obj.Path)
}

func (obj *MShellPipe) IndexErrStr() string {
	if len(obj.List.Items) == 0 {
		return ""
	}
	return fmt.Sprintf(" Last item: %s", obj.List.Items[len(obj.List.Items)-1].DebugString())
}

func (obj *MShellInt) IndexErrStr() string {
	return ""
}

func (obj *MShellFloat) IndexErrStr() string {
	return ""
}

func IndexCheck(index int, length int, obj MShellObject) error {
	if index < 0 || index >= length {
		return fmt.Errorf("Index %d out of range for %s with length %d.%s\n", index, obj.TypeName(), length, obj.IndexErrStr())
	} else {
		return nil
	}
}

func IndexCheckExc(index int, length int, obj MShellObject) error {
	if index < 0 || index > length {
		return fmt.Errorf("Index %d out of range for %s with length %d.%s\n", index, obj.TypeName(), length, obj.IndexErrStr())
	} else {
		return nil
	}
}

// Index
func (obj *MShellLiteral) Index(index int) (MShellObject, error) {
	if index < 0 {
		index = len(obj.LiteralText) + index
	}

	if err := IndexCheck(index, len(obj.LiteralText), obj); err != nil {
		return nil, err
	}
	return &MShellLiteral{LiteralText: string(obj.LiteralText[index])}, nil
}

func (obj *MShellBool) Index(index int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot index into a boolean.\n")
}

func (obj *MShellQuotation) Index(index int) (MShellObject, error) {
	if index < 0 {
		index = len(obj.Tokens) + index
	}
	if err := IndexCheck(index, len(obj.Tokens), obj); err != nil {
		return nil, err
	}
	return &MShellQuotation{Tokens: []MShellParseItem{obj.Tokens[index]}}, nil
}

func (obj *MShellList) Index(index int) (MShellObject, error) {
	if index < 0 {
		index = len(obj.Items) + index
	}

	if err := IndexCheck(index, len(obj.Items), obj); err != nil {
		return nil, err
	}
	return obj.Items[index], nil
}

func (obj *MShellString) Index(index int) (MShellObject, error) {
	if index < 0 {
		index = len(obj.Content) + index
	}

	if err := IndexCheck(index, len(obj.Content), obj); err != nil {
		return nil, err
	}
	return &MShellString{Content: string(obj.Content[index])}, nil
}

func (obj *MShellPath) Index(index int) (MShellObject, error) {
	if index < 0 {
		index = len(obj.Path) + index
	}

	if err := IndexCheck(index, len(obj.Path), obj); err != nil {
		return nil, err
	}
	return &MShellPath{Path: string(obj.Path[index])}, nil
}

func (obj *MShellPipe) Index(index int) (MShellObject, error) {
	if index < 0 {
		index = len(obj.List.Items) + index
	}

	if err := IndexCheck(index, len(obj.List.Items), obj); err != nil {
		return nil, err
	}

	newList := obj.List.Items[index]
	return newList, nil
}

func (obj *MShellInt) Index(index int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot index into an integer.\n")
}

func (obj *MShellFloat) Index(index int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot index into a float.\n")
}

func (obj *MShellSimple) Index(index int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot index into a simple token.\n")
}

// SliceStart
func (obj *MShellLiteral) SliceStart(start int) (MShellObject, error) {
	if start < 0 {
		start = len(obj.LiteralText) + start
	}

	if err := IndexCheck(start, len(obj.LiteralText), obj); err != nil {
		return nil, err
	}
	return &MShellLiteral{LiteralText: obj.LiteralText[start:]}, nil
}

func (obj *MShellBool) SliceStart(start int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a boolean.\n")
}

func (obj *MShellQuotation) SliceStart(start int) (MShellObject, error) {
	if start < 0 {
		start = len(obj.Tokens) + start
	}
	if err := IndexCheck(start, len(obj.Tokens), obj); err != nil {
		return nil, err
	}
	return &MShellQuotation{Tokens: obj.Tokens[start:]}, nil
}

func (obj *MShellList) SliceStart(start int) (MShellObject, error) {
	if start < 0 {
		start = len(obj.Items) + start
	}
	if err := IndexCheck(start, len(obj.Items), obj); err != nil {
		return nil, err
	}

	newList := NewList(0)
	newList.Items = obj.Items[start:]
	return newList, nil
}

func (obj *MShellString) SliceStart(start int) (MShellObject, error) {
	if start < 0 {
		start = len(obj.Content) + start
	}
	if err := IndexCheck(start, len(obj.Content), obj); err != nil {
		return nil, err
	}
	return &MShellString{Content: obj.Content[start:]}, nil
}

func (obj *MShellPath) SliceStart(start int) (MShellObject, error) {
	if start < 0 {
		start = len(obj.Path) + start
	}
	if err := IndexCheck(start, len(obj.Path), obj); err != nil {
		return nil, err
	}

	return &MShellPath{Path: obj.Path[start:]}, nil
}

func (obj *MShellPipe) SliceStart(start int) (MShellObject, error) {
	if start < 0 {
		start = len(obj.List.Items) + start
	}

	if err := IndexCheck(start, len(obj.List.Items), obj); err != nil {
		return nil, err
	}

	newList := NewList(0)
	newList.Items = obj.List.Items[start:]
	return newList, nil
}

func (obj *MShellInt) SliceStart(start int) (MShellObject, error) {
	return nil, fmt.Errorf("cannot slice an integer.\n")
}

func (obj *MShellFloat) SliceStart(start int) (MShellObject, error) {
	return nil, fmt.Errorf("cannot slice a float.\n")
}

func (obj *MShellSimple) SliceStart(start int) (MShellObject, error) {
	return nil, fmt.Errorf("cannot slice a simple token.\n")
}

// SliceEnd
func (obj *MShellLiteral) SliceEnd(end int) (MShellObject, error) {
	if end < 0 {
		end = len(obj.LiteralText) + end
	}

	if err := IndexCheckExc(end, len(obj.LiteralText), obj); err != nil {
		return nil, err
	}
	return &MShellLiteral{LiteralText: obj.LiteralText[:end]}, nil
}

func (obj *MShellBool) SliceEnd(end int) (MShellObject, error) {
	return nil, fmt.Errorf("cannot slice a boolean.\n")
}

func (obj *MShellQuotation) SliceEnd(end int) (MShellObject, error) {
	if end < 0 {
		end = len(obj.Tokens) + end
	}
	if err := IndexCheckExc(end, len(obj.Tokens), obj); err != nil {
		return nil, err
	}
	return &MShellQuotation{Tokens: obj.Tokens[:end]}, nil
}

func (obj *MShellList) SliceEnd(end int) (MShellObject, error) {
	if end < 0 {
		end = len(obj.Items) + end
	}
	if err := IndexCheckExc(end, len(obj.Items), obj); err != nil {
		return nil, err
	}
	newList := NewList(0)
	newList.Items = obj.Items[:end]
	return newList, nil
}

func (obj *MShellString) SliceEnd(end int) (MShellObject, error) {
	if end < 0 {
		end = len(obj.Content) + end
	}
	if err := IndexCheckExc(end, len(obj.Content), obj); err != nil {
		return nil, err
	}
	return &MShellString{Content: obj.Content[:end]}, nil
}

func (obj *MShellPath) SliceEnd(end int) (MShellObject, error) {
	if end < 0 {
		end = len(obj.Path) + end
	}
	if err := IndexCheckExc(end, len(obj.Path), obj); err != nil {
		return nil, err
	}

	return &MShellPath{Path: obj.Path[:end]}, nil
}

func (obj *MShellPipe) SliceEnd(end int) (MShellObject, error) {
	if end < 0 {
		end = len(obj.List.Items) + end
	}
	if err := IndexCheckExc(end, len(obj.List.Items), obj); err != nil {
		return nil, err
	}
	newList := NewList(0)
	newList.Items = obj.List.Items[:end]
	return newList, nil
}

func (obj *MShellInt) SliceEnd(end int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice an integer.\n")
}

func (obj *MShellFloat) SliceEnd(end int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a float.\n")
}

func (obj *MShellSimple) SliceEnd(end int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a simple token.\n")
}

// Slice
func SliceIndexCheck(startInc int, endExc int, length int, obj MShellObject) error {
	if startInc < 0 {
		startInc = length + startInc
	}

	if endExc < 0 {
		endExc = length + endExc
	}

	if startInc < 0 || startInc > endExc || endExc > length {
		return fmt.Errorf("Invalid slice range [%d:%d) for %s with length %d.\n", startInc, endExc, obj.TypeName(), length)
	} else {
		return nil
	}
}

func (obj *MShellLiteral) Slice(startInc int, endExc int) (MShellObject, error) {
	if startInc < 0 {
		startInc = len(obj.LiteralText) + startInc
	}

	if endExc < 0 {
		endExc = len(obj.LiteralText) + endExc
	}

	if err := SliceIndexCheck(startInc, endExc, len(obj.LiteralText), obj); err != nil {
		return nil, err
	}
	return &MShellLiteral{LiteralText: obj.LiteralText[startInc:endExc]}, nil
}

func (obj *MShellBool) Slice(startInc int, endExc int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a boolean.\n")
}

func (obj *MShellQuotation) Slice(startInc int, endExc int) (MShellObject, error) {
	if startInc < 0 {
		startInc = len(obj.Tokens) + startInc
	}

	if endExc < 0 {
		endExc = len(obj.Tokens) + endExc
	}

	if err := SliceIndexCheck(startInc, endExc, len(obj.Tokens), obj); err != nil {
		return nil, err
	}
	return &MShellQuotation{Tokens: obj.Tokens[startInc:endExc]}, nil
}

func (obj *MShellList) Slice(startInc int, endExc int) (MShellObject, error) {
	if startInc < 0 {
		startInc = len(obj.Items) + startInc
	}

	if endExc < 0 {
		endExc = len(obj.Items) + endExc
	}

	if err := SliceIndexCheck(startInc, endExc, len(obj.Items), obj); err != nil {
		return nil, err
	}

	newList := NewList(0)
	newList.Items = obj.Items[startInc:endExc]
	return newList, nil
}

func (obj *MShellString) Slice(startInc int, endExc int) (MShellObject, error) {
	if startInc < 0 {
		startInc = len(obj.Content) + startInc
	}

	if endExc < 0 {
		endExc = len(obj.Content) + endExc
	}

	if err := SliceIndexCheck(startInc, endExc, len(obj.Content), obj); err != nil {
		return nil, err
	}
	return &MShellString{Content: obj.Content[startInc:endExc]}, nil
}

func (obj *MShellPath) Slice(startInc int, endExc int) (MShellObject, error) {
	if startInc < 0 {
		startInc = len(obj.Path) + startInc
	}

	if endExc < 0 {
		endExc = len(obj.Path) + endExc
	}

	if err := SliceIndexCheck(startInc, endExc, len(obj.Path), obj); err != nil {
		return nil, err
	}

	return &MShellPath{Path: obj.Path[startInc:endExc]}, nil
}

func (obj *MShellPipe) Slice(startInc int, endExc int) (MShellObject, error) {
	if startInc < 0 {
		startInc = len(obj.List.Items) + startInc
	}

	if endExc < 0 {
		endExc = len(obj.List.Items) + endExc
	}

	if err := SliceIndexCheck(startInc, endExc, len(obj.List.Items), obj); err != nil {
		return nil, err
	}

	newList := NewList(0)
	newList.Items = obj.List.Items[startInc:endExc]
	return newList, nil
}

func (obj *MShellInt) Slice(startInc int, endExc int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice an integer.\n")
}

func (obj *MShellFloat) Slice(startInc int, endExc int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a float.\n")
}

func (obj *MShellSimple) Slice(startInc int, endExc int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a simple token.\n")
}

// ToJson
func (obj *MShellLiteral) ToJson() string {
	escBytes, _ := json.Marshal(obj.LiteralText)
	return fmt.Sprintf("{\"type\": \"Literal\", \"value\": \"%s\"}", string(escBytes))
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
	// Escape the content
	escBytes, _ := json.Marshal(obj.Content)
	return fmt.Sprintf("{\"type\": \"String\", \"content\": %s}", string(escBytes))
}

func (obj *MShellPath) ToJson() string {
	// Escape the content
	escBytes, _ := json.Marshal(obj.Path)
	return fmt.Sprintf("{\"type\": \"Path\", \"path\": %s}", string(escBytes))
}

func (obj *MShellPipe) ToJson() string {
	return fmt.Sprintf("{\"type\": \"Pipe\", \"list\": %s}", obj.List.ToJson())
}

func (obj *MShellInt) ToJson() string {
	return fmt.Sprintf("{\"type\": \"Integer\", \"value\": %d}", obj.Value)
}

func (obj *MShellFloat) ToJson() string {
	return fmt.Sprintf("{\"type\": \"Float\", \"value\": %f}", obj.Value)
}

func (obj *MShellSimple) ToJson() string {
	return fmt.Sprintf("{\"type\": \"Simple\", \"token\": %s}", obj.Token.ToJson())
}

// Concat
func (obj *MShellLiteral) Concat(other MShellObject) (MShellObject, error) {
	asLiteral, ok := other.(*MShellLiteral)
	if !ok {
		return nil, fmt.Errorf("Cannot concatenate a Literal with a %s.\n", other.TypeName())
	}

	return &MShellLiteral{LiteralText: obj.LiteralText + asLiteral.LiteralText}, nil
}

func (obj *MShellBool) Concat(other MShellObject) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot concatenate a boolean.\n")
}

func (obj *MShellQuotation) Concat(other MShellObject) (MShellObject, error) {
	asQuotation, ok := other.(*MShellQuotation)
	if !ok {
		return nil, fmt.Errorf("Cannot concatenate a Quotation with a %s.\n", other.TypeName())
	}

	newTokens := make([]MShellParseItem, len(obj.Tokens)+len(asQuotation.Tokens))
	copy(newTokens, obj.Tokens)
	copy(newTokens[len(obj.Tokens):], asQuotation.Tokens)
	return &MShellQuotation{Tokens: newTokens}, nil
}

func (obj *MShellList) Concat(other MShellObject) (MShellObject, error) {
	asList, ok := other.(*MShellList)
	if !ok {
		return nil, fmt.Errorf("Cannot concatenate a List with a %s.\n", other.TypeName())
	}

	newItems := make([]MShellObject, len(obj.Items)+len(asList.Items))
	copy(newItems, obj.Items)
	copy(newItems[len(obj.Items):], asList.Items)
	return &MShellList{Items: newItems}, nil
}

func (obj *MShellString) Concat(other MShellObject) (MShellObject, error) {
	asString, ok := other.(*MShellString)
	if !ok {
		return nil, fmt.Errorf("Cannot concatenate a String with a %s.\n", other.TypeName())
	}

	return &MShellString{Content: obj.Content + asString.Content}, nil
}

func (obj *MShellPath) Concat(other MShellObject) (MShellObject, error) {
	asPath, ok := other.(*MShellPath)
	if !ok {
		return nil, fmt.Errorf("Cannot concatenate a Path with a %s.\n", other.TypeName())
	}

	return &MShellPath{Path: obj.Path + asPath.Path}, nil
}

func (obj *MShellPipe) Concat(other MShellObject) (MShellObject, error) {
	asPipe, ok := other.(*MShellPipe)
	if !ok {
		return nil, fmt.Errorf("Cannot concatenate a Pipe with a %s.\n", other.TypeName())
	}

	newItems := make([]MShellObject, len(obj.List.Items)+len(asPipe.List.Items))
	copy(newItems, obj.List.Items)
	copy(newItems[len(obj.List.Items):], asPipe.List.Items)
	return &MShellPipe{List: MShellList{Items: newItems}}, nil
}

func (obj *MShellInt) Concat(other MShellObject) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot concatenate an integer.\n")
}

func (obj *MShellFloat) Concat(other MShellObject) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot concatenate a float.\n")
}

func ParseRawString(inputString string) (string, error) {
	// Purpose of this function is to remove outer quotes, handle escape characters
	if len(inputString) < 2 {
		return "", fmt.Errorf("input string should have a minimum length of 2 for surrounding double quotes.\n")
	}

	var b strings.Builder
	index := 1
	inEscape := false

	for index < len(inputString)-1 {
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


func ParseRawPath(inputString string) (string, error) {
	// Purpose of this function is to remove outer quotes, handle escape characters
	if len(inputString) < 2 {
		return "", fmt.Errorf("input string should have a minimum length of 2 for surrounding double quotes.\n")
	}

	var b strings.Builder
	index := 1
	inEscape := false

	for index < len(inputString)-1 {
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
			case '`':
				b.WriteRune('`')
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
