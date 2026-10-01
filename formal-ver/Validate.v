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
From MshellCore Require Import Syntax Subtyping Variance Typing Interp Invariant RtLemmas Commit.

Lemma oforall_true {A} (g : A -> option bool) l :
  oforall g l = Some true -> forall x, In x l -> g x = Some true.
Proof.
  induction l as [|y l IH]; simpl; [tauto|]. intros E x [<-|Hx].
  - destruct (g y) as [[|]|]; congruence.
  - destruct (g y) as [[|]|]; try congruence. auto.
Qed.

Lemma oand_true x y : oand x y = Some true -> x = Some true /\ y = Some true.
Proof. destruct x as [[|]|]; simpl; intros E; try discriminate; auto. Qed.

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

Section V.
Variable sigs : genv.

Definition vfield (f : nat) (H : heap) (st : fstat) (ov : option val) : option bool :=
  match st with
  | FReq t' => match ov with Some x => validate f H x t' | None => Some false end
  | FOpt t' | FDict t' => match ov with Some x => validate f H x t' | None => Some true end
  | FAbs => match ov with Some _ => Some false | None => Some true end
  | FOpen => Some true
  end.

Lemma validate_rec_true f H l kvs fs r : nth_error H l = Some (ODict kvs) ->
  validate (S f) H (VLoc l) (TRec fs r) = Some true ->
  (forall k x, In (k, x) kvs -> vfield f H (field_at k fs r) (Some x) = Some true) /\
  (forall k t, field_at k fs r = FReq t -> lookup k kvs <> None).
Proof.
  intros E Hv. simpl in Hv. rewrite E in Hv.
  apply oand_true in Hv as [Hk Hv]. apply oand_true in Hv as [Hf Hr]. split.
  - intros k x Hin. exact (oforall_true _ _ Hk (k, x) Hin).
  - intros k t Ht Hn. assert (Hfa := Ht). unfold field_at in Ht.
    destruct (lookup k fs) as [st|] eqn:Ef.
    + subst st. apply lookup_in in Ef. pose proof (oforall_true _ _ Hf _ Ef) as Hx. simpl in Hx.
      rewrite Hn, Hfa in Hx. discriminate.
    + subst r. discriminate.
Qed.

Lemma dtyped_nonloc Σ H v t O :
  dtyped sigs Σ H v t O ->
  match v with VLoc _ | VCon _ _ _ _ => True | _ => O = [] end.
Proof. induction 1; simpl; auto. Qed.

Lemma dtyped_con Σ H v t O :
  dtyped sigs Σ H v t O -> forall E c pts vs, v = VCon E c pts vs ->
  exists a Os, g_ctors sigs E c = Some pts /\ wf_payload E pts /\
    dtypedl sigs Σ H vs (map (subst a) pts) Os /\ O = concat Os /\ NoDup O.
Proof.
  induction 1; intros E0 c0 pts0 vs0 Ev; try discriminate; eauto.
  inversion Ev; subst. eauto 10.
Qed.

Lemma vtyped_con Σ v t : vtyped sigs Σ v t -> forall E c pts vs, v = VCon E c pts vs ->
  exists a, g_ctors sigs E c = Some pts /\ wf_payload E pts /\ vtypedl sigs Σ vs (map (subst a) pts).
Proof. induction 1; intros E0 c0 pts0 vs0 Ev; try discriminate; eauto. inversion Ev; subst. eauto. Qed.

Lemma dtyped_loc_list Σ H v t O :
  dtyped sigs Σ H v t O -> forall l vs, v = VLoc l -> nth_error H l = Some (OList vs) ->
  exists a Os, dtypeds sigs Σ H vs a Os /\ O = l :: concat Os /\ NoDup (l :: concat Os).
Proof.
  induction 1; intros l0 vs0 Ev E'; try discriminate; eauto.
  - inversion Ev; subst. rewrite H0 in E'. inversion E'; subst. eauto.
  - inversion Ev; subst. rewrite H0 in E'. discriminate.
Qed.

Lemma dtyped_loc_rec Σ H v t O :
  dtyped sigs Σ H v t O -> forall l kvs, v = VLoc l -> nth_error H l = Some (ODict kvs) ->
  exists fs r Os, NoDup (map fst kvs) /\ dfields sigs Σ H kvs fs r Os /\
                  O = l :: concat Os /\ NoDup (l :: concat Os).
Proof.
  induction 1; intros l0 kvs0 Ev E'; try discriminate; eauto.
  - inversion Ev; subst. rewrite H0 in E'. discriminate.
  - inversion Ev; subst. rewrite H0 in E'. inversion E'; subst. eauto 7.
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

Lemma dtypedl_retype Σ H (g : val -> ty -> option bool) vs ts Os ts' :
  dtypedl sigs Σ H vs ts Os -> oforall2 g vs ts' = Some true ->
  (forall x t t' O, In x vs -> dtyped sigs Σ H x t O -> g x t' = Some true -> dtyped sigs Σ H x t' O) ->
  dtypedl sigs Σ H vs ts' Os.
Proof.
  intros D. revert ts'. induction D; intros [|t' ts'] Hg Hf; simpl in Hg; try discriminate; constructor.
  - destruct (g v t') as [[|]|] eqn:Eg; try discriminate. eapply Hf; eauto. left; reflexivity.
  - destruct (g v t') as [[|]|] eqn:Eg; try discriminate.
    apply IHD; auto. intros; eapply Hf; eauto. right; assumption.
Qed.

Lemma vtypedl_retype Σ (g : val -> ty -> option bool) vs ts ts' :
  vtypedl sigs Σ vs ts -> oforall2 g vs ts' = Some true ->
  (forall x t t', In x vs -> vtyped sigs Σ x t -> g x t' = Some true -> In t' ts' ->
     vtyped sigs Σ x t' /\ vlocs x = []) ->
  vtypedl sigs Σ vs ts' /\ flat_map vlocs vs = [].
Proof.
  intros D. revert ts'. induction D; intros [|t' ts'] Hg Hf; simpl in Hg; try discriminate.
  - split; [constructor | reflexivity].
  - destruct (g v t') as [[|]|] eqn:Eg; try discriminate.
    destruct (Hf v t t' (in_eq _ _) H Eg (in_eq _ _)) as [V L].
    destruct (IHD ts' Hg) as [V' L'].
    { intros; eapply Hf; eauto; right; assumption. }
    split; [constructor; auto | simpl; rewrite L, L'; reflexivity].
Qed.

(** Validating a fresh value in place retypes it: no store typing changes. *)
Lemma validate_dtyped Σ H : forall f u v t O,
  dtyped sigs Σ H v t O -> validate f H v u = Some true -> dtyped sigs Σ H v u O.
Proof.
  induction f as [|f IH]; intros u v t O D Hv; [discriminate|].
  destruct u; simpl in Hv.
  - destruct v; try discriminate. apply dtyped_nonloc in D. subst. constructor.
  - destruct v; try discriminate. apply dtyped_nonloc in D. subst. constructor.
  - destruct v; try discriminate. apply dtyped_nonloc in D. subst. constructor.
  - discriminate.
  - apply dt_top with (t := t); exact D.
  - destruct v; try discriminate.
    destruct (nth_error H l) as [[vs| |]|] eqn:E; try discriminate.
    destruct (dtyped_loc_list _ _ _ _ _ D l vs eq_refl E) as (a & Os & Ds & -> & N).
    econstructor; eauto. eapply dtypeds_retype; eauto.
    intros x O Hx Dx. eapply IH; eauto. exact (oforall_true _ _ Hv x Hx).
  - destruct v; try discriminate.
    destruct (nth_error H l) as [[|kvs|]|] eqn:E; try discriminate.
    assert (Hv' : validate (S f) H (VLoc l) (TRec fs r) = Some true) by (simpl; rewrite E; exact Hv).
    destruct (validate_rec_true f H l kvs fs r E Hv') as [Hk Hq].
    destruct (dtyped_loc_rec _ _ _ _ _ D l kvs eq_refl E) as (fs0 & r0 & Os & Nk & Df & -> & N).
    eapply dt_rec; eauto. eapply dfields_retype; eauto.
    intros k x O Hin Dx. specialize (Hk k x Hin).
    destruct (field_at k fs r); simpl in Hk |- *; try discriminate.
    + eapply IH; eauto.
    + eapply IH; eauto.
    + eapply IH; eauto.
    + apply dt_top with (t := fty (field_at k fs0 r0)); exact Dx.
  - destruct (validate f H v u1) as [[|]|] eqn:E1.
    + apply dt_unionl. eapply IH; eauto.
    + apply dt_unionr. eapply IH; eauto.
    + discriminate.
  - discriminate.
  - destruct v; try discriminate.
    destruct (ename_eqb E E0) eqn:Ee; try discriminate. apply ename_eqb_true in Ee; subst E0.
    destruct (dtyped_con _ _ _ _ _ D E c pts vs eq_refl) as (a & Os & Ec & W & Dl & -> & N).
    eapply dt_con; eauto. eapply dtypedl_retype; eauto.
  - discriminate.
  - discriminate.
  - (* a recursive type: validated as its unfolding *)
    destruct (mu_ok u) eqn:M; [|discriminate]. apply dt_mu; auto. eapply IH; eauto.
  - discriminate.
Qed.

(** Validation against a type with no lists or dicts is sound even for a
    shared value: nothing is aliased. *)
Lemma validate_imm Σ H : forall f u v t,
  vtyped sigs Σ v t -> validate f H v u = Some true -> immutable u = true ->
  vtyped sigs Σ v u /\ vlocs v = [].
Proof.
  induction f as [|f IH]; intros u v t V Hv Hi; [discriminate|].
  destruct u; simpl in Hv, Hi; try discriminate.
  - destruct v; try discriminate. split; [constructor | reflexivity].
  - destruct v; try discriminate. split; [constructor | reflexivity].
  - destruct v; try discriminate. split; [constructor | reflexivity].
  - apply andb_true_iff in Hi as [H1 H2].
    destruct (validate f H v u1) as [[|]|] eqn:E1; try discriminate.
    + destruct (IH u1 v t V E1 H1). split; [apply vt_unionl|]; auto.
    + destruct (IH u2 v t V Hv H2). split; [apply vt_unionr|]; auto.
  - destruct v; try discriminate.
    destruct (ename_eqb E E0) eqn:Ee; try discriminate. apply ename_eqb_true in Ee; subst E0.
    destruct (vtyped_con _ _ _ V E c pts vs eq_refl) as (a & Ec & W & Vl).
    destruct (vtypedl_retype Σ _ vs _ _ Vl Hv) as [Vl' L].
    { intros x t0 t' Hx Vx Hg Ht'. apply in_map_iff in Ht' as (pt & <- & Hpt).
      eapply IH; eauto. eapply payload_imm; eauto. }
    split; [eapply vt_con; eauto | exact L].
  - destruct (mu_ok u) eqn:M; [|discriminate].
    assert (Hi' : immutable (tunfold u) = true) by (rewrite immutable_tunfold; exact Hi).
    destruct (IH _ v t V Hv Hi') as [V' L]. split; [apply vt_mu|]; auto.
Qed.

(** Objects outside the regions [R] are typed by [Σ] and do not point into [R]. *)
Definition heap_ok_out (Σ : store_ty) (H : heap) (R : list loc) : Prop :=
  forall l o, nth_error H l = Some o -> ~ In l R ->
    (exists h, nth_error Σ l = Some h /\ obj_ok sigs Σ o h) /\
    (forall r, In r (olocs o) -> ~ In r R).

End V.
