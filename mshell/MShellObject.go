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
	return false, nil
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
	return renderValue(m, flavorDebug)
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
	return renderValue(m, flavorJson)
}

func (m Maybe) ToString() string {
	return renderValue(m, flavorStr)
}

func (m Maybe) IndexErrStr() string {
	return ""
}

func (m Maybe) Concat(other MShellObject) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot concatenate a Maybe.\n")
}

func (m Maybe) Equals(other MShellObject) (bool, error) {
	return equalsIter(m, other)
}

func (m Maybe) CastString() (string, error) {
	return "", fmt.Errorf("Cannot cast a Maybe to a string.\n")
}

// }}}

// Null {{{
// MShellNull is the JSON `null` value. It is deliberately distinct from a
// `none` (the empty case of the Maybe type): `null` models the JSON literal
// and the `null` arm of union types like `int | null`, while `none` is the
// absence value of Maybe[T]. JSON parsing produces MShellNull; serializing it
// back with toJson yields `null`.
type MShellNull struct{}

func (n MShellNull) TypeName() string {
	return "Null"
}

func (n MShellNull) IsCommandLineable() bool {
	return false
}

func (n MShellNull) IsNumeric() bool {
	return false
}

func (n MShellNull) FloatNumeric() float64 {
	return 0
}

func (n MShellNull) CommandLine() string {
	return ""
}

func (n MShellNull) DebugString() string {
	return "null"
}

func (n MShellNull) Index(index int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot index into a null.\n")
}

func (n MShellNull) SliceStart(startInclusive int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a null.\n")
}

func (n MShellNull) SliceEnd(end int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a null.\n")
}

func (n MShellNull) Slice(startInc int, endExc int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice a null.\n")
}

func (n MShellNull) ToJson() string {
	return "null"
}

func (n MShellNull) ToString() string {
	return "null"
}

func (n MShellNull) IndexErrStr() string {
	return ""
}

func (n MShellNull) Concat(other MShellObject) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot concatenate a null.\n")
}

func (n MShellNull) Equals(other MShellObject) (bool, error) {
	_, ok := other.(MShellNull)
	return ok, nil
}

func (n MShellNull) CastString() (string, error) {
	return "", fmt.Errorf("Cannot cast a null to a string.\n")
}

// }}}

// Enum {{{

// MShellEnum is a value of a user-declared `enum` (a generative tagged sum
// type): the enum's declared name, the chosen member, and the member's payload
// values (nil for a nullary member). Member names are unique across enums, so
// the member identifies the value; the enum name rides along for diagnostics
// and `match`.
type MShellEnum struct {
	EnumName string
	Member   string
	// MemberIndex is the member's 0-based position in its enum declaration.
	// Sorting orders enum values by this (declaration order) rather than by
	// member name, so an ordered enum (`low | medium | high`) sorts in the
	// author's intended order. Stamped at construction from the enum registry.
	MemberIndex int
	Payload     []MShellObject
}

func (e *MShellEnum) TypeName() string       { return e.EnumName }
func (e *MShellEnum) IsCommandLineable() bool { return true }
func (e *MShellEnum) IsNumeric() bool         { return false }
func (e *MShellEnum) FloatNumeric() float64   { return 0 }
func (e *MShellEnum) CommandLine() string     { return renderValue(e, flavorStr) }
func (e *MShellEnum) DebugString() string     { return renderValue(e, flavorStr) }

func (e *MShellEnum) Index(index int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot index into an enum.\n")
}

func (e *MShellEnum) SliceStart(startInclusive int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice an enum.\n")
}

func (e *MShellEnum) SliceEnd(end int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice an enum.\n")
}

func (e *MShellEnum) Slice(startInc int, endExc int) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot slice an enum.\n")
}

// ToJson uses serde's externally-tagged convention — the de-facto standard for
// tagged unions in JSON: a nullary member is the bare member string; a member
// with a single payload is `{"member": value}`; with several, `{"member":
// [v0, v1, ...]}`. Rendering runs on renderValue's shared work stack, so an
// arbitrarily deep value cannot overflow the call stack.
func (e *MShellEnum) ToJson() string {
	return renderValue(e, flavorJson)
}

func (e *MShellEnum) ToString() string    { return renderValue(e, flavorStr) }
func (e *MShellEnum) IndexErrStr() string { return "" }

func (e *MShellEnum) Concat(other MShellObject) (MShellObject, error) {
	return nil, fmt.Errorf("Cannot concatenate an enum.\n")
}

func (e *MShellEnum) Equals(other MShellObject) (bool, error) {
	return equalsIter(e, other)
}

func (e *MShellEnum) CastString() (string, error) { return e.Member, nil }

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
	return renderValue(d, flavorDebug)
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
	return renderValue(d, flavorJson)
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
	return equalsIter(thisDict, other)
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
// valueTypeRank assigns each value kind a fixed slot in the cross-type sort
// order, so a list mixing types still sorts totally and deterministically. The
// exact sequence is arbitrary but stable; within a rank, compareValues uses the
// value's natural order. Text kinds (str/path/literal) share a rank and compare
// by content, matching structural equality.
func valueTypeRank(obj MShellObject) int {
	switch obj.(type) {
	case MShellNull:
		return 0
	case MShellBool:
		return 1
	case MShellInt, MShellFloat:
		return 2
	case MShellString, MShellPath, MShellLiteral:
		return 3
	case *MShellDateTime:
		return 4
	case MShellBinary:
		return 5
	case Maybe, *Maybe:
		return 6
	case *MShellList:
		return 7
	case *MShellDict:
		return 8
	case *MShellEnum:
		return 9
	default:
		return 10
	}
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func cmpFloat(a, b float64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// numericFloat returns an int/float value as a float64 for cross-type numeric
// comparison. Only called for MShellInt / MShellFloat.
func numericFloat(obj MShellObject) float64 {
	switch v := obj.(type) {
	case MShellInt:
		return float64(v.Value)
	case MShellFloat:
		return v.Value
	}
	return 0
}

// textContent returns the underlying string of a text-kind value
// (str / path / literal). Only called for those types.
func textContent(obj MShellObject) string {
	switch v := obj.(type) {
	case MShellString:
		return v.Content
	case MShellPath:
		return v.Path
	case MShellLiteral:
		return v.LiteralText
	}
	return ""
}

func asMaybe(obj MShellObject) (Maybe, bool) {
	switch v := obj.(type) {
	case Maybe:
		return v, true
	case *Maybe:
		return *v, true
	}
	return Maybe{}, false
}

func sortedDictKeys(m map[string]MShellObject) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sameRef reports whether a and b are the identical heap object, for the kinds
// that can form shared substructure (a value built as `@t @t node` reuses one
// subtree twice). A pointer-identical pair is equal by definition, so equality
// and ordering walks skip it instead of expanding it — without this, walking a
// value with n levels of sharing costs 2^n. Only pointer kinds are compared:
// comparing interfaces holding non-comparable dynamic types (e.g. MShellBinary,
// a []byte) panics at runtime.
func sameRef(a, b MShellObject) bool {
	switch av := a.(type) {
	case *MShellEnum:
		bv, ok := b.(*MShellEnum)
		return ok && av == bv
	case *MShellList:
		bv, ok := b.(*MShellList)
		return ok && av == bv
	case *MShellDict:
		bv, ok := b.(*MShellDict)
		return ok && av == bv
	case *Maybe:
		bv, ok := b.(*Maybe)
		return ok && av == bv
	case *MShellDateTime:
		bv, ok := b.(*MShellDateTime)
		return ok && av == bv
	case *MShellQuotation:
		bv, ok := b.(*MShellQuotation)
		return ok && av == bv
	}
	return false
}

// dagGuard bounds a comparison walk over values with shared substructure that
// sameRef alone cannot catch: two *independently built* DAGs share no pointers
// across operands, so every level re-expands and the walk goes exponential.
// The guard counts pops; once a walk runs long enough to suggest blowup, it
// memoizes the pointer pairs it has already expanded and skips repeats.
//
// Skipping a repeated pair is sound in a LIFO walk: the first occurrence's
// entire expansion resolves before any later duplicate (which sat lower in the
// stack) pops, and a mismatch anywhere returns from the walk immediately — so
// if a duplicate pops at all, its subtree already compared equal.
//
// Ordinary comparisons never allocate: below the step threshold the guard is
// one integer increment. The memo is capped so a legitimately huge linear
// value (millions of distinct pairs, no repeats) cannot balloon memory; a
// blowup DAG has few distinct pairs and fits far below the cap.
type dagGuard struct {
	steps int
	memo  map[refPair]bool
}

type refPair struct{ a, b MShellObject }

const dagStepThreshold = 1 << 19
const dagMemoCap = 1 << 18

// skip reports whether this pair was already expanded earlier in the walk.
// Call once per popped pair; it records the pair (past the threshold) so
// later duplicates skip.
func (g *dagGuard) skip(a, b MShellObject) bool {
	g.steps++
	if g.steps < dagStepThreshold {
		return false
	}
	key, ok := refPairKey(a, b)
	if !ok {
		return false
	}
	if g.memo == nil {
		g.memo = make(map[refPair]bool, 1024)
	}
	if g.memo[key] {
		return true
	}
	// Generational overflow: when the memo is full, clear it and keep
	// inserting rather than stopping (stopping would freeze the memo on the
	// walk's earliest pairs). Note the memo is NOT the defense against
	// self-doubling values — a walk whose pending-duplicate working set
	// exceeds the cap defeats any bounded memo (measured, not theorized).
	// That family is handled structurally by push-time pair dedup
	// (pushPairsDedup); the memo covers cross-parent duplicate pairs
	// (diamond-shaped sharing) up to the cap.
	if len(g.memo) >= dagMemoCap {
		g.memo = make(map[refPair]bool, 1024)
	}
	g.memo[key] = true
	return false
}

// refPairKey returns a comparable identity key when both values are the same
// container pointer kind — the kinds whose repeated pairs cause blowup.
// Interface keys are only safe when the dynamic values are comparable, which
// pointers are; scalar kinds are cheap to compare directly and get no key.
func refPairKey(a, b MShellObject) (refPair, bool) {
	switch a.(type) {
	case *MShellEnum:
		if _, ok := b.(*MShellEnum); ok {
			return refPair{a, b}, true
		}
	case *MShellList:
		if _, ok := b.(*MShellList); ok {
			return refPair{a, b}, true
		}
	case *MShellDict:
		if _, ok := b.(*MShellDict); ok {
			return refPair{a, b}, true
		}
	}
	return refPair{}, false
}

// renderFlavor selects which of a value's three textual forms renderValue
// emits: flavorStr is ToString (the `str` form), flavorDebug is DebugString
// (stack dumps, list display), flavorJson is ToJson. Containers pick their
// children's flavor the same way the per-type methods always did: a list
// renders children as DebugString, a dict's `str` form is its JSON form, an
// enum renders payloads with ToString, and Maybe keeps its own flavor.
type renderFlavor uint8

const (
	flavorStr renderFlavor = iota
	flavorDebug
	flavorJson
)

type renderTask struct {
	lit    string
	obj    MShellObject
	flavor renderFlavor
	isLit  bool
	// isExit marks the sentinel popped after a container's children have
	// rendered; it removes the container from the on-path cycle set.
	isExit bool
}

func renderLit(s string) renderTask { return renderTask{lit: s, isLit: true} }

// renderJoin builds the task sequence `open item0 sep item1 sep ... close`,
// rendering each item in the given flavor.
func renderJoin(open, sep, close string, items []MShellObject, flavor renderFlavor) []renderTask {
	seq := make([]renderTask, 0, len(items)*2+2)
	if open != "" {
		seq = append(seq, renderLit(open))
	}
	for i, it := range items {
		if i > 0 {
			seq = append(seq, renderLit(sep))
		}
		seq = append(seq, renderTask{obj: it, flavor: flavor})
	}
	if close != "" {
		seq = append(seq, renderLit(close))
	}
	return seq
}

// cycleTrackable reports whether obj is a heap container that could sit on a
// reference cycle (built via in-place list/dict mutation, e.g. a list appended
// to itself). Only pointer kinds qualify — value kinds are copied and cannot
// be revisited by identity.
func cycleTrackable(obj MShellObject) bool {
	switch obj.(type) {
	case *MShellEnum, *MShellList, *MShellDict, *Maybe, *MShellPipe:
		return true
	}
	return false
}

// renderValue renders a value in the requested flavor. It is total: a cyclic
// value renders with a `<cycle>` marker at the back-reference, which keeps
// internal rendering (error messages, stack dumps) from hanging. User-facing
// operations (`str`, `toJson`) call renderValueDetect instead and report a
// cyclic value as an error — mshell is strict, so a cycle is always the
// degenerate result of appending a container into itself, not a value with a
// meaningful rendering.
func renderValue(root MShellObject, flavor renderFlavor) string {
	s, _ := renderValueDetect(root, flavor)
	return s
}

// renderValueDetect renders a value in the requested flavor with one explicit
// work stack instead of method recursion, expanding every container kind —
// enum, Maybe, list, dict, pipe — inline. Arbitrarily deep values therefore
// cannot overflow the call stack even when kinds alternate (enum→Maybe→enum,
// ...), which per-type iterative renderers could not guarantee: each one
// delegated other kinds to the child's own recursive method. Leaf kinds
// (scalars, grids, quotations) still render via their own methods; their
// nesting depth is bounded by their own structure.
//
// Containers currently being expanded are tracked as an on-path set; reaching
// one again is a true reference cycle (a DAG merely revisits a finished
// pointer, which is fine), so the walk emits `<cycle>` instead of descending
// and reports cycled=true.
func renderValueDetect(root MShellObject, flavor renderFlavor) (string, bool) {
	var sb strings.Builder
	cycled := false
	var onPath map[MShellObject]bool
	stack := []renderTask{{obj: root, flavor: flavor}}
	// push schedules seq to pop in order (reversed onto the LIFO stack).
	push := func(seq []renderTask) {
		for i := len(seq) - 1; i >= 0; i-- {
			stack = append(stack, seq[i])
		}
	}
	// enter marks t.obj as on the current path and schedules its removal
	// after seq (the container's children) has fully rendered.
	enter := func(obj MShellObject, seq []renderTask) []renderTask {
		if !cycleTrackable(obj) {
			return seq
		}
		if onPath == nil {
			onPath = make(map[MShellObject]bool, 8)
		}
		onPath[obj] = true
		return append(seq, renderTask{obj: obj, isExit: true})
	}
	for len(stack) > 0 {
		t := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if t.isLit {
			sb.WriteString(t.lit)
			continue
		}
		if t.isExit {
			delete(onPath, t.obj)
			continue
		}
		// Only pointer kinds are ever on the path; the guard also keeps
		// unhashable dynamic types (MShellBinary, a []byte) away from the
		// map lookup, which would panic even on a read.
		if cycleTrackable(t.obj) && onPath[t.obj] {
			sb.WriteString("<cycle>")
			cycled = true
			continue
		}
		if m, ok := asMaybe(t.obj); ok {
			switch {
			case m.IsNone() && t.flavor == flavorJson:
				sb.WriteString("null")
			case m.IsNone():
				sb.WriteString("None")
			case t.flavor == flavorJson:
				push(enter(t.obj, []renderTask{{obj: m.obj, flavor: flavorJson}}))
			case t.flavor == flavorDebug:
				push(enter(t.obj, []renderTask{renderLit("Maybe("), {obj: m.obj, flavor: flavorDebug}, renderLit(")")}))
			default:
				push(enter(t.obj, []renderTask{renderLit("Just("), {obj: m.obj, flavor: flavorStr}, renderLit(")")}))
			}
			continue
		}
		switch v := t.obj.(type) {
		case *MShellEnum:
			if t.flavor == flavorJson {
				// serde's externally-tagged convention: a nullary member is
				// the bare member string, one payload is {"member": value},
				// several are {"member": [v0, v1, ...]}.
				if len(v.Payload) == 0 {
					fmt.Fprintf(&sb, "%q", v.Member)
					continue
				}
				seq := make([]renderTask, 0, len(v.Payload)*2+4)
				seq = append(seq, renderLit(fmt.Sprintf("{%q: ", v.Member)))
				if len(v.Payload) == 1 {
					seq = append(seq, renderTask{obj: v.Payload[0], flavor: flavorJson})
				} else {
					seq = append(seq, renderJoin("[", ", ", "]", v.Payload, flavorJson)...)
				}
				seq = append(seq, renderLit("}"))
				push(enter(t.obj, seq))
				continue
			}
			// `member` (nullary) or `member(p0 p1 ...)`, payloads as ToString.
			if len(v.Payload) == 0 {
				sb.WriteString(v.Member)
				continue
			}
			push(enter(t.obj, renderJoin(v.Member+"(", " ", ")", v.Payload, flavorStr)))
		case *MShellList:
			if t.flavor == flavorJson {
				push(enter(t.obj, renderJoin("[", ", ", "]", v.Items, flavorJson)))
			} else {
				push(enter(t.obj, renderJoin("[", " ", "]", v.Items, flavorDebug)))
			}
		case *MShellPipe:
			if t.flavor == flavorJson {
				push(enter(t.obj, renderJoin("[", ", ", "]", v.List.Items, flavorJson)))
			} else {
				push(enter(t.obj, renderJoin("", " | ", "", v.List.Items, flavorDebug)))
			}
		case *MShellDict:
			keys := sortedDictKeys(v.Items)
			if t.flavor == flavorDebug {
				seq := make([]renderTask, 0, len(keys)*3+2)
				seq = append(seq, renderLit("Dictionary{"))
				for _, k := range keys {
					seq = append(seq, renderLit(k+": "), renderTask{obj: v.Items[k], flavor: flavorDebug}, renderLit(", "))
				}
				seq = append(seq, renderLit("}"))
				push(enter(t.obj, seq))
				continue
			}
			// The `str` form of a dict is its JSON form.
			if len(keys) == 0 {
				sb.WriteString("{}")
				continue
			}
			seq := make([]renderTask, 0, len(keys)*2+2)
			seq = append(seq, renderLit("{"))
			for i, k := range keys {
				keyEnc, _ := json.Marshal(k)
				if i > 0 {
					seq = append(seq, renderLit(", "))
				}
				seq = append(seq, renderLit(string(keyEnc)+": "), renderTask{obj: v.Items[k], flavor: flavorJson})
			}
			seq = append(seq, renderLit("}"))
			push(enter(t.obj, seq))
		default:
			switch t.flavor {
			case flavorDebug:
				sb.WriteString(t.obj.DebugString())
			case flavorJson:
				sb.WriteString(t.obj.ToJson())
			default:
				sb.WriteString(t.obj.ToString())
			}
		}
	}
	return sb.String(), cycled
}

// equalsIter is structural equality over any two values, walked with one
// explicit pair stack that expands every container kind — enum, Maybe, list,
// dict, pipe — inline, so deep values cannot overflow the call stack even
// when kinds alternate. Pointer-identical pairs are skipped (equal by
// definition), and past a step threshold already-expanded pairs are memoized
// (see dagGuard), so shared substructure cannot blow up exponentially. Leaf
// kinds compare via their own Equals.
type eqPair struct{ a, b MShellObject }

// pushPairsDedup pushes element-wise comparison pairs, skipping a pair that is
// pointer-identical to the one just pushed. A self-doubling value
// (`@t @t node`, `[ @x @x ]`) expands to the SAME pair twice; pushing it once
// makes that whole family linear at any depth, with no reliance on the
// bounded dagGuard memo (whose eviction cannot cover a working set larger
// than its cap).
func pushPairsDedup(stack []eqPair, as, bs []MShellObject) []eqPair {
	var lastA, lastB MShellObject
	for i := range as {
		ca, cb := as[i], bs[i]
		if i > 0 && sameRef(ca, lastA) && sameRef(cb, lastB) {
			continue
		}
		lastA, lastB = ca, cb
		stack = append(stack, eqPair{a: ca, b: cb})
	}
	return stack
}

func equalsIter(a, b MShellObject) (bool, error) {
	var guard dagGuard
	stack := []eqPair{{a: a, b: b}}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if sameRef(p.a, p.b) || guard.skip(p.a, p.b) {
			continue
		}
		if am, aok := asMaybe(p.a); aok {
			bm, bok := asMaybe(p.b)
			if !bok || am.IsNone() != bm.IsNone() {
				return false, nil
			}
			if !am.IsNone() {
				stack = append(stack, eqPair{a: am.obj, b: bm.obj})
			}
			continue
		}
		switch av := p.a.(type) {
		case *MShellEnum:
			bv, ok := p.b.(*MShellEnum)
			if !ok || av.EnumName != bv.EnumName || av.Member != bv.Member || len(av.Payload) != len(bv.Payload) {
				return false, nil
			}
			stack = pushPairsDedup(stack, av.Payload, bv.Payload)
		case *MShellList:
			bv, ok := p.b.(*MShellList)
			if !ok || len(av.Items) != len(bv.Items) {
				return false, nil
			}
			stack = pushPairsDedup(stack, av.Items, bv.Items)
		case *MShellPipe:
			bv, ok := p.b.(*MShellPipe)
			if !ok || len(av.List.Items) != len(bv.List.Items) {
				return false, nil
			}
			stack = pushPairsDedup(stack, av.List.Items, bv.List.Items)
		case *MShellDict:
			bv, ok := p.b.(*MShellDict)
			if !ok || len(av.Items) != len(bv.Items) {
				return false, nil
			}
			for key, aval := range av.Items {
				bval, ok := bv.Items[key]
				if !ok {
					return false, nil
				}
				stack = append(stack, eqPair{a: aval, b: bval})
			}
		default:
			eq, err := p.a.Equals(p.b)
			if err != nil || !eq {
				return eq, err
			}
		}
	}
	return true, nil
}

// compareValues returns -1, 0, or 1, giving a total order over every value
// type. Different kinds are ordered by a fixed type rank (valueTypeRank); within
// a kind the natural order is used (numbers numerically with int/float
// interleaved, text lexically, dates chronologically, bytes bytewise).
// Structured values compare lexicographically: lists positionally (shorter
// prefix first), dicts by sorted key then value, enums by name then declaration
// order then payloads. The order agrees with structural equality: compareValues
// returns 0 exactly when the two values are Equals.
//
// The comparison is driven by an explicit work stack rather than recursion, so
// arbitrarily deep values (e.g. a long `node(node(...))` enum chain) cannot
// overflow the call stack. Each task is either a pair of values to compare or a
// precomputed literal result (used for length tiebreaks and dict key / enum
// name comparisons). Pending tasks pop in lexicographic order; the first
// non-zero result short-circuits. Children of a compound value are pushed on top
// of that value's own length-tiebreak, so the tiebreak is only reached when the
// whole prefix compared equal.
func compareValues(a, b MShellObject) int {
	type task struct {
		a, b  MShellObject
		lit   int
		isLit bool
	}
	var guard dagGuard
	stack := []task{{a: a, b: b}}
	for len(stack) > 0 {
		t := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if t.isLit {
			if t.lit != 0 {
				return t.lit
			}
			continue
		}
		// Shared substructure: a pointer-identical pair compares 0 by
		// definition, and a pair this walk already expanded proved 0 (any
		// non-zero would have returned; see dagGuard). Skipping both keeps
		// DAG-shaped values linear instead of 2^n.
		if sameRef(t.a, t.b) || guard.skip(t.a, t.b) {
			continue
		}
		ra, rb := valueTypeRank(t.a), valueTypeRank(t.b)
		if ra != rb {
			return cmpInt(ra, rb)
		}
		switch av := t.a.(type) {
		case MShellNull:
			// Two nulls are equal; move to the next task.
		case MShellBool:
			bv := t.b.(MShellBool)
			if av.Value != bv.Value {
				if !av.Value { // false < true
					return -1
				}
				return 1
			}
		case MShellInt:
			if bv, ok := t.b.(MShellInt); ok {
				if c := cmpInt(av.Value, bv.Value); c != 0 {
					return c
				}
			} else if c := cmpFloat(numericFloat(t.a), numericFloat(t.b)); c != 0 {
				return c
			}
		case MShellFloat:
			if c := cmpFloat(numericFloat(t.a), numericFloat(t.b)); c != 0 {
				return c
			}
		case MShellString, MShellPath, MShellLiteral:
			if c := strings.Compare(textContent(t.a), textContent(t.b)); c != 0 {
				return c
			}
		case *MShellDateTime:
			bt := t.b.(*MShellDateTime).Time
			if av.Time.Before(bt) {
				return -1
			}
			if av.Time.After(bt) {
				return 1
			}
		case MShellBinary:
			if c := bytes.Compare(av, t.b.(MShellBinary)); c != 0 {
				return c
			}
		case Maybe, *Maybe:
			am, _ := asMaybe(t.a)
			bm, _ := asMaybe(t.b)
			an, bn := am.IsNone(), bm.IsNone()
			if an != bn {
				if an { // none < just
					return -1
				}
				return 1
			}
			if !an { // both `just`: compare payloads
				stack = append(stack, task{a: am.obj, b: bm.obj})
			}
		case *MShellList:
			bl := t.b.(*MShellList)
			n := min(len(av.Items), len(bl.Items))
			stack = append(stack, task{lit: cmpInt(len(av.Items), len(bl.Items)), isLit: true})
			for i := n - 1; i >= 0; i-- {
				// Skip a pair pointer-identical to its neighbor: it compares 0
				// and would double the walk on self-doubling values.
				if i > 0 && sameRef(av.Items[i], av.Items[i-1]) && sameRef(bl.Items[i], bl.Items[i-1]) {
					continue
				}
				stack = append(stack, task{a: av.Items[i], b: bl.Items[i]})
			}
		case *MShellDict:
			bd := t.b.(*MShellDict)
			ak := sortedDictKeys(av.Items)
			bk := sortedDictKeys(bd.Items)
			n := min(len(ak), len(bk))
			stack = append(stack, task{lit: cmpInt(len(ak), len(bk)), isLit: true})
			for i := n - 1; i >= 0; i-- {
				// Pushed so `key compare` pops before its `value compare`.
				stack = append(stack, task{a: av.Items[ak[i]], b: bd.Items[bk[i]]})
				stack = append(stack, task{lit: strings.Compare(ak[i], bk[i]), isLit: true})
			}
		case *MShellEnum:
			be := t.b.(*MShellEnum)
			n := min(len(av.Payload), len(be.Payload))
			stack = append(stack, task{lit: cmpInt(len(av.Payload), len(be.Payload)), isLit: true})
			for i := n - 1; i >= 0; i-- {
				// Skip a pair pointer-identical to its neighbor (see list arm).
				if i > 0 && sameRef(av.Payload[i], av.Payload[i-1]) && sameRef(be.Payload[i], be.Payload[i-1]) {
					continue
				}
				stack = append(stack, task{a: av.Payload[i], b: be.Payload[i]})
			}
			// Name and member (declaration order) compare before any payload.
			stack = append(stack, task{lit: cmpInt(av.MemberIndex, be.MemberIndex), isLit: true})
			stack = append(stack, task{lit: strings.Compare(av.EnumName, be.EnumName), isLit: true})
		default:
			// Unorderable kinds (quotation, pipe, grid, ...) share a rank and
			// compare equal, so a stable sort leaves them in their original
			// relative order.
		}
	}
	return 0
}

// SortList returns a new list with the same elements sorted by the total order
// compareValues defines. Element identity and type are preserved (a list of
// ints stays ints, enum payloads are kept) — sorting only reorders.
func SortList(list *MShellList) (*MShellList, error) {
	newItems := make([]MShellObject, len(list.Items))
	copy(newItems, list.Items)
	sort.SliceStable(newItems, func(i, j int) bool {
		return compareValues(newItems[i], newItems[j]) < 0
	})
	newList := NewList(0)
	newList.Items = newItems
	CopyListParams(list, newList)
	return newList, nil
}

// SortListFunc sorts by a string key (each element's CastString) using the given
// string comparer — used for version sort. Original elements are preserved in
// the result. Returns an error if any element cannot be cast to a string.
func SortListFunc(list *MShellList, cmp func(a string, b string) int) (*MShellList, error) {
	type keyed struct {
		key string
		obj MShellObject
	}
	items := make([]keyed, len(list.Items))
	for i, item := range list.Items {
		str, err := item.CastString()
		if err != nil {
			return nil, fmt.Errorf("Cannot sort a list with a %s inside (%s).\n", item.TypeName(), item.DebugString())
		}
		items[i] = keyed{key: str, obj: item}
	}

	slices.SortStableFunc(items, func(a, b keyed) int {
		return cmp(a.key, b.key)
	})

	newList := NewList(0)
	for _, it := range items {
		newList.Items = append(newList.Items, it.obj)
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
	// Elements joined with a space, surrounded by '[' and ']'
	return renderValue(obj, flavorDebug)
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
	// Each item joined with ' | '
	return renderValue(obj, flavorDebug)
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
	return renderValue(obj, flavorJson)
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
		return false, nil
	}
}

func (obj MShellBool) Equals(other MShellObject) (bool, error) {
	asBool, ok := other.(MShellBool)
	if !ok {
		return false, nil
	}
	return obj.Value == asBool.Value, nil
}


func (obj *MShellQuotation) Equals(other MShellObject) (bool, error) {
	// Quotations are code values; two are equal only when they are the same
	// quotation object (reference identity).
	o, ok := other.(*MShellQuotation)
	return ok && obj == o, nil
}

func (obj *MShellList) Equals(other MShellObject) (bool, error) {
	return equalsIter(obj, other)
}

func (obj MShellString) Equals(other MShellObject) (bool, error) {
	// str/path/literal compare by their text content (the `=` overloads
	// permit str/path comparison); any other type is simply not equal.
	switch o := other.(type) {
	case MShellString:
		return obj.Content == o.Content, nil
	case MShellLiteral:
		return obj.Content == o.LiteralText, nil
	case MShellPath:
		return obj.Content == o.Path, nil
	default:
		return false, nil
	}
}

func (obj MShellPath) Equals(other MShellObject) (bool, error) {
	switch o := other.(type) {
	case MShellPath:
		return obj.Path == o.Path, nil
	case MShellLiteral:
		return obj.Path == o.LiteralText, nil
	case MShellString:
		return obj.Path == o.Content, nil
	default:
		return false, nil
	}
}

func (obj *MShellPipe) Equals(other MShellObject) (bool, error) {
	return equalsIter(obj, other)
}

func (obj MShellInt) Equals(other MShellObject) (bool, error) {
	asInt, ok := other.(MShellInt)
	if !ok {
		return false, nil
	}
	return obj.Value == asInt.Value, nil
}

func (obj MShellFloat) Equals(other MShellObject) (bool, error) {
	asFloat, ok := other.(MShellFloat)
	if !ok {
		return false, nil
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

// Set sets the value at the given row index. If a typed column is given a value
// of a different type, the column is promoted to generic storage so the value
// is stored rather than silently dropped.
func (col *GridColumn) Set(index int, value MShellObject) {
	switch col.ColType {
	case COL_INT:
		if intVal, ok := value.(MShellInt); ok {
			col.IntData[index] = int64(intVal.Value)
			return
		}
	case COL_FLOAT:
		if floatVal, ok := value.(MShellFloat); ok {
			col.FloatData[index] = floatVal.Value
			return
		}
	case COL_STRING:
		if strVal, ok := value.(MShellString); ok {
			col.StringData[index] = strVal.Content
			return
		}
	case COL_DATETIME:
		if dtVal, ok := value.(*MShellDateTime); ok {
			col.DateTimeData[index] = dtVal.Time
			return
		}
	case COL_GENERIC:
		col.GenericData[index] = value
		return
	}
	// Typed column received a value of a different type: promote the whole
	// column to generic storage, then store the value.
	col.promoteToGeneric()
	col.GenericData[index] = value
}

// promoteToGeneric materializes a typed column's data into generic storage so
// the column can hold values of any type. It is a no-op for an already-generic
// column.
func (col *GridColumn) promoteToGeneric() {
	if col.ColType == COL_GENERIC {
		return
	}
	n := col.Len()
	generic := make([]MShellObject, n)
	for i := 0; i < n; i++ {
		generic[i] = col.Get(i)
	}
	col.ColType = COL_GENERIC
	col.GenericData = generic
	col.IntData = nil
	col.FloatData = nil
	col.StringData = nil
	col.DateTimeData = nil
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
	o, ok := other.(*MShellGrid)
	if !ok {
		return false, nil
	}
	if g.RowCount != o.RowCount || len(g.Columns) != len(o.Columns) {
		return false, nil
	}
	for i, col := range g.Columns {
		if col.Name != o.Columns[i].Name {
			return false, nil
		}
	}
	for i := 0; i < g.RowCount; i++ {
		eq, err := g.GetRow(i).ToDict().Equals(o.GetRow(i).ToDict())
		if err != nil || !eq {
			return eq, err
		}
	}
	return true, nil
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
	o, ok := other.(*MShellGridView)
	if !ok {
		return false, nil
	}
	if len(v.Indices) != len(o.Indices) {
		return false, nil
	}
	for i := range v.Indices {
		eq, err := v.GetRow(i).ToDict().Equals(o.GetRow(i).ToDict())
		if err != nil || !eq {
			return eq, err
		}
	}
	return true, nil
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
	o, ok := other.(*MShellGridRow)
	if !ok {
		return false, nil
	}
	return r.ToDict().Equals(o.ToDict())
}

func (r *MShellGridRow) CastString() (string, error) {
	return "", fmt.Errorf("Cannot cast a GridRow to a string.\n")
}

// }}}

// }}}
