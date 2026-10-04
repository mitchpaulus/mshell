(** * Building partly new values: [set] on a new dict and push on a new
    list, with values of any mark.

    A dict or list literal that holds stored values is built by these
    steps.  The container stays new; each position records its value's
    mark (Typing.v).  The value [set] overwrites, if any, is forgotten: its
    objects have no store type yet and nothing else references them, so it
    needs no commit. *)

From Stdlib Require Import String List Arith Bool Lia Permutation.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Typing Interp Invariant RtLemmas Commit Partial Validate InvOps RecOps.

Section PartialOps.
Variable sigs : genv.

Lemma mfields_remove Σ H kvs fs r f Os k :
  mfields sigs Σ H kvs fs r f Os -> NoDup (map fst kvs) ->
  exists Oold Os', mfields sigs Σ H (remove_key k kvs) fs r f Os' /\
    Permutation (concat Os) (Oold ++ concat Os').
Proof.
  induction 1 as [fs r f|k0 v kvs fs r f O Os M Mf IH]; intros N.
  - exists [], []. simpl. split; [constructor | apply Permutation_refl].
  - inversion N as [|? ? Nk Nd]; subst. simpl.
    destruct (String.eqb_spec k0 k) as [->|Hn]; simpl.
    + exists O, Os. rewrite remove_key_notin; auto using Permutation_refl.
    + destruct (IH Nd) as (Oold & Os' & Mf' & P).
      exists Oold, (O :: Os'). split.
      * constructor; auto.
      * simpl. rewrite P. apply Permutation_app_swap_app.
Qed.

(** Setting key [k] of a new dict to a value with mark [m]. *)
Lemma inv_rec_set_m Σ H sc G x l L m t f fs r st Ox Ol Os k :
  inv sigs Σ H sc G (x :: VLoc l :: L) ((m, t) :: (MRec f, TRec fs r) :: st) (Ox :: Ol :: Os) ->
  bounded H ->
  exists kvs O', nth_error H l = Some (ODict kvs) /\
    inv sigs Σ (set_nth l (ODict (dset k x kvs)) H) sc G (VLoc l :: L)
        ((mset k m (MRec f), TRec ((k, FReq t) :: fs) r) :: st) (O' :: Os) /\
    bounded (set_nth l (ODict (dset k x kvs)) H).
Proof.
  intros I B. pose proof (inv_slot_lt _ _ _ _ _ _ _ _ I) as Slt.
  destruct I as [Ilen Islots Idisj Iown Iheap Ireg Iscope].
  inversion Islots as [|? ? ? ? ? ? Px F1]; subst. inversion F1 as [|? ? ? ? ? ? Pl F2]; subst.
  apply slot_ok_mtyped in Px. apply slot_ok_mtyped in Pl.
  apply mt_rec_inv in Pl as (l' & kvs & Osl & El & E & NK & Rq & Mf & -> & N). inversion El; subst l'.
  destruct (mfields_remove _ _ _ _ _ _ _ k Mf NK) as (Oold & Osl' & Mf' & P).
  simpl in Idisj.
  assert (Nx : ~ In l Ox).
  { intro Hc. apply nodup_app_inv in Idisj as (_ & _ & D). apply (D l Hc). apply in_eq. }
  set (H' := set_nth l (ODict (dset k x kvs)) H).
  assert (Hlt : l < length H) by (eapply nth_error_lt; eauto).
  assert (Agr : forall m0, m0 <> l -> nth_error H' m0 = nth_error H m0).
  { intros m0 Hm0. apply nth_error_set_nth_neq; auto. }
  inversion N as [|? ? Nl Nd]; subst.
  assert (Hset : forall m0, In m0 (concat Osl) <-> In m0 Oold \/ In m0 (concat Osl')).
  { intros m0; split; intros Hm0.
    - apply (Permutation_in _ P) in Hm0. apply in_app_or; auto.
    - apply (Permutation_in _ (Permutation_sym P)). apply in_or_app; auto. }
  assert (Prest : forall m0, In m0 (concat Osl') -> In m0 (concat Osl)) by (intros; apply Hset; auto).
  (* the regions after the step are a part of the regions before it *)
  assert (Sub : forall m0, In m0 (concat ((l :: Ox ++ concat Osl') :: Os)) ->
                          In m0 (concat (Ox :: (l :: concat Osl) :: Os))).
  { intros m0 Hm0. pose proof (Prest m0). insolve. }
  assert (NDx : NoDup (Oold ++ (l :: Ox ++ concat Osl') ++ concat Os)).
  { eapply Permutation_NoDup; [| exact Idisj].
    apply Permutation_trans with (l' := Ox ++ (l :: Oold ++ concat Osl') ++ concat Os).
    - apply Permutation_app_head. simpl. apply perm_skip. apply Permutation_app_tail. exact P.
    - simpl. rewrite <- !app_assoc.
      apply Permutation_trans with (l' := (l :: Oold) ++ Ox ++ concat Osl' ++ concat Os).
      + change (l :: Oold ++ concat Osl' ++ concat Os) with ((l :: Oold) ++ concat Osl' ++ concat Os).
        apply perm_mid.
      + simpl. apply Permutation_middle. }
  pose proof (NoDup_app_remove_l _ _ NDx) as ND'.
  exists kvs, (l :: Ox ++ concat Osl'). split; auto. split.
  - constructor; simpl.
    + unfold H'; rewrite set_nth_length; auto.
    + constructor.
      * apply slot_ok_mtyped. change (l :: Ox ++ concat Osl') with (l :: concat (Ox :: Osl')).
        apply mt_rec with (kvs := dset k x kvs).
        -- apply nth_error_set_nth_eq; auto.
        -- unfold dset. simpl. constructor.
           ++ apply lookup_none_notin. apply lookup_remove_eq.
           ++ apply nodup_keys_remove; auto.
        -- intros k' t' Hk'. unfold dset. simpl. destruct (String.eqb_spec k' k) as [->|Hn]; [discriminate|].
           rewrite field_at_cons_neq in Hk' by auto. rewrite lookup_remove_neq by auto. eapply Rq; eauto.
        -- constructor.
           ++ rewrite field_at_cons_eq. simpl. unfold mset. rewrite String.eqb_refl.
              eapply mtyped_keep; [exact Px | apply keeps_live_refl |].
              intros m0 Hm0. apply Agr. intro; subst; contradiction.
           ++ eapply mfields_ext.
              ** eapply (proj2 (proj2 (mtyped_keep_all sigs Σ H Σ H' (keeps_live_refl Σ)))); [exact Mf' |].
                 intros m0 Hm0. apply Agr. intro; subst. apply Nl. apply Prest; auto.
              ** intros k' Hk'. apply in_keys_remove in Hk' as [_ Hn]. split.
                 --- rewrite field_at_cons_neq; auto.
                 --- unfold mset. destruct (String.eqb_spec k' k); [contradiction | reflexivity].
        -- exact (NoDup_app_remove_r _ _ ND').
      * eapply Forall3_impl_in; [| exact F2]. intros w p Ow _ HOw Hs.
        eapply slot_ok_keep; [exact Hs | apply keeps_live_refl |].
        intros m0 Hm0. apply Agr. intro; subst.
        apply nodup_app_inv in Idisj as (_ & N2 & _).
        inversion N2 as [|? ? Nl' _]; subst. apply Nl'. apply in_or_app. right. apply in_concat. eauto.
    + exact ND'.
    + constructor.
      * intros m0 [<-|[]] _. apply in_eq.
      * inversion Iown as [|? ? ? ? ? ? _ G1]; subst. inversion G1 as [|? ? ? ? ? ? _ G2]; subst.
        eapply Forall3_impl; [| exact G2]. intros w p0 Ow Hw m0 Hm0 Hc. apply Hw; [exact Hm0|].
        exact (Sub m0 Hc).
    + intros m0 om Em Hm Hlv.
      destruct (Nat.eq_dec m0 l) as [->|Hne]; [exfalso; apply Hm; left; auto|].
      rewrite Agr in Em; auto.
      destruct (in_dec Nat.eq_dec m0 Oold) as [HO|HO].
      * exfalso. apply Hlv. apply Ireg. pose proof (proj2 (Hset m0)). insolve.
      * destruct (Iheap m0 om Em) as [Ho Hr]; [| exact Hlv |].
        { intro Hc. apply Hm. pose proof (proj1 (Hset m0)). assert (l <> m0) by auto. insolve. }
        split; auto. intros r0 Hr' Hc. apply (Hr r0 Hr'). apply Sub. exact Hc.
    + intros m0 Hm0. apply Ireg. apply Sub. exact Hm0.
    + exact Iscope.
  - apply bounded_update; auto. intros r0 Hr. unfold dset in Hr. simpl in Hr.
    apply in_app_or in Hr as [Hr|Hr].
    + apply (Slt x (in_eq _ _)); auto.
    + eapply B; [exact E|]. simpl. apply in_flat_map in Hr as ([k0 y] & Hy & Hr).
      apply in_flat_map. exists (k0, y). split; auto.
      unfold remove_key in Hy. apply filter_In in Hy as [Hy _]. exact Hy.
Qed.

(** Appending a value with mark [m] to a new list of such values. *)
Lemma inv_list_push_m Σ H sc G x l L m t st Ox Ol Os :
  inv sigs Σ H sc G (x :: VLoc l :: L) ((m, t) :: (MList m, TList t) :: st) (Ox :: Ol :: Os) ->
  bounded H ->
  exists vs O', nth_error H l = Some (OList vs) /\
    inv sigs Σ (set_nth l (OList (vs ++ [x])) H) sc G (VLoc l :: L) ((MList m, TList t) :: st) (O' :: Os) /\
    bounded (set_nth l (OList (vs ++ [x])) H).
Proof.
  intros I B. pose proof (inv_slot_lt _ _ _ _ _ _ _ _ I) as Slt.
  destruct I as [Ilen Islots Idisj Iown Iheap Ireg Iscope].
  inversion Islots as [|? ? ? ? ? ? Px F1]; subst. inversion F1 as [|? ? ? ? ? ? Pl F2]; subst.
  apply slot_ok_mtyped in Px. apply slot_ok_mtyped in Pl.
  apply mt_list_inv in Pl as (l' & vs & Osl & El & E & Ms & -> & N). inversion El; subst l'.
  simpl in Idisj.
  assert (Nx : ~ In l Ox).
  { intro Hc. apply nodup_app_inv in Idisj as (_ & _ & D). apply (D l Hc). apply in_eq. }
  set (H' := set_nth l (OList (vs ++ [x])) H).
  assert (Hlt : l < length H) by (eapply nth_error_lt; eauto).
  assert (Agr : forall m0, m0 <> l -> nth_error H' m0 = nth_error H m0).
  { intros m0 Hm0. apply nth_error_set_nth_neq; auto. }
  inversion N as [|? ? Nl Nd]; subst.
  exists vs, (l :: concat (Osl ++ [Ox])). split; auto. split.
  - constructor; simpl.
    + unfold H'; rewrite set_nth_length; auto.
    + constructor.
      * apply slot_ok_mtyped. apply mt_list with (vs := vs ++ [x]).
        -- apply nth_error_set_nth_eq; auto.
        -- apply mtypeds_app.
           ++ eapply (proj1 (proj2 (mtyped_keep_all sigs Σ H Σ H' (keeps_live_refl Σ)))); [exact Ms |].
              intros m0 Hm0. apply Agr. intro; subst; contradiction.
           ++ constructor; [|constructor]. eapply mtyped_keep; [exact Px | apply keeps_live_refl |].
              intros m0 Hm0. apply Agr. intro; subst; contradiction.
        -- rewrite concat_app. simpl. rewrite app_nil_r. constructor.
           ++ intro Hc. apply in_app_or in Hc as [Hc|Hc]; [contradiction|]. apply Nx; auto.
           ++ apply nodup_app_inv in Idisj as (N1 & N2 & D).
              inversion N2 as [|? ? _ N3]; subst. apply nodup_app_inv in N3 as (N4 & _ & _).
              apply NoDup_app; auto.
              intros a Ha Ha'. apply (D a Ha'). apply in_cons. apply in_or_app. left. auto.
      * eapply Forall3_impl_in; [| exact F2]. intros w p Ow _ HOw Hs.
        eapply slot_ok_keep; [exact Hs | apply keeps_live_refl |].
        intros m0 Hm0. apply Agr. intro; subst.
        apply nodup_app_inv in Idisj as (_ & N2 & _).
        inversion N2 as [|? ? Nl' _]; subst. apply Nl'. apply in_or_app. right. apply in_concat. eauto.
    + rewrite concat_app. simpl. rewrite app_nil_r.
      eapply Permutation_NoDup; [| exact Idisj].
      simpl. rewrite <- app_assoc.
      change (l :: concat Osl ++ concat Os) with ((l :: concat Osl) ++ concat Os).
      change (l :: concat Osl ++ Ox ++ concat Os) with ((l :: concat Osl) ++ Ox ++ concat Os).
      apply perm_mid.
    + constructor.
      * intros m0 [<-|[]] _. apply in_eq.
      * inversion Iown as [|? ? ? ? ? ? _ G1]; subst. inversion G1 as [|? ? ? ? ? ? _ G2]; subst.
        eapply Forall3_impl; [| exact G2]. simpl. intros w _ Ow Hw m0 Hm0 Hc. apply Hw; auto.
        rewrite concat_app in Hc. simpl in Hc. rewrite app_nil_r in Hc.
        insolve.
    + intros m0 om Em Hm Hlv. rewrite concat_app in Hm. simpl in Hm. rewrite app_nil_r in Hm.
      destruct (Nat.eq_dec m0 l) as [->|Hne]; [exfalso; apply Hm; left; auto|].
      rewrite Agr in Em; auto.
      destruct (Iheap m0 om Em) as [Ho Hr]; [| exact Hlv |].
      { intro Hc. apply Hm. insolve. }
      split; auto. intros r Hr' Hc. apply (Hr r Hr'). rewrite concat_app in Hc. simpl in Hc.
      rewrite app_nil_r in Hc. insolve.
    + intros m0 Hm0. apply Ireg. rewrite concat_app in Hm0. simpl in Hm0. rewrite app_nil_r in Hm0.
      insolve.
    + exact Iscope.
  - apply bounded_update; auto. intros r Hr. simpl in Hr.
    rewrite flat_map_app in Hr. apply in_app_or in Hr as [Hr|Hr].
    + eapply B; eauto.
    + simpl in Hr. rewrite app_nil_r in Hr. apply (Slt x (in_eq _ _)); auto.
Qed.

End PartialOps.
