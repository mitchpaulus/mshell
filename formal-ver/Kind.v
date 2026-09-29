(** * Kind patterns: canonical forms for [kind_then] / [kind_else]. *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Typing Interp Invariant RtLemmas Commit Validate.

Section K.
Variable sigs : string -> list ty -> option (list ty) -> Prop.

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
Proof. intros Hk Hs. inversion Hs; subst; simpl in Hk; discriminate. Qed.

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
  intros e t Hs; induction Hs; intros ke t1 Hk Ht.
  - destruct t; simpl in Hk; try discriminate; inversion Hk; subst;
      simpl in Ht; inversion Ht; subst; apply s_refl.
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
  - simpl in Hk; inversion Hk; subst; simpl in Ht; inversion Ht; subst. apply s_maybe; auto.
  - simpl in Hk; inversion Hk; subst; simpl in Ht; inversion Ht; subst. apply s_list; auto.
  - simpl in Hk; inversion Hk; subst; simpl in Ht; inversion Ht; subst. apply s_rec; auto.
  - simpl in Hk; inversion Hk; subst; simpl in Ht; inversion Ht; subst. apply s_quote; auto.
Qed.

Lemma ke_sub : forall e t, sub e t -> forall ke k,
  kind_of_ty e = Some ke -> kind_eqb k ke = false -> sub e (kind_else k t).
Proof.
  intros e t Hs; induction Hs; intros ke k Hk Hq.
  - destruct t; simpl in Hk; try discriminate; inversion Hk; subst; simpl; rewrite Hq; apply s_refl.
  - discriminate.
  - simpl. apply s_top.
  - discriminate.
  - simpl. eapply tunion_l; eauto.
  - simpl. eapply tunion_r; eauto.
  - simpl in Hk; inversion Hk; subst; simpl; rewrite Hq. apply s_maybe; auto.
  - simpl in Hk; inversion Hk; subst; simpl; rewrite Hq. apply s_list; auto.
  - simpl in Hk; inversion Hk; subst; simpl; rewrite Hq. apply s_rec; auto.
  - simpl in Hk; inversion Hk; subst; simpl; rewrite Hq. apply s_quote; auto.
Qed.

Lemma kind_list_head e : kind_of_ty e = Some KList -> exists a, e = TList a.
Proof. destruct e; simpl; intros E; try discriminate; eauto. Qed.

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

End K.
