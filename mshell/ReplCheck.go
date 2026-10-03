package main

import (
	"maps"
	"slices"
	"strconv"
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

// newReplChecker starts checking with the startup files' definitions and
// declarations, and the stack and variables they left (their values have
// unknown types: startup code is not checked).
// It returns the errors in the startup files, to show once: a line that
// calls a definition whose signature has one is refused (plan question 21).
func newReplChecker(stdlibDefs []MShellDefinition, decls []MShellParseItem, stackLen int, startupVars map[string]MShellObject) (*replChecker, []string) {
	base := NewCoreBase(stdlibDefs, decls)
	names := slices.Sorted(maps.Keys(startupVars))
	return &replChecker{session: base.NewSession(stackLen, names...)}, base.StartupErrors()
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
// the runtime: anything else is a bug in the checker, and checking stops.
func (r *replChecker) inSync(stack MShellStack) bool {
	return r.session.Len() == len(stack)
}
