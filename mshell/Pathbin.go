package main
import (
)

type IPathBinManager interface {
	Lookup(binName string) (string, bool)
	ExecuteArgs(execPath string) ([]string, error)
	DebugList() string
}
