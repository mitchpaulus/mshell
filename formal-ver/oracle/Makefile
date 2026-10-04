# The extracted oracle for the checker's <= and fresh-retype procedures.
#   make         extract from the proof and build ./oracle
#   make test    check the oracle against examples.txt
#   make clean
ROCQBIN ?= $(HOME)/.opam/rocq-mshell/bin
export PATH := $(ROCQBIN):$(PATH)

oracle: decide.ml oracle.ml
	ocamlfind ocamlopt -package str -w -a decide.mli decide.ml oracle.ml -o oracle

decide.ml: Extract.v ../Decide.vo ../Join.vo ../Recursive.vo
	rocq c -Q .. MshellCore Extract.v

../%.vo:
	$(MAKE) -C ..

examples.txt: oracle
	./oracle --examples > examples.txt

test: oracle
	./test.sh

clean:
	rm -f oracle decide.ml decide.mli *.cm* *.o Extract.vo Extract.vok Extract.vos Extract.glob .Extract.aux

.PHONY: test clean
