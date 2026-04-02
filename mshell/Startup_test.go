package main

import "testing"

func TestGetStartupFileSpecsUsesIndependentEnvironmentOverrides(t *testing.T) {
	t.Setenv("MSHINIT", "")

	defaultStdlibPath, defaultInitPath, err := getStartupPaths("v9.9.9", false)
	if err != nil {
		t.Fatalf("getStartupPaths() error = %v", err)
	}

	t.Setenv("MSHSTDLIB", "/tmp/custom-std.msh")

	stdlibSpec, initSpec, err := getStartupFileSpecs("v9.9.9", false)
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

	defaultStdlibPath, _, err := getStartupPaths("v1.2.3", true)
	if err != nil {
		t.Fatalf("getStartupPaths() error = %v", err)
	}

	t.Setenv("MSHINIT", "/tmp/custom-init.msh")

	stdlibSpec, initSpec, err := getStartupFileSpecs("v1.2.3", true)
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
