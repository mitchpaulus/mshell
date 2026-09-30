(** * The checker's decision procedures for [<=] and fresh retyping.

    [sub] and [rsub] (Subtyping.v) are greatest fixed points: relations on
    infinite trees.  The checker decides them with a set of assumed pairs
    (design doc, Aliases): to compare [a] and [b], if the pair is already
    assumed it holds; otherwise assume it and compare one level.  This
    file writes that procedure as a function, [subq] for [<=] and [rsubq]
    for fresh retyping, and proves each right whenever it answers yes
    ([subq_sound], [rsubq_sound]).  That discharges the hypothesis [le_ok]
    of the branch join (Join.v; [le_alg_ok], [join_slot_ub_alg]).
    Completeness is not proved: an answer of no is always safe.

    The procedure the proof covers:

    - *The set is threaded through the whole query.*  A pair assumed while
      comparing one child stays assumed for the next child, so no pair is
      decided twice in one query.  When a union alternative fails, the set
      goes back to what it was before the alternative ([step], the case
      [_, TUnion x y]).  This is the efficient form of the algorithm
      (TAPL 21.12); keeping only the current path is also sound, but can
      take exponential time.

    - *Assumptions are consulted and recorded only at the children of a
      type constructor* ([chk]): a list's element, a field, a quote's
      inputs and outputs, an enum argument.  Union and unfolding steps stay
      inside one level ([step]) and never look at the set.  Then every
      cycle through an assumption passes a constructor, whether or not the
      aliases are guarded, and the procedure is sound for every type.
      Guardedness is what makes it terminate; here fuel runs out instead,
      which answers no.  A procedure that consults the set at every step
      decides a different relation on unguarded types (H13;
      [every_step_accepts]).

    - *One set per relation.*  [rlvl] keeps its own set for fresh
      retyping.  Where it needs [<=] (a quote, or an enum argument that is
      not fresh-covariant) it starts a new [<=] query with an empty set
      ([subq]).  Sharing one set is H12 ([mixed_alg_accepts]).

    - *Caching.*  [C] and [Cr] are pairs already known to hold; the proofs
      need only that ([C_ok], [Cr_ok]).  After a top-level query says yes,
      every pair in its final set holds ([subq_set_sound]), so all of them
      may be cached.  After a no, none may: a pair can be accepted under an
      assumption that the query then refutes ([cache_early]).

    The proof is the usual one for assumption sets (Brandt and Henglein
    1998; TAPL 21.9).  The invariant [Inv A A'] says the query only adds
    pairs to the set, and each pair it adds holds one level down, with
    its children in the final set or already known to hold.  At the end
    the whole set is closed under one level, so it is below the greatest
    fixed point. *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Variance Typing Interp Invariant RtLemmas Validate Generic Join.
From MshellCore Require Recursive.

(** ** Assumption sets *)

Definition aset := list (ty * ty).

Definition assumed (A : aset) (a b : ty) : bool :=
  existsb (fun p => ty_eqb (fst p) a && ty_eqb (snd p) b) A.

Lemma assumed_in A a b : assumed A a b = true -> In (a, b) A.
Proof.
  unfold assumed. intros E. apply existsb_exists in E as [[x y] [Hin E]]. simpl in E.
  apply andb_true_iff in E as [E1 E2]. apply ty_eqb_true in E1, E2. subst. exact Hin.
Qed.

(** Continue with the set [A] when [o] succeeded with it.  [f] runs only
    then. *)
Definition obind (o : option aset) (f : aset -> option aset) : option aset :=
  match o with Some A => f A | None => None end.

Lemma obind_some o f A' : obind o f = Some A' -> exists A1, o = Some A1 /\ f A1 = Some A'.
Proof. destruct o as [A1|]; simpl; [eauto | discriminate]. Qed.

(** ** One level, shared by both procedures

    Each helper is given [c], the check of one child pair, which threads
    the set.  The labels of a dict-kinded type are the labels either side
    declares, plus one check of the two remainders, which stands for every
    other label. *)

Fixpoint tall {X : Type} (g : aset -> X -> option aset) (l : list X) (A : aset) : option aset :=
  match l with
  | [] => Some A
  | x :: l' => obind (g A x) (tall g l')
  end.

Fixpoint tall2 (c : aset -> ty -> ty -> option aset) (l1 l2 : list ty) (A : aset) : option aset :=
  match l1, l2 with
  | [], [] => Some A
  | x :: l1', y :: l2' => obind (c A x y) (tall2 c l1' l2')
  | _, _ => None
  end.

(** Both directions: an invariant position. *)
Definition both (c : aset -> ty -> ty -> option aset) (A : aset) (x y : ty) : option aset :=
  obind (c A x y) (fun A1 => c A1 y x).

Definition labels (fs1 fs2 : list (label * fstat)) : list label := map fst fs1 ++ map fst fs2.

Definition trec (fc : aset -> fstat -> fstat -> option aset)
                (fs1 : list (label * fstat)) (r1 : fstat)
                (fs2 : list (label * fstat)) (r2 : fstat) (A : aset) : option aset :=
  obind (tall (fun A k => fc A (field_at k fs1 r1) (field_at k fs2 r2)) (labels fs1 fs2) A)
        (fun A1 => fc A1 r1 r2).

(** A label of a view with status [g] on an object with status [f]. *)
Definition tfchk (c : aset -> ty -> ty -> option aset) (A : aset) (f g : fstat) : option aset :=
  match f, g with
  | FReq x, FReq y | FReq x, FOpt y | FOpt x, FOpt y | FDict x, FOpt y | FDict x, FDict y =>
      both c A x y
  | FAbs, FAbs => Some A
  | _, FOpen => Some A
  | _, _ => None
  end.

Definition tochk (c : aset -> ty -> ty -> option aset) (o1 o2 : option (list ty)) (A : aset)
  : option aset :=
  match o1, o2 with
  | None, _ => Some A
  | Some l1, Some l2 => tall2 c l1 l2 A
  | Some _, None => None
  end.

Fixpoint tvchk (c : aset -> ty -> ty -> option aset) (ps : list eparam) (xs ys : list ty) (A : aset)
  : option aset :=
  match ps, xs, ys with
  | [], [], [] => Some A
  | p :: ps', x :: xs', y :: ys' =>
      obind (match p_var p with
             | VCo => c A x y
             | VContra => c A y x
             | VInv => both c A x y
             end) (tvchk c ps' xs' ys')
  | _, _, _ => None
  end.

(** One level of [<=].  [rec] continues the same level (a union or
    unfolding step) and [c] compares the children of a constructor.  A
    failed union alternative leaves the set as it was. *)
Definition step (rec c : aset -> ty -> ty -> option aset) (A : aset) (a b : ty) : option aset :=
  match a, b with
  | TBot, _ => Some A
  | _, TTop => Some A
  | TUnion x y, _ => obind (rec A x b) (fun A1 => rec A1 y b)
  | TMu t, _ => if mu_ok t then rec A (tunfold t) b else None
  | _, TMu t => if mu_ok t then rec A a (tunfold t) else None
  | _, TUnion x y => match rec A a x with Some A1 => Some A1 | None => rec A a y end
  | TMaybe x, TMaybe y => c A x y
  | TList x, TList y => both c A x y
  | TRec fs1 r1, TRec fs2 r2 => trec (tfchk c) fs1 r1 fs2 r2 A
  | TQuote i1 o1, TQuote i2 o2 => obind (tall2 c i2 i1 A) (tochk c o1 o2)
  | TEnum E1 xs, TEnum E2 ys => if ename_eqb E1 E2 then tvchk c (en_params E1) xs ys A else None
  | _, _ => None
  end.

(** One level of fresh retyping.  [sq] is a new [<=] query with its own
    set; quotes are compared only by it. *)
Definition frchk (c : aset -> ty -> ty -> option aset) (A : aset) (f g : fstat) : option aset :=
  match f, g with
  | FReq x, FReq y => c A x y
  | FAbs, FOpt _ | FAbs, FDict _ | FAbs, FAbs => Some A
  | FReq x, FOpt y | FOpt x, FOpt y | FDict x, FOpt y
  | FReq x, FDict y | FOpt x, FDict y | FDict x, FDict y => c A x y
  | _, FOpen => Some A
  | _, _ => None
  end.

(** Enum arguments: a fresh-covariant one is retyped; any other is
    compared by its variance with [sq]. *)
Fixpoint tvrchk (c : aset -> ty -> ty -> option aset) (sq : ty -> ty -> bool)
                (ps : list eparam) (xs ys : list ty) (A : aset) : option aset :=
  match ps, xs, ys with
  | [], [], [] => Some A
  | p :: ps', x :: xs', y :: ys' =>
      obind (if p_fresh p then c A x y
             else if match p_var p with
                     | VCo => sq x y
                     | VContra => sq y x
                     | VInv => if sq x y then sq y x else false
                     end then Some A else None)
            (tvrchk c sq ps' xs' ys')
  | _, _, _ => None
  end.

Definition rstep (sq : ty -> ty -> bool) (rec c : aset -> ty -> ty -> option aset)
                 (A : aset) (a b : ty) : option aset :=
  if sq a b then Some A else
  match a, b with
  | TUnion x y, _ => obind (rec A x b) (fun A1 => rec A1 y b)
  | TMu t, _ => if mu_ok t then rec A (tunfold t) b else None
  | _, TMu t => if mu_ok t then rec A a (tunfold t) else None
  | _, TUnion x y => match rec A a x with Some A1 => Some A1 | None => rec A a y end
  | TMaybe x, TMaybe y => c A x y
  | TList x, TList y => c A x y
  | TRec fs1 r1, TRec fs2 r2 => trec (frchk c) fs1 r1 fs2 r2 A
  | TEnum E1 xs, TEnum E2 ys => if ename_eqb E1 E2 then tvrchk c sq (en_params E1) xs ys A else None
  | _, _ => None
  end.

(** ** The invariant, for either relation

    [F] is one level ([subF] or [rsubF]) and [B] the relation ([sub] or
    [rsub]). *)
Section Inv.
Variable F : (ty -> ty -> Prop) -> ty -> ty -> Prop.
Variable B : ty -> ty -> Prop.
Hypothesis F_mono : forall R R' : ty -> ty -> Prop,
  (forall x y, R x y -> R' x y) -> forall a b, F R a b -> F R' a b.

(** A child pair holds when it is in the set or already known to hold. *)
Definition Ext (A : aset) (u v : ty) : Prop := In (u, v) A \/ B u v.

(** The query went from [A] to [A'] by adding pairs, each of which holds
    one level down. *)
Definition Inv (A A' : aset) : Prop :=
  exists N, A' = N ++ A /\ forall u v, In (u, v) N -> F (Ext A') u v.

Lemma Inv_refl A : Inv A A.
Proof. exists []. split; [reflexivity | simpl; tauto]. Qed.

Lemma Ext_lift A1 A2 : Inv A1 A2 -> forall u v, Ext A1 u v -> Ext A2 u v.
Proof.
  intros (N & -> & _) u v [H|H]; [left; apply in_or_app; right; exact H | right; exact H].
Qed.

Lemma Inv_trans A A1 A2 : Inv A A1 -> Inv A1 A2 -> Inv A A2.
Proof.
  intros I1 I2. destruct I1 as (N1 & E1 & H1). destruct I2 as (N2 & E2 & H2).
  exists (N2 ++ N1). split; [subst; rewrite app_assoc; reflexivity|].
  intros u v Hin. apply in_app_or in Hin as [Hin|Hin]; [apply H2, Hin|].
  eapply F_mono; [apply Ext_lift; exists N2; split; [exact E2 | exact H2] | apply H1, Hin].
Qed.

(** What a check of one level, and of one child pair, establish. *)
Definition lvl_right (r : aset -> ty -> ty -> option aset) : Prop :=
  forall A x y A', r A x y = Some A' -> Inv A A' /\ F (Ext A') x y.

Definition chk_right (c : aset -> ty -> ty -> option aset) : Prop :=
  forall A x y A', c A x y = Some A' -> Inv A A' /\ Ext A' x y.

(** Comparing a child one level down with the pair assumed. *)
Lemma push_right (r : aset -> ty -> ty -> option aset) : lvl_right r ->
  forall A x y A', r ((x, y) :: A) x y = Some A' -> Inv A A' /\ Ext A' x y.
Proof.
  intros Hr A x y A' E. apply Hr in E as [(N & EA & HN) HF].
  split; [| left; subst; apply in_or_app; right; left; reflexivity].
  exists (N ++ [(x, y)]). split; [subst; rewrite <- app_assoc; reflexivity|].
  intros u v Hin. apply in_app_or in Hin as [Hin|[Hin|[]]]; [apply HN, Hin|].
  injection Hin as <- <-. exact HF.
Qed.

(** Once the pairs the query started with hold, so does every pair in its
    final set: they may all be cached. *)
Theorem Inv_sound :
  (forall R : ty -> ty -> Prop,
     (forall x y, R x y -> F (fun a b => R a b \/ B a b) x y) -> forall x y, R x y -> B x y) ->
  forall A A', Inv A A' -> (forall u v, In (u, v) A -> B u v) ->
  forall u v, In (u, v) A' -> B u v.
Proof.
  intros B_coind A A' (N & -> & HN) HA u v Hin. apply in_app_or in Hin as [Hin|Hin]; [|apply HA, Hin].
  apply (B_coind (fun a b => In (a, b) N)); [| exact Hin].
  intros x y Hxy. eapply F_mono; [| apply HN, Hxy].
  intros a b [H|H]; [apply in_app_or in H as [H|H]; [left; exact H | right; apply HA, H] | right; exact H].
Qed.

(** The helpers, for a child check that is right. *)
Section Helpers.
Variable c : aset -> ty -> ty -> option aset.
Hypothesis Hc : chk_right c.

Lemma both_right A x y A' : both c A x y = Some A' -> Inv A A' /\ Ext A' x y /\ Ext A' y x.
Proof.
  unfold both. intros E. apply obind_some in E as (A1 & E1 & E2).
  apply Hc in E1 as [I1 X1]. apply Hc in E2 as [I2 X2].
  split; [eapply Inv_trans; eauto | split; [eapply Ext_lift; eauto | exact X2]].
Qed.

Lemma tall2_right : forall l1 l2 A A', tall2 c l1 l2 A = Some A' -> Inv A A' /\ subsR (Ext A') l1 l2.
Proof.
  induction l1 as [|x l1 IH]; intros [|y l2] A A' E; simpl in E; try discriminate.
  - injection E as <-. split; [apply Inv_refl | constructor].
  - apply obind_some in E as (A1 & E1 & E2). apply Hc in E1 as [I1 X1].
    apply IH in E2 as [I2 S2]. split; [eapply Inv_trans; eauto|].
    constructor; [eapply Ext_lift; eauto | exact S2].
Qed.

Lemma tochk_right o1 o2 A A' : tochk c o1 o2 A = Some A' -> Inv A A' /\ osubR (Ext A') o1 o2.
Proof.
  destruct o1 as [l1|], o2 as [l2|]; simpl; intros E; try discriminate.
  - apply tall2_right in E as [I S]. split; [exact I | constructor; exact S].
  - injection E as <-. split; [apply Inv_refl | constructor].
  - injection E as <-. split; [apply Inv_refl | constructor].
Qed.

Lemma tvchk_right : forall ps xs ys A A', tvchk c ps xs ys A = Some A' -> Inv A A' /\ vsubsR (Ext A') ps xs ys.
Proof.
  induction ps as [|p ps IH]; intros [|x xs] [|y ys] A A' E; simpl in E; try discriminate.
  - injection E as <-. split; [apply Inv_refl | constructor].
  - apply obind_some in E as (A1 & E1 & E2). apply IH in E2 as [I2 S2].
    destruct (p_var p) eqn:V.
    + apply Hc in E1 as [I1 X1]. split; [eapply Inv_trans; eauto|].
      apply vs_co; auto. eapply Ext_lift; eauto.
    + apply Hc in E1 as [I1 X1]. split; [eapply Inv_trans; eauto|].
      apply vs_contra; auto. eapply Ext_lift; eauto.
    + apply both_right in E1 as (I1 & X1 & X2). split; [eapply Inv_trans; eauto|].
      apply vs_inv; auto; eapply Ext_lift; eauto.
Qed.
End Helpers.

Lemma field_at_notin k fs r : ~ In k (map fst fs) -> field_at k fs r = r.
Proof.
  unfold field_at. intros H. destruct (lookup k fs) eqn:E; auto.
  exfalso. apply H. apply lookup_in in E. apply in_map_iff. exists (k, f). auto.
Qed.

(** Every label, from the declared labels and the remainders. *)
Lemma trec_right (fc : aset -> fstat -> fstat -> option aset) (Rf : aset -> fstat -> fstat -> Prop) :
  (forall A1 A2 f g, Inv A1 A2 -> Rf A1 f g -> Rf A2 f g) ->
  (forall A f g A', fc A f g = Some A' -> Inv A A' /\ Rf A' f g) ->
  forall fs1 r1 fs2 r2 A A', trec fc fs1 r1 fs2 r2 A = Some A' ->
  Inv A A' /\ forall k, Rf A' (field_at k fs1 r1) (field_at k fs2 r2).
Proof.
  intros Mono Hf fs1 r1 fs2 r2 A A' E. unfold trec in E.
  apply obind_some in E as (A1 & E1 & E2). apply Hf in E2 as [I2 R2].
  assert (G : forall l A0 A1', tall (fun A k => fc A (field_at k fs1 r1) (field_at k fs2 r2)) l A0 = Some A1' ->
            Inv A0 A1' /\ forall k, In k l -> Rf A1' (field_at k fs1 r1) (field_at k fs2 r2)).
  { induction l as [|k0 l IH]; intros A0 A1' Et; simpl in Et.
    - injection Et as <-. split; [apply Inv_refl | simpl; tauto].
    - apply obind_some in Et as (A2 & Ek & Er). apply Hf in Ek as [Ik Rk].
      apply IH in Er as [Ir Rr]. split; [eapply Inv_trans; eauto|].
      intros k [<-|Hk]; [eapply Mono; eauto | apply Rr, Hk]. }
  apply G in E1 as [I1 R1]. split; [eapply Inv_trans; eauto|].
  intros k. destruct (in_dec String.string_dec k (labels fs1 fs2)) as [Hin|Hout].
  - eapply Mono; [exact I2 | apply R1, Hin].
  - unfold labels in Hout. rewrite in_app_iff in Hout. rewrite !field_at_notin by tauto. exact R2.
Qed.
End Inv.

(** ** The procedure for [<=] *)

Section Sub.
(** Pairs already known to hold: the final sets of earlier queries that
    said yes, say. *)
Variable C : ty -> ty -> bool.
Hypothesis C_ok : forall a b, C a b = true -> sub a b.

Notation Invs := (Inv subF sub).
Notation Exts := (Ext sub).

Fixpoint lvl (n : nat) (A : aset) (a b : ty) {struct n} : option aset :=
  match n with
  | 0 => None
  | S n' =>
      if ty_eqb a b then Some A else
      step (lvl n')
           (fun A x y => if assumed A x y then Some A else if C x y then Some A
                         else lvl n' ((x, y) :: A) x y)
           A a b
  end.

(** A child pair: assumed, known, or compared one level down with the pair
    assumed. *)
Definition chk (n : nat) (A : aset) (x y : ty) : option aset :=
  if assumed A x y then Some A else if C x y then Some A else lvl n ((x, y) :: A) x y.

(** A top-level query starts with an empty set.  [subq_set] also returns
    the final set. *)
Definition subq_set (n : nat) (a b : ty) : option aset := chk n [] a b.

Definition subq (n : nat) (a b : ty) : bool :=
  match subq_set n a b with Some _ => true | None => false end.

Lemma Exts_lift A1 A2 : Invs A1 A2 -> forall u v, Exts A1 u v -> Exts A2 u v.
Proof. apply Ext_lift. Qed.

Lemma step_right (rec c : aset -> ty -> ty -> option aset) :
  lvl_right subF sub rec -> chk_right subF sub c ->
  forall A a b A', step rec c A a b = Some A' -> Invs A A' /\ subF (Exts A') a b.
Proof.
  intros Hr Hc A a b A' E. unfold step in E.
  destruct a, b; simpl in E; try discriminate;
    repeat match goal with
      | E : Some _ = Some _ |- _ => injection E as <-
      | E : obind _ _ = Some _ |- _ => apply obind_some in E as (? & ? & ?)
      | E : (if ?c then _ else _) = Some _ |- _ => destruct c eqn:?; [|discriminate]
      | E : (match ?o with Some _ => _ | None => _ end) = Some _ |- _ => destruct o eqn:?
      end;
    repeat match goal with
      | E : rec _ _ _ = Some _ |- _ => apply Hr in E as [? ?]
      | E : c _ _ _ = Some _ |- _ => apply Hc in E as [? ?]
      | E : both c _ _ _ = Some _ |- _ => apply (both_right _ _ subF_mono c Hc) in E as (? & ? & ?)
      | E : tall2 c _ _ _ = Some _ |- _ => apply (tall2_right _ _ subF_mono c Hc) in E as [? ?]
      | E : tochk c _ _ _ = Some _ |- _ => apply (tochk_right _ _ subF_mono c Hc) in E as [? ?]
      | E : tvchk c _ _ _ _ = Some _ |- _ => apply (tvchk_right _ _ subF_mono c Hc) in E as [? ?]
      | E : ename_eqb _ _ = true |- _ => apply ename_eqb_true in E; subst
      end.
  all: try (split; [apply Inv_refl | first [apply sf_bot | apply sf_top]]; fail).
  all: try (split; [eassumption | solve [ apply sf_mul; eauto | apply sf_mur; eauto
                                         | apply sf_unionr1; eauto | apply sf_unionr2; eauto
                                         | apply sf_maybe; eauto | apply sf_enum; eauto ]]; fail).
  all: try (split; [eassumption | apply sf_list; assumption]; fail).
  all: try (split; [eapply Inv_trans; eauto using subF_mono|];
            apply sf_unionl; [eapply subF_mono; [| eassumption]; apply Exts_lift; assumption | assumption]; fail).
  all: try (split; [eapply Inv_trans; eauto using subF_mono|];
            apply sf_quote; [eapply subsR_mono; [| eassumption]; apply Exts_lift; assumption | assumption]; fail).
  (* records *)
  all: match goal with E : trec _ _ _ _ _ _ = Some _ |- _ =>
         eapply (trec_right _ _ subF_mono (tfchk c) (fun A f g => fsubR (Exts A) f g)) in E as [? ?] end.
  all: try (split; [eassumption | apply sf_rec; assumption]; fail).
  all: try (intros A1 A2 f g I H; eapply fsubR_mono; [| exact H]; apply Exts_lift; exact I; fail).
  all: intros A0 f g A0' Ef; destruct f, g; simpl in Ef; try discriminate;
         try (injection Ef as <-; split; [apply Inv_refl | constructor]; fail);
         apply (both_right _ _ subF_mono c Hc) in Ef as (? & ? & ?); split; auto; constructor; auto.
Qed.

Lemma lvl_ok : forall n, lvl_right subF sub (lvl n).
Proof.
  induction n as [|n IH]; intros A a b A' E; simpl in E; [discriminate|].
  destruct (ty_eqb a b) eqn:Eq.
  { apply ty_eqb_true in Eq. subst. injection E as <-. split; [apply Inv_refl | apply sf_refl]. }
  eapply step_right; [exact IH | | exact E].
  intros A0 x y A0' Ex.
  destruct (assumed A0 x y) eqn:Ea; [injection Ex as <-; split; [apply Inv_refl | left; apply assumed_in, Ea]|].
  destruct (C x y) eqn:Ec; [injection Ex as <-; split; [apply Inv_refl | right; apply C_ok, Ec]|].
  eapply push_right; eauto using subF_mono.
Qed.

Lemma chk_ok n : chk_right subF sub (chk n).
Proof.
  intros A x y A' E. unfold chk in E.
  destruct (assumed A x y) eqn:Ea; [injection E as <-; split; [apply Inv_refl | left; apply assumed_in, Ea]|].
  destruct (C x y) eqn:Ec; [injection E as <-; split; [apply Inv_refl | right; apply C_ok, Ec]|].
  eapply (push_right subF sub); [apply lvl_ok | exact E].
Qed.

(** An answer computed under the set [A] holds, and so does every pair in
    the final set, once every pair in [A] holds. *)
Theorem chk_confirmed n A x y A' : chk n A x y = Some A' -> (forall u v, In (u, v) A -> sub u v) ->
  sub x y /\ forall u v, In (u, v) A' -> sub u v.
Proof.
  intros E HA. apply chk_ok in E as [I X].
  assert (All : forall u v, In (u, v) A' -> sub u v)
    by (eapply Inv_sound; [apply subF_mono | apply sub_coind_upto | exact I | exact HA]).
  split; [destruct X as [X|X]; auto | exact All].
Qed.

(** After a top-level yes, the pair and every pair in the final set hold. *)
Theorem subq_set_sound n a b A' : subq_set n a b = Some A' ->
  sub a b /\ forall u v, In (u, v) A' -> sub u v.
Proof. intros E. eapply chk_confirmed; [exact E | simpl; tauto]. Qed.

Theorem subq_sound n a b : subq n a b = true -> sub a b.
Proof.
  unfold subq. destruct (subq_set n a b) eqn:E; [|discriminate].
  intros _. exact (proj1 (subq_set_sound n a b _ E)).
Qed.
End Sub.

(** ** The procedure for fresh retyping *)

Section RSub.
Variable C : ty -> ty -> bool.
Hypothesis C_ok : forall a b, C a b = true -> sub a b.
Variable Cr : ty -> ty -> bool.
Hypothesis Cr_ok : forall a b, Cr a b = true -> rsub a b.

Notation Invr := (Inv rsubF rsub).
Notation Extr := (Ext rsub).

(** The set holds only retyping pairs.  A [<=] question is a new query
    ([subq]) and never sees them (H12). *)
Fixpoint rlvl (n : nat) (A : aset) (a b : ty) {struct n} : option aset :=
  match n with
  | 0 => None
  | S n' =>
      rstep (subq C n') (rlvl n')
            (fun A x y => if assumed A x y then Some A else if Cr x y then Some A
                          else rlvl n' ((x, y) :: A) x y)
            A a b
  end.

Definition rchk (n : nat) (A : aset) (x y : ty) : option aset :=
  if assumed A x y then Some A else if Cr x y then Some A else rlvl n ((x, y) :: A) x y.

Definition rsubq_set (n : nat) (a b : ty) : option aset := rchk n [] a b.

Definition rsubq (n : nat) (a b : ty) : bool :=
  match rsubq_set n a b with Some _ => true | None => false end.

Lemma Extr_lift A1 A2 : Invr A1 A2 -> forall u v, Extr A1 u v -> Extr A2 u v.
Proof. apply Ext_lift. Qed.

Lemma tvrchk_right (c : aset -> ty -> ty -> option aset) (sq : ty -> ty -> bool) :
  chk_right rsubF rsub c -> (forall x y, sq x y = true -> sub x y) ->
  forall ps xs ys A A', tvrchk c sq ps xs ys A = Some A' -> Invr A A' /\ vrsubsR (Extr A') ps xs ys.
Proof.
  intros Hc Hs. induction ps as [|p ps IH]; intros [|x xs] [|y ys] A A' E; simpl in E; try discriminate.
  - injection E as <-. split; [apply Inv_refl | constructor].
  - apply obind_some in E as (A1 & E1 & E2). apply IH in E2 as [I2 S2].
    destruct (p_fresh p) eqn:F.
    + apply Hc in E1 as [I1 X1]. split; [eapply Inv_trans; eauto using rsubF_mono|].
      apply vrs_fresh; auto. eapply Extr_lift; eauto.
    + destruct (match p_var p with VCo => sq x y | VContra => sq y x
                | VInv => if sq x y then sq y x else false end) eqn:Q; [|discriminate].
      injection E1 as <-. split; [exact I2|].
      apply vrs_sub; auto. destruct (p_var p); simpl; auto.
      destruct (sq x y) eqn:Q1; [|discriminate]. split; auto.
Qed.

Lemma rstep_right (sq : ty -> ty -> bool) (rec c : aset -> ty -> ty -> option aset) :
  (forall x y, sq x y = true -> sub x y) ->
  lvl_right rsubF rsub rec -> chk_right rsubF rsub c ->
  forall A a b A', rstep sq rec c A a b = Some A' -> Invr A A' /\ rsubF (Extr A') a b.
Proof.
  intros Hs Hr Hc A a b A' E. unfold rstep in E.
  destruct (sq a b) eqn:Q.
  { injection E as <-. split; [apply Inv_refl | apply rf_sub, Hs, Q]. }
  destruct a, b; simpl in E; try discriminate;
    repeat match goal with
      | E : Some _ = Some _ |- _ => injection E as <-
      | E : obind _ _ = Some _ |- _ => apply obind_some in E as (? & ? & ?)
      | E : (if ?c then _ else _) = Some _ |- _ => destruct c eqn:?; [|discriminate]
      | E : (match ?o with Some _ => _ | None => _ end) = Some _ |- _ => destruct o eqn:?
      end;
    repeat match goal with
      | E : rec _ _ _ = Some _ |- _ => apply Hr in E as [? ?]
      | E : c _ _ _ = Some _ |- _ => apply Hc in E as [? ?]
      | E : tvrchk c sq _ _ _ _ = Some _ |- _ => apply (tvrchk_right c sq Hc Hs) in E as [? ?]
      | E : ename_eqb _ _ = true |- _ => apply ename_eqb_true in E; subst
      end.
  all: try (split; [eassumption | solve [ apply rf_mul; eauto | apply rf_mur; eauto
                                         | apply rf_unionr1; eauto | apply rf_unionr2; eauto
                                         | apply rf_maybe; eauto | apply rf_list; eauto
                                         | apply rf_enum; eauto ]]; fail).
  all: try (split; [eapply Inv_trans; eauto using rsubF_mono|];
            apply rf_unionl; [eapply rsubF_mono; [| eassumption]; apply Extr_lift; assumption | assumption]; fail).
  (* records *)
  all: match goal with E : trec _ _ _ _ _ _ = Some _ |- _ =>
         eapply (trec_right _ _ rsubF_mono (frchk c) (fun A f g => frsubR (Extr A) f g)) in E as [? ?] end.
  all: try (split; [eassumption | apply rf_rec; assumption]; fail).
  all: try (intros A1 A2 f g I H; eapply frsubR_mono; [| exact H]; apply Extr_lift; exact I; fail).
  all: intros A0 f g A0' Ef; destruct f, g; simpl in Ef; try discriminate;
         try (injection Ef as <-; split; [apply Inv_refl | constructor]; fail);
         apply Hc in Ef as [? ?]; split; auto;
         first [ constructor; auto; fail
               | eapply frs_opt; [| eassumption]; tauto
               | eapply frs_dict; [| eassumption]; tauto ].
Qed.

Lemma rlvl_ok : forall n, lvl_right rsubF rsub (rlvl n).
Proof.
  induction n as [|n IH]; intros A a b A' E; simpl in E; [discriminate|].
  eapply rstep_right; [| exact IH | | exact E].
  - intros x y Q. eapply subq_sound; eauto.
  - intros A0 x y A0' Ex.
    destruct (assumed A0 x y) eqn:Ea; [injection Ex as <-; split; [apply Inv_refl | left; apply assumed_in, Ea]|].
    destruct (Cr x y) eqn:Ec; [injection Ex as <-; split; [apply Inv_refl | right; apply Cr_ok, Ec]|].
    eapply push_right; eauto using rsubF_mono.
Qed.

Lemma rchk_ok n : chk_right rsubF rsub (rchk n).
Proof.
  intros A x y A' E. unfold rchk in E.
  destruct (assumed A x y) eqn:Ea; [injection E as <-; split; [apply Inv_refl | left; apply assumed_in, Ea]|].
  destruct (Cr x y) eqn:Ec; [injection E as <-; split; [apply Inv_refl | right; apply Cr_ok, Ec]|].
  eapply (push_right rsubF rsub); [apply rlvl_ok | exact E].
Qed.

Theorem rsubq_set_sound n a b A' : rsubq_set n a b = Some A' ->
  rsub a b /\ forall u v, In (u, v) A' -> rsub u v.
Proof.
  intros E. apply rchk_ok in E as [I X].
  assert (All : forall u v, In (u, v) A' -> rsub u v)
    by (eapply Inv_sound; [apply rsubF_mono | apply rsub_coind_upto | exact I | simpl; tauto]).
  split; [destruct X as [X|X]; auto | exact All].
Qed.

Theorem rsubq_sound n a b : rsubq n a b = true -> rsub a b.
Proof.
  unfold rsubq. destruct (rsubq_set n a b) eqn:E; [|discriminate].
  intros _. exact (proj1 (rsubq_set_sound n a b _ E)).
Qed.

(** ** What the join needs

    [le_alg] is the checker's decision procedure for the join: [<=] for
    shared arms, fresh retyping when both arms are fresh.  It is right when
    it says yes, which is the hypothesis [le_ok] of Join.v. *)
Definition le_alg (n : nat) (fr : bool) (a b : ty) : bool :=
  if fr then rsubq n a b else subq C n a b.

Theorem le_alg_ok n fr a b : le_alg n fr a b = true -> jrel fr a b.
Proof. destruct fr; simpl; [apply rsubq_sound | apply subq_sound; exact C_ok]. Qed.

(** So a join computed with the procedure is an upper bound of both arms,
    and an [if] joined this way checks in the core, with no hypothesis. *)
Corollary join_slot_ub_alg n p q r :
  join_slot (le_alg n) p q = Some r -> slot_sub p r /\ slot_sub q r.
Proof. apply join_slot_ub, le_alg_ok. Qed.

Corollary if_join_alg n sigs G Br Co R e1 e2 s s1 s2 s' :
  T sigs G Br Co R e1 s s1 -> T sigs G Br Co R e2 s s2 -> join_stack (le_alg n) s1 s2 = Some s' ->
  TW sigs G Br Co R (WIf e1 e2) ((Sh, TBool) :: s) s'.
Proof. apply if_join, le_alg_ok. Qed.
End RSub.

(** ** Examples

    The procedures run on the design's cases, with no cache and a fixed
    amount of fuel.  A yes is a proof, by [subq_sound] and [rsubq_sound]; a
    no that holds for all fuel is proved from the relation. *)
Import Recursive.

Definition nocache : ty -> ty -> bool := fun _ _ => false.
Lemma nocache_ok a b : nocache a b = true -> sub a b.
Proof. discriminate. Qed.
Lemma nocache_rok a b : nocache a b = true -> rsub a b.
Proof. discriminate. Qed.

Definition sq (a b : ty) : bool := subq nocache 30 a b.
Definition rq (a b : ty) : bool := rsubq nocache nocache 30 a b.
Definition ok (o : option aset) : bool := match o with Some _ => true | None => false end.

(** [Json] equals the same union with its members in another order.
    Deciding it compares [[Json]] with the other [[Json]], which is where
    the set is used. *)
Example alg_json_teq : sq Json Json2 = true /\ sq Json2 Json = true.
Proof. vm_compute. auto. Qed.

Example alg_int_json : sq TInt Json = true.
Proof. vm_compute. reflexivity. Qed.

Example alg_list_json : sq (TList Json) Json = true.
Proof. vm_compute. reflexivity. Qed.

Example alg_person_lit : sq PersonLit Person = true.
Proof. vm_compute. reflexivity. Qed.

(** A stored [[int]] is not a [Json] (lists are invariant); a fresh one may
    become one. *)
Example alg_ints_json : sq (TList TInt) Json = false /\ rq (TList TInt) Json = true.
Proof. vm_compute. auto. Qed.

(** [as C] on a fresh [A] ([a_retypes_to_c] in Recursive.v). *)
Example alg_a_to_c : rq RA RC = true.
Proof. vm_compute. reflexivity. Qed.

(** ** H12: one set per relation

    The procedure rejects retyping a fresh [A] to [B], for every amount of
    fuel.  A procedure with one set for both relations accepts it: each
    level may be a [<=] step or a retyping step, and every child is
    answered from the one set.  It is [mixed] of Recursive.v, as a
    function. *)
Example alg_h12 : forall n, rsubq nocache nocache n TA TBB = false.
Proof.
  intros n. destruct (rsubq nocache nocache n TA TBB) eqn:E; auto.
  exfalso. apply rsub_rejects. eapply rsubq_sound; [exact nocache_ok | exact nocache_rok | exact E].
Qed.

Fixpoint mlvl (n : nat) (A : aset) (a b : ty) {struct n} : option aset :=
  match n with
  | 0 => None
  | S n' =>
      let c := fun A x y => if assumed A x y then Some A else mlvl n' ((x, y) :: A) x y in
      if ty_eqb a b then Some A else
      match step (mlvl n') c A a b with
      | Some A1 => Some A1
      | None => rstep (fun _ _ => false) (mlvl n') c A a b
      end
  end.

Example mixed_alg_accepts : ok (mlvl 30 [(TA, TBB)] TA TBB) = true.
Proof. vm_compute. reflexivity. Qed.

(** ** H13: the set only at constructor children

    With [type V = int | V], unguarded, the procedure says no to
    [str <= V] for every amount of fuel: it runs out of fuel going round
    the union.  A procedure that consults the set at every step, union and
    unfolding steps included, says yes.  It is [subA] of Recursive.v, as a
    function. *)
Example alg_h13 : forall n, subq nocache n TStr V = false.
Proof.
  intros n. destruct (subq nocache n TStr V) eqn:E; auto.
  exfalso. apply unguarded_model. eapply subq_sound; [exact nocache_ok | exact E].
Qed.

Fixpoint lvl_every (n : nat) (A : aset) (a b : ty) {struct n} : option aset :=
  match n with
  | 0 => None
  | S n' =>
      let c := fun A x y => if assumed A x y then Some A else lvl_every n' ((x, y) :: A) x y in
      if ty_eqb a b then Some A else step c c A a b
  end.

Example every_step_accepts : ok (lvl_every 10 [(TStr, V)] TStr V) = true.
Proof. vm_compute. reflexivity. Qed.

(** ** Caching only after a yes

    Deciding [A <= B] assumes the pair, and the field [f] then asks
    [(-- A) <= (-- B)], which is yes under that assumption.  [A <= B] then
    fails at the field [x], so the assumption was false, and so is the
    answer for the quotes.  Caching it would let a later query about the
    quotes say yes.  After a top-level yes, by [subq_set_sound], every pair
    in the final set may be cached. *)
Lemma not_sub_qa_qb : ~ sub QA QB.
Proof.
  intros H. apply sub_unfold in H. inversion H; subst.
  match goal with Ho : osubR sub _ _ |- _ => inversion Ho; subst end.
  match goal with Hl : subsR sub _ _ |- _ => inversion Hl; subst end.
  eapply not_sub_ab; eassumption.
Qed.

Example cache_early :
  ok (chk nocache 10 [(TA, TBB)] QA QB) = true /\ subq nocache 10 TA TBB = false /\ ~ sub QA QB.
Proof. split; [vm_compute; reflexivity | split; [vm_compute; reflexivity | apply not_sub_qa_qb]]. Qed.

(** ** Joins with the procedure

    The joins of Recursive.v, computed with [le_alg] in place of the
    hand-written [le_ex]; each is an upper bound by [join_slot_ub_alg]. *)
Definition jl := le_alg nocache nocache 30.

Example alg_join_int_json : join_slot jl (Sh, TInt) (Dp, Json) = Some (Sh, Json).
Proof. vm_compute. reflexivity. Qed.

Example alg_join_fresh_list_json : join_slot jl (Dp, TList TInt) (Dp, Json) = Some (Dp, Json).
Proof. vm_compute. reflexivity. Qed.

Example alg_join_shared_list_json : join_slot jl (Sh, TList TInt) (Dp, Json) = None.
Proof. vm_compute. reflexivity. Qed.

Example alg_join_nested :
  join_slot jl (Dp, TList Person) (Dp, TList PersonLit) = Some (Dp, TList Person).
Proof. vm_compute. reflexivity. Qed.

Example alg_join_two_recursive : join_slot jl (Dp, RA) (Dp, RB) = None.
Proof. vm_compute. reflexivity. Qed.

Example alg_join_nested_ub :
  slot_sub (Dp, TList Person) (Dp, TList Person) /\ slot_sub (Dp, TList PersonLit) (Dp, TList Person).
Proof. exact (join_slot_ub_alg nocache nocache_ok nocache nocache_rok 30 _ _ _ alg_join_nested). Qed.

(** Joins that widen only by the other side being below, or by kinds.
    Branches leaving two different enums join to their union; a quote that
    never returns joins with a quote with the same inputs, giving the
    latter; two quotes neither of which is below the other have no join. *)
Definition EShape : ename := {| en_name := "Shape"; en_params := []; en_imm := true |}.
Definition ELoadError : ename := {| en_name := "LoadError"; en_params := []; en_imm := true |}.

Example alg_join_two_enums :
  join_slot jl (Sh, TEnum EShape []) (Sh, TEnum ELoadError [])
  = Some (Sh, TUnion (TEnum EShape []) (TEnum ELoadError [])).
Proof. vm_compute. reflexivity. Qed.

Example alg_join_never_quote :
  join_slot jl (Sh, TQuote [TStr] None) (Sh, TQuote [TStr] (Some [TStr]))
  = Some (Sh, TQuote [TStr] (Some [TStr])).
Proof. vm_compute. reflexivity. Qed.

Example alg_join_unrelated_quotes :
  join_slot jl (Dp, TQuote [TInt] (Some [TInt])) (Dp, TQuote [TInt] (Some [TStr])) = None.
Proof. vm_compute. reflexivity. Qed.

Example alg_join_union_below :
  join_slot jl (Sh, TUnion TInt TStr) (Sh, TUnion TInt (TUnion TStr TBool))
  = Some (Sh, TUnion TInt (TUnion TStr TBool)).
Proof. vm_compute. reflexivity. Qed.
