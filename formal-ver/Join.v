(** * Branch joins.

    When the arms of an [if] leave different types in one slot, a checker
    computes their join.  [join_slot] is the design's join, written as a
    function, and [join_slot_ub] proves it is an upper bound of both arms
    under slot subsumption, so an [if] whose arms are joined this way
    checks in the core.

    Choices the proof pins down:
    - the result is fresh only when both arms are fresh;
    - widening *inside* a list, shape or fresh-covariant enum argument
      happens only when both arms are fresh; inside a covariant enum
      argument of shared values (a [Maybe], say) the inner join is itself a
      shared join;
    - two instances of different enums have different kinds, and join to
      their union, like any two types of different kinds;
    - when the types cannot be widened inside (quotes, shared containers,
      invariant and contravariant arguments, two unions), the join is the
      other side if one side is below the other ([<=], or the fresh retype
      when both slots are fresh), and otherwise there is none.  So a quote
      that never returns joins with any quote with the same inputs;
    - joining into a union joins the member of the same kind;
    - a recursive type is never widened inside ([ajoin]): the join takes
      the other side when one is below the other ([<=], or the fresh retype
      when both slots are fresh), or makes a union when their kinds do not
      overlap, and otherwise fails.  Widening inside two different recursive
      types would need a new recursive type for the result.

    The join is given the checker's decision procedure for [<=] and fresh
    retyping ([le]); the proofs assume only that it is right when it says
    yes ([le_ok]). *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Variance Typing Interp Invariant RtLemmas Validate Generic.

(** ** Decidable syntactic equality of types *)
Fixpoint ty_eqb (a b : ty) : bool :=
  match a, b with
  | TInt, TInt | TStr, TStr | TBool, TBool | TBot, TBot | TTop, TTop => true
  | TList x, TList y => ty_eqb x y
  | TRec fs1 r1, TRec fs2 r2 =>
      forallb2 (fun p q => String.eqb (fst p) (fst q) && fst_eqb (snd p) (snd q)) fs1 fs2
      && fst_eqb r1 r2
  | TUnion x1 y1, TUnion x2 y2 => ty_eqb x1 x2 && ty_eqb y1 y2
  | TQuote i1 o1, TQuote i2 o2 =>
      forallb2 (fun x y => ty_eqb x y) i1 i2 &&
      match o1, o2 with
      | Some l1, Some l2 => forallb2 (fun x y => ty_eqb x y) l1 l2
      | None, None => true
      | _, _ => false
      end
  | TEnum E1 a1, TEnum E2 a2 => ename_eqb E1 E2 && forallb2 (fun x y => ty_eqb x y) a1 a2
  | TParam i, TParam j => Nat.eqb i j
  | TVar x, TVar y => Nat.eqb x y
  | TMu x, TMu y => ty_eqb x y
  | TRV i, TRV j => Nat.eqb i j
  | _, _ => false
  end
with fst_eqb (f g : fstat) : bool :=
  match f, g with
  | FReq x, FReq y | FOpt x, FOpt y | FDict x, FDict y => ty_eqb x y
  | FAbs, FAbs | FOpen, FOpen => true
  | _, _ => false
  end.

Lemma Forall2_eq {A} (R : A -> A -> Prop) l1 l2 :
  Forall (fun x => forall y, R x y -> x = y) l1 -> Forall2 R l1 l2 -> l1 = l2.
Proof. intros F G. revert F. induction G; intros F; inversion F; subst; f_equal; auto. Qed.

Lemma ty_eqb_true : forall a b, ty_eqb a b = true -> a = b.
Proof.
  apply (ty_ind2 (fun a => forall b, ty_eqb a b = true -> a = b)
                 (fun f => forall g, fst_eqb f g = true -> f = g)).
  - intros b E; destruct b; simpl in E; congruence.
  - intros b E; destruct b; simpl in E; congruence.
  - intros b E; destruct b; simpl in E; congruence.
  - intros b E; destruct b; simpl in E; congruence.
  - intros b E; destruct b; simpl in E; congruence.
  - intros t IH b E; destruct b; simpl in E; try congruence. f_equal; auto.
  - intros fs r Hfs Hr b E; destruct b as [| | | | | |fs2 r2| | | | | | |]; simpl in E; try congruence.
    apply andb_true_iff in E as [E1 E2]. f_equal; auto.
    eapply Forall2_eq; [| apply forallb2_Forall2; exact E1].
    eapply Forall_impl; [| exact Hfs]. intros [k f] Hf [k' f'] Hp. simpl in *.
    apply andb_true_iff in Hp as [Hk Hq]. apply String.eqb_eq in Hk. subst. f_equal. auto.
  - intros x y Hx Hy b E; destruct b; simpl in E; try congruence.
    apply andb_true_iff in E as [E1 E2]. f_equal; auto.
  - intros ins outs Hi Ho b E; destruct b as [| | | | | | | |i2 o2| | | | |]; simpl in E; try congruence.
    apply andb_true_iff in E as [E1 E2]. f_equal.
    + eapply Forall2_eq; [exact Hi | apply forallb2_Forall2; exact E1].
    + destruct outs as [o1|], o2 as [o2|]; try discriminate; auto. f_equal.
      eapply Forall2_eq; [exact (Ho o1 eq_refl) | apply forallb2_Forall2; exact E2].
  - intros E0 args Ha b Eb; destruct b; simpl in Eb; try congruence.
    apply andb_true_iff in Eb as [Q1 Q2]. apply ename_eqb_true in Q1. subst. f_equal.
    eapply Forall2_eq; [exact Ha | apply forallb2_Forall2; exact Q2].
  - intros i b E; destruct b; simpl in E; try congruence. apply Nat.eqb_eq in E. subst. reflexivity.
  - intros x b E; destruct b; simpl in E; try congruence. apply Nat.eqb_eq in E. subst. reflexivity.
  - intros t IH b E; destruct b; simpl in E; try congruence. f_equal; auto.
  - intros n b E; destruct b; simpl in E; try congruence. apply Nat.eqb_eq in E. subst. reflexivity.
  - intros t IH g E; destruct g; simpl in E; try congruence; f_equal; auto.
  - intros t IH g E; destruct g; simpl in E; try congruence; f_equal; auto.
  - intros t IH g E; destruct g; simpl in E; try congruence; f_equal; auto.
  - intros g E; destruct g; simpl in E; congruence.
  - intros g E; destruct g; simpl in E; congruence.
Qed.

(** ** The join *)

Fixpoint omapl {A B} (f : A -> option B) (l : list A) : option (list B) :=
  match l with
  | [] => Some []
  | x :: l' => match f x, omapl f l' with Some y, Some ys => Some (y :: ys) | _, _ => None end
  end.

Definition is_mu (t : ty) : bool := match t with TMu _ => true | _ => false end.

(** The size of a type, counting the body of a recursive type. *)
Fixpoint dsize (t : ty) : nat :=
  match t with
  | TMu t' | TList t' => S (dsize t')
  | TUnion a b => S (dsize a + dsize b)
  | TRec fs r => S (fdsize r + list_sum (map (fun p => fdsize (snd p)) fs))
  | TQuote ins outs =>
      S (list_sum (map dsize ins) + match outs with None => 0 | Some l => list_sum (map dsize l) end)
  | TEnum _ a => S (list_sum (map dsize a))
  | TInt | TStr | TBool | TBot | TTop | TParam _ | TVar _ | TRV _ => 1
  end
with fdsize (f : fstat) : nat :=
  match f with FReq t | FOpt t | FDict t => S (dsize t) | FAbs | FOpen => 1 end.

(** The kinds of the members of [t], looking through recursive types;
    [None] when a member has no kind.  [n] is fuel. *)
Fixpoint tkinds (n : nat) (t : ty) {struct n} : option (list kind) :=
  match n with
  | 0 => None
  | S n' =>
      match t with
      | TBot => Some []
      | TUnion a b =>
          match tkinds n' a, tkinds n' b with Some x, Some y => Some (x ++ y) | _, _ => None end
      | TMu t' => if mu_ok t' then tkinds n' (tunfold t') else None
      | _ => match kind_of_ty t with Some k => Some [k] | None => None end
      end
  end.

Definition akinds (t : ty) : option (list kind) := tkinds (S (dsize t)) t.

Definition kdisj (xs ys : list kind) : bool :=
  forallb (fun x => negb (existsb (kind_eqb x) ys)) xs.

(** Does the union tree [t] have a member of kind [k]?  A recursive type
    counts with the kinds of its members. *)
Fixpoint ukind (k : kind) (t : ty) : bool :=
  match t with
  | TUnion a b => ukind k a || ukind k b
  | TMu _ => match akinds t with Some ks => existsb (kind_eqb k) ks | None => false end
  | _ => match kind_of_ty t with Some k' => kind_eqb k k' | None => false end
  end.

(** Labels of two fresh shapes, joined: present in both stays present;
    otherwise it becomes optional; deletable stays deletable; unknown wins. *)
Definition fjoin (j : bool -> ty -> ty -> option ty) (f g : fstat) : option fstat :=
  match f, g with
  | FOpen, _ | _, FOpen => Some FOpen
  | FAbs, FAbs => Some FAbs
  | FReq x, FReq y => option_map FReq (j true x y)
  | FDict x, FDict y => option_map FDict (j true x y)
  | FAbs, FDict y | FDict y, FAbs => Some (FDict y)
  | FAbs, (FReq y | FOpt y) | (FReq y | FOpt y), FAbs => Some (FOpt y)
  | FReq x, (FOpt y | FDict y) | FOpt x, (FReq y | FOpt y | FDict y) | FDict x, (FReq y | FOpt y) =>
      option_map FOpt (j true x y)
  end.

Definition rjoin (j : bool -> ty -> ty -> option ty) fs1 r1 fs2 r2 : option ty :=
  match omapl (fun k => option_map (fun f => (k, f)) (fjoin j (field_at k fs1 r1) (field_at k fs2 r2)))
              (map fst fs1 ++ map fst fs2), fjoin j r1 r2 with
  | Some fs, Some r => Some (TRec fs r)
  | _, _ => None
  end.

(** Enum arguments: a fresh-covariant argument of fresh values widens; a
    covariant one joins as shared values; any other must be equal. *)
Fixpoint ejoin (j : bool -> ty -> ty -> option ty) (fr : bool) (ps : list eparam) (xs ys : list ty)
  : option (list ty) :=
  match ps, xs, ys with
  | [], [], [] => Some []
  | p :: ps', x :: xs', y :: ys' =>
      let r := if fr && p_fresh p then j true x y
               else match p_var p with
                    | VCo => j false x y
                    | _ => if ty_eqb x y then Some x else None
                    end in
      match r, ejoin j fr ps' xs' ys' with Some z, Some zs => Some (z :: zs) | _, _ => None end
  | _, _, _ => None
  end.

(** Below: [<=] for shared slots, the fresh retype when both are fresh. *)
Definition jrel (fr : bool) (x y : ty) : Prop := if fr then rsub x y else sub x y.

Section Join.
(** The checker's decision procedure for [jrel]. *)
Variable le : bool -> ty -> ty -> bool.

(** A recursive type on either side: never widened inside.  The other side
    if one is below the other; a union if their kinds do not overlap. *)
Definition ajoin (fr : bool) (a b : ty) : option ty :=
  if ty_eqb a TBot then Some b else if ty_eqb b TBot then Some a else
  if le fr a b then Some b else if le fr b a then Some a else
  match akinds a, akinds b with
  | Some ka, Some kb => if kdisj ka kb then Some (TUnion a b) else None
  | _, _ => None
  end.

(** One level of the join: the cases that look inside the two types, with
    [j] for the joins of their parts.  It gives [None] where a type cannot
    be widened inside, and [tjoin] then tries whether one side is below the
    other. *)
Definition tjoin_core (j : bool -> ty -> ty -> option ty) (fr : bool) (a b : ty) : option ty :=
  match a, b with
  | TBot, _ => Some b
  | _, TBot => Some a
  | TUnion _ _, TUnion _ _ => None
  | TUnion a1 a2, _ =>
      match kind_of_ty b with
      | Some k => if ukind k a1 then option_map (fun z => TUnion z a2) (j fr a1 b)
                  else if ukind k a2 then option_map (fun z => TUnion a1 z) (j fr a2 b)
                  else Some (TUnion a b)
      | None => None
      end
  | _, TUnion b1 b2 =>
      match kind_of_ty a with
      | Some k => if ukind k b1 then option_map (fun z => TUnion z b2) (j fr a b1)
                  else if ukind k b2 then option_map (fun z => TUnion b1 z) (j fr a b2)
                  else Some (TUnion b a)
      | None => None
      end
  | TList x, TList y => if fr then option_map TList (j true x y) else None
  | TRec fs1 r1, TRec fs2 r2 => if fr then rjoin j fs1 r1 fs2 r2 else None
  | TEnum E xs, TEnum E' ys =>
      if ename_eqb E E' then option_map (TEnum E) (ejoin j fr (en_params E) xs ys)
      else Some (TUnion a b)
  | _, _ =>
      match kind_of_ty a, kind_of_ty b with
      | Some ka, Some kb => if kind_eqb ka kb then None else Some (TUnion a b)
      | _, _ => None
      end
  end.

(** [tjoin n fr a b]: the join of two slot types, [fr] when both slots are
    fresh.  [n] is fuel. *)
Fixpoint tjoin (n : nat) (fr : bool) (a b : ty) {struct n} : option ty :=
  match n with
  | 0 => None
  | S n' =>
  if ty_eqb a b then Some a else
  if is_mu a || is_mu b then ajoin fr a b else
  match tjoin_core (tjoin n') fr a b with
  | Some c => Some c
  | None => if le fr a b then Some b else if le fr b a then Some a else None
  end
  end.

Definition join_slot (p q : slot) : option slot :=
  let fr := match fst p, fst q with Dp, Dp => true | _, _ => false end in
  match tjoin (S (dsize (snd p) + dsize (snd q))) fr (snd p) (snd q) with
  | Some c => Some (if fr then Dp else Sh, c)
  | None => None
  end.

Fixpoint join_stack (s1 s2 : sty) : option sty :=
  match s1, s2 with
  | [], [] => Some []
  | p :: s1', q :: s2' =>
      match join_slot p q, join_stack s1' s2' with Some r, Some rs => Some (r :: rs) | _, _ => None end
  | _, _ => None
  end.
End Join.

Arguments ajoin : simpl never.

(** ** The join is an upper bound *)

Lemma jrel_refl fr x : jrel fr x x.
Proof. destruct fr; simpl; [apply rs_sub|]; apply s_refl. Qed.

Lemma jrel_bot fr x : jrel fr TBot x.
Proof. destruct fr; simpl; [apply rs_sub|]; apply s_bot. Qed.

Lemma jrel_unionr1 fr x a b : jrel fr x a -> jrel fr x (TUnion a b).
Proof. destruct fr; simpl; intros; [apply rs_unionr1 | apply s_unionr1]; auto. Qed.

Lemma jrel_unionr2 fr x a b : jrel fr x b -> jrel fr x (TUnion a b).
Proof. destruct fr; simpl; intros; [apply rs_unionr2 | apply s_unionr2]; auto. Qed.

Lemma jrel_unionl fr a b c : jrel fr a c -> jrel fr b c -> jrel fr (TUnion a b) c.
Proof. destruct fr; simpl; intros; [apply rs_unionl | apply s_unionl]; auto. Qed.

Lemma jrel_maybe fr x y : jrel fr x y -> jrel fr (TMaybe x) (TMaybe y).
Proof. destruct fr; simpl; intros; [apply rs_maybe | apply s_maybe]; auto. Qed.

Definition jok (j : bool -> ty -> ty -> option ty) : Prop :=
  forall fr x y z, j fr x y = Some z -> jrel fr x z /\ jrel fr y z.

Lemma fjoin_ub j f g h : jok j -> fjoin j f g = Some h -> frsub f h /\ frsub g h.
Proof.
  intros J E. destruct f, g; simpl in E; try (inversion E; subst; split; constructor; fail);
    try (destruct (j true t t0) as [z|] eqn:Ej; simpl in E; inversion E; subst;
         destruct (J _ _ _ _ Ej) as [H1 H2]; simpl in H1, H2);
    try (inversion E; subst).
  all: try (split; constructor; auto; fail).
  all: try (split; [eapply frs_opt | eapply frs_opt]; eauto; fail).
  all: try (split; [eapply frs_dict | eapply frs_dict]; eauto; fail).
  all: try (split; [apply frs_abs_opt | eapply frs_opt; [|apply rs_sub, s_refl]]; eauto; fail).
  all: try (split; [eapply frs_opt; [|apply rs_sub, s_refl] | apply frs_abs_opt]; eauto; fail).
  all: try (split; [apply frs_abs_dict | eapply frs_dict; [|apply rs_sub, s_refl]]; eauto; fail).
  all: try (split; [eapply frs_dict; [|apply rs_sub, s_refl] | apply frs_abs_dict]; eauto; fail).
  all: split; constructor.
Qed.

Lemma omapl_lookup (F : string -> option fstat) : forall keys fs,
  omapl (fun k => option_map (fun f => (k, f)) (F k)) keys = Some fs ->
  map fst fs = keys /\ forall k v, lookup k fs = Some v -> F k = Some v.
Proof.
  induction keys as [|k0 keys IH]; simpl; intros fs E.
  - inversion E; subst. split; [reflexivity | intros k v H; discriminate].
  - destruct (F k0) as [v0|] eqn:Ek; simpl in E; [|discriminate].
    destruct (omapl _ keys) as [fs'|] eqn:Er; [|discriminate]. inversion E; subst.
    destruct (IH fs' eq_refl) as [Hk Hl]. split; [simpl; f_equal; auto|].
    intros k v H. simpl in H. destruct (String.eqb_spec k k0); [subst; inversion H; subst; auto | auto].
Qed.

Lemma field_at_notin k fs (r : fstat) : ~ In k (map fst fs) -> field_at k fs r = r.
Proof.
  intros H. unfold field_at. induction fs as [|[k' f] fs IH]; simpl in *; auto.
  destruct (String.eqb_spec k k'); [subst; exfalso; apply H; left; reflexivity | apply IH; tauto].
Qed.

Lemma rjoin_ub j fs1 r1 fs2 r2 c : jok j -> rjoin j fs1 r1 fs2 r2 = Some c ->
  rsub (TRec fs1 r1) c /\ rsub (TRec fs2 r2) c.
Proof.
  intros J E. unfold rjoin in E.
  destruct (omapl _ _) as [fs|] eqn:Eo; [|discriminate].
  destruct (fjoin j r1 r2) as [r|] eqn:Er; [|discriminate]. inversion E; subst.
  apply omapl_lookup in Eo as [Hk Hl].
  assert (Hf : forall k, fjoin j (field_at k fs1 r1) (field_at k fs2 r2) = Some (field_at k fs r)).
  { intros k. unfold field_at at 3. destruct (lookup k fs) as [v|] eqn:Ev.
    - apply Hl in Ev. exact Ev.
    - apply lookup_none_iff in Ev. rewrite Hk in Ev.
      rewrite !field_at_notin; auto; intro Hc; apply Ev; apply in_or_app; auto. }
  split; apply rs_rec; intros k; destruct (fjoin_ub j _ _ _ J (Hf k)); auto.
Qed.

Lemma ejoin_ub j fr : jok j -> forall ps xs ys zs, ejoin j fr ps xs ys = Some zs ->
  if fr then vrsubs ps xs zs /\ vrsubs ps ys zs else vsubs ps xs zs /\ vsubs ps ys zs.
Proof.
  intros J. induction ps as [|p ps IH]; intros [|x xs] [|y ys] zs E; simpl in E; try discriminate.
  - inversion E; subst. destruct fr; split; constructor.
  - match type of E with context [match ?r with _ => _ end] =>
      destruct r as [z|] eqn:Ez; [|discriminate] end.
    destruct (ejoin j fr ps xs ys) as [zs'|] eqn:Ezs; [|discriminate]. inversion E; subst.
    specialize (IH _ _ _ Ezs).
    destruct fr, (p_fresh p) eqn:Fp; simpl in Ez.
    + destruct (J _ _ _ _ Ez) as [H1 H2]; simpl in H1, H2. destruct IH.
      split; apply vrs_fresh; auto.
    + destruct IH. destruct (p_var p) eqn:Vp.
      * destruct (J _ _ _ _ Ez) as [H1 H2]; simpl in H1, H2.
        split; apply vrs_sub; auto; rewrite Vp; exact H1 || exact H2.
      * destruct (ty_eqb x y) eqn:Exy; inversion Ez; subst. apply ty_eqb_true in Exy; subst.
        split; apply vrs_sub; auto; rewrite Vp; apply s_refl.
      * destruct (ty_eqb x y) eqn:Exy; inversion Ez; subst. apply ty_eqb_true in Exy; subst.
        split; apply vrs_sub; auto; rewrite Vp; apply teq_refl.
    + destruct IH. destruct (p_var p) eqn:Vp.
      * destruct (J _ _ _ _ Ez) as [H1 H2]; simpl in H1, H2. split; apply vs_co; auto.
      * destruct (ty_eqb x y) eqn:Exy; inversion Ez; subst. apply ty_eqb_true in Exy; subst.
        split; apply vs_contra; auto; apply s_refl.
      * destruct (ty_eqb x y) eqn:Exy; inversion Ez; subst. apply ty_eqb_true in Exy; subst.
        split; apply vs_inv; auto; apply s_refl.
    + destruct IH. destruct (p_var p) eqn:Vp.
      * destruct (J _ _ _ _ Ez) as [H1 H2]; simpl in H1, H2. split; apply vs_co; auto.
      * destruct (ty_eqb x y) eqn:Exy; inversion Ez; subst. apply ty_eqb_true in Exy; subst.
        split; apply vs_contra; auto; apply s_refl.
      * destruct (ty_eqb x y) eqn:Exy; inversion Ez; subst. apply ty_eqb_true in Exy; subst.
        split; apply vs_inv; auto; apply s_refl.
Qed.

Ltac jfin := first
  [ split; [apply jrel_unionr1, jrel_refl | apply jrel_unionr2, jrel_refl]
  | split; [apply jrel_unionr2, jrel_refl | apply jrel_unionr1, jrel_refl]
  | split; [apply jrel_bot | apply jrel_refl]
  | split; [apply jrel_refl | apply jrel_bot] ].

(** The weakest decision procedure: it never says yes.  Joins that meet no
    recursive type do not consult it. *)
Definition le_none : bool -> ty -> ty -> bool := fun _ _ _ => false.

Lemma le_none_ok fr a b : le_none fr a b = true -> jrel fr a b.
Proof. discriminate. Qed.

Section UB.
Variable le : bool -> ty -> ty -> bool.
(** The decision procedure is right when it says yes. *)
Hypothesis le_ok : forall fr a b, le fr a b = true -> jrel fr a b.

Lemma ajoin_ub fr a b c : ajoin le fr a b = Some c -> jrel fr a c /\ jrel fr b c.
Proof.
  unfold ajoin. intros E.
  destruct (ty_eqb a TBot) eqn:Ea.
  { apply ty_eqb_true in Ea; subst. injection E as <-. split; [apply jrel_bot | apply jrel_refl]. }
  destruct (ty_eqb b TBot) eqn:Eb.
  { apply ty_eqb_true in Eb; subst. injection E as <-. split; [apply jrel_refl | apply jrel_bot]. }
  destruct (le fr a b) eqn:L1.
  { injection E as <-. split; [apply le_ok; exact L1 | apply jrel_refl]. }
  destruct (le fr b a) eqn:L2.
  { injection E as <-. split; [apply jrel_refl | apply le_ok; exact L2]. }
  destruct (akinds a), (akinds b); try discriminate.
  destruct (kdisj _ _); [|discriminate]. injection E as <-.
  split; [apply jrel_unionr1 | apply jrel_unionr2]; apply jrel_refl.
Qed.

Lemma tjoin_core_ub j fr a b c : jok j -> tjoin_core j fr a b = Some c -> jrel fr a c /\ jrel fr b c.
Proof.
  intros J E. unfold tjoin_core in E.
  destruct a, b; try discriminate;
    try (injection E as <-; jfin; fail).
  (* the remaining cases: unions, Maybe, lists, shapes, enums *)
  all: repeat match goal with
         | E : (if ?x then _ else _) = Some _ |- _ => destruct x eqn:?
         | E : option_map _ ?x = Some _ |- _ =>
             let Ex := fresh "Ex" in destruct x eqn:Ex; simpl in E; [injection E as <- | discriminate]
         | E : (match ?x with Some _ => _ | None => _ end) = Some _ |- _ => destruct x eqn:?
         | E : Some _ = Some _ |- _ => injection E as <-
         | E : None = Some _ |- _ => discriminate
         end.
  all: try jfin.
  all: try (match goal with Ex : _ = Some _ |- _ => destruct (J _ _ _ _ Ex) as [H1 H2] end).
  all: try (split; [apply jrel_unionl; [apply jrel_unionr1; auto | apply jrel_unionr2, jrel_refl]
                   | apply jrel_unionr1; auto]; fail).
  all: try (split; [apply jrel_unionl; [apply jrel_unionr1, jrel_refl | apply jrel_unionr2; auto]
                   | apply jrel_unionr2; auto]; fail).
  all: try (split; [apply jrel_unionr1; auto
                   | apply jrel_unionl; [apply jrel_unionr1; auto | apply jrel_unionr2, jrel_refl]]; fail).
  all: try (split; [apply jrel_unionr2; auto
                   | apply jrel_unionl; [apply jrel_unionr1, jrel_refl | apply jrel_unionr2; auto]]; fail).
  all: try (split; apply jrel_maybe; auto; fail).
  all: try (simpl in *; split; apply rs_list; auto; fail).
  all: try (eapply rjoin_ub; eauto; fail).
  (* enums *)
  match goal with H : ename_eqb _ _ = true |- _ => apply ename_eqb_true in H; subst end.
  match goal with Ex : ejoin _ _ _ _ _ = Some _ |- _ => pose proof (ejoin_ub _ fr J _ _ _ _ Ex) as Hj end.
  destruct fr; simpl in *; destruct Hj; split; [apply rs_enum | apply rs_enum | apply s_enum | apply s_enum]; auto.
Qed.

Lemma tjoin_ub : forall n fr a b c, tjoin le n fr a b = Some c -> jrel fr a c /\ jrel fr b c.
Proof.
  induction n as [|n IH]; intros fr a b c E; simpl in E; [discriminate|].
  destruct (ty_eqb a b) eqn:Eq.
  { apply ty_eqb_true in Eq. subst. inversion E; subst. split; apply jrel_refl. }
  destruct (is_mu a || is_mu b) eqn:Mu; [eapply ajoin_ub; exact E|].
  assert (J : jok (tjoin le n)) by (intros ? ? ? ? H; eapply IH; eauto).
  destruct (tjoin_core (tjoin le n) fr a b) as [c'|] eqn:Ec.
  { injection E as <-. eapply tjoin_core_ub; eauto. }
  destruct (le fr a b) eqn:L1.
  { injection E as <-. split; [apply le_ok; exact L1 | apply jrel_refl]. }
  destruct (le fr b a) eqn:L2; [|discriminate].
  injection E as <-. split; [apply jrel_refl | apply le_ok; exact L2].
Qed.

Theorem join_slot_ub p q r : join_slot le p q = Some r -> slot_sub p r /\ slot_sub q r.
Proof.
  destruct p as [m1 a], q as [m2 b]. unfold join_slot. cbn [fst snd].
  destruct (tjoin le (S (dsize a + dsize b)) (match m1, m2 with Dp, Dp => true | _, _ => false end) a b)
    as [c|] eqn:E; intros H; [|discriminate].
  injection H as <-. apply tjoin_ub in E as [J1 J2].
  destruct m1, m2; simpl in J1, J2 |- *; split;
    first [ apply ss_sh; auto | apply ss_dp; auto | apply ss_forget; auto
          | apply ss_m_forget; [reflexivity | auto] ].
Qed.

Theorem join_stack_ub : forall s1 s2 s3, join_stack le s1 s2 = Some s3 -> ssub s1 s3 /\ ssub s2 s3.
Proof.
  induction s1 as [|p s1 IH]; intros [|q s2] s3 E; simpl in E; try discriminate.
  - inversion E; subst. split; constructor.
  - destruct (join_slot le p q) as [r|] eqn:Er; [|discriminate].
    destruct (join_stack le s1 s2) as [rs|] eqn:Ers; [|discriminate]. inversion E; subst.
    destruct (join_slot_ub _ _ _ Er), (IH _ _ Ers). split; constructor; auto.
Qed.

(** An [if] whose arms are joined checks in the core. *)
Corollary if_join sigs G B C R e1 e2 s s1 s2 s' :
  T sigs G B C R e1 s s1 -> T sigs G B C R e2 s s2 -> join_stack le s1 s2 = Some s' ->
  TW sigs G B C R (WIf e1 e2) ((Sh, TBool) :: s) s'.
Proof.
  intros H1 H2 J. destruct (join_stack_ub _ _ _ J) as [S1 S2].
  apply tw_if; eapply t_sub; eauto using ssub_refl.
Qed.
End UB.
