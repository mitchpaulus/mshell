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
	"runtime"
	// "time"
	// "unicode"
	"sort"
	"strconv"
	// "runtime/debug"
	"path/filepath"
	"crypto/sha256"
	"time"
	"encoding/binary"
)

type CliCommand int

const (
	CLILEX CliCommand = iota
	CLIPARSE
	CLITYPECHECK
	CLIEXECUTE
)

var tempFiles []string


func longestCommonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	} else if len(strs) == 1 {
		return strs[0]
	}

	sort.Strings(strs)

	first := strs[0]
	last := strs[len(strs)-1]
	b := strings.Builder{}

	for i := 0; i < min(len(first), len(last)); i++ {
		if first[i] == last[i] {
			b.WriteByte(first[i])
		} else {
			return b.String()
		}
	}

	return b.String()
}

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
	var err error

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
		} else if arg == "--version" {
			fmt.Fprintf(os.Stdout, "0.4.0\n")
			os.Exit(0)
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

	// The Windows stdOutFd is not 0. Seen stuff like 124.
	stdOutFd := int(os.Stdout.Fd())

	// isTerminal := term.IsTerminal(fd)
	// fmt.Fprintf(os.Stdout, "Is terminal: %t %d\n", isTerminal, fd)

	if len(input) == 0 && term.IsTerminal(stdOutFd) {
		// fmt.Fprintf(os.Stdout, "Got here\n")
		numRows, numCols, err := term.GetSize(stdOutFd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting terminal size: %s\n", err)
			os.Exit(1)
		}

		// For debugging, write number of bytes read and bytes to /tmp/mshell.log
		// If on Windows
		var f *os.File
		if runtime.GOOS == "windows" {
			local_app_data, ok := os.LookupEnv("LOCALAPPDATA")
			if !ok {
				fmt.Fprintf(os.Stderr, "Error getting LOCALAPPDATA environment variable\n")
				os.Exit(1)
				return
			}

			// Make dir LOCALAPPDATA/mshell if it doesn't exist
			err = os.MkdirAll(local_app_data + "/mshell", 0755)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating directory %s/mshell: %s\n", local_app_data, err)
				os.Exit(1)
				return
			}

			// Open file for writing
			f, err = os.OpenFile(local_app_data + "/mshell/mshell.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error opening file %s/mshell/mshell.log: %s\n", local_app_data, err)
				os.Exit(1)
				return
			}
			defer f.Close()
		} else {
			// Open file for writing
			f, err = os.OpenFile("/tmp/mshell.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error opening file /tmp/mshell.log: %s\n", err)
				os.Exit(1)
				return
			}
			defer f.Close()
		}

		callStack := make(CallStack, 0, 10)

		stdInFd := int(os.Stdin.Fd())
		termState := TermState{
			stdInFd:        stdInFd,
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
				Pbm: NewPathBinManager(),
			},

			callStack : callStack,
			f: f,
			evalState : EvalState{
				PositionalArgs: make([]string, 0),
				LoopDepth:      0,
				StopOnError:	false,
				CallStack: callStack,
			},
			initCallStackItem : CallStackItem{
				MShellParseItem: nil,
				Name:  "main",
				CallStackType: CALLSTACKFILE,
			},
			pathBinManager: NewPathBinManager(),
		}

		err = termState.InteractiveMode()
		if err != nil {
			fmt.Fprintf(os.Stderr, err.Error())
			os.Exit(1)
		} else {
			os.Exit(0)
		}

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

	var callStack CallStack
	callStack = make([]CallStackItem, 0, 10)

	state := EvalState{
		PositionalArgs: positionalArgs,
		LoopDepth:      0,
		CallStack: callStack,
	}

	var stack MShellStack
	stack = []MShellObject{}
	context := ExecuteContext{
		StandardInput:  nil,
		StandardOutput: nil,
		Variables:      map[string]MShellObject{},
		Pbm: NewPathBinManager(),
	}

	var allDefinitions []MShellDefinition


	// Check for environment variable MSHSTDLIB and load that file. Read as UTF-8
	stdlibPathVar, stdlibSet := os.LookupEnv("MSHSTDLIB")
	if stdlibSet {
		// Split the path by :, except on Windows where it's ;
		// If there are multiple paths, load each one.
		var rcPaths []string
		if runtime.GOOS == "windows" {
			rcPaths = strings.Split(stdlibPathVar, ";")
			// fmt.Fprintf(os.Stderr, "Windows: %s\n", stdlibPathVar)
		} else {
			rcPaths = strings.Split(stdlibPathVar, ":")
		}

		for _, stdlibPath := range rcPaths {
			stdlibBytes, err := os.ReadFile(stdlibPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading file '%s': %s\n", stdlibPath, err)
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
					MShellParseItem: nil,
					Name:  stdlibPath,
					CallStackType: CALLSTACKFILE,
				}

				result := state.Evaluate(stdlibFile.Items, &stack, context, allDefinitions, callStackItem)
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
		MShellParseItem: nil,
		Name:  "main",
		CallStackType: CALLSTACKFILE,
	}

	result := state.Evaluate(file.Items, &stack, context, allDefinitions, callStackItem)

	if !result.Success {
		if result.ExitCode != 0 {
			os.Exit(result.ExitCode)
		} else {
			os.Exit(1)
		}
	}
}

type TermState struct {
	stdInFd int
	numRows int
	numCols int
	promptLength int
	numPromptLines int
	currentCommand []rune
	index int // index of cursor, starts at 0
	readBuffer []byte
	oldState term.State
	homeDir string
	l *Lexer
	p *MShellParser
	historyIndex int
	f *os.File // This is log file.
	// tokenChan chan TerminalToken
	stdInState *StdinReaderState

	stack MShellStack
	context ExecuteContext
	evalState EvalState
	callStack CallStack
	stdLibDefs []MShellDefinition
	initCallStackItem CallStackItem
	pathBinManager IPathBinManager
}

func IsDefinitionDefined(name string, definitions []MShellDefinition) bool {
	for _, def := range definitions {
		if def.Name == name {
			return true
		}
	}
	return false
}

func (state *TermState) clearToPrompt() {
	fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1)
	state.index = 0
	ClearToEnd()
}

func ClearToEnd() {
	fmt.Fprintf(os.Stdout, "\033[K")
}

func (state *TermState) ClearScreen() {
	// See https://github.com/microsoft/terminal/issues/17320
	// and https://github.com/microsoft/terminal/issues/11078
	// Some terminals are erasing text in scrollback buffer using the \e[nS escape sequence.

	// Implement using \n's instead.

	// Send off cursor position request
	curRow, curCol, err := state.getCurrentPos()
	if err != nil {
		fmt.Fprintf(state.f, "Error getting cursor position: %s\n", err)
		return
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
}

var tokenBuf []Token
var tokenBufBuilder strings.Builder
var aliases map[string]string
var history []string

var knownCommands = map[string]struct{}{ "cd": {} }

// printText prints the text at the current cursor position, moving existing text to the right.
func (state *TermState) printText(text string) {
	fmt.Fprintf(os.Stdout, "\033[K") // Delete to end of line
	fmt.Fprintf(os.Stdout, "%s", text)
	fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index:]))
	fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + state.index + len(text))

	state.currentCommand = append(state.currentCommand[:state.index], append([]rune(text), state.currentCommand[state.index:]...)...)
	state.index = state.index + len(text)
}

func (state *TermState) replaceText(newText string, replaceStart int, replaceEnd int) {
	fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + replaceStart)
	fmt.Fprintf(os.Stdout, "\033[K") // Delete to end of line
	fmt.Fprintf(os.Stdout, "%s", newText)
	fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[replaceEnd:]))
	fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + replaceStart + len(newText))

	state.currentCommand = append(state.currentCommand[:replaceStart], append([]rune(newText), state.currentCommand[replaceEnd:]...)...)
	state.index = replaceStart + len(newText)
}


type TerminalToken interface {
	String() string
}

type AsciiToken struct {
	Char byte
}

func (t AsciiToken) String() string {
	return fmt.Sprintf("AsciiToken: %d %c", t.Char, t.Char)
}

type CsiToken struct {
	FinalChar byte
	Params []byte
}

func (t CsiToken) String() string {
	chars := make([]string, len(t.Params))
	for i, b := range t.Params {
		chars[i] = fmt.Sprintf("%c", b)
	}

	bytes := make([]string, len(t.Params))
	for i, b := range t.Params {
		bytes[i] = fmt.Sprintf("%d", b)
	}

	return fmt.Sprintf("CsiToken %s %c [%s]", strings.Join(chars, " "), t.FinalChar, strings.Join(bytes, " "))
}

type UnknownToken struct { }
func (t UnknownToken) String() string {
	return fmt.Sprintf("UnknownToken")
}

type SpecialKey int

func (t SpecialKey) String() string {
	return fmt.Sprintf("SpecialKey: %d %s", t, SpecialKeyName[t])
}

var SpecialKeyName = []string{
    "KEY_F1",
    "KEY_F2",
    "KEY_F3",
    "KEY_F4",
    "KEY_F5",
    "KEY_F6",
    "KEY_F7",
    "KEY_F8",
    "KEY_F9",
    "KEY_F10",
    "KEY_F11",
    "KEY_F12",
    "KEY_UP",
    "KEY_DOWN",
    "KEY_LEFT",
    "KEY_RIGHT",
    "KEY_DELETE",
    "KEY_HOME",
    "KEY_END",
    "KEY_ALT_B",
    "KEY_ALT_F",
    "KEY_ALT_O",
    "KEY_CTRL_DELETE",
}

const (
	KEY_F1 SpecialKey = iota
	KEY_F2
	KEY_F3
	KEY_F4
	KEY_F5
	KEY_F6
	KEY_F7
	KEY_F8
	KEY_F9
	KEY_F10
	KEY_F11
	KEY_F12

	KEY_UP
	KEY_DOWN
	KEY_LEFT
	KEY_RIGHT

	KEY_DELETE

	KEY_HOME
	KEY_END

	KEY_ALT_B
	KEY_ALT_F
	KEY_ALT_O

	KEY_CTRL_DELETE
)

type EofTerminalToken struct { }
func (t EofTerminalToken) String() string {
	return fmt.Sprintf("EOF")
}

type StdinReaderState struct {
	array []byte
	i int
	n int
}

func (state *StdinReaderState) ReadByte() (byte, error) {
	if state.i >= state.n {
		// Do fresh read
		// fmt.Fprintf(f, "Reading from stdin...\n")
		// fmt.Fprintf(f, "%s", debug.Stack())
		n, err := os.Stdin.Read(state.array)
		// fmt.Fprintf(f, "Read %d from stdin...\n", n)

		if err != nil {
			return 0, err
		}

		state.n = n
		state.i = 0

		b := state.array[state.i]
		state.i++
		return b, nil
	} else {
		// fmt.Fprintf(f, "Reading from buffer at %d..\n", state.i)
		b := state.array[state.i]
		state.i++
		return b, nil
	}
}

func (state *TermState) StdinReader(stdInChan chan byte, pauseChan chan bool) {
	readBuffer := make([]byte, 1024)

	for {
		select {
		case shouldPause := <- pauseChan:
			if shouldPause {
				// Pause reading from stdin
				fmt.Fprintf(state.f, "Pausing stdin reader\n")
				for {
					// Wait for unpause
					shouldUnpause := <- pauseChan
					if !shouldUnpause {
						break
					}
				}
				fmt.Fprintf(state.f, "Unpausing stdin reader\n")
			}
		default:
			// Read char
			n, err := os.Stdin.Read(readBuffer)
			if err != nil {
				if err == io.EOF {
					os.Exit(0)
					return
				} else {
					fmt.Fprintf(os.Stderr, "Error reading from stdin: %s\n", err)
					os.Exit(1)
					return
				}
			}

			for i := 0; i < n; i++ {
				b := readBuffer[i]

				if b > 32 && b < 127 {
					fmt.Fprintf(state.f, "Sending %c..\n", b)
					stdInChan <- readBuffer[i]
					fmt.Fprintf(state.f, "Sent %c..\n", b)
				} else {
					fmt.Fprintf(state.f, "Sending %d..\n", b)
					stdInChan <- readBuffer[i]
					fmt.Fprintf(state.f, "Sent %d..\n", b)
				}
				// fmt.Fprintf(f, "Sending %d..\n", readBuffer[i])
			}
		}
	}
}

// Common Pn Values for ESC [ Pn ~:
// Pn Value	Key	Notes
// 1	Home	Sometimes ESC [ H or ESC [ 7 ~
// 2	Insert
// 3	Delete	The key often labeled "Del" or "Delete
// 4	End	Sometimes ESC [ F or ESC [ 8 ~
// 5	Page Up (PgUp)
// 6	Page Down (PgDn)
// 7	Home	Alternative mapping seen on some terminals
// 8	End	Alternative mapping seen on some terminals
// 11	F1	Often ESC O P in application mode
// 12	F2	Often ESC O Q in application mode
// 13	F3	Often ESC O R in application mode
// 14	F4	Often ESC O S in application mode
// 15	F5
// 17	F6	Note the gap (16 is sometimes used, often not)
// 18	F7
// 19	F8
// 20	F9
// 21	F10
// 23	F11	Note the gap (22 is sometimes used, often not)
// 24	F12
// 25	F13 (Shift+F1)	Sometimes, varies
// 26	F14 (Shift+F2)	Sometimes, varies
// 28	F15 (Shift+F3)	Sometimes, varies
// 29	F16 (Shift+F4)	Sometimes, varies
// 31	F17 (Shift+F5)	Sometimes, varies
// 32	F18 (Shift+F6)	Sometimes, varies
// 33	F19 (Shift+F7)	Sometimes, varies
// 34	F20 (Shift+F8)	Sometimes, varies


// This is intended to a be a lexer for the interactive mode.
// It should be operating in a goroutine.
func (state *TermState) InteractiveLexer(stdinReaderState *StdinReaderState) (TerminalToken, error)  {
	var c byte
	var err error

	for {
		// Read char
		// c := <-inputChan
		c, err = stdinReaderState.ReadByte()
		if err != nil {
			if err == io.EOF {
				return EofTerminalToken{}, nil
			} else {
				return nil, fmt.Errorf("Error reading from stdin: %s", err)
				// fmt.Fprintf(state.f, "Error reading from stdin: %s\n", err)
			}
		}

		if c < 128 && c != 27 {
			// If the character is a printable ASCII character, send it to the channel.
			return AsciiToken{Char: c}, nil
		} else if c == 27 { // ESC
			// c = <-inputChan
			c, err = stdinReaderState.ReadByte()
			if err != nil {
				if err == io.EOF {
					return EofTerminalToken{}, nil
				} else {
					return nil, fmt.Errorf("Error reading from stdin: %s", err)
				}
			}

			if c == 79 { // 79 = O
				// c = <- inputChan
				c, err = stdinReaderState.ReadByte()
				if err != nil {
					if err == io.EOF {
						return EofTerminalToken{}, nil
					} else {
						return nil, fmt.Errorf("Error reading from stdin: %s", err)
					}
				}

				if c == 80 { // F1
					return KEY_F1, nil
				} else if c == 81 { // F2
					return KEY_F2, nil
				} else if c == 82 { // F3
					return KEY_F3, nil
				} else if c == 83 { // F4
					return KEY_F4, nil
				} else {
					// Unknown escape sequence
					fmt.Fprintf(state.f, "Unknown escape sequence: ESC O %d\n", c)
					return UnknownToken{}, nil
				}
			} else if c == 91 { // 91 = [, CSI
				// read until we get a final char, @ to ~, or 0x40 to 0x7E
				// c = <-inputChan
				c, err = stdinReaderState.ReadByte()
				if err != nil {
					if err == io.EOF {
						return EofTerminalToken{}, nil
					} else {
						return nil, fmt.Errorf("Error reading from stdin: %s", err)
					}
				}

				if c >= 64 && c <= 126 {
					if c == 51  {
						// c = <-inputChan
						c, err = stdinReaderState.ReadByte()
						if err != nil {
							if err == io.EOF {
								return EofTerminalToken{}, nil
							} else {
								return nil, fmt.Errorf("Error reading from stdin: %s", err)
							}
						}

						if c == 126 {
							return KEY_DELETE, nil
							// Delete
						}
					} else if c == 65 {
						// Up arrow
						return KEY_UP, nil
					} else if c == 66 {
						// Down arrow
						return KEY_DOWN, nil
					} else if c == 67 {
						// Right arrow
						return KEY_RIGHT, nil
					} else if c == 68 {
						// Left arrow
						return KEY_LEFT, nil
					} else if c == 70 {
						return KEY_END, nil
					} else if c == 72 {
						return KEY_HOME, nil
					} else {
						// Unknown escape sequence
						fmt.Fprintf(state.f, "Unknown escape sequence: ESC [ %d\n", c)
						return UnknownToken{}, nil
					}
				} else { // else read until we get a final char, @ to ~, or 0x40 to 0x7E
					byteArray := make([]byte, 0)
					byteArray = append(byteArray, c)
					for {
						// c = <-inputChan
						// fmt.Fprintf(f, "Reading byte for CSI...\n")
						c, err = stdinReaderState.ReadByte()
						if err != nil {
							if err == io.EOF {
								return EofTerminalToken{}, nil
							} else {
								return nil, fmt.Errorf("Error reading from stdin: %s", err)
							}
						}

						if c >= 64 && c <= 126 {
							if len(byteArray) == 3 && byteArray[0] == 51 && byteArray[1] == 59 && byteArray[2] == 53 {
								return KEY_CTRL_DELETE, nil
							} else if len(byteArray) == 1 && byteArray[0] == 51 {
								return KEY_DELETE, nil
							} else {
								// fmt.Fprintf(f, "Sent CSI token: %d %d\n", c, byteArray)
								return CsiToken{FinalChar: c, Params: byteArray}, nil
							}
						}
						byteArray = append(byteArray, c)
					}
				}
			} else if c == 98 { // Alt-B
				// Move cursor left by word
				return KEY_ALT_B, nil
			} else if c == 102 { // Alt-F
				// Move cursor right by word
				return KEY_ALT_F, nil
			} else if c == 111 { // Alt-O
				return KEY_ALT_O, nil
				// Quit
			} else {
				// Unknown escape sequence
				fmt.Fprintf(state.f, "Unknown escape sequence: ESC %d\n", c)
				return UnknownToken{}, nil
				// return AsciiToken{Char: 27}
				// return AsciiToken{Char: c}
			}
		} else {
			fmt.Fprintf(state.f, "Unknown start byte: %d\n", c)
			// return AsciiToken{Char: c}
			return UnknownToken{}, nil
		}
	}
}

func (state *TermState) InteractiveMode() error {
	// FUTURE: Maybe Check for CSI u?
	stdInState := &StdinReaderState{
		array: make([]byte, 1024),
		i: 0,
		n: 0,
	}

	state.stdInState = stdInState

	// TODO: Read from file? Something like a snippet engine?
	aliases = map[string]string{
		"s": "git status -u",
		"v": "nvim",
		"mk": "mkdir",
		"ls": "ls -al --color",
		"gi": "nvim .gitignore",
		"a": "git add",
		"d": "git diff -w",
		"dc": "git diff -w --cached",
		"c": "git commit",
		"p": "git push",
		"u": ".. cd",
		"gu": "[git add -u]? ([git status -u];) iff",
		"ga": "[git add -A]? ([git status -u];) iff",
		"fp": "git fetch --prune",
	}

	// Put terminal into raw mode
	oldState, err := term.MakeRaw(state.stdInFd)
	if err != nil {
		return fmt.Errorf("Error setting terminal to raw mode at beginning of interactive mode: %s", err)
	}
	state.oldState = *oldState
	fmt.Fprintf(state.f, "Old state: %v\n", state.oldState)

	defer term.Restore(state.stdInFd, &state.oldState)

	state.l = NewLexer("")
	state.p = &MShellParser{lexer: state.l}

	stdLibDefs, err := stdLibDefinitions(state.stack, state.context, state.evalState)
	if err != nil {
		return fmt.Errorf("Error loading standard library: %s\n", err)
	}

	state.stdLibDefs = stdLibDefs

	history = make([]string, 0)
	state.historyIndex = 0

	err = state.printPrompt()
	if err != nil {
		return fmt.Errorf("Error printing prompt: %s\n", err)
	}

	var token TerminalToken
	var end bool
	for {

		fmt.Fprintf(state.f, "Waiting for token... ")
		state.f.Sync()
		token, err = state.InteractiveLexer(stdInState) // token = <- tokenChan
		if err != nil {
			return err
		}

		fmt.Fprintf(state.f, "Got token: %s\n", token)

		if _, ok := token.(EofTerminalToken); ok {
			return nil
		}

		end, err = state.HandleToken(token)

		if err != nil {
			return err
		}

		if end {
			break
		}
	}

	return nil
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

		firstTokenIsCmd := false
		_, isInPath := state.pathBinManager.Lookup(literalStr);
		if isInPath {
			firstTokenIsCmd = true
		} else {
			_, firstTokenIsCmd = knownCommands[literalStr]
		}

		if ((strings.Contains(literalStr, string(os.PathSeparator)) &&  state.pathBinManager.IsExecutableFile(literalStr)) || firstTokenIsCmd) && !IsDefinitionDefined(literalStr, state.stdLibDefs) {
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

		err = state.printPrompt()
		if err != nil {
			fmt.Fprintf(os.Stderr, err.Error())
			os.Exit(1)
		}

		state.index = 0
		return
	}

	// During evaluation, normal terminal output can happen, or TUI apps can be run.
	// So want them to see non-raw mode terminal state.
	term.Restore(state.stdInFd, &state.oldState)
	fmt.Fprintf(os.Stdout, "\n")

	if len(parsed.Items) > 0 {
		state.initCallStackItem.MShellParseItem = parsed.Items[0]
		result := state.evalState.Evaluate(parsed.Items, &state.stack, state.context, state.stdLibDefs, state.initCallStackItem)

		if result.ExitCalled {
			// Reset terminal to original state
			os.Exit(result.ExitCode)
		}

		if !result.Success {
			fmt.Fprintf(os.Stderr, "Error evaluating input.\n")
		}
	}

	fmt.Fprintf(os.Stdout, "\033[1G")
	err = state.printPrompt()
	if err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
		os.Exit(1)
	}

	state.index = 0

	// Put terminal back into raw mode
	_, err = term.MakeRaw(state.stdInFd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error setting terminal to raw mode: %s\n", err)
		os.Exit(1)
		return
	}
}

func (state *TermState) toCooked() {
	term.Restore(state.stdInFd, &state.oldState)
}

func (state *TermState) printPrompt() error {
	// Get out of raw mode
	state.toCooked()

	fmt.Fprintf(os.Stdout, "\033[35m")
	// Print PWD
	cwd, err := os.Getwd()

	// Print out escape sequence for Windows Terminal/others.
	fmt.Fprintf(os.Stdout, "\033]9;9;%s\033\\", cwd)

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

	_, err = term.MakeRaw(state.stdInFd)
	if err != nil {
		return fmt.Errorf("Error setting terminal to raw mode: %s", err)
	}

	_, col, err := state.getCurrentPos()
	if err != nil {
		return fmt.Errorf("Error getting cursor position: %s", err)
	}

	state.promptLength =  col - 1
	return nil
}

// Returns the current cursor position as (row, col)
// Row and col are 1-based.
// Extra bytes are returned in case the response contains more than just the cursor position escape codes.
// Returns row, col, err, extraBytes
func (state *TermState) getCurrentPos() (int, int, error) {

	// // var f *os.File
	// if runtime.GOOS == "windows" {
		// local_app_data, ok := os.LookupEnv("LOCALAPPDATA")
		// if !ok {
			// return 0, 0, fmt.Errorf("Error getting LOCALAPPDATA environment variable")
		// }

		// // Make dir LOCALAPPDATA/mshell if it doesn't exist
		// err := os.MkdirAll(local_app_data + "/mshell", 0755)
		// if err != nil {
			// fmt.Fprintf(os.Stderr, "Error creating directory %s/mshell: %s\n", local_app_data, err)
			// os.Exit(1)
			// return 0, 0, err
		// }

		// // Open file for writing
		// f, err := os.OpenFile(local_app_data + "/mshell/mshell.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		// if err != nil {
			// fmt.Fprintf(os.Stderr, "Error opening file %s/mshell/mshell.log: %s\n", local_app_data, err)
			// os.Exit(1)
			// return 0, 0, err
		// }
		// defer f.Close()
	// } else {
		// // Open file for writing
		// f, err := os.OpenFile("/tmp/mshell.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		// if err != nil {
			// fmt.Fprintf(os.Stderr, "Error opening file /tmp/mshell.log: %s\n", err)
			// os.Exit(1)
			// return 0, 0, err
		// }
		// defer f.Close()
	// }

	fmt.Fprintf(os.Stdout, "\033[6n")

	for {
		// TODO: This needs to handle case where terminal doesn't respond.
		token, err := state.InteractiveLexer(state.stdInState) // token = <- tokenChan
		if err != nil {
			return 0, 0, err
		}

		switch t := token.(type) {
		case CsiToken:
			if t.FinalChar == 'R' {
				parsedStr := string(t.Params)
				// Split on semicolon or colon
				parts := strings.Split(parsedStr, ";")
				if len(parts) != 2 {
					return 0, 0, fmt.Errorf("Invalid response for cursor position")
				}
				// Parse row
				row, err := strconv.Atoi(parts[0])
				if err != nil {
					return 0, 0, fmt.Errorf("Invalid response for cursor position")
				}
				// Parse column
				col, err := strconv.Atoi(parts[1])
				if err != nil {
					return 0, 0, fmt.Errorf("Invalid response for cursor position")
				}

				return row, col, nil
			}
		default:
			fmt.Fprintf(state.f, "Got other token: %v\n", t)
			// Ignore getting a token that ends the program for now.
			_, err = state.HandleToken(t)
			if err != nil {
				return 0, 0, err
			}
		}
	}
}

func stdLibDefinitions(stack MShellStack, context ExecuteContext, state EvalState) ([]MShellDefinition, error) {
	// Check for environment variable MSHSTDLIB and load that file. Read as UTF-8
	stdlibPath, stdlibSet := os.LookupEnv("MSHSTDLIB")
	definitions := make([]MShellDefinition, 0)

	if stdlibSet {
		// Split the path by :, except for Windows, where it's split by ;
		// If there are multiple paths, load each one.
		var rcPaths []string
		if runtime.GOOS == "windows" {
			rcPaths = strings.Split(stdlibPath, ";")
		} else {
			rcPaths = strings.Split(stdlibPath, ":")
		}

		for _, rcPath := range rcPaths {
			stdlibBytes, err := os.ReadFile(rcPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading file %s: %s\n", rcPath, err)
				return nil, err
			}

			stdlibLexer := NewLexer(string(stdlibBytes))
			stdlibParser := MShellParser{lexer: stdlibLexer}
			stdlibParser.NextToken()
			stdlibFile, err := stdlibParser.ParseFile()

			definitions = append(definitions, stdlibFile.Definitions...)

			if err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing file %s: %s\n", rcPath, err)
				return nil, err
			}

			if len(stdlibFile.Items) > 0 {
				callStackItem := CallStackItem{
					MShellParseItem: stdlibFile.Items[0],
					Name:  rcPath,
					CallStackType: CALLSTACKFILE,
				}

				// allDefinitions = append(allDefinitions, stdlibFile.Definitions...)
				result := state.Evaluate(stdlibFile.Items, &stack, context, stdlibFile.Definitions, callStackItem)

				if !result.Success {
					fmt.Fprintf(os.Stderr, "Error evaluating MSHSTDLIB file %s.\n", rcPath)
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

// This function pushes characters to the terminal and to the backing command.
func (state *TermState) PushChars(chars []rune) {
	// Push chars to current command
	ClearToEnd()
	fmt.Fprintf(os.Stdout, "%s", string(chars))
	// Add back what may have been deleted.
	if state.index <= len(state.currentCommand) {
		fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index:]))
		fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index+len(chars))
		state.currentCommand = append(state.currentCommand[:state.index], append(chars, state.currentCommand[state.index:]...)...)
	}
	state.index = state.index + len(chars)
}

func WriteToHistory(command string, directory string, historyFilePath string) error {
	// Each entry is fixed width:
	// 256 bit (32 byte) SHA hash of full directory path where command was run
	// 256 bit (32 byte) SHA hash of command
	// 64 bit (8 byte) timestamp

	// File is ~/.local/share/mshell/.mshell_history or $LOCALAPPDATA/mshell/.mshell_history depending on OS
	// If the file doesn't exist, create it.

	// var path string
	// if runtime.GOOS == "windows" {
		// localAppData, ok := os.LookupEnv("LOCALAPPDATA")
		// if !ok {
			// fmt.Fprintf(os.Stderr, "Error getting LOCALAPPDATA environment variable\n")
			// os.Exit(1)
		// }
		// path = localAppData + "/mshell/.mshell_history"
	// } else {
		// home, ok := os.LookupEnv("HOME")
		// if !ok {
			// fmt.Fprintf(os.Stderr, "Error getting HOME environment variable\n")
			// os.Exit(1)
		// }

		// path = home + "/.local/share/mshell/.mshell_history"
	// }

	// Check if the directory exists, if not, create it.
	dir := filepath.Dir(historyFilePath)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return fmt.Errorf("Error creating directory %s: %s\n", dir, err)
	}

	// Open file for appending
	file, err := os.OpenFile(historyFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("Error opening file %s: %s\n", historyFilePath, err)
	}
	defer file.Close()

	// Get SHA hash of directory
	dirHash := sha256.Sum256([]byte(directory))

	// Get SHA hash of command
	commandHash := sha256.Sum256([]byte(command))

	// Get current timestamp
	timestamp := time.Now().Unix()

	var recordSlice [72]byte

	// Add directory hash
	copy(recordSlice[0:32], dirHash[:])
	copy(recordSlice[32:64], commandHash[:])
	binary.BigEndian.PutUint64(recordSlice[64:72], uint64(timestamp))

	// Write to file, atomically with entire record
	_, err = file.Write(recordSlice[:])
	if err != nil {
		return fmt.Errorf("Error writing to file %s: %s\n", historyFilePath, err)
	}
	return nil
}

func (state *TermState) HandleToken(token TerminalToken) (bool, error) {
	var err error

	switch t := token.(type) {
	case AsciiToken:
		// If the character is a printable ASCII character, handle it.
		if t.Char > 32 && t.Char < 127 {

			if t.Char == ';' {
				// Check next token, if it's a 'r', open REPOs with lf
				// TODO: Handle EOF token case
				token, err = state.InteractiveLexer(state.stdInState)
				if err != nil {
					return false, err
				}

				if t, ok := token.(AsciiToken); ok {
					if t.Char == 'r' {
						// Open REPOs with lf
						// fmt.Fprintf(state.f, "Opening REPOs with lf...\n")
						state.clearToPrompt()
						state.currentCommand = state.currentCommand[:0]
						state.PushChars([]rune{'r'})
						state.ExecuteCurrentCommand()
					} else if t.Char == 'j' {
						state.clearToPrompt()
						state.currentCommand = state.currentCommand[:0]
						state.PushChars([]rune{'j'})
						state.ExecuteCurrentCommand()
					} else {
						// Push both tokens
						state.PushChars([]rune{';'})
						state.HandleToken(token)
					}
				} else {
					// Push just the semicolon
					state.PushChars([]rune{';'})
					return state.HandleToken(token)
				}
			} else if t.Char == 'j' {
				token, err = state.InteractiveLexer(state.stdInState)
				if err != nil {
					return false, err
				}

				if t, ok := token.(AsciiToken); ok {
					if t.Char == 'f' {
						return state.HandleToken(AsciiToken{Char: 13})
					} else {
						// Push both tokens
						state.PushChars([]rune{'j'})
						return state.HandleToken(token)
					}
				} else {
					// Push just the semicolon
					state.PushChars([]rune{'j'})
					return state.HandleToken(token)
				}
			} else if t.Char == 'v' {
				// Check if next token is 'l', then clear screen
				token, err = state.InteractiveLexer(state.stdInState)
				if err != nil {
					return false, err
				}

				if t, ok := token.(AsciiToken); ok {
					if t.Char == 'l' {
						// Clear screen
						state.ClearScreen()
					} else {
						// Push both tokens
						state.PushChars([]rune{'v'})
						return state.HandleToken(token)
					}
				} else {
					// Push just the 'v'
					state.PushChars([]rune{'v'})
					return state.HandleToken(token)
				}
			} else if t.Char == 'q' {
				// Check if next token is 'l', then clear screen
				token, err = state.InteractiveLexer(state.stdInState)
				if err != nil {
					return false, err
				}

				if t, ok := token.(AsciiToken); ok {
					if t.Char == 'q' {
						state.clearToPrompt()
						state.currentCommand = state.currentCommand[:0]
						state.PushChars([]rune("0 exit"))
						state.ExecuteCurrentCommand()
					} else {
						// Push both tokens
						state.PushChars([]rune{'q'})
						return state.HandleToken(token)
					}
				} else {
					// Push just the 'q'
					state.PushChars([]rune{'q'})
					return state.HandleToken(token)
				}
			} else {
				state.PushChars([]rune{rune(t.Char)})
			}


			// // Add chars to current command at current index
			// // fmt.Fprintf(state.f, "AsciiToken: %d\n", t.Char)
			// fmt.Fprintf(os.Stdout, "\033[K")
			// fmt.Fprintf(os.Stdout, "%c", t.Char)
			// fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index:]))
			// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index+1)

			// state.currentCommand = append(state.currentCommand[:state.index], append([]rune{rune(t.Char)}, state.currentCommand[state.index:]...)...)
			// state.index++
		} else if t.Char == 32 {
			// Space
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
				state.PushChars([]rune{rune(t.Char)})
			}
		} else if t.Char == 1 { // Ctrl-A
			// Move cursor to beginning of line.
			fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1)
			state.index = 0
		} else if t.Char == 2 { // CTRL-B
			// Move cursor left
			if state.index > 0 {
				state.index--
				fmt.Fprintf(os.Stdout, "\033[D")
			}
		} else if t.Char == 3 || t.Char == 4 {
			// Ctrl-C or Ctrl-D
			fmt.Fprintf(os.Stdout, "\r\n") // Print a nice clean newline.
			return true, nil
		} else if t.Char == 5 { // Ctrl-E
			// Move cursor to end of line
			fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + len(state.currentCommand))
			state.index = len(state.currentCommand)
		} else if t.Char == 6 { // Ctrl-F
			// Move cursor right
			if state.index < len(state.currentCommand) {
				state.index++
				fmt.Fprintf(os.Stdout, "\033[C")
			}
		} else if t.Char == 8 { // Backspace (or more typically CTRL-Backspace)
			// Do same as CTRL-W
			// Erase last word
			if state.index > 0 {
				origIndex := state.index
				// First consume all whitespace
				for state.index > 0 && state.currentCommand[state.index-1] == ' ' {
					state.index--
				}

				// Then consume all non-whitespace
				for state.index > 0 && state.currentCommand[state.index-1] != ' ' {
					state.index--
				}

				state.currentCommand = append(state.currentCommand[:state.index], state.currentCommand[origIndex:]...)

				// Erase the word
				fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + state.index)
				fmt.Fprintf(os.Stdout, "\033[K")

				// Print the rest of the command
				fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index:]))
				fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + state.index)
			}
		} else if t.Char == 9 { // Tab
			// Get all files in the current directory
			files, err := os.ReadDir(".")
			if err != nil {
				fmt.Fprintf(os.Stdout, "\a")
			}

			var prefix string
			state.l.allowUnterminatedString = true

			state.l.resetInput(string(state.currentCommand[0:state.index]))
			tokens := state.l.Tokenize()

			lastTokenLength := 0
			if len(tokens) == 1 { // 1 token = EOF
				prefix = ""
			} else {
				lastToken := tokens[len(tokens)-2]
				fmt.Fprintf(state.f, "Last token: %s\n", lastToken)

				zeroBasedStartOfToken := lastToken.Column - 1

				if state.index > zeroBasedStartOfToken + len(lastToken.Lexeme) {
					prefix = ""
				} else {

					lastTokenLength = len(lastToken.Lexeme)

					if lastToken.Type == UNFINISHEDSTRING || lastToken.Type == UNFINISHEDSINGLEQUOTESTRING {
						prefix = string(state.currentCommand[zeroBasedStartOfToken + 1:state.index])
					} else {
						prefix = string(state.currentCommand[zeroBasedStartOfToken:state.index])
					}
				}
			}

			fmt.Fprintf(state.f, "Prefix: %s\n", prefix)

			// Find all files that start with prefix
			var matches []string
			for _, file := range files {
				if strings.HasPrefix(file.Name(), prefix) {
					matches = append(matches, file.Name())
				}
			}

			var insertString string
			if len(matches) == 0 {
				fmt.Fprintf(os.Stdout, "\a")
			} else if len(matches) == 1 {
				// Lex the match and check if we have to quote around it
				state.l.resetInput(matches[0])
				tokens := state.l.Tokenize()
				if len(tokens) > 2 {
					// We have to quote around it
					insertString = "'" + matches[0] + "'"
				} else {
					insertString = matches[0]
				}

				// Replace the prefex
				state.replaceText(insertString, state.index - lastTokenLength, state.index)
			} else {
				// Print out the longest common prefix
				longestCommonPrefix := longestCommonPrefix(matches)

				if len(longestCommonPrefix) == len(prefix) {
					// Print bell
					fmt.Fprintf(os.Stdout, "\a")
				} else {

					state.l.resetInput(longestCommonPrefix)
					tokens := state.l.Tokenize()
					if len(tokens) > 2 {
						// We have to put start quote around it, but don't put end quote, wait for more input
						longestCommonPrefix = "'" + longestCommonPrefix
					}

					// Replace the prefix
					state.replaceText(longestCommonPrefix, state.index - lastTokenLength, state.index)
				}
			}

			state.l.allowUnterminatedString = false
		} else if t.Char == 11 { // Ctrl-K
			// Erase to end of line
			fmt.Fprintf(os.Stdout, "\033[K")
			state.currentCommand = state.currentCommand[:state.index]
		} else if t.Char == 12 { // Ctrl-L
			state.ClearScreen()
		} else if t.Char == 13 { // Enter
			// Add command to history
			state.ExecuteCurrentCommand()
		} else if t.Char == 21 { // Ctrl-U
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
		} else if t.Char == 23 { // Ctrl-W
			// Erase last word
			if state.index > 0 {
				origIndex := state.index
				// First consume all whitespace
				for state.index > 0 && state.currentCommand[state.index-1] == ' ' {
					state.index--
				}

				// Then consume all non-whitespace
				for state.index > 0 && state.currentCommand[state.index-1] != ' ' {
					state.index--
				}

				state.currentCommand = append(state.currentCommand[:state.index], state.currentCommand[origIndex:]...)

				// Erase the word
				fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + state.index)
				fmt.Fprintf(os.Stdout, "\033[K")

				// Print the rest of the command
				fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index:]))
				fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + state.index)
			}
		} else if t.Char == 127 { // Backspace
			// Erase last char
			if state.index > 0 {
				state.currentCommand = append(state.currentCommand[:state.index-1], state.currentCommand[state.index:]...)
				state.index--

				fmt.Fprintf(os.Stdout, "\033[D")
				fmt.Fprintf(os.Stdout, "\033[K")
				fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index:]))
				fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index)
			}
		}
	case SpecialKey:
		if t == KEY_F1 {
			// Set state.currentCommand to "lf"
			state.currentCommand = []rune{'l', 'f'}
			state.ExecuteCurrentCommand()
		} else if t == KEY_ALT_B {
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
		} else if t == KEY_ALT_F { // Alt-F
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
		} else if t == KEY_ALT_O { // Alt-O
			// Quit
			fmt.Fprintf(os.Stdout, "\r\n")
			return true, nil
		} else if t == KEY_UP {
			// Up arrow
			if state.historyIndex >= 0 && state.historyIndex < len(history) {
				state.historyIndex++
				// Clear back to prompt
				state.clearToPrompt()
				reverseIndex := len(history) - state.historyIndex
				// state.printPrompt()
				fmt.Fprintf(os.Stdout, history[reverseIndex])
				// fmt.Fprintf(os.Stdout, "mshell> %s", history[reverseIndex])
				state.currentCommand = []rune(history[reverseIndex])
				state.index = len(state.currentCommand)
			}
		} else if t == KEY_DOWN {
			// Down arrow
			if state.historyIndex > 0 && state.historyIndex <= len(history) + 1 {
				state.historyIndex--
				state.clearToPrompt()
				if state.historyIndex == 0 {
					// state.printPrompt()
					// fmt.Fprintf(os.Stdout, "mshell> ")
					state.currentCommand = []rune{}
					state.index = 0
				} else {
					reverseIndex := len(history) - state.historyIndex
					// fmt.Fprintf(os.Stdout, "mshell> %s", history[reverseIndex])
					// state.printPrompt()
					fmt.Fprintf(os.Stdout, history[reverseIndex])
					state.currentCommand = []rune(history[reverseIndex])
					state.index = len(state.currentCommand)
				}
			} else if state.historyIndex <= 0 {
				state.historyIndex = 0
			} else if state.historyIndex > len(history) {
				state.historyIndex = len(history)
			}
		} else if t == KEY_RIGHT {
			// Right arrow
			if state.index < len(state.currentCommand) {
				state.index++
				fmt.Fprintf(os.Stdout, "\033[C")
			}
		} else if t == KEY_LEFT {
			// Left arrow
			if state.index > 0 {
				state.index--
				fmt.Fprintf(os.Stdout, "\033[D")
			}
		} else if t == KEY_HOME {
			// Move cursor to beginning of line.
			fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1)
			state.index = 0
		} else if t == KEY_END {
			// Move cursor to end of line
			fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + len(state.currentCommand))
			state.index = len(state.currentCommand)
		} else if t == KEY_DELETE {
			if state.index < len(state.currentCommand) {
				fmt.Fprintf(os.Stdout, "\033[K")
				fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index + 1:]))
				fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index)

				state.currentCommand = append(state.currentCommand[:state.index], state.currentCommand[state.index+1:]...)
			}
		}
	}

	return false, nil
}
