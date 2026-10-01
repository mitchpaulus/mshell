(** * Syntax of the mshell core calculus: types, values, heap objects, words.

    This is the core of [ai/type-core-calculus.typ].  Everything here is
    deliberately small; see README.md for what is and is not modeled. *)

From Stdlib Require Import String List Arith Bool.
Import ListNotations.

Definition label := string.
Definition var := string.
Definition loc := nat.

(** ** Types

    [TRec fs r] is every dictionary-kinded type.  [fs] are the declared
    labels and [r] is the status of every other label (the "remainder").

    - A shape [{a: int, b?: str}] (exact)   is [TRec [(a, FReq int); (b, FOpt str)] FAbs].
    - A shape with [*: T] remainder         has remainder [FOpt T].
    - An open shape                         has remainder [FOpen].
    - A dictionary [{str: T}]               is [TRec [] (FDict T)].

    Per label, [FReq t] means present with type [t]; [FOpt t] may be present
    with type [t] and may be written; [FDict t] is like [FOpt t] but may also be
    deleted; [FAbs] means absent; [FOpen] means unknown (read-only).

    [TQuote ins None] is a quote whose output side is [never]. Stacks are
    written top-first everywhere in the formalization.

    [TEnum E args] is the enum [E] at arguments [args]; [TParam i] is the
    [i]th parameter of an enum declaration and appears only in the payload
    types of constructors (see [ename] below and Subtyping.v).

    [TVar x] is a rigid type variable, as in a definition's signature
    [(a -- a)]: the body is checked once with [a] rigid (Generic.v).  No
    value has type [TVar x]; it is substituted away at each instance.

    [TMu t] is a recursive type, the doc's recursive [type] alias: [TRV 0]
    inside [t] stands for [TMu t] itself (de Bruijn indices, so [TRV 1] under
    a nested [TMu] names the outer one).  [type Json = int | [Json]] is
    [TMu (TUnion TInt (TList (TRV 0)))].  A recursive type is used only when
    it is closed ([mu_ok]): aliases are not generic, so the body mentions no
    type variable and no enum parameter.  Substitution therefore never
    enters one: to [subst] and [tsub] a [TMu] is an atom. *)

(** ** Enum identities

    [p_var] is a parameter's variance for subtyping.  [p_fresh] says every
    occurrence of the parameter is in a data position (a payload, a list
    element, a dict value or a fresh-covariant argument of an enum, never
    under a quote), so a *fresh* value may retype that argument
    covariantly ([rsub]).  [en_imm] says a value of the enum holds no list
    or dict when its arguments cannot.

    In the model an enum's identity is its name together with this
    information.  The checker has one declaration per name, so this names
    the same enums; it lets subtyping and immutability be defined without a
    global declaration environment.  Payload types live in the typing
    environment ([g_ctors] in Typing.v) and are checked against the identity
    by [wf_payload] (Subtyping.v). *)
Inductive variance := VCo | VContra | VInv.

Record eparam := { p_var : variance; p_fresh : bool }.

Record ename := { en_name : string; en_params : list eparam; en_imm : bool }.

Definition cname := string.

Definition variance_eqb (a b : variance) : bool :=
  match a, b with VCo, VCo | VContra, VContra | VInv, VInv => true | _, _ => false end.

Definition eparam_eqb (a b : eparam) : bool :=
  variance_eqb (p_var a) (p_var b) && Bool.eqb (p_fresh a) (p_fresh b).

Fixpoint eparams_eqb (a b : list eparam) : bool :=
  match a, b with
  | [], [] => true
  | x :: a', y :: b' => eparam_eqb x y && eparams_eqb a' b'
  | _, _ => false
  end.

Definition ename_eqb (a b : ename) : bool :=
  String.eqb (en_name a) (en_name b) && eparams_eqb (en_params a) (en_params b)
  && Bool.eqb (en_imm a) (en_imm b).

Lemma ename_eqb_true a b : ename_eqb a b = true -> a = b.
Proof.
  destruct a as [n1 p1 i1], b as [n2 p2 i2]. unfold ename_eqb; simpl.
  intros E. apply andb_true_iff in E as [E E3]. apply andb_true_iff in E as [E1 E2].
  apply String.eqb_eq in E1. apply Bool.eqb_prop in E3. subst.
  assert (p1 = p2); [|subst; reflexivity].
  revert p2 E2. induction p1 as [|[v1 f1] p1 IH]; intros [|[v2 f2] p2] E; simpl in E; try discriminate; auto.
  apply andb_true_iff in E as [Ep E]. unfold eparam_eqb in Ep; simpl in Ep.
  apply andb_true_iff in Ep as [Ev Ef]. apply Bool.eqb_prop in Ef.
  destruct v1, v2; simpl in Ev; try discriminate; subst; f_equal; auto.
Qed.

Lemma ename_eqb_refl a : ename_eqb a a = true.
Proof.
  destruct a as [n p i]. unfold ename_eqb; simpl. rewrite String.eqb_refl, Bool.eqb_reflx.
  induction p as [|[v f] p IH]; simpl; auto.
  unfold eparam_eqb; simpl. rewrite Bool.eqb_reflx. destruct v; simpl; auto.
Qed.

Inductive ty : Type :=
| TInt | TStr | TBool
| TBot                     (* the empty type; [none : Maybe[Bot]] *)
| TTop                     (* an unknown type: abstract contents *)
| TList (t : ty)
| TRec (fs : list (label * fstat)) (r : fstat)
| TUnion (a b : ty)
| TQuote (ins : list ty) (outs : option (list ty))
| TEnum (E : ename) (args : list ty)
| TParam (i : nat)
| TVar (x : nat)           (* a rigid type variable of a polymorphic definition *)
| TMu (t : ty)             (* a recursive type; [TRV 0] in [t] is the type itself *)
| TRV (n : nat)            (* a recursion variable (de Bruijn index) *)
with fstat : Type :=
| FReq (t : ty) | FOpt (t : ty) | FDict (t : ty) | FAbs | FOpen.

Fixpoint lookup {A : Type} (k : string) (l : list (string * A)) : option A :=
  match l with
  | [] => None
  | (k', a) :: l' => if String.eqb k k' then Some a else lookup k l'
  end.

Definition field_at (k : label) (fs : list (label * fstat)) (r : fstat) : fstat :=
  match lookup k fs with Some f => f | None => r end.

(** The type of a value stored under a label with the given status. An
    absent label has type [TBot]: no value can be stored there. *)
Definition fty (f : fstat) : ty :=
  match f with
  | FReq t | FOpt t | FDict t => t
  | FAbs => TBot
  | FOpen => TTop
  end.

Definition TDict (t : ty) : ty := TRec [] (FDict t).

(** [forallb2 f ps xs]: [f] holds pairwise and the lists have equal length.
    Structural on the second list, so a fixpoint over types may recurse
    through it. *)
Definition forallb2 {A B : Type} (f : A -> B -> bool) :=
  fix go (l1 : list A) (l2 : list B) {struct l2} : bool :=
    match l1, l2 with
    | [], [] => true
    | x :: l1', y :: l2' => f x y && go l1' l2'
    | _, _ => false
    end.

(** Substitution of enum arguments for [TParam]s. *)
Fixpoint subst (a : list ty) (t : ty) : ty :=
  match t with
  | TParam i => nth i a TBot
  | TList t' => TList (subst a t')
  | TRec fs r => TRec (map (fun p => (fst p, fsubst a (snd p))) fs) (fsubst a r)
  | TUnion x y => TUnion (subst a x) (subst a y)
  | TQuote ins outs =>
      TQuote (map (subst a) ins) (match outs with Some o => Some (map (subst a) o) | None => None end)
  | TEnum E args => TEnum E (map (subst a) args)
  | TInt | TStr | TBool | TBot | TTop | TVar _ | TMu _ | TRV _ => t
  end
with fsubst (a : list ty) (f : fstat) : fstat :=
  match f with
  | FReq t => FReq (subst a t) | FOpt t => FOpt (subst a t) | FDict t => FDict (subst a t)
  | FAbs => FAbs | FOpen => FOpen
  end.

(** Substitution of types for type variables, and free type variables. *)
Fixpoint tsub (th : nat -> ty) (t : ty) : ty :=
  match t with
  | TVar x => th x
  | TList t' => TList (tsub th t')
  | TRec fs r => TRec (map (fun p => (fst p, ftsub th (snd p))) fs) (ftsub th r)
  | TUnion x y => TUnion (tsub th x) (tsub th y)
  | TQuote ins outs =>
      TQuote (map (tsub th) ins) (match outs with Some o => Some (map (tsub th) o) | None => None end)
  | TEnum E args => TEnum E (map (tsub th) args)
  | TInt | TStr | TBool | TBot | TTop | TParam _ | TMu _ | TRV _ => t
  end
with ftsub (th : nat -> ty) (f : fstat) : fstat :=
  match f with
  | FReq t => FReq (tsub th t) | FOpt t => FOpt (tsub th t) | FDict t => FDict (tsub th t)
  | FAbs => FAbs | FOpen => FOpen
  end.

Fixpoint fvt (t : ty) : list nat :=
  match t with
  | TVar x => [x]
  | TList t' => fvt t'
  | TRec fs r => flat_map (fun p => ffvt (snd p)) fs ++ ffvt r
  | TUnion x y => fvt x ++ fvt y
  | TQuote ins outs => flat_map fvt ins ++ match outs with Some o => flat_map fvt o | None => [] end
  | TEnum _ args => flat_map fvt args
  | TInt | TStr | TBool | TBot | TTop | TParam _ | TMu _ | TRV _ => []
  end
with ffvt (f : fstat) : list nat :=
  match f with FReq t | FOpt t | FDict t => fvt t | FAbs | FOpen => [] end.

(** ** An induction principle for types with nested lists *)
Section TyInd.
Variable P : ty -> Prop.
Variable Q : fstat -> Prop.
Hypothesis HInt : P TInt.
Hypothesis HStr : P TStr.
Hypothesis HBool : P TBool.
Hypothesis HBot : P TBot.
Hypothesis HTop : P TTop.
Hypothesis HList : forall t, P t -> P (TList t).
Hypothesis HRec : forall fs r, Forall (fun p => Q (snd p)) fs -> Q r -> P (TRec fs r).
Hypothesis HUnion : forall a b, P a -> P b -> P (TUnion a b).
Hypothesis HQuote : forall ins outs, Forall P ins ->
  (forall o, outs = Some o -> Forall P o) -> P (TQuote ins outs).
Hypothesis HEnum : forall E args, Forall P args -> P (TEnum E args).
Hypothesis HParam : forall i, P (TParam i).
Hypothesis HVar : forall x, P (TVar x).
Hypothesis HMu : forall t, P t -> P (TMu t).
Hypothesis HRV : forall n, P (TRV n).
Hypothesis QReq : forall t, P t -> Q (FReq t).
Hypothesis QOpt : forall t, P t -> Q (FOpt t).
Hypothesis QDict : forall t, P t -> Q (FDict t).
Hypothesis QAbs : Q FAbs.
Hypothesis QOpen : Q FOpen.

Fixpoint ty_ind2 (t : ty) : P t :=
  match t with
  | TInt => HInt | TStr => HStr | TBool => HBool | TBot => HBot | TTop => HTop
  | TList t' => HList t' (ty_ind2 t')
  | TRec fs r =>
      HRec fs r
        ((fix go (l : list (label * fstat)) : Forall (fun p => Q (snd p)) l :=
            match l with
            | [] => Forall_nil _
            | (k, f) :: l' => @Forall_cons _ (fun p => Q (snd p)) (k, f) l' (fstat_ind2 f) (go l')
            end) fs)
        (fstat_ind2 r)
  | TUnion a b => HUnion a b (ty_ind2 a) (ty_ind2 b)
  | TQuote ins outs =>
      HQuote ins outs
        ((fix go (l : list ty) : Forall P l :=
            match l with [] => Forall_nil _ | x :: l' => Forall_cons x (ty_ind2 x) (go l') end) ins)
        (match outs as o0 return (forall o, o0 = Some o -> Forall P o) with
         | Some o1 => fun o E =>
             match E in _ = y return (match y with Some o' => Forall P o' | None => True end) with
             | eq_refl =>
                 (fix go (l : list ty) : Forall P l :=
                    match l with [] => Forall_nil _ | x :: l' => Forall_cons x (ty_ind2 x) (go l') end) o1
             end
         | None => fun o E => match E with end
         end)
  | TEnum E args =>
      HEnum E args
        ((fix go (l : list ty) : Forall P l :=
            match l with [] => Forall_nil _ | x :: l' => Forall_cons x (ty_ind2 x) (go l') end) args)
  | TParam i => HParam i
  | TVar x => HVar x
  | TMu t' => HMu t' (ty_ind2 t')
  | TRV n => HRV n
  end
with fstat_ind2 (f : fstat) : Q f :=
  match f with
  | FReq t => QReq t (ty_ind2 t)
  | FOpt t => QOpt t (ty_ind2 t)
  | FDict t => QDict t (ty_ind2 t)
  | FAbs => QAbs
  | FOpen => QOpen
  end.
End TyInd.

(** ** Recursive types

    [tclosed d t]: [t] mentions no type variable and no enum parameter, and
    every recursion variable is bound, with [d] binders around [t]. *)
Fixpoint tclosed (d : nat) (t : ty) : bool :=
  match t with
  | TInt | TStr | TBool | TBot | TTop => true
  | TParam _ | TVar _ => false
  | TRV n => n <? d
  | TMu b => tclosed (S d) b
  | TList t' => tclosed d t'
  | TRec fs r => forallb (fun p => ftclosed d (snd p)) fs && ftclosed d r
  | TUnion a b => tclosed d a && tclosed d b
  | TQuote ins outs =>
      forallb (tclosed d) ins && match outs with Some o => forallb (tclosed d) o | None => true end
  | TEnum _ args => forallb (tclosed d) args
  end
with ftclosed (d : nat) (f : fstat) : bool :=
  match f with FReq t | FOpt t | FDict t => tclosed d t | FAbs | FOpen => true end.

(** The body of [TMu t] is closed apart from [TRV 0]. *)
Definition mu_ok (t : ty) : bool := tclosed 1 t.

(** [musubst k s t] puts [s] (a closed type) for recursion variable [k]. *)
Fixpoint musubst (k : nat) (s : ty) (t : ty) : ty :=
  match t with
  | TRV n => if Nat.eqb n k then s else t
  | TMu b => TMu (musubst (S k) s b)
  | TList t' => TList (musubst k s t')
  | TRec fs r => TRec (map (fun p => (fst p, fmusubst k s (snd p))) fs) (fmusubst k s r)
  | TUnion a b => TUnion (musubst k s a) (musubst k s b)
  | TQuote ins outs =>
      TQuote (map (musubst k s) ins) (match outs with Some o => Some (map (musubst k s) o) | None => None end)
  | TEnum E args => TEnum E (map (musubst k s) args)
  | TInt | TStr | TBool | TBot | TTop | TParam _ | TVar _ => t
  end
with fmusubst (k : nat) (s : ty) (f : fstat) : fstat :=
  match f with
  | FReq t => FReq (musubst k s t) | FOpt t => FOpt (musubst k s t) | FDict t => FDict (musubst k s t)
  | FAbs => FAbs | FOpen => FOpen
  end.

(** One step of unfolding: the body of [TMu t] with [TMu t] for [TRV 0]. *)
Definition tunfold (t : ty) : ty := musubst 0 (TMu t) t.

(** Runtime kinds.  Each enum is its own kind, shared by all its instances. *)
Inductive kind := KInt | KStr | KBool | KList | KDict | KQuote | KEnum (E : ename).

Definition kind_eqb (a b : kind) : bool :=
  match a, b with
  | KInt, KInt | KStr, KStr | KBool, KBool
  | KList, KList | KDict, KDict | KQuote, KQuote => true
  | KEnum x, KEnum y => ename_eqb x y
  | _, _ => false
  end.

(** ** The built-in [Maybe]

    [enum Maybe[a] = just a | none end]: an ordinary generic enum, covariant,
    fresh-covariant, and immutable when its argument is.  [TMaybe t] is its
    type at [t].  The typing environment must declare its two constructors
    ([maybe_ok] in Typing.v); its values and the words [just], [none] and
    [?] are below the definitions of words and values. *)
Definition EMaybe : ename :=
  {| en_name := "Maybe"; en_params := [{| p_var := VCo; p_fresh := true |}]; en_imm := true |}.

Notation TMaybe t := (TEnum EMaybe [t]).

(** ** Words

    Curry style: no word carries a type except [WTryAs], whose target type
    the runtime needs for validation.  [as T] is not a word: it is the
    subsumption rule of the typing judgment.  [tryAs] validates in place and
    never copies; copying is the explicit word [WCopy]. *)
Inductive word : Type :=
| WInt (n : nat) | WStr (s : string) | WBool (b : bool)
| WAdd                          (* int int -- int *)
| WCat                          (* str str -- str *)
| WDup | WDrop | WSwap
| WLoad (x : var) | WStore (x : var)
| WQuote (e : list word) | WExec
| WIf (e1 e2 : list word)
| WLoop (e : list word)
| WBreak | WContinue | WReturn | WExit
| WCall (f : string)
| WNil | WPush | WGetAt | WSetAt  (* lists; out of range is a checked error *)
| WEach (e : list word)           (* child-stack builtin with a literal body *)
| WMap (e : list word)            (* child-stack builtin: a new list of the body's results *)
| WTake | WSkip                   (* int list -- list: a new list of the first n elements, or
                                     of all but the first n; a count past the end is clamped *)
| WSlice (a : nat) (b : option nat) (* index slice [a:b] ([b = None]: [a:]), a new list;
                                     out of range is a checked error *)
| WDictNew
| WGetK (k : label)               (* literal key, returns Maybe *)
| WGetReq (k : label)             (* literal key known required: returns the value *)
| WSetK (k : label)
| WDel (k : label)
| WGetD                           (* runtime key, returns Maybe *)
| WSetD                           (* runtime key *)
| WKindIf (k : kind) (e1 e2 : list word)   (* kind pattern; value stays on the stack *)
| WTryAs (u : ty)                          (* validation, in place *)
| WCopy                                    (* explicit deep copy; the result is fresh *)
| WCon (E : ename) (c : cname) (pts : list ty)   (* constructor; payloads top-first *)
| WCase (E : ename) (arms : list (cname * list word)). (* constructor match; payloads pushed *)

Definition prog := list word.

(** [just], [none], and [?] (unwrap): a match with only a [just] arm, so [?]
    on [none] is a checked error, like any constructor with no arm. *)
Notation wjust := (WCon EMaybe "just"%string [TParam 0]).
Notation wnone := (WCon EMaybe "none"%string []).
Notation wunwrap := (WCase EMaybe [("just"%string, [])]).

(** ** Values and heap *)
Inductive val : Type :=
| VInt (n : nat) | VStr (s : string) | VBool (b : bool)
| VLoc (l : loc)                 (* a list or dict object *)
| VClo (sc : loc) (e : prog)     (* a quote closing over a variable scope *)
| VCon (E : ename) (c : cname) (pts : list ty) (vs : list val).
    (* an enum value: its enum, constructor, the constructor's declared
       payload types (the runtime's pointer to the declaration, which the
       validator reads) and its payloads *)

Notation vjust v := (VCon EMaybe "just"%string [TParam 0] [v]).
Notation vnone := (VCon EMaybe "none"%string [] []).

Inductive obj : Type :=
| OList (vs : list val)
| ODict (kvs : list (string * val))
| OScope (kvs : list (string * val)).

Definition heap := list obj.

Fixpoint set_nth {A : Type} (n : nat) (x : A) (l : list A) : list A :=
  match l, n with
  | [], _ => []
  | _ :: t, 0 => x :: t
  | h :: t, S n' => h :: set_nth n' x t
  end.

Definition remove_key {A : Type} (k : string) (l : list (string * A)) :=
  filter (fun p => negb (String.eqb (fst p) k)) l.

Definition dset {A : Type} (k : string) (v : A) (l : list (string * A)) :=
  (k, v) :: remove_key k l.

Lemma flat_map_nil_iff {A B} (f : A -> list B) l :
  flat_map f l = [] <-> forall x, In x l -> f x = [].
Proof.
  induction l as [|y l IH]; simpl; split; intros H; auto.
  - tauto.
  - apply app_eq_nil in H as [H1 H2]. intros x [<-|Hx]; auto. apply IH; auto.
  - rewrite H by (left; reflexivity). simpl. apply IH. intros; apply H; auto.
Qed.

