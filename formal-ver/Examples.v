(** * Examples: the interpreter really gets stuck on the unsound programs,
    and the core rejects the step each one needs.

    Each "hole" below is a program that a rule stated in the design document
    (as written before this formalization) would accept.  [eval] computes
    to [RStuck] on it: a genuine runtime type error.  The core's rule is then
    shown to reject the offending step. *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Typing Interp Invariant RtLemmas Soundness Generic Frame Join.
Open Scope string_scope.

Definition nodefs : string -> option prog := fun _ => None.
Definition run (e : prog) : result := eval nodefs 200 [OScope []] 0 [] e.

Definition is_stuck (r : result) : bool := match r with RStuck => true | _ => false end.

(** ** Hole 1: runtime-key [get] on a shape typed by its remainder.

    The doc: "Undeclared keys are read through [get] with a runtime key
    (giving [Maybe] of the remainder ...)".  But the runtime key may name a
    declared field.  [{a: 1}] retyped (fresh) to [{a: int, *: str}], then
    ["a"] read with a runtime key is claimed [Maybe str] but holds an int. *)
Definition hole_dynget : prog :=
  [ WDictNew; WInt 1; WSetK "a"          (* {a: 1}, fresh, {a: int} *)
    (* retype the fresh shape to {a: int, *: str}: allowed *)
  ; WStr "a"; WGetD; WUnwrap              (* doc: str.  actually: 1 *)
  ; WStr "x"; WCat ].                     (* "Cannot concatenate an int" *)

Example hole_dynget_stuck : is_stuck (run hole_dynget) = true.
Proof. vm_compute. reflexivity. Qed.

(** The core's rule needs a result type above every field and the
    remainder; [str] is not above [int]. *)
Example hole_dynget_rejected :
  ~ (forall k, sub (fty (field_at k [("a", FReq TInt)] (FOpt TStr))) TStr).
Proof.
  intros H. specialize (H "a"). unfold field_at in H. simpl in H. inversion H.
Qed.

(** ** Hole 2: an abstract type chosen once per pattern site.

    A kind pattern [list xs] on a value of unknown type binds [xs : [k]].
    If [k] is one rigid type for the pattern site (and may be unified with
    an outer type variable, or reused across loop iterations), the arm is
    effectively typed for *some* element type instead of *every* one.  The
    core types the arm for all element types ([tw_kind_list]).  Here the
    arm is typed as if [k = int] while the list holds strings. *)
Definition hole_exists : prog :=
  [ WNil; WStr "x"; WPush                 (* ["x"], seen as Top *)
  ; WKindIf KList [WInt 0; WGetAt; WInt 1; WAdd] [WDrop; WInt 0] ].

Example hole_exists_stuck : is_stuck (run hole_exists) = true.
Proof. vm_compute. reflexivity. Qed.

(** ** Hole 3: a literal is fresh only if its contents are.

    The doc's ShapeLit rule marks every shape literal fresh.  [{a: @xs}] is
    a literal, but [xs] is shared, so retyping the literal to
    [{a: [int | str]}] lets a string be appended to [xs]. *)
Definition hole_literal : prog :=
  [ WNil; WInt 1; WPush; WStore "xs"      (* xs = [1] *)
  ; WDictNew; WLoad "xs"; WSetK "a"       (* {a: @xs}, doc: fresh *)
    (* doc: retype to {a: [int | str]} *)
  ; WGetReq "a"; WStr "s"; WPush; WDrop   (* append "s" to xs *)
  ; WLoad "xs"; WInt 1; WGetAt; WInt 1; WAdd ].

Example hole_literal_stuck : is_stuck (run hole_literal) = true.
Proof. vm_compute. reflexivity. Qed.

(** In the core, [WSetK] keeps a record fresh only when the stored value is
    fresh ([tw_setk_dp] takes a [Dp] value); a value loaded from a variable
    is [Sh], and [Sh] becomes [Dp] only for immutable types ([ss_imm]). *)
Example hole_literal_rejected : immutable (TList TInt) = false.
Proof. reflexivity. Qed.

(** ** P1 (from the doc): shape field types must be invariant. *)
Example p1_rejected :
  ~ sub (TRec [("a", FReq TInt)] FAbs) (TRec [("a", FReq (TUnion TInt TStr))] FAbs).
Proof.
  intros H. inversion H as [| | | | | | | |fs1 r1 fs2 r2 Hf| |]; subst.
  specialize (Hf "a"). unfold field_at in Hf. simpl in Hf.
  inversion Hf; subst.
  match goal with Hs : sub (TUnion TInt TStr) TInt |- _ => inversion Hs; subst end.
  match goal with Hs : sub TStr TInt |- _ => inversion Hs end.
Qed.

(** ** R6 (from the doc): two refinements of one shared dict.

    Validating in place leaves one object with two incompatible static
    types; the write through [d] breaks the read through [p].  [tryAs]
    never copies, so the core rejects [r6].  With an explicit [copy] before
    the second validation ([r6_copy]) it type-checks and runs. *)
Definition TJ := TRec [("age", FReq TInt)] FAbs.
Definition TP := TRec [("age", FReq TInt)] FOpen.
Definition IS := TUnion TInt TStr.

Definition r6_with (d_src : prog) : prog :=
  ([ WDictNew; WInt 1; WSetK "age"; WStore "j"
   ; WLoad "j"; WTryAs TP; WUnwrap; WStore "p" ]
   ++ d_src ++
   [ WTryAs (TDict IS); WUnwrap; WStore "d"
   ; WLoad "d"; WStr "age"; WStr "x"; WSetD; WDrop
   ; WLoad "p"; WGetReq "age"; WInt 1; WAdd ])%list.

Definition r6 : prog := r6_with [WLoad "j"].
Definition r6_copy : prog := r6_with [WLoad "j"; WCopy].

Example r6_stuck : is_stuck (run r6) = true.
Proof. vm_compute. reflexivity. Qed.

Example r6_copy_runs : exists H, run r6_copy = ROk ONormal H [VInt 2].
Proof. vm_compute. eexists. reflexivity. Qed.

(** The in-place validation of [p] is allowed ([TJ <= TP]: nothing new can
    be written through [p]); the in-place validation of the shared [j] as
    [d] is not ([TJ] is not below [{str: int | str}]). *)
Example r6_p_in_place_ok : sub TJ TP.
Proof.
  apply s_rec. intros k. unfold field_at. simpl.
  destruct (String.eqb k "age"); [apply fs_req; apply s_refl | apply fs_open].
Qed.

Example r6_d_not_below : ~ sub TJ (TDict IS).
Proof.
  intros H. inversion H as [| | | | | | | |fs1 r1 fs2 r2 Hf| |]; subst.
  specialize (Hf "age"). unfold field_at in Hf. simpl in Hf. inversion Hf.
Qed.

Definition nosigs : genv := {| g_sigs := fun _ _ _ => False; g_ctors := fun _ _ => None |}.

(** Since [r6] gets stuck, the soundness theorem says it has no typing
    derivation, in any variable context. *)
Example r6_rejected : forall G s, ~ T nosigs G LNone LNone RNone r6 [] s.
Proof.
  intros G s HT.
  apply (soundness nosigs nodefs (fun f ins outs Hs => match Hs with end) G RNone r6 s HT 200).
  vm_compute. reflexivity.
Qed.

(** The copying version type-checks in the core, so the soundness theorem
    applies to it. *)
Definition GR6 : tenv := [("j", TJ); ("p", TP); ("d", TDict IS)].

Ltac step r := eapply t_cons; [ r | ].

Lemma sub_str_is : sub TStr IS.
Proof. apply s_unionr2, s_refl. Qed.

Example r6_copy_typed : T nosigs GR6 LNone LNone RNone r6_copy [] [(Sh, TInt)].
Proof.
  unfold r6_copy, r6_with. simpl.
  step ltac:(apply tw_dictnew).
  step ltac:(apply tw_int).
  (* make the int fresh (immutable), then strong-update the fresh record *)
  eapply t_sub; [ | eapply t_cons; [apply tw_setk_dp | ] | apply ssub_refl ].
  { constructor; [apply ss_imm; [reflexivity | apply s_refl] | constructor; [| constructor]].
    apply ss_dp, rs_sub, s_refl. }
  (* forget freshness and store j *)
  eapply t_sub; [ | eapply t_cons; [apply tw_store with (t := TJ); reflexivity | ] | apply ssub_refl ].
  { constructor; [| constructor]. apply ss_forget. unfold TJ.
    apply s_rec. intros k. unfold field_at. simpl. destruct (String.eqb k "age");
      [apply fs_req; apply s_refl | apply fs_abs]. }
  step ltac:(apply tw_load with (t := TJ); reflexivity).
  step ltac:(apply tw_try_sub; [reflexivity | apply r6_p_in_place_ok]).
  step ltac:(apply tw_unwrap).
  step ltac:(apply tw_store with (t := TP); reflexivity).
  step ltac:(apply tw_load with (t := TJ); reflexivity).
  (* the explicit copy is fresh, so it is validated in place *)
  step ltac:(apply tw_copy).
  step ltac:(apply tw_try_dp; reflexivity).
  step ltac:(apply tw_unwrap).
  eapply t_sub; [ | eapply t_cons; [apply tw_store with (t := TDict IS); reflexivity | ] | apply ssub_refl ].
  { constructor; [apply ss_forget, s_refl | constructor]. }
  step ltac:(apply tw_load with (t := TDict IS); reflexivity).
  step ltac:(apply tw_str).
  step ltac:(apply tw_str).
  eapply t_sub; [ | eapply t_cons; [apply tw_setd with (t := IS) | ] | apply ssub_refl ].
  { constructor; [apply ss_sh, sub_str_is | apply ssub_refl]. }
  { intros k. unfold field_at. simpl. right; right; reflexivity. }
  step ltac:(apply tw_drop).
  step ltac:(apply tw_load with (t := TP); reflexivity).
  step ltac:(apply tw_getreq; unfold field_at; simpl; reflexivity).
  step ltac:(apply tw_int).
  step ltac:(apply tw_add).
  apply t_nil.
Qed.

Example r6_copy_never_stuck : forall n, eval nodefs n [OScope []] 0 [] r6_copy <> RStuck.
Proof.
  intros n. eapply (soundness nosigs nodefs).
  - intros f ins outs [].
  - exact r6_copy_typed.
Qed.

(** ** The copy is per path

    One list stored under two keys ([{a: @xs, b: @xs}]) is copied twice, so
    the copy is a tree.  Writing [5] into the copy's [a] changes neither
    the copy's [b] nor [xs]: the result is [1 + 1].  A memoizing copy would
    give [5 + 1], and no copy at all [5 + 5]. *)
Definition copy_two_paths : prog :=
  [ WNil; WInt 1; WPush; WStore "xs"
  ; WDictNew; WLoad "xs"; WSetK "a"; WLoad "xs"; WSetK "b"; WStore "r"
  ; WLoad "r"; WCopy; WStore "c"
  ; WLoad "c"; WGetReq "a"; WInt 0; WInt 5; WSetAt; WDrop
  ; WLoad "c"; WGetReq "b"; WInt 0; WGetAt
  ; WLoad "xs"; WInt 0; WGetAt; WAdd ].

Example copy_two_paths_separate : exists H, run copy_two_paths = ROk ONormal H [VInt 2].
Proof. vm_compute. eexists. reflexivity. Qed.

(** And a cyclic value is a checked error, not a type error. *)
Definition copy_cycle : prog :=
  [ WNil; WStore "xs"; WLoad "xs"; WLoad "xs"; WPush; WDrop
  ; WLoad "xs"; WCopy ].

Example copy_cycle_err : run copy_cycle = RErr.
Proof. vm_compute. reflexivity. Qed.

(** * Generic enums

    Enum identities used below.  [en_params] lists each parameter's
    variance and whether it is fresh-covariant (only in data positions). *)
Definition co_f := {| p_var := VCo; p_fresh := true |}.
Definition inv_f := {| p_var := VInv; p_fresh := true |}.
Definition inv_nf := {| p_var := VInv; p_fresh := false |}.

(** [enum Maybe[a] = just a | none end]: covariant, fresh-covariant and
    immutable when its argument is.  This is the built-in [TMaybe] of the
    model, declared as an ordinary generic enum. *)
Definition EMaybe := {| en_name := "Maybe"; en_params := [co_f]; en_imm := true |}.

Example maybe_just_wf : wf_payload EMaybe [TParam 0].
Proof. reflexivity. Qed.
Example maybe_none_wf : wf_payload EMaybe [].
Proof. reflexivity. Qed.

(** [none : Maybe[⊥]] fits every [Maybe[T]], and [Maybe] is covariant. *)
Example maybe_bot_sub : sub (TEnum EMaybe [TBot]) (TEnum EMaybe [TInt]).
Proof. apply s_enum. apply vs_co; [reflexivity | apply s_bot | constructor]. Qed.

Example maybe_co : sub (TEnum EMaybe [TInt]) (TEnum EMaybe [IS]).
Proof. apply s_enum. apply vs_co; [reflexivity | apply s_unionr1, s_refl | constructor]. Qed.

(** A contravariant [Maybe] is rejected by the declaration check. *)
Example maybe_contra_rejected :
  wf_payload {| en_name := "Maybe"; en_params := [{| p_var := VContra; p_fresh := false |}]; en_imm := true |}
             [TParam 0] -> False.
Proof. unfold wf_payload. simpl. discriminate. Qed.

(** A recursive generic enum, [enum List[a] = cons a List[a] | nil end]: the
    recursive occurrence is checked with the variance [List] itself carries. *)
Definition EList := {| en_name := "List"; en_params := [co_f]; en_imm := true |}.
Example list_cons_wf : wf_payload EList [TParam 0; TEnum EList [TParam 0]].
Proof. reflexivity. Qed.

(** Non-regular recursion, [enum Nest[a] = nest a Nest[[a]] | stop end], is
    also well formed in the model (with [a] invariant, since it appears in a
    list).  The design's same-parameters restriction is not needed for
    soundness: it is for the checker and validator. *)
Definition ENest := {| en_name := "Nest"; en_params := [inv_f]; en_imm := false |}.
Example nest_wf : wf_payload ENest [TParam 0; TEnum ENest [TList (TParam 0)]].
Proof. reflexivity. Qed.

(** ** Hole 4: the variance of a parameter used in a list

    [enum Box[a] = box [a] end].  A shared box is a second view of its list.
    If [Box] were covariant, or declared immutable (so a shared box could be
    made fresh and retyped), [@xs box] could be seen as [Box[int | str]] and
    a string appended to [xs]. *)
Definition box_prog (E : ename) : prog :=
  [ WNil; WInt 1; WPush; WStore "xs"                       (* xs = [1] *)
  ; WLoad "xs"; WCon E "box" [TList (TParam 0)]             (* a box around the shared xs *)
  ; WCase E [("box", [WStr "s"; WPush; WDrop])]             (* append "s" through the box *)
  ; WLoad "xs"; WInt 1; WGetAt; WInt 1; WAdd ].             (* "s" + 1 *)

Definition EBox := {| en_name := "Box"; en_params := [inv_f]; en_imm := false |}.
Definition EBoxCo := {| en_name := "Box"; en_params := [co_f]; en_imm := false |}.
Definition EBoxImm := {| en_name := "Box"; en_params := [inv_f]; en_imm := true |}.

Example hole_box_stuck : is_stuck (run (box_prog EBox)) = true.
Proof. vm_compute. reflexivity. Qed.

Example box_wf : wf_payload EBox [TList (TParam 0)].
Proof. reflexivity. Qed.

(** The correct declaration is invariant: [Box[int]] is not below [Box[int | str]]. *)
Example box_invariant : ~ sub (TEnum EBox [TInt]) (TEnum EBox [IS]).
Proof.
  intros H. inversion H; subst. match goal with V : vsubs _ _ _ |- _ => inversion V; subst end;
    try discriminate.
  match goal with Hs : sub IS TInt |- _ => inversion Hs; subst end.
  match goal with Hs : sub TStr TInt |- _ => inversion Hs end.
Qed.

(** A covariant [Box], or an immutable one, is rejected by the declaration check. *)
Example box_co_rejected : wf_payload EBoxCo [TList (TParam 0)] -> False.
Proof. unfold wf_payload. simpl. discriminate. Qed.
Example box_imm_rejected : wf_payload EBoxImm [TList (TParam 0)] -> False.
Proof. unfold wf_payload. simpl. discriminate. Qed.

(** And since the program gets stuck, no declaration environment types it,
    whatever variances [Box] is given. *)
Example hole_box_rejected : forall E ctors G s, E = EBox \/ E = EBoxCo \/ E = EBoxImm ->
  ~ T {| g_sigs := fun _ _ _ => False; g_ctors := ctors |} G LNone LNone RNone (box_prog E) [] s.
Proof.
  intros E ctors G s HE HT.
  refine (soundness _ nodefs _ G RNone (box_prog E) s HT 200 _); [intros f ins outs [] |].
  destruct HE as [->|[->| ->]]; vm_compute; reflexivity.
Qed.

(** A *fresh* box may be retyped: its list has no other reference. *)
Example box_fresh_retype : rsub (TEnum EBox [TInt]) (TEnum EBox [IS]).
Proof. apply rs_enum. apply vrs_fresh; [reflexivity | apply rs_sub, s_unionr1, s_refl | constructor]. Qed.

(** ** Hole 5: fresh retyping stops at quotes

    [enum F[a] = f (a -- a) end].  A fresh [F[int]] holds only a quote, and
    a quote is not data: retyping the value to [F[int | str]] would let the
    quote [(1 +)] be called with a string.  So a parameter used under a quote
    is never fresh-covariant, and here it is invariant. *)
Definition QF := TQuote [TParam 0] (Some [TParam 0]).
Definition hole_quote_arg (E : ename) : prog :=
  [ WQuote [WInt 1; WAdd]; WCon E "f" [QF]                  (* a fresh F[int] *)
  ; WCase E [("f", [WStr "x"; WSwap; WExec])] ].            (* call it with "x" *)

Definition EF := {| en_name := "F"; en_params := [inv_nf]; en_imm := true |}.
Definition EFfresh := {| en_name := "F"; en_params := [inv_f]; en_imm := true |}.

Example hole_quote_arg_stuck : is_stuck (run (hole_quote_arg EF)) = true.
Proof. vm_compute. reflexivity. Qed.

Example f_wf : wf_payload EF [QF].
Proof. reflexivity. Qed.

Example f_fresh_rejected : wf_payload EFfresh [QF] -> False.
Proof. unfold wf_payload. simpl. discriminate. Qed.

(** ** Hole 6: joins of fresh quotes

    The same holds without enums.  If a join "widened inside" two fresh
    quotes, the arms [(1 +)] and [("a" ++)] would join to
    [(int | str -- int | str)].  Retyping a fresh quote is only subtyping. *)
Definition hole_quote_join : prog :=
  [ WBool true; WIf [WQuote [WInt 1; WAdd]] [WQuote [WStr "a"; WCat]]
  ; WStr "x"; WSwap; WExec ].

Example hole_quote_join_stuck : is_stuck (run hole_quote_join) = true.
Proof. vm_compute. reflexivity. Qed.

Example quote_no_widen : ~ rsub (TQuote [TInt] (Some [TInt])) (TQuote [IS] (Some [IS])).
Proof.
  intros H. inversion H; subst.
  match goal with Hs : sub _ _ |- _ => inversion Hs; subst end.
  match goal with Hs : subs _ _ |- _ => inversion Hs; subst end.
  match goal with Hs : sub IS TInt |- _ => inversion Hs; subst end.
  match goal with Hs : sub TStr TInt |- _ => inversion Hs end.
Qed.

(** * Type variables (Generic.v)

    A body is checked once with its type variables rigid.  Each rule below
    is what makes that sound; each program gets stuck under the alternative. *)

Definition defs1 (f : string) (b : prog) : string -> option prog :=
  fun g => if String.eqb g f then Some b else None.

(** ** Hole 7: a type variable counted as immutable

    [def g (a -- Maybe[[str]]) tryAs [str] end].  If [a] were immutable,
    the shared argument could be made fresh ([ss_imm]) and validated in
    place.  Called with a shared empty [[int]], it returns the same list
    as a [[str]]. *)
Definition hole_tvar_imm : prog :=
  [ WNil; WStore "xs"
  ; WLoad "xs"; WCall "g"; WUnwrap; WStr "s"; WPush; WDrop
  ; WLoad "xs"; WInt 0; WGetAt; WInt 1; WAdd ].

Example hole_tvar_imm_stuck :
  is_stuck (eval (defs1 "g" [WTryAs (TList TStr)]) 200 [OScope []] 0 [] hole_tvar_imm) = true.
Proof. vm_compute. reflexivity. Qed.

Example tvar_not_immutable : ~ slot_sub (Sh, TVar 0) (Dp, TVar 0).
Proof. intros H. inversion H; subst. discriminate. Qed.

(** ** Hole 8: a kind pattern on a type variable

    [def h (a -- int) match str x : x 1 + , _ : drop 0 end end].  If a kind
    pattern on [a] were read as "a has no member of kind str", the arm would
    be checked vacuously.  At the instance [a = str] it runs. *)
Definition hole_tvar_kind : prog := [ WStr "x"; WCall "h" ].

Example hole_tvar_kind_stuck :
  is_stuck (eval (defs1 "h" [WKindIf KStr [WInt 1; WAdd] [WDrop; WInt 0]]) 200 [OScope []] 0 []
                 hole_tvar_kind) = true.
Proof. vm_compute. reflexivity. Qed.

(** The arm sees the unknown contents of that kind, [str], not nothing. *)
Example tvar_kind_then : kind_then KStr (TVar 0) = Some TStr.
Proof. reflexivity. Qed.

(** ** A [tryAs] target has no type variable

    Types are erased: at runtime [tryAs a] has nothing to check against.
    The validator fails on a type variable, and the typing rules ask for
    closed targets, which is what the substitution lemma needs. *)
Example tvar_not_checkable : validate 10 [] (VInt 1) (TVar 0) = Some false.
Proof. reflexivity. Qed.

(** * Dead code after a diverging word

    [def f ( -- never) 1 exit 1 + end]: the [1 +] after [exit] cannot run.
    The checker's "diverging effect absorbs what follows" is the rule
    [t_div]; with it the body checks as [never]. *)
Example dead_code_never : diverges nosigs [] LNone LNone RNone [WInt 1; WExit; WInt 1; WAdd] [].
Proof. eapply div_tail; [apply tw_int |]. apply div_head. apply div_exit. Qed.

(** * Joins (Join.v)

    [Maybe] joins inside, and that is safe for shared values, but the join
    inside must itself be a shared join.  Arms leaving [@xs just] and
    [@ys just] with [xs : [int]], [ys : [str]] stored must not join to
    [Maybe[[int | str]]]: the list inside is still [xs]. *)
Definition hole_maybe_join : prog :=
  [ WNil; WInt 1; WPush; WStore "xs"; WNil; WStr "a"; WPush; WStore "ys"
  ; WBool true; WIf [WLoad "xs"; WJust] [WLoad "ys"; WJust]
  ; WUnwrap; WStr "s"; WPush; WDrop
  ; WLoad "xs"; WInt 1; WGetAt; WInt 1; WAdd ].

Example hole_maybe_join_stuck : is_stuck (run hole_maybe_join) = true.
Proof. vm_compute. reflexivity. Qed.

(** The join of the two shared slots does not exist ... *)
Example maybe_join_shared :
  join_slot (Sh, TMaybe (TList TInt)) (Sh, TMaybe (TList TStr)) = None.
Proof. reflexivity. Qed.

(** ... while fresh arms, [[1] just] and [["a"] just], join inside. *)
Example maybe_join_fresh :
  join_slot (Dp, TMaybe (TList TInt)) (Dp, TMaybe (TList TStr)) =
  Some (Dp, TMaybe (TList (TUnion TInt TStr))).
Proof. reflexivity. Qed.

(** The design's join table, computed. *)
Example join_int_float_like : join_slot (Sh, TInt) (Sh, TStr) = Some (Sh, TUnion TInt TStr).
Proof. reflexivity. Qed.
Example join_none_just : join_slot (Sh, TMaybe TBot) (Sh, TMaybe TInt) = Some (Sh, TMaybe TInt).
Proof. reflexivity. Qed.
Example join_fresh_shapes :
  join_slot (Dp, TRec [("a", FReq TInt)] FAbs) (Dp, TRec [("a", FReq TInt); ("b", FReq TInt)] FAbs) =
  Some (Dp, TRec [("a", FReq TInt); ("a", FReq TInt); ("b", FOpt TInt)] FAbs).
Proof. reflexivity. Qed.
Example join_quotes :
  join_slot (Dp, TQuote [TInt] (Some [TInt])) (Dp, TQuote [TStr] (Some [TStr])) = None.
Proof. reflexivity. Qed.

(** * [return] in top-level code

    [tests/success/return_top_level.msh]: a top-level [return] ends the
    script.  Top-level code has the return context [RAny]. *)
Definition top_return : prog := [ WBool true; WIf [WStr "in if"; WDrop; WReturn] []; WStr "not reached"; WDrop ].

Example top_return_typed : T nosigs [] LNone LNone RAny top_return [] [].
Proof.
  unfold top_return. eapply t_cons; [apply tw_bool |].
  eapply t_cons; [apply tw_if; [| apply t_nil] |].
  - eapply t_cons; [apply tw_str |]. eapply t_cons; [apply tw_drop |].
    eapply t_cons; [apply tw_return_any |]. apply t_nil.
  - eapply t_cons; [apply tw_str |]. eapply t_cons; [apply tw_drop |]. apply t_nil.
Qed.

Example top_return_never_stuck : forall n, eval nodefs n [OScope []] 0 [] top_return <> RStuck.
Proof.
  intros n. eapply (soundness nosigs nodefs); [intros f ins outs [] | exact top_return_typed].
Qed.

(** * Renaming a variable stored at a new type

    The design elaborates "[x!] reassigned at a new type, not captured by
    any quote" by renaming the variable.  The core then checks the renamed
    program, but the runtime runs the original one, so the renaming must not
    change what the program does.  Two ways a naive renaming does:

    - a loop body (a literal quote given to [loop] or [each]) that stores
      at the new type is read again on the next run;
    - a read after an [if] whose arms stored at different types. *)

(** [1 x!  [0 0] (drop @x 1 + drop "a" x!) each]: the second run reads ["a"]. *)
Definition loop_orig (x_first x_read x_second : string) : prog :=
  [ WNil; WInt 0; WPush; WInt 0; WPush; WStore "xs"
  ; WInt 1; WStore x_first
  ; WLoad "xs"; WEach [WDrop; WLoad x_read; WInt 1; WAdd; WDrop; WStr "a"; WStore x_second] ].

Example rename_loop_orig_stuck : is_stuck (run (loop_orig "x" "x" "x")) = true.
Proof. vm_compute. reflexivity. Qed.

(** The renamed program checks, so it never gets stuck; the original does. *)
Definition GL : tenv := [("xs", TList TInt); ("x1", TInt); ("x2", TStr)].

Example rename_loop_renamed_typed : T nosigs GL LNone LNone RNone (loop_orig "x1" "x1" "x2") [] [].
Proof.
  unfold loop_orig.
  step ltac:(apply tw_nil with (t := TInt)).
  step ltac:(apply tw_int).
  eapply t_sub; [ | eapply t_cons; [apply tw_push_dp | ] | apply ssub_refl ].
  { constructor; [apply ss_imm; [reflexivity | apply s_refl] | apply ssub_refl]. }
  step ltac:(apply tw_int).
  eapply t_sub; [ | eapply t_cons; [apply tw_push_dp | ] | apply ssub_refl ].
  { constructor; [apply ss_imm; [reflexivity | apply s_refl] | apply ssub_refl]. }
  eapply t_sub; [ | eapply t_cons; [apply tw_store with (t := TList TInt); reflexivity | ] | apply ssub_refl ].
  { constructor; [apply ss_forget, s_refl | constructor]. }
  step ltac:(apply tw_int).
  step ltac:(apply tw_store with (t := TInt); reflexivity).
  step ltac:(apply tw_load with (t := TList TInt); reflexivity).
  step ltac:(eapply tw_each with (B' := LNone) (C' := LNone); [constructor | constructor | ]).
  - step ltac:(apply tw_drop).
    step ltac:(apply tw_load with (t := TInt); reflexivity).
    step ltac:(apply tw_int).
    step ltac:(apply tw_add).
    step ltac:(apply tw_drop).
    step ltac:(apply tw_str).
    step ltac:(apply tw_store with (t := TStr); reflexivity).
    apply t_nil.
  - apply t_nil.
Qed.

Example rename_loop_renamed_runs : exists H, run (loop_orig "x1" "x1" "x2") = ROk ONormal H [].
Proof. vm_compute. eexists. reflexivity. Qed.

(** [false if 1 x! else "a" x! end  @x 1 +]: after the [if], [x] is [1] or ["a"]. *)
Definition if_orig (x_then x_else x_read : string) : prog :=
  [ WBool false; WIf [WInt 1; WStore x_then] [WStr "a"; WStore x_else]
  ; WLoad x_read; WInt 1; WAdd ].

Example rename_if_orig_stuck : is_stuck (run (if_orig "x" "x" "x")) = true.
Proof. vm_compute. reflexivity. Qed.

Definition GI : tenv := [("x1", TInt); ("x2", TStr)].

Example rename_if_renamed_typed : T nosigs GI LNone LNone RNone (if_orig "x1" "x2" "x1") [] [(Sh, TInt)].
Proof.
  unfold if_orig.
  step ltac:(apply tw_bool).
  step ltac:(apply tw_if).
  - step ltac:(apply tw_int). step ltac:(apply tw_store with (t := TInt); reflexivity). apply t_nil.
  - step ltac:(apply tw_str). step ltac:(apply tw_store with (t := TStr); reflexivity). apply t_nil.
  - step ltac:(apply tw_load with (t := TInt); reflexivity).
    step ltac:(apply tw_int). step ltac:(apply tw_add). apply t_nil.
Qed.

(** The renamed program reads an unset variable, a checked error: it does
    not do what the original does. *)
Example rename_if_renamed_differs : run (if_orig "x1" "x2" "x1") = RErr.
Proof. vm_compute. reflexivity. Qed.

(** * Fresh def outputs

    A signature may mark an output fresh.  [def mk ( -- [int] fresh) [1] end]
    returns a new list, so the caller may widen it in place, as it could a
    literal: [mk as [int | str] "s" append]. *)
Definition sigs_mk : genv :=
  {| g_sigs := fun f ins outs => f = "mk" /\ ins = [] /\ outs = Some [(Dp, TList TInt)];
     g_ctors := fun _ _ => None |}.

Definition mk_body : prog := [WNil; WInt 1; WPush].

Lemma mk_def_ok : def_ok sigs_mk (defs1 "mk" mk_body).
Proof.
  intros f ins outs (-> & -> & ->). exists mk_body, []. split; [reflexivity|]. intros s0. simpl.
  step ltac:(apply tw_nil).
  step ltac:(apply tw_int).
  eapply t_sub; [ | eapply t_cons; [apply tw_push_dp | apply t_nil] | apply ssub_refl ].
  constructor; [apply ss_imm; [reflexivity | apply s_refl] | apply ssub_refl].
Qed.

Definition mk_use : prog := [WCall "mk"; WStr "s"; WPush; WDrop].

Example mk_use_typed : T sigs_mk [] LNone LNone RNone mk_use [] [].
Proof.
  unfold mk_use.
  eapply t_cons; [apply tw_call with (ins := []) (outs := [(Dp, TList TInt)]); simpl; auto |]. simpl.
  step ltac:(apply tw_str).
  (* widen the fresh list, and make the string fresh (it is immutable) *)
  eapply t_sub; [ | eapply t_cons; [apply tw_push_dp with (t := IS) | ] | apply ssub_refl ].
  { constructor; [apply ss_imm; [reflexivity | apply sub_str_is] |].
    constructor; [apply ss_dp, rs_list, rs_sub, s_unionr1, s_refl | constructor]. }
  step ltac:(apply tw_drop). apply t_nil.
Qed.

Example mk_use_never_stuck : forall n, eval (defs1 "mk" mk_body) n [OScope []] 0 [] mk_use <> RStuck.
Proof. intros n. eapply (soundness sigs_mk); [exact mk_def_ok | exact mk_use_typed]. Qed.

(** * [map] results

    [map]'s result holds the body's results, which are shared values.  If it
    were marked "fresh when the input is fresh", [[0 0] (drop @ys) map] (the
    list [ys] twice) could be widened to [[[int | str]]], and a string
    appended to [ys] through it. *)
Definition hole_map_fresh : prog :=
  [ WNil; WInt 1; WPush; WStore "ys"
  ; WNil; WInt 0; WPush; WInt 0; WPush; WMap [WDrop; WLoad "ys"]
  ; WInt 0; WGetAt; WStr "s"; WPush; WDrop
  ; WLoad "ys"; WInt 1; WGetAt; WInt 1; WAdd ].

Example hole_map_fresh_stuck : is_stuck (run hole_map_fresh) = true.
Proof. vm_compute. reflexivity. Qed.

(** In the core, a [map] result is fresh only when its element type is
    immutable ([tw_map_imm]); [[int]] is not. *)
Example map_result_not_fresh : immutable (TList TInt) = false.
Proof. reflexivity. Qed.

(** With immutable results it is fresh: [[1 2] (1 +) map as [int | str] "s" append]. *)
Definition map_widen : prog :=
  [ WNil; WInt 1; WPush; WInt 2; WPush; WMap [WInt 1; WAdd]; WStr "s"; WPush; WDrop ].

Example map_widen_typed : T nosigs [] LNone LNone RNone map_widen [] [].
Proof.
  unfold map_widen.
  step ltac:(apply tw_nil with (t := TInt)).
  step ltac:(apply tw_int).
  eapply t_sub; [ | eapply t_cons; [apply tw_push_dp | ] | apply ssub_refl ].
  { constructor; [apply ss_imm; [reflexivity | apply s_refl] | apply ssub_refl]. }
  step ltac:(apply tw_int).
  eapply t_sub; [ | eapply t_cons; [apply tw_push_dp | ] | apply ssub_refl ].
  { constructor; [apply ss_imm; [reflexivity | apply s_refl] | apply ssub_refl]. }
  eapply t_sub; [ | eapply t_cons;
    [eapply tw_map_imm with (u := TInt) (B' := LNone) (C' := LNone); [reflexivity | constructor | constructor |] | ]
    | apply ssub_refl ].
  { constructor; [apply ss_forget, s_refl | constructor]. }
  - step ltac:(apply tw_int). step ltac:(apply tw_add). apply t_nil.
  - step ltac:(apply tw_str).
    eapply t_sub; [ | eapply t_cons; [apply tw_push_dp with (t := IS) | ] | apply ssub_refl ].
    { constructor; [apply ss_imm; [reflexivity | apply sub_str_is] |].
      constructor; [apply ss_dp, rs_list, rs_sub, s_unionr1, s_refl | constructor]. }
    step ltac:(apply tw_drop). apply t_nil.
Qed.

Example map_widen_runs : exists H, run map_widen = ROk ONormal H [].
Proof. vm_compute. eexists. reflexivity. Qed.

(** * Builtins whose quote sees the elements

    The design listed [filter] (and [sortBy], [groupBy]) as "fresh when the
    input is fresh", like [take].  But the quote is given each element and
    may store it, so the result's elements may be shared even when the input
    list was fresh.  A [map] whose body keeps its element is [filter]
    keeping everything: [[[1]] (dup e!) map], widened to [[[int | str]]],
    lets a string be appended to the list in [e]. *)
Definition hole_filter_fresh : prog :=
  [ WNil; WNil; WInt 1; WPush; WPush         (* [[1]], fresh *)
  ; WMap [WDup; WStore "e"]                  (* keeps each element; e is the inner list *)
    (* "fresh when the input is fresh": retype to [[int | str]] *)
  ; WInt 0; WGetAt; WStr "a"; WPush; WDrop   (* append "a" to the inner list *)
  ; WLoad "e"; WInt 1; WGetAt; WInt 1; WAdd ].

Example hole_filter_fresh_stuck : is_stuck (run hole_filter_fresh) = true.
Proof. vm_compute. reflexivity. Qed.

(** * Match bindings are variables

    At runtime a match arm's bindings are stored in the enclosing variable
    scope ([maps.Copy(frame.Context.Variables, bindings)] in Evaluator.go),
    like [x!].  So a binding is an ordinary variable with one type per
    scope.  Typing each arm's binding separately is the renaming of
    [rename_*] again, and it changes what the program does.

    [q! [1 "s"] (match int n : (@n) q! , str n : @q x 1 + drop end) each]:
    the quote made in the [int] arm reads [n] when it runs, and by then the
    [str] arm has stored ["s"] there. *)
Definition arms_prog (n_int n_str : string) : prog :=
  [ WQuote [WInt 0]; WStore "q"
  ; WNil; WInt 1; WPush; WStr "s"; WPush     (* [1 "s"] : [int | str] *)
  ; WEach [ WKindIf KInt [WStore n_int; WQuote [WLoad n_int]; WStore "q"]
                         [WStore n_str; WLoad "q"; WExec; WInt 1; WAdd; WDrop] ] ].

Example arms_same_name_stuck : is_stuck (run (arms_prog "n" "n")) = true.
Proof. vm_compute. reflexivity. Qed.

(** One name per arm type-checks and runs; the same name in both arms has no
    typing in any context. *)
Definition GA : tenv := [("q", TQuote [] (Some [TInt])); ("n1", TInt); ("n2", TStr)].

Example arms_two_names_typed : T nosigs GA LNone LNone RNone (arms_prog "n1" "n2") [] [].
Proof.
  unfold arms_prog.
  step ltac:(apply tw_quote with (ins := []) (outs := [TInt])).
  { intros s0. simpl. step ltac:(apply tw_int). apply t_nil. }
  step ltac:(apply tw_store with (t := TQuote [] (Some [TInt])); reflexivity).
  step ltac:(apply tw_nil with (t := IS)).
  step ltac:(apply tw_int).
  eapply t_sub; [ | eapply t_cons; [apply tw_push_dp | ] | apply ssub_refl ].
  { constructor; [apply ss_imm; [reflexivity | apply s_unionr1, s_refl] | apply ssub_refl]. }
  step ltac:(apply tw_str).
  eapply t_sub; [ | eapply t_cons; [apply tw_push_dp | ] | apply ssub_refl ].
  { constructor; [apply ss_imm; [reflexivity | apply sub_str_is] | apply ssub_refl]. }
  eapply t_sub; [ | eapply t_cons;
    [eapply tw_each with (B' := LNone) (C' := LNone); [constructor | constructor | ] | ]
    | apply ssub_refl ].
  { constructor; [apply ss_forget, s_refl | constructor]. }
  - step ltac:(apply tw_kind with (t1 := TInt); [reflexivity | | ]).
    + step ltac:(apply tw_store with (t := TInt); reflexivity).
      step ltac:(apply tw_quote with (ins := []) (outs := [TInt])).
      { intros s0. simpl. step ltac:(apply tw_load with (t := TInt); reflexivity). apply t_nil. }
      step ltac:(apply tw_store with (t := TQuote [] (Some [TInt])); reflexivity).
      apply t_nil.
    + cbn.
      step ltac:(apply tw_store with (t := TStr); reflexivity).
      step ltac:(apply tw_load with (t := TQuote [] (Some [TInt])); reflexivity).
      step ltac:(apply tw_exec with (ins := []) (outs := [TInt]) (s := [])).
      simpl. step ltac:(apply tw_int). step ltac:(apply tw_add). step ltac:(apply tw_drop).
      apply t_nil.
    + apply t_nil.
  - apply t_nil.
Qed.

Example arms_two_names_runs : exists H, run (arms_prog "n1" "n2") = ROk ONormal H [].
Proof. vm_compute. eexists. reflexivity. Qed.

Example arms_same_name_rejected : forall G s, ~ T nosigs G LNone LNone RNone (arms_prog "n" "n") [] s.
Proof.
  intros G s HT.
  apply (soundness nosigs nodefs (fun f ins outs Hs => match Hs with end) G RNone _ s HT 200).
  vm_compute. reflexivity.
Qed.

(** ** A binding of unknown contents cannot be a variable

    [list xs] on a value of unknown type gives [xs] an abstract element
    type, fixed for one run of the arm ([tw_kind_list] checks the arm for
    every element type).  A variable outlives the run.  Even when the checker
    keeps [xs]'s type to the arm, a call made inside the arm can run the same
    pattern again and store another list in [xs]:

    [(match list xs : @xs 0 getAt  @go if false go! @l2 @g x end  @xs swap append drop , _ : drop end) g!]
    [@l1 @g x] with [l1 = [1]], [l2 = ["s"]] appends [1] to [l2]. *)
Definition reentry_arm : prog :=
  [ WStore "xs"
  ; WLoad "xs"; WInt 0; WGetAt                               (* an element of this run's list *)
  ; WLoad "go"; WIf [WBool false; WStore "go"; WLoad "l2"; WLoad "g"; WExec] []
  ; WLoad "xs"; WSwap; WPush; WDrop ].                       (* xs now holds the inner run's list *)

Definition hole_bind_reentry : prog :=
  [ WNil; WInt 1; WPush; WStore "l1"
  ; WNil; WStr "s"; WPush; WStore "l2"
  ; WBool true; WStore "go"
  ; WQuote [WKindIf KList reentry_arm [WDrop]]; WStore "g"
  ; WLoad "l1"; WLoad "g"; WExec
  ; WLoad "l2"; WInt 2; WGetAt; WStr "x"; WCat ].

Example hole_bind_reentry_stuck : is_stuck (run hole_bind_reentry) = true.
Proof. vm_compute. reflexivity. Qed.

Example hole_bind_reentry_rejected : forall G s, ~ T nosigs G LNone LNone RNone hole_bind_reentry [] s.
Proof.
  intros G s HT.
  apply (soundness nosigs nodefs (fun f ins outs Hs => match Hs with end) G RNone _ s HT 200).
  vm_compute. reflexivity.
Qed.

(** Keeping the value on the stack ([list :>]) is the core's own form: the
    arm may reorder the list's elements.  [["x"] u!  @u match list :> dup 0 getAt append drop , _ : drop end]
    with [u] of unknown type. *)
Definition keep_on_stack : prog :=
  [ WNil; WStr "x"; WPush; WStore "u"
  ; WLoad "u"; WKindIf KList [WDup; WInt 0; WGetAt; WPush; WDrop] [WDrop] ].

Example keep_on_stack_typed : T nosigs [("u", TTop)] LNone LNone RNone keep_on_stack [] [].
Proof.
  unfold keep_on_stack.
  step ltac:(apply tw_nil with (t := TStr)).
  step ltac:(apply tw_str).
  eapply t_sub; [ | eapply t_cons; [apply tw_push_dp | ] | apply ssub_refl ].
  { constructor; [apply ss_imm; [reflexivity | apply s_refl] | apply ssub_refl]. }
  eapply t_sub; [ | eapply t_cons; [apply tw_store with (t := TTop); reflexivity | ] | apply ssub_refl ].
  { constructor; [apply ss_forget, s_top | constructor]. }
  step ltac:(apply tw_load with (t := TTop); reflexivity).
  step ltac:(apply tw_kind_list with (s' := [])).
  - intros a.
    step ltac:(apply tw_dup). step ltac:(apply tw_int). step ltac:(apply tw_getat).
    step ltac:(apply tw_push_sh). step ltac:(apply tw_drop). apply t_nil.
  - step ltac:(apply tw_drop). apply t_nil.
  - apply t_nil.
Qed.

Example keep_on_stack_never_stuck : forall n, eval nodefs n [OScope []] 0 [] keep_on_stack <> RStuck.
Proof. intros n. eapply (soundness nosigs nodefs); [intros f ins outs [] | exact keep_on_stack_typed]. Qed.
