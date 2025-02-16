package main

import (
	"fmt"
	"io"
	"os"
	// "bufio"
	"golang.org/x/term"
	"strings"
	// "runtime/pprof"
	// "runtime/trace"
	// "runtime"
	"time"
)

type CliCommand int

const (
	CLILEX CliCommand = iota
	CLIPARSE
	CLITYPECHECK
	CLIEXECUTE
)

var tempFiles []string

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

	defer cleanupTempFiles()

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
		numRows, numCols, err := term.GetSize(0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting terminal size: %s\n", err)
			os.Exit(1)
		}

		// For debugging, write number of bytes read and bytes to /tmp/mshell.log
		// Open file for writing
		f, err := os.OpenFile("/tmp/mshell.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening file /tmp/mshell.log: %s\n", err)
			os.Exit(1)
			return
		}
		defer f.Close()

		termState := TermState{
			numRows:        numRows,
			numCols:        numCols,
			promptLength:   0,
			currentCommand: make([]rune, 0, 100),
			index:          0,
			readBuffer:     make([]byte, 1024),
			homeDir:        os.Getenv("HOME"),

			stack : make(MShellStack, 0),

			context : ExecuteContext{
				StandardInput:  nil, // These should be nil as that represents using a "default", not os.Stdin/os.Stdout
				StandardOutput: nil,
				Variables:      map[string]MShellObject{},
			},

			callStack : make(CallStack, 10),
			f: f,
			evalState : EvalState{
				PositionalArgs: make([]string, 0),
				LoopDepth:      0,
				StopOnError:	false,
			},
			initCallStackItem : CallStackItem{
				MShellParseItem: nil,
				Name:  "main",
				CallStackType: CALLSTACKFILE,
			},
		}

		termState.InteractiveMode()
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
		StandardInput:  nil,
		StandardOutput: nil,
		Variables:      map[string]MShellObject{},
	}

	var allDefinitions []MShellDefinition

	var callStack CallStack
	callStack = make([]CallStackItem, 10)

	// Check for environment variable MSHSTDLIB and load that file. Read as UTF-8
	stdlibPath, stdlibSet := os.LookupEnv("MSHSTDLIB")
	if stdlibSet {
		// Split the path by :
		// If there are multiple paths, load each one.
		rcPaths := strings.Split(stdlibPath, ":")

		for _, stdlibPath := range rcPaths {
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

			if len(stdlibFile.Items) > 0 {
				callStackItem := CallStackItem{
					MShellParseItem: stdlibFile.Items[0],
					Name:  stdlibPath,
					CallStackType: CALLSTACKFILE,
				}

				result := state.Evaluate(stdlibFile.Items, &stack, context, allDefinitions, callStack, callStackItem)
				if !result.Success {
					fmt.Fprintf(os.Stderr, "Error evaluating MSHSTDLIB file %s.\n", stdlibPath)
					os.Exit(1)
					return
				}
			}
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

	if len(file.Items) == 0 {
		os.Exit(0)
	}

	callStackItem := CallStackItem{
		MShellParseItem: file.Items[0],
		Name:  "main",
		CallStackType: CALLSTACKFILE,
	}

	result := state.Evaluate(file.Items, &stack, context, allDefinitions, callStack, callStackItem)

	if !result.Success {
		if result.ExitCode != 0 {
			os.Exit(result.ExitCode)
		} else {
			os.Exit(1)
		}
	}
}

type TermState struct {
	numRows int
	numCols int
	promptLength int
	numPromptLines int
	currentCommand []rune
	index int // index of cursor, starts at 0
	readBuffer []byte
	oldState *term.State
	homeDir string
	l *Lexer
	p *MShellParser
	historyIndex int
	f *os.File

	stack MShellStack
	context ExecuteContext
	evalState EvalState
	callStack CallStack
	stdLibDefs []MShellDefinition
	initCallStackItem CallStackItem
}

func (state *TermState) clearToPrompt() {
	fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1)
	fmt.Fprintf(os.Stdout, "\033[K")
}

var tokenBuf []Token
var tokenBufBuilder strings.Builder
var aliases map[string]string
var history []string

func (state *TermState) InteractiveMode() {
	// FUTURE: Maybe Check for CSI u?

	// TODO: Read from file? Something like a snippet engine?
	aliases = map[string]string{
		"s": "[git status -u];",
		"v": "nvim",
		"mk": "mkdir",
		"ls": "['ls' -al --color];",
		"gi": "[nvim .gitignore];",
		"a": "[git add",
		"d": "[git diff -w];",
		"dc": "[git diff -w --cached];",
		"c": "[git commit];",
		"p": "[git push];",
		"u": ".. cd",
		"gu": "[git add -u]? ([git status -u];) () iff",
		"ga": "[git add -A]? ([git status -u];) () iff",
		"fp": "[git fetch --prune];",
	}

	// Put terminal into raw mode
	oldState, err := term.MakeRaw(0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error setting terminal to raw mode: %s\n", err)
		os.Exit(1)
		return
	}
	state.oldState = oldState
	defer term.Restore(0, oldState)

	state.l = NewLexer("")
	state.p = &MShellParser{lexer: state.l}

	stdLibDefs, err := stdLibDefinitions(state.stack, state.context, state.evalState, state.callStack)
	if err != nil {
		term.Restore(0, oldState)
		fmt.Fprintf(os.Stderr, "Error loading standard library: %s\n", err)
		os.Exit(1)
		return
	}
	state.stdLibDefs = stdLibDefs

	history = make([]string, 0)
	historyIndex := 0
	// readBuffer := make([]byte, 1024)
	// currentCommand := strings.Builder{}
	// currentCommand := make([]rune, 0, 100)

	state.printPrompt()

	// _, curCol, err, _ := getCurrentPos()
	// if err != nil {
		// fmt.Fprintf(os.Stderr, "Error getting cursor position: %s\n", err)
		// os.Exit(1)
	// }

	// promptLength := curCol - 1
	// index := 0


	for {
		// Read char
		n, err := os.Stdin.Read(state.readBuffer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading from stdin: %s\n", err)
			os.Exit(1)
			return
		}

		fmt.Fprintf(state.f, "%d\t", n)
		for i := 0; i < n; i++ {
			fmt.Fprintf(state.f, "%d ", state.readBuffer[i])
		}
		fmt.Fprintf(state.f, "\n")

		i := 0
		for i < n {
			c := state.readBuffer[i]
			i++

			if c == 1 { // Ctrl-A
				// Move cursor to beginning of line.
				fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1)
				state.index = 0
			} else if c == 2 { // CTRL-B
				// Move cursor left
				if state.index > 0 {
					state.index--
					fmt.Fprintf(os.Stdout, "\033[D")
				}
			} else if c == 3 || c == 4 {
				// Ctrl-C or Ctrl-D
				fmt.Fprintf(os.Stdout, "\r\n") // Print a nice clean newline.
				os.Exit(0)
			} else if c == 5 { // Ctrl-E
				// Move cursor to end of line
				fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + len(state.currentCommand))
				state.index = len(state.currentCommand)
			} else if c == 6 { // Ctrl-F
				// Move cursor right
				if state.index < len(state.currentCommand) {
					state.index++
					fmt.Fprintf(os.Stdout, "\033[C")
				}
			} else if c == 11 { // Ctrl-K
				// Erase to end of line
				fmt.Fprintf(os.Stdout, "\033[K")
				state.currentCommand = state.currentCommand[:state.index]
			} else if c == 12 { // Ctrl-L
				// See https://github.com/microsoft/terminal/issues/17320
				// and https://github.com/microsoft/terminal/issues/11078
				// Some terminals are erasing text in scrollback buffer using the \e[nS escape sequence.

				// Implement using \n's instead.
				curRow, curCol, err, _ := getCurrentPos()
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error getting cursor position: %s\n", err)
					os.Exit(1)
				}

				rowsToScroll := curRow - state.numPromptLines
				fmt.Fprintf(state.f, "%d %d %d\n", curRow, state.numPromptLines, rowsToScroll)

				// Move cursor to bottom of terminal, if you have a terminal that has over 10000 lines, I'm sorry.
				fmt.Fprintf(os.Stdout, "\033[10000B")
				// print out rowsToScroll newlines
				for i := 0; i < rowsToScroll; i++ {
					fmt.Fprintf(os.Stdout, "\n")
				}

				// Move cursor
				fmt.Fprintf(os.Stdout, "\033[%d;%dH", state.numPromptLines, curCol)
			} else if c == 13 { // Enter
				// Add command to history
				currentCommandStr := string(state.currentCommand)

				if state.index == len(state.currentCommand) {
					// Walk back to last whitespace, check if final element is an alias.
					i := state.index
					for {
						if i == 0 || state.currentCommand[i-1] == ' ' || state.currentCommand[i-1] == '[' {
							break
						}
						i--
					}

					lastWord := string(state.currentCommand[i:state.index])
					alias, aliasSet := aliases[lastWord]
					if aliasSet {
						currentCommandStr = currentCommandStr[:i] + alias
					}

					// Update the UI.
					state.clearToPrompt()
					fmt.Fprintf(os.Stdout, "%s", currentCommandStr)
					state.index = len(state.currentCommand)
					state.currentCommand = []rune(currentCommandStr)
					// Move cursor to end
					fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index+1)
				}

				if len(currentCommandStr) > 0 {
					history = append(history, currentCommandStr)
				}

				historyIndex = 0

				// Reset current command
				state.currentCommand = state.currentCommand[:0]

				p := state.p
				l := state.l

				state.l.resetInput(currentCommandStr)
				state.p.NextToken()

				if p.curr.Type == LITERAL {
					// Check for known commands. If so, we'll essentially wrap the entire command in a list to execute
					literalStr := p.curr.Lexeme

					if literalStr == "sudo" || literalStr == "git" || literalStr == "cd" || literalStr == "nvim" {
						tokenBufBuilder.Reset()
						tokenBufBuilder.WriteString("[")

						tokenBufBuilder.WriteString("'" + literalStr + "'")

						// Clear token buffer
						tokenBuf = tokenBuf[:0]

						// Consume all tokens
						for p.NextToken(); p.curr.Type != EOF; p.NextToken() {
							tokenBuf = append(tokenBuf, p.curr)
						}

						for _, t := range tokenBuf {
							tokenBufBuilder.WriteString(" ")
							tokenBufBuilder.WriteString(t.Lexeme)
						}

						tokenBufBuilder.WriteString("];")
						currentCommandStr = tokenBufBuilder.String()
						fmt.Fprintf(state.f, "Command: %s\n", currentCommandStr)
						l.resetInput(currentCommandStr)
						p.NextToken()
					}
				}

				parsed, err := p.ParseFile()
				if err != nil {
					fmt.Fprintf(os.Stderr, "\r\nError parsing input: %s\n", err)
					// Move to start
					fmt.Fprintf(os.Stdout, "\033[1G")
					state.printPrompt()
					state.index = 0
					continue
				}

				// During evaluation, normal terminal output can happen, or TUI apps can be run.
				// So want them to see non-raw mode terminal state.
				term.Restore(0, state.oldState)
				fmt.Fprintf(os.Stdout, "\n")

				if len(parsed.Items) > 0 {
					state.initCallStackItem.MShellParseItem = parsed.Items[0]
					result := state.evalState.Evaluate(parsed.Items, &state.stack, state.context, stdLibDefs, state.callStack, state.initCallStackItem)

					if result.ExitCalled {
						// Reset terminal to original state
						os.Exit(result.ExitCode)
						break
					}

					if !result.Success {
						fmt.Fprintf(os.Stderr, "Error evaluating input.\n")
					}
				}

				fmt.Fprintf(os.Stdout, "\033[1G")
				state.printPrompt()
				state.index = 0

				// Put terminal back into raw mode
				oldState, err = term.MakeRaw(0)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error setting terminal to raw mode: %s\n", err)
					os.Exit(1)
					return
				}
			} else if c == 21 { // Ctrl-U
				// Erase back to prompt start
				fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1)
				fmt.Fprintf(os.Stdout, "\033[K")
				// fmt.Fprintf(os.Stdout, "\033[2K\033[1G")
				// fmt.Fprintf(os.Stdout, "mshell> ")
				// state.printPrompt()

				// // Remaining chars in current command
				state.currentCommand = state.currentCommand[state.index:]
				for i := 0; i < len(state.currentCommand); i++ {
					fmt.Fprintf(os.Stdout, "%c", state.currentCommand[i])
				}

				fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1)
				state.index = 0
			} else if c == 23 { // Ctrl-W
				// Erase last word
				if state.index > 0 {
					// First consume all whitespace
					for state.index > 0 && state.currentCommand[state.index-1] == ' ' {
						state.index--
					}

					// Then consume all non-whitespace
					for state.index > 0 && state.currentCommand[state.index-1] != ' ' {
						state.index--
					}

					// Erase the word
					fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + state.index)
					fmt.Fprintf(os.Stdout, "\033[K")
					state.currentCommand = state.currentCommand[:state.index]
				}
			} else if c == 27 && i < n { // 27 = Escape
				c = state.readBuffer[i]
				i++

				if c == 79 && i < n {
					c = state.readBuffer[i]
					i++

					if c == 80 { // F1
						// Set state.currentCommand to "lf"
						state.currentCommand = []rune{'l', 'f'}
						state.ExecuteCurrentCommand()
					} else if c == 81 { // F2

					} else if c == 82 { // F3

					} else if c == 83 { // F4

					}
				} else if c == 91 && i < n { // 91 = [
					c = state.readBuffer[i]
					i++
					if c == 51 && i < n {
						c = state.readBuffer[i]
						i++
						if c == 126 {
							// Delete
							if state.index < len(state.currentCommand) {
								fmt.Fprintf(os.Stdout, "\033[K")
								fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index+1:]))
								state.currentCommand = append(state.currentCommand[:state.index], state.currentCommand[state.index+1:]...)
								fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index)
							}
						}
					} else if c == 65 {
						// Up arrow
						if historyIndex >= 0 && historyIndex < len(history) {
							historyIndex++
							// Clear back to prompt
							state.clearToPrompt()
							reverseIndex := len(history) - historyIndex
							// state.printPrompt()
							fmt.Fprintf(os.Stdout, history[reverseIndex])
							// fmt.Fprintf(os.Stdout, "mshell> %s", history[reverseIndex])
							state.currentCommand = []rune(history[reverseIndex])
							state.index = len(state.currentCommand)
						}
					} else if c == 66 {
						// Down arrow
						if historyIndex > 0 && historyIndex <= len(history) + 1 {
							historyIndex--
							state.clearToPrompt()
							if historyIndex == 0 {
								// state.printPrompt()
								// fmt.Fprintf(os.Stdout, "mshell> ")
								state.currentCommand = []rune{}
								state.index = 0
							} else {
								reverseIndex := len(history) - historyIndex
								// fmt.Fprintf(os.Stdout, "mshell> %s", history[reverseIndex])
								// state.printPrompt()
								fmt.Fprintf(os.Stdout, history[reverseIndex])
								state.currentCommand = []rune(history[reverseIndex])
								state.index = len(state.currentCommand)
							}
						} else if historyIndex <= 0 {
							historyIndex = 0
						} else if historyIndex > len(history) {
							historyIndex = len(history)
						}

					} else if c == 67 {
						// Right arrow
						if state.index < len(state.currentCommand) {
							state.index++
							fmt.Fprintf(os.Stdout, "\033[C")
						}
					} else if c == 68 {
						// Left arrow
						if state.index > 0 {
							state.index--
							fmt.Fprintf(os.Stdout, "\033[D")
						}
					}
				} else if c == 98 { // Alt-B
					// Move cursor left by word
					if state.index > 0 {
						// First consume all whitespace
						for state.index > 0 && state.currentCommand[state.index-1] == ' ' {
							state.index--
						}

						// Then consume all non-whitespace
						for state.index > 0 && state.currentCommand[state.index-1] != ' ' {
							state.index--
						}

						fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index)
					}
				} else if c == 102 { // Alt-F
					// Move cursor right by word
					if state.index < len(state.currentCommand) {
						// First consume all whitespace
						for state.index < len(state.currentCommand) && state.currentCommand[state.index] == ' ' {
							state.index++
						}

						// Then consume all non-whitespace
						for state.index < len(state.currentCommand) && state.currentCommand[state.index] != ' ' {
							state.index++
						}
					}

					fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index)
				} else {
					fmt.Fprintf(state.f, "Unknown sequence: %d %d %d\n", state.readBuffer[0], state.readBuffer[1], state.readBuffer[2])
				}
			} else if c == 32 { // Space
				// Check for aliases. Split current command by whitespace, and check if last word is in aliases.
				// If it is, replace last word with alias value.

				i := state.index - 1
				for {
					if i < 0 || state.currentCommand[i] == ' ' || state.currentCommand[i] == '[' {
						break
					}
					i--
				}

				lastWord := string(state.currentCommand[i+1:state.index])

				aliasValue, aliasSet := aliases[lastWord]
				if aliasSet {
					// Erase starting at beginning of last word
					fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+i+1)
					fmt.Fprintf(os.Stdout, "\033[K")

					// Print alias value
					fmt.Fprintf(os.Stdout, aliasValue)

					// Print the space
					fmt.Fprintf(os.Stdout, " ")

					// Print the rest of the command
					fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index:]))

					// Update current command
					startText := state.currentCommand[:i+1]
					endText := state.currentCommand[state.index:]

					state.currentCommand = state.currentCommand[:0]

					state.currentCommand = append(state.currentCommand, startText...)
					state.currentCommand = append(state.currentCommand, []rune(aliasValue)...)
					state.currentCommand = append(state.currentCommand, ' ')
					state.currentCommand = append(state.currentCommand, endText...)

					// state.currentCommand = append(state.currentCommand, ' ')
					// state.currentCommand = append(state.currentCommand, state.currentCommand[state.index:]...)

					fmt.Fprintf(state.f, "Alias: %s -> %s\n", lastWord, aliasValue)
					fmt.Fprintf(state.f, "Current command: %s\n", string(state.currentCommand))

					// Move cursor to end of the alias
					state.index = i + 1 + len(aliasValue) + 1
					fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index)
				} else {
					fmt.Fprintf(os.Stdout, "\033[K")
					fmt.Fprintf(os.Stdout, "%c", c)
					fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index:]))
					fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index+1)

					state.currentCommand = append(state.currentCommand[:state.index], append([]rune{rune(c)}, state.currentCommand[state.index:]...)...)
					state.index++
				}
			} else if c > 32 && c <= 126 {
				// Add chars to current command at current index
				fmt.Fprintf(os.Stdout, "\033[K")
				fmt.Fprintf(os.Stdout, "%c", c)
				fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index:]))
				fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index+1)

				state.currentCommand = append(state.currentCommand[:state.index], append([]rune{rune(c)}, state.currentCommand[state.index:]...)...)
				state.index++
			} else if c == 127 { // Backspace
				// Erase last char
				if state.index > 0 {
					state.currentCommand = append(state.currentCommand[:state.index-1], state.currentCommand[state.index:]...)
					state.index--

					fmt.Fprintf(os.Stdout, "\033[D")
					fmt.Fprintf(os.Stdout, "\033[K")
					fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index:]))
					fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index)
				}
			} else {
				fmt.Fprintf(state.f, "Unknown character: %d\n", c)
			}
		}


		fmt.Fprintf(state.f, "%s\t%d\n", string(state.currentCommand), state.index)
		// if !scanner.Scan() {
			// break
		// }
	}
}

func (state *TermState) ExecuteCurrentCommand() {
	// Add command to history
	currentCommandStr := string(state.currentCommand)

	if state.index == len(state.currentCommand) {
		// Walk back to last whitespace, check if final element is an alias.
		i := state.index
		for {
			if i == 0 || state.currentCommand[i-1] == ' ' || state.currentCommand[i-1] == '[' {
				break
			}
			i--
		}

		lastWord := string(state.currentCommand[i:state.index])
		alias, aliasSet := aliases[lastWord]
		if aliasSet {
			currentCommandStr = currentCommandStr[:i] + alias
		}

		// Update the UI.
		state.clearToPrompt()
		fmt.Fprintf(os.Stdout, "%s", currentCommandStr)
		state.index = len(state.currentCommand)
		state.currentCommand = []rune(currentCommandStr)
		// Move cursor to end
		fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index+1)
	}

	if len(currentCommandStr) > 0 {
		history = append(history, currentCommandStr)
	}

	state.historyIndex = 0

	// Reset current command
	state.currentCommand = state.currentCommand[:0]

	p := state.p
	l := state.l

	fmt.Fprintf(state.f, "Executing Command: '%s'\n", currentCommandStr)
	state.l.resetInput(currentCommandStr)
	state.p.NextToken()

	if p.curr.Type == LITERAL {
		// Check for known commands. If so, we'll essentially wrap the entire command in a list to execute
		literalStr := p.curr.Lexeme

		if literalStr == "sudo" || literalStr == "git" || literalStr == "cd" {
			tokenBufBuilder.Reset()
			tokenBufBuilder.WriteString("[")

			tokenBufBuilder.WriteString("'" + literalStr + "'")

			// Clear token buffer
			tokenBuf = tokenBuf[:0]

			// Consume all tokens
			for p.NextToken(); p.curr.Type != EOF; p.NextToken() {
				tokenBuf = append(tokenBuf, p.curr)
			}

			for _, t := range tokenBuf {
				tokenBufBuilder.WriteString(" ")
				tokenBufBuilder.WriteString(t.Lexeme)
			}

			tokenBufBuilder.WriteString("];")
			currentCommandStr = tokenBufBuilder.String()
			fmt.Fprintf(state.f, "Command: %s\n", currentCommandStr)
			l.resetInput(currentCommandStr)
			p.NextToken()
		}
	}

	parsed, err := p.ParseFile()
	if err != nil {
		fmt.Fprintf(os.Stderr, "\r\nError parsing input: %s\n", err)
		// Move to start
		fmt.Fprintf(os.Stdout, "\033[1G")
		state.printPrompt()
		state.index = 0
		return
	}

	// During evaluation, normal terminal output can happen, or TUI apps can be run.
	// So want them to see non-raw mode terminal state.
	term.Restore(0, state.oldState)
	fmt.Fprintf(os.Stdout, "\n")

	if len(parsed.Items) > 0 {
		state.initCallStackItem.MShellParseItem = parsed.Items[0]
		result := state.evalState.Evaluate(parsed.Items, &state.stack, state.context, state.stdLibDefs, state.callStack, state.initCallStackItem)

		if result.ExitCalled {
			// Reset terminal to original state
			os.Exit(result.ExitCode)
		}

		if !result.Success {
			fmt.Fprintf(os.Stderr, "Error evaluating input.\n")
		}
	}

	fmt.Fprintf(os.Stdout, "\033[1G")
	state.printPrompt()
	state.index = 0

	// Put terminal back into raw mode
	_, err = term.MakeRaw(0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error setting terminal to raw mode: %s\n", err)
		os.Exit(1)
		return
	}
}

func (state *TermState) toCooked() {
	term.Restore(0, state.oldState)
}

func (state *TermState) printPrompt() {
	// Get out of raw mode
	state.toCooked()

	fmt.Fprintf(os.Stdout, "\033[35m")
	// Print PWD
	cwd, err := os.Getwd()

	if len(state.homeDir) > 0 && strings.HasPrefix(cwd, state.homeDir) {
		cwd = "~" + cwd[len(state.homeDir):]
	}

	var promptText string
	if err != nil {
		promptText = "??? >"
	} else {
		promptText = fmt.Sprintf("%s > \n:: ", cwd)
	}
	fmt.Fprintf(os.Stdout, promptText)
	state.numPromptLines = strings.Count(promptText, "\n") + 1
	fmt.Fprintf(os.Stdout, "\033[0m")

	// fmt.Fprintf(os.Stdout, "mshell> ")

	_, err = term.MakeRaw(0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error setting terminal to raw mode: %s\n", err)
		os.Exit(1)
	}

	_, col, err, _ := getCurrentPos()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting cursor position: %s\n", err)
		os.Exit(1)
	}

	state.promptLength = col - 1
}

// Returns the current cursor position as (row, col)
// Row and col are 1-based.
// Extra bytes are returned in case the response contains more than just the cursor position escape codes.
func getCurrentPos() (row int, col int, err error, extraBytes []byte) {
	// Open log file
	f, err := os.OpenFile("/tmp/mshell.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file /tmp/mshell.log: %s\n", err)
		os.Exit(1)
		return
	}
	defer f.Close()

	fmt.Fprintf(os.Stdout, "\033[6n")

	// Read response
	readBuffer := make([]byte, 1024)
	os.Stdin.SetReadDeadline(time.Now().Add(1 * time.Second))

	fmt.Fprintf(f, "Starting Reading cursor position\n")

	n, err := os.Stdin.Read(readBuffer)
	if err != nil {
		return 0, 0, err, []byte{}
	}
	os.Stdin.SetReadDeadline(time.Time{})

	fmt.Fprintf(f, "Read %d bytes\n", n)

	// Parse response
	if n < 4 {
		return 0, 0, fmt.Errorf("Did not receive enough bytes for cursor position"), readBuffer[:n]
	}

	if readBuffer[0] != 27 || readBuffer[1] != 91 {
		return 0, 0, fmt.Errorf("Invalid response for cursor position"), readBuffer[:n]
	}

	// Parse row
	row = 0
	i := 2

	for i < n && readBuffer[i] != ';' {
		digit := int(readBuffer[i] - '0')
		if digit < 0 || digit > 9 {
			return 0, 0, fmt.Errorf("Invalid response for cursor position"), readBuffer[:n]
		}
		row = row*10 + digit
		i++
	}

	if i == n {
		return 0, 0, fmt.Errorf("Invalid response for cursor position"), readBuffer[:n]
	}
	// Skip ;
	i++

	// Parse column
	col = 0
	for i < n && readBuffer[i] != 'R' {
		digit := int(readBuffer[i] - '0')
		if digit < 0 || digit > 9 {
			return 0, 0, fmt.Errorf("Invalid response for cursor position"), readBuffer[:n]
		}

		col = col*10 + digit
		i++
	}

	if i == n {
		return 0, 0, fmt.Errorf("Invalid response for cursor position"), readBuffer[:n]
	}

	if readBuffer[i] != 'R' {
		return 0, 0, fmt.Errorf("Invalid response for cursor position"), readBuffer[:n]
	}

	i++ // Consume R

	return row, col, nil, readBuffer[i:]
}

func stdLibDefinitions(stack MShellStack, context ExecuteContext, state EvalState, callStack CallStack) ([]MShellDefinition, error) {
	// Check for environment variable MSHSTDLIB and load that file. Read as UTF-8
	stdlibPath, stdlibSet := os.LookupEnv("MSHSTDLIB")
	definitions := make([]MShellDefinition, 0)

	if stdlibSet {
		// Split the path by :
		// If there are multiple paths, load each one.
		rcPaths := strings.Split(stdlibPath, ":")

		for _, rcPath := range rcPaths {
			stdlibBytes, err := os.ReadFile(rcPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading file %s: %s\n", rcPath, err)
				os.Exit(1)
				return nil, err
			}

			stdlibLexer := NewLexer(string(stdlibBytes))
			stdlibParser := MShellParser{lexer: stdlibLexer}
			stdlibParser.NextToken()
			stdlibFile, err := stdlibParser.ParseFile()

			definitions = append(definitions, stdlibFile.Definitions...)

			if err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing file %s: %s\n", rcPath, err)
				os.Exit(1)
				return nil, err
			}

			if len(stdlibFile.Items) > 0 {
				callStackItem := CallStackItem{
					MShellParseItem: stdlibFile.Items[0],
					Name:  rcPath,
					CallStackType: CALLSTACKFILE,
				}

				// allDefinitions = append(allDefinitions, stdlibFile.Definitions...)
				result := state.Evaluate(stdlibFile.Items, &stack, context, stdlibFile.Definitions, callStack, callStackItem)

				if !result.Success {
					fmt.Fprintf(os.Stderr, "Error evaluating MSHSTDLIB file %s.\n", rcPath)
					os.Exit(1)
					return nil, err
				}
			}
		}
	}

	return definitions, nil
}

func registerTempFileForCleanup(tempFileName string) {
	tempFiles = append(tempFiles, tempFileName)
}

func cleanupTempFiles() {
	for _, tempFile := range tempFiles {
		err := os.Remove(tempFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error removing temp file '%s': %s\n", tempFile, err)
		}
	}
}
