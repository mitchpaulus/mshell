package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"golang.org/x/net/html"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
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
	start := s[:half]                // First part of the string
	end := s[len(s)-half-remainder:] // Last part of the string

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
	DebugString() string // This is meant for things like error messages, should be limited in length to 30 chars or so.
	Index(index int) (MShellObject, error)
	SliceStart(startInclusive int) (MShellObject, error)
	SliceEnd(end int) (MShellObject, error)
	Slice(startInc int, endExc int) (MShellObject, error)
	ToJson() string
	ToString() string // This is what is used with 'str' command
	IndexErrStr() string
	Concat(other MShellObject) (MShellObject, error)
	Equals(other MShellObject) (bool, error)
	CastString() (string, error) // This is meant for completely unambiougous conversion to a string value.
}

type MShellSimple struct {
	Token Token
}

// Binary {{{
// Represents raw binary data
type MShellBinary []byte

func (b MShellBinary) TypeName() string {
	return "Binary"
}

func (b MShellBinary) IsCommandLineable() bool {
	return false
}

func (b MShellBinary) IsNumeric() bool {
	return false
}

func (b MShellBinary) FloatNumeric() float64 {
	return 0
}

func (b MShellBinary) CommandLine() string {
	return ""
}

func (b MShellBinary) DebugString() string {
	// Return a hex representation of the first 5 bytes, or the whole thing if it's shorter
	if len(b) > 5 {
		return fmt.Sprintf("Binary(%x...)", b[:5])
	} else {
		return fmt.Sprintf("Binary(%x)", b)
	}
}

func (b MShellBinary) Index(index int) (MShellObject, error) {
	if index < 0 || index >= len(b) {
		return nil, fmt.Errorf("Index %d out of range for Binary with length %d.\n", index, len(b))
	}

	return MShellBinary{b[index]}, nil
}

func (b MShellBinary) SliceStart(startInclusive int) (MShellObject, error) {
	if startInclusive < 0 || startInclusive >= len(b) {
		return nil, fmt.Errorf("Start index %d out of range for Binary with length %d.\n", startInclusive, len(b))
	}
	return MShellBinary(b[startInclusive:]), nil
}

func (b MShellBinary) SliceEnd(end int) (MShellObject, error) {
	if end < 0 || end > len(b) {
		return nil, fmt.Errorf("End index %d out of range for Binary with length %d.\n", end, len(b))
	}

	return MShellBinary(b[:end]), nil
}

func (b MShellBinary) Slice(startInc int, endExc int) (MShellObject, error) {
	if startInc < 0 || startInc >= len(b) || endExc < 0 || endExc > len(b) || startInc > endExc {
		return nil, fmt.Errorf("Slice indices %d:%d out of range for Binary with length %d.\n", startInc, endExc, len(b))
	}

	return MShellBinary(b[startInc:endExc]), nil
}

func (b MShellBinary) ToJson() string {
	// Convert the binary data to a base64 string for JSON representation
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(b)))
	base64.StdEncoding.Encode(encoded, b)
	return fmt.Sprintf("\"%s\"", string(encoded))
}

func (b MShellBinary) ToString() string {
	// Convert the binary data to a hex string, the entire thing.
	return fmt.Sprintf("%x", b)
}

func (b MShellBinary) IndexErrStr() string {
	return fmt.Sprintf(" Indexing into Binary is not supported. Length: %d", len(b))
}

func (b MShellBinary) Concat(other MShellObject) (MShellObject, error) {
	if otherBinary, ok := other.(MShellBinary); ok {
		// Concatenate the two binary slices
		return MShellBinary(append(b, otherBinary...)), nil
	}
	return nil, fmt.Errorf("Cannot concatenate Binary with %s.\n", other.TypeName())
}

func (b MShellBinary) Equals(other MShellObject) (bool, error) {
	if otherBinary, ok := other.(MShellBinary); ok {
		// Compare the two binary slices
		if len(b) != len(otherBinary) {
			return false, nil
		} else {
			for i := range b {
				if b[i] != otherBinary[i] {
					return false, nil
				}
			}
			return true, nil
		}
	}
	return false, fmt.Errorf("Cannot compare Binary with %s.\n", other.TypeName())
}

func (b MShellBinary) CastString() (string, error) {
	return "", fmt.Errorf("Cannot cast Binary to a string.\n")
}

/// }}}

// Maybe {{{
type Maybe struct {
	obj MShellObject // If nil, the Maybe is None.
}

func (m Maybe) IsNone() bool {
	return m.obj == nil
}

func (m Maybe) TypeName() string {
	return "Maybe"
}

func (m Maybe) IsCommandLineable() bool {
	return false
}

func (m Maybe) IsNumeric() bool {
	return false
}

func (m Maybe) FloatNumeric() float64 {
	return 0
}
func (m Maybe) CommandLine() string {
	return ""
}

// This is meant for things like error messages, should be limited in length to 30 chars or so.
func (m Maybe) DebugString() string {
	if m.obj == nil {
		return "None"
	}
	return fmt.Sprintf("Maybe(%s)", m.obj.DebugString())
}
func (m Maybe) Index(index int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot index into a Maybe.\n")
}

func (m Maybe) SliceStart(startInclusive int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a Maybe.\n")
}

func (m Maybe) SliceEnd(end int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a Maybe.\n")
}

func (m Maybe) Slice(startInc int, endExc int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a Maybe.\n")
}

func (m Maybe) ToJson() string {
	if m.obj == nil {
		return "null"
	}
	return m.obj.ToJson()
}

func (m Maybe) ToString() string {
	if m.obj == nil {
		return "None"
	}
	return fmt.Sprintf("Just(%s)", m.obj.ToString())
}

func (m Maybe) IndexErrStr() string {
	return ""
}

func (m Maybe) Concat(other MShellObject) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot concatenate a Maybe.\n")
}

func (m Maybe) Equals(other MShellObject) (bool, error) {
	otherMaybe, ok := other.(Maybe)
	if !ok {
		return false, nil
	}

	if m.obj == nil && otherMaybe.obj == nil {
		return true, nil
	}

	if m.obj == nil || otherMaybe.obj == nil {
		return false, nil
	}

	equal, err := m.obj.Equals(otherMaybe.obj)
	return equal, err
}

func (m Maybe) CastString() (string, error) {
	return "", fmt.Errorf("Cannot cast a Maybe to a string.\n")
}

// }}}

// Date time {{{

type MShellDateTime struct {
	// TODO: replace with my own simpler int64 based on the Calendrical Calculation book.
	Time           time.Time
	OriginalString string
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
	return obj.ToString()
}

func (obj *MShellDateTime) DebugString() string {
	if len(obj.OriginalString) > 0 {
		return obj.OriginalString
	} else {
		return obj.Iso8601()
	}
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
	return fmt.Sprintf("\"%s\"", obj.Iso8601())
}

func (obj *MShellDateTime) ToString() string {
	if len(obj.OriginalString) > 0 {
		return obj.OriginalString
	} else {
		return obj.Iso8601()
	}
}

func (obj *MShellDateTime) Iso8601() string {
	year, month, day := obj.Time.Date()
	hour, min, sec := obj.Time.Clock()
	return fmt.Sprintf("%d-%02d-%02dT%02d:%02d:%02d", year, month, day, hour, min, sec)
}

func (obj *MShellDateTime) IndexErrStr() string {
	return ""
}

func (obj *MShellDateTime) Concat(other MShellObject) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot concatenate a DateTime.\n")
}

func (obj *MShellDateTime) Equals(other MShellObject) (bool, error) {
	asDateTime, ok := other.(*MShellDateTime)
	if !ok {
		return false, nil
	}

	return obj.Time.Equal(asDateTime.Time), nil
}

func (obj *MShellDateTime) CastString() (string, error) {
	return "", fmt.Errorf("Cannot cast a DateTime to a string.\n")
}

// }}}

// MShellDict {{{
type MShellDict struct {
	Items map[string]MShellObject
}

func NewDict() *MShellDict {
	return &MShellDict{
		Items: make(map[string]MShellObject),
	}
}

func (*MShellDict) TypeName() string {
	return "Dictionary"
}
func (*MShellDict) IsCommandLineable() bool {
	return false
}
func (*MShellDict) IsNumeric() bool {
	return false
}
func (*MShellDict) FloatNumeric() float64 {
	return 0
}
func (*MShellDict) CommandLine() string {
	return ""
}

// This is meant for things like error messages, should be limited in length to 30 chars or so.
func (d *MShellDict) DebugString() string {
	// TODO: implement this

	sb := strings.Builder{}
	sb.WriteString("Dictionary{")
	for key, value := range d.Items {
		sb.WriteString(fmt.Sprintf("%s: %s, ", key, value.DebugString()))
	}
	sb.WriteString("}")
	return sb.String()

}
func (*MShellDict) Index(index int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot index into a dictionary.\n")
}

func (*MShellDict) SliceStart(startInclusive int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a dictionary.\n")
}
func (*MShellDict) SliceEnd(end int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a dictionary.\n")
}
func (*MShellDict) Slice(startInc int, endExc int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a dictionary.\n")
}
func (d *MShellDict) ToJson() string {
	var sb strings.Builder

	if len(d.Items) == 0 {
		return "{}"
	}

	if len(d.Items) == 1 {
		for key, value := range d.Items {
			keyEnc, _ := json.Marshal(key)
			return fmt.Sprintf("{%s: %s}", string(keyEnc), value.ToJson())
		}
	}

	keys := make([]string, 0, len(d.Items))
	for key := range d.Items {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	sb.WriteString("{")

	// Write the first key-value pair
	firstKey := keys[0]
	firstValue := d.Items[firstKey]

	firstKeyEnc, _ := json.Marshal(firstKey)
	sb.WriteString(fmt.Sprintf("%s: %s", string(firstKeyEnc), firstValue.ToJson()))

	for _, key := range keys[1:] {
		value := d.Items[key]
		keyEnc, _ := json.Marshal(key)
		sb.WriteString(fmt.Sprintf(", %s: %s", string(keyEnc), value.ToJson()))
	}

	sb.WriteString("}")

	return sb.String()
}

func (d *MShellDict) ToString() string { // This is what is used with 'str' command
	return d.ToJson()
}

func (*MShellDict) IndexErrStr() string {
	return ""
}

func (*MShellDict) Concat(other MShellObject) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot concatenate a dictionary.\n")
}

func (thisDict *MShellDict) Equals(other MShellObject) (bool, error) {
	thisKeys := make([]string, 0, len(thisDict.Items))
	for key := range thisDict.Items {
		thisKeys = append(thisKeys, key)
	}
	sort.Strings(thisKeys)

	otherDict, ok := other.(*MShellDict)
	if !ok {
		return false, nil
	}

	otherKeys := make([]string, 0, len(otherDict.Items))
	for key := range otherDict.Items {
		otherKeys = append(otherKeys, key)
	}
	sort.Strings(otherKeys)

	if len(thisKeys) != len(otherKeys) {
		return false, nil
	}

	for i, key := range thisKeys {
		if key != otherKeys[i] {
			return false, nil
		}
	}

	for _, key := range thisKeys {
		thisValue := thisDict.Items[key]
		otherValue := otherDict.Items[key]

		if thisValue.TypeName() != otherValue.TypeName() {
			return false, nil
		}

		equal, err := thisValue.Equals(otherValue)
		if err != nil {
			return false, err
		}
		if !equal {
			return false, nil
		}
	}

	return true, nil
}

// This is meant for completely unambiougous conversion to a string value.
func (*MShellDict) CastString() (string, error) {
	return "", fmt.Errorf("Cannot cast a dictionary to a string.\n")
}

// }}}

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
	StandardInputBinary   []byte
	StandardInputFile     string
	StandardOutputFile    string
	AppendOutput          bool
	StandardErrorFile     string
	AppendError           bool
	Variables             map[string]MShellObject
}

func (q *MShellQuotation) GetStartToken() Token {
	return q.MShellParseQuote.StartToken
}

func (q *MShellQuotation) GetEndToken() Token {
	return q.MShellParseQuote.EndToken
}

// This function expects the caller to be the one to close the return context.
func (q *MShellQuotation) BuildExecutionContext(context *ExecuteContext) (*ExecuteContext, error) {
	quoteContext := ExecuteContext{
		StandardInput:     nil,
		StandardOutput:    nil,
		StandardError:     nil,
		Variables:         q.Variables,
		ShouldCloseInput:  false,
		ShouldCloseOutput: false,
		ShouldCloseError:  false,
		Pbm:               context.Pbm,
	}

	// Check for same-path stdout/stderr redirection
	samePath := q.StandardOutputFile != "" && q.StandardOutputFile == q.StandardErrorFile
	if samePath && q.AppendOutput != q.AppendError {
		t := q.GetStartToken()
		return nil, fmt.Errorf("%d:%d: Cannot redirect stdout and stderr to the same file '%s' with different append modes.\n", t.Line, t.Column, q.StandardOutputFile)
	}

	if q.StdinBehavior != STDIN_NONE {
		if q.StdinBehavior == STDIN_CONTENT {
			quoteContext.StandardInput = strings.NewReader(q.StandardInputContents)
		} else if q.StdinBehavior == STDIN_BINARY {
			quoteContext.StandardInput = bytes.NewReader(q.StandardInputBinary)
		} else if q.StdinBehavior == STDIN_FILE {
			file, err := os.Open(q.StandardInputFile)
			if err != nil {
				t := q.GetStartToken()
				return nil, fmt.Errorf("%d:%d: Error opening file %s for reading: %s\n", t.Line, t.Column, q.StandardInputFile, err.Error())
			}
			quoteContext.StandardInput = file
			quoteContext.ShouldCloseInput = true
		}
	} else if context.StandardInput != nil {
		quoteContext.StandardInput = context.StandardInput
	} else {
		// Default to stdin of this process itself
		quoteContext.StandardInput = os.Stdin
	}

	// Track shared output file for same-path case
	var sharedOutputFile *os.File

	if q.StandardOutputFile != "" {
		var file *os.File
		var err error
		if q.AppendOutput {
			file, err = os.OpenFile(q.StandardOutputFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
		} else {
			file, err = os.Create(q.StandardOutputFile)
		}
		if err != nil {
			t := q.GetStartToken()
			return nil, fmt.Errorf("%d:%d: Error opening file %s for writing: %s\n", t.Line, t.Column, q.StandardOutputFile, err.Error())
		}
		quoteContext.StandardOutput = file
		quoteContext.ShouldCloseOutput = true
		if samePath {
			sharedOutputFile = file
		}
	} else if context.StandardOutput != nil {
		quoteContext.StandardOutput = context.StandardOutput
	} else {
		// Default to stdout of this process itself
		quoteContext.StandardOutput = os.Stdout
	}

	// Handle stderr redirection
	if sharedOutputFile != nil {
		// Use the same file descriptor as stdout
		quoteContext.StandardError = sharedOutputFile
		// Don't set ShouldCloseError since stdout will close it
	} else if q.StandardErrorFile != "" {
		var file *os.File
		var err error
		if q.AppendError {
			file, err = os.OpenFile(q.StandardErrorFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
		} else {
			file, err = os.Create(q.StandardErrorFile)
		}
		if err != nil {
			t := q.GetStartToken()
			return nil, fmt.Errorf("%d:%d: Error opening file %s for writing: %s\n", t.Line, t.Column, q.StandardErrorFile, err.Error())
		}
		quoteContext.StandardError = file
		quoteContext.ShouldCloseError = true
	} else if context.StandardError != nil {
		quoteContext.StandardError = context.StandardError
	} else {
		// Default to stderr of this process itself
		quoteContext.StandardError = os.Stderr
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
	STDOUT_BINARY
)

type StdinBehavior int

const (
	STDIN_NONE StdinBehavior = iota
	STDIN_FILE
	STDIN_CONTENT
	STDIN_BINARY
)

type StderrBehavior int

const (
	STDERR_NONE StderrBehavior = iota
	STDERR_LINES
	STDERR_STRIPPED
	STDERR_COMPLETE
	STDERR_BINARY
)

type MShellList struct {
	Items                 []MShellObject
	StdinBehavior         StdinBehavior
	StandardInputContents string
	StandardInputBinary   []byte
	StandardInputFile     string
	StandardOutputFile    string
	AppendOutput          bool
	StandardErrorFile     string
	AppendError           bool
	// This sets how stdout is handled, whether it's broken up into lines, stripped of trailing newline, or left as is
	StdoutBehavior  StdoutBehavior
	StderrBehavior  StderrBehavior
	RunInBackground bool
	InPlaceFile     string // File path for in-place modification with <>
}

// initLength creates list like: make([]MShellObject, initLength)
// meaning the list would normally then be populated using
// list[n] = value
// not
// append(list, ..)
func NewList(initLength int) *MShellList {
	if initLength < 0 {
		// panic
		panic("Cannot create a list with a negative length.")
	}
	return &MShellList{
		Items:                 make([]MShellObject, initLength),
		StdinBehavior:         STDIN_NONE,
		StandardInputContents: "",
		StandardInputBinary:   nil,
		StandardInputFile:     "",
		StandardOutputFile:    "",
		AppendOutput:          false,
		StandardErrorFile:     "",
		AppendError:           false,
		StdoutBehavior:        STDOUT_NONE,
		StderrBehavior:        STDERR_NONE,
		RunInBackground:       false,
		InPlaceFile:           "",
	}
}

// Sort the list. Returns an error if any item cannot be cast to a string.
func SortList(list *MShellList) (*MShellList, error) {
	stringsToSort := make([]string, len(list.Items))
	for i, item := range list.Items {
		str, err := item.CastString()
		if err != nil {
			return nil, fmt.Errorf("Cannot sort a list with a %s inside (%s).\n", item.TypeName(), item.DebugString())
		}
		stringsToSort[i] = str
	}

	// Sort the strings
	sort.Strings(stringsToSort)

	// Create a new list and add the sorted strings to it
	newList := NewList(0)
	for _, str := range stringsToSort {
		newList.Items = append(newList.Items, MShellString{str})
	}
	CopyListParams(list, newList)
	return newList, nil
}

// Sort the list. Returns an error if any item cannot be cast to a string.
func SortListFunc(list *MShellList, cmp func(a string, b string) int) (*MShellList, error) {
	stringsToSort := make([]string, len(list.Items))
	for i, item := range list.Items {
		str, err := item.CastString()
		if err != nil {
			return nil, fmt.Errorf("Cannot sort a list with a %s inside (%s).\n", item.TypeName(), item.DebugString())
		}
		stringsToSort[i] = str
	}

	// Sort the strings to function
	slices.SortFunc(stringsToSort, cmp)

	// Create a new list and add the sorted strings to it
	newList := NewList(0)
	for _, str := range stringsToSort {
		newList.Items = append(newList.Items, MShellString{str})
	}
	CopyListParams(list, newList)
	return newList, nil
}

func CopyListParams(copyFromList *MShellList, copyToList *MShellList) {
	copyToList.StdinBehavior = copyFromList.StdinBehavior
	copyToList.StandardInputContents = copyFromList.StandardInputContents
	copyToList.StandardInputBinary = copyFromList.StandardInputBinary
	copyToList.StandardInputFile = copyFromList.StandardInputFile
	copyToList.StandardOutputFile = copyFromList.StandardOutputFile
	copyToList.StandardErrorFile = copyFromList.StandardErrorFile
	copyToList.AppendError = copyFromList.AppendError
	copyToList.StdoutBehavior = copyFromList.StdoutBehavior
	copyToList.StderrBehavior = copyFromList.StderrBehavior
	copyToList.InPlaceFile = copyFromList.InPlaceFile
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
	StderrBehavior StderrBehavior
}

type MShellInt struct {
	Value int
}

type MShellFloat struct {
	Value float64
}

// ToString
func (obj MShellLiteral) ToString() string {
	return obj.LiteralText
}

func (obj MShellBool) ToString() string {
	return strconv.FormatBool(obj.Value)
}

func (obj *MShellQuotation) ToString() string {
	return obj.DebugString()
}

func (obj *MShellList) ToString() string {
	return obj.DebugString()
}

func (obj MShellString) ToString() string {
	return obj.Content
}

func (obj MShellPath) ToString() string {
	return obj.Path
}

func (obj *MShellPipe) ToString() string {
	return obj.DebugString()
}

func (obj MShellInt) ToString() string {
	return strconv.Itoa(obj.Value)
}

func (obj MShellFloat) ToString() string {
	return strconv.FormatFloat(obj.Value, 'f', -1, 64)
}

func (obj *MShellSimple) ToString() string {
	return obj.Token.Lexeme
}

// TypeNames
func (obj MShellLiteral) TypeName() string {
	return "Literal"
}

func (obj MShellBool) TypeName() string {
	return "Boolean"
}

func (obj *MShellQuotation) TypeName() string {
	return "Quotation"
}

func (obj *MShellList) TypeName() string {
	return "List"
}

func (obj MShellString) TypeName() string {
	return "String"
}

func (obj MShellPath) TypeName() string {
	return "Path"
}

func (obj *MShellPipe) TypeName() string {
	return "Pipe"
}

func (obj MShellInt) TypeName() string {
	return "Integer"
}

func (obj MShellFloat) TypeName() string {
	return "Float"
}

func (obj *MShellSimple) TypeName() string {
	return obj.Token.Type.String()
}

// IsCommandLineable

func (obj MShellLiteral) IsCommandLineable() bool {
	return true
}

func (obj MShellBool) IsCommandLineable() bool {
	return false
}

func (obj *MShellQuotation) IsCommandLineable() bool {
	return false
}

func (obj *MShellList) IsCommandLineable() bool {
	return false
}

func (obj MShellString) IsCommandLineable() bool {
	return true
}

func (obj MShellPath) IsCommandLineable() bool {
	return true
}

func (obj *MShellPipe) IsCommandLineable() bool {
	return false
}

func (obj MShellInt) IsCommandLineable() bool {
	return true
}

func (obj *MShellSimple) IsCommandLineable() bool {
	return false
}

func (obj MShellFloat) IsCommandLineable() bool {
	return true
}

// IsNumeric
func (obj MShellLiteral) IsNumeric() bool {
	return false
}

func (obj MShellBool) IsNumeric() bool {
	return false
}

func (obj *MShellQuotation) IsNumeric() bool {
	return false
}

func (obj *MShellList) IsNumeric() bool {
	return false
}

func (obj MShellString) IsNumeric() bool {
	return false
}

func (obj MShellPath) IsNumeric() bool {
	return false
}

func (obj *MShellPipe) IsNumeric() bool {
	return false
}

func (obj MShellInt) IsNumeric() bool {
	return true
}

func (obj MShellFloat) IsNumeric() bool {
	return true
}

func (obj *MShellSimple) IsNumeric() bool {
	return false
}

// FloatNumeric
func (obj MShellLiteral) FloatNumeric() float64 {
	return 0
}

func (obj MShellBool) FloatNumeric() float64 {
	return 0
}

func (obj *MShellQuotation) FloatNumeric() float64 {
	return 0
}

func (obj *MShellList) FloatNumeric() float64 {
	return 0
}

func (obj MShellString) FloatNumeric() float64 {
	return 0
}

func (obj MShellPath) FloatNumeric() float64 {
	return 0
}

func (obj *MShellPipe) FloatNumeric() float64 {
	return 0
}

func (obj MShellInt) FloatNumeric() float64 {
	return float64(obj.Value)
}

func (obj MShellFloat) FloatNumeric() float64 {
	return obj.Value
}

func (obj *MShellSimple) FloatNumeric() float64 {
	return 0
}

// CommandLine
func (obj MShellLiteral) CommandLine() string {
	return obj.LiteralText
}

func (obj MShellBool) CommandLine() string {
	return ""
}

func (obj *MShellQuotation) CommandLine() string {
	return ""
}

func (obj *MShellList) CommandLine() string {
	return ""
}

func (obj MShellString) CommandLine() string {
	return obj.Content
}

func (obj MShellPath) CommandLine() string {
	return obj.Path
}

func (obj *MShellPipe) CommandLine() string {
	return ""
}

func (obj MShellInt) CommandLine() string {
	return strconv.Itoa(obj.Value)
}

func (obj *MShellSimple) CommandLine() string {
	return ""
}

func (obj MShellFloat) CommandLine() string {
	return strconv.FormatFloat(obj.Value, 'f', -1, 64)
}

// DebugString
func DebugStrs(objs []MShellObject) []string {
	debugStrs := make([]string, len(objs))
	for i, obj := range objs {
		if obj == nil {
			debugStrs[i] = "nil"
		} else {
			debugStrs[i] = obj.DebugString()
		}
	}
	return debugStrs
}

func (obj MShellLiteral) DebugString() string {
	return obj.LiteralText
}

func (obj MShellBool) DebugString() string {
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
	} else if obj.StdinBehavior == STDIN_BINARY {
		message += fmt.Sprintf(" < <binary %d bytes>", len(obj.StandardInputBinary))
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

func cleanStringForTerminal(input string) string {
	var builder strings.Builder
	builder.Grow(len(input) * 2) // Allocate efficiently

	length := len(input)
	for i := 0; i < length; i++ {
		if input[i] == '\r' {
			// If "\r\n" (Windows line ending), consume both
			if i+1 < length && input[i+1] == '\n' {
				i++ // Skip the '\n'
			}
			builder.WriteRune('↵') // Replace with ↵
		} else if input[i] == '\n' {
			builder.WriteRune('↵') // Replace standalone '\n' with ↵
		} else {
			builder.WriteByte(input[i]) // Keep other characters unchanged
		}
	}

	return builder.String()
}

var newlineCharRegex = regexp.MustCompile(`\r|\n`)

func (obj MShellString) DebugString() string {
	// Surround the string with double quotes, keep just the first 15 and last 15 characters
	if len(obj.Content) > 30 {
		return "\"" + cleanStringForTerminal(obj.Content[:15]) + "..." + cleanStringForTerminal(obj.Content[len(obj.Content)-15:]) + "\""
	}

	return "\"" + cleanStringForTerminal(obj.Content) + "\""
}

func (obj MShellPath) DebugString() string {
	if len(obj.Path) > 30 {
		return "`" + obj.Path[:15] + "..." + obj.Path[len(obj.Path)-15:] + "`"
	}

	return "`" + obj.Path + "`"
}

func (obj *MShellPipe) DebugString() string {
	// Join each item with a ' | '
	return strings.Join(DebugStrs(obj.List.Items), " | ")
}

func (obj MShellInt) DebugString() string {
	return strconv.Itoa(obj.Value)
}

func (obj MShellFloat) DebugString() string {
	return strconv.FormatFloat(obj.Value, 'f', -1, 64)
}

func (obj *MShellSimple) DebugString() string {
	return obj.Token.Lexeme
}

func (obj MShellLiteral) IndexErrStr() string {
	return fmt.Sprintf(" (%s)", obj.LiteralText)
}

func (obj MShellBool) IndexErrStr() string {
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

func (obj MShellString) IndexErrStr() string {
	return fmt.Sprintf(" '%s'", obj.Content)
}

func (obj MShellPath) IndexErrStr() string {
	return fmt.Sprintf(" `%s`", obj.Path)
}

func (obj *MShellPipe) IndexErrStr() string {
	if len(obj.List.Items) == 0 {
		return ""
	}
	return fmt.Sprintf(" Last item: %s", obj.List.Items[len(obj.List.Items)-1].DebugString())
}

func (obj MShellInt) IndexErrStr() string {
	return ""
}

func (obj MShellFloat) IndexErrStr() string {
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
func (obj MShellLiteral) Index(index int) (MShellObject, error) {
	if index < 0 {
		index = len(obj.LiteralText) + index
	}

	if err := IndexCheck(index, len(obj.LiteralText), obj); err != nil {
		return nil, err
	}
	return MShellLiteral{LiteralText: string(obj.LiteralText[index])}, nil
}

func (obj MShellBool) Index(index int) (MShellObject, error) {
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

func (obj MShellString) Index(index int) (MShellObject, error) {
	if index < 0 {
		index = len(obj.Content) + index
	}

	if err := IndexCheck(index, len(obj.Content), obj); err != nil {
		return nil, err
	}
	return MShellString{Content: string(obj.Content[index])}, nil
}

func (obj MShellPath) Index(index int) (MShellObject, error) {
	if index < 0 {
		index = len(obj.Path) + index
	}

	if err := IndexCheck(index, len(obj.Path), obj); err != nil {
		return nil, err
	}
	return MShellPath{Path: string(obj.Path[index])}, nil
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

func (obj MShellInt) Index(index int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot index into an integer.\n")
}

func (obj MShellFloat) Index(index int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot index into a float.\n")
}

func (obj *MShellSimple) Index(index int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot index into a simple token.\n")
}

// SliceStart
func (obj MShellLiteral) SliceStart(start int) (MShellObject, error) {
	if start < 0 {
		start = len(obj.LiteralText) + start
	}

	if err := IndexCheckExc(start, len(obj.LiteralText), obj); err != nil {
		return nil, err
	}
	return MShellLiteral{LiteralText: obj.LiteralText[start:]}, nil
}

func (obj MShellBool) SliceStart(start int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a boolean.\n")
}

func (obj *MShellQuotation) SliceStart(start int) (MShellObject, error) {
	if start < 0 {
		start = len(obj.Tokens) + start
	}
	if err := IndexCheckExc(start, len(obj.Tokens), obj); err != nil {
		return nil, err
	}
	return &MShellQuotation{Tokens: obj.Tokens[start:]}, nil
}

func (obj *MShellList) SliceStart(start int) (MShellObject, error) {
	if start < 0 {
		start = len(obj.Items) + start
	}
	if err := IndexCheckExc(start, len(obj.Items), obj); err != nil {
		return nil, err
	}

	n := len(obj.Items) - start
	newList := NewList(n)
	for i := range n {
		newList.Items[i] = obj.Items[start+i]
	}
	return newList, nil
}

func (obj MShellString) SliceStart(start int) (MShellObject, error) {
	if start < 0 {
		start = len(obj.Content) + start
	}
	if err := IndexCheckExc(start, len(obj.Content), obj); err != nil {
		return nil, err
	}
	return MShellString{Content: obj.Content[start:]}, nil
}

func (obj MShellPath) SliceStart(start int) (MShellObject, error) {
	if start < 0 {
		start = len(obj.Path) + start
	}
	if err := IndexCheckExc(start, len(obj.Path), obj); err != nil {
		return nil, err
	}

	return MShellPath{Path: obj.Path[start:]}, nil
}

func (obj *MShellPipe) SliceStart(start int) (MShellObject, error) {
	if start < 0 {
		start = len(obj.List.Items) + start
	}

	if err := IndexCheckExc(start, len(obj.List.Items), obj); err != nil {
		return nil, err
	}

	newList := NewList(0)
	newList.Items = obj.List.Items[start:]
	return newList, nil
}

func (obj MShellInt) SliceStart(start int) (MShellObject, error) {
	return nil, fmt.Errorf("cannot slice an integer.\n")
}

func (obj MShellFloat) SliceStart(start int) (MShellObject, error) {
	return nil, fmt.Errorf("cannot slice a float.\n")
}

func (obj *MShellSimple) SliceStart(start int) (MShellObject, error) {
	return nil, fmt.Errorf("cannot slice a simple token.\n")
}

// SliceEnd
func (obj MShellLiteral) SliceEnd(end int) (MShellObject, error) {
	if end < 0 {
		end = len(obj.LiteralText) + end
	}

	if err := IndexCheckExc(end, len(obj.LiteralText), obj); err != nil {
		return nil, err
	}
	return MShellLiteral{LiteralText: obj.LiteralText[:end]}, nil
}

func (obj MShellBool) SliceEnd(end int) (MShellObject, error) {
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
	n := end
	newList := NewList(n)
	for i := range n {
		newList.Items[i] = obj.Items[i]
	}
	return newList, nil
}

func (obj MShellString) SliceEnd(end int) (MShellObject, error) {
	if end < 0 {
		end = len(obj.Content) + end
	}
	if err := IndexCheckExc(end, len(obj.Content), obj); err != nil {
		return nil, err
	}
	return MShellString{Content: obj.Content[:end]}, nil
}

func (obj MShellPath) SliceEnd(end int) (MShellObject, error) {
	if end < 0 {
		end = len(obj.Path) + end
	}
	if err := IndexCheckExc(end, len(obj.Path), obj); err != nil {
		return nil, err
	}

	return MShellPath{Path: obj.Path[:end]}, nil
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

func (obj MShellInt) SliceEnd(end int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice an integer.\n")
}

func (obj MShellFloat) SliceEnd(end int) (MShellObject, error) {
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

func (obj MShellLiteral) Slice(startInc int, endExc int) (MShellObject, error) {
	if startInc < 0 {
		startInc = len(obj.LiteralText) + startInc
	}

	if endExc < 0 {
		endExc = len(obj.LiteralText) + endExc
	}

	if err := SliceIndexCheck(startInc, endExc, len(obj.LiteralText), obj); err != nil {
		return nil, err
	}
	return MShellLiteral{LiteralText: obj.LiteralText[startInc:endExc]}, nil
}

func (obj MShellBool) Slice(startInc int, endExc int) (MShellObject, error) {
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

	newList := NewList(endExc - startInc)
	for i := range endExc - startInc {
		newList.Items[i] = obj.Items[startInc+i]
	}
	return newList, nil
}

func (obj MShellString) Slice(startInc int, endExc int) (MShellObject, error) {
	if startInc < 0 {
		startInc = len(obj.Content) + startInc
	}

	if endExc < 0 {
		endExc = len(obj.Content) + endExc
	}

	if err := SliceIndexCheck(startInc, endExc, len(obj.Content), obj); err != nil {
		return nil, err
	}
	return MShellString{Content: obj.Content[startInc:endExc]}, nil
}

func (obj MShellPath) Slice(startInc int, endExc int) (MShellObject, error) {
	if startInc < 0 {
		startInc = len(obj.Path) + startInc
	}

	if endExc < 0 {
		endExc = len(obj.Path) + endExc
	}

	if err := SliceIndexCheck(startInc, endExc, len(obj.Path), obj); err != nil {
		return nil, err
	}

	return MShellPath{Path: obj.Path[startInc:endExc]}, nil
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

func (obj MShellInt) Slice(startInc int, endExc int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice an integer.\n")
}

func (obj MShellFloat) Slice(startInc int, endExc int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a float.\n")
}

func (obj *MShellSimple) Slice(startInc int, endExc int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a simple token.\n")
}

// ToJson
func (obj MShellLiteral) ToJson() string {
	escBytes, _ := json.Marshal(obj.LiteralText)
	return fmt.Sprintf("%s", string(escBytes))
}

func (obj MShellBool) ToJson() string {
	if obj.Value {
		return "true"
	} else {
		return "false"
	}
}

func (obj *MShellQuotation) ToJson() string {
	builder := strings.Builder{}
	builder.WriteString("[")
	if len(obj.Tokens) > 0 {
		builder.WriteString(obj.Tokens[0].ToJson())
		for _, token := range obj.Tokens[1:] {
			builder.WriteString(", ")
			builder.WriteString(token.ToJson())
		}
	}
	builder.WriteString("]")
	return builder.String()
}

func (obj *MShellList) ToJson() string {
	builder := strings.Builder{}
	builder.WriteString("[")
	if len(obj.Items) > 0 {
		builder.WriteString(obj.Items[0].ToJson())
		for _, item := range obj.Items[1:] {
			builder.WriteString(", ")
			builder.WriteString(item.ToJson())
		}
	}
	builder.WriteString("]")
	return builder.String()
}

func (obj MShellString) ToJson() string {
	// Escape the content
	escBytes, _ := json.Marshal(obj.Content)
	return fmt.Sprintf("%s", string(escBytes))
}

func (obj MShellPath) ToJson() string {
	// Escape the content
	escBytes, _ := json.Marshal(obj.Path)
	return fmt.Sprintf("%s", string(escBytes))
}

func (obj *MShellPipe) ToJson() string {
	return obj.List.ToJson()
}

func (obj MShellInt) ToJson() string {
	return fmt.Sprintf("%d", obj.Value)
}

func (obj MShellFloat) ToJson() string {
	escBytes, _ := json.Marshal(obj.Value)
	return fmt.Sprintf("%s", string(escBytes))
}

func (obj *MShellSimple) ToJson() string {
	return obj.Token.ToJson()
}

// Concat
func (obj MShellLiteral) Concat(other MShellObject) (MShellObject, error) {
	asLiteral, ok := other.(MShellLiteral)
	if !ok {
		return nil, fmt.Errorf("Cannot concatenate a Literal with a %s.\n", other.TypeName())
	}

	return MShellLiteral{LiteralText: obj.LiteralText + asLiteral.LiteralText}, nil
}

func (obj MShellBool) Concat(other MShellObject) (MShellObject, error) {
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

func (obj MShellString) Concat(other MShellObject) (MShellObject, error) {
	asString, ok := other.(MShellString)
	if !ok {
		return nil, fmt.Errorf("Cannot concatenate a String with a %s.\n", other.TypeName())
	}

	return MShellString{Content: obj.Content + asString.Content}, nil
}

func (obj MShellPath) Concat(other MShellObject) (MShellObject, error) {
	asPath, ok := other.(MShellPath)
	if !ok {
		return nil, fmt.Errorf("Cannot concatenate a Path with a %s.\n", other.TypeName())
	}

	return MShellPath{Path: obj.Path + asPath.Path}, nil
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

func (obj MShellInt) Concat(other MShellObject) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot concatenate an integer.\n")
}

func (obj MShellFloat) Concat(other MShellObject) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot concatenate a float.\n")
}

func ParseRawString(inputString string) (string, error) {
	// Purpose of this function is to remove outer quotes, handle escape characters
	if len(inputString) < 2 {
		return "", fmt.Errorf("input string should have a minimum length of 2 for surrounding double quotes.\n")
	}

	allRunes := []rune(inputString)

	var b strings.Builder
	index := 1
	inEscape := false

	for index < len(allRunes)-1 {
		c := allRunes[index]

		if inEscape {
			switch c {
			case 'e':
				b.WriteRune('\033') // Escape character
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
			case 'e':
				b.WriteRune('\033')
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

// Equals {{{
// MShellLiteral struct {
// MShellBool struct {
// MShellQuotation struct {
// MShellList struct {
// MShellString struct {
// MShellPath struct {
// MShellPipe struct {
// MShellInt struct {
// MShellFloat struct {

func (obj MShellLiteral) Equals(other MShellObject) (bool, error) {
	// Define equality for other as string or as literal or path.
	switch o := other.(type) {
	case MShellLiteral:
		return obj.LiteralText == o.LiteralText, nil
	case MShellString:
		return obj.LiteralText == o.Content, nil
	case MShellPath:
		return obj.LiteralText == o.Path, nil
	default:
		return false, fmt.Errorf("Cannot compare a literal with a %s.\n", other.TypeName())
	}
}

func (obj MShellBool) Equals(other MShellObject) (bool, error) {
	asBool, ok := other.(MShellBool)
	if !ok {
		return false, fmt.Errorf("Cannot compare a boolean with a %s.\n", other.TypeName())
	}
	return obj.Value == asBool.Value, nil
}

func (obj *MShellQuotation) Equals(other MShellObject) (bool, error) {
	return false, fmt.Errorf("Equality currently not defined for quotations.\n")
}

func (obj *MShellList) Equals(other MShellObject) (bool, error) {
	return false, fmt.Errorf("Equality currently not defined for lists.\n")
}

func (obj MShellString) Equals(other MShellObject) (bool, error) {
	// Define equality for other as string or as literal.
	switch other.(type) {
	case MShellString:
		asString, _ := other.(MShellString)
		return obj.Content == asString.Content, nil
	case MShellLiteral:
		asLiteral, _ := other.(MShellLiteral)
		return obj.Content == asLiteral.LiteralText, nil
	default:
		return false, fmt.Errorf("Cannot compare a string with a %s.\n", other.TypeName())
	}
}

func (obj MShellPath) Equals(other MShellObject) (bool, error) {
	// Define equality for other as string or as literal.
	switch other.(type) {
	case MShellPath:
		asPath, _ := other.(MShellPath)
		return obj.Path == asPath.Path, nil
	case MShellLiteral:
		asLiteral, _ := other.(MShellLiteral)
		return obj.Path == asLiteral.LiteralText, nil
	default:
		return false, fmt.Errorf("Cannot compare a path with a %s.\n", other.TypeName())
	}
}

func (obj *MShellPipe) Equals(other MShellObject) (bool, error) {
	return false, fmt.Errorf("Equality currently not defined for pipes.\n")
}

func (obj MShellInt) Equals(other MShellObject) (bool, error) {
	asInt, ok := other.(MShellInt)
	if !ok {
		return false, fmt.Errorf("Cannot compare an integer with a %s.\n", other.TypeName())
	}
	return obj.Value == asInt.Value, nil
}

func (obj MShellFloat) Equals(other MShellObject) (bool, error) {
	asFloat, ok := other.(MShellFloat)
	if !ok {
		return false, fmt.Errorf("Cannot compare a float with a %s.\n", other.TypeName())
	}
	return obj.Value == asFloat.Value, nil
}

// }}}

// CastString {{{

func (obj MShellLiteral) CastString() (string, error) {
	return obj.LiteralText, nil
}

func (obj MShellBool) CastString() (string, error) {
	return "", fmt.Errorf("Cannot cast a boolean to a string.\n")
}

func (obj *MShellQuotation) CastString() (string, error) {
	return "", fmt.Errorf("Cannot cast a quotation to a string.\n")
}

func (obj *MShellList) CastString() (string, error) {
	return "", fmt.Errorf("Cannot cast a list to a string.\n")
}

func (obj MShellString) CastString() (string, error) {
	return obj.Content, nil
}

func (obj MShellPath) CastString() (string, error) {
	return obj.Path, nil
}

func (obj *MShellPipe) CastString() (string, error) {
	return "", fmt.Errorf("Cannot cast a pipe to a string.\n")
}

func (obj MShellInt) CastString() (string, error) {
	return strconv.Itoa(obj.Value), nil
}

func (obj MShellFloat) CastString() (string, error) {
	return "", fmt.Errorf("Cannot cast a float to a string.\n")
}

// }}}

type NodeDict struct {
	Tag      string            `json:"tag"`
	Attrs    map[string]string `json:"attrs"`
	Children []NodeDict        `json:"children"`
	Text     string            `json:"text"`
}

func nodeToDict(n *html.Node) *MShellDict {
	d := NewDict()
	attrDict := NewDict()
	childList := NewList(0)

	d.Items["attr"] = attrDict
	d.Items["tag"] = MShellString{Content: n.Data}
	d.Items["children"] = childList

	for _, attr := range n.Attr {
		attrDict.Items[attr.Key] = MShellString{Content: attr.Val}
	}

	textBuilder := strings.Builder{}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			textBuilder.WriteString(strings.TrimSpace(c.Data))
			// d.Text += strings.TrimSpace(c.Data)
		} else if c.Type == html.ElementNode {
			d.Items["children"].(*MShellList).Items = append(childList.Items, nodeToDict(c))
		}
	}

	d.Items["text"] = MShellString{Content: textBuilder.String()}
	return d
}

// Grid types {{{

// ColumnType represents the storage type of a column
type ColumnType int

const (
	COL_GENERIC ColumnType = iota
	COL_INT
	COL_FLOAT
	COL_STRING
	COL_DATETIME
)

// GridColumn - Supports typed storage with fallback
type GridColumn struct {
	Name         string
	Meta         *MShellDict      // Optional column metadata
	ColType      ColumnType
	IntData      []int64
	FloatData    []float64
	StringData   []string
	DateTimeData []time.Time
	GenericData  []MShellObject   // Fallback for mixed types
}

// NewGridColumn creates a new column with the given name and row count
func NewGridColumn(name string, rowCount int) *GridColumn {
	return &GridColumn{
		Name:        name,
		Meta:        nil,
		ColType:     COL_GENERIC,
		GenericData: make([]MShellObject, rowCount),
	}
}

// Get returns the value at the given row index
func (col *GridColumn) Get(index int) MShellObject {
	switch col.ColType {
	case COL_INT:
		return MShellInt{Value: int(col.IntData[index])}
	case COL_FLOAT:
		return MShellFloat{Value: col.FloatData[index]}
	case COL_STRING:
		return MShellString{Content: col.StringData[index]}
	case COL_DATETIME:
		return &MShellDateTime{Time: col.DateTimeData[index]}
	default:
		return col.GenericData[index]
	}
}

// Set sets the value at the given row index
func (col *GridColumn) Set(index int, value MShellObject) {
	switch col.ColType {
	case COL_INT:
		if intVal, ok := value.(MShellInt); ok {
			col.IntData[index] = int64(intVal.Value)
		}
	case COL_FLOAT:
		if floatVal, ok := value.(MShellFloat); ok {
			col.FloatData[index] = floatVal.Value
		}
	case COL_STRING:
		if strVal, ok := value.(MShellString); ok {
			col.StringData[index] = strVal.Content
		}
	case COL_DATETIME:
		if dtVal, ok := value.(*MShellDateTime); ok {
			col.DateTimeData[index] = dtVal.Time
		}
	default:
		col.GenericData[index] = value
	}
}

// Len returns the number of rows in the column
func (col *GridColumn) Len() int {
	switch col.ColType {
	case COL_INT:
		return len(col.IntData)
	case COL_FLOAT:
		return len(col.FloatData)
	case COL_STRING:
		return len(col.StringData)
	case COL_DATETIME:
		return len(col.DateTimeData)
	default:
		return len(col.GenericData)
	}
}

// MShellGrid - Columnar storage for maximum performance
type MShellGrid struct {
	Meta     *MShellDict           // Optional grid-level metadata (nil if none)
	Columns  []*GridColumn         // Columnar storage
	ColIndex map[string]int        // Fast column name -> index lookup
	RowCount int                   // Number of rows
}

// NewGrid creates a new empty grid
func NewGrid() *MShellGrid {
	return &MShellGrid{
		Meta:     nil,
		Columns:  make([]*GridColumn, 0),
		ColIndex: make(map[string]int),
		RowCount: 0,
	}
}

// AddColumn adds a column to the grid
func (g *MShellGrid) AddColumn(col *GridColumn) {
	g.ColIndex[col.Name] = len(g.Columns)
	g.Columns = append(g.Columns, col)
}

// GetColumn returns the column with the given name, or nil if not found
func (g *MShellGrid) GetColumn(name string) *GridColumn {
	if idx, ok := g.ColIndex[name]; ok {
		return g.Columns[idx]
	}
	return nil
}

// GetRow returns the row at the given index as a GridRow
func (g *MShellGrid) GetRow(index int) *MShellGridRow {
	return &MShellGridRow{Grid: g, RowIndex: index}
}

// MShellGrid MShellObject interface {{{

func (g *MShellGrid) TypeName() string {
	return "Grid"
}

func (g *MShellGrid) IsCommandLineable() bool {
	return false
}

func (g *MShellGrid) IsNumeric() bool {
	return false
}

func (g *MShellGrid) FloatNumeric() float64 {
	return 0
}

func (g *MShellGrid) CommandLine() string {
	return ""
}

func (g *MShellGrid) DebugString() string {
	colNames := make([]string, len(g.Columns))
	for i, col := range g.Columns {
		colNames[i] = col.Name
	}
	return fmt.Sprintf("Grid{cols: [%s], rows: %d}", strings.Join(colNames, ", "), g.RowCount)
}

func (g *MShellGrid) Index(index int) (MShellObject, error) {
	if index < 0 {
		index = g.RowCount + index
	}
	if index < 0 || index >= g.RowCount {
		return nil, fmt.Errorf("Index %d out of range for Grid with %d rows.\n", index, g.RowCount)
	}
	return g.GetRow(index), nil
}

func (g *MShellGrid) SliceStart(startInclusive int) (MShellObject, error) {
	if startInclusive < 0 {
		startInclusive = g.RowCount + startInclusive
	}
	if startInclusive < 0 || startInclusive > g.RowCount {
		return nil, fmt.Errorf("Start index %d out of range for Grid with %d rows.\n", startInclusive, g.RowCount)
	}
	indices := make([]int, g.RowCount-startInclusive)
	for i := range indices {
		indices[i] = startInclusive + i
	}
	return &MShellGridView{Source: g, Indices: indices}, nil
}

func (g *MShellGrid) SliceEnd(end int) (MShellObject, error) {
	if end < 0 {
		end = g.RowCount + end
	}
	if end < 0 || end > g.RowCount {
		return nil, fmt.Errorf("End index %d out of range for Grid with %d rows.\n", end, g.RowCount)
	}
	indices := make([]int, end)
	for i := range indices {
		indices[i] = i
	}
	return &MShellGridView{Source: g, Indices: indices}, nil
}

func (g *MShellGrid) Slice(startInc int, endExc int) (MShellObject, error) {
	if startInc < 0 {
		startInc = g.RowCount + startInc
	}
	if endExc < 0 {
		endExc = g.RowCount + endExc
	}
	if startInc < 0 || endExc > g.RowCount || startInc > endExc {
		return nil, fmt.Errorf("Slice [%d:%d) out of range for Grid with %d rows.\n", startInc, endExc, g.RowCount)
	}
	indices := make([]int, endExc-startInc)
	for i := range indices {
		indices[i] = startInc + i
	}
	return &MShellGridView{Source: g, Indices: indices}, nil
}

func (g *MShellGrid) ToJson() string {
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; i < g.RowCount; i++ {
		if i > 0 {
			sb.WriteString(", ")
		}
		row := g.GetRow(i)
		sb.WriteString(row.ToJson())
	}
	sb.WriteString("]")
	return sb.String()
}

func (g *MShellGrid) ToString() string {
	return g.DebugString()
}

func (g *MShellGrid) IndexErrStr() string {
	return fmt.Sprintf(" Grid with %d rows", g.RowCount)
}

func (g *MShellGrid) Concat(other MShellObject) (MShellObject, error) {
	otherGrid, ok := other.(*MShellGrid)
	if !ok {
		return nil, fmt.Errorf("Cannot concatenate a Grid with a %s.\n", other.TypeName())
	}
	// Check column compatibility
	if len(g.Columns) != len(otherGrid.Columns) {
		return nil, fmt.Errorf("Cannot concatenate grids with different column counts (%d vs %d).\n", len(g.Columns), len(otherGrid.Columns))
	}
	for i, col := range g.Columns {
		if col.Name != otherGrid.Columns[i].Name {
			return nil, fmt.Errorf("Cannot concatenate grids with different column names at index %d (%s vs %s).\n", i, col.Name, otherGrid.Columns[i].Name)
		}
	}

	// Create new grid with combined rows
	newGrid := NewGrid()
	newGrid.RowCount = g.RowCount + otherGrid.RowCount

	for i, col := range g.Columns {
		newCol := NewGridColumn(col.Name, newGrid.RowCount)
		if col.Meta != nil {
			newCol.Meta = col.Meta
		}
		// Copy data from first grid
		for j := 0; j < g.RowCount; j++ {
			newCol.GenericData[j] = col.Get(j)
		}
		// Copy data from second grid
		for j := 0; j < otherGrid.RowCount; j++ {
			newCol.GenericData[g.RowCount+j] = otherGrid.Columns[i].Get(j)
		}
		newGrid.AddColumn(newCol)
	}

	return newGrid, nil
}

func (g *MShellGrid) Equals(other MShellObject) (bool, error) {
	return false, fmt.Errorf("Equality currently not defined for grids.\n")
}

func (g *MShellGrid) CastString() (string, error) {
	return "", fmt.Errorf("Cannot cast a Grid to a string.\n")
}

// }}}

// MShellGridView - Filtered view without copying data
type MShellGridView struct {
	Source  *MShellGrid  // Reference to original grid
	Indices []int        // Row indices in the view
}

// GetRow returns the row at the given view index as a GridRow
func (v *MShellGridView) GetRow(index int) *MShellGridRow {
	return &MShellGridRow{Grid: v.Source, RowIndex: v.Indices[index]}
}

// MShellGridView MShellObject interface {{{

func (v *MShellGridView) TypeName() string {
	return "GridView"
}

func (v *MShellGridView) IsCommandLineable() bool {
	return false
}

func (v *MShellGridView) IsNumeric() bool {
	return false
}

func (v *MShellGridView) FloatNumeric() float64 {
	return 0
}

func (v *MShellGridView) CommandLine() string {
	return ""
}

func (v *MShellGridView) DebugString() string {
	return fmt.Sprintf("GridView{source: %s, rows: %d}", v.Source.DebugString(), len(v.Indices))
}

func (v *MShellGridView) Index(index int) (MShellObject, error) {
	if index < 0 {
		index = len(v.Indices) + index
	}
	if index < 0 || index >= len(v.Indices) {
		return nil, fmt.Errorf("Index %d out of range for GridView with %d rows.\n", index, len(v.Indices))
	}
	return v.GetRow(index), nil
}

func (v *MShellGridView) SliceStart(startInclusive int) (MShellObject, error) {
	if startInclusive < 0 {
		startInclusive = len(v.Indices) + startInclusive
	}
	if startInclusive < 0 || startInclusive > len(v.Indices) {
		return nil, fmt.Errorf("Start index %d out of range for GridView with %d rows.\n", startInclusive, len(v.Indices))
	}
	return &MShellGridView{Source: v.Source, Indices: v.Indices[startInclusive:]}, nil
}

func (v *MShellGridView) SliceEnd(end int) (MShellObject, error) {
	if end < 0 {
		end = len(v.Indices) + end
	}
	if end < 0 || end > len(v.Indices) {
		return nil, fmt.Errorf("End index %d out of range for GridView with %d rows.\n", end, len(v.Indices))
	}
	return &MShellGridView{Source: v.Source, Indices: v.Indices[:end]}, nil
}

func (v *MShellGridView) Slice(startInc int, endExc int) (MShellObject, error) {
	if startInc < 0 {
		startInc = len(v.Indices) + startInc
	}
	if endExc < 0 {
		endExc = len(v.Indices) + endExc
	}
	if startInc < 0 || endExc > len(v.Indices) || startInc > endExc {
		return nil, fmt.Errorf("Slice [%d:%d) out of range for GridView with %d rows.\n", startInc, endExc, len(v.Indices))
	}
	return &MShellGridView{Source: v.Source, Indices: v.Indices[startInc:endExc]}, nil
}

func (v *MShellGridView) ToJson() string {
	var sb strings.Builder
	sb.WriteString("[")
	for i, idx := range v.Indices {
		if i > 0 {
			sb.WriteString(", ")
		}
		row := &MShellGridRow{Grid: v.Source, RowIndex: idx}
		sb.WriteString(row.ToJson())
	}
	sb.WriteString("]")
	return sb.String()
}

func (v *MShellGridView) ToString() string {
	return v.DebugString()
}

func (v *MShellGridView) IndexErrStr() string {
	return fmt.Sprintf(" GridView with %d rows", len(v.Indices))
}

func (v *MShellGridView) Concat(other MShellObject) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot concatenate a GridView. Use compact first.\n")
}

func (v *MShellGridView) Equals(other MShellObject) (bool, error) {
	return false, fmt.Errorf("Equality currently not defined for grid views.\n")
}

func (v *MShellGridView) CastString() (string, error) {
	return "", fmt.Errorf("Cannot cast a GridView to a string.\n")
}

// }}}

// MShellGridRow - Lazy row view (does not allocate per-row objects)
type MShellGridRow struct {
	Grid     *MShellGrid  // Parent grid reference
	RowIndex int          // Row index within grid
}

// Get returns the value at the given column name
func (r *MShellGridRow) Get(colName string) (MShellObject, bool) {
	col := r.Grid.GetColumn(colName)
	if col == nil {
		return nil, false
	}
	return col.Get(r.RowIndex), true
}

// ToDict materializes the row to an actual MShellDict
func (r *MShellGridRow) ToDict() *MShellDict {
	d := NewDict()
	for _, col := range r.Grid.Columns {
		d.Items[col.Name] = col.Get(r.RowIndex)
	}
	return d
}

// MShellGridRow MShellObject interface {{{

func (r *MShellGridRow) TypeName() string {
	return "GridRow"
}

func (r *MShellGridRow) IsCommandLineable() bool {
	return false
}

func (r *MShellGridRow) IsNumeric() bool {
	return false
}

func (r *MShellGridRow) FloatNumeric() float64 {
	return 0
}

func (r *MShellGridRow) CommandLine() string {
	return ""
}

func (r *MShellGridRow) DebugString() string {
	d := r.ToDict()
	return fmt.Sprintf("GridRow%s", d.DebugString())
}

func (r *MShellGridRow) Index(index int) (MShellObject, error) {
	if index < 0 {
		index = len(r.Grid.Columns) + index
	}
	if index < 0 || index >= len(r.Grid.Columns) {
		return nil, fmt.Errorf("Index %d out of range for GridRow with %d columns.\n", index, len(r.Grid.Columns))
	}
	return r.Grid.Columns[index].Get(r.RowIndex), nil
}

func (r *MShellGridRow) SliceStart(startInclusive int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a GridRow.\n")
}

func (r *MShellGridRow) SliceEnd(end int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a GridRow.\n")
}

func (r *MShellGridRow) Slice(startInc int, endExc int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a GridRow.\n")
}

func (r *MShellGridRow) ToJson() string {
	var sb strings.Builder
	sb.WriteString("{")
	for i, col := range r.Grid.Columns {
		if i > 0 {
			sb.WriteString(", ")
		}
		keyEnc, _ := json.Marshal(col.Name)
		sb.WriteString(fmt.Sprintf("%s: %s", string(keyEnc), col.Get(r.RowIndex).ToJson()))
	}
	sb.WriteString("}")
	return sb.String()
}

func (r *MShellGridRow) ToString() string {
	return r.DebugString()
}

func (r *MShellGridRow) IndexErrStr() string {
	return ""
}

func (r *MShellGridRow) Concat(other MShellObject) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot concatenate a GridRow.\n")
}

func (r *MShellGridRow) Equals(other MShellObject) (bool, error) {
	return false, fmt.Errorf("Equality currently not defined for grid rows.\n")
}

func (r *MShellGridRow) CastString() (string, error) {
	return "", fmt.Errorf("Cannot cast a GridRow to a string.\n")
}

// }}}

// }}}
