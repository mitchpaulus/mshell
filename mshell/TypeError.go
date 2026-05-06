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
	TErrBranchStackSize
	TErrBranchVarSet
	TErrNonExhaustiveMatch
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
	case TErrBranchStackSize:
		fmt.Fprintf(&sb, "branches produce stacks of differing sizes: %s", e.Hint)
	case TErrBranchVarSet:
		fmt.Fprintf(&sb, "branches bind different variable sets: %s", e.Hint)
	case TErrNonExhaustiveMatch:
		fmt.Fprintf(&sb, "non-exhaustive match: %s", e.Hint)
	default:
		fmt.Fprintf(&sb, "unknown type error")
	}
	return sb.String()
}

// FormatType renders a TypeId to source-shaped text. Primitives and the
// Phase-3 composite kinds are covered. Type variables (Phase 6) and grid
// schemas (Phase 8) extend this further.
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
	n := arena.Node(id)
	switch n.Kind {
	case TKMaybe:
		return "Maybe[" + FormatType(arena, names, TypeId(n.A)) + "]"
	case TKList:
		return "[" + FormatType(arena, names, TypeId(n.A)) + "]"
	case TKDict:
		return "{" + FormatType(arena, names, TypeId(n.A)) + ": " + FormatType(arena, names, TypeId(n.B)) + "}"
	case TKShape:
		var sb strings.Builder
		sb.WriteByte('{')
		for i, f := range arena.shapeFields[n.Extra] {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(names.Name(f.Name))
			sb.WriteString(": ")
			sb.WriteString(FormatType(arena, names, f.Type))
		}
		sb.WriteByte('}')
		return sb.String()
	case TKUnion:
		var sb strings.Builder
		if n.A != 0 {
			sb.WriteString(names.Name(NameId(n.A)))
			sb.WriteByte('(')
		}
		for i, arm := range arena.unionMembers[n.Extra] {
			if i > 0 {
				sb.WriteString(" | ")
			}
			sb.WriteString(FormatType(arena, names, arm))
		}
		if n.A != 0 {
			sb.WriteByte(')')
		}
		return sb.String()
	case TKBrand:
		return names.Name(NameId(n.A)) + "(" + FormatType(arena, names, TypeId(n.B)) + ")"
	case TKQuote:
		sig := arena.quoteSigs[n.Extra]
		var sb strings.Builder
		sb.WriteByte('(')
		for i, in := range sig.Inputs {
			if i > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(FormatType(arena, names, in))
		}
		sb.WriteString(" -- ")
		for i, out := range sig.Outputs {
			if i > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(FormatType(arena, names, out))
		}
		sb.WriteByte(')')
		return sb.String()
	case TKVar:
		return fmt.Sprintf("T%d", n.A)
	case TKGrid:
		return "Grid"
	case TKGridView:
		return "GridView"
	case TKGridRow:
		return "GridRow"
	}
	return fmt.Sprintf("<%s #%d>", n.Kind, uint32(id))
}
