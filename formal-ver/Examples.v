(** * Examples: the interpreter really gets stuck on the unsound programs,
    and the core rejects the step each one needs.

    Each "hole" below is a program that a rule stated in the design document
    (as written before this formalization) would accept.  [eval] computes
    to [RStuck] on it: a genuine runtime type error.  The core's rule is then
    shown to reject the offending step. *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Typing Interp Invariant RtLemmas Soundness.
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
  intros H. inversion H as [| | | | | | | |fs1 r1 fs2 r2 Hf|]; subst.
  specialize (Hf "a"). unfold field_at in Hf. simpl in Hf.
  inversion Hf; subst.
  match goal with Hs : sub (TUnion TInt TStr) TInt |- _ => inversion Hs; subst end.
  match goal with Hs : sub TStr TInt |- _ => inversion Hs end.
Qed.

(** ** R6 (from the doc): two refinements of one shared dict.

    Validating in place leaves one object with two incompatible static
    types; the write through [d] breaks the read through [p]. *)
Definition TJ := TRec [("age", FReq TInt)] FAbs.
Definition TP := TRec [("age", FReq TInt)] FOpen.
Definition IS := TUnion TInt TStr.

Definition r6 (copy_d : bool) : prog :=
  [ WDictNew; WInt 1; WSetK "age"; WStore "j"
  ; WLoad "j"; WTryAs TP false; WUnwrap; WStore "p"
  ; WLoad "j"; WTryAs (TDict IS) copy_d; WUnwrap; WStore "d"
  ; WLoad "d"; WStr "age"; WStr "x"; WSetD; WDrop
  ; WLoad "p"; WGetReq "age"; WInt 1; WAdd ].

Example r6_in_place_stuck : is_stuck (run (r6 false)) = true.
Proof. vm_compute. reflexivity. Qed.

Example r6_copy_runs : exists H, run (r6 true) = ROk ONormal H [VInt 2].
Proof. vm_compute. eexists. reflexivity. Qed.

(** The in-place validation of [p] is allowed ([TJ <= TP]: nothing new can
    be written through [p]); the in-place validation of [d] is not
    ([TJ] is not below [{str: int | str}]), so the core requires the copy. *)
Example r6_p_in_place_ok : sub TJ TP.
Proof.
  apply s_rec. intros k. unfold field_at. simpl.
  destruct (String.eqb k "age"); [apply fs_req; apply s_refl | apply fs_open].
Qed.

Example r6_d_needs_copy : ~ sub TJ (TDict IS).
Proof.
  intros H. inversion H as [| | | | | | | |fs1 r1 fs2 r2 Hf|]; subst.
  specialize (Hf "age"). unfold field_at in Hf. simpl in Hf. inversion Hf.
Qed.

(** And the copying version type-checks in the core, so the soundness
    theorem applies to it. *)
Definition GR6 : tenv := [("j", TJ); ("p", TP); ("d", TDict IS)].
Definition nosigs : string -> list ty -> option (list ty) -> Prop := fun _ _ _ => False.

Ltac step r := eapply t_cons; [ r | ].

Lemma sub_str_is : sub TStr IS.
Proof. apply s_unionr2, s_refl. Qed.

Example r6_typed : T nosigs GR6 LNone LNone None (r6 true) [] [(Sh, TInt)].
Proof.
  unfold r6.
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
  step ltac:(apply tw_try_sub; apply r6_p_in_place_ok).
  step ltac:(apply tw_unwrap).
  step ltac:(apply tw_store with (t := TP); reflexivity).
  step ltac:(apply tw_load with (t := TJ); reflexivity).
  step ltac:(apply tw_try_copy).
  step ltac:(apply tw_unwrap).
  step ltac:(apply tw_store with (t := TDict IS); reflexivity).
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

Example r6_never_stuck : forall n, eval nodefs n [OScope []] 0 [] (r6 true) <> RStuck.
Proof.
  intros n. eapply (soundness nosigs nodefs).
  - intros f ins outs [].
  - exact r6_typed.
Qed.
