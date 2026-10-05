package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// replChecker checks REPL lines before they run (ai/type-system-plan.md,
// stage 9), and puts the stack back after a line that stops with a runtime
// error. The checking itself is CoreSession's.
//
// The REPL calls, for each parsed line: check; if it checks, abort (the
// line is not run after all) or commit; then, after running a committed
// line, failed when it stopped with a runtime error.
type replChecker struct {
	session *CoreSession
	// pre is the stack before the committed line: its references, not
	// copies of the values (design doc, "Checking by default").
	pre MShellStack
}

// check checks a line. It returns the errors and the `dbg` snapshots,
// formatted, and whether the line may run.
func (r *replChecker) check(file *MShellFile) ([]string, bool) {
	diags, ok := r.session.Check(file)
	var out []string
	for _, e := range diags {
		if e.Severity == SeverityError || e.Kind == TErrDebugDump {
			out = append(out, e.Format(r.session.Arena(), r.session.Names()))
		}
	}
	return out, ok
}

// abort takes back a line that checked but will not run.
func (r *replChecker) abort() { r.session.Abort() }

// commit keeps a line that is about to run on stack.
func (r *replChecker) commit(stack MShellStack) {
	r.session.Commit()
	r.pre = append(r.pre[:0], stack...)
}

// failed restores *stack after the committed line stopped with a runtime
// error: the stack before the line, less the new values the line took,
// which it may have changed or stored (plan question 20). It returns how
// many were dropped.
func (r *replChecker) failed(stack *MShellStack) int {
	d, keep := r.session.RuntimeError()
	n := len(r.pre)
	restored := slices.Clone(r.pre[:n-d])
	dropped := 0
	for i, k := range keep {
		if k {
			restored = append(restored, r.pre[n-d+i])
		} else {
			dropped++
		}
	}
	*stack = restored
	r.pre = r.pre[:0]
	return dropped
}

// droppedNote is what the REPL says about values failed dropped.
func droppedNote(n int) string {
	if n == 1 {
		return "The stack is as it was before the line, except a new value the line took: it was dropped, since the line may have changed it."
	}
	return "The stack is as it was before the line, except " + strconv.Itoa(n) +
		" new values the line took: they were dropped, since the line may have changed them."
}

// inSync reports whether the checker has as many values on the stack as
// the runtime: anything else is a bug in the checker or the runtime, and
// the session does not go on as it is (lostTrack).
func (r *replChecker) inSync(stack MShellStack) bool {
	return r.session.Len() == len(stack)
}

// clearStack empties the checker's stack, for a runtime stack the REPL has
// just emptied.
func (r *replChecker) clearStack() {
	r.session.ClearStack()
	r.pre = r.pre[:0]
}

// lostTrackMessage tells the user the checker and the runtime disagree on
// how many values the stack holds.
func lostTrackMessage(checker, runtime int) string {
	return fmt.Sprintf("The type checker lost track of the stack: it has %d values, the stack %d.\n"+
		"This is a bug in mshell; please report it.\n"+
		"The values on the stack can no longer be checked, so the stack must be cleared, or the shell exits.\n",
		checker, runtime)
}

// lostTrackClears asks, with read, whether to clear the stack and go on
// (true) or exit (false), until the answer is one of them. Anything that
// stops the asking, such as the end of input, exits: the session never goes
// on with a stack the checker cannot follow.
func lostTrackClears(read func(prompt string) (string, error)) bool {
	for {
		answer, err := read("Clear the stack and continue [c], or exit [e]? ")
		if err != nil {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "c", "clear":
			return true
		case "e", "exit":
			return false
		}
	}
}
