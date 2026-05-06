package main

// Static type-check errors collected by the Checker. Format() materializes
// human-readable text only at print time, so the hot path can append errors
// without touching the arena's name machinery.
//
// Phase 2 scope: a small set of error kinds for arithmetic-only checking.
// More kinds (overload ambiguity, generic-instantiation failure, branch
// mismatch, etc.) land alongside the phases that need them.

import (
	"fmt"
	"strings"
)

// TypeErrorKind enumerates the static-checker error categories.
type TypeErrorKind uint8

const (
	TErrUnknown TypeErrorKind = iota
	TErrStackUnderflow
	TErrTypeMismatch
	TErrUnknownIdentifier
	TErrLeftoverStack // top-level program left items on the stack at end (informational; not always an error)
)

// TypeError is a single static-check failure. Pos is a Token (its line/column
// drive error formatting). Expected/Actual are TypeIds; the Hint is free text
// for cases where a more specific message helps.
type TypeError struct {
	Kind     TypeErrorKind
	Pos      Token
	Expected TypeId
	Actual   TypeId
	ArgIndex int    // 0-based index into the failing sig's inputs (TypeMismatch only)
	Name     string // identifier name for UnknownIdentifier
	Hint     string
}

// Format builds a human-readable message. The arena and name table are
// consulted to render TypeIds back to source-shaped text.
func (e TypeError) Format(arena *TypeArena, names *NameTable) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "type error at line %d, column %d: ", e.Pos.Line, e.Pos.Column)
	switch e.Kind {
	case TErrStackUnderflow:
		fmt.Fprintf(&sb, "stack underflow at '%s'", e.Pos.Lexeme)
		if e.Hint != "" {
			fmt.Fprintf(&sb, " (%s)", e.Hint)
		}
	case TErrTypeMismatch:
		fmt.Fprintf(&sb, "'%s' expected %s at argument %d, got %s",
			e.Pos.Lexeme,
			FormatType(arena, names, e.Expected),
			e.ArgIndex,
			FormatType(arena, names, e.Actual))
	case TErrUnknownIdentifier:
		fmt.Fprintf(&sb, "unknown identifier '%s'", e.Name)
	case TErrLeftoverStack:
		fmt.Fprintf(&sb, "values left on stack at end of program: %s", e.Hint)
	default:
		fmt.Fprintf(&sb, "unknown type error")
	}
	return sb.String()
}

// FormatType renders a TypeId to source-shaped text. Phase 2 only handles
// primitives; later phases extend this to composites.
func FormatType(arena *TypeArena, names *NameTable, id TypeId) string {
	switch id {
	case TidNothing:
		return "<nothing>"
	case TidBool:
		return "bool"
	case TidInt:
		return "int"
	case TidFloat:
		return "float"
	case TidStr:
		return "str"
	case TidBytes:
		return "bytes"
	case TidNone:
		return "none"
	case TidBottom:
		return "<bottom>"
	}
	// Composite kinds — minimal rendering for now.
	n := arena.Node(id)
	return fmt.Sprintf("<%s #%d>", n.Kind, uint32(id))
}
