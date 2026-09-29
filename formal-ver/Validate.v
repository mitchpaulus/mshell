(** * Validation ([tryAs]) and copying.

    - [validate_dtyped]: validating a fresh value in place retypes it (no
      store typing changes, since fresh values are deep-typed).
    - [validate_imm]: validation against a type with no lists or dicts needs
      no copy even for a shared value.
    - [copy_ok]: the per-path copy of a shared value made during validation
      is typed at the target, in new locations only. *)
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

Fixpoint map_st (g : heap -> val -> heap * val) (H0 : heap) (vs : list val) : heap * list val :=
  match vs with
  | [] => (H0, [])
  | x :: xs => let (H1, x') := g H0 x in let (H2, xs') := map_st g H1 xs in (H2, x' :: xs')
  end.

Fixpoint map_kv (g : string -> heap -> val -> heap * val) (H0 : heap)
         (kvs : list (string * val)) : heap * list (string * val) :=
  match kvs with
  | [] => (H0, [])
  | (k, x) :: rest =>
      let (H1, x') := g k H0 x in let (H2, rest') := map_kv g H1 rest in (H2, (k, x') :: rest')
  end.

Definition cfield (f : fstat) (H0 : heap) (x : val) : heap * val :=
  match f with
  | FReq t' | FOpt t' | FDict t' => copy H0 x t'
  | FAbs | FOpen => (H0, x)
  end.

Lemma copy_list_eq H l vs u : nth_error H l = Some (OList vs) ->
  copy H (VLoc l) (TList u) =
    let (H1, vs') := map_st (fun H0 x => copy H0 x u) H vs in
    (app H1 [OList vs'], VLoc (length H1)).
Proof.
  intros E. simpl. rewrite E.
  match goal with
  | |- context [ ?CL H vs ] =>
      assert (HC : forall H0 vs0, CL H0 vs0 = map_st (fun H0 x => copy H0 x u) H0 vs0);
      [ intros H0 vs0; revert H0; induction vs0 as [|x xs IH]; intros H0;
        [ reflexivity
        | simpl; destruct (copy H0 x u) as [H1 x']; rewrite IH; reflexivity ]
      | rewrite HC; reflexivity ]
  end.
Qed.

Lemma copy_rec_eq H l kvs fs r : nth_error H l = Some (ODict kvs) ->
  copy H (VLoc l) (TRec fs r) =
    let (H1, kvs') := map_kv (fun k H0 x => cfield (field_at k fs r) H0 x) H kvs in
    (app H1 [ODict kvs'], VLoc (length H1)).
Proof.
  intros E. simpl. rewrite E. unfold label in *.
  match goal with
  | |- context [ ?CD H kvs ] =>
      assert (HC : forall H0 kvs0, CD H0 kvs0 = map_kv (fun k H0 x => cfield (field_at k fs r) H0 x) H0 kvs0);
      [ intros H0 kvs0; revert H0; induction kvs0 as [|[k x] rest IH]; intros H0;
        [ reflexivity
        | simpl;
          match goal with
          | |- context [ ?CK fs k H0 x ] =>
              assert (HK : forall fs0, CK fs0 k H0 x = cfield (field_at k fs0 r) H0 x);
              [ induction fs0 as [|[k' f] rest' IHf];
                [ reflexivity
                | unfold field_at in *; simpl; destruct (String.eqb k k'); [reflexivity | exact IHf] ]
              | rewrite HK; destruct (cfield (field_at k fs r) H0 x) as [H1 x']; rewrite IH; reflexivity ]
          end ]
      | rewrite HC; reflexivity ]
  end.
Qed.

Lemma validate_list_eq H l vs u : nth_error H l = Some (OList vs) ->
  validate H (VLoc l) (TList u) = forallb (fun x => validate H x u) vs.
Proof. intros E. simpl. rewrite E. reflexivity. Qed.

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

Section Ext.
Variables H H' : heap.
Hypothesis Hag : forall l, l < length H -> nth_error H' l = nth_error H l.

Lemma nth_error_agree l o : nth_error H l = Some o -> nth_error H' l = Some o.
Proof. intros E. rewrite Hag; auto. eapply nth_error_Some. rewrite E; discriminate. Qed.

Lemma validate_ext_n : forall n u, size u < n -> forall v, validate H v u = true -> validate H' v u = true.
Proof.
  induction n as [|n IH]; intros u Hs v Hv; [lia|].
  destruct u; simpl in Hs.
  - exact Hv.
  - exact Hv.
  - exact Hv.
  - exact Hv.
  - reflexivity.
  - destruct v; simpl in Hv |- *; try discriminate; auto. apply IH with (u := u); auto; lia.
  - destruct v; simpl in Hv |- *; try discriminate.
    destruct (nth_error H l) as [[vs| |]|] eqn:E; try discriminate.
    rewrite (nth_error_agree _ _ E). rewrite forallb_forall in *.
    intros x Hx. apply IH with (u := u); auto; lia.
  - destruct v; try (simpl in Hv; discriminate).
    destruct (nth_error H l) as [[|kvs|]|] eqn:E;
      try (simpl in Hv; rewrite E in Hv; discriminate).
    pose proof (nth_error_agree _ _ E) as E'.
    rewrite (validate_rec_eq H l kvs fs r E) in Hv.
    rewrite (validate_rec_eq H' l kvs fs r E').
    assert (Hf : forall k ov, vfield H (field_at k fs r) ov = true -> vfield H' (field_at k fs r) ov = true).
    { intros k ov. pose proof (size_field_at k fs r) as Sz.
      destruct (field_at k fs r); simpl in *; destruct ov; auto;
        intros Hx; apply IH with (u := t); auto; lia. }
    apply andb_true_iff in Hv as [Hv Hr]. apply andb_true_iff in Hv as [Hk Hq].
    rewrite Hr, !andb_true_r. apply andb_true_iff; split.
    + rewrite forallb_forall in *. intros p Hp. apply Hf. apply Hk; auto.
    + rewrite forallb_forall in *. intros p Hp. specialize (Hq p Hp).
      destruct (lookup (fst p) kvs); auto.
  - apply orb_true_iff in Hv as [Hv|Hv]; apply orb_true_iff; [left|right];
      [apply IH with (u := u1) | apply IH with (u := u2)]; auto; lia.
  - discriminate.
Qed.

Lemma validate_ext v u : validate H v u = true -> validate H' v u = true.
Proof. apply validate_ext_n with (n := S (size u)). lia. Qed.
End Ext.


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

(** [Σ', H'] extend [Σ, H] with new, typed, non-scope objects that do not
    point into [R]. *)
Definition hext (Σ : store_ty) (H : heap) (Σ' : store_ty) (H' : heap) (R : list loc) : Prop :=
  length Σ' = length H' /\ length H <= length H' /\
  (forall l, l < length H -> nth_error H' l = nth_error H l) /\
  sagree Σ Σ' [] /\
  (forall l o, length H <= l -> nth_error H' l = Some o ->
     exists h, nth_error Σ' l = Some h /\ is_scope h = false /\ obj_ok sigs Σ' o h /\
               (forall r, In r (olocs o) -> ~ In r R)).

Lemma sagree_nil_scope Σ Σ' : sagree Σ Σ' [] -> scope_ext Σ Σ'.
Proof. intros Ha l G E. apply Ha; auto. Qed.

Lemma hext_refl Σ H R : length Σ = length H -> hext Σ H Σ H R.
Proof.
  intros L. repeat split; auto using sagree_refl.
  intros l o Hl E. apply nth_error_lt in E. lia.
Qed.

Lemma hext_trans Σ H Σ1 H1 Σ2 H2 R :
  hext Σ H Σ1 H1 R -> hext Σ1 H1 Σ2 H2 R -> hext Σ H Σ2 H2 R.
Proof.
  intros (L1 & Le1 & Ag1 & Sa1 & N1) (L2 & Le2 & Ag2 & Sa2 & N2).
  repeat split; auto; try lia.
  - intros l Hl. rewrite Ag2 by lia. auto.
  - eapply sagree_mono; [eapply sagree_trans; eauto | intros x Hx; exact Hx].
  - intros l o Hl E. destruct (Nat.lt_ge_cases l (length H1)) as [Hlt|Hge].
    + rewrite Ag2 in E by auto. destruct (N1 l o Hl E) as (h & Eh & Ns & Ok & Hr).
      exists h; repeat split; auto.
      eapply obj_ok_agree with (X := []); eauto using sagree_nil_scope.
    + apply N2; auto.
Qed.

Lemma hext_heap_ok Σ H Σ' H' R :
  hext Σ H Σ' H' R -> heap_ok_out Σ H R -> heap_ok_out Σ' H' R.
Proof.
  intros (L & Le & Ag & Sa & N) Hok l o E Hl.
  destruct (Nat.lt_ge_cases l (length H)) as [Hlt|Hge].
  - rewrite Ag in E by auto. destruct (Hok l o E Hl) as [(h & Eh & Ok) Hr].
    split; auto. exists h; split.
    + apply Sa; auto.
    + eapply obj_ok_agree with (X := []); eauto using sagree_nil_scope.
  - destruct (N l o Hge E) as (h & Eh & _ & Ok & Hr). split; eauto.
Qed.

Lemma nth_error_snoc {A} (l : list A) x : nth_error (l ++ [x]) (length l) = Some x.
Proof. rewrite nth_error_app2 by lia. rewrite Nat.sub_diag. reflexivity. Qed.

Lemma sagree_snoc (Σ : store_ty) h : sagree Σ (Σ ++ [h]) [].
Proof.
  intros l h' E _. rewrite nth_error_app1; auto. eapply nth_error_lt; eauto.
Qed.

Lemma hext_alloc Σ1 H1 R o h :
  length Σ1 = length H1 -> (forall r, In r (olocs o) -> ~ In r R) ->
  obj_ok sigs (Σ1 ++ [h]) o h -> is_scope h = false ->
  hext Σ1 H1 (Σ1 ++ [h]) (H1 ++ [o]) R.
Proof.
  intros L Hr Ok Ns. repeat split.
  - rewrite !length_app; simpl; lia.
  - rewrite length_app; lia.
  - intros l Hl. apply nth_error_app1; auto.
  - apply sagree_snoc.
  - intros l o' Hl E. assert (l = length H1).
    { apply nth_error_lt in E. rewrite length_app in E. simpl in E. lia. }
    subst l. rewrite nth_error_snoc in E. inversion E; subst.
    exists h; repeat split; auto. rewrite <- L. apply nth_error_snoc.
Qed.

Lemma vt_just_inv' Σ v t : vtyped sigs Σ v t -> forall x, v = VJust x -> exists t', vtyped sigs Σ x t'.
Proof. induction 1; intros x0 E; try discriminate; eauto. inversion E; subst; eauto. Qed.

Lemma vfield_ext H H' f ov :
  (forall l, l < length H -> nth_error H' l = nth_error H l) ->
  vfield H f ov = true -> vfield H' f ov = true.
Proof.
  intros Ag. destruct f, ov; simpl; auto; apply validate_ext; auto.
Qed.

Lemma map_kv_keys g H0 kvs H' kvs' : map_kv g H0 kvs = (H', kvs') -> map fst kvs' = map fst kvs.
Proof.
  revert H0 H' kvs'. induction kvs as [|[k x] rest IH]; simpl; intros H0 H' kvs' E.
  - inversion E; reflexivity.
  - destruct (g k H0 x) as [H1 x']. destruct (map_kv g H1 rest) as [H2 rest'] eqn:Em.
    inversion E; subst. simpl. f_equal. eapply IH; eauto.
Qed.

Section Maps.
Variable R : list loc.

Lemma map_st_ok (g : heap -> val -> heap * val) (u : ty) :
  (forall Σ0 H0 x H1 x', length Σ0 = length H0 -> heap_ok_out Σ0 H0 R -> (forall l, In l R -> l < length H0) ->
     (exists t0, vtyped sigs Σ0 x t0) -> (forall l, In l (vlocs x) -> ~ In l R) -> validate H0 x u = true ->
     g H0 x = (H1, x') ->
     exists Σ1, hext Σ0 H0 Σ1 H1 R /\ vtyped sigs Σ1 x' u /\ (forall l, In l (vlocs x') -> ~ In l R)) ->
  forall vs Σ0 H0 H' vs', length Σ0 = length H0 -> heap_ok_out Σ0 H0 R -> (forall l, In l R -> l < length H0) ->
  (forall x, In x vs -> (exists t0, vtyped sigs Σ0 x t0) /\ (forall l, In l (vlocs x) -> ~ In l R) /\
                        validate H0 x u = true) ->
  map_st g H0 vs = (H', vs') ->
  exists Σ', hext Σ0 H0 Σ' H' R /\ Forall (fun x => vtyped sigs Σ' x u) vs' /\
             (forall x, In x vs' -> forall l, In l (vlocs x) -> ~ In l R).
Proof.
  intros Hg vs. induction vs as [|a vs IH]; simpl; intros Σ0 H0 H' vs' L Hok HR Hx E.
  - inversion E; subst. exists Σ0; split; [apply hext_refl; auto | split; [constructor | intros x []]].
  - destruct (g H0 a) as [H1 x'] eqn:Eg. destruct (map_st g H1 vs) as [H2 xs'] eqn:Em.
    inversion E; subst.
    destruct (Hx a (in_eq _ _)) as (Ta & La & Va).
    destruct (Hg Σ0 H0 a H1 x' L Hok HR Ta La Va Eg) as (Σ1 & He1 & T1 & L1).
    pose proof He1 as (Ln1 & Le1 & Ag1 & Sa1 & _).
    destruct (IH Σ1 H1 H' xs') as (Σ2 & He2 & T2 & L2); auto.
    + eapply hext_heap_ok; eauto.
    + intros l Hl. specialize (HR l Hl). lia.
    + intros x Hin. destruct (Hx x (in_cons _ _ _ Hin)) as ((t0 & Tx) & Lx & Vx).
      repeat split; auto.
      * exists t0. eapply vtyped_ext; eauto using sagree_nil_scope.
      * eapply validate_ext; eauto.
    + pose proof He2 as (_ & _ & _ & Sa2 & _).
      exists Σ2; split; [|split].
      * eapply hext_trans; eauto.
      * constructor; auto. eapply vtyped_ext; eauto using sagree_nil_scope.
      * intros x [<-|Hin]; [exact L1 | exact (L2 x Hin)].
Qed.

Lemma map_kv_ok (g : string -> heap -> val -> heap * val) (F : string -> fstat) :
  (forall Σ0 H0 k x H1 x', length Σ0 = length H0 -> heap_ok_out Σ0 H0 R -> (forall l, In l R -> l < length H0) ->
     (exists t0, vtyped sigs Σ0 x t0) -> (forall l, In l (vlocs x) -> ~ In l R) ->
     vfield H0 (F k) (Some x) = true ->
     g k H0 x = (H1, x') ->
     exists Σ1, hext Σ0 H0 Σ1 H1 R /\ vtyped sigs Σ1 x' (fty (F k)) /\
                (forall l, In l (vlocs x') -> ~ In l R)) ->
  forall kvs Σ0 H0 H' kvs', length Σ0 = length H0 -> heap_ok_out Σ0 H0 R -> (forall l, In l R -> l < length H0) ->
  (forall k x, In (k, x) kvs -> (exists t0, vtyped sigs Σ0 x t0) /\ (forall l, In l (vlocs x) -> ~ In l R) /\
                        vfield H0 (F k) (Some x) = true) ->
  map_kv g H0 kvs = (H', kvs') ->
  exists Σ', hext Σ0 H0 Σ' H' R /\ Forall (fun p => vtyped sigs Σ' (snd p) (fty (F (fst p)))) kvs' /\
             (forall p, In p kvs' -> forall l, In l (vlocs (snd p)) -> ~ In l R).
Proof.
  intros Hg kvs. induction kvs as [|[k a] kvs IH]; simpl; intros Σ0 H0 H' kvs' L Hok HR Hx E.
  - inversion E; subst. exists Σ0; split; [apply hext_refl; auto | split; [constructor | intros x []]].
  - destruct (g k H0 a) as [H1 x'] eqn:Eg. destruct (map_kv g H1 kvs) as [H2 xs'] eqn:Em.
    inversion E; subst.
    destruct (Hx k a (in_eq _ _)) as (Ta & La & Va).
    destruct (Hg Σ0 H0 k a H1 x' L Hok HR Ta La Va Eg) as (Σ1 & He1 & T1 & L1).
    pose proof He1 as (Ln1 & Le1 & Ag1 & Sa1 & _).
    destruct (IH Σ1 H1 H' xs') as (Σ2 & He2 & T2 & L2); auto.
    + eapply hext_heap_ok; eauto.
    + intros l Hl. specialize (HR l Hl). lia.
    + intros k0 x Hin. destruct (Hx k0 x (in_cons _ _ _ Hin)) as ((t0 & Tx) & Lx & Vx).
      repeat split; auto.
      * exists t0. eapply vtyped_ext; eauto using sagree_nil_scope.
      * eapply vfield_ext; eauto.
    + pose proof He2 as (_ & _ & _ & Sa2 & _).
      exists Σ2; split; [|split].
      * eapply hext_trans; eauto.
      * constructor; auto. simpl. eapply vtyped_ext; eauto using sagree_nil_scope.
      * intros p [<-|Hin]; [exact L1 | exact (L2 p Hin)].
Qed.
End Maps.

Lemma copy_ok_n : forall n u, size u < n ->
  forall Σ H R v t H' v',
  length Σ = length H -> heap_ok_out Σ H R -> (forall l, In l R -> l < length H) ->
  vtyped sigs Σ v t -> (forall l, In l (vlocs v) -> ~ In l R) ->
  validate H v u = true -> copy H v u = (H', v') ->
  exists Σ', hext Σ H Σ' H' R /\ vtyped sigs Σ' v' u /\ (forall l, In l (vlocs v') -> ~ In l R).
Proof.
  induction n as [|n IH]; intros u Hs Σ H R v t H' v' L Hok HR Tv Lv Hv Hc; [lia|].
  destruct u; simpl in Hs.
  - destruct v; simpl in Hv; try discriminate. simpl in Hc. inversion Hc; subst.
    exists Σ; split; [apply hext_refl; auto | split; [constructor | intros ? []]].
  - destruct v; simpl in Hv; try discriminate. simpl in Hc. inversion Hc; subst.
    exists Σ; split; [apply hext_refl; auto | split; [constructor | intros ? []]].
  - destruct v; simpl in Hv; try discriminate. simpl in Hc. inversion Hc; subst.
    exists Σ; split; [apply hext_refl; auto | split; [constructor | intros ? []]].
  - simpl in Hv. discriminate.
  - simpl in Hc. inversion Hc; subst.
    exists Σ; split; [apply hext_refl; auto | split; [eapply vt_top; eauto | auto]].
  - destruct v; simpl in Hv; try discriminate.
    + simpl in Hc. inversion Hc; subst. exists Σ; split; [apply hext_refl; auto | split; [constructor | intros ? []]].
    + simpl in Hc. destruct (copy H v u) as [H1 x'] eqn:Ec. inversion Hc; subst.
      destruct (vt_just_inv' _ _ _ Tv v eq_refl) as (t' & Tx).
      destruct (IH u ltac:(lia) Σ H R v t' H' x' L Hok HR Tx Lv Hv Ec) as (Σ' & He & Tx' & Lx').
      exists Σ'; split; [exact He | split; [constructor; auto | exact Lx']].
  - destruct v; simpl in Hv; try discriminate.
    destruct (nth_error H l) as [[vs| |]|] eqn:E; try discriminate.
    rewrite (copy_list_eq H l vs u E) in Hc.
    destruct (map_st (fun H0 x => copy H0 x u) H vs) as [H1 vs'] eqn:Em. inversion Hc; subst.
    assert (Hl : ~ In l R) by (apply Lv; apply in_eq).
    destruct (Hok l _ E Hl) as [(h & Eh & Ok) Hr].
    destruct h as [a|fs r|G]; simpl in Ok; try contradiction.
    rewrite Forall_forall in Ok. rewrite forallb_forall in Hv.
    destruct (map_st_ok R (fun H0 x => copy H0 x u) u) with (vs := vs) (Σ0 := Σ) (H0 := H) (H' := H1) (vs' := vs')
      as (Σ1 & He1 & T1 & L1); auto.
    + intros Σ0 H0 x H2 x' L0 Hok0 HR0 (t0 & Tx) Lx Vx Ec.
      eapply (IH u ltac:(lia)); eauto.
    + intros x Hin. repeat split; eauto.
      intros l0 Hl0. apply Hr. simpl. apply in_flat_map. eauto.
    + pose proof He1 as (Ln1 & Le1 & _ & _ & _).
      exists (Σ1 ++ [HList u]). split; [|split].
      * eapply hext_trans; eauto. apply hext_alloc; auto.
        -- simpl. intros r0 Hr0. apply in_flat_map in Hr0 as (x & Hx & Hr0). eapply L1; eauto.
        -- simpl. rewrite Forall_forall in *. intros x Hx.
           eapply vtyped_ext; eauto using sagree_snoc, sagree_nil_scope.
      * eapply vt_list; [ rewrite <- Ln1; apply nth_error_snoc | apply s_refl ].
      * simpl. intros l0 [<-|[]] Hin. specialize (HR _ Hin). lia.
  - destruct v; try (simpl in Hv; discriminate).
    destruct (nth_error H l) as [[|kvs|]|] eqn:E;
      try (simpl in Hv; rewrite E in Hv; discriminate).
    destruct (validate_rec_true H l kvs fs r E Hv) as [Hk Hq].
    rewrite (copy_rec_eq H l kvs fs r E) in Hc.
    destruct (map_kv (fun k H0 x => cfield (field_at k fs r) H0 x) H kvs) as [H1 kvs'] eqn:Em.
    inversion Hc; subst.
    assert (Hl : ~ In l R) by (apply Lv; apply in_eq).
    destruct (Hok l _ E Hl) as [(h & Eh & Ok) Hr].
    destruct h as [a|fs0 r0|G]; simpl in Ok; try contradiction.
    destruct Ok as (Nk & Rq & Fe). rewrite Forall_forall in Fe.
    destruct (map_kv_ok R (fun k H0 x => cfield (field_at k fs r) H0 x) (fun k => field_at k fs r))
      with (kvs := kvs) (Σ0 := Σ) (H0 := H) (H' := H1) (kvs' := kvs')
      as (Σ1 & He1 & T1 & L1); auto.
    + intros Σ0 H0 k x H2 x' L0 Hok0 HR0 (t0 & Tx) Lx Vx Ec.
      pose proof (size_field_at k fs r) as Sz.
      destruct (field_at k fs r) as [t1|t1|t1| |]; simpl in Vx, Ec, Sz |- *; try discriminate.
      * eapply (IH t1 ltac:(lia)); eauto.
      * eapply (IH t1 ltac:(lia)); eauto.
      * eapply (IH t1 ltac:(lia)); eauto.
      * inversion Ec; subst. exists Σ0; split; [apply hext_refl; auto | split; [eapply vt_top; eauto | auto]].
    + intros k x Hin. repeat split; eauto.
      * eexists. apply (Fe (k, x) Hin).
      * intros l0 Hl0. apply Hr. simpl. apply in_flat_map. exists (k, x); split; auto.
    + pose proof He1 as (Ln1 & Le1 & _ & _ & _).
      pose proof (map_kv_keys _ _ _ _ _ Em) as Keys.
      exists (Σ1 ++ [HRec fs r]). split; [|split].
      * eapply hext_trans; eauto. apply hext_alloc; auto.
        -- simpl. intros r1 Hr1. apply in_flat_map in Hr1 as (p & Hp & Hr1). eapply L1; eauto.
        -- simpl. repeat split.
           ++ rewrite Keys; auto.
           ++ intros k t0 Hk0 Hn. apply lookup_none_iff in Hn. rewrite Keys in Hn.
              apply lookup_none_iff in Hn. eapply Hq; eauto.
           ++ rewrite Forall_forall in *. intros p Hp.
              eapply vtyped_ext; eauto using sagree_snoc, sagree_nil_scope.
      * eapply vt_rec; [ rewrite <- Ln1; apply nth_error_snoc | apply s_refl ].
      * simpl. intros l0 [<-|[]] Hin. specialize (HR _ Hin). lia.
  - simpl in Hc. simpl in Hv. destruct (validate H v u1) eqn:Va.
    + destruct (IH u1 ltac:(lia) Σ H R v t H' v' L Hok HR Tv Lv Va Hc) as (Σ' & He & T' & L').
      exists Σ'; split; [exact He | split; [apply vt_unionl; auto | exact L']].
    + simpl in Hv. destruct (IH u2 ltac:(lia) Σ H R v t H' v' L Hok HR Tv Lv Hv Hc) as (Σ' & He & T' & L').
      exists Σ'; split; [exact He | split; [apply vt_unionr; auto | exact L']].
  - simpl in Hv. discriminate.
Qed.

Lemma copy_ok Σ H R v t u H' v' :
  length Σ = length H -> heap_ok_out Σ H R -> (forall l, In l R -> l < length H) ->
  vtyped sigs Σ v t -> (forall l, In l (vlocs v) -> ~ In l R) ->
  validate H v u = true -> copy H v u = (H', v') ->
  exists Σ', hext Σ H Σ' H' R /\ vtyped sigs Σ' v' u /\ (forall l, In l (vlocs v') -> ~ In l R).
Proof. intros. eapply copy_ok_n with (n := S (size u)); eauto. Qed.

End V.
