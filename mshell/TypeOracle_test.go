package main

// The syntax of formal-ver/oracle (see its README) for Go types: a printer,
// to ask the oracle about a Go type, and a parser, to read its answers and
// its examples into Go types.
//
// A recursive type `(mu T)` becomes an alias whose body is T with `(rv 0)`
// referring to the alias. The same `(mu T)` text always gives the same
// alias, so an alias in the oracle's answer is the one from the question.

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

type sexp struct {
	atom string
	list []sexp
	isList bool
}

func tokenizeSexp(s string) []string {
	var toks []string
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			toks = append(toks, buf.String())
			buf.Reset()
		}
	}
	for _, c := range s {
		switch c {
		case '(', ')':
			flush()
			toks = append(toks, string(c))
		case ' ', '\t', '\r', '\n':
			flush()
		default:
			buf.WriteRune(c)
		}
	}
	flush()
	return toks
}

func parseSexps(s string) ([]sexp, error) {
	toks := tokenizeSexp(s)
	var one func(i int) (sexp, int, error)
	one = func(i int) (sexp, int, error) {
		if i >= len(toks) {
			return sexp{}, i, fmt.Errorf("unexpected end")
		}
		switch toks[i] {
		case "(":
			var items []sexp
			i++
			for i < len(toks) && toks[i] != ")" {
				x, j, err := one(i)
				if err != nil {
					return sexp{}, j, err
				}
				items = append(items, x)
				i = j
			}
			if i >= len(toks) {
				return sexp{}, i, fmt.Errorf("missing )")
			}
			return sexp{list: items, isList: true}, i + 1, nil
		case ")":
			return sexp{}, i, fmt.Errorf("unexpected )")
		}
		return sexp{atom: toks[i]}, i + 1, nil
	}
	var out []sexp
	for i := 0; i < len(toks); {
		x, j, err := one(i)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
		i = j
	}
	return out, nil
}

func (s sexp) String() string {
	if !s.isList {
		return s.atom
	}
	parts := make([]string, len(s.list))
	for i, x := range s.list {
		parts[i] = x.String()
	}
	return "(" + strings.Join(parts, " ") + ")"
}

// oracleTypes converts between Go types and oracle syntax over one arena.
type oracleTypes struct {
	arena *TypeArena
	names *NameTable
	mus   map[string]TypeId // the alias for each `(mu T)` text
	enums map[string]uint32 // the declaration for each enum identity
}

func newOracleTypes() *oracleTypes {
	return &oracleTypes{
		arena: NewTypeArena(),
		names: NewNameTable(),
		mus:   make(map[string]TypeId),
		enums: make(map[string]uint32),
	}
}

func (o *oracleTypes) parse(s string) (TypeId, error) {
	xs, err := parseSexps(s)
	if err != nil {
		return TidNothing, err
	}
	if len(xs) != 1 {
		return TidNothing, fmt.Errorf("expected one type in %q", s)
	}
	return o.fromSexp(xs[0], nil)
}

func (o *oracleTypes) mustParse(t testing.TB, s string) TypeId {
	t.Helper()
	id, err := o.parse(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return id
}

func parseOracleVariance(s string) (EnumParam, error) {
	fresh := false
	if v, ok := strings.CutSuffix(s, "/fresh"); ok {
		s, fresh = v, true
	}
	switch s {
	case "co":
		return EnumParam{Variance: VarCo, Fresh: fresh}, nil
	case "contra":
		return EnumParam{Variance: VarContra, Fresh: fresh}, nil
	case "inv":
		return EnumParam{Variance: VarInv, Fresh: fresh}, nil
	}
	return EnumParam{}, fmt.Errorf("unknown variance %q", s)
}

// fromSexp reads a type; binders are the aliases of the enclosing mus,
// innermost last.
func (o *oracleTypes) fromSexp(x sexp, binders []TypeId) (TypeId, error) {
	a := o.arena
	if !x.isList {
		switch x.atom {
		case "int":
			return TidInt, nil
		case "str":
			return TidStr, nil
		case "bool":
			return TidBool, nil
		case "bot":
			return TidBottom, nil
		case "top":
			return TidUnknown, nil
		}
		return TidNothing, fmt.Errorf("unknown type %q", x.atom)
	}
	if len(x.list) == 0 || x.list[0].isList {
		return TidNothing, fmt.Errorf("bad type %s", x)
	}
	args := x.list[1:]
	one := func() (TypeId, error) {
		if len(args) != 1 {
			return TidNothing, fmt.Errorf("expected one argument in %s", x)
		}
		return o.fromSexp(args[0], binders)
	}
	switch x.list[0].atom {
	case "maybe":
		t, err := one()
		if err != nil {
			return TidNothing, err
		}
		return a.MakeMaybe(t), nil
	case "list":
		t, err := one()
		if err != nil {
			return TidNothing, err
		}
		return a.MakeList(t), nil
	case "dict":
		t, err := one()
		if err != nil {
			return TidNothing, err
		}
		return a.MakeStrDict(t), nil
	case "rec":
		if len(args) != 2 || !args[0].isList {
			return TidNothing, fmt.Errorf("bad record %s", x)
		}
		var fields []RecordField
		seen := make(map[NameId]bool)
		for _, fx := range args[0].list {
			if !fx.isList || len(fx.list) != 2 || fx.list[0].isList {
				return TidNothing, fmt.Errorf("bad field %s", fx)
			}
			f, err := o.fieldFromSexp(fx.list[1], binders)
			if err != nil {
				return TidNothing, err
			}
			f.Name = o.names.Intern(fx.list[0].atom)
			if seen[f.Name] {
				continue // the first of a repeated label is the one that counts
			}
			seen[f.Name] = true
			fields = append(fields, f)
		}
		rest, err := o.fieldFromSexp(args[1], binders)
		if err != nil {
			return TidNothing, err
		}
		return a.MakeRecord(fields, rest), nil
	case "union":
		members := make([]TypeId, len(args))
		for i, m := range args {
			t, err := o.fromSexp(m, binders)
			if err != nil {
				return TidNothing, err
			}
			members[i] = t
		}
		return a.MakeUnion(members, NameNone), nil
	case "quote":
		if len(args) != 2 || !args[0].isList {
			return TidNothing, fmt.Errorf("bad quote %s", x)
		}
		var sig QuoteSig
		// The oracle writes stacks top first; QuoteSig lists them bottom first.
		for i := len(args[0].list) - 1; i >= 0; i-- {
			t, err := o.fromSexp(args[0].list[i], binders)
			if err != nil {
				return TidNothing, err
			}
			sig.Inputs = append(sig.Inputs, t)
		}
		if !args[1].isList && args[1].atom == "never" {
			sig.Diverges = true
		} else if args[1].isList {
			for i := len(args[1].list) - 1; i >= 0; i-- {
				t, err := o.fromSexp(args[1].list[i], binders)
				if err != nil {
					return TidNothing, err
				}
				sig.Outputs = append(sig.Outputs, t)
			}
		} else {
			return TidNothing, fmt.Errorf("bad quote outputs %s", x)
		}
		return a.MakeQuote(sig), nil
	case "enum":
		if len(args) != 4 || args[0].isList || args[1].isList || !args[2].isList || !args[3].isList {
			return TidNothing, fmt.Errorf("bad enum %s", x)
		}
		key := fmt.Sprintf("%s %s %s", args[0].atom, args[1].atom, args[2])
		idx, ok := o.enums[key]
		if !ok {
			decl := EnumDecl{Name: o.names.Intern(args[0].atom), Immutable: args[1].atom == "imm", Checkable: true}
			for i, p := range args[2].list {
				param, err := parseOracleVariance(p.atom)
				if err != nil {
					return TidNothing, err
				}
				param.Name = o.names.Intern(fmt.Sprintf("p%d", i))
				decl.Params = append(decl.Params, param)
			}
			idx = a.DeclareEnum(decl)
			o.enums[key] = idx
		}
		var targs []TypeId
		for _, ax := range args[3].list {
			t, err := o.fromSexp(ax, binders)
			if err != nil {
				return TidNothing, err
			}
			targs = append(targs, t)
		}
		return a.MakeEnum(idx, targs), nil
	case "var":
		if len(args) != 1 {
			return TidNothing, fmt.Errorf("bad var %s", x)
		}
		return a.MakeRigid(o.names.Intern("v" + args[0].atom)), nil
	case "param":
		i, err := strconv.Atoi(args[0].atom)
		if err != nil {
			return TidNothing, err
		}
		return a.MakeParam(i), nil
	case "mu":
		if len(args) != 1 {
			return TidNothing, fmt.Errorf("bad mu %s", x)
		}
		// A mu whose body refers to an enclosing mu depends on it, so only a
		// closed one is shared by its text.
		key := x.String()
		closed := !mentionsEnclosing(x)
		if id, ok := o.mus[key]; ok && closed {
			return id, nil
		}
		idx := a.DeclareAlias(o.names.Intern(fmt.Sprintf("M%d", len(a.aliases))))
		ref := a.MakeAliasRef(idx)
		if closed {
			o.mus[key] = ref
		}
		body, err := o.fromSexp(args[0], append(binders, ref))
		if err != nil {
			return TidNothing, err
		}
		a.SetAliasBody(idx, body)
		return ref, nil
	case "rv":
		n, err := strconv.Atoi(args[0].atom)
		if err != nil || n >= len(binders) {
			return TidNothing, fmt.Errorf("bad rv %s", x)
		}
		return binders[len(binders)-1-n], nil
	}
	return TidNothing, fmt.Errorf("unknown type form %s", x)
}

// mentionsEnclosing reports whether the mu x refers to a mu around it: some
// (rv n) inside it reaches past x itself.
func mentionsEnclosing(x sexp) bool {
	var walk func(y sexp, depth int) bool
	walk = func(y sexp, depth int) bool {
		if !y.isList || len(y.list) == 0 {
			return false
		}
		if !y.list[0].isList {
			switch y.list[0].atom {
			case "rv":
				n, _ := strconv.Atoi(y.list[1].atom)
				return n >= depth
			case "mu":
				return walk(y.list[1], depth+1)
			}
		}
		for _, z := range y.list[1:] {
			if walk(z, depth) {
				return true
			}
		}
		return false
	}
	return walk(x.list[1], 1)
}

func (o *oracleTypes) fieldFromSexp(x sexp, binders []TypeId) (RecordField, error) {
	if !x.isList {
		switch x.atom {
		case "abs":
			return RecordField{Status: FieldAbsent}, nil
		case "open":
			return RecordField{Status: FieldOpen}, nil
		}
		return RecordField{}, fmt.Errorf("unknown field status %q", x.atom)
	}
	if len(x.list) != 2 || x.list[0].isList {
		return RecordField{}, fmt.Errorf("bad field status %s", x)
	}
	t, err := o.fromSexp(x.list[1], binders)
	if err != nil {
		return RecordField{}, err
	}
	switch x.list[0].atom {
	case "req":
		return RecordField{Status: FieldRequired, Type: t}, nil
	case "opt":
		return RecordField{Status: FieldOptional, Type: t}, nil
	case "dict":
		return RecordField{Status: FieldDeletable, Type: t}, nil
	}
	return RecordField{}, fmt.Errorf("unknown field status %s", x)
}

// print writes t in oracle syntax. Each alias becomes a mu around its body,
// and a reference to an alias being written becomes an rv.
func (o *oracleTypes) print(t TypeId) string {
	var sb strings.Builder
	o.printTo(&sb, t, nil)
	return sb.String()
}

func (o *oracleTypes) printTo(sb *strings.Builder, t TypeId, binders []TypeId) {
	a := o.arena
	switch t {
	case TidInt:
		sb.WriteString("int")
		return
	case TidStr:
		sb.WriteString("str")
		return
	case TidBool:
		sb.WriteString("bool")
		return
	case TidBottom:
		sb.WriteString("bot")
		return
	case TidUnknown:
		sb.WriteString("top")
		return
	}
	n := a.Node(t)
	switch n.Kind {
	case TKMaybe:
		sb.WriteString("(maybe ")
		o.printTo(sb, TypeId(n.A), binders)
		sb.WriteString(")")
	case TKList:
		sb.WriteString("(list ")
		o.printTo(sb, TypeId(n.A), binders)
		sb.WriteString(")")
	case TKRecord:
		rec := a.records[n.Extra]
		if len(rec.Fields) == 0 && rec.Rest.Status == FieldDeletable {
			sb.WriteString("(dict ")
			o.printTo(sb, rec.Rest.Type, binders)
			sb.WriteString(")")
			return
		}
		sb.WriteString("(rec (")
		for i, f := range rec.Fields {
			if i > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString("(" + o.names.Name(f.Name) + " ")
			o.printField(sb, f, binders)
			sb.WriteString(")")
		}
		sb.WriteString(") ")
		o.printField(sb, rec.Rest, binders)
		sb.WriteString(")")
	case TKUnion:
		members := a.unionMembers[n.Extra]
		for i, m := range members {
			if i < len(members)-1 {
				sb.WriteString("(union ")
			}
			o.printTo(sb, m, binders)
			if i < len(members)-1 {
				sb.WriteString(" ")
			}
		}
		sb.WriteString(strings.Repeat(")", len(members)-1))
	case TKQuote:
		sig := a.quoteSigs[n.Extra]
		sb.WriteString("(quote (")
		for i := len(sig.Inputs) - 1; i >= 0; i-- {
			o.printTo(sb, sig.Inputs[i], binders)
			if i > 0 {
				sb.WriteString(" ")
			}
		}
		sb.WriteString(") ")
		if sig.Diverges {
			sb.WriteString("never)")
			return
		}
		sb.WriteString("(")
		for i := len(sig.Outputs) - 1; i >= 0; i-- {
			o.printTo(sb, sig.Outputs[i], binders)
			if i > 0 {
				sb.WriteString(" ")
			}
		}
		sb.WriteString("))")
	case TKEnum:
		decl := a.enumDecls[n.A]
		imm := "mut"
		if decl.Immutable {
			imm = "imm"
		}
		ps := make([]string, len(decl.Params))
		for i, p := range decl.Params {
			ps[i] = [...]string{"co", "contra", "inv"}[p.Variance]
			if p.Fresh {
				ps[i] += "/fresh"
			}
		}
		fmt.Fprintf(sb, "(enum %s %s (%s) (", o.names.Name(decl.Name), imm, strings.Join(ps, " "))
		for i, x := range a.enumArgs[n.Extra] {
			if i > 0 {
				sb.WriteString(" ")
			}
			o.printTo(sb, x, binders)
		}
		sb.WriteString("))")
	case TKRigid:
		name := o.names.Name(NameId(n.A))
		sb.WriteString("(var " + strings.TrimPrefix(name, "v") + ")")
	case TKParam:
		fmt.Fprintf(sb, "(param %d)", n.A)
	case TKAlias:
		for i := len(binders) - 1; i >= 0; i-- {
			if binders[i] == t {
				fmt.Fprintf(sb, "(rv %d)", len(binders)-1-i)
				return
			}
		}
		sb.WriteString("(mu ")
		o.printTo(sb, a.aliases[n.A].Body, append(binders, t))
		sb.WriteString(")")
	default:
		panic(fmt.Sprintf("no oracle syntax for %s", FormatType(a, o.names, t)))
	}
}

func (o *oracleTypes) printField(sb *strings.Builder, f RecordField, binders []TypeId) {
	switch f.Status {
	case FieldAbsent:
		sb.WriteString("abs")
		return
	case FieldOpen:
		sb.WriteString("open")
		return
	case FieldRequired:
		sb.WriteString("(req ")
	case FieldOptional:
		sb.WriteString("(opt ")
	case FieldDeletable:
		sb.WriteString("(dict ")
	}
	o.printTo(sb, f.Type, binders)
	sb.WriteString(")")
}

func (o *oracleTypes) parseSlot(x sexp) (Slot, error) {
	if !x.isList || len(x.list) != 2 || x.list[0].isList {
		return Slot{}, fmt.Errorf("bad slot %s", x)
	}
	t, err := o.fromSexp(x.list[1], nil)
	if err != nil {
		return Slot{}, err
	}
	switch x.list[0].atom {
	case "shared":
		return Slot{Type: t}, nil
	case "fresh":
		return Slot{Type: t, Fresh: true}, nil
	}
	return Slot{}, fmt.Errorf("bad slot %s", x)
}

func (o *oracleTypes) printSlot(s Slot) string {
	mark := "shared"
	if s.Fresh {
		mark = "fresh"
	}
	return "(" + mark + " " + o.print(s.Type) + ")"
}

// answer runs one oracle query line with the Go relations. A sub or rsub
// query gives "yes" or "no"; a join gives the slot, or ok false for none.
func (o *oracleTypes) answer(r *Relations, line string) (yes bool, slot Slot, ok bool, isJoin bool, err error) {
	xs, err := parseSexps(line)
	if err != nil {
		return
	}
	if len(xs) != 4 || xs[0].isList {
		err = fmt.Errorf("bad query %q", line)
		return
	}
	switch xs[0].atom {
	case "sub", "rsub":
		var a, b TypeId
		if a, err = o.fromSexp(xs[2], nil); err != nil {
			return
		}
		if b, err = o.fromSexp(xs[3], nil); err != nil {
			return
		}
		if xs[0].atom == "sub" {
			yes = r.Sub(a, b)
		} else {
			yes = r.Retype(a, b)
		}
		return
	case "join":
		isJoin = true
		var p, q Slot
		if p, err = o.parseSlot(xs[2]); err != nil {
			return
		}
		if q, err = o.parseSlot(xs[3]); err != nil {
			return
		}
		slot, ok = r.JoinSlot(p, q)
		return
	}
	err = fmt.Errorf("bad query %q", line)
	return
}

// agrees reports whether the Go answer to line agrees with the oracle's
// answer text. A join is compared as a type, parsed in the same context as
// the question, so its aliases are the question's.
func (o *oracleTypes) agrees(r *Relations, line, oracleAns string) (bool, string, error) {
	yes, slot, ok, isJoin, err := o.answer(r, line)
	if err != nil {
		return false, "", err
	}
	if !isJoin {
		goAns := "no"
		if yes {
			goAns = "yes"
		}
		return goAns == oracleAns, goAns, nil
	}
	goAns := "none"
	if ok {
		goAns = o.printSlot(slot)
	}
	if oracleAns == "none" || !ok {
		return oracleAns == "none" && !ok, goAns, nil
	}
	xs, err := parseSexps(oracleAns)
	if err != nil || len(xs) != 1 {
		return false, goAns, fmt.Errorf("bad oracle answer %q", oracleAns)
	}
	want, err := o.parseSlot(xs[0])
	if err != nil {
		return false, goAns, err
	}
	return want == slot, goAns, nil
}
