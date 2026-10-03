package main

// Type checker diagnostics. Format() makes the text only when one is
// printed, so the checker appends errors without touching names or types.

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
	TErrBranchStackSize
	TErrDefBodyMismatch // def's declared sig and body stack effect disagree
	TErrNonExhaustiveMatch
	TErrNoMatchingOverload
	// TErrAmbiguousTyping is an overload choice still open when its unit
	// is solved, whose candidates give different outputs: an annotation
	// is needed. Hint says which.
	TErrAmbiguousTyping
	TErrTypeParse
	// TErrChildStack is emitted when code the runtime runs on its own
	// stack (a dict value, grid cell, or format-string interpolation) leaves
	// something other than what that construct needs. Hint holds the message.
	TErrChildStack
	// TErrInvalidMatchPattern is emitted when a match arm pattern is not
	// one of the recognized forms. Hint lists the legal forms.
	TErrInvalidMatchPattern
	// TErrDebugDump is the snapshot of the stack and variables the `dbg`
	// word asks for. Informational: it does not fail the check.
	TErrDebugDump
	// TErrUnwrapAlwaysFails is an informational diagnostic emitted when a
	// `?` unwraps a value the checker can prove is always `None` — a getter
	// for a field a shape does not declare, or a `none`. Informational
	// severity (does not fail the type check); Hint holds the message.
	TErrUnwrapAlwaysFails

	// TErrVarType is a store whose value does not fit the variable's one
	// type. Name is the variable, Expected its type, Actual the value's.
	TErrVarType
	// TErrNoJoin is two values that meet (if arms, list elements) with no
	// common type. Hint holds the message.
	TErrNoJoin
	// TErrCoreUnsupported is a construct the checker has no rule for: a
	// gap in the checker, reported rather than accepted. Hint names it.
	TErrCoreUnsupported
	// TErrCoreInternal is a check the checker made that failed when
	// repeated with the final types: a bug in the checker, never a reason
	// to accept a program. Hint holds the details.
	TErrCoreInternal
	// TErrDeclaration is a `type` or `enum` declaration the checker
	// refuses: a name already taken, recursion with nothing in between.
	// Hint holds the message.
	TErrDeclaration
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
	// Fix is an edit that fixes the error, offered by the LSP as a code
	// action; its Kind is FixNone when there is none.
	Fix TypeFix
	// Earlier is set, in a REPL session, on an error from a check an
	// earlier line made that the current line's types make fail: its
	// position is on that earlier line (TypeCoreSession.go).
	Earlier bool
}

// TypeFixKind says what a TypeFix does.
type TypeFixKind uint8

const (
	FixNone TypeFixKind = iota
	// FixInsert inserts Text before the token At.
	FixInsert
	// FixDelete deletes the text from the start of At to the start of
	// Until.
	FixDelete
)

// TypeFix is an edit to the source that fixes a type error.
type TypeFix struct {
	Kind      TypeFixKind
	Title     string
	At, Until Token
	Text      string
}

// Format builds a human-readable message. The arena and name table are
// consulted to render TypeIds back to source-shaped text.
func (e TypeError) Format(arena *TypeArena, names *NameTable) string {
	var sb strings.Builder
	prefix := "type error"
	if e.Severity == SeverityInfo {
		prefix = "type info"
	}
	if e.Earlier {
		fmt.Fprintf(&sb, "%s at line %d, column %d of an earlier line, given this line: ", prefix, e.Pos.Line, e.Pos.Column)
	} else {
		fmt.Fprintf(&sb, "%s at line %d, column %d: ", prefix, e.Pos.Line, e.Pos.Column)
	}
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
			if e.Hint != "" {
				fmt.Fprintf(&sb, "; %s", e.Hint)
			}
		}
	case TErrUnknownIdentifier:
		fmt.Fprintf(&sb, "unknown identifier '%s'", e.Name)
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
	case TErrTypeParse:
		fmt.Fprintf(&sb, "type parse error: %s", e.Hint)
	case TErrChildStack:
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
	case TErrVarType:
		fmt.Fprintf(&sb, "variable '%s' has type %s, so it cannot store a value of type %s; use a new name, or widen the first store with `as`",
			e.Name, FormatType(arena, names, e.Expected), FormatType(arena, names, e.Actual))
		if e.Hint != "" {
			fmt.Fprintf(&sb, "; %s", e.Hint)
		}
	case TErrNoJoin:
		fmt.Fprintf(&sb, "%s", e.Hint)
	case TErrCoreUnsupported:
		fmt.Fprintf(&sb, "the type checker has no rule for %s; please report this", e.Hint)
	case TErrCoreInternal:
		fmt.Fprintf(&sb, "internal checker error: %s", e.Hint)
	case TErrDeclaration:
		fmt.Fprintf(&sb, "%s", e.Hint)
	default:
		fmt.Fprintf(&sb, "unknown type error")
	}
	return sb.String()
}

// FormatType renders a TypeId to source-shaped text.
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
	case TidUnknown:
		return "unknown"
	}
	n := arena.Node(id)
	switch n.Kind {
	case TKList:
		return "[" + FormatType(arena, names, TypeId(n.A)) + "]"
	case TKUnion:
		var sb strings.Builder
		for i, arm := range arena.unionMembers[n.Extra] {
			if i > 0 {
				sb.WriteString(" | ")
			}
			sb.WriteString(FormatType(arena, names, arm))
		}
		return sb.String()
	case TKCommand:
		var parts []string
		name := "Command["
		out := CommandCaptureMode(n.B)
		if out&CommandPipe != 0 {
			name, out = "Pipe[", out&^CommandPipe
		}
		if out != CommandCaptureNone {
			parts = append(parts, "stdout="+formatCommandCapture(out))
		}
		if n.Extra != uint32(CommandCaptureNone) {
			parts = append(parts, "stderr="+formatCommandCapture(CommandCaptureMode(n.Extra)))
		}
		if len(parts) == 0 {
			return name + FormatType(arena, names, TypeId(n.A)) + "]"
		}
		return name + FormatType(arena, names, TypeId(n.A)) + "; " + strings.Join(parts, ", ") + "]"
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
		if sig.Diverges && len(sig.Outputs) == 0 {
			sb.WriteString("never")
		}
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
	case TKRigid:
		return names.Name(NameId(n.A))
	case TKGrid, TKGridView, TKGridRow:
		name := "Grid"
		switch n.Kind {
		case TKGridView:
			name = "GridView"
		case TKGridRow:
			name = "GridRow"
		}
		if n.A == 0 {
			return name
		}
		if arena.Node(TypeId(n.A)).Kind != TKRecord {
			// A schema not known yet.
			return name + "[" + FormatType(arena, names, TypeId(n.A)) + "]"
		}
		return name + formatSchema(arena, names, arena.records[arena.Node(TypeId(n.A)).Extra])
	case TKRecord:
		return formatRecord(arena, names, arena.records[n.Extra])
	case TKEnum:
		decl := arena.enumDecls[n.A]
		args := arena.enumArgs[n.Extra]
		if n.A == EnumMaybe && args[0] == TidBottom {
			// The type of `none`: a Maybe that can hold nothing else.
			return "none"
		}
		if len(args) == 0 {
			return names.Name(decl.Name)
		}
		parts := make([]string, len(args))
		for i, t := range args {
			parts[i] = FormatType(arena, names, t)
		}
		return names.Name(decl.Name) + "[" + strings.Join(parts, " ") + "]"
	case TKAlias:
		return names.Name(arena.aliases[n.A].Name)
	case TKAbstract:
		return fmt.Sprintf("k%d", n.A)
	case TKParam:
		return fmt.Sprintf("$%d", n.A)
	}
	return fmt.Sprintf("<%s #%d>", n.Kind, uint32(id))
}

// formatRecord writes a dict-kinded type in the design document's notation:
// `{str: T}` for a dictionary, and otherwise the declared labels followed by
// the remainder, which is `*: T`, `| open` or `| exact`.
func formatRecord(arena *TypeArena, names *NameTable, r RecordType) string {
	if len(r.Fields) == 0 && r.Rest.Status == FieldDeletable {
		return "{str: " + FormatType(arena, names, r.Rest.Type) + "}"
	}
	var parts []string
	for _, f := range r.Fields {
		name := names.Name(f.Name)
		switch f.Status {
		case FieldRequired:
			parts = append(parts, name+": "+FormatType(arena, names, f.Type))
		case FieldOptional:
			parts = append(parts, name+"?: "+FormatType(arena, names, f.Type))
		case FieldDeletable:
			parts = append(parts, name+"?del: "+FormatType(arena, names, f.Type))
		case FieldAbsent:
			parts = append(parts, name+": absent")
		case FieldOpen:
			parts = append(parts, name+": open")
		}
	}
	body := strings.Join(parts, ", ")
	switch r.Rest.Status {
	case FieldOptional:
		if body != "" {
			body += ", "
		}
		return "{" + body + "*: " + FormatType(arena, names, r.Rest.Type) + "}"
	case FieldDeletable:
		return "{" + body + " | str: " + FormatType(arena, names, r.Rest.Type) + "}"
	case FieldAbsent:
		// A literal's type: these keys and no others. No type a user
		// writes means that, so it is marked; it is printed only.
		return "exact {" + body + "}"
	case FieldRequired:
		return "{" + body + " | *!: " + FormatType(arena, names, r.Rest.Type) + "}"
	}
	// Other keys may exist, unknown: what a written shape type means.
	return "{" + body + "}"
}

// formatSchema prints a core grid's schema: its columns, and what other
// columns there may be. A known schema, the usual case, is the columns in
// braces (`Grid{a: int}`); one that may have other columns ends in `...`;
// the unknown one is printed as nothing at all, so the type is `Grid`.
func formatSchema(arena *TypeArena, names *NameTable, r RecordType) string {
	switch r.Rest.Status {
	case FieldOpen:
		if len(r.Fields) == 0 {
			return ""
		}
		return strings.TrimSuffix(formatRecord(arena, names, r), "}") + ", ...}"
	case FieldAbsent:
		return strings.TrimPrefix(formatRecord(arena, names, r), "exact ")
	}
	return formatRecord(arena, names, r)
}

func formatCommandCapture(mode CommandCaptureMode) string {
	switch mode {
	case CommandCaptureStr:
		return "str"
	case CommandCaptureBytes:
		return "bytes"
	case CommandCaptureLines:
		return "[str]"
	case CommandDestFile:
		return "file"
	case CommandDestInPlace:
		return "in-place"
	case CommandDestMerged:
		return "merged"
	default:
		return "none"
	}
}
