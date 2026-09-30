(** * The explicit copy ([copy]).

    [dcopy_fresh]: the per-path deep copy of a shared value is a fresh value
    of the same type.  Its lists and dicts form a tree made of new locations
    only, and every new location belongs to it, so the copy becomes a new
    region of the invariant (InvOps.v, [inv_alloc_region]).

    The copy must be per path, not memoized.  A memoizing copy of
    [{a: @xs, b: @xs}] gives one new list under two keys, which is not a
    tree, so its result could not be fresh. *)
From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Typing Interp Invariant RtLemmas Commit Validate InvOps.

Section Copy.
Variable sigs : genv.
Variable Σ : store_ty.
Variable H : heap.
Variable R : list loc.
Hypothesis Ln : length Σ = length H.
Hypothesis Hok : heap_ok_out sigs Σ H R.

(** [O] is exactly the locations allocated between [H0] and [H1]. *)
Definition new_region (H0 H1 : heap) (O : list loc) : Prop :=
  NoDup O /\ forall l, In l O <-> length H0 <= l < length H1.

(** The heap during a copy: the original heap with new objects appended. *)
Definition ext (H0 : heap) : Prop := exists N, H0 = H ++ N.

Definition val_spec (c : heap -> val -> option (heap * val)) : Prop :=
  forall v t H0 H1 v', vtyped sigs Σ v t -> (forall l, In l (vlocs v) -> ~ In l R) -> ext H0 ->
    c H0 v = Some (H1, v') ->
    exists N O, H1 = H0 ++ N /\ dtyped sigs Σ H1 v' t O /\ new_region H0 H1 O.

Definition hty (h : htype) : ty :=
  match h with HList a => TList a | HRec fs r => TRec fs r | HScope _ => TTop end.

Definition loc_spec (g : heap -> loc -> option (heap * val)) : Prop :=
  forall l h H0 H1 v', nth_error Σ l = Some h -> ~ In l R -> ext H0 ->
    g H0 l = Some (H1, v') ->
    exists N O, H1 = H0 ++ N /\ dtyped sigs Σ H1 v' (hty h) O /\ new_region H0 H1 O.

Lemma ext_app H0 N : ext H0 -> ext (H0 ++ N).
Proof. intros (N0 & ->). exists (N0 ++ N). symmetry; apply app_assoc. Qed.

Lemma ext_nth H0 l : ext H0 -> l < length H -> nth_error H0 l = nth_error H l.
Proof. intros (N0 & ->) Hl. apply nth_error_app1; auto. Qed.

Lemma new_region_nil H0 : new_region H0 H0 [].
Proof. split; [constructor | intros l; simpl; split; [tauto | lia]]. Qed.

Lemma new_region_app H0 H1 H2 N1 N2 O1 O2 :
  H1 = H0 ++ N1 -> H2 = H1 ++ N2 ->
  new_region H0 H1 O1 -> new_region H1 H2 O2 -> new_region H0 H2 (O1 ++ O2).
Proof.
  intros E1 E2 [N1' R1] [N2' R2].
  assert (L1 : length H0 <= length H1) by (subst; rewrite !length_app; lia).
  assert (L2 : length H1 <= length H2) by (subst; rewrite !length_app; lia).
  split.
  - apply NoDup_app; auto. intros l Ha Hb. apply R1 in Ha. apply R2 in Hb. lia.
  - intros l. rewrite in_app_iff, R1, R2. lia.
Qed.

(** A value deep-typed in a heap stays deep-typed when objects are appended. *)
Lemma dtyped_extend H1 N v t O :
  dtyped sigs Σ H1 v t O -> (forall l, In l O -> l < length H1) -> dtyped sigs Σ (H1 ++ N) v t O.
Proof.
  intros D Hl. eapply dtyped_agree1; [exact D | apply scope_ext_refl |].
  intros m Hm. apply nth_error_app1. auto.
Qed.

Lemma dtypeds_extend H1 N vs t Os :
  dtypeds sigs Σ H1 vs t Os -> (forall l, In l (concat Os) -> l < length H1) ->
  dtypeds sigs Σ (H1 ++ N) vs t Os.
Proof.
  intros D Hl. eapply (proj1 (proj2 (dtyped_agree sigs Σ H1 Σ (H1 ++ N) (scope_ext_refl Σ)))); [exact D|].
  intros m Hm. apply nth_error_app1. auto.
Qed.

Lemma dfields_extend H1 N kvs fs r Os :
  dfields sigs Σ H1 kvs fs r Os -> (forall l, In l (concat Os) -> l < length H1) ->
  dfields sigs Σ (H1 ++ N) kvs fs r Os.
Proof.
  intros D Hl. eapply (proj2 (proj2 (dtyped_agree sigs Σ H1 Σ (H1 ++ N) (scope_ext_refl Σ)))); [exact D|].
  intros m Hm. apply nth_error_app1. auto.
Qed.

Lemma region_lt H0 H1 O : new_region H0 H1 O -> forall l, In l O -> l < length H1.
Proof. intros [_ Rg] l Hl. apply Rg in Hl. lia. Qed.

(** ** Copying the elements of an object *)

Lemma mapo_ok c a : val_spec c ->
  forall vs H0 H2 vs', Forall (fun x => vtyped sigs Σ x a) vs ->
  (forall x, In x vs -> forall l, In l (vlocs x) -> ~ In l R) -> ext H0 ->
  mapo c H0 vs = Some (H2, vs') ->
  exists N Os, H2 = H0 ++ N /\ dtypeds sigs Σ H2 vs' a Os /\ new_region H0 H2 (concat Os).
Proof.
  intros Hc vs. induction vs as [|x xs IH]; simpl; intros H0 H2 vs' F Hl Ex E.
  - inversion E; subst. exists [], []. split; [rewrite app_nil_r; reflexivity|].
    split; [constructor | apply new_region_nil].
  - destruct (c H0 x) as [[H1 x']|] eqn:Ex1; [|discriminate].
    destruct (mapo c H1 xs) as [[H3 xs']|] eqn:Em; [|discriminate].
    injection E as <- <-. inversion F as [|? ? Fx Fr]; subst.
    destruct (Hc x a H0 H1 x' Fx) as (N1 & O1 & E1 & D1 & R1); auto.
    { intros l Hx. exact (Hl x (in_eq _ _) l Hx). }
    destruct (IH H1 H3 xs' Fr) as (N2 & Os2 & E2 & D2 & R2); auto.
    { intros y Hy. apply Hl. apply in_cons. auto. }
    { subst. apply ext_app. auto. }
    exists (N1 ++ N2), (O1 :: Os2). split; [subst; symmetry; apply app_assoc|]. split.
    + constructor; auto. subst H3. apply dtyped_extend; auto. eapply region_lt; eauto.
    + simpl. eapply new_region_app; eauto.
Qed.

Lemma mapo_kv_ok c fs r : val_spec c ->
  forall kvs H0 H2 kvs', Forall (fun p => vtyped sigs Σ (snd p) (fty (field_at (fst p) fs r))) kvs ->
  (forall p, In p kvs -> forall l, In l (vlocs (snd p)) -> ~ In l R) -> ext H0 ->
  mapo_kv c H0 kvs = Some (H2, kvs') ->
  exists N Os, H2 = H0 ++ N /\ map fst kvs' = map fst kvs /\
    dfields sigs Σ H2 kvs' fs r Os /\ new_region H0 H2 (concat Os).
Proof.
  intros Hc kvs. induction kvs as [|[k x] rest IH]; simpl; intros H0 H2 kvs' F Hl Ex E.
  - inversion E; subst. exists [], []. split; [rewrite app_nil_r; reflexivity|].
    split; [reflexivity | split; [constructor | apply new_region_nil]].
  - destruct (c H0 x) as [[H1 x']|] eqn:Ex1; [|discriminate].
    destruct (mapo_kv c H1 rest) as [[H3 rest']|] eqn:Em; [|discriminate].
    injection E as <- <-. inversion F as [|? ? Fx Fr]; subst.
    destruct (Hc x (fty (field_at k fs r)) H0 H1 x' Fx) as (N1 & O1 & E1 & D1 & R1); auto.
    { intros l Hx. exact (Hl (k, x) (in_eq _ _) l Hx). }
    destruct (IH H1 H3 rest' Fr) as (N2 & Os2 & E2 & K2 & D2 & R2); auto.
    { intros p Hp. apply Hl. apply in_cons. auto. }
    { subst. apply ext_app. auto. }
    exists (N1 ++ N2), (O1 :: Os2). split; [subst; symmetry; apply app_assoc|].
    split; [simpl; f_equal; exact K2|]. split.
    + constructor; auto. subst H3. apply dtyped_extend; auto. eapply region_lt; eauto.
    + simpl. eapply new_region_app; eauto.
Qed.

(** ** Copying one object, and a whole value *)

Lemma ocopy_ok c : val_spec c -> loc_spec (ocopy c).
Proof.
  intros Hc l h H0 H1 v' Eh Hl Ex Eo.
  assert (Hlt : l < length H) by (rewrite <- Ln; eapply nth_error_lt; eauto).
  destruct (nth_error H l) as [o|] eqn:E0; [|apply nth_error_None in E0; lia].
  destruct (Hok l o E0 Hl) as [(h' & Eh' & Ok) Hr]. rewrite Eh in Eh'. inversion Eh'; subst h'.
  unfold ocopy in Eo. rewrite (ext_nth H0 l Ex Hlt), E0 in Eo.
  assert (Hin : forall x, In x (match o with OList vs => vs | _ => [] end) ->
                forall r, In r (vlocs x) -> ~ In r R).
  { intros x Hx r Hr'. apply Hr. destruct o as [vs|kvs|kvs]; simpl in Hx; try contradiction.
    simpl. apply in_flat_map. eauto. }
  destruct o as [vs|kvs|kvs]; [| | discriminate].
  - destruct h as [a|fs r|G]; simpl in Ok; try contradiction.
    destruct (mapo c H0 vs) as [[H2 vs']|] eqn:Em; [|discriminate]. inversion Eo; subst.
    destruct (mapo_ok c a Hc vs H0 H2 vs' Ok Hin Ex Em) as (N & Os & E2 & D & [Nd Rg]).
    exists (N ++ [OList vs']), (length H2 :: concat Os). split; [subst; symmetry; apply app_assoc|].
    assert (Nin : ~ In (length H2) (concat Os)) by (intro Hc'; apply Rg in Hc'; lia).
    split.
    + simpl. apply dt_list with (vs := vs').
      * apply nth_error_app_eq.
      * apply dtypeds_extend; auto. intros m Hm. apply Rg in Hm. lia.
      * constructor; auto.
    + split; [constructor; auto|]. intros m. simpl. rewrite Rg, length_app. simpl.
      assert (length H0 <= length H2) by (subst; rewrite !length_app; lia). lia.
  - destruct h as [a|fs r|G]; simpl in Ok; try contradiction.
    destruct Ok as (Nk & Rq & Fe).
    destruct (mapo_kv c H0 kvs) as [[H2 kvs']|] eqn:Em; [|discriminate]. inversion Eo; subst.
    destruct (mapo_kv_ok c fs r Hc kvs H0 H2 kvs' Fe) as (N & Os & E2 & K & D & [Nd Rg]); auto.
    { intros p Hp x Hx. apply Hr. simpl. apply in_flat_map. eauto. }
    exists (N ++ [ODict kvs']), (length H2 :: concat Os). split; [subst; symmetry; apply app_assoc|].
    assert (Nin : ~ In (length H2) (concat Os)) by (intro Hc'; apply Rg in Hc'; lia).
    split.
    + simpl. apply dt_rec with (kvs := kvs').
      * apply nth_error_app_eq.
      * rewrite K. exact Nk.
      * intros k t0 Hk Hn. apply lookup_none_iff in Hn. rewrite K in Hn.
        apply lookup_none_iff in Hn. exact (Rq k t0 Hk Hn).
      * apply dfields_extend; auto. intros m Hm. apply Rg in Hm. lia.
      * constructor; auto.
    + split; [constructor; auto|]. intros m. simpl. rewrite Rg, length_app. simpl.
      assert (length H0 <= length H2) by (subst; rewrite !length_app; lia). lia.
Qed.

Lemma vcopy_ok g : loc_spec g -> val_spec (vcopy g).
Proof.
  intros Hg.
  assert (K :
    (forall v t, vtyped sigs Σ v t -> forall H0 H1 v', (forall l, In l (vlocs v) -> ~ In l R) -> ext H0 ->
       vcopy g H0 v = Some (H1, v') ->
       exists N O, H1 = H0 ++ N /\ dtyped sigs Σ H1 v' t O /\ new_region H0 H1 O) /\
    (forall vs ts, vtypedl sigs Σ vs ts -> forall H0 H2 vs', (forall l, In l (flat_map vlocs vs) -> ~ In l R) ->
       ext H0 -> mapo (vcopy g) H0 vs = Some (H2, vs') ->
       exists N Os, H2 = H0 ++ N /\ dtypedl sigs Σ H2 vs' ts Os /\ new_region H0 H2 (concat Os))).
  { apply (vtyped_comb sigs Σ
      (fun v t _ => forall H0 H1 v', (forall l, In l (vlocs v) -> ~ In l R) -> ext H0 ->
         vcopy g H0 v = Some (H1, v') ->
         exists N O, H1 = H0 ++ N /\ dtyped sigs Σ H1 v' t O /\ new_region H0 H1 O)
      (fun vs ts _ => forall H0 H2 vs', (forall l, In l (flat_map vlocs vs) -> ~ In l R) ->
         ext H0 -> mapo (vcopy g) H0 vs = Some (H2, vs') ->
         exists N Os, H2 = H0 ++ N /\ dtypedl sigs Σ H2 vs' ts Os /\ new_region H0 H2 (concat Os)));
      simpl;
      try (intros; match goal with Ec : Some _ = Some _ |- _ => injection Ec as <- <- end; exists [], [];
           split; [rewrite app_nil_r; reflexivity | split; [constructor | apply new_region_nil]]; fail).
    - intros v t Hv IH Ha Hc v2 Hl Ex Ec.
      destruct (vcopy g Ha v) as [[Hc' x']|] eqn:E; [|discriminate]. injection Ec as <- <-.
      destruct (IH Ha Hc' x' Hl Ex E) as (N & O & E1 & D & Rg).
      exists N, O. split; [exact E1 | split; [constructor; exact D | exact Rg]].
    - intros l a t Ea Sa Ha Hb v2 Hl Ex Ec.
      destruct (Hg l (HList a) Ha Hb v2 Ea (Hl l (or_introl eq_refl)) Ex Ec) as (N & O & E1 & D & Rg).
      exists N, O. split; [exact E1 | split; [eapply dtyped_sub; eauto | exact Rg]].
    - intros l fs r t Er Sr Ha Hb v2 Hl Ex Ec.
      destruct (Hg l (HRec fs r) Ha Hb v2 Er (Hl l (or_introl eq_refl)) Ex Ec) as (N & O & E1 & D & Rg).
      exists N, O. split; [exact E1 | split; [eapply dtyped_sub; eauto | exact Rg]].
    - intros sc e G ins outs Esc Cl Ha Hb v2 Hl Ex Ec.
      injection Ec as <- <-. exists [], [].
      split; [rewrite app_nil_r; reflexivity | split; [| apply new_region_nil]].
      apply dt_clo. eapply vt_clo; eauto.
    - intros v a b Hv IH Ha Hb v2 Hl Ex Ec.
      destruct (IH Ha Hb v2 Hl Ex Ec) as (N & O & E1 & D & Rg).
      exists N, O. split; [exact E1 | split; [apply dt_unionl; exact D | exact Rg]].
    - intros v a b Hv IH Ha Hb v2 Hl Ex Ec.
      destruct (IH Ha Hb v2 Hl Ex Ec) as (N & O & E1 & D & Rg).
      exists N, O. split; [exact E1 | split; [apply dt_unionr; exact D | exact Rg]].
    - intros v t Hv IH Ha Hb v2 Hl Ex Ec.
      destruct (IH Ha Hb v2 Hl Ex Ec) as (N & O & E1 & D & Rg).
      exists N, O. split; [exact E1 | split; [eapply dt_top; exact D | exact Rg]].
    - (* an enum value: copy its payloads *)
      intros E c pts vs a Ec W Hvs IH Ha Hb v2 Hl Ex Ecp.
      destruct (mapo (vcopy g) Ha vs) as [[H1 vs']|] eqn:Em; [|discriminate]. injection Ecp as <- <-.
      destruct (IH Ha H1 vs' Hl Ex Em) as (N & Os & E1 & D & Rg).
      exists N, (concat Os). split; [exact E1 | split; [| exact Rg]].
      eapply dt_con; eauto. destruct Rg; auto.
    - intros v vs t ts Hv IH Hvs IHs H0 H2 vs' Hl Ex Em.
      destruct (vcopy g H0 v) as [[H1 x']|] eqn:Ex1; [|discriminate].
      destruct (mapo (vcopy g) H1 vs) as [[H3 xs']|] eqn:Em'; [|discriminate].
      injection Em as <- <-.
      destruct (IH H0 H1 x') as (N1 & O1 & E1 & D1 & R1); auto.
      { intros l Hx. apply Hl. apply in_or_app; auto. }
      destruct (IHs H1 H3 xs') as (N2 & Os2 & E2 & D2 & R2); auto.
      { intros l Hx. apply Hl. apply in_or_app; auto. }
      { subst. apply ext_app. auto. }
      exists (N1 ++ N2), (O1 :: Os2). split; [subst; symmetry; apply app_assoc|]. split.
      + constructor; auto. subst H3. apply dtyped_extend; auto. eapply region_lt; eauto.
      + simpl. eapply new_region_app; eauto. }
  intros v t H0 H1 v' Hv Hl Ex Ec. eapply (proj1 K); eauto.
Qed.

Lemma loc_spec_none : loc_spec (fun _ _ => None).
Proof. intros l h H0 H1 v' _ _ _ E. discriminate. Qed.

Lemma dcopy_ok : forall f, val_spec (dcopy f).
Proof.
  induction f as [|f IH].
  - exact (vcopy_ok _ loc_spec_none).
  - exact (vcopy_ok _ (ocopy_ok _ IH)).
Qed.

End Copy.

(** The form used by the soundness proof: copying a shared value gives a
    value of the same type whose region is exactly the new locations. *)
Lemma dcopy_fresh sigs Σ H R f v t H' v' :
  length Σ = length H -> heap_ok_out sigs Σ H R ->
  vtyped sigs Σ v t -> (forall l, In l (vlocs v) -> ~ In l R) ->
  dcopy f H v = Some (H', v') ->
  exists N O, H' = H ++ N /\ dtyped sigs Σ H' v' t O /\
    (forall l, In l O <-> length H <= l < length H + length N).
Proof.
  intros Ln Hok V Hl E.
  destruct (dcopy_ok sigs Σ H R Ln Hok f v t H H' v' V Hl) as (N & O & E1 & D & [_ Rg]); auto.
  - exists []. rewrite app_nil_r. reflexivity.
  - exists N, O. split; [exact E1 | split; [exact D|]]. intros l. rewrite Rg, E1, length_app. lia.
Qed.
