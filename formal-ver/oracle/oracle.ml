(* The proved decision procedures for <= and fresh retyping, and the
   branch join, extracted from Decide.v and Join.v, answering queries one
   line at a time.  See README.md for the query and type syntax.

     oracle              answer the queries on stdin, one answer per line
     oracle --examples   print the example corpus: query, tab, expected answer *)

open Decide

(* ---- S-expressions ---- *)

type sexp = Atom of string | Lst of sexp list

exception Parse of string

let tokenize (s : string) : string list =
  let toks = ref [] and buf = Buffer.create 16 in
  let flush () =
    if Buffer.length buf > 0 then (toks := Buffer.contents buf :: !toks; Buffer.clear buf) in
  String.iter
    (fun c ->
      match c with
      | '(' | ')' -> flush (); toks := String.make 1 c :: !toks
      | ' ' | '\t' | '\r' | '\n' -> flush ()
      | c -> Buffer.add_char buf c)
    s;
  flush ();
  List.rev !toks

(* All the s-expressions in a token list. *)
let parse_all (toks : string list) : sexp list =
  let rec one = function
    | "(" :: rest ->
        let items, rest = many rest in
        (match rest with ")" :: rest -> (Lst items, rest) | _ -> raise (Parse "missing )"))
    | ")" :: _ -> raise (Parse "unexpected )")
    | a :: rest -> (Atom a, rest)
    | [] -> raise (Parse "unexpected end")
  and many toks =
    match toks with
    | [] | ")" :: _ -> ([], toks)
    | _ ->
        let x, rest = one toks in
        let xs, rest = many rest in
        (x :: xs, rest)
  in
  match many toks with
  | xs, [] -> xs
  | _, _ -> raise (Parse "unexpected )")

(* ---- Types ---- *)

let int_of s = match int_of_string_opt s with Some n when n >= 0 -> n | _ -> raise (Parse ("not a number: " ^ s))

let variance_of = function
  | "co" -> VCo | "contra" -> VContra | "inv" -> VInv
  | s -> raise (Parse ("unknown variance: " ^ s))

let param_of = function
  | Atom s ->
      (match String.index_opt s '/' with
       | Some i when String.sub s (i + 1) (String.length s - i - 1) = "fresh" ->
           { p_var = variance_of (String.sub s 0 i); p_fresh = true }
       | None -> { p_var = variance_of s; p_fresh = false }
       | Some _ -> raise (Parse ("unknown parameter: " ^ s)))
  | Lst _ -> raise (Parse "a parameter is an atom")

let rec ty_of = function
  | Atom "int" -> TInt
  | Atom "str" -> TStr
  | Atom "bool" -> TBool
  | Atom "bot" -> TBot
  | Atom "top" -> TTop
  | Lst [ Atom "maybe"; t ] -> TMaybe (ty_of t)
  | Lst [ Atom "list"; t ] -> TList (ty_of t)
  | Lst [ Atom "dict"; t ] -> TRec ([], FDict (ty_of t))
  | Lst [ Atom "rec"; Lst fields; r ] ->
      TRec
        (List.map
           (function
             | Lst [ Atom k; f ] -> (k, fstat_of f)
             | _ -> raise (Parse "a field is (label status)"))
           fields,
         fstat_of r)
  | Lst (Atom "union" :: (_ :: _ :: _ as ts)) ->
      let rec go = function [ t ] -> ty_of t | t :: ts -> TUnion (ty_of t, go ts) | [] -> assert false in
      go ts
  | Lst [ Atom "quote"; Lst ins; Atom "never" ] -> TQuote (List.map ty_of ins, None)
  | Lst [ Atom "quote"; Lst ins; Lst outs ] -> TQuote (List.map ty_of ins, Some (List.map ty_of outs))
  | Lst [ Atom "enum"; Atom name; Atom imm; Lst params; Lst args ] ->
      let en_imm = match imm with "imm" -> true | "mut" -> false | _ -> raise (Parse "enum: imm or mut") in
      TEnum ({ en_name = name; en_params = List.map param_of params; en_imm }, List.map ty_of args)
  | Lst [ Atom "param"; Atom i ] -> TParam (int_of i)
  | Lst [ Atom "var"; Atom x ] -> TVar (int_of x)
  | Lst [ Atom "mu"; t ] -> TMu (ty_of t)
  | Lst [ Atom "rv"; Atom n ] -> TRV (int_of n)
  | Atom s -> raise (Parse ("unknown type: " ^ s))
  | Lst _ -> raise (Parse "unknown type form")

and fstat_of = function
  | Lst [ Atom "req"; t ] -> FReq (ty_of t)
  | Lst [ Atom "opt"; t ] -> FOpt (ty_of t)
  | Lst [ Atom "dict"; t ] -> FDict (ty_of t)
  | Atom "abs" -> FAbs
  | Atom "open" -> FOpen
  | _ -> raise (Parse "unknown field status")

let string_of_variance = function VCo -> "co" | VContra -> "contra" | VInv -> "inv"

let rec string_of_ty = function
  | TInt -> "int"
  | TStr -> "str"
  | TBool -> "bool"
  | TBot -> "bot"
  | TTop -> "top"
  | TMaybe t -> "(maybe " ^ string_of_ty t ^ ")"
  | TList t -> "(list " ^ string_of_ty t ^ ")"
  | TRec ([], FDict t) -> "(dict " ^ string_of_ty t ^ ")"
  | TRec (fs, r) ->
      "(rec ("
      ^ String.concat " " (List.map (fun (k, f) -> "(" ^ k ^ " " ^ string_of_fstat f ^ ")") fs)
      ^ ") " ^ string_of_fstat r ^ ")"
  | TUnion (a, b) -> "(union " ^ string_of_ty a ^ " " ^ string_of_ty b ^ ")"
  | TQuote (ins, outs) ->
      "(quote (" ^ String.concat " " (List.map string_of_ty ins) ^ ") "
      ^ (match outs with None -> "never" | Some o -> "(" ^ String.concat " " (List.map string_of_ty o) ^ ")")
      ^ ")"
  | TEnum (e, args) ->
      "(enum " ^ e.en_name ^ " " ^ (if e.en_imm then "imm" else "mut") ^ " ("
      ^ String.concat " "
          (List.map (fun p -> string_of_variance p.p_var ^ if p.p_fresh then "/fresh" else "") e.en_params)
      ^ ") (" ^ String.concat " " (List.map string_of_ty args) ^ "))"
  | TParam i -> "(param " ^ string_of_int i ^ ")"
  | TVar x -> "(var " ^ string_of_int x ^ ")"
  | TMu t -> "(mu " ^ string_of_ty t ^ ")"
  | TRV n -> "(rv " ^ string_of_int n ^ ")"

and string_of_fstat = function
  | FReq t -> "(req " ^ string_of_ty t ^ ")"
  | FOpt t -> "(opt " ^ string_of_ty t ^ ")"
  | FDict t -> "(dict " ^ string_of_ty t ^ ")"
  | FAbs -> "abs"
  | FOpen -> "open"

let slot_of = function
  | Lst [ Atom "shared"; t ] -> (Sh, ty_of t)
  | Lst [ Atom "fresh"; t ] -> (Dp, ty_of t)
  | _ -> raise (Parse "a slot is (shared T) or (fresh T)")

let string_of_slot (m, t) = "(" ^ (match m with Sh -> "shared" | Dp -> "fresh") ^ " " ^ string_of_ty t ^ ")"

(* ---- Queries ---- *)

let yn b = if b then "yes" else "no"

let answer (line : string) : string option =
  match parse_all (tokenize line) with
  | [] -> None
  | Atom c :: _ when String.length c > 0 && c.[0] = '#' -> None
  | [ Atom "sub"; Atom n; a; b ] -> Some (yn (subq nocache (int_of n) (ty_of a) (ty_of b)))
  | [ Atom "rsub"; Atom n; a; b ] -> Some (yn (rsubq nocache nocache (int_of n) (ty_of a) (ty_of b)))
  | [ Atom "join"; Atom n; p; q ] ->
      Some
        (match join_slot (le_alg nocache nocache (int_of n)) (slot_of p) (slot_of q) with
         | None -> "none"
         | Some r -> string_of_slot r)
  | _ -> raise (Parse "expected: sub N A B | rsub N A B | join N SLOT SLOT")

(* ---- The example corpus: the examples of Decide.v and Recursive.v ---- *)

let fuel = 30

let examples : (string * string) list =
  let s = string_of_ty and sl = string_of_slot in
  let sub a b = Printf.sprintf "sub %d %s %s" fuel (s a) (s b) in
  let rsub a b = Printf.sprintf "rsub %d %s %s" fuel (s a) (s b) in
  let join p q = Printf.sprintf "join %d %s %s" fuel (sl p) (sl q) in
  [
    ("# Json equals the same union in another order (alg_json_teq)", "");
    (sub json json2, "yes");
    (sub json2 json, "yes");
    ("# alg_int_json, alg_list_json", "");
    (sub TInt json, "yes");
    (sub (TList json) json, "yes");
    ("# a {name, age, friends} literal is a Person (alg_person_lit)", "");
    (sub personLit person, "yes");
    ("# a stored [int] is not Json; a fresh one may become one (alg_ints_json)", "");
    (sub (TList TInt) json, "no");
    (rsub (TList TInt) json, "yes");
    ("# alg_a_to_c", "");
    (rsub rA rC, "yes");
    ("# H12: one assumption set per relation (alg_h12)", "");
    (rsub tA tBB, "no");
    ("# H13: type V = int | V; str is not below V (alg_h13)", "");
    (sub TStr v, "no");
    ("# cache_early: A <= B is no", "");
    (sub tA tBB, "no");
    (sub qA qB, "no");
    ("# joins (alg_join_*)", "");
    (join (Sh, TInt) (Dp, json), sl (Sh, json));
    (join (Dp, TList TInt) (Dp, json), sl (Dp, json));
    (join (Sh, TList TInt) (Dp, json), "none");
    (join (Dp, TList person) (Dp, TList personLit), sl (Dp, TList person));
    (join (Dp, rA) (Dp, rB), "none");
    ("# type L = [L] is a list of itself (not an example in Rocq; a yes is a proof by subq_sound)", "");
    (sub l (TList l), "yes");
    (sub (TList l) l, "yes");
  ]

let () =
  if Array.length Sys.argv > 1 && Sys.argv.(1) = "--examples" then
    List.iter
      (fun (q, a) -> if a = "" then print_endline q else Printf.printf "%s\t%s\n" q a)
      examples
  else
    try
      while true do
        let line = input_line stdin in
        (try match answer line with Some a -> print_endline a | None -> ()
         with Parse msg -> print_endline ("error: " ^ msg));
        flush stdout
      done
    with End_of_file -> ()
