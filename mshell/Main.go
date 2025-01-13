package main

import (
	"fmt"
	"io"
	"os"
	// "bufio"
	"golang.org/x/term"
	// "strings"
	// "runtime/pprof"
	// "runtime/trace"
	// "runtime"
)

type CliCommand int

const (
	CLILEX CliCommand = iota
	CLIPARSE
	CLITYPECHECK
	CLIEXECUTE
)

func main() {
	// Enable profiling
	// runtime.SetCPUProfileRate(1000)
	// f, err := os.Create("mshell.prof")
	// if err != nil {
	// fmt.Println(err)
	// os.Exit(1)
	// return
	// }
	// pprof.StartCPUProfile(f)
	// defer pprof.StopCPUProfile()

	// Enable tracing
	// f, err := os.Create("mshell.trace")
	// if err != nil {
	// fmt.Println(err)
	// os.Exit(1)
	// return
	// }

	// trace.Start(f)
	// defer trace.Stop()

	command := CLIEXECUTE

	// printLex := false
	// printParse := false

	i := 1

	input := ""
	inputSet := false
	positionalArgs := []string{}

	if len(os.Args) == 1 {
		// Enter interactive mode

	}

	for i < len(os.Args) {
		arg := os.Args[i]
		i++
		if arg == "--lex" {
			command = CLILEX
			// printLex = true
		} else if arg == "--typecheck" {
			command = CLITYPECHECK
		} else if arg == "--parse" {
			command = CLIPARSE
			// printParse = true
		} else if arg == "-h" || arg == "--help" {
			fmt.Println("Usage: mshell [OPTION].. FILE [ARG]..")
			fmt.Println("Usage: mshell [OPTION].. [ARG].. < FILE")
			fmt.Println("Usage: mshell [OPTION].. -c INPUT [ARG]..")
			fmt.Println("Options:")
			fmt.Println("  --lex      Print the tokens of the input")
			fmt.Println("  --parse    Print the parsed Abstract Syntax Tree")
			fmt.Println("  -h, --help Print this help message")
			os.Exit(0)
			return
		} else if arg == "-c" {
			if i >= len(os.Args) {
				fmt.Println("Error: -c requires an argument")
				os.Exit(1)
				return
			}

			input = os.Args[i]
			inputSet = true
			positionalArgs = append(positionalArgs, os.Args[i:]...)
			break
		} else {
			inputSet = true
			inputBytes, err := os.ReadFile(arg)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
				return
			}
			input = string(inputBytes)
			// If there are more arguments, add them to positionalArgs.
			positionalArgs = append(positionalArgs, os.Args[i:]...)
			break
		}
	}

	if len(input) == 0 && term.IsTerminal(0) {
		InteractiveMode()
		return
	}

	if !inputSet {
		inputBytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Println(err)
			// Set exit code to 1
			os.Exit(1)
			return
		}
		input = string(inputBytes)
	}


	l := NewLexer(input)

	if command == CLILEX {
		tokens := l.Tokenize()
		fmt.Println("Tokens:")
		for _, t := range tokens {
			//                 Console.Write($"{t.Line}:{t.Column}:{t.TokenType} {t.RawText}\n");
			fmt.Printf("%d:%d:%s %s\n", t.Line, t.Column, t.Type, t.Lexeme)
		}
		return
	} else if command == CLIPARSE {
		p := MShellParser{lexer: l}
		p.NextToken()
		file, err := p.ParseFile()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing file %s: %s\n", input, err)
			os.Exit(1)
			return
		}

		fmt.Println(file.ToJson())
		return
	}

	state := EvalState{
		PositionalArgs: positionalArgs,
		LoopDepth:      0,
	}

	var stack MShellStack
	stack = []MShellObject{}
	context := ExecuteContext{
		StandardInput:  os.Stdin,
		StandardOutput: os.Stdout,
		Variables:      map[string]MShellObject{},
	}

	var allDefinitions []MShellDefinition

	var callStack CallStack
	callStack = make([]CallStackItem, 10)

	// Check for environment variable MSHSTDLIB and load that file. Read as UTF-8
	stdlibPath, stdlibSet := os.LookupEnv("MSHSTDLIB")
	if stdlibSet {
		stdlibBytes, err := os.ReadFile(stdlibPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading file %s: %s\n", stdlibPath, err)
			os.Exit(1)
			return
		}
		stdlibLexer := NewLexer(string(stdlibBytes))
		stdlibParser := MShellParser{lexer: stdlibLexer}
		stdlibParser.NextToken()
		stdlibFile, err := stdlibParser.ParseFile()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing file %s: %s\n", stdlibPath, err)
			os.Exit(1)
			return
		}

		allDefinitions = append(allDefinitions, stdlibFile.Definitions...)
		result := state.Evaluate(stdlibFile.Items, &stack, context, allDefinitions, callStack)

		if !result.Success {
			fmt.Fprintf(os.Stderr, "Error evaluating MSHSTDLIB file %s.\n", stdlibPath)
			os.Exit(1)
			return
		}
	}

	p := MShellParser{lexer: l}
	p.NextToken()
	file, err := p.ParseFile()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing file %s: %s\n", input, err)
		os.Exit(1)
		return
	}

	allDefinitions = append(allDefinitions, file.Definitions...)

	if command == CLITYPECHECK {
		var typeStack MShellTypeStack
		typeStack = make([]MShellType, 0)
		typeCheckResult := TypeCheck(file.Items, typeStack, allDefinitions, false)

		for _, typeError := range typeCheckResult.Errors {
			fmt.Fprintf(os.Stderr, "%s", typeError)
		}

		if len(typeCheckResult.Errors) > 0 {
			os.Exit(1)
		} else {
			os.Exit(0)
		}
	}

	result := state.Evaluate(file.Items, &stack, context, allDefinitions, callStack)

	if !result.Success {
		if result.ExitCode != 0 {
			os.Exit(result.ExitCode)
		} else {
			os.Exit(1)
		}
	}
}

func InteractiveMode() {
	// Put terminal into raw mode
	oldState, err := term.MakeRaw(0)

	defer term.Restore(0, oldState)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error setting terminal to raw mode: %s\n", err)
		os.Exit(1)
		return
	}

	l := NewLexer("")
	p := MShellParser{lexer: l}

	state := EvalState{
		PositionalArgs: make([]string, 0),
		LoopDepth:      0,
		StopOnError:	false,
	}

	stack := make(MShellStack, 0)

	context := ExecuteContext{
		StandardInput:  os.Stdin,
		StandardOutput: os.Stdout,
		Variables:      map[string]MShellObject{},
	}

	callStack := make(CallStack, 10)

	stdLibDefs, err := stdLibDefinitions(stack, context, state, callStack)
	if err != nil {
		term.Restore(0, oldState)
		fmt.Fprintf(os.Stderr, "Error loading standard library: %s\n", err)
		os.Exit(1)
		return
	}

	history := make([]string, 0)
	readBuffer := make([]byte, 3)
	// currentCommand := strings.Builder{}
	currentCommand := make([]rune, 100)

	// For debugging, write number of bytes read and bytes to /tmp/mshell.log
	// Open file for writing
	f, err := os.OpenFile("/tmp/mshell.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file /tmp/mshell.log: %s\n", err)
		os.Exit(1)
		return
	}
	defer f.Close()

	// Print prompt
	fmt.Print("mshell> ")
	index := 0

	for {
		// Read char
		n, err := os.Stdin.Read(readBuffer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading from stdin: %s\n", err)
			os.Exit(1)
			return
		}

		fmt.Fprintf(f, "%d\t", n)
		for i := 0; i < n; i++ {
			fmt.Fprintf(f, "%d ", readBuffer[i])
		}

		fmt.Fprintf(f, "\t%s\t%d", string(currentCommand), index)
		fmt.Fprintf(f, "\n")

		c := readBuffer[0]

		if c == '\n' {
			// Add command to history
			currentCommandStr := string(currentCommand)
			history = append(history, currentCommandStr)

			// Reset current command
			currentCommand = currentCommand[:0]

			// Print newline
			fmt.Println()

			l.resetInput(currentCommandStr)

			p.NextToken()

			parsed, err := p.ParseFile()
			if err != nil {
				fmt.Println(err)
				continue
			}

			result := state.Evaluate(parsed.Items, &stack, context, stdLibDefs, callStack)

			if !result.Success {
				fmt.Fprintf(os.Stderr, "Error evaluating input.\n")
			}

			if result.ExitCalled {
				// Reset terminal to original state
				os.Exit(result.ExitCode)
				break
			}

		} else if c == 3 || c == 4 {
			// Ctrl-C or Ctrl-D
			os.Exit(0)
		} else if c == 21 { // Ctrl-U
			// Erase current line and reset
			fmt.Fprintf(os.Stdout, "\033[2K\033[1G")
			fmt.Fprintf(os.Stdout, "mshell> ")

			// Remaining chars in current command
			currentCommand = currentCommand[index:]

			for i := 0; i < len(currentCommand); i++ {
				fmt.Fprintf(os.Stdout, "%c", currentCommand[i])
			}

			index = 0
		} else {
			// Add chars to current command
			for i := 0; i < n; i++ {
				currentCommand = append(currentCommand, rune(readBuffer[i]))
			}

			fmt.Fprintf(os.Stdout, "%s", string(readBuffer[:n]))
			index += n
		}

		// if !scanner.Scan() {
			// break
		// }
	}
}

func stdLibDefinitions(stack MShellStack, context ExecuteContext, state EvalState, callStack CallStack) ([]MShellDefinition, error) {
	// Check for environment variable MSHSTDLIB and load that file. Read as UTF-8
	stdlibPath, stdlibSet := os.LookupEnv("MSHSTDLIB")
	if stdlibSet {
		stdlibBytes, err := os.ReadFile(stdlibPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading file %s: %s\n", stdlibPath, err)
			os.Exit(1)
			return nil, err
		}
		stdlibLexer := NewLexer(string(stdlibBytes))
		stdlibParser := MShellParser{lexer: stdlibLexer}
		stdlibParser.NextToken()
		stdlibFile, err := stdlibParser.ParseFile()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing file %s: %s\n", stdlibPath, err)
			os.Exit(1)
			return nil, err
		}

		// allDefinitions = append(allDefinitions, stdlibFile.Definitions...)
		result := state.Evaluate(stdlibFile.Items, &stack, context, stdlibFile.Definitions, callStack)

		if !result.Success {
			fmt.Fprintf(os.Stderr, "Error evaluating MSHSTDLIB file %s.\n", stdlibPath)
			os.Exit(1)
			return nil, err
		}

		return stdlibFile.Definitions, nil
	}

	return make([]MShellDefinition, 0), nil
}
