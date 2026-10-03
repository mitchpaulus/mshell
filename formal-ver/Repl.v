(** * A REPL line that stops with a checked error.

    The REPL checks each line from the stack the previous lines left and
    runs it only if it checks.  When a line that checked stops with a
    checked error ([RErr]), the REPL restores the stack it had before the
    line, except the *new* values the line took off it, which are dropped
    (plan question 20, option (a)).  [repl_error] says the restored stack
    still has the types the checker had for it, so checking can go on:

    - the slots below the line's inputs are its frame, and keep their types
      and their regions;
    - the shared slots among the line's inputs keep their types: a shared
      slot owns no region, so a second copy of it can sit in the frame,
      where [eval_sound] already protects it;
    - the store typing holds for the heap the error left ([err_ok]), and
      the stack above the frame is committed and dropped.

    Variables need nothing: a scope keeps its type ([scope_ext]), and one
    the line never stored reads as unset, which is a checked error. *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Typing Interp Invariant RtLemmas InvOps Soundness.

(** The shared slots of a stack, in order, and their types: what the REPL
    keeps of a failed line's inputs. *)
Fixpoint keep_sh (S : list val) (st : sty) : list val * sty :=
  match S, st with
  | v :: S', (Sh, t) :: st' => let '(a, b) := keep_sh S' st' in (v :: a, (Sh, t) :: b)
  | _ :: S', _ :: st' => keep_sh S' st'
  | _, _ => ([], [])
  end.

Section Repl.
Variable sigs : genv.

(** Inserting shared slots in the middle of the stack, where they own no
    region and point into none. *)
Lemma inv_insert_shs Σ H sc G A B a b OA OB K t :
  length A = length a -> length a = length OA ->
  inv sigs Σ H sc G (A ++ B) (a ++ b) (OA ++ OB) ->
  Forall (fun v => vtyped sigs Σ v t /\ forall l, In l (vlocs v) -> ~ In l (concat (OA ++ OB))) K ->
  inv sigs Σ H sc G (A ++ K ++ B) (a ++ map (fun _ => (Sh, t)) K ++ b) (OA ++ map (fun _ => []) K ++ OB).
Proof.
  intros La LO I FK. induction FK as [|v K [Hv Hl] FK IH]; [exact I|].
  destruct IH as [Ilen Islots Idisj Iown Iheap Ireg Iscope].
  assert (Ec : concat (OA ++ map (fun _ => []) (v :: K) ++ OB) = concat (OA ++ map (fun _ => []) K ++ OB)).
  { rewrite !concat_app. reflexivity. }
  assert (Ec0 : concat (OA ++ map (fun _ => []) K ++ OB) = concat (OA ++ OB)).
  { rewrite !concat_app. clear. induction K; simpl; auto. }
  constructor.
  - exact Ilen.
  - apply Forall3_split in Islots as [F1 F2]; [| lia | lia].
    apply Forall3_app; [exact F1|]. simpl. constructor; [split; auto | exact F2].
  - rewrite Ec. exact Idisj.
  - rewrite Ec. apply Forall3_split in Iown as [F1 F2]; [| lia | lia].
    apply Forall3_app; [exact F1|]. simpl. constructor; [| exact F2].
    intros l Hv' Hc. exfalso. apply (Hl l Hv'). rewrite <- Ec0. exact Hc.
  - rewrite Ec. exact Iheap.
  - rewrite Ec. exact Ireg.
  - exact Iscope.
Qed.

(** The shared slots among some slots are typed and point into no region. *)
Lemma keep_sh_typed Σ H X : forall S st O,
  Forall3 (slot_ok sigs Σ H) S st O ->
  Forall3 (fun v _ O => forall l, In l (vlocs v) -> In l X -> In l O) S st O ->
  Forall2 (fun v p => vtyped sigs Σ v (snd p) /\ forall l, In l (vlocs v) -> ~ In l X)
    (fst (keep_sh S st)) (snd (keep_sh S st)) /\
  Forall (fun p => fst p = Sh) (snd (keep_sh S st)).
Proof.
  intros S st O F. induction F as [|v p O0 S st Os Hs F IH]; intros Ow; simpl; [split; constructor|].
  inversion Ow as [|? ? ? ? ? ? Hown Ow']; subst.
  destruct (IH Ow') as [IH1 IH2].
  destruct p as [m t]. unfold slot_ok in Hs. simpl in Hs.
  destruct m; simpl; try (split; assumption).
  destruct (keep_sh S st) as [ka kb] eqn:Ek. simpl in *.
  destruct Hs as [Hv ->]. split; [constructor; [split; [exact Hv|] | exact IH1] | constructor; auto].
  intros l Hl Hc. exact (Hown l Hl Hc).
Qed.

(** The shared slots of a stack's top part are typed and point into no region. *)
Lemma keep_sh_ok Σ H sc G S Sf st sf Os :
  length S = length st ->
  inv sigs Σ H sc G (S ++ Sf) (st ++ sf) Os ->
  exists OS OF, Os = OS ++ OF /\ length OS = length S /\
    Forall2 (fun v p => vtyped sigs Σ v (snd p) /\ forall l, In l (vlocs v) -> ~ In l (concat Os))
      (fst (keep_sh S st)) (snd (keep_sh S st)) /\
    Forall (fun p => fst p = Sh) (snd (keep_sh S st)).
Proof.
  intros L I. pose proof (inv_slots _ _ _ _ _ _ _ _ I) as F.
  pose proof (inv_own _ _ _ _ _ _ _ _ I) as Ow.
  destruct (Forall3_app_inv _ _ _ _ _ _ F L) as (OS & OF & -> & F1 & _).
  pose proof (Forall3_length _ _ _ _ F1) as [_ L1].
  exists OS, OF. split; [reflexivity|]. split; [lia|].
  apply Forall3_split in Ow as [Ow1 _]; [| exact L | lia].
  exact (keep_sh_typed Σ H (concat (OS ++ OF)) S st OS F1 Ow1).
Qed.

Lemma keep_sh_length S st : length (fst (keep_sh S st)) = length (snd (keep_sh S st)).
Proof.
  revert st; induction S as [|v S IH]; intros [|[m t] st]; simpl; auto.
  destruct m; simpl; auto. destruct (keep_sh S st) eqn:E; simpl. specialize (IH st). rewrite E in IH. simpl in IH. lia.
Qed.

(** Inserting shared slots with their own types: one at a time. *)
Lemma inv_insert_kept Σ H sc G A B a b OA OB K k :
  length A = length a -> length a = length OA ->
  inv sigs Σ H sc G (A ++ B) (a ++ b) (OA ++ OB) ->
  Forall2 (fun v p => vtyped sigs Σ v (snd p) /\ forall l, In l (vlocs v) -> ~ In l (concat (OA ++ OB))) K k ->
  Forall (fun p => fst p = Sh) k ->
  inv sigs Σ H sc G (A ++ K ++ B) (a ++ k ++ b) (OA ++ map (fun _ => []) K ++ OB).
Proof.
  intros La LO I F2. revert I. induction F2 as [|v [m t] K k [Hv Hl] F2 IH]; intros I Fk; [exact I|].
  inversion Fk as [|? ? Em Fk']; subst. simpl in Em. subst m.
  specialize (IH I Fk').
  pose proof (inv_insert_shs Σ H sc G A (K ++ B) a (k ++ b) OA (map (fun _ => []) K ++ OB) [v] t La LO IH) as I'.
  simpl in I'. apply I'. constructor; [|constructor]. split; [exact Hv|].
  intros l Hl' Hc. apply (Hl l Hl'). rewrite !concat_app in *. clear -Hc. induction K; simpl in *; auto.
Qed.

Variable defs : string -> option prog.
Hypothesis Hdefs : def_ok sigs defs.
Hypothesis Hmaybe : maybe_ok sigs.
Variable vd : nat -> heap -> val -> ty -> option bool.
Hypothesis vd_fresh : forall Σ H f u v t O,
  dtyped sigs Σ H v t O -> vd f H v u = Some true -> dtyped sigs Σ H v u O.
Hypothesis vd_imm : forall Σ H f u v t,
  vtyped sigs Σ v t -> vd f H v u = Some true -> immutable u = true -> vtyped sigs Σ v u /\ vlocs v = [].

(** A line checked from the inputs [s1] above the frame [sf], that stops
    with a checked error, leaves a state where the frame and the line's
    shared inputs have the types they had before the line. *)
Theorem repl_error : forall G B C R e s1 s2, T sigs G B C R e s1 s2 ->
  forall Σ H sc S Sf sf Os n He,
  INV sigs Σ H sc G (S ++ Sf) (s1 ++ sf) Os -> length S = length s1 ->
  evalv vd defs n H sc S e = RErr He ->
  exists Σ' Os', scope_ext Σ Σ' /\
    INV sigs Σ' He sc G (fst (keep_sh S s1) ++ Sf) (snd (keep_sh S s1) ++ sf) Os'.
Proof.
  intros G B C R e s1 s2 HT Σ H sc S Sf sf Os n He [I Bd] L Ev.
  destruct (keep_sh_ok Σ H sc G S Sf s1 sf Os L I) as (OS & OF & -> & LOS & Fk & Fsh).
  set (K := fst (keep_sh S s1)) in *. set (k := snd (keep_sh S s1)) in *.
  assert (I1 : inv sigs Σ H sc G (S ++ K ++ Sf) (s1 ++ k ++ sf) (OS ++ map (fun _ => []) K ++ OF)).
  { apply inv_insert_kept; [exact L | lia | exact I | exact Fk | exact Fsh]. }
  pose proof (eval_sound sigs defs Hdefs Hmaybe vd vd_fresh vd_imm n G B C R e s1 s2 HT
                Σ H sc S (K ++ Sf) (k ++ sf) _ (conj I1 Bd) L) as Hr.
  rewrite Ev in Hr. simpl in Hr.
  destruct Hr as (Σ1 & S' & st' & Os1 & Sx1 & [I2 Bd2] & L2).
  destruct (inv_drop_prefix sigs _ _ _ _ S' _ st' _ _ L2 I2) as (Σ2 & Os2 & Sx2 & _ & I3).
  exists Σ2, Os2. split; [eapply scope_ext_trans; eauto | split; [exact I3 | exact Bd2]].
Qed.

(** The checker may first commit the line's inputs as it likes, as long as
    the line still checks from them: [s0] is [s1] with some new slots made
    shared, which is always allowed ([ss_forget]), and the line checks from
    [s0] when every slot it changed is immutable ([ss_imm]: an immutable
    value is new whether or not it is stored).  So the REPL keeps a new
    slot of an immutable type too. *)
Corollary repl_error_commit : forall G B C R e s1 s2, T sigs G B C R e s1 s2 ->
  forall s0, ssub s0 s1 -> ssub s1 s0 ->
  forall Σ H sc S Sf sf Os n He,
  INV sigs Σ H sc G (S ++ Sf) (s1 ++ sf) Os -> length S = length s1 ->
  evalv vd defs n H sc S e = RErr He ->
  exists Σ' Os', scope_ext Σ Σ' /\
    INV sigs Σ' He sc G (fst (keep_sh S s0) ++ Sf) (snd (keep_sh S s0) ++ sf) Os'.
Proof.
  intros G B C R e s1 s2 HT s0 S01 S10 Σ H sc S Sf sf Os n He [I Bd] L Ev.
  assert (L0 : length S = length s0) by (rewrite L; symmetry; eapply Forall2_length; exact S01).
  destruct (inv_ssub sigs Σ H sc G S Sf s1 s0 sf Os S10 L I) as (Σ1 & Os1 & Sx1 & _ & I1).
  assert (HT0 : T sigs G B C R e s0 s2) by (eapply t_sub; [exact S01 | exact HT | apply ssub_refl]).
  destruct (repl_error G B C R e s0 s2 HT0 Σ1 H sc S Sf sf Os1 n He (conj I1 Bd) L0 Ev) as (Σ2 & Os2 & Sx2 & I2).
  exists Σ2, Os2. split; [eapply scope_ext_trans; eauto | exact I2].
Qed.

End Repl.
