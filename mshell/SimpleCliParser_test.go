package main

import (
	"testing"
)

func TestSimpleCliParser_SingleCommand(t *testing.T) {
	input := "ls -la"
	l := NewLexer(input, nil)
	p := NewMShellSimpleCliParser(l)

	pipeline, err := p.Parse()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if pipeline == nil {
		t.Fatal("Expected non-nil pipeline")
	}

	if len(pipeline.Commands) != 1 {
		t.Errorf("Expected 1 command, got %d", len(pipeline.Commands))
	}

	if len(pipeline.Commands[0].Tokens) != 2 {
		t.Errorf("Expected 2 tokens, got %d", len(pipeline.Commands[0].Tokens))
	}

	expected := "['ls' '-la'];"
	result := pipeline.ToMShellString()
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestSimpleCliParser_Pipeline(t *testing.T) {
	input := "cat file.txt | grep foo"
	l := NewLexer(input, nil)
	p := NewMShellSimpleCliParser(l)

	pipeline, err := p.Parse()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if pipeline == nil {
		t.Fatal("Expected non-nil pipeline")
	}

	if len(pipeline.Commands) != 2 {
		t.Errorf("Expected 2 commands, got %d", len(pipeline.Commands))
	}

	expected := "[['cat' 'file.txt'] ['grep' 'foo']]|;"
	result := pipeline.ToMShellString()
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestSimpleCliParser_StdinRedirect(t *testing.T) {
	input := "grep foo < input.txt"
	l := NewLexer(input, nil)
	p := NewMShellSimpleCliParser(l)

	pipeline, err := p.Parse()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if pipeline == nil {
		t.Fatal("Expected non-nil pipeline")
	}

	if pipeline.StdinRedirect == nil {
		t.Error("Expected stdin redirect to be set")
	}

	expected := "['grep' 'foo'] `input.txt` < ;"
	result := pipeline.ToMShellString()
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestSimpleCliParser_StdoutRedirect(t *testing.T) {
	input := "cat file.txt > output.txt"
	l := NewLexer(input, nil)
	p := NewMShellSimpleCliParser(l)

	pipeline, err := p.Parse()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if pipeline == nil {
		t.Fatal("Expected non-nil pipeline")
	}

	if pipeline.StdoutRedirect == nil {
		t.Error("Expected stdout redirect to be set")
	}

	expected := "['cat' 'file.txt'] `output.txt` > ;"
	result := pipeline.ToMShellString()
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestSimpleCliParser_PipelineWithStdoutRedirect(t *testing.T) {
	input := "cat file.txt | grep foo > output.txt"
	l := NewLexer(input, nil)
	p := NewMShellSimpleCliParser(l)

	pipeline, err := p.Parse()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if pipeline == nil {
		t.Fatal("Expected non-nil pipeline")
	}

	if len(pipeline.Commands) != 2 {
		t.Errorf("Expected 2 commands, got %d", len(pipeline.Commands))
	}

	if pipeline.StdoutRedirect == nil {
		t.Error("Expected stdout redirect to be set")
	}

	expected := "[['cat' 'file.txt'] ['grep' 'foo'] `output.txt` >]|;"
	result := pipeline.ToMShellString()
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestSimpleCliParser_StdinAndStdoutRedirect(t *testing.T) {
	input := "sort < input.txt > output.txt"
	l := NewLexer(input, nil)
	p := NewMShellSimpleCliParser(l)

	pipeline, err := p.Parse()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if pipeline == nil {
		t.Fatal("Expected non-nil pipeline")
	}

	if pipeline.StdinRedirect == nil {
		t.Error("Expected stdin redirect to be set")
	}

	if pipeline.StdoutRedirect == nil {
		t.Error("Expected stdout redirect to be set")
	}

	expected := "['sort'] `input.txt` < `output.txt` > ;"
	result := pipeline.ToMShellString()
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestSimpleCliParser_MultiplePipes(t *testing.T) {
	input := "cat file | grep foo | wc -l"
	l := NewLexer(input, nil)
	p := NewMShellSimpleCliParser(l)

	pipeline, err := p.Parse()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if pipeline == nil {
		t.Fatal("Expected non-nil pipeline")
	}

	if len(pipeline.Commands) != 3 {
		t.Errorf("Expected 3 commands, got %d", len(pipeline.Commands))
	}

	expected := "[['cat' 'file'] ['grep' 'foo'] ['wc' '-l']]|;"
	result := pipeline.ToMShellString()
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestSimpleCliParser_PathToken(t *testing.T) {
	input := "cat `myfile.txt`"
	l := NewLexer(input, nil)
	p := NewMShellSimpleCliParser(l)

	pipeline, err := p.Parse()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if pipeline == nil {
		t.Fatal("Expected non-nil pipeline")
	}

	// Path token should be preserved as-is
	expected := "['cat' `myfile.txt`];"
	result := pipeline.ToMShellString()
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestSimpleCliParser_StringToken(t *testing.T) {
	input := `echo "hello world"`
	l := NewLexer(input, nil)
	p := NewMShellSimpleCliParser(l)

	pipeline, err := p.Parse()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if pipeline == nil {
		t.Fatal("Expected non-nil pipeline")
	}

	// Double-quoted string should be preserved
	expected := `['echo' "hello world"];`
	result := pipeline.ToMShellString()
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}

func TestSimpleCliParser_ErrorEmptyPipe(t *testing.T) {
	input := "cat file |"
	l := NewLexer(input, nil)
	p := NewMShellSimpleCliParser(l)

	_, err := p.Parse()
	if err == nil {
		t.Error("Expected error for empty pipe, got nil")
	}
}

func TestSimpleCliParser_ErrorMissingRedirectFile(t *testing.T) {
	input := "cat file >"
	l := NewLexer(input, nil)
	p := NewMShellSimpleCliParser(l)

	_, err := p.Parse()
	if err == nil {
		t.Error("Expected error for missing redirect file, got nil")
	}
}

func TestSimpleCliParser_EmptyInput(t *testing.T) {
	input := ""
	l := NewLexer(input, nil)
	p := NewMShellSimpleCliParser(l)

	pipeline, err := p.Parse()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if pipeline != nil {
		t.Error("Expected nil pipeline for empty input")
	}
}

func TestSimpleCliParser_StdinRedirectWithPath(t *testing.T) {
	input := "grep foo < `input.txt`"
	l := NewLexer(input, nil)
	p := NewMShellSimpleCliParser(l)

	pipeline, err := p.Parse()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if pipeline == nil {
		t.Fatal("Expected non-nil pipeline")
	}

	// Path token should be preserved in redirect
	expected := "['grep' 'foo'] `input.txt` < ;"
	result := pipeline.ToMShellString()
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}
}
