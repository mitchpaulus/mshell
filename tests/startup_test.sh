#!/bin/bash
# Startup files and the type checker: each case writes an init.msh and a
# script, runs msh on the script, and checks the exit code, the standard
# output, and a part of the standard error.

cd "$(dirname "$0")" || exit 1

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export MSHSTDLIB="$(realpath ../lib/std.msh)"
export MSHINIT="$TMP/init.msh"
MSH="${MSH:-$(realpath ../mshell/mshell)}"

pass=0
fail=0

# check NAME INIT SCRIPT RC STDOUT STDERR_PART [ARGS...]
# STDERR_PART is a fixed string the standard error must contain, or "" for
# an empty standard error.
check() {
	name=$1 init=$2 script=$3 rc=$4 stdout=$5 stderr_part=$6
	shift 6
	printf '%s' "$init" > "$TMP/init.msh"
	printf '%s' "$script" > "$TMP/script.msh"
	"$MSH" "$@" "$TMP/script.msh" > "$TMP/out" 2> "$TMP/err"
	got_rc=$?
	ok=1
	if [ "$got_rc" -ne "$rc" ]; then
		ok=0
		printf '%s: exit code %s, want %s\n' "$name" "$got_rc" "$rc"
	fi
	if [ "$(cat "$TMP/out")" != "$stdout" ]; then
		ok=0
		printf '%s: stdout %s, want %s\n' "$name" "$(cat "$TMP/out")" "$stdout"
	fi
	if [ -z "$stderr_part" ]; then
		if [ -s "$TMP/err" ]; then
			ok=0
			printf '%s: unexpected stderr\n' "$name"
		fi
	elif ! grep -qF -- "$stderr_part" "$TMP/err"; then
		ok=0
		printf '%s: stderr does not contain %s\n' "$name" "$stderr_part"
	fi
	if [ "$ok" -eq 1 ]; then
		pass=$((pass+1))
	else
		fail=$((fail+1))
		sed 's/^/    /' "$TMP/err" | head -n 8
	fi
}

broken_def='def double (int -- int) 2 * end
def broken (int -- int) "x" end
'

# A broken definition the script does not call: a warning, and the script runs.
check unused-broken-def "$broken_def" '5 double wl' 0 '10' \
	'Warning: type errors in the startup files'

# A call to the broken definition is refused, with the reason.
check called-broken-def "$broken_def" '5 broken wl' 1 '' \
	"'broken', defined at"

# A clean init file: nothing on standard error.
check clean-init 'def triple (int -- int) 3 * end
' '2 triple wl' 0 '6' ''

echo "Passed: $pass"
echo "Failed: $fail"
test "$fail" -eq 0
