(** * Committing a fresh value.

    A fresh value is deep-typed straight off the heap; its locations' store
    types are stale.  When it stops being fresh (it is dropped, duplicated,
    stored, passed to a definition, or put in a shared container), its
    region gets store types that agree with its deep type.  Because the
    region is a tree, every location receives exactly one type.  Nothing
    outside the region changes. *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Variance Typing Interp Invariant RtLemmas.

Lemma set_nth_length {A} n (x : A) l : length (set_nth n x l) = length l.
Proof. revert n; induction l; intros [|n]; simpl; auto. Qed.

Lemma nth_error_set_nth_eq {A} n (x : A) l : n < length l -> nth_error (set_nth n x l) n = Some x.
Proof. revert n; induction l; intros [|n]; simpl; intros; try lia; auto. apply IHl; lia. Qed.

Lemma nth_error_set_nth_neq {A} n m (x : A) l : m <> n -> nth_error (set_nth n x l) m = nth_error l m.
Proof. revert n m; induction l; intros [|n] [|m]; simpl; intros; auto; try lia. Qed.

Lemma nth_error_lt {A} (l : list A) n x : nth_error l n = Some x -> n < length l.
Proof. intros E. apply nth_error_Some. rewrite E; discriminate. Qed.

Definition nonscope_on (Σ : store_ty) (O : list loc) :=
  forall l, In l O -> exists h, nth_error Σ l = Some h /\ is_scope h = false.

Lemma sagree_scope_ext Σ Σ' X :
  sagree Σ Σ' X -> nonscope_on Σ X -> scope_ext Σ Σ'.
Proof.
  intros Ha Hn l G E. apply Ha; auto. intros Hin. destruct (Hn l Hin) as (h & E' & Hs).
  rewrite E in E'. inversion E'; subst. discriminate.
Qed.

Lemma sagree_refl Σ X : sagree Σ Σ X.
Proof. unfold sagree; auto. Qed.

Lemma sagree_trans a b c X Y :
  sagree a b X -> sagree b c Y -> sagree a c (X ++ Y).
Proof.
  intros H1 H2 l h E Hn. apply H2; [apply H1|]; auto; intro; apply Hn; apply in_or_app; auto.
Qed.

Lemma sagree_mono a b X Y : sagree a b X -> incl X Y -> sagree a b Y.
Proof. intros H Hi l h E Hn. apply H; auto. Qed.

Lemma set_agree (Σ : store_ty) l h : sagree Σ (set_nth l h Σ) [l].
Proof.
  intros m h' E Hm. rewrite nth_error_set_nth_neq; auto. intro; subst; apply Hm; apply in_eq.
Qed.

Lemma set_scope_ext (Σ : store_ty) l h h0 :
  nth_error Σ l = Some h0 -> is_scope h0 = false -> scope_ext Σ (set_nth l h Σ).
Proof.
  intros E N m G Em. rewrite nth_error_set_nth_neq; auto.
  intro; subst. rewrite E in Em. inversion Em; subst. discriminate.
Qed.

Lemma nodup_app_inv {A} (l1 l2 : list A) :
  NoDup (l1 ++ l2) -> NoDup l1 /\ NoDup l2 /\ (forall x, In x l1 -> ~ In x l2).
Proof.
  induction l1 as [|a l1 IH]; simpl; intros Hn.
  - repeat split; auto. constructor.
  - inversion Hn as [|? ? Hna Hnd]; subst. destruct (IH Hnd) as (N1 & N2 & D).
    repeat split; auto.
    + constructor; auto. intro; apply Hna; apply in_or_app; auto.
    + intros x [<-|Hx] Hx2; [apply Hna; apply in_or_app; auto | eapply D; eauto].
Qed.

Section Commit.
Variable sigs : genv.

Definition committed (Σ' : store_ty) (H : heap) (O : list loc) :=
  forall l, In l O -> exists h o, nth_error Σ' l = Some h /\ is_scope h = false /\
                          nth_error H l = Some o /\ obj_ok sigs Σ' o h.

Lemma committed_agree Σ Σ' H O X :
  committed Σ H O -> sagree Σ Σ' X ->
  (forall l, In l O -> ~ In l X) ->
  (forall l o, In l O -> nth_error H l = Some o -> forall r, In r (olocs o) -> ~ In r X) ->
  scope_ext Σ Σ' -> committed Σ' H O.
Proof.
  intros Hc Ha Hd Ho Hs l Hl. destruct (Hc l Hl) as (h & o & E & Ns & Eo & Ok).
  exists h, o; repeat split; auto.
  eapply obj_ok_agree; eauto.
Qed.

Lemma commit_all Σ H :
  (forall v t O, dtyped sigs Σ H v t O ->
     forall Σ0, scope_ext Σ Σ0 -> length Σ0 = length H -> nonscope_on Σ0 O -> NoDup O ->
     exists Σ', length Σ' = length Σ0 /\ sagree Σ0 Σ' O /\ committed Σ' H O /\
                vtyped sigs Σ' v t) /\
  (forall vs t Os, dtypeds sigs Σ H vs t Os ->
     forall Σ0, scope_ext Σ Σ0 -> length Σ0 = length H -> nonscope_on Σ0 (concat Os) -> NoDup (concat Os) ->
     exists Σ', length Σ' = length Σ0 /\ sagree Σ0 Σ' (concat Os) /\ committed Σ' H (concat Os) /\
                Forall (fun v => vtyped sigs Σ' v t) vs) /\
  (forall kvs fs r Os, dfields sigs Σ H kvs fs r Os ->
     forall Σ0, scope_ext Σ Σ0 -> length Σ0 = length H -> nonscope_on Σ0 (concat Os) -> NoDup (concat Os) ->
     exists Σ', length Σ' = length Σ0 /\ sagree Σ0 Σ' (concat Os) /\ committed Σ' H (concat Os) /\
                Forall (fun p => vtyped sigs Σ' (snd p) (fty (field_at (fst p) fs r))) kvs) /\
  (forall vs ts Os, dtypedl sigs Σ H vs ts Os ->
     forall Σ0, scope_ext Σ Σ0 -> length Σ0 = length H -> nonscope_on Σ0 (concat Os) -> NoDup (concat Os) ->
     exists Σ', length Σ' = length Σ0 /\ sagree Σ0 Σ' (concat Os) /\ committed Σ' H (concat Os) /\
                vtypedl sigs Σ' vs ts).
Proof.
  pose proof (dtyped_struct sigs Σ H) as [Sv [Svs [Sfs Sl]]].
  apply (dtyped_comb sigs Σ H
    (fun v t O _ => forall Σ0, scope_ext Σ Σ0 -> length Σ0 = length H -> nonscope_on Σ0 O -> NoDup O ->
     exists Σ', length Σ' = length Σ0 /\ sagree Σ0 Σ' O /\ committed Σ' H O /\ vtyped sigs Σ' v t)
    (fun vs t Os _ => forall Σ0, scope_ext Σ Σ0 -> length Σ0 = length H -> nonscope_on Σ0 (concat Os) -> NoDup (concat Os) ->
     exists Σ', length Σ' = length Σ0 /\ sagree Σ0 Σ' (concat Os) /\ committed Σ' H (concat Os) /\
                Forall (fun v => vtyped sigs Σ' v t) vs)
    (fun kvs fs r Os _ => forall Σ0, scope_ext Σ Σ0 -> length Σ0 = length H -> nonscope_on Σ0 (concat Os) -> NoDup (concat Os) ->
     exists Σ', length Σ' = length Σ0 /\ sagree Σ0 Σ' (concat Os) /\ committed Σ' H (concat Os) /\
                Forall (fun p => vtyped sigs Σ' (snd p) (fty (field_at (fst p) fs r))) kvs)
    (fun vs ts Os _ => forall Σ0, scope_ext Σ Σ0 -> length Σ0 = length H -> nonscope_on Σ0 (concat Os) -> NoDup (concat Os) ->
     exists Σ', length Σ' = length Σ0 /\ sagree Σ0 Σ' (concat Os) /\ committed Σ' H (concat Os) /\
                vtypedl sigs Σ' vs ts)).
  (* int, str, bool, none *)
  - intros n Σ0 Hs Hl Hn Hd. exists Σ0; repeat split; auto using sagree_refl; try constructor.
    intros l [].
  - intros n Σ0 Hs Hl Hn Hd. exists Σ0; repeat split; auto using sagree_refl; try constructor.
    intros l [].
  - intros n Σ0 Hs Hl Hn Hd. exists Σ0; repeat split; auto using sagree_refl; try constructor.
    intros l [].
  - intros t Σ0 Hs Hl Hn Hd. exists Σ0; repeat split; auto using sagree_refl; try constructor.
    intros l [].
  - (* just *)
    intros v t O d IH Σ0 Hs Hl Hn Hd. destruct (IH Σ0 Hs Hl Hn Hd) as (Σ' & ? & ? & ? & ?).
    exists Σ'; repeat split; auto. constructor; auto.
  - (* closure *)
    intros sc e ins outs Hv Σ0 Hs Hl Hn Hd. exists Σ0; repeat split; auto using sagree_refl.
    + intros l [].
    + eapply vtyped_clo_scope; eauto.
  - (* list *)
    intros l vs t Os e d IH n Σ0 Hs Hl Hn Hd.
    inversion n as [|? ? Hnl Hnd]; subst.
    destruct (IH Σ0 Hs Hl) as (Σ1 & L1 & A1 & C1 & F1);
      [intros m Hm; apply Hn; apply in_cons; auto | auto |].
    destruct (Hn l (in_eq _ _)) as (h0 & E0 & N0).
    assert (E1 : nth_error Σ1 l = Some h0) by (apply A1; auto).
    assert (Hlt : l < length Σ1) by (eapply nth_error_lt; eauto).
    pose proof (set_agree Σ1 l (HList t)) as A2.
    pose proof (set_scope_ext Σ1 l (HList t) h0 E1 N0) as S2.
    destruct (Svs _ _ _ d) as [Hv Ho].
    exists (set_nth l (HList t) Σ1). repeat split.
    + rewrite set_nth_length; auto.
    + eapply sagree_mono; [eapply sagree_trans; eauto |].
      intros m Hm. apply in_app_or in Hm as [Hm|[<-|[]]]; [apply in_cons; auto | apply in_eq].
    + intros m Hm. simpl in Hm. destruct Hm as [<-|Hm].
      * exists (HList t), (OList vs); repeat split; auto.
        -- apply nth_error_set_nth_eq; auto.
        -- simpl. rewrite Forall_forall in *. intros x Hx.
           eapply vtyped_agree with (X := [l]); eauto.
           intros m Hm [<-|[]]. apply Hnl. apply Hv. apply in_flat_map. eauto.
      * eapply committed_agree with (X := [l]); eauto.
        -- intros m' Hm' [<-|[]]. contradiction.
        -- intros m' o Hm' Eo r0 Hr [<-|[]]. apply Hnl.
           destruct (Ho m' Hm') as (o' & Eo' & _ & Hr').
           rewrite Eo in Eo'; inversion Eo'; subst. auto.
    + eapply vt_list; [ apply nth_error_set_nth_eq; auto | apply s_refl ].
  - (* record *)
    intros l kvs fs r Os e n n0 d IH n1 Σ0 Hs Hl Hn Hd.
    inversion n1 as [|? ? Hnl Hnd]; subst.
    destruct (IH Σ0 Hs Hl) as (Σ1 & L1 & A1 & C1 & F1);
      [intros m Hm; apply Hn; apply in_cons; auto | auto |].
    destruct (Hn l (in_eq _ _)) as (h0 & E0 & N0).
    assert (E1 : nth_error Σ1 l = Some h0) by (apply A1; auto).
    assert (Hlt : l < length Σ1) by (eapply nth_error_lt; eauto).
    pose proof (set_agree Σ1 l (HRec fs r)) as A2.
    pose proof (set_scope_ext Σ1 l (HRec fs r) h0 E1 N0) as S2.
    destruct (Sfs _ _ _ _ d) as [Hv Ho].
    exists (set_nth l (HRec fs r) Σ1). repeat split.
    + rewrite set_nth_length; auto.
    + eapply sagree_mono; [eapply sagree_trans; eauto |].
      intros m Hm. apply in_app_or in Hm as [Hm|[<-|[]]]; [apply in_cons; auto | apply in_eq].
    + intros m Hm. simpl in Hm. destruct Hm as [<-|Hm].
      * exists (HRec fs r), (ODict kvs); repeat split; auto.
        -- apply nth_error_set_nth_eq; auto.
        -- rewrite Forall_forall in *. intros x Hx.
           eapply vtyped_agree with (X := [l]); eauto.
           intros m Hm [<-|[]]. apply Hnl. apply Hv. apply in_flat_map. eauto.
      * eapply committed_agree with (X := [l]); eauto.
        -- intros m' Hm' [<-|[]]. contradiction.
        -- intros m' o Hm' Eo r0 Hr [<-|[]]. apply Hnl.
           destruct (Ho m' Hm') as (o' & Eo' & _ & Hr').
           rewrite Eo in Eo'; inversion Eo'; subst. auto.
    + eapply vt_rec; [ apply nth_error_set_nth_eq; auto | apply s_refl ].
  - intros v a b O d IH Σ0 Hs Hl Hn Hd. destruct (IH Σ0 Hs Hl Hn Hd) as (Σ' & ? & ? & ? & ?).
    exists Σ'; repeat split; auto. apply vt_unionl; auto.
  - intros v a b O d IH Σ0 Hs Hl Hn Hd. destruct (IH Σ0 Hs Hl Hn Hd) as (Σ' & ? & ? & ? & ?).
    exists Σ'; repeat split; auto. apply vt_unionr; auto.
  - intros v t O d IH Σ0 Hs Hl Hn Hd. destruct (IH Σ0 Hs Hl Hn Hd) as (Σ' & ? & ? & ? & ?).
    exists Σ'; repeat split; auto. eapply vt_top; eauto.
  - (* enum value: commit its payloads *)
    intros E c pts vs a Os Ec W d IH N Σ0 Hs Hl Hn Hd. destruct (IH Σ0 Hs Hl Hn Hd) as (Σ' & ? & ? & ? & ?).
    exists Σ'; repeat split; auto. eapply vt_con; eauto.
  - (* recursive type: commit the unfolding *)
    intros v t O M d IH Σ0 Hs Hl Hn Hd. destruct (IH Σ0 Hs Hl Hn Hd) as (Σ' & ? & ? & ? & ?).
    exists Σ'; repeat split; auto. apply vt_mu; auto.
  - intros t Σ0 Hs Hl Hn Hd. exists Σ0; repeat split; auto using sagree_refl.
    intros l [].
  - intros v vs t O Os d IH d0 IH0 Σ0 Hs Hl Hn Hd. simpl in Hn, Hd.
    apply nodup_app_inv in Hd as (Hd1 & Hd2 & Dj).
    destruct (IH Σ0 Hs Hl) as (Σa & La & Aa & Ca & Fa); auto.
    { intros m Hm; apply Hn; apply in_or_app; auto. }
    assert (Nb : nonscope_on Σa (concat Os)).
    { intros m Hm. destruct (Hn m) as (h & E & N); [apply in_or_app; auto|].
      exists h; split; auto. apply Aa; auto. intro Hx; exact (Dj m Hx Hm). }
    assert (Sa : scope_ext Σ0 Σa).
    { eapply sagree_scope_ext; eauto. intros m Hm; apply Hn; apply in_or_app; auto. }
    destruct (IH0 Σa (scope_ext_trans _ _ _ Hs Sa)) as (Σb & Lb & Ab & Cb & Fb); auto; try lia.
    assert (Sb : scope_ext Σa Σb) by (eapply sagree_scope_ext; eauto).
    destruct (Sv _ _ _ d) as (_ & Hv & Ho).
    exists Σb. repeat split; auto.
    + lia.
    + eapply sagree_trans; eauto.
    + intros m Hm. apply in_app_or in Hm as [Hm|Hm]; [|auto].
      assert (CbO : committed Σb H O).
      { apply committed_agree with (Σ := Σa) (X := concat Os).
        - exact Ca.
        - exact Ab.
        - intros m' Hm' Hm''. exact (Dj m' Hm' Hm'').
        - intros m' o Hm' Eo r0 Hr Hr2. destruct (Ho m' Hm') as (o' & Eo' & _ & Hr').
          rewrite Eo in Eo'; inversion Eo'; subst. exact (Dj r0 (Hr' r0 Hr) Hr2).
        - exact Sb. }
      exact (CbO m Hm).
    + constructor; [| exact Fb].
      eapply vtyped_agree with (X := concat Os); [exact Fa | | exact Ab | exact Sb].
      intros m Hm Hm2. exact (Dj m (Hv m Hm) Hm2).
  - intros fs r Σ0 Hs Hl Hn Hd. exists Σ0; repeat split; auto using sagree_refl.
    intros l [].
  - intros k v kvs fs r O Os d IH d0 IH0 Σ0 Hs Hl Hn Hd. simpl in Hn, Hd.
    apply nodup_app_inv in Hd as (Hd1 & Hd2 & Dj).
    destruct (IH Σ0 Hs Hl) as (Σa & La & Aa & Ca & Fa); auto.
    { intros m Hm; apply Hn; apply in_or_app; auto. }
    assert (Nb : nonscope_on Σa (concat Os)).
    { intros m Hm. destruct (Hn m) as (h & E & N); [apply in_or_app; auto|].
      exists h; split; auto. apply Aa; auto. intro Hx; exact (Dj m Hx Hm). }
    assert (Sa : scope_ext Σ0 Σa).
    { eapply sagree_scope_ext; eauto. intros m Hm; apply Hn; apply in_or_app; auto. }
    destruct (IH0 Σa (scope_ext_trans _ _ _ Hs Sa)) as (Σb & Lb & Ab & Cb & Fb); auto; try lia.
    assert (Sb : scope_ext Σa Σb) by (eapply sagree_scope_ext; eauto).
    destruct (Sv _ _ _ d) as (_ & Hv & Ho).
    exists Σb. repeat split; auto.
    + lia.
    + eapply sagree_trans; eauto.
    + intros m Hm. apply in_app_or in Hm as [Hm|Hm]; [|auto].
      assert (CbO : committed Σb H O).
      { apply committed_agree with (Σ := Σa) (X := concat Os).
        - exact Ca.
        - exact Ab.
        - intros m' Hm' Hm''. exact (Dj m' Hm' Hm'').
        - intros m' o Hm' Eo r0 Hr Hr2. destruct (Ho m' Hm') as (o' & Eo' & _ & Hr').
          rewrite Eo in Eo'; inversion Eo'; subst. exact (Dj r0 (Hr' r0 Hr) Hr2).
        - exact Sb. }
      exact (CbO m Hm).
    + constructor; [| exact Fb]. simpl.
      eapply vtyped_agree with (X := concat Os); [exact Fa | | exact Ab | exact Sb].
      intros m Hm Hm2. exact (Dj m (Hv m Hm) Hm2).
  - intros Σ0 Hs Hl Hn Hd. exists Σ0; repeat split; auto using sagree_refl; [intros l [] | constructor].
  - intros v vs t ts O Os d IH d0 IH0 Σ0 Hs Hl Hn Hd. simpl in Hn, Hd.
    apply nodup_app_inv in Hd as (Hd1 & Hd2 & Dj).
    destruct (IH Σ0 Hs Hl) as (Σa & La & Aa & Ca & Fa); auto.
    { intros m Hm; apply Hn; apply in_or_app; auto. }
    assert (Nb : nonscope_on Σa (concat Os)).
    { intros m Hm. destruct (Hn m) as (h & E & N); [apply in_or_app; auto|].
      exists h; split; auto. apply Aa; auto. intro Hx; exact (Dj m Hx Hm). }
    assert (Sa : scope_ext Σ0 Σa).
    { eapply sagree_scope_ext; eauto. intros m Hm; apply Hn; apply in_or_app; auto. }
    destruct (IH0 Σa (scope_ext_trans _ _ _ Hs Sa)) as (Σb & Lb & Ab & Cb & Fb); auto; try lia.
    assert (Sb : scope_ext Σa Σb) by (eapply sagree_scope_ext; eauto).
    destruct (Sv _ _ _ d) as (_ & Hv & Ho).
    exists Σb. repeat split; auto.
    + lia.
    + eapply sagree_trans; eauto.
    + intros m Hm. apply in_app_or in Hm as [Hm|Hm]; [|auto].
      assert (CbO : committed Σb H O).
      { apply committed_agree with (Σ := Σa) (X := concat Os).
        - exact Ca.
        - exact Ab.
        - intros m' Hm' Hm''. exact (Dj m' Hm' Hm'').
        - intros m' o Hm' Eo r0 Hr Hr2. destruct (Ho m' Hm') as (o' & Eo' & _ & Hr').
          rewrite Eo in Eo'; inversion Eo'; subst. exact (Dj r0 (Hr' r0 Hr) Hr2).
        - exact Sb. }
      exact (CbO m Hm).
    + constructor; [| exact Fb].
      eapply vtyped_agree with (X := concat Os); [exact Fa | | exact Ab | exact Sb].
      intros m Hm Hm2. exact (Dj m (Hv m Hm) Hm2).
Qed.


End Commit.
