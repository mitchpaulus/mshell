(** * Cycles in validation.

    With recursive types a checked program can build a cyclic value: with
    [type L = [L]], [[] as L xs!  @xs @xs append] stores a list that contains
    itself, and it type-checks.  The model's [validate] runs out of its
    budget on such a value, a checked error.

    [cvalidate] instead keeps the (object, target type) pairs on the current
    path and answers [true] when one comes round again.  This is the
    assumption rule of recursive subtyping applied to values: it decides
    whether the (possibly cyclic) value has the type as an infinite tree.

    The soundness proof asks of a validator only that [true] be right for a
    fresh value and for a target with no lists or dicts ([vd_fresh],
    [vd_imm] in Soundness.v).  A fresh value is a tree, so no pair repeats
    on a path ([cval_dtyped]); an immutable target never reaches an object
    ([cval_imm]).  So [cvalidate] is sound ([soundness_cycles]), and it
    still never rejects a value of a checkable type ([cval_complete],
    [cval_complete_fresh]). *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Variance Typing Interp Invariant RtLemmas Validate
  Soundness Checkable Join.

Fixpoint pmem (l : loc) (t : ty) (P : list (loc * ty)) : bool :=
  match P with
  | [] => false
  | (l', t') :: P' => (Nat.eqb l l' && ty_eqb t t') || pmem l t P'
  end.

Lemma pmem_in l t P : pmem l t P = true -> In (l, t) P.
Proof.
  induction P as [|[l' t'] P IH]; simpl; [discriminate|]. intros E.
  apply orb_true_iff in E as [E|E]; [left | right; auto].
  apply andb_true_iff in E as [E1 E2]. apply Nat.eqb_eq in E1. apply ty_eqb_true in E2. subst; auto.
Qed.

Fixpoint cval (f : nat) (H : heap) (P : list (loc * ty)) (v : val) (t : ty) {struct f} : option bool :=
  match f with
  | 0 => None
  | S f' =>
  let vf := fun (P' : list (loc * ty)) (st : fstat) (ov : option val) =>
    match st with
    | FReq t' => match ov with Some x => cval f' H P' x t' | None => Some false end
    | FOpt t' | FDict t' => match ov with Some x => cval f' H P' x t' | None => Some true end
    | FAbs => match ov with Some _ => Some false | None => Some true end
    | FOpen => Some true
    end in
  match t with
  | TInt => Some (match v with VInt _ => true | _ => false end)
  | TStr => Some (match v with VStr _ => true | _ => false end)
  | TBool => Some (match v with VBool _ => true | _ => false end)
  | TBot | TParam _ | TVar _ | TRV _ => Some false
  | TTop => Some true
  | TMu t' => if mu_ok t' then cval f' H P v (tunfold t') else Some false
  | TQuote _ _ => Some false
  | TList t' =>
      match v with
      | VLoc l =>
          if pmem l t P then Some true else
          match nth_error H l with
          | Some (OList vs) => oforall (fun x => cval f' H ((l, t) :: P) x t') vs
          | _ => Some false
          end
      | _ => Some false
      end
  | TRec fs r =>
      match v with
      | VLoc l =>
          if pmem l t P then Some true else
          match nth_error H l with
          | Some (ODict kvs) =>
              oand (oforall (fun p => vf ((l, t) :: P) (field_at (fst p) fs r) (Some (snd p))) kvs)
                (oand (oforall (fun p => match lookup (fst p) kvs with
                                         | Some _ => Some true
                                         | None => vf ((l, t) :: P) (field_at (fst p) fs r) None end) fs)
                      (Some (match r with FReq _ => false | _ => true end)))
          | _ => Some false
          end
      | _ => Some false
      end
  | TUnion a b =>
      match cval f' H P v a with Some false => cval f' H P v b | r => r end
  | TEnum E a =>
      match v with
      | VCon E' _ pts vs =>
          if ename_eqb E E' then oforall2 (fun x t' => cval f' H P x t') vs (map (subst a) pts)
          else Some false
      | _ => Some false
      end
  end
  end.

(** Validation with cycles: start with nothing on the path. *)
Definition cvalidate (f : nat) (H : heap) (v : val) (t : ty) : option bool := cval f H [] v t.

Section C.
Variable sigs : genv.

Definition cfield (f : nat) (H : heap) (P : list (loc * ty)) (st : fstat) (ov : option val) : option bool :=
  match st with
  | FReq t' => match ov with Some x => cval f H P x t' | None => Some false end
  | FOpt t' | FDict t' => match ov with Some x => cval f H P x t' | None => Some true end
  | FAbs => match ov with Some _ => Some false | None => Some true end
  | FOpen => Some true
  end.

Lemma cval_rec_true f H P l kvs fs r : nth_error H l = Some (ODict kvs) -> pmem l (TRec fs r) P = false ->
  cval (S f) H P (VLoc l) (TRec fs r) = Some true ->
  (forall k x, In (k, x) kvs -> cfield f H ((l, TRec fs r) :: P) (field_at k fs r) (Some x) = Some true) /\
  (forall k t, field_at k fs r = FReq t -> lookup k kvs <> None).
Proof.
  intros E Pm Hv. simpl in Hv. rewrite Pm, E in Hv.
  apply oand_true in Hv as [Hk Hv]. apply oand_true in Hv as [Hf Hr]. split.
  - intros k x Hin. exact (oforall_true _ _ Hk (k, x) Hin).
  - intros k t Ht Hn. assert (Hfa := Ht). unfold field_at in Ht.
    destruct (lookup k fs) as [st|] eqn:Ef.
    + subst st. apply lookup_in in Ef. pose proof (oforall_true _ _ Hf _ Ef) as Hx. simpl in Hx.
      rewrite Hn, Hfa in Hx. discriminate.
    + subst r. discriminate.
Qed.

(** ** Retyping children, keeping track of their regions *)
Lemma dtypeds_retype_in Σ H vs a Os b :
  dtypeds sigs Σ H vs a Os ->
  (forall x O, In x vs -> incl O (concat Os) -> dtyped sigs Σ H x a O -> dtyped sigs Σ H x b O) ->
  dtypeds sigs Σ H vs b Os.
Proof.
  induction 1 as [|v vs t O Os D Ds IH]; intros Hf; constructor.
  - apply Hf; [apply in_eq | intros m Hm; simpl; apply in_or_app; auto | exact D].
  - apply IH. intros x O' Hx Hi Dx. apply Hf; auto; [apply in_cons; auto |].
    intros m Hm; simpl; apply in_or_app; right; auto.
Qed.

Lemma dfields_retype_in Σ H kvs fs0 r0 Os fs r :
  dfields sigs Σ H kvs fs0 r0 Os ->
  (forall k x O, In (k, x) kvs -> incl O (concat Os) -> dtyped sigs Σ H x (fty (field_at k fs0 r0)) O ->
                 dtyped sigs Σ H x (fty (field_at k fs r)) O) ->
  dfields sigs Σ H kvs fs r Os.
Proof.
  induction 1 as [|k v kvs fs' r' O Os D Ds IH]; intros Hf; constructor.
  - apply Hf; [apply in_eq | intros m Hm; simpl; apply in_or_app; auto | exact D].
  - apply IH. intros k' x O' Hx Hi Dx. apply Hf; auto; [apply in_cons; auto |].
    intros m Hm; simpl; apply in_or_app; right; auto.
Qed.

Lemma dtypedl_retype_in Σ H (g : val -> ty -> option bool) vs ts Os ts' :
  dtypedl sigs Σ H vs ts Os -> oforall2 g vs ts' = Some true ->
  (forall x t t' O, In x vs -> incl O (concat Os) -> dtyped sigs Σ H x t O -> g x t' = Some true ->
     dtyped sigs Σ H x t' O) ->
  dtypedl sigs Σ H vs ts' Os.
Proof.
  intros D. revert ts'. induction D as [|v vs t ts O Os Dv D IH]; intros [|t' ts'] Hg Hf;
    simpl in Hg; try discriminate; constructor.
  - destruct (g v t') as [[|]|] eqn:Eg; try discriminate.
    eapply Hf; eauto; [left; reflexivity | intros m Hm; simpl; apply in_or_app; auto].
  - destruct (g v t') as [[|]|] eqn:Eg; try discriminate.
    apply IH; auto. intros x t0 t1 O' Hx Hi Dx Hgx. eapply Hf; eauto; [right; exact Hx |].
    intros m Hm; simpl; apply in_or_app; right; auto.
Qed.

Definition off (P : list (loc * ty)) (O : list loc) : Prop := forall l t, In (l, t) P -> ~ In l O.

Lemma off_incl P O O' : off P O -> incl O' O -> off P O'.
Proof. intros Hoff Hi l t Hin Hl. exact (Hoff l t Hin (Hi l Hl)). Qed.

Lemma dtyped_head_in Σ H l t O : dtyped sigs Σ H (VLoc l) t O -> In l O.
Proof. intros D. destruct (proj1 (dtyped_struct sigs Σ H) _ _ _ D) as (_ & Hv & _). apply Hv. simpl; auto. Qed.

(** A child of the object at [l] has its region inside the parent's region,
    which does not contain [l]. *)
Lemma off_child P l t O Oc :
  off P (l :: O) -> NoDup (l :: O) -> incl Oc O -> off ((l, t) :: P) Oc.
Proof.
  intros Hoff Nd Hi l' t' [E|Hin] Hl'.
  - injection E as <- <-. inversion Nd; subst. apply H1, Hi, Hl'.
  - apply (Hoff l' t' Hin). right. apply Hi, Hl'.
Qed.

(** ** The contract: [true] is right for a fresh value ... *)
Lemma cval_dtyped Σ H : forall f P u v t O,
  dtyped sigs Σ H v t O -> off P O -> cval f H P v u = Some true -> dtyped sigs Σ H v u O.
Proof.
  induction f as [|f IH]; intros P u v t O D Hoff Hv; [discriminate|].
  destruct u; simpl in Hv.
  - destruct v; try discriminate. apply (dtyped_nonloc sigs) in D. subst. constructor.
  - destruct v; try discriminate. apply (dtyped_nonloc sigs) in D. subst. constructor.
  - destruct v; try discriminate. apply (dtyped_nonloc sigs) in D. subst. constructor.
  - discriminate.
  - apply dt_top with (t := t); exact D.
  - destruct v; try discriminate.
    destruct (pmem l (TList u) P) eqn:Pm.
    { exfalso. apply pmem_in in Pm. exact (Hoff _ _ Pm (dtyped_head_in _ _ _ _ _ D)). }
    destruct (nth_error H l) as [[vs| |]|] eqn:E; try discriminate.
    destruct (dtyped_loc_list sigs _ _ _ _ _ D l vs eq_refl E) as (a & Os & Ds & -> & N).
    econstructor; eauto. eapply dtypeds_retype_in; eauto.
    intros x O Hx Hi Dx. eapply (IH ((l, TList u) :: P)); eauto.
    + eapply off_child; eauto.
    + exact (oforall_true _ _ Hv x Hx).
  - destruct v; try discriminate.
    destruct (pmem l (TRec fs r) P) eqn:Pm.
    { exfalso. apply pmem_in in Pm. exact (Hoff _ _ Pm (dtyped_head_in _ _ _ _ _ D)). }
    destruct (nth_error H l) as [[|kvs|]|] eqn:E; try discriminate.
    assert (Hv' : cval (S f) H P (VLoc l) (TRec fs r) = Some true) by (simpl; rewrite Pm, E; exact Hv).
    destruct (cval_rec_true f H P l kvs fs r E Pm Hv') as [Hk Hq].
    destruct (dtyped_loc_rec sigs _ _ _ _ _ D l kvs eq_refl E) as (fs0 & r0 & Os & Nk & Df & -> & N).
    eapply dt_rec; eauto. eapply dfields_retype_in; eauto.
    intros k x O Hin Hi Dx. specialize (Hk k x Hin).
    assert (Ho : off ((l, TRec fs r) :: P) O) by (eapply off_child; eauto).
    destruct (field_at k fs r); simpl in Hk |- *; try discriminate.
    + eapply IH; eauto.
    + eapply IH; eauto.
    + eapply IH; eauto.
    + apply dt_top with (t := fty (field_at k fs0 r0)); exact Dx.
  - destruct (cval f H P v u1) as [[|]|] eqn:E1.
    + apply dt_unionl. eapply IH; eauto.
    + apply dt_unionr. eapply IH; eauto.
    + discriminate.
  - discriminate.
  - destruct v; try discriminate.
    destruct (ename_eqb E E0) eqn:Ee; try discriminate. apply ename_eqb_true in Ee; subst E0.
    destruct (dtyped_con sigs _ _ _ _ _ D E c pts vs eq_refl) as (a & Os & Ec & W & Dl & -> & N).
    eapply dt_con; eauto. eapply dtypedl_retype_in; eauto.
    intros x t0 t' O Hx Hi Dx Hg. eapply IH; eauto. eapply off_incl; eauto.
  - discriminate.
  - discriminate.
  - destruct (mu_ok u) eqn:M; [|discriminate]. apply dt_mu; auto. eapply IH; eauto.
  - discriminate.
Qed.

(** ... and for a target with no lists or dicts. *)
Lemma cval_imm Σ H : forall f P u v t,
  vtyped sigs Σ v t -> cval f H P v u = Some true -> immutable u = true ->
  vtyped sigs Σ v u /\ vlocs v = [].
Proof.
  induction f as [|f IH]; intros P u v t V Hv Hi; [discriminate|].
  destruct u; simpl in Hv, Hi; try discriminate.
  - destruct v; try discriminate. split; [constructor | reflexivity].
  - destruct v; try discriminate. split; [constructor | reflexivity].
  - destruct v; try discriminate. split; [constructor | reflexivity].
  - apply andb_true_iff in Hi as [H1 H2].
    destruct (cval f H P v u1) as [[|]|] eqn:E1; try discriminate.
    + destruct (IH P u1 v t V E1 H1). split; [apply vt_unionl|]; auto.
    + destruct (IH P u2 v t V Hv H2). split; [apply vt_unionr|]; auto.
  - destruct v; try discriminate.
    destruct (ename_eqb E E0) eqn:Ee; try discriminate. apply ename_eqb_true in Ee; subst E0.
    destruct (vtyped_con sigs _ _ _ V E c pts vs eq_refl) as (a & Ec & W & Vl).
    destruct (vtypedl_retype sigs Σ _ vs _ _ Vl Hv) as [Vl' L].
    { intros x t0 t' Hx Vx Hg Ht'. apply in_map_iff in Ht' as (pt & <- & Hpt).
      eapply IH; eauto. eapply payload_imm; eauto. }
    split; [eapply vt_con; eauto | exact L].
  - destruct (mu_ok u) eqn:M; [|discriminate].
    assert (Hi' : immutable (tunfold u) = true) by (rewrite immutable_tunfold; exact Hi).
    destruct (IH P _ v t V Hv Hi') as [V' L]. split; [apply vt_mu|]; auto.
Qed.

Lemma cvalidate_fresh Σ H f u v t O :
  dtyped sigs Σ H v t O -> cvalidate f H v u = Some true -> dtyped sigs Σ H v u O.
Proof. intros D E. eapply cval_dtyped; eauto. intros l t' []. Qed.

Lemma cvalidate_imm Σ H f u v t :
  vtyped sigs Σ v t -> cvalidate f H v u = Some true -> immutable u = true ->
  vtyped sigs Σ v u /\ vlocs v = [].
Proof. intros V E I. eapply cval_imm; eauto. Qed.

(** ** Completeness: a value of a checkable type is never rejected *)
Variable okE : ename -> bool.
Hypothesis okE_sound : forall E c pts, okE E = true -> g_ctors sigs E c = Some pts ->
  forallb (chk okE true) pts = true.

Lemma cval_complete Σ H R :
  length Σ = length H -> heap_ok_out sigs Σ H R ->
  forall f P t v, vtyped sigs Σ v t -> (forall l, In l (vlocs v) -> ~ In l R) ->
  chk okE false t = true -> cval f H P v t <> Some false.
Proof.
  intros Ln Ho. induction f as [|f IH]; intros P t v V Hr Hc; [simpl; discriminate|].
  destruct t; simpl in Hc |- *; try discriminate Hc.
  - destruct (vt_int_inv _ _ _ V) as [? ->]. discriminate.
  - destruct (vt_str_inv _ _ _ V) as [? ->]. discriminate.
  - destruct (vt_bool_inv _ _ _ V) as [? ->]. discriminate.
  - exfalso. exact (vt_bot _ _ _ V).
  - discriminate.
  - destruct (vt_list_inv _ _ _ _ V) as (l & a & -> & El & [Hat Hta]).
    destruct (pmem l (TList t) P); [discriminate|].
    assert (Hl : ~ In l R) by (apply Hr; simpl; auto).
    assert (Hlt : l < length H) by (rewrite <- Ln; apply nth_error_Some; congruence).
    destruct (nth_error H l) as [o|] eqn:Eo; [|apply nth_error_None in Eo; lia].
    destruct (Ho l o Eo Hl) as ((h & Eh & Ok) & Hin); [unfold live; rewrite El; congruence|].
    rewrite El in Eh. injection Eh as <-. destruct o; simpl in Ok; try contradiction.
    apply oforall_nf. intros x Hx. apply IH; auto.
    + rewrite Forall_forall in Ok. eapply vtyped_sub; eauto.
    + intros r Hr'. apply Hin. simpl. apply in_flat_map. eauto.
  - destruct (vt_rec_inv _ _ _ _ _ V) as (l & fs' & r' & -> & El & Hs).
    destruct (pmem l (TRec fs r) P); [discriminate|].
    pose proof (sub_rec_fsub _ _ _ _ Hs) as Hf.
    apply andb_true_iff in Hc as [Hc Hnr]. apply andb_true_iff in Hc as [Hcf Hcr].
    assert (Hl : ~ In l R) by (apply Hr; simpl; auto).
    assert (Hlt : l < length H) by (rewrite <- Ln; apply nth_error_Some; congruence).
    destruct (nth_error H l) as [o|] eqn:Eo; [|apply nth_error_None in Eo; lia].
    destruct (Ho l o Eo Hl) as ((h & Eh & Ok) & Hin); [unfold live; rewrite El; congruence|].
    rewrite El in Eh. injection Eh as <-. destruct o; simpl in Ok; try contradiction.
    destruct Ok as (_ & Hreq & Fv).
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
    + pose proof (IH P _ _ Va Hr Hc1). destruct (cval f H P v t1) as [[|]|]; congruence.
    + destruct (cval f H P v t1) as [[|]|]; try discriminate. apply IH; auto.
  - apply andb_true_iff in Hc as [He Ha].
    destruct (vt_enum_inv _ _ _ _ _ V) as (c & pts & vs & -> & Ec & W & Vl).
    rewrite ename_eqb_refl. apply oforall2_nf.
    assert (Ct := chk_payloads sigs okE okE_sound E c pts args He Ec Ha).
    assert (Hvs : forall x, In x vs -> forall r0, In r0 (vlocs x) -> ~ In r0 R).
    { intros x Hx r0 Hr0. apply Hr. simpl. apply in_flat_map. eauto. }
    clear -IH Vl Ct Hvs. remember (map (subst args) pts) as ts eqn:Ets. clear Ets.
    induction Vl as [|x xs t ts Vx Vl IHl]; constructor.
    + apply IH; [exact Vx | apply Hvs; left; auto | apply Ct; left; auto].
    + apply IHl; [intros; apply Ct; right; auto | intros y Hy r0 Hr0; eapply Hvs; [right; exact Hy | exact Hr0]].
  - destruct (vt_mu_inv _ _ _ _ V) as [M V']. rewrite M. apply IH; auto. apply chk_tunfold; auto.
  - exfalso. inversion V; subst;
      match goal with Hs : sub _ (TRV _) |- _ => apply sub_unfold in Hs; inversion Hs end.
Qed.

Lemma cval_complete_fresh Σ H :
  forall f P t v O, dtyped sigs Σ H v t O -> chk okE false t = true -> cval f H P v t <> Some false.
Proof.
  induction f as [|f IH]; intros P t v O D Hc; [simpl; discriminate|].
  destruct t; simpl in Hc |- *; try discriminate Hc;
    inversion D; subst; simpl; try discriminate.
  - destruct (pmem l (TList t) P); [discriminate|].
    match goal with E : nth_error H _ = Some (OList _) |- _ => rewrite E end.
    apply oforall_nf. intros x Hx.
    match goal with Hd : dtypeds _ _ _ _ _ _ |- _ => rename Hd into Ds end.
    clear -IH Ds Hx Hc. induction Ds as [|y ys t0 Oy Oys Dy Ds IHd]; [destruct Hx|].
    destruct Hx as [<-|Hx]; [eapply IH; eauto | auto].
  - destruct (pmem l (TRec fs r) P); [discriminate|].
    match goal with E : nth_error H _ = Some (ODict _) |- _ => rewrite E end.
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
    match goal with Da : dtyped _ _ _ v t1 _ |- _ => pose proof (IH P _ _ _ Da Hc1) end.
    destruct (cval f H P v t1) as [[|]|]; congruence.
  - apply andb_true_iff in Hc as [Hc1 Hc2].
    destruct (cval f H P v t1) as [[|]|]; try discriminate. eapply IH; eauto.
  - apply andb_true_iff in Hc as [He Ha].
    rewrite ename_eqb_refl. apply oforall2_nf.
    match goal with Ec : g_ctors sigs E c = Some pts |- _ =>
      assert (Ct := chk_payloads sigs okE okE_sound E c pts args He Ec Ha) end.
    match goal with Hd : dtypedl _ _ _ _ _ _ |- _ => rename Hd into Dl end.
    clear -IH Dl Ct. remember (map (subst args) pts) as ts eqn:Ets. clear Ets.
    induction Dl as [|x xs t ts Ox Oxs Dx Dl IHl]; constructor.
    + eapply IH; [exact Dx | apply Ct; left; auto].
    + apply IHl. intros; apply Ct; right; auto.
  - match goal with M : mu_ok _ = true |- _ => rewrite M end.
    eapply IH; [eassumption | apply chk_tunfold; auto].
Qed.
End C.

(** Type soundness with the validator that accepts cycles. *)
Theorem soundness_cycles : forall sigs defs, def_ok sigs defs -> maybe_ok sigs ->
  forall G R e s, T sigs G LNone LNone R e [] s ->
  forall n, evalv cvalidate defs n [OScope []] 0 [] e <> RStuck.
Proof.
  intros sigs defs Hdefs Hmaybe. apply (soundness_v sigs defs Hdefs Hmaybe cvalidate).
  - intros Σ H f u v t O D E. eapply cvalidate_fresh; eauto.
  - intros Σ H f u v t V E I. eapply cvalidate_imm; eauto.
Qed.
