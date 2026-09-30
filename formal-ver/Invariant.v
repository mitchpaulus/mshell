(** * The runtime invariant.

    A store typing [Σ] gives every heap location the type it was created at
    ([HList], [HRec]) or the variable context of a scope ([HScope]).

    - [vtyped Σ v t]: value [v] has type [t] given the store typing. A
      location is typed through [Σ] and subtyping, so a shared object is
      only ever seen through supertypes of its one declared type.  A value
      has a recursive type when it has its unfolding; values are finite, so
      the derivation unfolds finitely often (cycles go through [Σ]).
    - [dtyped Σ H v t O]: *deep* typing of a fresh value, read directly off
      the heap.  [O] lists the locations of the value's lists and dicts; it
      is a tree (each location occurs once).  [Σ] is ignored on [O]: the
      store typing of a fresh object is irrelevant until it stops being
      fresh ("commit", Commit.v).

    The invariant [inv] says the stack's fresh slots own disjoint regions,
    that no shared slot and no object outside the regions references into
    a region, and that everything outside the regions is typed by [Σ]. *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Typing Interp.

Inductive htype :=
| HList (t : ty)
| HRec (fs : list (label * fstat)) (r : fstat)
| HScope (G : tenv).

Definition store_ty := list htype.

Definition is_scope (h : htype) : bool :=
  match h with HScope _ => true | _ => false end.

Inductive Forall3 {A B C : Type} (P : A -> B -> C -> Prop) : list A -> list B -> list C -> Prop :=
| F3_nil : Forall3 P [] [] []
| F3_cons a b c la lb lc : P a b c -> Forall3 P la lb lc -> Forall3 P (a :: la) (b :: lb) (c :: lc).

Fixpoint vlocs (v : val) : list loc :=
  match v with
  | VLoc l => [l]
  | VJust v' => vlocs v'
  | VCon _ _ _ vs => flat_map vlocs vs
  | _ => []
  end.

Definition olocs (o : obj) : list loc :=
  match o with
  | OList vs => flat_map vlocs vs
  | ODict kvs | OScope kvs => flat_map (fun p => vlocs (snd p)) kvs
  end.

Section Runtime.
Variable sigs : genv.

Inductive vtyped (Σ : store_ty) : val -> ty -> Prop :=
| vt_int n : vtyped Σ (VInt n) TInt
| vt_str s : vtyped Σ (VStr s) TStr
| vt_bool b : vtyped Σ (VBool b) TBool
| vt_none t : vtyped Σ VNone (TMaybe t)
| vt_just v t : vtyped Σ v t -> vtyped Σ (VJust v) (TMaybe t)
| vt_list l a t : nth_error Σ l = Some (HList a) -> sub (TList a) t -> vtyped Σ (VLoc l) t
| vt_rec l fs r t : nth_error Σ l = Some (HRec fs r) -> sub (TRec fs r) t -> vtyped Σ (VLoc l) t
| vt_clo sc e G ins outs :
    nth_error Σ sc = Some (HScope G) -> closure_ok sigs G e ins outs ->
    vtyped Σ (VClo sc e) (TQuote ins outs)
| vt_unionl v a b : vtyped Σ v a -> vtyped Σ v (TUnion a b)
| vt_unionr v a b : vtyped Σ v b -> vtyped Σ v (TUnion a b)
| vt_top v t : vtyped Σ v t -> vtyped Σ v TTop
| vt_con E c pts vs a :
    g_ctors sigs E c = Some pts -> wf_payload E pts ->
    vtypedl Σ vs (map (subst a) pts) -> vtyped Σ (VCon E c pts vs) (TEnum E a)
| vt_mu v t : mu_ok t = true -> vtyped Σ v (tunfold t) -> vtyped Σ v (TMu t)
with vtypedl (Σ : store_ty) : list val -> list ty -> Prop :=
| vtl_nil : vtypedl Σ [] []
| vtl_cons v vs t ts : vtyped Σ v t -> vtypedl Σ vs ts -> vtypedl Σ (v :: vs) (t :: ts).

Definition obj_ok (Σ : store_ty) (o : obj) (h : htype) : Prop :=
  match o, h with
  | OList vs, HList t => Forall (fun v => vtyped Σ v t) vs
  | ODict kvs, HRec fs r =>
      NoDup (map fst kvs) /\
      (forall k t, field_at k fs r = FReq t -> lookup k kvs <> None) /\
      Forall (fun p => vtyped Σ (snd p) (fty (field_at (fst p) fs r))) kvs
  | OScope kvs, HScope G =>
      forall x v, lookup x kvs = Some v -> exists t, lookup x G = Some t /\ vtyped Σ v t
  | _, _ => False
  end.

Inductive dtyped (Σ : store_ty) (H : heap) : val -> ty -> list loc -> Prop :=
| dt_int n : dtyped Σ H (VInt n) TInt []
| dt_str s : dtyped Σ H (VStr s) TStr []
| dt_bool b : dtyped Σ H (VBool b) TBool []
| dt_none t : dtyped Σ H VNone (TMaybe t) []
| dt_just v t O : dtyped Σ H v t O -> dtyped Σ H (VJust v) (TMaybe t) O
| dt_clo sc e ins outs :
    vtyped Σ (VClo sc e) (TQuote ins outs) -> dtyped Σ H (VClo sc e) (TQuote ins outs) []
| dt_list l vs t Os :
    nth_error H l = Some (OList vs) -> dtypeds Σ H vs t Os -> NoDup (l :: concat Os) ->
    dtyped Σ H (VLoc l) (TList t) (l :: concat Os)
| dt_rec l kvs fs r Os :
    nth_error H l = Some (ODict kvs) -> NoDup (map fst kvs) ->
    (forall k t, field_at k fs r = FReq t -> lookup k kvs <> None) ->
    dfields Σ H kvs fs r Os -> NoDup (l :: concat Os) ->
    dtyped Σ H (VLoc l) (TRec fs r) (l :: concat Os)
| dt_unionl v a b O : dtyped Σ H v a O -> dtyped Σ H v (TUnion a b) O
| dt_unionr v a b O : dtyped Σ H v b O -> dtyped Σ H v (TUnion a b) O
| dt_top v t O : dtyped Σ H v t O -> dtyped Σ H v TTop O
| dt_con E c pts vs a Os :
    g_ctors sigs E c = Some pts -> wf_payload E pts ->
    dtypedl Σ H vs (map (subst a) pts) Os -> NoDup (concat Os) ->
    dtyped Σ H (VCon E c pts vs) (TEnum E a) (concat Os)
| dt_mu v t O : mu_ok t = true -> dtyped Σ H v (tunfold t) O -> dtyped Σ H v (TMu t) O
with dtypeds (Σ : store_ty) (H : heap) : list val -> ty -> list (list loc) -> Prop :=
| dts_nil t : dtypeds Σ H [] t []
| dts_cons v vs t O Os : dtyped Σ H v t O -> dtypeds Σ H vs t Os -> dtypeds Σ H (v :: vs) t (O :: Os)
with dfields (Σ : store_ty) (H : heap) : list (string * val) -> list (label * fstat) -> fstat -> list (list loc) -> Prop :=
| dfs_nil fs r : dfields Σ H [] fs r []
| dfs_cons k v kvs fs r O Os :
    dtyped Σ H v (fty (field_at k fs r)) O -> dfields Σ H kvs fs r Os ->
    dfields Σ H ((k, v) :: kvs) fs r (O :: Os)
(** The payloads of a fresh enum value: one region per payload. *)
with dtypedl (Σ : store_ty) (H : heap) : list val -> list ty -> list (list loc) -> Prop :=
| dtl_nil : dtypedl Σ H [] [] []
| dtl_cons v vs t ts O Os :
    dtyped Σ H v t O -> dtypedl Σ H vs ts Os -> dtypedl Σ H (v :: vs) (t :: ts) (O :: Os).

Definition slot_ok (Σ : store_ty) (H : heap) (v : val) (p : slot) (O : list loc) : Prop :=
  match fst p with
  | Sh => vtyped Σ v (snd p) /\ O = []
  | Dp => dtyped Σ H v (snd p) O
  end.

Record inv (Σ : store_ty) (H : heap) (sc : loc) (G : tenv)
           (S : list val) (st : sty) (Os : list (list loc)) : Prop := {
  inv_len : length Σ = length H;
  inv_slots : Forall3 (slot_ok Σ H) S st Os;
  inv_disj : NoDup (concat Os);
  (* a slot references a region location only if it is in its own region *)
  inv_own : Forall3 (fun v _ O => forall l, In l (vlocs v) -> In l (concat Os) -> In l O) S st Os;
  (* objects outside the regions are typed by Σ and do not point into regions *)
  inv_heap : forall l o, nth_error H l = Some o -> ~ In l (concat Os) ->
               (exists h, nth_error Σ l = Some h /\ obj_ok Σ o h) /\
               (forall r, In r (olocs o) -> ~ In r (concat Os));
  inv_reg : forall l, In l (concat Os) -> exists h, nth_error Σ l = Some h /\ is_scope h = false;
  inv_scope : nth_error Σ sc = Some (HScope G)
}.

End Runtime.

Scheme vtyped_mut := Induction for vtyped Sort Prop
with vtypedl_mut := Induction for vtypedl Sort Prop.

Combined Scheme vtyped_comb from vtyped_mut, vtypedl_mut.

Scheme dtyped_mut := Induction for dtyped Sort Prop
with dtypeds_mut := Induction for dtypeds Sort Prop
with dfields_mut := Induction for dfields Sort Prop
with dtypedl_mut := Induction for dtypedl Sort Prop.

Combined Scheme dtyped_comb from dtyped_mut, dtypeds_mut, dfields_mut, dtypedl_mut.
