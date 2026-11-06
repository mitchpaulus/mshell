(** * Joining partly new arms

    [join_slot2] (Join.v) gives only shared or fresh slots, so an [if] whose
    arms both leave a new dict holding a stored list,
    [if {a: @xs} else {a: @xs, b: [2]} end], had no join a declared type
    could take: the dicts were committed, and a shared [{a: [int]}] is not
    below [{a?: [int], b?: [int]}].

    This file is the checker's join for such arms.  The checker's marks
    are finite ([fmark]): a list's element mark, or a dict's marks for the
    labels it lists and one for the rest.  [den] gives the model's mark.
    The join first gives both arms one mark ([fmerge]): the arms' marks
    where a label holds a value in only one arm, and the merge of both
    where it holds one in each; a new arm counts as a partly new one whose
    positions are all new.  A position stored in one arm and new in the
    other has no merge (it would have to be committed).  Each arm is
    brought to the merged mark by [ss_mc].  Then the types are joined
    position by position under that mark ([mjoin]): [<=] at a stored
    position, the fresh join at a new one, and inside again at a partly new
    one, so each arm reaches the result by [ss_m].  When either step
    fails, the join is [join_slot2]'s.

    [join_slot3_ub] shows each arm reaches the join in two subsumption
    steps; [if_join3] that an [if] joined this way checks.  As in Join.v
    the procedures [le] and [rt] are assumed right only when they say yes;
    the merge and the position-by-position join are proved here. *)

From Stdlib Require Import String List Arith Bool Lia.
Import ListNotations.
From MshellCore Require Import Syntax Subtyping Variance Typing Interp Invariant RtLemmas Validate Generic Join.

(** ** The checker's marks *)

Inductive fmark :=
| FmSh
| FmDp
| FmList (e : fmark)
| FmRec (ls : list (label * fmark)) (d : fmark).

Fixpoint den (m : fmark) : mark :=
  match m with
  | FmSh => Sh
  | FmDp => Dp
  | FmList e => MList (den e)
  | FmRec ls d =>
      MRec (fun k => (fix go (ls : list (label * fmark)) : mark :=
                        match ls with
                        | [] => den d
                        | (k', e) :: ls' => if String.eqb k k' then den e else go ls'
                        end) ls)
  end.

(** The mark of label [k]. *)
Definition flook (k : label) (ls : list (label * fmark)) (d : fmark) : fmark :=
  match lookup k ls with Some e => e | None => d end.

Definition den_f (ls : list (label * fmark)) (d : fmark) : label -> mark :=
  match den (FmRec ls d) with MRec f => f | _ => fun _ => Dp end.

Lemma den_rec ls d : den (FmRec ls d) = MRec (den_f ls d).
Proof. reflexivity. Qed.

Lemma den_f_at ls d k : den_f ls d k = den (flook k ls d).
Proof.
  unfold den_f, flook. simpl. induction ls as [|[k' e] ls IH]; simpl; auto.
  destruct (String.eqb k k'); auto.
Qed.

Definition fpartial (m : fmark) : bool :=
  match m with FmList _ | FmRec _ _ => true | FmSh | FmDp => false end.

Lemma fpartial_den m : partial (den m) = fpartial m.
Proof. destruct m; reflexivity. Qed.

(** A new list or dict as a partly new one whose positions are all new. *)
Definition aslist (m : fmark) : option fmark :=
  match m with FmDp => Some FmDp | FmList e => Some e | _ => None end.

Definition asrec (m : fmark) : option (list (label * fmark) * fmark) :=
  match m with FmDp => Some ([], FmDp) | FmRec ls d => Some (ls, d) | _ => None end.

Lemma aslist_mc m e m' t : aslist m = Some e ->
  mcompat (MList (den e)) m' (TList t) -> mcompat (den m) m' (TList t).
Proof.
  destruct m; simpl; intros E H; inversion E; subst; auto.
  apply mc_dp_list. exact H.
Qed.

Lemma asrec_mc m ls d m' fs r : asrec m = Some (ls, d) ->
  mcompat (MRec (den_f ls d)) m' (TRec fs r) -> mcompat (den m) m' (TRec fs r).
Proof.
  destruct m; simpl; intros E H; inversion E; subst; auto.
  apply mc_dp_rec. exact H.
Qed.

Lemma omapl_lookupA {A} (F : string -> option A) : forall keys xs,
  omapl (fun k => option_map (fun x => (k, x)) (F k)) keys = Some xs ->
  map fst xs = keys /\ forall k v, lookup k xs = Some v -> F k = Some v.
Proof.
  induction keys as [|k0 keys IH]; simpl; intros xs E.
  - inversion E; subst. split; [reflexivity | intros k v H; discriminate].
  - destruct (F k0) as [v0|] eqn:Ek; simpl in E; [|discriminate].
    destruct (omapl _ keys) as [xs'|] eqn:Er; [|discriminate]. inversion E; subst.
    destruct (IH xs' eq_refl) as [Hk Hl]. split; [simpl; f_equal; auto|].
    intros k v H. simpl in H. destruct (String.eqb_spec k k0); [subst; inversion H; subst; auto | auto].
Qed.

Lemma lookup_notin {A} k (l : list (string * A)) : ~ In k (map fst l) -> lookup k l = None.
Proof. intros H. apply lookup_none_iff. exact H. Qed.

(** ** One mark for both arms *)

(** The merged mark of one label, from the arms' fields there. *)
Definition fmat (j : fmark -> fmark -> ty -> ty -> option fmark) (f1 f2 : fstat) (m1 m2 : fmark)
  : option fmark :=
  match f1, f2 with
  | FAbs, _ => Some m2
  | _, FAbs => Some m1
  | _, _ => j m1 m2 (fty f1) (fty f2)
  end.

Definition flat_eqb (m1 m2 : fmark) : bool :=
  match m1, m2 with FmSh, FmSh | FmDp, FmDp => true | _, _ => false end.

Lemma flat_eqb_eq m1 m2 : flat_eqb m1 m2 = true -> m1 = m2.
Proof. destruct m1, m2; simpl; congruence. Qed.

Fixpoint fmerge (n : nat) (m1 m2 : fmark) (a b : ty) {struct n} : option fmark :=
  match n with
  | 0 => None
  | S n' =>
    if flat_eqb m1 m2 then Some m1 else
    match a, b with
    | TList x, TList y =>
        match aslist m1, aslist m2 with
        | Some e1, Some e2 => option_map FmList (fmerge n' e1 e2 x y)
        | _, _ => None
        end
    | TRec fs1 r1, TRec fs2 r2 =>
        match asrec m1, asrec m2 with
        | Some (ls1, d1), Some (ls2, d2) =>
            match omapl (fun k => option_map (fun e => (k, e))
                           (fmat (fmerge n') (field_at k fs1 r1) (field_at k fs2 r2)
                                 (flook k ls1 d1) (flook k ls2 d2)))
                        (map fst ls1 ++ map fst ls2 ++ map fst fs1 ++ map fst fs2),
                  fmat (fmerge n') r1 r2 d1 d2 with
            | Some ls, Some d => Some (FmRec ls d)
            | _, _ => None
            end
        | _, _ => None
        end
    | _, _ => None
    end
  end.

Lemma fmat_ok j f1 f2 m1 m2 m :
  (forall e1 e2 x y e, j e1 e2 x y = Some e -> mcompat (den e1) (den e) x /\ mcompat (den e2) (den e) y) ->
  fmat j f1 f2 m1 m2 = Some m -> mcompat (den m1) (den m) (fty f1) /\ mcompat (den m2) (den m) (fty f2).
Proof.
  intros J E. destruct f1, f2; simpl in E; try (inversion E; subst; split; constructor; fail); auto.
Qed.

(** Both arms' marks change to the merged one where they hold values. *)
Lemma fmerge_ok : forall n m1 m2 a b m, fmerge n m1 m2 a b = Some m ->
  mcompat (den m1) (den m) a /\ mcompat (den m2) (den m) b.
Proof.
  induction n as [|n IH]; intros m1 m2 a b m E; simpl in E; [discriminate|].
  destruct (flat_eqb m1 m2) eqn:Fe.
  { apply flat_eqb_eq in Fe. subst. inversion E; subst. split; constructor. }
  destruct a as [| | | | | | x1 | fs1 r1 | u1 v1 | i1 o1 | E1 g1 | p1 | w1 | t1 | n1]; try discriminate; destruct b as [| | | | | | x2 | fs2 r2 | u2 v2 | i2 o2 | E2 g2 | p2 | w2 | t2 | n2]; try discriminate.
  - destruct (aslist m1) as [e1|] eqn:A1; [|discriminate].
    destruct (aslist m2) as [e2|] eqn:A2; [|discriminate].
    destruct (fmerge n e1 e2 x1 x2) as [e|] eqn:Ee; simpl in E; [|discriminate].
    inversion E; subst. destruct (IH _ _ _ _ _ Ee) as [H1 H2]. simpl.
    split; [eapply aslist_mc; [exact A1|] | eapply aslist_mc; [exact A2|]]; apply mc_list; auto.
  - destruct (asrec m1) as [[ls1 d1]|] eqn:A1; [|discriminate].
    destruct (asrec m2) as [[ls2 d2]|] eqn:A2; [|discriminate].
    match type of E with context [omapl ?F ?K] => destruct (omapl F K) as [ls|] eqn:Eo end;
      [|discriminate].
    destruct (fmat (fmerge n) r1 r2 d1 d2) as [d|] eqn:Ed; [|discriminate].
    inversion E; subst. clear E.
    apply omapl_lookupA in Eo as [Hk Hl].
    assert (Hf : forall k, fmat (fmerge n) (field_at k fs1 r1) (field_at k fs2 r2) (flook k ls1 d1) (flook k ls2 d2)
                           = Some (flook k ls d)).
    { intros k. unfold flook at 3. destruct (lookup k ls) as [v|] eqn:Ev.
      - apply Hl in Ev. exact Ev.
      - apply lookup_none_iff in Ev. rewrite Hk in Ev.
        rewrite !field_at_notin; [| intro Hc; apply Ev; rewrite !in_app_iff; tauto
                                  | intro Hc; apply Ev; rewrite !in_app_iff; tauto].
        unfold flook. rewrite !lookup_notin; [exact Ed | intro Hc; apply Ev; rewrite !in_app_iff; tauto
                                              | intro Hc; apply Ev; rewrite !in_app_iff; tauto]. }
    assert (J : forall e1 e2 x y e, fmerge n e1 e2 x y = Some e ->
                mcompat (den e1) (den e) x /\ mcompat (den e2) (den e) y) by (intros; eapply IH; eauto).
    rewrite den_rec. split.
    + eapply asrec_mc; [exact A1|]. apply mc_rec. intros k. rewrite !den_f_at.
      exact (proj1 (fmat_ok _ _ _ _ _ _ J (Hf k))).
    + eapply asrec_mc; [exact A2|]. apply mc_rec. intros k. rewrite !den_f_at.
      exact (proj2 (fmat_ok _ _ _ _ _ _ J (Hf k))).
Qed.

(** ** Joining the types under one mark *)

(** The join of one label under a mark: as [fjoin], but a label present in
    one arm only is joined with itself, since a partly new value is not
    always [msub] of itself. *)
Definition mfjoin (j : ty -> ty -> option ty) (f g : fstat) : option fstat :=
  match f, g with
  | FOpen, _ | _, FOpen => Some FOpen
  | FAbs, FAbs => Some FAbs
  | FReq x, FReq y => option_map FReq (j x y)
  | FDict x, FDict y => option_map FDict (j x y)
  | FAbs, FDict y | FDict y, FAbs => option_map FDict (j y y)
  | FAbs, (FReq y | FOpt y) | (FReq y | FOpt y), FAbs => option_map FOpt (j y y)
  | FReq x, (FOpt y | FDict y) | FOpt x, (FReq y | FOpt y | FDict y) | FDict x, (FReq y | FOpt y) =>
      option_map FOpt (j x y)
  end.

Lemma mfjoin_ub (R : ty -> ty -> Prop) j f g h :
  (forall x y z, j x y = Some z -> R x z /\ R y z) ->
  mfjoin j f g = Some h -> frsubR R f h /\ frsubR R g h.
Proof.
  intros J E.
  destruct f, g; simpl in E;
    try (inversion E; subst; split; constructor; fail);
    try (match type of E with context [j ?x ?y] => destruct (j x y) as [z|] eqn:Ej end;
         simpl in E; inversion E; subst; destruct (J _ _ _ Ej) as [H1 H2]).
  all: try (split; constructor; auto; fail).
  all: try (split; eapply frs_opt; eauto; fail).
  all: try (split; eapply frs_dict; eauto; fail).
  all: try (split; [apply frs_abs_opt | eapply frs_opt; eauto]; fail).
  all: try (split; [eapply frs_opt; eauto | apply frs_abs_opt]; fail).
  all: try (split; [apply frs_abs_dict | eapply frs_dict; eauto]; fail).
  all: try (split; [eapply frs_dict; eauto | apply frs_abs_dict]; fail).
Qed.

Definition is_open (f : fstat) : bool := match f with FOpen => true | _ => false end.

(** A label holding a partly new value is not made open ([msub]). *)
Definition mfield (j : fmark -> ty -> ty -> option ty) (e : fmark) (f g : fstat) : option fstat :=
  match mfjoin (j e) f g with
  | Some h => if fpartial e && is_open h then None else Some h
  | None => None
  end.

Section MJoin.
Variable le : bool -> ty -> ty -> bool.
Hypothesis le_ok : forall fr a b, le fr a b = true -> jrel fr a b.

Fixpoint mjoin (n : nat) (m : fmark) (a b : ty) {struct n} : option ty :=
  match n with
  | 0 => None
  | S n' =>
    match m with
    | FmSh => tjoin le (S (dsize a + dsize b)) false a b
    | FmDp => tjoin le (S (dsize a + dsize b)) true a b
    | FmList e =>
        match a, b with
        | TList x, TList y => option_map TList (mjoin n' e x y)
        | _, _ => None
        end
    | FmRec ls d =>
        match a, b with
        | TRec fs1 r1, TRec fs2 r2 =>
            match omapl (fun k => option_map (fun h => (k, h))
                           (mfield (mjoin n') (flook k ls d) (field_at k fs1 r1) (field_at k fs2 r2)))
                        (map fst ls ++ map fst fs1 ++ map fst fs2),
                  mfield (mjoin n') d r1 r2 with
            | Some fs, Some r => Some (TRec fs r)
            | _, _ => None
            end
        | _, _ => None
        end
    end
  end.

Lemma mfield_ub j e f g h :
  (forall x y z, j e x y = Some z -> msub (den e) x z /\ msub (den e) y z) ->
  mfield j e f g = Some h ->
  frsubR (msub (den e)) f h /\ frsubR (msub (den e)) g h /\ (partial (den e) = true -> h <> FOpen).
Proof.
  intros J E. unfold mfield in E.
  destruct (mfjoin (j e) f g) as [h'|] eqn:Em; [|discriminate].
  destruct (fpartial e && is_open h') eqn:Po; [discriminate|]. inversion E; subst.
  destruct (mfjoin_ub _ _ _ _ _ J Em) as [H1 H2]. split; [exact H1|]. split; [exact H2|].
  intros P Eo. subst. rewrite fpartial_den in P. rewrite P in Po. discriminate.
Qed.

Lemma msub_rec_intro f fs1 r1 fs r :
  (forall k, frsubR (msub (f k)) (field_at k fs1 r1) (field_at k fs r) /\
             (partial (f k) = true -> field_at k fs r <> FOpen)) ->
  msub (MRec f) (TRec fs1 r1) (TRec fs r).
Proof. intros H. simpl. do 4 eexists. split; [reflexivity|]. split; [reflexivity|]. exact H. Qed.

(** The join is above both arms by [msub] under the mark. *)
Lemma mjoin_ub : forall n m a b c, mjoin n m a b = Some c -> msub (den m) a c /\ msub (den m) b c.
Proof.
  induction n as [|n IH]; intros m a b c E; simpl in E; [discriminate|].
  destruct m as [| | e | ls d].
  - exact (tjoin_ub le le_ok (S (dsize a + dsize b)) false a b c E).
  - exact (tjoin_ub le le_ok (S (dsize a + dsize b)) true a b c E).
  - destruct a as [| | | | | | x1 | fs1 r1 | u1 v1 | i1 o1 | E1 g1 | p1 | w1 | t1 | n1]; try discriminate; destruct b as [| | | | | | x2 | fs2 r2 | u2 v2 | i2 o2 | E2 g2 | p2 | w2 | t2 | n2]; try discriminate.
    destruct (mjoin n e x1 x2) as [z|] eqn:Ez; simpl in E; [|discriminate]. inversion E; subst.
    destruct (IH _ _ _ _ Ez) as [H1 H2]. simpl.
    split; do 2 eexists; (split; [reflexivity | split; [reflexivity | eassumption]]).
  - destruct a as [| | | | | | x1 | fs1 r1 | u1 v1 | i1 o1 | E1 g1 | p1 | w1 | t1 | n1]; try discriminate; destruct b as [| | | | | | x2 | fs2 r2 | u2 v2 | i2 o2 | E2 g2 | p2 | w2 | t2 | n2]; try discriminate.
    match type of E with context [omapl ?F ?K] => destruct (omapl F K) as [fs|] eqn:Eo end;
      [|discriminate].
    destruct (mfield (mjoin n) d r1 r2) as [r|] eqn:Er; [|discriminate].
    inversion E; subst. clear E.
    apply omapl_lookupA in Eo as [Hk Hl].
    assert (Hf : forall k, mfield (mjoin n) (flook k ls d) (field_at k fs1 r1) (field_at k fs2 r2)
                           = Some (field_at k fs r)).
    { intros k. unfold field_at at 3. destruct (lookup k fs) as [v|] eqn:Ev.
      - apply Hl in Ev. exact Ev.
      - apply lookup_none_iff in Ev. rewrite Hk in Ev.
        rewrite !field_at_notin; [| intro Hc; apply Ev; rewrite !in_app_iff; tauto
                                  | intro Hc; apply Ev; rewrite !in_app_iff; tauto].
        unfold flook. rewrite lookup_notin; [exact Er | intro Hc; apply Ev; rewrite !in_app_iff; tauto]. }
    assert (J : forall e x y z, mjoin n e x y = Some z -> msub (den e) x z /\ msub (den e) y z)
      by (intros; eapply IH; eauto).
    rewrite den_rec. split; apply msub_rec_intro; intros k; rewrite den_f_at;
      destruct (mfield_ub _ _ _ _ _ (J (flook k ls d)) (Hf k)) as (H1 & H2 & H3); auto.
Qed.
End MJoin.

(** ** The checker's join *)

Definition fslot := (fmark * ty)%type.
Definition fden (p : fslot) : slot := (den (fst p), snd p).

(** The marks [join_slot2] gives back. *)
Definition of_mark (m : mark) : option fmark :=
  match m with Sh => Some FmSh | Dp => Some FmDp | _ => None end.

Lemma of_mark_den m fm : of_mark m = Some fm -> den fm = m.
Proof. destruct m; simpl; intros E; inversion E; reflexivity. Qed.

Section Join3.
Variable le : bool -> ty -> ty -> bool.
Hypothesis le_ok : forall fr a b, le fr a b = true -> jrel fr a b.
Variable rt : mark -> ty -> ty -> bool.
Hypothesis rt_ok : forall m a c, rt m a c = true ->
  (m = Dp /\ rsub a c) \/
  (partial m = true /\ ((exists b, msub m a b /\ sub b c) \/ sub a c)).

(** Fuel for the merge and the join: the checker recurses on the marks. *)
Fixpoint fmsize (m : fmark) : nat :=
  match m with
  | FmSh | FmDp => 1
  | FmList e => S (fmsize e)
  | FmRec ls d => S (fmsize d + list_sum (map (fun p => fmsize (snd p)) ls))
  end.

Definition pjoin (p q : fslot) : option fslot :=
  let '(m1, a) := p in
  let '(m2, b) := q in
  let n := S (fmsize m1 + fmsize m2 + dsize a + dsize b) in
  if fpartial m1 || fpartial m2 then
    match fmerge n m1 m2 a b with
    | Some m => if fpartial m then option_map (fun c => (m, c)) (mjoin le n m a b) else None
    | None => None
    end
  else None.

Definition join_slot3 (p q : fslot) : option fslot :=
  match pjoin p q with
  | Some r => Some r
  | None =>
      match join_slot2 le rt (fden p) (fden q) with
      | Some (m, c) => option_map (fun fm => (fm, c)) (of_mark m)
      | None => None
      end
  end.

Fixpoint join_stack3 (s1 s2 : list fslot) : option (list fslot) :=
  match s1, s2 with
  | [], [] => Some []
  | p :: s1', q :: s2' =>
      match join_slot3 p q, join_stack3 s1' s2' with Some r, Some rs => Some (r :: rs) | _, _ => None end
  | _, _ => None
  end.

Lemma pjoin_ub p q r : pjoin p q = Some r ->
  exists p' q', slot_sub (fden p) p' /\ slot_sub p' (fden r) /\ slot_sub (fden q) q' /\ slot_sub q' (fden r).
Proof.
  destruct p as [m1 a], q as [m2 b]. unfold pjoin.
  destruct (fpartial m1 || fpartial m2); [|discriminate].
  destruct (fmerge _ m1 m2 a b) as [m|] eqn:Em; [|discriminate].
  destruct (fpartial m) eqn:Pm; [|discriminate].
  destruct (mjoin le _ m a b) as [c|] eqn:Ec; simpl; [|discriminate].
  intros E. inversion E; subst. clear E.
  destruct (fmerge_ok _ _ _ _ _ _ Em) as [C1 C2].
  destruct (mjoin_ub le le_ok _ _ _ _ _ Ec) as [S1 S2].
  assert (P : partial (den m) = true) by (rewrite fpartial_den; exact Pm).
  exists (den m, a), (den m, b). unfold fden; simpl.
  repeat split; [apply ss_mc | apply ss_m | apply ss_mc | apply ss_m]; auto.
Qed.

(** The join is reached from each arm in at most two steps. *)
Theorem join_slot3_ub p q r : join_slot3 p q = Some r ->
  exists p' q', slot_sub (fden p) p' /\ slot_sub p' (fden r) /\ slot_sub (fden q) q' /\ slot_sub q' (fden r).
Proof.
  unfold join_slot3. destruct (pjoin p q) as [r0|] eqn:Ep.
  { intros E. inversion E; subst. apply pjoin_ub; auto. }
  destruct (join_slot2 le rt (fden p) (fden q)) as [[m c]|] eqn:J; [|discriminate].
  destruct (of_mark m) as [fm|] eqn:Om; simpl; [|discriminate].
  intros E. inversion E; subst.
  replace (fden (fm, c)) with (m, c) by (unfold fden; simpl; rewrite (of_mark_den _ _ Om); reflexivity).
  exact (join_slot2_ub le le_ok rt rt_ok _ _ _ J).
Qed.

Theorem join_stack3_ub : forall s1 s2 s3, join_stack3 s1 s2 = Some s3 ->
  exists s1' s2', ssub (map fden s1) s1' /\ ssub s1' (map fden s3) /\
                  ssub (map fden s2) s2' /\ ssub s2' (map fden s3).
Proof.
  induction s1 as [|p s1 IH]; intros [|q s2] s3 E; simpl in E; try discriminate.
  - inversion E; subst. exists [], []. repeat split; constructor.
  - destruct (join_slot3 p q) as [r|] eqn:Er; [|discriminate].
    destruct (join_stack3 s1 s2) as [rs|] eqn:Ers; [|discriminate]. inversion E; subst.
    destruct (join_slot3_ub _ _ _ Er) as (p' & q' & A1 & A2 & A3 & A4).
    destruct (IH _ _ Ers) as (s1' & s2' & B1 & B2 & B3 & B4).
    exists (p' :: s1'), (q' :: s2'). simpl. repeat split; constructor; auto.
Qed.

(** An [if] whose arms are joined this way checks in the core. *)
Corollary if_join3 sigs G B C R e1 e2 s s1 s2 s' :
  T sigs G B C R e1 s (map fden s1) -> T sigs G B C R e2 s (map fden s2) -> join_stack3 s1 s2 = Some s' ->
  TW sigs G B C R (WIf e1 e2) ((Sh, TBool) :: s) (map fden s').
Proof.
  intros H1 H2 J. destruct (join_stack3_ub _ _ _ J) as (s1' & s2' & A1 & A2 & A3 & A4).
  apply tw_if; eapply t_sub; [apply ssub_refl | eapply t_sub; [apply ssub_refl | eassumption | eassumption] | eassumption
                             | apply ssub_refl | eapply t_sub; [apply ssub_refl | eassumption | eassumption] | eassumption].
Qed.
End Join3.

(** ** Example

    [if {a: @xs} else {a: @xs, b: [2]} end], with [xs] a stored [[int]]:
    the first arm's mark lists [a] as stored, the second's also [b] as new.
    The join is a partly new [{a: [int], b?: [int]}] with [a] stored,
    and that retypes ([ss_m]) to [{a?: [int], b?: [int]}]. *)
Definition ex_li : ty := TList TInt.
Definition ex_p : fslot := (FmRec [("a"%string, FmSh)] FmDp, TRec [("a"%string, FReq ex_li)] FAbs).
Definition ex_q : fslot :=
  (FmRec [("a"%string, FmSh); ("b"%string, FmDp)] FmDp,
   TRec [("a"%string, FReq ex_li); ("b"%string, FReq ex_li)] FAbs).
Definition ex_R : ty := TRec [("a"%string, FOpt ex_li); ("b"%string, FOpt ex_li)] FAbs.

Example ex_partial_join :
  exists r, join_slot3 le_none (fun _ _ _ => false) ex_p ex_q = Some r /\
    fpartial (fst r) = true /\ slot_sub (fden r) (den (fst r), ex_R).
Proof.
  eexists. split; [vm_compute; reflexivity|]. split; [reflexivity|].
  apply ss_m; [reflexivity|]. simpl.
  do 4 eexists. split; [reflexivity|]. split; [reflexivity|]. intros k.
  unfold field_at. simpl.
  destruct (String.eqb k "a"%string); simpl; [|destruct (String.eqb k "b"%string); simpl];
    (split; [| intros P; discriminate P]);
    first [ apply frs_abs
          | eapply frs_opt; [eauto | apply s_refl]
          | eapply frs_opt; [eauto | apply rs_sub, s_refl] ].
Qed.
