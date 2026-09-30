# Build and check the mechanized core.  Needs the Rocq prover (tested with 9.1.1).
#   make          build everything
#   make check    build, then print the axioms the main theorems depend on
#   make clean
ROCQBIN ?= $(HOME)/.opam/rocq-mshell/bin
export PATH := $(ROCQBIN):$(PATH)

all: CoqMakefile
	$(MAKE) -f CoqMakefile

CoqMakefile: _CoqProject
	rocq makefile -f _CoqProject -o CoqMakefile

check: all
	@printf 'From MshellCore Require Import Soundness Decide Escape.\nPrint Assumptions soundness.\nPrint Assumptions if_join_alg.\nPrint Assumptions subq_set_sound.\nPrint Assumptions rsubq_set_sound.\nPrint Assumptions kind_list_once.\nPrint Assumptions kind_enum_once.\n' > Assumptions_check.v
	@rocq c -Q . MshellCore Assumptions_check.v; rm -f Assumptions_check.*

clean:
	if [ -f CoqMakefile ]; then $(MAKE) -f CoqMakefile clean; fi
	rm -f CoqMakefile CoqMakefile.conf Assumptions_check.*

.PHONY: all check clean
