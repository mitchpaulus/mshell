package main

import (
	"io/fs"
	"os"
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
	prefix := input.Prefix

	if prefix == "" {
		// Complete all files in current directory
		cwd, err := cfs.Getwd()
		if err != nil {
			return matches
		}
		entries, err := cfs.ReadDir(cwd)
		if err != nil {
			return matches
		}
		for _, entry := range entries {
			if entry.IsDir() {
				matches = append(matches, TabMatch{TABMATCHFILE, entry.Name() + string(os.PathSeparator)})
			} else {
				matches = append(matches, TabMatch{TABMATCHFILE, entry.Name()})
			}
		}
	} else {
		// Split on last path separator
		indexOfLastSeparator := -1
		for i := len(prefix) - 1; i >= 0; i-- {
			if IsPathSeparator(prefix[i]) {
				indexOfLastSeparator = i
				break
			}
		}

		dir := prefix[0 : indexOfLastSeparator+1]
		filename := prefix[indexOfLastSeparator+1:]

		var searchDir string
		if len(dir) == 0 {
			searchDir = "."
		} else {
			searchDir = dir
		}

		entries, err := cfs.ReadDir(searchDir)
		if err != nil {
			return matches
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), filename) {
				if entry.IsDir() {
					matches = append(matches, TabMatch{TABMATCHFILE, dir + entry.Name() + string(os.PathSeparator)})
				} else {
					matches = append(matches, TabMatch{TABMATCHFILE, dir + entry.Name()})
				}
			}
		}
	}

	return matches
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
