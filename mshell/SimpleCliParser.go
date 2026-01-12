package main

// SimpleCliParser parses simple CLI-style commands with pipes and redirects.
// Grammar:
//   CLI : tokens+ ('<' fileToken)? ('|' tokens+)* ('>' fileToken)?
//
// Where fileToken is a LITERAL, STRING, SINGLEQUOTESTRING, or PATH.

// SimpleCliCommand represents a single command with its arguments
type SimpleCliCommand struct {
	Tokens []Token
}

// SimpleCliPipeline represents the full CLI parse tree
type SimpleCliPipeline struct {
	Commands       []SimpleCliCommand // At least one command
	StdinRedirect  *Token             // Optional stdin redirect file
	StdoutRedirect *Token             // Optional stdout redirect file
}

// MShellSimpleCliParser parses simple CLI-style input
type MShellSimpleCliParser struct {
	lexer *Lexer
	curr  Token
}

// NewMShellSimpleCliParser creates a new simple CLI parser
func NewMShellSimpleCliParser(lexer *Lexer) *MShellSimpleCliParser {
	parser := &MShellSimpleCliParser{lexer: lexer}
	parser.NextToken()
	return parser
}

// NextToken advances to the next token
func (p *MShellSimpleCliParser) NextToken() {
	p.curr = p.lexer.scanToken()
}

// ResetInput resets the parser with new input
func (p *MShellSimpleCliParser) ResetInput(input string) {
	p.lexer.resetInput(input)
	p.NextToken()
}

// isFileToken returns true if the token can be used as a file path for redirects
func isFileToken(t Token) bool {
	return t.Type == LITERAL || t.Type == STRING || t.Type == SINGLEQUOTESTRING || t.Type == PATH
}

// isCommandToken returns true if the token is valid as part of a command
func isCommandToken(t Token) bool {
	switch t.Type {
	case EOF, PIPE, LESSTHAN, GREATERTHAN:
		return false
	default:
		return true
	}
}

// Parse parses the input according to the simple CLI grammar.
// Returns nil if the input is empty or doesn't start with a valid token.
func (p *MShellSimpleCliParser) Parse() (*SimpleCliPipeline, error) {
	result := &SimpleCliPipeline{
		Commands: make([]SimpleCliCommand, 0),
	}

	// Parse first command (required: tokens+)
	firstCmd := SimpleCliCommand{Tokens: make([]Token, 0)}
	for isCommandToken(p.curr) {
		firstCmd.Tokens = append(firstCmd.Tokens, p.curr)
		p.NextToken()
	}

	if len(firstCmd.Tokens) == 0 {
		return nil, nil // Empty input or no valid tokens
	}
	result.Commands = append(result.Commands, firstCmd)

	// Check for optional stdin redirect ('<' fileToken)?
	if p.curr.Type == LESSTHAN {
		p.NextToken() // consume '<'
		if !isFileToken(p.curr) {
			return nil, &SimpleCliParseError{Message: "expected file path after '<'", Token: p.curr}
		}
		stdinToken := p.curr // Make a copy
		result.StdinRedirect = &stdinToken
		p.NextToken()
	}

	// Parse zero or more piped commands ('|' tokens+)*
	for p.curr.Type == PIPE {
		p.NextToken() // consume '|'

		cmd := SimpleCliCommand{Tokens: make([]Token, 0)}
		for isCommandToken(p.curr) {
			cmd.Tokens = append(cmd.Tokens, p.curr)
			p.NextToken()
		}

		if len(cmd.Tokens) == 0 {
			return nil, &SimpleCliParseError{Message: "expected command after '|'", Token: p.curr}
		}
		result.Commands = append(result.Commands, cmd)
	}

	// Check for optional stdout redirect ('>' fileToken)?
	if p.curr.Type == GREATERTHAN {
		p.NextToken() // consume '>'
		if !isFileToken(p.curr) {
			return nil, &SimpleCliParseError{Message: "expected file path after '>'", Token: p.curr}
		}
		stdoutToken := p.curr // Make a copy
		result.StdoutRedirect = &stdoutToken
		p.NextToken()
	}

	// Should be at EOF now
	if p.curr.Type != EOF {
		return nil, &SimpleCliParseError{Message: "unexpected token after command", Token: p.curr}
	}

	return result, nil
}

// SimpleCliParseError represents a parse error
type SimpleCliParseError struct {
	Message string
	Token   Token
}

func (e *SimpleCliParseError) Error() string {
	return e.Message + " at " + e.Token.Lexeme
}

// ToMShellString transforms the simple CLI parse tree into mshell syntax.
// Examples:
//   - "ls -la" -> "['ls' '-la'];"
//   - "cat file | grep foo" -> "[['cat' 'file'] ['grep' 'foo']]|;"
//   - "grep foo < input.txt" -> "['grep' 'foo'] `input.txt` < ;"
//   - "cat file > out.txt" -> "['cat' 'file'] `out.txt` > ;"
//   - "cat file | grep foo > out.txt" -> "[['cat' 'file'] ['grep' 'foo'] `out.txt` >]|;"
//   - "sort < in.txt | wc -l > out.txt" -> "[['sort'] `in.txt` < ['wc' '-l'] `out.txt` >]|;"
func (pipeline *SimpleCliPipeline) ToMShellString() string {
	var sb stringBuilder

	hasPipe := len(pipeline.Commands) > 1

	if hasPipe {
		// Multiple commands with redirects on first/last command:
		// [[cmd1] stdin< [cmd2] stdout>]|;
		sb.WriteString("[")
		for i, cmd := range pipeline.Commands {
			sb.WriteString("[")
			writeCommandTokens(&sb, cmd.Tokens)
			sb.WriteString("]")

			// Stdin redirect goes on first command
			if i == 0 && pipeline.StdinRedirect != nil {
				sb.WriteString(" ")
				sb.WriteString(tokenToMShellPath(*pipeline.StdinRedirect))
				sb.WriteString(" <")
			}

			// Stdout redirect goes on last command
			if i == len(pipeline.Commands)-1 && pipeline.StdoutRedirect != nil {
				sb.WriteString(" ")
				sb.WriteString(tokenToMShellPath(*pipeline.StdoutRedirect))
				sb.WriteString(" >")
			}

			// Add space before next command (but not after last)
			if i < len(pipeline.Commands)-1 {
				sb.WriteString(" ")
			}
		}
		sb.WriteString("]|;")
	} else {
		// Single command: ['cmd' args]
		sb.WriteString("[")
		writeCommandTokens(&sb, pipeline.Commands[0].Tokens)
		sb.WriteString("]")

		// Add stdin redirect if present
		if pipeline.StdinRedirect != nil {
			sb.WriteString(" ")
			sb.WriteString(tokenToMShellPath(*pipeline.StdinRedirect))
			sb.WriteString(" <")
		}

		// Add stdout redirect if present
		if pipeline.StdoutRedirect != nil {
			sb.WriteString(" ")
			sb.WriteString(tokenToMShellPath(*pipeline.StdoutRedirect))
			sb.WriteString(" >")
		}

		// Add space before ; if we had any redirects
		if pipeline.StdinRedirect != nil || pipeline.StdoutRedirect != nil {
			sb.WriteString(" ")
		}
		sb.WriteString(";")
	}

	return sb.String()
}

// stringBuilder is a simple wrapper around string concatenation
type stringBuilder struct {
	result string
}

func (sb *stringBuilder) WriteString(s string) {
	sb.result += s
}

func (sb *stringBuilder) String() string {
	return sb.result
}

// writeCommandTokens writes command tokens to the string builder
func writeCommandTokens(sb *stringBuilder, tokens []Token) {
	for i, t := range tokens {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(tokenToMShellArg(t))
	}
}

// tokenToMShellArg converts a token to its mshell representation as a command argument
func tokenToMShellArg(t Token) string {
	switch t.Type {
	case STRING:
		return t.Lexeme // Already has quotes
	case SINGLEQUOTESTRING:
		return t.Lexeme // Already has quotes
	case PATH:
		return t.Lexeme // Already has backticks
	default:
		// Wrap literals and other tokens in single quotes
		return "'" + t.Lexeme + "'"
	}
}

// tokenToMShellPath converts a token to a path for redirects
func tokenToMShellPath(t Token) string {
	switch t.Type {
	case PATH:
		return t.Lexeme // Already has backticks
	case STRING:
		// Convert "file" to `file`
		// Remove quotes and wrap in backticks
		inner := t.Lexeme[1 : len(t.Lexeme)-1]
		return "`" + inner + "`"
	case SINGLEQUOTESTRING:
		// Convert 'file' to `file`
		inner := t.Lexeme[1 : len(t.Lexeme)-1]
		return "`" + inner + "`"
	default:
		// Wrap literal in backticks for path
		return "`" + t.Lexeme + "`"
	}
}
