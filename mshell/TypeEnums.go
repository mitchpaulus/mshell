package main

// What an enum declaration's constructors say about the enum
// (ai/type-core-calculus.typ, §Subtyping and §Freshness): each parameter's
// variance and whether it is fresh-covariant, and whether the enum is
// immutable and checkable. The proof takes these as declared and checks
// every payload type against them (wf_payload in formal-ver/Subtyping.v);
// Variance.v proves that makes the rules sound. The checker computes the
// most permissive values that pass that check, and WellFormedEnum is the
// check itself.
//
// Declarations may refer to each other and to themselves, so the values
// are fixed points over a group of declarations: variance is the least
// (start with every parameter unused, and add each occurrence's
// polarity), and fresh-covariance, immutability and checkability are the
// greatest (start true, and clear what a payload contradicts).

// polarity is where a parameter occurs: in the direction of the payload
// (positive), against it (negative), or both (invariant).
type polarity uint8

const (
	polPos polarity = iota
	polNeg
	polInv
)

func flipPolarity(p polarity) polarity {
	switch p {
	case polPos:
		return polNeg
	case polNeg:
		return polPos
	}
	return polInv
}

// composePolarity is the polarity of an enum argument, under polarity p, of
// a parameter with variance v: comp in Subtyping.v.
func composePolarity(p polarity, v Variance) polarity {
	switch v {
	case VarCo:
		return p
	case VarContra:
		return flipPolarity(p)
	}
	return polInv
}

// AnalyzeEnums sets Params[i].Variance, Params[i].Fresh, Immutable and
// Checkable of the enum declarations at idxs, from their constructors. The
// group must contain every declaration that the group's payloads refer to
// and that is not already analyzed.
func (r *Relations) AnalyzeEnums(idxs []uint32) {
	ar := r.arena
	for _, idx := range idxs {
		d := &ar.enumDecls[idx]
		for i := range d.Params {
			d.Params[i].Variance = VarCo
			d.Params[i].Fresh = true
		}
		d.Immutable = true
		d.Checkable = true
	}

	// Variance: the least fixed point. seen records the polarities each
	// parameter occurs at so far; a parameter with none yet is unused, and an
	// argument in its position constrains nothing. Occurrences only add, so
	// this settles, and a parameter still unused is covariant.
	seen := make(map[uint32][][2]bool, len(idxs))
	for _, idx := range idxs {
		seen[idx] = make([][2]bool, len(ar.enumDecls[idx].Params))
	}
	variance := func(decl uint32, j int) (Variance, bool) {
		s, ok := seen[decl]
		if !ok {
			return ar.enumDecls[decl].Params[j].Variance, true
		}
		switch {
		case s[j][0] && s[j][1]:
			return VarInv, true
		case s[j][1]:
			return VarContra, true
		case s[j][0]:
			return VarCo, true
		}
		return VarCo, false
	}
	for {
		for changed := true; changed; {
			changed = false
			for _, idx := range idxs {
				d := &ar.enumDecls[idx]
				for _, c := range d.Ctors {
					for _, t := range c.Payload {
						r.paramPolarities(t, polPos, variance, func(i int, p polarity) {
							s := &seen[idx][i]
							if (p == polPos || p == polInv) && !s[0] {
								s[0], changed = true, true
							}
							if (p == polNeg || p == polInv) && !s[1] {
								s[1], changed = true, true
							}
						})
					}
				}
			}
		}
		// A parameter still unused is covariant. That makes the arguments
		// in its position count, which can add occurrences (`enum R[a] = r
		// [R[a]] end` makes a invariant), so settle again.
		fixed := false
		for _, idx := range idxs {
			for i := range seen[idx] {
				if !seen[idx][i][0] && !seen[idx][i][1] {
					seen[idx][i][0], fixed = true, true
				}
			}
		}
		if !fixed {
			break
		}
	}
	for _, idx := range idxs {
		d := &ar.enumDecls[idx]
		for i := range d.Params {
			d.Params[i].Variance, _ = variance(idx, i)
		}
	}

	// Fresh-covariance, immutability and checkability: clear what a
	// payload contradicts until nothing changes.
	for changed := true; changed; {
		changed = false
		for _, idx := range idxs {
			d := &ar.enumDecls[idx]
			for _, c := range d.Ctors {
				for _, t := range c.Payload {
					r.nonDataParams(t, d.Params, false, func(i int) {
						if d.Params[i].Fresh {
							d.Params[i].Fresh, changed = false, true
						}
					})
					if d.Immutable && !r.Immutable(t) {
						d.Immutable, changed = false, true
					}
					if d.Checkable && !r.checkable(t, true, nil) {
						d.Checkable, changed = false, true
					}
				}
			}
		}
	}
}

// declaredVariance reads an enum parameter's variance from its declaration.
func (r *Relations) declaredVariance(decl uint32, j int) (Variance, bool) {
	return r.arena.enumDecls[decl].Params[j].Variance, true
}

// paramPolarities calls visit for each occurrence of an enum parameter in
// t, with the polarity it occurs at, t itself being at polarity p. Lists,
// fields and other invariant positions make an occurrence invariant.
// variance gives each enum parameter's variance, or false when it is unused,
// so that an argument in its position is skipped.
func (r *Relations) paramPolarities(t TypeId, p polarity, variance func(decl uint32, j int) (Variance, bool), visit func(i int, p polarity)) {
	ar := r.arena
	n := ar.Node(t)
	switch n.Kind {
	case TKParam:
		visit(int(n.A), p)
	case TKMaybe:
		r.paramPolarities(TypeId(n.A), p, variance, visit)
	case TKList:
		r.paramPolarities(TypeId(n.A), polInv, variance, visit)
	case TKRecord:
		rec := ar.records[n.Extra]
		for _, f := range rec.Fields {
			if f.Type != TidNothing {
				r.paramPolarities(f.Type, polInv, variance, visit)
			}
		}
		if rec.Rest.Type != TidNothing {
			r.paramPolarities(rec.Rest.Type, polInv, variance, visit)
		}
	case TKUnion:
		for _, m := range ar.unionMembers[n.Extra] {
			r.paramPolarities(m, p, variance, visit)
		}
	case TKQuote:
		sig := ar.quoteSigs[n.Extra]
		for _, x := range sig.Inputs {
			r.paramPolarities(x, flipPolarity(p), variance, visit)
		}
		for _, x := range sig.Outputs {
			r.paramPolarities(x, p, variance, visit)
		}
	case TKEnum:
		for j, x := range ar.enumArgs[n.Extra] {
			if v, used := variance(n.A, j); used {
				r.paramPolarities(x, composePolarity(p, v), variance, visit)
			}
		}
	case TKCommand:
		r.paramPolarities(TypeId(n.A), polInv, variance, visit)
	case TKGrid, TKGridView, TKGridRow:
		for _, col := range ar.gridSchemas[n.Extra].Columns {
			r.paramPolarities(col.Type, polInv, variance, visit)
		}
	}
}

// nonDataParams calls visit for each parameter that occurs in t outside a
// data position (occ_fresh in Subtyping.v). Data positions are a payload
// itself, a list element, a field value, a Maybe, a union member and a
// fresh-covariant enum argument; a quote is not data, and neither is an
// argument that is not fresh-covariant. underNonData says t is already
// outside data.
func (r *Relations) nonDataParams(t TypeId, params []EnumParam, underNonData bool, visit func(i int)) {
	ar := r.arena
	n := ar.Node(t)
	switch n.Kind {
	case TKParam:
		if underNonData {
			visit(int(n.A))
		}
	case TKMaybe, TKList:
		r.nonDataParams(TypeId(n.A), params, underNonData, visit)
	case TKRecord:
		rec := ar.records[n.Extra]
		for _, f := range rec.Fields {
			if f.Type != TidNothing {
				r.nonDataParams(f.Type, params, underNonData, visit)
			}
		}
		if rec.Rest.Type != TidNothing {
			r.nonDataParams(rec.Rest.Type, params, underNonData, visit)
		}
	case TKUnion:
		for _, m := range ar.unionMembers[n.Extra] {
			r.nonDataParams(m, params, underNonData, visit)
		}
	case TKQuote:
		sig := ar.quoteSigs[n.Extra]
		for _, x := range sig.Inputs {
			r.nonDataParams(x, params, true, visit)
		}
		for _, x := range sig.Outputs {
			r.nonDataParams(x, params, true, visit)
		}
	case TKEnum:
		eparams := ar.enumDecls[n.A].Params
		for j, x := range ar.enumArgs[n.Extra] {
			r.nonDataParams(x, params, underNonData || !eparams[j].Fresh, visit)
		}
	case TKCommand:
		r.nonDataParams(TypeId(n.A), params, underNonData, visit)
	case TKGrid, TKGridView, TKGridRow:
		for _, col := range ar.gridSchemas[n.Extra].Columns {
			r.nonDataParams(col.Type, params, underNonData, visit)
		}
	}
}

// WellFormedEnum checks every payload type of the enum at idx against its
// parameters' variance and fresh-covariance and its immutability: wf_pt in
// Subtyping.v. AnalyzeEnums always produces a declaration that passes.
func (r *Relations) WellFormedEnum(idx uint32) bool {
	d := r.arena.enumDecls[idx]
	for _, c := range d.Ctors {
		for _, t := range c.Payload {
			if !r.occursAt(t, d.Params, polPos) || !r.occursFresh(t, d.Params) {
				return false
			}
			if d.Immutable && !r.Immutable(t) {
				return false
			}
			if d.Checkable && !r.checkable(t, true, nil) {
				return false
			}
		}
	}
	return true
}

// occursAt is occ_sub in Subtyping.v: every parameter in t, t being at
// polarity p, occurs at a polarity its variance allows.
func (r *Relations) occursAt(t TypeId, params []EnumParam, p polarity) bool {
	ok := true
	r.paramPolarities(t, p, r.declaredVariance, func(i int, q polarity) {
		if i >= len(params) {
			ok = false
			return
		}
		switch params[i].Variance {
		case VarCo:
			ok = ok && q == polPos
		case VarContra:
			ok = ok && q == polNeg
		}
	})
	return ok
}

// occursFresh is occ_fresh in Subtyping.v: no fresh-covariant parameter
// occurs outside a data position.
func (r *Relations) occursFresh(t TypeId, params []EnumParam) bool {
	ok := true
	r.nonDataParams(t, params, false, func(i int) {
		if i >= len(params) || params[i].Fresh {
			ok = false
		}
	})
	return ok
}

// SubstParams replaces each enum parameter in t by the argument at its
// index: subst in Syntax.v. It is how a constructor's payload types are
// read at an instance of the enum.
func (r *Relations) SubstParams(t TypeId, args []TypeId) TypeId {
	ar := r.arena
	n := ar.Node(t)
	switch n.Kind {
	case TKParam:
		if int(n.A) < len(args) {
			return args[n.A]
		}
		return TidBottom
	case TKMaybe:
		return ar.MakeMaybe(r.SubstParams(TypeId(n.A), args))
	case TKList:
		return ar.MakeList(r.SubstParams(TypeId(n.A), args))
	case TKRecord:
		rec := ar.records[n.Extra]
		fields := make([]RecordField, len(rec.Fields))
		for i, f := range rec.Fields {
			fields[i] = f
			if f.Type != TidNothing {
				fields[i].Type = r.SubstParams(f.Type, args)
			}
		}
		rest := rec.Rest
		if rest.Type != TidNothing {
			rest.Type = r.SubstParams(rest.Type, args)
		}
		return ar.MakeRecord(fields, rest)
	case TKUnion:
		members := make([]TypeId, len(ar.unionMembers[n.Extra]))
		for i, m := range ar.unionMembers[n.Extra] {
			members[i] = r.SubstParams(m, args)
		}
		return ar.MakeUnion(members, NameNone)
	case TKQuote:
		sig := ar.quoteSigs[n.Extra]
		out := QuoteSig{Diverges: sig.Diverges}
		for _, x := range sig.Inputs {
			out.Inputs = append(out.Inputs, r.SubstParams(x, args))
		}
		for _, x := range sig.Outputs {
			out.Outputs = append(out.Outputs, r.SubstParams(x, args))
		}
		return ar.MakeQuote(out)
	case TKEnum:
		eargs := ar.enumArgs[n.Extra]
		out := make([]TypeId, len(eargs))
		for i, x := range eargs {
			out[i] = r.SubstParams(x, args)
		}
		return ar.MakeEnum(n.A, out)
	}
	return t
}
