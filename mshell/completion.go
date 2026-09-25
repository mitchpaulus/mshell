package main

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// TabMatchType identifies the source of a completion match.
type TabMatchType int

const (
	TABMATCHFILE TabMatchType = iota
	TABMATCHENVVAR
	TABMATCHVAR
	TABMATCHCMD
	TABMATCHBUILTIN
	TABMATCHDEF
)

// TabMatch represents a single completion match with its type.
type TabMatch struct {
	TabMatchType TabMatchType
	Match        string
}

// GetMatchTexts extracts the match strings from a slice of TabMatches.
func GetMatchTexts(matches []TabMatch) []string {
	matchText := make([]string, len(matches))
	for i, m := range matches {
		matchText[i] = m.Match
	}
	return matchText
}

// getLongestCommonPrefix returns the longest common prefix of a slice of strings.
func getLongestCommonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	} else if len(strs) == 1 {
		return strs[0]
	}

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

// CompletionFS abstracts filesystem operations for tab completion.
type CompletionFS interface {
	// ReadDir returns entries in the given directory path.
	ReadDir(dir string) ([]fs.DirEntry, error)
	// Getwd returns the current working directory.
	Getwd() (string, error)
}

// CompletionEnv abstracts environment variable access.
type CompletionEnv interface {
	// Environ returns all environment variables as "KEY=value" strings.
	Environ() []string
}

// CompletionInput contains the parsed input context for generating completions.
type CompletionInput struct {
	Prefix        string    // The text to complete (may include leading $ or @)
	LastTokenType TokenType // Type of the last token (affects completion behavior)
	PrevTokenType TokenType // Type of the token before the current completion target
	NumTokens     int       // Number of tokens in the input (includes EOF)
	InBinaryMode  bool      // True when current command line is in binary/pipeline mode
}

// CompletionDeps bundles all dependencies needed for generating completions.
type CompletionDeps struct {
	FS          CompletionFS
	Env         CompletionEnv
	Binaries    IPathBinManager
	Variables   map[string]struct{} // Variable names (without @ or !)
	BuiltIns    map[string]struct{} // Built-in command names
	Definitions []string            // Definition names
}

// GenerateCompletions produces completion matches for the given input.
// This is the core pure function that can be unit tested.
func GenerateCompletions(input CompletionInput, deps CompletionDeps) []TabMatch {
	var matches []TabMatch
	prefix := input.Prefix

	// For unfinished strings/paths, only complete files
	if input.LastTokenType == UNFINISHEDPATH ||
		input.LastTokenType == UNFINISHEDSTRING ||
		input.LastTokenType == UNFINISHEDSINGLEQUOTESTRING {
		return generateFileCompletions(input, deps.FS)
	}

	// 1. Environment variable completion ($VAR)
	if len(prefix) > 0 && prefix[0] == '$' {
		searchPrefix := prefix[1:]
		for _, envVar := range deps.Env.Environ() {
			if strings.HasPrefix(envVar, searchPrefix) {
				parts := strings.SplitN(envVar, "=", 2)
				if len(parts) > 0 {
					matches = append(matches, TabMatch{TABMATCHENVVAR, "$" + parts[0]})
				}
			}
		}
	}

	// 2. Binary name completion (first token position or after pipe/list start)
	// NumTokens == 2 means: one token + EOF
	binaryPosition := input.NumTokens == 2 ||
		input.PrevTokenType == PIPE ||
		input.PrevTokenType == LEFT_SQUARE_BRACKET
	if binaryPosition && len(prefix) > 0 && input.LastTokenType == LITERAL {
		binMatches := deps.Binaries.Matches(prefix)
		for _, match := range binMatches {
			matches = append(matches, TabMatch{TABMATCHCMD, match})
		}
	}

	// 3. File path completion
	matches = append(matches, generateFileCompletions(input, deps.FS)...)

	// 4. MShell variable completion (@var)
	if len(prefix) > 0 && prefix[0] == '@' {
		searchPrefix := prefix[1:]
		for v := range deps.Variables {
			if strings.HasPrefix(v, searchPrefix) {
				matches = append(matches, TabMatch{TABMATCHVAR, "@" + v})
			}
		}
	} else if input.LastTokenType == LITERAL && !input.InBinaryMode {
		// Completion on variables with ! suffix
		for v := range deps.Variables {
			if strings.HasPrefix(v, prefix) {
				matches = append(matches, TabMatch{TABMATCHVAR, v + "!"})
			}
		}
	}

	// 5. Built-in command completion
	if !input.InBinaryMode {
		for name := range deps.BuiltIns {
			if strings.HasPrefix(name, prefix) {
				matches = append(matches, TabMatch{TABMATCHBUILTIN, name})
			}
		}
	}

	// 6. Definition completion
	if !input.InBinaryMode {
		for _, defName := range deps.Definitions {
			if strings.HasPrefix(defName, prefix) {
				matches = append(matches, TabMatch{TABMATCHDEF, defName})
			}
		}
	}

	return matches
}

// generateFileCompletions handles file/directory completion.
func generateFileCompletions(input CompletionInput, cfs CompletionFS) []TabMatch {
	var matches []TabMatch
	forEachPathCompletion(cfs, input.Prefix, nil, func(match string) {
		matches = append(matches, TabMatch{TABMATCHFILE, match})
	})
	return matches
}

// forEachPathCompletion calls add with each file and directory the prefix can complete to:
// the entries of the directory named by the prefix up to its last path separator
// whose names start with the rest of the prefix.
// Each is spelled with that directory part so it can replace the prefix,
// and directories end in a path separator.
// keepFile, when not nil, decides which files (not directories) are included.
func forEachPathCompletion(cfs CompletionFS, prefix string, keepFile func(name string) bool, add func(string)) {
	dirEnd := 0
	for i := len(prefix) - 1; i >= 0; i-- {
		if IsPathSeparator(prefix[i]) {
			dirEnd = i + 1
			break
		}
	}
	dir := prefix[:dirEnd]
	namePrefix := prefix[dirEnd:]

	searchDir := dir
	if prefix == "" {
		cwd, err := cfs.Getwd()
		if err != nil {
			return
		}
		searchDir = cwd
	} else if dir == "" {
		searchDir = "."
	}

	entries, err := cfs.ReadDir(searchDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, namePrefix) {
			continue
		}
		if entry.IsDir() {
			add(dir + name + string(os.PathSeparator))
		} else if keepFile == nil || keepFile(name) {
			add(dir + name)
		}
	}
}

// CompletionRequest is what a binary's completion definitions ask Tab to offer.
// A definition returns either a list of values,
// or a dictionary with any of these keys:
//
//	values:   [str]        candidates for the argument
//	files:    str | [str]  files whose names match one of these glob patterns, and all directories
//	dirs:     bool         directories
//	binaries: bool         executables on the path
type CompletionRequest struct {
	Values       []string
	FilePatterns []string
	Dirs         bool
	Binaries     bool
	// Several definitions can list the same value; one definition should not.
	fromSeveralDefinitions bool
}

// addResult merges one definition's result into the request.
// It returns false when the result is not a list of strings or a dictionary of the keys above.
func (r *CompletionRequest) addResult(result MShellObject) bool {
	switch v := result.(type) {
	case *MShellList:
		return r.addValues(v)
	case *MShellDict:
		for key, item := range v.Items {
			switch key {
			case "values":
				list, ok := item.(*MShellList)
				if !ok || !r.addValues(list) {
					return false
				}
			case "files":
				switch files := item.(type) {
				case *MShellList:
					for _, pattern := range files.Items {
						s, err := pattern.CastString()
						if err != nil {
							return false
						}
						r.FilePatterns = append(r.FilePatterns, s)
					}
				default:
					pattern, err := files.CastString()
					if err != nil {
						return false
					}
					r.FilePatterns = append(r.FilePatterns, pattern)
				}
			case "dirs", "binaries":
				b, ok := item.(MShellBool)
				if !ok {
					return false
				}
				if key == "dirs" {
					r.Dirs = r.Dirs || b.Value
				} else {
					r.Binaries = r.Binaries || b.Value
				}
			default:
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (r *CompletionRequest) addValues(list *MShellList) bool {
	for _, item := range list.Items {
		s, err := item.CastString()
		if err != nil {
			return false
		}
		r.Values = append(r.Values, s)
	}
	return true
}

// ToObject returns the request as a completion definition would: the values alone as a list,
// or a dictionary when it asks for files, directories, or executables.
// The values are not filtered by the word being completed; Tab does that.
func (r *CompletionRequest) ToObject() MShellObject {
	values := NewList(len(r.Values))
	for i, value := range r.Values {
		values.Items[i] = MShellString{Content: value}
	}
	if len(r.FilePatterns) == 0 && !r.Dirs && !r.Binaries {
		return values
	}
	dict := NewDict()
	dict.Items["values"] = values
	if len(r.FilePatterns) > 0 {
		patterns := NewList(len(r.FilePatterns))
		for i, pattern := range r.FilePatterns {
			patterns.Items[i] = MShellString{Content: pattern}
		}
		dict.Items["files"] = patterns
	}
	if r.Dirs {
		dict.Items["dirs"] = MShellBool{true}
	}
	if r.Binaries {
		dict.Items["binaries"] = MShellBool{true}
	}
	return dict
}

// Matches returns what Tab offers for the request: the values that start with the prefix,
// then the files and directories, then the executables.
// Options (values starting with '-') are only offered when the prefix starts with '-'.
// A file or executable with the same name as a value is not offered twice.
func (r *CompletionRequest) Matches(prefix string, cfs CompletionFS, bins IPathBinManager) []TabMatch {
	var matches []TabMatch
	optionsWanted := strings.HasPrefix(prefix, "-")
	var seen map[string]struct{}
	if r.fromSeveralDefinitions {
		seen = make(map[string]struct{})
	}
	for _, value := range r.Values {
		if !strings.HasPrefix(value, prefix) || (!optionsWanted && strings.HasPrefix(value, "-")) {
			continue
		}
		if seen != nil {
			if _, repeated := seen[value]; repeated {
				continue
			}
			seen[value] = struct{}{}
		}
		matches = append(matches, TabMatch{TABMATCHCMD, value})
	}
	numValues := len(matches)

	// Values are few once filtered, so a scan beats building a set.
	isValue := func(match string) bool {
		for _, m := range matches[:numValues] {
			if m.Match == match {
				return true
			}
		}
		return false
	}

	if len(r.FilePatterns) > 0 || r.Dirs {
		var keepFile func(string) bool
		if len(r.FilePatterns) == 0 {
			keepFile = func(string) bool { return false }
		} else if !(len(r.FilePatterns) == 1 && r.FilePatterns[0] == "*") {
			keepFile = func(name string) bool {
				for _, pattern := range r.FilePatterns {
					if ok, _ := filepath.Match(pattern, name); ok {
						return true
					}
				}
				return false
			}
		}
		forEachPathCompletion(cfs, prefix, keepFile, func(match string) {
			if numValues == 0 || !isValue(match) {
				matches = append(matches, TabMatch{TABMATCHFILE, match})
			}
		})
	}

	if r.Binaries {
		for _, name := range bins.Matches(prefix) {
			if numValues == 0 || !isValue(name) {
				matches = append(matches, TabMatch{TABMATCHCMD, name})
			}
		}
	}
	return matches
}

// RunCompletionDefinitions runs the completion definitions registered for a command
// and merges what they ask Tab to offer.
// Each gets a fresh stack holding the command's finished arguments and,
// on top, the word being completed, which it can use to skip work;
// Tab does the matching against that word.
// Output from the definitions and the commands they run is discarded,
// because the line editor owns the screen while a completion runs.
// ok is false when no definition ran successfully; logf, when not nil, says why.
func (state *EvalState) RunCompletionDefinitions(defs []MShellDefinition, args []string, prefix string, context ExecuteContext, definitions []MShellDefinition, logf func(string, ...any)) (request CompletionRequest, ok bool) {
	context.StandardOutput = io.Discard
	context.StandardError = io.Discard
	context.ShouldCloseOutput = false
	context.ShouldCloseError = false
	request.fromSeveralDefinitions = len(defs) > 1

	for _, def := range defs {
		// A definition may change its argument list in place, so each gets its own.
		argList := NewList(len(args))
		for i, arg := range args {
			argList.Items[i] = MShellString{Content: arg}
		}
		stack := MShellStack{argList, MShellString{Content: prefix}}
		callStackItem := CallStackItem{MShellParseItem: def.NameToken, Name: def.Name, CallStackType: CALLSTACKDEF}
		result := state.Evaluate(def.Items, &stack, context, definitions, callStackItem)
		switch {
		case !result.Success:
			if logf != nil {
				logf("Completion definition '%s' failed to evaluate\n", def.Name)
			}
		case result.ExitCalled:
			if logf != nil {
				logf("Completion definition '%s' called exit\n", def.Name)
			}
		case len(stack) == 0:
			if logf != nil {
				logf("Completion definition '%s' left an empty stack\n", def.Name)
			}
		case !request.addResult(stack[len(stack)-1]):
			if logf != nil {
				logf("Completion definition '%s' did not return a list of strings or a completion dictionary\n", def.Name)
			}
		default:
			ok = true
		}
	}
	return request, ok
}

// OSCompletionFS implements CompletionFS using the real filesystem.
type OSCompletionFS struct{}

func (OSCompletionFS) ReadDir(dir string) ([]fs.DirEntry, error) {
	return os.ReadDir(dir)
}

func (OSCompletionFS) Getwd() (string, error) {
	return os.Getwd()
}

// OSCompletionEnv implements CompletionEnv using real environment.
type OSCompletionEnv struct{}

func (OSCompletionEnv) Environ() []string {
	return os.Environ()
}
