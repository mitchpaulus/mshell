package main
import (
	"os/exec"
)

type IPathBinManager interface {
	Lookup(binName string) (string, bool)
	ExecuteArgs(execPath string) ([]string, error)
	DebugList() *MShellList
	IsExecutableFile(path string) bool
	Matches(search string) ([]string)
	// EachName calls fn with every executable's name, sorted.
	EachName(fn func(name string))
	SetupCommand(allArgs []string) *exec.Cmd
	Update()
}
