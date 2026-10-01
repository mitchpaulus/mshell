package main

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
)

// Validation (ai/type-core-calculus.typ, "Validation: tryAs and is"): does a
// value conform to a type? `tryAs T` and the match pattern `is T x` ask it at
// run time. The value is never copied or changed: `tryAs` gives `just` the
// same value, or `none`.
//
// The types are the checker's own: the runtime resolves a target with the
// same resolver, against the same declarations (design doc, "Type
// expressions": one resolved form of each type).
//
// Union members have distinct kinds (a union that breaks this is refused
// when it is resolved, here too), so the value's runtime kind picks the one
// member it can belong to, and validation is a conjunction of checks with
// no alternatives to go back on. A pair (object, type) met a second
// time is then assumed to hold, whether it is still being checked (a cycle)
// or already checked (a DAG): if every check succeeds, every pair assumed
// is one that was checked in full (the greatest fixed point, as `cvalidate`
// in formal-ver/Cycles.v). So a list that contains itself validates against
// `[Json]`, and a value with shared parts is walked once per part.
//
// A container is remembered when it has at least smallContainer elements,
// or a child that is a container with that many elements or with a
// container in it. Every container on a cycle is remembered. One that is
// not has few elements, each a scalar or a small container of scalars, so
// a visit to it costs at most a constant; each visit comes from a slot of a
// remembered container (walked once) or is part of such a constant, so the
// whole walk is linear in the distinct values, however they are shared.
// Most JSON records are small and are not remembered.
//
// The walk uses an explicit work stack, so a deep value cannot overflow the
// Go stack, and counts its steps against a budget: running out is an error
// that stops the program, never a quiet `none`.

// validateBudget is the most steps one validation may take: one per value
// and per alias or union looked through. A variable only so tests can
// lower it.
var validateBudget = 1 << 26

// runtimeTypes resolves the types the runtime validates against: the
// built-in aliases, and the `type` and `enum` declarations of the startup
// files, the script and each REPL line, as the checker sees them. A state's
// runtime types are replaced, not changed, when declarations are added.
type runtimeTypes struct {
	// mu guards everything here: a pipeline runs its stages, quotations
	// included, at the same time, and resolving or validating may add types
	// to the arena.
	mu sync.Mutex
	c  *coreChecker
	// unions caches, per union type, the member for each runtime kind.
	unions map[TypeId][]validateMember
	v      validator
}

// runtimeTarget is a tryAs or `is` target resolved by env, once.
type runtimeTarget struct {
	env *runtimeTypes
	t   TypeId
	err string
}

func newRuntimeTypes() *runtimeTypes {
	arena, names := NewTypeArena(), NewNameTable()
	rel := NewRelations(arena)
	res := coreResolver{arena: arena, names: names, rel: rel, aliases: map[NameId]TypeId{}, self: -1}
	res.declareJson()
	res.declareHtmlNode()
	(&coreTableBuilder{res: &res, t: &coreTable{}}).builtinAliases()
	c := &coreChecker{arena: arena, names: names, rel: rel, table: &coreTable{}, res: res, defs: map[NameId]*coreSig{}}
	return &runtimeTypes{c: c}
}

// builtinTypeNames are the names of the built-in aliases (Json, HtmlNode,
// HttpRequest, ...), which a declaration cannot take.
var builtinTypeNames = sync.OnceValue(func() map[string]bool {
	c := newRuntimeTypes().c
	out := make(map[string]bool, len(c.res.aliases))
	for id := range c.res.aliases {
		out[c.names.Name(id)] = true
	}
	return out
})

// runtimeTypesMade guards replacing a state's runtime types.
var runtimeTypesMade sync.Mutex

// declareRuntimeTypes declares items, after every declaration registered
// before them, in new runtime types, and makes those the state's. It
// reports the first error in items' declarations, and then changes
// nothing: a REPL line whose declarations are refused adds none of them.
// New types also mean each tryAs and `is` resolves its type again, so one
// that named a type not declared yet finds it now.
func (state *EvalState) declareRuntimeTypes(items []MShellParseItem) error {
	env := newRuntimeTypes()
	c := env.c
	c.declareAll(state.declItems, nil)
	mark := len(c.errs)
	c.declareAll(items, nil)
	if len(c.errs) > mark {
		e := c.errs[mark]
		return fmt.Errorf("%s: %s.\n", tokenPosStr(e.Pos), e.Hint)
	}
	runtimeTypesMade.Lock()
	state.typeEnv = env
	runtimeTypesMade.Unlock()
	return nil
}

// runtimeTypeEnv returns the state's runtime types, locked. The caller
// unlocks env.mu.
func (state *EvalState) runtimeTypeEnv() *runtimeTypes {
	runtimeTypesMade.Lock()
	if state.typeEnv == nil {
		state.typeEnv = newRuntimeTypes()
	}
	env := state.typeEnv
	runtimeTypesMade.Unlock()
	env.mu.Lock()
	return env
}

// resolveTarget resolves a target type expression, the first time only.
// The caller holds env.mu.
func (env *runtimeTypes) resolveTarget(target MShellParseItem, cache *runtimeTarget) {
	if cache.env == env {
		return
	}
	c := env.c
	t := c.res.resolveType(target)
	cache.env, cache.t, cache.err = env, t, ""
	if len(c.res.errs) > 0 {
		cache.err = c.res.errs[0].Hint
		c.res.errs = c.res.errs[:0]
	} else if t == TidNothing {
		cache.err = "the type " + target.DebugString() + " cannot be resolved"
	}
}

// validateValue validates value against the target of a tryAs or `is`,
// resolving it first. err is a message for a target that cannot be used or
// a validation that ran out of its budget.
func (state *EvalState) validateValue(value MShellObject, target MShellParseItem, cache *runtimeTarget) (bool, string) {
	env := state.runtimeTypeEnv()
	defer env.mu.Unlock()
	env.resolveTarget(target, cache)
	if cache.err != "" {
		return false, cache.err
	}
	ok, err := env.validate(value, cache.t)
	if err != nil {
		return false, err.Error()
	}
	return ok, ""
}

// validateMember is a union member, the runtime kind it holds, and whether
// it is the unknown type, which every value has.
type validateMember struct {
	k   valueKind
	t   TypeId
	any bool
}

// validator holds one validation's work: the values still to check, and the
// container pairs met so far.
type validator struct {
	env    *runtimeTypes
	budget int
	work   []validateTask
	// seen holds the (object, type) pairs of lists, dicts and enum values
	// remembered (see above).
	seen map[validatePair]struct{}
	// kids holds a dict's values to check, with their types.
	kids []validateTask
}

type validateTask struct {
	v MShellObject
	t TypeId
}

type validatePair struct {
	obj any
	t   TypeId
}

// errValidateBudget is a validation that ran out of steps.
type errValidateBudget struct{ t string }

func (e errValidateBudget) Error() string {
	return "validating the value against " + e.t + " took more than " + strconv.Itoa(validateBudget) +
		" steps, so it was stopped"
}

// validate reports whether value conforms to t. The caller holds env.mu.
func (env *runtimeTypes) validate(value MShellObject, t TypeId) (bool, error) {
	v := &env.v
	v.env, v.budget, v.work = env, validateBudget, v.work[:0]
	ok, err := v.run(value, t)
	// Keep small buffers for the next validation, not large ones.
	if cap(v.work) > 1<<16 {
		v.work = nil
	}
	if len(v.seen) > 1<<16 {
		v.seen = nil
	} else {
		clear(v.seen)
	}
	if err != nil {
		return false, errValidateBudget{t: FormatType(env.c.arena, env.c.names, t)}
	}
	return ok, nil
}

// run checks a value, and everything pushed while checking it.
func (v *validator) run(value MShellObject, t TypeId) (bool, error) {
	v.work = append(v.work, validateTask{v: value, t: t})
	for len(v.work) > 0 {
		task := v.work[len(v.work)-1]
		v.work = v.work[:len(v.work)-1]
		ok, err := v.visit(task.v, task.t)
		if err != nil || !ok {
			v.work = v.work[:0]
			return false, err
		}
	}
	return true, nil
}

var errValidateSteps = errors.New("out of steps")

// visit checks value against t one level down: a scalar is checked here, a
// container's elements are pushed as tasks, and a Maybe's or alias's single
// child is followed in place.
func (v *validator) visit(value MShellObject, t TypeId) (bool, error) {
	ar := v.env.c.arena
	for {
		v.budget--
		if v.budget < 0 {
			return false, errValidateSteps
		}
		switch t {
		case TidUnknown:
			return true, nil
		case TidBottom, TidNothing:
			return false, nil
		case TidInt:
			_, ok := value.(MShellInt)
			return ok, nil
		case TidFloat:
			_, ok := value.(MShellFloat)
			return ok, nil
		case TidStr:
			switch value.(type) {
			case MShellString, MShellLiteral:
				// A bare word in a list literal is a string too.
				return true, nil
			}
			return false, nil
		case TidBool:
			_, ok := value.(MShellBool)
			return ok, nil
		case TidPath:
			_, ok := value.(MShellPath)
			return ok, nil
		case TidDateTime:
			_, ok := value.(*MShellDateTime)
			return ok, nil
		case TidBytes:
			_, ok := value.(MShellBinary)
			return ok, nil
		case TidNull:
			_, ok := value.(MShellNull)
			return ok, nil
		}
		n := ar.nodes[t]
		switch n.Kind {
		case TKAlias:
			t = ar.aliases[n.A].Body
			continue
		case TKUnion:
			m, ok := v.member(value, t)
			if !ok {
				return false, nil
			}
			t = m
			continue
		case TKList:
			l, ok := value.(*MShellList)
			if !ok || !plainList(l) {
				return false, nil
			}
			if !v.enter(l, t, l.Items) {
				return true, nil
			}
			elem := TypeId(n.A)
			for i := len(l.Items) - 1; i >= 0; i-- {
				if ok, err := v.child(l.Items[i], elem); err != nil || !ok {
					return false, err
				}
			}
			return true, nil
		case TKRecord:
			d, ok := value.(*MShellDict)
			if !ok {
				return false, nil
			}
			return v.record(d, t, ar.records[n.Extra])
		case TKEnum:
			if n.A == EnumMaybe {
				var inner MShellObject
				switch m := value.(type) {
				case *Maybe:
					inner = m.obj
				case Maybe:
					inner = m.obj
				default:
					return false, nil
				}
				if inner == nil {
					return true, nil
				}
				value, t = inner, ar.enumArgs[n.Extra][0]
				continue
			}
			e, ok := value.(*MShellEnum)
			if !ok {
				return false, nil
			}
			return v.enum(e, t, n)
		case TKGrid, TKGridView, TKGridRow:
			// No type expression names a grid's columns yet, and the
			// checker refuses a grid whose columns are not known as a
			// target. Validating against a known schema would check each
			// column's cells.
			return false, nil
		}
		// A quote cannot be looked inside, and type variables and abstract
		// types have nothing to check against: no value conforms.
		return false, nil
	}
}

// plainList reports whether l has none of the redirects a command type
// tracks: where stdout and stderr go. `<` and `&` change nothing a type says
// (the checker types `[cat] "x" <` as `[str]`), so they are allowed.
func plainList(l *MShellList) bool {
	return l.StandardOutputFile == "" && l.StandardErrorFile == "" && !l.AppendOutput && !l.AppendError &&
		l.StdoutBehavior == STDOUT_NONE && l.StderrBehavior == STDERR_NONE && l.InPlaceFile == "" &&
		!l.StdoutToStderr && !l.StderrToStdout
}

// holdsContainers reports whether a value may hold a list, dict or enum
// value, so a cycle or shared part can be reached through it.
func holdsContainers(value MShellObject) bool {
	for {
		switch o := value.(type) {
		case *MShellList, *MShellDict:
			return true
		case *MShellEnum:
			return len(o.Payload) > 0
		case *Maybe:
			value = o.obj
		case Maybe:
			value = o.obj
		default:
			return false
		}
	}
}

// child checks an element: a value that may hold containers is pushed, any
// other is checked now.
func (v *validator) child(value MShellObject, t TypeId) (bool, error) {
	if holdsContainers(value) {
		v.work = append(v.work, validateTask{v: value, t: t})
		return true, nil
	}
	return v.visit(value, t)
}

// smallContainer is the number of elements from which a container is
// remembered.
const smallContainer = 16

// heavy reports whether a value is a container with at least
// smallContainer elements, or with a container among its elements.
func heavy(value MShellObject) bool {
	for {
		switch o := value.(type) {
		case *Maybe:
			value = o.obj
			continue
		case Maybe:
			value = o.obj
			continue
		case *MShellList:
			return heavyItems(o.Items)
		case *MShellDict:
			if len(o.Items) >= smallContainer {
				return true
			}
			for _, it := range o.Items {
				if holdsContainers(it) {
					return true
				}
			}
		case *MShellEnum:
			return heavyItems(o.Payload)
		}
		return false
	}
}

func heavyItems(items []MShellObject) bool {
	if len(items) >= smallContainer {
		return true
	}
	for _, it := range items {
		if holdsContainers(it) {
			return true
		}
	}
	return false
}

// enter records the pair (obj, t) for a container with the elements items,
// when it is one to remember (see above). It reports false when the pair
// was met before: it holds, or is being checked.
func (v *validator) enter(obj any, t TypeId, items []MShellObject) bool {
	if len(items) >= smallContainer {
		return v.firstMeeting(obj, t)
	}
	for _, it := range items {
		if heavy(it) {
			return v.firstMeeting(obj, t)
		}
	}
	return true
}

// firstMeeting records the pair (obj, t), and reports whether it is new.
func (v *validator) firstMeeting(obj any, t TypeId) bool {
	p := validatePair{obj: obj, t: t}
	if _, ok := v.seen[p]; ok {
		return false
	}
	if v.seen == nil {
		v.seen = make(map[validatePair]struct{})
	}
	v.seen[p] = struct{}{}
	return true
}

func (v *validator) record(d *MShellDict, t TypeId, rec RecordType) (bool, error) {
	names := v.env.c.names
	v.kids = v.kids[:0]
	if rec.Rest.Status == FieldOpen {
		// Only the declared fields are looked at.
		for _, f := range rec.Fields {
			x, ok := d.Items[names.Name(f.Name)]
			switch f.Status {
			case FieldRequired:
				if !ok {
					return false, nil
				}
			case FieldAbsent:
				if ok {
					return false, nil
				}
				continue
			case FieldOpen:
				continue
			}
			if ok {
				v.kids = append(v.kids, validateTask{v: x, t: f.Type})
			}
		}
	} else {
		// Every key is looked at.
		required := 0
		for _, f := range rec.Fields {
			if f.Status == FieldRequired {
				required++
			}
		}
		for key, x := range d.Items {
			f := rec.Rest
			if len(rec.Fields) > 0 {
				if id, ok := names.Lookup(key); ok {
					f = rec.FieldAt(id)
				}
			}
			switch f.Status {
			case FieldAbsent:
				return false, nil
			case FieldOpen:
				continue
			case FieldRequired:
				required--
			}
			v.kids = append(v.kids, validateTask{v: x, t: f.Type})
		}
		if required > 0 {
			return false, nil
		}
	}
	remember := len(v.kids) >= smallContainer
	for i := 0; !remember && i < len(v.kids); i++ {
		remember = heavy(v.kids[i].v)
	}
	if remember && !v.firstMeeting(d, t) {
		return true, nil
	}
	// child checks a value with no containers in place, which never comes
	// back here, so kids is not overwritten while it is read.
	for i := len(v.kids) - 1; i >= 0; i-- {
		if ok, err := v.child(v.kids[i].v, v.kids[i].t); err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

func (v *validator) enum(e *MShellEnum, t TypeId, n TypeNode) (bool, error) {
	c := v.env.c
	decl := c.arena.EnumDecl(n.A)
	if c.names.Name(decl.Name) != e.EnumName || e.MemberIndex < 0 || e.MemberIndex >= len(decl.Ctors) {
		return false, nil
	}
	ctor := decl.Ctors[e.MemberIndex]
	if c.names.Name(ctor.Name) != e.Member || len(ctor.Payload) != len(e.Payload) {
		return false, nil
	}
	// Enum values share subtrees freely (`@t @t node`), so each (value,
	// type) pair is checked once.
	if !v.enter(e, t, e.Payload) {
		return true, nil
	}
	args := c.arena.enumArgs[n.Extra]
	for i := len(e.Payload) - 1; i >= 0; i-- {
		pt := ctor.Payload[i]
		if len(args) > 0 {
			pt = c.rel.SubstParams(pt, args)
		}
		if ok, err := v.child(e.Payload[i], pt); err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// member picks the member of union u that value can belong to: the one of
// its runtime kind, or the unknown type.
func (v *validator) member(value MShellObject, u TypeId) (TypeId, bool) {
	k, known := v.runtimeKind(value)
	for _, mem := range v.env.unionMembers(u) {
		if mem.any || (known && mem.k == k) {
			return mem.t, true
		}
	}
	return TidNothing, false
}

// unionMembers lists the members of u with their kinds, looking through
// aliases among them, once per union.
func (env *runtimeTypes) unionMembers(u TypeId) []validateMember {
	if ms, ok := env.unions[u]; ok {
		return ms
	}
	var out []validateMember
	ar := env.c.arena
	var add func(t TypeId, depth int)
	add = func(t TypeId, depth int) {
		if depth > 64 {
			return
		}
		if t == TidUnknown {
			out = append(out, validateMember{t: t, any: true})
			return
		}
		switch ar.nodes[t].Kind {
		case TKUnion:
			for _, m := range ar.unionMembers[ar.nodes[t].Extra] {
				add(m, depth+1)
			}
			return
		case TKAlias:
			// The alias's own kinds pick it; it is unfolded when visited.
			if ks, ok := env.c.rel.Kinds(t); ok {
				for _, k := range ks {
					out = append(out, validateMember{k: k, t: t})
				}
			}
			return
		}
		if k, ok := env.c.rel.kindOf(t); ok {
			out = append(out, validateMember{k: k, t: t})
		}
	}
	add(u, 0)
	if env.unions == nil {
		env.unions = make(map[TypeId][]validateMember)
	}
	env.unions[u] = out
	return out
}

// runtimeKind is the kind of a value, as kindOf gives the kind of a type.
func (v *validator) runtimeKind(value MShellObject) (valueKind, bool) {
	switch o := value.(type) {
	case MShellInt:
		return valueKind{code: uint32(TidInt)}, true
	case MShellFloat:
		return valueKind{code: uint32(TidFloat)}, true
	case MShellString, MShellLiteral:
		return valueKind{code: uint32(TidStr)}, true
	case MShellBool:
		return valueKind{code: uint32(TidBool)}, true
	case MShellPath:
		return valueKind{code: uint32(TidPath)}, true
	case *MShellDateTime:
		return valueKind{code: uint32(TidDateTime)}, true
	case MShellBinary:
		return valueKind{code: uint32(TidBytes)}, true
	case MShellNull:
		return valueKind{code: uint32(TidNull)}, true
	case *MShellList:
		return valueKind{code: kindList}, true
	case *MShellDict:
		return valueKind{code: kindDict}, true
	case *MShellQuotation:
		return valueKind{code: kindQuote}, true
	case *Maybe, Maybe:
		return valueKind{code: kindEnum, enum: EnumMaybe}, true
	case *MShellEnum:
		c := v.env.c
		if id, ok := c.names.Lookup(o.EnumName); ok {
			if idx, ok := c.res.enums[id]; ok {
				return valueKind{code: kindEnum, enum: idx}, true
			}
		}
	case *MShellGrid:
		return valueKind{code: kindGrid}, true
	case *MShellGridView:
		return valueKind{code: kindGridView}, true
	case *MShellGridRow:
		return valueKind{code: kindGridRow}, true
	}
	return valueKind{}, false
}
