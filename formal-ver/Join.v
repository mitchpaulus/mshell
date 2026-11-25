(** * Branch joins.

    When the arms of an [if] or [match], or the elements of a list, leave
    different types in one slot, a checker computes their join.  [tjoinN]
    is the design's join of any number of types, written as a function,
    and [tjoinN_ub] proves it is above every one of them.  [join_slot] is
    the join of two slots, and [join_slot_ub] proves it an upper bound of
    both arms under slot subsumption, so an [if] whose arms are joined this
    way checks in the core.

    The join takes all the types at once.  A join of two folded over the
    arms depended on their order, because it is not a least upper bound:
    where two types cannot be widened inside, the only join of two is the
    one the other is below, so [a] and [b] could have no join while both
    were below a third arm [c].

    Choices the proof pins down:
    - the result is fresh only when every arm is fresh;
    - if one of the types is above all the others, it is the join ([<=],
      or the fresh retype when the slots are fresh).  So a quote that never
      returns joins with any quote with the same inputs;
    - otherwise the members of the types are grouped by kind, each group is
      joined, and the join is the union of those.  Two instances of
      different enums have different kinds;
    - widening *inside* a list, shape or fresh-covariant enum argument
      happens only when the arms are fresh; inside a covariant enum
      argument of shared values (a [Maybe], say) the inner join is itself a
      shared join;
    - members of one kind that cannot be widened inside (quotes, shared
      containers, invariant and contravariant arguments) join only to the
      one all the others are below;
    - a recursive type is never widened inside.  The other members of its
      group may be, and that join is the group's if the recursive type is
      below it ([join_rest]).  Widening inside two different recursive
      types would need a new recursive type for the result.

    The proofs say a join is above its arms.  They do not say that a join
    is found whenever one exists, nor that it is the same for every order
    of the arms; the Go tests check the second on random arms.

    The join is given the checker's decision procedure for [<=] and fresh
    retyping ([le]); the proofs assume only that it is right when it says
    yes ([le_ok]). *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Variance Typing Interp Invariant RtLemmas Validate Generic.

(** ** Decidable syntactic equality of types *)
Fixpoint ty_eqb (a b : ty) : bool :=
  match a, b with
  | TInt, TInt | TStr, TStr | TBool, TBool | TRune, TRune | TBot, TBot | TTop, TTop => true
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
  - intros b E; destruct b; simpl in E; congruence.
  - intros t IH b E; destruct b; simpl in E; try congruence. f_equal; auto.
  - intros fs r Hfs Hr b E; destruct b as [| | | | | | |fs2 r2| | | | | | |]; simpl in E; try congruence.
    apply andb_true_iff in E as [E1 E2]. f_equal; auto.
    eapply Forall2_eq; [| apply forallb2_Forall2; exact E1].
    eapply Forall_impl; [| exact Hfs]. intros [k f] Hf [k' f'] Hp. simpl in *.
    apply andb_true_iff in Hp as [Hk Hq]. apply String.eqb_eq in Hk. subst. f_equal. auto.
  - intros x y Hx Hy b E; destruct b; simpl in E; try congruence.
    apply andb_true_iff in E as [E1 E2]. f_equal; auto.
  - intros ins outs Hi Ho b E; destruct b as [| | | | | | | | |i2 o2| | | | |]; simpl in E; try congruence.
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
  | TInt | TStr | TBool | TRune | TBot | TTop | TParam _ | TVar _ | TRV _ => 1
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

(** Below: [<=] for shared slots, the fresh retype when both are fresh. *)
Definition jrel (fr : bool) (x y : ty) : Prop := if fr then rsub x y else sub x y.

(** ** One join for any number of types

    The join takes all the types at once ([tjoinN]), and the join of two is
    the join of a list of two.  A join of two folded over the arms depended
    on their order: where two types cannot be widened inside, the only join
    of two is the one the other is below, so [a] and [b] could have no join
    while both were below a third arm [c].

    - [⊥] and repeated types are dropped.
    - If one of the types is above all the others, it is the join.
    - Otherwise the members of the types (a union's members, or the type
      itself) are put in groups: two members are in one group when their
      kinds overlap, directly or through other members.  Each group is
      joined, and the join is the union of those.
    - A group of one member is that member.  A group of lists, of dicts or
      of instances of one enum is widened inside, the parts joined all at
      once; lists and dicts only when the slots are fresh.  Otherwise, or
      when that fails, the join is the member all the others are below, and
      else there is none. *)

(** The members of a union. *)
Fixpoint leaves (t : ty) : list ty :=
  match t with TUnion a b => leaves a ++ leaves b | _ => [t] end.

(** Do two types have a kind in common?  A type with no kinds known
    overlaps every type. *)
Definition overlap (a b : ty) : bool :=
  match akinds a, akinds b with
  | Some ka, Some kb => negb (kdisj ka kb)
  | _, _ => true
  end.

Fixpoint munion (ts : list ty) : ty :=
  match ts with
  | [] => TBot
  | [t] => t
  | t :: ts' => TUnion t (munion ts')
  end.

Fixpoint dedup (ts : list ty) : list ty :=
  match ts with
  | [] => []
  | t :: r => if existsb (ty_eqb t) r then dedup r else t :: dedup r
  end.

Definition is_bot (t : ty) : bool := match t with TBot => true | _ => false end.

Definition clean (ts : list ty) : list ty := dedup (filter (fun t => negb (is_bot t)) ts).

(** The first of the types that all the others are below. *)
Definition max_of (le : ty -> ty -> bool) (ts : list ty) : option ty :=
  find (fun c => forallb (fun t => ty_eqb t c || le t c) ts) ts.

(** The groups: [t] joins every group it overlaps into one. *)
Definition add_comp (t : ty) (cs : list (list ty)) : list (list ty) :=
  let (hit, miss) := partition (existsb (overlap t)) cs in (t :: concat hit) :: miss.

Definition components (ts : list ty) : list (list ty) := fold_right add_comp [] ts.

(** One label of several fresh shapes: unknown if it is unknown in one;
    absent if absent in all; present if present in all; deletable if
    deletable or absent in all; otherwise optional.  The types it has are
    joined all at once. *)
Definition ftys (fs : list fstat) : list ty :=
  flat_map (fun f => match f with FReq t | FOpt t | FDict t => [t] | FAbs | FOpen => [] end) fs.

Definition is_open (f : fstat) : bool := match f with FOpen => true | _ => false end.
Definition is_req (f : fstat) : bool := match f with FReq _ => true | _ => false end.
Definition is_dict_abs (f : fstat) : bool := match f with FDict _ | FAbs => true | _ => false end.

Definition fstatus (fs : list fstat) (z : ty) : fstat :=
  if forallb is_req fs then FReq z else if forallb is_dict_abs fs then FDict z else FOpt z.

Definition fjoinN (j : list ty -> option ty) (fs : list fstat) : option fstat :=
  if existsb is_open fs then Some FOpen else
  match ftys fs with
  | [] => Some FAbs
  | ts => option_map (fstatus fs) (j ts)
  end.

Definition rjoinN (j : list ty -> option ty) (rs : list (list (label * fstat) * fstat)) : option ty :=
  match omapl (fun k => option_map (fun f => (k, f)) (fjoinN j (map (fun r => field_at k (fst r) (snd r)) rs)))
              (flat_map (fun r => map fst (fst r)) rs), fjoinN j (map snd rs) with
  | Some fs, Some r => Some (TRec fs r)
  | _, _ => None
  end.

(** Enum arguments: a fresh-covariant argument of fresh values widens; a
    covariant one joins as shared values; any other must be the same in
    all. *)
Definition all_eq (ts : list ty) : option ty :=
  match ts with
  | [] => None
  | x :: r => if forallb (ty_eqb x) r then Some x else None
  end.

Definition uncons (xs : list ty) : option (ty * list ty) :=
  match xs with x :: r => Some (x, r) | [] => None end.

Definition ecol (j : bool -> list ty -> option ty) (fr : bool) (p : eparam) (col : list ty) : option ty :=
  if fr && p_fresh p then j true col
  else match p_var p with VCo => j false col | _ => all_eq col end.

Fixpoint ejoinN (j : bool -> list ty -> option ty) (fr : bool) (ps : list eparam) (xss : list (list ty))
  : option (list ty) :=
  match ps with
  | [] => if forallb (fun xs => match xs with [] => true | _ => false end) xss then Some [] else None
  | p :: ps' =>
      match omapl uncons xss with
      | Some hts =>
          match ecol j fr p (map fst hts), ejoinN j fr ps' (map snd hts) with
          | Some z, Some zs => Some (z :: zs)
          | _, _ => None
          end
      | None => None
      end
  end.

Definition as_list (t : ty) : option ty := match t with TList x => Some x | _ => None end.
Definition as_rec (t : ty) : option (list (label * fstat) * fstat) :=
  match t with TRec fs r => Some (fs, r) | _ => None end.
Definition as_enum (E : ename) (t : ty) : option (list ty) :=
  match t with TEnum E' xs => if ename_eqb E E' then Some xs else None | _ => None end.

(** A group widened inside: every member a list, a dict, or an instance of
    one enum. *)
Definition structural (j : bool -> list ty -> option ty) (fr : bool) (c : list ty) : option ty :=
  match c with
  | TList _ :: _ =>
      if fr then match omapl as_list c with Some xs => option_map TList (j true xs) | None => None end
      else None
  | TRec _ _ :: _ =>
      if fr then match omapl as_rec c with Some rs => rjoinN (j true) rs | None => None end
      else None
  | TEnum E _ :: _ =>
      match omapl (as_enum E) c with
      | Some xss => option_map (TEnum E) (ejoinN j fr (en_params E) xss)
      | None => None
      end
  | _ => None
  end.

Section Join.
(** The checker's decision procedure for [jrel]. *)
Variable le : bool -> ty -> ty -> bool.

(** A recursive type is never widened inside, but the other members of
    its group may be: when no member is above the rest, the others are
    joined, and that is the join if every recursive type is below it. *)
Definition join_rest (j : bool -> list ty -> option ty) (fr : bool) (c : list ty) : option ty :=
  match filter is_mu c, filter (fun t => negb (is_mu t)) c with
  | a :: als, o :: others =>
      match j fr (o :: others) with
      | Some r => if forallb (fun x => le fr x r) (a :: als) then Some r else None
      | None => None
      end
  | _, _ => None
  end.

Definition join_comp (j : bool -> list ty -> option ty) (fr : bool) (c : list ty) : option ty :=
  match c with
  | [t] => Some t
  | _ => match structural j fr c with
         | Some r => Some r
         | None => match max_of (le fr) c with
                   | Some m => Some m
                   | None => join_rest j fr c
                   end
         end
  end.

(** Two or more different types, none [⊥]. *)
Definition join_many (j : bool -> list ty -> option ty) (fr : bool) (ts : list ty) : option ty :=
  match max_of (le fr) ts with
  | Some c => Some c
  | None =>
      match omapl (join_comp j fr) (components (dedup (flat_map leaves ts))) with
      | Some rs => Some (munion rs)
      | None => None
      end
  end.

(** [tjoinN n fr ts]: the join of the types [ts] of slots that are all
    fresh ([fr]) or not.  [n] is fuel. *)
Fixpoint tjoinN (n : nat) (fr : bool) (ts : list ty) {struct n} : option ty :=
  match n with
  | 0 => None
  | S n' =>
      match clean ts with
      | [] => Some TBot
      | [t] => Some t
      | ts' => join_many (tjoinN n') fr ts'
      end
  end.

(** The join of two slot types. *)
Definition tjoin (n : nat) (fr : bool) (a b : ty) : option ty := tjoinN n fr [a; b].

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

Definition jokN (j : bool -> list ty -> option ty) : Prop :=
  forall fr ts z, j fr ts = Some z -> Forall (fun t => jrel fr t z) ts.

Lemma field_at_notin k fs (r : fstat) : ~ In k (map fst fs) -> field_at k fs r = r.
Proof.
  intros H. unfold field_at. induction fs as [|[k' f] fs IH]; simpl in *; auto.
  destruct (String.eqb_spec k k'); [subst; exfalso; apply H; left; reflexivity | apply IH; tauto].
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

Lemma omapl_in {A B} (f : A -> option B) : forall l l', omapl f l = Some l' ->
  forall x, In x l -> exists y, In y l' /\ f x = Some y.
Proof.
  induction l as [|x0 l IH]; simpl; intros l' E x I; [contradiction|].
  destruct (f x0) as [y0|] eqn:Ex; [|discriminate].
  destruct (omapl f l) as [ys|] eqn:El; [|discriminate]. inversion E; subst.
  destruct I as [<-|I]; [exists y0; simpl; auto|].
  destruct (IH ys eq_refl x I) as (y & Iy & Ey). exists y. simpl. auto.
Qed.

(** Every member of a union reaches the join directly, so no transitivity
    of the fresh retype is needed. *)
Lemma munion_in fr x : forall ts t, In t ts -> jrel fr x t -> jrel fr x (munion ts).
Proof.
  induction ts as [|t0 ts IH]; simpl; intros t I H; [contradiction|].
  destruct ts as [|t1 ts'].
  - destruct I as [<-|[]]. exact H.
  - destruct I as [<-|I]; [apply jrel_unionr1; exact H | apply jrel_unionr2; eapply IH; eauto].
Qed.

Lemma leaves_ub fr c : forall a, (forall l, In l (leaves a) -> jrel fr l c) -> jrel fr a c.
Proof.
  induction a; simpl; intros H; try (apply H; left; reflexivity).
  apply jrel_unionl; [apply IHa1 | apply IHa2]; intros l I; apply H, in_or_app; auto.
Qed.

Lemma dedup_in t : forall ts, In t ts -> In t (dedup ts).
Proof.
  induction ts as [|t0 r IH]; simpl; intros I; [contradiction|].
  destruct (existsb (ty_eqb t0) r) eqn:Ex.
  - destruct I as [<-|I]; [|auto]. apply existsb_exists in Ex as (x & Ix & Ex).
    apply ty_eqb_true in Ex. subst. auto.
  - destruct I as [<-|I]; [left; reflexivity | right; auto].
Qed.

Lemma components_in t : forall ts, In t ts -> exists c, In c (components ts) /\ In t c.
Proof.
  induction ts as [|a ts IH]; simpl; intros I; [contradiction|].
  unfold add_comp. destruct (partition (existsb (overlap a)) (components ts)) as [hit miss] eqn:Ep.
  destruct I as [<-|I].
  - exists (a :: concat hit). simpl. auto.
  - destruct (IH I) as (c & Ic & It).
    apply (elements_in_partition _ _ Ep) in Ic as [Ic|Ic].
    + exists (a :: concat hit). split; [left; reflexivity|]. right. apply in_concat. eauto.
    + exists c. split; [right; exact Ic | exact It].
Qed.

Lemma max_of_ub (le0 : ty -> ty -> bool) (R : ty -> ty -> Prop) ts c :
  (forall a b, le0 a b = true -> R a b) -> (forall a, R a a) ->
  max_of le0 ts = Some c -> Forall (fun t => R t c) ts.
Proof.
  intros L Rf E. apply find_some in E as [_ E]. rewrite forallb_forall in E.
  apply Forall_forall. intros t I. specialize (E t I). apply orb_true_iff in E as [E|E].
  - apply ty_eqb_true in E. subst. apply Rf.
  - apply L, E.
Qed.

Lemma ftys_in t : forall fs f, In f fs -> (f = FReq t \/ f = FOpt t \/ f = FDict t) -> In t (ftys fs).
Proof.
  intros fs f I H. unfold ftys. apply in_flat_map. exists f. split; [exact I|].
  destruct H as [->|[->| ->]]; left; reflexivity.
Qed.

Lemma fjoinN_ub (R : ty -> ty -> Prop) j fs h :
  (forall ts z, j ts = Some z -> Forall (fun x => R x z) ts) ->
  fjoinN j fs = Some h -> Forall (fun f => frsubR R f h) fs.
Proof.
  intros J E. unfold fjoinN in E. apply Forall_forall. intros f I.
  destruct (existsb is_open fs) eqn:Op.
  { inversion E; subst. apply frs_open. }
  assert (No : is_open f = false).
  { destruct (is_open f) eqn:O; [|reflexivity].
    assert (X : existsb is_open fs = true) by (apply existsb_exists; eauto). congruence. }
  destruct (ftys fs) as [|t0 tl] eqn:Et.
  { inversion E; subst. destruct f as [t|t|t| |]; try discriminate; try apply frs_abs;
      exfalso; assert (X : In t (ftys fs)) by (eapply ftys_in; eauto); rewrite Et in X; exact X. }
  destruct (j (t0 :: tl)) as [z|] eqn:Ej; simpl in E; [|discriminate]. inversion E; subst. clear E.
  pose proof (J _ _ Ej) as F. rewrite <- Et in F. rewrite Forall_forall in F.
  unfold fstatus.
  destruct (forallb is_req fs) eqn:A.
  { rewrite forallb_forall in A. specialize (A f I). destruct f; try discriminate.
    apply frs_req. apply F. eapply ftys_in; eauto. }
  destruct (forallb is_dict_abs fs) eqn:B.
  { rewrite forallb_forall in B. specialize (B f I). destruct f as [t|t|t| |]; try discriminate.
    - eapply frs_dict; [right; right; reflexivity|]. apply F. eapply ftys_in; eauto.
    - apply frs_abs_dict. }
  destruct f as [t|t|t| |]; try discriminate.
  - eapply frs_opt; [left; reflexivity|]. apply F. eapply ftys_in; eauto.
  - eapply frs_opt; [right; left; reflexivity|]. apply F. eapply ftys_in; eauto.
  - eapply frs_opt; [right; right; reflexivity|]. apply F. eapply ftys_in; eauto.
  - apply frs_abs_opt.
Qed.

Lemma rjoinN_ub j rs c :
  (forall ts z, j ts = Some z -> Forall (fun x => rsub x z) ts) ->
  rjoinN j rs = Some c -> forall fs1 r1, In (fs1, r1) rs -> rsub (TRec fs1 r1) c.
Proof.
  intros J E fs1 r1 I. unfold rjoinN in E.
  destruct (omapl _ _) as [fs|] eqn:Eo; [|discriminate].
  destruct (fjoinN j (map snd rs)) as [r|] eqn:Er; [|discriminate]. inversion E; subst.
  apply omapl_lookup in Eo as [Hk Hl].
  assert (Hf : forall k, fjoinN j (map (fun r => field_at k (fst r) (snd r)) rs) = Some (field_at k fs r)).
  { intros k. unfold field_at at 2. destruct (lookup k fs) as [v|] eqn:Ev.
    - apply Hl in Ev. exact Ev.
    - apply lookup_none_iff in Ev. rewrite Hk in Ev.
      replace (map (fun r0 => field_at k (fst r0) (snd r0)) rs) with (map snd rs); [exact Er|].
      apply map_ext_in. intros [fs0 r0] I0. simpl. symmetry. apply field_at_notin.
      intro Hc. apply Ev. apply in_flat_map. exists (fs0, r0). auto. }
  apply rs_rec. intros k. pose proof (fjoinN_ub rsub j _ _ J (Hf k)) as F.
  rewrite Forall_forall in F. apply (F (field_at k fs1 r1)).
  apply in_map_iff. exists (fs1, r1). auto.
Qed.

Lemma all_eq_in ts z : all_eq ts = Some z -> forall x, In x ts -> x = z.
Proof.
  destruct ts as [|x0 r]; simpl; [discriminate|].
  destruct (forallb (ty_eqb x0) r) eqn:F; [|discriminate]. intros E x I. inversion E; subst.
  destruct I as [<-|I]; [reflexivity|]. rewrite forallb_forall in F. symmetry. apply ty_eqb_true, F, I.
Qed.

Lemma ejoinN_ub j fr : jokN j -> forall ps xss zs, ejoinN j fr ps xss = Some zs ->
  forall xs, In xs xss -> if fr then vrsubs ps xs zs else vsubs ps xs zs.
Proof.
  intros J. induction ps as [|p ps IH]; intros xss zs E xs I; simpl in E.
  - destruct (forallb _ xss) eqn:F; [|discriminate]. inversion E; subst.
    rewrite forallb_forall in F. specialize (F xs I). destruct xs; [|discriminate].
    destruct fr; constructor.
  - destruct (omapl uncons xss) as [hts|] eqn:Eo; [|discriminate].
    destruct (ecol j fr p (map fst hts)) as [z|] eqn:Ez; [|discriminate].
    destruct (ejoinN j fr ps (map snd hts)) as [zs'|] eqn:Ezs; [|discriminate]. inversion E; subst.
    destruct (omapl_in _ _ _ Eo xs I) as ([x r] & Ih & Eu).
    destruct xs as [|x' r']; simpl in Eu; [discriminate|]. inversion Eu; subst.
    assert (Ir : In r (map snd hts)) by (apply in_map_iff; exists (x, r); auto).
    assert (Ix : In x (map fst hts)) by (apply in_map_iff; exists (x, r); auto).
    specialize (IH _ _ Ezs r Ir). unfold ecol in Ez.
    destruct fr, (p_fresh p) eqn:Fp; simpl in Ez.
    + pose proof (J _ _ _ Ez) as F. rewrite Forall_forall in F. apply vrs_fresh; auto. exact (F x Ix).
    + destruct (p_var p) eqn:Vp.
      * pose proof (J _ _ _ Ez) as F. rewrite Forall_forall in F.
        apply vrs_sub; auto. rewrite Vp. exact (F x Ix).
      * rewrite (all_eq_in _ _ Ez x Ix). apply vrs_sub; auto. rewrite Vp. apply s_refl.
      * rewrite (all_eq_in _ _ Ez x Ix). apply vrs_sub; auto. rewrite Vp. apply teq_refl.
    + destruct (p_var p) eqn:Vp.
      * pose proof (J _ _ _ Ez) as F. rewrite Forall_forall in F. apply vs_co; auto. exact (F x Ix).
      * rewrite (all_eq_in _ _ Ez x Ix). apply vs_contra; auto. apply s_refl.
      * rewrite (all_eq_in _ _ Ez x Ix). apply vs_inv; auto; apply s_refl.
    + destruct (p_var p) eqn:Vp.
      * pose proof (J _ _ _ Ez) as F. rewrite Forall_forall in F. apply vs_co; auto. exact (F x Ix).
      * rewrite (all_eq_in _ _ Ez x Ix). apply vs_contra; auto. apply s_refl.
      * rewrite (all_eq_in _ _ Ez x Ix). apply vs_inv; auto; apply s_refl.
Qed.

Lemma structural_ub j fr c r : jokN j -> structural j fr c = Some r -> Forall (fun t => jrel fr t r) c.
Proof.
  intros J E. apply Forall_forall. intros t I. unfold structural in E.
  destruct c as [|h c']; [discriminate|].
  destruct h as [| | | | | | x0 | fs0 r0 | u0 v0 | i0 o0 | E0 g0 | p0 | w0 | t0 | n0]; try discriminate.
  - destruct fr; [|discriminate].
    destruct (omapl as_list _) as [xs|] eqn:Eo; [|discriminate].
    destruct (j true xs) as [z|] eqn:Ej; simpl in E; [|discriminate]. inversion E; subst.
    destruct (omapl_in _ _ _ Eo t I) as (x & Ix & Ex). destruct t; try discriminate. inversion Ex; subst.
    pose proof (J _ _ _ Ej) as F. rewrite Forall_forall in F. simpl. apply rs_list. exact (F x Ix).
  - destruct fr; [|discriminate].
    destruct (omapl as_rec _) as [rs|] eqn:Eo; [|discriminate].
    destruct (omapl_in _ _ _ Eo t I) as ([fs1 r1] & Ix & Ex). destruct t; try discriminate. inversion Ex; subst.
    simpl. eapply rjoinN_ub; [| exact E | exact Ix]. intros ts z Ez. exact (J true ts z Ez).
  - destruct (omapl (as_enum E0) _) as [xss|] eqn:Eo; [|discriminate].
    destruct (ejoinN j fr (en_params E0) xss) as [zs|] eqn:Ej; simpl in E; [|discriminate]. inversion E; subst.
    destruct (omapl_in _ _ _ Eo t I) as (xs & Ix & Ex).
    destruct t as [| | | | | | x1 | fs1 r1 | u1 v1 | i1 o1 | E1 g1 | p1 | w1 | t1 | n1]; try discriminate.
    simpl in Ex. destruct (ename_eqb E0 E1) eqn:En; [|discriminate]. inversion Ex; subst.
    apply ename_eqb_true in En. subst.
    pose proof (ejoinN_ub j fr J _ _ _ Ej xs Ix) as H.
    destruct fr; simpl in *; [apply rs_enum | apply s_enum]; exact H.
Qed.

(** The weakest decision procedure: it never says yes.  Joins that meet no
    recursive type do not consult it. *)
Definition le_none : bool -> ty -> ty -> bool := fun _ _ _ => false.

Lemma le_none_ok fr a b : le_none fr a b = true -> jrel fr a b.
Proof. discriminate. Qed.

Section UB.
Variable le : bool -> ty -> ty -> bool.
(** The decision procedure is right when it says yes. *)
Hypothesis le_ok : forall fr a b, le fr a b = true -> jrel fr a b.

Lemma join_rest_ub j fr c r : jokN j -> join_rest le j fr c = Some r -> Forall (fun t => jrel fr t r) c.
Proof.
  intros J E. unfold join_rest in E.
  destruct (filter is_mu c) as [|a als] eqn:Ea; [discriminate|].
  destruct (filter (fun t => negb (is_mu t)) c) as [|o others] eqn:Eo; [discriminate|].
  destruct (j fr (o :: others)) as [r0|] eqn:Ej; [|discriminate].
  destruct (forallb (fun x => le fr x r0) (a :: als)) eqn:Al; [|discriminate]. inversion E; subst.
  pose proof (J _ _ _ Ej) as F. rewrite Forall_forall in F. rewrite forallb_forall in Al.
  apply Forall_forall. intros t I. destruct (is_mu t) eqn:Mt.
  - apply le_ok, Al. rewrite <- Ea. apply filter_In. auto.
  - apply F. rewrite <- Eo. apply filter_In. rewrite Mt. auto.
Qed.

Lemma join_comp_ub j fr c r : jokN j -> join_comp le j fr c = Some r -> Forall (fun t => jrel fr t r) c.
Proof.
  intros J E. unfold join_comp in E.
  assert (G : match structural j fr c with
              | Some r0 => Some r0
              | None => match max_of (le fr) c with Some m => Some m | None => join_rest le j fr c end
              end = Some r -> Forall (fun t => jrel fr t r) c).
  { destruct (structural j fr c) as [r0|] eqn:Es; intros E'.
    - inversion E'; subst. eapply structural_ub; eauto.
    - destruct (max_of (le fr) c) as [m|] eqn:Em.
      + inversion E'; subst. eapply max_of_ub; [apply le_ok | apply jrel_refl | exact Em].
      + eapply join_rest_ub; eauto. }
  destruct c as [|t [|t' c']]; auto.
  inversion E; subst. constructor; [apply jrel_refl | constructor].
Qed.

Lemma join_many_ub j fr ts c : jokN j -> join_many le j fr ts = Some c -> Forall (fun t => jrel fr t c) ts.
Proof.
  intros J E. unfold join_many in E.
  destruct (max_of (le fr) ts) as [m|] eqn:Em.
  { inversion E; subst. eapply max_of_ub; [apply le_ok | apply jrel_refl | exact Em]. }
  destruct (omapl _ _) as [rs|] eqn:Eo; [|discriminate]. inversion E; subst.
  apply Forall_forall. intros t I. apply leaves_ub. intros l Il.
  assert (Ia : In l (dedup (flat_map leaves ts))) by (apply dedup_in, in_flat_map; eauto).
  destruct (components_in _ _ Ia) as (c0 & Ic & Ilc).
  destruct (omapl_in _ _ _ Eo c0 Ic) as (r & Ir & Er).
  pose proof (join_comp_ub _ _ _ _ J Er) as F. rewrite Forall_forall in F.
  eapply munion_in; [exact Ir | exact (F l Ilc)].
Qed.

Theorem tjoinN_ub : forall n fr ts c, tjoinN le n fr ts = Some c -> Forall (fun t => jrel fr t c) ts.
Proof.
  induction n as [|n IH]; intros fr ts c E; simpl in E; [discriminate|].
  assert (J : jokN (tjoinN le n)) by (intros ? ? ? H; eapply IH; eauto).
  apply Forall_forall. intros t I.
  destruct (is_bot t) eqn:Bt.
  { destruct t; try discriminate. apply jrel_bot. }
  assert (Ic : In t (clean ts)).
  { unfold clean. apply dedup_in, filter_In. split; [exact I | rewrite Bt; reflexivity]. }
  destruct (clean ts) as [|t0 [|t1 l]] eqn:Ec.
  - contradiction.
  - inversion E; subst. destruct Ic as [<-|[]]. apply jrel_refl.
  - pose proof (join_many_ub _ _ _ _ J E) as F. rewrite Forall_forall in F. exact (F t Ic).
Qed.

Lemma tjoin_ub : forall n fr a b c, tjoin le n fr a b = Some c -> jrel fr a c /\ jrel fr b c.
Proof.
  intros n fr a b c E. pose proof (tjoinN_ub _ _ _ _ E) as F.
  inversion F as [|? ? Ha F']; subst. inversion F' as [|? ? Hb ?]; subst. auto.
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

(** ** The checker's join: a new arm takes a shared arm's type

    When [join_slot] finds no join, the checker tries one more thing: if one
    arm is new or partly new and the other is not new, and the new value can
    be retyped to the other arm's type, the join is that type, shared.  A new
    [{values: [...], binaries: true}] and a stored [CompletionResult] give
    [CompletionResult].  The arm is retyped ([ss_dp], or [ss_m] position by
    position) and then forgotten ([ss_forget], [ss_m_forget]): two
    subsumption steps, which [t_sub] allows one after the other, so the
    result is not a [slot_sub] of the arm but still an upper bound of it.

    [rt] is the checker's procedure for that retype; like [le], it is
    assumed right only when it says yes.  For a partly new value it may go
    through a type [b] below the target, such as the member of a union with
    the value's kind. *)
Section Join2.
Variable le : bool -> ty -> ty -> bool.
Hypothesis le_ok : forall fr a b, le fr a b = true -> jrel fr a b.
Variable rt : mark -> ty -> ty -> bool.
Hypothesis rt_ok : forall m a c, rt m a c = true ->
  (m = Dp /\ rsub a c) \/
  (partial m = true /\ ((exists b, msub m a b /\ sub b c) \/ sub a c)).

Definition is_dp (m : mark) : bool := match m with Dp => true | _ => false end.
Definition newish (m : mark) : bool := match m with Sh => false | _ => true end.

Definition join_slot2 (p q : slot) : option slot :=
  match join_slot le p q with
  | Some r => Some r
  | None =>
      if newish (fst p) && negb (is_dp (fst q)) && rt (fst p) (snd p) (snd q) then Some (Sh, snd q)
      else if newish (fst q) && negb (is_dp (fst p)) && rt (fst q) (snd q) (snd p) then Some (Sh, snd p)
      else None
  end.

Fixpoint join_stack2 (s1 s2 : sty) : option sty :=
  match s1, s2 with
  | [], [] => Some []
  | p :: s1', q :: s2' =>
      match join_slot2 p q, join_stack2 s1' s2' with Some r, Some rs => Some (r :: rs) | _, _ => None end
  | _, _ => None
  end.

(** A slot that is not new is committed at its own type in one step. *)
Lemma forget_own m t : is_dp m = false -> slot_sub (m, t) (Sh, t).
Proof.
  destruct m; simpl; intros H; try discriminate.
  - apply slot_sub_refl.
  - apply ss_m_forget; [reflexivity | apply s_refl].
  - apply ss_m_forget; [reflexivity | apply s_refl].
Qed.

(** What [rt] accepts is two subsumption steps. *)
Lemma rt_two_steps m a c : newish m = true -> rt m a c = true ->
  exists x, slot_sub (m, a) x /\ slot_sub x (Sh, c).
Proof.
  intros Nm Hr. destruct (rt_ok _ _ _ Hr) as [[-> Hs] | [Hp [(b & Hm & Hb) | Hs]]].
  - exists (Dp, c). split; [apply ss_dp; exact Hs | apply ss_forget, s_refl].
  - exists (m, b). split; [apply ss_m; auto | apply ss_m_forget; auto].
  - exists (Sh, c). split; [apply ss_m_forget; auto | apply slot_sub_refl].
Qed.

(** The join is reached from each arm in at most two steps. *)
Theorem join_slot2_ub p q r : join_slot2 p q = Some r ->
  exists p' q', slot_sub p p' /\ slot_sub p' r /\ slot_sub q q' /\ slot_sub q' r.
Proof.
  unfold join_slot2. destruct (join_slot le p q) as [r0|] eqn:J.
  { intros E. injection E as <-. destruct (join_slot_ub le le_ok _ _ _ J) as [Hp Hq].
    exists r0, r0. repeat split; auto using slot_sub_refl. }
  destruct p as [mp tp], q as [mq tq]; simpl.
  destruct (newish mp) eqn:Np, (is_dp mq) eqn:Dq, (rt mp tp tq) eqn:Rp; simpl;
    try (intros E; injection E as <-;
         destruct (rt_two_steps _ _ _ Np Rp) as (x & H1 & H2);
         exists x, (Sh, tq); repeat split; auto using slot_sub_refl, forget_own; fail);
  destruct (newish mq) eqn:Nq, (is_dp mp) eqn:Dp', (rt mq tq tp) eqn:Rq; simpl; intros E; try discriminate;
  injection E as <-; destruct (rt_two_steps _ _ _ Nq Rq) as (x & H1 & H2);
  exists (Sh, tp), x; repeat split; auto using slot_sub_refl, forget_own.
Qed.

Theorem join_stack2_ub : forall s1 s2 s3, join_stack2 s1 s2 = Some s3 ->
  exists s1' s2', ssub s1 s1' /\ ssub s1' s3 /\ ssub s2 s2' /\ ssub s2' s3.
Proof.
  induction s1 as [|p s1 IH]; intros [|q s2] s3 E; simpl in E; try discriminate.
  - inversion E; subst. exists [], []. repeat split; constructor.
  - destruct (join_slot2 p q) as [r|] eqn:Er; [|discriminate].
    destruct (join_stack2 s1 s2) as [rs|] eqn:Ers; [|discriminate]. inversion E; subst.
    destruct (join_slot2_ub _ _ _ Er) as (p' & q' & A1 & A2 & A3 & A4).
    destruct (IH _ _ Ers) as (s1' & s2' & B1 & B2 & B3 & B4).
    exists (p' :: s1'), (q' :: s2'). repeat split; constructor; auto.
Qed.

(** An [if] whose arms are joined this way checks in the core: each arm is
    brought to the join by subsumption twice. *)
Corollary if_join2 sigs G B C R e1 e2 s s1 s2 s' :
  T sigs G B C R e1 s s1 -> T sigs G B C R e2 s s2 -> join_stack2 s1 s2 = Some s' ->
  TW sigs G B C R (WIf e1 e2) ((Sh, TBool) :: s) s'.
Proof.
  intros H1 H2 J. destruct (join_stack2_ub _ _ _ J) as (s1' & s2' & A1 & A2 & A3 & A4).
  apply tw_if; eapply t_sub; [apply ssub_refl | eapply t_sub; [apply ssub_refl | eassumption | eassumption] | eassumption
                             | apply ssub_refl | eapply t_sub; [apply ssub_refl | eassumption | eassumption] | eassumption].
Qed.
End Join2.
