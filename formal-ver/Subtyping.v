(** * Subtyping ([sub]) and fresh retyping ([rsub]).

    [sub a b] is the doc's [a <= b]: the only subtyping between types of
    objects that may be shared.  It is checked per label for dict-kinded
    types (see [fsubR]), which gives the doc's S1-S4 as a special case.
    Lists are invariant, [Maybe] is covariant, quotes are contravariant in
    inputs and covariant in outputs, and a [never] quote is below every
    quote with the same inputs.

    [rsub a b] is the retyping allowed on a *fresh* (unaliased) value: it is
    covariant everywhere in the value's data, because nobody else can
    observe the change.

    ** Recursive types

    Types may be recursive ([TMu]), so both relations are relations on
    infinite trees: each is the greatest fixed point of a one-level
    relation ([subF], [rsubF]).  One level means: union and unfolding steps
    are taken finitely often (they recurse inside the one-level relation),
    and the children of a type constructor (a list's element, a field, a
    quote's inputs and outputs, an enum argument) are compared with [R],
    which is the relation being defined.  So [A <= B] holds for two
    recursive types that unfold to the same tree, which is what the
    checker's assumption-set algorithm decides (Amadio and Cardelli).

    The names [s_refl], [s_list], [rs_list], ... of the rules are lemmas
    here; [sub_unfold] and [rsub_unfold] give the one-level view back for
    case analysis. *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax.

(** ** One level of subtyping *)
Section Level.
Variable R : ty -> ty -> Prop.

(** Per label: [fsubR s t] means a view with status [t] is safe on an
    object whose own status is [s].
    - reads through [t] see what [s] allows (presence and type),
    - writes through [t] store the exact type [s] declares (invariance),
    - deletion through [t] (only [FDict]) is allowed by [s]. *)
Inductive fsubR : fstat -> fstat -> Prop :=
| fs_req a b : R a b -> R b a -> fsubR (FReq a) (FReq b)
| fs_opt_req a b : R a b -> R b a -> fsubR (FReq a) (FOpt b)
| fs_opt a b : R a b -> R b a -> fsubR (FOpt a) (FOpt b)
| fs_opt_dict a b : R a b -> R b a -> fsubR (FDict a) (FOpt b)
| fs_dict a b : R a b -> R b a -> fsubR (FDict a) (FDict b)
| fs_abs : fsubR FAbs FAbs
| fs_open f : fsubR f FOpen.

Inductive subsR : list ty -> list ty -> Prop :=
| subs_nil : subsR [] []
| subs_cons a b l1 l2 : R a b -> subsR l1 l2 -> subsR (a :: l1) (b :: l2).

Inductive osubR : option (list ty) -> option (list ty) -> Prop :=
| osub_never o : osubR None o
| osub_some l1 l2 : subsR l1 l2 -> osubR (Some l1) (Some l2).

(** Enum arguments, one parameter at a time, by the parameter's variance. *)
Inductive vsubsR : list eparam -> list ty -> list ty -> Prop :=
| vs_nil : vsubsR [] [] []
| vs_co p ps x y xs ys :
    p_var p = VCo -> R x y -> vsubsR ps xs ys -> vsubsR (p :: ps) (x :: xs) (y :: ys)
| vs_contra p ps x y xs ys :
    p_var p = VContra -> R y x -> vsubsR ps xs ys -> vsubsR (p :: ps) (x :: xs) (y :: ys)
| vs_inv p ps x y xs ys :
    p_var p = VInv -> R x y -> R y x -> vsubsR ps xs ys -> vsubsR (p :: ps) (x :: xs) (y :: ys).

Inductive subF : ty -> ty -> Prop :=
| sf_refl t : subF t t
| sf_bot t : subF TBot t
| sf_top t : subF t TTop
| sf_unionl a b c : subF a c -> subF b c -> subF (TUnion a b) c
| sf_unionr1 a b c : subF a b -> subF a (TUnion b c)
| sf_unionr2 a b c : subF a c -> subF a (TUnion b c)
| sf_mul t b : mu_ok t = true -> subF (tunfold t) b -> subF (TMu t) b
| sf_mur a t : mu_ok t = true -> subF a (tunfold t) -> subF a (TMu t)
| sf_maybe a b : R a b -> subF (TMaybe a) (TMaybe b)
| sf_list a b : R a b -> R b a -> subF (TList a) (TList b)
| sf_rec fs1 r1 fs2 r2 :
    (forall k, fsubR (field_at k fs1 r1) (field_at k fs2 r2)) -> subF (TRec fs1 r1) (TRec fs2 r2)
| sf_quote i1 o1 i2 o2 : subsR i2 i1 -> osubR o1 o2 -> subF (TQuote i1 o1) (TQuote i2 o2)
| sf_enum E a b : vsubsR (en_params E) a b -> subF (TEnum E a) (TEnum E b).
End Level.

Arguments fs_req {R}. Arguments fs_opt_req {R}. Arguments fs_opt {R}.
Arguments fs_opt_dict {R}. Arguments fs_dict {R}. Arguments fs_abs {R}. Arguments fs_open {R}.
Arguments subs_nil {R}. Arguments subs_cons {R}.
Arguments osub_never {R}. Arguments osub_some {R}.
Arguments vs_nil {R}. Arguments vs_co {R}. Arguments vs_contra {R}. Arguments vs_inv {R}.

(** Subtyping is the greatest relation that is closed under one level. *)
Definition sub (a b : ty) : Prop :=
  exists R : ty -> ty -> Prop, (forall x y, R x y -> subF R x y) /\ R a b.

Notation fsub := (fsubR sub).
Notation subs := (subsR sub).
Notation osub := (osubR sub).
Notation vsubs := (vsubsR sub).

Definition teq a b := sub a b /\ sub b a.

(** The relation a variance asks of two arguments. *)
Definition vrel (v : variance) (x y : ty) : Prop :=
  match v with VCo => sub x y | VContra => sub y x | VInv => teq x y end.

(** *** Monotonicity, folding and unfolding *)
Section Mono.
Variables R R' : ty -> ty -> Prop.
Hypothesis HR : forall x y, R x y -> R' x y.

Lemma fsubR_mono f g : fsubR R f g -> fsubR R' f g.
Proof. intros H; destruct H; constructor; auto. Qed.

Lemma subsR_mono l1 l2 : subsR R l1 l2 -> subsR R' l1 l2.
Proof. intros H; induction H; constructor; auto. Qed.

Lemma osubR_mono o1 o2 : osubR R o1 o2 -> osubR R' o1 o2.
Proof. intros H; destruct H; constructor. apply subsR_mono; auto. Qed.

Lemma vsubsR_mono ps a b : vsubsR R ps a b -> vsubsR R' ps a b.
Proof. intros H; induction H; [constructor | apply vs_co | apply vs_contra | apply vs_inv]; auto. Qed.

Lemma subF_mono a b : subF R a b -> subF R' a b.
Proof.
  intros H; induction H; try (econstructor; eauto; fail).
  - apply sf_rec. intros k. apply fsubR_mono; auto.
  - apply sf_quote; [apply subsR_mono | apply osubR_mono]; auto.
  - apply sf_enum, vsubsR_mono; auto.
Qed.
End Mono.

Lemma sub_coind (R : ty -> ty -> Prop) :
  (forall x y, R x y -> subF R x y) -> forall x y, R x y -> sub x y.
Proof. intros HR x y H. exists R; split; auto. Qed.

Lemma sub_unfold a b : sub a b -> subF sub a b.
Proof.
  intros (R & HR & H). eapply subF_mono; [| apply HR; exact H].
  intros x y Hxy. exists R; auto.
Qed.

Lemma sub_fold a b : subF sub a b -> sub a b.
Proof.
  intros H. apply (sub_coind (subF sub)); auto.
  intros x y Hxy. eapply subF_mono; [| exact Hxy]. apply sub_unfold.
Qed.

(** Coinduction up to [sub]: the relation may fall back on [sub] itself. *)
Lemma sub_coind_upto (R : ty -> ty -> Prop) :
  (forall x y, R x y -> subF (fun a b => R a b \/ sub a b) x y) -> forall x y, R x y -> sub x y.
Proof.
  intros HR x y H. apply (sub_coind (fun a b => R a b \/ sub a b)); [| left; exact H].
  intros a b [Hr|Hs]; [apply HR; exact Hr |].
  eapply subF_mono; [| apply sub_unfold, Hs]. auto.
Qed.

(** *** The rules *)
Lemma s_refl t : sub t t.
Proof. apply sub_fold, sf_refl. Qed.

Lemma s_bot t : sub TBot t.
Proof. apply sub_fold, sf_bot. Qed.

Lemma s_top t : sub t TTop.
Proof. apply sub_fold, sf_top. Qed.

Lemma s_unionl a b c : sub a c -> sub b c -> sub (TUnion a b) c.
Proof. intros H1 H2. apply sub_fold, sf_unionl; apply sub_unfold; auto. Qed.

Lemma s_unionr1 a b c : sub a b -> sub a (TUnion b c).
Proof. intros H. apply sub_fold, sf_unionr1, sub_unfold, H. Qed.

Lemma s_unionr2 a b c : sub a c -> sub a (TUnion b c).
Proof. intros H. apply sub_fold, sf_unionr2, sub_unfold, H. Qed.

Lemma s_mul t b : mu_ok t = true -> sub (tunfold t) b -> sub (TMu t) b.
Proof. intros M H. apply sub_fold, sf_mul, sub_unfold; auto. Qed.

Lemma s_mur a t : mu_ok t = true -> sub a (tunfold t) -> sub a (TMu t).
Proof. intros M H. apply sub_fold, sf_mur, sub_unfold; auto. Qed.

Lemma s_maybe a b : sub a b -> sub (TMaybe a) (TMaybe b).
Proof. intros H. apply sub_fold, sf_maybe, H. Qed.

Lemma s_list a b : sub a b -> sub b a -> sub (TList a) (TList b).
Proof. intros H1 H2. apply sub_fold, sf_list; auto. Qed.

Lemma s_rec fs1 r1 fs2 r2 :
  (forall k, fsub (field_at k fs1 r1) (field_at k fs2 r2)) -> sub (TRec fs1 r1) (TRec fs2 r2).
Proof. intros H. apply sub_fold, sf_rec, H. Qed.

Lemma s_quote i1 o1 i2 o2 : subs i2 i1 -> osub o1 o2 -> sub (TQuote i1 o1) (TQuote i2 o2).
Proof. intros H1 H2. apply sub_fold, sf_quote; auto. Qed.

Lemma s_enum E a b : vsubs (en_params E) a b -> sub (TEnum E a) (TEnum E b).
Proof. intros H. apply sub_fold, sf_enum, H. Qed.

(** A recursive type and its unfolding are equal. *)
Lemma sub_unfold_l t : mu_ok t = true -> sub (TMu t) (tunfold t).
Proof. intros M. apply s_mul; auto. apply s_refl. Qed.

Lemma sub_unfold_r t : mu_ok t = true -> sub (tunfold t) (TMu t).
Proof. intros M. apply s_mur; auto. apply s_refl. Qed.

(** ** Transitivity

    By coinduction: the composite of two subtypings is closed under one
    level.  Inside one level the proof is by induction on the first
    derivation and, where the middle type is a union or a recursive type
    the first derivation ends at, on the second. *)

Definition subc (x z : ty) : Prop := exists y, sub x y /\ sub y z.

Lemma sub_subc a b : sub a b -> subc a b.
Proof. intros H. exists b; split; [exact H | apply s_refl]. Qed.

Lemma sub_subc_l a b : sub a b -> subc a b.
Proof. intros H. exists a; split; [apply s_refl | exact H]. Qed.

Lemma fsub_comp f g h : fsub f g -> fsub g h -> fsubR subc f h.
Proof.
  intros H1 H2. inversion H1; subst; inversion H2; subst;
    first [ apply fs_open | apply fs_abs
          | econstructor; (eexists; split; eassumption) ].
Qed.

Lemma subs_comp : forall l1 l2 l3, subs l1 l2 -> subs l2 l3 -> subsR subc l1 l3.
Proof.
  intros l1 l2 l3 Q1. revert l3. induction Q1 as [|a b l1 l2 Hab Q1 IH]; intros l3 Q2;
    inversion Q2; subst; constructor.
  - eexists; split; eassumption.
  - apply IH; auto.
Qed.

Lemma osub_comp o1 o2 o3 : osub o1 o2 -> osub o2 o3 -> osubR subc o1 o3.
Proof.
  intros Q1 Q2. inversion Q1; subst; [constructor|]. inversion Q2; subst.
  constructor. eapply subs_comp; eauto.
Qed.

Lemma vsubs_comp : forall ps a b c, vsubs ps a b -> vsubs ps b c -> vsubsR subc ps a c.
Proof.
  intros ps a b c Q1. revert c. induction Q1; intros c0 Q2; inversion Q2; subst; try congruence.
  - constructor.
  - apply vs_co; auto. eexists; split; eassumption.
  - apply vs_contra; auto. eexists; split; eassumption.
  - apply vs_inv; auto; eexists; split; eassumption.
Qed.

Lemma subF_comp : forall x y, subF sub x y -> forall z, subF sub y z -> subF subc x z.
Proof.
  intros x y Q1. induction Q1; intros z Q2.
  - (* refl *) eapply subF_mono; [apply sub_subc | exact Q2].
  - apply sf_bot.
  - (* middle is unknown *)
    remember TTop as u eqn:Eu. induction Q2; subst; try discriminate;
      try (econstructor; eauto; fail).
  - apply sf_unionl; auto.
  - (* middle is a union, entered on the left *)
    remember (TUnion b c) as u eqn:Eu. induction Q2; subst; try discriminate;
      try (econstructor; eauto; fail).
    + apply sf_unionr1. eapply subF_mono; [apply sub_subc | exact Q1].
    + injection Eu as -> ->. auto.
  - remember (TUnion b c) as u eqn:Eu. induction Q2; subst; try discriminate;
      try (econstructor; eauto; fail).
    + apply sf_unionr2. eapply subF_mono; [apply sub_subc | exact Q1].
    + injection Eu as -> ->. auto.
  - apply sf_mul; auto.
  - (* middle is a recursive type, entered on the right *)
    remember (TMu t) as u eqn:Eu. induction Q2; subst; try discriminate;
      try (econstructor; eauto; fail).
    + apply sf_mur; auto. eapply subF_mono; [apply sub_subc | exact Q1].
    + injection Eu as ->. auto.
  - remember (TMaybe b) as u eqn:Eu. induction Q2; subst; try discriminate;
      try (econstructor; eauto; fail).
    + apply sf_maybe. apply sub_subc; auto.
    + injection Eu as ->. apply sf_maybe. eexists; split; eassumption.
  - remember (TList b) as u eqn:Eu. induction Q2; subst; try discriminate;
      try (econstructor; eauto; fail).
    + apply sf_list; apply sub_subc; auto.
    + injection Eu as ->. apply sf_list; eexists; split; eassumption.
  - remember (TRec fs2 r2) as u eqn:Eu. induction Q2; subst; try discriminate;
      try (econstructor; eauto; fail).
    + apply sf_rec. intros k. eapply fsubR_mono; [apply sub_subc | apply H].
    + injection Eu as -> ->. apply sf_rec. intros k. eapply fsub_comp; eauto.
  - remember (TQuote i2 o2) as u eqn:Eu. induction Q2; subst; try discriminate;
      try (econstructor; eauto; fail).
    + apply sf_quote; [apply (subsR_mono sub) | apply (osubR_mono sub)]; auto using sub_subc.
    + injection Eu as -> ->. apply sf_quote; [eapply subs_comp | eapply osub_comp]; eauto.
  - remember (TEnum E b) as u eqn:Eu. induction Q2; subst; try discriminate;
      try (econstructor; eauto; fail).
    + apply sf_enum. eapply vsubsR_mono; [apply sub_subc | exact H].
    + injection Eu as -> ->. apply sf_enum. eapply vsubs_comp; eauto.
Qed.

Theorem sub_trans : forall a b c, sub a b -> sub b c -> sub a c.
Proof.
  intros a b c H1 H2. apply (sub_coind subc); [| exists b; auto].
  intros x z (y & Hxy & Hyz). eapply subF_comp; apply sub_unfold; eauto.
Qed.

Lemma sub_top_inv : forall b c, sub b c -> b = TTop -> forall a, sub a c.
Proof. intros b c H -> a. eapply sub_trans; [apply s_top | exact H]. Qed.

Lemma sub_bot_inv : forall a b, sub a b -> b = TBot -> forall c, sub a c.
Proof. intros a b H -> c. eapply sub_trans; [exact H | apply s_bot]. Qed.

(** ** Retyping of fresh values *)
Section RLevel.
Variable R : ty -> ty -> Prop.

Inductive frsubR : fstat -> fstat -> Prop :=
| frs_req a b : R a b -> frsubR (FReq a) (FReq b)
| frs_abs_opt b : frsubR FAbs (FOpt b)
| frs_abs_dict b : frsubR FAbs (FDict b)
| frs_opt f a b : (f = FReq a \/ f = FOpt a \/ f = FDict a) -> R a b -> frsubR f (FOpt b)
| frs_dict f a b : (f = FReq a \/ f = FOpt a \/ f = FDict a) -> R a b -> frsubR f (FDict b)
| frs_abs : frsubR FAbs FAbs
| frs_open f : frsubR f FOpen.

(** A fresh enum value may retype a fresh-covariant argument with [rsub];
    any other argument changes only as its variance allows. *)
Inductive vrsubsR : list eparam -> list ty -> list ty -> Prop :=
| vrs_nil : vrsubsR [] [] []
| vrs_fresh p ps x y xs ys :
    p_fresh p = true -> R x y -> vrsubsR ps xs ys -> vrsubsR (p :: ps) (x :: xs) (y :: ys)
| vrs_sub p ps x y xs ys :
    p_fresh p = false -> vrel (p_var p) x y -> vrsubsR ps xs ys -> vrsubsR (p :: ps) (x :: xs) (y :: ys).

(** Quotes are not data: under a quote only [sub] applies ([rf_sub]). *)
Inductive rsubF : ty -> ty -> Prop :=
| rf_sub a b : sub a b -> rsubF a b
| rf_maybe a b : R a b -> rsubF (TMaybe a) (TMaybe b)
| rf_list a b : R a b -> rsubF (TList a) (TList b)
| rf_rec fs1 r1 fs2 r2 :
    (forall k, frsubR (field_at k fs1 r1) (field_at k fs2 r2)) -> rsubF (TRec fs1 r1) (TRec fs2 r2)
| rf_unionl a b c : rsubF a c -> rsubF b c -> rsubF (TUnion a b) c
| rf_unionr1 a b c : rsubF a b -> rsubF a (TUnion b c)
| rf_unionr2 a b c : rsubF a c -> rsubF a (TUnion b c)
| rf_enum E a b : vrsubsR (en_params E) a b -> rsubF (TEnum E a) (TEnum E b)
| rf_mul t b : mu_ok t = true -> rsubF (tunfold t) b -> rsubF (TMu t) b
| rf_mur a t : mu_ok t = true -> rsubF a (tunfold t) -> rsubF a (TMu t).
End RLevel.

Arguments frs_req {R}. Arguments frs_abs_opt {R}. Arguments frs_abs_dict {R}.
Arguments frs_opt {R}. Arguments frs_dict {R}. Arguments frs_abs {R}. Arguments frs_open {R}.
Arguments vrs_nil {R}. Arguments vrs_fresh {R}. Arguments vrs_sub {R}.

Definition rsub (a b : ty) : Prop :=
  exists R : ty -> ty -> Prop, (forall x y, R x y -> rsubF R x y) /\ R a b.

Notation frsub := (frsubR rsub).
Notation vrsubs := (vrsubsR rsub).

Section RMono.
Variables R R' : ty -> ty -> Prop.
Hypothesis HR : forall x y, R x y -> R' x y.

Lemma frsubR_mono f g : frsubR R f g -> frsubR R' f g.
Proof. intros H; destruct H; econstructor; eauto. Qed.

Lemma vrsubsR_mono ps a b : vrsubsR R ps a b -> vrsubsR R' ps a b.
Proof. intros H; induction H; [constructor | apply vrs_fresh | apply vrs_sub]; auto. Qed.

Lemma rsubF_mono a b : rsubF R a b -> rsubF R' a b.
Proof.
  intros H; induction H; try (econstructor; eauto; fail).
  - apply rf_rec. intros k. apply frsubR_mono; auto.
  - apply rf_enum, vrsubsR_mono; auto.
Qed.
End RMono.

Lemma rsub_coind (R : ty -> ty -> Prop) :
  (forall x y, R x y -> rsubF R x y) -> forall x y, R x y -> rsub x y.
Proof. intros HR x y H. exists R; split; auto. Qed.

Lemma rsub_unfold a b : rsub a b -> rsubF rsub a b.
Proof.
  intros (R & HR & H). eapply rsubF_mono; [| apply HR; exact H].
  intros x y Hxy. exists R; auto.
Qed.

Lemma rsub_fold a b : rsubF rsub a b -> rsub a b.
Proof.
  intros H. apply (rsub_coind (rsubF rsub)); auto.
  intros x y Hxy. eapply rsubF_mono; [| exact Hxy]. apply rsub_unfold.
Qed.

Lemma rsub_coind_upto (R : ty -> ty -> Prop) :
  (forall x y, R x y -> rsubF (fun a b => R a b \/ rsub a b) x y) -> forall x y, R x y -> rsub x y.
Proof.
  intros HR x y H. apply (rsub_coind (fun a b => R a b \/ rsub a b)); [| left; exact H].
  intros a b [Hr|Hs]; [apply HR; exact Hr |].
  eapply rsubF_mono; [| apply rsub_unfold, Hs]. auto.
Qed.

Lemma rs_sub a b : sub a b -> rsub a b.
Proof. intros H. apply rsub_fold, rf_sub, H. Qed.

Lemma rs_maybe a b : rsub a b -> rsub (TMaybe a) (TMaybe b).
Proof. intros H. apply rsub_fold, rf_maybe, H. Qed.

Lemma rs_list a b : rsub a b -> rsub (TList a) (TList b).
Proof. intros H. apply rsub_fold, rf_list, H. Qed.

Lemma rs_rec fs1 r1 fs2 r2 :
  (forall k, frsub (field_at k fs1 r1) (field_at k fs2 r2)) -> rsub (TRec fs1 r1) (TRec fs2 r2).
Proof. intros H. apply rsub_fold, rf_rec, H. Qed.

Lemma rs_unionl a b c : rsub a c -> rsub b c -> rsub (TUnion a b) c.
Proof. intros H1 H2. apply rsub_fold, rf_unionl; apply rsub_unfold; auto. Qed.

Lemma rs_unionr1 a b c : rsub a b -> rsub a (TUnion b c).
Proof. intros H. apply rsub_fold, rf_unionr1, rsub_unfold, H. Qed.

Lemma rs_unionr2 a b c : rsub a c -> rsub a (TUnion b c).
Proof. intros H. apply rsub_fold, rf_unionr2, rsub_unfold, H. Qed.

Lemma rs_enum E a b : vrsubs (en_params E) a b -> rsub (TEnum E a) (TEnum E b).
Proof. intros H. apply rsub_fold, rf_enum, H. Qed.

Lemma rs_mul t b : mu_ok t = true -> rsub (tunfold t) b -> rsub (TMu t) b.
Proof. intros M H. apply rsub_fold, rf_mul, rsub_unfold; auto. Qed.

Lemma rs_mur a t : mu_ok t = true -> rsub a (tunfold t) -> rsub a (TMu t).
Proof. intros M H. apply rsub_fold, rf_mur, rsub_unfold; auto. Qed.

(** Types none of whose values contain a list or dict.  A type variable is
    not immutable: it may be instantiated with a list type (Generic.v).  An enum type is
    immutable when its declaration says so and its arguments are immutable
    (conservative: an argument used only under a quote does not really
    matter).  A [TParam] has no values; it counts as immutable so that
    [en_imm] can be checked on payload types before substitution. *)
Fixpoint immutable (t : ty) : bool :=
  match t with
  | TInt | TStr | TBool | TBot | TParam _ | TRV _ => true
  | TVar _ => false          (* an instance may hold a list *)
  | TMu t' => immutable t'   (* the greatest fixed point: the variable counts as immutable *)
  | TMaybe t' => immutable t'
  | TUnion a b => immutable a && immutable b
  | TQuote _ _ => true
  | TEnum E a => en_imm E && forallb immutable a
  | TTop | TList _ | TRec _ _ => false
  end.

(** ** Closed types

    Unfolding a closed recursive type gives a closed type. *)
Lemma forallb_impl {A} (h g : A -> bool) l :
  Forall (fun x => h x = true -> g x = true) l -> forallb h l = true -> forallb g l = true.
Proof.
  induction 1; simpl; auto. intros E. apply andb_true_iff in E as [E1 E2].
  rewrite H by auto. simpl. auto.
Qed.

Lemma tclosed_mono : forall t d d', d <= d' -> tclosed d t = true -> tclosed d' t = true.
Proof.
  apply (ty_ind2 (fun t => forall d d', d <= d' -> tclosed d t = true -> tclosed d' t = true)
                 (fun f => forall d d', d <= d' -> ftclosed d f = true -> ftclosed d' f = true));
    simpl; intros; auto; try discriminate.
  - eauto.
  - eauto.
  - apply andb_true_iff in H2 as [H2 H3]. apply andb_true_iff; split; [|eauto].
    eapply forallb_impl; [| exact H2]. eapply Forall_impl; [| exact H]. intros p Hp. eauto.
  - apply andb_true_iff in H2 as [? ?]. apply andb_true_iff; split; eauto.
  - apply andb_true_iff in H2 as [H2 H3]. apply andb_true_iff; split.
    + eapply forallb_impl; [| exact H2]. eapply Forall_impl; [| exact H]. intros x Hx. eauto.
    + destruct outs as [o|]; auto. eapply forallb_impl; [| exact H3].
      eapply Forall_impl; [| exact (H0 o eq_refl)]. intros x Hx. eauto.
  - eapply forallb_impl; [| exact H1]. eapply Forall_impl; [| exact H]. intros x Hx. eauto.
  - apply (H (S d) (S d')); auto. lia.
  - apply Nat.ltb_lt in H0. apply Nat.ltb_lt. lia.
  - eauto.
  - eauto.
  - eauto.
Qed.

Lemma forallb_map {A B} (h : B -> bool) (f : A -> B) l :
  forallb h (map f l) = forallb (fun x => h (f x)) l.
Proof. induction l; simpl; auto. rewrite IHl. reflexivity. Qed.

Lemma tclosed_musubst s : tclosed 0 s = true ->
  forall t d, tclosed (S d) t = true -> tclosed d (musubst d s t) = true.
Proof.
  intros Hs.
  apply (ty_ind2 (fun t => forall d, tclosed (S d) t = true -> tclosed d (musubst d s t) = true)
                 (fun f => forall d, ftclosed (S d) f = true -> ftclosed d (fmusubst d s f) = true));
    simpl; intros; auto; try discriminate.
  - apply andb_true_iff in H1 as [H1 H2]. apply andb_true_iff; split; auto.
    rewrite forallb_map. eapply forallb_impl; [| exact H1]. eapply Forall_impl; [| exact H].
    intros p Hp. simpl. auto.
  - apply andb_true_iff in H1 as [? ?]. apply andb_true_iff; split; auto.
  - apply andb_true_iff in H1 as [H1 H2]. apply andb_true_iff; split.
    + rewrite forallb_map. eapply forallb_impl; [| exact H1]. eapply Forall_impl; [| exact H]. auto.
    + destruct outs as [o|]; auto. rewrite forallb_map. eapply forallb_impl; [| exact H2].
      eapply Forall_impl; [| exact (H0 o eq_refl)]. auto.
  - rewrite forallb_map. eapply forallb_impl; [| exact H0]. eapply Forall_impl; [| exact H]. auto.
  - apply Nat.ltb_lt in H. destruct (Nat.eqb_spec n d).
    + eapply tclosed_mono; [| exact Hs]. lia.
    + simpl. apply Nat.ltb_lt. lia.
Qed.

Lemma tclosed_tunfold t : mu_ok t = true -> tclosed 0 (tunfold t) = true.
Proof. intros M. apply tclosed_musubst; auto. Qed.

(** Unfolding a recursive type does not change whether it is immutable:
    [immutable] is the greatest fixed point, reading the recursion variable
    as immutable. *)
Lemma forallb_map_true {A} (h : A -> bool) (f : A -> A) l :
  Forall (fun x => h x = true -> h (f x) = true) l -> forallb h l = true -> forallb h (map f l) = true.
Proof.
  induction 1; simpl; auto. intros E. apply andb_true_iff in E as [E1 E2].
  rewrite H by auto. simpl. auto.
Qed.

Lemma forallb_map_false {A} (h : A -> bool) (f : A -> A) l :
  Forall (fun x => h x = false -> h (f x) = false) l -> forallb h l = false -> forallb h (map f l) = false.
Proof.
  induction 1; simpl; [discriminate|]. intros E.
  destruct (h x) eqn:Ex; simpl in E.
  - rewrite IHForall; auto. apply andb_false_r.
  - rewrite H; auto.
Qed.

Lemma immutable_musubst_true s : immutable s = true ->
  forall t k, immutable t = true -> immutable (musubst k s t) = true.
Proof.
  intros Hs.
  apply (ty_ind2 (fun t => forall k, immutable t = true -> immutable (musubst k s t) = true)
                 (fun _ => True)); simpl; intros; auto; try discriminate.
  - apply andb_true_iff in H1 as [? ?]. rewrite H, H0; auto.
  - apply andb_true_iff in H0 as [He Ha]. rewrite He. simpl.
    apply forallb_map_true; auto. eapply Forall_impl; [| exact H]. intros x Hx. apply Hx.
  - destruct (Nat.eqb n k); auto.
Qed.

Lemma immutable_musubst_false s :
  forall t k, immutable t = false -> immutable (musubst k s t) = false.
Proof.
  apply (ty_ind2 (fun t => forall k, immutable t = false -> immutable (musubst k s t) = false)
                 (fun _ => True)); simpl; intros; auto; try discriminate.
  - apply andb_false_iff in H1 as [Ee|Ee]; [rewrite H | rewrite H0]; auto. apply andb_false_r.
  - apply andb_false_iff in H0 as [Ee|Ee]; [rewrite Ee; reflexivity|].
    destruct (en_imm E); simpl; auto. apply forallb_map_false; auto.
    eapply Forall_impl; [| exact H]. intros x Hx. apply Hx.
Qed.

Lemma immutable_tunfold t : immutable (tunfold t) = immutable (TMu t).
Proof.
  unfold tunfold. simpl. destruct (immutable t) eqn:E.
  - apply immutable_musubst_true; auto.
  - apply immutable_musubst_false; auto.
Qed.

(** ** Well-formed enum declarations

    A constructor's payload types are checked against its enum's identity:
    the variance of each parameter against the polarity of each occurrence
    ([occ_sub]), fresh-covariance against data positions ([occ_fresh]), and
    [en_imm] against immutability.  Recursion through the enum's own name
    needs no special case: an occurrence [TEnum E args] is checked with the
    variances [E] itself carries. *)
Inductive pol := PPos | PNeg | PInv.

Definition flip (p : pol) : pol := match p with PPos => PNeg | PNeg => PPos | PInv => PInv end.

(** The polarity of an argument, under polarity [p], of a parameter with variance [v]. *)
Definition comp (p : pol) (v : variance) : pol :=
  match v with VCo => p | VContra => flip p | VInv => PInv end.

(** A parameter with variance [v] may occur at polarity [p]. *)
Definition compat (p : pol) (v : variance) : bool :=
  match p, v with
  | _, VInv => true
  | PPos, VCo | PNeg, VContra => true
  | _, _ => false
  end.

Fixpoint occ_sub (ps : list eparam) (p : pol) (t : ty) : bool :=
  match t with
  | TParam i => match nth_error ps i with Some q => compat p (p_var q) | None => false end
  | TVar _ => false          (* declarations do not mention definitions' variables *)
  | TInt | TStr | TBool | TBot | TTop | TMu _ | TRV _ => true   (* a recursive type is closed *)
  | TMaybe t' => occ_sub ps p t'
  | TList t' => occ_sub ps PInv t'
  | TRec fs r => forallb (fun kf => focc_sub ps (snd kf)) fs && focc_sub ps r
  | TUnion a b => occ_sub ps p a && occ_sub ps p b
  | TQuote ins outs =>
      forallb (occ_sub ps (flip p)) ins &&
      match outs with Some o => forallb (occ_sub ps p) o | None => true end
  | TEnum E args => forallb2 (fun q x => occ_sub ps (comp p (p_var q)) x) (en_params E) args
  end
with focc_sub (ps : list eparam) (f : fstat) : bool :=
  match f with
  | FReq t | FOpt t | FDict t => occ_sub ps PInv t
  | FAbs | FOpen => true
  end.

(** The parameters occurring in a type. *)
Fixpoint pocc (t : ty) : list nat :=
  match t with
  | TParam i => [i]
  | TInt | TStr | TBool | TBot | TTop | TVar _ | TMu _ | TRV _ => []
  | TMaybe t' | TList t' => pocc t'
  | TRec fs r => flat_map (fun kf => fpocc (snd kf)) fs ++ fpocc r
  | TUnion a b => pocc a ++ pocc b
  | TQuote ins outs =>
      flat_map pocc ins ++ match outs with Some o => flat_map pocc o | None => [] end
  | TEnum _ args => flat_map pocc args
  end
with fpocc (f : fstat) : list nat :=
  match f with FReq t | FOpt t | FDict t => pocc t | FAbs | FOpen => [] end.

(** No fresh-covariant parameter occurs in [t]. *)
Definition no_fresh (ps : list eparam) (t : ty) : bool :=
  forallb (fun i => match nth_error ps i with Some q => negb (p_fresh q) | None => true end) (pocc t).

(** [t] is at a data position: every parameter in it may be retyped as its
    fresh-covariance says.  Quotes are not data: nothing fresh is under one. *)
Fixpoint occ_fresh (ps : list eparam) (t : ty) : bool :=
  match t with
  | TParam i =>
      match nth_error ps i with
      | Some q => p_fresh q || negb (variance_eqb (p_var q) VContra)
      | None => false
      end
  | TVar _ => false
  | TInt | TStr | TBool | TBot | TTop | TMu _ | TRV _ => true
  | TMaybe t' | TList t' => occ_fresh ps t'
  | TRec fs r => forallb (fun kf => focc_fresh ps (snd kf)) fs && focc_fresh ps r
  | TUnion a b => occ_fresh ps a && occ_fresh ps b
  | TQuote _ _ => no_fresh ps t && occ_sub ps PPos t
  | TEnum E args =>
      forallb2 (fun q x => if p_fresh q then occ_fresh ps x
                           else no_fresh ps x && occ_sub ps (comp PPos (p_var q)) x)
               (en_params E) args
  end
with focc_fresh (ps : list eparam) (f : fstat) : bool :=
  match f with
  | FReq t | FOpt t | FDict t => occ_fresh ps t
  | FAbs | FOpen => true
  end.

Definition wf_pt (E : ename) (t : ty) : bool :=
  occ_sub (en_params E) PPos t && occ_fresh (en_params E) t && (negb (en_imm E) || immutable t).

(** The payload types [pts] of a constructor of [E] are well formed. *)
Definition wf_payload (E : ename) (pts : list ty) : Prop := forallb (wf_pt E) pts = true.


(** ** Size of a type (a recursive type counts as one node) *)

Fixpoint size (t : ty) : nat :=
  match t with
  | TInt | TStr | TBool | TBot | TTop | TParam _ | TVar _ | TMu _ | TRV _ => 1
  | TMaybe t' | TList t' => S (size t')
  | TUnion a b => S (size a + size b)
  | TRec fs r => S (fsize r + list_sum (map (fun p => fsize (snd p)) fs))
  | TQuote ins outs =>
      S (list_sum (map size ins) +
         match outs with None => 0 | Some l => list_sum (map size l) end)
  | TEnum _ a => S (list_sum (map size a))
  end
with fsize (f : fstat) : nat :=
  match f with
  | FReq t | FOpt t | FDict t => S (size t)
  | FAbs | FOpen => 1
  end.

Lemma list_sum_map_in {A : Type} (f : A -> nat) x l :
  In x l -> f x <= list_sum (map f l).
Proof.
  induction l as [|y l IH]; simpl; [tauto|].
  intros [<-|Hin]; [lia|]. specialize (IH Hin). lia.
Qed.

Lemma lookup_in {A : Type} k (l : list (string * A)) a :
  lookup k l = Some a -> In (k, a) l.
Proof.
  induction l as [|[k' a'] l IH]; simpl; [discriminate|].
  destruct (String.eqb_spec k k'); intros E.
  - inversion E; subst. left; reflexivity.
  - right; auto.
Qed.

Lemma size_pos t : 0 < size t.
Proof. destruct t; simpl; lia. Qed.

Lemma size_fty f : size (fty f) <= fsize f.
Proof. destruct f; simpl; lia. Qed.

Lemma size_field_at k fs r : fsize (field_at k fs r) < size (TRec fs r).
Proof.
  change (size (TRec fs r)) with (S (fsize r + list_sum (map (fun p => fsize (snd p)) fs))).
  unfold field_at. destruct (lookup k fs) as [f|] eqn:E.
  - apply lookup_in in E.
    pose proof (list_sum_map_in (fun p => fsize (snd p)) _ _ E) as Hs. cbn [snd] in Hs. unfold label in *. lia.
  - lia.
Qed.

Lemma size_quote_in t ins outs : In t ins -> size t < size (TQuote ins outs).
Proof.
  intros Hin. pose proof (list_sum_map_in size _ _ Hin).
  change (size (TQuote ins outs)) with (S (list_sum (map size ins) +
         match outs with None => 0 | Some l => list_sum (map size l) end)). lia.
Qed.

Lemma size_quote_out t ins l : In t l -> size t < size (TQuote ins (Some l)).
Proof.
  intros Hin. pose proof (list_sum_map_in size _ _ Hin).
  change (size (TQuote ins (Some l))) with (S (list_sum (map size ins) + list_sum (map size l))). lia.
Qed.

Lemma size_enum_arg t E a : In t a -> size t < size (TEnum E a).
Proof.
  intros Hin. pose proof (list_sum_map_in size _ _ Hin).
  change (size (TEnum E a)) with (S (list_sum (map size a))). lia.
Qed.


Lemma teq_refl t : teq t t.
Proof. split; apply s_refl. Qed.

Lemma teq_sym a b : teq a b -> teq b a.
Proof. intros [? ?]; split; assumption. Qed.

Lemma teq_trans a b c : teq a b -> teq b c -> teq a c.
Proof. intros [? ?] [? ?]; split; eapply sub_trans; eauto. Qed.

