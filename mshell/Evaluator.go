package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"math"
	"slices"
	"errors"
	"runtime"
	"crypto/sha256"
	"encoding/hex"
	"encoding/csv"
	"encoding/json"
	"regexp"
	// "golang.org/x/term"
	_ "time/tzdata"
	"unicode"
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

func (objList *MShellStack) Pop1(t Token) (MShellObject, error) {
	obj1, err := objList.Pop()
	if err != nil {
		return nil, fmt.Errorf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme)
	}
	return obj1, nil
}

// Returns two objects from the stack.
// obj1, obj2 := stack.Pop2(t)
// obj1 was on top of the stack, obj2 was below it.
func (objList *MShellStack) Pop2(t Token) (MShellObject, MShellObject, error) {
	obj1, err := objList.Pop1(t)
	if err != nil {
		return nil, nil, err
	}
	obj2, err := objList.Pop()
	if err != nil {
		return nil, nil, fmt.Errorf("%d:%d: Cannot do '%s' operation on a stack with only one item.\n", t.Line, t.Column, t.Lexeme)
	}

	return obj1, obj2, nil
}

func (objList *MShellStack) Pop3(t Token) (MShellObject, MShellObject, MShellObject, error) {
	obj1, obj2, err := objList.Pop2(t)
	if err != nil {
		return nil, nil, nil, err
	}

	obj3, err := objList.Pop()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%d:%d: Cannot do '%s' operation on a stack with only two items.\n", t.Line, t.Column, t.Lexeme)
	}
	return obj1, obj2, obj3, nil
}

func (objList *MShellStack) Push(obj MShellObject) {
	*objList = append(*objList, obj)
}

func (objList *MShellStack) String() string {
	var builder strings.Builder
	builder.WriteString("Stack contents:\n")
	for i, obj := range *objList {
		strToWrite := fmt.Sprintf("%d: %s\n", i, obj.DebugString())
		builder.WriteString(strToWrite)
	}
	builder.WriteString("End of stack contents\n")
	return builder.String()
}

type EvalState struct {
	PositionalArgs []string
	LoopDepth      int

	StopOnError bool
	CallStack  CallStack
}

type EvalResult struct {
	Success    bool
	Continue   bool
	BreakNum   int
	ExitCode   int
	ExitCalled bool
}

func (result EvalResult) ShouldPassResultUpStack() bool {
	return !result.Success || result.BreakNum > 0 || result.ExitCalled || result.Continue
}

func (result EvalResult) String() string {
	return fmt.Sprintf("EvalResult: Success: %t, Continue: %t, BreakNum: %d, ExitCode: %d, ExitCalled: %t", result.Success, result.Continue, result.BreakNum, result.ExitCode, result.ExitCalled)
}

type ExecuteContext struct {
	StandardInput  io.Reader
	StandardOutput io.Writer
	StandardError  io.Writer
	Variables      map[string]MShellObject // Mapping from variable name without leading '@' or trailing '!' to object.
	ShouldCloseInput  bool
	ShouldCloseOutput bool
	Pbm IPathBinManager
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
	// Return Eval result with success, 0 exit code, and no break statement
	return EvalResult{true, false, -1, 0, false}
}

func (state *EvalState) FailWithMessage(message string)  EvalResult {
	// Log message to stderr
	if state.CallStack == nil {
		fmt.Fprintf(os.Stderr, "No call stack available.\n")
		fmt.Fprint(os.Stderr, message)
		return EvalResult{false, false, -1, 1, false}
	}

	// fmt.Fprintf(os.Stderr, "Call stack (%d):\n", len(state.CallStack))
	for _, callStackItem := range state.CallStack {
		parseItem := callStackItem.MShellParseItem

		if parseItem == nil {
			fmt.Fprintf(os.Stderr, "%s\n", callStackItem.Name)
		} else {
			startToken := callStackItem.MShellParseItem.GetStartToken()
			fmt.Fprintf(os.Stderr, "%d:%d %s\n", startToken.Line, startToken.Column, callStackItem.Name)
		}
	}

	fmt.Fprint(os.Stderr, message)
	return EvalResult{false, false, -1, 1, false}
}

type CallStackType int

const (
	CALLSTACKFILE CallStackType = iota
	CALLSTACKLIST
	CALLSTACKDICT
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

func (state *EvalState) Evaluate(objects []MShellParseItem, stack *MShellStack, context ExecuteContext, definitions []MShellDefinition, callStackItem CallStackItem) EvalResult {
	// Defer popping the call stack
	if callStackItem.MShellParseItem != nil {
		state.CallStack.Push(callStackItem)
		defer func() {
			state.CallStack.Pop()
		}()
	}

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
			result := state.Evaluate(list.Items, &listStack, context, definitions, callStackItem)

			if !result.Success {
				fmt.Fprint(os.Stderr, "Failed to evaluate list.\n")
				return result
			}

			if result.ExitCalled {
				return result
			}

			if result.BreakNum > 0 {
				return state.FailWithMessage("Encountered break within list.\n")
			}

			if result.Continue {
				return state.FailWithMessage("Encountered continue within list.\n")
			}

			newList := NewList(len(listStack))
			for i, item := range listStack {
				newList.Items[i] = item
			}
			stack.Push(newList)
		case *MShellParseDict:
			// Evaluate the dictionary
			parseDict := t.(*MShellParseDict)
			dict := NewDict()

			for _, keyValue := range parseDict.Items {
				key := keyValue.Key

				var dictStack MShellStack
				dictStack = []MShellObject{}
				callStackItem := CallStackItem{MShellParseItem: parseDict, Name: "dict", CallStackType: CALLSTACKDICT}
				result := state.Evaluate(keyValue.Value, &dictStack, context, definitions, callStackItem)
				if !result.Success {
					fmt.Fprint(os.Stderr, "Failed to evaluate dictionary.\n")
					return result
				}
				if result.ExitCalled {
					return result
				}
				if result.BreakNum > 0 {
					return state.FailWithMessage("Encountered break within dictionary.\n")
				}
				if result.Continue {
					return state.FailWithMessage("Encountered continue within dictionary.\n")
				}
				if len(dictStack) == 0 {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Dictionary key '%s' evaluated to an empty stack.\n", keyValue.Value[0].GetStartToken().Line, keyValue.Value[0].GetStartToken().Column, key))
				}

				if len(dictStack) > 1 {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Dictionary key '%s' evaluated to a stack with more than one item.\n", keyValue.Value[0].GetStartToken().Line, keyValue.Value[0].GetStartToken().Column, key))
				}

				dict.Items[keyValue.Key] = dictStack[0]
			}

			stack.Push(dict)
		case *MShellParseQuote:
			parseQuote := t.(*MShellParseQuote)
			q := MShellQuotation{Tokens: parseQuote.Items, StandardInputFile: "", StandardOutputFile: "", StandardErrorFile: "", Variables: context.Variables, MShellParseQuote: parseQuote}
			stack.Push(&q)
		case *MShellIndexerList:
			obj1, err := stack.Pop()
			if err != nil {
				startToken := t.GetStartToken()
				return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'indexer' operation on an empty stack.\n", startToken.Line, startToken.Column))
			}

			indexerList := t.(*MShellIndexerList)
			if len(indexerList.Indexers) == 1 && indexerList.Indexers[0].(Token).Type == INDEXER {
				t := indexerList.Indexers[0].(Token)
				// Indexer is a digit between ':' and ':'. Remove ends and parse the number
				indexStr := t.Lexeme[1 : len(t.Lexeme)-1]
				index, err := strconv.Atoi(indexStr)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing index: %s\n", t.Line, t.Column, err.Error()))
				}

				result, err := obj1.Index(index)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: %s", t.Line, t.Column, err.Error()))
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
return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing index: %s\n", indexerToken.Line, indexerToken.Column, err.Error()))
						}

						result, err := obj1.Index(index)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: %s", indexerToken.Line, indexerToken.Column, err.Error()))
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
								return state.FailWithMessage(fmt.Sprintf("%d:%d: %s", indexerToken.Line, indexerToken.Column, err.Error()))
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
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing index: %s\n", indexerToken.Line, indexerToken.Column, err.Error()))
						}

						var result MShellObject
						if indexerToken.Type == ENDINDEXER {
							result, err = obj1.SliceEnd(index)
						} else {
							result, err = obj1.SliceStart(index)
						}

						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: %s", indexerToken.Line, indexerToken.Column, err.Error()))
						}

						if newObject == nil {
							newObject = result
						} else {
							newObject, err  = newObject.Concat(result)
							if err != nil {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: %s", indexerToken.Line, indexerToken.Column, err.Error()))
							}
						}

					case SLICEINDEXER:
						// StartInc:EndExc
						parts := strings.Split(indexerToken.Lexeme, ":")
						startInt, err := strconv.Atoi(parts[0])
						endInt, err2 := strconv.Atoi(parts[1])

						if err != nil || err2 != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing slice indexes: %s\n", indexerToken.Line, indexerToken.Column, err.Error()))
						}

						result, err := obj1.Slice(startInt, endInt)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: %s.\n", indexerToken.Line, indexerToken.Column, err.Error()))
						}

						if newObject == nil {
							newObject = result
						} else {
							newObject, err  = newObject.Concat(result)
							if err != nil {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: %s", indexerToken.Line, indexerToken.Column, err.Error()))
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
						newContext.Pbm = context.Pbm

						callStackItem := CallStackItem{MShellParseItem: t, Name: definition.Name, CallStackType: CALLSTACKDEF}
						result := state.Evaluate(definition.Items, stack, newContext, definitions, callStackItem)

						if result.ShouldPassResultUpStack() {
							return result
						}

						continue MainLoop
					}
				}

				if t.Lexeme == ".s" {
					// Print current stack
					fmt.Fprint(os.Stderr, stack.String())
				} else if t.Lexeme == ".b" {
					// Print known binaries
					debugStr := context.Pbm.DebugList()
					fmt.Fprint(os.Stderr, debugStr)
				} else if t.Lexeme == ".def" {
					// Print out available definitions
					fmt.Fprint(os.Stderr, "Available definitions:\n")
					for _, definition := range definitions {
						fmt.Fprintf(os.Stderr, "%s\n", definition.Name)
					}
				} else if t.Lexeme == ".env" {
					// Print a list of all environment variables, sorted by key
					envVars := os.Environ()
					slices.Sort(envVars)

					for _, envVar := range envVars {
						fmt.Fprintf(os.Stderr, "%s\n", envVar)
					}
				} else if t.Lexeme == "dup" {
					top, err := stack.Peek()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot duplicate an empty stack.\n", t.Line, t.Column))
					}
					stack.Push(top)
				} else if t.Lexeme == "over" {
					stackLen := len(*stack)
					if stackLen < 2 {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'over' operation on a stack with less than two items.\n", t.Line, t.Column))
					}

					obj := (*stack)[stackLen-2]
					stack.Push(obj)
				} else if t.Lexeme == "swap" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					stack.Push(obj1)
					stack.Push(obj2)
				} else if t.Lexeme == "drop" {
					_, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot drop an empty stack.\n", t.Line, t.Column))
					}
				} else if t.Lexeme == "rot" {
					// Check that there are at least 3 items on the stack
					stackLen := len(*stack)
					if stackLen < 3 {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'rot' operation on a stack with less than three items.\n", t.Line, t.Column))
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
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'rot' operation on a stack with less than three items.\n", t.Line, t.Column))
					}
					top, _ := stack.Pop()
					second, _ := stack.Pop()
					third, _ := stack.Pop()
					stack.Push(top)
					stack.Push(third)
					stack.Push(second)
				} else if t.Lexeme == "nip" {
					if len(*stack) < 2 {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'nip' operation on a stack with less than two items.\n", t.Line, t.Column))
					}
					top, _ := stack.Pop()
					_, _ = stack.Pop()
					stack.Push(top)
				} else if t.Lexeme == "glob" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'glob' operation on an empty stack.\n", t.Line, t.Column))
					}

					globStr, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot glob a %s (%s).\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					files, err := filepath.Glob(globStr)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Malformed glob pattern: %s\n", t.Line, t.Column, err.Error()))
					}

					newList := NewList(len(files))
					if files != nil {
						for i, file := range files {
							newList.Items[i] = &MShellPath{file}
						}
					}

					stack.Push(newList)
				} else if t.Lexeme == "stdin" {
					// Dump all of current stdin onto the stack as a string
					var buffer bytes.Buffer
					var reader io.Reader
					if context.StandardInput == nil {
						reader = os.Stdin
					} else {
						reader = context.StandardInput
					}
					_, err := buffer.ReadFrom(reader)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error reading from stdin: %s\n", t.Line, t.Column, err.Error()))
					}
					stack.Push(&MShellString{buffer.String()})
				} else if t.Lexeme == "append" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'append' operation on an empty stack.\n", t.Line, t.Column))
					}

					obj2, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'append' operation on a stack with only one item.\n", t.Line, t.Column))
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
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot append a %s to a %s.\n", t.Line, t.Column, obj1.TypeName(), obj2.TypeName()))
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
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'len' operation on an empty stack.\n", t.Line, t.Column))
					}

					switch obj.(type) {
					case *MShellList:
						stack.Push(&MShellInt{len(obj.(*MShellList).Items)})
					case *MShellString:
						stack.Push(&MShellInt{len(obj.(*MShellString).Content)})
					case *MShellLiteral:
						stack.Push(&MShellInt{len(obj.(*MShellLiteral).LiteralText)})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get length of a %s.\n", t.Line, t.Column, obj.TypeName()))
					}
				} else if t.Lexeme == "nth" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'nth' operation on an empty stack.\n", t.Line, t.Column))
					}

					obj2, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'nth' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					int1, ok := obj1.(*MShellInt)
					if ok {
						result, err := obj2.Index(int1.Value)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
						}
						stack.Push(result)
					} else {
						int2, ok := obj2.(*MShellInt)
						if ok {
							result, err := obj1.Index(int2.Value)
							if err != nil {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
							}
							stack.Push(result)
						} else {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'nth' with a %s and a %s.\n", t.Line, t.Column, obj2.TypeName(), obj1.TypeName()))
						}
					}
				} else if t.Lexeme == "pick" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'pick' operation on an empty stack.\n", t.Line, t.Column))
					}
					// Check that obj1 is an integer
					int1, ok := obj1.(*MShellInt)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'pick' with a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					// Check that int is greater than or equal to 1
					if int1.Value < 1 {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'pick' with a value less than 1.\n", t.Line, t.Column))
					}

					// Check that the stack has enough items
					if int1.Value > len(*stack) {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'pick' on a stack with less than %d items.\n", t.Line, t.Column, int1.Value+1))
					}

					// Duplicate the nth item on the stack
					// a b c 2  -> a b c b
					stack.Push((*stack)[len(*stack)-int1.Value])
				} else if t.Lexeme == "w" || t.Lexeme == "wl" || t.Lexeme == "we" || t.Lexeme == "wle" {
					// Print the top of the stack to the console.
					top, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot write an empty stack.\n", t.Line, t.Column))
					}

					var writer io.Writer
					if t.Lexeme == "we" || t.Lexeme == "wle" {
						writer = os.Stderr // TODO: Update like below for stdout.
					} else {
						if context.StandardOutput == nil {
							writer = os.Stdout
						} else {
							writer = context.StandardOutput
						}
					}

					switch top.(type) {
					case *MShellLiteral:
						fmt.Fprint(writer, top.(*MShellLiteral).LiteralText)
					case *MShellString:
						fmt.Fprint(writer, top.(*MShellString).Content)
					case *MShellInt:
						fmt.Fprint(writer, top.(*MShellInt).Value)
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot write a %s.\n", t.Line, t.Column, top.TypeName()))
					}

					if t.Lexeme == "wl" || t.Lexeme == "wle" {
						fmt.Fprint(writer, "\n")
					}
				} else if t.Lexeme == "findReplace" {
					// Do simple find replace with the top three strings on stack
					obj1, obj2, obj3, err := stack.Pop3(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					var replacementStr string
					var findStr string
					var originalStr string

					replacementStr, err = obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot find-replace with a %s as the replacement string.\n", t.Line, t.Column, obj1.TypeName()))
					}

					findStr, err = obj2.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot find-replace with a %s as the find string.\n", t.Line, t.Column, obj2.TypeName()))
					}

					originalStr, err = obj3.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot find-replace with a %s as the original string.\n", t.Line, t.Column, obj3.TypeName()))
					}

					stack.Push(&MShellString{strings.Replace(originalStr, findStr, replacementStr, -1)})
				} else if t.Lexeme == "split" {
					delimiter, strLiteral, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					delimiterStr, err := delimiter.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot split with a %s (%s).\n", t.Line, t.Column, delimiter.TypeName(), delimiter.DebugString()))
					}

					strToSplit, err := strLiteral.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot split a %s (%s).\n", t.Line, t.Column, strLiteral.TypeName(), strLiteral.DebugString()))
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
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'wsplit' operation on an empty stack.\n", t.Line, t.Column))
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
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot split a %s.\n", t.Line, t.Column, obj.TypeName()))
					}
				} else if t.Lexeme == "join" {
					// This is a string join function
					delimiter, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'join' operation on an empty stack.\n", t.Line, t.Column))
					}

					list, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'join' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					var delimiterStr string
					var listItems []string

					switch delimiter.(type) {
					case *MShellString:
						delimiterStr = delimiter.(*MShellString).Content
					case *MShellLiteral:
						delimiterStr = delimiter.(*MShellLiteral).LiteralText
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot join with a %s.\n", t.Line, t.Column, delimiter.TypeName()))
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
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot join a list with a %s inside (%s).\n", t.Line, t.Column, item.TypeName(), item.DebugString()))
							}
						}
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Expected a list as the second item on stack for join, received a %s (%s). The delimiter was '%s'\n", t.Line, t.Column, list.TypeName(), list.DebugString(), delimiterStr))
					}

					stack.Push(&MShellString{strings.Join(listItems, delimiterStr)})
				} else if t.Lexeme == "lines" {
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot evaluate 'lines' on an empty stack.\n", t.Line, t.Column))
					}

					s1, ok := obj.(*MShellString)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot evaluate 'lines' on a %s.\n", t.Line, t.Column, obj.TypeName()))
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
					obj1, obj2, obj3, err := stack.Pop3(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					obj1Index, ok := obj1.(*MShellInt)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot set at a non-integer index at top of stack, found a %s (%s).\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					obj3List, ok := obj3.(*MShellList)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot set into a non-list as third item on stack, found a %s (%s).\n", t.Line, t.Column, obj3.TypeName(), obj3.DebugString()))
					}

					if obj1Index.Value < 0 {
						obj1Index.Value = len(obj3List.Items) + obj1Index.Value
					}

					if obj1Index.Value < 0 || obj1Index.Value >= len(obj3List.Items) {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Index out of range for 'setAt'.\n", t.Line, t.Column))
					}

					obj3List.Items[obj1Index.Value] = obj2
					stack.Push(obj3List)
				} else if t.Lexeme == "insert" {
					// Expected stack:
					// List item index
					// Index 0 based, negative indexes allowed
					obj1, obj2, obj3, err := stack.Pop3(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					obj1Index, ok := obj1.(*MShellInt)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot insert at a non-integer index.\n", t.Line, t.Column))
					}

					obj3List, ok := obj3.(*MShellList)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot insert into a non-list.\n", t.Line, t.Column))
					}

					if obj1Index.Value < 0 {
						obj1Index.Value = len(obj3List.Items) + obj1Index.Value
					}

					if obj1Index.Value < 0 || obj1Index.Value > len(obj3List.Items) {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Index out of range for 'insert'.\n", t.Line, t.Column))
					}

					obj3List.Items = append(obj3List.Items[:obj1Index.Value], append([]MShellObject{obj2}, obj3List.Items[obj1Index.Value:]...)...)
					stack.Push(obj3List)
				} else if t.Lexeme == "del" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
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
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Index out of range for 'del'.\n", t.Line, t.Column))
							}
							obj2.(*MShellList).Items = append(obj2.(*MShellList).Items[:index], obj2.(*MShellList).Items[index+1:]...)
							stack.Push(obj2)
						default:
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot delete from a %s.\n", t.Line, t.Column, obj2.TypeName()))
						}
					case *MShellList:
						switch obj2.(type) {
						case *MShellInt:
							index := obj2.(*MShellInt).Value
							if index < 0 {
								index = len(obj1.(*MShellList).Items) + index
							}
							if index < 0 || index >= len(obj1.(*MShellList).Items) {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Index out of range for 'del'.\n", t.Line, t.Column))
							}
							obj1.(*MShellList).Items = append(obj1.(*MShellList).Items[:index], obj1.(*MShellList).Items[index+1:]...)
							stack.Push(obj1)
						default:
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot delete from a %s.\n", t.Line, t.Column, obj2.TypeName()))
						}
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot delete from a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}
				} else if t.Lexeme == "readFile" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'readFile' operation on an empty stack.\n", t.Line, t.Column))
					}

					filePath, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot read from a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					content, err := os.ReadFile(filePath)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error reading file: %s\n", t.Line, t.Column, err.Error()))
					}

					stack.Push(&MShellString{string(content)})
				} else if t.Lexeme == "cd" {
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'cd' operation on an empty stack.\n", t.Line, t.Column))
					}

					dir, err := obj.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot cd to a %s.\n", t.Line, t.Column, obj.TypeName()))
					}

					result, _, _, _ := state.ChangeDirectory(dir)
					if !result.Success {
						return result
					}
				} else if t.Lexeme == "in" {
					substring, stringOrDict, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					substringText, err := substring.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot search for a %s.\n", t.Line, t.Column, substring.TypeName()))
					}

					if dict, ok := stringOrDict.(*MShellDict); ok {
						// Check if the substring is a key in the dictionary
						_, exists := dict.Items[substringText]
						stack.Push(&MShellBool{exists})
					} else {
						totalStringText, err := stringOrDict.CastString()
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot search in a %s.\n", t.Line, t.Column, stringOrDict.TypeName()))
						}

						stack.Push(&MShellBool{strings.Contains(totalStringText, substringText)})
					}
				} else if t.Lexeme == "/" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					if obj1.IsNumeric() && obj2.IsNumeric() {
						switch obj1.(type) {
						case *MShellInt:
							if obj1.(*MShellInt).Value == 0 {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot divide by zero.\n", t.Line, t.Column))
							}
							switch obj2.(type) {
							case *MShellInt:
								stack.Push(&MShellInt{obj2.(*MShellInt).Value / obj1.(*MShellInt).Value})
							case *MShellFloat:
								stack.Push(&MShellFloat{float64(obj2.(*MShellFloat).Value) / float64(obj1.(*MShellInt).Value)})
							}
						case *MShellFloat:
							if obj1.(*MShellFloat).Value == 0 {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot divide by zero.\n", t.Line, t.Column))
							}

							switch obj2.(type) {
							case *MShellInt:
								stack.Push(&MShellFloat{float64(obj2.(*MShellInt).Value) / obj1.(*MShellFloat).Value})
							case *MShellFloat:
								stack.Push(&MShellFloat{obj2.(*MShellFloat).Value / obj1.(*MShellFloat).Value})
							}
						default:
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot divide a %s and a %s.\n", t.Line, t.Column, obj2.TypeName(), obj1.TypeName()))
						}
					} else {
						// Check if both are paths
						switch obj1.(type) {
						case *MShellPath:
							switch obj2.(type) {
							case *MShellPath:
								// This is a path join operation
								newPath := filepath.Join(obj2.(*MShellPath).Path, obj1.(*MShellPath).Path)
								stack.Push(&MShellPath{newPath})
							default:
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do a join between a %s (%s) and a %s (%s).\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString(), obj1.TypeName(), obj1.DebugString()))
							}
						default:
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot divide a %s and a %s.\n", t.Line, t.Column, obj2.TypeName(), obj1.TypeName()))
						}
					}
				} else if t.Lexeme == "exit" {
					exitCode, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'exit' operation on an empty stack. If you are trying to exit out of the interactive shell, you are probably looking to do `0 exit`.\n", t.Line, t.Column))
					}

					exitInt, ok := exitCode.(*MShellInt)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot exit with a %s.\n", t.Line, t.Column, exitCode.TypeName()))
					}

					if exitInt.Value < 0 || exitInt.Value > 255 {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot exit with a value outside of 0-255.\n", t.Line, t.Column))
					}

					if exitInt.Value == 0 {
						return EvalResult{true, false, -1, 0, true}
					} else {
						return EvalResult{false, false, -1, exitInt.Value, true}
					}
				} else if t.Lexeme == "*" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '*' operation on an empty stack.\n", t.Line, t.Column))
					}

					obj2, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '*' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					if !obj1.IsNumeric() || !obj2.IsNumeric() {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot multiply a %s and a %s. If you are looking for wildcard glob, you want `\"*\" glob`.\n", t.Line, t.Column, obj2.TypeName(), obj1.TypeName()))
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
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'toFloat' operation on an empty stack.\n", t.Line, t.Column))
					}

					switch obj.(type) {
					case *MShellString:
						floatVal, err := strconv.ParseFloat(strings.TrimSpace(obj.(*MShellString).Content), 64)
						if err != nil {
							stack.Push(&Maybe{ obj: nil })
						} else {
							stack.Push(&Maybe{ obj: &MShellFloat{floatVal} })
						}
						// I don't believe checking for literal is required, because it should have been parsed as a float to start with?
					case *MShellInt:
						stack.Push(&MShellFloat{float64(obj.(*MShellInt).Value)})
					case *MShellFloat:
						stack.Push(obj)
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s to a float.\n", t.Line, t.Column, obj.TypeName()))
					}
				} else if t.Lexeme == "toInt" {
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'toInt' operation on an empty stack.\n", t.Line, t.Column))
					}

					switch obj.(type) {
					case *MShellString:
						intVal, err := strconv.Atoi(strings.TrimSpace(obj.(*MShellString).Content))
						if err != nil {
							stack.Push(&Maybe{ obj: nil })
						} else {
							stack.Push(&Maybe{ obj: &MShellInt{intVal} })
						}
						// I don't believe checking for literal is required, because it should have been parsed as a float to start with?
					case *MShellInt:
						stack.Push(obj)
					case *MShellFloat:
						stack.Push(&MShellInt{int(obj.(*MShellFloat).Value)})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s to an int.\n", t.Line, t.Column, obj.TypeName()))
					}
				} else if t.Lexeme == "toDt" {
					dateStrObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'toDt' operation on an empty stack.\n", t.Line, t.Column))
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
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s to a datetime.\n", t.Line, t.Column, dateStrObj.TypeName()))
					}

					// TODO: Don't make a new lexer object each time.
					parsedTime, err := ParseDateTime(dateStr)
					if err != nil {
						stack.Push(&Maybe{ obj: nil })
						// return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing date time '%s': %s\n", t.Line, t.Column, dateStr, err.Error()))
					} else {
						dt := MShellDateTime{Time: parsedTime, Token: t}
						stack.Push(&Maybe{ obj: &dt })
					}
				} else if t.Lexeme == "files" || t.Lexeme == "dirs" {
					// Dump all the files in the current directory to the stack. No sub-directories.
					files, err := os.ReadDir(".")
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error reading current directory: %s\n", t.Line, t.Column, err.Error()))
					}

					newList := 	NewList(0)
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
				} else if t.Lexeme == "isDir" || t.Lexeme == "isFile" {
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'isDir' operation on an empty stack.\n", t.Line, t.Column))
					}

					path, err := obj.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot check if a %s is a directory.\n", t.Line, t.Column, obj.TypeName()))
					}

					fileInfo, err := os.Stat(path)
					if err != nil {
						if errors.Is(err, os.ErrNotExist) {
							stack.Push(&MShellBool{false})
						} else {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error checking if %s is a directory: %s\n", t.Line, t.Column, path, err.Error()))
						}
					} else {
						if t.Lexeme == "isDir" {
							stack.Push(&MShellBool{fileInfo.IsDir()})
						} else if t.Lexeme == "isFile" {
							stack.Push(&MShellBool{!fileInfo.IsDir()})
						} else {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Unknown operation: %s\n", t.Line, t.Column, t.Lexeme))
						}
					}
				} else if t.Lexeme == "mkdir" || t.Lexeme == "mkdirp" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'mkdir' operation on an empty stack.\n", t.Line, t.Column))
					}

					dirPath, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot make a directory with a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					if t.Lexeme == "mkdir" {
						err = os.Mkdir(dirPath, 0755)
					} else if t.Lexeme == "mkdirp" {
						err = os.MkdirAll(dirPath, 0755)
					}
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error creating directory: %s\n", t.Line, t.Column, err.Error()))
					}
				} else if t.Lexeme == "~" || strings.HasPrefix(t.Lexeme, "~/") {
					// Only do tilde expansion
					homeDir, err := os.UserHomeDir()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error expanding ~: %s\n", t.Line, t.Column, err.Error()))
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
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error getting current directory: %s\n", t.Line, t.Column, err.Error()))
					}
					stack.Push(&MShellString{pwd})
				} else if t.Lexeme == "psub" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'psub' operation on an empty stack.\n", t.Line, t.Column))
					}

					// Do process substitution with temporary files
					// Create a temporary file
					tmpfile, err := os.CreateTemp("", "msh-")
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error creating temporary file: %s\n", t.Line, t.Column, err.Error()))
					}
					// Close the file
					defer tmpfile.Close()
					registerTempFileForCleanup(tmpfile.Name())

					// Write the contents of the object to the temporary file
					switch obj1.(type) {
					case *MShellString:
						_, err = tmpfile.WriteString(obj1.(*MShellString).Content)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error writing to temporary file: %s\n", t.Line, t.Column, err.Error()))
						}
					case *MShellLiteral:
						_, err = tmpfile.WriteString(obj1.(*MShellLiteral).LiteralText)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error writing to temporary file: %s\n", t.Line, t.Column, err.Error()))
						}
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'psub' with a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					stack.Push(&MShellString{tmpfile.Name()})
				} else if t.Lexeme == "date" {
					// Drop current local date time onto the stack
					stack.Push(&MShellDateTime{Time: time.Now(), Token: t})
				} else if t.Lexeme == "day" {
					dateTimeObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'day' operation on an empty stack.\n", t.Line, t.Column))
					}

					dateTime, ok := dateTimeObj.(*MShellDateTime)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the day of a %s.\n", t.Line, t.Column, dateTimeObj.TypeName()))
					}

					stack.Push(&MShellInt{dateTime.Time.Day()})
				} else if t.Lexeme == "month" {
					dateTimeObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'month' operation on an empty stack.\n", t.Line, t.Column))
					}

					dateTime, ok := dateTimeObj.(*MShellDateTime)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the month of a %s.\n", t.Line, t.Column, dateTimeObj.TypeName()))
					}

					stack.Push(&MShellInt{int(dateTime.Time.Month())})

				} else if t.Lexeme == "year" {
					dateTimeObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'year' operation on an empty stack.\n", t.Line, t.Column))
					}

					dateTime, ok := dateTimeObj.(*MShellDateTime)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the year of a %s.\n", t.Line, t.Column, dateTimeObj.TypeName()))
					}

					stack.Push(&MShellInt{dateTime.Time.Year()})
				} else if t.Lexeme == "hour" {
					dateTimeObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'hour' operation on an empty stack.\n", t.Line, t.Column))
					}

					dateTime, ok := dateTimeObj.(*MShellDateTime)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the hour of a %s.\n", t.Line, t.Column, dateTimeObj.TypeName()))
					}

					stack.Push(&MShellInt{dateTime.Time.Hour()})
				} else if t.Lexeme == "minute" {
					dateTimeObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'minute' operation on an empty stack.\n", t.Line, t.Column))
					}

					dateTime, ok := dateTimeObj.(*MShellDateTime)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the minute of a %s.\n", t.Line, t.Column, dateTimeObj.TypeName()))
					}

					stack.Push(&MShellInt{dateTime.Time.Minute()})
				} else if t.Lexeme == "mod" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'mod' operation on an empty stack.\n", t.Line, t.Column))
					}

					obj2, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'mod' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					switch obj1.(type) {
					case *MShellInt:
						switch obj2.(type) {
						case *MShellInt:
							if obj1.(*MShellInt).Value == 0 {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot mod by zero.\n", t.Line, t.Column))
							}

							stack.Push(&MShellInt{obj2.(*MShellInt).Value % obj1.(*MShellInt).Value})
						case *MShellFloat:
							if obj1.(*MShellInt).Value == 0 {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot mod by zero.\n", t.Line, t.Column))
							}

							stack.Push(&MShellFloat{math.Mod(obj2.(*MShellFloat).Value, float64(obj1.(*MShellInt).Value))})
						}

					case *MShellFloat:
						switch obj2.(type) {
						case *MShellInt:
							if obj1.(*MShellFloat).Value == 0 {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot mod by zero.\n", t.Line, t.Column))
							}

							stack.Push(&MShellFloat{math.Mod(float64(obj2.(*MShellInt).Value), obj1.(*MShellFloat).Value)})
						case *MShellFloat:
							if obj1.(*MShellFloat).Value == 0 {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot mod by zero.\n", t.Line, t.Column))
							}

							stack.Push(&MShellFloat{math.Mod(obj2.(*MShellFloat).Value, obj1.(*MShellFloat).Value)})
						}
					}
				} else if t.Lexeme == "basename" || t.Lexeme == "dirname" || t.Lexeme == "ext" || t.Lexeme == "stem" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
					}

					path, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the %s of a %s.\n", t.Line, t.Column, t.Lexeme, obj1.TypeName()))
					}

					if t.Lexeme == "basename" {
						stack.Push(&MShellString{filepath.Base(path)})
					} else if t.Lexeme == "dirname" {
						stack.Push(&MShellString{filepath.Dir(path)})
					} else if t.Lexeme == "ext" {
						stack.Push(&MShellString{filepath.Ext(path)})
					} else if t.Lexeme == "stem" {
						// This should include previous dir if it exists

						stack.Push(&MShellString{strings.TrimSuffix(path, filepath.Ext(path))})
					}
				} else if t.Lexeme == "toPath" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'toPath' operation on an empty stack.\n", t.Line, t.Column))
					}

					path, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s to a path.\n", t.Line, t.Column, obj1.TypeName()))
					}

					stack.Push(&MShellPath{path})
				} else if t.Lexeme == "dateFmt" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'dateFmt' operation on an empty stack.\n", t.Line, t.Column))
					}

					obj2, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'dateFmt' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					// Obj1 should be the format string, obj2 should be the date time object
					formatString, ok := obj1.(*MShellString)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot format a date with a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					dateTime, ok := obj2.(*MShellDateTime)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot format a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}

					newStr := dateTime.Time.Format(formatString.Content)
					stack.Push(&MShellString{newStr})
				} else if t.Lexeme == "trim" || t.Lexeme == "trimStart" || t.Lexeme == "trimEnd" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'trim' operation on an empty stack.\n", t.Line, t.Column))
					}


					str, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot trim a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					if t.Lexeme == "trim" {
						stack.Push(&MShellString{strings.TrimSpace(str)})
					} else if t.Lexeme == "trimStart" {
						stack.Push(&MShellString{strings.TrimLeft(str, " \t\n")})
					} else if t.Lexeme == "trimEnd" {
						stack.Push(&MShellString{strings.TrimRight(str, " \t\n")})
					}
				} else if t.Lexeme == "upper" || t.Lexeme == "lower" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
					}

					var f func(string) string
					if t.Lexeme == "upper" {
						f = strings.ToUpper
					} else if t.Lexeme == "lower" {
						f = strings.ToLower
					}

					switch obj1.(type) {
					case *MShellString:
						stack.Push(&MShellString{f(obj1.(*MShellString).Content)})
					case *MShellLiteral:
						stack.Push(&MShellLiteral{f(obj1.(*MShellLiteral).LiteralText)})
					case *MShellPath:
						stack.Push(&MShellPath{f(obj1.(*MShellPath).Path)})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot %s a %s (%s).\n", t.Line, t.Column, t.Lexeme, obj1.TypeName(), obj1.DebugString()))
					}
				} else if t.Lexeme == "hardLink" {
					newTarget, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'hardLink' operation on an empty stack.\n", t.Line, t.Column))
					}

					existingSource, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'hardLink' operation on a stack with only one item.\n", t.Line, t.Column))
					}

					sourcePath, err := existingSource.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot hardLink a %s.\n", t.Line, t.Column, existingSource.TypeName()))
					}

					targetPath, err := newTarget.CastString()
					if err != nil {

						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot hardLink to a %s.\n", t.Line, t.Column, newTarget.TypeName()))
					}

					err = os.Link(sourcePath, targetPath)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error hardLinking %s to %s: %s\n", t.Line, t.Column, sourcePath, targetPath, err.Error()))
					}
				} else if t.Lexeme == "tempFile" {
					tmpfile, err := os.CreateTemp("", "msh-")
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error creating temporary file: %s\n", t.Line, t.Column, err.Error()))
					}
					// Dump the full path to the stack
					stack.Push(&MShellPath{tmpfile.Name()})
				} else if t.Lexeme == "tempDir" {
					tmpdir := os.TempDir()
					stack.Push(&MShellPath{tmpdir})
				} else if t.Lexeme == "endsWith" || t.Lexeme == "startsWith" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					str1, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot check if a %s %s a %s.\n", t.Line, t.Column, obj1.TypeName(), t.Lexeme, obj2.TypeName()))
					}

					str2, err := obj2.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot check if a %s %s a %s.\n", t.Line, t.Column, obj2.TypeName(), t.Lexeme, obj1.TypeName()))
					}

					if t.Lexeme == "endsWith" {
						stack.Push(&MShellBool{strings.HasSuffix(str2, str1)})
					} else if t.Lexeme == "startsWith" {
						stack.Push(&MShellBool{strings.HasPrefix(str2, str1)})
					}
				} else if t.Lexeme == "isWeekend" || t.Lexeme == "isWeekday" || t.Lexeme == "dow" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
					}

					dateTimeObj, ok := obj1.(*MShellDateTime)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot check if a %s is a weekend.\n", t.Line, t.Column, obj1.TypeName()))
					}

					dayOfWeek := int(dateTimeObj.Time.Weekday())
					if t.Lexeme == "isWeekend" {
						stack.Push(&MShellBool{dayOfWeek == 0 || dayOfWeek == 6})
					} else if t.Lexeme == "isWeekday" {
						stack.Push(&MShellBool{dayOfWeek != 0 && dayOfWeek != 6})
					} else if t.Lexeme == "dow" {
						stack.Push(&MShellInt{dayOfWeek})
					}
				} else if t.Lexeme == "toUnixTime" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'unixTime' operation on an empty stack.\n", t.Line, t.Column))
					}
					dateTimeObj, ok := obj1.(*MShellDateTime)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the unix time of a %s (%s).\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					stack.Push(&MShellInt{int(dateTimeObj.Time.Unix())})
				} else if t.Lexeme == "fromUnixTime" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
					}

					intVal, ok := obj1.(*MShellInt)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s (%s) to a datetime.\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					stack.Push(&MShellDateTime{Time: time.Unix(int64(intVal.Value), 0).UTC(), Token: t})
				} else if t.Lexeme == "writeFile" || t.Lexeme == "appendFile" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					path, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot write to a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					content, err := obj2.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot write a %s to a file.\n", t.Line, t.Column, obj2.TypeName()))
					}

					var file *os.File
					if t.Lexeme == "writeFile" {
						file, err = os.Create(path)
					} else if t.Lexeme == "appendFile" {
						file, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
					}
					defer file.Close()

					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s: %s\n", t.Line, t.Column, path, err.Error()))
					}

					_, err = file.WriteString(content)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error writing to file %s: %s\n", t.Line, t.Column, path, err.Error()))
					}
				} else if t.Lexeme == "rm" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'rm' operation on an empty stack.\n", t.Line, t.Column))
					}

					path, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot remove a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					err = os.Remove(path)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error removing file %s: %s\n", t.Line, t.Column, path, err.Error()))
					}
				} else if t.Lexeme == "mv" || t.Lexeme == "cp" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					destination, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot %s to a %s.\n", t.Line, t.Column, t.Lexeme, obj1.TypeName()))
					}

					source, err := obj2.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot %s from a %s.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName()))
					}

					if t.Lexeme == "mv" {
						err = os.Rename(source, destination)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error in %s from '%s' to '%s': %s\n", t.Line, t.Column, t.Lexeme, source, destination, err.Error()))
						}
					} else if t.Lexeme == "cp" {
						err = CopyFile(source, destination)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error in %s from '%s' to '%s': %s\n", t.Line, t.Column, t.Lexeme, source, destination, err.Error()))
						}
					}
				} else if t.Lexeme == "skip" {
					// Skip like C# LINQ
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					intVal, ok := obj1.(*MShellInt)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot skip a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					newObj, err := obj2.SliceStart(intVal.Value)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
					}

					stack.Push(newObj)
				} else if t.Lexeme == "e" || t.Lexeme == "ec" || t.Lexeme == "es" {
					// Token Type
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot set stderr behavior to lines on an empty stack.\n", t.Line, t.Column))
					}

					switch obj.(type) {
					case *MShellList:
						list := obj.(*MShellList)
						if t.Lexeme == "e" {
							list.StderrBehavior = STDERR_LINES
						} else if t.Lexeme == "es" {
							list.StderrBehavior = STDERR_STRIPPED
						} else if t.Lexeme == "ec" {
							list.StderrBehavior = STDERR_COMPLETE
						} else {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: We haven't implemented the token type '%s' yet.\n", t.Line, t.Column, t.Type))
						}
						stack.Push(list)
					case *MShellPipe:
						pipe := obj.(*MShellPipe)
						if t.Lexeme == "e" {
							pipe.StderrBehavior = STDERR_LINES
						} else if t.Lexeme == "es" {
							pipe.StderrBehavior = STDERR_STRIPPED
						} else if t.Lexeme == "ec" {
							pipe.StderrBehavior = STDERR_COMPLETE
						} else {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: We haven't implemented the token type '%s' yet.\n", t.Line, t.Column, t.Type))
						}
						stack.Push(pipe)
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot set stdout behavior on a %s.\n", t.Line, t.Column, obj.TypeName()))
					}
				} else if t.Lexeme == "fileSize" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'fileSize' operation on an empty stack.\n", t.Line, t.Column))
					}

					path, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the file size of a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					var fileInfo os.FileInfo
					fileInfo, err = os.Stat(path)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error getting file size of %s: %s\n", t.Line, t.Column, path, err.Error()))
					}

					stack.Push(&MShellInt{int(fileInfo.Size())})
				} else if t.Lexeme == "lsDir" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'lsDir' operation on an empty stack.\n", t.Line, t.Column))
					}
					path, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the files in a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					// Get all the items in the directory
					files, err := os.ReadDir(path)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error getting items in directory %s: %s\n", t.Line, t.Column, path, err.Error()))
					}

					// Create a new list and add the files to it, full paths
					newList := NewList(0)
					for _, file := range files {
						newList.Items = append(newList.Items, &MShellPath{filepath.Join(path, file.Name())})
					}

					stack.Push(newList)
				} else if t.Lexeme == "runtime" {
					// Place the name of the current OS runtime on the stack
					stack.Push(&MShellString{runtime.GOOS})
				} else if t.Lexeme == "sort" ||  t.Lexeme == "sortV" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'sort' operation on an empty stack.\n", t.Line, t.Column))
					}

					// Check that obj1 is a list
					list, ok := obj1.(*MShellList)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot sort a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					var sortedList *MShellList

					if t.Lexeme == "sortV" || t.Lexeme == "sortVu" {
						sortedList, err = SortListFunc(list, VersionSortComparer)
					} else {
						sortedList, err = SortList(list)
					}

					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					stack.Push(sortedList)
				} else if t.Lexeme == "sha256sum" {
					// Get the SHA256 hash of a file
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'sha256sumfile' operation on an empty stack.\n", t.Line, t.Column))
					}

					path, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the SHA256 hash of a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					// Open the file
					file, err := os.Open(path)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s: %s\n", t.Line, t.Column, path, err.Error()))
					}
					defer file.Close()

					// Create a new SHA256 hash
					h := sha256.New()
					// Read the file and write it to the hash
					_, err = io.Copy(h, file)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error hashing file %s: %s\n", t.Line, t.Column, path, err.Error()))
					}

					// Get the hash sum
					sum := h.Sum(nil)
					// Convert to hex string
					hash := hex.EncodeToString(sum)
					// Push the hash onto the stack
					stack.Push(&MShellString{hash})
				} else if t.Lexeme == "get" {
					// Get a value from string key for a dictionary.
					// dict "key" get
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					keyStr, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The stack parameter for the dictionary key is not a string. Found a %s (%s).\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					dict, ok := obj2.(*MShellDict)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The stack parameter for the dictionary in 'get' is not a dictionary. Found a %s (%s). Key: %s\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString(), keyStr))
					}

					value, ok := dict.Items[keyStr]
					if !ok {
						// var sb strings.Builder
						// sb.WriteString(fmt.Sprintf("%d:%d: Key '%s' not found in dictionary.\n", t.Line, t.Column, keyStr))
						// sb.WriteString("Available keys:\n")
						// for k := range dict.Items {
							// // TODO: Escape
							// sb.WriteString(fmt.Sprintf("  - '%s'\n", k))
						// }
						// return state.FailWithMessage(sb.String())
						stack.Push(Maybe{ obj: nil })
					} else {
						maybe := Maybe{ obj: value }
						stack.Push( &maybe )
					}
				} else if t.Lexeme == "getDef" {
					// Get a value from string key for a dictionary.
					// dict "key" default getDef
					obj1, obj2, obj3, err := stack.Pop3(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					keyStr, err := obj2.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The stack parameter for the dictionary key is not a string. Found a %s (%s).\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString()))
					}

					dict, ok := obj3.(*MShellDict)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The stack parameter for the dictionary in '%s' is not a dictionary. Found a %s (%s). Key: %s\n, Default: %s\n", t.Line, t.Column, t.Lexeme, obj3.TypeName(), obj3.DebugString(), keyStr, obj1.DebugString()))
					}

					value, ok := dict.Items[keyStr]
					if !ok {
						stack.Push(obj1) // Push the default value
					} else {
						stack.Push(value)
					}
				} else if t.Lexeme == "set" || t.Lexeme == "setd" {
					// Set a value in a dictionary with a string key.
					// dict "key" value set
					value, key, dict, err := stack.Pop3(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					keyStr, err := key.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot set a value in a %s with a key %s.\n", t.Line, t.Column, dict.TypeName(), keyStr))
					}

					dictObj, ok := dict.(*MShellDict)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot set a value in a %s with a key %s.\n", t.Line, t.Column, dict.TypeName(), keyStr))
					}

					dictObj.Items[keyStr] = value
					if t.Lexeme == "set" {
						stack.Push(dictObj)
					}
				} else if t.Lexeme == "keys" || t.Lexeme == "values" {
					// Get the keys of a dictionary.
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
					}

					dict, ok := obj1.(*MShellDict)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the %s of a %s.\n", t.Line, t.Column, t.Lexeme, obj1.TypeName()))
					}

					// Create a new list and add the keys to it
					newList := NewList(len(dict.Items))
					i := 0

					if t.Lexeme == "keys" {
						for key := range dict.Items {
							newList.Items[i] = &MShellString{key}
							i++
						}
						sort.Slice(newList.Items, func(a, b int) bool {
							return newList.Items[a].(*MShellString).Content < newList.Items[b].(*MShellString).Content
						})
					} else if t.Lexeme == "values" {
						for _, value := range dict.Items {
							newList.Items[i] = value
							i++
						}
						sort.Slice(newList.Items, func(a, b int) bool {
							return newList.Items[a].DebugString() < newList.Items[b].DebugString()
						})
					} else {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: We haven't implemented the token type '%s' yet.\n", t.Line, t.Column, t.Lexeme))
					}

					stack.Push(newList)
				} else if t.Lexeme == "reMatch" {
					// Match a regex against a string
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					regexStr, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot match a regex against a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					str, err := obj2.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot match a regex against a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}

					re, err := regexp.Compile(regexStr)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error compiling regex '%s': %s\n", t.Line, t.Column, regexStr, err.Error()))
					}

					matches := re.FindStringSubmatch(str)
					if matches == nil {
						stack.Push(&MShellBool{false})
					} else {
						stack.Push(&MShellBool{true})
					}
				} else if t.Lexeme == "reReplace" {
					// Replace a regex in a string, fullString regex replacement
					obj1, obj2, obj3, err := stack.Pop3(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					replacement, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot use a %s as a replacement string.\n", t.Line, t.Column, obj1.TypeName()))
					}

					regexStr, err := obj2.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot use a %s as a regex.\n", t.Line, t.Column, obj2.TypeName()))
					}

					str, err := obj3.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot use a %s as a string to replace in.\n", t.Line, t.Column, obj3.TypeName()))
					}

					re, err := regexp.Compile(regexStr)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error compiling regex '%s': %s\n", t.Line, t.Column, regexStr, err.Error()))
					}

					newStr := re.ReplaceAllString(str, replacement)
					stack.Push(&MShellString{newStr})
				} else if t.Lexeme == "parseCsv" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'parseCsv' operation on an empty stack.\n", t.Line, t.Column))
					}

					// If a path or literal, read the file as UTF-8. Else, read the string as the contents directly.
					var reader *csv.Reader
					switch obj1.(type) {
					case *MShellPath, *MShellLiteral:
						path, _ := obj1.CastString()
						file, err := os.Open(path)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s: %s\n", t.Line, t.Column, path, err.Error()))
						}
						defer file.Close()
						reader = csv.NewReader(file)
					case *MShellString:
						// Create a new CSV reader directly from the string contents
						reader = csv.NewReader(strings.NewReader(obj1.(*MShellString).Content))
					}
					reader.FieldsPerRecord = -1

					// Create a new list and add the records to it
					newOuterList := NewList(0)

					// TODO: They are going to force me to roll my own CSV parser.
					// For now, going to have to deal with the skipped empty lines.
					for {
						record, err := reader.Read()
						if err == io.EOF {
							break
						} else if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error reading CSV: %s\n", t.Line, t.Column, err.Error()))
						}
						// Turn record into MShellList of strings
						newInnerList := NewList(len(record))
						for i, val := range record {
							newInnerList.Items[i] = &MShellString{val}
						}

						newOuterList.Items = append(newOuterList.Items, newInnerList)
					}
					stack.Push(newOuterList)
				} else if t.Lexeme == "parseJson" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'parseJson' operation on an empty stack.\n", t.Line, t.Column))
					}

					// If a path or literal, read the file as UTF-8. Else, read the string as the contents directly.
					var jsonData []byte
					switch obj1.(type) {
					case *MShellPath, *MShellLiteral:
						path, _ := obj1.CastString()
						file, err := os.Open(path)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s: %s\n", t.Line, t.Column, path, err.Error()))
						}

						defer file.Close()
						jsonData, err = io.ReadAll(file)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error reading file %s: %s\n", t.Line, t.Column, path, err.Error()))
						}

					case *MShellString:
						// Create a new JSON reader directly from the string contents
						jsonData = []byte(obj1.(*MShellString).Content)
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot parse a %s as JSON.\n", t.Line, t.Column, obj1.TypeName()))
					}

					var parsedData interface{}
					err = json.Unmarshal(jsonData, &parsedData)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing JSON: %s\n", t.Line, t.Column, err.Error()))
					}

					// Convert the parsed data to analgous MShell types
					resultObj := ParseJsonObjToMshell(parsedData)
					stack.Push(resultObj)
				} else if t.Lexeme == "type" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'type' operation on an empty stack.\n", t.Line, t.Column))
					}

					stack.Push(&MShellString{obj1.TypeName()})

				} else if t.Lexeme == "utcToCst" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'toCst' operation on an empty stack.\n", t.Line, t.Column))
					}

					// Convert the datetime to CST from assumed UTC
					dateTimeObj, ok := obj1.(*MShellDateTime)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s to CST.\n", t.Line, t.Column, obj1.TypeName()))
					}

					// Convert to CST
					cstLocation, err := time.LoadLocation("America/Chicago")
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error loading CST location: %s\n", t.Line, t.Column, err.Error()))
					}

					dateTimeObj.Time = dateTimeObj.Time.In(cstLocation)
					dateTimeObj.Token = t
					stack.Push(dateTimeObj)
				} else if t.Lexeme == "round" {
					// Round a float to the nearest integer
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'round' operation on an empty stack.\n", t.Line, t.Column))
					}

					if obj1.IsNumeric() {
						floatVal := obj1.FloatNumeric()
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot round a %s.\n", t.Line, t.Column, obj1.TypeName()))
						}

						rounded := int(math.Round(floatVal))
						stack.Push(&MShellInt{rounded})
					} else {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot round a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}
				} else if t.Lexeme == "toFixed" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					obj1Int, ok := obj1.(*MShellInt)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The number of decimal places parameter in toFixed is not an integer. Found a %s (%s)\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					if !obj2.IsNumeric() {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: For '%s', cannot convert a %s (%s) to a number.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName(), obj2.DebugString()))
					}

					floatVal := obj2.FloatNumeric()

					stack.Push(&MShellString{fmt.Sprintf("%.*f", obj1Int.Value, floatVal)})
				} else if t.Lexeme == "countSubStr" {
					// Count the number of times a substring appears in a string
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					subStr, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The first parameter in countSubStr is expected to be stringable, found a %s (%s)\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					str, err := obj2.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The second parameter in countSubStr is expected to be stringable, found a %s (%s)\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString()))
					}

					count := strings.Count(str, subStr)
					stack.Push(&MShellInt{count})
				} else if t.Lexeme == "uniq" {
					// Remove duplicates from a list
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'uniq' operation on an empty stack.\n", t.Line, t.Column))
					}

					if _, ok := obj1.(*MShellList); !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot remove duplicates from a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					listObj := obj1.(*MShellList)

					newList := NewList(0)
					stringsSeen := make(map[string]interface{})
					pathsSeen := make(map[string]interface{})
					intsSeen := make(map[int]interface{})
					floatsSeen := make(map[float64]interface{})
					dateTimesSeen := make(map[time.Time]interface{})

					for i, item := range listObj.Items {
						switch item.(type) {
						case *MShellString:
							strItem := item.(*MShellString)
							if _, ok := stringsSeen[strItem.Content]; !ok {
								newList.Items = append(newList.Items, strItem)
								stringsSeen[strItem.Content] = nil
							}
						case *MShellPath:
							pathItem := item.(*MShellPath)
							if _, ok := pathsSeen[pathItem.Path]; !ok {
								newList.Items = append(newList.Items, pathItem)
								pathsSeen[pathItem.Path] = nil
							}
						case *MShellInt:
							intItem := item.(*MShellInt)
							if _, ok := intsSeen[intItem.Value]; !ok {
								newList.Items = append(newList.Items, intItem)
								intsSeen[intItem.Value] = nil
							}
						case *MShellFloat:
							floatItem := item.(*MShellFloat)
							if _, ok := floatsSeen[floatItem.Value]; !ok {
								newList.Items = append(newList.Items, floatItem)
								floatsSeen[floatItem.Value] = nil
							}
						case *MShellDateTime:
							dateTimeItem := item.(*MShellDateTime)
							if _, ok := dateTimesSeen[dateTimeItem.Time]; !ok {
								newList.Items = append(newList.Items, dateTimeItem)
								dateTimesSeen[dateTimeItem.Time] = nil
							}
						case *MShellLiteral:
							// Treat like strings
							literalItem := item.(*MShellLiteral)
							if _, ok := stringsSeen[literalItem.LiteralText]; !ok {
								// Convert to a string
								newList.Items = append(newList.Items, &MShellString{literalItem.LiteralText})
								stringsSeen[literalItem.LiteralText] = nil
							}
						default:
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot remove duplicates from a list with a %s at index %d (%s).\n", t.Line, t.Column, item.TypeName(), i, item.DebugString()))
						}
					}

					stack.Push(newList)
				} else if t.Lexeme == "none" {
					stack.Push(&Maybe{obj: nil})
				} else if t.Lexeme == "just" {
					o, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'just' operation on an empty stack.\n", t.Line, t.Column))
					}
					stack.Push(&Maybe{obj: o})
				} else if t.Lexeme == "map" {
					// Map a function over a list, or a Maybe
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					// Check if obj1 is a function
					fn, ok := obj1.(*MShellQuotation)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The first parameter in 'map' is expected to be a function, found a %s (%s)\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					qContext, err := fn.BuildExecutionContext(&context)
					defer qContext.Close()
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					// Check if obj2 is a list or a Maybe
					switch obj2.(type) {
					case *MShellList:

						listObj := obj2.(*MShellList)
						newList := NewList(len(listObj.Items))

						var mapStack MShellStack
						mapStack = []MShellObject{}

						for i, item := range listObj.Items {
							mapStack.Push(item)
							result := state.Evaluate(fn.Tokens, &mapStack, (*qContext), definitions, CallStackItem{fn, "quote", CALLSTACKQUOTE})
							if result.ShouldPassResultUpStack() {
								return result
							}
							if len(mapStack) != 1 {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: The function in 'map' did not return a single value, found %d values.\n", t.Line, t.Column, len(mapStack)))
							}
							mapResult, _ := mapStack.Pop()
							newList.Items[i] = mapResult
						}
						stack.Push(newList)
					case *Maybe:
						maybe := obj2.(*Maybe)
						if maybe.obj == nil {
							stack.Push(maybe)
						} else {
							stack.Push(maybe.obj) // Push the object inside the Maybe
							preStackLen := len(*stack)
							result := state.Evaluate(fn.Tokens, stack, (*qContext), definitions, CallStackItem{fn, "quote", CALLSTACKQUOTE})
							if result.ShouldPassResultUpStack() {
								return result
							}
							if len(*stack) != preStackLen {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: The function in 'map' did not return a single value, found %d values.\n", t.Line, t.Column, len(*stack) - preStackLen))
							}
							mapResult, _ := stack.Pop()
							stack.Push(&Maybe{obj: mapResult}) // Wrap the result back in a Maybe
						}
					}
				} else if t.Lexeme == "isNone" {
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
					}

					if maybeObj, ok := obj.(*Maybe); ok {
						stack.Push(&MShellBool{ maybeObj.IsNone() })
					} else {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot check if a %s is None.\n", t.Line, t.Column, obj.TypeName()))
					}
				} else if t.Lexeme == "maybe" {
					// [Maybe] [default value]
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					// Check that obj2 is a Maybe
					maybeObj, ok := obj2.(*Maybe)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The second parameter in 'maybe' is expected to be a Maybe, found a %s (%s)\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString()))
					}

					if maybeObj.obj == nil {
						stack.Push(obj1) // Push the default value
					} else {
						stack.Push(maybeObj.obj)
					}
				}  else { // last new function
					// If we aren't in a list context, throw an error.
					// Nearly always this is unintended.
					if callStackItem.CallStackType != CALLSTACKLIST {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Found literal token '%s' outside of a list context. Normally this is unintended. Either make it a string literal or path, or ensure the definition is available.\n", t.Line, t.Column, t.Lexeme))
					}

					stack.Push(&MShellLiteral{t.Lexeme})
				}
			} else if t.Type == STDAPPEND {
				redirectPath, obj2, err := stack.Pop2(t)
				if err != nil {
					return state.FailWithMessage(err.Error())
				}

				path, err := redirectPath.CastString()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot append to a %s.\n", t.Line, t.Column, redirectPath.TypeName()))
				}

				var listObj *MShellList
				var ok bool
				if listObj, ok = obj2.(*MShellList); !ok {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Second item on stack for %s should be a list, found a %s.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName()))
				}

				listObj.StandardOutputFile = path
				listObj.AppendOutput = true
				stack.Push(listObj)
			} else if t.Type == LEFT_SQUARE_BRACKET { // Token Type
				return state.FailWithMessage(fmt.Sprintf("%d:%d: Found unexpected left square bracket.\n", t.Line, t.Column))
			} else if t.Type == LEFT_PAREN { // Token Type
				return state.FailWithMessage(fmt.Sprintf("%d:%d: Found unexpected left parenthesis.\n", t.Line, t.Column))
			} else if t.Type == EXECUTE || t.Type == QUESTION || t.Type == BANG { // Token Type
				top, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation an empty stack.\n", t.Line, t.Column, t.Lexeme))
				}

				if maybe, ok := top.(*Maybe); ok {
					// If none, we fail and return all the way up the stack.
					if maybe.obj == nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Failed on '?' with None value.\n", t.Line, t.Column))
					} else {
						stack.Push(maybe.obj) // Push the object inside the Maybe
					}
					continue MainLoop
				}

				// Switch on type
				var result EvalResult
				var exitCode int
				var stdout string
				var stderr string

				switch top.(type) {
				case *MShellList:
					result, exitCode, stdout, stderr = RunProcess(*top.(*MShellList), context, state)
				case *MShellPipe:
					result, exitCode, stdout, stderr = state.RunPipeline(*top.(*MShellPipe), context, stack)
				default:
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot execute a non-list object. Found %s %s\n", t.Line, t.Column, top.TypeName(), top.DebugString()))
				}

				if (state.StopOnError || (t.Type == BANG)) && exitCode != 0 {
					// Exit completely, with that exit code, don't need to print a different message. Usually the command itself will have printed an error.
					return EvalResult{false, false, -1, exitCode, false}
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

				var stderrBehavior StderrBehavior
				switch top.(type) {
				case *MShellList:
					stderrBehavior = top.(*MShellList).StderrBehavior
				case *MShellPipe:
					stderrBehavior = top.(*MShellPipe).StderrBehavior
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

				if stderrBehavior == STDERR_LINES {
					newMShellList := NewList(0)
					var scanner *bufio.Scanner
					scanner = bufio.NewScanner(strings.NewReader(stderr))
					for scanner.Scan() {
						newMShellList.Items = append(newMShellList.Items, &MShellString{scanner.Text()})
					}
					stack.Push(newMShellList)
				} else if stderrBehavior == STDERR_STRIPPED {
					stripped := strings.TrimSpace(stderr)
					stack.Push(&MShellString{stripped})
				} else if stderrBehavior == STDERR_COMPLETE {
					stack.Push(&MShellString{stderr})
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
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing integer: %s\n", t.Line, t.Column, err.Error()))
				}

				stack.Push(&MShellInt{intVal})
			} else if t.Type == STRING { // Token Type
				parsedString, err := ParseRawString(t.Lexeme)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing string: %s\n", t.Line, t.Column, err.Error()))
				}
				stack.Push(&MShellString{parsedString})
			} else if t.Type == SINGLEQUOTESTRING { // Token Type
				stack.Push(&MShellString{t.Lexeme[1 : len(t.Lexeme)-1]})
			} else if t.Type == IFF {
				iff_name := "iff"
				firstObj, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do an '%s' on a stack with only two items.\n", t.Line, t.Column, iff_name))
				}

				// Check that first obj is a quotation
				firstQuote, ok := firstObj.(*MShellQuotation)
				if !ok {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Expected a quotation on top of stack for %s, received a %s.\n", t.Line, t.Column, iff_name, firstObj.TypeName()))
				}

				secondObj, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do an '%s' on a stack with only one item.\n", t.Line, t.Column, iff_name))
				}

				var trueQuote *MShellQuotation
				var falseQuote *MShellQuotation
				var condition bool

				// Check that second obj is a quotation, boolean, or integer
				switch secondObj.(type) {
				case *MShellQuotation:
					falseQuote = firstQuote

					trueQuote = secondObj.(*MShellQuotation)

					// Read the next object, should be bool or integer
					thrirdObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do an '%s' on a stack with only two quotes.\n", t.Line, t.Column, iff_name))
					}

					switch thrirdObj.(type) {
					case *MShellBool:
						condition = thrirdObj.(*MShellBool).Value
					case *MShellInt:
						condition = thrirdObj.(*MShellInt).Value == 0
					}
				case *MShellBool:
					trueQuote = firstQuote
					condition = secondObj.(*MShellBool).Value
				case *MShellInt:
					trueQuote = firstQuote
					condition = secondObj.(*MShellInt).Value == 0
				default:
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Expected a quotation or boolean for %s, received a %s.\n", t.Line, t.Column, iff_name, secondObj.TypeName()))
				}

				var quoteToExecute *MShellQuotation
				if condition {
					quoteToExecute = trueQuote
				} else {
					quoteToExecute = falseQuote
				}

				// False quote could be nil in the true only style of iff
				if quoteToExecute != nil {
					qContext, err := quoteToExecute.BuildExecutionContext(&context)
					defer qContext.Close()
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					result := state.Evaluate(quoteToExecute.Tokens, stack, (*qContext), definitions, CallStackItem{trueQuote, "quote", CALLSTACKQUOTE})

					if result.ShouldPassResultUpStack() {
						return result
					}
				}

			} else if t.Type == IF { // Token Type
				obj, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do an 'if' on an empty stack.\n", t.Line, t.Column))
				}

				list, ok := obj.(*MShellList)
				if !ok {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Argument for if expected to be a list of quoations, received a %s\n", t.Line, t.Column, obj.TypeName()))
				}

				if len(list.Items) < 2 {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: If statement requires a list with at least 2 items. Found %d.\n", t.Line, t.Column, len(list.Items)))
				}

				// Check that all items are quotations
				for i, item := range list.Items {
					if _, ok := item.(*MShellQuotation); !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Item %d in if statement is not a quotation.\n", t.Line, t.Column, i))
					}
				}

				trueIndex := -1

			ListLoop:
				for i := 0; i < len(list.Items)-1; i += 2 {
					quotation := list.Items[i].(*MShellQuotation)

					result := state.Evaluate(quotation.Tokens, stack, context, definitions, CallStackItem{quotation, "quote", CALLSTACKQUOTE})

					if !result.Success || result.ExitCalled {
						return result
					}

					if result.BreakNum > 0 {
						return state.FailWithMessage("Encountered break within if statement.\n")
					}

					if result.Continue {
						return state.FailWithMessage("Encountered continue within if statement.\n")
					}

					top, err := stack.Pop()
					if err != nil {
						conditionNum := i/2 + 1
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Found an empty stack when evaluating condition #%d .\n", t.Line, t.Column, conditionNum))
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
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Expected an integer or boolean for condition #%d, received a %s.\n", t.Line, t.Column, i/2+1, top.TypeName()))
					}
				}

				if trueIndex > -1 {
					quotation := list.Items[trueIndex+1].(*MShellQuotation)

					result := state.Evaluate(quotation.Tokens, stack, context, definitions, CallStackItem{quotation, "quote", CALLSTACKQUOTE})

					// If we encounter a break, we should return it up the stack
					if result.ShouldPassResultUpStack() {
						return result
					}
				} else if len(list.Items)%2 == 1 { // Try to find a final else statement, will be the last item in the list if odd number of items
					quotation := list.Items[len(list.Items)-1].(*MShellQuotation)

					result := state.Evaluate(quotation.Tokens, stack, context, definitions, CallStackItem{quotation, "quote", CALLSTACKQUOTE})

					if result.ShouldPassResultUpStack() {
						return result
					}
				}
			} else if t.Type == PLUS { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '+' operation on an empty stack.\n", t.Line, t.Column))
				}

				obj2, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '+' operation on a stack with only one item.\n", t.Line, t.Column))
				}

				switch obj1.(type) {
				case *MShellInt:
					switch obj2.(type) {
					case *MShellInt:
						stack.Push(&MShellInt{obj2.(*MShellInt).Value + obj1.(*MShellInt).Value})
					case *MShellFloat:
						stack.Push(&MShellFloat{float64(obj2.(*MShellFloat).Value) + float64(obj1.(*MShellInt).Value)})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot add an integer to a %s (%s).\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString()))
					}
				case *MShellFloat:
					switch obj2.(type) {
					case *MShellFloat:
						stack.Push(&MShellFloat{obj2.(*MShellFloat).Value + obj1.(*MShellFloat).Value})
					case *MShellInt:
						stack.Push(&MShellFloat{float64(obj2.(*MShellInt).Value) + obj1.(*MShellFloat).Value})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot add a float to a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				case *MShellString:
					switch obj2.(type) {
					case *MShellString:
						stack.Push(&MShellString{obj2.(*MShellString).Content + obj1.(*MShellString).Content})
					case *MShellLiteral:
						stack.Push(&MShellString{obj2.(*MShellLiteral).LiteralText + obj1.(*MShellString).Content})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot add a string ('%s') to a %s (%s).\n", t.Line, t.Column, obj1.(*MShellString).Content, obj2.TypeName(), obj2.DebugString()))
					}
				case *MShellLiteral:
					switch obj2.(type) {
					case *MShellString:
						stack.Push(&MShellString{obj2.(*MShellString).Content + obj1.(*MShellLiteral).LiteralText})
					case *MShellLiteral:
						stack.Push(&MShellString{obj2.(*MShellLiteral).LiteralText + obj1.(*MShellLiteral).LiteralText})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot add a literal (%s) to a %s.\n", t.Line, t.Column, obj1.DebugString(), obj2.TypeName()))
					}
				case *MShellList:
					switch obj2.(type) {
					case *MShellList:
						newList := NewList(len(obj2.(*MShellList).Items)+len(obj1.(*MShellList).Items))
						copy(newList.Items, obj2.(*MShellList).Items)
						copy(newList.Items[len(obj2.(*MShellList).Items):], obj1.(*MShellList).Items)
						stack.Push(newList)
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot add a list to a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				case *MShellPath:
					switch obj2.(type) {
					case *MShellPath:
						// Do string join, not path join. Concat the strings
						stack.Push(&MShellPath{obj2.(*MShellPath).Path + obj1.(*MShellPath).Path})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot add a path to a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				default:
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '+' between a %s and a %s.\n", t.Line, t.Column, obj2.TypeName(), obj1.TypeName()))
				}
			} else if t.Type == MINUS { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '-' operation on an empty stack.\n", t.Line, t.Column))
				}

				obj2, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '-' operation on a stack with only one item.\n", t.Line, t.Column))
				}

				switch obj1.(type) {
				case *MShellInt:
					switch obj2.(type) {
					case *MShellInt:
						stack.Push(&MShellInt{obj2.(*MShellInt).Value - obj1.(*MShellInt).Value})
					case *MShellFloat:
						stack.Push(&MShellFloat{obj2.(*MShellFloat).Value - float64(obj1.(*MShellInt).Value)})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot subtract an integer from a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				case *MShellFloat:
					switch obj2.(type) {
					case *MShellFloat:
						stack.Push(&MShellFloat{obj2.(*MShellFloat).Value - obj1.(*MShellFloat).Value})
					case *MShellInt:
						stack.Push(&MShellFloat{float64(obj2.(*MShellInt).Value) - obj1.(*MShellFloat).Value})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot subtract a float from a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				case *MShellDateTime:
					switch obj2.(type) {
					case *MShellDateTime:
						// Return a float with the difference in days.
						days := obj2.(*MShellDateTime).Time.Sub(obj1.(*MShellDateTime).Time).Hours() / 24
						stack.Push(&MShellFloat{days})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot subtract a %s from a %s.\n", t.Line, t.Column, obj2.TypeName(), obj1.TypeName()))
					}
				default:
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '-' to a %s and %s.\n", t.Line, t.Column, obj2.TypeName(), obj1.TypeName()))
				}
			} else if t.Type == AND || t.Type == OR { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
				}

				obj2, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on a stack with only one item.\n", t.Line, t.Column, t.Lexeme))
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
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '%s' to a %s and %s.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName(), obj1.TypeName()))
					}
				case *MShellQuotation:
					if t.Type == AND {
						if obj2.(*MShellBool).Value {
							qContext, err := obj1.(*MShellQuotation).BuildExecutionContext(&context)
							defer qContext.Close()
							if err != nil {
								return state.FailWithMessage(err.Error())
							}

							result := state.Evaluate(obj1.(*MShellQuotation).Tokens, stack, (*qContext), definitions, CallStackItem{obj1.(*MShellQuotation), t.Lexeme, CALLSTACKQUOTE})
							// Pop the top off the stack
							secondObj, err := stack.Pop()
							if err != nil {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: After executing the quotation in %s, the stack was empty.\n", t.Line, t.Column, t.Lexeme))
							}

							if result.ShouldPassResultUpStack() {
								return result
							}

							seconObjBool, ok := secondObj.(*MShellBool)
							if !ok {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Expected a boolean after executing the quotation in %s, received a %s.\n", t.Line, t.Column, t.Lexeme, secondObj.TypeName()))
							}

							stack.Push(&MShellBool{seconObjBool.Value})
						} else {
							stack.Push(&MShellBool{false})
						}
					} else {
						if obj2.(*MShellBool).Value {
							stack.Push(&MShellBool{true})
						} else {
							qContext, err := obj1.(*MShellQuotation).BuildExecutionContext(&context)
							defer qContext.Close()
							if err != nil {
								return state.FailWithMessage(err.Error())
							}

							result := state.Evaluate(obj1.(*MShellQuotation).Tokens, stack, (*qContext), definitions, CallStackItem{obj1.(*MShellQuotation), t.Lexeme, CALLSTACKQUOTE})
							// Pop the top off the stack
							secondObj, err := stack.Pop()
							if err != nil {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: After executing the quotation in %s, the stack was empty.\n", t.Line, t.Column, t.Lexeme))
							}

							if result.ShouldPassResultUpStack() {
								return result
							}

							seconObjBool, ok := secondObj.(*MShellBool)
							if !ok {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Expected a boolean after executing the quotation in %s, received a %s.\n", t.Line, t.Column, t.Lexeme, secondObj.TypeName()))
							}

							stack.Push(&MShellBool{seconObjBool.Value})
						}
					}
				default:
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '%s' to a %s and %s.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName(), obj1.TypeName()))
				}
			} else if t.Type == NOT { // Token Type
				obj, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
				}

				switch obj.(type) {
				case *MShellBool:
					stack.Push(&MShellBool{!obj.(*MShellBool).Value})
				case *MShellInt:
					if obj.(*MShellInt).Value == 0 {
						stack.Push(&MShellBool{false})
					} else {
						stack.Push(&MShellBool{true})
					}
				default:
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '%s' to a %s.\n", t.Line, t.Column, t.Lexeme, obj.TypeName()))
				}
			} else if t.Type == GREATERTHANOREQUAL || t.Type == LESSTHANOREQUAL { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
				}

				obj2, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on a stack with only one item.\n", t.Line, t.Column, t.Lexeme))
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
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '%s' to a %s and a %s.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName(), obj1.TypeName()))
					}
				}
			} else if t.Type == GREATERTHAN || t.Type == LESSTHAN { // Token Type
				// This can either be normal comparison for numerics, or it's a redirect on a list or quotation.
				obj1, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
				}

				obj2, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on a stack with only one item.\n", t.Line, t.Column, t.Lexeme))
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
								obj2.(*MShellList).StandardOutputFile = obj1.(*MShellString).Content
							} else { // LESSTHAN, input redirection
								obj2.(*MShellList).StdinBehavior = STDIN_CONTENT
								obj2.(*MShellList).StandardInputContents = obj1.(*MShellString).Content
							}
							stack.Push(obj2)
						case *MShellQuotation:
							if t.Type == GREATERTHAN {
								obj2.(*MShellQuotation).StandardOutputFile = obj1.(*MShellString).Content
							} else { // LESSTHAN, input redirection
								obj2.(*MShellQuotation).StdinBehavior = STDIN_CONTENT
								obj2.(*MShellQuotation).StandardInputContents = obj1.(*MShellString).Content
							}
							stack.Push(obj2)
						case *MShellPipe:
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a string (%s) to a Pipe (%s). Add the redirection to the final item in the pipeline.\n", t.Line, t.Column, obj1.DebugString(), obj2.DebugString()))
						default:
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a string (%s) to a %s (%s).\n", t.Line, t.Column, obj1.DebugString(), obj2.TypeName(), obj2.DebugString()))
						}
					case *MShellLiteral:
						switch obj2.(type) {
						case *MShellList:
							if t.Type == GREATERTHAN {
								obj2.(*MShellList).StandardOutputFile = obj1.(*MShellLiteral).LiteralText
							} else { // LESSTHAN, input redirection
								obj2.(*MShellList).StdinBehavior = STDIN_CONTENT
								obj2.(*MShellList).StandardInputFile = obj1.(*MShellLiteral).LiteralText
							}
							stack.Push(obj2)
						case *MShellQuotation:
							if t.Type == GREATERTHAN {
								obj2.(*MShellQuotation).StandardOutputFile = obj1.(*MShellLiteral).LiteralText
							} else {
								obj2.(*MShellQuotation).StdinBehavior = STDIN_CONTENT
								obj2.(*MShellQuotation).StandardInputContents = obj1.(*MShellLiteral).LiteralText
							}
						default:
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a %s (%s) to a %s (%s).\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString(), obj2.TypeName(), obj2.DebugString()))
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
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a path (%s) to a %s (%s).\n", t.Line, t.Column, obj1.DebugString(), obj2.TypeName(), obj2.DebugString()))
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
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a datetime (%s) to a %s (%s).\n", t.Line, t.Column, obj1.DebugString(), obj2.TypeName(), obj2.DebugString()))
						}
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a %s (%s) to a %s (%s).\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString(), obj2.TypeName(), obj2.DebugString()))
					}
				}
			} else if t.Type == STDERRREDIRECT { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect stderr on an empty stack.\n", t.Line, t.Column))
				}
				obj2, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect stderr on a stack with only one item.\n", t.Line, t.Column))
				}

				redirectFile, err := obj1.CastString()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect stderr to a %s.\n", t.Line, t.Column, obj1.TypeName()))
				}

				switch obj2.(type) {
				case *MShellList:
					obj2.(*MShellList).StandardErrorFile = redirectFile
					stack.Push(obj2)
				case *MShellQuotation:
					obj2.(*MShellQuotation).StandardErrorFile = redirectFile
					stack.Push(obj2)
				default:
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect stderr to a %s.\n", t.Line, t.Column, obj2.TypeName()))
				}
			} else if t.Type == ENVSTORE { // Token Type
				obj, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Nothing on stack to set into %s environment variable.\n", t.Line, t.Column, t.Lexeme))
				}

				// Strip off the leading '$' and trailing '!' for the environment variable name
				varName := t.Lexeme[1:len(t.Lexeme) - 1]

				varValue, err := obj.CastString()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot export a %s.\n", t.Line, t.Column, obj.TypeName()))
				}

				err = os.Setenv(varName, varValue)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Could not set the environment variable '%s' to '%s'.\n", t.Line, t.Column, varName, varValue))
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
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Could not get the environment variable '%s'.\n", t.Line, t.Column, envVarName))
				}

				stack.Push(&MShellString{varValue})

			} else if t.Type == VARSTORE { // Token Type
				obj, err := stack.Pop()
				varName := t.Lexeme[0 : len(t.Lexeme)-1] // Remove the trailing !

				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Nothing on stack to store into variable %s.\n", t.Line, t.Column, varName))
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
						return state.FailWithMessage(message.String())
					}
				}
			} else if t.Type == LOOP { // Token Type
				obj, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do a loop on an empty stack.\n", t.Line, t.Column))
				}

				quotation, ok := obj.(*MShellQuotation)
				if !ok {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Argument for loop expected to be a quotation, received a %s\n", t.Line, t.Column, obj.TypeName()))
				}

				if len(quotation.Tokens) == 0 {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Loop quotation needs a minimum of one token.\n", t.Line, t.Column))
				}

				loopContext := ExecuteContext{
					StandardInput:  nil,
					StandardOutput: nil,
					Variables:      context.Variables,
					Pbm: 		context.Pbm,
				}

				if quotation.StdinBehavior != STDIN_NONE {
					if quotation.StdinBehavior == STDIN_CONTENT {
						loopContext.StandardInput = strings.NewReader(quotation.StandardInputContents)
					} else if quotation.StdinBehavior == STDIN_FILE {
						file, err := os.Open(quotation.StandardInputFile)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s for reading: %s\n", t.Line, t.Column, quotation.StandardInputFile, err.Error()))
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
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s for writing: %s\n", t.Line, t.Column, quotation.StandardOutputFile, err.Error()))
					}
					loopContext.StandardOutput = file
					defer file.Close()
				}

				maxLoops := 1500000
				loopCount := 0
				state.LoopDepth++

				// breakDiff := 0

				initialStackSize := len(*stack)

				for loopCount < maxLoops {
					result := state.Evaluate(quotation.Tokens, stack, loopContext, definitions, CallStackItem{quotation, "quote", CALLSTACKQUOTE})
					if !result.Success || result.ExitCalled {
						return result
					}

					if len(*stack) != initialStackSize {
						// If the stack size changed, we have an error.
						var errorMessage strings.Builder
						errorMessage.WriteString(fmt.Sprintf("%d:%d: Stack size changed from %d to %d in loop.\n", t.Line, t.Column, initialStackSize, len(*stack)))

						errorMessage.WriteString("Stack:\n")
						for i, item := range *stack {
							errorMessage.WriteString(fmt.Sprintf("  %d: %s\n", i, item.DebugString()))
						}

						return state.FailWithMessage(errorMessage.String())
					}


					// Assert that we never get into state in which we have a breakNum > 0 and continue == true
					if result.BreakNum > 0 && result.Continue {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot have both break and continue in the same loop.\n", t.Line, t.Column))
					}

					if result.BreakNum > 0 {
						// breakDiff = state.LoopDepth - result.BreakNum
						// if breakDiff >= 0 {
						break
						// }
					}

					if result.Continue {
						continue
					}

					loopCount++
				}

				if loopCount == maxLoops {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Loop exceeded maximum number of iterations (%d).\n", t.Line, t.Column, maxLoops))
				}

				state.LoopDepth--
				// // If we are breaking out of an inner loop to an outer loop (breakDiff - 1 > 0), then we need to return and go up the call stack.
				// // Else just continue on with tokens after the loop.
				// if breakDiff-1 > 0 {
					// fmt.Fprintf(os.Stderr, "Breaking out of loop %d, loop depth %d\n", breakDiff-1, state.LoopDepth)
					// return EvalResult{true, breakDiff - 1, 0, false}
				// }
			} else if t.Type == BREAK { // Token Type
				return EvalResult{true, false, 1, 0, false}
			} else if t.Type == CONTINUE { // Token Type
				return EvalResult{true, true, 0, 0, false}
			} else if t.Type == EQUALS { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '=' operation on an empty stack.\n", t.Line, t.Column))
				}
				obj2, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '=' operation on a stack with only one item.\n", t.Line, t.Column))
				}

				doesEqual, err := obj1.Equals(obj2)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot compare '=' between %s (%s) and %s (%s): %s\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString(), obj2.TypeName(), obj2.DebugString(), err.Error()))
				}

				stack.Push(&MShellBool{doesEqual})
			} else if t.Type == INTERPRET { // Token Type
				obj, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot interpret an empty stack.\n", t.Line, t.Column))
				}

				quotation, ok := obj.(*MShellQuotation)
				if !ok {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Argument for interpret expected to be a quotation, received a %s (%s)\n", t.Line, t.Column, obj.TypeName(), obj.DebugString()))
				}

				quoteContext, err := quotation.BuildExecutionContext(&context)
				defer quoteContext.Close()

				if err != nil {
					return state.FailWithMessage(err.Error())
				}

				result := state.Evaluate(quotation.Tokens, stack, (*quoteContext), definitions, CallStackItem{quotation, "quote", CALLSTACKQUOTE})

				if result.ShouldPassResultUpStack() {
					return result
				}
			} else if t.Type == POSITIONAL { // Token Type
				posNum := t.Lexeme[1:]
				posIndex, err := strconv.Atoi(posNum)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing positional argument number: %s\n", t.Line, t.Column, err.Error()))
				}

				if posIndex == 0 {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Positional argument are 1-based, first argument is $1, not $0.\n", t.Line, t.Column))
				}

				if posIndex < 0 {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Positional argument numbers must be positive.\n", t.Line, t.Column))
				}

				if posIndex > len(state.PositionalArgs) {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Positional argument %s is greater than the number of arguments provided.\n", t.Line, t.Column, t.Lexeme))
				}

				stack.Push(&MShellString{state.PositionalArgs[posIndex-1]})
			} else if t.Type == PIPE { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
				}

				// obj1 should be a list
				list, ok := obj1.(*MShellList)
				if !ok {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot pipe a %s.\n", t.Line, t.Column, obj1.TypeName()))
				}

				stack.Push(&MShellPipe{*list, list.StdoutBehavior, list.StderrBehavior})
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
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error reading from stdin: %s\n", t.Line, t.Column, err.Error()))
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
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error resetting position of reader: %s\n", t.Line, t.Column, err.Error()))
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
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Error reading from stdin: %s\n", t.Line, t.Column, err.Error()))
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
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert an empty stack to a string.\n", t.Line, t.Column))
				}

				stack.Push(&MShellString{obj.ToString()})
			} else if t.Type == INDEXER { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot index an empty stack.\n", t.Line, t.Column))
				}

				// Indexer is a digit between ':' and ':'. Remove ends and parse the number
				indexStr := t.Lexeme[1 : len(t.Lexeme)-1]
				index, err := strconv.Atoi(indexStr)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing index: %s\n", t.Line, t.Column, err.Error()))
				}

				result, err := obj1.Index(index)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: %s", t.Line, t.Column, err.Error()))
				}
				stack.Push(result)
			} else if t.Type == ENDINDEXER || t.Type == STARTINDEXER { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot end index an empty stack.\n", t.Line, t.Column))
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
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing index: %s\n", t.Line, t.Column, err.Error()))
				}

				var result MShellObject
				if t.Type == ENDINDEXER {
					result, err = obj1.SliceEnd(index)
				} else {
					result, err = obj1.SliceStart(index)
				}

				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: %s", t.Line, t.Column, err.Error()))
				}
				stack.Push(result)
			} else if t.Type == SLICEINDEXER { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot slice index an empty stack.\n", t.Line, t.Column))
				}

				// StartInc:EndExc
				parts := strings.Split(t.Lexeme, ":")
				startInt, err := strconv.Atoi(parts[0])
				endInt, err2 := strconv.Atoi(parts[1])

				if err != nil || err2 != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing slice indexes: %s\n", t.Line, t.Column, err.Error()))
				}

				result, err := obj1.Slice(startInt, endInt)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot slice index a %s.\n", t.Line, t.Column, obj1.TypeName()))
				}

				stack.Push(result)
			} else if t.Type == STDOUTLINES || t.Type == STDOUTSTRIPPED || t.Type == STDOUTCOMPLETE { // Token Type
				obj, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot set stdout behavior to lines on an empty stack.\n", t.Line, t.Column))
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
						return state.FailWithMessage(fmt.Sprintf("%d:%d: We haven't implemented the token type '%s' yet.\n", t.Line, t.Column, t.Type))
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
						return state.FailWithMessage(fmt.Sprintf("%d:%d: We haven't implemented the token type '%s' yet.\n", t.Line, t.Column, t.Type))
					}
					stack.Push(pipe)
				default:
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot set stdout behavior on a %s.\n", t.Line, t.Column, obj.TypeName()))
				}
			} else if t.Type == STOP_ON_ERROR { // Token Type
				state.StopOnError = true
			} else if t.Type == FLOAT { // Token Type
				floatVal, err := strconv.ParseFloat(t.Lexeme, 64)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing float: %s\n", t.Line, t.Column, err.Error()))
				}
				stack.Push(&MShellFloat{floatVal})
			} else if t.Type == PATH { // Token Type
				parsed, err := ParseRawPath(t.Lexeme)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing path: %s\n", t.Line, t.Column, err.Error()))
				}
				stack.Push(&MShellPath { parsed })
			} else if t.Type == DATETIME { // Token Type
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
			} else if t.Type == FORMATSTRING { // Token Type
				parsedString, err := state.EvaluateFormatString(t.Lexeme, context, definitions, callStackItem)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing format string '%s': %s\n", t.Line, t.Column, t.Lexeme, err.Error()))
				}

				stack.Push(parsedString)
			} else if t.Type == AMPERSAND { // Token Type
				obj, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the address of an empty stack.\n", t.Line, t.Column))
				}
				// Obj should be a list for now.
				list, ok := obj.(*MShellList)
				if !ok {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot execute '&' on a %s.\n", t.Line, t.Column, obj.TypeName()))
				}

				list.RunInBackground = true
				stack.Push(list)
			} else if t.Type == NOTEQUAL { // Token Type
				obj1, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '!=' operation on an empty stack.\n", t.Line, t.Column))
				}

				obj2, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '!=' operation on a stack with only one item.\n", t.Line, t.Column))
				}

				doesEqual, err := obj1.Equals(obj2)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot compare '!=' between %s and %s: %s\n", t.Line, t.Column, obj1.TypeName(), obj2.TypeName(), err.Error()))
				}

				stack.Push(&MShellBool{!doesEqual})
			}  else {
				return state.FailWithMessage(fmt.Sprintf("%d:%d: We haven't implemented the token type '%s' ('%s') yet.\n", t.Line, t.Column, t.Type, t.Lexeme))
			}
		default:
			return state.FailWithMessage(fmt.Sprintf("We haven't implemented the type '%T' yet.\n", t))
		}

	}

	return EvalResult{true, false, -1, 0, false}
}

const (
	FORMATMODENORMAL = iota
	FORMATMODEESCAPE
	FORMATMODEFORMAT
)

func (state *EvalState) EvaluateFormatString(lexeme string, context ExecuteContext, definitions []MShellDefinition, callStackItem CallStackItem) (*MShellString, error) {
	if len(lexeme) < 3 {
		return nil, fmt.Errorf("Found format string with less than 3 characters: %s", lexeme)
	}

	var b strings.Builder

	index := 2
	mode := FORMATMODENORMAL

	formatStrStartIndex := -1
	formatStrEndIndex := -1

	lexer := NewLexer("")
	parser := MShellParser{ lexer: lexer }

	for index < len(lexeme)-1 {
		c := lexeme[index]
		index++

		if mode == FORMATMODEESCAPE {
			switch c {
			case 'e':
				b.WriteRune('\033')
			case 'n':
				b.WriteRune('\n')
			case 't':
				b.WriteRune('\t')
			case 'r':
				b.WriteRune('\r')
			case '\\':
				b.WriteRune('\\')
			case '"':
				b.WriteRune('"')
			case '{':
				b.WriteRune('{') // This is a literal '{' in the format string
			default:
				return nil, fmt.Errorf("invalid escape character '%c'", c)
			}
			mode = FORMATMODENORMAL
		} else if mode == FORMATMODENORMAL {
			if c == '\\' {
				mode = FORMATMODEESCAPE
			} else if c == '{' {
				formatStrStartIndex = index - 1
				mode = FORMATMODEFORMAT
			} else {
				b.WriteRune(rune(c))
			}
		} else if mode == FORMATMODEFORMAT {
			if c == '}' {
				formatStrEndIndex = index - 1
				formatStr := lexeme[formatStrStartIndex+1:formatStrEndIndex]

				// Evaluate the format string
				lexer.resetInput(formatStr)
				parser.NextToken()
				contents, err := parser.ParseFile()
				if err != nil {
					return nil, fmt.Errorf("Error parsing format string %s: %s", formatStr, err)
				}

				// Evaluate the format string contents
				var stack MShellStack
				stack = []MShellObject{}
				result := state.Evaluate(contents.Items, &stack, context, definitions, callStackItem)
				if !result.Success {
					return nil, fmt.Errorf("Error evaluating format string %s", formatStr)
				}

				if len(stack) != 1 {
					return nil, fmt.Errorf("Format string %s did not evaluate to a single value", formatStr)
				}

				// Get the string representation of the result
				resultStr, err := stack[0].CastString()
				if err != nil {
					return nil, fmt.Errorf("Format string contents %s did not evaluate to a stringable value", formatStr)
				}

				b.WriteString(resultStr)

				formatStrStartIndex = -1
				formatStrEndIndex = -1
				mode = FORMATMODENORMAL
			}
		} else {
			panic("Unknown format mode")
		}
	}

	if mode != FORMATMODENORMAL {
		return nil, fmt.Errorf("Format string ended in an invalid state")
	}

	return &MShellString{b.String()}, nil
}


type Executable interface {
	Execute(state *EvalState, context ExecuteContext, stack *MShellStack) (EvalResult, int, string, string)
	GetStandardInputFile() string
	GetStandardOutputFile() string
}

func (list *MShellList) Execute(state *EvalState, context ExecuteContext, stack *MShellStack) (EvalResult, int, string, string) {
	result, exitCode, stdoutResult, stderrResult := RunProcess(*list, context, state)
	return result, exitCode, stdoutResult, stderrResult
}

func (quotation *MShellQuotation) Execute(state *EvalState, context ExecuteContext, stack *MShellStack, definitions []MShellDefinition, callStack CallStack) (EvalResult, int) {
	quotationContext := ExecuteContext{
		StandardInput:  nil,
		StandardOutput: nil,
		Variables:      quotation.Variables,
		Pbm: 		context.Pbm,
	}

	if quotation.StdinBehavior != STDIN_NONE {
		if quotation.StdinBehavior == STDIN_CONTENT {
			quotationContext.StandardInput = strings.NewReader(quotation.StandardInputContents)
		} else if quotation.StdinBehavior == STDIN_FILE {
			file, err := os.Open(quotation.StandardInputFile)
			if err != nil {
				return state.FailWithMessage(fmt.Sprintf("Error opening file %s for reading: %s\n", quotation.StandardInputFile, err.Error())), 1
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
			return state.FailWithMessage(fmt.Sprintf("Error opening file %s for writing: %s\n", quotation.StandardOutputFile, err.Error())), 1
		}
		quotationContext.StandardOutput = file
		defer file.Close()
	} else if context.StandardOutput != nil {
		quotationContext.StandardOutput = context.StandardOutput
	} else {
		// Default to stdout of this process itself
		quotationContext.StandardOutput = os.Stdout
	}

	result := state.Evaluate(quotation.Tokens, stack, quotationContext, definitions, CallStackItem{quotation, "quote", CALLSTACKQUOTE})

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

func (state *EvalState) ChangeDirectory(dir string) (EvalResult, int, string, string) {
	cwd, err := os.Getwd()
	if err != nil {
		return state.FailWithMessage(fmt.Sprintf("Error getting current directory: %s\n", err.Error())), 1, "", ""
	}

	err = os.Chdir(dir)
	if err != nil {
		return state.FailWithMessage(fmt.Sprintf("Error changing directory to %s: %s\n", dir, err.Error())), 1, "", ""
	}

	// Update OLDPWD and PWD
	err = os.Setenv("OLDPWD", cwd)
	if err != nil {
		return state.FailWithMessage(fmt.Sprintf("Error setting OLDPWD: %s\n", err.Error())), 1, "", ""
	}

	err = os.Setenv("PWD", dir)
	if err != nil {
		return state.FailWithMessage(fmt.Sprintf("Error setting PWD: %s\n", err.Error())), 1, "", ""
	}

	return SimpleSuccess(), 0, "", ""

}

func RunProcess(list MShellList, context ExecuteContext, state *EvalState) (EvalResult, int, string, string) {
	// Returns the result of running the process, the exit code, and the stdout result

	// Check for empty list
	if len(list.Items) == 0 {
		return state.FailWithMessage("Cannot execute an empty list.\n"), 1, "", ""
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
			return state.FailWithMessage(fmt.Sprintf("Item (%s) cannot be used as a command line argument.\n", item.DebugString())), 1, "", ""
		} else {
			commandLineArgs = append(commandLineArgs, item.CommandLine())
		}
	}

	// Need to check length here. You could have had a list of empty lists like:
	// [[] [] [] []]
	if len(commandLineArgs) == 0 {
		return state.FailWithMessage("After list flattening, there still were no arguments to execute.\n"), 1, "", ""
	}

	// Handle cd command specially
	if commandLineArgs[0] == "cd" {
		if len(commandLineArgs) > 3 {
			fmt.Fprint(os.Stderr, "cd command only takes one argument.\n")
		} else if len(commandLineArgs) == 2 {
			// Check for -h or --help
			if commandLineArgs[1] == "-h" || commandLineArgs[1] == "--help" {
				fmt.Fprint(os.Stderr, "cd: cd [dir]\nChange the shell working directory.\n")
				return SimpleSuccess(), 0, "", ""
			} else {
				return state.ChangeDirectory(commandLineArgs[1])
			}
		} else {
			// else cd to home directory
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return state.FailWithMessage(fmt.Sprintf("Error getting home directory: %s\n", err.Error())), 1, "", ""
			}

			return state.ChangeDirectory(homeDir)
		}

		return SimpleSuccess(), 0, "", ""
	}

	// Check that we find the command in the path

	var allArgs []string
	var cmdPath string

	// Check if there is a directory separator in the name of the command trying to execute
	if strings.Contains(commandLineArgs[0], string(os.PathSeparator)) {
		cmdPath = commandLineArgs[0]
	} else {
		var found bool
		if context.Pbm == nil {
			fmt.Fprint(os.Stderr, "No context found.\n")
			return state.FailWithMessage(fmt.Sprintf("Command '%s' not found in path.\n", commandLineArgs[0])), 1, "", ""
		}

		cmdPath, found = context.Pbm.Lookup(commandLineArgs[0])
		if !found {
			return state.FailWithMessage(fmt.Sprintf("Command '%s' not found in path.\n", commandLineArgs[0])), 1, "", ""
		}
	}

	// For interpreted files on Windows, we need to essentially do what a shebang on Linux does
	cmdItems, err := context.Pbm.ExecuteArgs(cmdPath)
	if err != nil {
		return state.FailWithMessage(fmt.Sprintf("On Windows, we currently don't handle this file/extension: %s\n", cmdPath)), 1, "", ""
	}

	allArgs = make([]string, 0, len(cmdItems)+len(commandLineArgs))
	allArgs = append(allArgs, cmdItems...)
	allArgs = append(allArgs, commandLineArgs[1:]...)

	cmd := context.Pbm.SetupCommand(allArgs)
	// cmd := exec.Command(allArgs[0], allArgs[1:]...)
	// cmd := exec.Command(commandLineArgs[0], commandLineArgs[1:]...)
	cmd.Env = os.Environ()

	// STDOUT HANDLING
	var commandSubWriter bytes.Buffer
	// TBD: Should we allow command substituation and redirection at the same time?
	// Probably more hassle than worth including, with probable workarounds for that rare case.
	if list.StdoutBehavior != STDOUT_NONE {
		cmd.Stdout = &commandSubWriter
	} else if list.StandardOutputFile != "" {
		// Open the file for writing
		var file *os.File
		if list.AppendOutput {
			file, err = os.OpenFile(list.StandardOutputFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
		} else {
			file, err = os.Create(list.StandardOutputFile)
		}

		if err != nil {
			return state.FailWithMessage(fmt.Sprintf("Error opening file %s for writing: %s\n", list.StandardOutputFile, err.Error())), 1, "", ""
		}
		cmd.Stdout = file
		defer file.Close()
	} else if context.StandardOutput != nil {
		cmd.Stdout = context.StandardOutput
	} else {
		if list.RunInBackground {
			cmd.Stdout = nil
		} else {
			// Default to stdout of this process itself
			cmd.Stdout = os.Stdout
			// cmd.Stdout = nil
		}
	}

	// STDIN HANDLING
	if list.StdinBehavior != STDIN_NONE {
		if list.StdinBehavior == STDIN_CONTENT {
			cmd.Stdin = strings.NewReader(list.StandardInputContents)
		} else if list.StdinBehavior == STDIN_FILE {
			// Open the file for reading
			file, err := os.Open(list.StandardInputFile)
			if err != nil {
				return state.FailWithMessage(fmt.Sprintf("Error opening file %s for reading: %s\n", list.StandardInputFile, err.Error())), 1, "", ""
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
		// return state.FailWithMessage(fmt.Sprintf("Error getting position of reader: %s\n", err.Error())), 1
		// }
		// fmt.Fprintf(os.Stderr, "Position of reader: %d\n", position)
	} else {
		// Default to stdin of this process itself
		cmd.Stdin = os.Stdin
	}

	// STDERR HANDLING
	var stderrBuffer bytes.Buffer

	if list.StderrBehavior != STDERR_NONE {
		cmd.Stderr = &stderrBuffer
	} else if list.StandardErrorFile != "" {
		// Open the file for writing
		file, err := os.Create(list.StandardErrorFile)
		if err != nil {
			return state.FailWithMessage(fmt.Sprintf("Error opening file %s for writing: %s\n", list.StandardErrorFile, err.Error())), 1, "", ""
		}
		cmd.Stderr = file
		defer file.Close()
	// } else if context.Stand != nil {
	// cmd.Stderr = context.StandardError  // TBD: Implement this
	} else {
		if list.RunInBackground {
			cmd.Stderr = nil
		} else {
			// Default to stdout of this process itself
			cmd.Stderr = os.Stderr
			// cmd.Stdout = nil
		}
	}

	var startErr error
	var exitCode int

	if list.RunInBackground {
		// Print out current stdout and stderr
		startErr = cmd.Start()

		if startErr != nil {
			fmt.Fprintf(os.Stderr, "Error starting command: %s\n", startErr.Error())
			exitCode = 1
		}
		exitCode = 0 // TODO: What to set here?
	} else {
		startErr := cmd.Run() // Manually deal with the exit code upstream
		if startErr != nil {
			if _, ok := startErr.(*exec.ExitError); !ok {
				fmt.Fprintf(os.Stderr, "Error running command: %s\n", startErr.Error())
				fmt.Fprintf(os.Stderr, "Command: '%s'\n", cmd.Path)
				for i, arg := range cmd.Args {
					fmt.Fprintf(os.Stderr, "Arg %d: '%s'\n", i, arg)
				}
				exitCode = 1
			} else {
				// Command exited with non-zero exit code
				exitCode = startErr.(*exec.ExitError).ExitCode()
			}
		} else {
			exitCode = cmd.ProcessState.ExitCode()
		}
	}
	// fmt.Fprintf(os.Stderr, "Command finished\n")

	// fmt.Fprintf(os.Stderr, "Exit code: %d\n", exitCode)

	var stdoutStr string
	var stderrStr string

	if list.StdoutBehavior != STDOUT_NONE {
		stdoutStr = commandSubWriter.String()
	} else {
		stdoutStr = ""
	}

	if list.StderrBehavior != STDERR_NONE {
		stderrStr = stderrBuffer.String()
	} else {
		stderrStr = ""
	}

	return SimpleSuccess(), exitCode, stdoutStr, stderrStr
}

func (state *EvalState) RunPipeline(MShellPipe MShellPipe, context ExecuteContext, stack *MShellStack) (EvalResult, int, string, string) {
	if len(MShellPipe.List.Items) == 0 {
		return state.FailWithMessage("Cannot execute an empty pipe.\n"), 1, "", ""
	}

	// Check that all list items are Executables
	for i, item := range MShellPipe.List.Items {
		if _, ok := item.(Executable); !ok {
			return state.FailWithMessage(fmt.Sprintf("Item %d (%s) in pipe is not a list or a quotation.\n", i, item.DebugString())), 1, "", ""
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
			return state.FailWithMessage(fmt.Sprintf("Error creating pipe: %s\n", err.Error())), 1, "", ""
		}
		pipeReaders[i] = pipeReader
		pipeWriters[i] = pipeWriter
	}

	var buf bytes.Buffer
	var stdErrBuf bytes.Buffer
	for i := 0; i < len(MShellPipe.List.Items); i++ {
		newContext := ExecuteContext{
			StandardInput:  nil,
			StandardOutput: nil,
			Variables:      context.Variables,
			Pbm:           context.Pbm,
		}

		if i == 0 {
			// Stdin should use the context of this function, or the file marked on the initial object
			executableStdinFile := MShellPipe.List.Items[i].(Executable).GetStandardInputFile()

			if executableStdinFile != "" {
				file, err := os.Open(executableStdinFile)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("Error opening file %s for reading: %s\n", executableStdinFile, err.Error())), 1, "", ""
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

			// Handle the stderr behavior
			if MShellPipe.StderrBehavior != STDERR_NONE {
				newContext.StandardError = &stdErrBuf
			} else {
				newContext.StandardError = context.StandardError
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
			results[i], exitCodes[i], _, _ = item.Execute(state, contexts[i], stack)

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

	var stdoutStr string
	var stderrStr string

	if MShellPipe.StdoutBehavior != STDOUT_NONE {
		stdoutStr = buf.String()
	} else {
		stdoutStr = ""
	}

	if MShellPipe.StderrBehavior != STDERR_NONE {
		stderrStr = stdErrBuf.String()
	} else {
		stderrStr = ""
	}

	// Check for errors
	for i, result := range results {
		if !result.Success {
			return result, exitCodes[i], stdoutStr, stderrStr
		}
	}

	return SimpleSuccess(), exitCodes[len(exitCodes)-1], stdoutStr, stderrStr
}

func CopyFile(source string, dest string) error {
	// TODO: Fix this dumb copy code.
	// Copy the file
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer output.Close()

	_, err = io.Copy(output, input)
	if err != nil {
		return err
	}

	return nil
}

func VersionSortComparer(a_str string, b_str string) int {
	a := []rune(a_str)
	b := []rune(b_str)

	a_index := 0
	b_index := 0

	a_end_index := 0
	b_end_index := 0

	for {
		if a_index >= len(a) {
			if b_index >= len(b) {
				return 0 // Both strings are equal
			} else {
				return -1 // a is shorter than b
			}
		}

		if b_index >= len(b) {
			return 1 // b is shorter than a
		}

		cur_a := a[a_index]
		cur_b := b[b_index]

		if unicode.IsDigit(cur_a) && unicode.IsDigit(cur_b) {
			a_end_index = a_index
			b_end_index = b_index

			// Find the end of the digit sequence in a
			for a_end_index < len(a) && unicode.IsDigit(a[a_end_index]) {
				a_end_index++
			}

			a_int, _ := strconv.Atoi(string(a[a_index:a_end_index]))

			// Find the end of the digit sequence in b
			for b_end_index < len(b) && unicode.IsDigit(b[b_end_index]) {
				b_end_index++
			}

			b_int, _ := strconv.Atoi(string(b[b_index:b_end_index]))

			if a_int < b_int {
				return -1
			} else if a_int > b_int {
				return 1
			} else {
				a_index = a_end_index
				b_index = b_end_index
			}
		} else if unicode.IsDigit(cur_a) {
			// Check is b is less than 0
			if cur_b < '0' {
				return 1
			} else {
				return -1
			}
		} else if unicode.IsDigit(cur_b) {
			// Check is a is less than 0
			if cur_a < '0' {
				return -1
			} else {
				return 1
			}
		} else {
			if cur_a < cur_b {
				return -1
			} else if cur_a > cur_b {
				return 1
			} else {
				a_index++
				b_index++
			}
		}
	}
}

func ParseJsonObjToMshell(jsonObj interface{}) MShellObject {
	// See https://pkg.go.dev/encoding/json#Unmarshal
	switch o := jsonObj.(type) {
	case []interface{}:
		list := NewList(0)
		for _, item := range o {
			parsedItem := ParseJsonObjToMshell(item)
			list.Items = append(list.Items, parsedItem)
		}
		return list

	case map[string]interface{}:
		dict := NewDict()
		// TODO: decide and document how to handle duplicate keys in JSON objects
		for key, value := range o {
			dict.Items[key] = ParseJsonObjToMshell(value)
		}
		return dict

	case string:
		return &MShellString{o}
	case float64:
		return &MShellFloat{o}
	case bool:
		if o {
			return &MShellBool{true}
		} else {
			return &MShellBool{false}
		}
	case nil:
		return &MShellInt{0}
	default:
		panic(fmt.Sprintf("Unknown JSON object type: %T", jsonObj))
		// There should be no other types in JSON, but if there are, we can handle them here
	}
}
