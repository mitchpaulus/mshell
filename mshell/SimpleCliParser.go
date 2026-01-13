package main

import (
	"fmt"
)

// SimpleCliParser parses simple CLI-style commands with pipes and redirects.
// Grammar:
//   CLI : item+ ('<' item)? ('|' item+)* ('>' item)?
//
// Items are parsed using the normal MShellParser, which handles lists, dicts, etc.
// Only PIPE, LESSTHAN, and GREATERTHAN are treated specially at the top level.

// SimpleCliCommand represents a single command with its arguments (parsed items)
type SimpleCliCommand struct {
	Items []MShellParseItem
}

// SimpleCliPipeline represents the full CLI parse tree
type SimpleCliPipeline struct {
	Commands       []SimpleCliCommand // At least one command
	StdinRedirect  MShellParseItem    // Optional stdin redirect (can be path, string, etc.)
	StdoutRedirect MShellParseItem    // Optional stdout redirect
}

// MShellSimpleCliParser parses simple CLI-style input using MShellParser for items
type MShellSimpleCliParser struct {
	parser *MShellParser
}

// NewMShellSimpleCliParser creates a new simple CLI parser
func NewMShellSimpleCliParser(lexer *Lexer) *MShellSimpleCliParser {
	return &MShellSimpleCliParser{
		parser: NewMShellParser(lexer),
	}
}

// isSimpleCliDelimiter returns true if the token delimits commands/redirects in simple CLI mode
func isSimpleCliDelimiter(t Token) bool {
	switch t.Type {
	case EOF, PIPE, LESSTHAN, GREATERTHAN:
		return true
	default:
		return false
	}
}

// Parse parses the input according to the simple CLI grammar.
// Returns nil if the input is empty or doesn't start with a valid token.
func (p *MShellSimpleCliParser) Parse() (*SimpleCliPipeline, error) {
	result := &SimpleCliPipeline{
		Commands: make([]SimpleCliCommand, 0),
	}

	// Parse first command (required: item+)
	firstCmd, err := p.parseCommand()
	if err != nil {
		return nil, err
	}
	if len(firstCmd.Items) == 0 {
		return nil, nil // Empty input
	}
	result.Commands = append(result.Commands, firstCmd)

	// Check for optional stdin redirect ('<' item)?
	if p.parser.curr.Type == LESSTHAN {
		p.parser.NextToken() // consume '<'
		item, err := p.parser.ParseItem()
		if err != nil {
			return nil, &SimpleCliParseError{Message: "expected item after '<'", Token: p.parser.curr}
		}
		result.StdinRedirect = item

		// After stdin redirect, only valid tokens are: PIPE, GREATERTHAN, or EOF
		if p.parser.curr.Type != PIPE && p.parser.curr.Type != GREATERTHAN && p.parser.curr.Type != EOF {
			return nil, &SimpleCliParseError{
				Message: "only a single item expected after '<'",
				Token:   p.parser.curr,
			}
		}
	}

	// Parse zero or more piped commands ('|' item+)*
	for p.parser.curr.Type == PIPE {
		p.parser.NextToken() // consume '|'

		cmd, err := p.parseCommand()
		if err != nil {
			return nil, err
		}
		if len(cmd.Items) == 0 {
			return nil, &SimpleCliParseError{Message: "expected command after '|'", Token: p.parser.curr}
		}
		result.Commands = append(result.Commands, cmd)
	}

	// Check for optional stdout redirect ('>' item)?
	if p.parser.curr.Type == GREATERTHAN {
		p.parser.NextToken() // consume '>'
		item, err := p.parser.ParseItem()
		if err != nil {
			return nil, &SimpleCliParseError{Message: "expected item after '>'", Token: p.parser.curr}
		}
		result.StdoutRedirect = item

		// After stdout redirect, only valid token is EOF
		if p.parser.curr.Type != EOF {
			return nil, &SimpleCliParseError{
				Message: "only a single item expected after '>'",
				Token:   p.parser.curr,
			}
		}
	}

	// Should be at EOF now
	if p.parser.curr.Type != EOF {
		return nil, &SimpleCliParseError{Message: "unexpected token after command", Token: p.parser.curr}
	}

	return result, nil
}

// parseCommand parses items until we hit a delimiter (PIPE, LESSTHAN, GREATERTHAN, EOF)
func (p *MShellSimpleCliParser) parseCommand() (SimpleCliCommand, error) {
	cmd := SimpleCliCommand{Items: make([]MShellParseItem, 0)}

	for !isSimpleCliDelimiter(p.parser.curr) {
		item, err := p.parser.ParseItem()
		if err != nil {
			return cmd, err
		}
		cmd.Items = append(cmd.Items, item)
	}

	return cmd, nil
}

// SimpleCliParseError represents a parse error
type SimpleCliParseError struct {
	Message string
	Token   Token
}

func (e *SimpleCliParseError) Error() string {
	return fmt.Sprintf("%s at '%s'", e.Message, e.Token.Lexeme)
}

// ToMShellString transforms the simple CLI parse tree into mshell syntax (for debugging).
func (pipeline *SimpleCliPipeline) ToMShellString() string {
	file, err := pipeline.ToMShellFile()
	if err != nil {
		return fmt.Sprintf("Error: %s", err)
	}
	var result string
	for _, item := range file.Items {
		result += item.DebugString()
	}
	return result
}

// ToMShellFile transforms the simple CLI parse tree directly into an MShellFile AST.
// Since items are already parsed as MShellParseItems, we just need to wrap them
// in the appropriate list structure and add operators.
func (pipeline *SimpleCliPipeline) ToMShellFile() (*MShellFile, error) {
	hasPipe := len(pipeline.Commands) > 1

	// Convert stdin redirect: LITERAL becomes PATH, string-like types pass through
	var stdinItem MShellParseItem
	if pipeline.StdinRedirect != nil {
		if t, ok := pipeline.StdinRedirect.(Token); ok {
			switch t.Type {
			case LITERAL:
				stdinItem = Token{Type: PATH, Lexeme: "`" + t.Lexeme + "`", Line: t.Line, Column: t.Column, Start: t.Start}
			case STRING, SINGLEQUOTESTRING, PATH:
				stdinItem = t
			default:
				return nil, &SimpleCliParseError{Message: "stdin redirect must be a string or path", Token: t}
			}
		} else {
			return nil, &SimpleCliParseError{Message: "stdin redirect must be a string or path", Token: Token{Lexeme: fmt.Sprintf("%v", pipeline.StdinRedirect)}}
		}
	}

	// Convert stdout redirect: LITERAL becomes SINGLEQUOTESTRING, string-like types pass through
	var stdoutItem MShellParseItem
	if pipeline.StdoutRedirect != nil {
		if t, ok := pipeline.StdoutRedirect.(Token); ok {
			switch t.Type {
			case LITERAL:
				stdoutItem = Token{Type: SINGLEQUOTESTRING, Lexeme: "'" + t.Lexeme + "'", Line: t.Line, Column: t.Column, Start: t.Start}
			case STRING, SINGLEQUOTESTRING, PATH:
				stdoutItem = t
			default:
				return nil, &SimpleCliParseError{Message: "stdout redirect must be a string or path", Token: t}
			}
		} else {
			return nil, &SimpleCliParseError{Message: "stdout redirect must be a string or path", Token: Token{Lexeme: fmt.Sprintf("%v", pipeline.StdoutRedirect)}}
		}
	}

	var items []MShellParseItem

	if hasPipe {
		// Multiple commands: [[cmd1 items] [cmd2 items] redirect >]|;
		// Create outer list containing command lists and redirects
		outerItems := make([]MShellParseItem, 0)

		for i, cmd := range pipeline.Commands {
			// Create inner command list from parsed items
			cmdList := &MShellParseList{
				Items:      convertItemsForExecution(cmd.Items),
				StartToken: Token{Type: LEFT_SQUARE_BRACKET, Lexeme: "["},
				EndToken:   Token{Type: RIGHT_SQUARE_BRACKET, Lexeme: "]"},
			}
			outerItems = append(outerItems, cmdList)

			// Stdin redirect goes after first command
			if i == 0 && stdinItem != nil {
				outerItems = append(outerItems, stdinItem)
				outerItems = append(outerItems, Token{Type: LESSTHAN, Lexeme: "<"})
			}

			// Stdout redirect goes after last command
			if i == len(pipeline.Commands)-1 && stdoutItem != nil {
				outerItems = append(outerItems, stdoutItem)
				outerItems = append(outerItems, Token{Type: GREATERTHAN, Lexeme: ">"})
			}
		}

		outerList := &MShellParseList{
			Items:      outerItems,
			StartToken: Token{Type: LEFT_SQUARE_BRACKET, Lexeme: "["},
			EndToken:   Token{Type: RIGHT_SQUARE_BRACKET, Lexeme: "]"},
		}

		items = []MShellParseItem{
			outerList,
			Token{Type: PIPE, Lexeme: "|"},
			Token{Type: EXECUTE, Lexeme: ";"},
		}
	} else {
		// Single command: [cmd items] redirect < redirect > ;
		cmdList := &MShellParseList{
			Items:      convertItemsForExecution(pipeline.Commands[0].Items),
			StartToken: Token{Type: LEFT_SQUARE_BRACKET, Lexeme: "["},
			EndToken:   Token{Type: RIGHT_SQUARE_BRACKET, Lexeme: "]"},
		}
		items = []MShellParseItem{cmdList}

		// Add stdin redirect if present
		if stdinItem != nil {
			items = append(items, stdinItem)
			items = append(items, Token{Type: LESSTHAN, Lexeme: "<"})
		}

		// Add stdout redirect if present
		if stdoutItem != nil {
			items = append(items, stdoutItem)
			items = append(items, Token{Type: GREATERTHAN, Lexeme: ">"})
		}

		items = append(items, Token{Type: EXECUTE, Lexeme: ";"})
	}

	return &MShellFile{
		Definitions: nil,
		Items:       items,
	}, nil
}

// convertItemsForExecution converts parsed items for use in a command list.
// Literals are converted to single-quoted strings for execution.
func convertItemsForExecution(items []MShellParseItem) []MShellParseItem {
	result := make([]MShellParseItem, len(items))
	for i, item := range items {
		result[i] = convertItemForExecution(item)
	}
	return result
}

// convertItemForExecution converts a single parsed item for execution.
// Literals become single-quoted strings, other types are preserved.
func convertItemForExecution(item MShellParseItem) MShellParseItem {
	switch v := item.(type) {
	case Token:
		// Convert LITERAL tokens to SINGLEQUOTESTRING for execution
		if v.Type == LITERAL {
			return Token{
				Type:   SINGLEQUOTESTRING,
				Lexeme: "'" + v.Lexeme + "'",
				Line:   v.Line,
				Column: v.Column,
				Start:  v.Start,
			}
		}
		return v
	default:
		// Lists, dicts, quotes, etc. are preserved as-is
		return item
	}
}
