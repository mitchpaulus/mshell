(** * Partly new values.

    A value with mark [MList m] or [MRec f] (Typing.v) is a new list or dict
    whose elements may be stored values.  Its own type may change, as for
    any new value, but the stored values inside it keep theirs: they are
    typed by the store, and retyped only by [sub].  This file proves the
    facts about [mtyped] (Invariant.v) the invariant needs: its structure,
    that it survives changes of the store typing that keep every live
    entry, retyping ([msub]), and committing.

    Stored values never point into a region: a region's locations have the
    store type [HDead] until they are committed ([inv_reg]), and a typed
    value reaches only live locations ([vt_live]). *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Variance Typing Interp Invariant RtLemmas Commit.

(** [Σ'] keeps every live entry of [Σ]: it changes only locations with no
    store type (dead ones, and new objects not yet committed). *)
Definition keeps_live (Σ Σ' : store_ty) : Prop :=
  forall l h, nth_error Σ l = Some h -> h <> HDead -> nth_error Σ' l = Some h.

Definition dead_on (Σ : store_ty) (O : list loc) : Prop :=
  forall l, In l O -> nth_error Σ l = Some HDead.

Lemma keeps_live_refl Σ : keeps_live Σ Σ.
Proof. intros l h E _; exact E. Qed.

Lemma keeps_live_trans a b c : keeps_live a b -> keeps_live b c -> keeps_live a c.
Proof. intros H1 H2 l h E N. apply H2; auto. Qed.

Lemma keeps_live_scope Σ Σ' : keeps_live Σ Σ' -> scope_ext Σ Σ'.
Proof. intros K l G E. apply K; auto. discriminate. Qed.

(** A change at locations with no store type keeps every live entry. *)
Lemma sagree_keeps_live Σ Σ' X : sagree Σ Σ' X -> dead_on Σ X -> keeps_live Σ Σ'.
Proof.
  intros A D l h E N. apply A; auto. intro Hx. rewrite (D l Hx) in E. inversion E; subst. auto.
Qed.

Lemma keeps_live_app Σ X : keeps_live Σ (Σ ++ X).
Proof. intros l h E _. rewrite nth_error_app1; auto. eapply nth_error_lt; eauto. Qed.

Lemma dead_on_nonscope Σ O : dead_on Σ O -> nonscope_on Σ O.
Proof. intros D l Hl. exists HDead. split; auto. Qed.

Lemma dead_on_mono Σ O1 O2 : dead_on Σ O1 -> (forall l, In l O2 -> In l O1) -> dead_on Σ O2.
Proof. intros D Hi l Hl. apply D, Hi, Hl. Qed.

Lemma dead_on_agree Σ Σ' X O : dead_on Σ O -> sagree Σ Σ' X -> (forall l, In l O -> ~ In l X) ->
  dead_on Σ' O.
Proof. intros D A Hd l Hl. apply A; auto. Qed.

Section Partial.
Variable sigs : genv.

(** ** Typed values reach only live locations *)

Lemma vt_live_all Σ :
  (forall v t, vtyped sigs Σ v t -> forall l, In l (vlocs v) -> live Σ l) /\
  (forall vs ts, vtypedl sigs Σ vs ts -> forall l, In l (flat_map vlocs vs) -> live Σ l).
Proof.
  apply (vtyped_comb sigs Σ
    (fun v t _ => forall l, In l (vlocs v) -> live Σ l)
    (fun vs ts _ => forall l, In l (flat_map vlocs vs) -> live Σ l)); simpl; try tauto.
  - intros l a t E _ l0 [<-|[]] E'. congruence.
  - intros l fs r t E _ l0 [<-|[]] E'. congruence.
  - intros v vs t ts _ IH _ IH' l Hl. apply in_app_or in Hl as [?|?]; auto.
Qed.

Lemma vt_live Σ v t : vtyped sigs Σ v t -> forall l, In l (vlocs v) -> live Σ l.
Proof. apply (proj1 (vt_live_all Σ)). Qed.

Lemma ok_live Σ o h :
  obj_ok sigs Σ o h -> is_scope h = false -> forall l, In l (olocs o) -> live Σ l.
Proof.
  intros Ok Ns l Hl. destruct o as [vs|kvs|kvs], h as [t|fs r|G|]; simpl in *; try contradiction;
    try discriminate.
  - apply in_flat_map in Hl as (v & Hv & Hl). rewrite Forall_forall in Ok.
    eapply vt_live; eauto.
  - destruct Ok as (_ & _ & Ok). apply in_flat_map in Hl as (p & Hp & Hl). rewrite Forall_forall in Ok.
    eapply vt_live; eauto.
Qed.

(** ** Keeping live entries keeps typings *)

Lemma vtyped_keep_all Σ Σ' :
  keeps_live Σ Σ' ->
  (forall v t, vtyped sigs Σ v t -> vtyped sigs Σ' v t) /\
  (forall vs ts, vtypedl sigs Σ vs ts -> vtypedl sigs Σ' vs ts).
Proof.
  intros K.
  apply (vtyped_comb sigs Σ (fun v t _ => vtyped sigs Σ' v t) (fun vs ts _ => vtypedl sigs Σ' vs ts)).
  - intros; constructor.
  - intros; constructor.
  - intros; constructor.
  - intros l a t E Hs. eapply vt_list; [apply K; [exact E | discriminate] | exact Hs].
  - intros l fs r t E Hs. eapply vt_rec; [apply K; [exact E | discriminate] | exact Hs].
  - intros sc e G ins outs E Hc. eapply vt_clo; [apply K; [exact E | discriminate] | exact Hc].
  - intros v a b _ IH. apply vt_unionl; auto.
  - intros v a b _ IH. apply vt_unionr; auto.
  - intros v t _ IH. eapply vt_top; eauto.
  - intros E c pts vs a Ec W _ IH. eapply vt_con; eauto.
  - intros v t M _ IH. apply vt_mu; auto.
  - constructor.
  - intros; constructor; auto.
Qed.

Lemma vtyped_keep Σ Σ' v t : vtyped sigs Σ v t -> keeps_live Σ Σ' -> vtyped sigs Σ' v t.
Proof. intros V K. exact (proj1 (vtyped_keep_all Σ Σ' K) v t V). Qed.

Lemma obj_ok_keep Σ Σ' o h : obj_ok sigs Σ o h -> keeps_live Σ Σ' -> obj_ok sigs Σ' o h.
Proof.
  intros Ok K. destruct o as [vs|kvs|kvs], h as [t|fs r|G|]; simpl in *; try contradiction.
  - rewrite Forall_forall in *. intros v Hv. eapply vtyped_keep; eauto.
  - destruct Ok as (Hn & Hq & Hf). repeat split; auto.
    rewrite Forall_forall in *. intros p Hp. eapply vtyped_keep; eauto.
  - intros x v Hx. destruct (Ok x v Hx) as (t & Ht & Hv). exists t; split; auto.
    eapply vtyped_keep; eauto.
Qed.

Lemma committed_keep Σ Σ' H O : committed sigs Σ H O -> keeps_live Σ Σ' -> committed sigs Σ' H O.
Proof.
  intros C K l Hl. destruct (C l Hl) as (h & o & E & Ns & Eo & Ok).
  exists h, o. repeat split; auto.
  - apply K; auto. intros ->. destruct o; simpl in Ok; exact Ok.
  - eapply obj_ok_keep; eauto.
Qed.

(** ** Structure *)

Lemma mtyped_struct Σ H :
  (forall v t m O, mtyped sigs Σ H v t m O ->
     NoDup O /\ (forall l, In l (vlocs v) -> In l O \/ live Σ l) /\
     (partial m = true -> exists l, v = VLoc l /\ In l O) /\
     (forall l, In l O -> exists o, nth_error H l = Some o /\ is_container o /\
                         forall r, In r (olocs o) -> In r O \/ live Σ r)) /\
  (forall vs t m Os, mtypeds sigs Σ H vs t m Os ->
     (forall l, In l (flat_map vlocs vs) -> In l (concat Os) \/ live Σ l) /\
     (forall l, In l (concat Os) -> exists o, nth_error H l = Some o /\ is_container o /\
                         forall r, In r (olocs o) -> In r (concat Os) \/ live Σ r)) /\
  (forall kvs fs r f Os, mfields sigs Σ H kvs fs r f Os ->
     (forall l, In l (flat_map (fun p => vlocs (snd p)) kvs) -> In l (concat Os) \/ live Σ l) /\
     (forall l, In l (concat Os) -> exists o, nth_error H l = Some o /\ is_container o /\
                         forall r, In r (olocs o) -> In r (concat Os) \/ live Σ r)).
Proof.
  apply (mtyped_comb sigs Σ H
    (fun v t m O _ => NoDup O /\ (forall l, In l (vlocs v) -> In l O \/ live Σ l) /\
       (partial m = true -> exists l, v = VLoc l /\ In l O) /\
       (forall l, In l O -> exists o, nth_error H l = Some o /\ is_container o /\
                          forall r, In r (olocs o) -> In r O \/ live Σ r))
    (fun vs t m Os _ =>
       (forall l, In l (flat_map vlocs vs) -> In l (concat Os) \/ live Σ l) /\
       (forall l, In l (concat Os) -> exists o, nth_error H l = Some o /\ is_container o /\
                           forall r, In r (olocs o) -> In r (concat Os) \/ live Σ r))
    (fun kvs fs r f Os _ =>
       (forall l, In l (flat_map (fun p => vlocs (snd p)) kvs) -> In l (concat Os) \/ live Σ l) /\
       (forall l, In l (concat Os) -> exists o, nth_error H l = Some o /\ is_container o /\
                           forall r, In r (olocs o) -> In r (concat Os) \/ live Σ r))).
  - (* stored *)
    intros v t Hv. repeat split.
    + constructor.
    + intros l Hl. right. eapply vt_live; eauto.
    + discriminate.
    + intros l [].
  - (* new *)
    intros v t O D. destruct (proj1 (dtyped_struct sigs Σ H) _ _ _ D) as (N & Hv & Ho).
    repeat split; auto.
    + discriminate.
    + intros l Hl. destruct (Ho l Hl) as (o & E & C & Hr). exists o; repeat split; auto.
  - (* list *)
    intros l vs a m Os E d [Hv Ho] N. repeat split; auto.
    + intros l0 [<-|[]]. left; apply in_eq.
    + intros _. exists l; split; auto. apply in_eq.
    + intros l0 [<-|Hl0].
      * exists (OList vs); repeat split; auto. intros r Hr. destruct (Hv r Hr); auto. left; apply in_cons; auto.
      * destruct (Ho l0 Hl0) as (o & Eo & C & Hr). exists o; repeat split; auto.
        intros r0 Hr0. destruct (Hr r0 Hr0); auto. left; apply in_cons; auto.
  - (* record *)
    intros l kvs fs r f Os E Nk Rq d [Hv Ho] N. repeat split; auto.
    + intros l0 [<-|[]]. left; apply in_eq.
    + intros _. exists l; split; auto. apply in_eq.
    + intros l0 [<-|Hl0].
      * exists (ODict kvs); repeat split; auto. intros r0 Hr. destruct (Hv r0 Hr); auto. left; apply in_cons; auto.
      * destruct (Ho l0 Hl0) as (o & Eo & C & Hr). exists o; repeat split; auto.
        intros r0 Hr0. destruct (Hr r0 Hr0); auto. left; apply in_cons; auto.
  - intros t m. simpl. split; intros l [].
  - intros v vs t m O Os d (_ & Hv & _ & Ho) d0 [Hvs Hos]. simpl. split.
    + intros l Hl. apply in_app_or in Hl as [Hl|Hl].
      * destruct (Hv l Hl); [left; apply in_or_app; auto | auto].
      * destruct (Hvs l Hl); [left; apply in_or_app; auto | auto].
    + intros l Hl. apply in_app_or in Hl as [Hl|Hl].
      * destruct (Ho l Hl) as (o & E & C & Hr). exists o; repeat split; auto.
        intros r Hr'. destruct (Hr r Hr'); [left; apply in_or_app | ]; auto.
      * destruct (Hos l Hl) as (o & E & C & Hr). exists o; repeat split; auto.
        intros r Hr'. destruct (Hr r Hr'); [left; apply in_or_app | ]; auto.
  - intros fs r f. simpl. split; intros l [].
  - intros k v kvs fs r f O Os d (_ & Hv & _ & Ho) d0 [Hvs Hos]. simpl. split.
    + intros l Hl. apply in_app_or in Hl as [Hl|Hl].
      * destruct (Hv l Hl); [left; apply in_or_app; auto | auto].
      * destruct (Hvs l Hl); [left; apply in_or_app; auto | auto].
    + intros l Hl. apply in_app_or in Hl as [Hl|Hl].
      * destruct (Ho l Hl) as (o & E & C & Hr). exists o; repeat split; auto.
        intros r0 Hr'. destruct (Hr r0 Hr'); [left; apply in_or_app | ]; auto.
      * destruct (Hos l Hl) as (o & E & C & Hr). exists o; repeat split; auto.
        intros r0 Hr'. destruct (Hr r0 Hr'); [left; apply in_or_app | ]; auto.
Qed.

Lemma mtyped_bot Σ H v m O : ~ mtyped sigs Σ H v TBot m O.
Proof.
  intros M. inversion M; subst.
  - eapply vt_bot; eauto.
  - eapply dtyped_bot; eauto.
Qed.

(** A partly new value is a location holding a list or a dict. *)
Lemma mtyped_partial Σ H v t m O :
  mtyped sigs Σ H v t m O -> partial m = true ->
  exists l, v = VLoc l /\ In l O /\
    ((exists m' a vs, m = MList m' /\ t = TList a /\ nth_error H l = Some (OList vs)) \/
     (exists f fs r kvs, m = MRec f /\ t = TRec fs r /\ nth_error H l = Some (ODict kvs))).
Proof.
  intros M P. inversion M; subst; try discriminate.
  - exists l. split; [reflexivity|]. split; [apply in_eq|]. left.
    do 3 eexists; split; [reflexivity | split; [reflexivity | eassumption]].
  - exists l. split; [reflexivity|]. split; [apply in_eq|]. right.
    do 4 eexists; split; [reflexivity | split; [reflexivity | eassumption]].
Qed.

Lemma mt_list_inv Σ H v t m O :
  mtyped sigs Σ H v (TList t) (MList m) O ->
  exists l vs Os, v = VLoc l /\ nth_error H l = Some (OList vs) /\ mtypeds sigs Σ H vs t m Os /\
                  O = l :: concat Os /\ NoDup (l :: concat Os).
Proof. intros M; inversion M; subst; eauto 10. Qed.

Lemma mt_rec_inv Σ H v fs r f O :
  mtyped sigs Σ H v (TRec fs r) (MRec f) O ->
  exists l kvs Os, v = VLoc l /\ nth_error H l = Some (ODict kvs) /\ NoDup (map fst kvs) /\
    (forall k t, field_at k fs r = FReq t -> lookup k kvs <> None) /\
    mfields sigs Σ H kvs fs r f Os /\ O = l :: concat Os /\ NoDup (l :: concat Os).
Proof. intros M; inversion M; subst; eauto 12. Qed.

(** A partly new value is a list or a dict, so a kind test on it is
    decided by its type. *)
Lemma partial_operand Σ H v t m O :
  mtyped sigs Σ H v t m O -> partial m = true ->
  (exists a, t = TList a /\ kind_of H v = Some KList) \/
  (exists fs r, t = TRec fs r /\ kind_of H v = Some KDict).
Proof.
  intros M P. destruct (mtyped_partial Σ H v t m O M P) as
    (l & -> & _ & [(m' & a & vs & _ & -> & E)|(f & fs & r & kvs & _ & -> & E)]).
  - left. exists a. split; auto. simpl. rewrite E. reflexivity.
  - right. exists fs, r. split; auto. simpl. rewrite E. reflexivity.
Qed.

Lemma mtypeds_app Σ H vs1 vs2 t m Os1 Os2 :
  mtypeds sigs Σ H vs1 t m Os1 -> mtypeds sigs Σ H vs2 t m Os2 ->
  mtypeds sigs Σ H (vs1 ++ vs2) t m (Os1 ++ Os2).
Proof. induction 1; simpl; auto. intros; constructor; auto. Qed.

Lemma mfields_ext Σ H kvs fs1 r1 f1 Os fs2 r2 f2 :
  mfields sigs Σ H kvs fs1 r1 f1 Os ->
  (forall k, In k (map fst kvs) -> field_at k fs1 r1 = field_at k fs2 r2 /\ f1 k = f2 k) ->
  mfields sigs Σ H kvs fs2 r2 f2 Os.
Proof.
  induction 1; intros E; constructor.
  - destruct (E k (in_eq _ _)) as [E1 E2]. rewrite <- E1, <- E2. auto.
  - apply IHmfields. intros; apply E; simpl; auto.
Qed.

(** A new dict is a partly new one whose every value is new. *)
Lemma dtyped_rec_mtyped Σ H v fs r O :
  dtyped sigs Σ H v (TRec fs r) O -> mtyped sigs Σ H v (TRec fs r) (MRec (fun _ => Dp)) O.
Proof.
  intros D. apply dt_rec_inv in D as (l & kvs & Os & -> & E & Nk & Rq & Df & -> & N).
  apply mt_rec with (kvs := kvs); auto.
  clear -Df. induction Df; constructor; auto. constructor; auto.
Qed.

Lemma mtyped_new_list Σ H l t m :
  nth_error H l = Some (OList []) -> mtyped sigs Σ H (VLoc l) (TList t) (MList m) [l].
Proof.
  intros E. apply (mt_list sigs Σ H l [] t m [] E); [constructor | constructor; [simpl; tauto | constructor]].
Qed.

(** ** Keeping live entries, and objects outside the region *)

Lemma mtyped_keep_all Σ H Σ' H' :
  keeps_live Σ Σ' ->
  (forall v t m O, mtyped sigs Σ H v t m O ->
     (forall l, In l O -> nth_error H' l = nth_error H l) -> mtyped sigs Σ' H' v t m O) /\
  (forall vs t m Os, mtypeds sigs Σ H vs t m Os ->
     (forall l, In l (concat Os) -> nth_error H' l = nth_error H l) -> mtypeds sigs Σ' H' vs t m Os) /\
  (forall kvs fs r f Os, mfields sigs Σ H kvs fs r f Os ->
     (forall l, In l (concat Os) -> nth_error H' l = nth_error H l) -> mfields sigs Σ' H' kvs fs r f Os).
Proof.
  intros K.
  apply (mtyped_comb sigs Σ H
    (fun v t m O _ => (forall l, In l O -> nth_error H' l = nth_error H l) -> mtyped sigs Σ' H' v t m O)
    (fun vs t m Os _ => (forall l, In l (concat Os) -> nth_error H' l = nth_error H l) -> mtypeds sigs Σ' H' vs t m Os)
    (fun kvs fs r f Os _ => (forall l, In l (concat Os) -> nth_error H' l = nth_error H l) ->
       mfields sigs Σ' H' kvs fs r f Os)).
  - intros v t Hv _. constructor. eapply vtyped_keep; eauto.
  - intros v t O D Hm. constructor. eapply dtyped_agree1; eauto. apply keeps_live_scope; auto.
  - intros l vs a m Os E d IH N Hm. apply mt_list with (vs := vs); auto.
    + rewrite Hm; [exact E | apply in_eq].
    + apply IH. intros l0 Hl0. apply Hm. apply in_cons; auto.
  - intros l kvs fs r f Os E Nk Rq d IH N Hm. apply mt_rec with (kvs := kvs); auto.
    + rewrite Hm; [exact E | apply in_eq].
    + apply IH. intros l0 Hl0. apply Hm. apply in_cons; auto.
  - intros; constructor.
  - intros v vs t m O Os d IH d0 IH0 Hm. constructor.
    + apply IH. intros l Hl. apply Hm. simpl. apply in_or_app; auto.
    + apply IH0. intros l Hl. apply Hm. simpl. apply in_or_app; auto.
  - intros; constructor.
  - intros k v kvs fs r f O Os d IH d0 IH0 Hm. constructor.
    + apply IH. intros l Hl. apply Hm. simpl. apply in_or_app; auto.
    + apply IH0. intros l Hl. apply Hm. simpl. apply in_or_app; auto.
Qed.

Lemma mtyped_keep Σ H Σ' H' v t m O :
  mtyped sigs Σ H v t m O -> keeps_live Σ Σ' ->
  (forall l, In l O -> nth_error H' l = nth_error H l) -> mtyped sigs Σ' H' v t m O.
Proof. intros M K Hm. exact (proj1 (mtyped_keep_all Σ H Σ' H' K) _ _ _ _ M Hm). Qed.

(** ** Retyping *)

Lemma mtyped_msub_all Σ H :
  (forall v t m O, mtyped sigs Σ H v t m O -> forall b, msub m t b -> mtyped sigs Σ H v b m O) /\
  (forall vs t m Os, mtypeds sigs Σ H vs t m Os -> forall b, msub m t b -> mtypeds sigs Σ H vs b m Os) /\
  (forall kvs fs r f Os, mfields sigs Σ H kvs fs r f Os -> forall fs2 r2,
     (forall k, In k (map fst kvs) ->
        frsubR (msub (f k)) (field_at k fs r) (field_at k fs2 r2) /\
        (partial (f k) = true -> field_at k fs2 r2 <> FOpen)) ->
     mfields sigs Σ H kvs fs2 r2 f Os).
Proof.
  apply (mtyped_comb sigs Σ H
    (fun v t m O _ => forall b, msub m t b -> mtyped sigs Σ H v b m O)
    (fun vs t m Os _ => forall b, msub m t b -> mtypeds sigs Σ H vs b m Os)
    (fun kvs fs r f Os _ => forall fs2 r2,
       (forall k, In k (map fst kvs) ->
          frsubR (msub (f k)) (field_at k fs r) (field_at k fs2 r2) /\
          (partial (f k) = true -> field_at k fs2 r2 <> FOpen)) ->
       mfields sigs Σ H kvs fs2 r2 f Os)).
  - intros v t Hv b Hs. constructor. eapply vtyped_sub; eauto.
  - intros v t O D b Hs. constructor. eapply dtyped_rsub; eauto.
  - intros l vs a m Os E d IH N b Hs. simpl in Hs. destruct Hs as (a' & b' & Ea & -> & Hs).
    injection Ea as <-. apply mt_list with (vs := vs); auto.
  - intros l kvs fs r f Os E Nk Rq d IH N b Hs. simpl in Hs.
    destruct Hs as (fs1 & r1 & fs2 & r2 & Ea & -> & Hs). injection Ea as <- <-.
    apply mt_rec with (kvs := kvs); [exact E | exact Nk | | | exact N].
    + intros k t Ek. destruct (Hs k) as [F _]. rewrite Ek in F. inversion F; subst.
      eapply Rq; eauto.
    + apply IH. intros k _. apply Hs.
  - intros; constructor.
  - intros v vs t m O Os d IH d0 IH0 b Hs. constructor; auto.
  - intros; constructor.
  - intros k v kvs fs r f O Os d IH d0 IH0 fs2 r2 Hk. constructor.
    + destruct (Hk k (in_eq _ _)) as [F P].
      revert d IH P F. generalize (field_at k fs r) as f1. generalize (field_at k fs2 r2) as f2.
      intros f2 f1 d IH P F.
      destruct F as [a b R|b|b|f0 a b Ha R|f0 a b Ha R| |f0]; simpl in *.
      * apply IH; auto.
      * exfalso; eapply mtyped_bot; eauto.
      * exfalso; eapply mtyped_bot; eauto.
      * destruct Ha as [-> | [-> | ->]]; simpl in *; apply IH; auto.
      * destruct Ha as [-> | [-> | ->]]; simpl in *; apply IH; auto.
      * exfalso; eapply mtyped_bot; eauto.
      * destruct (f k) eqn:Fk.
        -- inversion d; subst. constructor. eapply vt_top; eauto.
        -- inversion d; subst. constructor. eapply dt_top; eauto.
        -- exfalso. apply P; reflexivity.
        -- exfalso. apply P; reflexivity.
    + apply IH0. intros k' Hk'. apply Hk. simpl. auto.
Qed.

Lemma mtyped_msub Σ H v a m O b : mtyped sigs Σ H v a m O -> msub m a b -> mtyped sigs Σ H v b m O.
Proof. intros M Hs. exact (proj1 (mtyped_msub_all Σ H) _ _ _ _ M _ Hs). Qed.

(** ** Committing a partly new value *)

Lemma mtyped_commit_all Σ H :
  (forall v t m O, mtyped sigs Σ H v t m O ->
     forall Σ0, keeps_live Σ Σ0 -> length Σ0 = length H -> dead_on Σ0 O ->
     exists Σ', length Σ' = length Σ0 /\ sagree Σ0 Σ' O /\ committed sigs Σ' H O /\
                vtyped sigs Σ' v t) /\
  (forall vs t m Os, mtypeds sigs Σ H vs t m Os ->
     forall Σ0, keeps_live Σ Σ0 -> length Σ0 = length H -> dead_on Σ0 (concat Os) -> NoDup (concat Os) ->
     exists Σ', length Σ' = length Σ0 /\ sagree Σ0 Σ' (concat Os) /\ committed sigs Σ' H (concat Os) /\
                Forall (fun v => vtyped sigs Σ' v t) vs) /\
  (forall kvs fs r f Os, mfields sigs Σ H kvs fs r f Os ->
     forall Σ0, keeps_live Σ Σ0 -> length Σ0 = length H -> dead_on Σ0 (concat Os) -> NoDup (concat Os) ->
     exists Σ', length Σ' = length Σ0 /\ sagree Σ0 Σ' (concat Os) /\ committed sigs Σ' H (concat Os) /\
                Forall (fun p => vtyped sigs Σ' (snd p) (fty (field_at (fst p) fs r))) kvs).
Proof.
  apply (mtyped_comb sigs Σ H
    (fun v t m O _ => forall Σ0, keeps_live Σ Σ0 -> length Σ0 = length H -> dead_on Σ0 O ->
       exists Σ', length Σ' = length Σ0 /\ sagree Σ0 Σ' O /\ committed sigs Σ' H O /\ vtyped sigs Σ' v t)
    (fun vs t m Os _ => forall Σ0, keeps_live Σ Σ0 -> length Σ0 = length H -> dead_on Σ0 (concat Os) ->
       NoDup (concat Os) ->
       exists Σ', length Σ' = length Σ0 /\ sagree Σ0 Σ' (concat Os) /\ committed sigs Σ' H (concat Os) /\
                  Forall (fun v => vtyped sigs Σ' v t) vs)
    (fun kvs fs r f Os _ => forall Σ0, keeps_live Σ Σ0 -> length Σ0 = length H -> dead_on Σ0 (concat Os) ->
       NoDup (concat Os) ->
       exists Σ', length Σ' = length Σ0 /\ sagree Σ0 Σ' (concat Os) /\ committed sigs Σ' H (concat Os) /\
                  Forall (fun p => vtyped sigs Σ' (snd p) (fty (field_at (fst p) fs r))) kvs)).
  - (* stored: nothing to commit *)
    intros v t Hv Σ0 K Ln D. exists Σ0. repeat split; auto using sagree_refl.
    + intros l [].
    + eapply vtyped_keep; eauto.
  - (* new *)
    intros v t O Dv Σ0 K Ln D.
    destruct (proj1 (dtyped_struct sigs Σ H) _ _ _ Dv) as (N & _ & _).
    eapply (proj1 (commit_all sigs Σ H)); eauto using keeps_live_scope, dead_on_nonscope.
  - (* list *)
    intros l vs a m Os E d IH N Σ0 K Ln D. inversion N as [|? ? Nl Nd]; subst.
    destruct (IH Σ0 K Ln) as (Σ1 & L1 & A1 & C1 & F1);
      [eapply dead_on_mono; [exact D | intros; apply in_cons; auto] | exact Nd |].
    assert (E1 : nth_error Σ1 l = Some HDead) by (apply A1; [apply D, in_eq | exact Nl]).
    assert (Hlt : l < length Σ1) by (eapply nth_error_lt; eauto).
    set (Σ2 := set_nth l (HList a) Σ1).
    assert (K2 : keeps_live Σ1 Σ2) by (eapply sagree_keeps_live; [apply set_agree | intros l0 [<-|[]]; exact E1]).
    exists Σ2. repeat split.
    + unfold Σ2. rewrite set_nth_length; auto.
    + eapply sagree_mono; [eapply sagree_trans; [exact A1 | apply set_agree] |].
      intros m0 Hm0. apply in_app_or in Hm0 as [Hm0|[<-|[]]]; [apply in_cons; auto | apply in_eq].
    + intros m0 [<-|Hm0].
      * exists (HList a), (OList vs). repeat split; auto.
        -- apply nth_error_set_nth_eq; auto.
        -- simpl. rewrite Forall_forall in *. intros x Hx. eapply vtyped_keep; eauto.
      * exact (committed_keep _ _ _ _ C1 K2 m0 Hm0).
    + eapply vt_list; [apply nth_error_set_nth_eq; auto | apply s_refl].
  - (* record *)
    intros l kvs fs r f Os E Nk Rq d IH N Σ0 K Ln D. inversion N as [|? ? Nl Nd]; subst.
    destruct (IH Σ0 K Ln) as (Σ1 & L1 & A1 & C1 & F1);
      [eapply dead_on_mono; [exact D | intros; apply in_cons; auto] | exact Nd |].
    assert (E1 : nth_error Σ1 l = Some HDead) by (apply A1; [apply D, in_eq | exact Nl]).
    assert (Hlt : l < length Σ1) by (eapply nth_error_lt; eauto).
    set (Σ2 := set_nth l (HRec fs r) Σ1).
    assert (K2 : keeps_live Σ1 Σ2) by (eapply sagree_keeps_live; [apply set_agree | intros l0 [<-|[]]; exact E1]).
    exists Σ2. repeat split.
    + unfold Σ2. rewrite set_nth_length; auto.
    + eapply sagree_mono; [eapply sagree_trans; [exact A1 | apply set_agree] |].
      intros m0 Hm0. apply in_app_or in Hm0 as [Hm0|[<-|[]]]; [apply in_cons; auto | apply in_eq].
    + intros m0 [<-|Hm0].
      * exists (HRec fs r), (ODict kvs). repeat split; auto.
        -- apply nth_error_set_nth_eq; auto.
        -- rewrite Forall_forall in *. intros x Hx. eapply vtyped_keep; eauto.
      * exact (committed_keep _ _ _ _ C1 K2 m0 Hm0).
    + eapply vt_rec; [apply nth_error_set_nth_eq; auto | apply s_refl].
  - intros t m Σ0 K Ln D N. exists Σ0. repeat split; auto using sagree_refl; try (intros l []); try constructor.
  - intros v vs t m O Os d IH d0 IH0 Σ0 K Ln D N. simpl in D, N.
    apply nodup_app_inv in N as (N1 & N2 & Dj).
    destruct (IH Σ0 K Ln) as (Σa & La & Aa & Ca & Fa); [eapply dead_on_mono; [exact D | intros; apply in_or_app; auto] |].
    assert (Ka : keeps_live Σ0 Σa) by (eapply sagree_keeps_live; [exact Aa | eapply dead_on_mono; [exact D | intros; apply in_or_app; auto]]).
    destruct (IH0 Σa (keeps_live_trans _ _ _ K Ka)) as (Σb & Lb & Ab & Cb & Fb); auto; try lia.
    { eapply dead_on_agree; [eapply dead_on_mono; [exact D | intros; apply in_or_app; auto] | exact Aa |].
      intros l Hl Hl'. exact (Dj l Hl' Hl). }
    assert (Kb : keeps_live Σa Σb).
    { eapply sagree_keeps_live; [exact Ab|]. eapply dead_on_agree; [eapply dead_on_mono; [exact D | intros; apply in_or_app; auto] | exact Aa |].
      intros l Hl Hl'. exact (Dj l Hl' Hl). }
    exists Σb. repeat split; auto.
    + lia.
    + eapply sagree_trans; eauto.
    + intros l Hl. apply in_app_or in Hl as [Hl|Hl]; [exact (committed_keep _ _ _ _ Ca Kb l Hl) | exact (Cb l Hl)].
    + constructor; [eapply vtyped_keep; [exact Fa | exact Kb] | exact Fb].
  - intros fs r f Σ0 K Ln D N. exists Σ0. repeat split; auto using sagree_refl; try (intros l []); try constructor.
  - intros k v kvs fs r f O Os d IH d0 IH0 Σ0 K Ln D N. simpl in D, N.
    apply nodup_app_inv in N as (N1 & N2 & Dj).
    destruct (IH Σ0 K Ln) as (Σa & La & Aa & Ca & Fa); [eapply dead_on_mono; [exact D | intros; apply in_or_app; auto] |].
    assert (Ka : keeps_live Σ0 Σa) by (eapply sagree_keeps_live; [exact Aa | eapply dead_on_mono; [exact D | intros; apply in_or_app; auto]]).
    destruct (IH0 Σa (keeps_live_trans _ _ _ K Ka)) as (Σb & Lb & Ab & Cb & Fb); auto; try lia.
    { eapply dead_on_agree; [eapply dead_on_mono; [exact D | intros; apply in_or_app; auto] | exact Aa |].
      intros l Hl Hl'. exact (Dj l Hl' Hl). }
    assert (Kb : keeps_live Σa Σb).
    { eapply sagree_keeps_live; [exact Ab|]. eapply dead_on_agree; [eapply dead_on_mono; [exact D | intros; apply in_or_app; auto] | exact Aa |].
      intros l Hl Hl'. exact (Dj l Hl' Hl). }
    exists Σb. repeat split; auto.
    + lia.
    + eapply sagree_trans; eauto.
    + intros l Hl. apply in_app_or in Hl as [Hl|Hl]; [exact (committed_keep _ _ _ _ Ca Kb l Hl) | exact (Cb l Hl)].
    + constructor; [simpl; eapply vtyped_keep; [exact Fa | exact Kb] | exact Fb].
Qed.

Lemma mtyped_commit Σ H v t m O Σ0 :
  mtyped sigs Σ H v t m O -> keeps_live Σ Σ0 -> length Σ0 = length H -> dead_on Σ0 O ->
  exists Σ', length Σ' = length Σ0 /\ sagree Σ0 Σ' O /\ committed sigs Σ' H O /\ vtyped sigs Σ' v t.
Proof. intros M. exact (proj1 (mtyped_commit_all Σ H) _ _ _ _ M Σ0). Qed.

(** ** Slots *)

Lemma slot_ok_mtyped Σ H v m t O : slot_ok sigs Σ H v (m, t) O <-> mtyped sigs Σ H v t m O.
Proof.
  unfold slot_ok. destruct m; simpl; split; intros Hs.
  - destruct Hs as [Hv ->]. constructor; auto.
  - inversion Hs; subst; auto.
  - constructor; auto.
  - inversion Hs; subst; auto.
  - exact Hs.
  - exact Hs.
  - exact Hs.
  - exact Hs.
Qed.

End Partial.
