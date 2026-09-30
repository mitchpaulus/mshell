(** * The typing judgment of the core.

    [T G B C R e s1 s2] : in variable context [G], with break context [B],
    continue context [C] and return stack [R], program [e] turns a stack of
    type [s1] into one of type [s2].  Stacks are top-first lists of slots;
    a slot is a type with a freshness mark: [Sh] (may be shared) or [Dp]
    (deeply fresh: every list/dict reachable from the value is referenced
    exactly once, by this slot or by its parent in the tree).

    Break/continue contexts:
    - [LNone]      : not allowed here;
    - [LExact s]   : allowed when the stack is exactly [s] (a plain loop body);
    - [LChild]     : inside the body of a child-stack builtin ([each]) whose
                     enclosing loop's stack was already checked at the builtin;
                     the child stack is discarded, so any stack is fine. *)

From Stdlib Require Import String List Arith Bool.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping.

Inductive mark := Sh | Dp.
Definition slot := (mark * ty)%type.
Definition sty := list slot.
Definition shs (ts : list ty) : sty := map (fun t => (Sh, t)) ts.
Definition marks (m : mark) (ts : list ty) : sty := map (fun t => (m, t)) ts.
Definition tenv := list (var * ty).

Inductive lctx := LNone | LExact (s : sty) | LChild.

(** Return contexts: [RNone], [return] not allowed (a quote body);
    [RSome s], inside a definition returning [s]; [RAny], top-level code,
    where [return] ends the script and nothing reads the stack. *)
Inductive rctx := RNone | RSome (s : sty) | RAny.

(** Extending a derivation with a frame [s0] under the stack also extends
    the loop and return stacks (Frame.v). *)
Definition lframe (L : lctx) (s0 : sty) : lctx :=
  match L with LExact s => LExact (s ++ s0) | l => l end.
Definition rframe (R : rctx) (s0 : sty) : rctx :=
  match R with RSome s => RSome (s ++ s0) | r => r end.

(** The loop context seen by the body of a child-stack builtin that runs
    with [s] below its arguments. *)
Inductive child_ctx : lctx -> sty -> lctx -> Prop :=
| cc_none L s : child_ctx L s LNone
| cc_exact s : child_ctx (LExact s) s LChild
| cc_child s : child_ctx LChild s LChild.

(** Stack subsumption.  [ss_dp] is the doc's Retype (any covariant change
    of a fresh value); [ss_forget] is Forget followed by As. *)
Inductive slot_sub : slot -> slot -> Prop :=
| ss_sh a b : sub a b -> slot_sub (Sh, a) (Sh, b)
| ss_dp a b : rsub a b -> slot_sub (Dp, a) (Dp, b)
| ss_forget a b : sub a b -> slot_sub (Dp, a) (Sh, b)
| ss_imm a b : immutable a = true -> sub a b -> slot_sub (Sh, a) (Dp, b).

Definition ssub : sty -> sty -> Prop := Forall2 slot_sub.

Definition writable (f : fstat) (t : ty) : Prop :=
  f = FReq t \/ f = FOpt t \/ f = FDict t.

(** ** Kind patterns *)
Definition tunion (a b : ty) : ty :=
  match a, b with
  | TBot, _ => b
  | _, TBot => a
  | _, _ => TUnion a b
  end.

Definition kind_top (k : kind) : option ty :=
  match k with
  | KInt => Some TInt | KStr => Some TStr | KBool => Some TBool
  | KMaybe => Some (TMaybe TTop)
  | KDict => Some (TRec [] FOpen)
  | KQuote => Some TTop
  | KList => None           (* needs the abstract rule [tw_kind_list] *)
  | KEnum _ => None         (* needs the abstract rule [tw_kind_enum] *)
  end.

Definition kind_of_ty (t : ty) : option kind :=
  match t with
  | TInt => Some KInt | TStr => Some KStr | TBool => Some KBool
  | TMaybe _ => Some KMaybe | TList _ => Some KList
  | TRec _ _ => Some KDict | TQuote _ _ => Some KQuote
  | TEnum E _ => Some (KEnum E)
  | TBot | TTop | TUnion _ _ | TParam _ | TVar _ | TMu _ | TRV _ => None
  end.

(** The members of [t] of kind [k] (the then-branch type) *)
Fixpoint kind_then (k : kind) (t : ty) : option ty :=
  match t with
  | TBot => Some TBot
  | TTop => kind_top k
  | TVar _ => kind_top k     (* an instance may have any kind: like unknown contents *)
  | TMu _ => kind_top k      (* unfold first ([t_sub]) to see the member of kind [k] *)
  | TUnion a b =>
      match kind_then k a, kind_then k b with
      | Some x, Some y => Some (tunion x y)
      | _, _ => None
      end
  | _ => match kind_of_ty t with
         | Some k' => if kind_eqb k k' then Some t else Some TBot
         | None => Some TBot
         end
  end.

(** The members of [t] not of kind [k] (the else-branch type) *)
Fixpoint kind_else (k : kind) (t : ty) : ty :=
  match t with
  | TBot => TBot
  | TTop => TTop
  | TUnion a b => tunion (kind_else k a) (kind_else k b)
  | _ => match kind_of_ty t with
         | Some k' => if kind_eqb k k' then TBot else t
         | None => t
         end
  end.

(** The typing environment.

    [g_sigs f ins outs] lists the instances of definition [f]'s signature;
    a polymorphic signature is the set of its instances.  Inputs and outputs
    are stack slots, so a signature can say an input or output is fresh
    ([Dp]): the caller must pass a fresh value, or gets one back.
    [outs = None] is a [never] signature.

    [g_ctors E c] is the list of payload types of constructor [c] of enum
    [E] (top-first, mentioning [E]'s parameters as [TParam]s): the enum
    declarations. *)
Record genv := {
  g_sigs : string -> sty -> option sty -> Prop;
  g_ctors : ename -> cname -> option (list ty)
}.

Section Typing.
Variable sigs : genv.
Variable G : tenv.

Inductive TW : lctx -> lctx -> rctx -> word -> sty -> sty -> Prop :=
| tw_int B C R n s : TW B C R (WInt n) s ((Sh, TInt) :: s)
| tw_str B C R x s : TW B C R (WStr x) s ((Sh, TStr) :: s)
| tw_bool B C R b s : TW B C R (WBool b) s ((Sh, TBool) :: s)
| tw_add B C R s : TW B C R WAdd ((Sh, TInt) :: (Sh, TInt) :: s) ((Sh, TInt) :: s)
| tw_cat B C R s : TW B C R WCat ((Sh, TStr) :: (Sh, TStr) :: s) ((Sh, TStr) :: s)
| tw_dup B C R t s : TW B C R WDup ((Sh, t) :: s) ((Sh, t) :: (Sh, t) :: s)
| tw_drop B C R p s : TW B C R WDrop (p :: s) s
| tw_swap B C R p q s : TW B C R WSwap (p :: q :: s) (q :: p :: s)
| tw_none B C R s : TW B C R WNone s ((Sh, TMaybe TBot) :: s)
| tw_just B C R m t s : TW B C R WJust ((m, t) :: s) ((m, TMaybe t) :: s)
| tw_unwrap B C R m t s : TW B C R WUnwrap ((m, TMaybe t) :: s) ((m, t) :: s)
| tw_load B C R x t s : lookup x G = Some t -> TW B C R (WLoad x) s ((Sh, t) :: s)
| tw_store B C R x t s : lookup x G = Some t -> TW B C R (WStore x) ((Sh, t) :: s) s
| tw_quote B C R e ins outs s :
    (forall s0, T LNone LNone RNone e (shs ins ++ s0) (shs outs ++ s0)) ->
    TW B C R (WQuote e) s ((Sh, TQuote ins (Some outs)) :: s)
| tw_quote_never B C R e ins s :
    (forall s0 s', T LNone LNone RNone e (shs ins ++ s0) s') ->
    TW B C R (WQuote e) s ((Sh, TQuote ins None) :: s)
| tw_exec B C R ins outs s :
    TW B C R WExec ((Sh, TQuote ins (Some outs)) :: shs ins ++ s) (shs outs ++ s)
| tw_exec_never B C R ins s s' :
    TW B C R WExec ((Sh, TQuote ins None) :: shs ins ++ s) s'
| tw_if B C R e1 e2 s s' :
    T B C R e1 s s' -> T B C R e2 s s' -> TW B C R (WIf e1 e2) ((Sh, TBool) :: s) s'
| tw_loop B C R e s :
    T (LExact s) (LExact s) R e s s -> TW B C R (WLoop e) s s
| tw_loop_forever B C R e s s' :
    T LNone (LExact s) R e s s -> TW B C R (WLoop e) s s'
| tw_break_exact C R s s' : TW (LExact s) C R WBreak s s'
| tw_break_child C R s s' : TW LChild C R WBreak s s'
| tw_cont_exact B R s s' : TW B (LExact s) R WContinue s s'
| tw_cont_child B R s s' : TW B LChild R WContinue s s'
| tw_return B C s s' : TW B C (RSome s) WReturn s s'
| tw_return_any B C s s' : TW B C RAny WReturn s s'
| tw_exit B C R s s' : TW B C R WExit ((Sh, TInt) :: s) s'
| tw_call B C R f ins outs s :
    g_sigs sigs f ins (Some outs) -> TW B C R (WCall f) (ins ++ s) (outs ++ s)
| tw_call_never B C R f ins s s' :
    g_sigs sigs f ins None -> TW B C R (WCall f) (ins ++ s) s'
| tw_nil B C R t s : TW B C R WNil s ((Dp, TList t) :: s)
| tw_push_sh B C R t s :
    TW B C R WPush ((Sh, t) :: (Sh, TList t) :: s) ((Sh, TList t) :: s)
| tw_push_dp B C R t s :
    TW B C R WPush ((Dp, t) :: (Dp, TList t) :: s) ((Dp, TList t) :: s)
| tw_getat B C R t s :
    TW B C R WGetAt ((Sh, TInt) :: (Sh, TList t) :: s) ((Sh, t) :: s)
| tw_setat_sh B C R t s :
    TW B C R WSetAt ((Sh, t) :: (Sh, TInt) :: (Sh, TList t) :: s) ((Sh, TList t) :: s)
| tw_each B C R e t s B' C' :
    child_ctx B s B' -> child_ctx C s C' ->
    T B' C' RNone e [(Sh, t)] [] ->
    TW B C R (WEach e) ((Sh, TList t) :: s) s
(** [map] with a literal body.  Its result holds the body's results, which
    are shared values, so it is fresh only when they are immutable; "fresh
    when the input is fresh" would be wrong here. *)
| tw_map B C R e t u s B' C' :
    child_ctx B s B' -> child_ctx C s C' ->
    T B' C' RNone e [(Sh, t)] [(Sh, u)] ->
    TW B C R (WMap e) ((Sh, TList t) :: s) ((Sh, TList u) :: s)
| tw_map_imm B C R e t u s B' C' :
    immutable u = true ->
    child_ctx B s B' -> child_ctx C s C' ->
    T B' C' RNone e [(Sh, t)] [(Sh, u)] ->
    TW B C R (WMap e) ((Sh, TList t) :: s) ((Dp, TList u) :: s)
| tw_dictnew B C R s : TW B C R WDictNew s ((Dp, TRec [] FAbs) :: s)
| tw_getk B C R k fs r s :
    TW B C R (WGetK k) ((Sh, TRec fs r) :: s) ((Sh, TMaybe (fty (field_at k fs r))) :: s)
| tw_getreq B C R k fs r t s :
    field_at k fs r = FReq t ->
    TW B C R (WGetReq k) ((Sh, TRec fs r) :: s) ((Sh, t) :: s)
| tw_setk_sh B C R k fs r t s :
    writable (field_at k fs r) t ->
    TW B C R (WSetK k) ((Sh, t) :: (Sh, TRec fs r) :: s) ((Sh, TRec fs r) :: s)
| tw_setk_dp B C R k fs r t s :
    TW B C R (WSetK k) ((Dp, t) :: (Dp, TRec fs r) :: s) ((Dp, TRec ((k, FReq t) :: fs) r) :: s)
| tw_del_sh B C R k fs r t s :
    field_at k fs r = FDict t ->
    TW B C R (WDel k) ((Sh, TRec fs r) :: s) ((Sh, TRec fs r) :: s)
| tw_del_dp B C R k fs r s :
    TW B C R (WDel k) ((Dp, TRec fs r) :: s) ((Dp, TRec ((k, FAbs) :: fs) r) :: s)
| tw_getd B C R fs r t s :
    (forall k, sub (fty (field_at k fs r)) t) ->
    TW B C R WGetD ((Sh, TStr) :: (Sh, TRec fs r) :: s) ((Sh, TMaybe t) :: s)
| tw_setd B C R fs r t s :
    (forall k, writable (field_at k fs r) t) ->
    TW B C R WSetD ((Sh, t) :: (Sh, TStr) :: (Sh, TRec fs r) :: s) ((Sh, TRec fs r) :: s)
| tw_kind B C R k m t t1 e1 e2 s s' :
    kind_then k t = Some t1 ->
    T B C R e1 ((m, t1) :: s) s' ->
    T B C R e2 ((m, kind_else k t) :: s) s' ->
    TW B C R (WKindIf k e1 e2) ((m, t) :: s) s'
| tw_kind_list B C R m t e1 e2 s s' :
    (forall a, T B C R e1 ((m, TList a) :: s) s') ->
    T B C R e2 ((m, t) :: s) s' ->
    TW B C R (WKindIf KList e1 e2) ((m, t) :: s) s'
(** A [tryAs] target mentions no type variable: types are erased, so the
    runtime cannot validate against one (Generic.v). *)
| tw_try_dp B C R t u s :
    fvt u = [] -> TW B C R (WTryAs u) ((Dp, t) :: s) ((Dp, TMaybe u) :: s)
| tw_try_sub B C R t u s :
    fvt u = [] -> sub t u -> TW B C R (WTryAs u) ((Sh, t) :: s) ((Sh, TMaybe u) :: s)
| tw_try_imm B C R t u s :
    fvt u = [] -> immutable u = true -> TW B C R (WTryAs u) ((Sh, t) :: s) ((Sh, TMaybe u) :: s)
| tw_copy B C R t s :
    TW B C R WCopy ((Sh, t) :: s) ((Dp, t) :: s)
(** Enums.  A constructor is polymorphic in the enum's parameters ([a] is
    any list of arguments).  Its value is fresh when every payload is
    fresh (an immutable payload can be made fresh first, [ss_imm]). *)
| tw_con_sh B C R E c pts a s :
    g_ctors sigs E c = Some pts -> wf_payload E pts ->
    TW B C R (WCon E c pts) (shs (map (subst a) pts) ++ s) ((Sh, TEnum E a) :: s)
| tw_con_dp B C R E c pts a s :
    g_ctors sigs E c = Some pts -> wf_payload E pts ->
    TW B C R (WCon E c pts) (marks Dp (map (subst a) pts) ++ s) ((Dp, TEnum E a) :: s)
(** A match pushes the payloads, with the enum value's freshness, and runs
    the arm for its constructor.  A constructor with no arm is a checked
    error (a surface match without full coverage elaborates to that). *)
| tw_case B C R m E a arms s s' :
    (forall c pts e, g_ctors sigs E c = Some pts -> lookup c arms = Some e ->
       T B C R e (marks m (map (subst a) pts) ++ s) s') ->
    TW B C R (WCase E arms) ((m, TEnum E a) :: s) s'
(** A kind pattern for an enum on a value whose type does not say which
    instance it is: the arm is checked for every argument list, like
    [tw_kind_list]. *)
| tw_kind_enum B C R m E t e1 e2 s s' :
    (forall a, T B C R e1 ((m, TEnum E a) :: s) s') ->
    T B C R e2 ((m, t) :: s) s' ->
    TW B C R (WKindIf (KEnum E) e1 e2) ((m, t) :: s) s'

with T : lctx -> lctx -> rctx -> prog -> sty -> sty -> Prop :=
| t_nil B C R s : T B C R [] s s
| t_cons B C R w e s1 s2 s3 : TW B C R w s1 s2 -> T B C R e s2 s3 -> T B C R (w :: e) s1 s3
| t_sub B C R e s1 s1' s2 s2' :
    ssub s1' s1 -> T B C R e s1 s2 -> ssub s2 s2' -> T B C R e s1' s2'
(** Code after a word that never returns normally is not checked: it cannot
    run.  The word must diverge at every frame (with the loop and return
    stacks extended to match), which is what makes this rule survive the
    frame lemma. *)
| t_div B C R w e s1 s3 :
    (forall s0 s2, T (lframe B s0) (lframe C s0) (rframe R s0) [w] (s1 ++ s0) s2) ->
    T B C R (w :: e) s1 s3.

End Typing.

Scheme TW_mut := Induction for TW Sort Prop
with T_mut := Induction for T Sort Prop.

(** A closure body [e] in scope [G] has quote type [TQuote ins outs]. *)
Definition closure_ok sigs (G : tenv) (e : prog) (ins : list ty) (outs : option (list ty)) : Prop :=
  match outs with
  | Some o => forall s0, T sigs G LNone LNone RNone e (shs ins ++ s0) (shs o ++ s0)
  | None => forall s0 s', T sigs G LNone LNone RNone e (shs ins ++ s0) s'
  end.

(** A definition body is well typed for every instance of its signature. A
    [never] signature types the body with no return context. *)
Definition def_ok sigs (defs : string -> option prog) : Prop :=
  forall f ins outs, g_sigs sigs f ins outs ->
  exists body G, defs f = Some body /\
    match outs with
    | Some o => forall s0, T sigs G LNone LNone (RSome (o ++ s0)) body (ins ++ s0) (o ++ s0)
    | None => forall s0 s', T sigs G LNone LNone RNone body (ins ++ s0) s'
    end.
