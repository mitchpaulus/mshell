(** * Subtyping ([sub]) and fresh retyping ([rsub]).

    [sub a b] is the doc's [a <= b]: the only subtyping between types of
    objects that may be shared.  It is checked per label for dict-kinded
    types (see [fsub]), which gives the doc's S1-S4 as a special case
    (Shapes.v).  Lists are invariant, [Maybe] is covariant, quotes are
    contravariant in inputs and covariant in outputs, and a [never] quote is
    below every quote with the same inputs.

    [rsub a b] is the retyping allowed on a *fresh* (unaliased) value: it is
    covariant everywhere, because nobody else can observe the change. *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax.

Inductive sub : ty -> ty -> Prop :=
| s_refl t : sub t t
| s_bot t : sub TBot t
| s_top t : sub t TTop
| s_unionl a b c : sub a c -> sub b c -> sub (TUnion a b) c
| s_unionr1 a b c : sub a b -> sub a (TUnion b c)
| s_unionr2 a b c : sub a c -> sub a (TUnion b c)
| s_maybe a b : sub a b -> sub (TMaybe a) (TMaybe b)
| s_list a b : sub a b -> sub b a -> sub (TList a) (TList b)
| s_rec fs1 r1 fs2 r2 :
    (forall k, fsub (field_at k fs1 r1) (field_at k fs2 r2)) ->
    sub (TRec fs1 r1) (TRec fs2 r2)
| s_quote i1 o1 i2 o2 : subs i2 i1 -> osub o1 o2 -> sub (TQuote i1 o1) (TQuote i2 o2)
| s_enum E a b : vsubs (en_params E) a b -> sub (TEnum E a) (TEnum E b)

(** Per label: [fsub s t] means a view with status [t] is safe on an object
    whose own status is [s].
    - reads through [t] see what [s] allows (presence and type),
    - writes through [t] store the exact type [s] declares (invariance),
    - deletion through [t] (only [FDict]) is allowed by [s]. *)
with fsub : fstat -> fstat -> Prop :=
| fs_req a b : sub a b -> sub b a -> fsub (FReq a) (FReq b)
| fs_opt_req a b : sub a b -> sub b a -> fsub (FReq a) (FOpt b)
| fs_opt a b : sub a b -> sub b a -> fsub (FOpt a) (FOpt b)
| fs_opt_dict a b : sub a b -> sub b a -> fsub (FDict a) (FOpt b)
| fs_dict a b : sub a b -> sub b a -> fsub (FDict a) (FDict b)
| fs_abs : fsub FAbs FAbs
| fs_open f : fsub f FOpen

with subs : list ty -> list ty -> Prop :=
| subs_nil : subs [] []
| subs_cons a b l1 l2 : sub a b -> subs l1 l2 -> subs (a :: l1) (b :: l2)

with osub : option (list ty) -> option (list ty) -> Prop :=
| osub_never o : osub None o
| osub_some l1 l2 : subs l1 l2 -> osub (Some l1) (Some l2)

(** Enum arguments, one parameter at a time, by the parameter's variance. *)
with vsubs : list eparam -> list ty -> list ty -> Prop :=
| vs_nil : vsubs [] [] []
| vs_co p ps x y xs ys :
    p_var p = VCo -> sub x y -> vsubs ps xs ys -> vsubs (p :: ps) (x :: xs) (y :: ys)
| vs_contra p ps x y xs ys :
    p_var p = VContra -> sub y x -> vsubs ps xs ys -> vsubs (p :: ps) (x :: xs) (y :: ys)
| vs_inv p ps x y xs ys :
    p_var p = VInv -> sub x y -> sub y x -> vsubs ps xs ys -> vsubs (p :: ps) (x :: xs) (y :: ys).

Definition teq a b := sub a b /\ sub b a.

(** The relation a variance asks of two arguments. *)
Definition vrel (v : variance) (x y : ty) : Prop :=
  match v with VCo => sub x y | VContra => sub y x | VInv => teq x y end.

(** ** Retyping of fresh values *)
Inductive rsub : ty -> ty -> Prop :=
| rs_sub a b : sub a b -> rsub a b
| rs_maybe a b : rsub a b -> rsub (TMaybe a) (TMaybe b)
| rs_list a b : rsub a b -> rsub (TList a) (TList b)
| rs_rec fs1 r1 fs2 r2 :
    (forall k, frsub (field_at k fs1 r1) (field_at k fs2 r2)) ->
    rsub (TRec fs1 r1) (TRec fs2 r2)
| rs_unionl a b c : rsub a c -> rsub b c -> rsub (TUnion a b) c
| rs_unionr1 a b c : rsub a b -> rsub a (TUnion b c)
| rs_unionr2 a b c : rsub a c -> rsub a (TUnion b c)
| rs_enum E a b : vrsubs (en_params E) a b -> rsub (TEnum E a) (TEnum E b)
with frsub : fstat -> fstat -> Prop :=
| frs_req a b : rsub a b -> frsub (FReq a) (FReq b)
| frs_abs_opt b : frsub FAbs (FOpt b)
| frs_abs_dict b : frsub FAbs (FDict b)
| frs_opt f a b : (f = FReq a \/ f = FOpt a \/ f = FDict a) -> rsub a b -> frsub f (FOpt b)
| frs_dict f a b : (f = FReq a \/ f = FOpt a \/ f = FDict a) -> rsub a b -> frsub f (FDict b)
| frs_abs : frsub FAbs FAbs
| frs_open f : frsub f FOpen
(** A fresh enum value may retype a fresh-covariant argument with [rsub];
    any other argument changes only as its variance allows. *)
with vrsubs : list eparam -> list ty -> list ty -> Prop :=
| vrs_nil : vrsubs [] [] []
| vrs_fresh p ps x y xs ys :
    p_fresh p = true -> rsub x y -> vrsubs ps xs ys -> vrsubs (p :: ps) (x :: xs) (y :: ys)
| vrs_sub p ps x y xs ys :
    p_fresh p = false -> vrel (p_var p) x y -> vrsubs ps xs ys -> vrsubs (p :: ps) (x :: xs) (y :: ys).

(** Types none of whose values contain a list or dict.  A type variable is
    not immutable: it may be instantiated with a list type (Generic.v).  An enum type is
    immutable when its declaration says so and its arguments are immutable
    (conservative: an argument used only under a quote does not really
    matter).  A [TParam] has no values; it counts as immutable so that
    [en_imm] can be checked on payload types before substitution. *)
Fixpoint immutable (t : ty) : bool :=
  match t with
  | TInt | TStr | TBool | TBot | TParam _ => true
  | TVar _ => false          (* an instance may hold a list *)
  | TMaybe t' => immutable t'
  | TUnion a b => immutable a && immutable b
  | TQuote _ _ => true
  | TEnum E a => en_imm E && forallb immutable a
  | TTop | TList _ | TRec _ _ => false
  end.

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
  | TInt | TStr | TBool | TBot | TTop => true
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
  | TInt | TStr | TBool | TBot | TTop | TVar _ => []
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
  | TInt | TStr | TBool | TBot | TTop => true
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

(** ** Transitivity of [sub] *)

Fixpoint size (t : ty) : nat :=
  match t with
  | TInt | TStr | TBool | TBot | TTop | TParam _ | TVar _ => 1
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

Lemma vsubs_trans_mid : forall ps b a c,
  (forall y, In y b -> forall x z, sub x y -> sub y z -> sub x z) ->
  vsubs ps a b -> vsubs ps b c -> vsubs ps a c.
Proof.
  intros ps b a c Htr V1. revert c Htr. induction V1; intros c0 Htr V2; inversion V2; subst;
    try congruence.
  - apply vs_co; auto.
    + eapply Htr; [left; reflexivity | eassumption | eassumption].
    + apply IHV1; auto. intros; eapply Htr; eauto; right; assumption.
  - apply vs_contra; auto.
    + eapply Htr; [left; reflexivity | eassumption | eassumption].
    + apply IHV1; auto. intros; eapply Htr; eauto; right; assumption.
  - apply vs_inv; auto.
    + eapply Htr; [left; reflexivity | eassumption | eassumption].
    + eapply Htr; [left; reflexivity | eassumption | eassumption].
    + apply IHV1; auto. intros; eapply Htr; eauto; right; assumption.
Qed.

Lemma sub_top_inv : forall b c, sub b c -> b = TTop -> forall a, sub a c.
Proof.
  intros b c H. induction H; intros Eq a0; subst; try discriminate.
  - apply s_top.
  - apply s_top.
  - apply s_unionr1; auto.
  - apply s_unionr2; auto.
Qed.

Lemma sub_bot_inv : forall a b, sub a b -> b = TBot -> forall c, sub a c.
Proof.
  intros a b H. induction H; intros Eq c0; subst; try discriminate.
  - apply s_bot.
  - apply s_bot.
  - apply s_unionl; auto.
Qed.

Section FsubTrans.
Variable fb : fstat.
Hypothesis Htr : forall a c, sub a (fty fb) -> sub (fty fb) c -> sub a c.

Lemma fsub_trans_mid : forall fa fc, fsub fa fb -> fsub fb fc -> fsub fa fc.
Proof.
  intros fa fc H1 H2.
  inversion H1; subst; inversion H2; subst; simpl in Htr;
    first [ apply fs_open
          | econstructor; eauto ].
Qed.
End FsubTrans.

Lemma subs_trans_mid : forall l2 l1 l3,
  (forall b, In b l2 -> forall a c, sub a b -> sub b c -> sub a c) ->
  subs l1 l2 -> subs l2 l3 -> subs l1 l3.
Proof.
  induction l2 as [|b l2 IH]; intros l1 l3 Htr H1 H2;
    inversion H1; subst; inversion H2; subst; constructor.
  - eapply Htr; eauto. left; reflexivity.
  - eapply IH; eauto. intros; eapply Htr; eauto. right; assumption.
Qed.

Lemma sub_trans_n : forall n b, size b < n -> forall a c, sub a b -> sub b c -> sub a c.
Proof.
  induction n as [|n IHn]; intros b Hb a c Hab; [lia|].
  revert c Hb. induction Hab; intros c0 Hb Hbc.
  - exact Hbc.
  - apply s_bot.
  - eapply sub_top_inv; eauto.
  - apply s_unionl; auto.
  - (* a <= b | c via a <= b *)
    remember (TUnion b c) as u eqn:Eu. induction Hbc; subst; try discriminate.
    + apply s_unionr1; assumption.
    + apply s_top.
    + inversion Eu; subst. simpl in Hb. eapply IHn; [ | exact Hab | eassumption ]. lia.
    + apply s_unionr1; auto.
    + apply s_unionr2; auto.
  - remember (TUnion b c) as u eqn:Eu. induction Hbc; subst; try discriminate.
    + apply s_unionr2; assumption.
    + apply s_top.
    + inversion Eu; subst. simpl in Hb. eapply IHn; [ | exact Hab | eassumption ]. lia.
    + apply s_unionr1; auto.
    + apply s_unionr2; auto.
  - remember (TMaybe b) as u eqn:Eu. induction Hbc; subst; try discriminate.
    + apply s_maybe; assumption.
    + apply s_top.
    + apply s_unionr1; auto.
    + apply s_unionr2; auto.
    + inversion Eu; subst. simpl in Hb. apply s_maybe. eapply IHn; [ | exact Hab | eassumption ]. lia.
  - remember (TList b) as u eqn:Eu. induction Hbc; subst; try discriminate.
    + apply s_list; assumption.
    + apply s_top.
    + apply s_unionr1; auto.
    + apply s_unionr2; auto.
    + inversion Eu; subst. simpl in Hb.
      apply s_list; (eapply IHn; [ | eassumption | eassumption ]); lia.
  - remember (TRec fs2 r2) as u eqn:Eu. induction Hbc; subst; try discriminate.
    + apply s_rec; assumption.
    + apply s_top.
    + apply s_unionr1; auto.
    + apply s_unionr2; auto.
    + inversion Eu; subst. apply s_rec. intros k.
      match goal with
      | [ H1 : forall k, fsub (field_at k ?f1 ?q1) (field_at k ?f2 ?q2),
          H2 : forall k, fsub (field_at k ?f2 ?q2) (field_at k ?f3 ?q3)
          |- fsub (field_at ?k ?f1 ?q1) (field_at ?k ?f3 ?q3) ] =>
        apply fsub_trans_mid with (fb := field_at k f2 q2); [ | apply H1 | apply H2 ];
        intros a c Ha Hc; eapply IHn; [ | exact Ha | exact Hc ];
        pose proof (size_fty (field_at k f2 q2)); pose proof (size_field_at k f2 q2); lia
      end.
  - remember (TQuote i2 o2) as u eqn:Eu. induction Hbc; subst; try discriminate.
    + apply s_quote; assumption.
    + apply s_top.
    + apply s_unionr1; auto.
    + apply s_unionr2; auto.
    + inversion Eu; subst. apply s_quote.
      * apply subs_trans_mid with (l2 := i2); auto.
        intros b Hin a c Ha Hc. eapply IHn; [ | exact Ha | exact Hc ].
        pose proof (size_quote_in b i2 o2 Hin). lia.
      * inversion H0; subst; inversion H2; subst; constructor.
        match goal with
        | [ H1 : subs ?l1 ?l2, H2 : subs ?l2 ?l3 |- subs ?l1 ?l3 ] =>
            apply subs_trans_mid with (l2 := l2); [ | exact H1 | exact H2 ];
            intros b Hin a c Ha Hc; eapply IHn; [ | exact Ha | exact Hc ];
            pose proof (size_quote_out b i2 l2 Hin); lia
        end.
  - remember (TEnum E b) as u eqn:Eu. induction Hbc; subst; try discriminate.
    + apply s_enum; assumption.
    + apply s_top.
    + apply s_unionr1; auto.
    + apply s_unionr2; auto.
    + inversion Eu; subst. apply s_enum. eapply vsubs_trans_mid; [ | eassumption | eassumption ].
      intros y Hin x z Hx Hz. eapply IHn; [ | exact Hx | exact Hz ].
      pose proof (size_enum_arg y E b Hin). lia.
Qed.

Theorem sub_trans : forall a b c, sub a b -> sub b c -> sub a c.
Proof. intros a b c. apply (sub_trans_n (S (size b)) b). lia. Qed.

Lemma teq_refl t : teq t t.
Proof. split; apply s_refl. Qed.

Lemma teq_sym a b : teq a b -> teq b a.
Proof. intros [? ?]; split; assumption. Qed.

Lemma teq_trans a b c : teq a b -> teq b c -> teq a c.
Proof. intros [? ?] [? ?]; split; eapply sub_trans; eauto. Qed.
