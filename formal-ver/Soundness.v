(** * Type soundness.

    [eval_sound]: a well-typed program, run from a state satisfying the
    invariant, never evaluates to [RStuck] (a runtime type error), for any
    amount of fuel.  Break/continue/return outcomes carry stacks of the types
    their contexts promise, and the invariant is preserved. *)

From Stdlib Require Import String List Arith Bool Lia Permutation.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Variance Typing Interp Invariant RtLemmas Commit Partial Validate Kind InvOps Copy RecOps PartialOps Slice.

Section Sound.
Variable sigs : genv.
Variable defs : string -> option prog.
Hypothesis Hdefs : def_ok sigs defs.
Hypothesis Hmaybe : maybe_ok sigs.

(** The validator [tryAs] runs.  Soundness needs only this of it: an answer
    of [true] is right for a fresh value (a tree) and for a target with no
    lists or dicts.  Every other answer, including an error or [false], and
    any answer for a shared value validated against a type that has
    containers, is unconstrained. *)
Variable vd : nat -> heap -> val -> ty -> option bool.
Hypothesis vd_fresh : forall Σ H f u v t O,
  dtyped sigs Σ H v t O -> vd f H v u = Some true -> dtyped sigs Σ H v u O.
Hypothesis vd_imm : forall Σ H f u v t,
  vtyped sigs Σ v t -> vd f H v u = Some true -> immutable u = true -> vtyped sigs Σ v u /\ vlocs v = [].

Definition INV Σ H sc G L st Os := inv sigs Σ H sc G L st Os /\ bounded H.

Definition out_ok (o : outcome) (B C : lctx) (R : rctx) (s2 st' : sty) : Prop :=
  match o with
  | ONormal => st' = s2
  | OBreak => match B with LExact s => st' = s | LChild => True | LNone => False end
  | OContinue => match C with LExact s => st' = s | LChild => True | LNone => False end
  | OReturn => match R with RSome s => st' = s | RAny => True | RNone => False end
  end.

(** A checked error keeps the store typing and the caller's frame: the
    invariant holds with whatever stack the error left above the frame. *)
Definition err_ok (Σ : store_ty) (sc : loc) (G : tenv) (Sf : list val) (sf : sty) (He : heap) : Prop :=
  exists Σ' S' st' Os', scope_ext Σ Σ' /\ INV Σ' He sc G (S' ++ Sf) (st' ++ sf) Os' /\
    length S' = length st'.

Definition res_ok (Σ : store_ty) (sc : loc) (G : tenv) (B C : lctx) (R : rctx)
    (s2 : sty) (Sf : list val) (sf : sty) (r : result) : Prop :=
  match r with
  | RStuck => False
  | RTimeout | RExit _ => True
  | RErr H' => err_ok Σ sc G Sf sf H'
  | ROk o H' S' =>
      exists Σ' st' Os', scope_ext Σ Σ' /\ INV Σ' H' sc G (S' ++ Sf) (st' ++ sf) Os' /\
        length S' = length st' /\ out_ok o B C R s2 st'
  end.

Definition P (n : nat) : Prop :=
  forall G B C R e s1 s2, T sigs G B C R e s1 s2 ->
  forall Σ H sc stk Sf sf Os, INV Σ H sc G (stk ++ Sf) (s1 ++ sf) Os -> length stk = length s1 ->
  res_ok Σ sc G B C R s2 Sf sf (evalv vd defs n H sc stk e).

(** A checked error where the invariant holds. *)
Lemma err_here Σ H sc G B C R s2 Sf sf stk s Os :
  INV Σ H sc G (stk ++ Sf) (s ++ sf) Os -> length stk = length s ->
  res_ok Σ sc G B C R s2 Sf sf (RErr H).
Proof. intros I L. exists Σ, stk, s, Os. split; [apply scope_ext_refl | auto]. Qed.

Lemma err_weaken Σ Σ1 sc G Sf sf He : scope_ext Σ Σ1 -> err_ok Σ1 sc G Sf sf He -> err_ok Σ sc G Sf sf He.
Proof.
  intros Sx (Σ' & S' & st' & Os' & Sx' & I & L). exists Σ', S', st', Os'.
  split; [eapply scope_ext_trans; eauto | auto].
Qed.

Lemma res_ok_weaken Σ Σ' sc G B C R s2 Sf sf r :
  scope_ext Σ Σ' -> res_ok Σ' sc G B C R s2 Sf sf r -> res_ok Σ sc G B C R s2 Sf sf r.
Proof.
  intros Sx Hr. destruct r; simpl in *; auto.
  { eapply err_weaken; eauto. }
  destruct Hr as (Σ2 & st' & Os' & Sx2 & I & L & O).
  exists Σ2, st', Os'. split; [eapply scope_ext_trans; eauto | split; [exact I | split; [exact L | exact O]]].
Qed.

Lemma next_ok n (IH : P n) G B C R rest s2 s3 Σ Σ' H' sc stk' Sf sf Os' :
  T sigs G B C R rest s2 s3 -> INV Σ' H' sc G (stk' ++ Sf) (s2 ++ sf) Os' ->
  length stk' = length s2 -> scope_ext Σ Σ' ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs n H' sc stk' rest).
Proof. intros HT I L Sx. eapply res_ok_weaken; eauto. Qed.

(** Continue with [k] after a sub-evaluation in the same contexts. *)
Lemma cont_same Σ sc G B C R s2 s3 Sf sf r (k : heap -> list val -> result) :
  res_ok Σ sc G B C R s2 Sf sf r ->
  (forall Σ' H' S' Os', scope_ext Σ Σ' -> INV Σ' H' sc G (S' ++ Sf) (s2 ++ sf) Os' ->
     length S' = length s2 -> res_ok Σ sc G B C R s3 Sf sf (k H' S')) ->
  res_ok Σ sc G B C R s3 Sf sf (match r with ROk ONormal H' S' => k H' S' | r => r end).
Proof.
  destruct r as [| |He| |o H' S']; simpl; auto. intros (Σ' & st' & Os' & Sx & I & L & O) K.
  destruct o; simpl in O.
  - subst. eapply K; eauto.
  - exists Σ', st', Os'. split; [exact Sx | split; [exact I | split; [exact L | exact O]]].
  - exists Σ', st', Os'. split; [exact Sx | split; [exact I | split; [exact L | exact O]]].
  - exists Σ', st', Os'. split; [exact Sx | split; [exact I | split; [exact L | exact O]]].
Qed.

Lemma INV_scope Σ H sc sc' G G' L st Os :
  INV Σ H sc G L st Os -> nth_error Σ sc' = Some (HScope G') -> INV Σ H sc' G' L st Os.
Proof. intros [I B] E. split; auto. eapply inv_scope_change; eauto. Qed.

(** Values held above the frame by the caller become part of the error's stack. *)
Lemma err_frame Σ sc G X x Sf sf He :
  length X = length x -> err_ok Σ sc G (X ++ Sf) (x ++ sf) He -> err_ok Σ sc G Sf sf He.
Proof.
  intros Lx (Σ' & S' & st' & Os' & Sx & I & L). exists Σ', (S' ++ X), (st' ++ x), Os'.
  rewrite <- !app_assoc. split; [exact Sx | split; [exact I | rewrite !length_app; lia]].
Qed.

(** An error from a sub-evaluation (another scope or other contexts) over the
    same frame is an error here. *)
Lemma err_lift Σ sc sc' G G' Sf sf He :
  nth_error Σ sc = Some (HScope G) -> err_ok Σ sc' G' Sf sf He -> err_ok Σ sc G Sf sf He.
Proof.
  intros Esc (Σ' & S' & st' & Os' & Sx & I & L). exists Σ', S', st', Os'.
  split; [exact Sx | split; [eapply INV_scope; [exact I | apply Sx; exact Esc] | exact L]].
Qed.

(** A sub-evaluation in another scope with no loop or return context
    (a quote body): only a normal outcome is possible. *)
Lemma cont_quote Σ sc sc' G G' B C R s2 s3 Sf sf r (k : heap -> list val -> result) :
  nth_error Σ sc = Some (HScope G) ->
  res_ok Σ sc' G' LNone LNone RNone s2 Sf sf r ->
  (forall Σ' H' S' Os', scope_ext Σ Σ' -> INV Σ' H' sc G (S' ++ Sf) (s2 ++ sf) Os' ->
     length S' = length s2 -> res_ok Σ sc G B C R s3 Sf sf (k H' S')) ->
  res_ok Σ sc G B C R s3 Sf sf (match r with ROk ONormal H' S' => k H' S' | r => r end).
Proof.
  intros Esc. destruct r as [| |He| |o H' S']; simpl; auto.
  { intros E _. eapply err_lift; eauto. }
  intros (Σ' & st' & Os' & Sx & I & L & O) K.
  destruct o; simpl in O; try contradiction.
  subst. eapply K; eauto. eapply INV_scope; eauto.
Qed.

Lemma slot_bot_false Σ H sc G v L st Os :
  INV Σ H sc G (v :: L) ((Sh, TBot) :: st) Os -> False.
Proof.
  intros [I _]. pose proof (inv_slots _ _ _ _ _ _ _ _ I) as F.
  inversion F as [|? ? ? ? ? ? Hs _]; subst. destruct Hs as [Hv _]. eapply vt_bot; eauto.
Qed.

(** A never-returning body: no normal outcome. *)
Lemma never_normal Σ sc G B C R s3 Sf sf r (k : heap -> list val -> result) :
  res_ok Σ sc G B C R [(Sh, TBot)] Sf sf r ->
  (forall o H' S', r = ROk o H' S' -> o <> ONormal -> res_ok Σ sc G B C R s3 Sf sf r) ->
  res_ok Σ sc G B C R s3 Sf sf (match r with ROk ONormal H' S' => k H' S' | r => r end).
Proof.
  destruct r as [| |He| |o H' S']; simpl; auto. intros Hr K.
  destruct o.
  - exfalso. destruct Hr as (Σ' & st' & Os' & Sx & I & L & O). simpl in O. subst.
    destruct S' as [|v S']; simpl in L; [lia|]. eapply slot_bot_false; eauto.
  - apply (K OBreak H' S'); auto; discriminate.
  - apply (K OContinue H' S'); auto; discriminate.
  - apply (K OReturn H' S'); auto; discriminate.
Qed.

Lemma inv_cons_Os Σ H sc G v L p st Os :
  inv sigs Σ H sc G (v :: L) (p :: st) Os -> exists O Os', Os = O :: Os'.
Proof.
  intros I. pose proof (inv_slots _ _ _ _ _ _ _ _ I) as F. inversion F; subst; eauto.
Qed.

(** The object behind a location outside the regions. *)
Lemma inv_obj Σ H sc G L st Os l h :
  inv sigs Σ H sc G L st Os -> nth_error Σ l = Some h -> h <> HDead -> ~ In l (concat Os) ->
  exists o, nth_error H l = Some o /\ obj_ok sigs Σ o h /\ (forall r, In r (olocs o) -> ~ In r (concat Os)).
Proof.
  intros I E Hd Hn. pose proof (inv_len _ _ _ _ _ _ _ _ I) as Ln.
  assert (Hlt : l < length H) by (rewrite <- Ln; eapply nth_error_lt; eauto).
  destruct (nth_error H l) as [o|] eqn:Eo; [|apply nth_error_None in Eo; lia].
  destruct (inv_heap _ _ _ _ _ _ _ _ I l o Eo Hn) as [(h' & E' & Ok) Hr]; [unfold live; rewrite E; congruence|].
  rewrite E in E'. inversion E'; subst. eauto.
Qed.

Lemma inv_push_dp0 Σ H sc G v L t st Os :
  inv sigs Σ H sc G L st Os -> dtyped sigs Σ H v t [] ->
  inv sigs Σ H sc G (v :: L) ((Dp, t) :: st) ([] :: Os).
Proof.
  intros [Ilen Islots Idisj Iown Iheap Ireg Iscope] D. constructor; simpl; auto.
  - constructor; auto.
  - constructor; auto. destruct (proj1 (dtyped_struct sigs Σ H) _ _ _ D) as (_ & Hv & _).
    intros l Hl _. auto.
Qed.

Lemma inv_scope_obj Σ H sc G L st Os :
  inv sigs Σ H sc G L st Os ->
  exists kvs, nth_error H sc = Some (OScope kvs) /\ obj_ok sigs Σ (OScope kvs) (HScope G) /\
    (forall r, In r (olocs (OScope kvs)) -> ~ In r (concat Os)) /\ ~ In sc (concat Os).
Proof.
  intros Iv. pose proof (inv_scope _ _ _ _ _ _ _ _ Iv) as Esc.
  assert (Nsc : ~ In sc (concat Os)).
  { intro Hc. pose proof (inv_reg _ _ _ _ _ _ _ _ Iv sc Hc) as E.
    rewrite Esc in E. discriminate. }
  destruct (inv_obj _ _ _ _ _ _ _ _ _ Iv Esc ltac:(discriminate) Nsc) as (o & Eo & Ok & Hr).
  destruct o as [vs|kvs|kvs]; simpl in Ok; try contradiction. eauto.
Qed.

Ltac len := simpl in *; unfold sty, slot in *; lia.


Lemma fsub_writable f' f t :
  fsub f' f -> writable f t -> exists t', writable f' t' /\ teq t t'.
Proof.
  intros Hs [->|[->| ->]]; inversion Hs; subst.
  - exists a. split; [left; auto | split; auto].
  - exists a. split; [left; auto | split; auto].
  - exists a. split; [right; left; auto | split; auto].
  - exists a. split; [right; right; auto | split; auto].
  - exists a. split; [right; right; auto | split; auto].
Qed.

Lemma writable_fty f t : writable f t -> fty f = t.
Proof. intros [->|[->| ->]]; auto. Qed.

Lemma lookup_some_in {A} k (kvs : list (string * A)) v : lookup k kvs = Some v -> In v (map snd kvs).
Proof. intros E. apply lookup_in in E. apply in_map_iff. exists (k, v); auto. Qed.

Lemma olocs_dict_dset k x kvs r :
  In r (olocs (ODict (dset k x kvs))) -> In r (vlocs x) \/ In r (olocs (ODict kvs)).
Proof.
  simpl. intros [Hr|Hr]%in_app_or; auto. right.
  apply in_flat_map in Hr as ([k0 w] & Hk & Hr). apply in_flat_map. exists (k0, w). split; auto.
  unfold remove_key in Hk. apply filter_In in Hk as [Hk _]. auto.
Qed.

Lemma olocs_dict_remove k kvs r :
  In r (olocs (ODict (remove_key k kvs))) -> In r (olocs (ODict kvs)).
Proof.
  simpl. intros Hr. apply in_flat_map in Hr as ([k0 w] & Hk & Hr). apply in_flat_map. exists (k0, w).
  split; auto. unfold remove_key in Hk. apply filter_In in Hk as [Hk _]. auto.
Qed.

Lemma Forall_remove_key {A} (P' : string * A -> Prop) k kvs : Forall P' kvs -> Forall P' (remove_key k kvs).
Proof.
  intros F. unfold remove_key. rewrite Forall_forall in *. intros p Hp. apply filter_In in Hp as [Hp _]. auto.
Qed.

Lemma obj_ok_dset Σ kvs fs r k x t :
  obj_ok sigs Σ (ODict kvs) (HRec fs r) -> writable (field_at k fs r) t -> vtyped sigs Σ x t ->
  obj_ok sigs Σ (ODict (dset k x kvs)) (HRec fs r).
Proof.
  simpl. intros (N & Rq & F) Hw Hx. repeat split.
  - simpl. constructor.
    + intro Hc. apply in_keys_remove in Hc as [_ Hc]. contradiction.
    + apply nodup_keys_remove; auto.
  - intros k0 t0 E. unfold dset. simpl. destruct (String.eqb_spec k0 k); [discriminate|].
    rewrite lookup_remove_neq; auto. eapply Rq; eauto.
  - constructor.
    + simpl. rewrite (writable_fty _ _ Hw). exact Hx.
    + apply Forall_remove_key; auto.
Qed.

Lemma obj_ok_remove Σ kvs fs r k t :
  obj_ok sigs Σ (ODict kvs) (HRec fs r) -> field_at k fs r = FDict t ->
  obj_ok sigs Σ (ODict (remove_key k kvs)) (HRec fs r).
Proof.
  simpl. intros (N & Rq & F) Hk. repeat split.
  - apply nodup_keys_remove; auto.
  - intros k0 t0 E. destruct (String.eqb_spec k0 k) as [->|Hn]; [rewrite Hk in E; discriminate|].
    rewrite lookup_remove_neq; auto. eapply Rq; eauto.
  - apply Forall_remove_key; auto.
Qed.

Lemma obj_dict_lookup Σ kvs fs r k x :
  obj_ok sigs Σ (ODict kvs) (HRec fs r) -> lookup k kvs = Some x ->
  vtyped sigs Σ x (fty (field_at k fs r)).
Proof.
  simpl. intros (N & Rq & F) E. rewrite Forall_forall in F.
  pose proof (lookup_in _ _ _ E) as Hin. apply F in Hin. simpl in Hin.
  (* the first occurrence is the one looked up *)
  exact Hin.
Qed.

Lemma olocs_lookup kvs k x r : lookup k kvs = Some x -> In r (vlocs x) -> In r (olocs (ODict kvs)).
Proof.
  intros E Hr. simpl. apply in_flat_map. exists (k, x). split; auto. apply lookup_in; auto.
Qed.

Section Words.
Variable n : nat.
Hypothesis IH : P n.

Ltac pop_sh Iv :=
  match type of Iv with
  | inv _ _ _ _ _ (_ :: _) (_ :: _) _ =>
      let O := fresh "O" in let Os := fresh "Os" in let E := fresh "E" in
      destruct (inv_cons_Os _ _ _ _ _ _ _ _ _ Iv) as (O & Os & E); subst;
      apply inv_pop_sh in Iv
  end.

Lemma w_push_sh G B C R v t rest s s3 Σ H sc stk Sf sf Os (k : result) :
  k = evalv vd defs n H sc (v :: stk) rest ->
  vtyped sigs Σ v t -> vlocs v = [] ->
  T sigs G B C R rest ((Sh, t) :: s) s3 -> INV Σ H sc G (stk ++ Sf) (s ++ sf) Os ->
  length stk = length s -> res_ok Σ sc G B C R s3 Sf sf k.
Proof.
  intros -> Hv Hl HT [Iv Bd] L. eapply (next_ok n IH); [exact HT | | | apply scope_ext_refl].
  - split; [|exact Bd]. simpl. apply inv_push_sh; [exact Iv | exact Hv | rewrite Hl; simpl; tauto].
  - len.
Qed.

Lemma w_add G B C R rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest ((Sh, TInt) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, TInt) :: (Sh, TInt) :: s) ++ sf) Os ->
  length stk = length ((Sh, TInt) :: (Sh, TInt) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WAdd :: rest)).
Proof.
  intros HT [Iv Bd] L. destruct stk as [|v1 [|v2 stk]]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & V1 & _ & Iv). pop_sh Iv. destruct Iv as (-> & V2 & _ & Iv).
  apply vt_int_inv in V1 as (a & ->). apply vt_int_inv in V2 as (b & ->).
  eapply w_push_sh with (t := TInt).
  + reflexivity.
  + apply vt_int.
  + reflexivity.
  + exact HT.
  + split; eauto.
  + len.
Qed.

Lemma w_cat G B C R rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest ((Sh, TStr) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, TStr) :: (Sh, TStr) :: s) ++ sf) Os ->
  length stk = length ((Sh, TStr) :: (Sh, TStr) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WCat :: rest)).
Proof.
  intros HT [Iv Bd] L. destruct stk as [|v1 [|v2 stk]]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & V1 & _ & Iv). pop_sh Iv. destruct Iv as (-> & V2 & _ & Iv).
  apply vt_str_inv in V1 as (a & ->). apply vt_str_inv in V2 as (b & ->).
  eapply w_push_sh with (t := TStr).
  + reflexivity.
  + apply vt_str.
  + reflexivity.
  + exact HT.
  + split; eauto.
  + len.
Qed.

Lemma w_dup G B C R t rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest ((Sh, t) :: (Sh, t) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, t) :: s) ++ sf) Os ->
  length stk = length ((Sh, t) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WDup :: rest)).
Proof.
  intros HT [Iv Bd] L. destruct stk as [|v stk]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & V & Hl & Iv).
  simpl. eapply (next_ok n IH); [exact HT | | | apply scope_ext_refl].
  - split; [|exact Bd]. simpl. apply inv_push_sh; [apply inv_push_sh; [exact Iv | exact V | exact Hl] | exact V | simpl; exact Hl].
  - len.
Qed.

Lemma w_drop G B C R p rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest s s3 ->
  INV Σ H sc G (stk ++ Sf) ((p :: s) ++ sf) Os ->
  length stk = length (p :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WDrop :: rest)).
Proof.
  intros HT [Iv Bd] L. destruct stk as [|v stk]; try len. simpl in Iv.
  destruct (inv_cons_Os _ _ _ _ _ _ _ _ _ Iv) as (O & Os' & ->).
  destruct (inv_drop sigs _ _ _ _ _ _ _ _ _ _ Iv) as (Σ' & Sx & _ & Iv').
  simpl. eapply (next_ok n IH); [exact HT | split; eauto | len | exact Sx].
Qed.

Lemma w_swap G B C R p q rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest (q :: p :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) ((p :: q :: s) ++ sf) Os ->
  length stk = length (p :: q :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WSwap :: rest)).
Proof.
  intros HT [Iv Bd] L. destruct stk as [|v1 [|v2 stk]]; try len. simpl in Iv.
  destruct (inv_cons_Os _ _ _ _ _ _ _ _ _ Iv) as (O1 & Os1 & ->).
  pose proof (inv_slots _ _ _ _ _ _ _ _ Iv) as F. inversion F as [|? ? ? ? ? ? _ F1]; subst.
  inversion F1 as [|? ? ? ? ? ? _ _]; subst.
  simpl. eapply (next_ok n IH); [exact HT | | | apply scope_ext_refl].
  - split; [|exact Bd]. simpl. eapply inv_swap; exact Iv.
  - len.
Qed.

Lemma w_load G B C R x t rest s s3 Σ H sc stk Sf sf Os :
  lookup x G = Some t ->
  T sigs G B C R rest ((Sh, t) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (s ++ sf) Os -> length stk = length s ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WLoad x :: rest)).
Proof.
  intros Hx HT [Iv Bd] L. destruct (inv_scope_obj _ _ _ _ _ _ _ Iv) as (kvs & E & Ok & Hr & Nsc).
  simpl. unfold scope_get. rewrite E. destruct (lookup x kvs) as [v|] eqn:Ev; [|eapply err_here; [split; eauto | exact L]].
  simpl in Ok. destruct (Ok x v Ev) as (t' & Ht' & Hv). rewrite Hx in Ht'. inversion Ht'; subst.
  eapply (next_ok n IH); [exact HT | | len | apply scope_ext_refl].
  split; [|exact Bd]. simpl. apply inv_push_sh; [exact Iv | exact Hv |].
  intros l Hl. apply Hr. simpl. apply in_flat_map. exists (x, v). split; auto. apply lookup_in; auto.
Qed.

Lemma lookup_dset {A} x x' (v : A) kvs :
  lookup x' (dset x v kvs) = if String.eqb x' x then Some v else lookup x' kvs.
Proof.
  unfold dset. simpl. destruct (String.eqb_spec x' x); auto. apply lookup_remove_neq; auto.
Qed.

Lemma olocs_dset_scope x v kvs r :
  In r (olocs (OScope (dset x v kvs))) -> In r (vlocs v) \/ In r (olocs (OScope kvs)).
Proof.
  simpl. intros [Hr|Hr]%in_app_or; auto. right.
  apply in_flat_map in Hr as ([k w] & Hk & Hr). apply in_flat_map. exists (k, w). split; auto.
  unfold remove_key in Hk. apply filter_In in Hk as [Hk _]. auto.
Qed.

Lemma w_store G B C R x t rest s s3 Σ H sc stk Sf sf Os :
  lookup x G = Some t ->
  T sigs G B C R rest s s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, t) :: s) ++ sf) Os -> length stk = length ((Sh, t) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WStore x :: rest)).
Proof.
  intros Hx HT [Iv Bd] L. destruct stk as [|v stk]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & V & Hl & Iv).
  destruct (inv_scope_obj _ _ _ _ _ _ _ Iv) as (kvs & E & Ok & Hr & Nsc).
  simpl. unfold scope_get. rewrite E.
  eapply (next_ok n IH); [exact HT | | len | apply scope_ext_refl].
  split.
  - eapply inv_update_out; eauto.
    + apply (inv_scope _ _ _ _ _ _ _ _ Iv).
    + unfold obj_ok. intros x' v' Ex'. rewrite lookup_dset in Ex'. destruct (String.eqb_spec x' x) as [->|Hn].
      * inversion Ex'; subst. eauto.
      * simpl in Ok. apply Ok; auto.
    + intros r0 Hr0. apply olocs_dset_scope in Hr0 as [Hr0|Hr0]; auto.
  - apply bounded_update; auto. intros r0 Hr0.
    pose proof (inv_len _ _ _ _ _ _ _ _ Iv) as Ln.
    apply olocs_dset_scope in Hr0 as [Hr0|Hr0].
    + rewrite <- Ln. exact (vtyped_vlocs_lt sigs Σ v t V r0 Hr0).
    + eapply Bd; eauto.
Qed.


Lemma w_quote G B C R e ins outs rest s s3 Σ H sc stk Sf sf Os :
  closure_ok sigs G e ins outs ->
  T sigs G B C R rest ((Sh, TQuote ins outs) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (s ++ sf) Os -> length stk = length s ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WQuote e :: rest)).
Proof.
  intros Hc HT [Iv Bd] L. simpl.
  eapply w_push_sh with (t := TQuote ins outs).
  + reflexivity.
  + eapply vt_clo; [apply (inv_scope _ _ _ _ _ _ _ _ Iv) | exact Hc].
  + reflexivity.
  + exact HT.
  + split; eauto.
  + len.
Qed.

Lemma w_exec G B C R ins outs rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest (shs outs ++ s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, TQuote ins (Some outs)) :: shs ins ++ s) ++ sf) Os ->
  length stk = length ((Sh, TQuote ins (Some outs)) :: shs ins ++ s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WExec :: rest)).
Proof.
  intros HT [Iv Bd] L. destruct stk as [|v stk]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & V & Hl & Iv).
  apply vt_quote_inv in V as (sc' & e & G' & -> & Esc' & Hc).
  simpl. simpl in Hc.
  eapply cont_quote with (sc' := sc') (G' := G') (s2 := shs outs ++ s).
  - apply (inv_scope _ _ _ _ _ _ _ _ Iv).
  - eapply (IH G' LNone LNone RNone e (shs ins ++ s) (shs outs ++ s) (Hc s)).
    + split; [|exact Bd]. eapply inv_scope_change; [| exact Esc']. exact Iv.
    + len.
  - intros Σ' H' S' Os' Sx Iv' L'. eapply (next_ok n IH); [exact HT | exact Iv' | exact L' | exact Sx].
Qed.

Lemma w_exec_never G B C R ins rest s s' s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest s' s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, TQuote ins None) :: shs ins ++ s) ++ sf) Os ->
  length stk = length ((Sh, TQuote ins None) :: shs ins ++ s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WExec :: rest)).
Proof.
  intros HT [Iv Bd] L. destruct stk as [|v stk]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & V & Hl & Iv).
  apply vt_quote_inv in V as (sc' & e & G' & -> & Esc' & Hc).
  simpl. simpl in Hc.
  pose proof (IH G' LNone LNone RNone e (shs ins ++ s) [(Sh, TBot)] (Hc s [(Sh, TBot)]) Σ H sc' stk Sf sf Os0) as Hr.
  assert (Hr' : res_ok Σ sc' G' LNone LNone RNone [(Sh, TBot)] Sf sf (evalv vd defs n H sc' stk e)).
  { apply Hr; [| len]. split; [|exact Bd]. eapply inv_scope_change; [| exact Esc']. exact Iv. }
  destruct (evalv vd defs n H sc' stk e) as [| |He| |o H' S'] eqn:Ev; simpl; auto.
  { eapply err_lift; [exact (inv_scope _ _ _ _ _ _ _ _ Iv) | exact Hr']. }
  destruct Hr' as (Σ' & st' & Os' & Sx & Iv' & L' & O). destruct o; simpl in O; try contradiction.
  subst. destruct S' as [|w S']; [simpl in L'; lia|]. exfalso. eapply slot_bot_false; eauto.
Qed.

Lemma w_if G B C R e1 e2 rest s s' s3 Σ H sc stk Sf sf Os :
  T sigs G B C R e1 s s' -> T sigs G B C R e2 s s' ->
  T sigs G B C R rest s' s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, TBool) :: s) ++ sf) Os ->
  length stk = length ((Sh, TBool) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WIf e1 e2 :: rest)).
Proof.
  intros H1 H2 HT [Iv Bd] L. destruct stk as [|v stk]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & V & Hl & Iv).
  apply vt_bool_inv in V as (b & ->). simpl.
  apply cont_same with (s2 := s').
  - destruct b; [eapply (IH _ _ _ _ _ _ _ H1) | eapply (IH _ _ _ _ _ _ _ H2)];
      [split; [exact Iv | exact Bd] | len | split; [exact Iv | exact Bd] | len].
  - intros Σ' H' S' Os' Sx Iv' L'. eapply (next_ok n IH); [exact HT | exact Iv' | exact L' | exact Sx].
Qed.

Lemma w_break_exact C R s s3 rest Σ H sc stk Sf sf Os G :
  INV Σ H sc G (stk ++ Sf) (s ++ sf) Os -> length stk = length s ->
  res_ok Σ sc G (LExact s) C R s3 Sf sf (evalv vd defs (S n) H sc stk (WBreak :: rest)).
Proof.
  intros Iv L. simpl. exists Σ, s, Os.
  split; [apply scope_ext_refl | split; [exact Iv | split; [exact L | simpl; auto]]].
Qed.

Lemma w_break_child C R s s3 rest Σ H sc stk Sf sf Os G :
  INV Σ H sc G (stk ++ Sf) (s ++ sf) Os -> length stk = length s ->
  res_ok Σ sc G LChild C R s3 Sf sf (evalv vd defs (S n) H sc stk (WBreak :: rest)).
Proof.
  intros Iv L. simpl. exists Σ, s, Os.
  split; [apply scope_ext_refl | split; [exact Iv | split; [exact L | simpl; auto]]].
Qed.

Lemma w_cont_exact B R s s3 rest Σ H sc stk Sf sf Os G :
  INV Σ H sc G (stk ++ Sf) (s ++ sf) Os -> length stk = length s ->
  res_ok Σ sc G B (LExact s) R s3 Sf sf (evalv vd defs (S n) H sc stk (WContinue :: rest)).
Proof.
  intros Iv L. simpl. exists Σ, s, Os.
  split; [apply scope_ext_refl | split; [exact Iv | split; [exact L | simpl; auto]]].
Qed.

Lemma w_cont_child B R s s3 rest Σ H sc stk Sf sf Os G :
  INV Σ H sc G (stk ++ Sf) (s ++ sf) Os -> length stk = length s ->
  res_ok Σ sc G B LChild R s3 Sf sf (evalv vd defs (S n) H sc stk (WContinue :: rest)).
Proof.
  intros Iv L. simpl. exists Σ, s, Os.
  split; [apply scope_ext_refl | split; [exact Iv | split; [exact L | simpl; auto]]].
Qed.

Lemma w_return B C s s3 rest Σ H sc stk Sf sf Os G :
  INV Σ H sc G (stk ++ Sf) (s ++ sf) Os -> length stk = length s ->
  res_ok Σ sc G B C (RSome s) s3 Sf sf (evalv vd defs (S n) H sc stk (WReturn :: rest)).
Proof.
  intros Iv L. simpl. exists Σ, s, Os.
  split; [apply scope_ext_refl | split; [exact Iv | split; [exact L | simpl; auto]]].
Qed.

(** [return] in top-level code: the script ends, with any stack. *)
Lemma w_return_any B C s s3 rest Σ H sc stk Sf sf Os G :
  INV Σ H sc G (stk ++ Sf) (s ++ sf) Os -> length stk = length s ->
  res_ok Σ sc G B C RAny s3 Sf sf (evalv vd defs (S n) H sc stk (WReturn :: rest)).
Proof.
  intros Iv L. simpl. exists Σ, s, Os.
  split; [apply scope_ext_refl | split; [exact Iv | split; [exact L | simpl; auto]]].
Qed.

Lemma w_exit G B C R s s3 rest Σ H sc stk Sf sf Os :
  INV Σ H sc G (stk ++ Sf) (((Sh, TInt) :: s) ++ sf) Os -> length stk = length ((Sh, TInt) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WExit :: rest)).
Proof.
  intros [Iv Bd] L. destruct stk as [|v stk]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & V & Hl & Iv). apply vt_int_inv in V as (c & ->). simpl. exact Logic.I.
Qed.

Lemma w_loop G B C R e rest s s3 Σ H sc stk Sf sf Os :
  T sigs G (LExact s) (LExact s) R e s s ->
  T sigs G B C R rest s s3 ->
  INV Σ H sc G (stk ++ Sf) (s ++ sf) Os -> length stk = length s ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WLoop e :: rest)).
Proof.
  intros He HT Iv L. simpl.
  pose proof (IH _ _ _ _ _ _ _ He Σ H sc stk Sf sf Os Iv L) as Hr.
  destruct (evalv vd defs n H sc stk e) as [| |Herr| |o H' S'] eqn:Ev; simpl; auto.
  destruct Hr as (Σ' & st' & Os' & Sx & Iv' & L' & O).
  destruct o; simpl in O; subst.
  - eapply res_ok_weaken; [exact Sx|].
    eapply (IH G B C R (WLoop e :: rest) s s3); [| exact Iv' | exact L'].
    econstructor; [apply tw_loop; exact He | exact HT].
  - eapply (next_ok n IH); [exact HT | exact Iv' | exact L' | exact Sx].
  - eapply res_ok_weaken; [exact Sx|].
    eapply (IH G B C R (WLoop e :: rest) s s3); [| exact Iv' | exact L'].
    econstructor; [apply tw_loop; exact He | exact HT].
  - exists Σ', st', Os'. split; [exact Sx | split; [exact Iv' | split; [exact L' | exact O]]].
Qed.

Lemma w_loop_forever G B C R e rest s s' s3 Σ H sc stk Sf sf Os :
  T sigs G LNone (LExact s) R e s s ->
  T sigs G B C R rest s' s3 ->
  INV Σ H sc G (stk ++ Sf) (s ++ sf) Os -> length stk = length s ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WLoop e :: rest)).
Proof.
  intros He HT Iv L. simpl.
  pose proof (IH _ _ _ _ _ _ _ He Σ H sc stk Sf sf Os Iv L) as Hr.
  destruct (evalv vd defs n H sc stk e) as [| |Herr| |o H' S'] eqn:Ev; simpl; auto.
  destruct Hr as (Σ' & st' & Os' & Sx & Iv' & L' & O).
  destruct o; simpl in O; try contradiction; subst.
  - eapply res_ok_weaken; [exact Sx|].
    eapply (IH G B C R (WLoop e :: rest) s s3); [| exact Iv' | exact L'].
    econstructor; [apply tw_loop_forever; exact He | exact HT].
  - eapply res_ok_weaken; [exact Sx|].
    eapply (IH G B C R (WLoop e :: rest) s s3); [| exact Iv' | exact L'].
    econstructor; [apply tw_loop_forever; exact He | exact HT].
  - exists Σ', st', Os'. split; [exact Sx | split; [exact Iv' | split; [exact L' | exact O]]].
Qed.

Lemma INV_new_scope Σ H sc G G' L st Os :
  INV Σ H sc G L st Os ->
  INV (Σ ++ [HScope G']) (H ++ [OScope []]) (length H) G' L st Os /\ scope_ext Σ (Σ ++ [HScope G']).
Proof.
  intros [Iv Bd]. pose proof (inv_len _ _ _ _ _ _ _ _ Iv) as Ln. split; [split|].
  - eapply inv_scope_change.
    + eapply inv_alloc_out; [exact Iv | simpl; intros x v E; discriminate | simpl; tauto].
    + rewrite <- Ln. apply nth_error_app_eq.
  - apply bounded_app; auto. simpl; tauto.
  - apply scope_ext_app.
Qed.

Lemma w_call G B C R f ins outs rest s s3 Σ H sc stk Sf sf Os :
  g_sigs sigs f ins (Some outs) ->
  T sigs G B C R rest (outs ++ s) s3 ->
  INV Σ H sc G (stk ++ Sf) ((ins ++ s) ++ sf) Os -> length stk = length (ins ++ s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WCall f :: rest)).
Proof.
  intros Hs HT Iv L. destruct (Hdefs f ins (Some outs) Hs) as (body & G' & Ed & Hb).
  simpl. rewrite Ed.
  destruct (INV_new_scope _ _ _ _ G' _ _ _ Iv) as [Iv1 Sx1].
  pose proof (IH _ _ _ _ _ _ _ (Hb s) _ _ _ stk Sf sf Os Iv1 L) as Hr.
  pose proof (inv_scope _ _ _ _ _ _ _ _ (proj1 Iv)) as Esc.
  destruct (evalv vd defs n (H ++ [OScope []]) (length H) stk body) as [| |He| |o H' S'] eqn:Ev; simpl; auto;
    try (eapply err_weaken; [exact Sx1 | eapply err_lift; [apply Sx1; exact (inv_scope _ _ _ _ _ _ _ _ (proj1 Iv)) | exact Hr]]).
  destruct Hr as (Σ' & st' & Os' & Sx & Iv' & L' & O).
  destruct o; simpl in O; try contradiction; subst.
  - eapply (next_ok n IH); [exact HT | | exact L' | eapply scope_ext_trans; eauto].
    eapply INV_scope; [exact Iv' |]. apply Sx. apply Sx1. exact Esc.
  - eapply (next_ok n IH); [exact HT | | exact L' | eapply scope_ext_trans; eauto].
    eapply INV_scope; [exact Iv' |]. apply Sx. apply Sx1. exact Esc.
Qed.

Lemma w_call_never G B C R f ins rest s s' s3 Σ H sc stk Sf sf Os :
  g_sigs sigs f ins None ->
  T sigs G B C R rest s' s3 ->
  INV Σ H sc G (stk ++ Sf) ((ins ++ s) ++ sf) Os -> length stk = length (ins ++ s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WCall f :: rest)).
Proof.
  intros Hs HT Iv L. destruct (Hdefs f ins None Hs) as (body & G' & Ed & Hb).
  simpl. rewrite Ed.
  destruct (INV_new_scope _ _ _ _ G' _ _ _ Iv) as [Iv1 Sx1].
  pose proof (IH _ _ _ _ _ _ _ (Hb s [(Sh, TBot)]) _ _ _ stk Sf sf Os Iv1 L) as Hr.
  destruct (evalv vd defs n (H ++ [OScope []]) (length H) stk body) as [| |He| |o H' S'] eqn:Ev; simpl; auto;
    try (eapply err_weaken; [exact Sx1 | eapply err_lift; [apply Sx1; exact (inv_scope _ _ _ _ _ _ _ _ (proj1 Iv)) | exact Hr]]).
  destruct Hr as (Σ' & st' & Os' & Sx & Iv' & L' & O).
  destruct o; simpl in O; try contradiction; subst.
  exfalso. destruct S' as [|w S']; [simpl in L'; lia|]. eapply slot_bot_false; eauto.
Qed.

Lemma w_nil G B C R t rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest ((Dp, TList t) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (s ++ sf) Os -> length stk = length s ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WNil :: rest)).
Proof.
  intros HT [Iv Bd] L. simpl.
  eapply (next_ok n IH); [exact HT | | len | apply (scope_ext_app Σ (HDead))].
  split.
  - simpl. eapply inv_alloc_dp; [exact Iv | exact Bd | reflexivity |].
    apply dt_list with (vs := []) (Os := []); [apply nth_error_app_eq | constructor | constructor; [simpl; tauto | constructor]].
  - apply bounded_app; auto. simpl; tauto.
Qed.

Lemma w_dictnew G B C R rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest ((Dp, TRec [] FAbs) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (s ++ sf) Os -> length stk = length s ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WDictNew :: rest)).
Proof.
  intros HT [Iv Bd] L. simpl.
  eapply (next_ok n IH); [exact HT | | len | apply (scope_ext_app Σ (HDead))].
  split.
  - simpl. eapply inv_alloc_dp; [exact Iv | exact Bd | reflexivity |].
    apply dt_rec with (kvs := []) (Os := []).
    + apply nth_error_app_eq.
    + constructor.
    + intros k t0 E. unfold field_at in E. simpl in E. discriminate.
    + constructor.
    + constructor; [simpl; tauto | constructor].
  - apply bounded_app; auto. simpl; tauto.
Qed.

(** A shared list operand: its location, store type and object. *)
Lemma sh_list Σ H sc G v L t st Os :
  inv sigs Σ H sc G L st Os -> vtyped sigs Σ v (TList t) ->
  (forall l, In l (vlocs v) -> ~ In l (concat Os)) ->
  exists l a vs, v = VLoc l /\ nth_error Σ l = Some (HList a) /\ teq a t /\ ~ In l (concat Os) /\
    nth_error H l = Some (OList vs) /\ Forall (fun x => vtyped sigs Σ x a) vs /\
    (forall r, In r (olocs (OList vs)) -> ~ In r (concat Os)).
Proof.
  intros Iv V Hl. apply vt_list_inv in V as (l & a & -> & E & Ta).
  assert (Nl : ~ In l (concat Os)) by (apply Hl; apply in_eq).
  destruct (inv_obj _ _ _ _ _ _ _ _ _ Iv E ltac:(discriminate) Nl) as (o & Eo & Ok & Hr).
  destruct o; simpl in Ok; try contradiction. eexists _, _, _; repeat split; eauto.
  - destruct Ta; auto.
  - destruct Ta; auto.
Qed.

Lemma sh_rec Σ H sc G v L fs r st Os :
  inv sigs Σ H sc G L st Os -> vtyped sigs Σ v (TRec fs r) ->
  (forall l, In l (vlocs v) -> ~ In l (concat Os)) ->
  exists l fs' r' kvs, v = VLoc l /\ nth_error Σ l = Some (HRec fs' r') /\
    (forall k, fsub (field_at k fs' r') (field_at k fs r)) /\ ~ In l (concat Os) /\
    nth_error H l = Some (ODict kvs) /\ obj_ok sigs Σ (ODict kvs) (HRec fs' r') /\
    (forall r0, In r0 (olocs (ODict kvs)) -> ~ In r0 (concat Os)).
Proof.
  intros Iv V Hl. apply vt_rec_inv in V as (l & fs' & r' & -> & E & Hs).
  assert (Nl : ~ In l (concat Os)) by (apply Hl; apply in_eq).
  destruct (inv_obj _ _ _ _ _ _ _ _ _ Iv E ltac:(discriminate) Nl) as (o & Eo & Ok & Hr).
  destruct o; simpl in Ok; try contradiction.
  exists l, fs', r', kvs. repeat split; auto.
  - apply sub_rec_fsub; exact Hs.
  - destruct Ok as (? & ? & ?); auto.
  - destruct Ok as (? & ? & ?); auto.
  - destruct Ok as (? & ? & ?); auto.
Qed.

Lemma Forall_set_nth {A} (P' : A -> Prop) i x l : Forall P' l -> P' x -> Forall P' (set_nth i x l).
Proof. revert i; induction l; intros [|i] F Px; simpl; inversion F; subst; constructor; auto. Qed.

Lemma in_flat_map_set_nth i x vs r :
  In r (flat_map vlocs (set_nth i x vs)) -> In r (vlocs x) \/ In r (flat_map vlocs vs).
Proof.
  revert i; induction vs as [|v vs IHv]; intros [|i]; simpl; auto; intros Hr;
    apply in_app_or in Hr as [Hr|Hr]; auto.
  - right; apply in_or_app; auto.
  - right; apply in_or_app; auto.
  - destruct (IHv i Hr); auto. right; apply in_or_app; auto.
Qed.

Lemma w_push_sh_list G B C R t rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest ((Sh, TList t) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, t) :: (Sh, TList t) :: s) ++ sf) Os ->
  length stk = length ((Sh, t) :: (Sh, TList t) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WPush :: rest)).
Proof.
  intros HT [Iv Bd] L. destruct stk as [|x [|v stk]]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & Vx & Hx & Iv). pop_sh Iv. destruct Iv as (-> & Vv & Hv & Iv).
  destruct (sh_list _ _ _ _ _ _ _ _ _ Iv Vv Hv) as (l & a & vs & -> & E & Ta & Nl & Eo & Fv & Hr).
  simpl. rewrite Eo. pose proof (inv_len _ _ _ _ _ _ _ _ Iv) as Ln.
  eapply (next_ok n IH); [exact HT | | len | apply scope_ext_refl].
  split.
  - simpl. apply inv_push_sh.
    + eapply inv_update_out with (o := OList vs) (h := HList a); [exact Iv | exact Eo | exact Nl | exact E | |].
      * simpl. apply Forall_app; split; auto. constructor; auto. eapply vtyped_sub; eauto. apply Ta.
      * simpl. intros r0 Hr0. rewrite flat_map_app in Hr0. apply in_app_or in Hr0 as [Hr0|Hr0].
        -- apply Hr; auto.
        -- simpl in Hr0. rewrite app_nil_r in Hr0. exact (Hx r0 Hr0).
    + eapply vt_list; eauto. apply s_list; apply Ta.
    + simpl. intros l0 [<-|[]]. exact Nl.
  - apply bounded_update; auto. intros r0 Hr0. simpl in Hr0. rewrite flat_map_app in Hr0.
    apply in_app_or in Hr0 as [Hr0|Hr0].
    + eapply Bd; eauto.
    + simpl in Hr0. rewrite app_nil_r in Hr0. rewrite <- Ln. exact (vtyped_vlocs_lt sigs Σ x t Vx r0 Hr0).
Qed.

Lemma w_push_dp_list G B C R t rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest ((Dp, TList t) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Dp, t) :: (Dp, TList t) :: s) ++ sf) Os ->
  length stk = length ((Dp, t) :: (Dp, TList t) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WPush :: rest)).
Proof.
  intros HT [Iv Bd] L. destruct stk as [|x [|v stk]]; try len. simpl in Iv.
  destruct (inv_cons_Os _ _ _ _ _ _ _ _ _ Iv) as (Ox & Os1 & ->).
  pose proof (inv_slots _ _ _ _ _ _ _ _ Iv) as F. inversion F as [|? ? ? ? ? ? _ F1]; subst.
  inversion F1 as [|? ? ? ? ? ? Pv _]; subst. unfold slot_ok in Pv; simpl in Pv.
  apply dt_list_inv in Pv as (l & vs0 & Osl & -> & _ & _ & -> & _).
  destruct (inv_push_dp sigs _ _ _ _ _ _ _ _ _ _ _ _ Iv Bd) as (vs & O' & Eo & Iv' & Bd').
  simpl. rewrite Eo.
  eapply (next_ok n IH); [exact HT | split; [exact Iv' | exact Bd'] | len | apply scope_ext_refl].
Qed.

Lemma w_getat G B C R t rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest ((Sh, t) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, TInt) :: (Sh, TList t) :: s) ++ sf) Os ->
  length stk = length ((Sh, TInt) :: (Sh, TList t) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WGetAt :: rest)).
Proof.
  intros HT I0 L. pose proof I0 as [Iv Bd]. destruct stk as [|x [|v stk]]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & Vx & Hx & Iv). pop_sh Iv. destruct Iv as (-> & Vv & Hv & Iv).
  apply vt_int_inv in Vx as (i & ->).
  destruct (sh_list _ _ _ _ _ _ _ _ _ Iv Vv Hv) as (l & a & vs & -> & E & Ta & Nl & Eo & Fv & Hr).
  simpl. rewrite Eo. destruct (nth_error vs i) as [y|] eqn:Ey; [|eapply err_here; [exact I0 | exact L]].
  eapply (next_ok n IH); [exact HT | | len | apply scope_ext_refl].
  split; auto. simpl. apply inv_push_sh; [exact Iv | |].
  - rewrite Forall_forall in Fv. eapply vtyped_sub; [apply Fv; eapply nth_error_In; eauto | apply Ta].
  - intros l0 Hl0. apply Hr. simpl. apply in_flat_map. exists y. split; auto. eapply nth_error_In; eauto.
Qed.

Lemma w_setat G B C R t rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest ((Sh, TList t) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, t) :: (Sh, TInt) :: (Sh, TList t) :: s) ++ sf) Os ->
  length stk = length ((Sh, t) :: (Sh, TInt) :: (Sh, TList t) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WSetAt :: rest)).
Proof.
  intros HT I0 L. pose proof I0 as [Iv Bd]. destruct stk as [|x [|w [|v stk]]]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & Vx & Hx & Iv). pop_sh Iv. destruct Iv as (-> & Vw & Hw & Iv).
  pop_sh Iv. destruct Iv as (-> & Vv & Hv & Iv).
  apply vt_int_inv in Vw as (i & ->).
  destruct (sh_list _ _ _ _ _ _ _ _ _ Iv Vv Hv) as (l & a & vs & -> & E & Ta & Nl & Eo & Fv & Hr).
  simpl. rewrite Eo. destruct (i <? length vs); [|eapply err_here; [exact I0 | exact L]].
  pose proof (inv_len _ _ _ _ _ _ _ _ Iv) as Ln.
  eapply (next_ok n IH); [exact HT | | len | apply scope_ext_refl].
  split.
  - simpl. apply inv_push_sh.
    + eapply inv_update_out with (o := OList vs) (h := HList a); [exact Iv | exact Eo | exact Nl | exact E | |].
      * simpl. apply Forall_set_nth; auto. eapply vtyped_sub; eauto. apply Ta.
      * simpl. intros r0 Hr0. apply in_flat_map_set_nth in Hr0 as [Hr0|Hr0];
          [exact (Hx r0 Hr0) | exact (Hr r0 Hr0)].
    + eapply vt_list; eauto. apply s_list; apply Ta.
    + simpl. intros l0 [<-|[]]. exact Nl.
  - apply bounded_update; auto. intros r0 Hr0. simpl in Hr0.
    apply in_flat_map_set_nth in Hr0 as [Hr0|Hr0].
    + rewrite <- Ln. exact (vtyped_vlocs_lt sigs Σ x t Vx r0 Hr0).
    + eapply Bd; eauto.
Qed.

Lemma w_getk G B C R k fs r rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest ((Sh, TMaybe (fty (field_at k fs r))) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, TRec fs r) :: s) ++ sf) Os ->
  length stk = length ((Sh, TRec fs r) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WGetK k :: rest)).
Proof.
  intros HT [Iv Bd] L. destruct stk as [|v stk]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & Vv & Hv & Iv).
  destruct (sh_rec _ _ _ _ _ _ _ _ _ _ Iv Vv Hv) as (l & fs' & r' & kvs & -> & E & Hf & Nl & Eo & Ok & Hr).
  simpl. rewrite Eo.
  eapply (next_ok n IH); [exact HT | | len | apply scope_ext_refl].
  split; [|exact Bd]. simpl. apply inv_push_sh; [exact Iv | |].
  - destruct (lookup k kvs) as [x|] eqn:Ex; [apply (vt_vjust sigs Hmaybe) | apply (vt_vnone sigs Hmaybe)].
    eapply vtyped_sub; [eapply obj_dict_lookup; eauto | apply fsub_fty; auto].
  - destruct (lookup k kvs) as [x|] eqn:Ex; simpl; [rewrite app_nil_r | tauto].
    intros l0 Hl0. apply Hr. eapply olocs_lookup; eauto.
Qed.

Lemma w_getd G B C R fs r t rest s s3 Σ H sc stk Sf sf Os :
  (forall k, sub (fty (field_at k fs r)) t) ->
  T sigs G B C R rest ((Sh, TMaybe t) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, TStr) :: (Sh, TRec fs r) :: s) ++ sf) Os ->
  length stk = length ((Sh, TStr) :: (Sh, TRec fs r) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WGetD :: rest)).
Proof.
  intros Ht HT [Iv Bd] L. destruct stk as [|w [|v stk]]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & Vw & Hw & Iv). pop_sh Iv. destruct Iv as (-> & Vv & Hv & Iv).
  apply vt_str_inv in Vw as (k & ->).
  destruct (sh_rec _ _ _ _ _ _ _ _ _ _ Iv Vv Hv) as (l & fs' & r' & kvs & -> & E & Hf & Nl & Eo & Ok & Hr).
  simpl. rewrite Eo.
  eapply (next_ok n IH); [exact HT | | len | apply scope_ext_refl].
  split; [|exact Bd]. simpl. apply inv_push_sh; [exact Iv | |].
  - destruct (lookup k kvs) as [x|] eqn:Ex; [apply (vt_vjust sigs Hmaybe) | apply (vt_vnone sigs Hmaybe)].
    eapply vtyped_sub; [eapply obj_dict_lookup; eauto |].
    eapply sub_trans; [apply fsub_fty; apply Hf | apply Ht].
  - destruct (lookup k kvs) as [x|] eqn:Ex; simpl; [rewrite app_nil_r | tauto].
    intros l0 Hl0. apply Hr. eapply olocs_lookup; eauto.
Qed.

Lemma w_getreq G B C R k fs r t rest s s3 Σ H sc stk Sf sf Os :
  field_at k fs r = FReq t ->
  T sigs G B C R rest ((Sh, t) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, TRec fs r) :: s) ++ sf) Os ->
  length stk = length ((Sh, TRec fs r) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WGetReq k :: rest)).
Proof.
  intros Hk HT [Iv Bd] L. destruct stk as [|v stk]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & Vv & Hv & Iv).
  destruct (sh_rec _ _ _ _ _ _ _ _ _ _ Iv Vv Hv) as (l & fs' & r' & kvs & -> & E & Hf & Nl & Eo & Ok & Hr).
  specialize (Hf k). rewrite Hk in Hf. inversion Hf as [a b Hab Hba Ek| | | | | | ]; subst.
  simpl. rewrite Eo.
  destruct (lookup k kvs) as [x|] eqn:Ex.
  2:{ exfalso. destruct Ok as (_ & Rq & _). eapply Rq; eauto. }
  eapply (next_ok n IH); [exact HT | | len | apply scope_ext_refl].
  split; [|exact Bd]. simpl. apply inv_push_sh; [exact Iv | |].
  - eapply vtyped_sub; [eapply obj_dict_lookup; eauto |]. rewrite <- Ek. simpl. exact Hab.
  - intros l0 Hl0. apply Hr. eapply olocs_lookup; eauto.
Qed.

Lemma w_setk_sh G B C R k fs r t rest s s3 Σ H sc stk Sf sf Os :
  writable (field_at k fs r) t ->
  T sigs G B C R rest ((Sh, TRec fs r) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, t) :: (Sh, TRec fs r) :: s) ++ sf) Os ->
  length stk = length ((Sh, t) :: (Sh, TRec fs r) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WSetK k :: rest)).
Proof.
  intros Hw HT [Iv Bd] L. destruct stk as [|x [|v stk]]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & Vx & Hx & Iv). pop_sh Iv. destruct Iv as (-> & Vv & Hv & Iv).
  pose proof Vv as Vv0.
  destruct (sh_rec _ _ _ _ _ _ _ _ _ _ Iv Vv Hv) as (l & fs' & r' & kvs & -> & E & Hf & Nl & Eo & Ok & Hr).
  destruct (fsub_writable _ _ _ (Hf k) Hw) as (t' & Hw' & Tt).
  simpl. rewrite Eo. pose proof (inv_len _ _ _ _ _ _ _ _ Iv) as Ln.
  eapply (next_ok n IH); [exact HT | | len | apply scope_ext_refl].
  split.
  - simpl. apply inv_push_sh; [| exact Vv0 | simpl; intros l0 [<-|[]]; exact Nl].
    eapply inv_update_out with (o := ODict kvs) (h := HRec fs' r'); [exact Iv | exact Eo | exact Nl | exact E | |].
    + eapply obj_ok_dset; eauto. eapply vtyped_sub; eauto. apply Tt.
    + intros r0 Hr0. apply olocs_dict_dset in Hr0 as [Hr0|Hr0]; [exact (Hx r0 Hr0) | exact (Hr r0 Hr0)].
  - apply bounded_update; auto. intros r0 Hr0. apply olocs_dict_dset in Hr0 as [Hr0|Hr0].
    + rewrite <- Ln. exact (vtyped_vlocs_lt sigs Σ x t Vx r0 Hr0).
    + eapply Bd; eauto.
Qed.

Lemma w_setd G B C R fs r t rest s s3 Σ H sc stk Sf sf Os :
  (forall k, writable (field_at k fs r) t) ->
  T sigs G B C R rest ((Sh, TRec fs r) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, t) :: (Sh, TStr) :: (Sh, TRec fs r) :: s) ++ sf) Os ->
  length stk = length ((Sh, t) :: (Sh, TStr) :: (Sh, TRec fs r) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WSetD :: rest)).
Proof.
  intros Hw HT [Iv Bd] L. destruct stk as [|x [|w [|v stk]]]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & Vx & Hx & Iv). pop_sh Iv. destruct Iv as (-> & Vw & Hw' & Iv).
  pop_sh Iv. destruct Iv as (-> & Vv & Hv & Iv).
  apply vt_str_inv in Vw as (k & ->).
  pose proof Vv as Vv0.
  destruct (sh_rec _ _ _ _ _ _ _ _ _ _ Iv Vv Hv) as (l & fs' & r' & kvs & -> & E & Hf & Nl & Eo & Ok & Hr).
  destruct (fsub_writable _ _ _ (Hf k) (Hw k)) as (t' & Hw2 & Tt).
  simpl. rewrite Eo. pose proof (inv_len _ _ _ _ _ _ _ _ Iv) as Ln.
  eapply (next_ok n IH); [exact HT | | len | apply scope_ext_refl].
  split.
  - simpl. apply inv_push_sh; [| exact Vv0 | simpl; intros l0 [<-|[]]; exact Nl].
    eapply inv_update_out with (o := ODict kvs) (h := HRec fs' r'); [exact Iv | exact Eo | exact Nl | exact E | |].
    + eapply obj_ok_dset; eauto. eapply vtyped_sub; eauto. apply Tt.
    + intros r0 Hr0. apply olocs_dict_dset in Hr0 as [Hr0|Hr0]; [exact (Hx r0 Hr0) | exact (Hr r0 Hr0)].
  - apply bounded_update; auto. intros r0 Hr0. apply olocs_dict_dset in Hr0 as [Hr0|Hr0].
    + rewrite <- Ln. exact (vtyped_vlocs_lt sigs Σ x t Vx r0 Hr0).
    + eapply Bd; eauto.
Qed.

Lemma w_del_sh G B C R k fs r t rest s s3 Σ H sc stk Sf sf Os :
  field_at k fs r = FDict t ->
  T sigs G B C R rest ((Sh, TRec fs r) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, TRec fs r) :: s) ++ sf) Os ->
  length stk = length ((Sh, TRec fs r) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WDel k :: rest)).
Proof.
  intros Hk HT [Iv Bd] L. destruct stk as [|v stk]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & Vv & Hv & Iv).
  pose proof Vv as Vv0.
  destruct (sh_rec _ _ _ _ _ _ _ _ _ _ Iv Vv Hv) as (l & fs' & r' & kvs & -> & E & Hf & Nl & Eo & Ok & Hr).
  specialize (Hf k). rewrite Hk in Hf. inversion Hf as [| | | |a b Hab Hba Ek| |]; subst.
  simpl. rewrite Eo.
  eapply (next_ok n IH); [exact HT | | len | apply scope_ext_refl].
  split.
  - simpl. apply inv_push_sh; [| exact Vv0 | simpl; intros l0 [<-|[]]; exact Nl].
    eapply inv_update_out with (o := ODict kvs) (h := HRec fs' r'); [exact Iv | exact Eo | exact Nl | exact E | |].
    + eapply obj_ok_remove; eauto.
    + intros r0 Hr0. apply olocs_dict_remove in Hr0. exact (Hr r0 Hr0).
  - apply bounded_update; auto. intros r0 Hr0. apply olocs_dict_remove in Hr0. eapply Bd; eauto.
Qed.

Lemma inv_swap' Σ H sc G a b L p q st Os :
  inv sigs Σ H sc G (a :: b :: L) (p :: q :: st) Os ->
  exists Oa Ob Os', Os = Oa :: Ob :: Os' /\ inv sigs Σ H sc G (b :: a :: L) (q :: p :: st) (Ob :: Oa :: Os').
Proof.
  intros Iv. pose proof (inv_slots _ _ _ _ _ _ _ _ Iv) as F.
  inversion F as [|? ? ? ? ? ? _ F1]; subst. inversion F1 as [|? ? ? ? ? ? _ _]; subst.
  eexists _, _, _. split; [reflexivity|]. apply inv_swap; auto.
Qed.

Lemma w_setk_dp G B C R k fs r t rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest ((Dp, TRec ((k, FReq t) :: fs) r) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Dp, t) :: (Dp, TRec fs r) :: s) ++ sf) Os ->
  length stk = length ((Dp, t) :: (Dp, TRec fs r) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WSetK k :: rest)).
Proof.
  intros HT [Iv Bd] L. destruct stk as [|x [|v stk]]; try len. simpl in Iv.
  pose proof (inv_slots _ _ _ _ _ _ _ _ Iv) as F.
  inversion F as [|? ? ? ? ? ? _ F1]; subst. inversion F1 as [|? ? ? ? ? ? Pv _]; subst.
  unfold slot_ok in Pv; simpl in Pv.
  apply dt_rec_inv in Pv as (l & kvs0 & Osl & -> & _ & _ & _ & _ & -> & _).
  destruct (inv_swap' _ _ _ _ _ _ _ _ _ _ _ Iv) as (Ox & Ol & Os' & EOs & Iv1). inversion EOs; subst.
  destruct (inv_rec_remove sigs Hmaybe _ _ _ _ _ _ _ _ _ _ _ k Iv1 Bd) as (kvs & Oold & Ol' & Eo & Iv2 & Bd2).
  destruct (inv_drop sigs _ _ _ _ _ _ _ _ _ _ Iv2) as (Σ1 & Sx1 & _ & Iv3).
  destruct (inv_swap' _ _ _ _ _ _ _ _ _ _ _ Iv3) as (Ol2 & Ox2 & Os2 & EOs2 & Iv4). inversion EOs2; subst.
  destruct (inv_rec_insert sigs _ _ _ _ _ _ _ _ _ _ _ _ _ _ k Iv4 Bd2) as (kvs1 & O' & Eo1 & Hk1 & Iv5 & Bd5).
  { apply field_at_cons_eq. }
  rewrite nth_error_set_nth_eq in Eo1 by (eapply nth_error_lt; eauto). inversion Eo1; subst kvs1.
  rewrite set_nth_set_nth in Iv5, Bd5.
  simpl. rewrite Eo.
  eapply (next_ok n IH); [exact HT | | len | exact Sx1].
  split; [|exact Bd5]. simpl.
  eapply (inv_replace_slot_at sigs _ _ _ _ [] _ _ [] _ _ _ [] _ _ eq_refl eq_refl Iv5).
  pose proof (slot_at _ _ _ _ _ [] _ _ [] _ _ [] _ _ eq_refl eq_refl Iv5) as Sl.
  unfold slot_ok in *; simpl in *. eapply dtyped_rec_same; [| exact Sl].
  intros k'. destruct (String.eqb_spec k' k) as [->|Hn].
  - rewrite !field_at_cons_eq. auto.
  - rewrite !field_at_cons_neq; auto.
Qed.

Lemma w_del_dp G B C R k fs r rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest ((Dp, TRec ((k, FAbs) :: fs) r) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Dp, TRec fs r) :: s) ++ sf) Os ->
  length stk = length ((Dp, TRec fs r) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WDel k :: rest)).
Proof.
  intros HT [Iv Bd] L. destruct stk as [|v stk]; try len. simpl in Iv.
  pose proof (inv_slots _ _ _ _ _ _ _ _ Iv) as F.
  inversion F as [|? ? ? ? ? ? Pv _]; subst. unfold slot_ok in Pv; simpl in Pv.
  apply dt_rec_inv in Pv as (l & kvs0 & Osl & -> & _ & _ & _ & _ & -> & _).
  destruct (inv_rec_remove sigs Hmaybe _ _ _ _ _ _ _ _ _ _ _ k Iv Bd) as (kvs & Oold & Ol' & Eo & Iv2 & Bd2).
  destruct (inv_drop sigs _ _ _ _ _ _ _ _ _ _ Iv2) as (Σ1 & Sx1 & _ & Iv3).
  simpl. rewrite Eo.
  eapply (next_ok n IH); [exact HT | split; [exact Iv3 | exact Bd2] | len | exact Sx1].
Qed.

(** ** Building partly new lists and dicts *)

Lemma w_nil_m G B C R m t rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest ((MList m, TList t) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (s ++ sf) Os -> length stk = length s ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WNil :: rest)).
Proof.
  intros HT [Iv Bd] L. simpl.
  eapply (next_ok n IH); [exact HT | | len | apply (scope_ext_app Σ HDead)].
  split.
  - simpl. eapply (inv_replace_slot_at sigs _ _ _ _ [] _ _ [] (Dp, TList t) _ _ [] _ _ eq_refl eq_refl).
    + eapply inv_alloc_dp; [exact Iv | exact Bd | reflexivity |].
      apply dt_list with (vs := []) (Os := []); [apply nth_error_app_eq | constructor | constructor; [simpl; tauto | constructor]].
    + apply slot_ok_mtyped. apply mtyped_new_list. apply nth_error_app_eq.
  - apply bounded_app; auto. simpl; tauto.
Qed.

Lemma w_push_m G B C R m t rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest ((MList m, TList t) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((m, t) :: (MList m, TList t) :: s) ++ sf) Os ->
  length stk = length ((m, t) :: (MList m, TList t) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WPush :: rest)).
Proof.
  intros HT [Iv Bd] L. destruct stk as [|x [|v stk]]; try len. simpl in Iv.
  pose proof (inv_slots _ _ _ _ _ _ _ _ Iv) as F. inversion F as [|? ? ? ? ? ? _ F1]; subst.
  inversion F1 as [|? ? ? ? ? ? Pv _]; subst. apply slot_ok_mtyped in Pv.
  apply mt_list_inv in Pv as (l & vs0 & Osl & -> & _ & _ & -> & _).
  destruct (inv_list_push_m sigs _ _ _ _ _ _ _ _ _ _ _ _ _ Iv Bd) as (vs & O' & Eo & Iv' & Bd').
  simpl. rewrite Eo.
  eapply (next_ok n IH); [exact HT | split; [exact Iv' | exact Bd'] | len | apply scope_ext_refl].
Qed.

Lemma w_setk_m G B C R k fs r t m md rest s s3 Σ H sc stk Sf sf Os :
  rec_mark md = true ->
  T sigs G B C R rest ((mset k m md, TRec ((k, FReq t) :: fs) r) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((m, t) :: (md, TRec fs r) :: s) ++ sf) Os ->
  length stk = length ((m, t) :: (md, TRec fs r) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WSetK k :: rest)).
Proof.
  intros Rm HT [Iv Bd] L. destruct stk as [|x [|v stk]]; try len. simpl in Iv.
  pose proof (inv_slots _ _ _ _ _ _ _ _ Iv) as F. inversion F as [|? ? Ox ? ? Os1 _ F1]; subst.
  inversion F1 as [|? ? Ol ? ? Os' Pv _]; subst.
  (* the dict as a partly new one *)
  set (f := fun k' => match md with MRec g => g k' | _ => Dp end).
  assert (Iv1 : inv sigs Σ H sc G (x :: v :: stk ++ Sf) ((m, t) :: (MRec f, TRec fs r) :: s ++ sf) (Ox :: Ol :: Os')).
  { destruct md as [| | m0 | g]; try discriminate.
    - eapply (inv_replace_slot_at sigs _ _ _ _ [x] _ _ [(m, t)] _ _ _ [Ox] _ _ eq_refl eq_refl Iv).
      apply slot_ok_mtyped. apply dtyped_rec_mtyped. exact Pv.
    - exact Iv. }
  pose proof (slot_at _ _ _ _ _ [x] _ _ [(m, t)] _ _ [Ox] _ _ eq_refl eq_refl Iv1) as Sl.
  apply slot_ok_mtyped in Sl. apply mt_rec_inv in Sl as (l & kvs0 & Osl & -> & _).
  destruct (inv_rec_set_m sigs _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ _ k Iv1 Bd) as (kvs & O' & Eo & Iv2 & Bd2).
  simpl. rewrite Eo.
  destruct md as [| | m0 | g]; try discriminate;
    (eapply (next_ok n IH); [exact HT | split; [exact Iv2 | exact Bd2] | len | apply scope_ext_refl]).
Qed.

Lemma kind_eqb_true a b : kind_eqb a b = true -> a = b.
Proof.
  destruct a, b; simpl; try congruence. intros Ek. apply ename_eqb_true in Ek. subst. reflexivity.
Qed.

Ltac kcont IH HT He Iv' :=
  eapply cont_same;
  [ eapply IH; [exact He | exact Iv' | len]
  | let Σ' := fresh in let H' := fresh in let S' := fresh in let Os'' := fresh in
    let Sx := fresh in let Iv'' := fresh in let L' := fresh in
    intros Σ' H' S' Os'' Sx Iv'' L'; eapply (next_ok _ IH); [exact HT | exact Iv'' | exact L' | exact Sx] ].

Lemma w_kind G B C R k m t t1 e1 e2 rest s s' s3 Σ H sc stk Sf sf Os :
  kind_then k t = Some t1 ->
  T sigs G B C R e1 ((m, t1) :: s) s' -> T sigs G B C R e2 ((m, kind_else k t) :: s) s' ->
  T sigs G B C R rest s' s3 ->
  INV Σ H sc G (stk ++ Sf) (((m, t) :: s) ++ sf) Os -> length stk = length ((m, t) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WKindIf k e1 e2 :: rest)).
Proof.
  intros Kt H1 H2 HT [Iv Bd] L. destruct stk as [|v stk]; try len. simpl in Iv.
  pose proof (inv_heap_ok_out sigs _ _ _ _ _ _ _ Iv) as Ho.
  pose proof (inv_len _ _ _ _ _ _ _ _ Iv) as Ln.
  destruct (inv_cons_Os _ _ _ _ _ _ _ _ _ Iv) as (O & Os' & ->).
  destruct (partial m) eqn:Pm.
  { (* a partly new list or dict: the test keeps its type, or takes the else arm *)
    pose proof (slot_at _ _ _ _ _ [] _ _ [] _ _ [] _ _ eq_refl eq_refl Iv) as Sl. apply slot_ok_mtyped in Sl.
    destruct (partial_operand sigs _ _ _ _ _ _ Sl Pm) as [(a & -> & Ek)|(fs & r & -> & Ek)];
      simpl; rewrite Ek.
    - change (kind_then k (TList a)) with (if kind_eqb k KList then Some (TList a) else Some TBot) in Kt.
      change (kind_else k (TList a)) with (if kind_eqb k KList then TBot else TList a) in H2.
      destruct (kind_eqb k KList) eqn:Kb.
      + injection Kt as <-. kcont IH HT H1 (conj Iv Bd).
      + kcont IH HT H2 (conj Iv Bd).
    - change (kind_then k (TRec fs r)) with (if kind_eqb k KDict then Some (TRec fs r) else Some TBot) in Kt.
      change (kind_else k (TRec fs r)) with (if kind_eqb k KDict then TBot else TRec fs r) in H2.
      destruct (kind_eqb k KDict) eqn:Kb.
      + injection Kt as <-. kcont IH HT H1 (conj Iv Bd).
      + kcont IH HT H2 (conj Iv Bd). }
  destruct m; try discriminate.
  - pose proof Iv as Iv0. apply inv_pop_sh in Iv as (-> & V & Hl & Iv).
    destruct (vtyped_kind_of sigs _ _ _ _ _ V Ho Ln) as (k' & Ek); [simpl in Hl; exact Hl|].
    simpl. rewrite Ek. destruct (kind_eqb k k') eqn:Kb.
    + apply kind_eqb_true in Kb; subst k'.
      assert (Iv' : INV Σ H sc G ((v :: stk) ++ Sf) (((Sh, t1) :: s) ++ sf) ([] :: Os')).
      { split; [|exact Bd]. simpl. apply inv_push_sh; [exact Iv | | exact Hl]. eapply vtyped_kind_then; eauto. }
      kcont IH HT H1 Iv'.
    + assert (Iv' : INV Σ H sc G ((v :: stk) ++ Sf) (((Sh, kind_else k t) :: s) ++ sf) ([] :: Os')).
      { split; [|exact Bd]. simpl. apply inv_push_sh; [exact Iv | | exact Hl]. eapply vtyped_kind_else; eauto. }
      kcont IH HT H2 Iv'.
  - pose proof (slot_at _ _ _ _ _ [] _ _ [] _ _ [] _ _ eq_refl eq_refl Iv) as Sl.
    unfold slot_ok in Sl; simpl in Sl.
    destruct (dtyped_kind_of sigs _ _ _ _ _ Sl) as (k' & Ek).
    simpl. rewrite Ek. destruct (kind_eqb k k') eqn:Kb.
    + apply kind_eqb_true in Kb; subst k'.
      assert (Iv' : INV Σ H sc G ((v :: stk) ++ Sf) (((Dp, t1) :: s) ++ sf) (O :: Os')).
      { split; [|exact Bd]. simpl. eapply inv_replace_top_dp; [exact Iv|]. eapply dtyped_kind_then; eauto. }
      kcont IH HT H1 Iv'.
    + assert (Iv' : INV Σ H sc G ((v :: stk) ++ Sf) (((Dp, kind_else k t) :: s) ++ sf) (O :: Os')).
      { split; [|exact Bd]. simpl. eapply inv_replace_top_dp; [exact Iv|]. eapply dtyped_kind_else; eauto. }
      kcont IH HT H2 Iv'.
Qed.

Lemma w_kind_list G B C R m t e1 e2 rest s s' s3 Σ H sc stk Sf sf Os :
  (forall a, T sigs G B C R e1 ((m, TList a) :: s) s') -> T sigs G B C R e2 ((m, t) :: s) s' ->
  T sigs G B C R rest s' s3 ->
  INV Σ H sc G (stk ++ Sf) (((m, t) :: s) ++ sf) Os -> length stk = length ((m, t) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WKindIf KList e1 e2 :: rest)).
Proof.
  intros H1 H2 HT [Iv Bd] L. destruct stk as [|v stk]; try len. simpl in Iv.
  pose proof (inv_heap_ok_out sigs _ _ _ _ _ _ _ Iv) as Ho.
  pose proof (inv_len _ _ _ _ _ _ _ _ Iv) as Ln.
  destruct (inv_cons_Os _ _ _ _ _ _ _ _ _ Iv) as (O & Os' & ->).
  destruct (partial m) eqn:Pm.
  { pose proof (slot_at _ _ _ _ _ [] _ _ [] _ _ [] _ _ eq_refl eq_refl Iv) as Sl. apply slot_ok_mtyped in Sl.
    destruct (partial_operand sigs _ _ _ _ _ _ Sl Pm) as [(a & -> & Ek)|(fs & r & -> & Ek)];
      simpl; rewrite Ek; simpl.
    - kcont IH HT (H1 a) (conj Iv Bd).
    - kcont IH HT H2 (conj Iv Bd). }
  destruct m; try discriminate.
  - pose proof Iv as Iv0. apply inv_pop_sh in Iv as (-> & V & Hl & Iv).
    destruct (vtyped_kind_of sigs _ _ _ _ _ V Ho Ln) as (k' & Ek); [simpl in Hl; exact Hl|].
    simpl. rewrite Ek.
    assert (Iv2 : INV Σ H sc G ((v :: stk) ++ Sf) (((Sh, t) :: s) ++ sf) ([] :: Os')).
    { split; [|exact Bd]. exact Iv0. }
    destruct k'; simpl; try (kcont IH HT H2 Iv2).
    destruct (vtyped_kind_list sigs _ _ _ _ _ V Ho Ln Hl Ek) as (a & Va).
    assert (Iv' : INV Σ H sc G ((v :: stk) ++ Sf) (((Sh, TList a) :: s) ++ sf) ([] :: Os')).
    { split; [|exact Bd]. simpl. apply inv_push_sh; [exact Iv | exact Va | exact Hl]. }
    kcont IH HT (H1 a) Iv'.
  - pose proof (slot_at _ _ _ _ _ [] _ _ [] _ _ [] _ _ eq_refl eq_refl Iv) as Sl.
    unfold slot_ok in Sl; simpl in Sl.
    destruct (dtyped_kind_of sigs _ _ _ _ _ Sl) as (k' & Ek).
    simpl. rewrite Ek.
    assert (Iv2 : INV Σ H sc G ((v :: stk) ++ Sf) (((Dp, t) :: s) ++ sf) (O :: Os')).
    { split; [|exact Bd]. exact Iv. }
    destruct k'; simpl; try (kcont IH HT H2 Iv2).
    destruct (dtyped_kind_list sigs _ _ _ _ _ Sl Ek) as (a & Da).
    assert (Iv' : INV Σ H sc G ((v :: stk) ++ Sf) (((Dp, TList a) :: s) ++ sf) (O :: Os')).
    { split; [|exact Bd]. simpl. eapply inv_replace_top_dp; [exact Iv | exact Da]. }
    kcont IH HT (H1 a) Iv'.
Qed.

Lemma w_try_dp G B C R t u rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest ((Dp, TMaybe u) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Dp, t) :: s) ++ sf) Os -> length stk = length ((Dp, t) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WTryAs u :: rest)).
Proof.
  intros HT I0 L. pose proof I0 as [Iv Bd]. destruct stk as [|v stk]; try len. simpl in Iv.
  destruct (inv_cons_Os _ _ _ _ _ _ _ _ _ Iv) as (O & Os' & ->).
  pose proof (slot_at _ _ _ _ _ [] _ _ [] _ _ [] _ _ eq_refl eq_refl Iv) as Sl.
  unfold slot_ok in Sl; simpl in Sl.
  simpl. destruct (vd n H v u) as [[|]|] eqn:Ev; [| | eapply err_here; [exact I0 | exact L]].
  - eapply (next_ok n IH); [exact HT | | len | apply scope_ext_refl].
    split; [|exact Bd]. simpl. eapply inv_replace_top_dp; [exact Iv|].
    apply (dt_vjust sigs Hmaybe). eapply vd_fresh; eauto.
  - destruct (inv_drop sigs _ _ _ _ _ _ _ _ _ _ Iv) as (Σ1 & Sx1 & _ & Iv1).
    eapply (next_ok n IH); [exact HT | | len | exact Sx1].
    split; [|exact Bd]. simpl. apply inv_push_dp0; [exact Iv1 | apply (dt_vnone sigs Hmaybe)].
Qed.

Lemma w_try_sh G B C R t u rest s s3 Σ H sc stk Sf sf Os :
  (sub t u \/ immutable u = true) ->
  T sigs G B C R rest ((Sh, TMaybe u) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, t) :: s) ++ sf) Os -> length stk = length ((Sh, t) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WTryAs u :: rest)).
Proof.
  intros Hu HT I0 L. pose proof I0 as [Iv Bd]. destruct stk as [|v stk]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & V & Hl & Iv).
  simpl. destruct (vd n H v u) as [[|]|] eqn:Ev; [| | eapply err_here; [exact I0 | exact L]].
  - eapply (next_ok n IH); [exact HT | | len | apply scope_ext_refl].
    split; [|exact Bd]. simpl. apply inv_push_sh; [exact Iv | | simpl; rewrite app_nil_r; exact Hl].
    apply (vt_vjust sigs Hmaybe). destruct Hu as [Hs|Hi].
    + eapply vtyped_sub; eauto.
    + eapply vd_imm; eauto.
  - eapply (next_ok n IH); [exact HT | | len | apply scope_ext_refl].
    split; [|exact Bd]. simpl. apply inv_push_sh; [exact Iv | apply (vt_vnone sigs Hmaybe) | simpl; tauto].
Qed.

(** The explicit copy: the result is a new region, so the slot is fresh. *)
Lemma w_copy G B C R t rest s s3 Σ H sc stk Sf sf Os :
  T sigs G B C R rest ((Dp, t) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, t) :: s) ++ sf) Os -> length stk = length ((Sh, t) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WCopy :: rest)).
Proof.
  intros HT I0 L. pose proof I0 as [Iv Bd]. destruct stk as [|v stk]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & V & Hl & Iv).
  pose proof (inv_heap_ok_out sigs _ _ _ _ _ _ _ Iv) as Ho.
  pose proof (inv_len _ _ _ _ _ _ _ _ Iv) as Ln.
  simpl. destruct (dcopy (length H) H v) as [[H' v']|] eqn:Ec; [|eapply err_here; [exact I0 | exact L]].
  destruct (dcopy_fresh sigs Σ H (concat Os0) (length H) v t H' v' Ln Ho V Hl Ec) as (N & O & -> & D & Rg).
  destruct (inv_alloc_region sigs _ _ _ _ _ _ _ _ _ _ _ Iv Bd D Rg) as [Iv' Bd'].
  eapply (next_ok n IH); [exact HT | split; [exact Iv' | exact Bd'] | len | apply scope_ext_app_l].
Qed.

Lemma child_ctx_inv B s B' : child_ctx B s B' -> B' = LNone \/ (B' = LChild /\ (B = LExact s \/ B = LChild)).
Proof. intros Hc; inversion Hc; subst; auto. Qed.

Lemma w_each G B C R e t rest s s3 Σ H sc stk Sf sf Os B' C' :
  child_ctx B s B' -> child_ctx C s C' ->
  T sigs G B' C' RNone e [(Sh, t)] [] ->
  T sigs G B C R rest s s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, TList t) :: s) ++ sf) Os ->
  length stk = length ((Sh, TList t) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WEach e :: rest)).
Proof.
  intros CB CC He HT [Iv Bd] L. destruct stk as [|v stk0]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & V & Hl & Iv).
  destruct (sh_list _ _ _ _ _ _ _ _ _ Iv V Hl) as (l & a & vs & -> & E & Ta & Nl & Eo & Fv & Hr).
  simpl. rewrite Eo.
  eapply cont_same with (s2 := s);
    [| intros Σ' H' S' Os' Sx Iv' L'; eapply (next_ok n IH); [exact HT | exact Iv' | exact L' | exact Sx]].
  match goal with |- res_ok _ _ _ _ _ _ _ _ _ (?GO vs H) => set (go := GO) end.
  assert (Claim : forall vsr H0 Σ0 Osq, scope_ext Σ Σ0 ->
            INV Σ0 H0 sc G (vsr ++ stk0 ++ Sf) (map (fun _ => (Sh, t)) vsr ++ s ++ sf) Osq ->
            res_ok Σ sc G B C R s Sf sf (go vsr H0)).
  { induction vsr as [|x vsr IHv]; intros H0 Σ0 Osq Sx0 Iv0.
    - unfold go. simpl. exists Σ0, s, Osq. split; [exact Sx0|]. split; [exact Iv0|]. split; [len|]. reflexivity.
    - unfold go. simpl. fold go.
      pose proof (IH G B' C' RNone e [(Sh, t)] [] He Σ0 H0 sc [x] (vsr ++ stk0 ++ Sf)
                    (map (fun _ => (Sh, t)) vsr ++ s ++ sf) Osq Iv0 eq_refl) as Hr0.
      destruct (evalv vd defs n H0 sc [x] e) as [| |He1| |o H1 S1] eqn:Ev; simpl in Hr0 |- *; auto.
      { eapply err_weaken; [exact Sx0|]. eapply (err_frame _ _ _ (vsr ++ stk0) (map (fun _ => (Sh, t)) vsr ++ s));
          [rewrite !length_app, length_map; simpl in L; lia | rewrite <- !app_assoc; exact Hr0]. }
      destruct Hr0 as (Σ1 & st1 & Os1 & Sx1 & Iv1 & L1 & O1).
      assert (Drop : exists Σ2 Os2, scope_ext Σ1 Σ2 /\ INV Σ2 H1 sc G (stk0 ++ Sf) (s ++ sf) Os2).
      { destruct Iv1 as [Iv1 Bd1].
        replace (S1 ++ vsr ++ stk0 ++ Sf) with ((S1 ++ vsr) ++ stk0 ++ Sf) in Iv1 by (symmetry; apply app_assoc).
        replace (st1 ++ map (fun _ : val => (Sh, t)) vsr ++ s ++ sf)
          with ((st1 ++ map (fun _ : val => (Sh, t)) vsr) ++ s ++ sf) in Iv1 by (symmetry; apply app_assoc).
        destruct (inv_drop_prefix sigs _ _ _ _ (S1 ++ vsr) _ (st1 ++ map (fun _ : val => (Sh, t)) vsr) _ _
                   ltac:(rewrite !length_app, length_map; lia) Iv1) as (Σ2 & Os2 & Sx2 & _ & Iv2).
        exists Σ2, Os2. split; auto. split; auto. }
      destruct o; simpl in O1.
      + subst st1. destruct S1 as [|y S1]; [|simpl in L1; lia].
        eapply IHv; [eapply scope_ext_trans; eauto | exact Iv1].
      + apply child_ctx_inv in CB as [->|[-> HB]]; [contradiction|].
        destruct Drop as (Σ2 & Os2 & Sx2 & Iv2).
        exists Σ2, s, Os2. split; [eapply scope_ext_trans; [eapply scope_ext_trans; eauto | eauto]|].
        split; [exact Iv2|]. split; [len|]. simpl. destruct HB as [->| ->]; auto.
      + apply child_ctx_inv in CC as [->|[-> HC]]; [contradiction|].
        destruct Drop as (Σ2 & Os2 & Sx2 & Iv2).
        exists Σ2, s, Os2. split; [eapply scope_ext_trans; [eapply scope_ext_trans; eauto | eauto]|].
        split; [exact Iv2|]. split; [len|]. simpl. destruct HC as [->| ->]; auto.
      + contradiction. }
  eapply (Claim vs H Σ); [apply scope_ext_refl|].
  split; [|exact Bd].
  apply inv_push_shs; [exact Iv | |].
  - rewrite Forall_forall in *. intros x Hx. eapply vtyped_sub; [apply Fv; auto | apply Ta].
  - intros x Hx l0 Hl0. apply Hr. simpl. apply in_flat_map. eauto.
Qed.
(** ** Enums *)

Lemma split_stack {A} (k : nat) (l : list A) : k <= length l ->
  exists l1 l2, l = l1 ++ l2 /\ length l1 = k /\ firstn k l = l1 /\ skipn k l = l2.
Proof.
  intros Hk. exists (firstn k l), (skipn k l). split; [symmetry; apply firstn_skipn|].
  split; [apply firstn_length_le; auto | split; reflexivity].
Qed.

Lemma w_con_sh G B C R E c pts a rest s s3 Σ H sc stk Sf sf Os :
  g_ctors sigs E c = Some pts -> wf_payload E pts ->
  T sigs G B C R rest ((Sh, TEnum E a) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) ((shs (map (subst a) pts) ++ s) ++ sf) Os ->
  length stk = length (shs (map (subst a) pts) ++ s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WCon E c pts :: rest)).
Proof.
  intros Ec W HT [Iv Bd] L.
  assert (Lk : length pts <= length stk) by (rewrite L, length_app; unfold shs; rewrite !length_map; lia).
  destruct (split_stack _ _ Lk) as (S1 & L1 & -> & L2 & F1 & F2).
  simpl. apply Nat.leb_le in Lk. rewrite Lk, F1, F2.
  rewrite <- !app_assoc in Iv.
  destruct (inv_pop_shl sigs _ _ _ _ S1 _ _ _ _ ltac:(rewrite L2, length_map; reflexivity) Iv) as (Os' & -> & Vl & Hl & Iv').
  eapply (next_ok n IH); [exact HT | | | apply scope_ext_refl].
  - split; [|exact Bd]. simpl. apply inv_push_sh; [exact Iv' | eapply vt_con; eauto | exact Hl].
  - rewrite length_app, L2 in L. unfold shs in L. rewrite !length_app, !length_map in L. simpl. lia.
Qed.

Lemma w_con_dp G B C R E c pts a rest s s3 Σ H sc stk Sf sf Os :
  g_ctors sigs E c = Some pts -> wf_payload E pts ->
  T sigs G B C R rest ((Dp, TEnum E a) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) ((marks Dp (map (subst a) pts) ++ s) ++ sf) Os ->
  length stk = length (marks Dp (map (subst a) pts) ++ s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WCon E c pts :: rest)).
Proof.
  intros Ec W HT [Iv Bd] L.
  assert (Lk : length pts <= length stk) by (rewrite L, length_app; unfold marks; rewrite !length_map; lia).
  destruct (split_stack _ _ Lk) as (S1 & L1 & -> & L2 & F1 & F2).
  simpl. apply Nat.leb_le in Lk. rewrite Lk, F1, F2.
  rewrite <- !app_assoc in Iv.
  assert (Ln : length S1 = length (marks Dp (map (subst a) pts))) by (unfold marks; rewrite !length_map; auto).
  pose proof (Forall3_length _ _ _ _ (inv_slots _ _ _ _ _ _ _ _ Iv)) as [_ LO].
  rewrite length_app in LO.
  destruct (Forall3_app_inv _ _ _ _ _ _ (inv_slots _ _ _ _ _ _ _ _ Iv) Ln) as (Os1 & Os2 & -> & F3 & _).
  pose proof (Forall3_length _ _ _ _ F3) as [_ L3].
  eapply (next_ok n IH); [exact HT | | | apply scope_ext_refl].
  - split; [|exact Bd]. simpl.
    eapply inv_merge_top with (ts := map (subst a) pts); [ | | exact Iv | reflexivity | ].
    + rewrite L2, length_map; reflexivity.
    + unfold marks in L3; rewrite length_map in L3; exact L3.
    + intros Dl N. eapply dt_con; eauto.
  - rewrite length_app, L2 in L. unfold marks in L. rewrite !length_app, !length_map in L. simpl. lia.
Qed.

Lemma w_case G B C R m E a arms rest s s' s3 Σ H sc stk Sf sf Os :
  (forall c pts e, g_ctors sigs E c = Some pts -> lookup c arms = Some e ->
     T sigs G B C R e (marks m (map (subst a) pts) ++ s) s') ->
  T sigs G B C R rest s' s3 ->
  INV Σ H sc G (stk ++ Sf) (((m, TEnum E a) :: s) ++ sf) Os ->
  length stk = length ((m, TEnum E a) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WCase E arms :: rest)).
Proof.
  intros Harms HT I0 L. pose proof I0 as [Iv Bd]. destruct stk as [|v stk]; try len. simpl in Iv.
  destruct (inv_cons_Os _ _ _ _ _ _ _ _ _ Iv) as (O & Os' & ->).
  (* the value is a constructor of E, with payloads typed by its declaration *)
  assert (Hc : exists c pts vs Os1, v = VCon E c pts vs /\ g_ctors sigs E c = Some pts /\
     INV Σ H sc G ((vs ++ stk) ++ Sf) ((marks m (map (subst a) pts) ++ s) ++ sf) (Os1 ++ Os') /\
     length vs = length (map (subst a) pts)).
  { destruct (partial m) eqn:Pm.
    { exfalso. pose proof (slot_at _ _ _ _ _ [] _ _ [] _ _ [] _ _ eq_refl eq_refl Iv) as Sl.
      apply slot_ok_mtyped in Sl.
      destruct (partial_operand sigs _ _ _ _ _ _ Sl Pm) as [(? & E' & _)|(? & ? & E' & _)]; discriminate. }
    destruct m; try discriminate.
    - apply inv_pop_sh in Iv as (-> & V & Hl & Iv).
      apply vt_enum_inv in V as (c & pts & vs & -> & Ec & W & Vl).
      exists c, pts, vs, (map (fun _ => []) vs). split; [reflexivity|]. split; [exact Ec|]. split.
      + split; [|exact Bd]. rewrite <- !app_assoc. apply inv_push_shl; auto.
      + apply vtypedl_length in Vl. exact Vl.
    - pose proof (slot_at _ _ _ _ _ [] _ _ [] _ _ [] _ _ eq_refl eq_refl Iv) as Sl.
      unfold slot_ok in Sl; simpl in Sl.
      apply dt_enum_inv in Sl as (c & pts & vs & Os1 & -> & Ec & W & Dl & -> & N).
      exists c, pts, vs, Os1. split; [reflexivity|]. split; [exact Ec|]. split.
      + split; [|exact Bd]. rewrite <- !app_assoc. eapply inv_split_top; eauto.
      + clear -Dl. induction Dl; simpl; auto. }
  destruct Hc as (c & pts & vs & Os1 & -> & Ec & Iv' & Lv).
  simpl. rewrite ename_eqb_refl.
  destruct (@lookup (list word) c arms) as [e|] eqn:Ea; [|eapply err_here; [exact I0 | exact L]].
  apply cont_same with (s2 := s').
  - eapply (IH _ _ _ _ _ _ _ (Harms c pts e Ec Ea)); [exact Iv' |].
    rewrite !length_app, Lv. unfold marks. rewrite !length_map. len.
  - intros Σ' H' S' Os'' Sx Iv'' L'. eapply (next_ok n IH); [exact HT | exact Iv'' | exact L' | exact Sx].
Qed.

Lemma w_kind_enum G B C R m E t e1 e2 rest s s' s3 Σ H sc stk Sf sf Os :
  (forall a, length a = length (en_params E) -> T sigs G B C R e1 ((m, TEnum E a) :: s) s') ->
  T sigs G B C R e2 ((m, t) :: s) s' ->
  T sigs G B C R rest s' s3 ->
  INV Σ H sc G (stk ++ Sf) (((m, t) :: s) ++ sf) Os -> length stk = length ((m, t) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WKindIf (KEnum E) e1 e2 :: rest)).
Proof.
  intros H1 H2 HT [Iv Bd] L. destruct stk as [|v stk]; try len. simpl in Iv.
  pose proof (inv_heap_ok_out sigs _ _ _ _ _ _ _ Iv) as Ho.
  pose proof (inv_len _ _ _ _ _ _ _ _ Iv) as Ln.
  destruct (inv_cons_Os _ _ _ _ _ _ _ _ _ Iv) as (O & Os' & ->).
  destruct (partial m) eqn:Pm.
  { pose proof (slot_at _ _ _ _ _ [] _ _ [] _ _ [] _ _ eq_refl eq_refl Iv) as Sl. apply slot_ok_mtyped in Sl.
    destruct (partial_operand sigs _ _ _ _ _ _ Sl Pm) as [(a & -> & Ek)|(fs & r & -> & Ek)];
      simpl; rewrite Ek; simpl; kcont IH HT H2 (conj Iv Bd). }
  destruct m; try discriminate.
  - pose proof Iv as Iv0. apply inv_pop_sh in Iv as (-> & V & Hl & Iv).
    destruct (vtyped_kind_of sigs _ _ _ _ _ V Ho Ln) as (k' & Ek); [simpl in Hl; exact Hl|].
    simpl. rewrite Ek.
    assert (Iv2 : INV Σ H sc G ((v :: stk) ++ Sf) (((Sh, t) :: s) ++ sf) ([] :: Os')).
    { split; [|exact Bd]. exact Iv0. }
    destruct k'; simpl; try (kcont IH HT H2 Iv2).
    destruct (ename_eqb E E0) eqn:Kb; [|kcont IH HT H2 Iv2].
    apply ename_eqb_true in Kb; subst E0.
    destruct (vtyped_kind_enum sigs _ _ _ _ _ _ V Ho Ln Hl Ek) as (a & La & Va).
    assert (Iv' : INV Σ H sc G ((v :: stk) ++ Sf) (((Sh, TEnum E a) :: s) ++ sf) ([] :: Os')).
    { split; [|exact Bd]. simpl. apply inv_push_sh; [exact Iv | exact Va | exact Hl]. }
    kcont IH HT (H1 a La) Iv'.
  - pose proof (slot_at _ _ _ _ _ [] _ _ [] _ _ [] _ _ eq_refl eq_refl Iv) as Sl.
    unfold slot_ok in Sl; simpl in Sl.
    destruct (dtyped_kind_of sigs _ _ _ _ _ Sl) as (k' & Ek).
    simpl. rewrite Ek.
    assert (Iv2 : INV Σ H sc G ((v :: stk) ++ Sf) (((Dp, t) :: s) ++ sf) (O :: Os')).
    { split; [|exact Bd]. exact Iv. }
    destruct k'; simpl; try (kcont IH HT H2 Iv2).
    destruct (ename_eqb E E0) eqn:Kb; [|kcont IH HT H2 Iv2].
    apply ename_eqb_true in Kb; subst E0.
    destruct (dtyped_kind_enum sigs _ _ _ _ _ _ Sl Ek) as (a & La & Da).
    assert (Iv' : INV Σ H sc G ((v :: stk) ++ Sf) (((Dp, TEnum E a) :: s) ++ sf) (O :: Os')).
    { split; [|exact Bd]. simpl. eapply inv_replace_top_dp; [exact Iv | exact Da]. }
    kcont IH HT (H1 a La) Iv'.
Qed.
(** ** [map] *)

Lemma map_shs_const {A} (l : list A) u : map (fun _ => (Sh, u)) l = shs (map (fun _ => u) l).
Proof. unfold shs. rewrite map_map. reflexivity. Qed.

Lemma vtypedl_const Σ vs u : vtypedl sigs Σ vs (map (fun _ => u) vs) -> Forall (fun v => vtyped sigs Σ v u) vs.
Proof. induction vs; simpl; intros H; inversion H; subst; constructor; auto. Qed.

(** [m] is the result's mark: [Sh], or [Dp] when [u] is immutable. *)
Lemma w_map G B C R m e t u rest s s3 Σ H sc stk Sf sf Os B' C' :
  (m = Sh \/ (m = Dp /\ immutable u = true)) ->
  child_ctx B s B' -> child_ctx C s C' ->
  T sigs G B' C' RNone e [(Sh, t)] [(Sh, u)] ->
  T sigs G B C R rest ((m, TList u) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) (((Sh, TList t) :: s) ++ sf) Os ->
  length stk = length ((Sh, TList t) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (WMap e :: rest)).
Proof.
  intros Hm CB CC He HT [Iv Bd] L. destruct stk as [|v stk0]; try len. simpl in Iv.
  pop_sh Iv. destruct Iv as (-> & V & Hl & Iv).
  destruct (sh_list _ _ _ _ _ _ _ _ _ Iv V Hl) as (l & a & vs & -> & E & Ta & Nl & Eo & Fv & Hr).
  simpl. rewrite Eo.
  eapply cont_same with (s2 := (m, TList u) :: s);
    [| intros Σ' H' S' Os' Sx Iv' L'; eapply (next_ok n IH); [exact HT | exact Iv' | exact L' | exact Sx]].
  match goal with |- res_ok _ _ _ _ _ _ _ _ _ (?GO vs H []) => set (go := GO) end.
  assert (Claim : forall vsr acc H0 Σ0 Osq, scope_ext Σ Σ0 ->
            INV Σ0 H0 sc G (vsr ++ acc ++ stk0 ++ Sf)
                (map (fun _ => (Sh, t)) vsr ++ map (fun _ => (Sh, u)) acc ++ s ++ sf) Osq ->
            res_ok Σ sc G B C R ((m, TList u) :: s) Sf sf (go vsr H0 acc)).
  { induction vsr as [|x vsr IHv]; intros acc H0 Σ0 Osq Sx0 [Iv0 Bd0].
    - (* all elements done: allocate the result list *)
      unfold go. simpl. simpl in Iv0. rewrite (map_shs_const acc u) in Iv0.
      destruct (inv_pop_shl sigs _ _ _ _ acc _ _ _ _ ltac:(rewrite length_map; reflexivity) Iv0)
        as (Os1 & -> & Vl & Hla & Iv1).
      apply vtypedl_const in Vl.
      pose proof (inv_len _ _ _ _ _ _ _ _ Iv1) as Ln.
      assert (Fr : Forall (fun w => vtyped sigs Σ0 w u) (rev acc)) by (apply Forall_rev; auto).
      assert (Hlr : forall r, In r (olocs (OList (rev acc))) -> ~ In r (concat Os1)).
      { intros r Hr'. simpl in Hr'. apply in_flat_map in Hr' as (w & Hw & Hr').
        apply in_rev in Hw. apply Hla. apply in_flat_map. eauto. }
      assert (Hlt : forall r, In r (olocs (OList (rev acc))) -> r < length H0).
      { intros r Hr'. simpl in Hr'. apply in_flat_map in Hr' as (w & Hw & Hr').
        apply in_rev in Hw. rewrite <- Ln. eapply vtyped_vlocs_lt; eauto. rewrite Forall_forall in Vl. auto. }
      destruct m; [| | exfalso; destruct Hm as [Em|[Em _]]; discriminate ..].
      + exists (Σ0 ++ [HList u]), ((Sh, TList u) :: s), ([] :: Os1).
        split; [eapply scope_ext_trans; [exact Sx0 | apply scope_ext_app] |].
        split; [| split; [len | reflexivity]].
        split.
        * simpl. apply inv_push_sh.
          -- apply inv_alloc_out; auto. simpl. rewrite Forall_forall in *. intros w Hw.
             eapply vtyped_ext; [apply Fr; auto | apply sagree_app | apply scope_ext_app].
          -- eapply vt_list; [rewrite nth_error_app2 by lia; rewrite Ln, Nat.sub_diag; reflexivity | apply s_refl].
          -- simpl. intros r [<-|[]] Hc. pose proof (inv_reg_lt sigs _ _ _ _ _ _ _ _ Iv1 Hc). lia.
        * apply bounded_app; auto. intros r Hr'. specialize (Hlt r Hr'). lia.
      + assert (Hi : immutable u = true) by (destruct Hm as [Em|[_ Em]]; [discriminate | exact Em]).
        exists (Σ0 ++ [HDead]), ((Dp, TList u) :: s), ([length H0] :: Os1).
        split; [eapply scope_ext_trans; [exact Sx0 | apply scope_ext_app] |].
        split; [| split; [len | reflexivity]].
        assert (Nl0 : olocs (OList (rev acc)) = []).
        { simpl. apply flat_map_nil_iff. intros w Hw. rewrite Forall_forall in Fr.
          eapply vtyped_imm_vlocs; eauto. }
        split.
        * simpl. eapply inv_alloc_dp; [exact Iv1 | exact Bd0 | exact Nl0 |].
          replace [length H0] with (length H0 :: concat (map (fun _ => @nil loc) (rev acc)))
            by (rewrite concat_map_nil; reflexivity).
          apply dt_list with (vs := rev acc) (Os := map (fun _ => []) (rev acc)).
          -- apply nth_error_app_eq.
          -- remember (H0 ++ [OList (rev acc)]) as Hx. clear HeqHx. clear -Fr Hi.
             induction Fr; simpl; constructor; auto.
             apply vtyped_imm_dtyped; auto. eapply vtyped_ext; [eauto | apply sagree_app | apply scope_ext_app].
          -- rewrite concat_map_nil. constructor; [simpl; tauto | constructor].
        * apply bounded_app; auto. rewrite Nl0. simpl; tauto.
    - (* run the body on the next element *)
      unfold go. simpl. fold go.
      pose proof (IH G B' C' RNone e [(Sh, t)] [(Sh, u)] He Σ0 H0 sc [x] (vsr ++ acc ++ stk0 ++ Sf)
                    (map (fun _ => (Sh, t)) vsr ++ map (fun _ => (Sh, u)) acc ++ s ++ sf) Osq
                    (conj Iv0 Bd0) eq_refl) as Hr0.
      destruct (evalv vd defs n H0 sc [x] e) as [| |He1| |o H1 S1] eqn:Ev; simpl in Hr0 |- *; auto.
      { eapply err_weaken; [exact Sx0|]. eapply (err_frame _ _ _ (vsr ++ acc ++ stk0) (map (fun _ => (Sh, t)) vsr ++ map (fun _ => (Sh, u)) acc ++ s));
          [rewrite !length_app, !length_map; simpl in L; lia | rewrite <- !app_assoc; exact Hr0]. }
      destruct Hr0 as (Σ1 & st1 & Os1 & Sx1 & Iv1 & L1 & O1).
      assert (Drop : exists Σ2 Os2, scope_ext Σ1 Σ2 /\ INV Σ2 H1 sc G (stk0 ++ Sf) (s ++ sf) Os2).
      { destruct Iv1 as [Iv1 Bd1].
        replace (S1 ++ vsr ++ acc ++ stk0 ++ Sf) with ((S1 ++ vsr ++ acc) ++ stk0 ++ Sf) in Iv1 by (rewrite !app_assoc; reflexivity).
        replace (st1 ++ map (fun _ : val => (Sh, t)) vsr ++ map (fun _ : val => (Sh, u)) acc ++ s ++ sf)
          with ((st1 ++ map (fun _ : val => (Sh, t)) vsr ++ map (fun _ : val => (Sh, u)) acc) ++ s ++ sf)
          in Iv1 by (rewrite !app_assoc; reflexivity).
        destruct (inv_drop_prefix sigs _ _ _ _ (S1 ++ vsr ++ acc) _
                   (st1 ++ map (fun _ : val => (Sh, t)) vsr ++ map (fun _ : val => (Sh, u)) acc) _ _
                   ltac:(rewrite !length_app, !length_map; lia) Iv1) as (Σ2 & Os2 & Sx2 & _ & Iv2).
        exists Σ2, Os2. split; auto. split; auto. }
      destruct o; simpl in O1.
      + subst st1. destruct S1 as [|y [|y' S1]]; simpl in L1; try lia.
        destruct Iv1 as [Iv1 Bd1].
        destruct (inv_cons_Os _ _ _ _ _ _ _ _ _ Iv1) as (Oy & Os1' & ->).
        pose proof (Forall3_length _ _ _ _ (inv_slots _ _ _ _ _ _ _ _ Iv1)) as [_ LO].
        simpl in LO. rewrite !length_app, !length_map in LO.
        assert (Hsp : exists Osv Osr, Os1' = Osv ++ Osr /\ length Osv = length vsr).
        { exists (firstn (length vsr) Os1'), (skipn (length vsr) Os1'). split; [symmetry; apply firstn_skipn|].
          apply firstn_length_le. lia. }
        destruct Hsp as (Osv & Osr & -> & LOv).
        eapply (IHv (y :: acc) H1 Σ1 (Osv ++ Oy :: Osr)); [eapply scope_ext_trans; eauto |].
        split; [| exact Bd1]. simpl.
        apply inv_move_top; [rewrite length_map; reflexivity | rewrite length_map; auto | exact Iv1].
      + apply child_ctx_inv in CB as [->|[-> HB]]; [contradiction|].
        destruct Drop as (Σ2 & Os2 & Sx2 & Iv2).
        exists Σ2, s, Os2. split; [eapply scope_ext_trans; [eapply scope_ext_trans; eauto | eauto]|].
        split; [exact Iv2|]. split; [len|]. simpl. destruct HB as [->| ->]; auto.
      + apply child_ctx_inv in CC as [->|[-> HC]]; [contradiction|].
        destruct Drop as (Σ2 & Os2 & Sx2 & Iv2).
        exists Σ2, s, Os2. split; [eapply scope_ext_trans; [eapply scope_ext_trans; eauto | eauto]|].
        split; [exact Iv2|]. split; [len|]. simpl. destruct HC as [->| ->]; auto.
      + contradiction. }
  eapply (Claim vs [] H Σ); [apply scope_ext_refl|].
  split; [|exact Bd]. simpl.
  apply inv_push_shs; [exact Iv | |].
  - rewrite Forall_forall in *. intros x Hx. eapply vtyped_sub; [apply Fv; auto | apply Ta].
  - intros x Hx l0 Hl0. apply Hr. simpl. apply in_flat_map. eauto.
Qed.
(** ** [take], [skip] and slices *)

(** A list operand, shared or fresh: a location holding a list. *)
Lemma list_operand Σ H sc G v L m t st Os :
  inv sigs Σ H sc G (v :: L) ((m, TList t) :: st) Os ->
  exists l vs, v = VLoc l /\ nth_error H l = Some (OList vs).
Proof.
  intros I. destruct (inv_cons_Os _ _ _ _ _ _ _ _ _ I) as (O & Os' & ->).
  destruct (partial m) eqn:Pm.
  { pose proof (slot_at _ _ _ _ _ [] _ _ [] _ _ [] _ _ eq_refl eq_refl I) as Sl. apply slot_ok_mtyped in Sl.
    destruct (mtyped_partial sigs _ _ _ _ _ _ Sl Pm) as
      (l & -> & _ & [(? & ? & vs & _ & _ & E)|(? & ? & ? & ? & _ & E' & _)]); [eauto | discriminate]. }
  destruct m; try discriminate.
  - apply inv_pop_sh in I as (_ & V & Hl & I').
    destruct (sh_list _ _ _ _ _ _ _ _ _ I' V Hl) as (l & a & vs & -> & _ & _ & _ & Eo & _). eauto.
  - pose proof (slot_at _ _ _ _ _ [] _ _ [] _ _ [] _ _ eq_refl eq_refl I) as Sl.
    unfold slot_ok in Sl; simpl in Sl.
    apply dt_list_inv in Sl as (l & vs & Os1 & -> & Eo & _). eauto.
Qed.

(** Pushing a new list over the run [mid] of the operand's elements, with
    mark [m']: fresh when the operand was fresh or the elements are
    immutable. *)
Lemma w_newlist0 G B C R m m' t rest s s3 Σ H sc l stk0 Sf sf Os vs pre mid post :
  partial m = false ->
  (m' = Sh \/ (m' = Dp /\ (m = Dp \/ immutable t = true))) ->
  T sigs G B C R rest ((m', TList t) :: s) s3 ->
  INV Σ H sc G (VLoc l :: stk0 ++ Sf) ((m, TList t) :: s ++ sf) Os ->
  length stk0 = length s ->
  nth_error H l = Some (OList vs) -> vs = pre ++ mid ++ post ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs n (H ++ [OList mid]) sc (VLoc (length H) :: stk0) rest).
Proof.
  intros Pm Hm HT [Iv Bd] L El Evs. subst vs.
  destruct (inv_cons_Os _ _ _ _ _ _ _ _ _ Iv) as (O & Os' & ->).
  destruct m; try discriminate.
  - (* a shared operand: its elements are typed by the store *)
    apply inv_pop_sh in Iv as (-> & V & Hl & Iv).
    destruct (sh_list _ _ _ _ _ _ _ _ _ Iv V Hl) as (l' & a & vs & E & Ea & Ta & Nl & Eo & Fv & Hr).
    injection E as <-. rewrite El in Eo. injection Eo as <-.
    pose proof (inv_len _ _ _ _ _ _ _ _ Iv) as Ln.
    assert (Hin : forall r, In r (olocs (OList mid)) -> In r (olocs (OList (pre ++ mid ++ post)))).
    { simpl. intros r. rewrite !flat_map_app, !in_app_iff. tauto. }
    assert (Fm : Forall (fun w => vtyped sigs Σ w t) mid).
    { rewrite Forall_forall in *. intros w Hw. eapply vtyped_sub; [apply Fv | apply (proj1 Ta)].
      rewrite !in_app_iff. auto. }
    assert (Hlt : forall r, In r (olocs (OList mid)) -> r < length H) by (intros r Hr'; eapply Bd; eauto).
    destruct m'; [| | exfalso; destruct Hm as [Em|[Em _]]; discriminate ..].
    + (* shared result *)
      eapply (next_ok n IH) with (Σ' := Σ ++ [HList t]); [exact HT | | len | apply scope_ext_app].
      split.
      * simpl. apply inv_push_sh.
        -- apply inv_alloc_out; [exact Iv | | intros r Hr'; apply Hr; auto].
           simpl. rewrite Forall_forall in Fm |- *. intros w Hw.
           eapply vtyped_ext; [apply Fm; auto | apply sagree_app | apply scope_ext_app].
        -- eapply vt_list; [rewrite nth_error_app2 by lia; rewrite Ln, Nat.sub_diag; reflexivity | apply s_refl].
        -- simpl. intros r [<-|[]] Hc. pose proof (inv_reg_lt sigs _ _ _ _ _ _ _ _ Iv Hc). lia.
      * apply bounded_app; auto. intros r Hr'. specialize (Hlt r Hr'). lia.
    + (* fresh result: the elements are immutable *)
      assert (Hi : immutable t = true) by (destruct Hm as [E|[_ [E|E]]]; [discriminate | discriminate | exact E]).
      assert (Nl0 : olocs (OList mid) = []).
      { simpl. apply flat_map_nil_iff. intros w Hw. rewrite Forall_forall in Fm. eapply vtyped_imm_vlocs; eauto. }
      eapply (next_ok n IH) with (Σ' := Σ ++ [HDead]); [exact HT | | len | apply scope_ext_app].
      split.
      * simpl. eapply inv_alloc_dp; [exact Iv | exact Bd | exact Nl0 |].
        replace [length H] with (length H :: concat (map (fun _ => @nil loc) mid))
          by (rewrite concat_map_nil; reflexivity).
        apply dt_list with (vs := mid) (Os := map (fun _ => []) mid).
        -- apply nth_error_app_eq.
        -- remember (H ++ [OList mid]) as Hx. clear HeqHx. clear -Fm Hi.
           induction Fm; simpl; constructor; auto.
           apply vtyped_imm_dtyped; auto. eapply vtyped_ext; [eauto | apply sagree_app | apply scope_ext_app].
        -- rewrite concat_map_nil. constructor; [simpl; tauto | constructor].
      * apply bounded_app; auto. rewrite Nl0. simpl; tauto.
  - (* a fresh operand: the kept elements move to the new list; the rest dies *)
    destruct (inv_slice_dp sigs Σ H sc G l _ t _ O Os' pre mid post Iv Bd El) as (Σ1 & O1 & Sx1 & Iv1 & Bd1).
    destruct m'; [| | exfalso; destruct Hm as [Em|[Em _]]; discriminate ..].
    + destruct (inv_commit_top sigs _ _ _ _ _ _ _ _ _ _ Iv1) as (Σ2 & Sx2 & _ & Iv2).
      eapply (next_ok n IH); [exact HT | split; [exact Iv2 | exact Bd1] | len | eapply scope_ext_trans; eauto].
    + eapply (next_ok n IH); [exact HT | split; [exact Iv1 | exact Bd1] | len | exact Sx1].
Qed.

(** A partly new operand is committed first: then it is a stored list. *)
Lemma w_newlist G B C R m m' t rest s s3 Σ H sc l stk0 Sf sf Os vs pre mid post :
  (m' = Sh \/ (m' = Dp /\ (m = Dp \/ immutable t = true))) ->
  T sigs G B C R rest ((m', TList t) :: s) s3 ->
  INV Σ H sc G (VLoc l :: stk0 ++ Sf) ((m, TList t) :: s ++ sf) Os ->
  length stk0 = length s ->
  nth_error H l = Some (OList vs) -> vs = pre ++ mid ++ post ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs n (H ++ [OList mid]) sc (VLoc (length H) :: stk0) rest).
Proof.
  intros Hm HT Iv L El Evs. destruct (partial m) eqn:Pm.
  - destruct Iv as [Iv Bd].
    destruct (inv_cons_Os _ _ _ _ _ _ _ _ _ Iv) as (O & Os' & ->).
    destruct (inv_commit_at_m sigs _ _ _ _ [] _ _ [] _ _ _ [] _ _ eq_refl eq_refl Iv) as (Σ1 & Sx1 & _ & Iv1).
    eapply res_ok_weaken; [exact Sx1|].
    eapply (w_newlist0 G B C R Sh m'); [reflexivity | | exact HT | split; [exact Iv1 | exact Bd] | exact L | exact El | exact Evs].
    destruct Hm as [E|[E [E'|E']]]; [left; exact E | subst m; discriminate | right; split; [exact E | right; exact E']].
  - eapply w_newlist0; eauto.
Qed.

Lemma w_slice G B C R w a m m' t rest s s3 Σ H sc stk Sf sf Os :
  slice_args w a -> (m' = Sh \/ (m' = Dp /\ (m = Dp \/ immutable t = true))) ->
  T sigs G B C R rest ((m', TList t) :: s) s3 ->
  INV Σ H sc G (stk ++ Sf) ((a ++ (m, TList t) :: s) ++ sf) Os ->
  length stk = length (a ++ (m, TList t) :: s) ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (w :: rest)).
Proof.
  intros Sa Hm HT Iv L. destruct Sa as [| | i b]; simpl in Iv, L.
  - (* take *)
    destruct stk as [|x [|y stk0]]; try len. simpl in Iv. destruct Iv as [Iv Bd].
    pop_sh Iv. destruct Iv as (-> & V & _ & Iv). apply vt_int_inv in V as (k & ->).
    destruct (list_operand _ _ _ _ _ _ _ _ _ _ Iv) as (l & vs & -> & El).
    simpl. rewrite El.
    eapply w_newlist with (pre := []) (post := skipn k vs);
      [exact Hm | exact HT | split; [exact Iv | exact Bd] | len | exact El |].
    simpl. symmetry. apply firstn_skipn.
  - (* skip *)
    destruct stk as [|x [|y stk0]]; try len. simpl in Iv. destruct Iv as [Iv Bd].
    pop_sh Iv. destruct Iv as (-> & V & _ & Iv). apply vt_int_inv in V as (k & ->).
    destruct (list_operand _ _ _ _ _ _ _ _ _ _ Iv) as (l & vs & -> & El).
    simpl. rewrite El.
    eapply w_newlist with (pre := firstn k vs) (post := []);
      [exact Hm | exact HT | split; [exact Iv | exact Bd] | len | exact El |].
    rewrite app_nil_r. symmetry. apply firstn_skipn.
  - (* index slice *)
    destruct stk as [|y stk0]; try len. simpl in Iv. pose proof Iv as I0. destruct Iv as [Iv Bd].
    destruct (list_operand _ _ _ _ _ _ _ _ _ _ Iv) as (l & vs & -> & El).
    simpl. rewrite El.
    remember (match b with Some e => e | None => length vs end) as e eqn:Ee.
    destruct ((i <=? e) && (e <=? length vs));
      [|apply (err_here _ _ _ _ _ _ _ _ _ _ (VLoc l :: stk0) ((m, TList t) :: s) Os); [exact I0 | exact L]].
    eapply w_newlist with (pre := firstn i vs) (post := skipn (e - i) (skipn i vs));
      [exact Hm | exact HT | split; [exact Iv | exact Bd] | len | exact El |].
    rewrite firstn_skipn. symmetry. apply firstn_skipn.
Qed.
End Words.

Lemma word_ok n (IH : P n) G B C R w s1 s2 rest s3 Σ H sc stk Sf sf Os :
  TW sigs G B C R w s1 s2 -> T sigs G B C R rest s2 s3 ->
  INV Σ H sc G (stk ++ Sf) (s1 ++ sf) Os -> length stk = length s1 ->
  res_ok Σ sc G B C R s3 Sf sf (evalv vd defs (S n) H sc stk (w :: rest)).
Proof.
  intros Htw HT Iv L. destruct Htw.
  - eapply (w_push_sh n IH) with (t := TInt); [reflexivity | apply vt_int | reflexivity | exact HT | exact Iv | exact L].
  - eapply (w_push_sh n IH) with (t := TStr); [reflexivity | apply vt_str | reflexivity | exact HT | exact Iv | exact L].
  - eapply (w_push_sh n IH) with (t := TBool); [reflexivity | apply vt_bool | reflexivity | exact HT | exact Iv | exact L].
  - eapply w_add; eauto.
  - eapply w_cat; eauto.
  - eapply w_dup; eauto.
  - eapply w_drop; eauto.
  - eapply w_swap; eauto.
  - eapply w_load; eauto.
  - eapply w_store; eauto.
  - eapply w_quote; eauto. exact H0.
  - eapply w_quote; eauto. exact H0.
  - eapply w_exec; eauto.
  - eapply w_exec_never; eauto.
  - eapply w_if; eauto.
  - eapply w_loop; eauto.
  - eapply w_loop_forever; eauto.
  - eapply w_break_exact; eauto.
  - eapply w_break_child; eauto.
  - eapply w_cont_exact; eauto.
  - eapply w_cont_child; eauto.
  - eapply w_return; eauto.
  - eapply w_return_any; eauto.
  - eapply w_exit; eauto.
  - eapply w_call; eauto.
  - eapply w_call_never; eauto.
  - eapply w_nil; eauto.
  - eapply w_nil_m; eauto.
  - eapply w_push_m; eauto.
  - eapply w_push_sh_list; eauto.
  - eapply w_push_dp_list; eauto.
  - eapply w_getat; eauto.
  - eapply w_setat; eauto.
  - eapply w_each; eauto.
  - eapply w_map; eauto.
  - eapply w_map; eauto.
  - eapply w_slice; eauto.
  - eapply w_dictnew; eauto.
  - eapply w_getk; eauto.
  - eapply w_getreq; eauto.
  - eapply w_setk_sh; eauto.
  - eapply w_setk_dp; eauto.
  - eapply w_setk_m; eauto.
  - eapply w_del_sh; eauto.
  - eapply w_del_dp; eauto.
  - eapply w_getd; eauto.
  - eapply w_setd; eauto.
  - eapply w_kind; eauto.
  - eapply w_kind_list; eauto.
  - eapply w_try_dp; eauto.
  - eapply w_try_sh; eauto.
  - eapply w_try_sh; eauto.
  - eapply w_copy; eauto.
  - eapply w_con_sh; eauto.
  - eapply w_con_dp; eauto.
  - eapply w_case; eauto.
  - eapply w_kind_enum; eauto.
Qed.

(** Running [w] and then [e]: when [w] alone does not finish normally, [e]
    is never reached. *)
Lemma eval_cons_div : forall n H sc st w e,
  (forall H' st', evalv vd defs n H sc st [w] <> ROk ONormal H' st') ->
  evalv vd defs n H sc st (w :: e) = evalv vd defs n H sc st [w].
Proof.
  induction n as [|n IH]; intros H sc st w e; [reflexivity|].
  destruct w; simpl;
    repeat match goal with
           | |- context [match ?x with _ => _ end] => destruct x
           end;
    intros Hn; auto;
    try (destruct n; [reflexivity | exfalso; eapply Hn; reflexivity]).
  all: apply IH; auto.
Qed.

Lemma lframe_nil L : lframe L [] = L.
Proof. destruct L; simpl; rewrite ?app_nil_r; reflexivity. Qed.

Lemma rframe_nil R : rframe R [] = R.
Proof. destruct R; simpl; rewrite ?app_nil_r; reflexivity. Qed.

Theorem eval_sound : forall n, P n.
Proof.
  induction n as [|n IHn].
  - intros G B C R e s1 s2 HT Σ H sc stk Sf sf Os Iv L. simpl. exact Logic.I.
  - intros G B C R e s1 s2 HT.
    induction HT as [B C R s | B C R w e s1 s2 s3 Hw He IHe | B C R e s1 s1' s2 s2' Hs1 He IHe Hs2
                    | B C R w e s1 s3 Hd IHd];
      intros Σ H sc stk Sf sf Os Iv L.
    + simpl. exists Σ, s, Os. split; [apply scope_ext_refl|]. split; [exact Iv|]. split; [exact L | reflexivity].
    + eapply word_ok; eauto.
    + destruct Iv as [Iv Bd].
      destruct (inv_ssub sigs Σ H sc G stk Sf s1' s1 sf Os Hs1 L Iv) as (Σ1 & Os1 & Sx1 & _ & Iv1).
      assert (L1 : length stk = length s1) by (rewrite L; eapply Forall2_length; eauto).
      pose proof (IHe Σ1 H sc stk Sf sf Os1 (conj Iv1 Bd) L1) as Hr.
      eapply res_ok_weaken; [exact Sx1|].
      destruct (evalv vd defs (S n) H sc stk e) as [| | | |o H' S']; simpl in *; auto.
      destruct Hr as (Σ2 & st' & Os2 & Sx2 & [Iv2 Bd2] & L2 & O2).
      destruct o; simpl in O2.
      * subst st'.
        destruct (inv_ssub sigs Σ2 H' sc G S' Sf s2 s2' sf Os2 Hs2 L2 Iv2) as (Σ3 & Os3 & Sx3 & _ & Iv3).
        exists Σ3, s2', Os3. split; [eapply scope_ext_trans; eauto|]. split; [split; auto|].
        split; [rewrite L2; eapply Forall2_length; eauto | reflexivity].
      * exists Σ2, st', Os2. split; [auto|]. split; [split; auto|]. split; auto.
      * exists Σ2, st', Os2. split; [auto|]. split; [split; auto|]. split; auto.
      * exists Σ2, st', Os2. split; [auto|]. split; [split; auto|]. split; auto.
    + (* dead code after a diverging word *)
      pose proof (IHd [] [(Sh, TBot)] Σ H sc stk Sf sf Os) as Hr.
      rewrite app_nil_r, lframe_nil, lframe_nil, rframe_nil in Hr. specialize (Hr Iv L).
      assert (Nn : forall H' st', evalv vd defs (S n) H sc stk [w] <> ROk ONormal H' st').
      { intros H' st' E. rewrite E in Hr. destruct Hr as (Σ' & st2 & Os' & _ & Iv' & L' & O).
        simpl in O. subst. destruct st' as [|v st']; [simpl in L'; lia|]. eapply slot_bot_false; eauto. }
      rewrite (eval_cons_div _ _ _ _ _ _ Nn).
      destruct (evalv vd defs (S n) H sc stk [w]) as [| | | |o H' S'] eqn:Ev; simpl in *; auto.
      destruct o; [exfalso; eapply Nn; reflexivity | exact Hr | exact Hr | exact Hr].
Qed.

(** The initial state: an empty stack and one empty top-level scope. *)
Lemma INV_init G : INV [HScope G] [OScope []] 0 G [] [] [].
Proof.
  split.
  - constructor; simpl; auto.
    + constructor.
    + constructor.
    + constructor.
    + intros [|l] o E Hn; simpl in E; [inversion E; subst | destruct l; discriminate].
      split; [exists (HScope G); split; auto; simpl; intros x v Hx; discriminate | simpl; tauto].
    + tauto.
  - intros [|l] o E r Hr; simpl in E; [inversion E; subst; simpl in Hr; tauto | destruct l; discriminate].
Qed.

(** ** Type soundness *)
(** [R] is [RAny] for a script whose top level may [return]; [RNone]
    otherwise.  Any return context gives the same guarantee. *)
Theorem soundness_v : forall G R e s,
  T sigs G LNone LNone R e [] s ->
  forall n, evalv vd defs n [OScope []] 0 [] e <> RStuck.
Proof.
  intros G R e s HT n Hst.
  pose proof (eval_sound n G LNone LNone R e [] s HT [HScope G] [OScope []] 0 [] [] [] [] (INV_init G) eq_refl) as Hr.
  rewrite Hst in Hr. exact Hr.
Qed.
End Sound.

(** Type soundness with the model's validator. *)
Theorem soundness : forall sigs defs, def_ok sigs defs -> maybe_ok sigs ->
  forall G R e s, T sigs G LNone LNone R e [] s ->
  forall n, eval defs n [OScope []] 0 [] e <> RStuck.
Proof.
  intros sigs defs Hdefs Hmaybe. apply (soundness_v sigs defs Hdefs Hmaybe validate).
  - intros Σ H f u v t O D E. eapply validate_dtyped; eauto.
  - intros Σ H f u v t V E I. eapply validate_imm; eauto.
Qed.
