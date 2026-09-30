(** * A definitional interpreter for the core.

    [eval] returns [RStuck] exactly where the Go runtime would report a type
    mismatch ("Cannot add an integer to a String", a missing required key,
    stack underflow, a non-quote given to [x], ...).  Checked errors
    ([?] on none, index out of range, reading an unset variable) return
    [RErr].  Soundness (Soundness.v) says a well-typed program never
    evaluates to [RStuck], for any amount of fuel. *)

From Stdlib Require Import String List Arith Bool.
Import ListNotations.
From MshellCore Require Import Syntax.
Open Scope list_scope.

Inductive outcome := ONormal | OBreak | OContinue | OReturn.

Inductive result :=
| RTimeout                                    (* out of fuel *)
| RStuck                                      (* a runtime type error *)
| RErr                                        (* a checked error *)
| RExit (code : nat)
| ROk (o : outcome) (H : heap) (st : list val).

Definition kind_of (H : heap) (v : val) : option kind :=
  match v with
  | VInt _ => Some KInt
  | VStr _ => Some KStr
  | VBool _ => Some KBool
  | VNone | VJust _ => Some KMaybe
  | VClo _ _ => Some KQuote
  | VCon E _ _ _ => Some (KEnum E)
  | VLoc l =>
      match nth_error H l with
      | Some (OList _) => Some KList
      | Some (ODict _) => Some KDict
      | _ => None
      end
  end.

(** ** Validation ([tryAs])

    [validate f H v t] checks [v] against [t] in place, with a work budget
    [f].  Running out of budget is [None], a checked error: this is how a
    cyclic value meets a recursive type (an enum whose payload holds a list
    of the enum, say).  An enum value is checked by identity and then its
    payloads against the constructor's payload types, which the value
    carries, with the target's arguments substituted.  A recursive type is
    unfolded one step, which costs one unit of budget: a cyclic value
    validated against a recursive type runs out, which is a checked error. *)
Fixpoint oforall {A : Type} (g : A -> option bool) (l : list A) : option bool :=
  match l with
  | [] => Some true
  | x :: l' => match g x with Some true => oforall g l' | r => r end
  end.

Fixpoint oforall2 {A B : Type} (g : A -> B -> option bool) (l1 : list A) (l2 : list B) : option bool :=
  match l1, l2 with
  | [], [] => Some true
  | x :: l1', y :: l2' => match g x y with Some true => oforall2 g l1' l2' | r => r end
  | _, _ => Some false
  end.

Definition oand (x y : option bool) : option bool :=
  match x with Some true => y | r => r end.

Fixpoint validate (f : nat) (H : heap) (v : val) (t : ty) {struct f} : option bool :=
  match f with
  | 0 => None
  | S f' =>
  let vf := fun (st : fstat) (ov : option val) =>
    match st with
    | FReq t' => match ov with Some x => validate f' H x t' | None => Some false end
    | FOpt t' | FDict t' => match ov with Some x => validate f' H x t' | None => Some true end
    | FAbs => match ov with Some _ => Some false | None => Some true end
    | FOpen => Some true
    end in
  match t with
  | TInt => Some (match v with VInt _ => true | _ => false end)
  | TStr => Some (match v with VStr _ => true | _ => false end)
  | TBool => Some (match v with VBool _ => true | _ => false end)
  | TBot | TParam _ | TVar _ | TRV _ => Some false
  | TTop => Some true
  | TMu t' => if mu_ok t' then validate f' H v (tunfold t') else Some false
  | TQuote _ _ => Some false          (* not checkable *)
  | TMaybe t' =>
      match v with
      | VNone => Some true
      | VJust x => validate f' H x t'
      | _ => Some false
      end
  | TList t' =>
      match v with
      | VLoc l =>
          match nth_error H l with
          | Some (OList vs) => oforall (fun x => validate f' H x t') vs
          | _ => Some false
          end
      | _ => Some false
      end
  | TRec fs r =>
      match v with
      | VLoc l =>
          match nth_error H l with
          | Some (ODict kvs) =>
              oand (oforall (fun p => vf (field_at (fst p) fs r) (Some (snd p))) kvs)
                (oand (oforall (fun p => match lookup (fst p) kvs with
                                         | Some _ => Some true
                                         | None => vf (field_at (fst p) fs r) None end) fs)
                      (Some (match r with FReq _ => false | _ => true end)))
          | _ => Some false
          end
      | _ => Some false
      end
  | TUnion a b =>
      match validate f' H v a with Some false => validate f' H v b | r => r end
  | TEnum E a =>
      match v with
      | VCon E' _ pts vs =>
          if ename_eqb E E' then oforall2 (fun x t' => validate f' H x t') vs (map (subst a) pts)
          else Some false
      | _ => Some false
      end
  end
  end.

(** ** The explicit copy ([copy])

    [dcopy] copies every list and dict reachable from a value, per path: two
    paths to one object give two copies, so the result is a tree that
    nothing else references.  Base values and quotes are shared (a quote's
    captured scope is not part of the value).  The fuel [f] bounds how many
    objects deep the copy goes; [WCopy] gives it [length H], which runs out
    only on a cyclic value (a path of distinct objects is at most as long as
    the heap).  Running out is a checked error. *)
Fixpoint mapo (c : heap -> val -> option (heap * val)) (H0 : heap) (vs : list val)
  : option (heap * list val) :=
  match vs with
  | [] => Some (H0, [])
  | x :: xs =>
      match c H0 x with
      | Some (H1, x') =>
          match mapo c H1 xs with
          | Some (H2, xs') => Some (H2, x' :: xs')
          | None => None
          end
      | None => None
      end
  end.

Fixpoint mapo_kv (c : heap -> val -> option (heap * val)) (H0 : heap)
    (kvs : list (string * val)) : option (heap * list (string * val)) :=
  match kvs with
  | [] => Some (H0, [])
  | (k, x) :: rest =>
      match c H0 x with
      | Some (H1, x') =>
          match mapo_kv c H1 rest with
          | Some (H2, rest') => Some (H2, (k, x') :: rest')
          | None => None
          end
      | None => None
      end
  end.

(** Copy the list or dict at [l], copying its elements with [c]; the new
    object is appended to the heap. *)
Definition ocopy (c : heap -> val -> option (heap * val)) (H : heap) (l : loc)
  : option (heap * val) :=
  match nth_error H l with
  | Some (OList vs) =>
      match mapo c H vs with
      | Some (H1, vs') => Some (H1 ++ [OList vs'], VLoc (length H1))
      | None => None
      end
  | Some (ODict kvs) =>
      match mapo_kv c H kvs with
      | Some (H1, kvs') => Some (H1 ++ [ODict kvs'], VLoc (length H1))
      | None => None
      end
  | _ => None
  end.

(** Copy a value, copying the objects it references with [g]. *)
Fixpoint vcopy (g : heap -> loc -> option (heap * val)) (H : heap) (v : val)
  : option (heap * val) :=
  match v with
  | VJust x =>
      match vcopy g H x with
      | Some (H1, x') => Some (H1, VJust x')
      | None => None
      end
  | VLoc l => g H l
  | VCon E c pts vs =>
      match mapo (vcopy g) H vs with
      | Some (H1, vs') => Some (H1, VCon E c pts vs')
      | None => None
      end
  | _ => Some (H, v)
  end.

Fixpoint dcopy (f : nat) (H : heap) (v : val) : option (heap * val) :=
  match f with
  | 0 => vcopy (fun _ _ => None) H v
  | S f' => vcopy (ocopy (dcopy f')) H v
  end.

(** ** The interpreter *)
Section Eval.
(** The validator [tryAs] uses.  The model's is [validate]; the soundness
    proof needs only the two properties in Soundness.v ([vd_fresh],
    [vd_imm]), so any validator with them may replace it. *)
Variable vd : nat -> heap -> val -> ty -> option bool.
Variable defs : string -> option prog.

Definition scope_get (H : heap) (sc : loc) : option (list (string * val)) :=
  match nth_error H sc with Some (OScope kvs) => Some kvs | _ => None end.

Fixpoint evalv (n : nat) (H : heap) (sc : loc) (st : list val) (e : prog) {struct n} : result :=
  match n with
  | 0 => RTimeout
  | S n' =>
  match e with
  | [] => ROk ONormal H st
  | w :: rest =>
  let next := fun H' st' => evalv n' H' sc st' rest in
  let cont := fun r => match r with ROk ONormal H' st' => next H' st' | r => r end in
  match w with
  | WInt m => next H (VInt m :: st)
  | WStr s => next H (VStr s :: st)
  | WBool b => next H (VBool b :: st)
  | WAdd => match st with VInt a :: VInt b :: st' => next H (VInt (b + a) :: st') | _ => RStuck end
  | WCat => match st with VStr a :: VStr b :: st' => next H (VStr (String.append b a) :: st') | _ => RStuck end
  | WDup => match st with v :: st' => next H (v :: v :: st') | _ => RStuck end
  | WDrop => match st with _ :: st' => next H st' | _ => RStuck end
  | WSwap => match st with a :: b :: st' => next H (b :: a :: st') | _ => RStuck end
  | WNone => next H (VNone :: st)
  | WJust => match st with v :: st' => next H (VJust v :: st') | _ => RStuck end
  | WUnwrap =>
      match st with
      | VJust v :: st' => next H (v :: st')
      | VNone :: _ => RErr
      | _ => RStuck
      end
  | WLoad x =>
      match scope_get H sc with
      | Some kvs => match lookup x kvs with Some v => next H (v :: st) | None => RErr end
      | None => RStuck
      end
  | WStore x =>
      match st, scope_get H sc with
      | v :: st', Some kvs => next (set_nth sc (OScope (dset x v kvs)) H) st'
      | _, _ => RStuck
      end
  | WQuote body => next H (VClo sc body :: st)
  | WExec =>
      match st with
      | VClo sc' body :: st' => cont (evalv n' H sc' st' body)
      | _ => RStuck
      end
  | WIf e1 e2 =>
      match st with
      | VBool b :: st' => cont (evalv n' H sc st' (if b then e1 else e2))
      | _ => RStuck
      end
  | WLoop body =>
      match evalv n' H sc st body with
      | ROk ONormal H' st' | ROk OContinue H' st' => evalv n' H' sc st' (WLoop body :: rest)
      | ROk OBreak H' st' => next H' st'
      | r => r
      end
  | WBreak => ROk OBreak H st
  | WContinue => ROk OContinue H st
  | WReturn => ROk OReturn H st
  | WExit => match st with VInt c :: _ => RExit c | _ => RStuck end
  | WCall f =>
      match defs f with
      | Some body =>
          match evalv n' (app H [OScope []]) (length H) st body with
          | ROk ONormal H' st' | ROk OReturn H' st' => next H' st'
          | r => r
          end
      | None => RStuck
      end
  | WNil => next (app H [OList []]) (VLoc (length H) :: st)
  | WPush =>
      match st with
      | x :: VLoc l :: st' =>
          match nth_error H l with
          | Some (OList vs) => next (set_nth l (OList (app vs [x])) H) (VLoc l :: st')
          | _ => RStuck
          end
      | _ => RStuck
      end
  | WGetAt =>
      match st with
      | VInt i :: VLoc l :: st' =>
          match nth_error H l with
          | Some (OList vs) =>
              match nth_error vs i with Some x => next H (x :: st') | None => RErr end
          | _ => RStuck
          end
      | _ => RStuck
      end
  | WSetAt =>
      match st with
      | x :: VInt i :: VLoc l :: st' =>
          match nth_error H l with
          | Some (OList vs) =>
              if i <? length vs then next (set_nth l (OList (set_nth i x vs)) H) (VLoc l :: st')
              else RErr
          | _ => RStuck
          end
      | _ => RStuck
      end
  | WEach body =>
      match st with
      | VLoc l :: st0 =>
          match nth_error H l with
          | Some (OList vs) =>
              let fix go (vs : list val) (H0 : heap) : result :=
                match vs with
                | [] => ROk ONormal H0 st0
                | v :: vs' =>
                    match evalv n' H0 sc [v] body with
                    | ROk ONormal H1 [] => go vs' H1
                    | ROk ONormal _ _ => RStuck
                    | ROk OBreak H1 _ => ROk OBreak H1 st0
                    | ROk OContinue H1 _ => ROk OContinue H1 st0
                    | r => r
                    end
                end in
              cont (go vs H)
          | _ => RStuck
          end
      | _ => RStuck
      end
  | WMap body =>
      match st with
      | VLoc l :: st0 =>
          match nth_error H l with
          | Some (OList vs) =>
              let fix go (vs : list val) (H0 : heap) (acc : list val) : result :=
                match vs with
                | [] => ROk ONormal (app H0 [OList (rev acc)]) (VLoc (length H0) :: st0)
                | v :: vs' =>
                    match evalv n' H0 sc [v] body with
                    | ROk ONormal H1 [v'] => go vs' H1 (v' :: acc)
                    | ROk ONormal _ _ => RStuck
                    | ROk OBreak H1 _ => ROk OBreak H1 st0
                    | ROk OContinue H1 _ => ROk OContinue H1 st0
                    | r => r
                    end
                end in
              cont (go vs H [])
          | _ => RStuck
          end
      | _ => RStuck
      end
  | WDictNew => next (app H [ODict []]) (VLoc (length H) :: st)
  | WGetK k =>
      match st with
      | VLoc l :: st' =>
          match nth_error H l with
          | Some (ODict kvs) =>
              next H (match lookup k kvs with Some x => VJust x | None => VNone end :: st')
          | _ => RStuck
          end
      | _ => RStuck
      end
  | WGetReq k =>
      match st with
      | VLoc l :: st' =>
          match nth_error H l with
          | Some (ODict kvs) =>
              match lookup k kvs with Some x => next H (x :: st') | None => RStuck end
          | _ => RStuck
          end
      | _ => RStuck
      end
  | WSetK k =>
      match st with
      | x :: VLoc l :: st' =>
          match nth_error H l with
          | Some (ODict kvs) => next (set_nth l (ODict (dset k x kvs)) H) (VLoc l :: st')
          | _ => RStuck
          end
      | _ => RStuck
      end
  | WDel k =>
      match st with
      | VLoc l :: st' =>
          match nth_error H l with
          | Some (ODict kvs) => next (set_nth l (ODict (remove_key k kvs)) H) (VLoc l :: st')
          | _ => RStuck
          end
      | _ => RStuck
      end
  | WGetD =>
      match st with
      | VStr k :: VLoc l :: st' =>
          match nth_error H l with
          | Some (ODict kvs) =>
              next H (match lookup k kvs with Some x => VJust x | None => VNone end :: st')
          | _ => RStuck
          end
      | _ => RStuck
      end
  | WSetD =>
      match st with
      | x :: VStr k :: VLoc l :: st' =>
          match nth_error H l with
          | Some (ODict kvs) => next (set_nth l (ODict (dset k x kvs)) H) (VLoc l :: st')
          | _ => RStuck
          end
      | _ => RStuck
      end
  | WKindIf k e1 e2 =>
      match st with
      | v :: _ =>
          match kind_of H v with
          | Some k' => cont (evalv n' H sc st (if kind_eqb k k' then e1 else e2))
          | None => RStuck
          end
      | _ => RStuck
      end
  | WTryAs u =>
      match st with
      | v :: st' =>
          match vd n' H v u with
          | Some true => next H (VJust v :: st')
          | Some false => next H (VNone :: st')
          | None => RErr                     (* validation budget exhausted *)
          end
      | _ => RStuck
      end
  | WCopy =>
      match st with
      | v :: st' =>
          match dcopy (length H) H v with
          | Some (H', v') => next H' (v' :: st')
          | None => RErr                     (* a cyclic value *)
          end
      | _ => RStuck
      end
  | WCon E c pts =>
      let k := length pts in
      if k <=? length st then next H (VCon E c pts (firstn k st) :: skipn k st) else RStuck
  | WCase E arms =>
      match st with
      | VCon E' c _ vs :: st' =>
          if ename_eqb E E' then
            match lookup c arms with
            | Some e => cont (evalv n' H sc (vs ++ st') e)
            | None => RErr                   (* no arm: a checked error *)
            end
          else RStuck
      | _ => RStuck
      end
  end
  end
  end.

End Eval.

(** The interpreter with the model's validator. *)
Definition eval := evalv validate.
