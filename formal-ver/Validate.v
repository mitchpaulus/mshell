(** * Validation ([tryAs]).

    [tryAs] validates in place and never copies.

    - [validate_dtyped]: validating a fresh value in place retypes it (no
      store typing changes, since fresh values are deep-typed).
    - [validate_imm]: validation against a type with no lists or dicts is
      sound even for a shared value.

    A shared value validated against any other type is rejected by the
    core; the program must [copy] it first (Copy.v). *)
From Stdlib Require Import String List Arith Bool Lia.

Import ListNotations.
From MshellCore Require Import Syntax Subtyping Typing Interp Invariant RtLemmas Commit.

Lemma forallb_ext {A} (f g : A -> bool) l : (forall x, f x = g x) -> forallb f l = forallb g l.
Proof. intros E; induction l; simpl; auto. rewrite E, IHl; auto. Qed.

Definition vfield (H : heap) (f : fstat) (ov : option val) : bool :=
  match f with
  | FReq t' => match ov with Some x => validate H x t' | None => false end
  | FOpt t' | FDict t' => match ov with Some x => validate H x t' | None => true end
  | FAbs => match ov with Some _ => false | None => true end
  | FOpen => true
  end.

Lemma validate_rec_eq H l kvs fs r : nth_error H l = Some (ODict kvs) ->
  validate H (VLoc l) (TRec fs r) =
    forallb (fun p => vfield H (field_at (fst p) fs r) (Some (snd p))) kvs
    && forallb (fun p => match lookup (fst p) kvs with Some _ => true
                         | None => vfield H (field_at (fst p) fs r) None end) fs
    && match r with FReq _ => false | _ => true end.
Proof.
  intros E. simpl. rewrite E. unfold label in *.
  match goal with
  | |- context [ forallb (fun p => ?F fs (fst p) (Some (snd p))) kvs ] =>
      assert (HF : forall fs0 k ov, F fs0 k ov = vfield H (field_at k fs0 r) ov);
      [ intros fs0 k ov; induction fs0 as [|[k' f] rest IH];
        [ reflexivity
        | cbn [lookup field_at] in *; unfold field_at in *; simpl;
          destruct (String.eqb k k'); [reflexivity | exact IH] ]
      | rewrite (forallb_ext _ _ kvs (fun p => HF fs (fst p) (Some (snd p))));
        f_equal; f_equal; apply forallb_ext; intros [k f]; simpl;
        destruct (lookup k kvs); [reflexivity | apply HF] ]
  end.
Qed.

Lemma lookup_none_iff {A} k (l : list (string * A)) :
  lookup k l = None <-> ~ In k (map fst l).
Proof.
  induction l as [|[k' a] l IH]; simpl; [tauto|].
  destruct (String.eqb_spec k k'); subst; split; intros Hx.
  - discriminate.
  - exfalso; apply Hx; left; reflexivity.
  - intros [E|E]; [congruence | apply IH in Hx; auto].
  - apply IH. intro; apply Hx; right; auto.
Qed.

Lemma validate_rec_true H l kvs fs r : nth_error H l = Some (ODict kvs) ->
  validate H (VLoc l) (TRec fs r) = true ->
  (forall k x, In (k, x) kvs -> vfield H (field_at k fs r) (Some x) = true) /\
  (forall k t, field_at k fs r = FReq t -> lookup k kvs <> None).
Proof.
  intros E Hv. rewrite (validate_rec_eq H l kvs fs r E) in Hv.
  apply andb_true_iff in Hv as [Hv Hr]. apply andb_true_iff in Hv as [Hk Hf].
  rewrite forallb_forall in Hk, Hf. split.
  - intros k x Hin. apply (Hk (k, x) Hin).
  - intros k t Ht Hn. assert (Hfa := Ht). unfold field_at in Ht.
    destruct (lookup k fs) as [f|] eqn:Ef.
    + subst f. apply lookup_in in Ef. specialize (Hf _ Ef). simpl in Hf.
      rewrite Hn, Hfa in Hf. simpl in Hf. discriminate.
    + subst r. simpl in Hr. discriminate.
Qed.

Section V.
Variable sigs : string -> list ty -> option (list ty) -> Prop.

Lemma dtyped_nonloc Σ H v t O :
  dtyped sigs Σ H v t O ->
  match v with VLoc _ | VJust _ => True | _ => O = [] end.
Proof. induction 1; simpl; auto. Qed.

Lemma dtyped_just Σ H v t O :
  dtyped sigs Σ H v t O -> forall x, v = VJust x -> exists t', dtyped sigs Σ H x t' O.
Proof.
  induction 1; intros x0 E; try discriminate; eauto.
  inversion E; subst. eauto.
Qed.

Lemma dtyped_loc_list Σ H v t O :
  dtyped sigs Σ H v t O -> forall l vs, v = VLoc l -> nth_error H l = Some (OList vs) ->
  exists a Os, dtypeds sigs Σ H vs a Os /\ O = l :: concat Os /\ NoDup (l :: concat Os).
Proof.
  induction 1; intros l0 vs0 E E'; try discriminate; eauto.
  - inversion E; subst. rewrite H0 in E'. inversion E'; subst. eauto.
  - inversion E; subst. rewrite H0 in E'. discriminate.
Qed.

Lemma dtyped_loc_rec Σ H v t O :
  dtyped sigs Σ H v t O -> forall l kvs, v = VLoc l -> nth_error H l = Some (ODict kvs) ->
  exists fs r Os, NoDup (map fst kvs) /\ dfields sigs Σ H kvs fs r Os /\
                  O = l :: concat Os /\ NoDup (l :: concat Os).
Proof.
  induction 1; intros l0 kvs0 E E'; try discriminate; eauto.
  - inversion E; subst. rewrite H0 in E'. discriminate.
  - inversion E; subst. rewrite H0 in E'. inversion E'; subst. eauto 7.
Qed.

Lemma dtypeds_retype Σ H vs a Os b :
  dtypeds sigs Σ H vs a Os ->
  (forall x O, In x vs -> dtyped sigs Σ H x a O -> dtyped sigs Σ H x b O) ->
  dtypeds sigs Σ H vs b Os.
Proof.
  induction 1; intros Hf; constructor.
  - apply Hf; auto. apply in_eq.
  - apply IHdtypeds. intros; apply Hf; auto. apply in_cons; auto.
Qed.

Lemma dfields_retype Σ H kvs fs0 r0 Os fs r :
  dfields sigs Σ H kvs fs0 r0 Os ->
  (forall k x O, In (k, x) kvs -> dtyped sigs Σ H x (fty (field_at k fs0 r0)) O ->
                 dtyped sigs Σ H x (fty (field_at k fs r)) O) ->
  dfields sigs Σ H kvs fs r Os.
Proof.
  induction 1; intros Hf; constructor.
  - apply Hf; auto. apply in_eq.
  - apply IHdfields. intros; apply Hf; auto. apply in_cons; auto.
Qed.

Lemma validate_dtyped_n Σ H : forall n u, size u < n ->
  forall v t O, dtyped sigs Σ H v t O -> validate H v u = true -> dtyped sigs Σ H v u O.
Proof.
  induction n as [|n IH]; intros u Hs v t O D Hv; [lia|].
  destruct u; simpl in Hs.
  - destruct v; simpl in Hv; try discriminate. apply dtyped_nonloc in D. subst. constructor.
  - destruct v; simpl in Hv; try discriminate. apply dtyped_nonloc in D. subst. constructor.
  - destruct v; simpl in Hv; try discriminate. apply dtyped_nonloc in D. subst. constructor.
  - discriminate.
  - apply dt_top with (t := t); exact D.
  - destruct v; simpl in Hv; try discriminate.
    + apply dtyped_nonloc in D. subst. constructor.
    + destruct (dtyped_just _ _ _ _ _ D v eq_refl) as (t' & D').
      constructor. apply IH with (u := u) (t := t'); auto; lia.
  - destruct v; simpl in Hv; try discriminate.
    destruct (nth_error H l) as [[vs| |]|] eqn:E; try discriminate.
    destruct (dtyped_loc_list _ _ _ _ _ D l vs eq_refl E) as (a & Os & Ds & -> & N).
    econstructor; eauto. eapply dtypeds_retype; eauto.
    intros x O Hx Dx. rewrite forallb_forall in Hv.
    apply IH with (u := u) (t := a); auto; lia.
  - destruct v; try (simpl in Hv; discriminate).
    destruct (nth_error H l) as [[|kvs|]|] eqn:E;
      try (simpl in Hv; rewrite E in Hv; discriminate).
    destruct (validate_rec_true H l kvs fs r E Hv) as [Hk Hq].
    destruct (dtyped_loc_rec _ _ _ _ _ D l kvs eq_refl E) as (fs0 & r0 & Os & Nk & Df & -> & N).
    econstructor; eauto. eapply dfields_retype; eauto.
    intros k x O Hin Dx. specialize (Hk k x Hin).
    pose proof (size_field_at k fs r) as Sz.
    destruct (field_at k fs r); simpl in Hk |- *; simpl in Sz; try discriminate.
    + apply IH with (u := t0) (t := fty (field_at k fs0 r0)); auto; lia.
    + apply IH with (u := t0) (t := fty (field_at k fs0 r0)); auto; lia.
    + apply IH with (u := t0) (t := fty (field_at k fs0 r0)); auto; lia.
    + apply dt_top with (t := fty (field_at k fs0 r0)); exact Dx.
  - apply orb_true_iff in Hv as [Hv|Hv].
    + apply dt_unionl. apply IH with (u := u1) (t := t); auto; lia.
    + apply dt_unionr. apply IH with (u := u2) (t := t); auto; lia.
  - discriminate.
Qed.

Lemma validate_dtyped Σ H v t O u :
  dtyped sigs Σ H v t O -> validate H v u = true -> dtyped sigs Σ H v u O.
Proof. intros. eapply validate_dtyped_n with (n := S (size u)); eauto. Qed.

Lemma validate_imm Σ H v u :
  validate H v u = true -> immutable u = true -> vtyped sigs Σ v u /\ vlocs v = [].
Proof.
  revert v. induction u; intros v Hv Hi; simpl in Hi; try discriminate.
  - destruct v; simpl in Hv; try discriminate. split; [constructor | reflexivity].
  - destruct v; simpl in Hv; try discriminate. split; [constructor | reflexivity].
  - destruct v; simpl in Hv; try discriminate. split; [constructor | reflexivity].
  - destruct v; simpl in Hv; try discriminate.
    + split; [constructor | reflexivity].
    + destruct (IHu v Hv Hi) as [Hv' Hl]. split; [constructor; auto | exact Hl].
  - apply andb_true_iff in Hi as [H1 H2]. simpl in Hv. apply orb_true_iff in Hv as [Hv|Hv].
    + destruct (IHu1 v Hv H1). split; [apply vt_unionl|]; auto.
    + destruct (IHu2 v Hv H2). split; [apply vt_unionr|]; auto.
Qed.

(** Objects outside the regions [R] are typed by [Σ] and do not point into [R]. *)
Definition heap_ok_out (Σ : store_ty) (H : heap) (R : list loc) : Prop :=
  forall l o, nth_error H l = Some o -> ~ In l R ->
    (exists h, nth_error Σ l = Some h /\ obj_ok sigs Σ o h) /\
    (forall r, In r (olocs o) -> ~ In r R).

End V.
