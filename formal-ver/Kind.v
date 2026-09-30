(** * Kind patterns: canonical forms for [kind_then] / [kind_else]. *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Variance Typing Interp Invariant RtLemmas Commit Validate.

Section K.
Variable sigs : genv.

Lemma tunion_spec x y :
  (x = TBot /\ tunion x y = y) \/
  (x <> TBot /\ y = TBot /\ tunion x y = x) \/
  (x <> TBot /\ y <> TBot /\ tunion x y = TUnion x y).
Proof.
  destruct x, y; simpl;
    first [ left; split; reflexivity
          | right; left; repeat split; congruence
          | right; right; repeat split; congruence ].
Qed.

Lemma head_not_bot e ke : kind_of_ty e = Some ke -> ~ sub e TBot.
Proof. intros Hk Hs. apply sub_unfold in Hs. inversion Hs; subst; simpl in Hk; discriminate. Qed.

Lemma kind_head_top t k u : kind_of_ty t = Some k -> kind_top k = Some u -> sub t u.
Proof.
  destruct t; simpl; intros E1 E2; inversion E1; subst; simpl in E2; inversion E2; subst;
    try apply s_refl; try apply s_top.
  - apply s_maybe, s_top.
  - apply s_rec. intros k. unfold field_at at 2. simpl. apply fs_open.
Qed.

Lemma tunion_l e ke x y : kind_of_ty e = Some ke -> sub e x -> sub e (tunion x y).
Proof.
  intros Hk Hs. destruct (tunion_spec x y) as [[-> E]|[[Nx [-> E]]|[Nx [Ny E]]]]; rewrite E.
  - exfalso; eapply head_not_bot; eauto.
  - exact Hs.
  - apply s_unionr1; exact Hs.
Qed.

Lemma tunion_r e ke x y : kind_of_ty e = Some ke -> sub e y -> sub e (tunion x y).
Proof.
  intros Hk Hs. destruct (tunion_spec x y) as [[-> E]|[[Nx [-> E]]|[Nx [Ny E]]]]; rewrite E.
  - exact Hs.
  - exfalso; eapply head_not_bot; eauto.
  - apply s_unionr2; exact Hs.
Qed.

Lemma kt_sub : forall e t, sub e t -> forall ke t1,
  kind_of_ty e = Some ke -> kind_then ke t = Some t1 -> sub e t1.
Proof.
  intros e t Hs; apply sub_unfold in Hs; induction Hs; intros ke t1 Hk Ht.
  - destruct t; simpl in Hk; try discriminate; inversion Hk; subst;
      simpl in Ht; try rewrite ename_eqb_refl in Ht; inversion Ht; subst; apply s_refl.
  - discriminate.
  - destruct t; simpl in Hk; try discriminate; inversion Hk; subst;
      simpl in Ht; inversion Ht; subst.
    + apply s_refl. + apply s_refl. + apply s_refl.
    + apply s_maybe; apply s_top.
    + apply s_rec. intros k. unfold field_at at 2. simpl. apply fs_open.
    + apply s_top.
  - discriminate.
  - simpl in Ht. destruct (kind_then ke b) eqn:Eb; [|discriminate].
    destruct (kind_then ke c) eqn:Ec; [|discriminate].
    inversion Ht; subst. eapply tunion_l; eauto.
  - simpl in Ht. destruct (kind_then ke b) eqn:Eb; [|discriminate].
    destruct (kind_then ke c) eqn:Ec; [|discriminate].
    inversion Ht; subst. eapply tunion_r; eauto.
  - discriminate.
  - (* a recursive type: the pattern gives unknown contents of the kind *)
    simpl in Ht. eapply kind_head_top; eauto.
  - simpl in Hk; inversion Hk; subst; simpl in Ht; inversion Ht; subst. apply s_maybe; auto.
  - simpl in Hk; inversion Hk; subst; simpl in Ht; inversion Ht; subst. apply s_list; auto.
  - simpl in Hk; inversion Hk; subst; simpl in Ht; inversion Ht; subst. apply s_rec; auto.
  - simpl in Hk; inversion Hk; subst; simpl in Ht; inversion Ht; subst. apply s_quote; auto.
  - simpl in Hk; inversion Hk; subst; simpl in Ht; rewrite ename_eqb_refl in Ht.
    inversion Ht; subst. apply s_enum; auto.
Qed.

Lemma ke_sub : forall e t, sub e t -> forall ke k,
  kind_of_ty e = Some ke -> kind_eqb k ke = false -> sub e (kind_else k t).
Proof.
  intros e t Hs; apply sub_unfold in Hs; induction Hs; intros ke k Hk Hq.
  - destruct t; simpl in Hk; try discriminate; inversion Hk; subst; simpl; rewrite Hq; apply s_refl.
  - discriminate.
  - simpl. apply s_top.
  - discriminate.
  - simpl. eapply tunion_l; eauto.
  - simpl. eapply tunion_r; eauto.
  - discriminate.
  - simpl. apply sub_fold, sf_mur; auto.
  - simpl in Hk; inversion Hk; subst; simpl; rewrite Hq. apply s_maybe; auto.
  - simpl in Hk; inversion Hk; subst; simpl; rewrite Hq. apply s_list; auto.
  - simpl in Hk; inversion Hk; subst; simpl; rewrite Hq. apply s_rec; auto.
  - simpl in Hk; inversion Hk; subst; simpl; rewrite Hq. apply s_quote; auto.
  - simpl in Hk; inversion Hk; subst; simpl; rewrite Hq. apply s_enum; auto.
Qed.

Lemma kind_list_head e : kind_of_ty e = Some KList -> exists a, e = TList a.
Proof. destruct e; simpl; intros Ek; try discriminate; eauto. Qed.

Lemma kind_enum_head e E : kind_of_ty e = Some (KEnum E) -> exists a, e = TEnum E a.
Proof. destruct e; simpl; intros Ek; try discriminate; inversion Ek; subst; eauto. Qed.

(** ** One argument per parameter

    A constructor's payload types mention only the enum's own parameters,
    so the arguments past the last parameter never matter, and missing ones
    read as [TBot].  Every enum value therefore has a type with exactly one
    argument per parameter, which is all the enum kind pattern needs. *)

Lemma Forall2_in_l {A B} (P : A -> B -> Prop) l1 l2 y :
  Forall2 P l1 l2 -> In y l2 -> exists x, P x y.
Proof. induction 1; simpl; [tauto|]. intros [<-|Hy]; eauto. Qed.

Lemma occ_sub_pocc ps : forall t p i, occ_sub ps p t = true -> In i (pocc t) -> i < length ps.
Proof.
  apply (ty_ind2 (fun t => forall p i, occ_sub ps p t = true -> In i (pocc t) -> i < length ps)
                 (fun f => forall i, focc_sub ps f = true -> In i (fpocc f) -> i < length ps));
    simpl; intros; try tauto; try discriminate; eauto.
  - apply andb_true_iff in H1 as [E1 E2]. apply in_app_or in H2 as [Hi|Hi]; eauto.
    apply in_flat_map in Hi as (kf & Hin & Hi).
    rewrite Forall_forall in H. apply (H kf Hin); auto. rewrite forallb_forall in E1. auto.
  - apply andb_true_iff in H1 as [E1 E2]. apply in_app_or in H2 as [Hi|Hi]; eauto.
  - apply andb_true_iff in H1 as [E1 E2]. apply in_app_or in H2 as [Hi|Hi].
    + apply in_flat_map in Hi as (x & Hin & Hi). rewrite Forall_forall in H.
      rewrite forallb_forall in E1. eauto.
    + destruct outs as [o|]; [|destruct Hi]. specialize (H0 o eq_refl). rewrite Forall_forall in H0.
      apply in_flat_map in Hi as (x & Hin & Hi). rewrite forallb_forall in E2. eauto.
  - apply in_flat_map in H1 as (x & Hin & Hi). rewrite Forall_forall in H.
    apply forallb2_Forall2 in H0. destruct (Forall2_in_l _ _ _ _ H0 Hin) as (q & Hq). eauto.
  - destruct (nth_error ps i) eqn:E; [|discriminate]. destruct H0 as [<-|[]].
    apply nth_error_Some. rewrite E. discriminate.
Qed.

Lemma subst_pocc : forall t a b, (forall i, In i (pocc t) -> nth i a TBot = nth i b TBot) ->
  subst a t = subst b t.
Proof.
  apply (ty_ind2 (fun t => forall a b, (forall i, In i (pocc t) -> nth i a TBot = nth i b TBot) ->
                             subst a t = subst b t)
                 (fun f => forall a b, (forall i, In i (fpocc f) -> nth i a TBot = nth i b TBot) ->
                             fsubst a f = fsubst b f));
    simpl; intros; auto; try (f_equal; auto; fail).
  - f_equal.
    + apply map_ext_in. intros kf Hin. rewrite Forall_forall in H. f_equal. apply H; auto.
      intros i Hi. apply H1, in_or_app. left. apply in_flat_map. eauto.
    + apply H0. intros i Hi. apply H1, in_or_app. auto.
  - f_equal; [apply H | apply H0]; intros i Hi; apply H1, in_or_app; auto.
  - f_equal.
    + apply map_ext_in. intros x Hin. rewrite Forall_forall in H. apply H; auto.
      intros i Hi. apply H1, in_or_app. left. apply in_flat_map. eauto.
    + destruct outs as [o|]; auto. f_equal. apply map_ext_in. intros x Hin.
      specialize (H0 o eq_refl). rewrite Forall_forall in H0. apply H0; auto.
      intros i Hi. apply H1, in_or_app. right. apply in_flat_map. eauto.
  - f_equal. apply map_ext_in. intros x Hin. rewrite Forall_forall in H. apply H; auto.
    intros i Hi. apply H0, in_flat_map. eauto.
Qed.

(** The first [n] arguments, padded with [TBot]. *)
Definition fit (n : nat) (a : list ty) : list ty := map (fun i => nth i a TBot) (seq 0 n).

Lemma fit_length n a : length (fit n a) = n.
Proof. unfold fit. rewrite length_map, length_seq. reflexivity. Qed.

Lemma fit_nth n a i : i < n -> nth i (fit n a) TBot = nth i a TBot.
Proof.
  intros Hi. unfold fit.
  assert (E : nth_error (map (fun j => nth j a TBot) (seq 0 n)) i = Some (nth i a TBot)).
  { rewrite nth_error_map, nth_error_seq. destruct (i <? n) eqn:L; [reflexivity|].
    apply Nat.ltb_ge in L. lia. }
  apply nth_error_nth with (d := TBot) in E. exact E.
Qed.

Lemma payload_fit E pts a : wf_payload E pts ->
  map (subst (fit (length (en_params E)) a)) pts = map (subst a) pts.
Proof.
  intros W. apply map_ext_in. intros t Hin. apply subst_pocc. intros i Hi.
  apply fit_nth. unfold wf_payload in W. rewrite forallb_forall in W. specialize (W t Hin).
  unfold wf_pt in W. apply andb_true_iff in W as [W _]. apply andb_true_iff in W as [W _].
  eapply occ_sub_pocc; eauto.
Qed.

Lemma kind_of_enum H v E : kind_of H v = Some (KEnum E) -> exists c pts vs, v = VCon E c pts vs.
Proof.
  destruct v; simpl; intros Hk; try discriminate.
  - destruct (nth_error H l) as [[]|]; discriminate.
  - inversion Hk; subst. eauto.
Qed.

Lemma loc_obj Σ H R l h :
  heap_ok_out sigs Σ H R -> length Σ = length H -> ~ In l R -> nth_error Σ l = Some h ->
  exists o, nth_error H l = Some o /\ obj_ok sigs Σ o h.
Proof.
  intros Hh Hlen Hn E. pose proof (nth_error_lt _ _ _ E) as Hlt. rewrite Hlen in Hlt.
  destruct (nth_error H l) as [o|] eqn:Eo.
  - destruct (Hh l o Eo Hn) as [[h' [E' Ok]] _]. rewrite E in E'; inversion E'; subst. eauto.
  - apply nth_error_None in Eo; lia.
Qed.

Lemma vhead Σ H R v t :
  vtyped sigs Σ v t -> heap_ok_out sigs Σ H R -> length Σ = length H ->
  (forall l, In l (vlocs v) -> ~ In l R) ->
  exists e ke, vtyped sigs Σ v e /\ sub e t /\ kind_of_ty e = Some ke /\ kind_of H v = Some ke.
Proof.
  intros Hv Hh Hlen. induction Hv; intros Hl.
  - exists TInt, KInt; repeat split; [constructor | apply s_refl].
  - exists TStr, KStr; repeat split; [constructor | apply s_refl].
  - exists TBool, KBool; repeat split; [constructor | apply s_refl].
  - exists (TMaybe t), KMaybe; repeat split; [constructor | apply s_refl].
  - exists (TMaybe t), KMaybe; repeat split; [constructor; auto | apply s_refl].
  - destruct (loc_obj Σ H R l (HList a) Hh Hlen) as (o & Eo & Ok); auto.
    { apply Hl; apply in_eq. }
    destruct o; simpl in Ok; try contradiction.
    exists (TList a), KList; repeat split; auto.
    + eapply vt_list; eauto. apply s_refl.
    + simpl. rewrite Eo. reflexivity.
  - destruct (loc_obj Σ H R l (HRec fs r) Hh Hlen) as (o & Eo & Ok); auto.
    { apply Hl; apply in_eq. }
    destruct o; simpl in Ok; try contradiction.
    exists (TRec fs r), KDict; repeat split; auto.
    + eapply vt_rec; eauto. apply s_refl.
    + simpl. rewrite Eo. reflexivity.
  - exists (TQuote ins outs), KQuote; repeat split; [econstructor; eauto | apply s_refl].
  - destruct (IHHv Hl) as (e & ke & A & B & C & D). exists e, ke; repeat split; auto.
    apply s_unionr1; auto.
  - destruct (IHHv Hl) as (e & ke & A & B & C & D). exists e, ke; repeat split; auto.
    apply s_unionr2; auto.
  - destruct (IHHv Hl) as (e & ke & A & B & C & D). exists e, ke; repeat split; auto.
    apply s_top.
  - exists (TEnum E a), (KEnum E); repeat split; [eapply vt_con; eauto | apply s_refl].
  - destruct (IHHv Hl) as (e & ke & A & B & C & D). exists e, ke; repeat split; auto.
    apply s_mur; auto.
Qed.

Lemma dhead Σ H v t O :
  dtyped sigs Σ H v t O ->
  exists e ke, dtyped sigs Σ H v e O /\ sub e t /\ kind_of_ty e = Some ke /\ kind_of H v = Some ke.
Proof.
  intros D. induction D.
  - exists TInt, KInt; repeat split; [constructor | apply s_refl].
  - exists TStr, KStr; repeat split; [constructor | apply s_refl].
  - exists TBool, KBool; repeat split; [constructor | apply s_refl].
  - exists (TMaybe t), KMaybe; repeat split; [constructor | apply s_refl].
  - exists (TMaybe t), KMaybe; repeat split; [constructor; auto | apply s_refl].
  - exists (TQuote ins outs), KQuote; repeat split; [constructor; auto | apply s_refl].
  - exists (TList t), KList; repeat split.
    + econstructor; eauto.
    + apply s_refl.
    + simpl. match goal with E : nth_error _ l = Some (OList _) |- _ => rewrite E end. reflexivity.
  - exists (TRec fs r), KDict; repeat split.
    + econstructor; eauto.
    + apply s_refl.
    + simpl. match goal with E : nth_error _ l = Some (ODict _) |- _ => rewrite E end. reflexivity.
  - destruct IHD as (e & ke & A & B & C & E). exists e, ke; repeat split; auto.
    apply s_unionr1; auto.
  - destruct IHD as (e & ke & A & B & C & E). exists e, ke; repeat split; auto.
    apply s_unionr2; auto.
  - destruct IHD as (e & ke & A & B & C & E). exists e, ke; repeat split; auto.
    apply s_top.
  - exists (TEnum E a), (KEnum E); repeat split; [eapply dt_con; eauto | apply s_refl].
  - destruct IHD as (e & ke & A & B & C & E). exists e, ke; repeat split; auto.
    apply s_mur; auto.
Qed.

(** Shared values *)
Lemma vtyped_kind_of Σ H R v t :
  vtyped sigs Σ v t -> heap_ok_out sigs Σ H R -> length Σ = length H ->
  (forall l, In l (vlocs v) -> ~ In l R) ->
  exists k, kind_of H v = Some k.
Proof.
  intros Hv Hh Hlen Hl. destruct (vhead Σ H R v t Hv Hh Hlen Hl) as (e & ke & _ & _ & _ & E).
  eauto.
Qed.

Lemma vtyped_kind_then Σ H R v t k t1 :
  vtyped sigs Σ v t -> heap_ok_out sigs Σ H R -> length Σ = length H ->
  (forall l, In l (vlocs v) -> ~ In l R) ->
  kind_of H v = Some k -> kind_then k t = Some t1 -> vtyped sigs Σ v t1.
Proof.
  intros Hv Hh Hlen Hl Hk Ht.
  destruct (vhead Σ H R v t Hv Hh Hlen Hl) as (e & ke & A & B & C & E).
  rewrite E in Hk; inversion Hk; subst.
  eapply vtyped_sub; [exact A|]. eapply kt_sub; eauto.
Qed.

Lemma vtyped_kind_else Σ H R v t k k' :
  vtyped sigs Σ v t -> heap_ok_out sigs Σ H R -> length Σ = length H ->
  (forall l, In l (vlocs v) -> ~ In l R) ->
  kind_of H v = Some k' -> kind_eqb k k' = false -> vtyped sigs Σ v (kind_else k t).
Proof.
  intros Hv Hh Hlen Hl Hk Hq.
  destruct (vhead Σ H R v t Hv Hh Hlen Hl) as (e & ke & A & B & C & E).
  rewrite E in Hk; inversion Hk; subst.
  eapply vtyped_sub; [exact A|]. eapply ke_sub; eauto.
Qed.

Lemma vtyped_kind_list Σ H R v t :
  vtyped sigs Σ v t -> heap_ok_out sigs Σ H R -> length Σ = length H ->
  (forall l, In l (vlocs v) -> ~ In l R) ->
  kind_of H v = Some KList -> exists a, vtyped sigs Σ v (TList a).
Proof.
  intros Hv Hh Hlen Hl Hk.
  destruct (vhead Σ H R v t Hv Hh Hlen Hl) as (e & ke & A & B & C & E).
  rewrite E in Hk; inversion Hk; subst.
  destruct (kind_list_head e C) as [a ->]. eauto.
Qed.

Lemma vtyped_kind_enum Σ H R v t E :
  vtyped sigs Σ v t -> heap_ok_out sigs Σ H R -> length Σ = length H ->
  (forall l, In l (vlocs v) -> ~ In l R) ->
  kind_of H v = Some (KEnum E) ->
  exists a, length a = length (en_params E) /\ vtyped sigs Σ v (TEnum E a).
Proof.
  intros Hv Hh Hlen Hl Hk. pose proof Hk as Hk0.
  destruct (vhead Σ H R v t Hv Hh Hlen Hl) as (e & ke & A & B & C & D).
  rewrite D in Hk; inversion Hk; subst.
  destruct (kind_enum_head e E C) as [a ->].
  destruct (kind_of_enum H v E Hk0) as (c & pts & vs & ->).
  inversion A; subst.
  exists (fit (length (en_params E)) a). split; [apply fit_length|].
  eapply vt_con; eauto. rewrite payload_fit; auto.
Qed.

(** Fresh values *)
Lemma dtyped_kind_of Σ H v t O :
  dtyped sigs Σ H v t O -> exists k, kind_of H v = Some k.
Proof.
  intros D. destruct (dhead Σ H v t O D) as (e & ke & _ & _ & _ & E). eauto.
Qed.

Lemma dtyped_kind_then Σ H v t O k t1 :
  dtyped sigs Σ H v t O -> kind_of H v = Some k -> kind_then k t = Some t1 ->
  dtyped sigs Σ H v t1 O.
Proof.
  intros D Hk Ht. destruct (dhead Σ H v t O D) as (e & ke & A & B & C & E).
  rewrite E in Hk; inversion Hk; subst.
  eapply dtyped_sub; [exact A|]. eapply kt_sub; eauto.
Qed.

Lemma dtyped_kind_else Σ H v t O k k' :
  dtyped sigs Σ H v t O -> kind_of H v = Some k' -> kind_eqb k k' = false ->
  dtyped sigs Σ H v (kind_else k t) O.
Proof.
  intros D Hk Hq. destruct (dhead Σ H v t O D) as (e & ke & A & B & C & E).
  rewrite E in Hk; inversion Hk; subst.
  eapply dtyped_sub; [exact A|]. eapply ke_sub; eauto.
Qed.

Lemma dtyped_kind_list Σ H v t O :
  dtyped sigs Σ H v t O -> kind_of H v = Some KList -> exists a, dtyped sigs Σ H v (TList a) O.
Proof.
  intros D Hk. destruct (dhead Σ H v t O D) as (e & ke & A & B & C & E).
  rewrite E in Hk; inversion Hk; subst.
  destruct (kind_list_head e C) as [a ->]. eauto.
Qed.

Lemma dtyped_kind_enum Σ H v t O E :
  dtyped sigs Σ H v t O -> kind_of H v = Some (KEnum E) ->
  exists a, length a = length (en_params E) /\ dtyped sigs Σ H v (TEnum E a) O.
Proof.
  intros D Hk. pose proof Hk as Hk0. destruct (dhead Σ H v t O D) as (e & ke & A & B & C & F).
  rewrite F in Hk; inversion Hk; subst.
  destruct (kind_enum_head e E C) as [a ->].
  destruct (kind_of_enum H v E Hk0) as (c & pts & vs & ->).
  inversion A; subst.
  exists (fit (length (en_params E)) a). split; [apply fit_length|].
  eapply dt_con; eauto. rewrite payload_fit; auto.
Qed.

End K.
