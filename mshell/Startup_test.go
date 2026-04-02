package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetStartupFileSpecsUsesIndependentEnvironmentOverrides(t *testing.T) {
	t.Setenv("MSHINIT", "")

	defaultStdlibPath, defaultInitPath, err := getStartupPaths("v9.9.9", true, false)
	if err != nil {
		t.Fatalf("getStartupPaths() error = %v", err)
	}

	t.Setenv("MSHSTDLIB", "/tmp/custom-std.msh")

	stdlibSpec, initSpec, err := getStartupFileSpecs("v9.9.9", true, false)
	if err != nil {
		t.Fatalf("getStartupFileSpecs() error = %v", err)
	}

	if stdlibSpec.path != "/tmp/custom-std.msh" {
		t.Fatalf("stdlibSpec.path = %q, want %q", stdlibSpec.path, "/tmp/custom-std.msh")
	}

	if stdlibSpec.envVar != "MSHSTDLIB" {
		t.Fatalf("stdlibSpec.envVar = %q, want %q", stdlibSpec.envVar, "MSHSTDLIB")
	}

	if initSpec.path != defaultInitPath {
		t.Fatalf("initSpec.path = %q, want %q", initSpec.path, defaultInitPath)
	}

	if initSpec.envVar != "" {
		t.Fatalf("initSpec.envVar = %q, want empty string", initSpec.envVar)
	}

	if defaultStdlibPath == stdlibSpec.path {
		t.Fatalf("expected stdlib override to differ from default path %q", defaultStdlibPath)
	}
}

func TestGetStartupFileSpecsUsesMSHINITForVersionedScripts(t *testing.T) {
	t.Setenv("MSHSTDLIB", "")

	defaultStdlibPath, _, err := getStartupPaths("v1.2.3", true, true)
	if err != nil {
		t.Fatalf("getStartupPaths() error = %v", err)
	}

	t.Setenv("MSHINIT", "/tmp/custom-init.msh")

	stdlibSpec, initSpec, err := getStartupFileSpecs("v1.2.3", true, true)
	if err != nil {
		t.Fatalf("getStartupFileSpecs() error = %v", err)
	}

	if stdlibSpec.path != defaultStdlibPath {
		t.Fatalf("stdlibSpec.path = %q, want %q", stdlibSpec.path, defaultStdlibPath)
	}

	if stdlibSpec.envVar != "" {
		t.Fatalf("stdlibSpec.envVar = %q, want empty string", stdlibSpec.envVar)
	}

	if initSpec.path != "/tmp/custom-init.msh" {
		t.Fatalf("initSpec.path = %q, want %q", initSpec.path, "/tmp/custom-init.msh")
	}

	if initSpec.envVar != "MSHINIT" {
		t.Fatalf("initSpec.envVar = %q, want %q", initSpec.envVar, "MSHINIT")
	}
}

func TestLoadStartupDefinitionsAllowsMissingDefaultInit(t *testing.T) {
	t.Setenv("MSHSTDLIB", "")
	t.Setenv("MSHINIT", "")

	dataHome := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	version := "v9.9.9"
	stdlibDir := filepath.Join(dataHome, "msh", "lib", version)
	if err := os.MkdirAll(stdlibDir, 0755); err != nil {
		t.Fatalf("MkdirAll(stdlibDir) error = %v", err)
	}

	stdlibPath := filepath.Join(stdlibDir, "std.msh")
	if err := os.WriteFile(stdlibPath, []byte("\"from-stdlib\" startup!\n"), 0644); err != nil {
		t.Fatalf("WriteFile(stdlibPath) error = %v", err)
	}

	stack := MShellStack{}
	context := ExecuteContext{
		Variables: map[string]MShellObject{},
		Pbm:       NewPathBinManager(),
	}
	state := EvalState{}

	definitions, err := loadStartupDefinitions(version, true, false, &stack, context, &state)
	if err != nil {
		t.Fatalf("loadStartupDefinitions() error = %v", err)
	}

	if len(definitions) != 0 {
		t.Fatalf("len(definitions) = %d, want 0", len(definitions))
	}

	startupValue, ok := context.Variables["startup"]
	if !ok {
		t.Fatalf("expected startup variable to be set by stdlib")
	}

	startupStr, ok := startupValue.(MShellString)
	if !ok {
		t.Fatalf("startup variable type = %T, want MShellString", startupValue)
	}

	if startupStr.Content != "from-stdlib" {
		t.Fatalf("startup variable = %q, want %q", startupStr.Content, "from-stdlib")
	}
}

func TestStdLibDefinitionsUsesUnversionedStartupFilesForInteractiveMode(t *testing.T) {
	t.Setenv("MSHSTDLIB", "")
	t.Setenv("MSHINIT", "")

	dataHome := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	stdlibDir := filepath.Join(dataHome, "msh", "lib")
	if err := os.MkdirAll(filepath.Join(stdlibDir, mshellVersion), 0755); err != nil {
		t.Fatalf("MkdirAll(versioned stdlib dir) error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(stdlibDir, "std.msh"), []byte("\"from-unversioned-stdlib\" stdlibSource!\n"), 0644); err != nil {
		t.Fatalf("WriteFile(unversioned stdlib) error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(stdlibDir, mshellVersion, "std.msh"), []byte("\"from-versioned-stdlib\" stdlibSource!\n"), 0644); err != nil {
		t.Fatalf("WriteFile(versioned stdlib) error = %v", err)
	}

	initDir := filepath.Join(configHome, "msh", "init")
	if err := os.MkdirAll(filepath.Join(initDir, mshellVersion), 0755); err != nil {
		t.Fatalf("MkdirAll(initDir) error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(initDir, "init.msh"), []byte("\"from-unversioned-init\" startup!\n"), 0644); err != nil {
		t.Fatalf("WriteFile(unversioned init) error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(initDir, mshellVersion, "init.msh"), []byte("\"from-versioned-init\" startup!\n"), 0644); err != nil {
		t.Fatalf("WriteFile(versioned init) error = %v", err)
	}

	stack := MShellStack{}
	context := ExecuteContext{
		Variables: map[string]MShellObject{},
		Pbm:       NewPathBinManager(),
	}
	state := EvalState{}

	if _, err := stdLibDefinitions(stack, context, state); err != nil {
		t.Fatalf("stdLibDefinitions() error = %v", err)
	}

	startupValue, ok := context.Variables["startup"]
	if !ok {
		t.Fatalf("expected startup variable to be set")
	}

	startupStr, ok := startupValue.(MShellString)
	if !ok {
		t.Fatalf("startup variable type = %T, want MShellString", startupValue)
	}

	if startupStr.Content != "from-unversioned-init" {
		t.Fatalf("startup variable = %q, want %q", startupStr.Content, "from-unversioned-init")
	}

	stdlibValue, ok := context.Variables["stdlibSource"]
	if !ok {
		t.Fatalf("expected stdlibSource variable to be set")
	}

	stdlibStr, ok := stdlibValue.(MShellString)
	if !ok {
		t.Fatalf("stdlibSource variable type = %T, want MShellString", stdlibValue)
	}

	if stdlibStr.Content != "from-unversioned-stdlib" {
		t.Fatalf("stdlibSource variable = %q, want %q", stdlibStr.Content, "from-unversioned-stdlib")
	}
}
