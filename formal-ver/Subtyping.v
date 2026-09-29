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
| osub_some l1 l2 : subs l1 l2 -> osub (Some l1) (Some l2).

Definition teq a b := sub a b /\ sub b a.

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
with frsub : fstat -> fstat -> Prop :=
| frs_req a b : rsub a b -> frsub (FReq a) (FReq b)
| frs_abs_opt b : frsub FAbs (FOpt b)
| frs_abs_dict b : frsub FAbs (FDict b)
| frs_opt f a b : (f = FReq a \/ f = FOpt a \/ f = FDict a) -> rsub a b -> frsub f (FOpt b)
| frs_dict f a b : (f = FReq a \/ f = FOpt a \/ f = FDict a) -> rsub a b -> frsub f (FDict b)
| frs_abs : frsub FAbs FAbs
| frs_open f : frsub f FOpen.

(** Types none of whose values contain a list or dict. *)
Fixpoint immutable (t : ty) : bool :=
  match t with
  | TInt | TStr | TBool | TBot => true
  | TMaybe t' => immutable t'
  | TUnion a b => immutable a && immutable b
  | TQuote _ _ => true
  | TTop | TList _ | TRec _ _ => false
  end.

(** ** Transitivity of [sub] *)

Fixpoint size (t : ty) : nat :=
  match t with
  | TInt | TStr | TBool | TBot | TTop => 1
  | TMaybe t' | TList t' => S (size t')
  | TUnion a b => S (size a + size b)
  | TRec fs r => S (fsize r + list_sum (map (fun p => fsize (snd p)) fs))
  | TQuote ins outs =>
      S (list_sum (map size ins) +
         match outs with None => 0 | Some l => list_sum (map size l) end)
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

Lemma sub_top_inv : forall b c, sub b c -> b = TTop -> forall a, sub a c.
Proof.
  intros b c H. induction H; intros E a0; subst; try discriminate.
  - apply s_top.
  - apply s_top.
  - apply s_unionr1; auto.
  - apply s_unionr2; auto.
Qed.

Lemma sub_bot_inv : forall a b, sub a b -> b = TBot -> forall c, sub a c.
Proof.
  intros a b H. induction H; intros E c0; subst; try discriminate.
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
Qed.

Theorem sub_trans : forall a b c, sub a b -> sub b c -> sub a c.
Proof. intros a b c. apply (sub_trans_n (S (size b)) b). lia. Qed.

Lemma teq_refl t : teq t t.
Proof. split; apply s_refl. Qed.

Lemma teq_sym a b : teq a b -> teq b a.
Proof. intros [? ?]; split; assumption. Qed.

Lemma teq_trans a b c : teq a b -> teq b c -> teq a c.
Proof. intros [? ?] [? ?]; split; eapply sub_trans; eauto. Qed.
