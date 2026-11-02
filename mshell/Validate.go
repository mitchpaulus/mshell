package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// Validation (ai/type-core-calculus.typ, "Validation: tryAs and is"): does a
// value conform to a type? `tryAs T` and the match pattern `is T x` ask it at
// run time. The value is never copied or changed: `tryAs` gives `ok` with
// the same value, or `err` with why it does not conform.
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
// that stops the program, never a quiet `err`.
//
// Why a value does not conform is found by running the failed validation
// again, recording where each value sits (explainFailure). Whether a value
// conforms does not depend on the order of the walk, so the second walk
// fails too; it takes dict keys in sorted order, so the same value always
// gets the same reason. A validation that succeeds pays nothing for it.

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
	res.declareToml()
	res.declareHtmlNode()
	(&coreTableBuilder{res: &res, t: &coreTable{}}).builtinAliases()
	c := &coreChecker{arena: arena, names: names, rel: rel, table: &coreTable{}, res: res, defs: map[NameId]*coreSig{}}
	c.declareBuiltins()
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
// resolving it first. err is a target that cannot be used (a type
// mismatch: the checker resolves every target), or a validation that ran
// out of its budget (a checked error).
func (state *EvalState) validateValue(value MShellObject, target MShellParseItem, cache *runtimeTarget) (bool, error) {
	env := state.runtimeTypeEnv()
	defer env.mu.Unlock()
	env.resolveTarget(target, cache)
	if cache.err != "" {
		return false, errors.New(cache.err)
	}
	ok, err := env.validate(value, cache.t)
	return ok, asChecked(err)
}

// tryValidate validates value against the target of a tryAs, as
// validateValue, and when it does not conform says why.
func (state *EvalState) tryValidate(value MShellObject, target MShellParseItem, cache *runtimeTarget) (bool, string, error) {
	env := state.runtimeTypeEnv()
	defer env.mu.Unlock()
	env.resolveTarget(target, cache)
	if cache.err != "" {
		return false, "", errors.New(cache.err)
	}
	ok, err := env.validate(value, cache.t)
	if err != nil || ok {
		return ok, "", asChecked(err)
	}
	why, err := env.explainFailure(value, cache.t)
	return false, why, asChecked(err)
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
	// kidKeys holds the key of each of kids, when explaining.
	kidKeys []string

	// The rest is used only to say why a value does not conform: the
	// failed validation is run again with explain set, and the walk then
	// records where each value sits, as a step from the container holding
	// it, and the reason the first failing value fails.
	explain bool
	steps   []validateStep
	// workAt holds the step of each value in work.
	workAt []int32
	// at is the step of the value being checked, next the step of the
	// value child is given; -1 is the validated value itself.
	at, next int32
	// why is the reason of the first failure, and whyAt its step.
	why   string
	whyAt int32
}

// validateStep is where a value sits in the container holding it: at a
// list index, under a dict key, or in a payload slot of an enum member.
type validateStep struct {
	parent int32
	kind   uint8
	key    string
	index  int
	arity  int
}

const (
	stepIndex uint8 = iota
	stepKey
	stepPayload
)

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
	v.reset()
	if err != nil {
		return false, errValidateBudget{t: FormatType(env.c.arena, env.c.names, t)}
	}
	return ok, nil
}

// reset keeps small buffers for the next validation, not large ones.
func (v *validator) reset() {
	if cap(v.work) > 1<<16 {
		v.work = nil
	}
	if len(v.seen) > 1<<16 {
		v.seen = nil
	} else {
		clear(v.seen)
	}
}

// explainFailure says why value does not conform to t, which a validation
// found: where in the value the first failing part is, and why it fails.
// The caller holds env.mu.
func (env *runtimeTypes) explainFailure(value MShellObject, t TypeId) (string, error) {
	v := &env.v
	v.env, v.budget, v.work = env, validateBudget, v.work[:0]
	v.explain, v.steps, v.workAt, v.at, v.next, v.why, v.whyAt = true, nil, nil, -1, -1, "", -1
	defer func() {
		v.explain, v.steps, v.workAt, v.kidKeys, v.why = false, nil, nil, nil, ""
		v.reset()
	}()
	ok, err := v.run(value, t)
	if err != nil {
		return "", errValidateBudget{t: FormatType(env.c.arena, env.c.names, t)}
	}
	target := shortText(FormatType(env.c.arena, env.c.names, t), 60)
	if ok || v.why == "" {
		// Not expected: the same walk failed before.
		return "not of type " + target, nil
	}
	if v.whyAt < 0 && strings.HasPrefix(v.why, "expected ") {
		// The value itself has another type: the reason names the target.
		return v.why, nil
	}
	var sb strings.Builder
	sb.WriteString("not of type ")
	sb.WriteString(target)
	sb.WriteString(": ")
	if v.whyAt >= 0 {
		sb.WriteString("at ")
		v.writePath(&sb, v.whyAt)
		sb.WriteString(", ")
	}
	sb.WriteString(v.why)
	return sb.String(), nil
}

// stepTo records the step of the next child from the value being checked,
// when explaining. It is kept out of line, so validating without
// explaining stays as fast as it can.
//
//go:noinline
func (v *validator) stepTo(kind uint8, key string, index, arity int) {
	v.steps = append(v.steps, validateStep{parent: v.at, kind: kind, key: key, index: index, arity: arity})
	v.next = int32(len(v.steps) - 1)
}

// writePath writes the path to a step, as jq writes one: .a.b[0]["odd key"].
// An enum payload is written (member), or (member i) when the member has
// several.
func (v *validator) writePath(sb *strings.Builder, at int32) {
	var path []int32
	for ; at >= 0; at = v.steps[at].parent {
		path = append(path, at)
	}
	for i := len(path) - 1; i >= 0; i-- {
		s := v.steps[path[i]]
		switch s.kind {
		case stepIndex:
			sb.WriteString("[" + strconv.Itoa(s.index) + "]")
		case stepKey:
			if plainKey(s.key) {
				sb.WriteString("." + s.key)
			} else {
				sb.WriteString("[" + strconv.Quote(s.key) + "]")
			}
		case stepPayload:
			if s.arity > 1 {
				sb.WriteString("(" + s.key + " " + strconv.Itoa(s.index) + ")")
			} else {
				sb.WriteString("(" + s.key + ")")
			}
		}
	}
}

// plainKey reports whether a dict key can be written after a dot.
func plainKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// fail records why the value at the current step fails, when explaining
// and no failure was recorded yet, and reports false.
//
//go:noinline
func (v *validator) fail(why string) bool {
	if v.explain && v.why == "" {
		v.why, v.whyAt = why, v.at
	}
	return false
}

// mismatch is the reason a value is not of the type t.
func (v *validator) mismatch(value MShellObject, t TypeId) string {
	c := v.env.c
	want := FormatType(c.arena, c.names, t)
	// Name what a named union stands for, when that is short.
	body := t
	for i := 0; i < 64 && c.arena.nodes[body].Kind == TKAlias; i++ {
		body = c.arena.aliases[c.arena.nodes[body].A].Body
	}
	if n := c.arena.nodes[body]; n.Kind == TKUnion {
		var sb strings.Builder
		for i, m := range c.arena.unionMembers[n.Extra] {
			if i > 0 {
				sb.WriteString(" | ")
			}
			sb.WriteString(FormatType(c.arena, c.names, m))
		}
		if b := sb.String(); b != want && len(b) <= 60 {
			want += " (" + b + ")"
		}
	}
	return "expected " + shortText(want, 80) + ", found " + describeValue(value)
}

// describeValue names a value's kind, with the value itself when it is
// short.
func describeValue(value MShellObject) string {
	switch o := value.(type) {
	case MShellInt:
		return "int " + o.ToString()
	case MShellFloat:
		f := o.ToString()
		if !strings.ContainsAny(f, ".eEnN") {
			f += ".0"
		}
		return "float " + f
	case MShellBool:
		return "bool " + o.ToString()
	case MShellString:
		return "str " + strconv.Quote(shortText(o.Content, 40))
	case MShellNull:
		return "null"
	case MShellPath:
		return "path " + strconv.Quote(shortText(o.Path, 40))
	case *MShellList:
		return "a list of " + plural(len(o.Items), "item")
	case *MShellDict:
		return "a dict with " + plural(len(o.Items), "key")
	case *Maybe:
		if o.obj == nil {
			return "none"
		}
		return "just " + describeValue(o.obj)
	case Maybe:
		if o.obj == nil {
			return "none"
		}
		return "just " + describeValue(o.obj)
	case *MShellEnum:
		return o.Member + " (" + o.EnumName + ")"
	case MShellBinary:
		return "binary"
	case *MShellDateTime:
		return "datetime"
	case MShellRune:
		return "rune"
	case *MShellQuotation:
		return "a quotation"
	case *MShellGrid, *MShellGridView, *MShellGridRow:
		return "a grid"
	}
	return value.TypeName()
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// shortText cuts s to at most n bytes, marking the cut.
func shortText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n - 3
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// run checks a value, and everything pushed while checking it.
func (v *validator) run(value MShellObject, t TypeId) (bool, error) {
	v.work = append(v.work, validateTask{v: value, t: t})
	if v.explain {
		v.workAt = append(v.workAt, -1)
	}
	for len(v.work) > 0 {
		task := v.work[len(v.work)-1]
		v.work = v.work[:len(v.work)-1]
		var ok bool
		var err error
		if v.explain {
			v.at = v.workAt[len(v.workAt)-1]
			v.workAt = v.workAt[:len(v.workAt)-1]
			ok, err = v.visitExplained(task.v, task.t)
		} else {
			ok, err = v.visit(task.v, task.t)
		}
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
			case MShellString:
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
		case TidRune:
			_, ok := value.(MShellRune)
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
				if v.explain {
					v.stepTo(stepIndex, "", i, 0)
				}
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

// visitExplained is visit, recording a value not of the type as the reason
// of the failure, when no other reason was recorded.
func (v *validator) visitExplained(value MShellObject, t TypeId) (bool, error) {
	ok, err := v.visit(value, t)
	if !ok && err == nil {
		v.fail(v.mismatch(value, t))
	}
	return ok, err
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
	if v.explain {
		return v.childExplained(value, t)
	}
	if holdsContainers(value) {
		v.work = append(v.work, validateTask{v: value, t: t})
		return true, nil
	}
	return v.visit(value, t)
}

// childExplained is child when explaining: v.next is the element's step.
//
//go:noinline
func (v *validator) childExplained(value MShellObject, t TypeId) (bool, error) {
	if holdsContainers(value) {
		v.work = append(v.work, validateTask{v: value, t: t})
		v.workAt = append(v.workAt, v.next)
		return true, nil
	}
	parent := v.at
	v.at = v.next
	ok, err := v.visitExplained(value, t)
	v.at = parent
	return ok, err
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
	if v.explain {
		v.kidKeys = v.kidKeys[:0]
	}
	if rec.Rest.Status == FieldOpen {
		// Only the declared fields are looked at.
		for _, f := range rec.Fields {
			name := names.Name(f.Name)
			x, ok := d.Items[name]
			switch f.Status {
			case FieldRequired:
				if !ok {
					return v.fail("missing the field " + strconv.Quote(name)), nil
				}
			case FieldAbsent:
				if ok {
					return v.fail("the field " + strconv.Quote(name) + " is not allowed"), nil
				}
				continue
			case FieldOpen:
				continue
			}
			if ok {
				v.kids = append(v.kids, validateTask{v: x, t: f.Type})
				if v.explain {
					v.kidKeys = append(v.kidKeys, name)
				}
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
		if v.explain {
			if !v.explainKeys(d, rec, &required) {
				return false, nil
			}
		} else {
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
		}
		if required > 0 {
			if v.explain {
				for _, f := range rec.Fields {
					name := names.Name(f.Name)
					if _, ok := d.Items[name]; f.Status == FieldRequired && !ok {
						return v.fail("missing the field " + strconv.Quote(name)), nil
					}
				}
			}
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
		if v.explain {
			v.stepTo(stepKey, v.kidKeys[i], 0, 0)
		}
		if ok, err := v.child(v.kids[i].v, v.kids[i].t); err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// explainKeys is the loop of record over every key of d, when explaining:
// it takes the keys in order, so the reason given is always the same, and
// records the key of each value added to kids in kidKeys.
func (v *validator) explainKeys(d *MShellDict, rec RecordType, required *int) bool {
	names := v.env.c.names
	keys := make([]string, 0, len(d.Items))
	for key := range d.Items {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		f := rec.Rest
		if len(rec.Fields) > 0 {
			if id, ok := names.Lookup(key); ok {
				f = rec.FieldAt(id)
			}
		}
		switch f.Status {
		case FieldAbsent:
			return v.fail("the field " + strconv.Quote(key) + " is not allowed")
		case FieldOpen:
			continue
		case FieldRequired:
			*required--
		}
		v.kids = append(v.kids, validateTask{v: d.Items[key], t: f.Type})
		v.kidKeys = append(v.kidKeys, key)
	}
	return true
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
		if v.explain {
			v.stepTo(stepPayload, e.Member, i, len(e.Payload))
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
	case MShellString:
		return valueKind{code: uint32(TidStr)}, true
	case MShellBool:
		return valueKind{code: uint32(TidBool)}, true
	case MShellPath:
		return valueKind{code: uint32(TidPath)}, true
	case *MShellDateTime:
		return valueKind{code: uint32(TidDateTime)}, true
	case MShellRune:
		return valueKind{code: uint32(TidRune)}, true
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
