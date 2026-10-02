package main

import (
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The soundness oracle on generated programs (ai/type-system-plan.md,
// stage 7; design doc "Getting confidence"). A generator writes random
// programs, biased toward aliasing: stores, aliases, writes through every
// view, widening with `as`, joins, quotes, loops, matches, tryAs and defs.
// It builds each program one statement at a time and keeps a statement
// only if the whole program still checks, so every program it finishes is
// one the checker accepts, including whatever risky statements got
// through. Then it runs the program, and a type mismatch or a panic is a
// soundness bug: in the checker, in a builtin's table entry, or in the
// runtime's classification of the failure.
//
// Every variable is read back at its static type (consume), deeply, with
// words that fail on a value of the wrong kind, so a retyping the checker
// should have refused shows up as a type mismatch.
//
// MSH_GEN_PROGRAMS sets the number of programs (default 60) and
// MSH_GEN_SEED the first seed (default 1).

// ---- The generator's types ----

type gkind uint8

const (
	gInt gkind = iota
	gFloat
	gStr
	gBool
	gList
	gDict  // {str: T}
	gShape // {a: T, b?: U}, a written (open) shape
	gMaybe
	gUnion
	gBox // the enum Box[a] = box [a] | empty
	gJson
	gQuote // ( -- )
)

type gfield struct {
	name string
	t    *gty
	opt  bool
}

type gty struct {
	k       gkind
	elem    *gty
	fields  []gfield // sorted by name
	members []*gty
	s       string
}

func (t *gty) src() string {
	if t.s != "" {
		return t.s
	}
	switch t.k {
	case gInt:
		t.s = "int"
	case gFloat:
		t.s = "float"
	case gStr:
		t.s = "str"
	case gBool:
		t.s = "bool"
	case gList:
		t.s = "[" + t.elem.src() + "]"
	case gDict:
		t.s = "{str: " + t.elem.src() + "}"
	case gShape:
		parts := make([]string, len(t.fields))
		for i, f := range t.fields {
			q := ""
			if f.opt {
				q = "?"
			}
			parts[i] = f.name + q + ": " + f.t.src()
		}
		t.s = "{" + strings.Join(parts, ", ") + "}"
	case gMaybe:
		t.s = "Maybe[" + t.elem.src() + "]"
	case gUnion:
		parts := make([]string, len(t.members))
		for i, m := range t.members {
			parts[i] = m.src()
		}
		t.s = strings.Join(parts, " | ")
	case gBox:
		t.s = "Box[" + t.elem.src() + "]"
	case gJson:
		t.s = "Json"
	case gQuote:
		t.s = "( -- )"
	}
	return t.s
}

// kindClass is the runtime kind of a type that is not a union.
func (t *gty) kindClass() string {
	switch t.k {
	case gInt:
		return "int"
	case gFloat:
		return "float"
	case gStr:
		return "str"
	case gBool:
		return "bool"
	case gList:
		return "list"
	case gDict, gShape:
		return "dict"
	case gMaybe:
		return "maybe"
	case gBox:
		return "Box"
	case gQuote:
		return "quotation"
	}
	return ""
}

var (
	tInt   = &gty{k: gInt}
	tFloat = &gty{k: gFloat}
	tStr   = &gty{k: gStr}
	tBool  = &gty{k: gBool}
	tJson  = &gty{k: gJson}
	tQuote = &gty{k: gQuote}
	gPrims = []*gty{tInt, tFloat, tStr, tBool}
)

func listOf(t *gty) *gty  { return &gty{k: gList, elem: t} }
func dictOf(t *gty) *gty  { return &gty{k: gDict, elem: t} }
func maybeOf(t *gty) *gty { return &gty{k: gMaybe, elem: t} }
func boxOf(t *gty) *gty   { return &gty{k: gBox, elem: t} }
func shapeOf(fs ...gfield) *gty {
	fs = append([]gfield(nil), fs...)
	sort.Slice(fs, func(i, j int) bool { return fs[i].name < fs[j].name })
	return &gty{k: gShape, fields: fs}
}

// unionOf joins types of distinct kinds; nil if two share a kind.
func unionOf(ts ...*gty) *gty {
	var members []*gty
	seen := map[string]bool{}
	for _, t := range ts {
		ms := []*gty{t}
		if t.k == gUnion {
			ms = t.members
		}
		for _, m := range ms {
			if seen[m.kindClass()] {
				if m.k == gUnion || !containsSrc(members, m) {
					return nil
				}
				continue
			}
			seen[m.kindClass()] = true
			members = append(members, m)
		}
	}
	if len(members) == 1 {
		return members[0]
	}
	sort.Slice(members, func(i, j int) bool { return members[i].src() < members[j].src() })
	return &gty{k: gUnion, members: members}
}

func containsSrc(ts []*gty, t *gty) bool {
	for _, x := range ts {
		if x.src() == t.src() {
			return true
		}
	}
	return false
}

// immutable is the design's immutability: no list or dict inside.
func (t *gty) immutable() bool {
	switch t.k {
	case gInt, gFloat, gStr, gBool, gQuote:
		return true
	case gMaybe:
		return t.elem.immutable()
	case gUnion:
		for _, m := range t.members {
			if !m.immutable() {
				return false
			}
		}
		return true
	}
	return false
}

// ---- The program tree ----

// gnode is one statement: text, or a compound with blocks between its
// parts (parts[0] blocks[0] parts[1] ... parts[n]).
type gnode struct {
	text   string
	parts  []string
	blocks []*gblock
}

type gblock struct{ nodes []*gnode }

func (b *gblock) render(w *strings.Builder, indent string) {
	for _, n := range b.nodes {
		w.WriteString(indent)
		if n.blocks == nil {
			w.WriteString(n.text)
			w.WriteByte('\n')
			continue
		}
		for i, part := range n.parts {
			w.WriteString(part)
			if i < len(n.blocks) {
				w.WriteByte('\n')
				n.blocks[i].render(w, indent+"    ")
				w.WriteString(indent)
			}
		}
		w.WriteByte('\n')
	}
}

// ---- Scopes ----

type gvar struct {
	name string
	t    *gty
	set  bool // definitely set at this point
	// counter marks a loop counter, which no statement stores to: the
	// loop would never end.
	counter bool
}

type gscope struct {
	vars []*gvar
}

func (s *gscope) add(name string, t *gty) *gvar {
	v := &gvar{name: name, t: t, set: true}
	s.vars = append(s.vars, v)
	return v
}

// dasnap is what a block may change: the variables unset before it, and
// how many there were.
type dasnap struct {
	n     int
	unset []*gvar
}

func (s *gscope) snap() dasnap {
	d := dasnap{n: len(s.vars)}
	for _, v := range s.vars {
		if !v.set {
			d.unset = append(d.unset, v)
		}
	}
	return d
}

// restore makes every variable set inside a block that may not run, or
// may run later, unset again.
func (s *gscope) restore(d dasnap) {
	for _, v := range d.unset {
		v.set = false
	}
	for _, v := range s.vars[d.n:] {
		v.set = false
	}
}

type gdef struct {
	name      string
	ins, outs []*gty
}

type gctx struct {
	sc    *gscope
	loop  bool // break and continue are allowed
	depth int
}

// ---- The generator ----

type progGen struct {
	rng    *rand.Rand
	base   *CoreBase
	risk   float64
	header string
	defs   []*gnode
	sigs   []gdef
	main   *gblock
	names  int
	checks int
	// accepted and rejected count statements by risk.
	safeOK, safeNo, riskyOK, riskyNo int
	// last is the most recent rendering.
	last string
}

const genHeader = "enum Box[a] = box [a] | empty end\n"

func (p *progGen) render() string {
	var w strings.Builder
	w.WriteString(p.header)
	for _, d := range p.defs {
		(&gblock{nodes: []*gnode{d}}).render(&w, "")
	}
	p.main.render(&w, "")
	return w.String()
}

// ok reports whether the program as it stands checks.
func (p *progGen) ok() bool {
	p.checks++
	src := p.render()
	p.last = src
	file, err := parseMShellInput(src, &TokenFile{"gen.msh"})
	if err != nil {
		return false
	}
	_, ok := p.base.Check(file)
	return ok
}

func (p *progGen) name(prefix string) string {
	p.names++
	return prefix + strconv.Itoa(p.names)
}

func (p *progGen) chance(x float64) bool { return p.rng.Float64() < x }

func (p *progGen) risky() bool { return p.chance(p.risk) }

// try appends a statement to blk and keeps it if the program checks.
func (p *progGen) try(blk *gblock, text string, risky bool, apply func()) bool {
	blk.nodes = append(blk.nodes, &gnode{text: text})
	if p.ok() {
		if apply != nil {
			apply()
		}
		p.count(risky, true)
		return true
	}
	blk.nodes = blk.nodes[:len(blk.nodes)-1]
	p.count(risky, false)
	return false
}

func (p *progGen) count(risky, ok bool) {
	switch {
	case risky && ok:
		p.riskyOK++
	case risky:
		p.riskyNo++
	case ok:
		p.safeOK++
	default:
		p.safeNo++
	}
}

// compound appends a compound statement with empty blocks, and returns it
// if the program checks.
func (p *progGen) compound(blk *gblock, parts ...string) *gnode {
	n := &gnode{parts: parts, blocks: make([]*gblock, len(parts)-1)}
	for i := range n.blocks {
		n.blocks[i] = &gblock{}
	}
	blk.nodes = append(blk.nodes, n)
	if p.ok() {
		p.count(false, true)
		return n
	}
	blk.nodes = blk.nodes[:len(blk.nodes)-1]
	p.count(false, false)
	return nil
}

// ---- Types ----

func (p *progGen) randType(depth int) *gty {
	if depth <= 0 {
		return gPrims[p.rng.Intn(len(gPrims))]
	}
	switch r := p.rng.Intn(100); {
	case r < 30:
		return gPrims[p.rng.Intn(len(gPrims))]
	case r < 52:
		return listOf(p.randType(depth - 1))
	case r < 60:
		return dictOf(p.randType(depth - 1))
	case r < 72:
		return p.randShape(depth - 1)
	case r < 82:
		return maybeOf(p.randType(depth - 1))
	case r < 92:
		for {
			if u := unionOf(p.randType(depth-1), p.randType(depth-1)); u != nil && u.k == gUnion {
				return u
			}
		}
	default:
		return boxOf(p.randType(depth - 1))
	}
}

var gFieldNames = []string{"a", "b", "c", "d"}

func (p *progGen) randShape(depth int) *gty {
	n := 1 + p.rng.Intn(3)
	fs := make([]gfield, n)
	for i := range fs {
		fs[i] = gfield{name: gFieldNames[i], t: p.randType(depth), opt: i > 0 && p.chance(0.4)}
	}
	return shapeOf(fs...)
}

// widen gives a type wider than t, in a way that is sound for a new value
// and not for a shared one with a list or dict in it.
func (p *progGen) widen(t *gty) *gty {
	switch t.k {
	case gInt, gFloat, gStr, gBool:
		for {
			if u := unionOf(t, gPrims[p.rng.Intn(len(gPrims))]); u != nil && u.k == gUnion {
				return u
			}
		}
	case gList:
		return listOf(p.widen(t.elem))
	case gDict:
		return dictOf(p.widen(t.elem))
	case gShape:
		fs := append([]gfield(nil), t.fields...)
		if len(fs) < len(gFieldNames) && p.chance(0.5) {
			fs = append(fs, gfield{name: gFieldNames[len(fs)], t: p.randType(1), opt: true})
		} else {
			i := p.rng.Intn(len(fs))
			fs[i].t = p.widen(fs[i].t)
		}
		return shapeOf(fs...)
	case gMaybe:
		return maybeOf(p.widen(t.elem))
	case gBox:
		return boxOf(p.widen(t.elem))
	case gUnion:
		for i := 0; i < 8; i++ {
			if u := unionOf(t, p.randType(1)); u != nil && u.src() != t.src() {
				return u
			}
		}
		ms := append([]*gty(nil), t.members...)
		i := p.rng.Intn(len(ms))
		ms[i] = p.widen(ms[i])
		if u := unionOf(ms...); u != nil {
			return u
		}
	}
	return t
}

// ---- Expressions ----

var gStrs = []string{`""`, `"a"`, `"hello"`, `"12"`, `"x y"`, `"é"`}

func (p *progGen) varsOf(ctx *gctx, f func(v *gvar) bool) []*gvar {
	var out []*gvar
	for _, v := range ctx.sc.vars {
		if v.set && f(v) {
			out = append(out, v)
		}
	}
	return out
}

func (p *progGen) pickVar(ctx *gctx, f func(v *gvar) bool) *gvar {
	vs := p.varsOf(ctx, f)
	if len(vs) == 0 {
		return nil
	}
	return vs[p.rng.Intn(len(vs))]
}

func sameType(t *gty) func(v *gvar) bool {
	return func(v *gvar) bool { return v.t.src() == t.src() }
}

// expr writes code that pushes a value of type t: a literal, a stored
// value, a copy, or a value computed from stored ones.
func (p *progGen) expr(ctx *gctx, t *gty, depth int) string {
	if depth > 0 && p.chance(0.35) {
		if v := p.pickVar(ctx, sameType(t)); v != nil {
			if !t.immutable() && p.chance(0.2) {
				return "@" + v.name + " deepCopy"
			}
			return "@" + v.name
		}
	}
	switch t.k {
	case gInt:
		switch r := p.rng.Intn(10); {
		case r < 1 && depth > 0:
			if l := p.pickVar(ctx, func(v *gvar) bool { return v.t.k == gList }); l != nil {
				return "@" + l.name + " len"
			}
		case r < 2 && depth > 0:
			return p.expr(ctx, tInt, depth-1) + " " + p.expr(ctx, tInt, depth-1) + " +"
		}
		return strconv.Itoa(p.rng.Intn(20) - 5)
	case gFloat:
		return strconv.Itoa(p.rng.Intn(9)) + "." + strconv.Itoa(p.rng.Intn(10))
	case gStr:
		if depth > 0 && p.chance(0.15) {
			return p.expr(ctx, tInt, depth-1) + " str"
		}
		return gStrs[p.rng.Intn(len(gStrs))]
	case gBool:
		if depth > 0 && p.chance(0.3) {
			return p.expr(ctx, tInt, depth-1) + " " + p.expr(ctx, tInt, depth-1) + " <"
		}
		if p.chance(0.5) {
			return "true"
		}
		return "false"
	case gList:
		if depth > 0 && p.chance(0.15) {
			if l := p.pickVar(ctx, func(v *gvar) bool { return v.t.k == gList }); l != nil {
				// A new list from a stored one.
				return "@" + l.name + " (drop " + p.expr(ctx, t.elem, depth-1) + ") map"
			}
		}
		if depth > 0 && p.chance(0.1) {
			if l := p.pickVar(ctx, sameType(t)); l != nil {
				return "@" + l.name + " " + p.pick([]string{"1 take", "1 skip", "reverse", ":1", "(drop true) filter"})
			}
		}
		n := p.rng.Intn(3)
		if depth <= 0 {
			n = 0
		}
		parts := make([]string, n)
		for i := range parts {
			parts[i] = p.expr(ctx, t.elem, depth-1)
		}
		return "[" + strings.Join(parts, " ") + "]"
	case gDict:
		if depth <= 0 || p.chance(0.3) {
			return "{}"
		}
		n := 1 + p.rng.Intn(2)
		parts := make([]string, n)
		for i := range parts {
			parts[i] = "k" + strconv.Itoa(i) + ": " + p.expr(ctx, t.elem, depth-1)
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case gShape:
		var parts []string
		for _, f := range t.fields {
			if f.opt && (depth <= 0 || p.chance(0.5)) {
				continue
			}
			parts = append(parts, f.name+": "+p.expr(ctx, f.t, depth-1))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case gMaybe:
		if depth <= 0 || p.chance(0.3) {
			return "none"
		}
		return p.expr(ctx, t.elem, depth-1) + " just"
	case gUnion:
		return p.expr(ctx, t.members[p.rng.Intn(len(t.members))], depth)
	case gBox:
		if depth <= 0 || p.chance(0.3) {
			return "empty"
		}
		return p.expr(ctx, listOf(t.elem), depth-1) + " box"
	case gJson:
		return p.pick([]string{`"[1, 2]"`, `"{\"a\": 1}"`, `"5"`, `"[[1], [\"x\"]]"`, `"{\"a\": [1, 2], \"b\": \"s\"}"`}) + " parseJson"
	case gQuote:
		return "( )"
	}
	return "0"
}

func (p *progGen) pick(xs []string) string { return xs[p.rng.Intn(len(xs))] }

// consume writes code that takes a value of type t and reads all of it at
// that type, with words that fail on a value of another kind.
func (p *progGen) consume(ctx *gctx, t *gty, depth int) string {
	switch t.k {
	case gInt:
		return "1 + drop"
	case gFloat:
		return "1.0 + drop"
	case gStr:
		return `"" + drop`
	case gBool:
		return "not drop"
	case gList:
		if depth <= 0 {
			return "len drop"
		}
		return "(" + p.consume(ctx, t.elem, depth-1) + ") each"
	case gDict:
		if depth <= 0 {
			return "keys drop"
		}
		return "values (" + p.consume(ctx, t.elem, depth-1) + ") each"
	case gShape:
		var b strings.Builder
		for _, f := range t.fields {
			if f.opt {
				fmt.Fprintf(&b, "dup :%s %s ", f.name, p.consume(ctx, maybeOf(f.t), depth-1))
			} else {
				fmt.Fprintf(&b, "dup :%s? %s ", f.name, p.consume(ctx, f.t, depth-1))
			}
		}
		b.WriteString("drop")
		return b.String()
	case gMaybe:
		return "dup isNone if drop else ? " + p.consume(ctx, t.elem, depth-1) + " end"
	case gUnion:
		var b strings.Builder
		b.WriteString("match ")
		for _, m := range t.members {
			pat := m.kindClass()
			fmt.Fprintf(&b, "%s :> %s, ", pat, p.consume(ctx, m, depth-1))
		}
		b.WriteString("end")
		return b.String()
	case gBox:
		n := p.name("cb")
		ctx.sc.add(n, listOf(t.elem)).set = false
		return "match box " + n + " : @" + n + " " + p.consume(ctx, listOf(t.elem), depth-1) + ", empty : , end"
	}
	return "drop"
}

// ---- Statements ----

// stmt adds one statement to blk: it tries a few kinds and reports
// whether one checked.
func (p *progGen) stmt(ctx *gctx, blk *gblock) bool {
	for attempt := 0; attempt < 6; attempt++ {
		if p.stmtOnce(ctx, blk) {
			return true
		}
	}
	return false
}

func (p *progGen) stmtOnce(ctx *gctx, blk *gblock) bool {
	sc := ctx.sc
	r := p.rng.Intn(100)
	if ctx.depth >= 3 && r >= 60 {
		r = p.rng.Intn(60)
	}
	switch {
	case r < 14: // a new variable
		t := p.randType(2)
		n := p.name("v")
		return p.try(blk, p.expr(ctx, t, 2)+" as "+t.src()+" "+n+"!", false, func() { sc.add(n, t) })
	case r < 20: // store again, at its type or (risky) another
		v := p.pickAny(ctx)
		if v == nil {
			return false
		}
		if p.risky() {
			w := p.widen(v.t)
			val := p.outside(ctx, w, v.t)
			if val == "" {
				val = p.expr(ctx, w, 2)
			}
			if !p.try(blk, val+" as "+w.src()+" "+v.name+"!", true, nil) {
				return false
			}
			p.try(blk, "@"+v.name+" "+p.consume(ctx, v.t, 4), false, nil)
			return true
		}
		return p.try(blk, p.expr(ctx, v.t, 2)+" as "+v.t.src()+" "+v.name+"!", false, func() { v.set = true })
	case r < 28: // an alias, widened (risky), or a widened copy
		v := p.pickVar(ctx, func(*gvar) bool { return true })
		if v == nil {
			return false
		}
		n := p.name("v")
		switch {
		case p.risky():
			w := p.widen(v.t)
			if !p.try(blk, "@"+v.name+" as "+w.src()+" "+n+"!", true, func() { sc.add(n, w) }) {
				return false
			}
			p.exploit(ctx, blk, "@"+n, w, v.t, v)
			return true
		case p.chance(0.4):
			w := p.widen(v.t)
			return p.try(blk, "@"+v.name+" deepCopy as "+w.src()+" "+n+"!", false, func() { sc.add(n, w) })
		}
		return p.try(blk, "@"+v.name+" "+n+"!", false, func() { sc.add(n, v.t) })
	case r < 31: // two names for one new value, one of them widened
		t := p.randType(2)
		w := p.widen(t)
		a, b := p.name("v"), p.name("v")
		var bv *gvar
		if !p.try(blk, p.expr(ctx, t, 2)+" as "+t.src()+" dup as "+w.src()+" "+a+"! "+b+"!", true,
			func() { sc.add(a, w); bv = sc.add(b, t) }) {
			return false
		}
		p.exploit(ctx, blk, "@"+a, w, t, bv)
		return true
	case r < 46:
		return p.write(ctx, blk)
	case r < 54: // read a variable back
		v := p.pickVar(ctx, func(*gvar) bool { return true })
		if v == nil {
			return false
		}
		return p.try(blk, "@"+v.name+" "+p.consume(ctx, v.t, 3), false, nil)
	case r < 58: // a join
		return p.join(ctx, blk)
	case r < 61: // a partly new dict around a stored value
		v := p.pickVar(ctx, func(v *gvar) bool { return !v.t.immutable() })
		if v == nil {
			return false
		}
		vt := v.t
		risky := p.risky()
		if risky {
			vt = p.widen(v.t)
		}
		t := shapeOf(gfield{name: "a", t: vt}, gfield{name: "b", t: tInt}, gfield{name: "c", t: tStr, opt: true})
		n := p.name("v")
		if !p.try(blk, "{a: @"+v.name+", b: 1} as "+t.src()+" "+n+"!", risky, func() { sc.add(n, t) }) {
			return false
		}
		if risky {
			p.exploit(ctx, blk, "@"+n+" :a?", vt, v.t, v)
		}
		return true
	case r < 64: // validate
		return p.validate(ctx, blk)
	case r < 67:
		return p.callDef(ctx, blk)
	case r < 70: // a new list from a stored one
		v := p.pickVar(ctx, func(v *gvar) bool { return v.t.k == gList })
		if v == nil {
			return false
		}
		n := p.name("v")
		op := p.pick([]string{"1 take", "1 skip", "reverse", ":1", "1:", "(drop true) filter", "(drop false) filter"})
		t := v.t
		risky := false
		if p.risky() {
			t, risky = p.widen(v.t), true
		}
		if !p.try(blk, "@"+v.name+" "+op+" as "+t.src()+" "+n+"!", risky, func() { sc.add(n, t) }) {
			return false
		}
		if risky {
			p.exploit(ctx, blk, "@"+n, t, v.t, v)
		}
		return true
	case r < 72 && ctx.loop:
		return p.try(blk, p.expr(ctx, tBool, 1)+" if "+p.pick([]string{"break", "continue"})+" end", false, nil)
	case r < 78:
		return p.ifStmt(ctx, blk)
	case r < 83:
		return p.eachStmt(ctx, blk)
	case r < 86:
		return p.loopStmt(ctx, blk)
	case r < 91:
		return p.quoteStmt(ctx, blk)
	default:
		return p.matchStmt(ctx, blk)
	}
}

func (p *progGen) pickAny(ctx *gctx) *gvar {
	if len(ctx.sc.vars) == 0 {
		return nil
	}
	v := ctx.sc.vars[p.rng.Intn(len(ctx.sc.vars))]
	if v.t.k == gQuote || v.counter {
		return nil
	}
	return v
}

// write changes a stored value in place, through its variable, at its
// element type or (risky) a wider one.
func (p *progGen) write(ctx *gctx, blk *gblock) bool {
	v := p.pickVar(ctx, func(v *gvar) bool {
		switch v.t.k {
		case gList, gDict, gShape, gMaybe, gUnion, gBox:
			return true
		}
		return false
	})
	if v == nil {
		return false
	}
	risky := p.risky()
	val := func(t *gty) string {
		if risky {
			t = p.widen(t)
		}
		return p.expr(ctx, t, 2)
	}
	at := "@" + v.name
	switch v.t.k {
	case gList:
		e := v.t.elem
		switch p.rng.Intn(6) {
		case 0:
			return p.try(blk, at+" "+val(e)+" 0 insert drop", risky, nil)
		case 1:
			return p.try(blk, at+" len 0 > if "+at+" "+val(e)+" 0 setAt drop end", risky, nil)
		case 2:
			return p.try(blk, at+" len 0 > if "+at+" 0 del drop end", false, nil)
		case 3:
			// A literal: a stored list may be this one, and extending a
			// list with itself in a loop doubles it each time.
			return p.try(blk, at+" ["+val(e)+"] extend drop", risky, nil)
		case 4:
			return p.try(blk, at+" pop drop", false, nil)
		}
		return p.try(blk, at+" "+val(e)+" append drop", risky, nil)
	case gDict:
		k := `"k` + strconv.Itoa(p.rng.Intn(3)) + `"`
		switch p.rng.Intn(4) {
		case 0:
			return p.try(blk, at+" "+k+" "+val(v.t.elem)+" setd", risky, nil)
		case 1:
			return p.try(blk, at+" "+k+" del drop", false, nil)
		case 2:
			// A key known only at run time.
			return p.try(blk, at+" "+k+` "" + `+val(v.t.elem)+" set drop", risky, nil)
		}
		return p.try(blk, at+" "+k+" "+val(v.t.elem)+" set drop", risky, nil)
	case gShape:
		if risky && p.chance(0.3) {
			// A key the shape does not declare.
			return p.try(blk, at+` "z" 1 set drop`, true, nil)
		}
		f := v.t.fields[p.rng.Intn(len(v.t.fields))]
		return p.try(blk, at+` "`+f.name+`" `+val(f.t)+" set drop", risky, nil)
	case gMaybe, gBox, gUnion:
		// Write into the list inside, if there is one.
		inner, path := p.innerList(v.t)
		if inner == nil {
			return false
		}
		n := p.name("w")
		code := at + " " + strings.ReplaceAll(path, "$", n) + " " + val(inner.elem) + " append drop" + strings.Repeat(", _ : , end", strings.Count(path, "match"))
		return p.try(blk, code, risky, func() { ctx.sc.add(n, inner).set = false })
	}
	return false
}

// innerList finds a list inside a Maybe, Box or union, and the pattern
// code that reaches it with the binding `$`.
func (p *progGen) innerList(t *gty) (*gty, string) {
	switch t.k {
	case gMaybe:
		if t.elem.k == gList {
			return t.elem, "match just $ : @$"
		}
	case gBox:
		return listOf(t.elem), "match box $ : @$"
	case gUnion:
		for _, m := range t.members {
			if m.k == gList {
				return m, "match list $ : @$"
			}
		}
	}
	return nil, ""
}

func (p *progGen) join(ctx *gctx, blk *gblock) bool {
	sc := ctx.sc
	n := p.name("v")
	cond := p.expr(ctx, tBool, 1)
	if p.risky() {
		// Two stored values of the same kind and different types.
		a := p.pickVar(ctx, func(v *gvar) bool { return !v.t.immutable() })
		if a == nil {
			return false
		}
		b := p.pickVar(ctx, func(v *gvar) bool { return v.t.kindClass() == a.t.kindClass() && v.t.src() != a.t.src() })
		if b == nil {
			return false
		}
		if !p.try(blk, cond+" if @"+a.name+" else @"+b.name+" end "+n+"!", true, nil) {
			return false
		}
		p.try(blk, "@"+a.name+" "+p.consume(ctx, a.t, 4), false, nil)
		p.try(blk, "@"+b.name+" "+p.consume(ctx, b.t, 4), false, nil)
		return true
	}
	t := p.randType(2)
	var a, b *gty = t, t
	if t.k == gUnion {
		a, b = t.members[0], t.members[1]
	}
	return p.try(blk, cond+" if "+p.expr(ctx, a, 2)+" else "+p.expr(ctx, b, 2)+" end as "+t.src()+" "+n+"!", false,
		func() { sc.add(n, t) })
}

// validate runs tryAs or an `is` arm on a stored or a new value.
func (p *progGen) validate(ctx *gctx, blk *gblock) bool {
	sc := ctx.sc
	var subject string
	var t *gty
	var origin *gvar
	var jsonTarget *gty
	if v := p.pickVar(ctx, func(v *gvar) bool { return v.t.k != gQuote }); v != nil && p.chance(0.6) {
		subject, t, origin = "@"+v.name, v.t, v
	} else if p.chance(0.5) {
		j := gJsonTexts[p.rng.Intn(len(gJsonTexts))]
		subject, t, jsonTarget = mshStr(j.text)+" parseJson", tJson, j.t
	} else {
		t = p.randType(2)
		subject = p.expr(ctx, t, 2)
	}
	target := t
	risky := false
	if t.k == gJson || p.chance(0.5) {
		target, risky = p.randType(2), true
		if t.k != gJson && p.chance(0.5) {
			target = p.widen(t)
		}
	}
	if jsonTarget != nil && p.chance(0.7) {
		target = jsonTarget
	}
	if target.k == gJson {
		target = listOf(tInt)
	}
	n := p.name("t")
	snap := sc.snap()
	var node *gnode
	if p.chance(0.5) {
		node = p.compound(blk, subject+" tryAs "+target.src()+" match just "+n+" : ", ", none : , end")
	} else {
		node = p.compound(blk, subject+" match is "+target.src()+" "+n+" : ", ", _ : , end")
	}
	if node == nil {
		return false
	}
	p.count(risky, true)
	sc.add(n, target)
	inner := &gctx{sc: sc, loop: ctx.loop, depth: ctx.depth + 1}
	if origin != nil && target.src() != origin.t.src() {
		p.exploit(inner, node.blocks[0], "@"+n, target, origin.t, origin)
	}
	p.fill(inner, node.blocks[0], 1+p.rng.Intn(3))
	sc.restore(snap)
	return true
}

func (p *progGen) ifStmt(ctx *gctx, blk *gblock) bool {
	node := p.compound(blk, p.expr(ctx, tBool, 1)+" if", "else", "end")
	if node == nil {
		return false
	}
	inner := &gctx{sc: ctx.sc, loop: ctx.loop, depth: ctx.depth + 1}
	for _, b := range node.blocks {
		snap := ctx.sc.snap()
		p.fill(inner, b, 1+p.rng.Intn(3))
		ctx.sc.restore(snap)
	}
	return true
}

func (p *progGen) eachStmt(ctx *gctx, blk *gblock) bool {
	v := p.pickVar(ctx, func(v *gvar) bool { return v.t.k == gList })
	if v == nil {
		return false
	}
	e := p.name("e")
	snap := ctx.sc.snap()
	node := p.compound(blk, "@"+v.name+" ("+e+"!", ") each")
	if node == nil {
		return false
	}
	ctx.sc.add(e, v.t.elem)
	// A literal given to each may break the enclosing loop.
	inner := &gctx{sc: ctx.sc, loop: ctx.loop, depth: ctx.depth + 1}
	p.fill(inner, node.blocks[0], 1+p.rng.Intn(3))
	ctx.sc.restore(snap)
	return true
}

func (p *progGen) loopStmt(ctx *gctx, blk *gblock) bool {
	c := p.name("c")
	snap := ctx.sc.snap()
	node := p.compound(blk, "0 "+c+"! ( @"+c+" 2 >= if break end @"+c+" 1 + "+c+"!", ") loop")
	if node == nil {
		return false
	}
	ctx.sc.add(c, tInt).counter = true
	inner := &gctx{sc: ctx.sc, loop: true, depth: ctx.depth + 1}
	p.fill(inner, node.blocks[0], 1+p.rng.Intn(3))
	ctx.sc.restore(snap)
	// The counter is set before the loop.
	for _, v := range ctx.sc.vars {
		if v.name == c {
			v.set = true
		}
	}
	return true
}

// quoteStmt stores a quote literal, and later statements run it.
func (p *progGen) quoteStmt(ctx *gctx, blk *gblock) bool {
	if q := p.pickVar(ctx, func(v *gvar) bool { return v.t.k == gQuote }); q != nil && p.chance(0.5) {
		return p.try(blk, "@"+q.name+" x", false, nil)
	}
	n := p.name("q")
	snap := ctx.sc.snap()
	node := p.compound(blk, "(", ") "+n+"!")
	if node == nil {
		return false
	}
	inner := &gctx{sc: ctx.sc, depth: ctx.depth + 1}
	p.fill(inner, node.blocks[0], 1+p.rng.Intn(3))
	ctx.sc.restore(snap)
	ctx.sc.add(n, tQuote)
	return true
}

// matchStmt takes a union, Maybe or Box apart, with bindings or `:>`.
func (p *progGen) matchStmt(ctx *gctx, blk *gblock) bool {
	v := p.pickVar(ctx, func(v *gvar) bool { return v.t.k == gUnion || v.t.k == gMaybe || v.t.k == gBox })
	if v == nil {
		return false
	}
	type arm struct {
		pat  string
		bind *gty
	}
	var arms []arm
	switch v.t.k {
	case gUnion:
		for _, m := range v.t.members {
			arms = append(arms, arm{m.kindClass(), m})
		}
	case gMaybe:
		arms = []arm{{"just", v.t.elem}, {"none", nil}}
	case gBox:
		arms = []arm{{"box", listOf(v.t.elem)}, {"empty", nil}}
	}
	parts := []string{"@" + v.name + " match "}
	binds := make([]string, len(arms))
	for i, a := range arms {
		pat := a.pat
		if a.bind != nil {
			binds[i] = p.name("m")
			pat += " " + binds[i]
		}
		if i > 0 {
			parts[len(parts)-1] += ", "
		}
		parts[len(parts)-1] += pat + " :"
		parts = append(parts, "")
	}
	parts[len(parts)-1] = ", end"
	snap := ctx.sc.snap()
	node := p.compound(blk, parts...)
	if node == nil {
		return false
	}
	inner := &gctx{sc: ctx.sc, loop: ctx.loop, depth: ctx.depth + 1}
	for i, a := range arms {
		armSnap := ctx.sc.snap()
		if a.bind != nil {
			ctx.sc.add(binds[i], a.bind)
		}
		p.fill(inner, node.blocks[i], 1+p.rng.Intn(2))
		ctx.sc.restore(armSnap)
	}
	ctx.sc.restore(snap)
	return true
}

// callDef calls a def with arguments of its parameter types (or, risky,
// stored values of a narrower type), and stores what it returns.
func (p *progGen) callDef(ctx *gctx, blk *gblock) bool {
	if len(p.sigs) == 0 {
		return false
	}
	d := p.sigs[p.rng.Intn(len(p.sigs))]
	var code strings.Builder
	risky := false
	var passed []*gvar
	for _, in := range d.ins {
		if v := p.pickVar(ctx, func(v *gvar) bool { return v.t.kindClass() == in.kindClass() && v.t.src() != in.src() }); v != nil && p.risky() {
			code.WriteString("@" + v.name + " ")
			risky = true
			passed = append(passed, v)
			continue
		}
		code.WriteString(p.expr(ctx, in, 2) + " ")
	}
	code.WriteString(d.name)
	names := make([]string, len(d.outs))
	for i := len(d.outs) - 1; i >= 0; i-- {
		names[i] = p.name("r")
		code.WriteString(" as " + d.outs[i].src() + " " + names[i] + "!")
	}
	if !p.try(blk, code.String(), risky, func() {
		for i, o := range d.outs {
			ctx.sc.add(names[i], o)
		}
	}) {
		return false
	}
	for _, v := range passed {
		p.try(blk, "@"+v.name+" "+p.consume(ctx, v.t, 4), false, nil)
	}
	return true
}

// gJsonTexts are JSON texts with a type each one has, so a tryAs on
// parseJson's result often succeeds.
var gJsonTexts = []struct {
	text string
	t    *gty
}{
	{"[1, 2]", listOf(tInt)},
	{"[[1], [2, 3]]", listOf(listOf(tInt))},
	{`{"a": 1, "b": 2}`, dictOf(tInt)},
	{`{"a": [1], "b": [2]}`, dictOf(listOf(tInt))},
	{`{"a": 1, "b": "s"}`, shapeOf(gfield{name: "a", t: tInt}, gfield{name: "b", t: tStr})},
	{`{"a": [1, "x"]}`, shapeOf(gfield{name: "a", t: listOf(unionOf(tInt, tStr))})},
	{`[1, "x", 2]`, listOf(unionOf(tInt, tStr))},
	{`["x", "y"]`, listOf(tStr)},
	{"5", tInt},
	{"2.5", tFloat},
}

// outside writes a value of type w that is not a value of type t, where
// w is t widened. "" when there is none (an added optional field).
func (p *progGen) outside(ctx *gctx, w, t *gty) string {
	if w.src() == t.src() {
		return ""
	}
	byKind := func(k string) *gty {
		for _, m := range unionMembers(t) {
			if m.kindClass() == k {
				return m
			}
		}
		return nil
	}
	switch w.k {
	case gUnion:
		for _, m := range w.members {
			if byKind(m.kindClass()) == nil {
				return p.expr(&gctx{sc: &gscope{}}, m, 1)
			}
		}
		for _, m := range w.members {
			if u := byKind(m.kindClass()); u != nil {
				if s := p.outside(ctx, m, u); s != "" {
					return s
				}
			}
		}
	case gList:
		if t.k == gList {
			if s := p.outside(ctx, w.elem, t.elem); s != "" {
				return "[" + s + "]"
			}
		}
	case gDict:
		if t.k == gDict {
			if s := p.outside(ctx, w.elem, t.elem); s != "" {
				return "{k9: " + s + "}"
			}
		}
	case gMaybe:
		if t.k == gMaybe {
			if s := p.outside(ctx, w.elem, t.elem); s != "" {
				return s + " just"
			}
		}
	case gBox:
		if t.k == gBox {
			if s := p.outside(ctx, w.elem, t.elem); s != "" {
				return "[" + s + "] box"
			}
		}
	case gShape:
		if t.k != gShape {
			return ""
		}
		for _, f := range w.fields {
			for _, g := range t.fields {
				if g.name != f.name || g.t.src() == f.t.src() {
					continue
				}
				if s := p.outside(ctx, f.t, g.t); s != "" {
					var parts []string
					for _, h := range w.fields {
						switch {
						case h.name == f.name:
							parts = append(parts, h.name+": "+s)
						case !h.opt:
							parts = append(parts, h.name+": "+p.expr(ctx, h.t, 1))
						}
					}
					return "{" + strings.Join(parts, ", ") + "}"
				}
			}
		}
	}
	return ""
}

// writeOutside writes, through the value `at` pushes (of type w, a
// widening of t), a value that is not of the type t gives that place.
func (p *progGen) writeOutside(ctx *gctx, at string, w, t *gty) string {
	switch {
	case w.k == gList && t.k == gList:
		if !w.elem.immutable() && p.chance(0.5) {
			// Into the first element, which the original also holds.
			if inner := p.writeOutside(ctx, at+" :0:", w.elem, t.elem); inner != "" {
				return at + " len 0 > if " + inner + " end"
			}
		}
		if s := p.outside(ctx, w.elem, t.elem); s != "" {
			return at + " " + s + " append drop"
		}
	case w.k == gDict && t.k == gDict:
		if s := p.outside(ctx, w.elem, t.elem); s != "" {
			return at + ` "k9" ` + s + " set drop"
		}
	case w.k == gShape && t.k == gShape:
		for _, f := range w.fields {
			for _, g := range t.fields {
				if g.name != f.name || g.t.src() == f.t.src() {
					continue
				}
				if s := p.outside(ctx, f.t, g.t); s != "" {
					return at + ` "` + f.name + `" ` + s + " set drop"
				}
				if !g.opt && !f.opt {
					if inner := p.writeOutside(ctx, at+" :"+f.name+"?", f.t, g.t); inner != "" {
						return inner
					}
				}
			}
		}
	case w.k == gMaybe && t.k == gMaybe:
		n := p.name("w")
		ctx.sc.add(n, w.elem).set = false
		if inner := p.writeOutside(ctx, "@"+n, w.elem, t.elem); inner != "" {
			return at + " match just " + n + " : " + inner + ", _ : , end"
		}
	case w.k == gBox && t.k == gBox:
		n := p.name("w")
		ctx.sc.add(n, listOf(w.elem)).set = false
		if inner := p.writeOutside(ctx, "@"+n, listOf(w.elem), listOf(t.elem)); inner != "" {
			return at + " match box " + n + " : " + inner + ", _ : , end"
		}
	case w.k == gUnion:
		for _, m := range w.members {
			for _, u := range unionMembers(t) {
				if m.kindClass() != u.kindClass() || m.src() == u.src() || (m.k != gList && m.k != gDict && m.k != gShape) {
					continue
				}
				n := p.name("w")
				ctx.sc.add(n, m).set = false
				if inner := p.writeOutside(ctx, "@"+n, m, u); inner != "" {
					return at + " match " + m.kindClass() + " " + n + " : " + inner + ", _ : , end"
				}
			}
		}
	}
	return ""
}

func unionMembers(t *gty) []*gty {
	if t.k == gUnion {
		return t.members
	}
	return []*gty{t}
}

// exploit follows a statement that gave a value a second type w, wider
// than its type t: it writes a value outside t through the new view, then
// reads the original back at t. Both check only if the statement before
// them should not have been accepted, or if the value was new.
func (p *progGen) exploit(ctx *gctx, blk *gblock, at string, w, t *gty, origin *gvar) {
	if code := p.writeOutside(ctx, at, w, t); code != "" {
		p.try(blk, code, true, nil)
	}
	if origin != nil {
		p.try(blk, "@"+origin.name+" "+p.consume(ctx, origin.t, 4), false, nil)
	}
}

// fill adds up to n statements to blk.
func (p *progGen) fill(ctx *gctx, blk *gblock, n int) {
	for i := 0; i < n; i++ {
		p.stmt(ctx, blk)
	}
}

// genDef adds a def: parameters stored in variables, a body of
// statements in the def's own scope, and outputs. An output is `new`
// when the checker says so: both marks are tried.
func (p *progGen) genDef() {
	name := p.name("f")
	nin := p.rng.Intn(3)
	ins := make([]*gty, nin)
	for i := range ins {
		ins[i] = p.randType(2)
	}
	var outs []*gty
	if p.chance(0.6) {
		outs = []*gty{p.randType(2)}
	}
	sc := &gscope{}
	params := make([]string, nin)
	for i := range params {
		params[i] = p.name("p")
	}
	var stores strings.Builder
	for i := nin - 1; i >= 0; i-- {
		stores.WriteString(" " + params[i] + "!")
	}
	sig := func(newOut bool) string {
		var b strings.Builder
		b.WriteString("def " + name + " (")
		for _, t := range ins {
			b.WriteString(t.src() + " ")
		}
		b.WriteString("--")
		for _, t := range outs {
			if newOut {
				b.WriteString(" new")
			}
			b.WriteString(" " + t.src())
		}
		b.WriteString(")" + stores.String())
		return b.String()
	}
	for i, t := range ins {
		sc.add(params[i], t)
	}
	ctx := &gctx{sc: sc, depth: 1}
	out := ""
	for _, t := range outs {
		out += " " + p.expr(ctx, t, 2) + " as " + t.src()
	}
	node := &gnode{parts: []string{sig(false), out + " end"}, blocks: []*gblock{{}}}
	p.defs = append(p.defs, node)
	if !p.ok() {
		node.parts[0] = sig(true)
		if !p.ok() {
			p.defs = p.defs[:len(p.defs)-1]
			return
		}
	}
	p.fill(ctx, node.blocks[0], 1+p.rng.Intn(4))
	p.sigs = append(p.sigs, gdef{name: name, ins: ins, outs: outs})
}

// generate builds one program.
func (p *progGen) generate() string {
	p.header = genHeader
	p.main = &gblock{}
	for i := p.rng.Intn(3); i > 0; i-- {
		p.genDef()
	}
	sc := &gscope{}
	ctx := &gctx{sc: sc}
	p.fill(ctx, p.main, 15+p.rng.Intn(25))
	// Read every variable back at its type.
	for _, v := range sc.vars {
		if v.set && v.t.k != gQuote {
			p.try(p.main, "@"+v.name+" "+p.consume(ctx, v.t, 4), false, nil)
		}
	}
	return p.render()
}

// ---- Running ----

type genFailure struct {
	seed int64
	src  string
	why  string
}

// genOutcome runs src and says why it is a soundness failure, or "".
func genOutcome(base *CoreBase, src string, errFile *os.File) (why string, checked bool) {
	file, err := parseMShellInput(src, &TokenFile{"gen.msh"})
	if err != nil {
		return "", false
	}
	if _, ok := base.Check(file); !ok {
		return "", false
	}
	run, err := runProgram(file, errFile)
	if err != nil {
		return "", false
	}
	switch {
	case run.panicked != "":
		return "panic: " + run.panicked, true
	case !run.ok && run.kind == TypeMismatch:
		return "type mismatch: " + strings.TrimSpace(run.stderr), true
	}
	return "", true
}

// shrink removes statements while the program still checks and still
// fails the same way.
func shrinkProgram(base *CoreBase, p *progGen, errFile *os.File) string {
	still := func() bool {
		why, _ := genOutcome(base, p.render(), errFile)
		return why != ""
	}
	var blocks []*gblock
	var collect func(b *gblock)
	collect = func(b *gblock) {
		blocks = append(blocks, b)
		for _, n := range b.nodes {
			for _, c := range n.blocks {
				collect(c)
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for i := len(p.defs) - 1; i >= 0; i-- {
			saved := p.defs
			p.defs = append(append([]*gnode(nil), p.defs[:i]...), p.defs[i+1:]...)
			if still() {
				changed = true
			} else {
				p.defs = saved
			}
		}
		blocks = blocks[:0]
		collect(p.main)
		for _, d := range p.defs {
			for _, c := range d.blocks {
				collect(c)
			}
		}
		for _, b := range blocks {
			for i := len(b.nodes) - 1; i >= 0; i-- {
				saved := b.nodes
				b.nodes = append(append([]*gnode(nil), b.nodes[:i]...), b.nodes[i+1:]...)
				if still() {
					changed = true
				} else {
					b.nodes = saved
				}
			}
		}
	}
	return p.render()
}

func TestGeneratedProgramsSound(t *testing.T) {
	programs := envTrials("MSH_GEN_PROGRAMS", 60)
	seed0 := int64(envTrials("MSH_GEN_SEED", 1))
	base := NewCoreBase(nil, nil)

	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()

	type result struct {
		seed                           int64
		failed                         bool
		statements, checks             int
		safeOK, safeNo, riskyOK, riskyNo int
		ranToEnd                       bool
		checkedMsg                     string
		genTime, runTime               time.Duration
	}
	results := make([]result, programs)
	saved := os.Stderr
	os.Stderr = devnull
	var wg sync.WaitGroup
	next := make(chan int)
	workers := runtime.NumCPU()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				seed := seed0 + int64(i)
				p := &progGen{rng: rand.New(rand.NewSource(seed)), base: base}
				p.risk = p.rng.Float64() * 0.3
				start := time.Now()
				src := p.generate()
				genTime := time.Since(start)
				res := result{genTime: genTime, seed: seed, checks: p.checks, safeOK: p.safeOK, safeNo: p.safeNo, riskyOK: p.riskyOK, riskyNo: p.riskyNo}
				res.statements = strings.Count(src, "\n")
				file, err := parseMShellInput(src, &TokenFile{"gen.msh"})
				if err == nil {
					if _, ok := base.Check(file); ok {
						start := time.Now()
						run, err := runProgram(file, devnull)
						res.runTime = time.Since(start)
						if err == nil {
							res.failed = run.panicked != "" || (!run.ok && run.kind == TypeMismatch)
							res.ranToEnd = run.ok
						}
					}
				}
				results[i] = res
			}
		}()
	}
	for i := 0; i < programs; i++ {
		next <- i
	}
	close(next)
	wg.Wait()
	os.Stderr = saved

	var failures []genFailure
	total := result{}
	ran := 0
	for _, r := range results {
		total.statements += r.statements
		total.checks += r.checks
		total.safeOK += r.safeOK
		total.safeNo += r.safeNo
		total.riskyOK += r.riskyOK
		total.riskyNo += r.riskyNo
		if r.ranToEnd {
			ran++
		}
		if r.failed {
			failures = append(failures, genFailure{seed: r.seed})
		}
	}
	t.Logf("%d programs, %d lines, %d checks; statements kept/refused: safe %d/%d, risky %d/%d; %d ran to the end",
		programs, total.statements, total.checks, total.safeOK, total.safeNo, total.riskyOK, total.riskyNo, ran)
	slow := append([]result(nil), results...)
	sort.Slice(slow, func(i, j int) bool { return slow[i].genTime+slow[i].runTime > slow[j].genTime+slow[j].runTime })
	for _, r := range slow[:min(3, len(slow))] {
		t.Logf("slowest: seed %d, %d lines, %d checks, generating %v, running %v", r.seed, r.statements, r.checks, r.genTime, r.runTime)
	}

	// Report each failure shrunk, with its message, one at a time.
	withStderrFile(t, func(errFile *os.File) {
		for i, f := range failures {
			if i >= 5 {
				t.Errorf("... and %d more failing seeds", len(failures)-5)
				break
			}
			p := &progGen{rng: rand.New(rand.NewSource(f.seed)), base: base}
			p.risk = p.rng.Float64() * 0.3
			p.generate()
			saved := os.Stderr
			os.Stderr = devnull
			small := shrinkProgram(base, p, errFile)
			os.Stderr = saved
			why, _ := genOutcome(base, small, errFile)
			t.Errorf("seed %d: a checked program reached a %s\n--- program (shrunk)\n%s", f.seed, why, small)
		}
	})
}

// TestShowGeneratedProgram prints the program for MSH_GEN_SHOW (a seed).
func TestShowGeneratedProgram(t *testing.T) {
	seed := envTrials("MSH_GEN_SHOW", 0)
	if seed == 0 {
		t.Skip("MSH_GEN_SHOW is not set")
	}
	p := &progGen{rng: rand.New(rand.NewSource(int64(seed))), base: NewCoreBase(nil, nil)}
	p.risk = p.rng.Float64() * 0.3
	t.Logf("risk %.2f\n%s", p.risk, p.generate())
}
