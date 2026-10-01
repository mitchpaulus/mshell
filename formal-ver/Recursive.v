(** * Recursive types: examples and counterexamples.

    [type X = T] with guarded recursion is [TMu] in the model (Syntax.v).
    This file checks the rules on the design's examples ([Json], [Person],
    an alias guarded by an enum, a cyclic value) and shows what goes wrong
    in a checker that implements them naively. *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Variance Typing Interp Invariant RtLemmas Soundness
  Generic Join Cycles.
Open Scope string_scope.

Definition nodefs : string -> option prog := fun _ => None.
Definition run (e : prog) : result := eval nodefs 200 [OScope []] 0 [] e.
Definition runc (e : prog) : result := evalv cvalidate nodefs 200 [OScope []] 0 [] e.
Definition nosigs : genv := {| g_sigs := fun _ _ _ => False; g_ctors := maybe_ctors |}.
Definition is_stuck (r : result) : bool := match r with RStuck => true | _ => false end.
Definition IS := TUnion TInt TStr.

Ltac step r := eapply t_cons; [ r | ].

(** ** Json

    [type Json = bool | int | str | [Json] | {str: Json}] (the model has no
    [null] or [float]; they change nothing). *)
Definition JsonB : ty := TUnion TBool (TUnion TInt (TUnion TStr (TUnion (TList (TRV 0)) (TDict (TRV 0))))).
Definition Json : ty := TMu JsonB.

Example json_closed : mu_ok JsonB = true.
Proof. reflexivity. Qed.

(** The same type spelled with its members in another order.  [Json] and
    [Json2] unfold to the same infinite tree, so each is below the other.
    The derivation is infinite: comparing the list members needs [Json] and
    [Json2] equal again (lists are invariant), so this holds only because
    [sub] is the greatest fixed point.  It is what the checker's
    assumption-set algorithm decides. *)
Definition Json2B : ty := TUnion (TDict (TRV 0)) (TUnion (TList (TRV 0)) (TUnion TStr (TUnion TInt TBool))).
Definition Json2 : ty := TMu Json2B.

(** Pick the member of a union on the right that [tac] proves. *)
Ltac pick tac := first [ tac | apply sf_unionr1; pick tac | apply sf_unionr2; pick tac ].

Example json_teq : teq Json Json2.
Proof.
  set (R := fun x y => (x = Json /\ y = Json2) \/ (x = Json2 /\ y = Json)).
  assert (HR : forall x y, R x y -> subF R x y).
  { intros x y [[-> ->]|[-> ->]].
    - apply sf_mul; [reflexivity|]. cbn.
      repeat (apply sf_unionl; [ apply sf_mur; [reflexivity|]; cbn | ]);
        [ | | | | apply sf_mur; [reflexivity|]; cbn ].
      all: pick ltac:(first
        [ apply sf_refl
        | apply sf_list; unfold R; [left | right]; split; reflexivity
        | apply sf_rec; intros k; unfold field_at; simpl;
          apply fs_dict; unfold R; [left | right]; split; reflexivity ]).
    - apply sf_mul; [reflexivity|]. cbn.
      repeat (apply sf_unionl; [ apply sf_mur; [reflexivity|]; cbn | ]);
        [ | | | | apply sf_mur; [reflexivity|]; cbn ].
      all: pick ltac:(first
        [ apply sf_refl
        | apply sf_list; unfold R; [right | left]; split; reflexivity
        | apply sf_rec; intros k; unfold field_at; simpl;
          apply fs_dict; unfold R; [right | left]; split; reflexivity ]). }
  split; apply (sub_coind R HR); unfold R; auto.
Qed.

(** Pick the member of a union on the right, for [sub] and [rsub]. *)
Ltac pick_s tac := first [ tac | apply s_unionr1; pick_s tac | apply s_unionr2; pick_s tac ].
Ltac pick_r tac := first [ tac | apply rs_unionr1; pick_r tac | apply rs_unionr2; pick_r tac ].

(** Split on which declared label [k] is. *)
Ltac labels := repeat match goal with
  | |- context [String.eqb ?k ?s] => destruct (String.eqb_spec k s); subst; try discriminate
  end.

(** ** Person

    [type Person = {name: str, age: int, friends: [Person]}], an open shape
    (a written shape type is open). *)
Definition PersonB : ty :=
  TRec [("name", FReq TStr); ("age", FReq TInt); ("friends", FReq (TList (TRV 0)))] FOpen.
Definition Person : ty := TMu PersonB.

(** The literal [{name: "a", age: 1, friends: []}] is fresh, so [as Person]
    retypes it in place ([Retype]). *)
Definition person_lit : prog :=
  [WDictNew; WStr "a"; WSetK "name"; WInt 1; WSetK "age"; WNil; WSetK "friends"].

(** A fresh value of an immutable type may take the fresh slot a strong
    update needs. *)
Lemma imm_fresh t : immutable t = true -> ssub [(Sh, t)] [(Dp, t)].
Proof. intros I. constructor; [apply ss_imm; [exact I | apply s_refl] | constructor]. Qed.

Ltac setk_imm k :=
  eapply t_sub; [ | eapply t_cons; [apply (tw_setk_dp _ _ _ _ _ k) | ] | apply ssub_refl ];
  [ constructor; [apply ss_imm; [reflexivity | apply s_refl] | apply ssub_refl] | ].

Example person_lit_typed :
  T nosigs [] LNone LNone RNone person_lit [] [(Dp, Person)].
Proof.
  unfold person_lit.
  step ltac:(apply tw_dictnew).
  step ltac:(apply tw_str). setk_imm "name".
  step ltac:(apply tw_int). setk_imm "age".
  step ltac:(apply (tw_nil _ _ _ _ _ Person)).
  step ltac:(apply tw_setk_dp).
  eapply t_sub; [apply ssub_refl | apply t_nil |].
  constructor; [| constructor]. apply ss_dp.
  apply rs_mur; [reflexivity|]. cbn. apply rs_rec. intros k. unfold field_at. simpl. labels;
    first [ apply frs_req, rs_sub, s_refl | apply frs_open ].
Qed.

(** ** [parseJson tryAs [Person] ?]

    A JSON-shaped fresh value: [[{name: "a", age: 1, friends: [{name: "b",
    age: 2, friends: []}]}]], typed [Json] (as [parseJson]'s result is),
    validated in place against [[Person]] and then read. *)
Definition friend_b : prog :=
  [WDictNew; WStr "b"; WSetK "name"; WInt 2; WSetK "age"; WNil; WSetK "friends"].
Definition person_a : prog :=
  ([WDictNew; WStr "a"; WSetK "name"; WInt 1; WSetK "age"; WNil] ++ friend_b ++ [WPush; WSetK "friends"])%list.
Definition people_json : prog := ([WNil] ++ person_a ++ [WPush])%list.
Definition people_read : prog :=
  [WTryAs (TList Person); wunwrap; WInt 0; WGetAt; WGetReq "friends"; WInt 0; WGetAt;
   WGetReq "age"; WInt 40; WAdd].

Definition top_int (r : result) : option nat :=
  match r with ROk ONormal _ (VInt n :: _) => Some n | _ => None end.

Example people_runs : top_int (run (people_json ++ people_read)%list) = Some 42.
Proof. vm_compute. reflexivity. Qed.

(** With an age that is a string, validation answers [none], and [?] is a
    checked error. *)
Definition people_bad : prog :=
  [WNil; WDictNew; WStr "a"; WSetK "name"; WStr "old"; WSetK "age"; WNil; WSetK "friends"; WPush].

Example people_bad_err : run (people_bad ++ people_read)%list = RErr.
Proof. vm_compute. reflexivity. Qed.

(** A shape literal is a [{str: Json}]: every label may become deletable. *)
Lemma rsub_json_base t : In t [TBool; TInt; TStr] -> rsub t Json.
Proof.
  intros Hin. apply rs_sub, s_mur; [reflexivity|]. cbn.
  destruct Hin as [<-|[<-|[<-|[]]]]; pick_s ltac:(apply s_refl).
Qed.

Lemma rsub_json_list : rsub (TList Json) Json.
Proof. apply rs_sub, s_mur; [reflexivity|]. cbn. pick_s ltac:(apply s_refl). Qed.

Lemma rsub_json_rec fs :
  (forall k f, lookup k fs = Some f -> exists a, f = FReq a /\ rsub a Json) ->
  rsub (TRec fs FAbs) Json.
Proof.
  intros Hf. apply rs_mur; [reflexivity|]. cbn.
  apply rs_unionr2, rs_unionr2, rs_unionr2, rs_unionr2. apply rs_rec. intros k.
  unfold field_at at 2. simpl. unfold field_at. destruct (lookup k fs) as [f|] eqn:E.
  - destruct (Hf k f E) as (a & -> & Ha). eapply frs_dict; [left; reflexivity | exact Ha].
  - apply frs_abs_dict.
Qed.

(** Retype the fresh top of the stack. *)
Ltac retype_top R :=
  eapply t_sub; [ | | apply ssub_refl ]; [ constructor; [apply ss_dp; R | apply ssub_refl] | ].

Ltac labels_in E := repeat match type of E with
  | context [String.eqb ?k ?s] => destruct (String.eqb_spec k s); subst; try discriminate
  end.

Ltac rec_json :=
  apply rsub_json_rec; intros k f E; simpl in E; labels_in E; injection E as <-;
  eexists; (split; [reflexivity|]);
  first [ apply rsub_json_base; simpl; tauto | apply rsub_json_list ].

Example people_typed :
  T nosigs [] LNone LNone RNone (people_json ++ people_read)%list [] [(Sh, TInt)].
Proof.
  unfold people_json, person_a, friend_b, people_read. simpl.
  step ltac:(apply (tw_nil _ _ _ _ _ Json)).
  step ltac:(apply tw_dictnew).
  step ltac:(apply tw_str). setk_imm "name".
  step ltac:(apply tw_int). setk_imm "age".
  step ltac:(apply (tw_nil _ _ _ _ _ Json)).
  step ltac:(apply tw_dictnew).
  step ltac:(apply tw_str). setk_imm "name".
  step ltac:(apply tw_int). setk_imm "age".
  step ltac:(apply (tw_nil _ _ _ _ _ Json)).
  step ltac:(apply tw_setk_dp).
  (* the friend is a Json object; push it *)
  retype_top ltac:(rec_json).
  step ltac:(apply tw_push_dp).
  step ltac:(apply tw_setk_dp).
  retype_top ltac:(rec_json).
  step ltac:(apply tw_push_dp).
  (* the parsed value: a fresh Json *)
  retype_top ltac:(apply rsub_json_list).
  (* validated in place: fresh, so no condition on the static type *)
  step ltac:(apply tw_try_dp; reflexivity).
  step ltac:(apply tw_wunwrap, maybe_ctors_ok).
  step ltac:(apply tw_int).
  eapply t_sub; [ | eapply t_cons; [apply tw_getat | ] | apply ssub_refl ].
  { constructor; [apply ss_sh, s_refl | constructor; [apply ss_forget, s_refl | constructor]]. }
  eapply t_sub; [ | eapply t_cons; [apply tw_getreq with (fs := [("name", FReq TStr); ("age", FReq TInt);
      ("friends", FReq (TList Person))]) (r := FOpen); reflexivity | ] | apply ssub_refl ].
  { constructor; [apply ss_sh, sub_unfold_l; reflexivity | constructor]. }
  step ltac:(apply tw_int).
  step ltac:(apply tw_getat).
  eapply t_sub; [ | eapply t_cons; [apply tw_getreq with (fs := [("name", FReq TStr); ("age", FReq TInt);
      ("friends", FReq (TList Person))]) (r := FOpen); reflexivity | ] | apply ssub_refl ].
  { constructor; [apply ss_sh, sub_unfold_l; reflexivity | constructor]. }
  step ltac:(apply tw_int).
  step ltac:(apply tw_add).
  apply t_nil.
Qed.

(** ** An alias guarded by an enum: [type T = Box[T]]

    [enum Box[a] = box [a] | empty end].  An enum instance is a type
    constructor, so it guards the recursion like a list or a shape does. *)
Definition pinv_fresh : eparam := {| p_var := VInv; p_fresh := true |}.
Definition EBox : ename := {| en_name := "Box"; en_params := [pinv_fresh]; en_imm := false |}.
Definition box_sigs : genv :=
  {| g_sigs := fun _ _ _ => False;
     g_ctors := fun E c =>
       if ename_eqb E EBox then
         if String.eqb c "box" then Some [TList (TParam 0)]
         else if String.eqb c "empty" then Some [] else None
       else maybe_ctors E c |}.
Definition TBoxB : ty := TEnum EBox [TRV 0].
Definition TB : ty := TMu TBoxB.

Example box_wf : wf_payload EBox [TList (TParam 0)].
Proof. reflexivity. Qed.
Example tb_closed : mu_ok TBoxB = true.
Proof. reflexivity. Qed.
Example tb_mutable : immutable TB = false.
Proof. reflexivity. Qed.

(** [[] empty append box tryAs T ?]: built fresh, validated in place. *)
Definition tb_prog : prog :=
  [WNil; WCon EBox "empty" []; WPush; WCon EBox "box" [TList (TParam 0)]; WTryAs TB; wunwrap].

Example tb_runs : match run tb_prog with ROk ONormal _ [VCon _ "box" _ _] => true | _ => false end = true.
Proof. vm_compute. reflexivity. Qed.

Example tb_typed : T box_sigs [] LNone LNone RNone tb_prog [] [(Dp, TB)].
Proof.
  unfold tb_prog.
  step ltac:(apply (tw_nil _ _ _ _ _ TB)).
  step ltac:(apply (tw_con_dp box_sigs [] LNone LNone RNone EBox "empty" [] [TB]); reflexivity).
  retype_top ltac:(apply (rs_mur _ TBoxB); [reflexivity | apply rs_sub, s_refl]).
  step ltac:(apply tw_push_dp).
  step ltac:(apply (tw_con_dp box_sigs [] LNone LNone RNone EBox "box" [TList (TParam 0)] [TB]); reflexivity).
  step ltac:(apply tw_try_dp; reflexivity).
  step ltac:(apply tw_wunwrap; split; reflexivity).
  apply t_nil.
Qed.

(** ** A cyclic value is well typed

    [type L = [L]]: [[] as L xs!  @xs @xs append] stores a list that
    contains itself, and it type-checks.  Before recursive types no checked program
    could build a cycle; now any recursive runtime walk (printing,
    [toJson], equality, [deepCopy], validation) can meet one. *)
Definition LB : ty := TList (TRV 0).
Definition L : ty := TMu LB.

Definition cyc_build : prog := [WNil; WStore "xs"; WLoad "xs"; WLoad "xs"; WPush; WDrop].
Definition cyc_try : prog := (cyc_build ++ [WLoad "xs"; WTryAs L])%list.
Definition cyc_copy : prog := (cyc_build ++ [WLoad "xs"; WCopy])%list.

Lemma list_L_below : sub (TList L) L.
Proof. apply s_mur; [reflexivity | apply s_refl]. Qed.

Example cyc_try_typed : T nosigs [("xs", TList L)] LNone LNone RNone cyc_try [] [(Sh, TMaybe L)].
Proof.
  unfold cyc_try, cyc_build. simpl.
  step ltac:(apply (tw_nil _ _ _ _ _ L)).
  eapply t_sub; [ | eapply t_cons; [apply tw_store; reflexivity | ] | apply ssub_refl ].
  { constructor; [apply ss_forget, s_refl | constructor]. }
  step ltac:(apply tw_load; reflexivity).
  step ltac:(apply tw_load; reflexivity).
  eapply t_sub; [ | eapply t_cons; [apply tw_push_sh | ] | apply ssub_refl ].
  { constructor; [apply ss_sh, list_L_below | apply ssub_refl]. }
  step ltac:(apply tw_drop).
  step ltac:(apply tw_load; reflexivity).
  step ltac:(apply tw_try_sub; [reflexivity | apply list_L_below]).
  apply t_nil.
Qed.

(** The model's validator runs out of budget on the cycle (a checked error),
    and so does [deepCopy]. *)
Example cyc_try_err : run cyc_try = RErr.
Proof. vm_compute. reflexivity. Qed.

Example cyc_copy_err : run cyc_copy = RErr.
Proof. vm_compute. reflexivity. Qed.

(** The validator of Cycles.v assumes a repeated (object, type) pair and
    answers [just]: the value does have type [L].  Both are sound. *)
Example cyc_try_cycles :
  match runc cyc_try with ROk ONormal _ [VCon _ "just" _ _] => true | _ => false end = true.
Proof. vm_compute. reflexivity. Qed.

(** ** Guardedness is a soundness condition for the assumption rule

    [type V = int | V] is not guarded: the recursion is not under a type
    constructor.  The model's [sub] takes union and unfolding steps
    inductively, so an assumption can only be used after a constructor
    step, and [str <= V] does not hold ([unguarded_model]).

    A checker that assumes [(a, b)] on entry and accepts it when it comes
    round again, with no guardedness check, uses an assumption after
    union and unfolding steps alone.  [subA] is what it computes, and it
    proves [str <= V] and [V <= int].  Guardedness is what makes every
    cycle of the algorithm pass a constructor, so the two agree. *)
Definition VB : ty := TUnion TInt (TRV 0).
Definition V : ty := TMu VB.

Inductive subAF (R : ty -> ty -> Prop) : ty -> ty -> Prop :=
| saf_step a b : subF R a b -> subAF R a b
| saf_unionl a b c : R a c -> R b c -> subAF R (TUnion a b) c
| saf_unionr1 a b c : R a b -> subAF R a (TUnion b c)
| saf_unionr2 a b c : R a c -> subAF R a (TUnion b c)
| saf_mul t b : mu_ok t = true -> R (tunfold t) b -> subAF R (TMu t) b
| saf_mur a t : mu_ok t = true -> R a (tunfold t) -> subAF R a (TMu t).

Definition subA (a b : ty) : Prop := exists R, (forall x y, R x y -> subAF R x y) /\ R a b.

Example unguarded_str_below : subA TStr V.
Proof.
  exists (fun x y => (x = TStr /\ y = V) \/ (x = TStr /\ y = TUnion TInt V)). split; [| left; auto].
  intros x y [[-> ->]|[-> ->]].
  - apply saf_mur; [reflexivity|]. right. split; reflexivity.
  - apply saf_unionr2. left. split; reflexivity.
Qed.

Example unguarded_below_int : subA V TInt.
Proof.
  exists (fun x y => (x = V \/ x = TUnion TInt V \/ x = TInt) /\ y = TInt). split; [| auto].
  intros x y [[->|[->| ->]] ->].
  - apply saf_mul; [reflexivity|]. split; [right; left; reflexivity | reflexivity].
  - apply saf_unionl; split; auto.
  - apply saf_step, sf_refl.
Qed.

Example unguarded_model : ~ sub TStr V.
Proof.
  intros H. apply sub_unfold in H.
  assert (G : forall x y, subF sub x y -> x = TStr -> y = V \/ y = TUnion TInt V -> False).
  { intros x y D. induction D; intros Ex Ey; subst; try discriminate;
      destruct Ey as [Ey|Ey]; try discriminate.
    - injection Ey as E1 E2. subst. inversion D.
    - injection Ey as E1 E2. subst. auto.
    - injection Ey as ->. apply IHD; auto. }
  eapply G; eauto.
Qed.

(** ** One assumption set for [<=] and for fresh retyping is unsound

    The checker decides two relations: [<=] ([sub]) and the retyping of a
    fresh value ([rsub]), which is covariant in data but falls back to
    [<=] under a quote.  Each keeps its own assumption set.  A checker that
    keeps one set of pairs for both answers a [<=] question under a quote
    from an assumption made for [rsub].

    [A = {x: [int], f: (-- A)}] and [B = {x: [int | str], f: (-- B)}].
    Retyping a fresh [A] to [B] would widen [x] (fine: it is fresh) and needs
    [(-- A) <= (-- B)], so [A <= B], which is false: the quote returns a
    shared [A] whose list is not fresh.  The mixed relation [mixed] (each
    step may be a [rsub] step or a [sub] step, children answered from the
    one set) proves [A] to [B]; [rsub] does not, and the program that uses
    it gets stuck. *)
Definition ABB (x : ty) : ty :=
  TRec [("x", FReq (TList x)); ("f", FReq (TQuote [] (Some [TRV 0])))] FAbs.
Definition TA : ty := TMu (ABB TInt).
Definition TBB : ty := TMu (ABB IS).
Definition QA : ty := TQuote [] (Some [TA]).
Definition QB : ty := TQuote [] (Some [TBB]).

Inductive mixF (R : ty -> ty -> Prop) : ty -> ty -> Prop :=
| mix_fresh a b : rsubF R a b -> mixF R a b
| mix_shared a b : subF R a b -> mixF R a b.

Definition mixed (a b : ty) : Prop := exists R, (forall x y, R x y -> mixF R x y) /\ R a b.

Example mixed_accepts : mixed TA TBB.
Proof.
  exists (fun x y => (x = TA /\ y = TBB) \/ (x = TList TInt /\ y = TList IS) \/
                     (x = TInt /\ y = IS) \/ (x = QA /\ y = QB)).
  split; [| left; auto].
  intros x y [[-> ->]|[[-> ->]|[[-> ->]|[-> ->]]]].
  - apply mix_fresh, rf_mul; [reflexivity|]. apply rf_mur; [reflexivity|]. cbn.
    apply rf_rec. intros k. unfold field_at. simpl. labels.
    + apply frs_req. right; left; auto.
    + apply frs_req. right; right; right; auto.
    + apply frs_abs.
  - apply mix_fresh, rf_list. right; right; left; auto.
  - apply mix_fresh, rf_sub. apply s_unionr1, s_refl.
  - (* the quote: a [<=] step answered from the retyping assumption *)
    apply mix_shared, sf_quote; constructor. constructor; [| constructor]. left; auto.
Qed.

Lemma not_sub_ab : ~ sub TA TBB.
Proof.
  intros H0. apply sub_unfold in H0.
  assert (G : forall x y, subF sub x y -> (x = TA \/ x = tunfold (ABB TInt)) ->
                (y = TBB \/ y = tunfold (ABB IS)) -> False).
  { intros x y D. induction D; intros Ex Ey; unfold TA, TBB in *.
    all: destruct Ex as [Ex|Ex]; destruct Ey as [Ey|Ey]; cbn in Ex, Ey; try discriminate.
    all: try (rewrite Ex in Ey; discriminate).
    all: try (injection Ex as Et; subst t; apply IHD; auto; fail).
    all: try (injection Ey as Et; subst t; apply IHD; auto; fail).
    (* the two shapes: [x] is [[int]] on one side and [[int | str]] on the other *)
    inversion Ex; inversion Ey; subst.
    match goal with Hf : forall k, fsubR sub _ _ |- _ =>
      specialize (Hf "x"); unfold field_at in Hf; simpl in Hf; inversion Hf; subst end.
    match goal with Hs : sub (TList (TUnion TInt TStr)) (TList TInt) |- _ => apply sub_list_inv in Hs as [Hs _] end.
    match goal with Hs : sub (TUnion TInt TStr) TInt |- _ => apply sub_unfold in Hs; inversion Hs; subst end.
    match goal with Hs : subF sub TStr TInt |- _ => inversion Hs end. }
  eapply G; eauto.
Qed.

Example rsub_rejects : ~ rsub TA TBB.
Proof.
  intros H0. apply rsub_unfold in H0.
  assert (G : forall x y, rsubF rsub x y -> (x = TA \/ x = tunfold (ABB TInt)) ->
                (y = TBB \/ y = tunfold (ABB IS)) -> False).
  { intros x y D. induction D; intros Ex Ey.
    1: { (* a [<=] step *)
      assert (Hab : sub TA TBB).
      { destruct Ex as [->| ->], Ey as [->| ->]; auto.
        - eapply sub_trans; [exact H | apply sub_unfold_r; reflexivity].
        - eapply sub_trans; [apply sub_unfold_l; reflexivity | exact H].
        - eapply sub_trans; [apply sub_unfold_l; reflexivity |].
          eapply sub_trans; [exact H | apply sub_unfold_r; reflexivity]. }
      exact (not_sub_ab Hab). }
    all: destruct Ex as [Ex|Ex]; destruct Ey as [Ey|Ey]; cbn in Ex, Ey; try discriminate.
    all: try (injection Ex as Et; subst t; apply IHD; auto; fail).
    all: try (injection Ey as Et; subst t; apply IHD; auto; fail).
    (* the two shapes: the quote at [f] needs [A <= B] *)
    inversion Ex; inversion Ey; subst.
    match goal with Hf : forall k, frsubR rsub _ _ |- _ =>
      specialize (Hf "f"); unfold field_at in Hf; simpl in Hf; inversion Hf; subst end.
    match goal with Hr : rsub _ _ |- _ => apply rsub_unfold in Hr; inversion Hr; subst end.
    match goal with Hs : sub _ _ |- _ => apply sub_unfold in Hs; inversion Hs; subst end.
    repeat match goal with
      | Ho : osubR sub (Some _) (Some _) |- _ => inversion Ho; subst; clear Ho
      | Hl : subsR sub (_ :: _) (_ :: _) |- _ => inversion Hl; subst; clear Hl
      end.
    eapply not_sub_ab; eassumption. }
  eapply G; eauto.
Qed.

(** [r = {x: [1], f: (@r)}] is a shared [A].  [v = {x: [2], f: (@r)}] is a
    fresh [A]; the mixed checker retypes it to [B].  Then [v]'s [f] gives [r]
    at type [B], a string goes into [r]'s list, and [r] reads it as an int. *)
Definition hole_mixed : prog :=
  [ WDictNew; WNil; WInt 1; WPush; WSetK "x"; WQuote [WLoad "r"]; WSetK "f"; WStore "r"
  ; WDictNew; WNil; WInt 2; WPush; WSetK "x"; WQuote [WLoad "r"]; WSetK "f"
  ; WGetReq "f"; WExec; WGetReq "x"; WStr "s"; WPush; WDrop
  ; WLoad "r"; WGetReq "x"; WInt 1; WGetAt; WInt 1; WAdd ].

Example hole_mixed_stuck : is_stuck (run hole_mixed) = true.
Proof. vm_compute. reflexivity. Qed.

Example hole_mixed_rejected : forall G R s, ~ T nosigs G LNone LNone R hole_mixed [] s.
Proof.
  intros G R s HT. apply (soundness nosigs nodefs (fun f ins outs H => match H with end) (maybe_ctors_ok _) G R _ s HT 200).
  vm_compute. reflexivity.
Qed.

(** ** Joins that meet a recursive type

    A join never widens inside a recursive type ([ajoin] in Join.v): it
    takes the other arm's type when one is below the other ([<=], or the
    fresh retype when both arms are fresh), makes a union when the kinds do
    not overlap, and otherwise fails.  [le_ex] is a decision procedure that
    knows the facts these examples need, each proved ([le_ex_ok]). *)
Definition PersonLit : ty :=
  TRec [("friends", FReq (TList Person)); ("age", FReq TInt); ("name", FReq TStr)] FAbs.

Definition le_ex (fr : bool) (a b : ty) : bool :=
  (ty_eqb a TInt && ty_eqb b Json)                                (* int <= Json *)
  || (fr && ty_eqb a (TList TInt) && ty_eqb b Json)               (* a fresh [int] may become Json *)
  || (ty_eqb a PersonLit && ty_eqb b Person).                     (* the literal <= Person *)

Lemma lit_below_person : sub PersonLit Person.
Proof.
  apply s_mur; [reflexivity|]. cbn. apply s_rec. intros k. unfold field_at. simpl. labels;
    first [ apply fs_req; apply s_refl | apply fs_open ].
Qed.

Lemma int_below_json : sub TInt Json.
Proof. apply s_mur; [reflexivity|]. cbn. pick_s ltac:(apply s_refl). Qed.

Lemma fresh_ints_json : rsub (TList TInt) Json.
Proof.
  apply rs_mur; [reflexivity|]. cbn.
  pick_r ltac:(apply rs_list, rsub_json_base; simpl; tauto).
Qed.

Lemma le_ex_ok fr a b : le_ex fr a b = true -> jrel fr a b.
Proof.
  unfold le_ex. intros E.
  apply orb_true_iff in E as [E|E]; [apply orb_true_iff in E as [E|E]|].
  - apply andb_true_iff in E as [E1 E2]. apply ty_eqb_true in E1, E2. subst.
    destruct fr; simpl; [apply rs_sub|]; apply int_below_json.
  - apply andb_true_iff in E as [E E2]. apply andb_true_iff in E as [F E1].
    apply ty_eqb_true in E1, E2. subst. simpl. apply fresh_ints_json.
  - apply andb_true_iff in E as [E1 E2]. apply ty_eqb_true in E1, E2. subst.
    destruct fr; simpl; [apply rs_sub|]; apply lit_below_person.
Qed.

(** [true if 5 else parseJson end]: int is below Json.  The result is
    shared, since one arm is. *)
Example join_int_json : join_slot le_ex (Sh, TInt) (Dp, Json) = Some (Sh, Json).
Proof. vm_compute. reflexivity. Qed.

(** [true if [1] else parseJson end]: both fresh, and a fresh [[int]] may
    become a Json.  The result is fresh. *)
Example join_fresh_list_json : join_slot le_ex (Dp, TList TInt) (Dp, Json) = Some (Dp, Json).
Proof. vm_compute. reflexivity. Qed.

(** [[1] xs!  true if @xs else parseJson end]: [xs] is stored, [[int]] is not
    below Json (lists are invariant), and both can be lists: no join.  A
    [Json] here would let a [list l] arm write into [xs]. *)
Example join_shared_list_json : join_slot le_ex (Sh, TList TInt) (Dp, Json) = None.
Proof. vm_compute. reflexivity. Qed.

(** Different kinds: a union ([Maybe] stands in for [datetime], which the
    model does not have). *)
Example join_json_maybe :
  join_slot le_ex (Sh, Json) (Sh, TMaybe TInt) = Some (Sh, TUnion Json (TMaybe TInt)).
Proof. vm_compute. reflexivity. Qed.

(** [def load ( -- Person)]; [true if load else {name: "a", age: 1,
    friends: []} end]: the literal is below [Person]. *)
Example join_person_lit : join_slot le_ex (Sh, Person) (Dp, PersonLit) = Some (Sh, Person).
Proof. vm_compute. reflexivity. Qed.

(** Inside a fresh widening: [[Person]] from [parseJson tryAs [Person] ?] and
    a fresh [[{name: "b", ...}]].  The lists widen inside, and at the alias
    the rule takes [Person]. *)
Example join_nested :
  join_slot le_ex (Dp, TList Person) (Dp, TList PersonLit) = Some (Dp, TList Person).
Proof. vm_compute. reflexivity. Qed.

(** Two different recursive types: [type A = {x: [A], y: int}],
    [type B = {x: [B], y: str}].  Widening inside would ask for the join of
    [A] and [B] again; the rule fails at once instead.  Declaring
    [type C = {x: [C], y: int | str}] and writing [as C] in both arms gives
    two [C]s. *)
Definition RA : ty := TMu (TRec [("x", FReq (TList (TRV 0))); ("y", FReq TInt)] FOpen).
Definition RB : ty := TMu (TRec [("x", FReq (TList (TRV 0))); ("y", FReq TStr)] FOpen).
Definition RC : ty := TMu (TRec [("x", FReq (TList (TRV 0))); ("y", FReq IS)] FOpen).

Example join_two_recursive : join_slot le_ex (Dp, RA) (Dp, RB) = None.
Proof. vm_compute. reflexivity. Qed.

Example join_declared : join_slot le_ex (Dp, RC) (Dp, RC) = Some (Dp, RC).
Proof. vm_compute. reflexivity. Qed.

(** [as C] is a fresh retype on each arm: [A] may become [C]. *)
Example a_retypes_to_c : rsub RA RC.
Proof.
  apply (rsub_coind_upto (fun x y => (x = RA /\ y = RC) \/ (x = TList RA /\ y = TList RC))); [| auto].
  intros x y [[-> ->]|[-> ->]].
  - apply rf_mul; [reflexivity|]. apply rf_mur; [reflexivity|]. cbn.
    apply rf_rec. intros k. unfold field_at. simpl. labels.
    + apply frs_req. left; right; auto.
    + apply frs_req. right. apply rs_sub, s_unionr1, s_refl.
    + apply frs_open.
  - apply rf_list. left; left; auto.
Qed.

(** Each join above checks in the core, by [join_slot_ub] with [le_ex_ok]. *)
Example join_nested_ub :
  slot_sub (Dp, TList Person) (Dp, TList Person) /\ slot_sub (Dp, TList PersonLit) (Dp, TList Person).
Proof. apply (join_slot_ub le_ex le_ex_ok _ _ _ join_nested). Qed.
