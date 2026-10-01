(** * Strong (type-changing) updates of fresh records. *)

From Stdlib Require Import String List Arith Bool Lia Permutation.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Typing Interp Invariant RtLemmas Commit Validate InvOps.

(** ** Association lists *)
Lemma lookup_remove_neq {A} k k' (kvs : list (string * A)) :
  k' <> k -> lookup k' (remove_key k kvs) = lookup k' kvs.
Proof.
  intros Hne. induction kvs as [|[k0 a] kvs IH]; simpl; auto.
  destruct (String.eqb_spec k0 k) as [->|Hn]; simpl.
  - destruct (String.eqb_spec k' k); [contradiction|]. auto.
  - destruct (String.eqb_spec k' k0); auto.
Qed.

Lemma lookup_remove_eq {A} k (kvs : list (string * A)) : lookup k (remove_key k kvs) = None.
Proof.
  induction kvs as [|[k0 a] kvs IH]; simpl; auto.
  destruct (String.eqb_spec k0 k) as [->|Hn]; simpl; auto.
  destruct (String.eqb_spec k k0); [subst; contradiction|]. auto.
Qed.

Lemma in_keys_remove {A} k k' (kvs : list (string * A)) :
  In k' (map fst (remove_key k kvs)) -> In k' (map fst kvs) /\ k' <> k.
Proof.
  induction kvs as [|[k0 a] kvs IH]; simpl; [tauto|].
  destruct (String.eqb_spec k0 k) as [->|Hn]; simpl.
  - intros H. destruct (IH H). split; auto.
  - intros [<-|H]; [split; auto|]. destruct (IH H); split; auto.
Qed.

Lemma nodup_keys_remove {A} k (kvs : list (string * A)) :
  NoDup (map fst kvs) -> NoDup (map fst (remove_key k kvs)).
Proof.
  induction kvs as [|[k0 a] kvs IH]; simpl; intros N; auto.
  inversion N; subst. destruct (String.eqb_spec k0 k); simpl; auto.
  constructor; auto. intro Hc. apply in_keys_remove in Hc as [Hc _]. contradiction.
Qed.

Lemma lookup_none_notin {A} k (kvs : list (string * A)) : lookup k kvs = None -> ~ In k (map fst kvs).
Proof.
  induction kvs as [|[k0 a] kvs IH]; simpl; auto.
  destruct (String.eqb_spec k k0); [discriminate|]. intros E [->|Hc]; [contradiction|]. apply IH; auto.
Qed.

Lemma remove_key_notin {A} k (kvs : list (string * A)) : ~ In k (map fst kvs) -> remove_key k kvs = kvs.
Proof.
  induction kvs as [|[k0 a] kvs IH]; simpl; auto. intros Hn.
  destruct (String.eqb_spec k0 k); [subst; exfalso; apply Hn; left; auto|].
  simpl. f_equal. apply IH. auto.
Qed.

Lemma remove_key_idem {A} k (kvs : list (string * A)) : remove_key k (remove_key k kvs) = remove_key k kvs.
Proof.
  apply remove_key_notin. intro Hc. apply in_keys_remove in Hc as [_ Hc]. contradiction.
Qed.

Lemma set_nth_set_nth {A} n (x y : A) l : set_nth n x (set_nth n y l) = set_nth n x l.
Proof. revert n; induction l; intros [|n]; simpl; auto. f_equal; auto. Qed.

Lemma field_at_cons_eq k f fs r : field_at k ((k, f) :: fs) r = f.
Proof. unfold field_at; simpl. rewrite String.eqb_refl. auto. Qed.

Lemma field_at_cons_neq k k' f fs r : k' <> k -> field_at k' ((k, f) :: fs) r = field_at k' fs r.
Proof. intros Hn. unfold field_at; simpl. destruct (String.eqb_spec k' k); [contradiction|auto]. Qed.

Lemma frsub_refl f : frsub f f.
Proof.
  destruct f.
  - apply frs_req. apply rs_sub, s_refl.
  - eapply frs_opt; [right; left; reflexivity | apply rs_sub, s_refl].
  - eapply frs_dict; [right; right; reflexivity | apply rs_sub, s_refl].
  - apply frs_abs.
  - apply frs_open.
Qed.

Section RecOps.
Variable sigs : genv.
Hypothesis Hmaybe : maybe_ok sigs.

Lemma dtyped_rec_same Σ H v fs1 r1 fs2 r2 O :
  (forall k, field_at k fs1 r1 = field_at k fs2 r2) ->
  dtyped sigs Σ H v (TRec fs1 r1) O -> dtyped sigs Σ H v (TRec fs2 r2) O.
Proof.
  intros E D. eapply dtyped_rsub; [| exact D]. apply rs_rec. intros k. rewrite E. apply frsub_refl.
Qed.

Lemma dfields_ext Σ H kvs fs1 r1 fs2 r2 Os :
  dfields sigs Σ H kvs fs1 r1 Os ->
  (forall k, In k (map fst kvs) -> field_at k fs1 r1 = field_at k fs2 r2) ->
  dfields sigs Σ H kvs fs2 r2 Os.
Proof.
  induction 1; intros E; constructor.
  - rewrite <- E; auto. simpl; auto.
  - apply IHdfields. intros; apply E; simpl; auto.
Qed.

Lemma dfields_remove Σ H kvs fs r Os k :
  dfields sigs Σ H kvs fs r Os -> NoDup (map fst kvs) ->
  exists Oold Os', dfields sigs Σ H (remove_key k kvs) fs r Os' /\
    Permutation (concat Os) (Oold ++ concat Os') /\
    match lookup k kvs with
    | Some old => dtyped sigs Σ H old (fty (field_at k fs r)) Oold
    | None => Oold = []
    end.
Proof.
  induction 1 as [fs r|k0 v kvs fs r O Os D Df IH]; intros N.
  - exists [], []. simpl. repeat split; auto. constructor.
  - inversion N as [|? ? Nk Nd]; subst. simpl.
    destruct (String.eqb_spec k0 k) as [->|Hn]; simpl.
    + rewrite String.eqb_refl. exists O, Os. repeat split; auto.
      rewrite remove_key_notin; auto.
    + destruct (String.eqb_spec k k0) as [->|_]; [contradiction|].
      destruct (IH Nd) as (Oold & Os' & Df' & P & Hl).
      exists Oold, (O :: Os'). repeat split; auto.
      * constructor; auto.
      * simpl. rewrite P. apply Permutation_app_swap_app.
Qed.


Definition opt_val (o : option val) : val := match o with Some v => vjust v | None => vnone end.

(** Removing key [k] from a fresh record: the removed value (if any) becomes
    its own fresh slot, and the record's type says [k] is absent. *)
Lemma inv_rec_remove Σ H sc G l L fs r st Ol Os k :
  inv sigs Σ H sc G (VLoc l :: L) ((Dp, TRec fs r) :: st) (Ol :: Os) ->
  bounded H ->
  exists kvs Oold Ol', nth_error H l = Some (ODict kvs) /\
    inv sigs Σ (set_nth l (ODict (remove_key k kvs)) H) sc G
        (opt_val (lookup k kvs) :: VLoc l :: L)
        ((Dp, TMaybe (fty (field_at k fs r))) :: (Dp, TRec ((k, FAbs) :: fs) r) :: st)
        (Oold :: Ol' :: Os) /\
    bounded (set_nth l (ODict (remove_key k kvs)) H).
Proof.
  intros I B.
  destruct I as [Ilen Islots Idisj Iown Iheap Ireg Iscope].
  inversion Islots as [|? ? ? ? ? ? Pl F1]; subst. unfold slot_ok in Pl; simpl in Pl.
  apply dt_rec_inv in Pl as (l' & kvs & Osl & El & E & NK & Rq & Df & -> & N). inversion El; subst l'.
  destruct (dfields_remove Σ H kvs fs r Osl k Df NK) as (Oold & Osl' & Df' & P & Hold).
  set (H' := set_nth l (ODict (remove_key k kvs)) H).
  assert (Hlt : l < length H) by (eapply nth_error_lt; eauto).
  assert (Agr : forall m, m <> l -> nth_error H' m = nth_error H m).
  { intros m Hm. apply nth_error_set_nth_neq; auto. }
  inversion N as [|? ? Nl Nd]; subst.
  assert (Pold : forall m, In m Oold -> In m (concat Osl)).
  { intros m Hm. apply (Permutation_in _ (Permutation_sym P)). apply in_or_app; auto. }
  assert (Prest : forall m, In m (concat Osl') -> In m (concat Osl)).
  { intros m Hm. apply (Permutation_in _ (Permutation_sym P)). apply in_or_app; auto. }
  assert (Hset : forall m, In m (concat Osl) <-> In m Oold \/ In m (concat Osl')).
  { intros m; split; intros Hm.
    - apply (Permutation_in _ P) in Hm. apply in_app_or; auto.
    - destruct Hm; auto. }
  simpl in Idisj.
  assert (Nrest : forall m, In m (concat Os) -> m <> l /\ ~ In m (concat Osl)).
  { intros m Hm. inversion Idisj as [|? ? Nl' Nd']; subst.
    apply nodup_app_inv in Nd' as (_ & _ & D). split.
    - intro; subst. apply Nl'. apply in_or_app; auto.
    - intro Hc. exact (D m Hc Hm). }
  exists kvs, Oold, (l :: concat Osl'). split; auto. split.
  - constructor; simpl.
    + unfold H'; rewrite set_nth_length; auto.
    + constructor; [|constructor].
      * unfold slot_ok; simpl. destruct (lookup k kvs) as [old|]; simpl.
        -- apply (dt_vjust sigs Hmaybe). eapply dtyped_agree1; eauto using scope_ext_refl.
           intros m Hm. apply Agr. intro; subst. apply Nl. apply Pold; auto.
        -- subst. apply (dt_vnone sigs Hmaybe).
      * unfold slot_ok; simpl. apply dt_rec with (kvs := remove_key k kvs).
        -- apply nth_error_set_nth_eq; auto.
        -- apply nodup_keys_remove; auto.
        -- intros k' t' Hk'. destruct (String.eqb_spec k' k) as [->|Hn].
           ++ rewrite field_at_cons_eq in Hk'. discriminate.
           ++ rewrite field_at_cons_neq in Hk' by auto. rewrite lookup_remove_neq by auto. eapply Rq; eauto.
        -- eapply dfields_ext.
           ++ eapply (proj2 (proj2 (dtyped_agree sigs Σ H Σ H' (scope_ext_refl Σ)))); eauto.
              intros m Hm. apply Agr. intro; subst. apply Nl. apply Prest; auto.
           ++ intros k' Hk'. apply in_keys_remove in Hk' as [_ Hn]. rewrite field_at_cons_neq; auto.
        -- constructor.
           ++ intro Hc. apply Nl. apply Prest; auto.
           ++ eapply NoDup_app_remove_l. eapply Permutation_NoDup; [exact P | exact Nd].
      * eapply Forall3_impl_in; [| exact F1]. intros w p Ow _ HOw Hs.
        unfold slot_ok in *. destruct p as [[|] t']; simpl in *; auto.
        eapply dtyped_agree1; eauto using scope_ext_refl.
        intros m Hm. apply Agr. intro; subst.
        apply (proj1 (Nrest l (proj2 (in_concat _ _) (ex_intro _ Ow (conj HOw Hm))))). auto.
    + (* disjointness *)
      eapply Permutation_NoDup; [| exact Idisj]. simpl.
      apply Permutation_trans with (l' := l :: (Oold ++ concat Osl') ++ concat Os).
      * apply perm_skip. apply Permutation_app_tail. exact P.
      * rewrite <- app_assoc. apply Permutation_middle.
    + constructor; [| constructor].
      * intros m Hm _. destruct (lookup k kvs) as [old|]; simpl in Hm; rewrite ?app_nil_r in Hm.
        -- destruct (proj1 (dtyped_struct sigs Σ H) _ _ _ Hold) as (_ & Hv & _). auto.
        -- contradiction.
      * intros m [<-|[]] _. apply in_eq.
      * inversion Iown as [|? ? ? ? ? ? _ G1]; subst.
        eapply Forall3_impl; [| exact G1]. simpl. intros w _ Ow Hw m Hm Hc. apply Hw; auto.
        simpl in *; repeat rewrite in_app_iff in *; rewrite ?Hset in *; insolve.
    + intros m om Em Hm Hlv. destruct (Nat.eq_dec m l) as [->|Hne]; [exfalso; apply Hm; simpl; insolve|].
      rewrite Agr in Em; auto.
      destruct (Iheap m om Em) as [Ho Hr]; [| exact Hlv |].
      { intro Hc. apply Hm. simpl in *; repeat rewrite in_app_iff in *; rewrite ?Hset in *; insolve. }
      split; auto. intros r0 Hr' Hc. apply (Hr r0 Hr'). simpl in *; repeat rewrite in_app_iff in *; rewrite ?Hset in *; insolve.
    + intros m Hm. apply Ireg. simpl in *; repeat rewrite in_app_iff in *; rewrite ?Hset in *; insolve.
    + exact Iscope.
  - apply bounded_update; auto. intros r0 Hr. simpl in Hr.
    eapply B; [exact E|]. simpl. apply in_flat_map in Hr as ([k0 x] & Hx & Hr).
    apply in_flat_map. exists (k0, x). split; auto.
    unfold remove_key in Hx. apply filter_In in Hx as [Hx _]. exact Hx.
Qed.

Lemma dfields_lookup Σ H kvs fs r Os k v :
  dfields sigs Σ H kvs fs r Os -> lookup k kvs = Some v ->
  exists O, dtyped sigs Σ H v (fty (field_at k fs r)) O.
Proof.
  induction 1 as [|k0 v0 kvs fs r O Os D Df IH]; simpl; [discriminate|].
  destruct (String.eqb_spec k k0) as [->|Hn]; intros E; [inversion E; subst; eauto | auto].
Qed.

(** Inserting a fresh value under an absent key of a fresh record. *)
Lemma inv_rec_insert Σ H sc G x l L t fs r st Ox Ol Os k :
  inv sigs Σ H sc G (x :: VLoc l :: L) ((Dp, t) :: (Dp, TRec fs r) :: st) (Ox :: Ol :: Os) ->
  bounded H -> field_at k fs r = FAbs ->
  exists kvs O', nth_error H l = Some (ODict kvs) /\ lookup k kvs = None /\
    inv sigs Σ (set_nth l (ODict ((k, x) :: kvs)) H) sc G (VLoc l :: L)
        ((Dp, TRec ((k, FReq t) :: fs) r) :: st) (O' :: Os) /\
    bounded (set_nth l (ODict ((k, x) :: kvs)) H).
Proof.
  intros I B Fk. pose proof (inv_slot_lt _ _ _ _ _ _ _ _ I) as Slt.
  destruct I as [Ilen Islots Idisj Iown Iheap Ireg Iscope].
  inversion Islots as [|? ? ? ? ? ? Px F1]; subst. inversion F1 as [|? ? ? ? ? ? Pl F2]; subst.
  unfold slot_ok in Px, Pl; simpl in Px, Pl.
  apply dt_rec_inv in Pl as (l' & kvs & Osl & El & E & NK & Rq & Df & -> & N). inversion El; subst l'.
  assert (Hk : lookup k kvs = None).
  { destruct (lookup k kvs) as [v|] eqn:Ek; auto. exfalso.
    destruct (dfields_lookup _ _ _ _ _ _ _ _ Df Ek) as (O & D). rewrite Fk in D. simpl in D.
    eapply dtyped_bot; eauto. }
  simpl in Idisj.
  assert (Nx : ~ In l Ox).
  { intro Hc. apply nodup_app_inv in Idisj as (_ & _ & D). apply (D l Hc). apply in_eq. }
  set (H' := set_nth l (ODict ((k, x) :: kvs)) H).
  assert (Hlt : l < length H) by (eapply nth_error_lt; eauto).
  assert (Agr : forall m, m <> l -> nth_error H' m = nth_error H m).
  { intros m Hm. apply nth_error_set_nth_neq; auto. }
  inversion N as [|? ? Nl Nd]; subst.
  exists kvs, (l :: concat (Ox :: Osl)). split; auto. split; auto. split.
  - constructor; simpl.
    + unfold H'; rewrite set_nth_length; auto.
    + constructor.
      * unfold slot_ok; simpl. change (l :: Ox ++ concat Osl) with (l :: concat (Ox :: Osl)).
        apply dt_rec with (kvs := (k, x) :: kvs).
        -- apply nth_error_set_nth_eq; auto.
        -- simpl. constructor; auto. apply lookup_none_notin; auto.
        -- intros k' t' Hk'. destruct (String.eqb_spec k' k) as [->|Hn].
           ++ simpl. rewrite String.eqb_refl. discriminate.
           ++ simpl. destruct (String.eqb_spec k' k); [contradiction|].
              rewrite field_at_cons_neq in Hk' by auto. eapply Rq; eauto.
        -- constructor.
           ++ rewrite field_at_cons_eq. simpl. eapply dtyped_agree1; eauto using scope_ext_refl.
              intros m Hm. apply Agr. intro; subst; contradiction.
           ++ eapply dfields_ext.
              ** eapply (proj2 (proj2 (dtyped_agree sigs Σ H Σ H' (scope_ext_refl Σ)))); eauto.
                 intros m Hm. apply Agr. intro; subst; contradiction.
              ** intros k' Hk'. rewrite field_at_cons_neq; auto.
                 intro; subst. apply (lookup_none_notin _ _ Hk). auto.
        -- simpl. constructor.
           ++ intro Hc. apply in_app_or in Hc as [Hc|Hc]; [contradiction|]. apply Nl; auto.
           ++ apply nodup_app_inv in Idisj as (N1 & N2 & D).
              inversion N2 as [|? ? _ N3]; subst. apply nodup_app_inv in N3 as (N4 & _ & _).
              apply NoDup_app; auto.
              intros a Ha Ha'. apply (D a Ha). apply in_cons. apply in_or_app. left. auto.
      * eapply Forall3_impl_in; [| exact F2]. intros w p Ow _ HOw Hs.
        unfold slot_ok in *. destruct p as [[|] t']; simpl in *; auto.
        eapply dtyped_agree1; eauto using scope_ext_refl.
        intros m Hm. apply Agr. intro; subst.
        apply nodup_app_inv in Idisj as (_ & N2 & _).
        inversion N2 as [|? ? Nl' _]; subst. apply Nl'. apply in_or_app. right. apply in_concat. eauto.
    + eapply Permutation_NoDup; [| exact Idisj].
      simpl. rewrite <- app_assoc.
      change (l :: concat Osl ++ concat Os) with ((l :: concat Osl) ++ concat Os).
      change (l :: Ox ++ concat Osl ++ concat Os) with ((l :: Ox) ++ concat Osl ++ concat Os).
      apply Permutation_trans with (l' := (l :: concat Osl) ++ Ox ++ concat Os); [apply perm_mid|].
      simpl. apply perm_skip. rewrite !app_assoc. apply Permutation_app_tail. apply Permutation_app_comm.
    + constructor.
      * intros m [<-|[]] _. apply in_eq.
      * inversion Iown as [|? ? ? ? ? ? _ G1]; subst. inversion G1 as [|? ? ? ? ? ? _ G2]; subst.
        eapply Forall3_impl; [| exact G2]. simpl. intros w _ Ow Hw m Hm Hc. apply Hw; auto.
        insolve.
    + intros m om Em Hm Hlv.
      destruct (Nat.eq_dec m l) as [->|Hne]; [exfalso; apply Hm; left; auto|].
      rewrite Agr in Em; auto.
      destruct (Iheap m om Em) as [Ho Hr]; [| exact Hlv |].
      { intro Hc. apply Hm. insolve. }
      split; auto. intros r0 Hr' Hc. apply (Hr r0 Hr'). insolve.
    + intros m Hm. apply Ireg. insolve.
    + exact Iscope.
  - apply bounded_update; auto. intros r0 Hr. simpl in Hr.
    apply in_app_or in Hr as [Hr|Hr].
    + apply (Slt x (in_eq _ _)); auto.
    + eapply B; [exact E|]. exact Hr.
Qed.
End RecOps.
