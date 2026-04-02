package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStartupTestContext() (MShellStack, ExecuteContext, EvalState) {
	stack := MShellStack{}
	context := ExecuteContext{
		Variables: map[string]MShellObject{},
		Pbm:       NewPathBinManager(),
	}
	state := EvalState{}
	return stack, context, state
}

func TestGetStartupPathsUsesVersionDirectories(t *testing.T) {
	dataHome := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	stdlibPath, initPath, err := getStartupPaths("v1.2.3")
	if err != nil {
		t.Fatalf("getStartupPaths() error = %v", err)
	}

	expectedStdlibPath := filepath.Join(dataHome, "msh", "v1.2.3", "std.msh")
	if stdlibPath != expectedStdlibPath {
		t.Fatalf("stdlibPath = %q, want %q", stdlibPath, expectedStdlibPath)
	}

	expectedInitPath := filepath.Join(configHome, "msh", "v1.2.3", "init.msh")
	if initPath != expectedInitPath {
		t.Fatalf("initPath = %q, want %q", initPath, expectedInitPath)
	}
}

func TestGetStartupFileSpecsUsesIndependentEnvironmentOverrides(t *testing.T) {
	t.Setenv("MSHSTDLIB", "/tmp/custom-std.msh")
	t.Setenv("MSHINIT", "")

	dataHome := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	_, defaultInitPath, err := getStartupPaths("v9.9.9")
	if err != nil {
		t.Fatalf("getStartupPaths() error = %v", err)
	}

	stdlibSpec, initSpec, err := getStartupFileSpecs(startupLoadOptions{
		version:           "v9.9.9",
		allowEnvOverrides: true,
	})
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
}

func TestGetStartupFileSpecsIgnoresEnvironmentOverridesWhenDisabled(t *testing.T) {
	t.Setenv("MSHSTDLIB", "/tmp/custom-std.msh")
	t.Setenv("MSHINIT", "/tmp/custom-init.msh")

	dataHome := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	defaultStdlibPath, defaultInitPath, err := getStartupPaths("v1.2.3")
	if err != nil {
		t.Fatalf("getStartupPaths() error = %v", err)
	}

	stdlibSpec, initSpec, err := getStartupFileSpecs(startupLoadOptions{
		version:           "v1.2.3",
		allowEnvOverrides: false,
	})
	if err != nil {
		t.Fatalf("getStartupFileSpecs() error = %v", err)
	}

	if stdlibSpec.path != defaultStdlibPath {
		t.Fatalf("stdlibSpec.path = %q, want %q", stdlibSpec.path, defaultStdlibPath)
	}

	if initSpec.path != defaultInitPath {
		t.Fatalf("initSpec.path = %q, want %q", initSpec.path, defaultInitPath)
	}

	if stdlibSpec.envVar != "" {
		t.Fatalf("stdlibSpec.envVar = %q, want empty string", stdlibSpec.envVar)
	}

	if initSpec.envVar != "" {
		t.Fatalf("initSpec.envVar = %q, want empty string", initSpec.envVar)
	}
}

func TestLoadStartupDefinitionsLoadsVersionedStdlibAndInit(t *testing.T) {
	t.Setenv("MSHSTDLIB", "")
	t.Setenv("MSHINIT", "")

	dataHome := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	version := "v9.9.9"
	stdlibDir := filepath.Join(dataHome, "msh", version)
	if err := os.MkdirAll(stdlibDir, 0755); err != nil {
		t.Fatalf("MkdirAll(stdlibDir) error = %v", err)
	}

	configDir := filepath.Join(configHome, "msh", version)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("MkdirAll(configDir) error = %v", err)
	}

	stdlibPath := filepath.Join(stdlibDir, "std.msh")
	if err := os.WriteFile(stdlibPath, []byte("\"from-stdlib\" stdlibSource!\n"), 0644); err != nil {
		t.Fatalf("WriteFile(stdlibPath) error = %v", err)
	}

	initPath := filepath.Join(configDir, "init.msh")
	if err := os.WriteFile(initPath, []byte("\"from-init\" startup!\n"), 0644); err != nil {
		t.Fatalf("WriteFile(initPath) error = %v", err)
	}

	stack, context, state := newStartupTestContext()

	definitions, err := loadStartupDefinitions(startupLoadOptions{
		version:           version,
		allowEnvOverrides: false,
	}, &stack, context, &state)
	if err != nil {
		t.Fatalf("loadStartupDefinitions() error = %v", err)
	}

	if len(definitions) != 0 {
		t.Fatalf("len(definitions) = %d, want 0", len(definitions))
	}

	startupValue, ok := context.Variables["startup"]
	if !ok {
		t.Fatalf("expected startup variable to be set by init")
	}

	startupStr, ok := startupValue.(MShellString)
	if !ok {
		t.Fatalf("startup variable type = %T, want MShellString", startupValue)
	}

	if startupStr.Content != "from-init" {
		t.Fatalf("startup variable = %q, want %q", startupStr.Content, "from-init")
	}

	stdlibValue, ok := context.Variables["stdlibSource"]
	if !ok {
		t.Fatalf("expected stdlibSource variable to be set by stdlib")
	}

	stdlibStr, ok := stdlibValue.(MShellString)
	if !ok {
		t.Fatalf("stdlibSource variable type = %T, want MShellString", stdlibValue)
	}

	if stdlibStr.Content != "from-stdlib" {
		t.Fatalf("stdlibSource variable = %q, want %q", stdlibStr.Content, "from-stdlib")
	}
}

func TestLoadStartupDefinitionsRequiresVersionedInit(t *testing.T) {
	t.Setenv("MSHSTDLIB", "")
	t.Setenv("MSHINIT", "")

	dataHome := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	version := "v9.9.9"
	stdlibDir := filepath.Join(dataHome, "msh", version)
	if err := os.MkdirAll(stdlibDir, 0755); err != nil {
		t.Fatalf("MkdirAll(stdlibDir) error = %v", err)
	}

	stdlibPath := filepath.Join(stdlibDir, "std.msh")
	if err := os.WriteFile(stdlibPath, []byte("\"from-stdlib\" startup!\n"), 0644); err != nil {
		t.Fatalf("WriteFile(stdlibPath) error = %v", err)
	}

	stack, context, state := newStartupTestContext()

	_, err := loadStartupDefinitions(startupLoadOptions{
		version:           version,
		allowEnvOverrides: false,
	}, &stack, context, &state)
	if err == nil {
		t.Fatalf("loadStartupDefinitions() error = nil, want missing init error")
	}

	if !strings.Contains(err.Error(), filepath.Join(configHome, "msh", version, "init.msh")) {
		t.Fatalf("loadStartupDefinitions() error = %q, want missing init path", err.Error())
	}
}

func TestStdLibDefinitionsUsesCurrentVersionStartupFilesWithEnvOverrides(t *testing.T) {
	dataHome := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	versionDirData := filepath.Join(dataHome, "msh", mshellVersion)
	if err := os.MkdirAll(versionDirData, 0755); err != nil {
		t.Fatalf("MkdirAll(versionDirData) error = %v", err)
	}

	versionDirConfig := filepath.Join(configHome, "msh", mshellVersion)
	if err := os.MkdirAll(versionDirConfig, 0755); err != nil {
		t.Fatalf("MkdirAll(versionDirConfig) error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(versionDirData, "std.msh"), []byte("\"from-versioned-stdlib\" stdlibSource!\n"), 0644); err != nil {
		t.Fatalf("WriteFile(versioned stdlib) error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(versionDirConfig, "init.msh"), []byte("\"from-versioned-init\" startup!\n"), 0644); err != nil {
		t.Fatalf("WriteFile(versioned init) error = %v", err)
	}

	overrideDir := t.TempDir()
	overrideStdlibPath := filepath.Join(overrideDir, "std.msh")
	overrideInitPath := filepath.Join(overrideDir, "init.msh")
	if err := os.WriteFile(overrideStdlibPath, []byte("\"from-env-stdlib\" stdlibSource!\n"), 0644); err != nil {
		t.Fatalf("WriteFile(override stdlib) error = %v", err)
	}

	if err := os.WriteFile(overrideInitPath, []byte("\"from-env-init\" startup!\n"), 0644); err != nil {
		t.Fatalf("WriteFile(override init) error = %v", err)
	}

	t.Setenv("MSHSTDLIB", overrideStdlibPath)
	t.Setenv("MSHINIT", overrideInitPath)

	stack, context, state := newStartupTestContext()

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

	if startupStr.Content != "from-env-init" {
		t.Fatalf("startup variable = %q, want %q", startupStr.Content, "from-env-init")
	}

	stdlibValue, ok := context.Variables["stdlibSource"]
	if !ok {
		t.Fatalf("expected stdlibSource variable to be set")
	}

	stdlibStr, ok := stdlibValue.(MShellString)
	if !ok {
		t.Fatalf("stdlibSource variable type = %T, want MShellString", stdlibValue)
	}

	if stdlibStr.Content != "from-env-stdlib" {
		t.Fatalf("stdlibSource variable = %q, want %q", stdlibStr.Content, "from-env-stdlib")
	}
}

func TestEnvWithoutStartupOverridesRemovesOnlyStartupVars(t *testing.T) {
	t.Setenv("MSHSTDLIB", "/tmp/custom-std.msh")
	t.Setenv("MSHINIT", "/tmp/custom-init.msh")
	t.Setenv("KEEP_ME", "1")

	filteredEnv := envWithoutStartupOverrides()
	filteredJoined := strings.Join(filteredEnv, "\n")

	if strings.Contains(filteredJoined, "MSHSTDLIB=") {
		t.Fatalf("filtered env still contains MSHSTDLIB: %q", filteredJoined)
	}

	if strings.Contains(filteredJoined, "MSHINIT=") {
		t.Fatalf("filtered env still contains MSHINIT: %q", filteredJoined)
	}

	if !strings.Contains(filteredJoined, "KEEP_ME=1") {
		t.Fatalf("filtered env missing KEEP_ME: %q", filteredJoined)
	}
}
