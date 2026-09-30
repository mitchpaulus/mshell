(** * Checkable types: validation never rejects a well-typed value.

    [tryAs T] and [is T x] validate a value against [T] at runtime.  An
    exhaustiveness check that counts an [is T] arm as covering a member [M]
    of the matched type relies on the converse of soundness: a value that
    has type [M] (so also [T], when [M <= T]) is never rejected by the
    validator.  That holds only for *checkable* targets:

    - no quote type anywhere, including inside the payload types of an enum
      the target mentions ([validate] cannot look inside a closure, so it
      returns [false] for every quote type);
    - no type variable or enum parameter (types are erased);
    - no remainder that makes every undeclared key required (not surface
      syntax).

    Enums are checkable by declaration: [okE] names the enums whose payload
    types (with their parameters left open) are checkable, and
    [okE_sound] says the declarations agree.  A recursive enum is checked
    against [okE] itself, as [en_imm] is.

    [validate_complete] (shared values) and [validate_complete_fresh]
    (fresh values): for a checkable [t], validating a value of type [t]
    against [t] succeeds, or runs out of its work budget (a checked error).
    It never answers [false]. *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Typing Interp Invariant RtLemmas Validate Generic.

Definition is_req (f : fstat) : bool := match f with FReq _ => true | _ => false end.

(** [pok]: whether an enum parameter is allowed (true in payload types,
    where it is substituted away before validation). *)
Fixpoint chk (okE : ename -> bool) (pok : bool) (t : ty) : bool :=
  match t with
  | TInt | TStr | TBool | TBot | TTop => true
  | TParam _ => pok
  | TVar _ | TQuote _ _ => false
  | TMaybe t' | TList t' => chk okE pok t'
  | TRec fs r => forallb (fun kf => fchk okE pok (snd kf)) fs && fchk okE pok r && negb (is_req r)
  | TUnion a b => chk okE pok a && chk okE pok b
  | TEnum E a => okE E && forallb (chk okE pok) a
  | TMu t' => chk okE false t'   (* the greatest fixed point: the variable counts as checkable *)
  | TRV _ => true
  end
with fchk (okE : ename -> bool) (pok : bool) (f : fstat) : bool :=
  match f with
  | FReq t | FOpt t | FDict t => chk okE pok t
  | FAbs | FOpen => true
  end.

Lemma forallb_in' {A} (g : A -> bool) l x : forallb g l = true -> In x l -> g x = true.
Proof. intros H Hx. rewrite forallb_forall in H. auto. Qed.

(** Substituting checkable arguments into a checkable payload type gives a
    checkable type. *)
Lemma chk_subst okE a : forallb (chk okE false) a = true ->
  forall p, chk okE true p = true -> chk okE false (subst a p) = true.
Proof.
  intros Ha.
  apply (ty_ind2 (fun p => chk okE true p = true -> chk okE false (subst a p) = true)
                 (fun f => fchk okE true f = true -> fchk okE false (fsubst a f) = true));
    simpl; intros; auto; try discriminate.
  - apply andb_true_iff in H1 as [H1 Hr]. apply andb_true_iff in H1 as [Hfs Hr0].
    apply andb_true_iff; split; [apply andb_true_iff; split|].
    + apply forallb_forall. intros kf Hkf. apply in_map_iff in Hkf as (p & <- & Hp). simpl.
      rewrite Forall_forall in H. apply H; auto. exact (forallb_in' _ _ _ Hfs Hp).
    + auto.
    + destruct r; simpl in *; auto.
  - apply andb_true_iff in H1 as [? ?]. rewrite H, H0; auto.
  - apply andb_true_iff in H0 as [He Hargs]. rewrite He. simpl.
    apply forallb_forall. intros y Hy. apply in_map_iff in Hy as (x & <- & Hx).
    rewrite Forall_forall in H. apply H; auto. exact (forallb_in' _ _ _ Hargs Hx).
  - destruct (nth_error a i) as [x|] eqn:E.
    + rewrite (nth_error_nth _ _ _ E). eapply forallb_in'; [exact Ha | eapply nth_error_In; eauto].
    + rewrite nth_overflow; [reflexivity|]. apply nth_error_None; auto.
Qed.

(** Unfolding a checkable recursive type gives a checkable type. *)
Lemma chk_musubst okE s : chk okE false s = true ->
  forall t k, chk okE false t = true -> chk okE false (musubst k s t) = true.
Proof.
  intros Hs.
  apply (ty_ind2 (fun t => forall k, chk okE false t = true -> chk okE false (musubst k s t) = true)
                 (fun f => forall k, fchk okE false f = true -> fchk okE false (fmusubst k s f) = true));
    simpl; intros; auto; try discriminate.
  - apply andb_true_iff in H1 as [H1 Hr]. apply andb_true_iff in H1 as [Hfs Hr0].
    apply andb_true_iff; split; [apply andb_true_iff; split|].
    + rewrite forallb_map. eapply forallb_impl; [| exact Hfs]. eapply Forall_impl; [| exact H].
      intros p Hp. simpl. auto.
    + auto.
    + destruct r; simpl in *; auto.
  - apply andb_true_iff in H1 as [? ?]. rewrite H, H0; auto.
  - apply andb_true_iff in H0 as [He Ha]. rewrite He. simpl.
    rewrite forallb_map. eapply forallb_impl; [| exact Ha]. eapply Forall_impl; [| exact H]. auto.
  - destruct (Nat.eqb n k); auto.
Qed.

Lemma chk_tunfold okE t : chk okE false (TMu t) = true -> chk okE false (tunfold t) = true.
Proof. intros H. apply chk_musubst; auto. Qed.

(** ** Helpers about the validator's combinators *)
Lemma oforall_nf {A} (g : A -> option bool) l :
  (forall x, In x l -> g x <> Some false) -> oforall g l <> Some false.
Proof.
  induction l as [|y l IH]; simpl; intros Hg; [discriminate|].
  destruct (g y) as [[|]|] eqn:E.
  - apply IH. intros; apply Hg; auto.
  - exfalso. apply (Hg y); auto.
  - discriminate.
Qed.

Lemma oforall2_nf {A B} (g : A -> B -> option bool) l1 l2 :
  Forall2 (fun x y => g x y <> Some false) l1 l2 -> oforall2 g l1 l2 <> Some false.
Proof.
  induction 1 as [|x y l1 l2 Hxy _ IH]; simpl; [discriminate|].
  destruct (g x y) as [[|]|]; [exact IH | congruence | discriminate].
Qed.

Lemma oand_nf x y : x <> Some false -> y <> Some false -> oand x y <> Some false.
Proof. destruct x as [[|]|]; simpl; auto. Qed.


Section Complete.
Variable sigs : genv.
Variable okE : ename -> bool.
Hypothesis okE_sound : forall E c pts, okE E = true -> g_ctors sigs E c = Some pts ->
  forallb (chk okE true) pts = true.

Lemma chk_payloads E c pts a :
  okE E = true -> g_ctors sigs E c = Some pts -> forallb (chk okE false) a = true ->
  forall t, In t (map (subst a) pts) -> chk okE false t = true.
Proof.
  intros He Hc Ha t Ht. apply in_map_iff in Ht as (p & <- & Hp).
  apply chk_subst; auto. exact (forallb_in' _ _ _ (okE_sound E c pts He Hc) Hp).
Qed.

(** ** Shared values *)
Lemma validate_complete Σ H R :
  length Σ = length H -> heap_ok_out sigs Σ H R ->
  forall f t v, vtyped sigs Σ v t -> (forall l, In l (vlocs v) -> ~ In l R) ->
  chk okE false t = true -> validate f H v t <> Some false.
Proof.
  intros Ln Ho. induction f as [|f IH]; intros t v V Hr Hc; [simpl; discriminate|].
  destruct t; simpl in Hc |- *; try discriminate Hc.
  - destruct (vt_int_inv _ _ _ V) as [? ->]. discriminate.
  - destruct (vt_str_inv _ _ _ V) as [? ->]. discriminate.
  - destruct (vt_bool_inv _ _ _ V) as [? ->]. discriminate.
  - exfalso. exact (vt_bot _ _ _ V).
  - discriminate.
  - destruct (vt_maybe_inv _ _ _ _ V) as [->|(x & -> & Vx)]; [discriminate|].
    apply IH; auto.
  - destruct (vt_list_inv _ _ _ _ V) as (l & a & -> & El & [Hat Hta]).
    assert (Hl : ~ In l R) by (apply Hr; simpl; auto).
    assert (Hlt : l < length H) by (rewrite <- Ln; apply nth_error_Some; congruence).
    destruct (nth_error H l) as [o|] eqn:Eo; [|apply nth_error_None in Eo; lia].
    destruct (Ho l o Eo Hl) as ((h & Eh & Ok) & Hin).
    rewrite El in Eh. injection Eh as <-. destruct o; simpl in Ok; try contradiction.
    apply oforall_nf. intros x Hx. apply IH; auto.
    + rewrite Forall_forall in Ok. eapply vtyped_sub; eauto.
    + intros r Hr'. apply Hin. simpl. apply in_flat_map. eauto.
  - destruct (vt_rec_inv _ _ _ _ _ V) as (l & fs' & r' & -> & El & Hs).
    pose proof (sub_rec_fsub _ _ _ _ Hs) as Hf.
    apply andb_true_iff in Hc as [Hc Hnr]. apply andb_true_iff in Hc as [Hcf Hcr].
    assert (Hl : ~ In l R) by (apply Hr; simpl; auto).
    assert (Hlt : l < length H) by (rewrite <- Ln; apply nth_error_Some; congruence).
    destruct (nth_error H l) as [o|] eqn:Eo; [|apply nth_error_None in Eo; lia].
    destruct (Ho l o Eo Hl) as ((h & Eh & Ok) & Hin).
    rewrite El in Eh. injection Eh as <-. destruct o; simpl in Ok; try contradiction.
    destruct Ok as (_ & Hreq & Fv).
    (* the target's status at each label, and that it is checkable *)
    assert (Cf : forall k, fchk okE false (field_at k fs r) = true).
    { intros k. unfold field_at. destruct (lookup k fs) as [st|] eqn:E; auto.
      apply lookup_in in E. exact (forallb_in' _ _ _ Hcf E). }
    apply oand_nf; [|apply oand_nf].
    + apply oforall_nf. intros [k x] Hkx. simpl.
      rewrite Forall_forall in Fv. specialize (Fv _ Hkx). simpl in Fv.
      assert (Hx : forall r0, In r0 (vlocs x) -> ~ In r0 R).
      { intros r0 Hr0. apply Hin. simpl. apply in_flat_map. exists (k, x). auto. }
      specialize (Hf k). specialize (Cf k).
      set (s0 := field_at k fs' r') in *. set (t0 := field_at k fs r) in *. clearbody s0 t0.
      destruct Hf; simpl in Fv, Cf |- *; try discriminate;
        try (exfalso; exact (vt_bot _ _ _ Fv));
        (apply IH; [eapply vtyped_sub; eauto | exact Hx | exact Cf]).
    + apply oforall_nf. intros [k st] Hkst. simpl.
      destruct (lookup k kvs) as [x|] eqn:Ek; [discriminate|].
      destruct (field_at k fs r) eqn:Ef; simpl; try discriminate.
      specialize (Hf k). rewrite Ef in Hf. inversion Hf; subst.
      exfalso. eapply Hreq; eauto.
    + destruct r; simpl in Hnr |- *; discriminate.
  - apply andb_true_iff in Hc as [Hc1 Hc2].
    destruct (vt_union_inv _ _ _ _ _ V) as [Va|Vb].
    + pose proof (IH _ _ Va Hr Hc1). destruct (validate f H v t1) as [[|]|]; congruence.
    + destruct (validate f H v t1) as [[|]|]; try discriminate. apply IH; auto.
  - apply andb_true_iff in Hc as [He Ha].
    destruct (vt_enum_inv _ _ _ _ _ V) as (c & pts & vs & -> & Ec & W & Vl).
    rewrite ename_eqb_refl. apply oforall2_nf.
    assert (Ct := chk_payloads E c pts args He Ec Ha).
    assert (Hvs : forall x, In x vs -> forall r0, In r0 (vlocs x) -> ~ In r0 R).
    { intros x Hx r0 Hr0. apply Hr. simpl. apply in_flat_map. eauto. }
    clear -IH Vl Ct Hvs. remember (map (subst args) pts) as ts eqn:Ets. clear Ets.
    induction Vl as [|x xs t ts Vx Vl IHl]; constructor.
    + apply IH; [exact Vx | apply Hvs; left; auto | apply Ct; left; auto].
    + apply IHl; [intros; apply Ct; right; auto | intros y Hy r0 Hr0; eapply Hvs; [right; exact Hy | exact Hr0]].
  - (* a recursive type: the value has its unfolding *)
    destruct (vt_mu_inv _ _ _ _ V) as [M V']. rewrite M. apply IH; auto. apply chk_tunfold; auto.
  - (* a free recursion variable has no values *)
    exfalso. inversion V; subst;
      match goal with Hs : sub _ (TRV _) |- _ => apply sub_unfold in Hs; inversion Hs end.
Qed.

(** ** Fresh values *)
Lemma validate_complete_fresh Σ H :
  forall f t v O, dtyped sigs Σ H v t O -> chk okE false t = true -> validate f H v t <> Some false.
Proof.
  induction f as [|f IH]; intros t v O D Hc; [simpl; discriminate|].
  destruct t; simpl in Hc |- *; try discriminate Hc;
    inversion D; subst; simpl; try discriminate.
  - apply IH with (O := O); auto.
  - match goal with E : nth_error H _ = Some (OList _) |- _ => rewrite E end.
    apply oforall_nf. intros x Hx.
    match goal with Hd : dtypeds _ _ _ _ _ _ |- _ => rename Hd into Ds end.
    clear -IH Ds Hx Hc. induction Ds as [|y ys t0 Oy Oys Dy Ds IHd]; [destruct Hx|].
    destruct Hx as [<-|Hx]; [eapply IH; eauto | auto].
  - match goal with E : nth_error H _ = Some (ODict _) |- _ => rewrite E end.
    apply andb_true_iff in Hc as [Hc Hnr]. apply andb_true_iff in Hc as [Hcf Hcr].
    assert (Cf : forall k, fchk okE false (field_at k fs r) = true).
    { intros k. unfold field_at. destruct (lookup k fs) as [st|] eqn:E; auto.
      apply lookup_in in E. exact (forallb_in' _ _ _ Hcf E). }
    match goal with Hd : dfields _ _ _ _ _ _ _ |- _ => rename Hd into Ds end.
    match goal with Hq : forall k t, field_at k fs r = FReq t -> lookup k _ <> None |- _ =>
      rename Hq into Hreq end.
    apply oand_nf; [|apply oand_nf].
    + apply oforall_nf. intros [k x] Hkx. simpl.
      assert (Dx : exists Ox, dtyped sigs Σ H x (fty (field_at k fs r)) Ox).
      { clear -Ds Hkx. induction Ds as [|k' v' kvs' fs' r' O' Os' Dv Ds IHd]; [destruct Hkx|].
        destruct Hkx as [E|Hkx]; [injection E as -> ->; eauto | auto]. }
      destruct Dx as (Ox & Dx). specialize (Cf k).
      destruct (field_at k fs r); simpl in Cf, Dx |- *; try discriminate;
        try (eapply IH; eauto).
      exfalso. exact (dtyped_bot _ _ _ _ _ Dx).
    + apply oforall_nf. intros [k st] Hkst. simpl.
      destruct (lookup k kvs) as [x|] eqn:Ek; [discriminate|].
      destruct (field_at k fs r) eqn:Ef; simpl; try discriminate.
      exfalso. eapply Hreq; eauto.
    + destruct r; simpl in Hnr |- *; discriminate.
  - apply andb_true_iff in Hc as [Hc1 Hc2].
    match goal with Da : dtyped _ _ _ v t1 _ |- _ => pose proof (IH _ _ _ Da Hc1) end. destruct (validate f H v t1) as [[|]|]; congruence.
  - apply andb_true_iff in Hc as [Hc1 Hc2].
    destruct (validate f H v t1) as [[|]|]; try discriminate. eapply IH; eauto.
  - apply andb_true_iff in Hc as [He Ha].
    rewrite ename_eqb_refl. apply oforall2_nf.
    match goal with Ec : g_ctors sigs E c = Some pts |- _ => assert (Ct := chk_payloads E c pts args He Ec Ha) end.
    match goal with Hd : dtypedl _ _ _ _ _ _ |- _ => rename Hd into Dl end.
    clear -IH Dl Ct. remember (map (subst args) pts) as ts eqn:Ets. clear Ets.
    induction Dl as [|x xs t ts Ox Oxs Dx Dl IHl]; constructor.
    + eapply IH; [exact Dx | apply Ct; left; auto].
    + apply IHl. intros; apply Ct; right; auto.
  - match goal with M : mu_ok _ = true |- _ => rewrite M end.
    eapply IH; [eassumption | apply chk_tunfold; auto].
Qed.

End Complete.

Open Scope string_scope.

(** ** Examples

    [enum F = f (int -- int) end] is not checkable: a real [F] value fails
    validation against [F], so an [is F x] arm must not count as covering
    [F] (and the checker should reject [F] as a [tryAs] target). *)
Definition EF1 : ename := {| en_name := "F"; en_params := []; en_imm := true |}.
Definition sigs_F : genv :=
  {| g_sigs := fun _ _ _ => False;
     g_ctors := fun E c => if ename_eqb E EF1 && String.eqb c "f"
                           then Some [TQuote [TInt] (Some [TInt])] else None |}.

Example quote_enum_fails :
  validate 10 [] (VCon EF1 "f" [TQuote [TInt] (Some [TInt])] [VClo 0 []]) (TEnum EF1 []) = Some false.
Proof. reflexivity. Qed.

Example quote_enum_not_checkable : forall okE,
  (forall E c pts, okE E = true -> g_ctors sigs_F E c = Some pts -> forallb (chk okE true) pts = true) ->
  okE EF1 = false.
Proof.
  intros okE Hs. destruct (okE EF1) eqn:E; auto.
  specialize (Hs EF1 "f" _ E eq_refl). discriminate.
Qed.
