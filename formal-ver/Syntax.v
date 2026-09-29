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
    written top-first everywhere in the formalization. *)
Inductive ty : Type :=
| TInt | TStr | TBool
| TBot                     (* the empty type; [none : Maybe Bot] *)
| TTop                     (* an unknown type: abstract contents *)
| TMaybe (t : ty)
| TList (t : ty)
| TRec (fs : list (label * fstat)) (r : fstat)
| TUnion (a b : ty)
| TQuote (ins : list ty) (outs : option (list ty))
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

(** Runtime kinds. *)
Inductive kind := KInt | KStr | KBool | KMaybe | KList | KDict | KQuote.

Definition kind_eqb (a b : kind) : bool :=
  match a, b with
  | KInt, KInt | KStr, KStr | KBool, KBool | KMaybe, KMaybe
  | KList, KList | KDict, KDict | KQuote, KQuote => true
  | _, _ => false
  end.

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
| WNone | WJust | WUnwrap       (* [?]: none is a checked error *)
| WLoad (x : var) | WStore (x : var)
| WQuote (e : list word) | WExec
| WIf (e1 e2 : list word)
| WLoop (e : list word)
| WBreak | WContinue | WReturn | WExit
| WCall (f : string)
| WNil | WPush | WGetAt | WSetAt  (* lists; out of range is a checked error *)
| WEach (e : list word)           (* child-stack builtin with a literal body *)
| WDictNew
| WGetK (k : label)               (* literal key, returns Maybe *)
| WGetReq (k : label)             (* literal key known required: returns the value *)
| WSetK (k : label)
| WDel (k : label)
| WGetD                           (* runtime key, returns Maybe *)
| WSetD                           (* runtime key *)
| WKindIf (k : kind) (e1 e2 : list word)   (* kind pattern; value stays on the stack *)
| WTryAs (u : ty)                          (* validation, in place *)
| WCopy.                                   (* explicit deep copy; the result is fresh *)

Definition prog := list word.

(** ** Values and heap *)
Inductive val : Type :=
| VInt (n : nat) | VStr (s : string) | VBool (b : bool)
| VNone | VJust (v : val)
| VLoc (l : loc)                 (* a list or dict object *)
| VClo (sc : loc) (e : prog).    (* a quote closing over a variable scope *)

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
