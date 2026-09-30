(** * Variance of enum parameters.

    The declaration checks in Subtyping.v ([occ_sub], [occ_fresh], [en_imm])
    are what make the enum rules sound:

    - [payload_sub]: if [E[a] <= E[b]] by the parameters' variances, every
      payload type satisfies [subst a t <= subst b t].  So a shared enum
      value typed at [E[a]] can be seen at [E[b]].
    - [payload_rsub]: if a fresh [E[a]] is retyped to [E[b]] ([rsub]), every
      payload satisfies [rsub (subst a t) (subst b t)].  A parameter under a
      quote is never retyped this way, because quotes are not data.
    - [payload_imm]: an immutable enum type has immutable payload types.

    These hold for recursive enums with no extra argument: an occurrence
    of an enum inside a payload is compared by that enum's variances, not by
    unfolding it.  Nothing here needs the recursive references to use the
    same parameters. *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping.

(** The relation a polarity asks of the two sides of a substitution. *)
Definition prel (p : pol) (x y : ty) : Prop :=
  match p with PPos => sub x y | PNeg => sub y x | PInv => teq x y end.

Lemma prel_refl p x : prel p x x.
Proof. destruct p; simpl; auto using s_refl, teq_refl. Qed.

Lemma prel_inv p x y : teq x y -> prel p x y.
Proof. intros [H1 H2]; destruct p; simpl; auto. split; auto. Qed.

Lemma compat_vrel p v x y : compat p v = true -> vrel v x y -> prel p x y.
Proof.
  destruct p, v; simpl; intros C R; try discriminate; auto;
    try (apply prel_inv; exact R); try (destruct R; auto).
Qed.

Lemma prel_comp_pos v x y : prel (comp PPos v) x y = vrel v x y.
Proof. destruct v; reflexivity. Qed.

(** ** Lists *)

Lemma forallb2_Forall2 {A B} (h : A -> B -> bool) l1 l2 :
  forallb2 h l1 l2 = true -> Forall2 (fun a b => h a b = true) l1 l2.
Proof.
  revert l1; induction l2 as [|y l2 IH]; intros [|x l1]; simpl; intros E;
    try discriminate; constructor.
  - apply andb_true_iff in E as [E _]; auto.
  - apply andb_true_iff in E as [_ E]; auto.
Qed.

Lemma Forall2_in_r {A B} (P : A -> B -> Prop) l1 l2 :
  Forall2 P l1 l2 -> Forall2 (fun a b => In b l2 /\ P a b) l1 l2.
Proof.
  induction 1; constructor.
  - split; [left; reflexivity | assumption].
  - eapply Forall2_impl; [| exact IHForall2]. intros a b [Hb Hp]. split; [right|]; auto.
Qed.

Lemma vsubs_map qs args (f g : ty -> ty) :
  Forall2 (fun q x => vrel (p_var q) (f x) (g x)) qs args -> vsubs qs (map f args) (map g args).
Proof.
  induction 1 as [|q x qs args R _ IH]; simpl; [constructor|].
  destruct (p_var q) eqn:Ev; simpl in R.
  - apply vs_co; auto.
  - apply vs_contra; auto.
  - destruct R. apply vs_inv; auto.
Qed.

Lemma vrsubs_map qs args (f g : ty -> ty) :
  Forall2 (fun q x => (p_fresh q = true /\ rsub (f x) (g x)) \/
                      (p_fresh q = false /\ vrel (p_var q) (f x) (g x))) qs args ->
  vrsubs qs (map f args) (map g args).
Proof.
  induction 1 as [|q x qs args R _ IH]; simpl; [constructor|].
  destruct R as [[F R]|[F R]]; [apply vrs_fresh | apply vrs_sub]; auto.
Qed.

Lemma subs_map_in l (f g : ty -> ty) :
  (forall x, In x l -> sub (f x) (g x)) -> subs (map f l) (map g l).
Proof.
  induction l as [|x l IH]; simpl; intros H; constructor.
  - apply H; left; reflexivity.
  - apply IH; intros; apply H; right; assumption.
Qed.

Lemma Forall2_map_in {A B} (R : B -> B -> Prop) (f g : A -> B) l :
  (forall x, In x l -> R (f x) (g x)) -> Forall2 R (map f l) (map g l).
Proof.
  induction l as [|x l IH]; simpl; intros H; constructor.
  - apply H; left; reflexivity.
  - apply IH; intros; apply H; right; assumption.
Qed.

Lemma forallb_in {A} (h : A -> bool) l x : forallb h l = true -> In x l -> h x = true.
Proof. rewrite forallb_forall; auto. Qed.

(** ** Substitution and labels *)

Lemma field_at_subst a k fs r :
  field_at k (map (fun p => (fst p, fsubst a (snd p))) fs) (fsubst a r) = fsubst a (field_at k fs r).
Proof.
  unfold field_at. induction fs as [|[k' f] fs IH]; simpl; auto.
  destruct (String.eqb k k'); auto.
Qed.

Lemma field_at_in_or k fs r : In (field_at k fs r) (map snd fs) \/ field_at k fs r = r.
Proof.
  unfold field_at. destruct (lookup k fs) eqn:E; auto. left.
  apply lookup_in in E. apply in_map_iff. exists (k, f); auto.
Qed.

Lemma focc_sub_field ps k fs r :
  forallb (fun kf => focc_sub ps (snd kf)) fs && focc_sub ps r = true ->
  focc_sub ps (field_at k fs r) = true.
Proof.
  intros E. apply andb_true_iff in E as [E1 E2].
  destruct (field_at_in_or k fs r) as [Hin| ->]; auto.
  apply in_map_iff in Hin as (kf & <- & Hin). exact (forallb_in _ _ _ E1 Hin).
Qed.

Lemma focc_fresh_field ps k fs r :
  forallb (fun kf => focc_fresh ps (snd kf)) fs && focc_fresh ps r = true ->
  focc_fresh ps (field_at k fs r) = true.
Proof.
  intros E. apply andb_true_iff in E as [E1 E2].
  destruct (field_at_in_or k fs r) as [Hin| ->]; auto.
  apply in_map_iff in Hin as (kf & <- & Hin). exact (forallb_in _ _ _ E1 Hin).
Qed.

Lemma pocc_field i k fs r :
  In i (fpocc (field_at k fs r)) -> In i (pocc (TRec fs r)).
Proof.
  intros Hi. simpl. apply in_or_app.
  destruct (field_at_in_or k fs r) as [Hin|E].
  - left. apply in_map_iff in Hin as (kf & E & Hin). apply in_flat_map. exists kf. rewrite E. auto.
  - right. rewrite <- E. exact Hi.
Qed.

Lemma pocc_fty i f : In i (pocc (fty f)) -> In i (fpocc f).
Proof. destruct f; simpl; tauto. Qed.

(** ** Monotonicity of substitution *)

Section Mono.
Variable ps : list eparam.
Variables a b : list ty.

Definition rel_on (t : ty) : Prop :=
  forall i q, In i (pocc t) -> nth_error ps i = Some q -> vrel (p_var q) (nth i a TBot) (nth i b TBot).

Lemma rel_on_sub t t' : (forall i, In i (pocc t') -> In i (pocc t)) -> rel_on t -> rel_on t'.
Proof. intros Hs R i q Hi Hq. apply R; auto. Qed.

Lemma mono_s : forall n t p, size t < n -> occ_sub ps p t = true -> rel_on t ->
  prel p (subst a t) (subst b t).
Proof.
  induction n as [|n IH]; intros t p Hs Ho R; [lia|].
  destruct t as [| | | | |t'|t'|fs r|x y|ins outs|E args|i|v]; simpl in Hs, Ho |- *;
    try apply prel_refl.
  - (* Maybe *)
    assert (IHt : prel p (subst a t') (subst b t')) by (apply IH; auto; lia).
    destruct p; simpl in *; [apply s_maybe; auto | apply s_maybe; auto |].
    destruct IHt; split; apply s_maybe; auto.
  - (* List: invariant *)
    assert (IHt : prel PInv (subst a t') (subst b t')) by (apply IH; auto; lia).
    destruct IHt as [H1 H2]. apply prel_inv. split; apply s_list; auto.
  - (* Record: every field type invariant *)
    assert (Hk : forall k, teq (fty (fsubst a (field_at k fs r))) (fty (fsubst b (field_at k fs r)))).
    { intros k. pose proof (focc_sub_field ps k fs r Ho) as Hf.
      pose proof (size_field_at k fs r) as Sz.
      destruct (field_at k fs r) eqn:Ef; simpl in Hf |- *; try apply teq_refl.
      all: simpl in Sz; apply (IH _ PInv); [lia | exact Hf |].
      all: apply (rel_on_sub (TRec fs r)); [intros i Hi; apply (pocc_field i k); rewrite Ef; exact Hi | exact R]. }
    apply prel_inv. split; apply s_rec; intros k; rewrite !field_at_subst; specialize (Hk k);
      destruct (field_at k fs r); simpl in Hk |- *; destruct Hk;
      first [ apply fs_req | apply fs_opt | apply fs_dict | apply fs_abs | apply fs_open ]; auto.
  - (* Union *)
    apply andb_true_iff in Ho as [Hx Hy].
    assert (Ix : prel p (subst a x) (subst b x)).
    { apply IH; auto; [lia|]. apply (rel_on_sub (TUnion x y)); auto. intros i Hi; simpl; apply in_or_app; auto. }
    assert (Iy : prel p (subst a y) (subst b y)).
    { apply IH; auto; [lia|]. apply (rel_on_sub (TUnion x y)); auto. intros i Hi; simpl; apply in_or_app; auto. }
    destruct p; simpl in *.
    + apply s_unionl; [apply s_unionr1 | apply s_unionr2]; auto.
    + apply s_unionl; [apply s_unionr1 | apply s_unionr2]; auto.
    + destruct Ix, Iy. split; (apply s_unionl; [apply s_unionr1 | apply s_unionr2]; auto).
  - (* Quote: contravariant inputs, covariant outputs *)
    apply andb_true_iff in Ho as [Hi Ho].
    assert (Iin : forall x, In x ins -> prel (flip p) (subst a x) (subst b x)).
    { intros x Hx. apply IH; [pose proof (size_quote_in x ins outs Hx) as Q; simpl in Q; lia | exact (forallb_in _ _ _ Hi Hx) |].
      apply (rel_on_sub (TQuote ins outs)); auto. intros j Hj; simpl; apply in_or_app; left.
      apply in_flat_map; eauto. }
    assert (Iout : forall o x, outs = Some o -> In x o -> prel p (subst a x) (subst b x)).
    { intros o x -> Hx. simpl in Ho. apply IH; [pose proof (size_quote_out x ins o Hx) as Q; simpl in Q; lia | exact (forallb_in _ _ _ Ho Hx) |].
      apply (rel_on_sub (TQuote ins (Some o))); auto. intros j Hj; simpl; apply in_or_app; right.
      apply in_flat_map; eauto. }
    destruct p; simpl in *.
    + apply s_quote.
      * apply subs_map_in. intros x Hx. exact (Iin x Hx).
      * destruct outs as [o|]; constructor. apply subs_map_in. intros x Hx. exact (Iout o x eq_refl Hx).
    + apply s_quote.
      * apply subs_map_in. intros x Hx. exact (Iin x Hx).
      * destruct outs as [o|]; constructor. apply subs_map_in. intros x Hx. exact (Iout o x eq_refl Hx).
    + split; apply s_quote.
      * apply subs_map_in. intros x Hx. apply (Iin x Hx).
      * destruct outs as [o|]; constructor. apply subs_map_in. intros x Hx. apply (Iout o x eq_refl Hx).
      * apply subs_map_in. intros x Hx. apply (Iin x Hx).
      * destruct outs as [o|]; constructor. apply subs_map_in. intros x Hx. apply (Iout o x eq_refl Hx).
  - (* Enum: each argument by its parameter's variance *)
    apply forallb2_Forall2, Forall2_in_r in Ho.
    assert (F : Forall2 (fun q x => prel (comp p (p_var q)) (subst a x) (subst b x)) (en_params E) args).
    { eapply Forall2_impl; [| exact Ho]. intros q x [Hx Hq]. apply IH; auto.
      - pose proof (size_enum_arg x E args Hx) as Q; simpl in Q; lia.
      - apply (rel_on_sub (TEnum E args)); auto. intros j Hj; simpl. apply in_flat_map; eauto. }
    destruct p.
    + apply s_enum, vsubs_map. eapply Forall2_impl; [| exact F]. intros q x Hq.
      rewrite <- prel_comp_pos. exact Hq.
    + apply s_enum, vsubs_map. eapply Forall2_impl; [| exact F]. intros q x Hq.
      simpl in Hq. destruct (p_var q); simpl in Hq |- *; auto. destruct Hq; split; auto.
    + assert (G : Forall2 (fun q x => teq (subst a x) (subst b x)) (en_params E) args).
      { eapply Forall2_impl; [| exact F]. intros q x Hq. simpl in Hq. destruct (p_var q); exact Hq. }
      split; apply s_enum, vsubs_map; eapply Forall2_impl; try exact G;
        intros q x [H1 H2]; destruct (p_var q); simpl; auto; split; auto.
  - (* Parameter *)
    destruct (nth_error ps i) as [q|] eqn:Eq; [|discriminate].
    eapply compat_vrel; [exact Ho |]. apply (R i q); auto. left; reflexivity.
Qed.

Lemma mono_s_pos t : occ_sub ps PPos t = true -> rel_on t -> sub (subst a t) (subst b t).
Proof. intros. exact (mono_s (S (size t)) t PPos ltac:(lia) H H0). Qed.

(** The fresh retyping. *)
Hypothesis Hr : vrsubs ps a b.

Lemma vrsubs_nth : forall ps' a' b', vrsubs ps' a' b' -> forall i q, nth_error ps' i = Some q ->
  (p_fresh q = true /\ rsub (nth i a' TBot) (nth i b' TBot)) \/
  (p_fresh q = false /\ vrel (p_var q) (nth i a' TBot) (nth i b' TBot)).
Proof.
  induction 1; intros [|i] q0 E; simpl in E; try discriminate.
  - inversion E; subst. left; auto.
  - apply IHvrsubs; auto.
  - inversion E; subst. right; auto.
  - apply IHvrsubs; auto.
Qed.

Lemma no_fresh_rel t : no_fresh ps t = true -> rel_on t.
Proof.
  intros Nf i q Hi Hq. unfold no_fresh in Nf. pose proof (forallb_in _ _ _ Nf Hi) as E. simpl in E.
  rewrite Hq in E. apply negb_true_iff in E.
  destruct (vrsubs_nth _ _ _ Hr i q Hq) as [[F _]|[_ R]]; [congruence | exact R].
Qed.

Lemma mono_r : forall n t, size t < n -> occ_fresh ps t = true -> rsub (subst a t) (subst b t).
Proof.
  induction n as [|n IH]; intros t Hs Ho; [lia|].
  destruct t as [| | | | |t'|t'|fs r|x y|ins outs|E args|i|v]; simpl in Hs, Ho |- *;
    try (apply rs_sub, s_refl).
  - apply rs_maybe, IH; auto; lia.
  - apply rs_list, IH; auto; lia.
  - apply rs_rec. intros k. rewrite !field_at_subst.
    pose proof (focc_fresh_field ps k fs r Ho) as Hf. pose proof (size_field_at k fs r) as Sz.
    destruct (field_at k fs r) eqn:Ef; simpl in Hf, Sz |- *.
    + apply frs_req, IH; auto; lia.
    + apply frs_opt with (a := subst a t); [right; left; reflexivity | apply IH; auto; lia].
    + apply frs_dict with (a := subst a t); [right; right; reflexivity | apply IH; auto; lia].
    + apply frs_abs.
    + apply frs_open.
  - apply andb_true_iff in Ho as [Hx Hy].
    apply rs_unionl; [apply rs_unionr1 | apply rs_unionr2]; apply IH; auto; lia.
  - apply andb_true_iff in Ho as [Nf Ho]. apply rs_sub.
    exact (mono_s_pos (TQuote ins outs) Ho (no_fresh_rel _ Nf)).
  - apply rs_enum, vrsubs_map. apply forallb2_Forall2, Forall2_in_r in Ho.
    eapply Forall2_impl; [| exact Ho]. intros q x [Hx Hq].
    destruct (p_fresh q) eqn:F.
    + left. split; auto. apply IH; auto. pose proof (size_enum_arg x E args Hx) as Q; simpl in Q; lia.
    + right. split; auto. apply andb_true_iff in Hq as [Nf Hq].
      rewrite <- prel_comp_pos. apply (mono_s (S (size x))); auto. apply no_fresh_rel; auto.
  - destruct (nth_error ps i) as [q|] eqn:Eq; [|discriminate].
    destruct (vrsubs_nth _ _ _ Hr i q Eq) as [[F R]|[F R]]; auto.
    rewrite F in Ho. simpl in Ho. apply rs_sub.
    destruct (p_var q); simpl in Ho, R; try discriminate; auto. destruct R; auto.
Qed.
End Mono.

Lemma vsubs_nth : forall ps a b, vsubs ps a b -> forall i q, nth_error ps i = Some q ->
  vrel (p_var q) (nth i a TBot) (nth i b TBot).
Proof.
  induction 1; intros [|i] q0 E; simpl in E; try discriminate; auto;
    inversion E; subst; simpl; try rewrite H; simpl; auto. split; auto.
Qed.

(** ** Immutability *)

Lemma nth_immutable a i : forallb immutable a = true -> immutable (nth i a TBot) = true.
Proof.
  intros F. destruct (Nat.lt_ge_cases i (length a)) as [Hl|Hl].
  - apply (forallb_in _ _ _ F). apply nth_In; auto.
  - rewrite nth_overflow; auto.
Qed.

Lemma subst_immutable a : forall n t, size t < n -> immutable t = true ->
  forallb immutable a = true -> immutable (subst a t) = true.
Proof.
  induction n as [|n IH]; intros t Hs Hi Ha; [lia|].
  destruct t as [| | | | |t'|t'|fs r|x y|ins outs|E args|i|v]; simpl in Hs, Hi |- *; auto; try discriminate.
  - apply IH; auto; lia.
  - apply andb_true_iff in Hi as [Hx Hy]. rewrite !IH; auto; lia.
  - apply andb_true_iff in Hi as [He Hf]. rewrite He. simpl.
    apply forallb_forall. intros y Hy. apply in_map_iff in Hy as (x & <- & Hx).
    apply IH; auto; [pose proof (size_enum_arg x E args Hx) as Q; simpl in Q; lia | exact (forallb_in _ _ _ Hf Hx)].
  - apply nth_immutable; auto.
Qed.

(** ** Payloads *)

Lemma wf_payload_in E pts t : wf_payload E pts -> In t pts ->
  occ_sub (en_params E) PPos t = true /\ occ_fresh (en_params E) t = true /\
  (en_imm E = true -> immutable t = true).
Proof.
  intros W Hin. pose proof (forallb_in _ _ _ W Hin) as Wt. unfold wf_pt in Wt.
  apply andb_true_iff in Wt as [Wt Wi]. apply andb_true_iff in Wt as [Ws Wf].
  repeat split; auto. intros Ei. rewrite Ei in Wi. exact Wi.
Qed.

Lemma payload_sub E pts a b : wf_payload E pts -> vsubs (en_params E) a b ->
  forall t, In t pts -> sub (subst a t) (subst b t).
Proof.
  intros W V t Hin. destruct (wf_payload_in E pts t W Hin) as (Ws & _ & _).
  apply (mono_s_pos (en_params E)); auto. intros i q _ Hq. eapply vsubs_nth; eauto.
Qed.

Lemma payload_rsub E pts a b : wf_payload E pts -> vrsubs (en_params E) a b ->
  forall t, In t pts -> rsub (subst a t) (subst b t).
Proof.
  intros W V t Hin. destruct (wf_payload_in E pts t W Hin) as (_ & Wf & _).
  exact (mono_r _ _ _ V (S (size t)) t ltac:(lia) Wf).
Qed.

Lemma payload_imm E pts a : wf_payload E pts -> immutable (TEnum E a) = true ->
  forall t, In t pts -> immutable (subst a t) = true.
Proof.
  intros W Hi t Hin. simpl in Hi. apply andb_true_iff in Hi as [He Ha].
  destruct (wf_payload_in E pts t W Hin) as (_ & _ & Wi).
  apply (subst_immutable a (S (size t))); auto.
Qed.
