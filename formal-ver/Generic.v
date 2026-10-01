(** * Polymorphic definitions checked once.

    The soundness theorem asks every definition body to check at every
    instance of its signature ([def_ok]).  A checker checks the body once,
    with the signature's type variables rigid.  This file proves that is
    enough: typing is closed under substitution of types for type variables
    ([T_subst]), so a body that checks at its generic signature checks at
    every instance ([generic_def_ok], [soundness_generic]).

    The proof fixes how a rigid type variable must be treated, and each
    choice is forced (the counterexamples are in Examples.v):

    - it is not [immutable]: an instance may be a list, and a shared value
      must not become fresh ([immutable_tsub]);
    - a kind pattern treats it like unknown contents, not as "no value of
      this kind" ([kind_then_tsub]);
    - it cannot be a [tryAs] target: types are erased at runtime, so the
      validator could not know what to check ([tw_try_*] need [fvt u = []]).

    Quantified premises (quote bodies for every frame, kind-pattern arms for
    every element type) are handled by shifting the variables of the chosen
    instance out of the way ([shift], [unshift]). *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Variance Typing Interp Invariant RtLemmas Kind Soundness.

(** ** Substitution: basic facts *)

Lemma map_ext_Forall {A B} (f g : A -> B) l : Forall (fun x => f x = g x) l -> map f l = map g l.
Proof. induction 1; simpl; f_equal; auto. Qed.

Lemma Forall_impl_in {A} (P' R : A -> Prop) l : Forall P' l -> (forall x, In x l -> P' x -> R x) -> Forall R l.
Proof.
  intros F H. induction F; constructor.
  - apply H; [left; reflexivity | assumption].
  - apply IHF. intros; apply H; auto. right; assumption.
Qed.

Definition agree (th1 th2 : nat -> ty) (l : list nat) := forall x, In x l -> th1 x = th2 x.

Lemma agree_app_l th1 th2 a b : agree th1 th2 (a ++ b) -> agree th1 th2 a.
Proof. intros H x Hx; apply H; apply in_or_app; auto. Qed.
Lemma agree_app_r th1 th2 a b : agree th1 th2 (a ++ b) -> agree th1 th2 b.
Proof. intros H x Hx; apply H; apply in_or_app; auto. Qed.
Lemma agree_flat {A} (f : A -> list nat) th1 th2 l x :
  agree th1 th2 (flat_map f l) -> In x l -> agree th1 th2 (f x).
Proof. intros H Hx y Hy; apply H; apply in_flat_map; eauto. Qed.

Lemma tsub_ext_t : forall t th1 th2, agree th1 th2 (fvt t) -> tsub th1 t = tsub th2 t.
Proof.
  apply (ty_ind2 (fun t => forall th1 th2, agree th1 th2 (fvt t) -> tsub th1 t = tsub th2 t)
                 (fun f => forall th1 th2, agree th1 th2 (ffvt f) -> ftsub th1 f = ftsub th2 f));
    simpl; auto.
  - intros t IH th1 th2 A. f_equal; auto.
  - intros fs r Hfs Hr th1 th2 A. f_equal.
    + apply map_ext_Forall. eapply Forall_impl_in; [exact Hfs|].
      intros [k f] Hin Hq. simpl in *. f_equal. apply Hq.
      exact (agree_flat (fun p => ffvt (snd p)) _ _ _ _ (agree_app_l _ _ _ _ A) Hin).
    + apply Hr. exact (agree_app_r _ _ _ _ A).
  - intros a b Ha Hb th1 th2 A. f_equal; [apply Ha; eapply agree_app_l | apply Hb; eapply agree_app_r]; eauto.
  - intros ins outs Hi Ho th1 th2 A. f_equal.
    + apply map_ext_Forall. eapply Forall_impl_in; [exact Hi|].
      intros x Hin Hx. apply Hx. exact (agree_flat fvt _ _ _ _ (agree_app_l _ _ _ _ A) Hin).
    + destruct outs as [o|]; auto. f_equal. apply map_ext_Forall.
      eapply Forall_impl_in; [exact (Ho o eq_refl)|].
      intros x Hin Hx. apply Hx. exact (agree_flat fvt _ _ _ _ (agree_app_r _ _ _ _ A) Hin).
  - intros E args Ha th1 th2 A. f_equal. apply map_ext_Forall. eapply Forall_impl_in; [exact Ha|].
    intros x Hin Hx. apply Hx. exact (agree_flat fvt _ _ _ _ A Hin).
  - intros x th1 th2 A. apply A. left; reflexivity.
  - intros t IH th1 th2 A. f_equal; auto.
  - intros t IH th1 th2 A. f_equal; auto.
  - intros t IH th1 th2 A. f_equal; auto.
Qed.

Lemma tsub_ext t th1 th2 : agree th1 th2 (fvt t) -> tsub th1 t = tsub th2 t.
Proof. apply tsub_ext_t. Qed.

Lemma tsub_comp th1 th2 : forall t, tsub th2 (tsub th1 t) = tsub (fun x => tsub th2 (th1 x)) t.
Proof.
  apply (ty_ind2 (fun t => tsub th2 (tsub th1 t) = tsub (fun x => tsub th2 (th1 x)) t)
                 (fun f => ftsub th2 (ftsub th1 f) = ftsub (fun x => tsub th2 (th1 x)) f));
    simpl; intros; try (f_equal; auto; fail).
  - f_equal; auto. rewrite map_map. apply map_ext_Forall. eapply Forall_impl; [| eassumption].
    intros [k f] Hq; simpl in *; f_equal; auto.
  - f_equal.
    + rewrite map_map. apply map_ext_Forall; auto.
    + destruct outs as [o|]; auto. f_equal. rewrite map_map. apply map_ext_Forall; auto.
  - f_equal. rewrite map_map. apply map_ext_Forall; auto.
Qed.

Lemma tsub_id : forall t, tsub TVar t = t.
Proof.
  apply (ty_ind2 (fun t => tsub TVar t = t) (fun f => ftsub TVar f = f));
    simpl; intros; try (f_equal; auto; fail).
  - f_equal; auto. rewrite <- (map_id fs) at 2. apply map_ext_Forall. eapply Forall_impl; [| eassumption].
    intros [k f] Hq; simpl in *; f_equal; auto.
  - f_equal.
    + rewrite <- (map_id ins) at 2. apply map_ext_Forall; auto.
    + destruct outs as [o|]; auto. f_equal. rewrite <- (map_id o) at 2. apply map_ext_Forall; auto.
  - f_equal. rewrite <- (map_id args) at 2. apply map_ext_Forall; auto.
Qed.

Lemma tsub_closed th t : fvt t = [] -> tsub th t = t.
Proof.
  intros E. rewrite <- (tsub_id t) at 2. apply tsub_ext. rewrite E. intros x [].
Qed.

(** Enum payload types mention no type variable, so substituting a
    definition's variables commutes with substituting enum arguments. *)
Lemma tsub_subst th a : forall t, fvt t = [] -> tsub th (subst a t) = subst (map (tsub th) a) t.
Proof.
  apply (ty_ind2 (fun t => fvt t = [] -> tsub th (subst a t) = subst (map (tsub th) a) t)
                 (fun f => ffvt f = [] -> ftsub th (fsubst a f) = fsubst (map (tsub th) a) f));
    simpl; auto; try discriminate.
  - intros t IH E. f_equal; auto.
  - intros fs r Hfs Hr E. apply app_eq_nil in E as [E1 E2]. f_equal; auto.
    rewrite map_map. apply map_ext_Forall. eapply Forall_impl_in; [exact Hfs|].
    intros [k f] Hin Hq; simpl in *. f_equal. apply Hq.
    exact (proj1 (flat_map_nil_iff _ _) E1 _ Hin).
  - intros x y Hx Hy E. apply app_eq_nil in E as [E1 E2]. f_equal; auto.
  - intros ins outs Hi Ho E. apply app_eq_nil in E as [E1 E2]. f_equal.
    + rewrite map_map. apply map_ext_Forall. eapply Forall_impl_in; [exact Hi|].
      intros x Hin Hx. apply Hx. exact (proj1 (flat_map_nil_iff _ _) E1 _ Hin).
    + destruct outs as [o|]; auto. f_equal. rewrite map_map. apply map_ext_Forall.
      eapply Forall_impl_in; [exact (Ho o eq_refl)|]. intros x Hin Hx. apply Hx.
      exact (proj1 (flat_map_nil_iff _ _) E2 _ Hin).
  - intros E args Ha Ef. f_equal. rewrite map_map. apply map_ext_Forall. eapply Forall_impl_in; [exact Ha|].
    intros x Hin Hx. apply Hx. exact (proj1 (flat_map_nil_iff _ _) Ef _ Hin).
  - intros i _. symmetry. exact (map_nth (tsub th) a TBot i).
  - intros t IH E. f_equal; auto.
  - intros t IH E. f_equal; auto.
  - intros t IH E. f_equal; auto.
Qed.

Lemma field_at_tsub th k fs r :
  field_at k (map (fun p => (fst p, ftsub th (snd p))) fs) (ftsub th r) = ftsub th (field_at k fs r).
Proof.
  unfold field_at. induction fs as [|[k' f] fs IH]; simpl; auto.
  destruct (String.eqb k k'); auto.
Qed.

Lemma fty_tsub th f : fty (ftsub th f) = tsub th (fty f).
Proof. destruct f; reflexivity. Qed.

Lemma Forall2_in_r_ex {A B} (R : A -> B -> Prop) l1 l2 y :
  Forall2 R l1 l2 -> In y l2 -> exists x, In x l1 /\ R x y.
Proof.
  induction 1; simpl; [tauto|]. intros [<-|Hy]; [eauto|].
  destruct (IHForall2 Hy) as (x' & Hx & Hr). eauto.
Qed.

(** Declarations mention no type variable. *)
Lemma occ_sub_closed ps : forall t p, occ_sub ps p t = true -> fvt t = [].
Proof.
  apply (ty_ind2 (fun t => forall p, occ_sub ps p t = true -> fvt t = [])
                 (fun f => focc_sub ps f = true -> ffvt f = []));
    simpl; intros; auto; try discriminate; eauto.
  - apply andb_true_iff in H1 as [H1 H2]. rewrite (H0 H2), app_nil_r.
    apply flat_map_nil_iff. intros [k f] Hin. rewrite Forall_forall in H.
    apply (H _ Hin). exact (forallb_in _ _ _ H1 Hin).
  - apply andb_true_iff in H1 as [H1 H2]. erewrite H, H0; eauto.
  - apply andb_true_iff in H1 as [H1 H2]. cut (flat_map fvt ins = [] /\ match outs with Some o => flat_map fvt o | None => [] end = []);
    [intros [-> ->]; reflexivity|]. split.
    + apply flat_map_nil_iff. intros x Hin. rewrite Forall_forall in H.
      eapply H; eauto. exact (forallb_in _ _ _ H1 Hin).
    + destruct outs as [o|]; auto. apply flat_map_nil_iff. intros x Hin.
      specialize (H0 o eq_refl). rewrite Forall_forall in H0.
      eapply H0; eauto. exact (forallb_in _ _ _ H2 Hin).
  - apply flat_map_nil_iff. intros x Hin. apply forallb2_Forall2, Forall2_in_r in H0.
    destruct (Forall2_in_r_ex _ _ _ _ H0 Hin) as (q & _ & _ & Hq).
    rewrite Forall_forall in H. eapply H; eauto.
Qed.

Lemma wf_payload_closed E pts t : wf_payload E pts -> In t pts -> fvt t = [].
Proof. intros W Hin. destruct (wf_payload_in E pts t W Hin) as (Ws & _). eapply occ_sub_closed; eauto. Qed.

Definition omap (th : nat -> ty) (o : option (list ty)) : option (list ty) :=
  match o with Some l => Some (map (tsub th) l) | None => None end.

(** A closed type (in particular the unfolding of a closed recursive type)
    mentions no type variable, so substitution leaves it alone. *)
Lemma tsub_tclosed th : forall t d, tclosed d t = true -> tsub th t = t.
Proof.
  apply (ty_ind2 (fun t => forall d, tclosed d t = true -> tsub th t = t)
                 (fun f => forall d, ftclosed d f = true -> ftsub th f = f));
    simpl; intros; auto; try discriminate.
  - f_equal; eauto.
  - apply andb_true_iff in H1 as [H1 H2]. f_equal; eauto.
    rewrite <- (map_id fs) at 2. apply map_ext_Forall. rewrite Forall_forall in H |- *.
    intros [k f] Hin. simpl. f_equal. apply (H (k, f) Hin d). exact (forallb_in _ _ _ H1 Hin).
  - apply andb_true_iff in H1 as [? ?]. f_equal; eauto.
  - apply andb_true_iff in H1 as [H1 H2]. f_equal.
    + rewrite <- (map_id ins) at 2. apply map_ext_Forall. rewrite Forall_forall in H |- *.
      intros x Hin. eapply H; eauto. exact (forallb_in _ _ _ H1 Hin).
    + destruct outs as [o|]; auto. f_equal. rewrite <- (map_id o) at 2. apply map_ext_Forall.
      specialize (H0 o eq_refl). rewrite Forall_forall in H0 |- *.
      intros x Hin. eapply H0; eauto. exact (forallb_in _ _ _ H2 Hin).
  - f_equal. rewrite <- (map_id args) at 2. apply map_ext_Forall. rewrite Forall_forall in H |- *.
    intros x Hin. eapply H; eauto. exact (forallb_in _ _ _ H0 Hin).
  - f_equal; eauto.
  - f_equal; eauto.
  - f_equal; eauto.
Qed.

Lemma tsub_tunfold th t : mu_ok t = true -> tsub th (tunfold t) = tunfold t.
Proof. intros M. eapply tsub_tclosed. apply tclosed_tunfold; exact M. Qed.

(** Subtyping is closed under substitution: by coinduction, relating the
    substituted types. *)
Section SubTsub.
Variable th : nat -> ty.

Definition Rth (x y : ty) : Prop := exists a b, x = tsub th a /\ y = tsub th b /\ sub a b.

Lemma Rth_in a b : sub a b -> Rth (tsub th a) (tsub th b).
Proof. intros H. exists a, b; auto. Qed.

Lemma fsub_Rth f g : fsub f g -> fsubR Rth (ftsub th f) (ftsub th g).
Proof. intros H; destruct H; simpl; constructor; apply Rth_in; auto. Qed.

Lemma subs_Rth l1 l2 : subs l1 l2 -> subsR Rth (map (tsub th) l1) (map (tsub th) l2).
Proof. intros H; induction H; simpl; constructor; auto. apply Rth_in; auto. Qed.

Lemma osub_Rth o1 o2 : osub o1 o2 -> osubR Rth (omap th o1) (omap th o2).
Proof. intros H; destruct H; simpl; constructor. apply subs_Rth; auto. Qed.

Lemma vsubs_Rth ps a b : vsubs ps a b -> vsubsR Rth ps (map (tsub th) a) (map (tsub th) b).
Proof.
  intros H; induction H; simpl; [constructor | apply vs_co | apply vs_contra | apply vs_inv];
    auto using Rth_in.
Qed.

Lemma subF_Rth a b : subF sub a b -> subF Rth (tsub th a) (tsub th b).
Proof.
  intros H; induction H; simpl; try (econstructor; eauto using Rth_in; fail).
  - apply sf_mul; auto. rewrite <- (tsub_tunfold th t); auto.
  - apply sf_mur; auto. rewrite <- (tsub_tunfold th t); auto.
  - apply sf_rec. intros k. rewrite !field_at_tsub. apply fsub_Rth; auto.
  - apply sf_quote; [apply subs_Rth | apply (osub_Rth o1 o2)]; auto.
  - apply sf_enum, vsubs_Rth; auto.
Qed.
End SubTsub.

Lemma sub_tsub th : forall a b, sub a b -> sub (tsub th a) (tsub th b).
Proof.
  intros a b H. apply (sub_coind (Rth th)); [| apply Rth_in; exact H].
  intros x y (a' & b' & -> & -> & Hs). apply subF_Rth, sub_unfold, Hs.
Qed.

Lemma vrel_tsub th v x y : vrel v x y -> vrel v (tsub th x) (tsub th y).
Proof. destruct v; simpl; intros H; try apply sub_tsub; auto. destruct H; split; apply sub_tsub; auto. Qed.

Section RsubTsub.
Variable th : nat -> ty.

Definition RRth (x y : ty) : Prop := exists a b, x = tsub th a /\ y = tsub th b /\ rsub a b.

Lemma RRth_in a b : rsub a b -> RRth (tsub th a) (tsub th b).
Proof. intros H. exists a, b; auto. Qed.

Lemma frsub_RRth f g : frsub f g -> frsubR RRth (ftsub th f) (ftsub th g).
Proof.
  intros H; destruct H; simpl; try (constructor; auto using RRth_in; fail).
  - destruct H as [->|[->| ->]]; simpl; eapply frs_opt; eauto using RRth_in.
  - destruct H as [->|[->| ->]]; simpl; eapply frs_dict; eauto using RRth_in.
Qed.

Lemma vrsubs_RRth ps a b : vrsubs ps a b -> vrsubsR RRth ps (map (tsub th) a) (map (tsub th) b).
Proof.
  intros H; induction H; simpl; [constructor | apply vrs_fresh | apply vrs_sub];
    auto using RRth_in, vrel_tsub.
Qed.

Lemma rsubF_RRth a b : rsubF rsub a b -> rsubF RRth (tsub th a) (tsub th b).
Proof.
  intros H; induction H; simpl; try (econstructor; eauto using RRth_in; fail).
  - apply rf_sub, sub_tsub; auto.
  - apply rf_rec. intros k. rewrite !field_at_tsub. apply frsub_RRth; auto.
  - apply rf_enum, vrsubs_RRth; auto.
  - apply rf_mul; auto. rewrite <- (tsub_tunfold th t); auto.
  - apply rf_mur; auto. rewrite <- (tsub_tunfold th t); auto.
Qed.
End RsubTsub.

Lemma rsub_tsub th : forall a b, rsub a b -> rsub (tsub th a) (tsub th b).
Proof.
  intros a b H. apply (rsub_coind (RRth th)); [| apply RRth_in; exact H].
  intros x y (a' & b' & -> & -> & Hs). apply rsubF_RRth, rsub_unfold, Hs.
Qed.

Lemma immutable_tsub th : forall t, immutable t = true -> immutable (tsub th t) = true.
Proof.
  apply (ty_ind2 (fun t => immutable t = true -> immutable (tsub th t) = true)
                 (fun f => True)); simpl; intros; auto; try discriminate.
  - apply andb_true_iff in H1 as [? ?]. rewrite H, H0; auto.
  - apply andb_true_iff in H0 as [He Ha]. rewrite He. simpl.
    apply forallb_forall. intros y Hy. apply in_map_iff in Hy as (x & <- & Hx).
    rewrite Forall_forall in H. apply H; auto. exact (forallb_in _ _ _ Ha Hx).
Qed.

Lemma writable_tsub th f t : writable f t -> writable (ftsub th f) (tsub th t).
Proof. intros [->|[->| ->]]; [left | right; left | right; right]; reflexivity. Qed.

(** ** Kind patterns under substitution *)

Lemma tunion_lub a b c : sub a c -> sub b c -> sub (tunion a b) c.
Proof. intros Ha Hb. destruct a, b; simpl; auto; apply s_unionl; auto. Qed.

Lemma tunion_ub_l th x y : sub (tsub th x) (tsub th (tunion x y)).
Proof.
  destruct x, y; simpl; try apply s_bot; try apply s_refl; try apply s_unionr1; try apply s_refl.
Qed.

Lemma tunion_ub_r th x y : sub (tsub th y) (tsub th (tunion x y)).
Proof.
  destruct x, y; simpl; try apply s_bot; try apply s_refl; try apply s_unionr2; try apply s_refl.
Qed.

Lemma kind_top_closed k u : kind_top k = Some u -> forall th, tsub th u = u.
Proof. destruct k; simpl; intros Ek; inversion Ek; subst; reflexivity. Qed.


(** What a kind pattern binds is never more than unknown contents of that kind. *)
Lemma kind_then_below_top k u : kind_top k = Some u ->
  forall t, exists t1, kind_then k t = Some t1 /\ sub t1 u.
Proof.
  intros Hu. induction t; simpl;
    try (match goal with |- exists _, (if kind_eqb k ?k' then Some ?t else Some TBot) = Some _ /\ _ =>
           destruct (kind_eqb k k') eqn:Ek; eexists; (split; [reflexivity|]);
           [apply kind_eqb_true in Ek; subst; eapply kind_head_top; [reflexivity | exact Hu] | apply s_bot] end).
  all: try (rewrite Hu; eauto using s_refl; fail).
  all: try (eexists; split; [reflexivity | apply s_bot]; fail).
  destruct IHt1 as (x & Ex & Sx), IHt2 as (y & Ey & Sy). rewrite Ex, Ey.
  eexists; split; [reflexivity|]. apply tunion_lub; auto.
Qed.

Lemma kind_of_ty_tsub th t : (forall x, t <> TVar x) -> kind_of_ty (tsub th t) = kind_of_ty t.
Proof. destruct t; simpl; auto. intros H. exfalso. eapply H; reflexivity. Qed.

Lemma kind_then_tsub th k : forall t u1, kind_then k t = Some u1 ->
  exists t1', kind_then k (tsub th t) = Some t1' /\ sub t1' (tsub th u1).
Proof.
  induction t; intros u1 Ekt; simpl in Ekt |- *;
    try (match goal with H : (if kind_eqb k ?k' then _ else _) = Some _ |- _ =>
           destruct (kind_eqb k k'); injection H as <-; eexists; (split; [reflexivity | apply s_refl]) end; fail).
  - inversion Ekt; subst. eexists; split; [reflexivity | apply s_refl].
  - rewrite Ekt. eexists; split; [reflexivity|]. rewrite (kind_top_closed _ _ Ekt). apply s_refl.
  - destruct (kind_then k t1) as [x|] eqn:Ex; [|discriminate].
    destruct (kind_then k t2) as [y|] eqn:Ey; [|discriminate]. inversion Ekt; subst.
    destruct (IHt1 x eq_refl) as (x' & Ex' & Sx). destruct (IHt2 y eq_refl) as (y' & Ey' & Sy).
    rewrite Ex', Ey'. eexists; split; [reflexivity|]. apply tunion_lub.
    + eapply sub_trans; [exact Sx | apply tunion_ub_l].
    + eapply sub_trans; [exact Sy | apply tunion_ub_r].
  - inversion Ekt; subst. eexists; split; [reflexivity | apply s_refl].
  - destruct (kind_then_below_top k u1 Ekt (th x)) as (t1' & E' & S').
    rewrite E'. eexists; split; [reflexivity|]. rewrite (kind_top_closed _ _ Ekt). exact S'.
  - rewrite Ekt. eexists; split; [reflexivity|]. rewrite (kind_top_closed _ _ Ekt). apply s_refl.
  - inversion Ekt; subst. eexists; split; [reflexivity | apply s_refl].
Qed.

Lemma kind_else_sub k : forall t, sub (kind_else k t) t.
Proof.
  induction t; simpl; try apply s_refl;
    try (match goal with |- sub (if kind_eqb k ?k' then _ else _) _ =>
           destruct (kind_eqb k k'); [apply s_bot | apply s_refl] end).
  apply tunion_lub; [apply s_unionr1 | apply s_unionr2]; auto.
Qed.

Lemma kind_else_tsub th k : forall t, sub (kind_else k (tsub th t)) (tsub th (kind_else k t)).
Proof.
  induction t; simpl; try apply s_refl;
    try (match goal with |- sub (if kind_eqb k ?k' then _ else _) _ =>
           destruct (kind_eqb k k'); apply s_refl end).
  - apply tunion_lub.
    + eapply sub_trans; [exact IHt1 | apply tunion_ub_l].
    + eapply sub_trans; [exact IHt2 | apply tunion_ub_r].
  - apply kind_else_sub.
Qed.

(** ** Substitution on stacks, contexts and environments *)

Definition ssubst (th : nat -> ty) (s : sty) : sty := map (fun p => (fst p, tsub th (snd p))) s.
Definition esubst (th : nat -> ty) (G : tenv) : tenv := map (fun p => (fst p, tsub th (snd p))) G.
Definition lsubst (th : nat -> ty) (L : lctx) : lctx :=
  match L with LExact s => LExact (ssubst th s) | l => l end.
Definition rsubst (th : nat -> ty) (R : rctx) : rctx :=
  match R with RSome s => RSome (ssubst th s) | r => r end.

Definition fvs (s : sty) : list nat := flat_map (fun p => fvt (snd p)) s.
Definition fvG (G : tenv) : list nat := flat_map (fun p => fvt (snd p)) G.
Definition fvL (L : lctx) : list nat := match L with LExact s => fvs s | _ => [] end.
Definition fvR (R : rctx) : list nat := match R with RSome s => fvs s | _ => [] end.

Lemma ssubst_app th a b : ssubst th (a ++ b) = ssubst th a ++ ssubst th b.
Proof. apply map_app. Qed.

Lemma ssubst_marks th m l : ssubst th (marks m l) = marks m (map (tsub th) l).
Proof. unfold ssubst, marks. rewrite !map_map. reflexivity. Qed.

Lemma ssubst_shs th l : ssubst th (shs l) = shs (map (tsub th) l).
Proof. unfold ssubst, shs. rewrite !map_map. reflexivity. Qed.

Lemma ssubst_ext th1 th2 s : agree th1 th2 (fvs s) -> ssubst th1 s = ssubst th2 s.
Proof.
  intros A. apply map_ext_in. intros [m t] Hin. simpl. f_equal. apply tsub_ext.
  exact (agree_flat (fun p => fvt (snd p)) _ _ _ _ A Hin).
Qed.

Lemma esubst_ext th1 th2 G : agree th1 th2 (fvG G) -> esubst th1 G = esubst th2 G.
Proof.
  intros A. apply map_ext_in. intros [x t] Hin. simpl. f_equal. apply tsub_ext.
  exact (agree_flat (fun p => fvt (snd p)) _ _ _ _ A Hin).
Qed.

Lemma lsubst_ext th1 th2 L : agree th1 th2 (fvL L) -> lsubst th1 L = lsubst th2 L.
Proof. destruct L; simpl; intros; auto. f_equal. apply ssubst_ext; auto. Qed.

Lemma rsubst_ext th1 th2 R : agree th1 th2 (fvR R) -> rsubst th1 R = rsubst th2 R.
Proof. destruct R; simpl; intros; auto. f_equal. apply ssubst_ext; auto. Qed.

Lemma ssubst_comp th1 th2 s : ssubst th2 (ssubst th1 s) = ssubst (fun x => tsub th2 (th1 x)) s.
Proof. unfold ssubst. rewrite map_map. apply map_ext. intros [m t]; simpl. rewrite tsub_comp. reflexivity. Qed.

Lemma ssubst_id s : ssubst TVar s = s.
Proof. unfold ssubst. rewrite <- (map_id s) at 2. apply map_ext. intros [m t]; simpl. rewrite tsub_id. reflexivity. Qed.

Lemma lookup_esubst th x G t : lookup x G = Some t -> lookup x (esubst th G) = Some (tsub th t).
Proof.
  induction G as [|[y u] G IH]; simpl; [discriminate|].
  destruct (String.eqb x y); auto. intros E; inversion E; subst; reflexivity.
Qed.

Lemma slot_sub_subst th p q : slot_sub p q ->
  slot_sub (fst p, tsub th (snd p)) (fst q, tsub th (snd q)).
Proof.
  intros H; inversion H; subst; simpl.
  - apply ss_sh, sub_tsub; auto.
  - apply ss_dp, rsub_tsub; auto.
  - apply ss_forget, sub_tsub; auto.
  - apply ss_imm; [apply immutable_tsub; auto | apply sub_tsub; auto].
Qed.

Lemma ssub_subst th s1 s2 : ssub s1 s2 -> ssub (ssubst th s1) (ssubst th s2).
Proof. induction 1; simpl; constructor; auto. apply slot_sub_subst; auto. Qed.

Lemma child_ctx_subst th B s B' : child_ctx B s B' -> child_ctx (lsubst th B) (ssubst th s) (lsubst th B').
Proof. intros H; inversion H; subst; simpl; constructor. Qed.

(** ** Moving a chosen instance's variables out of the way *)

Fixpoint maxl (l : list nat) : nat := match l with [] => 0 | x :: l' => Nat.max x (maxl l') end.

Lemma maxl_lt l x : In x l -> x < S (maxl l).
Proof. induction l as [|y l IH]; simpl; [tauto|]. intros [<-|H]; [lia | specialize (IH H); lia]. Qed.

Definition shift (N : nat) : nat -> ty := fun y => TVar (y + N).
Definition unshift (N : nat) (th : nat -> ty) : nat -> ty :=
  fun x => if x <? N then th x else TVar (x - N).

Lemma unshift_shift N th t : tsub (unshift N th) (tsub (shift N) t) = t.
Proof.
  rewrite tsub_comp. rewrite <- (tsub_id t) at 2. apply tsub_ext. intros y _.
  unfold unshift, shift. simpl. destruct (y + N <? N) eqn:E.
  - apply Nat.ltb_lt in E. lia.
  - f_equal. lia.
Qed.

Lemma unshift_shift_s N th s : ssubst (unshift N th) (ssubst (shift N) s) = s.
Proof.
  unfold ssubst. rewrite map_map. rewrite <- (map_id s) at 2. apply map_ext.
  intros [m t]; simpl. f_equal. apply unshift_shift.
Qed.

Lemma unshift_agree N th l : (forall x, In x l -> x < N) -> agree (unshift N th) th l.
Proof. intros H x Hx. unfold unshift. specialize (H x Hx). apply Nat.ltb_lt in H. rewrite H. reflexivity. Qed.

Lemma below_maxl l l' : incl l' l -> forall x, In x l' -> x < S (maxl l).
Proof. intros Hi x Hx. apply maxl_lt, Hi, Hx. Qed.

Lemma lsubst_frame th L s0 : lsubst th (lframe L s0) = lframe (lsubst th L) (ssubst th s0).
Proof. destruct L; simpl; auto. rewrite ssubst_app. reflexivity. Qed.

Lemma rsubst_frame th R s0 : rsubst th (rframe R s0) = rframe (rsubst th R) (ssubst th s0).
Proof. destruct R; simpl; auto. rewrite ssubst_app. reflexivity. Qed.

Arguments ssubst th s : simpl never.

Lemma ssubst_cons th p s : ssubst th (p :: s) = (fst p, tsub th (snd p)) :: ssubst th s.
Proof. reflexivity. Qed.

Lemma ssubst_nil th : ssubst th [] = [].
Proof. reflexivity. Qed.

Lemma map_unshift_shift N th l : map (tsub (unshift N th)) (map (tsub (shift N)) l) = l.
Proof. rewrite map_map. rewrite <- (map_id l) at 2. apply map_ext. apply unshift_shift. Qed.

Definition osubst (th : nat -> ty) (o : option sty) : option sty :=
  match o with Some s => Some (ssubst th s) | None => None end.

Definition fvo (o : option sty) : list nat := match o with Some s => fvs s | None => [] end.

Definition bound (N : nat) (l : list nat) := forall x, In x l -> x < N.

Lemma bound_maxl L l : incl l L -> bound (S (maxl L)) l.
Proof. intros Hi x Hx. apply maxl_lt, Hi, Hx. Qed.

Lemma tsub_unshift N th t : bound N (fvt t) -> tsub (unshift N th) t = tsub th t.
Proof. intros B. apply tsub_ext, unshift_agree, B. Qed.

Lemma map_unshift N th l : bound N (flat_map fvt l) -> map (tsub (unshift N th)) l = map (tsub th) l.
Proof. intros B. apply map_ext_in. intros t Ht. apply tsub_unshift. intros x Hx. apply B, in_flat_map; eauto. Qed.

Lemma ssubst_unshift N th s : bound N (fvs s) -> ssubst (unshift N th) s = ssubst th s.
Proof. intros B. apply ssubst_ext, unshift_agree, B. Qed.

Lemma esubst_unshift N th G : bound N (fvG G) -> esubst (unshift N th) G = esubst th G.
Proof. intros B. apply esubst_ext, unshift_agree, B. Qed.

Lemma lsubst_unshift N th L : bound N (fvL L) -> lsubst (unshift N th) L = lsubst th L.
Proof. intros B. apply lsubst_ext, unshift_agree, B. Qed.

Lemma rsubst_unshift N th R : bound N (fvR R) -> rsubst (unshift N th) R = rsubst th R.
Proof. intros B. apply rsubst_ext, unshift_agree, B. Qed.

Ltac inclapp := let x := fresh in let Hx := fresh in intros x Hx; rewrite ?in_app_iff; tauto.

Ltac ss := repeat (rewrite ?ssubst_cons, ?ssubst_app, ?ssubst_shs, ?ssubst_marks, ?ssubst_nil); simpl.

Combined Scheme TW_comb from TW_mut, T_mut.

Section Subst.
Variable sigs : genv.
(** The instances of a signature are closed under substitution, and enum
    declarations are well formed. *)
Hypothesis Hsigs : forall f ins outs th, g_sigs sigs f ins outs ->
  g_sigs sigs f (ssubst th ins) (osubst th outs).
Hypothesis Hctors : forall E c pts, g_ctors sigs E c = Some pts -> wf_payload E pts.
Variable G : tenv.

Lemma map_subst_tsub th a pts : (forall t, In t pts -> fvt t = []) ->
  map (tsub th) (map (subst a) pts) = map (subst (map (tsub th) a)) pts.
Proof. intros H. rewrite map_map. apply map_ext_in. intros t Ht. apply tsub_subst; auto. Qed.

Theorem T_subst :
  (forall B C R w s1 s2, TW sigs G B C R w s1 s2 -> forall th,
     TW sigs (esubst th G) (lsubst th B) (lsubst th C) (rsubst th R) w (ssubst th s1) (ssubst th s2)) /\
  (forall B C R e s1 s2, T sigs G B C R e s1 s2 -> forall th,
     T sigs (esubst th G) (lsubst th B) (lsubst th C) (rsubst th R) e (ssubst th s1) (ssubst th s2)).
Proof.
  apply (TW_comb sigs G
    (fun B C R w s1 s2 _ => forall th,
       TW sigs (esubst th G) (lsubst th B) (lsubst th C) (rsubst th R) w (ssubst th s1) (ssubst th s2))
    (fun B C R e s1 s2 _ => forall th,
       T sigs (esubst th G) (lsubst th B) (lsubst th C) (rsubst th R) e (ssubst th s1) (ssubst th s2)));
    intros; ss.
  (* literals, arithmetic, shuffles *)
  - constructor.
  - constructor.
  - constructor.
  - constructor.
  - constructor.
  - constructor.
  - constructor.
  - constructor.
  - apply tw_load. apply lookup_esubst; auto.
  - apply tw_store. apply lookup_esubst; auto.
  - (* quote: every frame *)
    apply tw_quote. intros s0.
    set (N := S (maxl (fvG G ++ flat_map fvt ins ++ flat_map fvt outs))).
    specialize (H (ssubst (shift N) s0) (unshift N th)). revert H. ss. intros H.
    rewrite !unshift_shift_s, esubst_unshift, !map_unshift in H; auto;
      apply bound_maxl; inclapp.
  - apply tw_quote_never. intros s0 s'.
    set (N := S (maxl (fvG G ++ flat_map fvt ins))).
    specialize (H (ssubst (shift N) s0) (ssubst (shift N) s') (unshift N th)). revert H. ss. intros H.
    rewrite !unshift_shift_s, esubst_unshift, !map_unshift in H; auto;
      apply bound_maxl; inclapp.
  - constructor.
  - constructor.
  - constructor; auto.
  - constructor. specialize (H th). exact H.
  - apply tw_loop_forever. specialize (H th). exact H.
  - constructor.
  - constructor.
  - constructor.
  - constructor.
  - constructor.
  - constructor.
  - constructor.
  - apply tw_call. exact (Hsigs _ _ _ th g).
  - apply tw_call_never. exact (Hsigs _ _ _ th g).
  - constructor.
  - constructor.
  - constructor.
  - constructor.
  - constructor.
  - (* each *)
    eapply tw_each; [apply child_ctx_subst; eauto | apply child_ctx_subst; eauto |].
    specialize (H th). revert H. ss. auto.
  - eapply tw_map; [apply child_ctx_subst; eauto | apply child_ctx_subst; eauto |].
    specialize (H th). revert H. ss. auto.
  - eapply tw_map_imm; [apply immutable_tsub; eauto | apply child_ctx_subst; eauto | apply child_ctx_subst; eauto |].
    specialize (H th). revert H. ss. auto.
  - (* take, skip, slices *)
    match goal with Hs : slice_args _ _ |- _ => destruct Hs end; ss;
      [eapply tw_slice with (a := [(Sh, TInt)]) | eapply tw_slice with (a := [(Sh, TInt)])
      | eapply tw_slice with (a := [])];
      solve [constructor | intuition (subst; auto using immutable_tsub)].
  - constructor.
  - rewrite <- fty_tsub, <- field_at_tsub. apply tw_getk.
  - apply tw_getreq. rewrite field_at_tsub, e. reflexivity.
  - apply tw_setk_sh. rewrite field_at_tsub. apply writable_tsub; auto.
  - constructor.
  - eapply tw_del_sh. rewrite field_at_tsub, e. reflexivity.
  - constructor.
  - apply tw_getd. intros k. rewrite field_at_tsub, fty_tsub. apply sub_tsub; auto.
  - apply tw_setd. intros k. rewrite field_at_tsub. apply writable_tsub; auto.
  - (* kind pattern *)
    destruct (kind_then_tsub th k t t1 e) as (t1' & E' & S').
    eapply tw_kind; [exact E' | |].
    + eapply t_sub; [| apply (H th) | apply ssub_refl]. revert S'. ss. intros S'.
      constructor; [| apply ssub_refl]. destruct m; constructor; auto. apply rs_sub; auto.
    + eapply t_sub; [| apply (H0 th) | apply ssub_refl]. ss.
      constructor; [| apply ssub_refl]. destruct m; constructor; auto using kind_else_tsub.
      apply rs_sub, kind_else_tsub.
  - (* list kind pattern: every element type *)
    apply tw_kind_list; [| specialize (H0 th); revert H0; ss; auto].
    intros a'.
    set (N := S (maxl (fvG G ++ fvL B ++ fvL C ++ fvR R ++ fvs s ++ fvs s'))).
    specialize (H (tsub (shift N) a') (unshift N th)). revert H. ss. intros H.
    rewrite unshift_shift, esubst_unshift, lsubst_unshift, lsubst_unshift, rsubst_unshift,
      !ssubst_unshift in H; auto; apply bound_maxl; inclapp.
  - rewrite (tsub_closed th u e). apply tw_try_dp; auto.
  - rewrite (tsub_closed th u e). apply tw_try_sub; auto. rewrite <- (tsub_closed th u e). apply sub_tsub; auto.
  - rewrite (tsub_closed th u e). apply tw_try_imm; auto.
  - constructor.
  - (* constructors *)
    rewrite map_subst_tsub by (intros; eapply wf_payload_closed; eauto). eapply tw_con_sh; eauto.
  - rewrite map_subst_tsub by (intros; eapply wf_payload_closed; eauto). eapply tw_con_dp; eauto.
  - (* match *)
    apply tw_case. intros c pts e0 Ec Ea. specialize (H c pts e0 Ec Ea th). revert H. ss.
    rewrite map_subst_tsub by (intros; eapply wf_payload_closed; eauto). auto.
  - (* enum kind pattern: every argument list *)
    apply tw_kind_enum; [| specialize (H0 th); revert H0; ss; auto].
    intros a' La.
    set (N := S (maxl (fvG G ++ fvL B ++ fvL C ++ fvR R ++ fvs s ++ fvs s'))).
    specialize (H (map (tsub (shift N)) a') ltac:(rewrite length_map; exact La) (unshift N th)).
    revert H. ss. intros H.
    rewrite map_unshift_shift in H.
    rewrite esubst_unshift, lsubst_unshift, lsubst_unshift, rsubst_unshift,
      !ssubst_unshift in H; auto; apply bound_maxl; inclapp.
  - constructor.
  - econstructor; eauto.
  - eapply t_sub; [apply ssub_subst; eauto | eauto | apply ssub_subst; eauto].
  - (* dead code after a diverging word: every frame, every output *)
    apply t_div. intros s0 s2.
    set (N := S (maxl (fvG G ++ fvL B ++ fvL C ++ fvR R ++ fvs s1))).
    specialize (H (ssubst (shift N) s0) (ssubst (shift N) s2) (unshift N th)).
    rewrite ssubst_app, lsubst_frame, lsubst_frame, rsubst_frame, !unshift_shift_s in H.
    rewrite esubst_unshift, lsubst_unshift, lsubst_unshift, rsubst_unshift, ssubst_unshift in H;
      auto; apply bound_maxl; inclapp.
Qed.
End Subst.

(** ** Definitions checked once at their generic signatures *)

(** [gs f] is [f]'s declared signature, mentioning type variables. *)
Definition gsig_env := string -> option (sty * option sty).

(** The instances of the declared signatures. *)
Definition instances (gs : gsig_env) (f : string) (ins : sty) (outs : option sty) : Prop :=
  exists th gi go, gs f = Some (gi, go) /\ ins = ssubst th gi /\ outs = osubst th go.

(** Every body checks once, at its declared signature, with its type
    variables rigid (they are just [TVar]s: no rule knows anything about them). *)
Definition gdefs_ok (sigs : genv) (gs : gsig_env) (defs : string -> option prog) : Prop :=
  forall f gi go, gs f = Some (gi, go) ->
  exists body Gf, defs f = Some body /\
    match go with
    | Some o => forall s0, T sigs Gf LNone LNone (RSome (o ++ s0)) body (gi ++ s0) (o ++ s0)
    | None => forall s0 s', T sigs Gf LNone LNone RNone body (gi ++ s0) s'
    end.

Lemma instances_closed gs f ins outs th :
  instances gs f ins outs -> instances gs f (ssubst th ins) (osubst th outs).
Proof.
  intros (th0 & gi & go & E & -> & ->). exists (fun x => tsub th (th0 x)), gi, go. split; auto.
  split.
  - apply ssubst_comp.
  - destruct go; simpl; auto. f_equal. apply ssubst_comp.
Qed.

Theorem generic_def_ok sigs gs defs :
  (forall f ins outs, g_sigs sigs f ins outs <-> instances gs f ins outs) ->
  (forall E c pts, g_ctors sigs E c = Some pts -> wf_payload E pts) ->
  gdefs_ok sigs gs defs -> def_ok sigs defs.
Proof.
  intros Hs Hc Hg.
  assert (Hsigs : forall f ins outs th, g_sigs sigs f ins outs -> g_sigs sigs f (ssubst th ins) (osubst th outs)).
  { intros f ins outs th H. apply Hs. apply instances_closed. apply Hs. exact H. }
  intros f ins outs Hf. apply Hs in Hf as (th & gi & go & Eg & -> & ->).
  destruct (Hg f gi go Eg) as (body & Gf & Ed & Hb). exists body, (esubst th Gf). split; auto.
  destruct go as [o|]; simpl.
  - intros s0.
    set (N := S (maxl (fvG Gf ++ fvs gi ++ fvs o))).
    pose proof (proj2 (T_subst sigs Hsigs Hc Gf) _ _ _ _ _ _ (Hb (ssubst (shift N) s0)) (unshift N th)) as H.
    revert H. simpl. ss. intros H.
    rewrite !unshift_shift_s, esubst_unshift, !ssubst_unshift in H; auto; apply bound_maxl; inclapp.
  - intros s0 s'.
    set (N := S (maxl (fvG Gf ++ fvs gi))).
    pose proof (proj2 (T_subst sigs Hsigs Hc Gf) _ _ _ _ _ _
                  (Hb (ssubst (shift N) s0) (ssubst (shift N) s')) (unshift N th)) as H.
    revert H. simpl. ss. intros H.
    rewrite !unshift_shift_s, esubst_unshift, !ssubst_unshift in H; auto; apply bound_maxl; inclapp.
Qed.

(** Type soundness, with every definition checked once. *)
Theorem soundness_generic : forall sigs gs defs,
  (forall f ins outs, g_sigs sigs f ins outs <-> instances gs f ins outs) ->
  (forall E c pts, g_ctors sigs E c = Some pts -> wf_payload E pts) ->
  maybe_ok sigs ->
  gdefs_ok sigs gs defs ->
  forall G R e s, T sigs G LNone LNone R e [] s ->
  forall n, eval defs n [OScope []] 0 [] e <> RStuck.
Proof.
  intros sigs gs defs Hs Hc Hm Hg. apply soundness; auto. eapply generic_def_ok; eauto.
Qed.
