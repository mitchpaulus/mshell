(** Extract the checker's decision procedures for [<=] and fresh retyping
    (Decide.v) and the branch join (Join.v) to OCaml, together with the
    example types of Recursive.v, for comparing with the Go checker.
    See README.md in this directory. *)
From Stdlib Require Import Extraction ExtrOcamlBasic ExtrOcamlNatInt ExtrOcamlNativeString.
From MshellCore Require Import Syntax Join Decide Recursive.

Extraction Language OCaml.
Extraction "decide.ml"
  subq subq_set rsubq rsubq_set le_alg join_slot nocache
  Json Json2 Person PersonLit TA TBB QA QB V RA RB RC L.
