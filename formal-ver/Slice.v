(** * [take], [skip] and slices of a fresh list.

    These words return a new list over a run of the input's elements.  When
    the input was fresh, the result is fresh too: the kept elements'
    subtrees move into the new list's region.  The old list stays in the
    heap, still pointing at them, and so does every element it dropped, but
    the program can no longer reach any of it: the input slot was consumed,
    and a region is referenced only by its own slot.  Those objects become
    *dead* ([kill]): [Σ] gives them [HDead], and the invariant asks nothing
    of them ([inv_heap] is about live objects only).

    [dead_unreachable] and [vtyped_live] are the reason this is not a
    loophole: nothing on the stack and nothing typed references a dead
    location. *)

From Stdlib Require Import String List Arith Bool Lia Permutation.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Typing Interp Invariant RtLemmas Commit Validate InvOps.

(** Mark the locations [X] dead. *)
Fixpoint kill (X : list loc) (Σ : store_ty) : store_ty :=
  match X with [] => Σ | x :: X' => set_nth x HDead (kill X' Σ) end.

Lemma kill_length X Σ : length (kill X Σ) = length Σ.
Proof. induction X; simpl; rewrite ?set_nth_length; auto. Qed.

Lemma kill_out X Σ l : ~ In l X -> nth_error (kill X Σ) l = nth_error Σ l.
Proof.
  induction X as [|x X IH]; simpl; intros Hn; auto.
  rewrite nth_error_set_nth_neq; [apply IH; tauto | intros ->; apply Hn; left; reflexivity].
Qed.

Lemma kill_in X Σ l : In l X -> l < length Σ -> nth_error (kill X Σ) l = Some HDead.
Proof.
  induction X as [|x X IH]; simpl; intros Hin Hl; [contradiction|].
  destruct (Nat.eq_dec l x) as [->|Hne].
  - apply nth_error_set_nth_eq. rewrite kill_length; auto.
  - rewrite nth_error_set_nth_neq by auto. apply IH; auto. destruct Hin; [congruence | auto].
Qed.

Lemma kill_agree X Σ : sagree Σ (kill X Σ) X.
Proof. intros l h E Hn. rewrite kill_out; auto. Qed.

Section Slice.
Variable sigs : genv.

(** ** Dead locations are unreachable *)

(** A typed value references only live locations. *)
Lemma vtyped_live Σ :
  (forall v t, vtyped sigs Σ v t -> forall l, In l (vlocs v) -> live Σ l) /\
  (forall vs ts, vtypedl sigs Σ vs ts -> forall l, In l (flat_map vlocs vs) -> live Σ l).
Proof.
  apply (vtyped_comb sigs Σ
    (fun v t _ => forall l, In l (vlocs v) -> live Σ l)
    (fun vs ts _ => forall l, In l (flat_map vlocs vs) -> live Σ l)); simpl; try tauto.
  - intros l a t E _ l0 [<-|[]] E'. congruence.
  - intros l fs r t E _ l0 [<-|[]] E'. congruence.
  - intros v vs t ts _ IH _ IH' l Hl. apply in_app_or in Hl as [?|?]; auto.
Qed.

(** So does a typed list or dict object. *)
Lemma obj_ok_live Σ o h :
  obj_ok sigs Σ o h -> is_scope h = false -> forall l, In l (olocs o) -> live Σ l.
Proof.
  intros Ok Ns l Hl. destruct o as [vs|kvs|kvs], h as [t|fs r|G|]; simpl in *; try contradiction;
    try discriminate.
  - apply in_flat_map in Hl as (v & Hv & Hl). rewrite Forall_forall in Ok.
    eapply (proj1 (vtyped_live Σ)); eauto.
  - destruct Ok as (_ & _ & Ok). apply in_flat_map in Hl as (p & Hp & Hl). rewrite Forall_forall in Ok.
    eapply (proj1 (vtyped_live Σ)); eauto.
Qed.

(** No stack value references a dead location. *)
Lemma dead_unreachable Σ H sc G L st Os d :
  inv sigs Σ H sc G L st Os -> nth_error Σ d = Some HDead -> ~ In d (concat Os) ->
  forall v, In v L -> ~ In d (vlocs v).
Proof.
  intros I Ed Nd. pose proof (inv_slots _ _ _ _ _ _ _ _ I) as F. clear I.
  induction F as [|v p O L st Os Pv F IH]; simpl; [tauto|].
  intros w [<-|Hw] Hd.
  - unfold slot_ok in Pv. destruct p as [[|] t]; simpl in Pv.
    + destruct Pv as [Pv _]. exact (proj1 (vtyped_live Σ) _ _ Pv d Hd Ed).
    + destruct (proj1 (dtyped_struct sigs Σ H) _ _ _ Pv) as (_ & Hv & _).
      apply Nd. simpl. apply in_or_app. left. auto.
  - eapply IH; eauto. intro Hc. apply Nd. simpl. apply in_or_app. right. auto.
Qed.

(** ** The fresh case *)

Lemma dtypeds_app_inv Σ H t : forall vs1 vs2 Os, dtypeds sigs Σ H (vs1 ++ vs2) t Os ->
  exists Os1 Os2, Os = Os1 ++ Os2 /\ dtypeds sigs Σ H vs1 t Os1 /\ dtypeds sigs Σ H vs2 t Os2.
Proof.
  induction vs1 as [|v vs1 IH]; simpl; intros vs2 Os D.
  - exists [], Os. repeat split; auto. constructor.
  - inversion D as [|? ? ? O Os' Dv Ds]; subst.
    destruct (IH _ _ Ds) as (Os1 & Os2 & -> & D1 & D2).
    exists (O :: Os1), Os2. repeat split; auto. constructor; auto.
Qed.

(** A new list over the run [mid] of a fresh list's elements is fresh.  Its
    region is the new location and the kept elements' regions; the old list
    and the dropped elements' regions die. *)
Lemma inv_slice_dp Σ H sc G l L t st Ol Os pre mid post :
  inv sigs Σ H sc G (VLoc l :: L) ((Dp, TList t) :: st) (Ol :: Os) -> bounded H ->
  nth_error H l = Some (OList (pre ++ mid ++ post)) ->
  exists Σ' O', scope_ext Σ Σ' /\
    inv sigs Σ' (H ++ [OList mid]) sc G (VLoc (length H) :: L) ((Dp, TList t) :: st) (O' :: Os) /\
    bounded (H ++ [OList mid]).
Proof.
  intros I B El.
  pose proof (inv_slot_lt sigs _ _ _ _ _ _ _ I) as Slt.
  assert (Rlt : forall x, In x (concat (Ol :: Os)) -> x < length H) by (intros; eapply inv_reg_lt; eauto).
  destruct I as [Ilen Islots Idisj Iown Iheap Ireg Iscope].
  pose proof (Forall3_and _ _ _ _ _ Islots Iown) as SO.
  inversion SO as [|? ? ? ? ? ? [Pl _] SO']; subst. clear SO.
  unfold slot_ok in Pl; simpl in Pl.
  apply dt_list_inv in Pl as (l' & vs & Oes & E1 & E2 & Ds & -> & Nd).
  injection E1 as <-. rewrite El in E2. injection E2 as <-.
  apply dtypeds_app_inv in Ds as (Op & Os' & -> & Dp & Ds).
  apply dtypeds_app_inv in Ds as (Om & Oq & -> & Dm & Dq).
  rewrite !concat_app in *. simpl in Idisj, Rlt.
  set (cp := concat Op) in *. set (cm := concat Om) in *. set (cq := concat Oq) in *.
  set (R := concat Os) in *.
  set (X := l :: cp ++ cq).
  set (Σ' := kill X Σ ++ [HList TBot]).
  (* disjointness *)
  assert (Idisj' : NoDup ((l :: cp) ++ cm ++ cq ++ R)) by (simpl; rewrite <- !app_assoc in Idisj; exact Idisj).
  assert (NZ : NoDup (cm ++ X ++ R)).
  { unfold X. simpl. rewrite <- app_assoc. exact (Permutation_NoDup (perm_mid _ _ _) Idisj'). }
  pose proof (nodup_remove_mid _ _ _ NZ) as NmR.
  apply nodup_app_inv in NZ as (Ndm & NXR & Dm_XR).
  apply nodup_app_inv in NXR as (_ & _ & DX_R).
  assert (InOl : forall x, In x (l :: cp ++ cm ++ cq) -> In x X \/ In x cm).
  { intros x Hx. unfold X. simpl in *. rewrite !in_app_iff in *. tauto. }
  assert (XOl : forall x, In x X -> In x (l :: cp ++ cm ++ cq)).
  { intros x Hx. unfold X in Hx. simpl in *. rewrite !in_app_iff in *. tauto. }
  assert (MOl : forall x, In x cm -> In x (l :: cp ++ cm ++ cq)).
  { intros x Hx. simpl. rewrite !in_app_iff. tauto. }
  assert (Old : forall x, In x (l :: cp ++ cm ++ cq) \/ In x R -> In x ((l :: cp ++ cm ++ cq) ++ R)).
  { intros x [Hx|Hx]; apply in_or_app; auto. }
  set (N := length H).
  assert (NR : forall x, In x (cm ++ R) -> x <> N).
  { intros x Hx E. assert (x < length H); [|unfold N in E; lia].
    apply Rlt. apply in_app_or in Hx as [Hx|Hx]; apply Old; auto. }
  (* the new store typing *)
  assert (NsX : nonscope_on Σ X).
  { intros x Hx. apply Ireg. apply Old. left. auto. }
  assert (Sx : scope_ext Σ Σ').
  { eapply scope_ext_trans; [eapply sagree_scope_ext; [apply kill_agree | exact NsX] | apply scope_ext_app]. }
  assert (Agr : sagree Σ Σ' X).
  { intros m h E Hm. unfold Σ'. rewrite nth_error_app1 by (rewrite kill_length; eapply nth_error_lt; eauto).
    rewrite kill_out; auto. }
  assert (Pre : forall m, m < length H -> nth_error (H ++ [OList mid]) m = nth_error H m)
    by (intros; apply nth_error_app1; auto).
  assert (Ekill : forall m, m < length H -> In m X -> nth_error Σ' m = Some HDead).
  { intros m Hm HX. unfold Σ'. rewrite nth_error_app1 by (rewrite kill_length; lia).
    apply kill_in; auto. lia. }
  exists Σ', (N :: cm). split; [exact Sx|]. split.
  - constructor; simpl.
    + unfold Σ'. rewrite !length_app, kill_length. simpl. lia.
    + constructor.
      * unfold slot_ok; simpl. apply dt_list with (vs := mid).
        -- apply nth_error_app_eq.
        -- apply (proj1 (proj2 (dtyped_agree sigs Σ H Σ' (H ++ [OList mid]) Sx))); auto.
           intros m Hm. apply Pre. apply Rlt. apply Old. left. apply MOl. auto.
        -- constructor; auto. intro Hc. apply (NR N); auto. apply in_or_app; auto.
      * eapply Forall3_impl_in; [| exact SO']. intros w p Ow Hw HOw [Hs Ho].
        eapply slot_ok_agree with (X := X); [| exact Agr | exact Sx |].
        -- unfold slot_ok in *. destruct p as [[|] t']; simpl in *; auto.
           eapply dtyped_agree1; [exact Hs | apply scope_ext_refl |].
           intros m Hm. apply Pre. apply Rlt. apply Old. right. apply in_concat. eauto.
        -- intros Hsh x Hx HxX. unfold slot_ok in Hs. rewrite Hsh in Hs. destruct Hs as [_ ->].
           exact (Ho x Hx (Old x (or_introl (XOl x HxX)))).
    + constructor.
      * intro Hc. apply (NR N); auto.
      * exact NmR.
    + constructor.
      * intros x [<-|[]] _. apply in_eq.
      * eapply Forall3_impl_in; [| exact SO']. intros w p Ow Hw HOw [_ Ho] x Hx Hc.
        apply Ho; auto. destruct Hc as [<-|Hc].
        -- exfalso. specialize (Slt w (in_cons _ _ _ Hw) _ Hx). unfold N in *. lia.
        -- apply in_app_or in Hc as [Hc|Hc]; apply Old; auto.
    + intros m om Em Hm Hlv.
      destruct (Nat.lt_ge_cases m (length H)) as [Hlt|Hge].
      * rewrite Pre in Em by auto.
        assert (HmX : ~ In m X) by (intro HX; exact (Hlv (Ekill m Hlt HX))).
        assert (HmO : ~ In m ((l :: cp ++ cm ++ cq) ++ R)).
        { intro Hc. apply in_app_or in Hc as [Hc|Hc].
          - destruct (InOl m Hc) as [Hc'|Hc']; [exact (HmX Hc') | apply Hm; right; apply in_or_app; auto].
          - apply Hm. right. apply in_or_app. auto. }
        destruct (Iheap m om Em HmO (live_back _ _ _ _ Agr HmX Hlv)) as [(h & Eh & Ok) Hr]. split.
        -- exists h. split; [apply Agr; auto|].
           eapply obj_ok_agree; [exact Ok | | exact Agr | exact Sx].
           intros r Hr' HrX. exact (Hr r Hr' (Old r (or_introl (XOl r HrX)))).
        -- intros r Hr' [Er|Hc].
           ++ specialize (B m om Em r Hr'). unfold N in Er. lia.
           ++ apply in_app_or in Hc as [Hc|Hc]; apply (Hr r Hr'); apply Old; auto.
      * exfalso. apply Hm. left. apply nth_error_lt in Em. rewrite length_app in Em. simpl in Em.
        unfold N. lia.
    + intros x [<-|Hx].
      * exists (HList TBot). split; auto. unfold Σ', N.
        rewrite <- Ilen, <- (kill_length X Σ). apply nth_error_app_eq.
      * assert (HxX : ~ In x X).
        { intro HX. apply in_app_or in Hx as [Hx|Hx]; [exact (Dm_XR x Hx (in_or_app _ _ _ (or_introl HX))) |].
          exact (DX_R x HX Hx). }
        destruct (Ireg x) as (h & E & Ns).
        { apply in_app_or in Hx as [Hx|Hx]; apply Old; auto. }
        exists h. split; auto.
    + apply Sx. exact Iscope.
  - apply bounded_app; auto. intros r Hr. assert (r < length H); [|lia].
    eapply B; [exact El|]. simpl in *. rewrite !flat_map_app, !in_app_iff. auto.
Qed.

End Slice.
