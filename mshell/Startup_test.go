package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mitchpaulus/mshell/lib"
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

// stdLibDefCount is how many definitions the built-in standard library has.
func stdLibDefCount(t *testing.T) int {
	t.Helper()
	file, err := parseStdLib()
	if err != nil {
		t.Fatalf("parseStdLib() error = %v", err)
	}
	if len(file.Definitions) == 0 {
		t.Fatalf("the built-in standard library has no definitions")
	}
	return len(file.Definitions)
}

// writeVersionedInit writes source as the init file for version under a new
// XDG_CONFIG_HOME, and returns its path.
func writeVersionedInit(t *testing.T, version string, source string) string {
	t.Helper()
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configDir := filepath.Join(configHome, "msh", version)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("MkdirAll(configDir) error = %v", err)
	}
	initPath := filepath.Join(configDir, "init.msh")
	if err := os.WriteFile(initPath, []byte(source), 0644); err != nil {
		t.Fatalf("WriteFile(initPath) error = %v", err)
	}
	return initPath
}

func startupStringVariable(t *testing.T, context ExecuteContext, name string) string {
	t.Helper()
	value, ok := context.Variables[name]
	if !ok {
		t.Fatalf("expected the %s variable to be set", name)
	}
	str, ok := value.(MShellString)
	if !ok {
		t.Fatalf("%s variable type = %T, want MShellString", name, value)
	}
	return str.Content
}

func TestGetInitPathUsesVersionDirectory(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	initPath, err := getInitPath("v1.2.3")
	if err != nil {
		t.Fatalf("getInitPath() error = %v", err)
	}

	expectedInitPath := filepath.Join(configHome, "msh", "v1.2.3", "init.msh")
	if initPath != expectedInitPath {
		t.Fatalf("initPath = %q, want %q", initPath, expectedInitPath)
	}
}

func TestGetInitFileSpecUsesEnvironmentOverride(t *testing.T) {
	t.Setenv("MSHINIT", "/tmp/custom-init.msh")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	initSpec, err := getInitFileSpec(startupLoadOptions{
		version:           "v9.9.9",
		allowEnvOverrides: true,
		requireInit:       false,
	})
	if err != nil {
		t.Fatalf("getInitFileSpec() error = %v", err)
	}

	if initSpec.path != "/tmp/custom-init.msh" {
		t.Fatalf("initSpec.path = %q, want %q", initSpec.path, "/tmp/custom-init.msh")
	}

	if initSpec.envVar != "MSHINIT" {
		t.Fatalf("initSpec.envVar = %q, want %q", initSpec.envVar, "MSHINIT")
	}

	if !initSpec.required {
		t.Fatalf("an init file named by MSHINIT must be required")
	}
}

func TestGetInitFileSpecIgnoresEnvironmentOverrideWhenDisabled(t *testing.T) {
	t.Setenv("MSHINIT", "/tmp/custom-init.msh")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	defaultInitPath, err := getInitPath("v1.2.3")
	if err != nil {
		t.Fatalf("getInitPath() error = %v", err)
	}

	initSpec, err := getInitFileSpec(startupLoadOptions{
		version:           "v1.2.3",
		allowEnvOverrides: false,
		requireInit:       true,
	})
	if err != nil {
		t.Fatalf("getInitFileSpec() error = %v", err)
	}

	if initSpec.path != defaultInitPath {
		t.Fatalf("initSpec.path = %q, want %q", initSpec.path, defaultInitPath)
	}

	if initSpec.envVar != "" {
		t.Fatalf("initSpec.envVar = %q, want empty string", initSpec.envVar)
	}
}

func TestLoadStartupDefinitionsLoadsBuiltInStdLibAndVersionedInit(t *testing.T) {
	t.Setenv("MSHINIT", "")
	version := "v9.9.9"
	// The init file calls a standard library definition.
	writeVersionedInit(t, version, "def initDef (-- str) \"from-init\" end\n[1 2 3] len str startup!\n")

	stack, context, state := newStartupTestContext()

	definitions, err := loadStartupDefinitions(startupLoadOptions{
		version:           version,
		allowEnvOverrides: false,
		requireInit:       true,
	}, &stack, context, &state)
	if err != nil {
		t.Fatalf("loadStartupDefinitions() error = %v", err)
	}

	if want := stdLibDefCount(t) + 1; len(definitions) != want {
		t.Fatalf("len(definitions) = %d, want %d", len(definitions), want)
	}

	if got := startupStringVariable(t, context, "startup"); got != "3" {
		t.Fatalf("startup variable = %q, want %q", got, "3")
	}

	if len(state.StartupTypeErrors) != 0 {
		t.Fatalf("startup type errors: %v", state.StartupTypeErrors)
	}
	if errs := state.StartupBase.StartupErrors(); len(errs) != 0 {
		t.Fatalf("startup errors: %v", errs)
	}
}

// A file at the place the standard library used to be read from, and
// MSHSTDLIB, are not read: the standard library is the one in the binary.
func TestLoadStartupDefinitionsIgnoresStdLibFiles(t *testing.T) {
	t.Setenv("MSHINIT", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	version := "v9.9.9"
	stdlibDir := filepath.Join(dataHome, "msh", version)
	if err := os.MkdirAll(stdlibDir, 0755); err != nil {
		t.Fatalf("MkdirAll(stdlibDir) error = %v", err)
	}
	source := []byte("\"from-file\" stdlibSource!\n")
	if err := os.WriteFile(filepath.Join(stdlibDir, "std.msh"), source, 0644); err != nil {
		t.Fatalf("WriteFile(std.msh) error = %v", err)
	}
	envPath := filepath.Join(t.TempDir(), "std.msh")
	if err := os.WriteFile(envPath, source, 0644); err != nil {
		t.Fatalf("WriteFile(envPath) error = %v", err)
	}
	t.Setenv("MSHSTDLIB", envPath)

	stack, context, state := newStartupTestContext()

	definitions, err := loadStartupDefinitions(startupLoadOptions{
		version:           version,
		allowEnvOverrides: true,
	}, &stack, context, &state)
	if err != nil {
		t.Fatalf("loadStartupDefinitions() error = %v", err)
	}

	if want := stdLibDefCount(t); len(definitions) != want {
		t.Fatalf("len(definitions) = %d, want %d", len(definitions), want)
	}
	if _, ok := context.Variables["stdlibSource"]; ok {
		t.Fatalf("a std.msh file was run; only the built-in standard library should load")
	}
}

func TestLoadStartupDefinitionsRequiresInitForExplicitVersion(t *testing.T) {
	t.Setenv("MSHINIT", "")
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	version := "v9.9.9"
	stack, context, state := newStartupTestContext()

	_, err := loadStartupDefinitions(startupLoadOptions{
		version:           version,
		allowEnvOverrides: false,
		requireInit:       true,
	}, &stack, context, &state)
	if err == nil {
		t.Fatalf("loadStartupDefinitions() error = nil, want missing init error")
	}

	initPath := filepath.Join(configHome, "msh", version, "init.msh")
	if !strings.Contains(err.Error(), initPath) {
		t.Fatalf("loadStartupDefinitions() error = %q, want missing init path", err.Error())
	}

	message := formatStartupErrorMessage(err, "script.msh", version, 1, 1)
	for _, want := range []string{"Create an init.msh at " + initPath, "remove the VER directive"} {
		if !strings.Contains(message, want) {
			t.Errorf("the startup error message does not contain %q:\n%s", want, message)
		}
	}
	if strings.Contains(message, "MSHSTDLIB") || strings.Contains(message, "std.msh") {
		t.Errorf("the startup error message tells of a standard library file:\n%s", message)
	}
}

func TestLoadStartupDefinitionsAllowsMissingInitForImplicitVersion(t *testing.T) {
	t.Setenv("MSHINIT", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	stack, context, state := newStartupTestContext()

	definitions, err := loadStartupDefinitions(startupLoadOptions{
		version:           "v9.9.9",
		allowEnvOverrides: true,
		requireInit:       false,
	}, &stack, context, &state)
	if err != nil {
		t.Fatalf("loadStartupDefinitions() error = %v", err)
	}

	if want := stdLibDefCount(t); len(definitions) != want {
		t.Fatalf("len(definitions) = %d, want %d", len(definitions), want)
	}

	if _, ok := context.Variables["startup"]; ok {
		t.Fatalf("expected startup variable to remain unset without init")
	}
}

// The init file is the program when it is the script being run: it is not
// also loaded as a startup file, which would define its names twice.
func TestLoadStartupDefinitionsLeavesOutInitWhenItIsTheScript(t *testing.T) {
	t.Setenv("MSHINIT", "")
	version := "v9.9.9"
	initPath := writeVersionedInit(t, version, "def initDef (-- str) \"from-init\" end\n")

	stack, context, state := newStartupTestContext()

	definitions, err := loadStartupDefinitions(startupLoadOptions{
		version:           version,
		allowEnvOverrides: true,
		script:            initPath,
	}, &stack, context, &state)
	if err != nil {
		t.Fatalf("loadStartupDefinitions() error = %v", err)
	}

	if want := stdLibDefCount(t); len(definitions) != want {
		t.Fatalf("len(definitions) = %d, want %d", len(definitions), want)
	}
}

// --is-std-lib names the file the built-in standard library is built from.
// When that file is the script, it is the program: neither the built-in
// standard library, which has the same definitions, nor the init file is
// loaded. Any other script gets both.
func TestLoadStartupDefinitionsLeavesOutStartupFilesForStdLibSource(t *testing.T) {
	t.Setenv("MSHINIT", "")
	version := "v9.9.9"
	writeVersionedInit(t, version, "def initDef (-- str) \"from-init\" end\n")
	stdLibSource := filepath.Join(t.TempDir(), "std.msh")
	if err := os.WriteFile(stdLibSource, []byte(lib.Std), 0644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other.msh")
	if err := os.WriteFile(other, []byte("1 wl\n"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		script string
		want   int
	}{
		{stdLibSource, 0},
		{other, stdLibDefCount(t) + 1},
	} {
		stack, context, state := newStartupTestContext()
		definitions, err := loadStartupDefinitions(startupLoadOptions{
			version:           version,
			allowEnvOverrides: true,
			script:            tc.script,
			stdLibSource:      stdLibSource,
		}, &stack, context, &state)
		if err != nil {
			t.Fatalf("loadStartupDefinitions() error = %v", err)
		}
		if len(definitions) != tc.want {
			t.Errorf("script %s: len(definitions) = %d, want %d", filepath.Base(tc.script), len(definitions), tc.want)
		}
	}
}

func TestStdLibDefinitionsUsesCurrentVersionInitWithEnvOverride(t *testing.T) {
	t.Setenv("MSHINIT", "")
	writeVersionedInit(t, mshellVersion, "\"from-versioned-init\" startup!\n")

	overrideInitPath := filepath.Join(t.TempDir(), "init.msh")
	if err := os.WriteFile(overrideInitPath, []byte("\"from-env-init\" startup!\n"), 0644); err != nil {
		t.Fatalf("WriteFile(override init) error = %v", err)
	}

	stack, context, state := newStartupTestContext()
	if _, err := stdLibDefinitions(&stack, context, &state); err != nil {
		t.Fatalf("stdLibDefinitions() error = %v", err)
	}
	if got := startupStringVariable(t, context, "startup"); got != "from-versioned-init" {
		t.Fatalf("startup variable = %q, want %q", got, "from-versioned-init")
	}

	t.Setenv("MSHINIT", overrideInitPath)
	stack, context, state = newStartupTestContext()
	if _, err := stdLibDefinitions(&stack, context, &state); err != nil {
		t.Fatalf("stdLibDefinitions() error = %v", err)
	}
	if got := startupStringVariable(t, context, "startup"); got != "from-env-init" {
		t.Fatalf("startup variable = %q, want %q", got, "from-env-init")
	}
}

func TestStdLibDefinitionsAddsCompletionDefinitionsToState(t *testing.T) {
	t.Setenv("MSHINIT", "")
	writeVersionedInit(t, mshellVersion, "def __demoCompletion { 'complete': ['demo'] } ([str] -- [str])\n    ['--flag']\nend\n")

	stack, context, state := newStartupTestContext()

	if _, err := stdLibDefinitions(&stack, context, &state); err != nil {
		t.Fatalf("stdLibDefinitions() error = %v", err)
	}

	completionDefs := state.CompletionDefinitions["demo"]
	if len(completionDefs) != 1 {
		t.Fatalf("len(state.CompletionDefinitions[\"demo\"]) = %d, want 1", len(completionDefs))
	}

	if completionDefs[0].Name != "__demoCompletion" {
		t.Fatalf("completionDefs[0].Name = %q, want %q", completionDefs[0].Name, "__demoCompletion")
	}

	// The built-in standard library's completions are there too.
	if len(state.CompletionDefinitions["git"]) == 0 {
		t.Fatalf("the standard library's completion for git is not registered")
	}
}

func TestEnvWithoutStartupOverridesRemovesOnlyStartupVars(t *testing.T) {
	t.Setenv("MSHINIT", "/tmp/custom-init.msh")
	t.Setenv("KEEP_ME", "1")

	filteredEnv := envWithoutStartupOverrides()
	filteredJoined := strings.Join(filteredEnv, "\n")

	if strings.Contains(filteredJoined, "MSHINIT=") {
		t.Fatalf("filtered env still contains MSHINIT: %q", filteredJoined)
	}

	if !strings.Contains(filteredJoined, "KEEP_ME=1") {
		t.Fatalf("filtered env missing KEEP_ME: %q", filteredJoined)
	}
}

// TestBuiltInStdLibChecks checks the bodies of the built-in standard
// library's definitions. A run does not (NewCoreBase): they are the same
// in every run of a binary, so they are checked here, once for each build.
func TestBuiltInStdLibChecks(t *testing.T) {
	// Parsed as a file of its own, the definitions are not stdLibTokenFile's,
	// so NewCoreBase checks their bodies.
	file, err := parseMShellInput(lib.Std, &TokenFile{"lib/std.msh"})
	if err != nil {
		t.Fatalf("lib/std.msh does not parse: %v", err)
	}
	base := NewCoreBase(file.Definitions, declarationItems(file.Items))
	for _, e := range base.StartupErrors() {
		t.Error(e)
	}

	// The standard library a run loads has the same definitions, and
	// NewCoreBase reports no errors for them.
	builtIn, err := parseStdLib()
	if err != nil {
		t.Fatal(err)
	}
	if len(builtIn.Definitions) != len(file.Definitions) {
		t.Fatalf("parseStdLib() has %d definitions, want %d", len(builtIn.Definitions), len(file.Definitions))
	}
	for i := range builtIn.Definitions {
		if builtIn.Definitions[i].File != stdLibTokenFile {
			t.Fatalf("definition %s is not in stdLibTokenFile", builtIn.Definitions[i].Name)
		}
	}
	if errs := NewCoreBase(builtIn.Definitions, declarationItems(builtIn.Items)).StartupErrors(); len(errs) != 0 {
		t.Fatalf("the built-in standard library has errors: %v", errs)
	}
}

// TestLoadStartupDefinitionsChecksTopLevel: the startup files' top-level
// code is checked as the first lines of the session the interactive shell
// and the script continue. init.msh's top level runs only if it checks;
// then the session has the stack and variables it left, with their types.
func TestLoadStartupDefinitionsChecksTopLevel(t *testing.T) {
	cases := []struct {
		name     string
		init     string
		ran      bool
		stackLen int
		errPart  string
		next     string // a line after the startup code
		nextOk   bool
	}{
		{"checks", "5 n!\n\"x\"\n", true, 1, "", "@n 1 + wl wl", true},
		{"stack type", "5 n!\n\"x\"\n", true, 1, "", "@n + wl", false},
		{"error", "5 n!\n1 \"a\" +\n", false, 0, "init.msh: type error at line 2", "@n 1 + wl", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", dir)
			initPath := filepath.Join(dir, "init.msh")
			if err := os.WriteFile(initPath, []byte(tc.init), 0644); err != nil {
				t.Fatal(err)
			}
			t.Setenv("MSHINIT", initPath)

			stack, context, state := newStartupTestContext()
			if _, err := loadStartupDefinitions(startupLoadOptions{version: "v9.9.9", allowEnvOverrides: true}, &stack, context, &state); err != nil {
				t.Fatalf("loadStartupDefinitions() error = %v", err)
			}
			if _, ran := context.Variables["n"]; ran != tc.ran {
				t.Errorf("init.msh ran = %v, want %v", ran, tc.ran)
			}
			if len(stack) != tc.stackLen || state.StartupSession.Len() != len(stack) {
				t.Errorf("stack has %d values and the session %d, want %d", len(stack), state.StartupSession.Len(), tc.stackLen)
			}
			errs := strings.Join(state.StartupTypeErrors, "\n")
			if (tc.errPart == "") != (errs == "") || !strings.Contains(errs, tc.errPart) {
				t.Errorf("startup type errors %q, want %q", errs, tc.errPart)
			}
			next, err := parseMShellInput(tc.next, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := state.StartupSession.Check(next); ok != tc.nextOk {
				t.Errorf("%q checks = %v, want %v", tc.next, ok, tc.nextOk)
			}
		})
	}
}

// TestNewCoreBaseChecksOnlyOtherFilesBodies: NewCoreBase does not check
// the body of a definition in the built-in standard library's file, and
// checks the same definition in any other file.
func TestNewCoreBaseChecksOnlyOtherFilesBodies(t *testing.T) {
	source := "def wrongBody (int -- int) \"x\" end\n"
	builtIn, err := parseMShellInput(source, stdLibTokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if errs := NewCoreBase(builtIn.Definitions, nil).StartupErrors(); len(errs) != 0 {
		t.Errorf("a body in the built-in standard library was checked: %v", errs)
	}
	other, err := parseMShellInput(source, &TokenFile{"init.msh"})
	if err != nil {
		t.Fatal(err)
	}
	if errs := NewCoreBase(other.Definitions, nil).StartupErrors(); len(errs) != 1 {
		t.Errorf("a body in init.msh: %d errors, want 1: %v", len(errs), errs)
	}
}
