(** * Abstract types checked once: the escape check.

    A kind pattern on a value whose element type is unknown types its arm
    for every element type ([tw_kind_list]), and an enum kind pattern for
    every argument list with one argument per parameter ([tw_kind_enum]).
    A checker cannot try every type.  It checks the arm once, with a new
    type variable standing for the unknown type (one per enum parameter),
    and then checks that the variable does not escape: it must not appear
    in the variable context, the break, continue or return stacks, the
    stack below the matched value, or the stack the arm leaves.

    This file proves that check is enough ([kind_list_once],
    [kind_enum_once]).  It is the substitution lemma [T_subst]: replace the
    new variable with any type; everything outside the arm does not mention
    it, so only the arm's input changes.

    Nothing inside the arm needs checking for the variable: words carry no
    types except [tryAs] targets, which mention no type variable, and
    constructor payload types, which mention none either. *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Typing Generic.

(** The variables [xs] replaced by the types [a], in order. *)
Fixpoint sks (xs : list nat) (a : list ty) : nat -> ty :=
  match xs, a with
  | x :: xs', b :: a' => fun y => if Nat.eqb y x then b else sks xs' a' y
  | _, _ => TVar
  end.

Lemma sks_off xs a y : ~ In y xs -> sks xs a y = TVar y.
Proof.
  revert a. induction xs as [|x xs IH]; intros [|b a] Hy; simpl; auto.
  destruct (Nat.eqb_spec y x) as [->|Ne]; [exfalso; apply Hy; left; reflexivity|].
  apply IH. intros Hi. apply Hy. right; exact Hi.
Qed.

Lemma sks_map xs a : NoDup xs -> length xs = length a -> map (fun x => sks xs a x) xs = a.
Proof.
  revert a. induction xs as [|x xs IH]; intros [|b a] Nd L; simpl in *; try discriminate; auto.
  inversion Nd; subst. rewrite Nat.eqb_refl. f_equal.
  transitivity (map (fun y => sks xs a y) xs); [|apply IH; auto].
  apply map_ext_in. intros y Hy.
  destruct (Nat.eqb_spec y x) as [->|]; [contradiction|reflexivity].
Qed.

Lemma agree_off xs a l : (forall y, In y l -> ~ In y xs) -> agree (sks xs a) TVar l.
Proof. intros H y Hy. apply sks_off, H, Hy. Qed.

Lemma esubst_id G : esubst TVar G = G.
Proof.
  unfold esubst. rewrite <- (map_id G) at 2. apply map_ext. intros [y t]; simpl. rewrite tsub_id. reflexivity.
Qed.

Lemma lsubst_id L : lsubst TVar L = L.
Proof. destruct L; simpl; auto. rewrite ssubst_id. reflexivity. Qed.

Lemma rsubst_id R : rsubst TVar R = R.
Proof. destruct R; simpl; auto. rewrite ssubst_id. reflexivity. Qed.

(** Everything outside the arm: the variable context, the loop and return
    stacks, the stack below the matched value, and the arm's result. *)
Definition outside (G : tenv) (B C : lctx) (R : rctx) (s s' : sty) : list nat :=
  fvG G ++ fvL B ++ fvL C ++ fvR R ++ fvs s ++ fvs s'.

Section Once.
Variable sigs : genv.
(** As in [T_subst]: signatures are closed under substitution (true of the
    instances of declared signatures, [instances_closed]) and enum
    declarations are well formed. *)
Hypothesis Hsigs : forall f ins outs th, g_sigs sigs f ins outs ->
  g_sigs sigs f (ssubst th ins) (osubst th outs).
Hypothesis Hctors : forall E c pts, g_ctors sigs E c = Some pts -> wf_payload E pts.

(** The arm checked with new variables [xs] checks with any types [a] for them. *)
Lemma arm_inst G B C R e m (tv : list ty -> ty) s s' xs a :
  (forall th a', tsub th (tv a') = tv (map (tsub th) a')) ->
  NoDup xs -> length xs = length a ->
  (forall y, In y (outside G B C R s s') -> ~ In y xs) ->
  T sigs G B C R e ((m, tv (map TVar xs)) :: s) s' ->
  T sigs G B C R e ((m, tv a) :: s) s'.
Proof.
  intros Htv Nd L Out HT.
  pose proof (proj2 (T_subst sigs Hsigs Hctors G) _ _ _ _ _ _ HT (sks xs a)) as H.
  unfold outside in Out.
  assert (AG : agree (sks xs a) TVar (fvG G)) by (apply agree_off; intros y Hy; apply Out; rewrite ?in_app_iff; tauto).
  assert (AB : agree (sks xs a) TVar (fvL B)) by (apply agree_off; intros y Hy; apply Out; rewrite ?in_app_iff; tauto).
  assert (AC : agree (sks xs a) TVar (fvL C)) by (apply agree_off; intros y Hy; apply Out; rewrite ?in_app_iff; tauto).
  assert (AR : agree (sks xs a) TVar (fvR R)) by (apply agree_off; intros y Hy; apply Out; rewrite ?in_app_iff; tauto).
  assert (AS : agree (sks xs a) TVar (fvs s)) by (apply agree_off; intros y Hy; apply Out; rewrite ?in_app_iff; tauto).
  assert (AS' : agree (sks xs a) TVar (fvs s')) by (apply agree_off; intros y Hy; apply Out; rewrite ?in_app_iff; tauto).
  rewrite (esubst_ext _ _ _ AG), esubst_id, (lsubst_ext _ _ _ AB), (lsubst_ext _ _ _ AC), !lsubst_id,
    (rsubst_ext _ _ _ AR), rsubst_id, ssubst_cons, (ssubst_ext _ _ _ AS), (ssubst_ext _ _ _ AS'),
    !ssubst_id in H.
  simpl in H. rewrite Htv, map_map in H. simpl in H. rewrite sks_map in H; auto.
Qed.

(** [list xs] on unknown contents: the arm checked once, at [[x]] for a
    variable [x] that does not escape. *)
Theorem kind_list_once G B C R m t e1 e2 s s' x :
  ~ In x (outside G B C R s s') ->
  T sigs G B C R e1 ((m, TList (TVar x)) :: s) s' ->
  T sigs G B C R e2 ((m, t) :: s) s' ->
  TW sigs G B C R (WKindIf KList e1 e2) ((m, t) :: s) s'.
Proof.
  intros Out H1 H2. apply tw_kind_list; auto. intros a.
  apply (arm_inst G B C R e1 m (fun l => TList (hd TBot l)) s s' [x] [a]); auto.
  - intros th [|b l]; reflexivity.
  - constructor; [intros []|constructor].
  - intros y Hy [<-|[]]. contradiction.
Qed.

(** An enum kind pattern on unknown contents: the arm checked once, with
    one new variable per parameter, none of which escapes. *)
Theorem kind_enum_once G B C R m E t e1 e2 s s' xs :
  NoDup xs -> length xs = length (en_params E) ->
  (forall y, In y (outside G B C R s s') -> ~ In y xs) ->
  T sigs G B C R e1 ((m, TEnum E (map TVar xs)) :: s) s' ->
  T sigs G B C R e2 ((m, t) :: s) s' ->
  TW sigs G B C R (WKindIf (KEnum E) e1 e2) ((m, t) :: s) s'.
Proof.
  intros Nd L Out H1 H2. apply tw_kind_enum; auto. intros a La.
  apply (arm_inst G B C R e1 m (TEnum E) s s' xs a); auto.
  - rewrite L, La. reflexivity.
Qed.
End Once.
