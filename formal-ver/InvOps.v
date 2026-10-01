(** * Operations on the runtime invariant. *)

From Stdlib Require Import String List Arith Bool Lia Permutation.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Typing Interp Invariant RtLemmas Commit Partial Validate.

Ltac insolve := repeat (first [rewrite in_app_iff in * | progress simpl in *]); tauto.

(** ** Forall3 *)
Lemma Forall3_length {A B C} (P : A -> B -> C -> Prop) la lb lc :
  Forall3 P la lb lc -> length la = length lb /\ length lb = length lc.
Proof. induction 1; simpl; lia. Qed.

Lemma Forall3_app {A B C} (P : A -> B -> C -> Prop) la lb lc la' lb' lc' :
  Forall3 P la lb lc -> Forall3 P la' lb' lc' -> Forall3 P (la ++ la') (lb ++ lb') (lc ++ lc').
Proof. induction 1; simpl; auto. constructor; auto. Qed.

Lemma Forall3_app_inv {A B C} (P : A -> B -> C -> Prop) la1 la2 lb1 lb2 lc :
  Forall3 P (la1 ++ la2) (lb1 ++ lb2) lc -> length la1 = length lb1 ->
  exists lc1 lc2, lc = lc1 ++ lc2 /\ Forall3 P la1 lb1 lc1 /\ Forall3 P la2 lb2 lc2.
Proof.
  revert lb1 lc. induction la1 as [|a la1 IH]; intros [|b lb1] lc HF Hl; simpl in *; try lia.
  - exists [], lc; repeat split; auto. constructor.
  - inversion HF; subst. destruct (IH lb1 lc0) as (lc1 & lc2 & -> & F1 & F2); auto.
    exists (c :: lc1), lc2; repeat split; auto. constructor; auto.
Qed.

Lemma Forall3_impl {A B C} (P Q : A -> B -> C -> Prop) la lb lc :
  (forall a b c, P a b c -> Q a b c) -> Forall3 P la lb lc -> Forall3 Q la lb lc.
Proof. intros HPQ; induction 1; constructor; auto. Qed.

Lemma Forall3_impl_in {A B C} (P Q : A -> B -> C -> Prop) la lb lc :
  (forall a b c, In a la -> In c lc -> P a b c -> Q a b c) -> Forall3 P la lb lc -> Forall3 Q la lb lc.
Proof.
  intros HPQ; induction 1; constructor.
  - apply HPQ; auto; apply in_eq.
  - apply IHForall3. intros; apply HPQ; auto; apply in_cons; auto.
Qed.

Lemma Forall3_in_c {A B C} (P : A -> B -> C -> Prop) la lb lc c :
  Forall3 P la lb lc -> In c lc -> exists a b, In a la /\ P a b c.
Proof.
  induction 1; simpl; [tauto|]. intros [<-|Hc]; eauto. destruct (IHForall3 Hc) as (a' & b' & ? & ?); eauto.
Qed.

Lemma in_concat_iff {A} (x : A) (ls : list (list A)) : In x (concat ls) <-> exists l, In l ls /\ In x l.
Proof. apply in_concat. Qed.

(** ** Basic facts from the invariant *)
Section Ops.
Variable sigs : genv.

Lemma nodup_perm_in {A} (l1 l2 : list A) : Permutation l1 l2 -> NoDup l1 -> NoDup l2.
Proof. intros P N. eapply Permutation_NoDup; eauto. Qed.

Lemma inv_reg_lt Σ H sc G L st Os l :
  inv sigs Σ H sc G L st Os -> In l (concat Os) -> l < length H.
Proof.
  intros I Hl. pose proof (inv_reg _ _ _ _ _ _ _ _ I l Hl) as E.
  rewrite <- (inv_len _ _ _ _ _ _ _ _ I). eapply nth_error_lt; eauto.
Qed.

Lemma vtyped_vlocs_lt_all Σ :
  (forall v t, vtyped sigs Σ v t -> forall l, In l (vlocs v) -> l < length Σ) /\
  (forall vs ts, vtypedl sigs Σ vs ts -> forall l, In l (flat_map vlocs vs) -> l < length Σ).
Proof.
  apply (vtyped_comb sigs Σ
    (fun v t _ => forall l, In l (vlocs v) -> l < length Σ)
    (fun vs ts _ => forall l, In l (flat_map vlocs vs) -> l < length Σ)); simpl; try tauto.
  - intros l a t E _ l0 [<-|[]]; eapply nth_error_lt; eauto.
  - intros l fs r t E _ l0 [<-|[]]; eapply nth_error_lt; eauto.
  - intros v vs t ts _ IH _ IH' l Hl. apply in_app_or in Hl as [?|?]; auto.
Qed.

Lemma vtyped_vlocs_lt Σ v t : vtyped sigs Σ v t -> forall l, In l (vlocs v) -> l < length Σ.
Proof. intros. eapply (proj1 (vtyped_vlocs_lt_all Σ)); eauto. Qed.

Lemma dtyped_vlocs_lt Σ H v t O : dtyped sigs Σ H v t O -> forall l, In l (vlocs v) -> l < length H.
Proof.
  intros D l Hl. destruct (proj1 (dtyped_struct sigs Σ H) _ _ _ D) as (_ & Hv & Ho).
  destruct (Ho l (Hv l Hl)) as (o & E & _). eapply nth_error_lt; eauto.
Qed.

Lemma slots_lt Σ H L st Os :
  length Σ = length H -> Forall3 (slot_ok sigs Σ H) L st Os ->
  forall v, In v L -> forall l, In l (vlocs v) -> l < length H.
Proof.
  intros Hlen F. induction F as [|a b c la lb lc Pa F IH]; simpl; [tauto|].
  intros v [<-|Hv] l Hl.
  - unfold slot_ok in Pa. destruct b as [[| | m0 | f0] t]; simpl in Pa.
    + destruct Pa as [Pa _]. rewrite <- Hlen. eapply vtyped_vlocs_lt; eauto.
    + eapply dtyped_vlocs_lt; eauto.
    + destruct (mtyped_partial sigs _ _ _ _ _ _ Pa eq_refl) as (l0 & -> & _ & [(? & ? & ? & _ & _ & E)|(? & ? & ? & ? & _ & _ & E)]);
        simpl in Hl; destruct Hl as [<-|[]]; eapply nth_error_lt; eauto.
    + destruct (mtyped_partial sigs _ _ _ _ _ _ Pa eq_refl) as (l0 & -> & _ & [(? & ? & ? & _ & _ & E)|(? & ? & ? & ? & _ & _ & E)]);
        simpl in Hl; destruct Hl as [<-|[]]; eapply nth_error_lt; eauto.
  - eapply IH; eauto.
Qed.

Lemma inv_slot_lt Σ H sc G L st Os :
  inv sigs Σ H sc G L st Os -> forall v, In v L -> forall l, In l (vlocs v) -> l < length H.
Proof. intros I. eapply slots_lt; [apply (inv_len _ _ _ _ _ _ _ _ I) | apply (inv_slots _ _ _ _ _ _ _ _ I)]. Qed.

(** Every reference stored in the heap points to an allocated location.
    (Kept alongside [inv] rather than inside it.) *)
Definition bounded (H : heap) : Prop :=
  forall l o, nth_error H l = Some o -> forall r, In r (olocs o) -> r < length H.


Lemma Forall3_split {A B C} (P : A -> B -> C -> Prop) a1 a2 b1 b2 c1 c2 :
  Forall3 P (a1 ++ a2) (b1 ++ b2) (c1 ++ c2) -> length a1 = length b1 -> length b1 = length c1 ->
  Forall3 P a1 b1 c1 /\ Forall3 P a2 b2 c2.
Proof.
  revert b1 c1. induction a1 as [|a a1 IH]; intros [|b b1] [|c c1] F L1 L2; simpl in *; try lia.
  - split; auto. constructor.
  - inversion F; subst. destruct (IH b1 c1) as [F1 F2]; auto. split; auto. constructor; auto.
Qed.

Lemma perm_mid {A} (a b c : list A) : Permutation (a ++ b ++ c) (b ++ a ++ c).
Proof. rewrite !app_assoc. apply Permutation_app_tail. apply Permutation_app_comm. Qed.

Lemma nodup_remove_mid {A} (a b c : list A) : NoDup (a ++ b ++ c) -> NoDup (a ++ c).
Proof.
  intros N. assert (N' : NoDup (b ++ a ++ c)) by (eapply Permutation_NoDup; [apply perm_mid | exact N]).
  apply NoDup_app_remove_l in N'. exact N'.
Qed.

Lemma nodup_mid_disj {A} (a b c : list A) x : NoDup (a ++ b ++ c) -> In x b -> ~ In x (a ++ c).
Proof.
  intros N Hb Hac. assert (N' : NoDup (b ++ a ++ c)) by (eapply Permutation_NoDup; [apply perm_mid | exact N]).
  apply nodup_app_inv in N' as (_ & _ & D). exact (D x Hb Hac).
Qed.

Lemma Forall3_and {A B C} (P Q : A -> B -> C -> Prop) la lb lc :
  Forall3 P la lb lc -> Forall3 Q la lb lc -> Forall3 (fun a b c => P a b c /\ Q a b c) la lb lc.
Proof. induction 1; intros HQ; inversion HQ; subst; constructor; auto. Qed.

Lemma slot_ok_agree Σ Σ' H w p Ow X :
  slot_ok sigs Σ H w p Ow -> sagree Σ Σ' X -> scope_ext Σ Σ' ->
  (fst p = Sh -> forall l, In l (vlocs w) -> ~ In l X) ->
  (partial (fst p) = true -> dead_on Σ X) -> slot_ok sigs Σ' H w p Ow.
Proof.
  unfold slot_ok. destruct p as [[| | m | f] t]; simpl; intros Hs Ha He Hx Hd.
  - destruct Hs as [Hv ->]. split; auto. eapply vtyped_agree; eauto.
  - eapply dtyped_agree1; eauto.
  - eapply mtyped_keep; [exact Hs | eapply sagree_keeps_live; eauto | intros; reflexivity].
  - eapply mtyped_keep; [exact Hs | eapply sagree_keeps_live; eauto | intros; reflexivity].
Qed.

(** A slot whose region is unchanged in the heap survives a change of the
    store typing that keeps every live entry. *)
Lemma slot_ok_keep Σ Σ' H H' w p Ow :
  slot_ok sigs Σ H w p Ow -> keeps_live Σ Σ' ->
  (forall l, In l Ow -> nth_error H' l = nth_error H l) -> slot_ok sigs Σ' H' w p Ow.
Proof.
  destruct p as [m t]. rewrite !slot_ok_mtyped. intros M K Hm. eapply mtyped_keep; eauto.
Qed.

(** Replacing the type of one slot, keeping its value and region. *)
Lemma inv_replace_slot_at Σ H sc G P v L sp p p' st Op O Os :
  length P = length sp -> length sp = length Op ->
  inv sigs Σ H sc G (P ++ v :: L) (sp ++ p :: st) (Op ++ O :: Os) ->
  slot_ok sigs Σ H v p' O ->
  inv sigs Σ H sc G (P ++ v :: L) (sp ++ p' :: st) (Op ++ O :: Os).
Proof.
  intros L1 L2 [Ilen Islots Idisj Iown Iheap Ireg Iscope] Hs. constructor; auto.
  - apply Forall3_split in Islots as [F1 F2]; auto. apply Forall3_app; auto.
    inversion F2; subst. constructor; auto.
  - apply Forall3_split in Iown as [F1 F2]; auto. apply Forall3_app; auto.
    inversion F2; subst. constructor; auto.
Qed.

(** Committing a fresh slot anywhere in the stack. *)
Lemma inv_commit_at_m Σ H sc G P v L sp m t st Op O Os :
  length P = length sp -> length sp = length Op ->
  inv sigs Σ H sc G (P ++ v :: L) (sp ++ (m, t) :: st) (Op ++ O :: Os) ->
  exists Σ', scope_ext Σ Σ' /\ length Σ' = length Σ /\
    inv sigs Σ' H sc G (P ++ v :: L) (sp ++ (Sh, t) :: st) (Op ++ [] :: Os).
Proof.
  intros L1 L2 I. destruct I as [Ilen Islots Idisj Iown Iheap Ireg Iscope].
  pose proof Islots as Islots'.
  apply Forall3_split in Islots' as [F1 F2]; auto.
  inversion F2 as [|? ? ? ? ? ? Pv F3]; subst. apply slot_ok_mtyped in Pv.
  rewrite concat_app in *. simpl in *.
  destruct (proj1 (mtyped_struct sigs Σ H) _ _ _ _ Pv) as (NO & Hvl & _ & Hobj).
  assert (DO : dead_on Σ O).
  { intros l Hl. apply Ireg. apply in_or_app; right; apply in_or_app; auto. }
  destruct (mtyped_commit sigs Σ H v t m O Σ Pv (keeps_live_refl Σ) Ilen DO) as (Σ' & L' & A' & C' & V').
  assert (Sx : scope_ext Σ Σ') by (eapply sagree_scope_ext; eauto using dead_on_nonscope).
  (* locations of O are not in the other regions *)
  assert (Dj : forall l, In l O -> ~ In l (concat Op ++ concat Os)).
  { intros l Hl. eapply nodup_mid_disj; eauto. }
  assert (Hall : forall l, In l O -> In l (concat Op ++ O ++ concat Os)).
  { intros l Hl. apply in_or_app; right; apply in_or_app; auto. }
  assert (Hsub : forall l, In l (concat Op ++ concat Os) -> In l (concat Op ++ O ++ concat Os)).
  { intros l Hl. apply in_app_or in Hl as [Hl|Hl]; apply in_or_app; [left|right; apply in_or_app; right]; auto. }
  (* a live location is in no region *)
  assert (NotReg : forall l, live Σ l -> ~ In l (concat Op ++ concat Os)).
  { intros l Hlv Hc. apply Hlv. apply Ireg. apply Hsub; auto. }
  assert (Out : forall l, In l O \/ live Σ l -> ~ In l (concat Op ++ concat Os)).
  { intros l [Hl|Hl]; [apply Dj | apply NotReg]; auto. }
  pose proof (Forall3_and _ _ _ _ _ Islots Iown) as SO.
  apply Forall3_split in SO as [SO1 SO2]; auto.
  inversion SO2 as [|? ? ? ? ? ? _ SO3]; subst.
  assert (Hsl : forall w p Ow,
    slot_ok sigs Σ H w p Ow /\
    (forall l, In l (vlocs w) -> In l (concat Op ++ O ++ concat Os) -> In l Ow) ->
    slot_ok sigs Σ' H w p Ow /\
    (forall l, In l (vlocs w) -> In l (concat (Op ++ [] :: Os)) -> In l Ow)).
  { intros w p Ow [Hs Ho]. split.
    - eapply slot_ok_agree; eauto.
      intros Hsh l Hl HlO.
      unfold slot_ok in Hs. rewrite Hsh in Hs. destruct Hs as [_ ->].
      exact (Ho l Hl (Hall l HlO)).
    - intros l Hl Hc. rewrite concat_app in Hc. simpl in Hc. apply Ho; auto. }
  exists Σ'. split; [exact Sx|]. split; [lia|]. constructor.
  - lia.
  - apply Forall3_app.
    + eapply Forall3_impl; [| exact SO1]. intros w p Ow HH. exact (proj1 (Hsl w p Ow HH)).
    + constructor.
      * split; auto.
      * eapply Forall3_impl; [| exact SO3]. intros w p Ow HH. exact (proj1 (Hsl w p Ow HH)).
  - rewrite concat_app. simpl. eapply nodup_remove_mid. exact Idisj.
  - apply Forall3_app.
    + eapply Forall3_impl; [| exact SO1]. intros w p Ow HH. exact (proj2 (Hsl w p Ow HH)).
    + constructor.
      * intros l Hl Hc. rewrite concat_app in Hc. simpl in Hc. exfalso. exact (Out l (Hvl l Hl) Hc).
      * eapply Forall3_impl; [| exact SO3]. intros w p Ow HH. exact (proj2 (Hsl w p Ow HH)).
  - rewrite concat_app. simpl. intros l o E Hn Hlv.
    destruct (in_dec Nat.eq_dec l O) as [HlO|HlO].
    + destruct (C' l HlO) as (h & o' & E' & Ns & Eo & Ok). rewrite E in Eo. inversion Eo; subst.
      split; [exists h; split; auto|].
      destruct (Hobj l HlO) as (o'' & Eo'' & _ & Hr). rewrite E in Eo''. inversion Eo''; subst.
      intros r Hr' Hc. exact (Out r (Hr r Hr') Hc).
    + assert (Hn' : ~ In l (concat Op ++ O ++ concat Os)).
      { intros Hc. apply in_app_or in Hc as [Hc|Hc]; [apply Hn; apply in_or_app; auto|].
        apply in_app_or in Hc as [Hc|Hc]; [contradiction | apply Hn; apply in_or_app; auto]. }
      destruct (Iheap l o E Hn' (live_back _ _ _ _ A' HlO Hlv)) as [(h & Eh & Ok) Hr]. split.
      * exists h. split; [apply A'; auto|].
        eapply obj_ok_agree with (X := O); [exact Ok | | exact A' | exact Sx].
        intros r Hr' HrO. exact (Hr r Hr' (Hall r HrO)).
      * intros r Hr' Hc. exact (Hr r Hr' (Hsub r Hc)).
  - rewrite concat_app. simpl. intros l Hl.
    apply A'; [apply Ireg, Hsub; auto |]. intro HlO. exact (Dj l HlO Hl).
  - apply Sx; auto.
Qed.

Lemma inv_commit_at Σ H sc G P v L sp t st Op O Os :
  length P = length sp -> length sp = length Op ->
  inv sigs Σ H sc G (P ++ v :: L) (sp ++ (Dp, t) :: st) (Op ++ O :: Os) ->
  exists Σ', scope_ext Σ Σ' /\ length Σ' = length Σ /\
    inv sigs Σ' H sc G (P ++ v :: L) (sp ++ (Sh, t) :: st) (Op ++ [] :: Os).
Proof. apply inv_commit_at_m. Qed.

Lemma slot_at Σ H sc G P v L sp p st Op O Os :
  length P = length sp -> length sp = length Op ->
  inv sigs Σ H sc G (P ++ v :: L) (sp ++ p :: st) (Op ++ O :: Os) -> slot_ok sigs Σ H v p O.
Proof.
  intros L1 L2 I. pose proof (inv_slots _ _ _ _ _ _ _ _ I) as F.
  apply Forall3_split in F as [_ F]; auto. inversion F; subst; auto.
Qed.

Lemma inv_slot_sub_at Σ H sc G P v L sp p p' st Op O Os :
  slot_sub p p' -> length P = length sp -> length sp = length Op ->
  inv sigs Σ H sc G (P ++ v :: L) (sp ++ p :: st) (Op ++ O :: Os) ->
  exists Σ' O', scope_ext Σ Σ' /\ length Σ' = length Σ /\
    inv sigs Σ' H sc G (P ++ v :: L) (sp ++ p' :: st) (Op ++ O' :: Os).
Proof.
  intros Hs L1 L2 I. pose proof (slot_at _ _ _ _ _ _ _ _ _ _ _ _ _ L1 L2 I) as Sl.
  destruct Hs as [a b Hab|a b Hab|a b Hab|a b Hi Hab|m a b Pm Hab|m a Pm|m a b Pm Hab].
  5: { exists Σ, O. split; [apply scope_ext_refl|]. split; auto.
       eapply inv_replace_slot_at; eauto. apply slot_ok_mtyped. apply slot_ok_mtyped in Sl.
       eapply mtyped_msub; eauto. }
  5: { exists Σ, O. split; [apply scope_ext_refl|]. split; auto. }
  5: { destruct (inv_commit_at_m _ _ _ _ _ _ _ _ _ _ _ _ _ _ L1 L2 I) as (Σ' & Sx & Ln & I').
       exists Σ', []. split; auto. split; auto.
       eapply inv_replace_slot_at; eauto.
       pose proof (slot_at _ _ _ _ _ _ _ _ _ _ _ _ _ L1 L2 I') as Sl'. destruct Sl' as [Hv _].
       split; auto. eapply vtyped_sub; eauto. }
  all: unfold slot_ok in Sl; simpl in Sl.
  - destruct Sl as [Hv ->]. exists Σ, []. split; [apply scope_ext_refl|]. split; auto.
    eapply inv_replace_slot_at; eauto. split; auto. eapply vtyped_sub; eauto.
  - exists Σ, O. split; [apply scope_ext_refl|]. split; auto.
    eapply inv_replace_slot_at; eauto. unfold slot_ok; simpl. eapply dtyped_rsub; eauto.
  - destruct (inv_commit_at _ _ _ _ _ _ _ _ _ _ _ _ _ L1 L2 I) as (Σ' & Sx & Ln & I').
    exists Σ', []. split; auto. split; auto.
    eapply inv_replace_slot_at; eauto.
    pose proof (slot_at _ _ _ _ _ _ _ _ _ _ _ _ _ L1 L2 I') as Sl'. destruct Sl' as [Hv _].
    split; auto. eapply vtyped_sub; eauto.
  - destruct Sl as [Hv ->]. exists Σ, []. split; [apply scope_ext_refl|]. split; auto.
    eapply inv_replace_slot_at; eauto. unfold slot_ok; simpl.
    eapply dtyped_sub; [ eapply vtyped_imm_dtyped; eauto | exact Hab ].
Qed.

Lemma inv_ssub_gen : forall s1 s1', ssub s1 s1' ->
  forall Σ H sc G P S1 L2 sp rest Op Os,
  length P = length sp -> length sp = length Op -> length S1 = length s1 ->
  inv sigs Σ H sc G (P ++ S1 ++ L2) (sp ++ s1 ++ rest) (Op ++ Os) ->
  exists Σ' Os', scope_ext Σ Σ' /\ length Σ' = length Σ /\
    inv sigs Σ' H sc G (P ++ S1 ++ L2) (sp ++ s1' ++ rest) (Op ++ Os').
Proof.
  intros s1 s1' Hs. induction Hs as [|p p' s1 s1' Hp Hs IH];
    intros Σ H sc G P S1 L2 sp rest Op Os L1 L2' L3 I.
  - exists Σ, Os. split; [apply scope_ext_refl|]. auto.
  - destruct S1 as [|v S1]; simpl in L3; [lia|].
    pose proof (Forall3_length _ _ _ _ (inv_slots _ _ _ _ _ _ _ _ I)) as [La Lb].
    rewrite !length_app in La, Lb. simpl in La, Lb.
    destruct Os as [|O Os]; [simpl in Lb; lia|].
    destruct (inv_slot_sub_at _ _ _ _ _ _ _ _ _ _ _ _ _ _ Hp L1 L2' I) as (Σ1 & O1 & Sx1 & Ln1 & I1).
    assert (I1' : inv sigs Σ1 H sc G ((P ++ [v]) ++ S1 ++ L2) ((sp ++ [p']) ++ s1 ++ rest) ((Op ++ [O1]) ++ Os)).
    { rewrite <- !app_assoc. exact I1. }
    destruct (IH Σ1 H sc G (P ++ [v]) S1 L2 (sp ++ [p']) rest (Op ++ [O1]) Os) as (Σ2 & Os2 & Sx2 & Ln2 & I2);
      try (rewrite !length_app; simpl; lia); auto.
    exists Σ2, (O1 :: Os2). split; [eapply scope_ext_trans; eauto|]. split; [lia|].
    rewrite <- !app_assoc in I2. exact I2.
Qed.

Lemma inv_ssub Σ H sc G S Sf s1 s1' sf Os :
  ssub s1 s1' -> length S = length s1 ->
  inv sigs Σ H sc G (S ++ Sf) (s1 ++ sf) Os ->
  exists Σ' Os', scope_ext Σ Σ' /\ length Σ' = length Σ /\ inv sigs Σ' H sc G (S ++ Sf) (s1' ++ sf) Os'.
Proof.
  intros Hs L I. destruct (inv_ssub_gen s1 s1' Hs Σ H sc G [] S Sf [] sf [] Os) as (Σ' & Os' & ? & ? & ?);
    simpl; auto. eauto.
Qed.

(** ** Stack operations at the top *)

Lemma inv_pop_sh Σ H sc G v L t st O Os :
  inv sigs Σ H sc G (v :: L) ((Sh, t) :: st) (O :: Os) ->
  O = [] /\ vtyped sigs Σ v t /\ (forall l, In l (vlocs v) -> ~ In l (concat Os)) /\
  inv sigs Σ H sc G L st Os.
Proof.
  intros [Ilen Islots Idisj Iown Iheap Ireg Iscope].
  inversion Islots as [|? ? ? ? ? ? Pa Fs]; subst. destruct Pa as [Hv ->]. simpl in *.
  inversion Iown as [|? ? ? ? ? ? Oa Fo]; subst.
  split; auto. split; auto. split.
  - intros l Hl Hc. exact (Oa l Hl Hc).
  - constructor; auto.
Qed.

Lemma inv_push_sh Σ H sc G v L t st Os :
  inv sigs Σ H sc G L st Os -> vtyped sigs Σ v t ->
  (forall l, In l (vlocs v) -> ~ In l (concat Os)) ->
  inv sigs Σ H sc G (v :: L) ((Sh, t) :: st) ([] :: Os).
Proof.
  intros [Ilen Islots Idisj Iown Iheap Ireg Iscope] Hv Hl. constructor; simpl.
  - exact Ilen.
  - constructor; [split; auto | exact Islots].
  - exact Idisj.
  - constructor; [intros l Hl1 Hl2; exact (Hl l Hl1 Hl2) | exact Iown].
  - exact Iheap.
  - exact Ireg.
  - exact Iscope.
Qed.

Lemma inv_commit_top Σ H sc G v L t st O Os :
  inv sigs Σ H sc G (v :: L) ((Dp, t) :: st) (O :: Os) ->
  exists Σ', scope_ext Σ Σ' /\ length Σ' = length Σ /\
    inv sigs Σ' H sc G (v :: L) ((Sh, t) :: st) ([] :: Os).
Proof.
  intros I. apply (inv_commit_at Σ H sc G [] v L [] t st [] O Os); auto.
Qed.

Lemma inv_drop Σ H sc G v L p st O Os :
  inv sigs Σ H sc G (v :: L) (p :: st) (O :: Os) ->
  exists Σ', scope_ext Σ Σ' /\ length Σ' = length Σ /\ inv sigs Σ' H sc G L st Os.
Proof.
  intros I. destruct p as [m t].
  destruct (inv_commit_at_m Σ H sc G [] v L [] m t st [] O Os eq_refl eq_refl I) as (Σ' & Sx & Ln & I').
  simpl in I'. apply inv_pop_sh in I' as (_ & _ & _ & I'). exists Σ'; auto.
Qed.

Lemma inv_drop_prefix Σ H sc G S1 L st1 st Os :
  length S1 = length st1 ->
  inv sigs Σ H sc G (S1 ++ L) (st1 ++ st) Os ->
  exists Σ' Os', scope_ext Σ Σ' /\ length Σ' = length Σ /\ inv sigs Σ' H sc G L st Os'.
Proof.
  revert Σ st1 Os. induction S1 as [|v S1 IH]; intros Σ [|p st1] Os Ln I; simpl in *; try lia.
  - exists Σ, Os. split; [apply scope_ext_refl|]. auto.
  - pose proof (Forall3_length _ _ _ _ (inv_slots _ _ _ _ _ _ _ _ I)) as [_ Lb].
    destruct Os as [|O Os]; simpl in Lb; [lia|].
    destruct (inv_drop _ _ _ _ _ _ _ _ _ _ I) as (Σ1 & Sx1 & Ln1 & I1).
    destruct (IH Σ1 st1 Os) as (Σ2 & Os2 & Sx2 & Ln2 & I2); auto.
    exists Σ2, Os2. split; [eapply scope_ext_trans; eauto|]. split; [lia|]. auto.
Qed.

Lemma inv_swap Σ H sc G a b L p q st Oa Ob Os :
  inv sigs Σ H sc G (a :: b :: L) (p :: q :: st) (Oa :: Ob :: Os) ->
  inv sigs Σ H sc G (b :: a :: L) (q :: p :: st) (Ob :: Oa :: Os).
Proof.
  intros [Ilen Islots Idisj Iown Iheap Ireg Iscope]. simpl in *.
  assert (Hin : forall x, In x (Oa ++ Ob ++ concat Os) <-> In x (Ob ++ Oa ++ concat Os)).
  { intros x; split; intros Hx; rewrite !in_app_iff in *; tauto. }
  inversion Islots as [|? ? ? ? ? ? Pa F1]; subst. inversion F1 as [|? ? ? ? ? ? Pb F2]; subst.
  inversion Iown as [|? ? ? ? ? ? Oa' G1]; subst. inversion G1 as [|? ? ? ? ? ? Ob' G2]; subst.
  constructor; simpl; auto.
  - constructor; auto. constructor; auto.
  - eapply Permutation_NoDup; [apply perm_mid | exact Idisj].
  - constructor; [intros l Hl Hc; apply Ob'; auto; apply Hin; auto|].
    constructor; [intros l Hl Hc; apply Oa'; auto; apply Hin; auto|].
    eapply Forall3_impl; [| exact G2]. simpl. intros w _ Ow Hw l Hl Hc. apply Hw; auto. apply Hin; auto.
  - intros l o E Hn Hlv. destruct (Iheap l o E) as [Ho Hr]; [intro Hc; apply Hn; apply Hin; auto | exact Hlv |].
    split; auto. intros r Hr' Hc. apply (Hr r Hr'). apply Hin; auto.
  - intros l Hl. apply Ireg. apply Hin; auto.
Qed.

Lemma inv_scope_change Σ H sc sc' G G' L st Os :
  inv sigs Σ H sc G L st Os -> nth_error Σ sc' = Some (HScope G') -> inv sigs Σ H sc' G' L st Os.
Proof. intros [? ? ? ? ? ? ?] E. constructor; auto. Qed.

Lemma inv_scope_ext Σ H sc G L st Os :
  inv sigs Σ H sc G L st Os -> forall Σ0, scope_ext Σ0 Σ -> nth_error Σ0 sc = Some (HScope G) -> True.
Proof. auto. Qed.

(** ** Heap operations *)

Lemma nth_error_app_lt {A} (l : list A) x n : n < length l -> nth_error (l ++ [x]) n = nth_error l n.
Proof. intros; rewrite nth_error_app1; auto. Qed.

Lemma nth_error_app_eq {A} (l : list A) x : nth_error (l ++ [x]) (length l) = Some x.
Proof. rewrite nth_error_app2; [rewrite Nat.sub_diag; reflexivity | lia]. Qed.

Lemma sagree_app (Σ : store_ty) h : sagree Σ (Σ ++ [h]) [].
Proof. intros l h' E _. rewrite nth_error_app1; auto. eapply nth_error_lt; eauto. Qed.

Lemma scope_ext_app (Σ : store_ty) h : scope_ext Σ (Σ ++ [h]).
Proof. intros l G E. rewrite nth_error_app1; auto. eapply nth_error_lt; eauto. Qed.

(** Updating an object outside all regions. *)
Lemma inv_update_out Σ H sc G L st Os l o o' h :
  inv sigs Σ H sc G L st Os -> nth_error H l = Some o -> ~ In l (concat Os) ->
  nth_error Σ l = Some h -> obj_ok sigs Σ o' h ->
  (forall r, In r (olocs o') -> ~ In r (concat Os)) ->
  inv sigs Σ (set_nth l o' H) sc G L st Os.
Proof.
  intros [Ilen Islots Idisj Iown Iheap Ireg Iscope] E Hn Eh Ok Hr. constructor; auto.
  - rewrite set_nth_length; auto.
  - eapply Forall3_impl_in; [| exact Islots]. intros w p Ow _ HOw Hs.
    eapply slot_ok_keep; [exact Hs | apply keeps_live_refl |].
    intros m Hm. rewrite nth_error_set_nth_neq; auto. intro; subst.
    apply Hn. apply in_concat. eauto.
  - intros m om Em Hm Hlv. destruct (Nat.eq_dec m l) as [->|Hne].
    + rewrite nth_error_set_nth_eq in Em by (eapply nth_error_lt; eauto). inversion Em; subst.
      split; eauto.
    + rewrite nth_error_set_nth_neq in Em; auto.
Qed.

Lemma bounded_update H l o' :
  bounded H -> (forall r, In r (olocs o') -> r < length H) -> bounded (set_nth l o' H).
Proof.
  intros B Hr m om Em r Hin. rewrite set_nth_length.
  destruct (Nat.eq_dec m l) as [->|Hne].
  - destruct (Nat.lt_ge_cases l (length H)) as [Hlt|Hge].
    + rewrite nth_error_set_nth_eq in Em; auto. inversion Em; subst; auto.
    + exfalso. assert (nth_error (set_nth l o' H) l = None).
      { apply nth_error_None. rewrite set_nth_length; auto. }
      congruence.
  - rewrite nth_error_set_nth_neq in Em; auto. eapply B; eauto.
Qed.

Lemma bounded_app H o :
  bounded H -> (forall r, In r (olocs o) -> r < S (length H)) -> bounded (H ++ [o]).
Proof.
  intros B Hr m om Em r Hin. rewrite length_app; simpl.
  destruct (Nat.lt_ge_cases m (length H)) as [Hlt|Hge].
  - rewrite nth_error_app1 in Em; auto. specialize (B m om Em r Hin). lia.
  - rewrite nth_error_app2 in Em by lia. destruct (m - length H) as [|k] eqn:K; simpl in Em.
    + inversion Em; subst. specialize (Hr r Hin). lia.
    + destruct k; discriminate.
Qed.

(** Allocating an object outside all regions (a scope). *)
Lemma inv_alloc_out Σ H sc G L st Os o h :
  inv sigs Σ H sc G L st Os -> obj_ok sigs (Σ ++ [h]) o h ->
  (forall r, In r (olocs o) -> ~ In r (concat Os)) ->
  inv sigs (Σ ++ [h]) (H ++ [o]) sc G L st Os.
Proof.
  intros I Ok Hr. assert (Rlt : forall l, In l (concat Os) -> l < length H) by (intros; eapply inv_reg_lt; eauto).
  destruct I as [Ilen Islots Idisj Iown Iheap Ireg Iscope]. constructor; auto.
  - rewrite !length_app; simpl; lia.
  - eapply Forall3_impl_in; [| exact Islots]. intros w p Ow _ HOw Hs.
    eapply slot_ok_agree with (Σ := Σ) (X := []);
      [| apply sagree_app | apply scope_ext_app | intros _ l0 _ [] | intros _ l0 []].
    + eapply slot_ok_keep; [exact Hs | apply keeps_live_refl |].
      intros m Hm. apply nth_error_app_lt. apply Rlt. apply in_concat. eauto.
  - intros m om Em Hm Hlv. destruct (Nat.lt_ge_cases m (length H)) as [Hlt|Hge].
    + rewrite nth_error_app_lt in Em; auto.
      destruct (Iheap m om Em Hm (live_app Σ _ m ltac:(lia) Hlv)) as [(h' & Eh & Okh) Hr'].
      split; auto. exists h'. split.
      * rewrite nth_error_app1; auto. lia.
      * eapply obj_ok_agree with (X := []); eauto using sagree_app, scope_ext_app.
    + rewrite nth_error_app2 in Em by lia. destruct (m - length H) as [|k] eqn:K; simpl in Em.
      * inversion Em; subst. split; auto. exists h. split; auto.
        assert (m = length Σ) by lia. subst. apply nth_error_app_eq.
      * destruct k; discriminate.
  - intros m Hm. pose proof (Ireg m Hm) as E.
    rewrite nth_error_app1; auto. eapply nth_error_lt; eauto.
  - rewrite nth_error_app1; auto. eapply nth_error_lt; eauto.
Qed.

(** Allocating a fresh (empty) list or dict and pushing it as a fresh slot. *)
Lemma inv_alloc_dp Σ H sc G L st Os o t :
  inv sigs Σ H sc G L st Os -> bounded H -> olocs o = [] ->
  dtyped sigs (Σ ++ [HDead]) (H ++ [o]) (VLoc (length H)) t [length H] ->
  inv sigs (Σ ++ [HDead]) (H ++ [o]) sc G (VLoc (length H) :: L) ((Dp, t) :: st) ([length H] :: Os).
Proof.
  intros I B Ho D. assert (Rlt : forall l, In l (concat Os) -> l < length H) by (intros; eapply inv_reg_lt; eauto).
  pose proof (inv_slot_lt _ _ _ _ _ _ _ I) as Slt.
  destruct I as [Ilen Islots Idisj Iown Iheap Ireg Iscope].
  assert (NR : ~ In (length H) (concat Os)) by (intro Hc; specialize (Rlt _ Hc); lia).
  constructor; simpl.
  - rewrite !length_app; simpl; lia.
  - constructor; [exact D|].
    eapply Forall3_impl_in; [| exact Islots]. intros w p Ow _ HOw Hs.
    eapply slot_ok_agree with (Σ := Σ) (X := []);
      [| apply sagree_app | apply scope_ext_app | intros _ l0 _ [] | intros _ l0 []].
    + eapply slot_ok_keep; [exact Hs | apply keeps_live_refl |].
      intros m Hm. apply nth_error_app_lt. apply Rlt. apply in_concat. eauto.
  - constructor; auto.
  - constructor.
    + intros l [<-|[]] _. apply in_eq.
    + eapply Forall3_impl_in; [| exact Iown]. intros w p Ow Hw HOw Hown l Hl [<-|Hc].
      * specialize (Slt w Hw _ Hl). lia.
      * apply Hown; auto.
  - intros m om Em Hm Hlv. destruct (Nat.lt_ge_cases m (length H)) as [Hlt|Hge].
    + rewrite nth_error_app_lt in Em; auto.
      destruct (Iheap m om Em (fun Hc => Hm (or_intror Hc)) (live_app Σ _ m ltac:(lia) Hlv)) as [(h' & Eh & Okh) Hr'].
      split.
      * exists h'. split; [rewrite nth_error_app1; auto; lia|].
        eapply obj_ok_agree with (X := []); eauto using sagree_app, scope_ext_app.
      * intros r Hr [<-|Hc]; [specialize (B m om Em _ Hr); lia | exact (Hr' r Hr Hc)].
    + exfalso. apply Hm. left.
      rewrite nth_error_app2 in Em by lia. destruct (m - length H) as [|k] eqn:K; simpl in Em.
      * lia.
      * destruct k; discriminate.
  - intros m [<-|Hm].
    + rewrite <- Ilen. apply nth_error_app_eq.
    + pose proof (Ireg m Hm) as E.
      rewrite nth_error_app1; auto. eapply nth_error_lt; eauto.
  - rewrite nth_error_app1; auto. eapply nth_error_lt; eauto.
Qed.

Lemma inv_heap_ok_out Σ H sc G L st Os :
  inv sigs Σ H sc G L st Os -> heap_ok_out sigs Σ H (concat Os).
Proof. intros I l o E Hn. eapply (inv_heap _ _ _ _ _ _ _ _ I); eauto. Qed.

Lemma inv_replace_top_dp Σ H sc G v v' L t t' st O Os :
  inv sigs Σ H sc G (v :: L) ((Dp, t) :: st) (O :: Os) -> dtyped sigs Σ H v' t' O ->
  inv sigs Σ H sc G (v' :: L) ((Dp, t') :: st) (O :: Os).
Proof.
  intros [Ilen Islots Idisj Iown Iheap Ireg Iscope] D. constructor; auto.
  - inversion Islots; subst. constructor; auto.
  - inversion Iown; subst. constructor; auto.
    destruct (proj1 (dtyped_struct sigs Σ H) _ _ _ D) as (_ & Hv & _). intros l Hl _. auto.
Qed.

Lemma concat_nils {A B} (vs : list A) (Os : list (list B)) :
  concat (map (fun _ => []) vs ++ Os) = concat Os.
Proof. induction vs; simpl; auto. Qed.

Lemma inv_push_shs Σ H sc G L st Os t vs :
  inv sigs Σ H sc G L st Os -> Forall (fun v => vtyped sigs Σ v t) vs ->
  (forall v, In v vs -> forall l, In l (vlocs v) -> ~ In l (concat Os)) ->
  inv sigs Σ H sc G (vs ++ L) (map (fun _ => (Sh, t)) vs ++ st) (map (fun _ => []) vs ++ Os).
Proof.
  intros I F Hl. induction vs as [|v vs IH]; simpl; auto.
  inversion F; subst.
  apply inv_push_sh.
  - apply IH; auto. intros w Hw. apply Hl. apply in_cons; auto.
  - auto.
  - rewrite concat_nils. apply Hl. apply in_eq.
Qed.

Lemma sagree_app_l (Σ X : store_ty) : sagree Σ (Σ ++ X) [].
Proof. intros l h' E _. rewrite nth_error_app1; auto. eapply nth_error_lt; eauto. Qed.

Lemma scope_ext_app_l (Σ X : store_ty) : scope_ext Σ (Σ ++ X).
Proof. intros l G E. rewrite nth_error_app1; auto. eapply nth_error_lt; eauto. Qed.

(** Allocating a whole new region (the result of an explicit copy) and
    pushing its root as a fresh slot.  The region is exactly the new
    locations; their store types are irrelevant while the slot is fresh. *)
Lemma inv_alloc_region Σ H sc G L st Os N v t O :
  inv sigs Σ H sc G L st Os -> bounded H ->
  dtyped sigs Σ (H ++ N) v t O ->
  (forall l, In l O <-> length H <= l < length H + length N) ->
  inv sigs (Σ ++ repeat (HDead) (length N)) (H ++ N) sc G (v :: L) ((Dp, t) :: st) (O :: Os) /\
  bounded (H ++ N).
Proof.
  intros I B D Rg.
  assert (Rlt : forall l, In l (concat Os) -> l < length H) by (intros; eapply inv_reg_lt; eauto).
  pose proof (inv_slot_lt _ _ _ _ _ _ _ I) as Slt.
  destruct (proj1 (dtyped_struct sigs Σ (H ++ N)) _ _ _ D) as (NO & Vo & Oo).
  destruct I as [Ilen Islots Idisj Iown Iheap Ireg Iscope].
  set (Σ' := Σ ++ repeat (HDead) (length N)).
  assert (Pre : forall m, m < length H -> nth_error (H ++ N) m = nth_error H m)
    by (intros; apply nth_error_app1; auto).
  assert (NOs : forall l, In l O -> ~ In l (concat Os))
    by (intros l Hl Hc; apply Rg in Hl; specialize (Rlt _ Hc); lia).
  split.
  - constructor; simpl.
    + unfold Σ'. rewrite !length_app, repeat_length. lia.
    + constructor.
      * unfold slot_ok; simpl. eapply dtyped_agree1; [exact D | apply scope_ext_app_l | auto].
      * eapply Forall3_impl_in; [| exact Islots]. intros w p Ow _ HOw Hs.
        eapply slot_ok_agree with (Σ := Σ) (X := []);
          [| apply sagree_app_l | apply scope_ext_app_l | intros _ l _ [] | intros _ l []].
        eapply slot_ok_keep; [exact Hs | apply keeps_live_refl |].
        intros m Hm. apply Pre. apply Rlt. apply in_concat. eauto.
    + apply NoDup_app; auto.
    + constructor.
      * intros l Hl _. auto.
      * eapply Forall3_impl_in; [| exact Iown]. intros w p Ow Hw HOw Hown l Hl Hc.
        apply in_app_or in Hc as [Hc|Hc].
        -- exfalso. apply Rg in Hc. specialize (Slt w Hw _ Hl). lia.
        -- apply Hown; auto.
    + intros m om Em Hm Hlv. destruct (Nat.lt_ge_cases m (length H)) as [Hlt|Hge].
      * rewrite Pre in Em; auto.
        destruct (Iheap m om Em (fun Hc => Hm (in_or_app _ _ _ (or_intror Hc))) (live_app Σ _ m ltac:(lia) Hlv))
          as [(h' & Eh & Okh) Hr'].
        split.
        -- exists h'. split; [unfold Σ'; rewrite nth_error_app1; auto; lia|].
           eapply obj_ok_agree with (X := []); eauto using sagree_app_l, scope_ext_app_l.
        -- intros r Hr Hc. apply in_app_or in Hc as [Hc|Hc].
           ++ apply Rg in Hc. specialize (B m om Em _ Hr). lia.
           ++ exact (Hr' r Hr Hc).
      * exfalso. apply Hm. apply in_or_app. left. apply Rg.
        apply nth_error_lt in Em. rewrite length_app in Em. lia.
    + intros m Hm. apply in_app_or in Hm as [Hm|Hm].
      * apply Rg in Hm. unfold Σ'.
        rewrite nth_error_app2 by lia. apply nth_error_repeat. lia.
      * pose proof (Ireg m Hm) as E.
        unfold Σ'. rewrite nth_error_app1; auto. eapply nth_error_lt; eauto.
    + unfold Σ'. rewrite nth_error_app1; auto. eapply nth_error_lt; eauto.
  - intros m om Em r Hr. rewrite length_app.
    destruct (Nat.lt_ge_cases m (length H)) as [Hlt|Hge].
    + rewrite Pre in Em; auto. specialize (B m om Em r Hr). lia.
    + assert (Hm : In m O) by (apply Rg; apply nth_error_lt in Em; rewrite length_app in Em; lia).
      destruct (Oo m Hm) as (o & Eo & _ & Ho). rewrite Em in Eo. inversion Eo; subst.
      apply Rg. auto.
Qed.

Lemma dtypeds_app Σ H vs1 vs2 t Os1 Os2 :
  dtypeds sigs Σ H vs1 t Os1 -> dtypeds sigs Σ H vs2 t Os2 -> dtypeds sigs Σ H (vs1 ++ vs2) t (Os1 ++ Os2).
Proof. induction 1; simpl; auto. intros; constructor; auto. Qed.

(** Appending a fresh value to a fresh list: the two regions merge. *)
Lemma inv_push_dp Σ H sc G x l L t st Ox Ol Os :
  inv sigs Σ H sc G (x :: VLoc l :: L) ((Dp, t) :: (Dp, TList t) :: st) (Ox :: Ol :: Os) ->
  bounded H ->
  exists vs O', nth_error H l = Some (OList vs) /\
    inv sigs Σ (set_nth l (OList (vs ++ [x])) H) sc G (VLoc l :: L) ((Dp, TList t) :: st) (O' :: Os) /\
    bounded (set_nth l (OList (vs ++ [x])) H).
Proof.
  intros I B. pose proof (inv_slot_lt _ _ _ _ _ _ _ I) as Slt.
  destruct I as [Ilen Islots Idisj Iown Iheap Ireg Iscope].
  inversion Islots as [|? ? ? ? ? ? Px F1]; subst. inversion F1 as [|? ? ? ? ? ? Pl F2]; subst.
  unfold slot_ok in Px, Pl; simpl in Px, Pl.
  apply dt_list_inv in Pl as (l' & vs & Osl & El & E & Ds & -> & N). inversion El; subst l'.
  simpl in Idisj.
  (* disjointness facts *)
  assert (Nx : ~ In l Ox).
  { intro Hc. apply nodup_app_inv in Idisj as (_ & _ & D). apply (D l Hc). apply in_eq. }
  set (H' := set_nth l (OList (vs ++ [x])) H).
  assert (Hlt : l < length H) by (eapply nth_error_lt; eauto).
  assert (Agr : forall m, m <> l -> nth_error H' m = nth_error H m).
  { intros m Hm. apply nth_error_set_nth_neq; auto. }
  inversion N as [|? ? Nl Nd]; subst.
  exists vs, (l :: concat (Osl ++ [Ox])). split; auto. split.
  - constructor; simpl.
    + unfold H'; rewrite set_nth_length; auto.
    + constructor.
      * unfold slot_ok; simpl. apply dt_list with (vs := vs ++ [x]).
        -- apply nth_error_set_nth_eq; auto.
        -- apply dtypeds_app.
           ++ eapply (proj1 (proj2 (dtyped_agree sigs Σ H Σ H' (scope_ext_refl Σ)))); eauto.
              intros m Hm. apply Agr. intro; subst; contradiction.
           ++ constructor; [|constructor]. eapply dtyped_agree1; eauto using scope_ext_refl.
              intros m Hm. apply Agr. intro; subst; contradiction.
        -- rewrite concat_app. simpl. rewrite app_nil_r. constructor.
           ++ intro Hc. apply in_app_or in Hc as [Hc|Hc]; [contradiction|]. apply Nx; auto.
           ++ apply nodup_app_inv in Idisj as (N1 & N2 & D).
              inversion N2 as [|? ? _ N3]; subst. apply nodup_app_inv in N3 as (N4 & _ & _).
              apply NoDup_app; auto.
              intros a Ha Ha'. apply (D a Ha'). apply in_cons. apply in_or_app. left. auto.
      * eapply Forall3_impl_in; [| exact F2]. intros w p Ow _ HOw Hs.
        eapply slot_ok_keep; [exact Hs | apply keeps_live_refl |].
        intros m Hm. apply Agr. intro; subst.
        apply nodup_app_inv in Idisj as (_ & N2 & _).
        inversion N2 as [|? ? Nl' _]; subst. apply Nl'. apply in_or_app. right. apply in_concat. eauto.
    + rewrite concat_app. simpl. rewrite app_nil_r.
      eapply Permutation_NoDup; [| exact Idisj].
      simpl. rewrite <- app_assoc.
      change (l :: concat Osl ++ concat Os) with ((l :: concat Osl) ++ concat Os).
      change (l :: concat Osl ++ Ox ++ concat Os) with ((l :: concat Osl) ++ Ox ++ concat Os).
      apply perm_mid.
    + constructor.
      * intros m [<-|[]] _. apply in_eq.
      * inversion Iown as [|? ? ? ? ? ? _ G1]; subst. inversion G1 as [|? ? ? ? ? ? _ G2]; subst.
        eapply Forall3_impl; [| exact G2]. simpl. intros w _ Ow Hw m Hm Hc. apply Hw; auto.
        rewrite concat_app in Hc. simpl in Hc. rewrite app_nil_r in Hc.
        insolve.
    + intros m om Em Hm Hlv. rewrite concat_app in Hm. simpl in Hm. rewrite app_nil_r in Hm.
      destruct (Nat.eq_dec m l) as [->|Hne]; [exfalso; apply Hm; left; auto|].
      rewrite Agr in Em; auto.
      destruct (Iheap m om Em) as [Ho Hr]; [| exact Hlv |].
      { intro Hc. apply Hm. insolve. }
      split; auto. intros r Hr' Hc. apply (Hr r Hr'). rewrite concat_app in Hc. simpl in Hc.
      rewrite app_nil_r in Hc. insolve.
    + intros m Hm. apply Ireg. rewrite concat_app in Hm. simpl in Hm. rewrite app_nil_r in Hm.
      insolve.
    + exact Iscope.
  - apply bounded_update; auto. intros r Hr. simpl in Hr.
    rewrite flat_map_app in Hr. apply in_app_or in Hr as [Hr|Hr].
    + eapply B; eauto.
    + simpl in Hr. rewrite app_nil_r in Hr. apply (Slt x (in_eq _ _)); auto.
Qed.
(** ** Enum values: several payload slots become one slot, and back *)

Lemma inv_pop_shl Σ H sc G : forall S1 L ts st Os,
  length S1 = length ts ->
  inv sigs Σ H sc G (S1 ++ L) (shs ts ++ st) Os ->
  exists Os', Os = map (fun _ => []) S1 ++ Os' /\ vtypedl sigs Σ S1 ts /\
    (forall l, In l (flat_map vlocs S1) -> ~ In l (concat Os')) /\ inv sigs Σ H sc G L st Os'.
Proof.
  induction S1 as [|v S1 IH]; intros L [|t ts] st Os Ln I; simpl in *; try lia.
  - exists Os. split; [reflexivity|]. split; [constructor|]. split; [simpl; tauto | exact I].
  - destruct Os as [|O Os].
    { pose proof (inv_slots _ _ _ _ _ _ _ _ I) as F. inversion F. }
    apply inv_pop_sh in I as (-> & Hv & Hl & I).
    destruct (IH L ts st Os ltac:(lia) I) as (Os' & -> & Vl & Hl' & I').
    exists Os'. split; [reflexivity|]. split; [constructor; auto|]. split; [|exact I'].
    intros l Hin. apply in_app_or in Hin as [Hin|Hin]; auto.
      intro Hc. apply (Hl l Hin). rewrite concat_nils. exact Hc.
Qed.

Lemma inv_push_shl Σ H sc G L st Os : forall vs ts,
  inv sigs Σ H sc G L st Os -> vtypedl sigs Σ vs ts ->
  (forall l, In l (flat_map vlocs vs) -> ~ In l (concat Os)) ->
  inv sigs Σ H sc G (vs ++ L) (shs ts ++ st) (map (fun _ => []) vs ++ Os).
Proof.
  intros vs ts I Vl. induction Vl as [|v vs t ts Hv Vl IH]; simpl; intros Hl; auto.
  apply inv_push_sh.
  - apply IH. intros l Hin. apply Hl. apply in_or_app; auto.
  - exact Hv.
  - rewrite concat_nils. intros l Hin. apply Hl. apply in_or_app; auto.
Qed.

Lemma Forall3_in_a {A B C} (P : A -> B -> C -> Prop) la lb lc a :
  Forall3 P la lb lc -> In a la -> exists b c, In c lc /\ P a b c.
Proof.
  induction 1; simpl; [tauto|]. intros [<-|Hin]; [eauto|].
  destruct (IHForall3 Hin) as (b' & c' & Hc & Hp). eauto.
Qed.

Lemma slots_dp_dtypedl Σ H : forall S1 ts Os1,
  Forall3 (slot_ok sigs Σ H) S1 (marks Dp ts) Os1 -> dtypedl sigs Σ H S1 ts Os1.
Proof.
  intros S1 ts. revert S1. induction ts as [|t ts IH]; intros S1 Os1 F; simpl in F;
    inversion F; subst; constructor; auto.
Qed.

Lemma dtypedl_slots_dp Σ H : forall S1 ts Os1,
  dtypedl sigs Σ H S1 ts Os1 -> Forall3 (slot_ok sigs Σ H) S1 (marks Dp ts) Os1.
Proof. induction 1; simpl; constructor; auto. Qed.

(** Fresh payload slots become one fresh slot owning all their regions. *)
Lemma inv_merge_top Σ H sc G S1 L ts st Os1 Os v t :
  length S1 = length ts -> length ts = length Os1 ->
  inv sigs Σ H sc G (S1 ++ L) (marks Dp ts ++ st) (Os1 ++ Os) ->
  vlocs v = flat_map vlocs S1 ->
  (dtypedl sigs Σ H S1 ts Os1 -> NoDup (concat Os1) -> dtyped sigs Σ H v t (concat Os1)) ->
  inv sigs Σ H sc G (v :: L) ((Dp, t) :: st) (concat Os1 :: Os).
Proof.
  intros L1 L2 [Ilen Islots Idisj Iown Iheap Ireg Iscope] Ev Hd.
  assert (Lm : length S1 = length (marks Dp ts)) by (unfold marks; rewrite length_map; auto).
  assert (Lm' : length (marks Dp ts) = length Os1) by (unfold marks; rewrite length_map; auto).
  apply Forall3_split in Islots as [F1 F2]; auto.
  apply Forall3_split in Iown as [G1 G2]; auto.
  rewrite concat_app in *.
  apply nodup_app_inv in Idisj as (N1 & N2 & D).
  constructor; simpl; auto.
  - constructor; auto. apply Hd; auto. apply slots_dp_dtypedl; auto.
  - apply NoDup_app; auto.
  - constructor; auto. intros l Hl Hc. rewrite Ev in Hl.
    apply in_flat_map in Hl as (w & Hw & Hl).
    destruct (Forall3_in_a _ _ _ _ w G1 Hw) as (p & O & HO & Ho).
    apply in_concat. exists O. split; auto.
Qed.

Lemma dtypedl_own Σ H X : forall vs ts Os1, dtypedl sigs Σ H vs ts Os1 ->
  Forall3 (fun v _ O => forall l, In l (vlocs v) -> In l X -> In l O) vs (marks Dp ts) Os1.
Proof.
  induction 1 as [|w ws t ts O Os1 D Dl IH]; simpl; constructor; auto.
  destruct (proj1 (dtyped_struct sigs Σ H) _ _ _ D) as (_ & Hv & _). intros l Hl _. auto.
Qed.

(** One fresh slot whose value's payloads own disjoint parts of its region
    becomes one fresh slot per payload. *)
Lemma inv_split_top Σ H sc G v L t st Os vs ts Os1 :
  inv sigs Σ H sc G (v :: L) ((Dp, t) :: st) (concat Os1 :: Os) ->
  dtypedl sigs Σ H vs ts Os1 ->
  inv sigs Σ H sc G (vs ++ L) (marks Dp ts ++ st) (Os1 ++ Os).
Proof.
  intros [Ilen Islots Idisj Iown Iheap Ireg Iscope] Dl.
  inversion Islots as [|? ? ? ? ? ? _ F]; subst. inversion Iown as [|? ? ? ? ? ? _ G1]; subst.
  simpl in *. constructor; try rewrite concat_app; auto.
  - apply Forall3_app; auto. apply dtypedl_slots_dp; auto.
  - apply Forall3_app; auto. eapply dtypedl_own; eauto.
Qed.

(** Moving the top slot down, below [k] others. *)
Lemma in_concat_move {A} (O : list A) Os1 Os2 x :
  In x (concat (Os1 ++ O :: Os2)) <-> In x (concat (O :: Os1 ++ Os2)).
Proof. simpl. rewrite !concat_app, !in_app_iff. simpl. rewrite !in_app_iff. tauto. Qed.

Lemma inv_move_top Σ H sc G v L1 L2 p s1 s2 O Os1 Os2 :
  length L1 = length s1 -> length s1 = length Os1 ->
  inv sigs Σ H sc G (v :: L1 ++ L2) (p :: s1 ++ s2) (O :: Os1 ++ Os2) ->
  inv sigs Σ H sc G (L1 ++ v :: L2) (s1 ++ p :: s2) (Os1 ++ O :: Os2).
Proof.
  intros La Lb [Ilen Islots Idisj Iown Iheap Ireg Iscope].
  inversion Islots as [|? ? ? ? ? ? Pv F]; subst.
  inversion Iown as [|? ? ? ? ? ? Ov G1]; subst.
  apply Forall3_split in F as [F1 F2]; auto.
  apply Forall3_split in G1 as [G1 G2]; auto.
  constructor; auto.
  - apply Forall3_app; auto. constructor; auto.
  - eapply Permutation_NoDup; [| exact Idisj]. simpl. rewrite !concat_app. simpl.
    apply Permutation_sym. apply perm_mid.
  - apply Forall3_app; [| constructor].
    + eapply Forall3_impl; [| exact G1]. intros w q O' Hw l Hl Hc. apply Hw; auto. apply in_concat_move; auto.
    + intros l Hl Hc. apply Ov; auto. apply in_concat_move; auto.
    + eapply Forall3_impl; [| exact G2]. intros w q O' Hw l Hl Hc. apply Hw; auto. apply in_concat_move; auto.
  - intros l o E Hn Hlv. destruct (Iheap l o E) as [Hh Hr]; [| exact Hlv |].
    { intro Hc; apply Hn; apply in_concat_move; auto. }
    split; auto. intros r Hr' Hc. apply (Hr r Hr'). apply in_concat_move; auto.
  - intros l Hl. apply Ireg. apply in_concat_move; auto.
Qed.

End Ops.
