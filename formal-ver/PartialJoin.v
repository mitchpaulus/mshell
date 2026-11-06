(** * One join for all the arms

    [join_slot2] (Join.v) joins two arms and gives only shared or fresh
    slots.  Folded over the arms of a [match], it lets the order of the arms
    decide whether they join, and an [if] whose arms both leave a new dict
    holding a stored list, [if {a: @xs} else {a: @xs, b: [2]} end], had no
    join a declared type could take: the dicts were committed, and a shared
    [{a: [int]}] is not below [{a?: [int], b?: [int]}].

    This file is the checker's join of all the arms at once ([joinN];
    ai/n-way-join.md).  The checker's marks are finite ([fmark]): a list's
    element mark, or a dict's marks for the labels it lists and one for the
    rest; [den] gives the model's mark.  Arms of two or more kinds are
    joined kind by kind and the join is the union.  Arms of one kind
    ([joinG]):
    - every arm new: the fresh join, folded;
    - some arm partly new, none stored: one mark for all ([fmergeN]), each
      arm brought to it by [ss_mc], then the types joined under it
      ([mjoin]: [<=] at a stored position, the fresh join at a new one), so
      each arm reaches the result by [ss_m];
    - otherwise shared: a target the new and partly new arms take where they
      can, with the rest committed and joined in ([shjoin]).

    [joinN_stack_ub]: every arm's stack reaches the join by subsumption,
    repeated; [arm_joinN] and [if_joinN] give the rules.  As in Join.v the
    procedures [le] and [rt] are assumed right only when they say yes; the
    merge, the joins under a mark and the targets are proved here. *)

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

(** ** One mark for all the arms *)

Definition flat_eqb (m1 m2 : fmark) : bool :=
  match m1, m2 with FmSh, FmSh | FmDp, FmDp => true | _, _ => false end.

Lemma flat_eqb_eq m1 m2 : flat_eqb m1 m2 = true -> m1 = m2.
Proof. destruct m1, m2; simpl; congruence. Qed.

Lemma omapl_forall2 {A B} (f : A -> option B) : forall l l',
  omapl f l = Some l' -> Forall2 (fun a b => f a = Some b) l l'.
Proof.
  induction l as [|x l IH]; simpl; intros l' E.
  - inversion E; constructor.
  - destruct (f x) as [y|] eqn:Ex; [|discriminate].
    destruct (omapl f l) as [ys|] eqn:El; [|discriminate]. inversion E; subst. constructor; auto.
Qed.

(** Every arm a list: its element mark and element type. *)
Definition all_lists (ps : list (fmark * ty)) : option (list (fmark * ty)) :=
  omapl (fun p => match aslist (fst p), snd p with Some e, TList x => Some (e, x) | _, _ => None end) ps.

Definition arec := (list (label * fmark) * fmark * list (label * fstat) * fstat)%type.

(** Every arm a dict: its marks and its fields. *)
Definition all_recs (ps : list (fmark * ty)) : option (list arec) :=
  omapl (fun p => match asrec (fst p), snd p with
                  | Some (ls, d), TRec fs r => Some (ls, d, fs, r)
                  | _, _ => None
                  end) ps.

(** The arms that hold a value under [k], with its mark and type. *)
Fixpoint at_label (k : label) (rs : list arec) : list (fmark * ty) :=
  match rs with
  | [] => []
  | (ls, d, fs, r) :: rs' =>
      match field_at k fs r with
      | FAbs => at_label k rs'
      | f => (flook k ls d, fty f) :: at_label k rs'
      end
  end.

Fixpoint at_rest (rs : list arec) : list (fmark * ty) :=
  match rs with
  | [] => []
  | (ls, d, fs, r) :: rs' =>
      match r with
      | FAbs => at_rest rs'
      | f => (d, fty f) :: at_rest rs'
      end
  end.

Definition rec_keys (rs : list arec) : list label :=
  flat_map (fun x => match x with (ls, _, fs, _) => map fst ls ++ map fst fs end) rs.

(** [fmergeN n ps]: one mark for arms with marks and types [ps].  Where no
    arm holds a value, any mark will do. *)
Fixpoint fmergeN (n : nat) (ps : list (fmark * ty)) {struct n} : option fmark :=
  match n with
  | 0 => None
  | S n' =>
    match ps with
    | [] => Some FmDp
    | (m1, _) :: rest =>
      if flat_eqb m1 m1 && forallb (fun p => flat_eqb m1 (fst p)) rest then Some m1 else
      match all_lists ps with
      | Some es => option_map FmList (fmergeN n' es)
      | None =>
        match all_recs ps with
        | Some rs =>
            match omapl (fun k => option_map (fun e => (k, e)) (fmergeN n' (at_label k rs))) (rec_keys rs),
                  fmergeN n' (at_rest rs) with
            | Some ls, Some d => Some (FmRec ls d)
            | _, _ => None
            end
        | None => None
        end
      end
    end
  end.

Lemma at_label_in k : forall rs ls d fs r, In (ls, d, fs, r) rs -> field_at k fs r <> FAbs ->
  In (flook k ls d, fty (field_at k fs r)) (at_label k rs).
Proof.
  induction rs as [|[[[ls' d'] fs'] r'] rs IH]; simpl; intros ls d fs r I N; [contradiction|].
  destruct I as [E|I].
  - inversion E; subst. destruct (field_at k fs r); try (left; reflexivity). contradiction.
  - destruct (field_at k fs' r'); try (right; apply IH; auto; fail). apply IH; auto.
Qed.

Lemma at_rest_in : forall rs ls d fs r, In (ls, d, fs, r) rs -> r <> FAbs -> In (d, fty r) (at_rest rs).
Proof.
  induction rs as [|[[[ls' d'] fs'] r'] rs IH]; simpl; intros ls d fs r I N; [contradiction|].
  destruct I as [E|I].
  - inversion E; subst. destruct r; try (left; reflexivity). contradiction.
  - destruct r'; try (right; eapply IH; eauto; fail). eapply IH; eauto.
Qed.

Lemma rec_keys_in k : forall rs ls d fs r, In (ls, d, fs, r) rs -> ~ In k (rec_keys rs) ->
  ~ In k (map fst ls) /\ ~ In k (map fst fs).
Proof.
  induction rs as [|x rs IH]; simpl; intros ls d fs r I N; [contradiction|].
  rewrite in_app_iff in N. destruct I as [E|I].
  - subst. simpl in N. rewrite in_app_iff in N. tauto.
  - apply (IH ls d fs r I). tauto.
Qed.

Lemma at_label_rest k : forall rs, ~ In k (rec_keys rs) -> at_label k rs = at_rest rs.
Proof.
  induction rs as [|[[[ls d] fs] r] rs IH]; simpl; intros N; [reflexivity|].
  rewrite !in_app_iff in N.
  rewrite field_at_notin by tauto. unfold flook. rewrite lookup_notin by tauto.
  rewrite IH by tauto. destruct r; reflexivity.
Qed.

(** Each arm's mark changes to the merged one where the arm holds values. *)
Lemma fmergeN_ok : forall n ps m, fmergeN n ps = Some m ->
  Forall (fun p => mcompat (den (fst p)) (den m) (snd p)) ps.
Proof.
  induction n as [|n IH]; intros ps m E; simpl in E; [discriminate|].
  destruct ps as [|[m1 t1] rest]; [constructor|].
  destruct (flat_eqb m1 m1 && forallb (fun p => flat_eqb m1 (fst p)) rest) eqn:Fl.
  { inversion E; subst. apply andb_true_iff in Fl as [_ Fa]. rewrite forallb_forall in Fa.
    constructor; [apply mc_refl|]. apply Forall_forall. intros [m2 t2] I.
    apply Fa, flat_eqb_eq in I. simpl in *. subst. apply mc_refl. }
  clear Fl. remember ((m1, t1) :: rest) as ps eqn:Eps. clear Eps.
  destruct (all_lists ps) as [es|] eqn:Al.
  { destruct (fmergeN n es) as [e|] eqn:Ee; simpl in E; [|discriminate]. inversion E; subst.
    pose proof (IH _ _ Ee) as Fe. apply omapl_forall2 in Al. clear Ee E. revert Fe.
    induction Al as [|[mp tp] [me te] ps es Hp _ IHa]; intros Fe; constructor.
    - simpl in *. destruct (aslist mp) as [e'|] eqn:A; [|discriminate].
      destruct tp; try discriminate. inversion Hp; subst.
      inversion Fe; subst. eapply aslist_mc; [exact A|]. apply mc_list. auto.
    - inversion Fe; subst. auto. }
  destruct (all_recs ps) as [rs|] eqn:Ar; [|discriminate].
  match type of E with context [omapl ?F ?K] => destruct (omapl F K) as [ls|] eqn:Eo end;
    [|discriminate].
  destruct (fmergeN n (at_rest rs)) as [d|] eqn:Ed; [|discriminate].
  inversion E; subst. clear E.
  apply omapl_lookupA in Eo as [Hk Hl].
  pose proof (IH _ _ Ed) as Fr.
  assert (Hlab : forall k, fmergeN n (at_label k rs) = Some (flook k ls d)).
  { intros k. unfold flook. destruct (lookup k ls) as [v|] eqn:Ev.
    - apply Hl in Ev. exact Ev.
    - apply lookup_none_iff in Ev. rewrite Hk in Ev. rewrite at_label_rest by exact Ev. exact Ed. }
  apply omapl_forall2 in Ar.
  assert (Gen : forall ps' rs', Forall2 (fun p b => (fun p => match asrec (fst p), snd p with
                  | Some (ls, d), TRec fs r => Some (ls, d, fs, r) | _, _ => None end) p = Some b) ps' rs' ->
                (forall x, In x rs' -> In x rs) ->
                Forall (fun p => mcompat (den (fst p)) (den (FmRec ls d)) (snd p)) ps').
  { intros ps' rs' F2. induction F2 as [|[mp tp] [[[lp dp] fp] rp] ps' rs' Hp _ IHa]; intros Sub; constructor.
    - simpl in Hp. destruct (asrec mp) as [[lq dq]|] eqn:A; [|discriminate].
      destruct tp as [| | | | | | x1 | fs1 r1 | u1 v1 | i1 o1 | E1 g1 | p1 | w1 | t1' | n1]; try discriminate. injection Hp as <- <- <- <-. cbn [fst snd].
      eapply asrec_mc; [exact A|]. rewrite den_rec. apply mc_rec. intros k. rewrite !den_f_at.
      destruct (field_at k fs1 r1) eqn:Fk.
      all: try (simpl; apply mc_bot).
      all: assert (Ik : In (flook k lq dq, fty (field_at k fs1 r1)) (at_label k rs))
             by (apply at_label_in with (fs := fs1) (r := r1); [apply Sub; left; reflexivity | rewrite Fk; discriminate]).
      all: pose proof (IH _ _ (Hlab k)) as Fk'; rewrite Forall_forall in Fk';
           specialize (Fk' _ Ik); simpl in Fk'; rewrite Fk in Fk'; exact Fk'.
    - apply IHa. intros x I. apply Sub. right. exact I. }
  apply (Gen _ _ Ar). auto.
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

(** Repeated steps of a relation. *)
Inductive steps {A} (R : A -> A -> Prop) : A -> A -> Prop :=
| st_refl x : steps R x x
| st_step x y z : R x y -> steps R y z -> steps R x z.

Lemma steps_one {A} (R : A -> A -> Prop) x y : R x y -> steps R x y.
Proof. intros H. eapply st_step; [exact H | apply st_refl]. Qed.

Lemma steps_trans {A} (R : A -> A -> Prop) x y z : steps R x y -> steps R y z -> steps R x z.
Proof. induction 1; auto. intros H'. eapply st_step; eauto. Qed.

Lemma steps_map {A B} (R : A -> A -> Prop) (R' : B -> B -> Prop) (f : A -> B) :
  (forall x y, R x y -> R' (f x) (f y)) -> forall x y, steps R x y -> steps R' (f x) (f y).
Proof. intros H x y S. induction S; [apply st_refl | eapply st_step; eauto]. Qed.

Lemma steps_sub a b : steps sub a b -> sub a b.
Proof. induction 1; [apply s_refl | eapply sub_trans; eauto]. Qed.

(** A binary join folded over a list. *)
Fixpoint foldj (j : ty -> ty -> option ty) (acc : ty) (xs : list ty) : option ty :=
  match xs with
  | [] => Some acc
  | x :: xs' => match j acc x with Some a => foldj j a xs' | None => None end
  end.

Lemma foldj_ub (R : ty -> ty -> Prop) j :
  (forall a b c, j a b = Some c -> R a c /\ R b c) ->
  forall xs acc r, foldj j acc xs = Some r -> steps R acc r /\ Forall (fun x => steps R x r) xs.
Proof.
  intros J. induction xs as [|x xs IH]; simpl; intros acc r E.
  - inversion E; subst. split; [apply st_refl | constructor].
  - destruct (j acc x) as [a|] eqn:Ej; [|discriminate].
    destruct (J _ _ _ Ej) as [H1 H2]. destruct (IH _ _ E) as [S F].
    split; [eapply st_step; eauto | constructor; [eapply st_step; eauto | exact F]].
Qed.

(** The first candidate that gives a result. *)
Fixpoint first_some {A B} (f : A -> option B) (xs : list A) : option B :=
  match xs with
  | [] => None
  | x :: xs' => match f x with Some y => Some y | None => first_some f xs' end
  end.

Lemma first_some_in {A B} (f : A -> option B) : forall xs y, first_some f xs = Some y ->
  exists x, In x xs /\ f x = Some y.
Proof.
  induction xs as [|x xs IH]; simpl; intros y E; [discriminate|].
  destruct (f x) eqn:Ex; [inversion E; subst; eauto|].
  destruct (IH _ E) as (x' & I & F). eauto.
Qed.

Definition is_dpf (m : fmark) : bool := match m with FmDp => true | _ => false end.
Definition is_shf (m : fmark) : bool := match m with FmSh => true | _ => false end.

Fixpoint fmsize (m : fmark) : nat :=
  match m with
  | FmSh | FmDp => 1
  | FmList e => S (fmsize e)
  | FmRec ls d => S (fmsize d + list_sum (map (fun p => fmsize (snd p)) ls))
  end.

Section JoinN.
Variable le : bool -> ty -> ty -> bool.
Hypothesis le_ok : forall fr a b, le fr a b = true -> jrel fr a b.
Variable rt : mark -> ty -> ty -> bool.
Hypothesis rt_ok : forall m a c, rt m a c = true ->
  (m = Dp /\ rsub a c) \/
  (partial m = true /\ ((exists b, msub m a b /\ sub b c) \/ sub a c)).

(** The relations' binary join ([join_slot]'s). *)
Definition jt (fr : bool) (a b : ty) : option ty := tjoin le (S (dsize a + dsize b)) fr a b.

Lemma jt_ub fr a b c : jt fr a b = Some c -> jrel fr a c /\ jrel fr b c.
Proof. apply tjoin_ub; exact le_ok. Qed.

(** Fuel for the merge and the join under a mark: the checker recurses on
    the marks and types. *)
Definition fuel (ps : list fslot) : nat :=
  S (list_sum (map (fun p => fmsize (fst p) + dsize (snd p)) ps)).

(** 1. Every arm new: the fresh join. *)
Definition newjoin (ps : list fslot) : option ty :=
  match ps with [] => None | p :: rest => foldj (jt true) (snd p) (map snd rest) end.

(** 2. Some arm partly new and none stored: one mark, then the types
    joined under it. *)
Definition pjoinN (ps : list fslot) : option fslot :=
  if existsb (fun p => fpartial (fst p)) ps && forallb (fun p => negb (is_shf (fst p))) ps then
    match fmergeN (fuel ps) ps, ps with
    | Some m, p :: rest =>
        if fpartial m then option_map (fun t => (m, t)) (foldj (mjoin le (fuel ps) m) (snd p) (map snd rest))
        else None
    | _, _ => None
    end
  else None.

(** 3. Shared: a target [t0], which new and partly new arms take where they
    can; the rest are committed and joined in.  The arms [abs] says are
    already reach [t0]. *)
Definition layer (ps : list fslot) (c : ty * (fslot -> bool)) : option ty :=
  let '(t0, abs) := c in
  match foldj (jt false) t0
          (map snd (filter (fun p => fpartial (fst p) && negb (abs p) && negb (rt (den (fst p)) (snd p) t0)) ps)) with
  | Some t1 =>
      foldj (jt false) t1
        (map snd (filter (fun p => is_dpf (fst p) && negb (abs p) && negb (rt Dp (snd p) t1)) ps))
  | None => None
  end.

Definition partials (ps : list fslot) : list fslot := filter (fun p => fpartial (fst p)) ps.

(** A new arm whose mark agrees with the partly new arms' joins them. *)
Definition agrees (ps : list fslot) (p : fslot) : bool :=
  is_dpf (fst p) && match fmergeN (fuel (partials ps ++ [p])) (partials ps ++ [p]) with
                    | Some _ => true
                    | None => false
                    end.

Definition joint (ps : list fslot) (p : fslot) : bool := fpartial (fst p) || agrees ps p.

(** The targets: the join of the stored arms; with no stored arm, the join
    of the partly new arms with the new arms that agree, then of the partly
    new arms alone, then each one's own type. *)
Definition targets (ps : list fslot) : list (ty * (fslot -> bool)) :=
  match map snd (filter (fun p => is_shf (fst p)) ps) with
  | h :: hs => match foldj (jt false) h hs with Some t => [(t, fun _ => false)] | None => [] end
  | [] =>
      match pjoinN (filter (joint ps) ps) with Some (_, t) => [(t, joint ps)] | None => [] end ++
      match pjoinN (partials ps) with Some (_, t) => [(t, fun p => fpartial (fst p))] | None => [] end ++
      map (fun p => (snd p, fun _ => false)) (partials ps)
  end.

Definition shjoin (ps : list fslot) : option ty := first_some (layer ps) (targets ps).

(** The join of arms of one kind, or of arms not grouped by kind. *)
Definition joinG (ps : list fslot) : option fslot :=
  if forallb (fun p => is_dpf (fst p)) ps then option_map (fun t => (FmDp, t)) (newjoin ps)
  else match pjoinN ps with
       | Some r => Some r
       | None => option_map (fun t => (FmSh, t)) (shjoin ps)
       end.

Lemma newjoin_ub ps t : forallb (fun p => is_dpf (fst p)) ps = true -> newjoin ps = Some t ->
  Forall (fun p => steps slot_sub (fden p) (fden (FmDp, t))) ps.
Proof.
  intros Dp' E. destruct ps as [|[m0 t0] rest]; [discriminate|]. simpl in E.
  destruct (foldj_ub (jrel true) (jt true) (jt_ub true) _ _ _ E) as [S F].
  assert (L : forall x y, steps rsub x y -> steps slot_sub (Dp, x) (Dp, y))
    by (apply (steps_map rsub slot_sub (fun x => (Dp, x))); intros; apply ss_dp; auto).
  rewrite forallb_forall in Dp'. constructor.
  - destruct m0; try (specialize (Dp' _ (in_eq _ _)); discriminate). apply L, S.
  - apply Forall_forall. intros [m x] I.
    destruct m; try (specialize (Dp' _ (in_cons _ _ _ I)); discriminate).
    rewrite Forall_forall in F. apply L, F. apply in_map_iff. exists (FmDp, x). auto.
Qed.

Lemma pjoinN_ub ps r : pjoinN ps = Some r -> Forall (fun p => steps slot_sub (fden p) (fden r)) ps.
Proof.
  unfold pjoinN. destruct (_ && _); [|discriminate].
  destruct (fmergeN (fuel ps) ps) as [m|] eqn:Em; [|discriminate].
  destruct ps as [|[m0 t0] rest]; [discriminate|].
  destruct (fpartial m) eqn:Pm; [|discriminate].
  destruct (foldj _ _ _) as [t|] eqn:Ef; simpl; [|discriminate]. intros E. inversion E; subst. clear E.
  pose proof (fmergeN_ok _ _ _ Em) as C.
  assert (P : partial (den m) = true) by (rewrite fpartial_den; exact Pm).
  destruct (foldj_ub (msub (den m)) _ (mjoin_ub le le_ok _ m) _ _ _ Ef) as [S F].
  assert (L : forall x y, steps (msub (den m)) x y -> steps slot_sub (den m, x) (den m, y))
    by (apply (steps_map (msub (den m)) slot_sub (fun x => (den m, x))); intros; apply ss_m; auto).
  rewrite Forall_forall in C, F |- *. intros [mp tp] I. unfold fden; simpl.
  eapply st_step; [apply ss_mc; [exact P | exact (C _ I)]|]. apply L.
  destruct I as [E|I]; [inversion E; subst; exact S|].
  apply F. apply in_map_iff. exists (mp, tp). auto.
Qed.

(** What [rt] accepts is two subsumption steps, ending shared. *)
Lemma rt_steps m a c : newish m = true -> rt m a c = true -> steps slot_sub (m, a) (Sh, c).
Proof.
  intros N R. destruct (rt_two_steps rt rt_ok m a c N R) as (x & H1 & H2).
  eapply st_step; [exact H1 | apply steps_one; exact H2].
Qed.

Lemma layer_ub ps t0 abs t : layer ps (t0, abs) = Some t ->
  (forall p, In p ps -> abs p = true -> steps slot_sub (fden p) (Sh, t0)) ->
  sub t0 t /\
  Forall (fun p => is_shf (fst p) = false -> steps slot_sub (fden p) (Sh, t)) ps.
Proof.
  unfold layer.
  destruct (foldj (jt false) t0 _) as [t1|] eqn:E1; [|discriminate]. intros E2 Ab.
  destruct (foldj_ub (jrel false) (jt false) (jt_ub false) _ _ _ E1) as [S1 F1].
  destruct (foldj_ub (jrel false) (jt false) (jt_ub false) _ _ _ E2) as [S2 F2].
  simpl in S1, S2. apply steps_sub in S1, S2.
  split; [eapply sub_trans; eauto|].
  rewrite Forall_forall in F1, F2 |- *. intros [m x] I Nsh.
  destruct (abs (m, x)) eqn:Ap.
  { eapply steps_trans; [exact (Ab _ I Ap)|]. apply steps_one, ss_sh. eapply sub_trans; eauto. }
  unfold fden; cbn [fst snd].
  destruct (is_dpf m) eqn:Dm.
  - destruct m; try discriminate. simpl.
    destruct (rt Dp x t1) eqn:R.
    + eapply steps_trans; [apply rt_steps; [reflexivity | exact R]|]. apply steps_one, ss_sh. exact S2.
    + apply steps_one, ss_forget. apply steps_sub. apply F2. apply in_map_iff. exists (FmDp, x).
      split; [reflexivity|]. apply filter_In. split; [exact I | simpl; rewrite Ap, R; reflexivity].
  - assert (Pm : fpartial m = true) by (destruct m; simpl in *; congruence).
    assert (P : partial (den m) = true) by (rewrite fpartial_den; exact Pm).
    assert (N : newish (den m) = true) by (destruct m; simpl in *; congruence).
    destruct (rt (den m) x t0) eqn:R.
    + eapply steps_trans; [apply rt_steps; [exact N | exact R]|].
      apply steps_one, ss_sh. eapply sub_trans; eauto.
    + apply steps_one, ss_m_forget; [exact P|].
      eapply sub_trans; [| exact S2]. apply steps_sub. apply F1. apply in_map_iff. exists (m, x).
      split; [reflexivity|]. apply filter_In. split; [exact I | simpl; rewrite Pm, Ap, R; reflexivity].
Qed.

Lemma pjoinN_partial ps m t : pjoinN ps = Some (m, t) -> fpartial m = true.
Proof.
  unfold pjoinN. destruct (_ && _); [|discriminate].
  destruct (fmergeN _ _) as [m'|]; [|discriminate]. destruct ps as [|p rest]; [discriminate|].
  destruct (fpartial m') eqn:P; [|discriminate].
  destruct (foldj _ _ _); simpl; intros E; inversion E; subst; auto.
Qed.

(** The arms a target says reach it do. *)
Lemma pjoinN_reach qs ps (f : fslot -> bool) m t :
  pjoinN qs = Some (m, t) -> (forall p, In p ps -> f p = true -> In p qs) ->
  forall p, In p ps -> f p = true -> steps slot_sub (fden p) (Sh, t).
Proof.
  intros Ep Sub p I Fp.
  pose proof (pjoinN_ub _ _ Ep) as F. pose proof (pjoinN_partial _ _ _ Ep) as Pm.
  rewrite Forall_forall in F.
  eapply steps_trans; [apply F; apply Sub; auto|]. unfold fden; cbn [fst snd].
  apply steps_one, ss_m_forget; [rewrite fpartial_den; exact Pm | apply s_refl].
Qed.

Lemma targets_reach ps t0 abs : In (t0, abs) (targets ps) ->
  forall p, In p ps -> abs p = true -> steps slot_sub (fden p) (Sh, t0).
Proof.
  unfold targets. destruct (map snd (filter (fun p => is_shf (fst p)) ps)) as [|h hs].
  2: { destruct (foldj _ h hs); [|intros []]. intros [E|[]]. inversion E; subst. discriminate. }
  intros It. apply in_app_or in It as [It|It].
  { destruct (pjoinN (filter (joint ps) ps)) as [[m t]|] eqn:Ep; [|contradiction].
    destruct It as [E|[]]. inversion E; subst.
    eapply pjoinN_reach; [exact Ep|]. intros p I J. apply filter_In. auto. }
  apply in_app_or in It as [It|It].
  { destruct (pjoinN (partials ps)) as [[m t]|] eqn:Ep; [|contradiction].
    destruct It as [E|[]]. inversion E; subst.
    eapply pjoinN_reach; [exact Ep|]. intros p I J. apply filter_In. auto. }
  apply in_map_iff in It as (q & E & _). inversion E; subst. discriminate.
Qed.

Lemma shjoin_ub ps t : shjoin ps = Some t -> Forall (fun p => steps slot_sub (fden p) (Sh, t)) ps.
Proof.
  unfold shjoin. intros E. destruct (first_some_in _ _ _ E) as ([t0 abs] & It & L).
  destruct (layer_ub _ _ _ _ L (targets_reach _ _ _ It)) as [St F]. rewrite Forall_forall in F |- *.
  intros [m x] I. destruct (is_shf m) eqn:Sm; [|apply F; auto].
  destruct m; try discriminate. unfold fden; simpl. apply steps_one, ss_sh.
  unfold targets in It.
  destruct (map snd (filter (fun p => is_shf (fst p)) ps)) as [|h hs] eqn:Eh.
  - exfalso. assert (Hx : In x (map snd (filter (fun p => is_shf (fst p)) ps))).
    { apply in_map_iff. exists (FmSh, x). split; [reflexivity|]. apply filter_In. auto. }
    rewrite Eh in Hx. contradiction.
  - destruct (foldj (jt false) h hs) as [th|] eqn:Ef; [|contradiction].
    destruct It as [Et|[]]. inversion Et; subst.
    destruct (foldj_ub (jrel false) (jt false) (jt_ub false) _ _ _ Ef) as [S F'].
    assert (Ix : In x (h :: hs)).
    { rewrite <- Eh. apply in_map_iff. exists (FmSh, x). split; [reflexivity|]. apply filter_In. auto. }
    eapply sub_trans; [| exact St]. apply steps_sub.
    destruct Ix as [<-|Ix]; [exact S|]. rewrite Forall_forall in F'. exact (F' _ Ix).
Qed.

(** Every arm reaches the join by subsumption, repeated. *)
Lemma joinG_ub ps r : joinG ps = Some r -> Forall (fun p => steps slot_sub (fden p) (fden r)) ps.
Proof.
  unfold joinG. destruct (forallb (fun p => is_dpf (fst p)) ps) eqn:Al.
  - destruct (newjoin ps) as [t|] eqn:E; simpl; [|discriminate]. intros E'. inversion E'; subst.
    apply newjoin_ub; auto.
  - destruct (pjoinN ps) as [r'|] eqn:E; [intros E'; inversion E'; subst; apply pjoinN_ub; auto|].
    destruct (shjoin ps) as [t|] eqn:Es; simpl; [|discriminate]. intros E'. inversion E'; subst.
    apply shjoin_ub; auto.
Qed.

(** Arms of different kinds: each kind's arms are joined, and the join is
    the union of those, as [tjoin] makes a union of two kinds. *)
Fixpoint add_kind (k : kind) (p : fslot) (gs : list (kind * list fslot)) : list (kind * list fslot) :=
  match gs with
  | [] => [(k, [p])]
  | (k', g) :: gs' => if kind_eqb k k' then (k', g ++ [p]) :: gs' else (k', g) :: add_kind k p gs'
  end.

Fixpoint group_kinds (ps : list fslot) (gs : list (kind * list fslot)) : option (list (kind * list fslot)) :=
  match ps with
  | [] => Some gs
  | p :: ps' => match kind_of_ty (snd p) with
                | Some k => group_kinds ps' (add_kind k p gs)
                | None => None
                end
  end.

Fixpoint munion (ts : list ty) : ty :=
  match ts with
  | [] => TBot
  | [t] => t
  | t :: ts' => TUnion t (munion ts')
  end.

Definition combine_kinds (rs : list fslot) : fslot :=
  (if forallb (fun r => is_dpf (fst r)) rs then FmDp else FmSh, munion (map snd rs)).

Definition joinN (ps : list fslot) : option fslot :=
  match group_kinds ps [] with
  | Some (g1 :: g2 :: gs) =>
      match omapl (fun g => joinG (snd g)) (g1 :: g2 :: gs) with
      | Some rs => Some (combine_kinds rs)
      | None => joinG ps
      end
  | _ => joinG ps
  end.

Lemma add_kind_in k p : forall gs q, (q = p \/ exists g, In g gs /\ In q (snd g)) ->
  exists g, In g (add_kind k p gs) /\ In q (snd g).
Proof.
  induction gs as [|[k' g] gs IH]; simpl; intros q H.
  - destruct H as [->|(g & [] & _)]. exists (k, [p]). simpl. auto.
  - destruct (kind_eqb k k').
    + destruct H as [->|(g' & [E|I] & Iq)].
      * exists (k', g ++ [p]). simpl. split; [auto|]. apply in_or_app. right. left. reflexivity.
      * subst. exists (k', g ++ [p]). simpl. split; [auto|]. apply in_or_app. left. exact Iq.
      * exists g'. simpl. auto.
    + destruct H as [->|(g' & [E|I] & Iq)].
      * destruct (IH p (or_introl eq_refl)) as (g'' & I'' & Iq''). exists g''. simpl. auto.
      * subst. exists (k', g). simpl. auto.
      * destruct (IH q (or_intror (ex_intro _ g' (conj I Iq)))) as (g'' & I'' & Iq''). exists g''. simpl. auto.
Qed.

Lemma group_kinds_in : forall ps gs gs', group_kinds ps gs = Some gs' ->
  forall q, (In q ps \/ exists g, In g gs /\ In q (snd g)) -> exists g, In g gs' /\ In q (snd g).
Proof.
  induction ps as [|p ps IH]; simpl; intros gs gs' E q H.
  - inversion E; subst. destruct H as [[]|H]; exact H.
  - destruct (kind_of_ty (snd p)) as [k|]; [|discriminate].
    apply (IH _ _ E). destruct H as [[->|I]|H]; [right | left; exact I | right].
    + apply add_kind_in. left. reflexivity.
    + apply add_kind_in. right. exact H.
Qed.

Lemma munion_ub fr : forall ts t, In t ts -> jrel fr t (munion ts).
Proof.
  induction ts as [|t0 ts IH]; simpl; intros t I; [contradiction|].
  destruct ts as [|t1 ts'].
  - destruct I as [<-|[]]. apply jrel_refl.
  - destruct I as [<-|I]; [apply jrel_unionr1, jrel_refl | apply jrel_unionr2, IH; exact I].
Qed.

Lemma combine_kinds_ub rs r : In r rs -> steps slot_sub (fden r) (fden (combine_kinds rs)).
Proof.
  intros I. unfold combine_kinds, fden; cbn [fst snd].
  assert (It : In (snd r) (map snd rs)) by (apply in_map; exact I).
  destruct (forallb (fun r => is_dpf (fst r)) rs) eqn:Al.
  - rewrite forallb_forall in Al. destruct r as [m t]. specialize (Al _ I). simpl in Al.
    destruct m; try discriminate. apply steps_one, ss_dp. exact (munion_ub true _ _ It).
  - apply steps_one. pose proof (munion_ub false _ _ It) as Hs. simpl in Hs.
    destruct r as [[| | e | ls d] t]; simpl in *.
    + apply ss_sh. exact Hs.
    + apply ss_forget. exact Hs.
    + apply ss_m_forget; [reflexivity | exact Hs].
    + apply ss_m_forget; [reflexivity | exact Hs].
Qed.

(** Every arm reaches the join by subsumption, repeated. *)
Theorem joinN_ub ps r : joinN ps = Some r -> Forall (fun p => steps slot_sub (fden p) (fden r)) ps.
Proof.
  unfold joinN. destruct (group_kinds ps []) as [gs|] eqn:Eg; [|apply joinG_ub].
  destruct gs as [|g1 [|g2 gs]]; try apply joinG_ub.
  destruct (omapl (fun g => joinG (snd g)) (g1 :: g2 :: gs)) as [rs|] eqn:Eo; [|apply joinG_ub].
  intros E. inversion E; subst. clear E.
  apply Forall_forall. intros p I.
  destruct (group_kinds_in _ _ _ Eg p (or_introl I)) as (g & Ig & Ip).
  apply omapl_forall2 in Eo.
  assert (Hg : exists r', In r' rs /\ joinG (snd g) = Some r').
  { clear -Eo Ig. induction Eo as [|g0 r0 gs0 rs0 H _ IH]; [contradiction|].
    destruct Ig as [<-|Ig]; [exists r0; simpl; auto|]. destruct (IH Ig) as (r' & I' & J). exists r'. simpl. auto. }
  destruct Hg as (r' & Ir & J). pose proof (joinG_ub _ _ J) as F. rewrite Forall_forall in F.
  eapply steps_trans; [exact (F _ Ip)|]. apply combine_kinds_ub. exact Ir.
Qed.

(** Whole stacks, position by position: [k] is the stacks' length. *)
Fixpoint joinN_stack (k : nat) (ss : list (list fslot)) : option (list fslot) :=
  match k with
  | 0 => if forallb (fun s => match s with [] => true | _ => false end) ss then Some [] else None
  | S k' =>
      match omapl (fun s => match s with p :: s' => Some (p, s') | [] => None end) ss with
      | Some hts =>
          match joinN (map fst hts), joinN_stack k' (map snd hts) with
          | Some r, Some rs => Some (r :: rs)
          | _, _ => None
          end
      | None => None
      end
  end.

Lemma steps_cons p q s s' : steps slot_sub p q -> steps ssub s s' -> steps ssub (p :: s) (q :: s').
Proof.
  intros Hp Hs. eapply steps_trans.
  - apply (steps_map slot_sub ssub (fun x => x :: s)); [| exact Hp].
    intros x y H. constructor; [exact H | apply ssub_refl].
  - apply (steps_map ssub ssub (fun t => q :: t)); [| exact Hs].
    intros x y H. constructor; [apply slot_sub_refl | exact H].
Qed.

Theorem joinN_stack_ub : forall k ss rs, joinN_stack k ss = Some rs ->
  Forall (fun s => steps ssub (map fden s) (map fden rs)) ss.
Proof.
  induction k as [|k IH]; simpl; intros ss rs E.
  - destruct (forallb _ ss) eqn:Al; [|discriminate]. inversion E; subst.
    apply Forall_forall. intros s I. apply forallb_forall with (x := s) in Al; auto.
    destruct s; [apply st_refl | discriminate].
  - match type of E with context [omapl ?F ss] => destruct (omapl F ss) as [hts|] eqn:Eo end;
      [|discriminate].
    destruct (joinN (map fst hts)) as [r|] eqn:Er; [|discriminate].
    destruct (joinN_stack k (map snd hts)) as [rs'|] eqn:Ers; [|discriminate]. inversion E; subst.
    pose proof (joinN_ub _ _ Er) as Fh. pose proof (IH _ _ Ers) as Ft.
    apply omapl_forall2 in Eo. clear E Er Ers IH.
    induction Eo as [|s [p s'] ss hts Hs _ IHo]; constructor.
    + destruct s as [|p0 s0]; [discriminate|]. inversion Hs; subst. simpl.
      inversion Fh; inversion Ft; subst. apply steps_cons; auto.
    + inversion Fh; inversion Ft; subst. apply IHo; auto.
Qed.
End JoinN.

(** A judgment stays derivable after repeated subsumption of its output. *)
Lemma T_steps sigs G B C R e s s1 s2 :
  T sigs G B C R e s s1 -> steps ssub s1 s2 -> T sigs G B C R e s s2.
Proof.
  intros H S. induction S as [x|x y z Hxy _ IH]; [exact H|].
  apply IH. eapply t_sub; [apply ssub_refl | exact H | exact Hxy].
Qed.

Section Rules.
Variable le : bool -> ty -> ty -> bool.
Hypothesis le_ok : forall fr a b, le fr a b = true -> jrel fr a b.
Variable rt : mark -> ty -> ty -> bool.
Hypothesis rt_ok : forall m a c, rt m a c = true ->
  (m = Dp /\ rsub a c) \/
  (partial m = true /\ ((exists b, msub m a b /\ sub b c) \/ sub a c)).

(** An arm that checks against its own stack checks against the join of
    all the arms: what any rule with n arms ([tw_case], kind patterns)
    needs. *)
Corollary arm_joinN sigs G B C R e s k ss s_i rs :
  In s_i ss -> joinN_stack le rt k ss = Some rs ->
  T sigs G B C R e s (map fden s_i) -> T sigs G B C R e s (map fden rs).
Proof.
  intros I J H. eapply T_steps; [exact H|].
  pose proof (joinN_stack_ub le le_ok rt rt_ok _ _ _ J) as F. rewrite Forall_forall in F. auto.
Qed.

Corollary if_joinN sigs G B C R e1 e2 s k s1 s2 rs :
  T sigs G B C R e1 s (map fden s1) -> T sigs G B C R e2 s (map fden s2) ->
  joinN_stack le rt k [s1; s2] = Some rs ->
  TW sigs G B C R (WIf e1 e2) ((Sh, TBool) :: s) (map fden rs).
Proof.
  intros H1 H2 J. apply tw_if.
  - apply (arm_joinN sigs G B C R e1 s k [s1; s2] s1 rs); [left; reflexivity | exact J | exact H1].
  - apply (arm_joinN sigs G B C R e2 s k [s1; s2] s2 rs); [right; left; reflexivity | exact J | exact H2].
Qed.
End Rules.

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
  exists r, joinN le_none (fun _ _ _ => false) [ex_p; ex_q] = Some r /\
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
