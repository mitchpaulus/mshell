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
	// "sort"
	"strconv"
	// "runtime/debug"
	"path/filepath"
	"crypto/sha256"
	"time"
	"encoding/binary"
	"github.com/cespare/xxhash"
	"unicode/utf8"
	"html"
)

type CliCommand int

const (
	CLILEX CliCommand = iota
	CLIPARSE
	CLITYPECHECK
	CLIEXECUTE
	CLIHTML
)

type TabMatch struct {
	TabMatchType TabMatchType
	Match string
}

func GetMatchTexts(matches []TabMatch) []string {
	matchText := make([]string, len(matches))
	for i, m := range matches {
		matchText[i] = m.Match
	}
	return matchText
}

type TabMatchType int

const (
	TABMATCHFILE TabMatchType = iota
	TABMATCHENVVAR
	TABMATCHVAR
	TABMATCHCMD
)

var tempFiles []string


func getLongestCommonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	} else if len(strs) == 1 {
		return strs[0]
	}

	// sort.Strings(strs)

	// first := strs[0]
	// last := strs[len(strs)-1]
	b := strings.Builder{}

	// Max int
	minLen := int(^uint(0) >> 1)

	if len(strs[0]) == 0 {
		return ""
	}

	first_byte_of_first := strs[0][0]

	for _, str := range strs {
		if len(str) == 0 {
			return ""
		}
		l := len(str)
		if l < minLen {
			minLen = l
		}
		if str[0] != first_byte_of_first {
			return ""
		}
	}

	b.WriteByte(first_byte_of_first)

	for i := 1; i < minLen; i++ {
		first_byte := strs[0][i]
		for _, str := range strs {
			if str[i] != first_byte {
				return b.String()
			}
		}
		b.WriteByte(first_byte)
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
	var inputFile *TokenFile
	inputFile = nil

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
		} else if arg == "--html" {
			command = CLIHTML
		} else if arg == "-h" || arg == "--help" {
			fmt.Println("Usage: mshell [OPTION].. FILE [ARG]..")
			fmt.Println("Usage: mshell [OPTION].. [ARG].. < FILE")
			fmt.Println("Usage: mshell [OPTION].. -c INPUT [ARG]..")
			fmt.Println("Options:")
			fmt.Println("  --html     Print HTML representation of the input")
			fmt.Println("  --lex      Print the tokens of the input")
			fmt.Println("  --parse    Print the parsed Abstract Syntax Tree")
			fmt.Println("  -h, --help Print this help message")
			os.Exit(0)
			return
		} else if arg == "--version" {
			fmt.Fprintf(os.Stdout, "0.6.0\n")
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
	if command == CLIHTML {
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

		html := HtmlFromInput(input)
		fmt.Fprintf(os.Stdout, "%s", html)
		return
	}

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

			tabCompletions0: make([]string, 0, 10),
			tabCompletions1: make([]string, 0, 10),
			currentTabComplete: 0,

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

	l := NewLexer(input, inputFile)

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
			stdlibLexer := NewLexer(string(stdlibBytes), &TokenFile{stdlibPath})
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
	numRows int // Number of rows in the terminal
	numCols int // Number of columns in the terminal
	promptRow int // Row where the prompt ends, 1-based
	promptLength int // Length of
	numPromptLines int // Number of lines the prompt takes up
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

	previousHistory []HistoryItem // Previous history items loaded from file

	historyComplete []rune // Completed history search for current command
	completeHistory bool

	renderBuffer []byte // Buffer for rendering the current command

	tabCompletions0 []string // Tab completions for the current command
	tabCompletions1 []string // Tab completions for the current command
	currentTabComplete int

	stack MShellStack
	context ExecuteContext
	evalState EvalState
	callStack CallStack
	stdLibDefs []MShellDefinition
	initCallStackItem CallStackItem
	// pathBinManager IPathBinManager
}

func (s *TermState) Render() {
	s.renderBuffer = s.renderBuffer[:0] // Clear the buffer
	// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1)
	// state.index = 0
	// ClearToEnd()
	s.renderBuffer = append(s.renderBuffer, fmt.Sprintf("\033[%dG", s.promptLength + 1)...)
	s.renderBuffer = append(s.renderBuffer, "\033[K"...)
	// Print the current command
	for _, r := range s.currentCommand {
		s.renderBuffer = utf8.AppendRune(s.renderBuffer, r)
	}

	// Search for history
	historySearchNew := SearchHistory(string(s.currentCommand), historyToSave)
	s.historyComplete = []rune(historySearchNew)
	numToAdd := len(s.historyComplete) - len(s.currentCommand)
	if numToAdd < 0 {
		historySearch := SearchHistory(string(s.currentCommand), s.previousHistory)
		s.historyComplete = []rune(historySearch)
		numToAdd = len(s.historyComplete) - len(s.currentCommand)
	}

	// Print escape code for light gray
	s.renderBuffer = append(s.renderBuffer, "\033[90m"...)
	for i := 0; i < numToAdd; i++ {
		s.renderBuffer = utf8.AppendRune(s.renderBuffer, s.historyComplete[len(s.currentCommand) + i])
	}
	// Reset color
	s.renderBuffer = append(s.renderBuffer, "\033[0m"...)

	var currentTabCompletion []string
	var previousTabCompletion []string
	if s.currentTabComplete == 0 {
		currentTabCompletion = s.tabCompletions0
		previousTabCompletion = s.tabCompletions1
	} else {
		currentTabCompletion = s.tabCompletions1
		previousTabCompletion = s.tabCompletions0
	}

	// Do current completions, up to 10
	limit := s.numRows - s.promptRow
	if len(currentTabCompletion) > limit {
		linesPossible := max(0, s.promptRow - s.numPromptLines)
		fmt.Fprintf(s.f, "Lines possible: %d\n", linesPossible)
		diff := len(currentTabCompletion) - limit
		s.ScrollDown(min(diff, linesPossible))
		limit = s.numRows - s.promptRow
	}

	// Clean previous tab completions
	for i := 0; i < min(len(previousTabCompletion), limit); i++ {
		// Do \n to move to the next line
		s.renderBuffer = append(s.renderBuffer, "\n"...)
		s.renderBuffer = append(s.renderBuffer, "\033[2K"...)
	}
	// Move back up number of completion lines
	for i := 0; i < min(len(previousTabCompletion), limit); i++ {
		s.renderBuffer = append(s.renderBuffer, "\033[A"...)
	}

	for i := 0; i < min(len(currentTabCompletion), limit); i++ {
		// // Do \r\n to move to the next line
		s.renderBuffer = append(s.renderBuffer, "\r\n"...)
		s.renderBuffer = append(s.renderBuffer, []byte(currentTabCompletion[i])...)
	}

	// Move back up number of completion lines
	for i := 0; i < min(len(currentTabCompletion), limit); i++ {
		s.renderBuffer = append(s.renderBuffer, "\033[A"...)
	}

	// Move cursor to correct position. This often will backtrack because of history completion.
	pos := s.promptLength + 1 + s.index
	s.renderBuffer = append(s.renderBuffer, fmt.Sprintf("\033[%dG", pos)...)

	fmt.Fprintf(s.f, "Term index: %d, command length: %d, num completions: %d, limit: %d\n, prompt row: %d, numRows: %d", s.index, len(s.currentCommand), len(currentTabCompletion), limit, s.promptRow, s.numRows)

	// Push the buffer to stdout
	// fmt.Fprintf(s.f, "Rendering buffer: %s\n", string(s.renderBuffer))
	os.Stdout.Write(s.renderBuffer)

	// Move cursor back to the beginning of the line.
	// s.clearToPrompt()
	// fmt.Fprintf(os.Stdout, "%s", string(s.currentCommand))
}

type HistoryItem struct {
	UnixTimeUtc int64
	Directory string
	Command string
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
	// state.index = 0
	ClearToEnd()
}

func ClearToEnd() {
	fmt.Fprintf(os.Stdout, "\033[K")
}

func (state *TermState) ScrollDown(numLines int) {
	// See https://github.com/microsoft/terminal/issues/17320
	// and https://github.com/microsoft/terminal/issues/11078
	// Some terminals are erasing text in scrollback buffer using the \e[nS escape sequence.

	// Implement using \n's instead.

	// Send off cursor position request
	state.UpdateSize()
	curRow, curCol, err := state.getCurrentPos()
	if err != nil {
		fmt.Fprintf(state.f, "Error getting cursor position: %s\n", err)
		return
	}

	// TODO: Limit to current size of the terminal.

	// rowsToScroll := curRow - state.numPromptLines
	fmt.Fprintf(state.f, "Cur Row: %d, Lines to scroll: %d", curRow, numLines)

	// Move cursor to bottom of terminal, if you have a terminal that has over 10000 lines, I'm sorry.
	fmt.Fprintf(os.Stdout, "\033[10000B")
	// print out rowsToScroll newlines
	for i := 0; i < numLines; i++ {
		fmt.Fprintf(os.Stdout, "\n")
	}

	// Move cursor
	fmt.Fprintf(os.Stdout, "\033[%d;%dH", curRow - numLines, curCol)
	state.promptRow = state.promptRow - numLines
}



func (state *TermState) ClearScreen() {
	// See https://github.com/microsoft/terminal/issues/17320
	// and https://github.com/microsoft/terminal/issues/11078
	// Some terminals are erasing text in scrollback buffer using the \e[nS escape sequence.

	// Implement using \n's instead.

	// Send off cursor position request
	state.UpdateSize()
	curRow, _, err := state.getCurrentPos()
	if err != nil {
		fmt.Fprintf(state.f, "Error getting cursor position: %s\n", err)
		return
	}

	rowsToScroll := curRow - state.numPromptLines
	state.ScrollDown(rowsToScroll)
	fmt.Fprintf(state.f, "Cleared screen, scrolled %d rows\n", rowsToScroll)
	// fmt.Fprintf(state.f, "%d %d %d\n", curRow, state.numPromptLines, rowsToScroll)

	// // Move cursor to bottom of terminal, if you have a terminal that has over 10000 lines, I'm sorry.
	// fmt.Fprintf(os.Stdout, "\033[10000B")
	// // print out rowsToScroll newlines
	// for i := 0; i < rowsToScroll; i++ {
		// fmt.Fprintf(os.Stdout, "\n")
	// }

	// // Move cursor
	// fmt.Fprintf(os.Stdout, "\033[%d;%dH", state.numPromptLines, curCol)
	// state.promptRow = state.numPromptLines
}

var tokenBuf []Token
var tokenBufBuilder strings.Builder
var aliases map[string]string
var history []string

var historyToSave []HistoryItem

var knownCommands = map[string]struct{}{ "cd": {} }

// // printText prints the text at the current cursor position, moving existing text to the right.
// func (state *TermState) printText(text string) {
	// fmt.Fprintf(os.Stdout, "\033[K") // Delete to end of line
	// fmt.Fprintf(os.Stdout, "%s", text)
	// fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index:]))
	// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + state.index + len(text))

	// state.currentCommand = append(state.currentCommand[:state.index], append([]rune(text), state.currentCommand[state.index:]...)...)
	// state.index = state.index + len(text)
// }

func (state *TermState) replaceText(newText string, replaceStart int, replaceEnd int) {
	// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + replaceStart)
	// fmt.Fprintf(os.Stdout, "\033[K") // Delete to end of line
	// fmt.Fprintf(os.Stdout, "%s", newText)
	// fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[replaceEnd:]))
	// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + replaceStart + len(newText))

	state.currentCommand = append(state.currentCommand[:replaceStart], append([]rune(newText), state.currentCommand[replaceEnd:]...)...)
	state.index = replaceStart + len(newText)
}


type TerminalToken interface {
	String() string
}

type AsciiToken struct {
	Char byte
}

type MutliByteToken struct {
	Char rune
}

func (t MutliByteToken) String() string {
	return fmt.Sprintf("MultiByteToken: %d %s", t.Char, string(t.Char))
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
				} else if c == 65 { // Up arrow
					return KEY_UP, nil
				} else if c == 66 { // Down arrow
					return KEY_DOWN, nil
				} else if c == 67 { // Right arrow
					return KEY_RIGHT, nil
				} else if c == 68 { // Left arrow
					return KEY_LEFT, nil
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
		} else if c >= 192 && c <= 223 { // 192-223 are the first byte of a 2-byte UTF-8 character{
			// Read the next byte
			var b2 byte
			b2, err = stdinReaderState.ReadByte()
			if err != nil {
				if err == io.EOF {
					return EofTerminalToken{}, nil
				} else {
					return nil, fmt.Errorf("Error reading from stdin: %s", err)
				}
			}

			if b2 >= 128 && b2 <= 191 { // 128-191 are the second byte of a 2-byte UTF-8 character
				fmt.Fprintf(state.f, "Got 2-byte UTF-8 character: %d %d\n", c, b2)
				// Return the 2-byte UTF-8 character as a single token
				// Convert to rune
				r := rune((int32(b2) & 0x3F) | ((int32(c) & 0x1F) << 6))
				return MutliByteToken{Char: r}, nil
			}
		} else if c >= 224 && c <= 239 { // 224-239 are the first byte of a 3-byte UTF-8 character
			// Read the next two bytes
			var b2, b3 byte
			b2, err = stdinReaderState.ReadByte()
			if err != nil {
				if err == io.EOF {
					return EofTerminalToken{}, nil
				} else {
					return nil, fmt.Errorf("Error reading from stdin: %s", err)
				}
			}
			if b2 >= 128 && b2 <= 191 { // 128-191 are the second byte of a 2-byte UTF-8 character
				b3, err = stdinReaderState.ReadByte()
				if err != nil {
					if err == io.EOF {
						return EofTerminalToken{}, nil
					} else {
						return nil, fmt.Errorf("Error reading from stdin: %s", err)
					}
				}

				if b3 >= 128 && b3 <= 191 { // 128-191 are the third byte of a 3-byte UTF-8 character
					fmt.Fprintf(state.f, "Got 3-byte UTF-8 character: %d %d %d (%x %x %x)\n", c, b2, b3, c, b2, b3)
					// Return the 3-byte UTF-8 character as a single token
					// Convert to rune
					r := rune((int32(c & 0x0F)<<12) | (int32(b2&0x3F)<<6) | int32(b3&0x3F))
					return MutliByteToken{Char: r}, nil
				} else {
					fmt.Fprintf(state.f, "Unknown second byte for 3-byte UTF-8 character: %d\n", b2)
					// return AsciiToken{Char: c}
					return UnknownToken{}, nil
				}
			}
		} else if c >= 240 && c <= 247 { // 240-247 are the first byte of a 4-byte UTF-8 character
			// Read the next three bytes
			var b2, b3, b4 byte
			b2, err = stdinReaderState.ReadByte()
			if err != nil {
				if err == io.EOF {
					return EofTerminalToken{}, nil
				} else {
					return nil, fmt.Errorf("Error reading from stdin: %s", err)
				}
			}
			if b2 >= 128 && b2 <= 191 { // 128-191 are the second byte of a 2-byte UTF-8 character
				b3, err = stdinReaderState.ReadByte()
				if err != nil {
					if err == io.EOF {
						return EofTerminalToken{}, nil
					} else {
						return nil, fmt.Errorf("Error reading from stdin: %s", err)
					}
				}

				if b3 >= 128 && b3 <= 191 { // 128-191 are the third byte of a 3-byte UTF-8 character
					b4, err = stdinReaderState.ReadByte()
					if err != nil {
						if err == io.EOF {
							return EofTerminalToken{}, nil
						} else {
							return nil, fmt.Errorf("Error reading from stdin: %s", err)
						}
					}

					if b4 >= 128 && b4 <= 191 { // 128-191 are the fourth byte of a 4-byte UTF-8 character
						fmt.Fprintf(state.f, "Got 4-byte UTF-8 character: %d %d %d %d\n", c, b2, b3, b4)
						// Return the 4-byte UTF-8 character as a single token
						// Convert to rune
						r := rune((int32(c & 0x07) << 18) | (int32(b2 & 0x3F) << 12) | (int32(b3 & 0x3F) << 6) | int32(b4 & 0x3F))
						return MutliByteToken{Char: r}, nil
					} else {
						fmt.Fprintf(state.f, "Unknown third byte for 4-byte UTF-8 character: %d\n", b3)
						// return AsciiToken{Char: c}
						return UnknownToken{}, nil
					}
				} else {
					fmt.Fprintf(state.f, "Unknown second byte for 3-byte UTF-8 character: %d\n", b2)
					// return AsciiToken{Char: c}
					return UnknownToken{}, nil
				}
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
		"u": "'..' cd",
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

	state.l = NewLexer("", nil)
	state.p = &MShellParser{lexer: state.l}

	stdLibDefs, err := stdLibDefinitions(state.stack, state.context, state.evalState)
	if err != nil {
		return fmt.Errorf("Error loading standard library: %s\n", err)
	}

	state.stdLibDefs = stdLibDefs

	history = make([]string, 0)
	state.historyIndex = 0

	// Fill history
	historyDir, err := GetHistoryDir()
	if err == nil {
		state.previousHistory, err = ReadHistory(historyDir)
		if err == nil {
			for _, item := range state.previousHistory {
				// Add to history
				history = append(history, item.Command)
			}
		} else {
			fmt.Fprintf(state.f, "Error reading history file %s: %s\n", filepath.Join(historyDir, "msh_history"), err)
		}
		fmt.Fprintf(state.f, "%d items loaded from history file %s\n", len(state.previousHistory), filepath.Join(historyDir, "msh_history"))
	} else {
		fmt.Fprintf(state.f, "Error getting history directory: %s\n", err)
	}

	err = state.printPrompt()
	if err != nil {
		return fmt.Errorf("Error printing prompt: %s\n", err)
	}

	defer state.TrySaveHistory()

	var token TerminalToken
	var end bool

	for {
		if (state.currentTabComplete == 0) {
			state.tabCompletions0 = state.tabCompletions0[:0]
		} else {
			state.tabCompletions1 = state.tabCompletions1[:0]
		}

		fmt.Fprintf(state.f, "Waiting for token... ")
		state.f.Sync()
		token, err = state.InteractiveLexer(stdInState) // token = <- tokenChan
		if err != nil {
			fmt.Fprintf(state.f, "Got err from interactive lexer: %s\n", err)
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
		state.Render()

		// Swap tab completions
		state.currentTabComplete = 1 - state.currentTabComplete
	}

	return nil
}

func (state *TermState) TrySaveHistory() {
	if len(historyToSave) == 0 {
		fmt.Fprintf(state.f, "No history to save.\n")
		return
	}

	historyDir, err := GetHistoryDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting history directory: %s\n", err)
		return
	}

	// We are going to save 3 files.
	// File 1: Main history made up of records of
	//   1. 8 byte unix timestamp in UTC
	//   2. 8 byte xxHash of the command
	//   3. 8 byte xxHash of the directory
	// File 2: Unique Commands, only escape would be '\n'
	// File 3: Unique Directories, only escape would be '\n'
	// We leave it to the user to clean up duplicates in the commands and directories files.
	historyFile := filepath.Join(historyDir, "msh_history")
	commandFile := filepath.Join(historyDir, "msh_commands")
	directoryFile := filepath.Join(historyDir, "msh_dirs")

	// Open history file for appending
	historyF, err := os.OpenFile(historyFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening history file %s: %s\n", historyFile, err)
		return
	}
	defer historyF.Close()

	// Open command file for appending
	commandF, err := os.OpenFile(commandFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening command file %s: %s\n", commandFile, err)
		return
	}
	defer commandF.Close()

	// Open directory file for appending
	directoryF, err := os.OpenFile(directoryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening directory file %s: %s\n", directoryFile, err)
		return
	}
	defer directoryF.Close()

	for _, item := range historyToSave {
		// Hash command
		commandHash := xxhash.Sum64String(item.Command)
		directoryHash := xxhash.Sum64String(item.Directory)

		// Write to history file, directly as binary.
		bytes := make([]byte, 8 + 8 + 8)
		binary.BigEndian.PutUint64(bytes[0:8], uint64(item.UnixTimeUtc))
		binary.BigEndian.PutUint64(bytes[8:16], commandHash)
		binary.BigEndian.PutUint64(bytes[16:24], directoryHash)

		_, err = historyF.Write(bytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing to history file %s: %s\n", historyFile, err)
			return
		}

		// Write command to command file, escape any newlines.
		_, err = commandF.WriteString(strings.ReplaceAll(item.Command, "\n", "\\n") + "\n")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing to command file %s: %s\n", commandFile, err)
			return
		}

		// Write directory to directory file, escape any newlines.
		_, err = directoryF.WriteString(strings.ReplaceAll(item.Directory, "\n", "\\n") + "\n")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing to directory file %s: %s\n", directoryFile, err)
			return
		}
	}

	fmt.Fprintf(state.f, "Saved %d history items to %s\n", len(historyToSave), historyFile)

	// Clear history to save
	historyToSave = historyToSave[:0]
	return
}

func (state *TermState) ExecuteCurrentCommand() (bool, int) {
	// Add command to history
	currentCommandStr := strings.TrimSpace(string(state.currentCommand), )

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

		currentDir, err := os.Getwd()
		if err == nil {
			historyToSave = append(historyToSave, HistoryItem{
				UnixTimeUtc: time.Now().Unix(),
				Directory: currentDir,
				Command: currentCommandStr,
			})
		}
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
		_, isInPath := state.context.Pbm.Lookup(literalStr);
		if isInPath {
			firstTokenIsCmd = true
		} else {
			// This is a secondary check to capture things like 'cd'.
			_, firstTokenIsCmd = knownCommands[literalStr]
		}

		hasPipe := false
		if ((strings.Contains(literalStr, string(os.PathSeparator)) && state.context.Pbm.IsExecutableFile(literalStr)) || firstTokenIsCmd) && !IsDefinitionDefined(literalStr, state.stdLibDefs) {
			tokenBufBuilder.Reset()
			// Clear token buffer
			tokenBuf = tokenBuf[:0]
			tokenBuf = append(tokenBuf, p.curr)
			// Consume all tokens up until EOF or PIPE
			for p.NextToken(); p.curr.Type != EOF; p.NextToken() {
				if p.curr.Type == PIPE {
					hasPipe = true
				}
				tokenBuf = append(tokenBuf, p.curr)
			}

			tokenBufBuilder.WriteString("[")
			if hasPipe {
				// If we have a PIPE, we need to split the command into multiple commands.
				tokenBufBuilder.WriteString("[")
				tokenBufBuilder.WriteString("'" + literalStr + "'")
				for _, t := range tokenBuf[1:] {
					if t.Type == PIPE {
						tokenBufBuilder.WriteString("] [")
					} else {
						tokenBufBuilder.WriteString(" ")
						tokenBufBuilder.WriteString(t.Lexeme)
					}
				}
				tokenBufBuilder.WriteString("]")
			} else {
				tokenBufBuilder.WriteString("'" + literalStr + "'")
				for _, t := range tokenBuf[1:] {
					tokenBufBuilder.WriteString(" ")
					tokenBufBuilder.WriteString(t.Lexeme)
				}
			}
			tokenBufBuilder.WriteString("]")

			if hasPipe {
				tokenBufBuilder.WriteString("|;")
			} else {
				tokenBufBuilder.WriteString(";")
			}

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
			fmt.Fprint(os.Stderr, err.Error())
			return true, 1
		}

		state.index = 0
		return false, 0
	}

	// During evaluation, normal terminal output can happen, or TUI apps can be run.
	// So want them to see non-raw mode terminal state.
	term.Restore(state.stdInFd, &state.oldState)
	fmt.Fprintf(os.Stdout, "\n")

	if len(parsed.Items) > 0 {
		state.initCallStackItem.MShellParseItem = parsed.Items[0]
		result := state.evalState.Evaluate(parsed.Items, &state.stack, state.context, state.stdLibDefs, state.initCallStackItem)

		if result.ExitCalled {
			return true, result.ExitCode
		}

		if !result.Success {
			fmt.Fprintf(os.Stderr, "Error evaluating input.\n")
		}
	}

	fmt.Fprintf(os.Stdout, "\033[1G")
	err = state.printPrompt()
	if err != nil {
		fmt.Fprint(os.Stderr, err.Error())
		return true, 1
	}

	state.index = 0

	// Put terminal back into raw mode
	_, err = term.MakeRaw(state.stdInFd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error setting terminal to raw mode: %s\n", err)
		return true, 1
	}

	return false, 0
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
	// Check if we are in windows terminal by looking for WT_SESSION env variable.
	if wtSession, ok := os.LookupEnv("WT_SESSION"); ok && len(wtSession) > 0 {
		fmt.Fprintf(os.Stdout, "\033]9;9;%s\033\\", cwd)
	} else {
		// Print using OSC 7
		hostname, err := os.Hostname()
		if err != nil {
			fmt.Fprintf(os.Stdout, "\033]7;file://%s%s\033\\", hostname, cwd)
		}
	}

	if len(state.homeDir) > 0 && strings.HasPrefix(cwd, state.homeDir) {
		cwd = "~" + cwd[len(state.homeDir):]
	}

	var promptText string
	if err != nil {
		promptText = "??? >"
	} else {
		promptText = fmt.Sprintf("%s (%d)> \n:: ", cwd, len(state.stack))
	}

	fmt.Fprint(os.Stdout, promptText)
	state.numPromptLines = strings.Count(promptText, "\n") + 1
	fmt.Fprintf(os.Stdout, "\033[0m")

	// fmt.Fprintf(os.Stdout, "mshell> ")

	_, err = term.MakeRaw(state.stdInFd)
	if err != nil {
		return fmt.Errorf("Error setting terminal to raw mode: %s", err)
	}

	var col int
	state.promptRow, col, err = state.getCurrentPos()
	if err != nil {
		return fmt.Errorf("Error getting cursor position: %s", err)
	}

	state.UpdateSize()

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

			stdlibLexer := NewLexer(string(stdlibBytes), &TokenFile{rcPath})
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
	// Push at the correct index
	state.currentCommand = append(state.currentCommand[:state.index], append(chars, state.currentCommand[state.index:]...)...)
	state.index += len(chars)

	// // Push chars to current command
	// ClearToEnd()
	// fmt.Fprintf(os.Stdout, "%s", string(chars))
	// // Add back what may have been deleted.
	// if state.index <= len(state.currentCommand) {
		// fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index:]))
		// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index+len(chars))
		// state.currentCommand = append(state.currentCommand[:state.index], append(chars, state.currentCommand[state.index:]...)...)
	// }
	// state.index = state.index + len(chars)
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

// Returns boolean on whether to end the CLI. Think CTRL-c/d or other exit command.
func (state *TermState) HandleToken(token TerminalToken) (bool, error) {
	var err error

	switch t := token.(type) {
	case MutliByteToken:
		state.PushChars([]rune{t.Char})
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
						shouldExit, _ := state.ExecuteCurrentCommand()
						return shouldExit, nil
					} else if t.Char == 'j' {
						state.clearToPrompt()
						state.currentCommand = state.currentCommand[:0]
						state.PushChars([]rune{'j'})
						shouldExit, _ := state.ExecuteCurrentCommand()
						return shouldExit, nil
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
						shouldExit, _ := state.ExecuteCurrentCommand()
						return shouldExit, nil
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
				// state.currentCommand = append(state.currentCommand, rune(t.Char))
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
				// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+i+1)
				// fmt.Fprintf(os.Stdout, "\033[K")

				// Print alias value
				// fmt.Fprint(os.Stdout, aliasValue)

				// Print the space
				// fmt.Fprintf(os.Stdout, " ")

				// Print the rest of the command
				// fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index:]))

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
				// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index)
			} else {
				state.PushChars([]rune{rune(t.Char)})
			}
		} else if t.Char == 1 { // Ctrl-A
			// Move cursor to beginning of line.
			// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1)
			state.index = 0
		} else if t.Char == 2 { // CTRL-B
			// Move cursor left
			if state.index > 0 {
				state.index--
				// fmt.Fprintf(os.Stdout, "\033[D")
			}
		} else if t.Char == 3 || t.Char == 4 {
			// Ctrl-C or Ctrl-D
			fmt.Fprintf(os.Stdout, "\r\n") // Print a nice clean newline.
			return true, nil
		} else if t.Char == 5 { // Ctrl-E
			// Move cursor to end of line
			// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + len(state.currentCommand))
			state.index = len(state.currentCommand)
		} else if t.Char == 6 { // Ctrl-F
			// Move cursor right
			if state.index < len(state.currentCommand) {
				state.index++
				// fmt.Fprintf(os.Stdout, "\033[C")
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
				// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + state.index)
				// fmt.Fprintf(os.Stdout, "\033[K")

				// // Print the rest of the command
				// fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index:]))
				// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + state.index)
			}
		} else if t.Char == 9 { // Tab complete
			// Get all files in the current directory

			var prefix string
			state.l.allowUnterminatedString = true
			defer func() {
				state.l.allowUnterminatedString = false
			}()

			state.l.resetInput(string(state.currentCommand[0:state.index]))
			tokens := state.l.Tokenize()
			lastTokenLength := 0

			var lastToken Token

			if len(tokens) == 1 { // 1 token = EOF
				prefix = ""
				lastToken = tokens[0]
			} else {
				lastToken = tokens[len(tokens)-2]

				zeroBasedStartOfToken := lastToken.Column - 1

				if state.index > zeroBasedStartOfToken + len(lastToken.Lexeme) {
					prefix = ""
				} else {

					lastTokenLength = len(lastToken.Lexeme)

					if lastToken.Type == UNFINISHEDSTRING || lastToken.Type == UNFINISHEDSINGLEQUOTESTRING || lastToken.Type == UNFINISHEDPATH {
						prefix = string(state.currentCommand[zeroBasedStartOfToken + 1:state.index])
					} else {
						prefix = string(state.currentCommand[zeroBasedStartOfToken:state.index])
					}
				}
			}

			fmt.Fprintf(state.f, "Last token: %s %d\n", lastToken, len(tokens))
			fmt.Fprintf(state.f, "Prefix: %s\n", prefix)

			var matches []TabMatch

			// Environment variable completion
			if len(prefix) > 0 && prefix[0] == '$' {
				vars := os.Environ()
				for _, envVar := range vars {
					// state.f.Write([]byte(fmt.Sprintf("Checking env var: '%s'\n", envVar)))
					if strings.HasPrefix(envVar, prefix[1:]) {
						// Split on '=' and take the first part
						parts := strings.SplitN(envVar, "=", 2)
						if len(parts) > 0 {
							matches = append(matches, TabMatch{ TABMATCHENVVAR,  "$" + parts[0]})
						}
					}
				}
			}

			// Variable completion
			if len(prefix) > 0 && prefix[0] == '@' {
				searchPrefix := prefix[1:]
				for v, _ := range state.context.Variables {
					if strings.HasPrefix(v, searchPrefix) {
						matches = append(matches, TabMatch {TABMATCHVAR,  "@" + v})
					}
				}
			} else {
				// Completion on variables, since you could always end with !
				for v, _ := range state.context.Variables {
					if strings.HasPrefix(v, prefix) {
						matches = append(matches, TabMatch {TABMATCHVAR, v + "!"})
					}
				}
			}


			if len(tokens) == 2 && len(prefix) > 0 {
				// Try to complete on command names
				binMatches := state.context.Pbm.Matches(prefix)
				for _, match := range binMatches {
					matches = append(matches, TabMatch { TABMATCHCMD, match})
				}
			}


			if prefix == "" {
				// Dump everything in the current directory
				cwd, err := os.Getwd()
				if err == nil {
					files, err := os.ReadDir(cwd)
					if err == nil {
						for _, file := range files {
							if file.IsDir() {
								matches = append(matches, TabMatch { TABMATCHFILE, file.Name() + string(os.PathSeparator)})
							} else {
								matches = append(matches, TabMatch { TABMATCHFILE, file.Name()} )
							}
						}
					}
				}
			} else {
				// Split on last path separator
				indexOfLastSeparator := -1
				for i := len(prefix) - 1; i >= 0; i-- {
					if IsPathSeparator(prefix[i]) {
						// Found a path separator, split here
						indexOfLastSeparator = i
						break
					}
				}

				dir := prefix[0:indexOfLastSeparator + 1]
				filename := prefix[indexOfLastSeparator + 1:]

				// cleanPath := filepath.Clean(prefix)
				// dir := filepath.Dir(cleanPath)
				// filename := filepath.Base(prefix)
				// dir := UnmodifiedDir(prefix)

				fmt.Fprintf(state.f, "Dir: '%s', Filename: '%s'\n", dir, filename)

				var searchDir string
				if len(dir) == 0 {
					searchDir = "."
				} else {
					searchDir = dir
				}

				files, err := os.ReadDir(searchDir)
				if err == nil {
					// Find all files that start with prefix
					for _, file := range files {
						if strings.HasPrefix(file.Name(), filename) {
							// Rejoin the directory and filename. If a directory, end with a path separator.
							if file.IsDir() {
								matches = append(matches, TabMatch {TABMATCHFILE, dir + file.Name() + string(os.PathSeparator)})
							} else {
								matches = append(matches, TabMatch {TABMATCHFILE, dir + file.Name()})
							}
						}
					}
				}
			}

			var insertString string
			fmt.Fprintf(state.f, "Len matches: '%d'\n", len(matches))

			if len(matches) < 5 {
				// Print matches
				fmt.Fprintf(state.f, "Matches: %s\n", strings.Join(GetMatchTexts(matches), ", "))
			}

			if len(matches) == 0 {
				fmt.Fprintf(os.Stdout, "\a")
			} else if len(matches) == 1 {
				// Lex the match and check if we have to quote around it
				if lastToken.Type == UNFINISHEDSINGLEQUOTESTRING {
					insertString = "'" + matches[0].Match
				} else if lastToken.Type == UNFINISHEDPATH {
					insertString = "`" + matches[0].Match
				} else {
					state.l.resetInput(matches[0].Match)
					tokens := state.l.Tokenize()
					if len(tokens) > 2 {
						// We have to quote around it
						// TODO: Deal with case when the match itself has a single quote.
						insertString = "'" + matches[0].Match + "'"
					} else {
						insertString = matches[0].Match
					}
				}

				// Replace the prefex
				state.replaceText(insertString, state.index - lastTokenLength, state.index)
			} else {
				// Print out the longest common prefix
				longestCommonPrefix := getLongestCommonPrefix(GetMatchTexts(matches))
				fmt.Fprintf(state.f, "Longest common prefix: '%s'\n", longestCommonPrefix)

				if len(longestCommonPrefix) <= len(prefix) {
					// Print bell
					fmt.Fprintf(os.Stdout, "\a")
				} else {
					if lastToken.Type == UNFINISHEDSINGLEQUOTESTRING {
						longestCommonPrefix = "'" + longestCommonPrefix
					} else if lastToken.Type == UNFINISHEDPATH {
						longestCommonPrefix = "`" + longestCommonPrefix
					} else {
						state.l.resetInput(longestCommonPrefix)
						tokens := state.l.Tokenize()
						if len(tokens) > 2 {
							// We have to put start quote around it, but don't put end quote, wait for more input
							longestCommonPrefix = "'" + longestCommonPrefix
						}
					}

					// Replace the prefix
					state.replaceText(longestCommonPrefix, state.index - lastTokenLength, state.index)
				}

				if state.currentTabComplete == 0 {
					state.tabCompletions0 = append(state.tabCompletions0, GetMatchTexts(matches)...)
				} else {
					state.tabCompletions1 = append(state.tabCompletions1, GetMatchTexts(matches)...)
				}
			}
		} else if t.Char == 11 { // Ctrl-K
			// Erase to end of line
			// fmt.Fprintf(os.Stdout, "\033[K")
			state.currentCommand = state.currentCommand[:state.index]
		} else if t.Char == 12 { // Ctrl-L
			state.ClearScreen()
		} else if t.Char == 13 { // Enter
			// Add command to history
			shouldExit, _ := state.ExecuteCurrentCommand()
			if shouldExit {
				return true, nil
			}
		} else if t.Char == 21 { // Ctrl-U
			// Erase back to prompt start
			// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1)
			// fmt.Fprintf(os.Stdout, "\033[K")
			// fmt.Fprintf(os.Stdout, "\033[2K\033[1G")
			// fmt.Fprintf(os.Stdout, "mshell> ")
			// state.printPrompt()

			// // Remaining chars in current command
			state.currentCommand = state.currentCommand[state.index:]
			// for i := 0; i < len(state.currentCommand); i++ {
				// fmt.Fprintf(os.Stdout, "%c", state.currentCommand[i])
			// }

			// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1)
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

				// // Erase the word
				// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + state.index)
				// fmt.Fprintf(os.Stdout, "\033[K")

				// Print the rest of the command
				// fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index:]))
				// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + state.index)
			}
		} else if t.Char == 25 { // Ctrl-Y
			// Ctrl-y to complete history
			if len(state.historyComplete) >= len(state.currentCommand) {
				fmt.Fprintf(state.f, "History complete: %s\n", string(state.historyComplete))
				if cap(state.currentCommand) < cap(state.historyComplete) {
					state.currentCommand = make([]rune, len(state.historyComplete), cap(state.historyComplete))
				} else {
					state.currentCommand = state.currentCommand[:len(state.historyComplete)]
				}

				copy(state.currentCommand, state.historyComplete)
			 	state.index = len(state.currentCommand)
			}
		} else if t.Char == 127 { // Backspace
			// Erase last char
			if state.index > 0 {
				state.currentCommand = append(state.currentCommand[:state.index-1], state.currentCommand[state.index:]...)
				state.index--

				// fmt.Fprintf(os.Stdout, "\033[D")
				// fmt.Fprintf(os.Stdout, "\033[K")
				// fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index:]))
				// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index)
			}
		}
	case SpecialKey:
		if t == KEY_F1 {
			// Set state.currentCommand to "lf"
			state.currentCommand = []rune{'l', 'f'}
			shouldExit, _ := state.ExecuteCurrentCommand()
			if shouldExit {
				return true, nil
			}
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

				// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index)
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

			// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index)
		} else if t == KEY_ALT_O { // Alt-O
			// Quit
			fmt.Fprintf(os.Stdout, "\r\n")
			fmt.Fprintf(state.f, "Exiting mshell using ALT-o...\n")
			return true, nil
		} else if t == KEY_UP {
			// Up arrow
			for state.historyIndex >= 0 && state.historyIndex < len(history) {
				state.historyIndex++
				reverseIndex := len(history) - state.historyIndex
				// Compare current command to history, if the same, skip and continue to search
				if string(state.currentCommand) == string(history[reverseIndex]) {
					continue
				}

				// Clear back to prompt
				// state.clearToPrompt()
				// state.printPrompt()
				// fmt.Fprint(os.Stdout, history[reverseIndex])
				// fmt.Fprintf(os.Stdout, "mshell> %s", history[reverseIndex])
				state.currentCommand = []rune(history[reverseIndex])
				state.index = len(state.currentCommand)
				break
			}
		} else if t == KEY_DOWN {
			if state.historyIndex <= 0 {
				state.historyIndex = 0
			} else if state.historyIndex > len(history) {
				state.historyIndex = len(history)
			} else {
				// Down arrow
				for state.historyIndex > 0 && state.historyIndex <= len(history) + 1 {
					state.historyIndex--
					// state.clearToPrompt()
					if state.historyIndex == 0 {
						// state.printPrompt()
						// fmt.Fprintf(os.Stdout, "mshell> ")
						state.currentCommand = []rune{}
						state.index = 0
					} else {
						reverseIndex := len(history) - state.historyIndex
						// Compare current command to history, if the same, skip and continue to search
						if string(state.currentCommand) == string(history[reverseIndex]) {
							continue
						}
						// fmt.Fprintf(os.Stdout, "mshell> %s", history[reverseIndex])
						// state.printPrompt()
						// fmt.Fprint(os.Stdout, history[reverseIndex])
						state.currentCommand = []rune(history[reverseIndex])
						state.index = len(state.currentCommand)
					}
					break
				}
			}
		} else if t == KEY_RIGHT {
			// Right arrow
			if state.index < len(state.currentCommand) {
				state.index++
				// fmt.Fprintf(os.Stdout, "\033[C")
			}
		} else if t == KEY_LEFT {
			// Left arrow
			if state.index > 0 {
				state.index--
				// fmt.Fprintf(os.Stdout, "\033[D")
			}
		} else if t == KEY_HOME {
			// Move cursor to beginning of line.
			// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1)
			state.index = 0
		} else if t == KEY_END {
			// Move cursor to end of line
			// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength + 1 + len(state.currentCommand))
			state.index = len(state.currentCommand)
		} else if t == KEY_DELETE {
			if state.index < len(state.currentCommand) {
				// fmt.Fprintf(os.Stdout, "\033[K")
				// fmt.Fprintf(os.Stdout, "%s", string(state.currentCommand[state.index + 1:]))
				// fmt.Fprintf(os.Stdout, "\033[%dG", state.promptLength+1+state.index)

				state.currentCommand = append(state.currentCommand[:state.index], state.currentCommand[state.index+1:]...)
			}
		}
	}

	return false, nil
}

func UnmodifiedDir(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if IsPathSeparator(path[i]) {
			return path[0:i + 1]
		}
	}
	return ""
}

func HtmlFromInput(input string) string {
	l := NewLexer(input, nil)
	l.emitWhitespace = true
	l.emitComments = true

	tokens := l.Tokenize()

	sb := strings.Builder{}
	sb.WriteString("<code>")
	for _, t := range tokens {
		if t.Type == WHITESPACE {
			sb.WriteString(t.Lexeme)
		} else {
			sb.WriteString("<span class=\"mshell")
			sb.WriteString(t.Type.String())
			sb.WriteString("\">")
			sb.WriteString(html.EscapeString(t.Lexeme))
			sb.WriteString("</span>")
		}
	}
	sb.WriteString("</code>")
	return sb.String()
}
