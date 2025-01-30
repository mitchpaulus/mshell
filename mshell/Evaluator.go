package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type MShellStack []MShellObject

func (objList *MShellStack) Peek() (MShellObject, error)            {
	if len(*objList) == 0 {
		return nil, fmt.Errorf("Empty stack")
	}
	return (*objList)[len(*objList)-1], nil
}

func (objList *MShellStack) Pop() (MShellObject, error) {
	if len(*objList) == 0 {
		return nil, fmt.Errorf("Empty stack")
	}
	popped := (*objList)[len(*objList)-1]
	*objList = (*objList)[:len(*objList)-1]
	return popped, nil
}

func (objList *MShellStack) Push(obj MShellObject) {
	*objList = append(*objList, obj)
}

func (objList *MShellStack) String() string {
	var builder strings.Builder
	builder.WriteString("Stack contents:\n")
	for i, obj := range *objList {
		builder.WriteString(fmt.Sprintf("%d: %s\n", i, obj.DebugString()))
	}
	builder.WriteString("End of stack contents\n")
	return builder.String()
}

type EvalState struct {
	PositionalArgs []string
	LoopDepth      int

	StopOnError bool
}

type EvalResult struct {
	Success    bool
	BreakNum   int
	ExitCode   int
	ExitCalled bool
}

type ExecuteContext struct {
	StandardInput  io.Reader
	StandardOutput io.Writer
	Variables      map[string]MShellObject
	ShouldCloseInput  bool
	ShouldCloseOutput bool
}

func (context *ExecuteContext) Close() {
	if context.ShouldCloseInput {
		if context.StandardInput != nil {
			if closer, ok := context.StandardInput.(io.Closer); ok {
				closer.Close()
			}
		}
	}

	if context.ShouldCloseOutput {
		if context.StandardOutput != nil {
			if closer, ok := context.StandardOutput.(io.Closer); ok {
				closer.Close()
			}
		}
	}
}

func SimpleSuccess() EvalResult {
	return EvalResult{true, -1, 0, false}
}

func FailWithMessage(message string) EvalResult {
	// Log message to stderr
	fmt.Fprintf(os.Stderr, message)
	return EvalResult{false, -1, 1, false}
}

type CallStackType int

const (
	CALLSTACKFILE CallStackType = iota
	CALLSTACKLIST
	CALLSTACKQUOTE
	CALLSTACKDEF
)

type CallStackItem struct {
	MShellParseItem MShellParseItem
	Name            string
	CallStackType   CallStackType
}

type CallStack []CallStackItem

func (stack *CallStack) Push(item CallStackItem) {
	*stack = append(*stack, item)
}

func (stack *CallStack) Pop() (CallStackItem, error) {
	if len(*stack) == 0 {
		return CallStackItem{}, fmt.Errorf("Empty stack")
	}
	popped := (*stack)[len(*stack)-1]
	*stack = (*stack)[:len(*stack)-1]
	return popped, nil
}

func (state *EvalState) Evaluate(objects []MShellParseItem, stack *MShellStack, context ExecuteContext, definitions []MShellDefinition, callStack CallStack, callStackItem CallStackItem) EvalResult {
	// Defer popping the call stack
	callStack.Push(callStackItem)
	defer func() {
		callStack.Pop()
	}()

	index := 0

MainLoop:
	for index < len(objects) {
		t := objects[index]
		index++

		switch t.(type) {
		case *MShellParseList:
			// Evaluate the list
			list := t.(*MShellParseList)
			var listStack MShellStack
			listStack = []MShellObject{}

			callStackItem := CallStackItem{MShellParseItem: list, Name: "list", CallStackType: CALLSTACKLIST}
			result := state.Evaluate(list.Items, &listStack, context, definitions, callStack, callStackItem)

			if !result.Success {
				fmt.Fprintf(os.Stderr, "Failed to evaluate list.\n")
				return result
			}

			if result.ExitCalled {
				return result
			}

			if result.BreakNum > 0 {
				return FailWithMessage("Encountered break within list.\n")
			}

			newList := NewList(len(listStack))
			for i, item := range listStack {
				newList.Items[i] = item
			}
			stack.Push(newList)
		case *MShellParseQuote:
			parseQuote := t.(*MShellParseQuote)
			q := MShellQuotation{Tokens: parseQuote.Items, StandardInputFile: "", StandardOutputFile: "", StandardErrorFile: "", Variables: context.Variables, MShellParseQuote: parseQuote}
			stack.Push(&q)
		case *MShellIndexerList:
			obj1, err := stack.Pop()
			if err != nil {
				startToken := t.GetStartToken()
				return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'indexer' operation on an empty stack.\n", startToken.Line, startToken.Column))
			}

			indexerList := t.(*MShellIndexerList)
			if len(indexerList.Indexers) == 1 && indexerList.Indexers[0].(Token).Type == INDEXER {
				t := indexerList.Indexers[0].(Token)
				// Indexer is a digit between ':' and ':'. Remove ends and parse the number
				indexStr := t.Lexeme[1 : len(t.Lexeme)-1]
				index, err := strconv.Atoi(indexStr)
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Error parsing index: %s\n", t.Line, t.Column, err.Error()))
				}

				result, err := obj1.Index(index)
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: %s", t.Line, t.Column, err.Error()))
				}
				stack.Push(result)
			} else {
				var newObject MShellObject;
				newObject = nil

				for _, indexer := range indexerList.Indexers {
					indexerToken := indexer.(Token)
					switch indexerToken.Type {
					case INDEXER:
						indexStr := indexerToken.Lexeme[1 : len(indexerToken.Lexeme)-1]
						index, err := strconv.Atoi(indexStr)
						if err != nil {
							return FailWithMessage(fmt.Sprintf("%d:%d: Error parsing index: %s\n", indexerToken.Line, indexerToken.Column, err.Error()))
						}

						result, err := obj1.Index(index)
						if err != nil {
							return FailWithMessage(fmt.Sprintf("%d:%d: %s", indexerToken.Line, indexerToken.Column, err.Error()))
						}

						var wrappedResult MShellObject
						switch obj1.(type) {
						case *MShellList:
							wrappedResult = NewList(0)
							wrappedResult.(*MShellList).Items = append(wrappedResult.(*MShellList).Items, result)
						case *MShellQuotation:
							wrappedResult = &MShellQuotation{Tokens: []MShellParseItem{result.(MShellParseItem)}, StandardInputFile: "", StandardOutputFile: "", StandardErrorFile: "", Variables: context.Variables, MShellParseQuote: nil}
						case *MShellPipe:
							newList := NewList(0)
							wrappedResult = &MShellPipe{List: *newList, StdoutBehavior: STDOUT_NONE }
							wrappedResult.(*MShellPipe).List.Items = append(wrappedResult.(*MShellPipe).List.Items, result)
						default:
							wrappedResult = result
						}

						if newObject == nil {
							newObject = wrappedResult
						} else {
							newObject, err  = newObject.Concat(wrappedResult)
							if err != nil {
								return FailWithMessage(fmt.Sprintf("%d:%d: %s", indexerToken.Line, indexerToken.Column, err.Error()))
							}
						}
					case STARTINDEXER, ENDINDEXER:
						var indexStr string
						// Parse the index value
						if indexerToken.Type == ENDINDEXER {
							indexStr = indexerToken.Lexeme[1:]
						} else {
							indexStr = indexerToken.Lexeme[:len(indexerToken.Lexeme)-1]
						}

						index, err := strconv.Atoi(indexStr)
						if err != nil {
							return FailWithMessage(fmt.Sprintf("%d:%d: Error parsing index: %s\n", indexerToken.Line, indexerToken.Column, err.Error()))
						}

						var result MShellObject
						if indexerToken.Type == ENDINDEXER {
							result, err = obj1.SliceEnd(index)
						} else {
							result, err = obj1.SliceStart(index)
						}

						if err != nil {
							return FailWithMessage(fmt.Sprintf("%d:%d: %s", indexerToken.Line, indexerToken.Column, err.Error()))
						}

						if newObject == nil {
							newObject = result
						} else {
							newObject, err  = newObject.Concat(result)
							if err != nil {
								return FailWithMessage(fmt.Sprintf("%d:%d: %s", indexerToken.Line, indexerToken.Column, err.Error()))
							}
						}

					case SLICEINDEXER:
						// StartInc:EndExc
						parts := strings.Split(indexerToken.Lexeme, ":")
						startInt, err := strconv.Atoi(parts[0])
						endInt, err2 := strconv.Atoi(parts[1])

						if err != nil || err2 != nil {
							return FailWithMessage(fmt.Sprintf("%d:%d: Error parsing slice indexes: %s\n", indexerToken.Line, indexerToken.Column, err.Error()))
						}

						result, err := obj1.Slice(startInt, endInt)
						if err != nil {
							return FailWithMessage(fmt.Sprintf("%d:%d: Cannot slice index a %s.\n", indexerToken.Line, indexerToken.Column, obj1.TypeName()))
						}

						if newObject == nil {
							newObject = result
						} else {
							newObject, err  = newObject.Concat(result)
							if err != nil {
								return FailWithMessage(fmt.Sprintf("%d:%d: %s", indexerToken.Line, indexerToken.Column, err.Error()))
							}
						}

					}
				}

				stack.Push(newObject)
			}
		case Token:
			t := t.(Token)

			if t.Type == EOF {
				return SimpleSuccess()
			} else if t.Type == LITERAL {

				// Check for definitions
				for _, definition := range definitions {
					if definition.Name == t.Lexeme {
						// Evaluate the definition

						var newContext ExecuteContext
						newContext.Variables = make(map[string]MShellObject)
						newContext.StandardInput = context.StandardInput
						newContext.StandardOutput = context.StandardOutput

						callStackItem := CallStackItem{MShellParseItem: t, Name: definition.Name, CallStackType: CALLSTACKDEF}
						result := state.Evaluate(definition.Items, stack, newContext, definitions, callStack, callStackItem)

						if !result.Success || result.BreakNum > 0 || result.ExitCalled {
							return result
						}

						continue MainLoop
					}
				}

				if t.Lexeme == ".s" {
					// Print current stack
					fmt.Fprintf(os.Stderr, stack.String())
				} else if t.Lexeme == ".def" {
					// Print out available definitions
					fmt.Fprintf(os.Stderr, "Available definitions:\n")
					for _, definition := range definitions {
						fmt.Fprintf(os.Stderr, "%s\n", definition.Name)
					}
				} else if t.Lexeme == "dup" {
					top, err := stack.Peek()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot duplicate an empty stack.\n", t.Line, t.Column))
					}
					stack.Push(top)
				} else if t.Lexeme == "over" {
					stackLen := len(*stack)
					if stackLen < 2 {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'over' operation on a stack with less than two items.\n", t.Line, t.Column))
					}

					obj := (*stack)[stackLen-2]
					stack.Push(obj)
				} else if t.Lexeme == "swap" {
					obj1, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'swap' operation on an empty stack.\n", t.Line, t.Column))
					}

					obj2, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'swap' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					stack.Push(obj1)
					stack.Push(obj2)
				} else if t.Lexeme == "drop" {
					_, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot drop an empty stack.\n", t.Line, t.Column))
					}
				} else if t.Lexeme == "rot" {
					// Check that there are at least 3 items on the stack
					stackLen := len(*stack)
					if stackLen < 3 {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'rot' operation on a stack with less than three items.\n", t.Line, t.Column))
					}
					top, _ := stack.Pop()
					second, _ := stack.Pop()
					third, _ := stack.Pop()
					stack.Push(second)
					stack.Push(top)
					stack.Push(third)
				} else if t.Lexeme == "-rot" {
					// Check that there are at least 3 items on the stack
					if len(*stack) < 3 {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'rot' operation on a stack with less than three items.\n", t.Line, t.Column))
					}
					top, _ := stack.Pop()
					second, _ := stack.Pop()
					third, _ := stack.Pop()
					stack.Push(top)
					stack.Push(third)
					stack.Push(second)
				} else if t.Lexeme == "nip" {
					if len(*stack) < 2 {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'nip' operation on a stack with less than two items.\n", t.Line, t.Column))
					}
					top, _ := stack.Pop()
					_, _ = stack.Pop()
					stack.Push(top)
				} else if t.Lexeme == "glob" {
					obj1, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'glob' operation on an empty stack.\n", t.Line, t.Column))
					}

					// Can be a string or literal
					var globStr string
					switch obj1.(type) {
					case *MShellString:
						globStr = obj1.(*MShellString).Content
					case *MShellLiteral:
						globStr = obj1.(*MShellLiteral).LiteralText
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot glob a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					files, err := filepath.Glob(globStr)
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Malformed glob pattern: %s\n", t.Line, t.Column, err.Error()))
					}

					newList := NewList(len(files))
					for i, file := range files {
						newList.Items[i] = &MShellString{file}
					}

					stack.Push(newList)
				} else if t.Lexeme == "stdin" {
					// Dump all of current stdin onto the stack as a string
					var buffer bytes.Buffer
					_, err := buffer.ReadFrom(context.StandardInput)
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Error reading from stdin: %s\n", t.Line, t.Column, err.Error()))
					}
					stack.Push(&MShellString{buffer.String()})
				} else if t.Lexeme == "append" {
					obj1, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'append' operation on an empty stack.\n", t.Line, t.Column))
					}

					obj2, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'append' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					// Can do append with list and object in either order. If two lists, append obj1 into obj2
					switch obj1.(type) {
					case *MShellList:
						switch obj2.(type) {
						case *MShellList:
							obj2.(*MShellList).Items = append(obj2.(*MShellList).Items, obj1)
							stack.Push(obj2)
						default:
							obj1.(*MShellList).Items = append(obj1.(*MShellList).Items, obj2)
							stack.Push(obj1)
						}
					default:
						switch obj2.(type) {
						case *MShellList:
							obj2.(*MShellList).Items = append(obj2.(*MShellList).Items, obj1)
							stack.Push(obj2)
						default:
							return FailWithMessage(fmt.Sprintf("%d:%d: Cannot append a %s to a %s.\n", t.Line, t.Column, obj1.TypeName(), obj2.TypeName()))
						}
					}
				} else if t.Lexeme == "args" {
					// Dump the positional arguments onto the stack as a list of strings
					newList := NewList(len(state.PositionalArgs))
					for i, arg := range state.PositionalArgs {
						newList.Items[i] = &MShellString{arg}
					}
					stack.Push(newList)
				} else if t.Lexeme == "len" {
					obj, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'len' operation on an empty stack.\n", t.Line, t.Column))
					}

					switch obj.(type) {
					case *MShellList:
						stack.Push(&MShellInt{len(obj.(*MShellList).Items)})
					case *MShellString:
						stack.Push(&MShellInt{len(obj.(*MShellString).Content)})
					case *MShellLiteral:
						stack.Push(&MShellInt{len(obj.(*MShellLiteral).LiteralText)})
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot get length of a %s.\n", t.Line, t.Column, obj.TypeName()))
					}
				} else if t.Lexeme == "nth" {
					obj1, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'nth' operation on an empty stack.\n", t.Line, t.Column))
					}

					obj2, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'nth' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					int1, ok := obj1.(*MShellInt)
					if ok {
						result, err := obj2.Index(int1.Value)
						if err != nil {
							return FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
						}
						stack.Push(result)
					} else {
						int2, ok := obj2.(*MShellInt)
						if ok {
							result, err := obj1.Index(int2.Value)
							if err != nil {
								return FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
							}
							stack.Push(result)
						} else {
							return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'nth' with a %s and a %s.\n", t.Line, t.Column, obj2.TypeName(), obj1.TypeName()))
						}
					}
				} else if t.Lexeme == "pick" {
					obj1, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'pick' operation on an empty stack.\n", t.Line, t.Column))
					}
					// Check that obj1 is an integer
					int1, ok := obj1.(*MShellInt)
					if !ok {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'pick' with a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					// Check that int is greater than or equal to 1
					if int1.Value < 1 {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'pick' with a value less than 1.\n", t.Line, t.Column))
					}

					// Check that the stack has enough items
					if int1.Value > len(*stack) {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'pick' on a stack with less than %d items.\n", t.Line, t.Column, int1.Value+1))
					}

					// Duplicate the nth item on the stack
					// a b c 2  -> a b c b
					stack.Push((*stack)[len(*stack)-int1.Value])
				} else if t.Lexeme == "w" || t.Lexeme == "wl" || t.Lexeme == "we" || t.Lexeme == "wle" {
					// Print the top of the stack to the console.
					top, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot write an empty stack.\n", t.Line, t.Column))
					}

					var writer io.Writer
					if t.Lexeme == "we" || t.Lexeme == "wle" {
						writer = os.Stderr
					} else {
						writer = os.Stdout
					}

					switch top.(type) {
					case *MShellLiteral:
						fmt.Fprintf(writer, "%s", top.(*MShellLiteral).LiteralText)
					case *MShellString:
						fmt.Fprintf(writer, "%s", top.(*MShellString).Content)
					case *MShellInt:
						fmt.Fprintf(writer, "%d", top.(*MShellInt).Value)
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot write a %s.\n", t.Line, t.Column, top.TypeName()))
					}

					if t.Lexeme == "wl" || t.Lexeme == "wle" {
						fmt.Fprintf(writer, "\n")
					}
				} else if t.Lexeme == "findReplace" {
					// Do simple find replace with the top three strings on stack
					obj1, err := stack.Pop() // Replacement
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'find-replace' operation on an empty stack.\n", t.Line, t.Column))
					}

					obj2, err := stack.Pop() // Find
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'find-replace' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					obj3, err := stack.Pop() // Original string
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'find-replace' operation on a stack with only two items.\n", t.Line, t.Column))
					}

					var replacementStr string
					var findStr string
					var originalStr string

					switch obj1.(type) {
					case *MShellString:
						replacementStr = obj1.(*MShellString).Content
					case *MShellLiteral:
						replacementStr = obj1.(*MShellLiteral).LiteralText
					case *MShellPath:
						replacementStr = obj1.(*MShellPath).Path
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot find-replace with a %s as the replacement string.\n", t.Line, t.Column, obj1.TypeName()))
					}

					switch obj2.(type) {
					case *MShellString:
						findStr = obj2.(*MShellString).Content
					case *MShellLiteral:
						findStr = obj2.(*MShellLiteral).LiteralText
					case *MShellPath:
						findStr = obj2.(*MShellPath).Path
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot find-replace with a %s as the find string.\n", t.Line, t.Column, obj2.TypeName()))
					}

					switch obj3.(type) {
					case *MShellString:
						originalStr = obj3.(*MShellString).Content
					case *MShellLiteral:
						originalStr = obj3.(*MShellLiteral).LiteralText
					case *MShellPath:
						originalStr = obj3.(*MShellPath).Path
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot find-replace with a %s as the original string.\n", t.Line, t.Column, obj3.TypeName()))
					}

					stack.Push(&MShellString{strings.Replace(originalStr, findStr, replacementStr, -1)})

				} else if t.Lexeme == "split" {
					delimiter, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'split' operation on an empty stack.\n", t.Line, t.Column))
					}

					strLiteral, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'split' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					var delimiterStr string
					var strToSplit string

					switch delimiter.(type) {
					case *MShellString:
						delimiterStr = delimiter.(*MShellString).Content
					case *MShellLiteral:
						delimiterStr = delimiter.(*MShellLiteral).LiteralText
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot split with a %s.\n", t.Line, t.Column, delimiter.TypeName()))
					}

					switch strLiteral.(type) {
					case *MShellString:
						strToSplit = strLiteral.(*MShellString).Content
					case *MShellLiteral:
						strToSplit = strLiteral.(*MShellLiteral).LiteralText
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot split a %s.\n", t.Line, t.Column, strLiteral.TypeName()))
					}

					split := strings.Split(strToSplit, delimiterStr)
					newList := NewList(len(split))
					for i, item := range split {
						newList.Items[i] = &MShellString{item}
					}
					stack.Push(newList)
				} else if t.Lexeme == "wsplit" {
					// Split on whitespace
					obj, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'wsplit' operation on an empty stack.\n", t.Line, t.Column))
					}

					switch obj.(type) {
					case *MShellString:
						split := strings.Fields(obj.(*MShellString).Content)
						newList := NewList(len(split))
						for i, item := range split {
							newList.Items[i] = &MShellString{item}
						}

						stack.Push(newList)
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot split a %s.\n", t.Line, t.Column, obj.TypeName()))
					}
				} else if t.Lexeme == "join" {
					delimiter, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'join' operation on an empty stack.\n", t.Line, t.Column))
					}

					list, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'join' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					var delimiterStr string
					var listItems []string

					switch delimiter.(type) {
					case *MShellString:
						delimiterStr = delimiter.(*MShellString).Content
					case *MShellLiteral:
						delimiterStr = delimiter.(*MShellLiteral).LiteralText
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot join with a %s.\n", t.Line, t.Column, delimiter.TypeName()))
					}

					switch list.(type) {
					case *MShellList:
						for _, item := range list.(*MShellList).Items {
							switch item.(type) {
							case *MShellString:
								listItems = append(listItems, item.(*MShellString).Content)
							case *MShellLiteral:
								listItems = append(listItems, item.(*MShellLiteral).LiteralText)
							default:
								return FailWithMessage(fmt.Sprintf("%d:%d: Cannot join a list with a %s inside (%s).\n", t.Line, t.Column, item.TypeName(), item.DebugString()))
							}
						}
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot join a %s (%s).\n", t.Line, t.Column, list.TypeName(), list.DebugString()))
					}

					stack.Push(&MShellString{strings.Join(listItems, delimiterStr)})
				} else if t.Lexeme == "lines" {
					obj, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot evaluate 'lines' on an empty stack.\n", t.Line, t.Column))
					}

					s1, ok := obj.(*MShellString)
					if !ok {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot evaluate 'lines' on a %s.\n", t.Line, t.Column, obj.TypeName()))
					}

					// TODO: Maybe reuse a scanner?
					scanner := bufio.NewScanner(strings.NewReader(s1.Content))
					newList := NewList(0)
					for scanner.Scan() {
						newList.Items = append(newList.Items, &MShellString{scanner.Text()})
					}

					stack.Push(newList)
				} else if t.Lexeme == "setAt" {
					// Expected stack:
					// List item index
					// Index 0 based, negative indexes allowed
					obj1, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'setAt' operation on an empty stack.\n", t.Line, t.Column))
					}

					obj2, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'setAt' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					obj3, err := stack.Peek()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'setAt' operation on a stack with only two items.\n", t.Line, t.Column))
					}

					obj1Index, ok := obj1.(*MShellInt)
					if !ok {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot set at a non-integer index.\n", t.Line, t.Column))
					}

					obj3List, ok := obj3.(*MShellList)
					if !ok {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot set into a non-list.\n", t.Line, t.Column))
					}

					if obj1Index.Value < 0 {
						obj1Index.Value = len(obj3List.Items) + obj1Index.Value
					}

					if obj1Index.Value < 0 || obj1Index.Value >= len(obj3List.Items) {
						return FailWithMessage(fmt.Sprintf("%d:%d: Index out of range for 'setAt'.\n", t.Line, t.Column))
					}

					obj3List.Items[obj1Index.Value] = obj2
				} else if t.Lexeme == "insert" {
					// Expected stack:
					// List item index
					// Index 0 based, negative indexes allowed
					obj1, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'insert' operation on an empty stack.\n", t.Line, t.Column))
					}

					obj2, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'insert' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					obj3, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'insert' operation on a stack with only two items.\n", t.Line, t.Column))
					}

					obj1Index, ok := obj1.(*MShellInt)
					if !ok {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot insert at a non-integer index.\n", t.Line, t.Column))
					}

					obj3List, ok := obj3.(*MShellList)
					if !ok {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot insert into a non-list.\n", t.Line, t.Column))
					}

					if obj1Index.Value < 0 {
						obj1Index.Value = len(obj3List.Items) + obj1Index.Value
					}

					if obj1Index.Value < 0 || obj1Index.Value > len(obj3List.Items) {
						return FailWithMessage(fmt.Sprintf("%d:%d: Index out of range for 'insert'.\n", t.Line, t.Column))
					}

					obj3List.Items = append(obj3List.Items[:obj1Index.Value], append([]MShellObject{obj2}, obj3List.Items[obj1Index.Value:]...)...)
					stack.Push(obj3List)
				} else if t.Lexeme == "del" {
					obj1, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'del' operation on an empty stack.\n", t.Line, t.Column))
					}

					obj2, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'del' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					switch obj1.(type) {
					case *MShellInt:
						switch obj2.(type) {
						case *MShellList:
							index := obj1.(*MShellInt).Value
							if index < 0 {
								index = len(obj2.(*MShellList).Items) + index
							}

							if index < 0 || index >= len(obj2.(*MShellList).Items) {
								return FailWithMessage(fmt.Sprintf("%d:%d: Index out of range for 'del'.\n", t.Line, t.Column))
							}
							obj2.(*MShellList).Items = append(obj2.(*MShellList).Items[:index], obj2.(*MShellList).Items[index+1:]...)
							stack.Push(obj2)
						default:
							return FailWithMessage(fmt.Sprintf("%d:%d: Cannot delete from a %s.\n", t.Line, t.Column, obj2.TypeName()))
						}
					case *MShellList:
						switch obj2.(type) {
						case *MShellInt:
							index := obj2.(*MShellInt).Value
							if index < 0 {
								index = len(obj1.(*MShellList).Items) + index
							}
							if index < 0 || index >= len(obj1.(*MShellList).Items) {
								return FailWithMessage(fmt.Sprintf("%d:%d: Index out of range for 'del'.\n", t.Line, t.Column))
							}
							obj1.(*MShellList).Items = append(obj1.(*MShellList).Items[:index], obj1.(*MShellList).Items[index+1:]...)
							stack.Push(obj1)
						default:
							return FailWithMessage(fmt.Sprintf("%d:%d: Cannot delete from a %s.\n", t.Line, t.Column, obj2.TypeName()))
						}
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot delete from a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}
				} else if t.Lexeme == "readFile" {
					obj1, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'readFile' operation on an empty stack.\n", t.Line, t.Column))
					}

					switch obj1.(type) {
					case *MShellString:
						content, err := os.ReadFile(obj1.(*MShellString).Content)
						if err != nil {
							return FailWithMessage(fmt.Sprintf("%d:%d: Error reading file: %s\n", t.Line, t.Column, err.Error()))
						}
						stack.Push(&MShellString{string(content)})
					case *MShellLiteral:
						content, err := os.ReadFile(obj1.(*MShellLiteral).LiteralText)
						if err != nil {
							return FailWithMessage(fmt.Sprintf("%d:%d: Error reading file: %s\n", t.Line, t.Column, err.Error()))
						}
						stack.Push(&MShellString{string(content)})
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot read from a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}
				} else if t.Lexeme == "cd" {
					obj, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'cd' operation on an empty stack.\n", t.Line, t.Column))
					}

					var dir string

					switch obj.(type) {
					case *MShellString:
						dir = obj.(*MShellString).Content
					case *MShellLiteral:
						dir = obj.(*MShellLiteral).LiteralText
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot cd to a %s.\n", t.Line, t.Column, obj.TypeName()))
					}

					// Update the OLDPWD variable
					oldPwd, err := os.Getwd()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Error getting current directory: %s\n", t.Line, t.Column, err.Error()))
					}

					err = os.Setenv("OLDPWD", oldPwd)
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Error setting OLDPWD: %s\n", t.Line, t.Column, err.Error()))
					}

					context.Variables["OLDPWD"] = &MShellString{oldPwd}

					err = os.Chdir(dir)
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Error changing directory: %s\n", t.Line, t.Column, err.Error()))
					}

					// Update the PWD variable
					pwd, err := os.Getwd()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Error getting current directory: %s\n", t.Line, t.Column, err.Error()))
					}

					err = os.Setenv("PWD", pwd)
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Error setting PWD: %s\n", t.Line, t.Column, err.Error()))
					}

					context.Variables["PWD"] = &MShellString{pwd}
				} else if t.Lexeme == "in" {
					substring, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'in' operation on an empty stack.\n", t.Line, t.Column))
					}

					totalString, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'in' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					var substringText string
					var totalStringText string

					switch substring.(type) {
					case *MShellString:
						substringText = substring.(*MShellString).Content
					case *MShellLiteral:
						substringText = substring.(*MShellLiteral).LiteralText
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot search for a %s.\n", t.Line, t.Column, substring.TypeName()))
					}

					switch totalString.(type) {
					case *MShellString:
						totalStringText = totalString.(*MShellString).Content
					case *MShellLiteral:
						totalStringText = totalString.(*MShellLiteral).LiteralText
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot search in a %s.\n", t.Line, t.Column, totalString.TypeName()))
					}

					stack.Push(&MShellBool{strings.Contains(totalStringText, substringText)})
				} else if t.Lexeme == "/" {
					obj1, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '/' operation on an empty stack.\n", t.Line, t.Column))
					}

					obj2, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '/' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					if !obj1.IsNumeric() || !obj2.IsNumeric() {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot divide a %s and a %s.\n", t.Line, t.Column, obj2.TypeName(), obj1.TypeName()))
					}

					switch obj1.(type) {
					case *MShellInt:
						if obj1.(*MShellInt).Value == 0 {
							return FailWithMessage(fmt.Sprintf("%d:%d: Cannot divide by zero.\n", t.Line, t.Column))
						}
						switch obj2.(type) {
						case *MShellInt:
							stack.Push(&MShellInt{obj2.(*MShellInt).Value / obj1.(*MShellInt).Value})
						case *MShellFloat:
							stack.Push(&MShellFloat{float64(obj2.(*MShellFloat).Value) / float64(obj1.(*MShellInt).Value)})
						}
					case *MShellFloat:
						if obj1.(*MShellFloat).Value == 0 {
							return FailWithMessage(fmt.Sprintf("%d:%d: Cannot divide by zero.\n", t.Line, t.Column))
						}

						switch obj2.(type) {
						case *MShellInt:
							stack.Push(&MShellFloat{float64(obj2.(*MShellInt).Value) / obj1.(*MShellFloat).Value})
						case *MShellFloat:
							stack.Push(&MShellFloat{obj2.(*MShellFloat).Value / obj1.(*MShellFloat).Value})
						}
					}
				} else if t.Lexeme == "exit" {
					exitCode, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'exit' operation on an empty stack. If you are trying to exit out of the interactive shell, you are probably looking to do `0 exit`.\n", t.Line, t.Column))
					}

					exitInt, ok := exitCode.(*MShellInt)
					if !ok {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot exit with a %s.\n", t.Line, t.Column, exitCode.TypeName()))
					}

					if exitInt.Value < 0 || exitInt.Value > 255 {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot exit with a value outside of 0-255.\n", t.Line, t.Column))
					}

					if exitInt.Value == 0 {
						return EvalResult{true, -1, 0, true}
					} else {
						return EvalResult{false, -1, exitInt.Value, true}
					}
				} else if t.Lexeme == "*" {
					obj1, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '*' operation on an empty stack.\n", t.Line, t.Column))
					}

					obj2, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '*' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					if !obj1.IsNumeric() || !obj2.IsNumeric() {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot multiply a %s and a %s. If you are looking for wildcard glob, you want `\"*\" glob`.\n", t.Line, t.Column, obj2.TypeName(), obj1.TypeName()))
					}

					switch obj1.(type) {
					case *MShellInt:
						switch obj2.(type) {
						case *MShellInt:
							stack.Push(&MShellInt{obj2.(*MShellInt).Value * obj1.(*MShellInt).Value})
						case *MShellFloat:
							stack.Push(&MShellFloat{obj2.(*MShellFloat).Value * float64(obj1.(*MShellInt).Value)})
						}
					case *MShellFloat:
						switch obj2.(type) {
						case *MShellInt:
							stack.Push(&MShellFloat{float64(obj2.(*MShellInt).Value) * float64(obj1.(*MShellFloat).Value)})
						case *MShellFloat:
							stack.Push(&MShellFloat{obj2.(*MShellFloat).Value * obj1.(*MShellFloat).Value})
						}
					}
				} else if t.Lexeme == "toFloat" {
					obj, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'toFloat' operation on an empty stack.\n", t.Line, t.Column))
					}

					switch obj.(type) {
					case *MShellString:
						floatVal, err := strconv.ParseFloat(obj.(*MShellString).Content, 64)
						if err != nil {
							return FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert %s to float: %s\n", t.Line, t.Column, obj.(*MShellString).Content, err.Error()))
						}
						stack.Push(&MShellFloat{floatVal})
						// I don't believe checking for literal is required, because it should have been parsed as a float to start with?
					case *MShellInt:
						stack.Push(&MShellFloat{float64(obj.(*MShellInt).Value)})
					case *MShellFloat:
						stack.Push(obj)
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s to a float.\n", t.Line, t.Column, obj.TypeName()))
					}
				} else if t.Lexeme == "toInt" {
					obj, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'toInt' operation on an empty stack.\n", t.Line, t.Column))
					}

					switch obj.(type) {
					case *MShellString:
						intVal, err := strconv.Atoi(obj.(*MShellString).Content)
						if err != nil {
							return FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert %s to int %s\n", t.Line, t.Column, obj.(*MShellString).Content, err.Error()))
						}
						stack.Push(&MShellInt{intVal})
						// I don't believe checking for literal is required, because it should have been parsed as a float to start with?
					case *MShellInt:
						stack.Push(obj)
					case *MShellFloat:
						stack.Push(&MShellInt{int(obj.(*MShellFloat).Value)})
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s to a float.\n", t.Line, t.Column, obj.TypeName()))
					}
				} else if t.Lexeme == "toDt" {
					dateStrObj, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'toDt' operation on an empty stack.\n", t.Line, t.Column))
					}

					var dateStr string
					switch dateStrObj.(type) {
					case *MShellString:
						dateStr = dateStrObj.(*MShellString).Content
					case *MShellLiteral:
						dateStr = dateStrObj.(*MShellLiteral).LiteralText
					case *MShellDateTime:
						stack.Push(dateStrObj)
						continue MainLoop
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s to a datetime.\n", t.Line, t.Column, dateStrObj.TypeName()))
					}

					// TODO: Don't make a new lexer object each time.
					parsedTime, err := ParseDateTime(dateStr)
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Error parsing date time: %s\n", t.Line, t.Column, err.Error()))
					}

					stack.Push(&MShellDateTime{Time: parsedTime, Token: t})
				} else if t.Lexeme == "files" || t.Lexeme == "dirs" {
					// Dump all the files in the current directory to the stack. No sub-directories.
					files, err := os.ReadDir(".")
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Error reading current directory: %s\n", t.Line, t.Column, err.Error()))
					}

					newList := 	&MShellList{
						Items:                 make([]MShellObject, 0, len(files)),
						StdinBehavior:         STDIN_NONE,
						StandardInputContents: "",
						StandardInputFile:     "",
						StandardOutputFile:    "",
						StandardErrorFile:     "",
						StdoutBehavior:        STDOUT_NONE,
					}

					if t.Lexeme == "files" {
						for _, file := range files {
							if !file.IsDir() {
								newList.Items = append(newList.Items, &MShellPath{file.Name()})
							}
						}
					} else {
						for _, file := range files {
							if file.IsDir() {
								newList.Items = append(newList.Items, &MShellPath{file.Name()})
							}
						}
					}
					stack.Push(newList)
				} else if t.Lexeme == "mkdir" || t.Lexeme == "mkdirp" {
					obj1, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'mkdir' operation on an empty stack.\n", t.Line, t.Column))
					}

					var dirPath string
					switch obj1.(type) {
					case *MShellString:
						dirPath = obj1.(*MShellString).Content
					case *MShellLiteral:
						dirPath = obj1.(*MShellLiteral).LiteralText
					case *MShellPath:
						dirPath = obj1.(*MShellPath).Path
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot make a directory with a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					if t.Lexeme == "mkdir" {
						err = os.Mkdir(dirPath, 0755)
					} else if t.Lexeme == "mkdirp" {
						err = os.MkdirAll(dirPath, 0755)
					}
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Error creating directory: %s\n", t.Line, t.Column, err.Error()))
					}
				} else if t.Lexeme == "~" || strings.HasPrefix(t.Lexeme, "~/") {
					// Only do tilde expansion
					homeDir, err := os.UserHomeDir()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Error expanding ~: %s\n", t.Line, t.Column, err.Error()))
					}

					var tildeExpanded string
					if t.Lexeme == "~" {
						tildeExpanded = homeDir
					} else {
						tildeExpanded = filepath.Join(homeDir, t.Lexeme[2:])
					}

					stack.Push(&MShellString{tildeExpanded})
				} else if t.Lexeme == "pwd" {
					pwd, err := os.Getwd()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Error getting current directory: %s\n", t.Line, t.Column, err.Error()))
					}
					stack.Push(&MShellString{pwd})
				} else if t.Lexeme == "psub" {
					obj1, err := stack.Pop()
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'psub' operation on an empty stack.\n", t.Line, t.Column))
					}

					// Do process substitution with temporary files
					// Create a temporary file
					tmpfile, err := os.CreateTemp("", "msh-")
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Error creating temporary file: %s\n", t.Line, t.Column, err.Error()))
					}
					// Close the file
					defer tmpfile.Close()
					registerTempFileForCleanup(tmpfile.Name())

					// Write the contents of the object to the temporary file
					switch obj1.(type) {
					case *MShellString:
						_, err = tmpfile.WriteString(obj1.(*MShellString).Content)
						if err != nil {
							return FailWithMessage(fmt.Sprintf("%d:%d: Error writing to temporary file: %s\n", t.Line, t.Column, err.Error()))
						}
					case *MShellLiteral:
						_, err = tmpfile.WriteString(obj1.(*MShellLiteral).LiteralText)
						if err != nil {
							return FailWithMessage(fmt.Sprintf("%d:%d: Error writing to temporary file: %s\n", t.Line, t.Column, err.Error()))
						}
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'psub' with a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					stack.Push(&MShellString{tmpfile.Name()})
				}  else {
					stack.Push(&MShellLiteral{t.Lexeme})
				}
			} else if t.Type == LEFT_SQUARE_BRACKET { // Token Type
				return FailWithMessage(fmt.Sprintf("%d:%d: Found unexpected left square bracket.\n", t.Line, t.Column))
			} else if t.Type == LEFT_PAREN { // Token Type
				return FailWithMessage(fmt.Sprintf("%d:%d: Found unexpected left parenthesis.\n", t.Line, t.Column))
			} else if t.Type == EXECUTE || t.Type == QUESTION { // Token Type
				top, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot execute an empty stack.\n", t.Line, t.Column))
				}

				// Switch on type
				var result EvalResult
				var exitCode int
				var stdout string

				switch top.(type) {
				case *MShellList:
					result, exitCode, stdout = RunProcess(*top.(*MShellList), context, state)
				case *MShellPipe:
					result, exitCode, stdout = state.RunPipeline(*top.(*MShellPipe), context, stack)
				default:
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot execute a non-list object. Found %s %s\n", t.Line, t.Column, top.TypeName(), top.DebugString()))
				}

				if state.StopOnError && exitCode != 0 {
					// Exit completely, with that exit code, don't need to print a different message. Usually the command itself will have printed an error.
					return EvalResult{false, -1, exitCode, false}
				}

				if !result.Success {
					return result
				}

				var stdoutBehavior StdoutBehavior
				switch top.(type) {
				case *MShellList:
					stdoutBehavior = top.(*MShellList).StdoutBehavior
				case *MShellPipe:
					stdoutBehavior = top.(*MShellPipe).StdoutBehavior
				}

				if stdoutBehavior == STDOUT_LINES {
					newMShellList := NewList(0)
					var scanner *bufio.Scanner
					scanner = bufio.NewScanner(strings.NewReader(stdout))
					for scanner.Scan() {
						newMShellList.Items = append(newMShellList.Items, &MShellString{scanner.Text()})
					}
					stack.Push(newMShellList)
				} else if stdoutBehavior == STDOUT_STRIPPED {
					stripped := strings.TrimSpace(stdout)
					stack.Push(&MShellString{stripped})
				} else if stdoutBehavior == STDOUT_COMPLETE {
					stack.Push(&MShellString{stdout})
				}

				// Push the exit code onto the stack if a question was used to execute
				if t.Type == QUESTION {
					stack.Push(&MShellInt{exitCode})
				}
			} else if t.Type == TRUE { // Token Type
				stack.Push(&MShellBool{true})
			} else if t.Type == FALSE { // Token Type
				stack.Push(&MShellBool{false})
			} else if t.Type == INTEGER { // Token Type
				intVal, err := strconv.Atoi(t.Lexeme)
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Error parsing integer: %s\n", t.Line, t.Column, err.Error()))
				}

				stack.Push(&MShellInt{intVal})
			} else if t.Type == STRING { // Token Type
				parsedString, err := ParseRawString(t.Lexeme)
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Error parsing string: %s\n", t.Line, t.Column, err.Error()))
				}
				stack.Push(&MShellString{parsedString})
			} else if t.Type == SINGLEQUOTESTRING { // Token Type
				stack.Push(&MShellString{t.Lexeme[1 : len(t.Lexeme)-1]})
			} else if t.Type == IFF {
				falseQuoteObj, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do an 'iff' on a stack with only two items.\n", t.Line, t.Column))
				}

				trueQuoteObj, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do an 'iff' on a stack with only one item.\n", t.Line, t.Column))
				}

				boolObj, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do an 'iff' on an empty stack.\n", t.Line, t.Column))
				}


				trueQuote, ok := trueQuoteObj.(*MShellQuotation)
				if !ok {
					return FailWithMessage(fmt.Sprintf("%d:%d: Expected a quotation for iff, received a %s.\n", t.Line, t.Column, trueQuoteObj.TypeName()))
				}

				falseQuote, ok := falseQuoteObj.(*MShellQuotation)
				if !ok {
					return FailWithMessage(fmt.Sprintf("%d:%d: Expected a quotation for iff, received a %s.\n", t.Line, t.Column, falseQuoteObj.TypeName()))
				}

				var condition bool
				switch boolObj.(type) {
				case *MShellInt:
					intVal := boolObj.(*MShellInt)
					condition = intVal.Value == 0
				case *MShellBool:
					condition = boolObj.(*MShellBool).Value
				default:
					return FailWithMessage(fmt.Sprintf("%d:%d: Expected an integer or boolean for iff, received a %s.\n", t.Line, t.Column, boolObj.TypeName()))
				}

				var quoteToExecute *MShellQuotation
				if condition {
					quoteToExecute = trueQuote
				} else {
					quoteToExecute = falseQuote
				}

				qContext, err := quoteToExecute.BuildExecutionContext(&context)
				defer qContext.Close()
				if err != nil {
					return FailWithMessage(err.Error())
				}

				result := state.Evaluate(quoteToExecute.Tokens, stack, (*qContext), definitions, callStack, CallStackItem{trueQuote, "quote", CALLSTACKQUOTE})

				if !result.Success || result.BreakNum != -1 || result.ExitCalled  {
					return result
				}

			} else if t.Type == IF { // Token Type
				obj, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do an 'if' on an empty stack.\n", t.Line, t.Column))
				}

				list, ok := obj.(*MShellList)
				if !ok {
					return FailWithMessage(fmt.Sprintf("%d:%d: Argument for if expected to be a list of quoations, received a %s\n", t.Line, t.Column, obj.TypeName()))
				}

				if len(list.Items) < 2 {
					return FailWithMessage(fmt.Sprintf("%d:%d: If statement requires a list with at least 2 items. Found %d.\n", t.Line, t.Column, len(list.Items)))
				}

				// Check that all items are quotations
				for i, item := range list.Items {
					if _, ok := item.(*MShellQuotation); !ok {
						return FailWithMessage(fmt.Sprintf("%d:%d: Item %d in if statement is not a quotation.\n", t.Line, t.Column, i))
					}
				}

				trueIndex := -1

			ListLoop:
				for i := 0; i < len(list.Items)-1; i += 2 {
					quotation := list.Items[i].(*MShellQuotation)

					result := state.Evaluate(quotation.Tokens, stack, context, definitions, callStack, CallStackItem{quotation, "quote", CALLSTACKQUOTE})

					if !result.Success || result.ExitCalled {
						return result
					}

					if result.BreakNum > 0 {
						return FailWithMessage("Encountered break within if statement.\n")
					}

					top, err := stack.Pop()
					if err != nil {
						conditionNum := i/2 + 1
						return FailWithMessage(fmt.Sprintf("%d:%d: Found an empty stack when evaluating condition #%d .\n", t.Line, t.Column, conditionNum))
					}

					// Check for either integer or boolean
					switch top.(type) {
					case *MShellInt:
						if top.(*MShellInt).Value == 0 {
							trueIndex = i
							break ListLoop
						}
					case *MShellBool:
						if top.(*MShellBool).Value {
							trueIndex = i
							break ListLoop
						}
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Expected an integer or boolean for condition #%d, received a %s.\n", t.Line, t.Column, i/2+1, top.TypeName()))
					}
				}

				if trueIndex > -1 {
					quotation := list.Items[trueIndex+1].(*MShellQuotation)

					result := state.Evaluate(quotation.Tokens, stack, context, definitions, callStack, CallStackItem{quotation, "quote", CALLSTACKQUOTE})

					// If we encounter a break, we should return it up the stack
					if !result.Success || result.BreakNum != -1 || result.ExitCalled {
						return result
					}
				} else if len(list.Items)%2 == 1 { // Try to find a final else statement, will be the last item in the list if odd number of items
					quotation := list.Items[len(list.Items)-1].(*MShellQuotation)

					result := state.Evaluate(quotation.Tokens, stack, context, definitions, callStack, CallStackItem{quotation, "quote", CALLSTACKQUOTE})

					if !result.Success || result.BreakNum != -1 || result.ExitCalled {
						return result
					}
				}
			} else if t.Type == PLUS { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '+' operation on an empty stack.\n", t.Line, t.Column))
				}

				obj2, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '+' operation on a stack with only one item.\n", t.Line, t.Column))
				}

				switch obj1.(type) {
				case *MShellInt:
					switch obj2.(type) {
					case *MShellInt:
						stack.Push(&MShellInt{obj2.(*MShellInt).Value + obj1.(*MShellInt).Value})
					case *MShellFloat:
						stack.Push(&MShellFloat{float64(obj2.(*MShellFloat).Value) + float64(obj1.(*MShellInt).Value)})
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot add an integer to a %s (%s).\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString()))
					}
				case *MShellFloat:
					switch obj2.(type) {
					case *MShellFloat:
						stack.Push(&MShellFloat{obj2.(*MShellFloat).Value + obj1.(*MShellFloat).Value})
					case *MShellInt:
						stack.Push(&MShellFloat{float64(obj2.(*MShellInt).Value) + obj1.(*MShellFloat).Value})
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot add a float to a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				case *MShellString:
					switch obj2.(type) {
					case *MShellString:
						stack.Push(&MShellString{obj2.(*MShellString).Content + obj1.(*MShellString).Content})
					case *MShellLiteral:
						stack.Push(&MShellString{obj2.(*MShellLiteral).LiteralText + obj1.(*MShellString).Content})
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot add a string to a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				case *MShellLiteral:
					switch obj2.(type) {
					case *MShellString:
						stack.Push(&MShellString{obj2.(*MShellString).Content + obj1.(*MShellLiteral).LiteralText})
					case *MShellLiteral:
						stack.Push(&MShellString{obj2.(*MShellLiteral).LiteralText + obj1.(*MShellLiteral).LiteralText})
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot add a literal (%s) to a %s.\n", t.Line, t.Column, obj1.DebugString(), obj2.TypeName()))
					}
				case *MShellList:
					switch obj2.(type) {
					case *MShellList:
						newList := NewList(len(obj2.(*MShellList).Items)+len(obj1.(*MShellList).Items))
						copy(newList.Items, obj2.(*MShellList).Items)
						copy(newList.Items[len(obj2.(*MShellList).Items):], obj1.(*MShellList).Items)
						stack.Push(newList)
					}
				default:
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '+' between a %s and a %s.\n", t.Line, t.Column, obj2.TypeName(), obj1.TypeName()))
				}
			} else if t.Type == MINUS { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '-' operation on an empty stack.\n", t.Line, t.Column))
				}

				obj2, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '-' operation on a stack with only one item.\n", t.Line, t.Column))
				}

				switch obj1.(type) {
				case *MShellInt:
					switch obj2.(type) {
					case *MShellInt:
						stack.Push(&MShellInt{obj2.(*MShellInt).Value - obj1.(*MShellInt).Value})
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot subtract an integer from a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				default:
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '-' to a %s and %s.\n", t.Line, t.Column, obj2.TypeName(), obj1.TypeName()))
				}
			} else if t.Type == AND || t.Type == OR { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
				}

				obj2, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on a stack with only one item.\n", t.Line, t.Column, t.Lexeme))
				}

				switch obj1.(type) {
				case *MShellBool:
					switch obj2.(type) {
					case *MShellBool:
						if t.Type == AND {
							stack.Push(&MShellBool{obj2.(*MShellBool).Value && obj1.(*MShellBool).Value})
						} else {
							stack.Push(&MShellBool{obj2.(*MShellBool).Value || obj1.(*MShellBool).Value})
						}
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '%s' to a %s and %s.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName(), obj1.TypeName()))
					}
				default:
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '%s' to a %s and %s.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName(), obj1.TypeName()))
				}
			} else if t.Type == NOT { // Token Type
				obj, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
				}

				switch obj.(type) {
				case *MShellBool:
					stack.Push(&MShellBool{!obj.(*MShellBool).Value})
				default:
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '%s' to a %s.\n", t.Line, t.Column, t.Lexeme, obj.TypeName()))
				}
			} else if t.Type == GREATERTHANOREQUAL || t.Type == LESSTHANOREQUAL { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
				}

				obj2, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on a stack with only one item.\n", t.Line, t.Column, t.Lexeme))
				}

				if obj1.IsNumeric() && obj2.IsNumeric() {
					if t.Type == GREATERTHANOREQUAL {
						stack.Push(&MShellBool{obj2.FloatNumeric() >= obj1.FloatNumeric()})
					} else {
						stack.Push(&MShellBool{obj2.FloatNumeric() <= obj1.FloatNumeric()})
					}
				} else {

					obj1Date, ok1 := obj1.(*MShellDateTime)
					obj2Date, ok2 := obj2.(*MShellDateTime)

					if ok1 && ok2 {
						if t.Type == GREATERTHANOREQUAL {
							stack.Push(&MShellBool{obj2Date.Time.After(obj1Date.Time) || obj2Date.Time.Equal(obj1Date.Time)})
						} else {
							stack.Push(&MShellBool{obj2Date.Time.Before(obj1Date.Time) || obj2Date.Time.Equal(obj1Date.Time)})
						}
					} else {
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '%s' to a %s and a %s.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName(), obj1.TypeName()))
					}
				}
			} else if t.Type == GREATERTHAN || t.Type == LESSTHAN { // Token Type
				// This can either be normal comparison for numerics, or it's a redirect on a list or quotation.
				obj1, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
				}

				obj2, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on a stack with only one item.\n", t.Line, t.Column, t.Lexeme))
				}

				if obj1.IsNumeric() && obj2.IsNumeric() {
					if t.Type == GREATERTHAN {
						stack.Push(&MShellBool{obj2.FloatNumeric() > obj1.FloatNumeric()})
					} else {
						stack.Push(&MShellBool{obj2.FloatNumeric() < obj1.FloatNumeric()})
					}
				} else {
					switch obj1.(type) {
					case *MShellString:
						switch obj2.(type) {
						case *MShellList:
							if t.Type == GREATERTHAN {
								// Fail, and tell user to use path literal instead of string.
								return FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a %s (%s) to a %s (%s). Use a path literal instead.\n", t.Line, t.Column, obj1.TypeName(), obj1.(*MShellString).Content, obj2.TypeName(), obj2.DebugString()))
							} else { // LESSTHAN, input redirection
								obj2.(*MShellList).StdinBehavior = STDIN_CONTENT
								obj2.(*MShellList).StandardInputContents = obj1.(*MShellString).Content
							}
							stack.Push(obj2)
						case *MShellQuotation:
							if t.Type == GREATERTHAN {
								return FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a %s (%s) to a %s (%s). Use a path literal instead.\n", t.Line, t.Column, obj1.TypeName(), obj1.(*MShellString).Content, obj2.TypeName(), obj2.DebugString()))
							} else { // LESSTHAN, input redirection
								obj2.(*MShellQuotation).StdinBehavior = STDIN_CONTENT
								obj2.(*MShellQuotation).StandardInputContents = obj1.(*MShellString).Content
							}
							stack.Push(obj2)
						case *MShellPipe:
							return FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a string (%s) to a Pipe (%s). Add the redirection to the final item in the pipeline.\n", t.Line, t.Column, obj1.DebugString(), obj2.DebugString()))
						default:
							return FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a string (%s) to a %s (%s).\n", t.Line, t.Column, obj1.DebugString(), obj2.TypeName(), obj2.DebugString()))
						}
					case *MShellLiteral:
						switch obj2.(type) {
						case *MShellList:
							if t.Type == GREATERTHAN {
								return FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a %s (%s) to a %s (%s). Use a path literal instead.\n", t.Line, t.Column, obj1.TypeName(), obj1.(*MShellLiteral).LiteralText, obj2.TypeName(), obj2.DebugString()))
							} else { // LESSTHAN, input redirection
								obj2.(*MShellList).StdinBehavior = STDIN_CONTENT
								obj2.(*MShellList).StandardInputFile = obj1.(*MShellLiteral).LiteralText
							}
							stack.Push(obj2)
						case *MShellQuotation:
							if t.Type == GREATERTHAN {
								return FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a %s (%s) to a %s (%s). Use a path literal instead.\n", t.Line, t.Column, obj1.TypeName(), obj1.(*MShellString).Content, obj2.TypeName(), obj2.DebugString()))
							} else {
								obj2.(*MShellQuotation).StdinBehavior = STDIN_CONTENT
								obj2.(*MShellQuotation).StandardInputContents = obj1.(*MShellLiteral).LiteralText
							}
						default:
							return FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a %s (%s) to a %s (%s).\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString(), obj2.TypeName(), obj2.DebugString()))
						}

					case *MShellPath:
						switch obj2.(type) {
						case *MShellList:
							if t.Type == GREATERTHAN {
								obj2.(*MShellList).StandardOutputFile = obj1.(*MShellPath).Path
							} else { // LESSTHAN, input redirection
								obj2.(*MShellList).StdinBehavior = STDIN_FILE
								obj2.(*MShellList).StandardInputFile = obj1.(*MShellPath).Path
							}
							stack.Push(obj2)
						case *MShellQuotation:
							if t.Type == GREATERTHAN {
								obj2.(*MShellQuotation).StandardOutputFile = obj1.(*MShellPath).Path
							} else {
								obj2.(*MShellQuotation).StdinBehavior = STDIN_FILE
								obj2.(*MShellQuotation).StandardInputFile = obj1.(*MShellPath).Path
							}
							stack.Push(obj2)
						default:
							return FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a path (%s) to a %s (%s).\n", t.Line, t.Column, obj1.DebugString(), obj2.TypeName(), obj2.DebugString()))
						}
					case *MShellDateTime:
						switch obj2.(type) {
						case *MShellDateTime:
							if t.Type == GREATERTHAN {
								stack.Push(&MShellBool{obj2.(*MShellDateTime).Time.After(obj1.(*MShellDateTime).Time)})
							} else {
								stack.Push(&MShellBool{obj2.(*MShellDateTime).Time.Before(obj1.(*MShellDateTime).Time)})
							}
						default:
							return FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a datetime (%s) to a %s (%s).\n", t.Line, t.Column, obj1.DebugString(), obj2.TypeName(), obj2.DebugString()))
						}
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a %s (%s) to a %s (%s).\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString(), obj2.TypeName(), obj2.DebugString()))
					}
				}
			} else if t.Type == STDERRREDIRECT { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect stderr on an empty stack.\n", t.Line, t.Column))
				}
				obj2, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect stderr on a stack with only one item.\n", t.Line, t.Column))
				}

				switch obj1.(type) {
				case *MShellString:
					switch obj2.(type) {
					case *MShellList:
						obj2.(*MShellList).StandardErrorFile = obj1.(*MShellString).Content
						stack.Push(obj2)
					case *MShellQuotation:
						obj2.(*MShellQuotation).StandardErrorFile = obj1.(*MShellString).Content
						stack.Push(obj2)
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect stderr to a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				case *MShellLiteral:
					switch obj2.(type) {
					case *MShellList:
						obj2.(*MShellList).StandardErrorFile = obj1.(*MShellLiteral).LiteralText
						stack.Push(obj2)
					case *MShellQuotation:
						obj2.(*MShellQuotation).StandardErrorFile = obj1.(*MShellLiteral).LiteralText
						stack.Push(obj2)
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect stderr to a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				default:
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect stderr to a %s.\n", t.Line, t.Column, obj1.TypeName()))
				}
			} else if t.Type == ENVSTORE { // Token Type
				obj, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Nothing on stack to set into %s environment variable.\n", t.Line, t.Column, t.Lexeme))
				}

				// Strip off the leading '$' and trailing '!' for the environment variable name
				varName := t.Lexeme[1:len(t.Lexeme) - 1]

				var varValue string
				switch obj.(type) {
				case *MShellString:
					varValue = obj.(*MShellString).Content
				case *MShellLiteral:
					varValue = obj.(*MShellLiteral).LiteralText
				default:
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot export a %s.\n", t.Line, t.Column, obj.TypeName()))
				}

				err = os.Setenv(varName, varValue)
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Could not set the environment variable '%s' to '%s'.\n", t.Line, t.Column, varName, varValue))
				}
			} else if t.Type == ENVCHECK {
				// Strip off the leading '$' and trailing '!' for the environment variable name
				varName := t.Lexeme[1:len(t.Lexeme) - 1]
				_, found := os.LookupEnv(varName)
				stack.Push(&MShellBool{found})
			} else if t.Type == ENVRETREIVE { // Token Type
				envVarName := t.Lexeme[1:len(t.Lexeme)]
				varValue, found := os.LookupEnv(envVarName)

				if !found {
					return FailWithMessage(fmt.Sprintf("%d:%d: Could not get the environment variable '%s'.\n", t.Line, t.Column, envVarName))
				}

				stack.Push(&MShellString{varValue})

			} else if t.Type == VARSTORE { // Token Type
				obj, err := stack.Pop()
				varName := t.Lexeme[0 : len(t.Lexeme)-1] // Remove the trailing !

				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Nothing on stack to store into variable %s.\n", t.Line, t.Column, varName))
				}

				context.Variables[varName] = obj
			} else if t.Type == VARRETRIEVE { // Token Type
				name := t.Lexeme[1:] // Remove the leading @
				obj, found_mshell_variable := context.Variables[name]
				if found_mshell_variable {
					stack.Push(obj)
				} else {
					// Check current environment variables
					envValue, ok := os.LookupEnv(name)
					if ok {
						stack.Push(&MShellString{envValue})
					} else {
						var message strings.Builder
						message.WriteString(fmt.Sprintf("%d:%d: Variable %s not found.\n", t.Line, t.Column, name))
						message.WriteString("Variables:\n")
						for key := range context.Variables {
							message.WriteString(fmt.Sprintf("  %s\n", key))
						}
						return FailWithMessage(message.String())
					}
				}
			} else if t.Type == LOOP { // Token Type
				obj, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do a loop on an empty stack.\n", t.Line, t.Column))
				}

				quotation, ok := obj.(*MShellQuotation)
				if !ok {
					return FailWithMessage(fmt.Sprintf("%d:%d: Argument for loop expected to be a quotation, received a %s\n", t.Line, t.Column, obj.TypeName()))
				}

				if len(quotation.Tokens) == 0 {
					return FailWithMessage(fmt.Sprintf("%d:%d: Loop quotation needs a minimum of one token.\n", t.Line, t.Column))
				}

				loopContext := ExecuteContext{
					StandardInput:  nil,
					StandardOutput: nil,
					Variables:      context.Variables,
				}

				if quotation.StdinBehavior != STDIN_NONE {
					if quotation.StdinBehavior == STDIN_CONTENT {
						loopContext.StandardInput = strings.NewReader(quotation.StandardInputContents)
					} else if quotation.StdinBehavior == STDIN_FILE {
						file, err := os.Open(quotation.StandardInputFile)
						if err != nil {
							return FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s for reading: %s\n", t.Line, t.Column, quotation.StandardInputFile, err.Error()))
						}
						loopContext.StandardInput = file
						defer file.Close()
					} else {
						panic("Unknown stdin behavior")
					}
				}

				if quotation.StandardOutputFile != "" {
					file, err := os.Create(quotation.StandardOutputFile)
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s for writing: %s\n", t.Line, t.Column, quotation.StandardOutputFile, err.Error()))
					}
					loopContext.StandardOutput = file
					defer file.Close()
				}

				maxLoops := 150000
				loopCount := 0
				state.LoopDepth++

				breakDiff := 0

				initialStackSize := len(*stack)

				for loopCount < maxLoops {
					result := state.Evaluate(quotation.Tokens, stack, loopContext, definitions, callStack, CallStackItem{quotation, "quote", CALLSTACKQUOTE})

					if len(*stack) != initialStackSize {
						// If the stack size changed, we have an error.
						return FailWithMessage(fmt.Sprintf("%d:%d: Stack size changed from %d to %d in loop.\n", t.Line, t.Column, initialStackSize, len(*stack)))
					}

					if !result.Success || result.ExitCalled {
						return result
					}

					if result.BreakNum >= 0 {
						breakDiff = state.LoopDepth - result.BreakNum
						if breakDiff >= 0 {
							break
						}
					}

					loopCount++
				}

				if loopCount == maxLoops {
					return FailWithMessage(fmt.Sprintf("%d:%d: Loop exceeded maximum number of iterations (%d).\n", t.Line, t.Column, maxLoops))
				}

				state.LoopDepth--

				// If we are breaking out of an inner loop to an outer loop (breakDiff - 1 > 0), then we need to return an go up the call stack.
				// Else just continue on with tokens after the loop.
				if breakDiff-1 > 0 {
					return EvalResult{true, breakDiff - 1, 0, false}
				}
			} else if t.Type == BREAK { // Token Type
				return EvalResult{true, 1, 0, false}
			} else if t.Type == EQUALS { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '=' operation on an empty stack.\n", t.Line, t.Column))
				}
				obj2, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '=' operation on a stack with only one item.\n", t.Line, t.Column))
				}

				// Implement for integers, strings, datetimes, and bools
				switch obj1.(type) {
				case *MShellInt:
					switch obj2.(type) {
					case *MShellInt:
						stack.Push(&MShellBool{obj2.(*MShellInt).Value == obj1.(*MShellInt).Value})
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot compare equality from an integer to a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				case *MShellString:
					switch obj2.(type) {
					case *MShellString:
						stack.Push(&MShellBool{obj2.(*MShellString).Content == obj1.(*MShellString).Content})
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot compare equality from a string to a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				case *MShellDateTime:
					switch obj2.(type) {
					case *MShellDateTime:
						stack.Push(&MShellBool{obj2.(*MShellDateTime).Time.Equal(obj1.(*MShellDateTime).Time)})
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot compare equality from a datetime to a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				case *MShellBool:
					switch obj2.(type) {
					case *MShellBool:
						stack.Push(&MShellBool{obj2.(*MShellBool).Value == obj1.(*MShellBool).Value})
					default:
						return FailWithMessage(fmt.Sprintf("%d:%d: Cannot compare equality from a bool to a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				default:
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot complete %s with a %s to a %s.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName(), obj1.TypeName()))
				}
			} else if t.Type == INTERPRET { // Token Type
				obj, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot interpret an empty stack.\n", t.Line, t.Column))
				}

				quotation, ok := obj.(*MShellQuotation)
				if !ok {
					return FailWithMessage(fmt.Sprintf("%d:%d: Argument for interpret expected to be a quotation, received a %s (%s)\n", t.Line, t.Column, obj.TypeName(), obj.DebugString()))
				}

				quoteContext, err := quotation.BuildExecutionContext(&context)
				defer quoteContext.Close()

				if err != nil {
					return FailWithMessage(err.Error())
				}

				result := state.Evaluate(quotation.Tokens, stack, (*quoteContext), definitions, callStack,CallStackItem{quotation, "quote", CALLSTACKQUOTE})

				if !result.Success || result.ExitCalled || result.BreakNum > 0 {
					return result
				}
			} else if t.Type == POSITIONAL { // Token Type
				posNum := t.Lexeme[1:]
				posIndex, err := strconv.Atoi(posNum)
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Error parsing positional argument number: %s\n", t.Line, t.Column, err.Error()))
				}

				if posIndex == 0 {
					return FailWithMessage(fmt.Sprintf("%d:%d: Positional argument are 1-based, first argument is $1, not $0.\n", t.Line, t.Column))
				}

				if posIndex < 0 {
					return FailWithMessage(fmt.Sprintf("%d:%d: Positional argument numbers must be positive.\n", t.Line, t.Column))
				}

				if posIndex > len(state.PositionalArgs) {
					return FailWithMessage(fmt.Sprintf("%d:%d: Positional argument %s is greater than the number of arguments provided.\n", t.Line, t.Column, t.Lexeme))
				}

				stack.Push(&MShellString{state.PositionalArgs[posIndex-1]})
			} else if t.Type == PIPE { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
				}

				// obj1 should be a list
				list, ok := obj1.(*MShellList)
				if !ok {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot pipe a %s.\n", t.Line, t.Column, obj1.TypeName()))
				}

				stack.Push(&MShellPipe{*list, list.StdoutBehavior})
			} else if t.Type == READ { // Token Type
				var reader io.Reader
				// Check if what we are reading from is seekable. If so, we can do a buffered read and reset the position.
				// Else, we have to read byte by byte.

				isSeekable := false

				if context.StandardInput == nil {
					reader = os.Stdin
					_, err := reader.(*os.File).Seek(0, io.SeekCurrent)
					isSeekable = err == nil

				} else {
					reader = context.StandardInput
					_, err := reader.(*os.File).Seek(0, io.SeekCurrent)
					isSeekable = err == nil
				}

				if isSeekable {
					// Do a buffered read
					bufferedReader := bufio.NewReader(reader)
					line, err := bufferedReader.ReadString('\n')
					if err != nil {
						if err == io.EOF {
							stack.Push(&MShellString{""})
							stack.Push(&MShellBool{false})
						} else {
							return FailWithMessage(fmt.Sprintf("%d:%d: Error reading from stdin: %s\n", t.Line, t.Column, err.Error()))
						}
					} else {
						// Check if the last character is a '\r' and remove it if it is. Also remove the '\n' itself
						if len(line) > 0 && line[len(line)-1] == '\n' {
							line = line[:len(line)-1]
						}
						if len(line) > 0 && line[len(line)-1] == '\r' {
							line = line[:len(line)-1]
						}

						stack.Push(&MShellString{line})
						stack.Push(&MShellBool{true})
					}

					// Reset the position of the reader to the position after the read
					offset, err := reader.(*os.File).Seek(0, io.SeekCurrent)
					if err != nil {
						return FailWithMessage(fmt.Sprintf("%d:%d: Error resetting position of reader: %s\n", t.Line, t.Column, err.Error()))
					}
					remainingInBuffer := bufferedReader.Buffered()
					// fmt.Fprintf(os.Stderr, "Offset: %d, Remaining in buffer: %d\n", offset, remainingInBuffer)
					newPosition := offset - int64(remainingInBuffer)
					// fmt.Fprintf(os.Stderr, "New position: %d\n", newPosition)
					_, err = reader.(*os.File).Seek(newPosition, io.SeekStart)
				} else {
					// Do a byte by byte read
					var line strings.Builder
					for {
						b := make([]byte, 1)
						_, err := reader.Read(b)
						if err != nil {
							if err == io.EOF {
								// If nothing in line, then this was the end of the file
								if line.Len() == 0 {
									stack.Push(&MShellString{""})
									stack.Push(&MShellBool{false})
								} else {
									// Else, we have a final that wasn't terminated by a newline. Still try to remove '\r' if it's there
									builderStr := line.String()
									if len(builderStr) > 0 && builderStr[len(builderStr)-1] == '\r' {
										builderStr = builderStr[:len(builderStr)-1]
									}
									stack.Push(&MShellString{builderStr})
									stack.Push(&MShellBool{true})
								}
								break
							} else {
								return FailWithMessage(fmt.Sprintf("%d:%d: Error reading from stdin: %s\n", t.Line, t.Column, err.Error()))
							}
						}

						if b[0] == '\n' {
							builderStr := line.String()

							// Check if the last character is a '\r' and remove it if it is
							if len(builderStr) > 0 && builderStr[len(builderStr)-1] == '\r' {
								builderStr = builderStr[:len(builderStr)-1]
							}

							stack.Push(&MShellString{builderStr})
							stack.Push(&MShellBool{true})
							break
						} else {
							line.WriteByte(b[0])
						}
					}
				}
			} else if t.Type == STR { // Token Type
				obj, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert an empty stack to a string.\n", t.Line, t.Column))
				}

				stack.Push(&MShellString{obj.ToString()})
			} else if t.Type == INDEXER { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot index an empty stack.\n", t.Line, t.Column))
				}

				// Indexer is a digit between ':' and ':'. Remove ends and parse the number
				indexStr := t.Lexeme[1 : len(t.Lexeme)-1]
				index, err := strconv.Atoi(indexStr)
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Error parsing index: %s\n", t.Line, t.Column, err.Error()))
				}

				result, err := obj1.Index(index)
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: %s", t.Line, t.Column, err.Error()))
				}
				stack.Push(result)
			} else if t.Type == ENDINDEXER || t.Type == STARTINDEXER { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot end index an empty stack.\n", t.Line, t.Column))
				}

				var indexStr string
				// Parse the index value
				if t.Type == ENDINDEXER {
					indexStr = t.Lexeme[1:]
				} else {
					indexStr = t.Lexeme[:len(t.Lexeme)-1]
				}

				index, err := strconv.Atoi(indexStr)
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Error parsing index: %s\n", t.Line, t.Column, err.Error()))
				}

				var result MShellObject
				if t.Type == ENDINDEXER {
					result, err = obj1.SliceEnd(index)
				} else {
					result, err = obj1.SliceStart(index)
				}

				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: %s", t.Line, t.Column, err.Error()))
				}
				stack.Push(result)
			} else if t.Type == SLICEINDEXER { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot slice index an empty stack.\n", t.Line, t.Column))
				}

				// StartInc:EndExc
				parts := strings.Split(t.Lexeme, ":")
				startInt, err := strconv.Atoi(parts[0])
				endInt, err2 := strconv.Atoi(parts[1])

				if err != nil || err2 != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Error parsing slice indexes: %s\n", t.Line, t.Column, err.Error()))
				}

				result, err := obj1.Slice(startInt, endInt)
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot slice index a %s.\n", t.Line, t.Column, obj1.TypeName()))
				}

				stack.Push(result)
			} else if t.Type == STDOUTLINES || t.Type == STDOUTSTRIPPED || t.Type == STDOUTCOMPLETE { // Token Type
				obj, err := stack.Pop()
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot set stdout behavior to lines on an empty stack.\n", t.Line, t.Column))
				}

				switch obj.(type) {
				case *MShellList:
					list := obj.(*MShellList)
					if t.Type == STDOUTLINES {
						list.StdoutBehavior = STDOUT_LINES
					} else if t.Type == STDOUTSTRIPPED {
						list.StdoutBehavior = STDOUT_STRIPPED
					} else if t.Type == STDOUTCOMPLETE {
						list.StdoutBehavior = STDOUT_COMPLETE
					} else {
						return FailWithMessage(fmt.Sprintf("%d:%d: We haven't implemented the token type '%s' yet.\n", t.Line, t.Column, t.Type))
					}
					stack.Push(list)
				case *MShellPipe:
					pipe := obj.(*MShellPipe)
					if t.Type == STDOUTLINES {
						pipe.StdoutBehavior = STDOUT_LINES
					} else if t.Type == STDOUTSTRIPPED {
						pipe.StdoutBehavior = STDOUT_STRIPPED
					} else if t.Type == STDOUTCOMPLETE {
						pipe.StdoutBehavior = STDOUT_COMPLETE
					} else {
						return FailWithMessage(fmt.Sprintf("%d:%d: We haven't implemented the token type '%s' yet.\n", t.Line, t.Column, t.Type))
					}
					stack.Push(pipe)
				default:
					return FailWithMessage(fmt.Sprintf("%d:%d: Cannot set stdout behavior on a %s.\n", t.Line, t.Column, obj.TypeName()))
				}
			} else if t.Type == STOP_ON_ERROR { // Token Type
				state.StopOnError = true
			} else if t.Type == FLOAT { // Token Type
				floatVal, err := strconv.ParseFloat(t.Lexeme, 64)
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Error parsing float: %s\n", t.Line, t.Column, err.Error()))
				}
				stack.Push(&MShellFloat{floatVal})
			} else if t.Type == PATH { // Token Type
				parsed, err := ParseRawPath(t.Lexeme)
				if err != nil {
					return FailWithMessage(fmt.Sprintf("%d:%d: Error parsing path: %s\n", t.Line, t.Column, err.Error()))
				}
				stack.Push(&MShellPath { parsed })
			} else if t.Type == DATETIME {
				year, _ := strconv.Atoi(t.Lexeme[0:4])
				month, _ := strconv.Atoi(t.Lexeme[5:7])
				day, _ := strconv.Atoi(t.Lexeme[8:10])

				hour := 0
				minute := 0
				second := 0
				if len(t.Lexeme) >= 13 {
					hour, _ = strconv.Atoi(t.Lexeme[11:13])
				}

				if len(t.Lexeme) >= 16 {
					minute, _ = strconv.Atoi(t.Lexeme[14:16])
				}

				if len(t.Lexeme) >= 19 {
					second, _ = strconv.Atoi(t.Lexeme[17:19])
				}

				dt := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)
				stack.Push(&MShellDateTime{Time: dt, Token: t})
			} else {
				return FailWithMessage(fmt.Sprintf("%d:%d: We haven't implemented the token type '%s' yet.\n", t.Line, t.Column, t.Type))
			}
		default:
			return FailWithMessage(fmt.Sprintf("We haven't implemented the type '%T' yet.\n", t))
		}

	}

	return EvalResult{true, -1, 0, false}
}

type Executable interface {
	Execute(state *EvalState, context ExecuteContext, stack *MShellStack) (EvalResult, int, string)
	GetStandardInputFile() string
	GetStandardOutputFile() string
}

func (list *MShellList) Execute(state *EvalState, context ExecuteContext, stack *MShellStack) (EvalResult, int, string) {
	result, exitCode, stdoutResult := RunProcess(*list, context, state)
	return result, exitCode, stdoutResult
}

func (quotation *MShellQuotation) Execute(state *EvalState, context ExecuteContext, stack *MShellStack, definitions []MShellDefinition, callStack CallStack) (EvalResult, int) {
	quotationContext := ExecuteContext{
		StandardInput:  nil,
		StandardOutput: nil,
		Variables:      quotation.Variables,
	}

	if quotation.StdinBehavior != STDIN_NONE {
		if quotation.StdinBehavior == STDIN_CONTENT {
			quotationContext.StandardInput = strings.NewReader(quotation.StandardInputContents)
		} else if quotation.StdinBehavior == STDIN_FILE {
			file, err := os.Open(quotation.StandardInputFile)
			if err != nil {
				return FailWithMessage(fmt.Sprintf("Error opening file %s for reading: %s\n", quotation.StandardInputFile, err.Error())), 1
			}
			quotationContext.StandardInput = file
			defer file.Close()
		} else {
			panic("Unknown stdin behavior")
		}
	} else if context.StandardInput != nil {
		quotationContext.StandardInput = context.StandardInput
	} else {
		// Default to stdin of this process itself
		quotationContext.StandardInput = os.Stdin
	}

	if quotation.StandardOutputFile != "" {
		file, err := os.Create(quotation.StandardOutputFile)
		if err != nil {
			return FailWithMessage(fmt.Sprintf("Error opening file %s for writing: %s\n", quotation.StandardOutputFile, err.Error())), 1
		}
		quotationContext.StandardOutput = file
		defer file.Close()
	} else if context.StandardOutput != nil {
		quotationContext.StandardOutput = context.StandardOutput
	} else {
		// Default to stdout of this process itself
		quotationContext.StandardOutput = os.Stdout
	}

	result := state.Evaluate(quotation.Tokens, stack, quotationContext, definitions, callStack, CallStackItem{quotation, "quote", CALLSTACKQUOTE})

	if !result.Success || result.ExitCalled {
		return result, result.ExitCode
	} else {
		return SimpleSuccess(), 0
	}
}

func (list *MShellList) GetStandardInputFile() string {
	return list.StandardInputFile
}

func (list *MShellList) GetStandardOutputFile() string {
	return list.StandardOutputFile
}

func (quotation *MShellQuotation) GetStandardInputFile() string {
	return quotation.StandardInputFile
}

func (quotation *MShellQuotation) GetStandardOutputFile() string {
	return quotation.StandardOutputFile
}

func RunProcess(list MShellList, context ExecuteContext, state *EvalState) (EvalResult, int, string) {
	// Check for empty list
	if len(list.Items) == 0 {
		return FailWithMessage("Cannot execute an empty list.\n"), 1, ""
	}

	commandLineArgs := make([]string, 0)
	var commandLineQueue []MShellObject

	// Add all items to the queue, first in is the end of the slice, so add in reverse order
	for i := len(list.Items) - 1; i >= 0; i-- {
		commandLineQueue = append(commandLineQueue, list.Items[i])
	}

	for len(commandLineQueue) > 0 {
		item := commandLineQueue[len(commandLineQueue)-1]
		commandLineQueue = commandLineQueue[:len(commandLineQueue)-1]

		if innerList, ok := item.(*MShellList); ok {
			// Add to queue, first in is the end of the slice, so add in reverse order
			for i := len(innerList.Items) - 1; i >= 0; i-- {
				commandLineQueue = append(commandLineQueue, innerList.Items[i])
			}
		} else if !item.IsCommandLineable() {
			return FailWithMessage(fmt.Sprintf("Item (%s) cannot be used as a command line argument.\n", item.DebugString())), 1, ""
		} else {
			commandLineArgs = append(commandLineArgs, item.CommandLine())
		}
	}

	cmd := exec.Command(commandLineArgs[0], commandLineArgs[1:]...)
	cmd.Env = os.Environ()

	var commandSubWriter bytes.Buffer
	// TBD: Should we allow command substituation and redirection at the same time?
	// Probably more hassle than worth including, with probable workarounds for that rare case.
	if list.StdoutBehavior != STDOUT_NONE {
		cmd.Stdout = &commandSubWriter
	} else if list.StandardOutputFile != "" {
		// Open the file for writing
		file, err := os.Create(list.StandardOutputFile)
		if err != nil {
			return FailWithMessage(fmt.Sprintf("Error opening file %s for writing: %s\n", list.StandardOutputFile, err.Error())), 1, ""
		}
		cmd.Stdout = file
		defer file.Close()
	} else if context.StandardOutput != nil {
		cmd.Stdout = context.StandardOutput
	} else {
		// Default to stdout of this process itself
		cmd.Stdout = os.Stdout
	}

	if list.StdinBehavior != STDIN_NONE {
		if list.StdinBehavior == STDIN_CONTENT {
			cmd.Stdin = strings.NewReader(list.StandardInputContents)
		} else if list.StdinBehavior == STDIN_FILE {
			// Open the file for reading
			file, err := os.Open(list.StandardInputFile)
			if err != nil {
				return FailWithMessage(fmt.Sprintf("Error opening file %s for reading: %s\n", list.StandardInputFile, err.Error())), 1, ""
			}
			cmd.Stdin = file
			defer file.Close()
		} else {
			panic("Unknown stdin behavior")
		}

	} else if context.StandardInput != nil {
		cmd.Stdin = context.StandardInput

		// Print position of reader
		// position, err := cmd.Stdin.(*os.File).Seek(0, io.SeekCurrent)
		// if err != nil {
		// return FailWithMessage(fmt.Sprintf("Error getting position of reader: %s\n", err.Error())), 1
		// }
		// fmt.Fprintf(os.Stderr, "Position of reader: %d\n", position)
	} else {
		// Default to stdin of this process itself
		cmd.Stdin = os.Stdin
	}

	if list.StandardErrorFile != "" {
		// Open the file for writing
		file, err := os.Create(list.StandardErrorFile)
		if err != nil {
			return FailWithMessage(fmt.Sprintf("Error opening file %s for writing: %s\n", list.StandardErrorFile, err.Error())), 1, ""
		}
		cmd.Stderr = file
		defer file.Close()
	} else {
		cmd.Stderr = os.Stderr
	}

	// fmt.Fprintf(os.Stderr, "Running command: %s\n", cmd.String())
	err := cmd.Run() // Manually deal with the exit code upstream
	// fmt.Fprintf(os.Stderr, "Command finished\n")
	var exitCode int
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			fmt.Fprintf(os.Stderr, "Error running command: %s\n", err.Error())
			exitCode = 1
		} else {
			// Command exited with non-zero exit code
			exitCode = err.(*exec.ExitError).ExitCode()
		}
	} else {
		exitCode = cmd.ProcessState.ExitCode()
	}

	// fmt.Fprintf(os.Stderr, "Exit code: %d\n", exitCode)

	if list.StdoutBehavior != STDOUT_NONE {
		return SimpleSuccess(), exitCode, commandSubWriter.String()
	} else {
		return SimpleSuccess(), exitCode, ""
	}
}

func (state *EvalState) RunPipeline(MShellPipe MShellPipe, context ExecuteContext, stack *MShellStack) (EvalResult, int, string) {
	if len(MShellPipe.List.Items) == 0 {
		return FailWithMessage("Cannot execute an empty pipe.\n"), 1, ""
	}

	// Check that all list items are Executables
	for i, item := range MShellPipe.List.Items {
		if _, ok := item.(Executable); !ok {
			return FailWithMessage(fmt.Sprintf("Item %d (%s) in pipe is not a list or a quotation.\n", i, item.DebugString())), 1, ""
		}
	}

	if len(MShellPipe.List.Items) == 1 {
		// Just run the Execute on the first item
		asExecutable, _ := MShellPipe.List.Items[0].(Executable)
		return asExecutable.Execute(state, context, stack)
	}

	// Have at least 2 items here, create pipeline of Executables, set up list of contexts
	contexts := make([]ExecuteContext, len(MShellPipe.List.Items))

	pipeReaders := make([]io.Reader, len(MShellPipe.List.Items)-1)
	pipeWriters := make([]io.Writer, len(MShellPipe.List.Items)-1)

	// Set up pipes
	for i := 0; i < len(MShellPipe.List.Items)-1; i++ {
		pipeReader, pipeWriter, err := os.Pipe()
		if err != nil {
			return FailWithMessage(fmt.Sprintf("Error creating pipe: %s\n", err.Error())), 1, ""
		}
		pipeReaders[i] = pipeReader
		pipeWriters[i] = pipeWriter
	}

	var buf bytes.Buffer
	for i := 0; i < len(MShellPipe.List.Items); i++ {
		newContext := ExecuteContext{
			StandardInput:  nil,
			StandardOutput: nil,
			Variables:      context.Variables,
		}

		if i == 0 {
			// Stdin should use the context of this function, or the file marked on the initial object
			executableStdinFile := MShellPipe.List.Items[i].(Executable).GetStandardInputFile()

			if executableStdinFile != "" {
				file, err := os.Open(executableStdinFile)
				if err != nil {
					return FailWithMessage(fmt.Sprintf("Error opening file %s for reading: %s\n", executableStdinFile, err.Error())), 1, ""
				}
				newContext.StandardInput = file
				defer file.Close()
			} else if context.StandardInput != nil {
				newContext.StandardInput = context.StandardInput
			} else {
				// Default to stdin of this process itself
				newContext.StandardInput = os.Stdin
			}

			newContext.StandardOutput = pipeWriters[0]
		} else if i == len(MShellPipe.List.Items)-1 {
			newContext.StandardInput = pipeReaders[len(pipeReaders)-1]

			// Stdout should use the context of this function
			if MShellPipe.StdoutBehavior != STDOUT_NONE {
				newContext.StandardOutput = &buf
			} else {
				newContext.StandardOutput = context.StandardOutput
			}
		} else {
			newContext.StandardInput = pipeReaders[i-1]
			newContext.StandardOutput = pipeWriters[i]
		}

		contexts[i] = newContext
	}

	// Run the executables concurrently
	var wg sync.WaitGroup
	results := make([]EvalResult, len(MShellPipe.List.Items))
	exitCodes := make([]int, len(MShellPipe.List.Items))

	for i, item := range MShellPipe.List.Items {
		wg.Add(1)
		go func(i int, item Executable) {
			defer wg.Done()
			// fmt.Fprintf(os.Stderr, "Running item %d\n", i)
			results[i], exitCodes[i], _ = item.Execute(state, contexts[i], stack)

			// Close pipe ends that are no longer needed
			if i > 0 {
				pipeReaders[i-1].(io.Closer).Close()
			}
			if i < len(MShellPipe.List.Items)-1 {
				pipeWriters[i].(io.Closer).Close()
			}
		}(i, item.(Executable))
	}

	// Wait for all processes to complete
	wg.Wait()

	// Check for errors
	for i, result := range results {
		if !result.Success {

			if MShellPipe.StdoutBehavior == STDOUT_NONE {
				return result, exitCodes[i], ""
			} else {
				return result, exitCodes[i], buf.String()
			}
		}
	}

	if MShellPipe.StdoutBehavior == STDOUT_NONE {
		// Return the exit code of the last item
		return SimpleSuccess(), exitCodes[len(exitCodes)-1], ""
	} else {
		return SimpleSuccess(), exitCodes[len(exitCodes)-1], buf.String()
	}
}
