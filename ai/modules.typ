// Modules for mshell: research and design.
// Build: typst compile ai/modules.typ
// Research reports behind this document: ai/modules-research/*.md

#set document(title: "Modules for mshell", author: "Claude (draft for Mitchell)")
#set page(paper: "us-letter", margin: (x: 1in, y: 1in), numbering: "1")
#set text(font: "Libertinus Serif", size: 11pt)
#set par(justify: true, leading: 0.62em)
#show table: set par(justify: false)
#set table(inset: 5pt, stroke: 0.5pt + luma(170))
#set heading(numbering: "1.1", supplement: [§])
#show heading.where(level: 1): it => { v(0.8em); it; v(0.3em) }
#show raw: set text(font: "DejaVu Sans Mono", size: 0.86em)
#show raw.where(block: true): it => block(
  fill: luma(245), inset: 8pt, radius: 3pt, width: 100%, it,
)
#show link: set text(fill: rgb("#1a4f8b"))

// ---------- helpers ----------

#let callout(kind, title, body) = block(
  width: 100%, inset: (left: 10pt, rest: 8pt), stroke: (left: 2pt + luma(120)),
  fill: luma(250), breakable: true,
  [*#kind*#if title != none [ (#title)]. #body],
)
#let rec(body) = callout("Recommendation", none, body)
#let theorem(title, body) = callout("Theorem", title, body)
#let defn(title, body) = callout("Definition", title, body)
#let owner(body) = callout("Owner decision", none, body)
#let lesson(body) = callout("Lesson", none, body)
#let small(body) = text(size: 0.78em, body)
#let w(s) = raw(s)

// ---------- title ----------

#align(center)[
  #text(size: 20pt, weight: "bold")[Modules for mshell]
  #v(0.2em)
  #text(size: 13pt)[Prior art, trade-offs, and a proposed design]
  #v(0.4em)
  #text(size: 10pt)[Draft for review, 2026-10-08, branch `modules-design` (from `main` at 594776c)]
  #v(0.1em)
  #text(size: 10pt, style: "italic")[Written by Claude from six research passes (reports in `ai/modules-research/`)
  and a read of the current implementation. Your notes in `design/modules.md` and `design/startup_and_versioning.md` are the starting point.]
]

#v(0.6em)

#outline(depth: 2, indent: 1.2em)
#pagebreak()

= Summary

You asked for a module system that favours *simplicity*, *rigorous proof about the code tree*, and *explicitness*.
Your own notes already fix three things:

- the file is the unit of identity;
- modules are content-addressed by hash;
- the hash is required.

Six research passes covered typed functional languages, systems languages, scripting languages and shells, concatenative languages, theory and supply chain, and mshell's own internals.
They all point to a design that keeps those three choices and adds a small number of hard rules.
Every rule below was adopted by at least one mature language after the opposite choice hurt it, and no language I found moved the other way.

#rec[
  1. *Identity.* A module is one file. Its identity is `sha256` of its exact bytes, written `'sha256:<64 lowercase hex>'`.
  2. *Merkle imports.* Each import line carries the hash of the file it imports. A file's hash therefore pins its whole dependency tree, so there is no lockfile, and cycles cannot be built.
  3. *Qualified only.* `import geo 'sha256:…' 'gh:…/geo.msh'` binds the alias `geo`, and its names are used as `geo.area`. There are no glob imports, no `open`, and no unqualified import lists in v1. An import adds no bare names to the file, ever.
  4. *Not transitive.* A file sees only what its own import lines bind. Re-exports are explicit and listed by name (`export s.area`); there is no `export *`.
  5. *Private by default.* An `export` line in the header lists the file's interface. An exported signature may not mention a private enum.
  6. *Declarations only.* An imported file holds `VER`, `import`, `export`, `def`, `type` and `enum`, and nothing else. Importing runs no code, pushes nothing, and registers nothing (no completions, no prompt, no env).
  7. *`VER` fixes the bare names.* Every imported file declares `VER`. A file's bare words resolve against the builtins and std words that existed at *its* `VER`, like a Rust edition. Stdlib growth can then never break or reinterpret a published module.
  8. *Sealed and local files* (Dhall's rule). A file reached through a hash is _sealed_ and may import only by hash. The root script and plain local files may import local files by relative path without a hash. "Hash required to run" becomes "a hash is required to enter sealed code", which is the property that matters for sharing and security.
  9. *No version resolution, ever.* Two hashes of one library both load. Defs and structural types work across them; enums are distinct types, identified by (file, name).
  10. *No implicit network.* Running never fetches. `msh fetch` fills a content-addressed store, and the hash, not the URL, is what gets looked up.
  11. *Check everything loaded.* Each file is checked against its own declarations and the interfaces of its direct imports (Cardelli-style separate checking). That is the hook for a per-hash check cache.
  12. *Prove it first.* Seven theorems (@sec-proof) are cheap given these rules. Mechanize T1–T5 in `formal-ver/` (about a week) before writing Go.
]

The rules are not free.
Their costs are in @sec-ledger:

- verbosity from qualified names;
- an update cascade when a deep dependency changes;
- duplicate copies of a library in a diamond;
- a small version table for the names in each `VER`;
- new CLI tooling, which is required rather than optional.

The decisions that are yours to make are collected in @sec-decisions.
The two biggest are whether scripts stop loading `init.msh` (I recommend yes) and the sealed/local split in item 8, which relaxes your "hash is required to run".

#callout("Found along the way", none)[
  A one-line `init.msh` holding `type T = int` turns std.msh's generic `T` into `int`.
  I reproduced it on `main`: `foldl`, `concat` and several completion defs then fail to check on every run.
  Per-file type scopes fix it as a side effect; a direct fix is in @sec-incidental with the other small findings.
]

= Starting point

== Your goals

From `design/modules.md`:

- A large builtin set and stdlib, so most one-off scripts need no module statements.
- Modules are *content-addressed by hash*, for de-duplication, caching and security.
- The *file* is the unit of identity.
- The hash is *required* to run.
- Align with Deno: a library is a `mod.ts`-style file that re-exports.
- CLI: `msh add "gh:mitchpaulus/mylibrary" myscript.msh` rewrites the script in place; `msh lookup` prints the line.
- Users should understand they can just hash the file themselves.

From this request: simplicity, a rigorous proof of the code tree, and explicitness over implicitness.

== Facts about mshell today that constrain the design

These come from reading the source and running a binary built from HEAD (details with file and line numbers in `ai/modules-research/00-mshell-internals.md`).

#table(
  columns: (1.15fr, 2fr),
  [*Fact*], [*Why it matters for modules*],
  [Defs, `type`s, enums, enum members and builtins share *one flat global namespace*. Defining a name twice anywhere (std, init, script, REPL) is an error. Enforced in `CheckDefinitionNames`, `RegisterDeclarations`, and the checker's `checkDefName`/`reserve`.],
  [Strict and simple: ambiguity is already an error, not shadowing. But a frozen (hashed) file cannot be edited, so "std grew a name you define" becomes a permanent break unless std growth is versioned (@sec-ver).],
  [Inside a list literal, *a bare word that is not a def or builtin becomes a string*. Defs are called even inside lists: with `def foo.txt`, `[echo foo.txt]` calls it.],
  [The meaning of a word depends on the *absence* of a name. Any layer of names that can grow under a file (std growth, glob imports) silently turns strings into calls. This is the single most important hazard for stability.],
  [`.` is a legal mid-word character. `mod.word` lexes as one `LITERAL` today. A word ending in `.` is a prefix quote, so `mod.map.` is already one `PREFIXQUOTE` whose head is `mod.map`.],
  [`alias.name` needs no lexer change and composes with prefix quotes. Dotted words are also common command arguments (`notes.txt`, `python3.11`).],
  [`:` always ends a word. `mod:word` lexes as `mod` then the getter `:word`; `mod::word` as two `COLON`s.],
  [`:` and `::` are unavailable, and `mod:word` would be a plausible *wrong* parse (field `word` of `mod`).],
  [There is no `source`, `import`, `include` or file `eval`. The only multi-file path is std.msh, then init.msh, then the script, concatenated into one def slice.],
  [Every name a program can call is known before it runs. Nothing has to be undone.],
  [Within and across those files, forward references and mutual recursion work. std.msh can call a def from init.msh (verified: it prints 42, no warning).],
  [Keep order-independence within a file. Across files the dependency direction must follow the import graph.],
  [Startup top-level code runs on the script's stack and variables. init.msh's variables and env changes are visible to the script.],
  [A module must never see this. Whether *scripts* should see it is @sec-init.],
  [All startup `type`/`enum` declarations are declared before any startup signature is resolved, and "a single letter that is not a type is a generic" is decided against that global table.],
  [The `type T = int` bug above. Type names must be per file.],
  [Every startup def body is re-checked on every run: std.msh is about 3 ms of a roughly 9 ms start. Nothing is cached across runs *because nothing guarantees the files are unchanged* (`ai/type-system-progress.md`).],
  [Content-addressed files remove exactly that objection. A check result can be cached by hash.],
  [`'complete'` metadata registers a completion def globally by command name, as a side effect of a file being loaded.],
  [Registration on import is Haskell's orphan-instance problem under another name (@sec-init).],
  [The Rocq core has one flat global environment: `WCall` takes a `string`, `genv` has `g_sigs` and `g_ctors`, and `tw_case` quantifies over `g_ctors` (exhaustiveness).],
  [Modules can be proven by *translation* into the existing core (resolved names become `"<hash>.<name>"`), as long as constructor tables agree everywhere.],
)

= What other languages did

== Five questions every module system answers

Every system answers these five questions, implicitly or not.
The failures in the record nearly all come from answering one implicitly.

+ *Identity.* What is a module (definition, file, directory, package), and what names it (path, URL, registry name, hash)?
+ *Resolution.* Which definition does a word in file F denote, and is that unique?
+ *Visibility.* Who may see a name, and can you tell from local syntax alone?
+ *Loading.* What runs when a module loads, and in what order?
+ *Provenance.* How are the bytes obtained, and how do you know they are the right ones, now and later?

The strongest recurring lesson from the systems-language research is that *the answers to questions 1–3 must be computable from syntax alone, without evaluation*.
C/C++ `#include`, Rust 2015's `extern crate`, Zig's `usingnamespace` and C++20 modules' file-name decoupling each broke that rule and each cost years.

== The direction of travel

The typed-functional research found the clearest pattern in the survey.
Every language that started permissive moved toward the same endpoint, often late and at large migration cost.

#table(
  columns: (auto, 1fr, 1fr),
  [*Language*], [*Started with*], [*Moved to*],
  [Haskell], [implicit Prelude; open imports; global instances], [community norm of qualified or listed imports; the versioning policy (PVP) says only those are safe against minor upgrades],
  [OCaml], [flat global namespace of compilation units], [dune `(wrapped true)`: name mangling plus a re-exporting alias module, which is your `mod` file, reached independently],
  [Lean 4], [transitive imports; everything public], [the `module` system (stable 4.27.0, Jan 2026): private by default, non-transitive `import`, explicit `public import`],
  [Koka], [public by default], [private by default (2022)],
  [Rust], [2015 paths with two meanings; `extern crate`], [2018 path clarity (RFC 2126); prelude additions gated on editions],
  [Zig], [`usingnamespace` (glob mixin)], [removed in 0.15: names in a file must be knowable from syntax],
  [Ruby], [one global constant namespace, open classes], [refinements (lexical); `Ruby::Box` isolation (2025)],
  [Python], [implicit imports with effects; lazy imports rejected (PEP 690)], [lazy imports only as explicit syntax (PEP 810)],
  [Deno], [raw HTTPS URL imports, `deps.ts`], [JSR and `npm:`; JSR refuses to publish packages with HTTPS imports],
  [min], [prefixed flat stdlib (`fread`)], [module-qualified stdlib (`fs.read`) in v0.45, "the release with most breaking changes"],
  [Uiua], [member access written `~`], [member access written `.` (0.19.0, Aug 2026)],
  [Factor], [eight import forms], [deprecating `QUALIFIED:`, debating `EXCLUDE:`/`RENAME:`; converging on `USING:` plus `vocab:word`],
)

The common endpoint:

- private by default;
- explicit, non-transitive imports;
- explicit re-exports;
- importer-chosen qualifiers;
- no import cycles;
- no global side channels.

None of these languages moved the other way.
mshell has no users of a module system yet, so it can start at the endpoint.
Lean's porting recipe ("make all imports public first, then minimize") shows what retrofitting costs.

== Typed functional languages

- *Haskell.*
  - *The implicit Prelude.* Every addition to it is a breaking change for someone. That led to an ecosystem of alternative preludes.
  - *Open imports.* These make upstream additions break downstream code. The PVP is written around this one default.
  - *Instances.* They cannot be named in import lists, so orphan instances leak through the whole transitive closure. The PVP treats adding one as a major change.
  - *Cycles.* The Report allows them, but GHC needs hand-written `.hs-boot` files to compile them.
  - *Ambient environment files.* `.ghc.environment.*` files were silently picked up from any parent directory, and were eventually turned off by default. That is the direct analogue of a script reading `init.msh`.
  - *What went right.* Importer-chosen qualified aliases, and ambiguity decided by *entity* rather than spelling: the same def reached two ways is not a clash.
- *OCaml.*
  - *What went right.* `.mli` interfaces make the interface a checked, diffable artifact.
  - *What went wrong.* The flat namespace of unit names forced name mangling. `open` shadows silently. Top-level effects run in link order, so whether a module happens to be linked changes behaviour.
- *Elm.*
  - *What went right.* Mandatory `exposing`, no cycles (with a first-class error), no global side channels, and semver *computed from the API diff* (`toMagnitude`: removed = major, added = minor).
  - *What went wrong.* Locking down kernel code late ruptured the community. The lesson: an escape hatch anyone can reach becomes API.
- *Roc.* This is the closest existing design to yours: the hash sits in the import line.
  Read from source:
  - The hash is BLAKE3 of *compressed archive* bytes, so it identifies a blob, not content.
  - Packages with the same major version are unified to the highest version mentioned, so *a transitive package's hash is not necessarily what runs*.
  - Cache hits are not re-hashed.
  Roc chose ecosystem dedup over "the hash determines the program". mshell should not.
- *Gleam.* The file path is the module name, imports are qualified by default, nothing is public without `pub`, and cycles are an error. Its trust anchor is a lockfile beside the source, so a copied script loses its pin.
- *Unison.* Each definition is identified by its hash, and names are metadata.
  - *What went right.* Coexisting versions and a cache that never has to be invalidated.
  - *What went wrong.*
    - Tooling lock-in, because code is not stored as text.
    - Structural identity merged `Suit` and `Direction`, so `unique type` became the default.
    - Every edit to a leaf propagates up through every dependent's hash, so the update tooling kept changing.
  You chose the file over the definition. That keeps `git`, `grep` and `sha256sum`, and still inherits the propagation cost.
- *Koka.* Type-directed static overloading resolves a name from the types around it. For mshell that would make resolution depend on stack-effect inference. Avoid it.

== Systems languages

- *Go.*
  - *Mechanism.* Imports come first and are literal, so `go/build` reads only the import block. Imports are always qualified, cycles are a DFS error that prints the path, and unused imports are errors.
  - *Init side effects.* `import _ "pq"` registers a driver as a hidden side effect. The order of `init` across packages was observable, but was only specified in Go 1.21.
  - *Minimal version selection.* MVS avoids NP-complete solving, and nothing changes unless you ask.
  - *Integrity.* `go.sum` hashes the extracted *tree*, not the archive. Changing which files were excluded had to be gated on the module's declared Go version, so that published checksums still verify: the hashed set is a forever-API. `go.sum` alone is trust-on-first-use; sumdb (a transparency log) closes that.
  - *Proxy.* Proxy immutability kept a backdoored typosquat available for three years.
  - *Overrides.* `replace` works only in the main module, so a library cannot impose a fork on its users.
- *Rust.*
  - *Path reform.* RFC 2126 fixed "one syntax, two meanings". Its stabilization rule: an import path resolves exactly like a non-import path.
  - *Globs.* RFC 1105 concedes that glob imports make "add a public item" a breaking change. Glob ambiguity across crates was also implemented wrongly for years (`ambiguous_glob_imports`).
  - *Preludes.* Adding three traits to the prelude needed the 2021 edition. The prelude is an API, and growing it needs versioning.
  - *Two versions of one crate* give "expected `foo::Bar`, found `foo::Bar`".
- *Zig* is the closest analogue to your goals: the file is a struct, the hash is mandatory, and "packages do not come from a `url`; they come from a `hash`".
  Read from `src/Package/Fetch.zig`:
  - *Order of work.* The cache is looked up by hash before any network access. Files outside the `paths` allow-list are deleted before hashing, so the cache holds exactly what was hashed.
  - *What is hashed.* Mode bits are not hashed, and executability is derived from content.
  - *Format churn.* The hash format changed in 0.14 (multihash to `name-version-…`), and the shipped docs still describe the old one. `fingerprint` had to be added because pure content hashes forget that Y is the next version of X.
  - *A hashing flaw.* Symlink entries hash `path ‖ target` with no separator, so `x→yz` and `xy→z` collide. The rule is to hash an unambiguous encoding.
  - *Cycles.* Allowed, because Zig has no import-time execution.
  - *Lazy analysis.* Unreferenced code is never checked. Users ship broken dead code, and the workaround (`refAllDecls`) is being removed.
- *D.* Anti-hijacking: overloads from different imports form separate sets, and a call that matches in two of them is an error. Importing a module cannot silently redirect an existing call.
- *Nim.* Open imports plus cross-module overloading mean an upstream addition can change *which* proc is called, with no error. This is the worst failure mode in the survey.
- *C++20 modules.* About six years to reach build tools, because module names are decoupled from file names. Every source must be scanned (P1689) before anything compiles, and built interfaces are not portable.
- *Swift.* A type named like its module breaks `Module.Name` qualification. SE-0491 (2025) added a separate `Module::name` operator.

== Scripting languages and shells

- *Deno.* This tests your Deno model directly.
  Ryan Dahl's "What we got wrong about HTTP imports" (2024) names four failures:
  - long specifiers;
  - `deps.ts` boilerplate;
  - duplicate versions, because "URLs lack semantic versioning";
  - dependence on "the least reliable host".
  Deno's docs now say HTTPS imports "suit small single-file scripts", and JSR rejects packages that contain them.
  Hashes fix *mutability*, which Deno solved separately, and make the length and duplication problems *worse*.
  The plan is defensible because mshell's audience is exactly the small scripts where Deno says the model still works.
  It should say so, and not promise library-of-libraries ergonomics.
- *Node.*
  - *The dual package hazard.* Two copies of a package mean two class identities and two module states. Duplicates are a *correctness* problem only when modules carry identity or state.
  - *Top-level await.* It made import-time effects leak into the type of the whole graph (`ERR_REQUIRE_ASYNC_MODULE`).
- *Python.*
  - A local `random.py` shadows the stdlib for every module in the process. Python 3.13 added a dedicated error message, and 3.11 added `-P`.
  - Circular imports see a partially initialized module.
  - Import-time effects made `.pth` files a startup hook.
  - PEP 723 / uv is the useful prior art. A single-file script declares its dependencies in a delimited comment block, which `uv add --script` rewrites. But uv put the *lock* (hashes) in a sibling file, because hashes for every transitive wheel would swamp a short script.
  - mshell's Merkle design needs only one hash per *direct* import, because each library pins its own children.
- *Ruby.* A global constant namespace and open classes took 20 years and two mechanisms to partly undo.
- *Perl.* `use` runs code at compile time, so `perl -c` executes code. `.` was removed from `@INC` in 5.26 as a security fix.
- *bash.*
  - `source` is textual and global.
  - A file name without a slash is searched on `$PATH`.
  - Relative paths resolve against the cwd, not against the sourcing file.
  - `BASH_ENV` (like `.zshenv` and fish's `config.fish`) runs for non-interactive scripts, so user configuration leaks into them.
- *fish and zsh autoload.* Superb for interactive use and wrong for scripts, where the loaded set must be static. A good model for the planned "snippets directory".
- *PowerShell.* Microsoft tells authors not to use wildcards in `FunctionsToExport`, because otherwise auto-loading must load the module to learn its commands. Explicit export lists turned out to be a performance requirement.
- *Nushell* (read from source).
  - `use` is resolved *in the parser*.
  - A module body may hold only `def`, `const`, `extern`, `alias`, `use`, `module`, `export` and `export-env`; anything else is a parse error.
  - Paths resolve relative to the current file, and `NU_LIB_DIRS` must be a parse-time constant.
  - Reload compares file contents, not mtimes.
  - The closest shell to what mshell wants.
  - Its `mod cmd` two-word form does not transplant: in a concatenative language a space is composition.
- *Elvish.* `mod:fn` is a single token, but modules execute at `use` time, and cycles see partial namespaces.
- *Oils/YSH.* Effects at module top level are only discouraged. `use --extern grep sed` declares external commands for static analysis, which is a nice idea for later.
- *Dhall* (read the standard). The best prior art for required hashes:
  - The hash is authoritative: a cached import resolves even if the URL is dead.
  - _Referential sanity_: remote imports may import only remote imports.
  - Imports are closed terms, and cycles are an error.
  - The `?` fallback never hides a hash mismatch.
  - Its *semantic* hash (CBOR of the normal form) should not be copied. Standard 2.0.0 changed every hash, a version tag had to be pulled out of the hash input (issue \#335), and the versioning doc still says the hash "always changes with a new release". PureScript's Spago dropped Dhall partly because tools could not edit it without syntax-tree surgery.
- *Nix.*
  - `<nixpkgs>` via `NIX_PATH` was ambient resolution; flakes replaced it with locked inputs.
  - Each input pins its own nixpkgs, so dedup is a manual root-level `follows`.
  - The NAR tree hash survived GitHub's January 2023 archive change, which broke every checksum taken over archive bytes.

== Concatenative languages

- *Factor* is the worked example of mshell's design space.
  It has mandatory stack effects, parse-time resolution and a listener.
  The search order, traced in `core/vocabs/parser/parser.factor`:
  1. closed imports, where the newest silently wins;
  2. open imports (`USING:`), where several matches raise `ambiguous-use-error` with restarts;
  3. numbers;
  4. a not-found fallback that searches *every vocabulary loaded in the image* and can auto-use one.
  The problems:
  - Its own docs say open imports are what make ambiguity errors necessary.
  - Issue \#762 is the "newest closed import wins" rule biting in the listener.
  - Auto-use makes parsing depend on session history.
  - Cycles are not detected: the in-progress vocab is skipped, and you get a misleading missing-word error.
  - Privacy (`.private`) is not enforced.
  - Inline combinators make callers depend on callee *bodies*, which breaks per-vocab checking. mshell's typed quotation parameters already avoid that.
  The best part is the UX: errors propose the import and print a corrected `USING:` line to paste.
- *Forth.*
  - The search order is global mutable state. gforth's manual says: "Using unique names is a better approach."
  - `REQUIRE` load-once is keyed on the path string, and its own source comment admits the problem.
  - *Hyper-static binding*: a word keeps the definition that was visible when it was compiled. Content-addressed modules are that property lifted to whole files, minus Forth's stale-caller and duplicate-generation problems.
- *PostScript.*
  - Names resolve at run time through the dictionary stack.
  - `bind` and `//name` had to be added to say "resolve now".
  - The Ghostscript `-dSAFER` escapes (2016–2019) come from first-class mutable namespaces.
  - The lesson: modules must not be runtime dicts. That warning matters because `:field` makes "a module is a dict" tempting.
- *Kitten* (the nearest typed relative). Resolution produces absolute names before type checking, and signatures are mandatory. Its author's plan, never finished, was private by default with no wildcard imports, because "it should always be possible to grep the source for where an identifier came from".
- *Uiua.* The surface matches your sketch: a file is a module, aliases are chosen by the importer, and an import can list names. Git modules are cached by `(url, subfolder)`, so once a repo is cloned a changed `commit:` is ignored. That is exactly the bug class a required content hash removes. It moved to `.` for qualification.
- *Retro and 8th.* `s:length`-style prefixes act as namespaces with no mechanism behind them.
- *min.* Retrofitting namespaces onto a flat stdlib was its most breaking release.

The concatenative-specific lesson: qualified names read fine in concatenative code as long as each one is *one whitespace-free token*.
The separator question is purely lexical.

== Theory

- *Scope graphs* (Néron, Tolmach, Visser and Wachsmuth, ESOP 2015; later Statix).
  Every fixpoint in practical name resolution has the same root: an import's target is looked up in the same scope the import adds to.
  This covers Rust's import loop, Haskell's export fixpoint and the scope-graph "seen imports" set.
  Remove that circularity and resolution is two linear passes over one file plus its direct imports' export tables:
  - literal (hash) import targets;
  - qualified-only imports;
  - finite, explicit re-export lists;
  - no cycles;
  - a declaration set fixed by syntax.
- *Cardelli, "Program Fragments, Linking, and Modularization" (POPL 1997).*
  This states separate compilation as a theorem.
  If each fragment checks against its import interface (intra-checking) and the interfaces agree (inter-checking), the linked program is well typed without re-checking any body.
  That is the theorem mshell wants (T6).
- *ML module theory* (functors, translucent sums, recursive and mixin modules, Backpack) solves type abstraction and sharing.
  mshell has transparent aliases, fully exposed enums and no functors, so it needs none of it.
  Recursive modules are hard for four reasons: forward signatures, double vision, initialization order, and the fact that a hash cannot express a cycle. No cycles is the right rule.
- *Mechanized and mature systems.*
  - *CakeML* shows that without functors, the module layer is a small proof (finite maps and long identifiers).
  - *Rocq's* `Require`/`Import`/`Export` is the cautionary example:
    - everything loaded is reachable by its full name, even if never required;
    - `Import` order silently changes meaning;
    - `Import` activates notations and hints;
    - hint locality was global by default for years.
  - *Agda*: no cycles, and interface files keyed by source hash.

== A catalogue of mistakes

#table(
  columns: (1.3fr, 1.2fr, 1.6fr),
  [*Mistake*], [*Who paid*], [*Rule here*],
  [Bulk unqualified imports (glob, `open`, `import *`, `USING:`)], [Haskell, Rust, Nim, Zig, Factor, Python, Odin], [Qualified only (@sec-forms)],
  [Implicit prelude that grows unversioned], [Haskell, Rust (needed editions), min], [`VER` fixes the bare-name set (@sec-ver)],
  [Imports that run code or register things], [Go `init`, Python, Perl, Elvish, Node top-level await, Haskell instances, Rocq hints], [Imported files are declarations only (@sec-effects)],
  [Transitive imports or reachable-but-unimported modules], [old Lean, Rocq `Require`, npm phantom deps], [A file sees only its own import lines],
  [Names a module's author chose are global], [OCaml, Haskell `PackageImports`, Swift], [The importer chooses the alias],
  [Identity by path, name or "first found"], [gforth, Factor roots, min, Uiua, Python `sys.path`], [Identity is the hash],
  [Version unification over pins], [Roc], [Never; duplicates coexist (@sec-diamond)],
  [Hashing a transport format (archives)], [Bazel and Homebrew in GitHub's January 2023 change, Roc], [Hash the file's bytes],
  [Semantic (normalized-AST) hash], [Dhall 2.0.0, Unison structural types], [Byte hash plus Merkle import lines],
  [Silent normalization before hashing], [(a hazard)], [Reject CR and BOM in sealed files, never normalize],
  [Ambient environment changes meaning], [`.ghc.environment`, `BASH_ENV`, `NIX_PATH`, `PYTHONPATH`], [No search path; scripts don't load `init.msh` (@sec-init)],
  [Cycles that "work" by skipping a half-loaded file], [Factor, Forth, Python, Elvish], [Cycle error that prints the path],
  [Lazy checking of unreferenced code], [Zig], [Check everything loaded],
  [Name resolution that depends on types], [Koka, Nim], [Resolution is a pass before type checking],
  [Escape hatches that become API], [Elm native modules, Zig `path` deps], [None in v1; overrides deferred (@sec-overrides)],
  [Retrofitting privacy], [Lean `module`, Koka], [Private by default from day one],
)

= Design space and trade-offs

Each subsection states the options, the trade-off, and the recommendation.

== Unit of identity <sec-unit>

#table(
  columns: (auto, 1fr, 1fr),
  [*Unit*], [*For*], [*Against*],
  [Definition (Unison)], [finest dedup; renames free; no conflicts], [needs a codebase database; no text tooling; the propagation UX is hard; finer than anything a person reviews],
  [*File* (Zig, Deno, Lua, Gleam, Nushell)], [the unit people edit, review and `sha256sum`; no tree canonicalization], [a library of many files needs a facade file and pays per-file costs],
  [Directory or package (Go, Rust, Odin)], [one name for many files], [needs a tree hash (NAR, dirhash) and its canonicalization rules (order, modes, symlinks, case, Windows names) as a forever-API],
)

#rec[The file, as you planned. It is the only choice where "the user can hash it themselves" is literally true.]

== What is hashed, and how it is written <sec-hash>

*Byte or semantic hash.*

- A semantic hash survives reformatting.
- It ties identity to the parser forever: any change to the parse tree or its encoding renames the whole ecosystem, as Dhall 2.0.0 did.
- Nobody can check it without `msh`.
- mshell is neither total nor pure, so it has no normal form to hash anyway.

Semantic hashing's real advantage is covering the transitive closure, and Merkle import lines give that for free (@sec-merkle).

*Canonicalization.*
A normalization step is sound only if the checker and the evaluator consume the normalized bytes.
I checked that CR matters inside string literals: `"a\r\nb" len` is 4, while the LF version is 3.
So hashing a CRLF→LF-normalized file while lexing the original would give one hash two meanings.

- *CR:* reject CR bytes in sealed files, with an error that says how to fix it.
  `sha256sum file` then equals the import hash for every accepted file.
  Recommend `*.msh text eol=lf` in `.gitattributes`, and fetch Git *blob* bytes, never an `autocrlf` checkout.
- *BOM:* it is already a lex error (it sticks to the first token).
  Make the error say so plainly.
- *Unicode:* do not normalize.
  The source is UTF-8 by definition, and NFC would change string literals.
- *Trailing whitespace and newlines:* hash exactly.

*Algorithm and encoding.*

- SHA-256, untruncated:
  - It is in Go's standard library.
  - `sha256sum`, `shasum -a 256` and `Get-FileHash` ship on every platform.
  - BLAKE3 would need a third-party package and a `b3sum` nobody has.
  - Truncation saves about 20 characters and halves the collision margin.
- The written form is `'sha256:<64 lowercase hex>'`, as in Dhall and OCI digests:
  - The `sha256:` prefix gives algorithm agility from day one, the lesson of Zig's format churn and Go's `h1:`.
  - Lowercase only means one spelling per hash.
  - Unknown algorithms are rejected.

*Cost.*
I measured Go's `crypto/sha256` on the benchmark machine (FX-8350, no SHA extensions): 113 MB/s, about 0.18 ms for a 20 KB file.
That is below the cost of parsing the same file, so verifying on every load is affordable.
Machines with SHA instructions are an order of magnitude faster.

#rec[
  Identity = SHA-256 of the exact bytes, written `'sha256:<hex>'`.
  Sealed files must be valid UTF-8 with no CR and no BOM, and at most 16 MiB.
  The hash definition is one sentence in the docs plus golden test vectors (the empty file is `sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`).
]

== Where the hash lives: Merkle import lines <sec-merkle>

The options are a lockfile beside the source (Cargo, Gleam, uv, `deno.lock`), a registry (Elm), or the import line itself (Roc, Zig `.zon`, Dhall `sha256:`).

If file A's bytes contain `import b 'sha256:H(B)'`, then `H(A)` commits to `H(B)`, which commits to B's imports, and so on down.
So *the hash of a file pins its entire code tree*, exactly as a Git tree object does.
The consequences:

+ *Single-file reproducibility.* A script stays one file: copy it anywhere, and it pins everything below it. This is a real advantage over manifests for a scripting language.
+ *Cycles are impossible to build.* A cycle would need a SHA-256 fixpoint. The loader still checks, so no proof relies on cryptography.
+ *The update cascade.* Changing a leaf changes the hash of every file on every path above it. Each intermediate published library must publish again. Unison needed a codebase manager for this; mshell needs `msh rehash`/`msh update` (@sec-tooling). This is the main ergonomic cost of the design, and it is paid by library authors, not script authors.
+ *No version resolution.* There is nothing to resolve and nothing to unify.
+ *The location is not the identity.* An import needs a "where" (for fetching) and a "what" (the hash), and meaning comes from the hash alone. Any mirror, vendored copy or paste is equally good.

#rec[Hashes in import lines; no lockfile. Keep the locator in the line as a hint, and never let it affect meaning.]

== Import forms <sec-forms>

The survey's surviving import forms are three:

+ bind a module to an alias;
+ bring in an explicit list of unqualified names;
+ qualified access, `alias.name`.

Glob imports (`*`, `open`, `USING:`, `exposing (..)`, `usingnamespace`) are where every resolution fixpoint, ambiguity rule and "upstream addition broke me" incident in the survey came from.
In mshell a glob would be worse than elsewhere.
A module exporting `status` would silently turn `[git status]` from two strings into a string and a call, with no error at all.

An *explicit list* (`import geo '…' (area perimeter)`) is safe against upstream growth, since hashes pin the list's targets.
Most of the research recommends allowing it.
I recommend leaving it out of v1, for three reasons:

- *The list-literal capture hazard is local but real.* Importing `status` to use once also changes every `[git status]` in the file. Today the same thing happens with a local `def status`, but that def is visible where it is written; an imported name is not.
- *Starting strict is reversible.* Adding the list form later is backward compatible; removing it is a flag day (the Lean lesson).
- *Local definitions already rename.* `def area (geo.Shape -- float) geo.area end` is an explicit, greppable synonym, and `type Shape = geo.Shape` works for types. The cost is restating the signature.

#rec[
  v1 has one import form: `import <alias> <hash> [<locator>]`, with every use written `alias.name`.
  An import adds *no* bare names to a file.
  Revisit explicit name lists when real code shows the verbosity hurts, and add them only with an eager collision check at the import line.
]

== Qualified-name syntax <sec-syntax>

#table(
  columns: (auto, 1.4fr, auto),
  [*Form*], [*Status in mshell today*], [*Verdict*],
  [`geo.area`], [one `LITERAL`; a def named `a.b` already works; `geo.map.` is a prefix quote with head `geo.map`], [*use*],
  [`geo:area`], [`geo` then the getter `:area`: a plausible wrong parse], [no],
  [`geo::area`], [two `COLON` tokens; collides with `:field`, `:1:`; the CHANGELOG mentions an interactive `::` shorthand], [no],
  [`geo/area`], [one literal, but looks like a path, and the REPL treats a first word containing `/` as an executable path], [no],
  [`geo~area`, `geo#area`], [legal but odd; `~/` is home expansion], [no],
)

`.` reads naturally (Go, Python, Deno, Joy, min, and Uiua since 2026).
Floats start with a digit, so a letter-initial alias never collides with a number.
The danger is that dotted words are common command arguments (`[cat notes.txt]`, `[python3.11 x.py]`).
Four rules make it safe:

+ A word `a.b` is a qualified reference *if and only if `a` is an alias imported in this file*.
  Otherwise it is a plain literal, exactly as today, so `[cat notes.txt]` keeps working.
+ A qualified reference must resolve to an export of `a`, *even inside a list literal*.
  It never falls back to a string.
  With an alias `notes`, `[cat notes.txt]` is the error "`notes` has no export `txt`".
  It fails loudly, and the cause is a local edit in plain sight.
+ Declared names (def, type, enum, member) may not contain `.`.
  This is new, and nothing in std uses one.
  It keeps `a.b` unambiguous for readers and for tools.
+ An alias may not equal any bare name visible in the file (a local declaration, a builtin, a std word).
  Aliases and words cannot be confused, which is the Swift `XCTest.XCTest` lesson.

There is one level only: `a.b.c` is an error when `a` is an alias.
Nesting is expressed with re-exports (@sec-visibility), so resolution is always one map lookup.
A trailing `.` keeps its meaning: `geo.each. … end` is a prefix quote over `geo.each`.

#rec[`alias.name`, with the four rules above.]

== Name resolution and shadowing <sec-resolution>

Resolution runs as its own pass, after parsing and before type checking.
It maps every name occurrence (word, type name, enum member in a constructor or pattern) to a *global name*:

- `(file, name)` for a declaration in a file, where `file` is a hash or, for local files, a canonical path;
- `std.name` for the standard library;
- `builtin.name` for a builtin.

The checker, the evaluator and the Rocq model see only global names.
This follows Kitten's pipeline, and Koka shows what happens otherwise.

For a bare word `x` in file F, the priority is fixed:

+ keywords and stack words (lexical, as today);
+ F's own declarations;
+ builtins and std words in the name set of F's `VER` (@sec-ver);
+ inside a list literal, a string; otherwise an "unknown identifier" error.

Overlap between layers 2 and 3 stays an error at the declaration, which is today's no-shadowing rule.
That rule is fine for a file someone can edit.
It is fatal for a frozen file unless layer 3 cannot grow under it, which is what the next decision is for.

#rec[
  Keep the no-shadowing rule.
  It needs no change, because imports contribute no bare names.
  Resolution is a function of F's text, the export tables of F's direct imports, and the name set of F's `VER`.
]

== `VER` and the standard library <sec-ver>

A hash pins bytes.
What those bytes mean depends on the interpreter and std they run under.
Today `VER` pins both for a *root* script by re-running it with `msh-<ver>`.
One process cannot run two interpreters, so a v0.15 script importing a v0.12 library runs the library on v0.15.
That leaves two hazards:

- *Collision.* The library defines `chunk2`, and v0.14 adds `std.chunk2`. Under no-shadowing the library no longer loads, and its users cannot fix a hashed file.
- *Capture.* The library has `[ls -l]`, and std adds `ls`. The list silently becomes a call.

The options:

#table(
  columns: (auto, 1.3fr, 1.3fr),
  [*Option*], [*How*], [*Cost*],
  [A. One `VER` per program], [Libraries run under the root's `VER`; a library's `VER` is a minimum.], [Both hazards stay. You would need two more rules: locals shadow std in sealed files, and sealed files lose the bare-word-to-string fallback (all command words quoted).],
  [*B. `VER` is an edition for names*], [Every imported file declares `VER`. The bare words of a file resolve against the builtins and std words that existed at its `VER`. The interpreter's version must be at least every file's `VER`.], [A table from each builtin and std name to the release that introduced it (and removed it, if ever), generated at release time from tags. Removed or changed builtins remain a normal compatibility problem.],
  [C. Every file pins a std hash], [std becomes an ordinary sealed module per version.], [Builtins live in Go and change too; this only solves half and adds a second std mechanism.],
)

Option B is the only one under which stability under extension (T2) holds without changing any existing rule.
That covers the no-shadowing rule and the string fallback.
It is Rust's edition idea scoped to the one thing that grows: the set of names.
The table is data, and generating it can be a step in the release checklist in `CLAUDE.md`.

#rec[
  Option B.
  `VER` is required in every imported file.
  For a root script it keeps its current meaning, which also selects the binary.
  Root scripts without `VER` use the running binary's name set, as today.
  I list this as an owner decision anyway, because it adds a release step.
]

== Visibility, export and re-export <sec-visibility>

- *Private by default* (Lean `module`, Koka, Gleam, Kitten's plan).
- *An explicit `export` line in the header* (Elm, Factor exports, PowerShell manifests).
  You can read a file's interface from its first lines, and so can a tool, without type checking the body.
- *Re-export by name*: `export s.area` makes `area` an export of this file whose global name is still `(H(shapes), area)`.
  Reaching the same def through two facades is then not a clash, which is Haskell's entity rule.
- *No `export *`.*
  ECMAScript silently drops names that two star-exports both provide, so adding a name upstream can *remove* one downstream.
  A hard-error variant would still break a facade when a dependency grows.
- *Exporting an enum exports all its members.*
  Exhaustiveness needs every constructor (`tw_case`), so members cannot be exported one by one.
- *No private enum in a public signature* (Lean; Rust RFC 2145).
  A private `type` alias is fine: it is transparent and expands in the interface.
- *Interfaces are type-closed.*
  A file's interface includes the declaration of every enum its exported signatures mention, even when that enum comes from a deeper file.

Re-exports exist for Deno-style facades, but the facade has a cost the Deno research spells out: barrel files.
`geo.msh` re-exporting 20 files makes every importer fetch, hash, parse and check 20 files to use one word.

#rec[
  `export` lines in the header, listing local names and `alias.name` re-exports.
  No renaming on export in v1; a local wrapper def renames.
  Write the docs to steer library authors toward one-file libraries, with facades for when a library really is large.
]

== What an imported file may contain <sec-effects>

The scripting research summarizes it in one table (§11 of `03-scripting-shells.md`).

- Nushell makes top-level commands in a module a parse error, and has no reported problems from that rule.
- Everyone who allows top-level code got at least one of these:
  - partially initialized cycles;
  - order-dependent registration;
  - "checking runs code";
  - import order becoming semantics forever (Python's PEP 690).

mshell's std.msh already has no top-level code.

#rec[
  An imported file contains only `VER`, `import`, `export`, `def`, `type` and `enum`.
  Any other top-level item is an error at the import site ("x.msh has top-level code; only declaration files can be imported").
  Constants are zero-input defs (`def pi (-- float) 3.14159 end`), so there is no separate constant form.
  `'complete'` metadata in an imported file registers nothing (@sec-init).
  Never add macros or compile-time evaluation: they are what break "type checking untrusted code is safe".
]

With this rule, loading is parsing, load order cannot matter, two copies of a library cannot interact, and `msh --type-check-only` never executes user code.
A def body can still do anything when it is *called*; only importing is inert.

== The development loop: sealed and local files <sec-dev>

"The hash is required to run", applied to a script split into `main.msh` and `util.msh`, means every edit to `util.msh` invalidates `main.msh`'s import line.
With three levels, one edit means a cascade of rewrites.

#table(
  columns: (auto, 1.4fr, 1.2fr),
  [*Option*], [*Rule*], [*Cost*],
  [Strict], [Every import has a hash. `msh rehash main.msh` rewrites local hashes bottom-up; the error on a mismatch prints that command.], [A rehash after every edit, or a file watcher. The theorems are the cleanest.],
  [*Sealed/local split* (Dhall)], [A file reached through a hash is *sealed* and may import only by hash. The root, and files the root imports by relative path without a hash, are *local*. A local file may import local files by path without a hash. `msh freeze` turns local imports into hashed ones bottom-up, for publishing.], [Determinism (T3) becomes: the program is a function of the root's bytes, the local files' bytes, and the store. Sealed subtrees stay a function of their hash.],
  [Escape-hatch flag], [`--no-verify` or a dev environment variable.], [An escape hatch that becomes API (Zig `path`, Elm); avoid.],
)

The purposes you gave for hashing are de-duplication, caching and security.
All three concern code you did not write or do not control.
A file next to your script, which you are editing, needs none of them.
Its bytes are read from disk on every run, so nothing can be stale.

#rec[
  The sealed/local split.
  A hash is required to *enter* sealed code, and sealed code can never reach unhashed or local code.
  A local import is a relative path only.
  It is resolved against the importing file's directory, never against the cwd, `PATH` or a search path.
  This is the one place I recommend departing from your notes, so it is an owner decision.
]

== Diamonds, duplicates, and nominal types <sec-diamond>

Suppose `A → B → D@h1` and `A → C → D@h2`.
Both copies of D load; there is no resolution step to fail and no silent unification.
Whether that matters depends on two properties, and the design gives both:

- *No module state.* Modules hold no top-level code and therefore no state, so two copies cannot diverge. That is Node's dual package hazard, ruled out by construction.
- *Structural types interoperate.* Lists, dicts, shapes and `type` aliases are structural, so values pass freely between code from the two copies.

Enums are nominal.
An enum's identity is `(file, name)`, so `D@h1.Shape` and `D@h2.Shape` are different types.
That is correct: the two files may define different members.
It needs a clear error:

```
type error at app.msh:12:9: 'c.draw' takes a Shape from
  sha256:4c1e…  (geo.msh, imported by c.msh:3)
but this is a Shape from
  sha256:9a07…  (geo.msh, imported by b.msh:4)
These are two versions of geo.msh. `msh deps app.msh` shows both.
```

The same hash reached by two paths is one file, so dedup is automatic.

#rec[Duplicates allowed, enum identity `(file, name)`, and a diagnostic that names both hashes and both import paths.]

== Cycles <sec-cycles>

The sealed graph cannot have a cycle.
The local graph can, because path imports are not hashed.
mshell already allows mutual recursion *within* a file, so a cycle between files is never necessary.

#rec[
  A cycle is an error that prints the cycle, with each file and import line.
  Put it in the spec, so nobody later adds an `.hs-boot`-style escape.
]

== Provenance: locators, store, fetching <sec-provenance>

*Locators* are hints for fetching, and are part of the importing file's bytes:

- `'./x.msh'`, `'../lib/x.msh'`: relative to the importing file's own location. For a fetched file, that is the location it was fetched from, as in Deno.
- `'https://host/path/x.msh'`: HTTPS only, redirects re-validated (Roc), size-capped.
- `'gh:owner/repo@<40-hex commit>/path/x.msh'`: the full commit, never a branch or tag, because refs move (Uiua's floating default, `boltdb-go`). It is fetched as raw blob bytes, never a generated archive.

*The store* is `$XDG_DATA_HOME/msh/store/sha256/<hex>.msh`.
It is data, not cache: if the host disappears, the store is the only copy (left-pad).
Files are written to a staging name, verified, renamed atomically (Roc, Zig), and made read-only.
Every load re-hashes, at a cost of about 0.18 ms per 20 KB on the slowest machine (@sec-hash).
The theorem then needs no "the store was not tampered with" hypothesis.

*Lookup order for a sealed import:*

+ the store, by hash;
+ if the locator is relative and the importing file was read from disk, the file at that path, verified against the hash;
+ otherwise, fail with the fetch command.

A mismatch is always fatal and never falls through to another source (Dhall's `?` rule).

*Network.*
Should `msh script.msh` fetch a missing module itself, as Deno does?
- *For.* It is convenient, and fetching is safe because the hash verifies the content.
- *Against.* A shell script that quietly reaches the network is surprising, and it couples "does it run" to "is GitHub up".

Explicit is your stated preference.

#rec[
  Running never touches the network.
  `msh fetch script.msh` fills the store for a file's whole tree.
  A missing module is an error naming the file, the hash and the command to run.
  This is an owner decision, and an easy one to relax later.
]

Error messages follow Go and Zig: expected and actual hash, where the expectation came from (file and line), a one-line threat model, and the exact fix:

```
error: hash mismatch for import 'geo' at app.msh:3
  expected sha256:997e46a5c14e29d5aff36d7f01c3c48154143cfd99b35c64a928f78d97c5f502
  ./vendor/geo.msh has
           sha256:5d2f0c7a…
The file changed after app.msh pinned it. If you changed it, run
  msh rehash app.msh
Otherwise someone else changed it; the pinned version is not in the store.
```

== Type checking and caching <sec-checking>

- *Eager.* Every def in every loaded file is checked, whether or not it is reachable. This avoids Zig's lazy-analysis problem: broken code that ships because nothing called it.
- *Strict for imports.* A file that does not check cannot be imported, and the import line is the error site. Today's lenient policy for broken startup defs ("refused if called, warning otherwise") stays for std and init only.
- *Separate.* Each file is checked against its own declarations, the type-closed interfaces of its direct imports, and the builtin and std signatures of its `VER`. Nothing else is in scope: no init, no top-level variables, no other files. Checking order is any topological order (T5).
- *Cacheable.*
  - A sealed file's verdict and interface are a function of (checker version, file hash), because its dependencies' interfaces are pinned by its bytes.
  - A local file's key is (checker version, hash of its bytes, interface hashes of its direct imports).
  - Interface hashes also allow early cutoff: if a local dependency's body changes but its interface does not, its importers need not be re-checked.

Within one process, each module can become one frozen overlay of the base arena (`TypeArena.Overlay`, `NameTable.Overlay`), built in topological order.
A cache across processes needs the arena serialized or re-interned.
Measure first; with a handful of small modules, checking from scratch may well be cheaper than reading a cache.

#rec[
  v1: eager, strict, separate checking with in-process overlays, and no persistent cache.
  Design the interface hash now, so a persistent cache is a pure optimization later (T7 makes its key provably sound).
]

== `init.msh`, completions, and interactive use <sec-init>

*Scripts and `init.msh`.*
Today a script declaring `VER` *requires* the versioned `init.msh`, and the script runs on the stack, variables and env that init left.
Every script's meaning therefore depends on an unhashed, per-user file.
This repeats `BASH_ENV`, `.zshenv`, fish's `config.fish`, Python's `.pth` and GHC environment files; each was later restricted.
It also makes "the code tree is a function of the root" false before modules even exist.

#owner[
  I recommend that scripts stop loading `init.msh`.
  Interactive sessions (and `-c` when stdin is a terminal) keep it.
  A script that wants your helpers imports them, with a hash, like any other code.
  This changes current behaviour: your own init.msh defines `f`, which a script could already be calling.
  So it needs a CHANGELOG entry and probably a release of warnings first ("this script calls `f` from init.msh").
  In any case, imported files never see init.msh.
]

*Completions.*
`'complete'` metadata registers a def by command name as a side effect of loading.
On import that would be an orphan instance: a global effect that the importing file cannot see or control.

- *Rule:* metadata is honoured only for defs declared in std, init.msh, and the root script itself.
- *For a completion library:* init.msh imports it and registers explicitly, with a small new builtin, for example `'git' (gitc.complete) registerCompletion`.
- *The planned snippets directory* is a set of extra *roots* loaded by the interactive shell, autoload-style (fish), not imports.

*The REPL* is a local root.

- `import` works on a REPL line, and an alias stays bound for the session.
- Nobody types 64 hex digits at a prompt. Two paths keep that from mattering:
  - init.msh's imports are bound in the session (init is its first line);
  - `msh lookup` prints a ready line to paste.
- Re-importing the same alias with the same hash is a no-op, and with a different hash it is an error, consistent with no redefinition.
- Content-based reload of a local import (Nushell compares file contents) is a later nicety.

== Overrides and updates <sec-overrides>

*Overrides.*
A security fix in a deep dependency D currently means every file between you and D republishes.
The alternative is a root-level line such as `override 'sha256:OLD' 'sha256:NEW'` (Nix `follows`, Go `replace`, npm `overrides`).
It would be in the root's bytes, so the root hash still pins it.

- *The cost.* B was checked against `iface(D@OLD)`, so it must be re-checked against `iface(D@NEW)`, and cache keys must then include resolved interface hashes. "The hash says it all" gains a second mechanism.
- *Recommendation:* none in v1. If the cascade proves painful in practice, add root-only overrides (never in a library, as Go does), with mandatory re-checking.

*Updates* are tooling:

- `msh update <alias> [<ref>] file.msh` resolves a new commit and rewrites the line.
- `msh rehash` propagates the change upward through local files.

The locator must keep enough to re-resolve, which is why `gh:` locators carry the repo and path as well as the commit.

= Proposed design

== Two kinds of file

#defn("Sealed and local")[
  The *root* is the file `msh` was asked to run (or the REPL session).
  An import with a hash is *sealed*: the imported file is sealed.
  An import without a hash is *local*: it is allowed only in the root or a local file, and its locator must be a relative path.
  Every file reached from a sealed file is sealed.
]

#table(
  columns: (auto, 1fr, 1fr, 1fr),
  [], [*Root*], [*Local file*], [*Sealed file*],
  [Identity], [its path], [canonical path], [`sha256` of bytes],
  [Top-level code], [yes], [no], [no],
  [`VER`], [optional (as today)], [required], [required],
  [Imports allowed], [sealed, local], [sealed, local], [sealed only],
  [CR or BOM bytes], [allowed], [allowed], [rejected],
  [Sees init.msh], [interactive only (@sec-init)], [never], [never],
)

== File layout

```
#!/usr/bin/env msh                      # optional shebang and comments
VER "v0.15.0"                           # header: VER, then imports, then exports
import <alias> '<sha256:…>' '<locator>' # sealed import (locator optional)
import <alias> '<relative path>'        # local import (root and local files only)
export <name> <alias>.<name> …          # one or more export lines
                                        # body: def, type, enum (+ top-level code in the root)
```

The header is a fixed, line-oriented region: one import per line, and nothing but comments between header lines.
Tools rewrite import lines and nothing else.
They byte-preserve everything outside those lines and write atomically.
That is the lesson of PEP 723 and uv, and of Spago leaving Dhall.
An `import` or `export` after the first body item is a parse error.

== A worked example

A library in two files plus a facade.
The hashes are real `sha256sum` output for the files shown.

`shapes.msh`:

```
# shapes.msh: plane shapes.
VER "v0.15.0"
export Shape area perimeter

enum Shape = circle float | rect float float end

def pi (-- float) 3.14159 end

def area (Shape -- float)
    match
        circle r : @r @r * pi *,
        rect w h : @w @h *,
    end
end

def perimeter (Shape -- float)
    match
        circle r : @r 2.0 * pi *,
        rect w h : @w @h + 2.0 *,
    end
end
```

`pi` is private.
Exporting `Shape` exports `circle` and `rect`.

`units.msh`:

```
# units.msh: length conversions.
VER "v0.15.0"
export feetToMeters

def feetToMeters (float -- float) 0.3048 * end
```

`geo.msh`, the facade:

#small[
```
# geo.msh: the public face of the geo library.
VER "v0.15.0"
import s 'sha256:f7e82352ef7d55a769f406236c7825b006ec290539fa3bee6108defbb82084c4' './shapes.msh'
import u 'sha256:d59339e8a9ca900ebf8a754f7237b03f8bdbfc5fc365614e0667e0b333c2e469' './units.msh'
export s.Shape s.area s.perimeter u.feetToMeters
```
]

`sha256sum geo.msh` gives `997e46a5…f502`.
That one hash commits to all three files.

A script that uses it, with a local helper:

#small[
```
#!/usr/bin/env msh
VER "v0.15.0"
import geo 'sha256:997e46a5c14e29d5aff36d7f01c3c48154143cfd99b35c64a928f78d97c5f502' 'gh:mitchpaulus/geo@0b9d1c4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c/geo.msh'
import h './helpers.msh'

def describe (geo.Shape -- str)
    match
        geo.circle r : "circle",
        geo.rect w h : "rectangle",
    end
end

[2.0 geo.circle  3.0 4.0 geo.rect] shapes!
@shapes map. geo.area end sum h.show
@shapes (describe) map uw
[cat notes.txt];          # 'notes.txt' is a string: 'notes' is not an alias
```
]

`helpers.msh` is a local file: `VER`, `export show`, and `def show (float -- ) str wl end`.
No hash, and edits take effect on the next run.

Before publishing `helpers.msh` as part of something else, `msh freeze script.msh` hashes it and rewrites the line.

== Resolution, precisely

For each file F, with `A` its alias map (alias to the imported file's interface), `D` its declarations, and `N_v` the builtin/std name set of its `VER` `v`:

```
resolve(F, x):                          -- x is a word, type name, or member
  if x has the form a.b:
    if a ∈ dom(A):                      -- qualified
      if b ∈ exports(A[a]): return global name of A[a].b
      else: error "a has no export b"   -- even inside a list literal
    -- otherwise x is a plain literal and falls through
  if x ∈ D:   return (F, x)
  if x ∈ N_v: return std.x or builtin.x
  if inside a list literal: return the string x
  error "unknown identifier x"
```

These checks run once per file, before resolution:

- `D` and `N_v` are disjoint (today's no-shadowing rule);
- `dom(A)` is disjoint from `D ∪ N_v`;
- no name in `D` contains `.`;
- aliases are unique;
- every export resolves.

There is no iteration, no search path and no global knowledge.

== Loading, precisely

```
load(root):
  parse the header of root; for each import, recursively:
    sealed:  bytes := store[h] or (relative locator from a disk file, verified)
             check H(bytes) = h; reject CR/BOM; parse; reject top-level code
             reject unhashed imports inside it
    local:   bytes := read(path relative to importer); parse; reject top-level code
  track an in-progress set; a back edge is a cycle error with the path
  the result is a DAG with each file once (sealed by hash, local by canonical path)
check(DAG):  in topological order, check each file against its direct imports' interfaces
run:         evaluate only the root's top-level code (and, interactively, init's)
```

Loading reads headers and bodies but evaluates nothing.
`--type-check-only` runs exactly `load` and `check`.

== Tooling <sec-tooling>

Required for v1, because the Merkle design is not usable without them:

#table(
  columns: (auto, 1fr),
  [*Command*], [*Does*],
  [`msh hash FILE`], [Prints `sha256:<hex>`, the same as `sha256sum`. The documented primitive.],
  [`msh lookup LOCATOR`], [Fetches, verifies the file is a valid sealed module, stores it, and prints the import line with its hash and the resolved commit.],
  [`msh add ALIAS LOCATOR FILE`], [`lookup` plus an atomic rewrite of FILE's header. Idempotent: re-adding the alias updates the line.],
  [`msh fetch FILE`], [Fills the store for FILE's whole tree. The only command that touches the network besides `lookup` and `add`.],
  [`msh rehash FILE`], [Recomputes the hashes of sealed imports whose locator is a relative path, bottom-up, and rewrites their lines.],
  [`msh freeze FILE`], [Turns FILE's local imports into sealed ones, bottom-up (Dhall `freeze`).],
  [`msh deps FILE`], [Prints the tree: alias, hash, locator, and duplicates (Roc `roc deps`).],
)

Later: `msh update`, `msh diff OLD NEW` (an API diff from signatures, as in Elm's `toMagnitude`), `msh audit` (effectful builtins reachable per module), and `msh bundle` (inline a tree into one self-contained script).
The language server gains go-to-definition (the file of a global name is known), `alias.` completion, and an "add import" code action (Factor's restarts, never applied automatically at run time).
Per `CLAUDE.md`, new subcommands also need updates to `__mshCompletion` and `msh completions <shell>`.

= What can be proven <sec-proof>

The value of the restrictions is that the theorems become short.
The candidates, ordered by value per unit of effort, are from `05-theory-integrity.md`, with the `VER` editions of @sec-ver substituted for its "pinned std" assumption.

#table(
  columns: (auto, 2.2fr, auto, auto),
  [*\#*], [*Guarantee*], [*Value*], [*Rocq effort*],
  [T1], [*Local, unique resolution.* Every name occurrence in F resolves to at most one global name, computed from F, the interfaces of F's direct imports, and `N_v`. The algorithm matches a declarative scope-graph-style relation.], [high], [~300 lines],
  [T2], [*Stability under extension.* Adding declarations to a dependency, or to std/builtins at a later version, never changes the resolution, typing or meaning of a file that already checked.], [very high], [~200 on T1],
  [T3], [*The code tree is determined by the root.* With consistent stores (hash injective on the files they hold), the loaded sealed program is a function of the root hash. With local files, it is a function of the root's bytes, the local files' bytes, and the store. The environment affects only whether loading *succeeds*.], [high], [~200],
  [T4], [*Finite DAG.* A successful load gives a finite acyclic graph, every node reachable from the root, in topological order.], [medium], [~400],
  [T5], [*Inert, order-independent loading.* Linking is a disjoint union keyed by global names, so every topological order gives the same environment. Loading evaluates nothing.], [medium–high], [~150],
  [T6], [*Modular soundness* (Cardelli). If every file checks against its direct imports' type-closed interfaces and the interfaces are honest, the linked program satisfies `gdefs_ok`, and `soundness_generic` applies to the root.], [highest], [800–1500, 1–2 weeks],
  [T7], [*Sound cache keys.* The verdict for F is a function of (checker, `VER`, F's bytes, the interface hashes of F's direct imports).], [high for speed], [small after T6],
)

*Strategy: translate, don't extend the core.*
The module layer elaborates into the existing core, the F-ing-modules approach:

- resolved names become `WCall "<file>.<name>"`;
- enum names become `"<file>.<Enum>"`.

`ename_eqb` is string equality, so the 15k-line soundness proof is reused unchanged.
There are two new obligations:

- *A `T`-weakening lemma over `g_sigs`.* `g_sigs` is used only positively (`tw_call`), so ordinary inclusion suffices. This is mechanical, but `TW` has about 40 constructors, so budget 2–4 days.
- *Constructor-table agreement.* `tw_case` quantifies over `g_ctors`, so linking needs `g_ctors` *agreement*, not inclusion. It holds by construction: an enum's table comes only from its defining file, which its global name fixes. That makes it a lemma about `link`, not about typing.

The model, in outline:

```coq
Inductive qname := Bare (x : name) | Qual (a : alias) (x : name).
Inductive gname := GDef (f : fileid) (x : name) | GStd (x : name) | GBuiltin (x : name).

Record file := {
  f_ver     : version;
  f_imports : list (alias * import_target);   (* Sealed h | Local p; aliases NoDup *)
  f_decls   : list (name * decl);             (* names NoDup, no '.' *)
  f_exports : list qname;                      (* explicit; Qual a x re-exports *)
  f_body    : option (list rword)              (* Some only for the root *)
}.

Definition resolve (N : version -> name -> option gname) (fid : fileid) (F : file)
                   (ifc : alias -> option iface) (q : qname) : option gname := ...

Theorem res_unique    : ...                               (* T1 *)
Theorem resolve_stable :                                   (* T2 *)
  (forall x g, N v x = Some g -> N v' x = Some g) ->       (* only for v' at least v, as an edition *)
  (forall a I, ifc a = Some I -> exists I', ifc' a = Some I' /\ iface_ext I I') ->
  resolve N fid F ifc q = Some g -> resolve N fid F ifc' q = Some g.
Theorem load_det  : consistent S S' -> Loaded S h P -> Loaded S' h P' -> P = P'.   (* T3 *)
Theorem modular_soundness : Loaded S h P -> intra_checks P -> ifaces_honest P ->
  let (sigs, defs) := link P in forall n, eval defs n ... (elab_root P h) <> RStuck. (* T6 *)
```

Hash collisions enter only as the hypothesis `consistent S S'`, never as an axiom, so `make check` stays closed under the global context.
Under your prove-first rule, the order is:

+ T1–T5, about a week. These fix the restrictions before any Go exists. Each hole found becomes a stuck example in `Examples.v`, as in the type-system work.
+ T6 and T7 before the persistent cache.

Out of scope for the model: fetching, the store layout, and the cache implementation.
The Go cache must key on exactly the T7 arguments.
A test can partly check that: perturb anything outside the key and assert that the verdict does not change.

= The engineering ledger <sec-ledger>

What each rule buys and what it costs, stated plainly.

#table(
  columns: (1fr, 1.3fr, 1.3fr),
  [*Rule*], [*Buys*], [*Costs*],
  [Byte hash, Merkle lines], [single-file reproducibility; no lockfile; cycles impossible; `sha256sum` verifies], [an update cascade through every intermediate library; whitespace edits change identity; a 64-hex token per import],
  [No version resolution], [the root hash means one program; no SAT, MVS or ranges], [duplicate copies in diamonds; two versions' enums don't mix; security fixes need republishing (or later overrides)],
  [Qualified only], [T1 and T2 nearly trivial; greppable origins; no list-literal capture from imports], [`geo.` on every use; wrapper defs to shorten names],
  [`VER` editions], [std can grow without breaking any published module; T2 holds with no-shadowing and the string fallback intact], [a name→version table maintained at release; required `VER` lines in libraries],
  [Declarations only], [inert loading; order irrelevant; safe checking of untrusted code; harmless duplicates], [no module-level configuration or state; libraries can't self-register completions],
  [Sealed/local split], [a normal edit-run loop for multi-file scripts], [T3 is weaker for local files; one more concept to document],
  [No implicit network], [a script never reaches the network by surprise; availability failures are explicit], [an extra `msh fetch` step on a new machine],
  [Strict, eager checking], [nothing broken ships unnoticed in a library], [every loaded def is checked on every run until a persistent cache exists],
  [Private by default, explicit exports], [interfaces readable from the header; the cache can key on interfaces], [export lines to maintain],
  [Scripts skip init.msh], [scripts mean the same on every machine], [a breaking change; users must import their helpers],
)

The biggest single cost is the update cascade.
It is the same structural price Go pays with `/v2` paths and Unison pays with `update`, paid on every release instead of every major version.
It falls on library authors, and mostly on authors of libraries of libraries.
At script scale, which Deno itself now says is where URL imports work, it is small.
If it turns out not to be small, the answer is root overrides (@sec-overrides), not version ranges.

= Rejected alternatives

#table(
  columns: (1fr, 2fr),
  [*Alternative*], [*Why not*],
  [Per-definition identity (Unison)], [needs a code database; loses text tooling; you chose the file],
  [Semantic (normalized-AST) hash (Dhall)], [ties identity to the parser forever; can't be checked with `sha256sum`; mshell has no normal form],
  [Directory as module (Go, Rust)], [needs a tree hash and its canonicalization (order, modes, symlinks, case, Windows names) as a forever-API],
  [Lockfile or manifest (Cargo, Gleam, uv)], [a copied script loses its pins; Merkle lines need none],
  [Semver ranges, MVS, unification (Roc)], [contradict "the hash is the identity"; version solving with richer constraints is NP-complete],
  [Glob or `open` imports], [ambiguity rules, upstream additions breaking downstream code, list-literal capture],
  [Unqualified explicit name lists in v1], [deferred, not rejected; local wrapper defs rename for now],
  [`mod:word`, `mod::word`, `mod/word`], [conflict with `:field`, indexing and paths (@sec-syntax)],
  [Modules as runtime dicts (min, PostScript)], [makes resolution a run-time property; Ghostscript SAFER CVEs],
  [Top-level code or `init()` in modules], [order-dependent effects; partially initialized cycles; "checking runs code"],
  [Lazy checking (Zig)], [broken unreferenced code ships],
  [Type-directed resolution (Koka, Nim)], [couples resolution to stack-effect inference],
  [Cycles via forward declarations (`.hs-boot`, `DEFER:`)], [within-file mutual recursion covers the need; cycles break Merkle identity],
  [Search paths (`MSHPATH`, `sys.path`, `NU_LIB_DIRS`)], [ambient resolution; the `random.py` and `.`-in-`@INC` class of bugs],
  [Capability annotations on imports (Deno permissions)], [`['sh' '-c' …]` gives full authority anyway; security theatre at real complexity cost; `msh audit` is the cheap substitute],
  [Functors, Backpack, mixins, local modules], [solve problems mshell does not have; quotations already parameterize behaviour],
  [Namespacing the stdlib (`str.len`)], [min's most breaking release; one owner, pinned by `VER`: flat names are fine],
  [Signing, TUF, a sumdb-like transparency log], [run-time identity is already the hash; revisit only if a name registry appears],
)

= Decisions for you, and things I could not resolve <sec-decisions>

== Decisions

+ *Sealed/local split* (@sec-dev): relax "hash required to run" to "hash required to enter sealed code"? I recommend yes. The alternative is strict hashing with `msh rehash` on every edit.
+ *Scripts and `init.msh`* (@sec-init): stop loading init for scripts? I recommend yes, behind a deprecation period. In any case, imported files never see it.
+ *`VER` editions* (@sec-ver): Option B (a per-file name set with a release-time table), or Option A (a single root `VER` plus "locals shadow std in sealed files" and quoted command words in sealed files)?
+ *Network on run* (@sec-provenance): never (recommended), or fetch on a store miss?
+ *Unqualified name lists*: leave them out of v1 (recommended) or include them with eager collision checks?
+ *Overrides* (@sec-overrides): none in v1 (recommended), or root-only from the start?
+ *Keyword spelling.* Lexer keywords are recognized everywhere, including inside list literals, so making `import` and `export` keywords would break command lines like `[docker export c]` and `[podman import f]`.
  The options:
  - *contextual keywords*: `import`/`export` are keywords only as the first word of a header line. This is what I wrote throughout, and it is my recommendation.
  - *uppercase* `IMPORT`/`EXPORT`, following `VER`.
  Nothing in std uses either spelling as a bare word.
+ *`gh:` locators*: commit-only (recommended), or also record the ref the user asked for, to help `msh update`?

== Things I did not understand or could not find

- The CHANGELOG (v0.13.0 entry, line 572) describes "the interactive `::` CLI shorthand", but I could not find an implementation in `Main.go` or `SimpleCliParser.go`. Is `::` reserved or used? It does not affect the recommendation (`.`), but it rules `::` in or out for anything else.
- `Main.go` and `lsp.go` cite `ai/startup-checking.md`, which does not exist.
- `CheckDefinitionNames` cites a "design doc, 'Names'" that I could not find in `design/` or `ai/`.
- Several research claims are from the agents' memory rather than re-read sources, and are marked as such in the reports: OCaml warning numbers, Stack's Backpack support, Hare's cycle rule, Odin's cycle rule, and several Deno internals. None of them carries a recommendation here.

= Incidental findings <sec-incidental>

These came up while reading the implementation.
They are independent of modules, and none was changed.

+ *Generic capture by startup types* (reproduced). With `type T = int` in init.msh, std.msh's generic `T` becomes `int` in every signature (`NewCoreBase` declares all startup decls before resolving any signature, `TypeCore.go:115-161`). `foldl`, `concat` and several completion defs then fail on every run. A script's own `type a = int` does the same to its own `(a -- a)` signatures. Per-file type scopes fix the cross-file case. A direct fix for both is to reject type names that match the generic pattern (one letter, optional digits).
+ *Inverted startup dependency.* std.msh can call a def that only init.msh defines (verified; no warning). Modules fix it structurally; until then, check std as if init did not exist.
+ *Nested `def` or `type` in a def body* is not rejected by the parser. The checker then reports "the type checker has no rule for 'def'; please report this".
+ *Checker-only names.* `NewCoreBase` silently skips a startup def whose name is in the checker's core table but not in `BuiltInList` (`TypeCore.go:133-138`). There is no current collision.
+ *Stale design docs.* `ai/type-system-plan.md:171` and `ai/type-system-progress.md` still describe the lazy base removed in \#356.
+ *VS Code grammar.*
  - `x!` is scoped `variable.other.retreive` and `@x` is scoped `variable.other.set`; they look swapped.
  - The keyword regex `\b(…)\b` would highlight `str` inside `str.trim`, and variable patterns are ASCII-only with no `.`.
  - `.` qualification will need grammar updates in `code/`, `sublime/` and `Notepad++/`.

= Implementation impact

For when this moves past design.
File and line references are from `00-mshell-internals.md`.

- *Lexer:* no change for `alias.name`. `import` and `export` are recognized only as the first word of a header line, not as global keywords (decision 7).
- *Parser:* header grammar (`VER`, `import`, `export`); reject imports and exports after the body starts; reject top-level items in non-root files; reject `.` in declared names; also reject nested `def`/`type`.
- *Resolver:* new pass producing global names for words, type names and members, in every syntactic position (calls, prefix quotes, signatures, `as`, `tryAs`, `match` patterns, enum payloads).
- *Loader:* store, hashing, CR/BOM checks, cycle detection, sealed/local rules, all in `Main.go` beside `loadStartupDefinitions`.
- *Evaluator:* the def table is keyed by global name rather than spelling (`lookupDefinition`, `defIndex`). The `completionDefs` builtin must call a def by identity, not by its bare name (`Evaluator.go:6486`).
- *Type checker:*
  - a per-file declaration scope, which also fixes the generic-capture bug;
  - a base per `VER` name set;
  - one overlay per module in topological order;
  - interface extraction and hashing.
- *Name sets:* the name→version table, generated at release, plus a release-checklist step in `CLAUDE.md`.
- *LSP and REPL:* `alias.` lexes as a prefix quote while it is being typed, so completion needs a branch for it. Also go-to-definition, hover keyed by global name, and an alias-aware `IsDefinitionDefined`.
- *Editors:* `code/`, `sublime/`, `Notepad++/`.
- *Docs:* user docs and `doc/mshell.md`, limited to user-facing behaviour.

= References

The six research reports in `ai/modules-research/` carry full citations, source paths and quotes:

- `00-mshell-internals.md`: the current implementation, with file and line references.
- `01-typed-functional.md`: Haskell, OCaml/SML, Elm, Roc (source), Gleam (source), Lean 4, Unison, Koka.
- `02-systems.md`: Go (cmd/go, x/mod), Rust, Zig (`Fetch.zig`), Odin, Hare, Nim, D, C++20, Swift.
- `03-scripting-shells.md`: Deno, Node, Python and PEP 723, Lua, Ruby, Perl, Tcl, PHP, bash, fish, zsh, PowerShell, Nushell (source), Elvish (source), YSH (source), Dhall (standard), Nix.
- `04-concatenative.md`: Factor (source), Forth/gforth (source), PostScript, Kitten (source), Uiua (source), Retro, min, Joy, 8th.
- `05-theory-integrity.md`: scope graphs, Cardelli 1997, ML module theory, CakeML, Rocq, Lean, Agda, content addressing, supply chain, and the theorem sketch.

Key primary sources:

- Néron, Tolmach, Visser, Wachsmuth, _A Theory of Name Resolution_, ESOP 2015.
- Cardelli, _Program Fragments, Linking, and Modularization_, POPL 1997.
- Rossberg, Russo, Dreyer, _F-ing Modules_, JFP 2014.
- Rust RFC 2126 (path clarity) and RFC 1105 (API evolution).
- Zig issue \#20663 (remove `usingnamespace`) and `src/Package/Fetch.zig`.
- Go: `research.swtch.com/vgo-import`, `vgo-mvs`; the sumdb design (proposal 25530).
- Ryan Dahl, "What we got wrong about HTTP imports", deno.com/blog/http-imports, 2024.
- Dhall standard, `standard/imports.md` and `standard/versioning.md`.
- Lean reference manual, "The Module System".
- PEP 723 and the uv scripts guide.
- Factor `core/vocabs/parser/parser.factor` and issue \#762.
- Kitten issue \#117.
- Nushell `crates/nu-parser/src/parse_module.rs`.
