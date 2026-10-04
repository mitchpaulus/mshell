(** * The frame lemma.

    A checker types a quote body once, from its own entry stack.  The core's
    quote rule asks for the body at every rest of the stack ([tw_quote]).
    [T_frame] bridges the two for quotes that return: nothing in the rules
    looks below a word's own arguments, so a derivation stays valid with any
    stack appended underneath (and the loop and return stacks extended to
    match).  [quote_once] is the form a checker uses.

    A [never] quote is different: its rule asks for every output stack at
    every frame, and appending a frame to an arbitrary output is not an
    arbitrary output.  See the design document on dead code after a
    diverging word. *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Typing RtLemmas Generic.


Lemma lframe_app L a b : lframe (lframe L a) b = lframe L (a ++ b).
Proof. destruct L; simpl; rewrite ?app_assoc; reflexivity. Qed.

Lemma rframe_app R a b : rframe (rframe R a) b = rframe R (a ++ b).
Proof. destruct R; simpl; rewrite ?app_assoc; reflexivity. Qed.

Lemma child_ctx_frame B s B' s0 : child_ctx B s B' -> child_ctx (lframe B s0) (s ++ s0) (lframe B' s0).
Proof. intros H; inversion H; subst; simpl; constructor. Qed.

Section Frame.
Variable sigs : genv.
Variable G : tenv.

Theorem T_frame :
  (forall B C R w s1 s2, TW sigs G B C R w s1 s2 -> forall s0,
     TW sigs G (lframe B s0) (lframe C s0) (rframe R s0) w (s1 ++ s0) (s2 ++ s0)) /\
  (forall B C R e s1 s2, T sigs G B C R e s1 s2 -> forall s0,
     T sigs G (lframe B s0) (lframe C s0) (rframe R s0) e (s1 ++ s0) (s2 ++ s0)).
Proof.
  apply (TW_comb sigs G
    (fun B C R w s1 s2 _ => forall s0,
       TW sigs G (lframe B s0) (lframe C s0) (rframe R s0) w (s1 ++ s0) (s2 ++ s0))
    (fun B C R e s1 s2 _ => forall s0,
       T sigs G (lframe B s0) (lframe C s0) (rframe R s0) e (s1 ++ s0) (s2 ++ s0)));
    intros; simpl;
    try solve [econstructor; eauto].
  all: rewrite <- ?app_assoc in *; try solve [econstructor; eauto].
  - eapply tw_each; [apply child_ctx_frame; eauto | apply child_ctx_frame; eauto |].
    inversion c; inversion c0; subst; simpl; exact t0.
  - eapply tw_map; [apply child_ctx_frame; eauto | apply child_ctx_frame; eauto |].
    inversion c; inversion c0; subst; simpl; exact t0.
  - eapply tw_map_imm; [eauto | apply child_ctx_frame; eauto | apply child_ctx_frame; eauto |].
    inversion c; inversion c0; subst; simpl; exact t0.
  - apply tw_case. intros c pts e0 Ec Ea. specialize (H c pts e0 Ec Ea s0).
    rewrite <- app_assoc in H. exact H.
  - eapply t_sub; [apply ssub_app; [eauto | apply ssub_refl] | eauto | apply ssub_app; [eauto | apply ssub_refl]].
  - (* dead code: the premise is already stated at every frame *)
    apply t_div. intros s1' s2'. rewrite !lframe_app, rframe_app, <- app_assoc. apply t.
Qed.

(** A quote body typed once, from its own entry stack, has the quote type. *)
Corollary quote_once B C R e ins outs s :
  T sigs G LNone LNone RNone e (shs ins) (shs outs) ->
  TW sigs G B C R (WQuote e) s ((Sh, TQuote ins (Some outs)) :: s).
Proof.
  intros H. apply tw_quote. intros s0. exact (proj2 T_frame _ _ _ _ _ _ H s0).
Qed.
(** ** Divergence, as a checker tracks it

    [diverges B C R e s]: [e] never returns normally from [s], at every
    frame.  This is the "diverges" flag on an inferred effect.  The lemmas
    below are the ways a checker sets and propagates it; together with
    [t_div] they justify "a diverging effect absorbs what follows". *)
Definition diverges (B C : lctx) (R : rctx) (e : prog) (s : sty) : Prop :=
  forall s0 s2, T sigs G (lframe B s0) (lframe C s0) (rframe R s0) e (s ++ s0) s2.

Lemma one_word B C R w s1 s2 : TW sigs G B C R w s1 s2 -> T sigs G B C R [w] s1 s2.
Proof. intros H. eapply t_cons; [exact H | apply t_nil]. Qed.

Lemma div_exit B C R s : diverges B C R [WExit] ((Sh, TInt) :: s).
Proof. intros s0 s2. apply one_word, tw_exit. Qed.

Lemma div_break C R s : diverges (LExact s) C R [WBreak] s.
Proof. intros s0 s2. apply one_word, tw_break_exact. Qed.

Lemma div_continue B R s : diverges B (LExact s) R [WContinue] s.
Proof. intros s0 s2. apply one_word, tw_cont_exact. Qed.

Lemma div_return B C s : diverges B C (RSome s) [WReturn] s.
Proof. intros s0 s2. apply one_word, tw_return. Qed.

Lemma div_return_any B C s : diverges B C RAny [WReturn] s.
Proof. intros s0 s2. apply one_word, tw_return_any. Qed.

Lemma div_call_never B C R f ins s : g_sigs sigs f ins None -> diverges B C R [WCall f] (ins ++ s).
Proof. intros Hf s0 s2. rewrite <- app_assoc. apply one_word, tw_call_never; auto. Qed.

Lemma div_exec_never B C R ins s : diverges B C R [WExec] ((Sh, TQuote ins None) :: shs ins ++ s).
Proof. intros s0 s2. simpl. rewrite <- app_assoc. apply one_word, tw_exec_never. Qed.

Lemma div_loop_forever B C R e s : T sigs G LNone (LExact s) R e s s -> diverges B C R [WLoop e] s.
Proof.
  intros H s0 s2. apply one_word, tw_loop_forever.
  exact (proj2 T_frame _ _ _ _ _ _ H s0).
Qed.

Lemma div_if B C R e1 e2 s : diverges B C R e1 s -> diverges B C R e2 s ->
  diverges B C R [WIf e1 e2] ((Sh, TBool) :: s).
Proof. intros H1 H2 s0 s2. apply one_word, tw_if; auto. Qed.

(** A diverging word absorbs what follows it. *)
Lemma div_head B C R w e s : diverges B C R [w] s -> diverges B C R (w :: e) s.
Proof.
  intros H s0 s2. apply t_div. intros s1 s3.
  rewrite lframe_app, lframe_app, rframe_app, <- app_assoc. apply H.
Qed.

(** A word that returns normally, then a sequence that diverges. *)
Lemma div_tail B C R w e s1 s2 : TW sigs G B C R w s1 s2 -> diverges B C R e s2 ->
  diverges B C R (w :: e) s1.
Proof.
  intros Hw He s0 s3. eapply t_cons; [exact (proj1 T_frame _ _ _ _ _ _ Hw s0) | apply He].
Qed.

(** A [never] quote body checked once. *)
Corollary quote_never_once B C R e ins s :
  diverges LNone LNone RNone e (shs ins) ->
  TW sigs G B C R (WQuote e) s ((Sh, TQuote ins None) :: s).
Proof. intros H. apply tw_quote_never. intros s0 s'. exact (H s0 s'). Qed.
End Frame.
