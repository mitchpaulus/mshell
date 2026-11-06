package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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
	// ReadDir lists a directory. The listing is only valid until the next call.
	ReadDir(dir string) (*DirListing, error)
	// Getwd returns the current working directory.
	Getwd() (string, error)
	// Stat follows symbolic links, so a link to a directory completes as one.
	Stat(path string) (fs.FileInfo, error)
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
	// Spec is what the binary's completion definitions asked for. When nil,
	// every file is offered.
	Spec *CompletionSpec
}

// completionGlobKind is how a file name pattern is tested. Nearly every
// completion pattern is '*' or '*.ext', so those are plain string compares and
// only unusual patterns pay for filepath.Match.
type completionGlobKind uint8

const (
	globAny     completionGlobKind = iota // '*'
	globSuffix                            // '*.typ'
	globPrefix                            // 'Makefile*'
	globExact                             // 'Makefile'
	globGeneral                           // anything else
)

// CompletionGlob is a compiled file name pattern.
type CompletionGlob struct {
	Kind completionGlobKind
	Text string // Literal part for suffix/prefix/exact, the whole pattern for general.
}

// CompileCompletionGlob classifies a pattern. Text is a substring of the
// pattern, so compiling does not allocate.
func CompileCompletionGlob(pattern string) (CompletionGlob, error) {
	if pattern == "*" {
		return CompletionGlob{globAny, ""}, nil
	}
	const meta = "*?[\\"
	if !strings.ContainsAny(pattern, meta) {
		return CompletionGlob{globExact, pattern}, nil
	}
	if pattern[0] == '*' && !strings.ContainsAny(pattern[1:], meta) {
		return CompletionGlob{globSuffix, pattern[1:]}, nil
	}
	last := len(pattern) - 1
	if pattern[last] == '*' && !strings.ContainsAny(pattern[:last], meta) {
		return CompletionGlob{globPrefix, pattern[:last]}, nil
	}
	if _, err := filepath.Match(pattern, ""); err != nil {
		return CompletionGlob{}, err
	}
	return CompletionGlob{globGeneral, pattern}, nil
}

// Matches reports whether a file name (not a path) fits the pattern.
func (g CompletionGlob) Matches(name string) bool {
	switch g.Kind {
	case globAny:
		return true
	case globSuffix:
		return strings.HasSuffix(name, g.Text)
	case globPrefix:
		return strings.HasPrefix(name, g.Text)
	case globExact:
		return name == g.Text
	default:
		ok, _ := filepath.Match(g.Text, name)
		return ok
	}
}

// CompletionSpec is what a binary's completion definitions ask Tab to offer
// for an argument. A definition that returns a plain list is the same as
// {values: list, files: '*'}.
type CompletionSpec struct {
	Values         []string
	PreferredFiles []CompletionGlob
	Files          []CompletionGlob
	Dirs           bool
	Binaries       bool
}

// fileFilter picks which files a directory listing offers. Directories are
// always offered so the user can move into them.
type fileFilter struct {
	globs   []CompletionGlob
	anyFile bool
}

func newFileFilter(globs []CompletionGlob) fileFilter {
	for _, g := range globs {
		if g.Kind == globAny {
			return fileFilter{anyFile: true}
		}
	}
	return fileFilter{globs: globs}
}

func (f *fileFilter) matchesFile(name string) bool {
	if f.anyFile {
		return true
	}
	for i := range f.globs {
		if f.globs[i].Matches(name) {
			return true
		}
	}
	return false
}

// fileTiers turns a spec into the filters tried in order; the second runs only
// when the first offers nothing. ok is false when no files or directories
// should be offered at all.
func (spec *CompletionSpec) fileTiers() (first fileFilter, fallback fileFilter, hasFallback bool, ok bool) {
	if spec == nil {
		return fileFilter{anyFile: true}, fileFilter{}, false, true
	}
	if len(spec.PreferredFiles) > 0 {
		fallback = fileFilter{anyFile: true}
		if len(spec.Files) > 0 {
			fallback = newFileFilter(spec.Files)
		}
		return newFileFilter(spec.PreferredFiles), fallback, true, true
	}
	if len(spec.Files) > 0 {
		return newFileFilter(spec.Files), fileFilter{}, false, true
	}
	if spec.Dirs {
		return fileFilter{}, fileFilter{}, false, true
	}
	return fileFilter{}, fileFilter{}, false, false
}

// CompletionDeps bundles all dependencies needed for generating completions.
type CompletionDeps struct {
	FS          CompletionFS
	Env         CompletionEnv
	Binaries    IPathBinManager
	Variables   map[string]struct{} // Variable names (without @ or !)
	BuiltIns    map[string]struct{} // Built-in command names
	Definitions []string            // Definition names
	// Abbreviations are the words of the abbreviations that expand in
	// command position.
	Abbreviations []string
}

// GenerateCompletions produces completion matches for the given input.
// This is the core pure function that can be unit tested.
func GenerateCompletions(input CompletionInput, deps CompletionDeps) []TabMatch {
	matches, _ := generateRankedCompletions(input, deps)
	return matches
}

// generateRankedCompletions also returns the rank every match shares. A
// candidate fits when it starts with the typed text, or failing that, when
// the typed text appears anywhere in it. Case is ignored, but when the typed
// text has an uppercase letter, candidates with the same case win. Only the
// best rank across every source is kept.
func generateRankedCompletions(input CompletionInput, deps CompletionDeps) ([]TabMatch, matchRank) {
	c := newCompletionCollector()
	prefix := input.Prefix
	typed := newTypedText(prefix)

	// 0. What the binary's completion definitions asked for comes first.
	if spec := input.Spec; spec != nil {
		// Options (starting with '-') are only offered once the prefix starts with '-'.
		showOptions := strings.HasPrefix(prefix, "-")
		for _, value := range spec.Values {
			if !showOptions && strings.HasPrefix(value, "-") {
				continue
			}
			c.add(TABMATCHCMD, value, typed.rank(value, c.best))
		}
		if spec.Binaries {
			deps.Binaries.EachName(func(name string) {
				c.add(TABMATCHCMD, name, typed.rank(name, c.best))
			})
		}
	}

	// For unfinished strings/paths, only complete files
	if input.LastTokenType == UNFINISHEDPATH ||
		input.LastTokenType == UNFINISHEDSTRING ||
		input.LastTokenType == UNFINISHEDFORMATSTRING ||
		input.LastTokenType == UNFINISHEDSINGLEQUOTESTRING {
		appendFileCompletions(&c, input, deps.FS)
		return c.matches, c.best
	}

	// 1. Environment variable completion ($VAR)
	if len(prefix) > 0 && prefix[0] == '$' {
		typedName := newTypedText(prefix[1:])
		for _, envVar := range deps.Env.Environ() {
			name := envVar
			if i := strings.IndexByte(envVar, '='); i >= 0 {
				name = envVar[:i]
			}
			if name == "" {
				continue
			}
			if r := typedName.rank(name, c.best); r != rankNone {
				c.add(TABMATCHENVVAR, "$"+name, r)
			}
		}
	}

	// 2. Binary name completion (first token position or after pipe/list start)
	// NumTokens == 2 means: one token + EOF
	binaryPosition := input.NumTokens == 2 ||
		input.PrevTokenType == PIPE ||
		input.PrevTokenType == LEFT_SQUARE_BRACKET
	if binaryPosition && len(prefix) > 0 && input.LastTokenType == LITERAL && (input.Spec == nil || !input.Spec.Binaries) {
		deps.Binaries.EachName(func(name string) {
			c.add(TABMATCHCMD, name, typed.rank(name, c.best))
		})
	}
	if binaryPosition && len(prefix) > 0 && input.LastTokenType == LITERAL {
		for _, word := range deps.Abbreviations {
			c.add(TABMATCHCMD, word, typed.rank(word, c.best))
		}
	}

	// 3. File path completion
	appendFileCompletions(&c, input, deps.FS)

	// 4. MShell variable completion (@var)
	if len(prefix) > 0 && prefix[0] == '@' {
		typedName := newTypedText(prefix[1:])
		for v := range deps.Variables {
			if r := typedName.rank(v, c.best); r != rankNone {
				c.add(TABMATCHVAR, "@"+v, r)
			}
		}
	} else if input.LastTokenType == LITERAL && !input.InBinaryMode {
		// Completion on variables with ! suffix
		for v := range deps.Variables {
			if r := typed.rank(v, c.best); r != rankNone {
				c.add(TABMATCHVAR, v+"!", r)
			}
		}
	}

	// 5. Built-in command completion
	if !input.InBinaryMode {
		for name := range deps.BuiltIns {
			c.add(TABMATCHBUILTIN, name, typed.rank(name, c.best))
		}
	}

	// 6. Definition completion
	if !input.InBinaryMode {
		for _, defName := range deps.Definitions {
			c.add(TABMATCHDEF, defName, typed.rank(defName, c.best))
		}
	}

	return c.matches, c.best
}

// appendFileCompletions adds the files and directories the input's spec
// allows. The directory is read once; the fallback tier rescans the same
// entries only when the first tier offered nothing.
func appendFileCompletions(c *completionCollector, input CompletionInput, cfs CompletionFS) {
	first, fallback, hasFallback, ok := input.Spec.fileTiers()
	if !ok {
		return
	}
	prefix := input.Prefix

	// Split on last path separator
	indexOfLastSeparator := -1
	for i := len(prefix) - 1; i >= 0; i-- {
		if IsPathSeparator(prefix[i]) {
			indexOfLastSeparator = i
			break
		}
	}
	dir := prefix[0 : indexOfLastSeparator+1]
	typed := newTypedText(prefix[indexOfLastSeparator+1:])

	var searchDir string
	if prefix == "" {
		cwd, err := cfs.Getwd()
		if err != nil {
			return
		}
		searchDir = cwd
	} else if len(dir) == 0 {
		searchDir = "."
	} else {
		searchDir = dir
	}

	listing, err := cfs.ReadDir(searchDir)
	if err != nil {
		return
	}

	offered := appendFilteredEntries(c, listing, &first, dir, &typed, searchDir, cfs)
	if hasFallback && offered == 0 {
		appendFilteredEntries(c, listing, &fallback, dir, &typed, searchDir, cfs)
	}
}

// appendFilteredEntries adds the entries that fit the filter and the typed
// name, sorted by name, and returns how many it added. The first pass picks
// entries and sizes the output, so every match string is cut from a single
// buffer instead of one allocation per match.
func appendFilteredEntries(c *completionCollector, listing *DirListing, filter *fileFilter, dir string, typed *typedText, searchDir string, cfs CompletionFS) int {
	listing.picked = listing.picked[:0]
	size := 0
	best := c.best
	for i := range listing.entries {
		name := listing.name(i)
		r := typed.rank(name, best)
		if r == rankNone {
			continue
		}
		kind := listing.entries[i].kind
		if kind == dirKindLink || kind == dirKindUnknown {
			// Only links and unknowns pay for a stat, and only once the name
			// fits. The answer is kept for a fallback pass.
			kind = dirKindFile
			if info, err := cfs.Stat(filepath.Join(searchDir, name)); err == nil && info.IsDir() {
				kind = dirKindDir
			}
			listing.entries[i].kind = kind
		}
		n := len(name)
		if kind == dirKindDir {
			n++
		} else if !filter.matchesFile(name) {
			continue
		}
		if r < best {
			// A better rank drops the entries picked so far.
			best = r
			listing.picked = listing.picked[:0]
			size = 0
		}
		size += n
		listing.picked = append(listing.picked, int32(i))
	}
	count := len(listing.picked)
	if count == 0 {
		return 0
	}
	c.useRank(best)
	listing.sortPicked()

	var b strings.Builder
	b.Grow(size + count*len(dir))
	for _, i := range listing.picked {
		b.WriteString(dir)
		b.WriteString(listing.name(int(i)))
		if listing.entries[i].kind == dirKindDir {
			b.WriteByte(byte(os.PathSeparator))
		}
	}

	all := b.String()
	c.matches = slices.Grow(c.matches, count)
	offset := 0
	for _, i := range listing.picked {
		n := len(dir) + int(listing.entries[i].n)
		if listing.entries[i].kind == dirKindDir {
			n++
		}
		c.matches = append(c.matches, TabMatch{TABMATCHFILE, all[offset : offset+n]})
		offset += n
	}
	return count
}

// OSCompletionFS implements CompletionFS using the real filesystem. Listing is
// reused across reads; when nil, each read gets a fresh one.
type OSCompletionFS struct {
	Listing *DirListing
}

func (f OSCompletionFS) ReadDir(dir string) (*DirListing, error) {
	listing := f.Listing
	if listing == nil {
		listing = &DirListing{}
	}
	if err := readDirListing(dir, listing); err != nil {
		return nil, err
	}
	return listing, nil
}

func (OSCompletionFS) Getwd() (string, error) {
	return os.Getwd()
}

func (OSCompletionFS) Stat(path string) (fs.FileInfo, error) {
	return os.Stat(path)
}

// OSCompletionEnv implements CompletionEnv using real environment.
type OSCompletionEnv struct{}

func (OSCompletionEnv) Environ() []string {
	return os.Environ()
}
