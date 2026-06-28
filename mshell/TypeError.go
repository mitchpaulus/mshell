package main

// Static type-check errors collected by the Checker. Format() materializes
// human-readable text only at print time, so the hot path can append errors
// without touching the arena's name machinery.

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
	TErrMaybeUnset // variable bound on some control-flow paths but not others
	TErrLeftoverStack // top-level program left items on the stack at end (informational; not always an error)
	TErrBranchStackSize
	TErrDefBodyMismatch // def's declared sig and body stack effect disagree
	TErrNonExhaustiveMatch
	TErrNoMatchingOverload
	// TErrAmbiguousTyping is emitted when the branching walker reaches
	// the end of a program (or a synchronization point) with more than
	// one surviving branch whose typings disagree. The user must add
	// an annotation upstream to disambiguate. Hint lists the surviving
	// final stacks.
	TErrAmbiguousTyping
	TErrReservedTypeName
	TErrDuplicateTypeName
	// TErrRebrand is emitted when a `type X = ...` right-hand side is
	// already a branded type (re-branding is not allowed for unions).
	TErrRebrand
	TErrInvalidCast
	TErrTypeParse
	TErrInterpolationArity
	// TErrInvalidMatchPattern is emitted when a match arm pattern is not
	// one of the recognized forms. Hint lists the legal forms.
	TErrInvalidMatchPattern
	// TErrDebugDump is emitted by the `dbg` builtin at each branch
	// that walks past it. Informational severity — does not fail the
	// type check. Hint holds the formatted snapshot of stack + vars.
	TErrDebugDump
	// TErrUnwrapAlwaysFails is an informational diagnostic emitted when a
	// `?` unwraps a value the checker can prove is always `None` — a getter
	// for a field a shape does not declare, or a `none`. Informational
	// severity (does not fail the type check); Hint holds the message.
	TErrUnwrapAlwaysFails
)

// TypeErrorSeverity classifies a diagnostic. Severity-error blocks
// the type check; severity-info is purely informational (used by
// `dbg` snapshots) and never causes the type checker to fail.
type TypeErrorSeverity uint8

const (
	SeverityError TypeErrorSeverity = iota
	SeverityInfo
)

// TypeError is a single static-check finding. Pos is a Token (its line/column
// drive error formatting). Expected/Actual are TypeIds; the Hint is free text
// for cases where a more specific message helps. Severity defaults to
// SeverityError; set SeverityInfo for non-fatal diagnostics.
type TypeError struct {
	Severity TypeErrorSeverity
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
	prefix := "type error"
	if e.Severity == SeverityInfo {
		prefix = "type info"
	}
	fmt.Fprintf(&sb, "%s at line %d, column %d: ", prefix, e.Pos.Line, e.Pos.Column)
	switch e.Kind {
	case TErrStackUnderflow:
		fmt.Fprintf(&sb, "stack underflow at '%s'", e.Pos.Lexeme)
		if e.Hint != "" {
			fmt.Fprintf(&sb, " (%s)", e.Hint)
		}
	case TErrTypeMismatch:
		if e.Expected == TidNothing && e.Hint != "" {
			// Custom-hint-driven mismatch (e.g. domain rules like
			// pivot's "no container cells"); skip the canned
			// "expected X at argument N" template.
			fmt.Fprintf(&sb, "%s", e.Hint)
		} else {
			fmt.Fprintf(&sb, "'%s' expected %s at argument %d, got %s",
				e.Pos.Lexeme,
				FormatType(arena, names, e.Expected),
				e.ArgIndex,
				FormatType(arena, names, e.Actual))
		}
	case TErrUnknownIdentifier:
		fmt.Fprintf(&sb, "unknown identifier '%s'", e.Name)
	case TErrMaybeUnset:
		fmt.Fprintf(&sb, "variable '%s' may be unset here: it is bound on some control-flow paths but not all", e.Name)
	case TErrLeftoverStack:
		fmt.Fprintf(&sb, "values left on stack at end of program: %s", e.Hint)
	case TErrBranchStackSize:
		fmt.Fprintf(&sb, "branches produce stacks of differing sizes: %s", e.Hint)
	case TErrNonExhaustiveMatch:
		fmt.Fprintf(&sb, "non-exhaustive match: %s", e.Hint)
	case TErrNoMatchingOverload:
		fmt.Fprintf(&sb, "no matching overload for '%s': %s", e.Pos.Lexeme, e.Hint)
	case TErrAmbiguousTyping:
		fmt.Fprintf(&sb, "ambiguous typing — add an annotation to disambiguate: %s", e.Hint)
	case TErrDebugDump:
		fmt.Fprintf(&sb, "dbg: %s", e.Hint)
	case TErrUnwrapAlwaysFails:
		fmt.Fprintf(&sb, "%s", e.Hint)
	case TErrReservedTypeName:
		fmt.Fprintf(&sb, "cannot redefine reserved type name '%s'", e.Name)
		if e.Hint != "" {
			fmt.Fprintf(&sb, " (%s)", e.Hint)
		}
	case TErrDuplicateTypeName:
		fmt.Fprintf(&sb, "type '%s' is already declared", e.Name)
	case TErrRebrand:
		fmt.Fprintf(&sb, "cannot declare type '%s': right-hand side is already a branded type", e.Name)
	case TErrInvalidCast:
		fmt.Fprintf(&sb, "invalid cast: cannot cast %s to %s",
			FormatType(arena, names, e.Actual),
			FormatType(arena, names, e.Expected))
	case TErrTypeParse:
		fmt.Fprintf(&sb, "type parse error: %s", e.Hint)
	case TErrInterpolationArity:
		fmt.Fprintf(&sb, "%s", e.Hint)
	case TErrInvalidMatchPattern:
		fmt.Fprintf(&sb, "unrecognized match arm pattern '%s'", e.Pos.Lexeme)
		if e.Hint != "" {
			fmt.Fprintf(&sb, " (%s)", e.Hint)
		}
	case TErrDefBodyMismatch:
		// Hint carries the human-readable "declared vs body"
		// description. Pos is the def's name token (the body could
		// span many lines, so the name is the most stable anchor).
		fmt.Fprintf(&sb, "definition and body do not match for '%s': %s", e.Name, e.Hint)
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
	case TidNull:
		return "null"
	case TidPath:
		return "path"
	case TidDateTime:
		return "datetime"
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
			if f.Optional {
				sb.WriteByte('?')
			}
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
	case TKCommand:
		var parts []string
		if n.B != uint32(CommandCaptureNone) {
			parts = append(parts, "stdout="+formatCommandCapture(CommandCaptureMode(n.B)))
		}
		if n.Extra != uint32(CommandCaptureNone) {
			parts = append(parts, "stderr="+formatCommandCapture(CommandCaptureMode(n.Extra)))
		}
		if len(parts) == 0 {
			return "Command[" + FormatType(arena, names, TypeId(n.A)) + "]"
		}
		return "Command[" + FormatType(arena, names, TypeId(n.A)) + "; " + strings.Join(parts, ", ") + "]"
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
	case TKOverloadedQuote:
		sigs := arena.overloadedQuoteSigs[n.Extra]
		var sb strings.Builder
		sb.WriteString("overload{")
		for i, sig := range sigs {
			if i > 0 {
				sb.WriteString(" | ")
			}
			sb.WriteString(FormatType(arena, names, arena.MakeQuote(sig)))
		}
		sb.WriteByte('}')
		return sb.String()
	case TKVar:
		return fmt.Sprintf("T%d", n.A)
	case TKRigid:
		return names.Name(NameId(n.A))
	case TKGrid:
		return "Grid"
	case TKGridView:
		return "GridView"
	case TKGridRow:
		return "GridRow"
	}
	return fmt.Sprintf("<%s #%d>", n.Kind, uint32(id))
}

func formatCommandCapture(mode CommandCaptureMode) string {
	switch mode {
	case CommandCaptureStr:
		return "str"
	case CommandCaptureBytes:
		return "bytes"
	case CommandCaptureLines:
		return "[str]"
	default:
		return "none"
	}
}
