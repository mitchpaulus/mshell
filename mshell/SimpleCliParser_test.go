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

	if len(pipeline.Commands[0].Items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(pipeline.Commands[0].Items))
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
}

func TestSimpleCliParser_ListArgument(t *testing.T) {
	input := "numargs 1 2 [4 5 6]"
	l := NewLexer(input, nil)
	p := NewMShellSimpleCliParser(l)

	pipeline, err := p.Parse()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if pipeline == nil {
		t.Fatal("Expected non-nil pipeline")
	}

	// Should have 4 items: numargs, 1, 2, [4 5 6]
	if len(pipeline.Commands[0].Items) != 4 {
		t.Errorf("Expected 4 items, got %d", len(pipeline.Commands[0].Items))
	}

	// Fourth item should be a list
	_, ok := pipeline.Commands[0].Items[3].(*MShellParseList)
	if !ok {
		t.Errorf("Expected fourth item to be MShellParseList, got %T", pipeline.Commands[0].Items[3])
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

func TestSimpleCliParser_ToMShellFile_SingleCommand(t *testing.T) {
	input := "ls -la"
	l := NewLexer(input, nil)
	p := NewMShellSimpleCliParser(l)

	pipeline, err := p.Parse()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	file := pipeline.ToMShellFile()
	if file == nil {
		t.Fatal("Expected non-nil MShellFile")
	}

	// Should have: [list] ;
	if len(file.Items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(file.Items))
	}

	// First item should be a list
	list, ok := file.Items[0].(*MShellParseList)
	if !ok {
		t.Errorf("Expected first item to be MShellParseList, got %T", file.Items[0])
	}

	// List should have 2 items: 'ls' and '-la'
	if len(list.Items) != 2 {
		t.Errorf("Expected 2 items in command list, got %d", len(list.Items))
	}

	// Second item should be EXECUTE
	execToken, ok := file.Items[1].(Token)
	if !ok || execToken.Type != EXECUTE {
		t.Errorf("Expected EXECUTE token, got %T %v", file.Items[1], file.Items[1])
	}
}

func TestSimpleCliParser_ToMShellFile_PipelineWithRedirect(t *testing.T) {
	input := "cat file.txt | wc -l > count.txt"
	l := NewLexer(input, nil)
	p := NewMShellSimpleCliParser(l)

	pipeline, err := p.Parse()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	file := pipeline.ToMShellFile()
	if file == nil {
		t.Fatal("Expected non-nil MShellFile")
	}

	// Should have: [outer list] | ;
	if len(file.Items) != 3 {
		t.Errorf("Expected 3 items, got %d", len(file.Items))
	}

	// First item should be outer list
	outerList, ok := file.Items[0].(*MShellParseList)
	if !ok {
		t.Errorf("Expected first item to be MShellParseList, got %T", file.Items[0])
	}

	// Outer list should have: [cmd1] [cmd2] path >
	// That's 4 items
	if len(outerList.Items) != 4 {
		t.Errorf("Expected 4 items in outer list, got %d", len(outerList.Items))
	}

	// Second item should be PIPE
	pipeToken, ok := file.Items[1].(Token)
	if !ok || pipeToken.Type != PIPE {
		t.Errorf("Expected PIPE token, got %T %v", file.Items[1], file.Items[1])
	}

	// Third item should be EXECUTE
	execToken, ok := file.Items[2].(Token)
	if !ok || execToken.Type != EXECUTE {
		t.Errorf("Expected EXECUTE token, got %T %v", file.Items[2], file.Items[2])
	}
}

func TestSimpleCliParser_ToMShellFile_ListArgument(t *testing.T) {
	input := "numargs 1 2 [4 5 6]"
	l := NewLexer(input, nil)
	p := NewMShellSimpleCliParser(l)

	pipeline, err := p.Parse()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	file := pipeline.ToMShellFile()
	if file == nil {
		t.Fatal("Expected non-nil MShellFile")
	}

	// Should have: [list] ;
	if len(file.Items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(file.Items))
	}

	// First item should be a list
	cmdList, ok := file.Items[0].(*MShellParseList)
	if !ok {
		t.Fatalf("Expected first item to be MShellParseList, got %T", file.Items[0])
	}

	// Command list should have 4 items: 'numargs' '1' '2' [4 5 6]
	if len(cmdList.Items) != 4 {
		t.Errorf("Expected 4 items in command list, got %d", len(cmdList.Items))
	}

	// Fourth item should be a nested list (preserved from input)
	_, ok = cmdList.Items[3].(*MShellParseList)
	if !ok {
		t.Errorf("Expected fourth item to be MShellParseList, got %T", cmdList.Items[3])
	}
}
