# Build and check the mechanized core.  Needs the Rocq prover (tested with 9.1.1).
#   make          build everything
#   make check    build, then print the axioms the main theorem depends on
#   make clean
ROCQBIN ?= $(HOME)/.opam/rocq-mshell/bin
export PATH := $(ROCQBIN):$(PATH)

all: CoqMakefile
	$(MAKE) -f CoqMakefile

CoqMakefile: _CoqProject
	rocq makefile -f _CoqProject -o CoqMakefile

check: all
	@printf 'From MshellCore Require Import Soundness.\nPrint Assumptions soundness.\n' > Assumptions_check.v
	@rocq c -Q . MshellCore Assumptions_check.v; rm -f Assumptions_check.*

clean:
	if [ -f CoqMakefile ]; then $(MAKE) -f CoqMakefile clean; fi
	rm -f CoqMakefile CoqMakefile.conf Assumptions_check.*

.PHONY: all check clean
