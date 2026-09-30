(** * Basic lemmas about runtime typing. *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Variance Typing Interp Invariant.

Definition sagree (Σ Σ' : store_ty) (X : list loc) : Prop :=
  forall l h, nth_error Σ l = Some h -> ~ In l X -> nth_error Σ' l = Some h.

Definition scope_ext (Σ Σ' : store_ty) : Prop :=
  forall l G, nth_error Σ l = Some (HScope G) -> nth_error Σ' l = Some (HScope G).

Lemma scope_ext_refl Σ : scope_ext Σ Σ.
Proof. unfold scope_ext; auto. Qed.

Lemma scope_ext_trans a b c : scope_ext a b -> scope_ext b c -> scope_ext a c.
Proof. unfold scope_ext; auto. Qed.

(** ** Subtyping facts *)

Lemma sub_refl t : sub t t.
Proof. apply s_refl. Qed.

Lemma subs_refl l : subs l l.
Proof. induction l; constructor; auto using s_refl. Qed.

Lemma fsub_fty f g : fsub f g -> sub (fty f) (fty g).
Proof. intros H; inversion H; subst; simpl; auto using s_refl, s_top. Qed.

Lemma slot_sub_refl p : slot_sub p p.
Proof. destruct p as [[|] t]; constructor; auto using s_refl, rs_sub. Qed.

Lemma ssub_refl s : ssub s s.
Proof. induction s; constructor; auto using slot_sub_refl. Qed.

Lemma ssub_app a b c d : ssub a b -> ssub c d -> ssub (a ++ c) (b ++ d).
Proof. intros; apply Forall2_app; auto. Qed.

Lemma ssub_shs l1 l2 : subs l1 l2 -> ssub (shs l1) (shs l2).
Proof. induction 1; constructor; auto. constructor; auto. Qed.

Lemma sub_list_inv a t : sub (TList a) (TList t) -> teq a t.
Proof. intros H; apply sub_unfold in H; inversion H; subst; [apply teq_refl | split; auto]. Qed.

Lemma fsub_refl f : fsub f f.
Proof. destruct f; constructor; apply s_refl. Qed.

Lemma sub_rec_fsub fs1 r1 fs2 r2 :
  sub (TRec fs1 r1) (TRec fs2 r2) -> forall k, fsub (field_at k fs1 r1) (field_at k fs2 r2).
Proof. intros H; apply sub_unfold in H; inversion H; subst; auto. intros k. apply fsub_refl. Qed.

Lemma sub_top_any t a : sub TTop t -> sub a t.
Proof. intros H; eapply sub_top_inv; eauto. Qed.

(** ** Closures *)
Section Clo.
Variable sigs : genv.

Lemma closure_ok_sub G e i1 o1 i2 o2 :
  closure_ok sigs G e i1 o1 -> subs i2 i1 -> osub o1 o2 -> closure_ok sigs G e i2 o2.
Proof.
  intros Hc Hi Ho. destruct Ho as [o|l1 l2 Hl]; simpl in *.
  - destruct o as [o|].
    + intros s0. eapply t_sub; [ | apply (Hc s0 (shs o ++ s0)) | apply ssub_refl ].
      apply ssub_app; [apply ssub_shs; auto | apply ssub_refl].
    + intros s0 s'. eapply t_sub; [ | apply (Hc s0 s') | apply ssub_refl ].
      apply ssub_app; [apply ssub_shs; auto | apply ssub_refl].
  - intros s0. eapply t_sub; [ | apply (Hc s0) | ].
    + apply ssub_app; [apply ssub_shs; auto | apply ssub_refl].
    + apply ssub_app; [apply ssub_shs; auto | apply ssub_refl].
Qed.

(** ** Subsumption for [vtyped] *)
Lemma vtyped_sub_all Σ :
  (forall v a, vtyped sigs Σ v a -> forall b, sub a b -> vtyped sigs Σ v b) /\
  (forall vs ts, vtypedl sigs Σ vs ts -> forall ts', Forall2 sub ts ts' -> vtypedl sigs Σ vs ts').
Proof.
  apply (vtyped_comb sigs Σ
    (fun v a _ => forall b, sub a b -> vtyped sigs Σ v b)
    (fun vs ts _ => forall ts', Forall2 sub ts ts' -> vtypedl sigs Σ vs ts')).
  - intros n b0 Hs. apply sub_unfold in Hs. remember TInt as x eqn:E; induction Hs; subst; try discriminate; eauto using vtyped.
  - intros n b0 Hs. apply sub_unfold in Hs. remember TStr as x eqn:E; induction Hs; subst; try discriminate; eauto using vtyped.
  - intros n b0 Hs. apply sub_unfold in Hs. remember TBool as x eqn:E; induction Hs; subst; try discriminate; eauto using vtyped.
  - intros t b0 Hs. apply sub_unfold in Hs. remember (TMaybe t) as x eqn:E; induction Hs; subst; try discriminate; eauto using vtyped.
  - intros v t Hv IH b0 Hs. apply sub_unfold in Hs. remember (TMaybe t) as x eqn:E; induction Hs; subst; try discriminate; eauto using vtyped.
    inversion E; subst. constructor. auto.
  - intros l a t E Hs b0 Hs'. eapply vt_list; eauto. eapply sub_trans; eauto.
  - intros l fs r t E Hs b0 Hs'. eapply vt_rec; eauto. eapply sub_trans; eauto.
  - intros sc e G ins outs Esc Hc b0 Hs. apply sub_unfold in Hs.
    remember (TQuote ins outs) as x eqn:E; induction Hs; subst; try discriminate; eauto using vtyped.
    inversion E; subst. econstructor; eauto. eapply closure_ok_sub; eauto.
  - intros v a b Hv IH b0 Hs. apply sub_unfold in Hs.
    remember (TUnion a b) as x eqn:E; induction Hs; subst; try discriminate; eauto using vtyped.
    inversion E; subst. apply IH, sub_fold; auto.
  - intros v a b Hv IH b0 Hs. apply sub_unfold in Hs.
    remember (TUnion a b) as x eqn:E; induction Hs; subst; try discriminate; eauto using vtyped.
    inversion E; subst. apply IH, sub_fold; auto.
  - intros v t Hv IH b Hs. apply IH. eapply sub_top_any; eauto.
  - intros E c pts vs a Ec W Hl IH b0 Hs. apply sub_unfold in Hs.
    remember (TEnum E a) as x eqn:Ex; induction Hs; subst; try discriminate; eauto using vtyped.
    inversion Ex; subst. eapply vt_con; eauto. apply IH.
    apply Forall2_map_in. intros t Ht. eapply payload_sub; eauto.
  - intros v t M Hv IH b Hs. apply IH. eapply sub_trans; [apply sub_unfold_r; exact M | exact Hs].
  - intros ts' F. inversion F; subst. constructor.
  - intros v vs t ts Hv IH Hl IHl ts' F. inversion F; subst. constructor; auto.
  Unshelve. all: exact TBot.
Qed.

Lemma vtyped_sub Σ v a : vtyped sigs Σ v a -> forall b, sub a b -> vtyped sigs Σ v b.
Proof. intros. eapply (proj1 (vtyped_sub_all Σ)); eauto. Qed.

(** ** Canonical forms for [vtyped] *)
Lemma vt_bot Σ v : ~ vtyped sigs Σ v TBot.
Proof. intros H; inversion H; subst; match goal with Hs : sub _ TBot |- _ => apply sub_unfold in Hs; inversion Hs end. Qed.

Lemma vt_int_inv Σ v : vtyped sigs Σ v TInt -> exists n, v = VInt n.
Proof. intros H; inversion H; subst; eauto; match goal with Hs : sub _ TInt |- _ => apply sub_unfold in Hs; inversion Hs end. Qed.

Lemma vt_str_inv Σ v : vtyped sigs Σ v TStr -> exists s, v = VStr s.
Proof. intros H; inversion H; subst; eauto; match goal with Hs : sub _ TStr |- _ => apply sub_unfold in Hs; inversion Hs end. Qed.

Lemma vt_bool_inv Σ v : vtyped sigs Σ v TBool -> exists b, v = VBool b.
Proof. intros H; inversion H; subst; eauto; match goal with Hs : sub _ TBool |- _ => apply sub_unfold in Hs; inversion Hs end. Qed.

Lemma vt_maybe_inv Σ v t :
  vtyped sigs Σ v (TMaybe t) -> v = VNone \/ exists x, v = VJust x /\ vtyped sigs Σ x t.
Proof. intros H; inversion H; subst; eauto; match goal with Hs : sub _ (TMaybe _) |- _ => apply sub_unfold in Hs; inversion Hs end. Qed.

Lemma vt_list_inv Σ v t :
  vtyped sigs Σ v (TList t) -> exists l a, v = VLoc l /\ nth_error Σ l = Some (HList a) /\ teq a t.
Proof.
  intros H; inversion H; subst.
  - exists l, a; split; [reflexivity | split; [assumption | apply sub_list_inv; assumption]].
  - match goal with Hs : sub _ (TList _) |- _ => apply sub_unfold in Hs; inversion Hs end.
Qed.

Lemma vt_rec_inv Σ v fs r :
  vtyped sigs Σ v (TRec fs r) ->
  exists l fs' r', v = VLoc l /\ nth_error Σ l = Some (HRec fs' r') /\ sub (TRec fs' r') (TRec fs r).
Proof.
  intros H; inversion H; subst.
  - match goal with Hs : sub _ (TRec _ _) |- _ => apply sub_unfold in Hs; inversion Hs end.
  - eexists _, _, _; eauto.
Qed.

Lemma vt_quote_inv Σ v ins outs :
  vtyped sigs Σ v (TQuote ins outs) ->
  exists sc e G, v = VClo sc e /\ nth_error Σ sc = Some (HScope G) /\ closure_ok sigs G e ins outs.
Proof.
  intros H; inversion H; subst; eauto 6;
    match goal with Hs : sub _ (TQuote _ _) |- _ => apply sub_unfold in Hs; inversion Hs end.
Qed.

Lemma vt_enum_inv Σ v E a :
  vtyped sigs Σ v (TEnum E a) ->
  exists c pts vs, v = VCon E c pts vs /\ g_ctors sigs E c = Some pts /\ wf_payload E pts /\
                   vtypedl sigs Σ vs (map (subst a) pts).
Proof.
  intros H; inversion H; subst; eauto 7;
    match goal with Hs : sub _ (TEnum _ _) |- _ => apply sub_unfold in Hs; inversion Hs end.
Qed.

Lemma vtypedl_length Σ vs ts : vtypedl sigs Σ vs ts -> length vs = length ts.
Proof. induction 1; simpl; auto. Qed.

Lemma vt_union_inv Σ v a b :
  vtyped sigs Σ v (TUnion a b) -> vtyped sigs Σ v a \/ vtyped sigs Σ v b.
Proof.
  intros H; inversion H; subst; auto;
    match goal with Hs : sub _ (TUnion _ _) |- _ =>
      apply sub_unfold in Hs; inversion Hs; subst; eauto using vtyped, sub_fold end.
Qed.

Lemma vt_mu_inv Σ v t :
  vtyped sigs Σ v (TMu t) -> mu_ok t = true /\ vtyped sigs Σ v (tunfold t).
Proof.
  intros H; inversion H; subst; auto;
    match goal with Hs : sub _ (TMu _) |- _ =>
      apply sub_unfold in Hs; inversion Hs; subst; split; eauto using vtyped, sub_fold end.
Qed.

(** ** Stability under changes of Σ *)
Lemma vtyped_agree_all Σ Σ' X :
  sagree Σ Σ' X -> scope_ext Σ Σ' ->
  (forall v t, vtyped sigs Σ v t -> (forall l, In l (vlocs v) -> ~ In l X) -> vtyped sigs Σ' v t) /\
  (forall vs ts, vtypedl sigs Σ vs ts -> (forall l, In l (flat_map vlocs vs) -> ~ In l X) ->
     vtypedl sigs Σ' vs ts).
Proof.
  intros Ha Hs.
  apply (vtyped_comb sigs Σ
    (fun v t _ => (forall l, In l (vlocs v) -> ~ In l X) -> vtyped sigs Σ' v t)
    (fun vs ts _ => (forall l, In l (flat_map vlocs vs) -> ~ In l X) -> vtypedl sigs Σ' vs ts));
    simpl.
  - intros; constructor.
  - intros; constructor.
  - intros; constructor.
  - intros; constructor.
  - intros v t _ IH Hl. constructor; auto.
  - intros l a t E Hsb Hl. eapply vt_list; [ apply Ha; [eassumption | apply Hl; left; reflexivity] | assumption ].
  - intros l fs r t E Hsb Hl. eapply vt_rec; [ apply Ha; [eassumption | apply Hl; left; reflexivity] | assumption ].
  - intros sc e G ins outs E Hc Hl. eapply vt_clo; eauto.
  - intros v a b _ IH Hl. apply vt_unionl; auto.
  - intros v a b _ IH Hl. apply vt_unionr; auto.
  - intros v t _ IH Hl. eapply vt_top; eauto.
  - intros E c pts vs a Ec W _ IH Hl. eapply vt_con; eauto.
  - intros v t M _ IH Hl. apply vt_mu; auto.
  - intros; constructor.
  - intros v vs t ts _ IH _ IHl Hl.
    constructor; [apply IH; intros l Hl'; apply Hl; apply in_or_app; auto
                 | apply IHl; intros l Hl'; apply Hl; apply in_or_app; auto].
Qed.

Lemma vtyped_agree Σ Σ' X v t :
  vtyped sigs Σ v t ->
  (forall l, In l (vlocs v) -> ~ In l X) -> sagree Σ Σ' X -> scope_ext Σ Σ' ->
  vtyped sigs Σ' v t.
Proof. intros Hv Hl Ha Hs. eapply (proj1 (vtyped_agree_all Σ Σ' X Ha Hs)); eauto. Qed.

Lemma vtyped_ext Σ Σ' v t :
  vtyped sigs Σ v t -> sagree Σ Σ' [] -> scope_ext Σ Σ' -> vtyped sigs Σ' v t.
Proof. intros; eapply vtyped_agree; eauto. intros ? ? []. Qed.

Lemma obj_ok_agree Σ Σ' X o h :
  obj_ok sigs Σ o h ->
  (forall r, In r (olocs o) -> ~ In r X) -> sagree Σ Σ' X -> scope_ext Σ Σ' ->
  obj_ok sigs Σ' o h.
Proof.
  intros Ho Hr Ha Hs. destruct o as [vs|kvs|kvs], h as [t|fs r|G]; simpl in *; try contradiction.
  - rewrite Forall_forall in *. intros v Hin. eapply vtyped_agree; eauto.
    intros l Hl. apply Hr. apply in_flat_map. eauto.
  - destruct Ho as (Hn & Hq & Hf). repeat split; auto.
    rewrite Forall_forall in *. intros p Hin. eapply vtyped_agree; eauto.
    intros l Hl. apply Hr. apply in_flat_map. eauto.
  - intros x v Hx. destruct (Ho x v Hx) as (t & Ht & Hv). exists t; split; auto.
    eapply vtyped_agree; eauto. intros l Hl. apply Hr. apply in_flat_map.
    exists (x, v); split; auto. apply lookup_in; auto.
Qed.

Lemma vtyped_clo_dtyped Σ H sc e t :
  vtyped sigs Σ (VClo sc e) t -> dtyped sigs Σ H (VClo sc e) t [].
Proof.
  intros Hv. remember (VClo sc e) as v eqn:E. induction Hv; subst; try discriminate.
  - inversion E; subst. apply dt_clo. eapply vt_clo; eauto.
  - apply dt_unionl; auto.
  - apply dt_unionr; auto.
  - eapply dt_top; eauto.
  - apply dt_mu; auto.
Qed.

(** ** Deep typing: stability and structure *)

Lemma vtyped_clo_scope Σ Σ' sc e t :
  vtyped sigs Σ (VClo sc e) t -> scope_ext Σ Σ' -> vtyped sigs Σ' (VClo sc e) t.
Proof.
  intros Hv Hs. remember (VClo sc e) as v eqn:E. induction Hv; subst; try discriminate.
  - inversion E; subst. eapply vt_clo; eauto.
  - apply vt_unionl; auto.
  - apply vt_unionr; auto.
  - eapply vt_top; eauto.
  - apply vt_mu; auto.
Qed.

Lemma dtyped_agree Σ H Σ' H' :
  scope_ext Σ Σ' ->
  (forall v t O, dtyped sigs Σ H v t O ->
     (forall m, In m O -> nth_error H' m = nth_error H m) -> dtyped sigs Σ' H' v t O) /\
  (forall vs t Os, dtypeds sigs Σ H vs t Os ->
     (forall m, In m (concat Os) -> nth_error H' m = nth_error H m) -> dtypeds sigs Σ' H' vs t Os) /\
  (forall kvs fs r Os, dfields sigs Σ H kvs fs r Os ->
     (forall m, In m (concat Os) -> nth_error H' m = nth_error H m) -> dfields sigs Σ' H' kvs fs r Os) /\
  (forall vs ts Os, dtypedl sigs Σ H vs ts Os ->
     (forall m, In m (concat Os) -> nth_error H' m = nth_error H m) -> dtypedl sigs Σ' H' vs ts Os).
Proof.
  intros Hs.
  apply (dtyped_comb sigs Σ H
    (fun v t O _ => (forall m, In m O -> nth_error H' m = nth_error H m) -> dtyped sigs Σ' H' v t O)
    (fun vs t Os _ => (forall m, In m (concat Os) -> nth_error H' m = nth_error H m) -> dtypeds sigs Σ' H' vs t Os)
    (fun kvs fs r Os _ => (forall m, In m (concat Os) -> nth_error H' m = nth_error H m) -> dfields sigs Σ' H' kvs fs r Os)
    (fun vs ts Os _ => (forall m, In m (concat Os) -> nth_error H' m = nth_error H m) -> dtypedl sigs Σ' H' vs ts Os)).
  - intros; constructor.
  - intros; constructor.
  - intros; constructor.
  - intros; constructor.
  - intros v t O d IH Hm. constructor. auto.
  - intros sc e ins outs Hv Hm. constructor. eapply vtyped_clo_scope; eauto.
  - intros l vs t Os e d IH n Hm. apply dt_list with (vs := vs); auto.
    + rewrite Hm; [exact e | apply in_eq].
    + apply IH. intros m Hm'. apply Hm. apply in_cons; assumption.
  - intros l kvs fs r Os e n n0 d IH n1 Hm. apply dt_rec with (kvs := kvs); auto.
    + rewrite Hm; [exact e | apply in_eq].
    + apply IH. intros m Hm'. apply Hm. apply in_cons; assumption.
  - intros v a b O d IH Hm. apply dt_unionl; auto.
  - intros v a b O d IH Hm. apply dt_unionr; auto.
  - intros v t O d IH Hm. eapply dt_top; eauto.
  - intros E c pts vs a Os Ec W d IH N Hm. eapply dt_con; eauto.
  - intros v t O M d IH Hm. apply dt_mu; auto.
  - intros; constructor.
  - intros v vs t O Os d IH d0 IH0 Hm. constructor.
    + apply IH. intros m Hm'. apply Hm. simpl. apply in_or_app; auto.
    + apply IH0. intros m Hm'. apply Hm. simpl. apply in_or_app; auto.
  - intros; constructor.
  - intros k v kvs fs r O Os d IH d0 IH0 Hm. constructor.
    + apply IH. intros m Hm'. apply Hm. simpl. apply in_or_app; auto.
    + apply IH0. intros m Hm'. apply Hm. simpl. apply in_or_app; auto.
  - intros; constructor.
  - intros v vs t ts O Os d IH d0 IH0 Hm. constructor.
    + apply IH. intros m Hm'. apply Hm. simpl. apply in_or_app; auto.
    + apply IH0. intros m Hm'. apply Hm. simpl. apply in_or_app; auto.
Qed.

Lemma dtyped_agree1 Σ H Σ' H' v t O :
  dtyped sigs Σ H v t O -> scope_ext Σ Σ' ->
  (forall m, In m O -> nth_error H' m = nth_error H m) -> dtyped sigs Σ' H' v t O.
Proof. intros. eapply (proj1 (dtyped_agree Σ H Σ' H' H1)); eauto. Qed.

(** Structure of a region: every location in it holds a list or dict whose
    references stay inside the region, and the value's own references are in
    it. *)
Definition is_container (o : obj) : Prop :=
  match o with OScope _ => False | _ => True end.

Lemma dtyped_struct Σ H :
  (forall v t O, dtyped sigs Σ H v t O ->
     NoDup O /\ (forall l, In l (vlocs v) -> In l O) /\
     (forall l, In l O -> exists o, nth_error H l = Some o /\ is_container o /\
                         forall r, In r (olocs o) -> In r O)) /\
  (forall vs t Os, dtypeds sigs Σ H vs t Os ->
     (forall l, In l (flat_map vlocs vs) -> In l (concat Os)) /\
     (forall l, In l (concat Os) -> exists o, nth_error H l = Some o /\ is_container o /\
                         forall r, In r (olocs o) -> In r (concat Os))) /\
  (forall kvs fs r Os, dfields sigs Σ H kvs fs r Os ->
     (forall l, In l (flat_map (fun p => vlocs (snd p)) kvs) -> In l (concat Os)) /\
     (forall l, In l (concat Os) -> exists o, nth_error H l = Some o /\ is_container o /\
                         forall r, In r (olocs o) -> In r (concat Os))) /\
  (forall vs ts Os, dtypedl sigs Σ H vs ts Os ->
     (forall l, In l (flat_map vlocs vs) -> In l (concat Os)) /\
     (forall l, In l (concat Os) -> exists o, nth_error H l = Some o /\ is_container o /\
                         forall r, In r (olocs o) -> In r (concat Os))).
Proof.
  apply (dtyped_comb sigs Σ H
    (fun v t O _ => NoDup O /\ (forall l, In l (vlocs v) -> In l O) /\
     (forall l, In l O -> exists o, nth_error H l = Some o /\ is_container o /\
                         forall r, In r (olocs o) -> In r O))
    (fun vs t Os _ => (forall l, In l (flat_map vlocs vs) -> In l (concat Os)) /\
     (forall l, In l (concat Os) -> exists o, nth_error H l = Some o /\ is_container o /\
                         forall r, In r (olocs o) -> In r (concat Os)))
    (fun kvs fs r Os _ => (forall l, In l (flat_map (fun p => vlocs (snd p)) kvs) -> In l (concat Os)) /\
     (forall l, In l (concat Os) -> exists o, nth_error H l = Some o /\ is_container o /\
                         forall r, In r (olocs o) -> In r (concat Os)))
    (fun vs ts Os _ => (forall l, In l (flat_map vlocs vs) -> In l (concat Os)) /\
     (forall l, In l (concat Os) -> exists o, nth_error H l = Some o /\ is_container o /\
                         forall r, In r (olocs o) -> In r (concat Os)))).
  - intros; simpl; repeat split; try constructor; tauto.
  - intros; simpl; repeat split; try constructor; tauto.
  - intros; simpl; repeat split; try constructor; tauto.
  - intros; simpl; repeat split; try constructor; tauto.
  - intros v t O d IH. simpl. exact IH.
  - intros; simpl; repeat split; try constructor; tauto.
  - intros l vs t Os e d [Hv Ho] n. repeat split.
    + exact n.
    + simpl. intros l0 [<-|F]; [apply in_eq | contradiction].
    + intros l0 Hl0. simpl in Hl0. destruct Hl0 as [<-|Hl0].
      * exists (OList vs); repeat split; auto. simpl. intros r0 Hr. apply in_cons. apply Hv; auto.
      * destruct (Ho l0 Hl0) as (o & Eo & Co & Ro). exists o; repeat split; auto.
        intros r0 Hr. apply in_cons. auto.
  - intros l kvs fs r Os e n n0 d [Hv Ho] n1. repeat split.
    + exact n1.
    + simpl. intros l0 [<-|F]; [apply in_eq | contradiction].
    + intros l0 Hl0. simpl in Hl0. destruct Hl0 as [<-|Hl0].
      * exists (ODict kvs); repeat split; auto. simpl. intros r0 Hr. apply in_cons. apply Hv; auto.
      * destruct (Ho l0 Hl0) as (o & Eo & Co & Ro). exists o; repeat split; auto.
        intros r0 Hr. apply in_cons. auto.
  - intros v a b O d IH. exact IH.
  - intros v a b O d IH. exact IH.
  - intros v t O d IH. exact IH.
  - intros E c pts vs a Os Ec W d [Hv Ho] N. repeat split; auto.
  - intros v t O M d IH. exact IH.
  - intros; simpl; split; tauto.
  - intros v vs t O Os d (Hn & Hv & Ho) d0 (Hv' & Ho'). split.
    + simpl. intros l Hl. apply in_app_or in Hl as [Hl|Hl]; apply in_or_app; auto.
    + simpl. intros l Hl. apply in_app_or in Hl as [Hl|Hl].
      * destruct (Ho l Hl) as (o & ? & ? & Hr). exists o; repeat split; auto.
        intros r0 Hr0; apply in_or_app; auto.
      * destruct (Ho' l Hl) as (o & ? & ? & Hr). exists o; repeat split; auto.
        intros r0 Hr0; apply in_or_app; auto.
  - intros; simpl; split; tauto.
  - intros k v kvs fs r O Os d (Hn & Hv & Ho) d0 (Hv' & Ho'). split.
    + simpl. intros l Hl. apply in_app_or in Hl as [Hl|Hl]; apply in_or_app; auto.
    + simpl. intros l Hl. apply in_app_or in Hl as [Hl|Hl].
      * destruct (Ho l Hl) as (o & ? & ? & Hr). exists o; repeat split; auto.
        intros r0 Hr0; apply in_or_app; auto.
      * destruct (Ho' l Hl) as (o & ? & ? & Hr). exists o; repeat split; auto.
        intros r0 Hr0; apply in_or_app; auto.
  - intros; simpl; split; tauto.
  - intros v vs t ts O Os d (Hn & Hv & Ho) d0 (Hv' & Ho'). split.
    + simpl. intros l Hl. apply in_app_or in Hl as [Hl|Hl]; apply in_or_app; auto.
    + simpl. intros l Hl. apply in_app_or in Hl as [Hl|Hl].
      * destruct (Ho l Hl) as (o & ? & ? & Hr). exists o; repeat split; auto.
        intros r0 Hr0; apply in_or_app; auto.
      * destruct (Ho' l Hl) as (o & ? & ? & Hr). exists o; repeat split; auto.
        intros r0 Hr0; apply in_or_app; auto.
Qed.


(** ** Subsumption and retyping for deep typing *)

(** One level of [sub] on the right of a deep typing [D0] of [v] at a
    head type: the cases that do not look at the head. *)
Ltac dsub_rest D0 :=
  first [ exact D0 | eapply dt_top; exact D0
        | apply dt_unionl; solve [auto] | apply dt_unionr; solve [auto]
        | apply dt_mu; solve [auto] ].

Lemma dtyped_sub_all Σ H :
  (forall v t O, dtyped sigs Σ H v t O -> forall b, sub t b -> dtyped sigs Σ H v b O) /\
  (forall vs t Os, dtypeds sigs Σ H vs t Os -> forall b, sub t b -> dtypeds sigs Σ H vs b Os) /\
  (forall kvs fs r Os, dfields sigs Σ H kvs fs r Os ->
     forall fs' r', (forall k, fsub (field_at k fs r) (field_at k fs' r')) -> dfields sigs Σ H kvs fs' r' Os) /\
  (forall vs ts Os, dtypedl sigs Σ H vs ts Os -> forall ts', Forall2 sub ts ts' -> dtypedl sigs Σ H vs ts' Os).
Proof.
  apply (dtyped_comb sigs Σ H
    (fun v t O _ => forall b, sub t b -> dtyped sigs Σ H v b O)
    (fun vs t Os _ => forall b, sub t b -> dtypeds sigs Σ H vs b Os)
    (fun kvs fs r Os _ => forall fs' r', (forall k, fsub (field_at k fs r) (field_at k fs' r')) ->
                          dfields sigs Σ H kvs fs' r' Os)
    (fun vs ts Os _ => forall ts', Forall2 sub ts ts' -> dtypedl sigs Σ H vs ts' Os)).
  - intros n b Hs. assert (D0 : dtyped sigs Σ H (VInt n) TInt []) by constructor.
    apply sub_unfold in Hs. remember TInt as x eqn:E. induction Hs; subst; try discriminate; dsub_rest D0.
  - intros n b Hs. assert (D0 : dtyped sigs Σ H (VStr n) TStr []) by constructor.
    apply sub_unfold in Hs. remember TStr as x eqn:E. induction Hs; subst; try discriminate; dsub_rest D0.
  - intros n b Hs. assert (D0 : dtyped sigs Σ H (VBool n) TBool []) by constructor.
    apply sub_unfold in Hs. remember TBool as x eqn:E. induction Hs; subst; try discriminate; dsub_rest D0.
  - intros t b Hs. assert (D0 : dtyped sigs Σ H VNone (TMaybe t) []) by constructor.
    apply sub_unfold in Hs. remember (TMaybe t) as x eqn:E. induction Hs; subst; try discriminate;
      first [dsub_rest D0 | constructor].
  - intros v t O d IH b Hs. assert (D0 : dtyped sigs Σ H (VJust v) (TMaybe t) O) by (constructor; exact d).
    apply sub_unfold in Hs. remember (TMaybe t) as x eqn:E. induction Hs; subst; try discriminate;
      first [dsub_rest D0 | injection E as ->; constructor; auto].
  - intros sc e ins outs Hv b Hs. apply vtyped_clo_dtyped. eapply vtyped_sub; eauto.
  - intros l vs t Os e d IH n b Hs. assert (D0 : dtyped sigs Σ H (VLoc l) (TList t) (l :: concat Os))
      by (econstructor; eauto).
    apply sub_unfold in Hs. remember (TList t) as x eqn:E. induction Hs; subst; try discriminate;
      first [dsub_rest D0 | injection E as ->; econstructor; eauto].
  - intros l kvs fs r Os e n n0 d IH n1 b Hs.
    assert (D0 : dtyped sigs Σ H (VLoc l) (TRec fs r) (l :: concat Os)) by (econstructor; eauto).
    apply sub_unfold in Hs. remember (TRec fs r) as x eqn:E.
    induction Hs; subst; try discriminate; try dsub_rest D0.
    injection E as -> ->. econstructor; eauto.
    match goal with Hf : forall k, fsubR sub _ _ |- _ =>
      intros k t0 Hk; specialize (Hf k); rewrite Hk in Hf; inversion Hf; subst; eapply n0; eauto end.
  - intros v a b O d IH b0 Hs. assert (D0 : dtyped sigs Σ H v (TUnion a b) O) by (apply dt_unionl; exact d).
    apply sub_unfold in Hs. remember (TUnion a b) as x eqn:E. induction Hs; subst; try discriminate;
      first [dsub_rest D0 | injection E as -> ->; apply IH, sub_fold; auto].
  - intros v a b O d IH b0 Hs. assert (D0 : dtyped sigs Σ H v (TUnion a b) O) by (apply dt_unionr; exact d).
    apply sub_unfold in Hs. remember (TUnion a b) as x eqn:E. induction Hs; subst; try discriminate;
      first [dsub_rest D0 | injection E as -> ->; apply IH, sub_fold; auto].
  - intros v t O d IH b Hs. apply IH. eapply sub_top_any; eauto.
  - intros E c pts vs a Os Ec W d IH N b Hs.
    assert (D0 : dtyped sigs Σ H (VCon E c pts vs) (TEnum E a) (concat Os)) by (econstructor; eauto).
    apply sub_unfold in Hs. remember (TEnum E a) as x eqn:Ex. induction Hs; subst; try discriminate;
      try dsub_rest D0.
    injection Ex as -> ->. econstructor; eauto. apply IH.
    apply Forall2_map_in. intros t Ht. eapply payload_sub; eauto.
  - intros v t O M d IH b Hs. apply IH. eapply sub_trans; [apply sub_unfold_r; exact M | exact Hs].
  - intros; constructor.
  - intros v vs t O Os d IH d0 IH0 b Hs. constructor; auto.
  - intros; constructor.
  - intros k v kvs fs r O Os d IH d0 IH0 fs' r' Hf. constructor; auto.
    apply IH. apply fsub_fty. auto.
  - intros ts' F. inversion F; subst. constructor.
  - intros v vs t ts O Os d IH d0 IH0 ts' F. inversion F; subst. constructor; auto.
Qed.

Lemma dtyped_sub Σ H v t O b :
  dtyped sigs Σ H v t O -> sub t b -> dtyped sigs Σ H v b O.
Proof. intros. eapply (proj1 (dtyped_sub_all Σ H)); eauto. Qed.

Lemma dtyped_bot Σ H v O : ~ dtyped sigs Σ H v TBot O.
Proof. intros D; inversion D. Qed.

Lemma dt_maybe_inv Σ H v t O :
  dtyped sigs Σ H v (TMaybe t) O ->
  (v = VNone /\ O = []) \/ exists x, v = VJust x /\ dtyped sigs Σ H x t O.
Proof. intros D; inversion D; subst; eauto. Qed.

Lemma dt_list_inv Σ H v t O :
  dtyped sigs Σ H v (TList t) O ->
  exists l vs Os, v = VLoc l /\ nth_error H l = Some (OList vs) /\ dtypeds sigs Σ H vs t Os /\
                  O = l :: concat Os /\ NoDup (l :: concat Os).
Proof. intros D; inversion D; subst; eauto 10. Qed.

Lemma dt_rec_inv Σ H v fs r O :
  dtyped sigs Σ H v (TRec fs r) O ->
  exists l kvs Os, v = VLoc l /\ nth_error H l = Some (ODict kvs) /\ NoDup (map fst kvs) /\
    (forall k t, field_at k fs r = FReq t -> lookup k kvs <> None) /\
    dfields sigs Σ H kvs fs r Os /\ O = l :: concat Os /\ NoDup (l :: concat Os).
Proof. intros D; inversion D; subst; eauto 12. Qed.

Lemma dt_enum_inv Σ H v E a O :
  dtyped sigs Σ H v (TEnum E a) O ->
  exists c pts vs Os, v = VCon E c pts vs /\ g_ctors sigs E c = Some pts /\ wf_payload E pts /\
    dtypedl sigs Σ H vs (map (subst a) pts) Os /\ O = concat Os /\ NoDup O.
Proof. intros D; inversion D; subst; eauto 12. Qed.

Lemma dt_union_inv Σ H v a b O :
  dtyped sigs Σ H v (TUnion a b) O -> dtyped sigs Σ H v a O \/ dtyped sigs Σ H v b O.
Proof. intros D; inversion D; subst; auto. Qed.

Lemma dtypeds_map Σ H a b :
  (forall v O, dtyped sigs Σ H v a O -> dtyped sigs Σ H v b O) ->
  forall vs Os, dtypeds sigs Σ H vs a Os -> dtypeds sigs Σ H vs b Os.
Proof. intros Hf vs Os D; induction D; constructor; auto. Qed.

Lemma dfields_map Σ H fs1 r1 fs2 r2 :
  (forall k v O, dtyped sigs Σ H v (fty (field_at k fs1 r1)) O -> dtyped sigs Σ H v (fty (field_at k fs2 r2)) O) ->
  forall kvs Os, dfields sigs Σ H kvs fs1 r1 Os -> dfields sigs Σ H kvs fs2 r2 Os.
Proof. intros Hf kvs Os D; induction D; constructor; auto. Qed.

End Clo.

Section Retype.
Variable sigs : genv.

Lemma frsub_fty_dtyped Σ H f g v O :
  frsub f g -> dtyped sigs Σ H v (fty f) O ->
  (forall b, rsub (fty f) b -> dtyped sigs Σ H v b O) -> dtyped sigs Σ H v (fty g) O.
Proof.
  intros Hf D IH. inversion Hf; subst; simpl in *.
  - apply IH; auto.
  - exfalso; eapply dtyped_bot; eauto.
  - exfalso; eapply dtyped_bot; eauto.
  - destruct H0 as [->|[->| ->]]; apply IH; auto.
  - destruct H0 as [->|[->| ->]]; apply IH; auto.
  - exact D.
  - eapply dt_top; eauto.
Qed.

(** Retyping a fresh value changes no state: the deep typing follows.  By
    induction on the value's typing, since an enum's retyping is justified
    by its payload types ([payload_rsub]), not by a smaller [rsub]. *)
(** A fresh value's type unfolds before it is retyped. *)
Lemma rsub_mu_l t b : mu_ok t = true -> rsub (TMu t) b -> rsub (tunfold t) b.
Proof.
  intros M R. apply rsub_unfold in R. remember (TMu t) as x eqn:E.
  induction R; subst; try discriminate.
  - apply rs_sub. eapply sub_trans; [apply sub_unfold_r; exact M | exact H].
  - apply rs_unionr1; auto.
  - apply rs_unionr2; auto.
  - injection E as ->. apply rsub_fold; auto.
  - apply rs_mur; auto.
Qed.

(** One level of [rsub] on the right of a deep typing [D0] at a head type. *)
Ltac drsub_rest D0 :=
  first [ eapply dtyped_sub; [exact D0 | eassumption]
        | apply dt_unionl; solve [auto] | apply dt_unionr; solve [auto]
        | apply dt_mu; solve [auto] ].

Lemma dtyped_rsub_all Σ H :
  (forall v a O, dtyped sigs Σ H v a O -> forall b, rsub a b -> dtyped sigs Σ H v b O) /\
  (forall vs t Os, dtypeds sigs Σ H vs t Os -> forall b, rsub t b -> dtypeds sigs Σ H vs b Os) /\
  (forall kvs fs r Os, dfields sigs Σ H kvs fs r Os ->
     forall fs' r', (forall k, frsub (field_at k fs r) (field_at k fs' r')) -> dfields sigs Σ H kvs fs' r' Os) /\
  (forall vs ts Os, dtypedl sigs Σ H vs ts Os -> forall ts', Forall2 rsub ts ts' -> dtypedl sigs Σ H vs ts' Os).
Proof.
  apply (dtyped_comb sigs Σ H
    (fun v a O _ => forall b, rsub a b -> dtyped sigs Σ H v b O)
    (fun vs t Os _ => forall b, rsub t b -> dtypeds sigs Σ H vs b Os)
    (fun kvs fs r Os _ => forall fs' r', (forall k, frsub (field_at k fs r) (field_at k fs' r')) ->
                          dfields sigs Σ H kvs fs' r' Os)
    (fun vs ts Os _ => forall ts', Forall2 rsub ts ts' -> dtypedl sigs Σ H vs ts' Os)).
  - intros n b R. assert (D0 : dtyped sigs Σ H (VInt n) TInt []) by constructor.
    apply rsub_unfold in R. remember TInt as x eqn:E. induction R; subst; try discriminate; drsub_rest D0.
  - intros n b R. assert (D0 : dtyped sigs Σ H (VStr n) TStr []) by constructor.
    apply rsub_unfold in R. remember TStr as x eqn:E. induction R; subst; try discriminate; drsub_rest D0.
  - intros n b R. assert (D0 : dtyped sigs Σ H (VBool n) TBool []) by constructor.
    apply rsub_unfold in R. remember TBool as x eqn:E. induction R; subst; try discriminate; drsub_rest D0.
  - intros t b R. assert (D0 : dtyped sigs Σ H VNone (TMaybe t) []) by constructor.
    apply rsub_unfold in R. remember (TMaybe t) as x eqn:E. induction R; subst; try discriminate;
      first [drsub_rest D0 | constructor].
  - intros v t O d IH b R. assert (D0 : dtyped sigs Σ H (VJust v) (TMaybe t) O) by (constructor; exact d).
    apply rsub_unfold in R. remember (TMaybe t) as x eqn:E. induction R; subst; try discriminate;
      first [drsub_rest D0 | injection E as ->; constructor; auto].
  - intros sc e ins outs Hv b R. assert (D0 : dtyped sigs Σ H (VClo sc e) (TQuote ins outs) [])
      by (constructor; exact Hv).
    apply rsub_unfold in R. remember (TQuote ins outs) as x eqn:E. induction R; subst; try discriminate;
      drsub_rest D0.
  - intros l vs t Os e d IH n b R. assert (D0 : dtyped sigs Σ H (VLoc l) (TList t) (l :: concat Os))
      by (econstructor; eauto).
    apply rsub_unfold in R. remember (TList t) as x eqn:E. induction R; subst; try discriminate;
      first [drsub_rest D0 | injection E as ->; econstructor; eauto].
  - intros l kvs fs r Os e n n0 d IH n1 b R.
    assert (D0 : dtyped sigs Σ H (VLoc l) (TRec fs r) (l :: concat Os)) by (econstructor; eauto).
    apply rsub_unfold in R. remember (TRec fs r) as x eqn:E.
    induction R; subst; try discriminate; try drsub_rest D0.
    injection E as -> ->. eapply dt_rec; eauto.
    match goal with Hf : forall k, frsubR rsub _ _ |- _ =>
      intros k t Hk; specialize (Hf k); rewrite Hk in Hf; inversion Hf; subst; eapply n0; eauto end.
  - intros v a b O d IH c R. assert (D0 : dtyped sigs Σ H v (TUnion a b) O) by (apply dt_unionl; exact d).
    apply rsub_unfold in R. remember (TUnion a b) as x eqn:E. induction R; subst; try discriminate;
      first [drsub_rest D0 | injection E as -> ->; apply IH, rsub_fold; auto].
  - intros v a b O d IH c R. assert (D0 : dtyped sigs Σ H v (TUnion a b) O) by (apply dt_unionr; exact d).
    apply rsub_unfold in R. remember (TUnion a b) as x eqn:E. induction R; subst; try discriminate;
      first [drsub_rest D0 | injection E as -> ->; apply IH, rsub_fold; auto].
  - intros v t O d IH b R. assert (D0 : dtyped sigs Σ H v TTop O) by (eapply dt_top; exact d).
    apply rsub_unfold in R. remember TTop as x eqn:E. induction R; subst; try discriminate;
      drsub_rest D0.
  - intros E c pts vs a Os Ec W d IH N b R.
    assert (D0 : dtyped sigs Σ H (VCon E c pts vs) (TEnum E a) (concat Os)) by (econstructor; eauto).
    apply rsub_unfold in R. remember (TEnum E a) as x eqn:Ex. induction R; subst; try discriminate;
      try drsub_rest D0.
    injection Ex as -> ->. eapply dt_con; eauto. apply IH.
    apply Forall2_map_in. intros t Ht. eapply payload_rsub; eauto.
  - intros v t O M d IH b R. apply IH. apply rsub_mu_l; auto.
  - intros; constructor.
  - intros v vs t O Os d IH d0 IH0 b R. constructor; auto.
  - intros; constructor.
  - intros k v kvs fs r O Os d IH d0 IH0 fs' r' Hf. constructor; auto.
    eapply frsub_fty_dtyped; eauto.
  - intros ts' F. inversion F; subst. constructor.
  - intros v vs t ts O Os d IH d0 IH0 ts' F. inversion F; subst. constructor; auto.
Qed.

Lemma dtyped_rsub Σ H a b :
  rsub a b -> forall v O, dtyped sigs Σ H v a O -> dtyped sigs Σ H v b O.
Proof. intros R v O D. eapply (proj1 (dtyped_rsub_all Σ H)); eauto. Qed.

(** A list or record type is never below an immutable type. *)
Lemma sub_loc_mut a b : sub a b -> immutable a = false ->
  (exists t, a = TList t) \/ (exists fs r, a = TRec fs r) -> immutable b = false.
Proof.
  intros Hs. apply sub_unfold in Hs. induction Hs; simpl; intros Ha Hk; auto;
    try (destruct Hk as [[t0 Eq]|(fs0 & r0 & Eq)]; discriminate).
  - rewrite IHHs; auto.
  - rewrite IHHs; auto. apply andb_false_r.
  - pose proof (immutable_tunfold t) as Et. simpl in Et. rewrite <- Et. auto.
Qed.

Lemma sub_list_imm x b : sub (TList x) b -> immutable b = false.
Proof. intros Hs. eapply sub_loc_mut; eauto. Qed.

Lemma sub_rec_imm fs r b : sub (TRec fs r) b -> immutable b = false.
Proof. intros Hs. eapply sub_loc_mut; eauto. Qed.

Lemma concat_map_nil {A B} (l : list A) : concat (map (fun _ => @nil B) l) = [].
Proof. induction l; simpl; auto. Qed.

Lemma vtyped_imm_all Σ H :
  (forall v t, vtyped sigs Σ v t -> immutable t = true -> dtyped sigs Σ H v t [] /\ vlocs v = []) /\
  (forall vs ts, vtypedl sigs Σ vs ts -> forallb immutable ts = true ->
     dtypedl sigs Σ H vs ts (map (fun _ => []) vs) /\ flat_map vlocs vs = []).
Proof.
  apply (vtyped_comb sigs Σ
    (fun v t _ => immutable t = true -> dtyped sigs Σ H v t [] /\ vlocs v = [])
    (fun vs ts _ => forallb immutable ts = true ->
       dtypedl sigs Σ H vs ts (map (fun _ => []) vs) /\ flat_map vlocs vs = []));
    simpl; intros; try discriminate.
  - split; [constructor | reflexivity].
  - split; [constructor | reflexivity].
  - split; [constructor | reflexivity].
  - split; [constructor | reflexivity].
  - destruct (H0 H1). split; [constructor|]; auto.
  - rewrite (sub_list_imm _ _ s) in H0. discriminate.
  - rewrite (sub_rec_imm _ _ _ s) in H0. discriminate.
  - split; [constructor; econstructor; eauto | reflexivity].
  - apply andb_true_iff in H1 as [? ?]. destruct (H0 H1). split; [apply dt_unionl|]; auto.
  - apply andb_true_iff in H1 as [? ?]. destruct (H0 H2). split; [apply dt_unionr|]; auto.
  - assert (Hi : forallb immutable (map (subst a) pts) = true).
    { apply forallb_forall. intros t Ht. apply in_map_iff in Ht as (pt & <- & Hpt).
      eapply payload_imm; eauto. }
    destruct (H0 Hi) as [D L]. split; auto. rewrite <- (concat_map_nil vs).
    eapply dt_con; eauto. rewrite concat_map_nil. constructor.
  - assert (Hi : immutable (tunfold t) = true) by (rewrite immutable_tunfold; exact H1).
    destruct (H0 Hi) as [D L]. split; [apply dt_mu|]; auto.
  - split; constructor.
  - apply andb_true_iff in H2 as [? ?]. destruct (H0 H2), (H1 H3). split; [constructor|]; auto.
    rewrite H5, H7. reflexivity.
Qed.

Lemma vtyped_imm_dtyped Σ H v t :
  vtyped sigs Σ v t -> immutable t = true -> dtyped sigs Σ H v t [].
Proof. intros. eapply (proj1 (vtyped_imm_all Σ H)); eauto. Qed.

Lemma vtyped_imm_vlocs Σ v t :
  vtyped sigs Σ v t -> immutable t = true -> vlocs v = [].
Proof. intros. eapply (proj1 (vtyped_imm_all Σ [])); eauto. Qed.

End Retype.
