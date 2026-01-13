package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	// "golang.org/x/term"
	"crypto/md5"
	"golang.org/x/net/html"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"net/http"
	"net/url"
	_ "time/tzdata"
	"unicode"
	"unicode/utf8"
	// "net/http/httputil"
)

type MShellFunction struct {
	Name       string
	Evaluate   func(stack MShellStack, Context ExecuteContext)
	InputTypes []MShellType
}

var oleAutomationEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

type MShellStack []MShellObject

func (objList *MShellStack) Peek() (MShellObject, error) {
	if len(*objList) == 0 {
		return nil, fmt.Errorf("Empty stack")
	}
	return (*objList)[len(*objList)-1], nil
}

func (objList *MShellStack) Clear() {
	*objList = (*objList)[:0]
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

// Returns three objects from the stack.
// obj1, obj2, obj3 := stack.Pop3(t)
// obj1 was on top of the stack, obj2 was below it, and obj3 was below that.
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

func getIntOption(dict *MShellDict, key string) (int, bool, error) {
	if val, ok := dict.Items[key]; ok {
		if intObj, ok := val.(MShellInt); ok {
			return intObj.Value, true, nil
		}
		return 0, true, fmt.Errorf("The '%s' option in 'numFmt' must be an int, found a %s (%s)", key, val.TypeName(), val.DebugString())
	}
	return 0, false, nil
}

func getStringOption(dict *MShellDict, key string) (string, bool, error) {
	if val, ok := dict.Items[key]; ok {
		str, err := val.CastString()
		if err != nil {
			return "", true, fmt.Errorf("The '%s' option in 'numFmt' must be a string, found a %s (%s)", key, val.TypeName(), val.DebugString())
		}
		return str, true, nil
	}
	return "", false, nil
}

func getBoolOption(dict *MShellDict, key string) (bool, bool, error) {
	if val, ok := dict.Items[key]; ok {
		boolObj, ok := val.(MShellBool)
		if !ok {
			return false, true, fmt.Errorf("The '%s' option in 'numFmt' must be a bool, found a %s (%s)", key, val.TypeName(), val.DebugString())
		}
		return boolObj.Value, true, nil
	}
	return false, false, nil
}

func escapeMshellString(input string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range input {
		switch r {
		case '\\':
			b.WriteString("\\\\")
		case '"':
			b.WriteString("\\\"")
		case '\n':
			b.WriteString("\\n")
		case '\t':
			b.WriteString("\\t")
		case '\r':
			b.WriteString("\\r")
		case '\033':
			b.WriteString("\\e")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func formatWithSigFigs(num float64, sigfigs int) string {
	if num == 0 {
		return fmt.Sprintf("0.%s", strings.Repeat("0", sigfigs))
	}

	sign := ""
	if num < 0 {
		sign = "-"
		num = -num
	}

	exp := int(math.Floor(math.Log10(num)))
	decimalsNeeded := sigfigs - (exp + 1)

	var digits string
	if decimalsNeeded >= 0 {
		scale := math.Pow10(decimalsNeeded)
		rounded := math.Round(num * scale)
		intVal := int64(rounded)
		digits = fmt.Sprintf("%d", intVal)
		if len(digits) < decimalsNeeded+1 {
			digits = strings.Repeat("0", decimalsNeeded+1-len(digits)) + digits
		}
		decimalPos := len(digits) - decimalsNeeded
		if decimalPos == len(digits) {
			return sign + digits
		}
		left := digits[:decimalPos]
		right := digits[decimalPos:]
		return sign + left + "." + right
	}

	// decimalsNeeded < 0 => round to whole number digits
	roundPow := -decimalsNeeded
	scale := math.Pow10(roundPow)
	rounded := math.Round(num / scale)
	intRounded := int64(rounded) * int64(scale)
	digits = fmt.Sprintf("%d", intRounded)
	return sign + digits
}

func formatWithDecimals(num float64, decimals int) string {
	sign := ""
	if num < 0 {
		sign = "-"
		num = -num
	}

	scale := math.Pow10(decimals)
	rounded := math.Round(num * scale)
	intPart := int64(rounded) / int64(scale)
	fracPart := int64(math.Abs(rounded)) % int64(scale)

	intStr := fmt.Sprintf("%d", intPart)
	if decimals == 0 {
		return sign + intStr
	}

	fracStr := fmt.Sprintf("%0*d", decimals, fracPart)
	return sign + intStr + "." + fracStr
}

func getGroupingOption(dict *MShellDict, key string) ([]int, bool, error) {
	val, ok := dict.Items[key]
	if !ok {
		return nil, false, nil
	}

	list, ok := val.(*MShellList)
	if !ok {
		return nil, true, fmt.Errorf("The '%s' option in 'numFmt' must be a list of ints, found a %s (%s)", key, val.TypeName(), val.DebugString())
	}

	grouping := make([]int, len(list.Items))
	for i, item := range list.Items {
		intItem, ok := item.(MShellInt)
		if !ok {
			return nil, true, fmt.Errorf("The '%s' option in 'numFmt' must be a list of ints, found a %s (%s) at index %d", key, item.TypeName(), item.DebugString(), i)
		}
		if intItem.Value <= 0 {
			return nil, true, fmt.Errorf("The '%s' option in 'numFmt' must contain positive integers, found %d at index %d", key, intItem.Value, i)
		}
		grouping[i] = intItem.Value
	}

	return grouping, true, nil
}

func groupIntPart(intPart string, sep string, grouping []int, hasSep bool, hasGrouping bool) string {
	if !hasSep || sep == "" {
		return intPart
	}

	if !hasGrouping {
		grouping = []int{3}
	}
	if len(grouping) == 0 {
		return intPart
	}

	if len(intPart) <= grouping[0] {
		return intPart
	}

	var parts []string
	remaining := intPart
	groupIdx := 0
	for len(remaining) > 0 {
		size := grouping[groupIdx]
		if size <= 0 {
			size = grouping[len(grouping)-1]
		}

		if len(remaining) <= size {
			parts = append(parts, remaining)
			break
		}

		parts = append(parts, remaining[len(remaining)-size:])
		remaining = remaining[:len(remaining)-size]

		if groupIdx < len(grouping)-1 {
			groupIdx++
		}
	}

	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}

	return strings.Join(parts, sep)
}

type EvalState struct {
	PositionalArgs []string
	LoopDepth      int

	StopOnError bool
	CallStack   CallStack
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
	StandardInput     io.Reader
	StandardOutput    io.Writer
	StandardError     io.Writer
	Variables         map[string]MShellObject // Mapping from variable name without leading '@' or trailing '!' to object.
	ShouldCloseInput  bool
	ShouldCloseOutput bool
	Pbm               IPathBinManager
}

func (context *ExecuteContext) CloneLessVariables() *ExecuteContext {
	newContext := &ExecuteContext{
		StandardInput:     context.StandardInput,
		StandardOutput:    context.StandardOutput,
		StandardError:     context.StandardError,
		ShouldCloseInput:  context.ShouldCloseInput,
		ShouldCloseOutput: context.ShouldCloseOutput,
		Pbm:               context.Pbm,
	}

	newContext.Variables = make(map[string]MShellObject)
	return newContext
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

func (state *EvalState) FailWithMessage(message string) EvalResult {
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
			if startToken.TokenFile != nil {
				fmt.Fprintf(os.Stderr, "%s:%d:%d %s\n", startToken.TokenFile.Path, startToken.Line, startToken.Column, callStackItem.Name)
			} else {
				fmt.Fprintf(os.Stderr, "%d:%d %s\n", startToken.Line, startToken.Column, callStackItem.Name)
			}
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
	CALLSTACKIF
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

func (state *EvalState) EvaluateQuote(quotation MShellQuotation, stack *MShellStack, outerContext ExecuteContext, definitions []MShellDefinition) (EvalResult, error) {
	qContext, err := quotation.BuildExecutionContext(&outerContext)
	defer qContext.Close()
	if err != nil {
		return EvalResult{}, err
	}
	callStackItem := CallStackItem{MShellParseItem: &quotation, Name: "Quote", CallStackType: CALLSTACKQUOTE}
	return state.Evaluate(quotation.Tokens, stack, (*qContext), definitions, callStackItem), nil
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

		switch t := t.(type) {
		case *MShellParseList:
			// Evaluate the list
			list := t
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
			copy(newList.Items, listStack) // Copy the items from the stack to the new list
			stack.Push(newList)
		case *MShellParseDict:
			// Evaluate the dictionary
			parseDict := t
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
			parseQuote := t
			q := MShellQuotation{Tokens: parseQuote.Items, StandardInputFile: "", StandardOutputFile: "", StandardErrorFile: "", Variables: context.Variables, MShellParseQuote: parseQuote}
			stack.Push(&q)
		case *MShellParseIfBlock:
			ifBlock := t
			startToken := ifBlock.GetStartToken()

			// Pop the condition from the stack
			condObj, err := stack.Pop()
			if err != nil {
				return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot evaluate 'if' on an empty stack.\n", startToken.Line, startToken.Column))
			}

			// Evaluate condition
			var condition bool
			switch condTyped := condObj.(type) {
			case MShellBool:
				condition = condTyped.Value
			case MShellInt:
				condition = condTyped.Value == 0
			default:
				return state.FailWithMessage(fmt.Sprintf("%d:%d: Expected a boolean or integer for if condition, received a %s.\n", startToken.Line, startToken.Column, condObj.TypeName()))
			}

			if condition {
				// Execute if body
				callStackItem := CallStackItem{MShellParseItem: ifBlock, Name: "if", CallStackType: CALLSTACKIF}
				result := state.Evaluate(ifBlock.IfBody, stack, context, definitions, callStackItem)
				if result.ShouldPassResultUpStack() {
					return result
				}
			} else {
				// Check else-if branches
				foundMatch := false
				for _, elseIf := range ifBlock.ElseIfs {
					// Evaluate condition
					callStackItem := CallStackItem{MShellParseItem: ifBlock, Name: "else-if-condition", CallStackType: CALLSTACKIF}
					result := state.Evaluate(elseIf.Condition, stack, context, definitions, callStackItem)
					if !result.Success || result.ExitCalled {
						return result
					}
					if result.BreakNum > 0 {
						return state.FailWithMessage("Encountered break within else-if condition.\n")
					}
					if result.Continue {
						return state.FailWithMessage("Encountered continue within else-if condition.\n")
					}

					elseIfCondObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Found an empty stack when evaluating else-if condition.\n", startToken.Line, startToken.Column))
					}

					var elseIfCondition bool
					switch elseIfCondTyped := elseIfCondObj.(type) {
					case MShellBool:
						elseIfCondition = elseIfCondTyped.Value
					case MShellInt:
						elseIfCondition = elseIfCondTyped.Value == 0
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Expected a boolean or integer for else-if condition, received a %s.\n", startToken.Line, startToken.Column, elseIfCondObj.TypeName()))
					}

					if elseIfCondition {
						// Execute else-if body
						callStackItem := CallStackItem{MShellParseItem: ifBlock, Name: "else-if", CallStackType: CALLSTACKIF}
						result := state.Evaluate(elseIf.Body, stack, context, definitions, callStackItem)
						if result.ShouldPassResultUpStack() {
							return result
						}
						foundMatch = true
						break
					}
				}

				// If no else-if matched, execute else body if present
				if !foundMatch && len(ifBlock.ElseBody) > 0 {
					callStackItem := CallStackItem{MShellParseItem: ifBlock, Name: "else", CallStackType: CALLSTACKIF}
					result := state.Evaluate(ifBlock.ElseBody, stack, context, definitions, callStackItem)
					if result.ShouldPassResultUpStack() {
						return result
					}
				}
			}
		case *MShellIndexerList:
			obj1, err := stack.Pop()
			if err != nil {
				startToken := t.GetStartToken()
				return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'indexer' operation on an empty stack.\n", startToken.Line, startToken.Column))
			}

			indexerList := t
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
				var newObject MShellObject
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
							wrappedResult = &MShellPipe{List: *newList, StdoutBehavior: STDOUT_NONE}
							wrappedResult.(*MShellPipe).List.Items = append(wrappedResult.(*MShellPipe).List.Items, result)
						default:
							wrappedResult = result
						}

						if newObject == nil {
							newObject = wrappedResult
						} else {
							newObject, err = newObject.Concat(wrappedResult)
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
							newObject, err = newObject.Concat(result)
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
							newObject, err = newObject.Concat(result)
							if err != nil {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: %s", indexerToken.Line, indexerToken.Column, err.Error()))
							}
						}

					}
				}

				stack.Push(newObject)
			}
		case MShellVarstoreList:
			varstoreList := t
			// First check lengths
			if len(varstoreList.VarStores) > len(*stack) {
				return state.FailWithMessage(fmt.Sprintf("%d:%d: Not enough items on stack (%d) to store into %d variables.\n", varstoreList.GetStartToken().Line, varstoreList.GetStartToken().Column, len(*stack), len(varstoreList.VarStores)))
			}

			// Have to bind in revsere order
			for i := len(varstoreList.VarStores) - 1; i >= 0; i-- {
				varstoreToken := varstoreList.VarStores[i]
				obj, _ := stack.Pop()
				varName := varstoreToken.Lexeme[0 : len(varstoreToken.Lexeme)-1] // Remove the trailing !
				context.Variables[varName] = obj
			}
		case *MShellGetter:
			// Handle like 'get'
			getter := t
			obj, err := stack.Pop()
			if err != nil {
				return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do ':' operation on an empty stack.\n", getter.Token.Line, getter.Token.Column))
			}

			dict, ok := obj.(*MShellDict)
			if !ok {
				return state.FailWithMessage(fmt.Sprintf("%d:%d: The stack parameter for the dictionary in ':' is not a dictionary. Found a %s (%s). Key: %s\n", getter.Token.Line, getter.Token.Column, obj.TypeName(), obj.DebugString(), getter.String))
			}

			value, ok := dict.Items[getter.String]
			if !ok {
				stack.Push(&Maybe{obj: nil})
			} else {
				maybe := Maybe{obj: value}
				stack.Push(&maybe)
			}

		case Token:

			if t.Type == EOF {
				return SimpleSuccess()
			} else if t.Type == LITERAL {

				// Check for definitions
				for _, definition := range definitions {
					if definition.Name == t.Lexeme {
						// Evaluate the definition
						newContext := context.CloneLessVariables()
						callStackItem := CallStackItem{MShellParseItem: t, Name: definition.Name, CallStackType: CALLSTACKDEF}
						result := state.Evaluate(definition.Items, stack, *newContext, definitions, callStackItem)

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
					debugList := context.Pbm.DebugList()
					for _, item := range debugList.Items {
						fmt.Fprintf(os.Stderr, "%s\t%s\n", item.(*MShellList).Items[0].(MShellString).Content, item.(*MShellList).Items[1].(MShellString).Content)
					}

					// fmt.Fprint(os.Stderr, debugStr)
				} else if t.Lexeme == "binPaths" {
					stack.Push(context.Pbm.DebugList())

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
					for i, file := range files {
						newList.Items[i] = MShellPath{file}
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
					stack.Push(MShellString{buffer.String()})
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
					switch obj1Typed := obj1.(type) {
					case *MShellList:
						switch obj2Typed := obj2.(type) {
						case *MShellList:
							obj2Typed.Items = append(obj2Typed.Items, obj1)
							stack.Push(obj2Typed)
						default:
							obj1Typed.Items = append(obj1Typed.Items, obj2)
							stack.Push(obj1Typed)
						}
					default:
						switch obj2Typed := obj2.(type) {
						case *MShellList:
							obj2Typed.Items = append(obj2Typed.Items, obj1)
							stack.Push(obj2Typed)
						default:
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot append a %s to a %s.\n", t.Line, t.Column, obj1.TypeName(), obj2.TypeName()))
						}
					}
				} else if t.Lexeme == "args" {
					// Dump the positional arguments onto the stack as a list of strings
					newList := NewList(len(state.PositionalArgs))
					for i, arg := range state.PositionalArgs {
						newList.Items[i] = MShellString{arg}
					}
					stack.Push(newList)
				} else if t.Lexeme == "len" {
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'len' operation on an empty stack.\n", t.Line, t.Column))
					}

					switch objTyped := obj.(type) {
					case *MShellList:
						stack.Push(MShellInt{len(objTyped.Items)})
					case MShellString:
						stack.Push(MShellInt{len(objTyped.Content)})
					case MShellPath:
						stack.Push(MShellInt{len(objTyped.Path)})
					case MShellLiteral:
						stack.Push(MShellInt{len(objTyped.LiteralText)})
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

					int1, ok := obj1.(MShellInt)
					if ok {
						result, err := obj2.Index(int1.Value)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
						}
						stack.Push(result)
					} else {
						int2, ok := obj2.(MShellInt)
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
					int1, ok := obj1.(MShellInt)
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

					switch topTyped := top.(type) {
					case MShellLiteral:
						fmt.Fprint(writer, topTyped.LiteralText)
					case MShellString:
						fmt.Fprint(writer, topTyped.Content)
					case MShellInt:
						fmt.Fprint(writer, topTyped.Value)
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

					stack.Push(MShellString{strings.ReplaceAll(originalStr, findStr, replacementStr)})
				} else if t.Lexeme == "strEscape" {
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'strEscape' operation on an empty stack.\n", t.Line, t.Column))
					}

					strToEscape, err := obj.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot escape a %s (%s).\n", t.Line, t.Column, obj.TypeName(), obj.DebugString()))
					}

					stack.Push(MShellString{escapeMshellString(strToEscape)})
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
						newList.Items[i] = MShellString{item}
					}
					stack.Push(newList)
				} else if t.Lexeme == "wsplit" {
					// Split on whitespace
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'wsplit' operation on an empty stack.\n", t.Line, t.Column))
					}
					strToSplit, err := obj.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'wsplit' operation on a %s (%s).\n", t.Line, t.Column, obj.TypeName(), obj.DebugString()))
					}

					split := strings.Fields(strToSplit)
					newList := NewList(len(split))
					for i, item := range split {
						newList.Items[i] = MShellString{item}
					}

					stack.Push(newList)
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

					switch delimiterTyped := delimiter.(type) {
					case MShellString:
						delimiterStr = delimiterTyped.Content
					case MShellLiteral:
						delimiterStr = delimiterTyped.LiteralText
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot join with a %s.\n", t.Line, t.Column, delimiter.TypeName()))
					}

					switch listTyped := list.(type) {
					case *MShellList:
						for _, item := range listTyped.Items {
							switch itemTyped := item.(type) {
							case MShellString:
								listItems = append(listItems, itemTyped.Content)
							case MShellLiteral:
								listItems = append(listItems, itemTyped.LiteralText)
							default:
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot join a list with a %s inside (%s).\n", t.Line, t.Column, item.TypeName(), item.DebugString()))
							}
						}
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Expected a list as the second item on stack for join, received a %s (%s). The delimiter was '%s'\n", t.Line, t.Column, list.TypeName(), list.DebugString(), delimiterStr))
					}

					stack.Push(MShellString{strings.Join(listItems, delimiterStr)})
				} else if t.Lexeme == "lines" {
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot evaluate 'lines' on an empty stack.\n", t.Line, t.Column))
					}

					s1, ok := obj.(MShellString)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot evaluate 'lines' on a %s.\n", t.Line, t.Column, obj.TypeName()))
					}

					newList := NewList(0)
					for line := range strings.Lines(s1.Content) {
						if len(line) > 0 && line[len(line)-1] == '\n' {
							line = line[:len(line)-1]
							if len(line) > 0 && line[len(line)-1] == '\r' {
								line = line[:len(line)-1]
							}
						}

						newList.Items = append(newList.Items, MShellString{line})
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

					obj1Index, ok := obj1.(MShellInt)
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

					obj1Index, ok := obj1.(MShellInt)
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
					case MShellInt:
						switch obj2.(type) {
						case *MShellList:
							index := obj1.(MShellInt).Value
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
						case MShellInt:
							index := obj2.(MShellInt).Value
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

					stack.Push(MShellString{string(content)})
				} else if t.Lexeme == "readFileBytes" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'readFileBytes' operation on an empty stack.\n", t.Line, t.Column))
					}

					filePath, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot read from a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					content, err := os.ReadFile(filePath)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error reading file: %s\n", t.Line, t.Column, err.Error()))
					}

					stack.Push(MShellBinary(content))
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
						stack.Push(MShellBool{exists})
					} else {
						totalStringText, err := stringOrDict.CastString()
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot search in a %s.\n", t.Line, t.Column, stringOrDict.TypeName()))
						}

						stack.Push(MShellBool{strings.Contains(totalStringText, substringText)})
					}
				} else if t.Lexeme == "/" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					if obj1.IsNumeric() && obj2.IsNumeric() {
						switch obj1 := obj1.(type) {
						case MShellInt:
							if obj1.Value == 0 {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot divide by zero.\n", t.Line, t.Column))
							}
							switch obj2 := obj2.(type) {
							case MShellInt:
								stack.Push(MShellInt{obj2.Value / obj1.Value})
							case MShellFloat:
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot divide a float by an int.\n", t.Line, t.Column))
							}
						case MShellFloat:
							if obj1.Value == 0 {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot divide by zero.\n", t.Line, t.Column))
							}

							switch obj2 := obj2.(type) {
							case MShellInt:
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot divide an int by a float.\n", t.Line, t.Column))
							case MShellFloat:
								stack.Push(MShellFloat{obj2.Value / obj1.Value})
							}
						default:
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot divide a %s and a %s.\n", t.Line, t.Column, obj2.TypeName(), obj1.TypeName()))
						}
					} else {
						// Check if both are paths
						switch obj1.(type) {
						case MShellPath:
							switch obj2.(type) {
							case MShellPath:
								// This is a path join operation
								newPath := filepath.Join(obj2.(MShellPath).Path, obj1.(MShellPath).Path)
								stack.Push(MShellPath{newPath})
							default:
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do a join between a %s (%s) and a %s (%s).\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString(), obj1.TypeName(), obj1.DebugString()))
							}
						default:
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do a '%s' operation between a %s and a %s.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName(), obj1.TypeName()))
						}
					}
				} else if t.Lexeme == "exit" {
					exitCode, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'exit' operation on an empty stack. If you are trying to exit out of the interactive shell, you are probably looking to do `0 exit`.\n", t.Line, t.Column))
					}

					exitInt, ok := exitCode.(MShellInt)
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
				} else if t.Lexeme == "toFloat" {
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'toFloat' operation on an empty stack.\n", t.Line, t.Column))
					}

					switch objTyped := obj.(type) {
					case MShellString:
						floatVal, err := strconv.ParseFloat(strings.TrimSpace(objTyped.Content), 64)
						if err != nil {
							stack.Push(&Maybe{obj: nil})
						} else {
							stack.Push(&Maybe{obj: MShellFloat{floatVal}})
						}
						// I don't believe checking for literal is required, because it should have been parsed as a float to start with?
					case MShellInt:
						stack.Push(MShellFloat{float64(objTyped.Value)})
					case MShellFloat:
						stack.Push(obj)
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s to a float.\n", t.Line, t.Column, obj.TypeName()))
					}
				} else if t.Lexeme == "toInt" {
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'toInt' operation on an empty stack.\n", t.Line, t.Column))
					}

					switch objTyped := obj.(type) {
					case MShellString:
						intVal, err := strconv.Atoi(strings.TrimSpace(objTyped.Content))
						if err != nil {
							stack.Push(&Maybe{obj: nil})
						} else {
							stack.Push(&Maybe{obj: MShellInt{intVal}})
						}
						// I don't believe checking for literal is required, because it should have been parsed as a float to start with?
					case MShellInt:
						stack.Push(obj)
					case MShellFloat:
						stack.Push(MShellInt{int(objTyped.Value)})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s to an int.\n", t.Line, t.Column, obj.TypeName()))
					}
				} else if t.Lexeme == "toDt" {
					dateStrObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'toDt' operation on an empty stack.\n", t.Line, t.Column))
					}

					var dateStr string
					switch dateStrTyped := dateStrObj.(type) {
					case MShellString:
						dateStr = dateStrTyped.Content
					case MShellLiteral:
						dateStr = dateStrTyped.LiteralText
					case *MShellDateTime:
						stack.Push(dateStrObj)
						continue MainLoop
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s to a datetime.\n", t.Line, t.Column, dateStrObj.TypeName()))
					}

					// TODO: Don't make a new lexer object each time.
					parsedTime, err := ParseDateTime(dateStr)
					if err != nil {
						stack.Push(&Maybe{obj: nil})
						// return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing date time '%s': %s\n", t.Line, t.Column, dateStr, err.Error()))
					} else {
						dt := MShellDateTime{Time: parsedTime, OriginalString: dateStr}
						stack.Push(&Maybe{obj: &dt})
					}
				} else if t.Lexeme == "files" || t.Lexeme == "dirs" {
					// Dump all the files in the current directory to the stack. No sub-directories.
					files, err := os.ReadDir(".")
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error reading current directory: %s\n", t.Line, t.Column, err.Error()))
					}

					newList := NewList(0)
					if t.Lexeme == "files" {
						for _, file := range files {
							if !file.IsDir() {
								newList.Items = append(newList.Items, MShellPath{file.Name()})
							}
						}
					} else {
						for _, file := range files {
							if file.IsDir() {
								newList.Items = append(newList.Items, MShellPath{file.Name()})
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
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot check if a %s for %s.\n", t.Line, t.Column, obj.TypeName(), t.Lexeme))
					}

					fileInfo, err := os.Stat(path)
					if err != nil {
						if errors.Is(err, os.ErrNotExist) {
							stack.Push(MShellBool{false})
						} else {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error checking if %s is a directory: %s\n", t.Line, t.Column, path, err.Error()))
						}
					} else {
						switch t.Lexeme {
						case "isDir":
							stack.Push(MShellBool{fileInfo.IsDir()})
						case "isFile":
							stack.Push(MShellBool{!fileInfo.IsDir()})
						default:
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
					switch t.Lexeme {
					case "mkdir":
						err = os.Mkdir(dirPath, 0755)
					case "mkdirp":
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

					stack.Push(MShellString{tildeExpanded})
				} else if t.Lexeme == "pwd" {
					pwd, err := os.Getwd()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error getting current directory: %s\n", t.Line, t.Column, err.Error()))
					}
					stack.Push(MShellString{pwd})
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
					registerTempFileForCleanup(tmpfile.Name())

					// Write the contents of the object to the temporary file
					switch obj1Typed := obj1.(type) {
					case MShellString:
						_, err = tmpfile.WriteString(obj1Typed.Content)
						if err != nil {
							tmpfile.Close()
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error writing to temporary file: %s\n", t.Line, t.Column, err.Error()))
						}
					case MShellLiteral:
						_, err = tmpfile.WriteString(obj1Typed.LiteralText)
						if err != nil {
							tmpfile.Close()
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error writing to temporary file: %s\n", t.Line, t.Column, err.Error()))
						}
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'psub' with a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}
					tmpfile.Close()
					stack.Push(MShellString{tmpfile.Name()})
				} else if t.Lexeme == "now" {
					// Drop current local date time onto the stack
					now := time.Now()
					stack.Push(&MShellDateTime{Time: now, OriginalString: now.Format(time.RFC3339)})
				} else if t.Lexeme == "date" {
					dateTimeObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'day' operation on an empty stack.\n", t.Line, t.Column))
					}

					dateTime, ok := dateTimeObj.(*MShellDateTime)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the date component of a %s.\n", t.Line, t.Column, dateTimeObj.TypeName()))
					}

					datePortion := time.Date(dateTime.Time.Year(), dateTime.Time.Month(), dateTime.Time.Day(), 0, 0, 0, 0, dateTime.Time.Location())
					// Original string is ISO 8601 format
					stack.Push(&MShellDateTime{Time: datePortion, OriginalString: datePortion.Format(time.RFC3339)})
				} else if t.Lexeme == "day" {
					dateTimeObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'day' operation on an empty stack.\n", t.Line, t.Column))
					}

					dateTime, ok := dateTimeObj.(*MShellDateTime)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the day of a %s.\n", t.Line, t.Column, dateTimeObj.TypeName()))
					}

					stack.Push(MShellInt{dateTime.Time.Day()})
				} else if t.Lexeme == "month" {
					dateTimeObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'month' operation on an empty stack.\n", t.Line, t.Column))
					}

					dateTime, ok := dateTimeObj.(*MShellDateTime)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the month of a %s.\n", t.Line, t.Column, dateTimeObj.TypeName()))
					}

					stack.Push(MShellInt{int(dateTime.Time.Month())})

				} else if t.Lexeme == "year" {
					dateTimeObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'year' operation on an empty stack.\n", t.Line, t.Column))
					}

					dateTime, ok := dateTimeObj.(*MShellDateTime)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the year of a %s.\n", t.Line, t.Column, dateTimeObj.TypeName()))
					}

					stack.Push(MShellInt{dateTime.Time.Year()})
				} else if t.Lexeme == "hour" {
					dateTimeObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'hour' operation on an empty stack.\n", t.Line, t.Column))
					}

					dateTime, ok := dateTimeObj.(*MShellDateTime)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the hour of a %s.\n", t.Line, t.Column, dateTimeObj.TypeName()))
					}

					stack.Push(MShellInt{dateTime.Time.Hour()})
				} else if t.Lexeme == "minute" {
					dateTimeObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'minute' operation on an empty stack.\n", t.Line, t.Column))
					}

					dateTime, ok := dateTimeObj.(*MShellDateTime)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the minute of a %s.\n", t.Line, t.Column, dateTimeObj.TypeName()))
					}

					stack.Push(MShellInt{dateTime.Time.Minute()})
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
					case MShellInt:
						switch obj2.(type) {
						case MShellInt:
							if obj1.(MShellInt).Value == 0 {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot mod by zero.\n", t.Line, t.Column))
							}

							stack.Push(MShellInt{obj2.(MShellInt).Value % obj1.(MShellInt).Value})
						case MShellFloat:
							if obj1.(MShellInt).Value == 0 {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot mod by zero.\n", t.Line, t.Column))
							}

							stack.Push(MShellFloat{math.Mod(obj2.(MShellFloat).Value, float64(obj1.(MShellInt).Value))})
						}

					case MShellFloat:
						switch obj2.(type) {
						case MShellInt:
							if obj1.(MShellFloat).Value == 0 {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot mod by zero.\n", t.Line, t.Column))
							}

							stack.Push(MShellFloat{math.Mod(float64(obj2.(MShellInt).Value), obj1.(MShellFloat).Value)})
						case MShellFloat:
							if obj1.(MShellFloat).Value == 0 {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot mod by zero.\n", t.Line, t.Column))
							}

							stack.Push(MShellFloat{math.Mod(obj2.(MShellFloat).Value, obj1.(MShellFloat).Value)})
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
						stack.Push(MShellPath{filepath.Base(path)})
					} else if t.Lexeme == "dirname" {
						stack.Push(MShellPath{filepath.Dir(path)})
					} else if t.Lexeme == "ext" {
						stack.Push(MShellString{filepath.Ext(path)})
					} else if t.Lexeme == "stem" {
						// This should include previous dir if it exists

						stack.Push(MShellPath{strings.TrimSuffix(path, filepath.Ext(path))})
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

					stack.Push(MShellPath{path})
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
					formatString, ok := obj1.(MShellString)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot format a date with a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					dateTime, ok := obj2.(*MShellDateTime)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot format a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}

					newStr := dateTime.Time.Format(formatString.Content)
					stack.Push(MShellString{newStr})
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
						stack.Push(MShellString{strings.TrimSpace(str)})
					} else if t.Lexeme == "trimStart" {
						stack.Push(MShellString{strings.TrimLeft(str, " \t\n")})
					} else if t.Lexeme == "trimEnd" {
						stack.Push(MShellString{strings.TrimRight(str, " \t\n")})
					}
				} else if t.Lexeme == "upper" || t.Lexeme == "lower" || t.Lexeme == "title" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
					}

					var f func(string) string
					if t.Lexeme == "upper" {
						f = strings.ToUpper
					} else if t.Lexeme == "lower" {
						f = strings.ToLower
					} else if t.Lexeme == "title" {
						titleCaser := cases.Title(language.English)
						f = titleCaser.String
					}

					switch obj1Typed := obj1.(type) {
					case MShellString:
						stack.Push(MShellString{f(obj1Typed.Content)})
					case MShellLiteral:
						stack.Push(MShellLiteral{f(obj1Typed.LiteralText)})
					case MShellPath:
						stack.Push(MShellPath{f(obj1Typed.Path)})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot %s a %s (%s).\n", t.Line, t.Column, t.Lexeme, obj1.TypeName(), obj1.DebugString()))
					}
				} else if t.Lexeme == "numFmt" {
					optionsObj, numberObj, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					optionsDict, ok := optionsObj.(*MShellDict)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The top of stack for 'numFmt' must be a dictionary, found a %s (%s)\n", t.Line, t.Column, optionsObj.TypeName(), optionsObj.DebugString()))
					}

					var num float64
					switch n := numberObj.(type) {
					case MShellInt:
						num = float64(n.Value)
					case MShellFloat:
						num = n.Value
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The second parameter in 'numFmt' must be numeric, found a %s (%s)\n", t.Line, t.Column, numberObj.TypeName(), numberObj.DebugString()))
					}

					decimals, hasDecimals, err := getIntOption(optionsDict, "decimals")
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
					}
					sigfigs, hasSigfigs, err := getIntOption(optionsDict, "sigFigs")
					if err == nil && !hasSigfigs {
						sigfigs, hasSigfigs, err = getIntOption(optionsDict, "sigfigs")
					}
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
					}
					preserveInt, _, err := getBoolOption(optionsDict, "preserveInt")
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
					}
					thousandsSep, hasThousandsSep, err := getStringOption(optionsDict, "thousandsSep")
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
					}
					decimalPoint, hasDecimalPoint, err := getStringOption(optionsDict, "decimalPoint")
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
					}
					grouping, hasGrouping, err := getGroupingOption(optionsDict, "grouping")
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
					}

					if hasDecimals && decimals < 0 {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: 'decimals' in 'numFmt' must be non-negative, got %d\n", t.Line, t.Column, decimals))
					}
					if hasSigfigs && sigfigs <= 0 {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: 'sigfigs' in 'numFmt' must be positive, got %d\n", t.Line, t.Column, sigfigs))
					}

					if !hasDecimals && !hasSigfigs {
						sigfigs = 3
						hasSigfigs = true
					}

					if !hasDecimalPoint {
						decimalPoint = "."
					}

					// I Basically hate all this codex generated code, but it works for now.
					// Turns out formating a float isn't trivial.
					// See:
					// G. L. Steele and J. L. White. How to print floating-point numbers accurately. In PLDI, 1991.
					// F. Loitsch. Printing floating-point numbers quickly and accurately with integers. In PLDI, 2010.
					// Andrysco Jhala Lerner - Printing Floating-Point Numbers An Always Correct Method
					var formatted string
					if hasDecimals {
						formatted = fmt.Sprintf("%.*f", decimals, num)
					} else if hasSigfigs {
						if preserveInt {
							if intObj, ok := numberObj.(MShellInt); ok {
								absStr := fmt.Sprintf("%d", int(math.Abs(float64(intObj.Value))))
								if len(absStr) > sigfigs {
									formatted = fmt.Sprintf("%d", intObj.Value)
								}
							}
						}
						if formatted == "" {
							formatted = formatWithSigFigs(num, sigfigs)
						}
					} else {
						switch n := numberObj.(type) {
						case MShellInt:
							formatted = fmt.Sprintf("%d", n.Value)
						case MShellFloat:
							formatted = strconv.FormatFloat(n.Value, 'f', -1, 64)
						}
					}

					sign := ""
					if strings.HasPrefix(formatted, "-") {
						sign = "-"
						formatted = formatted[1:]
					}

					intPart := formatted
					fracPart := ""
					if idx := strings.IndexByte(formatted, '.'); idx != -1 {
						intPart = formatted[:idx]
						fracPart = formatted[idx+1:]
					}

					intPart = groupIntPart(intPart, thousandsSep, grouping, hasThousandsSep, hasGrouping)

					if fracPart != "" {
						formatted = sign + intPart + "." + fracPart
					} else {
						formatted = sign + intPart
					}

					if decimalPoint != "." {
						if idx := strings.IndexByte(formatted, '.'); idx != -1 {
							formatted = formatted[:idx] + decimalPoint + formatted[idx+1:]
						}
					}

					stack.Push(MShellString{formatted})
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
					stack.Push(MShellPath{tmpfile.Name()})
				} else if t.Lexeme == "tempDir" {
					tmpdir := os.TempDir()
					stack.Push(MShellPath{tmpdir})
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
						stack.Push(MShellBool{strings.HasSuffix(str2, str1)})
					} else if t.Lexeme == "startsWith" {
						stack.Push(MShellBool{strings.HasPrefix(str2, str1)})
					}
				} else if t.Lexeme == "leftPad" {
					obj1, obj2, obj3, err := stack.Pop3(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					totalLen, ok := obj1.(MShellInt)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot leftPad with a %s as the total length.\n", t.Line, t.Column, obj1.TypeName()))
					}

					padStr, err := obj2.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot leftPad with a %s as the pad string.\n", t.Line, t.Column, obj2.TypeName()))
					}

					if padStr == "" {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot leftPad with an empty pad string.\n", t.Line, t.Column))
					}

					inputStr, err := obj3.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot leftPad a %s.\n", t.Line, t.Column, obj3.TypeName()))
					}

					if totalLen.Value < 0 {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot leftPad to a negative total length (%d).\n", t.Line, t.Column, totalLen.Value))
					}

					if len(inputStr) >= totalLen.Value {
						stack.Push(MShellString{inputStr})
					} else {
						needed := totalLen.Value - len(inputStr)
						padLen := len(padStr)

						repeatCount := needed / padLen
						remainder := needed % padLen

						var builder strings.Builder
						builder.Grow(totalLen.Value)

						for range repeatCount {
							builder.WriteString(padStr)
						}

						if remainder > 0 {
							builder.WriteString(padStr[:remainder])
						}

						builder.WriteString(inputStr)
						stack.Push(MShellString{builder.String()})
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
						stack.Push(MShellBool{dayOfWeek == 0 || dayOfWeek == 6})
					} else if t.Lexeme == "isWeekday" {
						stack.Push(MShellBool{dayOfWeek != 0 && dayOfWeek != 6})
					} else if t.Lexeme == "dow" {
						stack.Push(MShellInt{dayOfWeek})
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

					stack.Push(MShellInt{int(dateTimeObj.Time.Unix())})
				} else if t.Lexeme == "toOleDate" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'toOleDate' operation on an empty stack.\n", t.Line, t.Column))
					}

					dateTimeObj, ok := obj1.(*MShellDateTime)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get an OLE date from a %s (%s).\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					oleDays := dateTimeObj.Time.In(time.UTC).Sub(oleAutomationEpoch).Hours() / 24
					stack.Push(MShellFloat{oleDays})
				} else if t.Lexeme == "fromUnixTime" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
					}

					intVal, ok := obj1.(MShellInt)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s (%s) to a datetime.\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					newTime := time.Unix(int64(intVal.Value), 0).UTC()

					stack.Push(&MShellDateTime{Time: newTime, OriginalString: newTime.Format("2006-01-02T15:04")})
				} else if t.Lexeme == "fromOleDate" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
					}

					if !obj1.IsNumeric() {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s (%s) to a datetime.\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					oleDays := obj1.FloatNumeric()
					newTime := oleAutomationEpoch.Add(time.Duration(oleDays * float64(24*time.Hour)))
					stack.Push(&MShellDateTime{Time: newTime, OriginalString: ""})
				} else if t.Lexeme == "writeFile" || t.Lexeme == "appendFile" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					path, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot write to a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					var contentBytes []byte
					if asBinary, ok := obj2.(MShellBinary); ok {
						contentBytes = []byte(asBinary)
					} else {
						contentStr, err := obj2.CastString()
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot write a %s to a file.\n", t.Line, t.Column, obj2.TypeName()))
						}
						contentBytes = []byte(contentStr)
					}

					var file *os.File
					if t.Lexeme == "writeFile" {
						file, err = os.Create(path)
					} else if t.Lexeme == "appendFile" {
						file, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
					}
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s: %s\n", t.Line, t.Column, path, err.Error()))
					}

					_, err = file.Write(contentBytes)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error writing to file %s: %s\n", t.Line, t.Column, path, err.Error()))
					}
					err = file.Close()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error closing file %s: %s\n", t.Line, t.Column, path, err.Error()))
					}
				} else if t.Lexeme == "rm" || t.Lexeme == "rmf" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'rm' operation on an empty stack.\n", t.Line, t.Column))
					}

					path, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot remove a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					// Check if the path is '/'. Refuse to remove it.
					if path == "/" {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: I'm sorry, I'm going to refuse to delete your root directory. Find another way.\n", t.Line, t.Column))
					}

					err = os.Remove(path)
					if err != nil && t.Lexeme == "rm" {
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

					// Check if destination is a directory
					if fi, err := os.Stat(destination); err == nil && fi.IsDir() {
						// If it is a directory, append the source file name to the destination
						destination = filepath.Join(destination, filepath.Base(source))
					}

					if t.Lexeme == "mv" {
						const tries = 5
						var mvErr error
						mvErr = nil
						for mvTryCount := range tries {
							mvErr = os.Rename(source, destination)
							if mvErr == nil {
								break
							} else {
								time.Sleep(time.Duration(25*(mvTryCount+1)) * time.Millisecond) // linear backoff
							}
						}

						if mvErr != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error in %s from '%s' to '%s': %s\n", t.Line, t.Column, t.Lexeme, source, destination, mvErr.Error()))
						}
					} else if t.Lexeme == "cp" {
						err = CopyFile(source, destination)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error in %s from '%s' to '%s': %s\n", t.Line, t.Column, t.Lexeme, source, destination, err.Error()))
						}
					}
				} else if t.Lexeme == "zipDirInc" || t.Lexeme == "zipDirExc" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					zipPath, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot zip into a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					sourceDir, err := obj2.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot zip from a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}

					preserveRoot := t.Lexeme == "zipDirInc"
					if err := zipDirectory(sourceDir, zipPath, preserveRoot); err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
					}
				} else if t.Lexeme == "zipPack" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					zipPath, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot zip into a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					list, ok := obj2.(*MShellList)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: zipPack expects a list of dictionaries describing the entries to add. Found %s.\n", t.Line, t.Column, obj2.TypeName()))
					}

					entries := make([]zipPackItem, 0, len(list.Items))
					for idx, item := range list.Items {
						entryDict, ok := item.(*MShellDict)
						if !ok {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: zipPack entry %d is not a dictionary. Found %s.\n", t.Line, t.Column, idx, item.TypeName()))
						}

						sourceObj, ok := entryDict.Items["path"]
						if !ok {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: zipPack entry %d is missing required 'path'.\n", t.Line, t.Column, idx))
						}

						sourcePath, err := sourceObj.CastString()
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: zipPack entry %d had a non-string path (%s).\n", t.Line, t.Column, idx, sourceObj.TypeName()))
						}

						packItem := zipPackItem{SourcePath: sourcePath, PreserveRoot: true}
						if archiveObj, ok := entryDict.Items["archivePath"]; ok {
							archivePath, err := archiveObj.CastString()
							if err != nil {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: zipPack entry %d had an invalid archivePath (%s).\n", t.Line, t.Column, idx, archiveObj.TypeName()))
							}
							packItem.ArchivePath = archivePath
							packItem.PreserveRoot = false
						}
						if modeObj, ok := entryDict.Items["mode"]; ok {
							modeInt, ok := modeObj.(MShellInt)
							if !ok {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: zipPack entry %d had a non-integer mode (%s).\n", t.Line, t.Column, idx, modeObj.TypeName()))
							}
							mode := os.FileMode(modeInt.Value)
							packItem.ModeOverride = &mode
						}
						entries = append(entries, packItem)
					}

					if err := buildZipFromEntries(entries, zipPath); err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
					}
				} else if t.Lexeme == "zipList" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'zipList' operation on an empty stack.\n", t.Line, t.Column))
					}

					zipPath, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot list entries in a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					entries, err := collectZipMetadata(zipPath)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
					}

					result := NewList(0)
					for _, entry := range entries {
						dict := NewDict()
						dict.Items["name"] = MShellString{entry.Name}
						dict.Items["compressedSize"] = MShellInt{entry.CompressedSize}
						dict.Items["uncompressedSize"] = MShellInt{entry.UncompressedSize}
						dict.Items["isDir"] = MShellBool{entry.IsDir}
						dict.Items["perm"] = MShellInt{int(entry.Mode.Perm())}
						dict.Items["executable"] = MShellBool{!entry.IsDir && entry.Mode.Perm()&0o111 != 0}
						dict.Items["modified"] = &MShellDateTime{Time: entry.Modified, OriginalString: entry.Modified.Format("2006-01-02T15:04:05")}
						result.Items = append(result.Items, dict)
					}
					stack.Push(result)
				} else if t.Lexeme == "zipExtract" {
					obj1, obj2, obj3, err := stack.Pop3(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					optionsDict, ok := obj1.(*MShellDict)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: zipExtract expects an options dictionary. Found %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					destDir, err := obj2.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: zipExtract destination must be a string/path. Found %s.\n", t.Line, t.Column, obj2.TypeName()))
					}

					zipPath, err := obj3.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: zipExtract source must be a string/path. Found %s.\n", t.Line, t.Column, obj3.TypeName()))
					}

					options, err := parseZipExtractOptions(optionsDict)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
					}

					if err := extractZipArchive(zipPath, destDir, options); err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
					}
				} else if t.Lexeme == "zipExtractEntry" {
					optionsObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'zipExtractEntry' operation on an empty stack.\n", t.Line, t.Column))
					}
					destObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: zipExtractEntry requires four arguments.\n", t.Line, t.Column))
					}
					entryObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: zipExtractEntry requires four arguments.\n", t.Line, t.Column))
					}
					zipObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: zipExtractEntry requires four arguments.\n", t.Line, t.Column))
					}

					optionsDict, ok := optionsObj.(*MShellDict)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: zipExtractEntry expects an options dictionary. Found %s.\n", t.Line, t.Column, optionsObj.TypeName()))
					}

					destPath, err := destObj.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: zipExtractEntry destination must be a string/path. Found %s.\n", t.Line, t.Column, destObj.TypeName()))
					}

					entryPath, err := entryObj.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: zipExtractEntry entry name must be a string/path. Found %s.\n", t.Line, t.Column, entryObj.TypeName()))
					}

					zipPath, err := zipObj.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: zipExtractEntry source must be a string/path. Found %s.\n", t.Line, t.Column, zipObj.TypeName()))
					}

					options, err := parseZipExtractEntryOptions(optionsDict)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
					}

					if err := extractZipEntry(zipPath, entryPath, destPath, options); err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
					}
				} else if t.Lexeme == "zipRead" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					entryPath, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: zipRead entry name must be a string/path. Found %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					zipPath, err := obj2.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: zipRead archive path must be a string/path. Found %s.\n", t.Line, t.Column, obj2.TypeName()))
					}

					data, found, err := readZipEntry(zipPath, entryPath)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: %s\n", t.Line, t.Column, err.Error()))
					}
					if !found {
						stack.Push(&Maybe{obj: nil})
					} else {
						stack.Push(&Maybe{obj: MShellBinary(data)})
					}
				} else if t.Lexeme == "skip" {
					// Skip like C# LINQ
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					intVal, ok := obj1.(MShellInt)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot skip a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}

					listVal, ok := obj2.(*MShellList)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot skip on a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}

					if intVal.Value < 0 {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot skip a negative number of items (%d).\n", t.Line, t.Column, intVal.Value))
					}

					// Don't fail on n > len(list), just return empty
					newObj, err := obj2.SliceStart(min(intVal.Value, len(listVal.Items)))
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

					switch objTyped := obj.(type) {
					case *MShellList:
						list := objTyped
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
						pipe := objTyped
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
						stack.Push(&Maybe{obj: nil})
					} else {
						stack.Push(&Maybe{obj: MShellInt{int(fileInfo.Size())}})
					}
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
						newList.Items = append(newList.Items, MShellPath{filepath.Join(path, file.Name())})
					}

					stack.Push(newList)
				} else if t.Lexeme == "runtime" {
					// Place the name of the current OS runtime on the stack
					stack.Push(MShellString{runtime.GOOS})
				} else if t.Lexeme == "sort" || t.Lexeme == "sortV" {
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

					// Create a new SHA256 hash
					h := sha256.New()
					// Read the file and write it to the hash
					_, err = io.Copy(h, file)
					file.Close()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error hashing file %s: %s\n", t.Line, t.Column, path, err.Error()))
					}

					// Get the hash sum
					sum := h.Sum(nil)
					// Convert to hex string
					hash := hex.EncodeToString(sum)
					// Push the hash onto the stack
					stack.Push(MShellString{hash})
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
						stack.Push(&Maybe{obj: nil})
					} else {
						maybe := Maybe{obj: value}
						stack.Push(&maybe)
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
							newList.Items[i] = MShellString{key}
							i++
						}
						sort.Slice(newList.Items, func(a, b int) bool {
							return newList.Items[a].(MShellString).Content < newList.Items[b].(MShellString).Content
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
						stack.Push(MShellBool{false})
					} else {
						stack.Push(MShellBool{true})
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
					stack.Push(MShellString{newStr})
				} else if t.Lexeme == "reFindAll" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}
					regexStr, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot use a %s as a regex.\n", t.Line, t.Column, obj1.TypeName()))
					}

					str, err := obj2.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot use a %s as a string to find all in.\n", t.Line, t.Column, obj2.TypeName()))
					}

					re, err := regexp.Compile(regexStr)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error compiling regex '%s': %s\n", t.Line, t.Column, regexStr, err.Error()))
					}

					matches := re.FindAllStringSubmatch(str, -1)
					matchList := NewList(0)
					for _, groupMatches := range matches {
						innerList := NewList(0)
						for _, groupMatch := range groupMatches {
							innerList.Items = append(innerList.Items, MShellString{groupMatch})
						}
						matchList.Items = append(matchList.Items, innerList)
					}

					stack.Push(matchList)
				} else if t.Lexeme == "reFindAllIndex" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}
					regexStr, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot use a %s as a regex.\n", t.Line, t.Column, obj1.TypeName()))
					}

					str, err := obj2.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot use a %s as a string to find all in.\n", t.Line, t.Column, obj2.TypeName()))
					}

					re, err := regexp.Compile(regexStr)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error compiling regex '%s': %s\n", t.Line, t.Column, regexStr, err.Error()))
					}

					matches := re.FindAllStringSubmatchIndex(str, -1)
					matchList := NewList(0)
					for _, groupMatches := range matches {
						innerList := NewList(0)
						for _, groupMatch := range groupMatches {
							innerList.Items = append(innerList.Items, MShellInt{groupMatch})
						}
						matchList.Items = append(matchList.Items, innerList)
					}

					stack.Push(matchList)
				} else if t.Lexeme == "parseCsv" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'parseCsv' operation on an empty stack.\n", t.Line, t.Column))
					}

					// If a path or literal, read the file as UTF-8. Else, read the string as the contents directly.
					var reader *csv.Reader
					var file *os.File
					switch obj1Typed := obj1.(type) {
					case MShellPath, MShellLiteral:
						path, _ := obj1.CastString()
						file, err = os.Open(path)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s: %s\n", t.Line, t.Column, path, err.Error()))
						}
						reader = csv.NewReader(file)
					case MShellString:
						// Create a new CSV reader directly from the string contents
						reader = csv.NewReader(strings.NewReader(obj1Typed.Content))
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
							file.Close()
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error reading CSV: %s\n", t.Line, t.Column, err.Error()))
						}
						// Turn record into MShellList of strings
						newInnerList := NewList(len(record))
						for i, val := range record {
							newInnerList.Items[i] = MShellString{val}
						}

						newOuterList.Items = append(newOuterList.Items, newInnerList)
					}
					stack.Push(newOuterList)
					file.Close()
				} else if t.Lexeme == "parseJson" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'parseJson' operation on an empty stack.\n", t.Line, t.Column))
					}

					// If a path or literal, read the file as UTF-8. Else, read the string as the contents directly.
					var jsonData []byte
					switch obj1Typed := obj1.(type) {
					case MShellPath, MShellLiteral:
						path, _ := obj1.CastString()
						file, err := os.Open(path)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s: %s\n", t.Line, t.Column, path, err.Error()))
						}

						jsonData, err = io.ReadAll(file)
						file.Close()
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error reading file %s: %s\n", t.Line, t.Column, path, err.Error()))
						}

					case MShellString:
						// Create a new JSON reader directly from the string contents
						jsonData = []byte(obj1Typed.Content)
					case MShellBinary:
						binaryData := []byte(obj1Typed)
						if !utf8.Valid(binaryData) {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing JSON: input is not valid UTF-8.\n", t.Line, t.Column))
						}
						jsonData = binaryData
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot parse a %s as JSON.\n", t.Line, t.Column, obj1.TypeName()))
					}

					var parsedData any
					err = json.Unmarshal(jsonData, &parsedData)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing JSON: %s\n", t.Line, t.Column, err.Error()))
					}

					// Convert the parsed data to analgous MShell types
					resultObj := ParseJsonObjToMshell(parsedData)
					stack.Push(resultObj)
				} else if t.Lexeme == "toJson" {
					// Convert an object to JSON
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'toJson' operation on an empty stack.\n", t.Line, t.Column))
					}

					jsonStr := obj1.ToJson()
					stack.Push(MShellString{jsonStr})
				} else if t.Lexeme == "type" {
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'type' operation on an empty stack.\n", t.Line, t.Column))
					}

					stack.Push(MShellString{obj1.TypeName()})

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
					dateTimeObj.OriginalString = ""
					stack.Push(dateTimeObj)
				} else if t.Lexeme == "floor" {
					// Round a number down to the nearest integer
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'floor' operation on an empty stack.\n", t.Line, t.Column))
					}

					if obj1.IsNumeric() {
						switch num := obj1.(type) {
						case MShellInt:
							stack.Push(num)
						case MShellFloat:
							stack.Push(MShellInt{int(math.Floor(num.Value))})
						default:
							floatVal := obj1.FloatNumeric()
							stack.Push(MShellInt{int(math.Floor(floatVal))})
						}
					} else {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot floor a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}
				} else if t.Lexeme == "ceil" {
					// Round a number up to the nearest integer
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'ceil' operation on an empty stack.\n", t.Line, t.Column))
					}

					if obj1.IsNumeric() {
						switch num := obj1.(type) {
						case MShellInt:
							stack.Push(num)
						case MShellFloat:
							stack.Push(MShellInt{int(math.Ceil(num.Value))})
						default:
							floatVal := obj1.FloatNumeric()
							stack.Push(MShellInt{int(math.Ceil(floatVal))})
						}
					} else {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot ceil a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}
				} else if t.Lexeme == "round" {
					// Round a float to the nearest integer
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'round' operation on an empty stack.\n", t.Line, t.Column))
					}

					if obj1.IsNumeric() {
						floatVal := obj1.FloatNumeric()
						rounded := int(math.Round(floatVal))
						stack.Push(MShellInt{rounded})
					} else {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot round a %s.\n", t.Line, t.Column, obj1.TypeName()))
					}
				} else if t.Lexeme == "toFixed" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					obj1Int, ok := obj1.(MShellInt)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The number of decimal places parameter in toFixed is not an integer. Found a %s (%s)\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					if !obj2.IsNumeric() {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: For '%s', cannot convert a %s (%s) to a number.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName(), obj2.DebugString()))
					}

					floatVal := obj2.FloatNumeric()

					stack.Push(MShellString{fmt.Sprintf("%.*f", obj1Int.Value, floatVal)})
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
					stack.Push(MShellInt{count})
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
					stringsSeen := make(map[string]any)
					pathsSeen := make(map[string]any)
					intsSeen := make(map[int]any)
					floatsSeen := make(map[float64]any)
					dateTimesSeen := make(map[time.Time]any)

					for i, item := range listObj.Items {
						switch itemTyped := item.(type) {
						case MShellString:
							strItem := itemTyped
							if _, ok := stringsSeen[strItem.Content]; !ok {
								newList.Items = append(newList.Items, strItem)
								stringsSeen[strItem.Content] = nil
							}
						case MShellPath:
							pathItem := itemTyped
							if _, ok := pathsSeen[pathItem.Path]; !ok {
								newList.Items = append(newList.Items, pathItem)
								pathsSeen[pathItem.Path] = nil
							}
						case MShellInt:
							intItem := itemTyped
							if _, ok := intsSeen[intItem.Value]; !ok {
								newList.Items = append(newList.Items, intItem)
								intsSeen[intItem.Value] = nil
							}
						case MShellFloat:
							floatItem := itemTyped
							if _, ok := floatsSeen[floatItem.Value]; !ok {
								newList.Items = append(newList.Items, floatItem)
								floatsSeen[floatItem.Value] = nil
							}
						case *MShellDateTime:
							dateTimeItem := itemTyped
							if _, ok := dateTimesSeen[dateTimeItem.Time]; !ok {
								newList.Items = append(newList.Items, dateTimeItem)
								dateTimesSeen[dateTimeItem.Time] = nil
							}
						case MShellLiteral:
							// Treat like strings
							literalItem := itemTyped
							if _, ok := stringsSeen[literalItem.LiteralText]; !ok {
								// Convert to a string
								newList.Items = append(newList.Items, MShellString{literalItem.LiteralText})
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

					// Check if obj2 is a list or a Maybe
					switch obj2Typed := obj2.(type) {
					case *MShellList:

						listObj := obj2Typed
						newList := NewList(len(listObj.Items))

						var mapStack MShellStack
						mapStack = []MShellObject{}

						for i, item := range listObj.Items {
							mapStack.Push(item)
							result, err := state.EvaluateQuote(*fn, &mapStack, context, definitions)
							if err != nil {
								return state.FailWithMessage(err.Error())
							}
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
						maybe := obj2Typed
						if maybe.obj == nil {
							stack.Push(maybe)
						} else {
							stack.Push(maybe.obj) // Push the object inside the Maybe
							preStackLen := len(*stack)
							result, err := state.EvaluateQuote(*fn, stack, context, definitions)
							if err != nil {
								return state.FailWithMessage(err.Error())
							}
							if result.ShouldPassResultUpStack() {
								return result
							}
							if len(*stack) != preStackLen {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: The function in 'map' did not return a single value, found %d values.\n", t.Line, t.Column, len(*stack)-preStackLen))
							}
							mapResult, _ := stack.Pop()
							stack.Push(&Maybe{obj: mapResult}) // Wrap the result back in a Maybe
						}
					}
				} else if t.Lexeme == "map2" {
					// Allow a binary function over two maybes
					obj1, obj2, obj3, err := stack.Pop3(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					// Check if obj1 is a function
					fn, ok := obj1.(*MShellQuotation)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The first parameter in 'map2' is expected to be a quotation function, found a %s (%s)\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					// Check if obj2 and obj3 are Maybe objects
					maybe1, ok1 := obj2.(*Maybe)
					maybe2, ok2 := obj3.(*Maybe)

					if !ok1 || !ok2 {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The second and third parameters in 'map2' are expected to be Maybe objects, found a %s (%s) and a %s (%s)\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString(), obj3.TypeName(), obj3.DebugString()))
					}

					if maybe1.obj == nil || maybe2.obj == nil {
						stack.Push(&Maybe{obj: nil}) // Both are None
					} else {
						// Push the objects inside the Maybe onto the stack
						preStackLen := len(*stack)

						stack.Push(maybe2.obj)
						stack.Push(maybe1.obj)

						result, err := state.EvaluateQuote(*fn, stack, context, definitions)
						if err != nil {
							return state.FailWithMessage(err.Error())
						}

						if result.ShouldPassResultUpStack() {
							return result
						}

						if len(*stack) != preStackLen+1 {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: The function in 'map2' did not return a single value, found %d values.\n", t.Line, t.Column, len(*stack)-preStackLen))
						}

						mapResult, _ := stack.Pop()
						stack.Push(&Maybe{obj: mapResult}) // Wrap the result back in a Maybe
					}
				} else if t.Lexeme == "isNone" {
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
					}

					if maybeObj, ok := obj.(*Maybe); ok {
						stack.Push(MShellBool{maybeObj.IsNone()})
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
				} else if t.Lexeme == "addDays" {
					// Add days to a date
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					if !obj1.IsNumeric() {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The first parameter in 'addDays' is expected to be numeric, found a %s (%s)\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					if dt, ok := obj2.(*MShellDateTime); !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The second parameter in 'addDays' is expected to be a date, found a %s (%s)\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString()))
					} else {
						// Add the days to the date
						daysToAdd := obj1.FloatNumeric()
						newTime := dt.Time.Add(time.Duration(daysToAdd * float64(24*time.Hour)))
						newDateTime := &MShellDateTime{Time: newTime, OriginalString: ""}
						stack.Push(newDateTime)
					}
				} else if t.Lexeme == "isCmd" {
					// Check if the top of the stack is a known command in the PATH
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
					}

					objStr, err := obj.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot check if a %s is a command.\n", t.Line, t.Column, obj.TypeName()))
					}

					// Check if the command is in the PATH
					_, found := context.Pbm.Lookup(objStr)
					stack.Push(MShellBool{found})
				} else if t.Lexeme == "parseHtml" {
					// Parse HTML from a string or file
					obj1, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'parseHtml' operation on an empty stack.\n", t.Line, t.Column))
					}

					var reader io.Reader
					var file *os.File
					switch obj1Typed := obj1.(type) {
					case MShellPath, MShellLiteral:
						path, _ := obj1.CastString()
						file, err = os.Open(path)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s: %s\n", t.Line, t.Column, path, err.Error()))
						}

						reader = file
					case MShellString:
						// Create a new HTML reader directly from the string contents
						reader = strings.NewReader(obj1Typed.Content)
					}

					// Parse file with html.Parse
					doc, err := html.Parse(reader)
					file.Close()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing HTML: %s\n", t.Line, t.Column, err.Error()))
					}

					// Convert the parsed HTML document to a dictionary
					d := nodeToDict(doc)
					stack.Push(d)
				} else if t.Lexeme == "inc" {
					// Increment an integer on the top of the stack, but do it as a reference.
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'inc' operation on an empty stack.\n", t.Line, t.Column))
					}

					intObj, ok := obj.(MShellInt)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot increment a %s.\n", t.Line, t.Column, obj.TypeName()))
					}

					// Increment the integer
					intObj.Value++
					stack.Push(intObj)
				} else if t.Lexeme == "keyValues" {
					// Get the keys and values of a dictionary as a list of lists
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'keyValues' operation on an empty stack.\n", t.Line, t.Column))
					}

					dict, ok := obj.(*MShellDict)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot get the key-values of a %s.\n", t.Line, t.Column, obj.TypeName()))
					}

					// Create a new list and add the key-value pairs to it
					newList := NewList(len(dict.Items))

					// Get keys and sort them
					keys := make([]string, 0, len(dict.Items))
					for key := range dict.Items {
						keys = append(keys, key)
					}

					sort.Strings(keys)

					// Add each key-value pair as a list
					for i, key := range keys {
						// Create a new list for the key-value pair
						pairList := NewList(2)
						pairList.Items[0] = MShellString{key} // Key
						pairList.Items[1] = dict.Items[key]    // Value
						newList.Items[i] = pairList
					}

					stack.Push(newList)
				} else if t.Lexeme == "extend" {
					// Extend a list with another list in place
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					listObj, ok := obj1.(*MShellList)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The top of stack in 'extend' is expected to be a list, found a %s (%s)\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					extendListObj, ok := obj2.(*MShellList)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The second item on stack in 'extend' is expected to be a list, found a %s (%s)\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString()))
					}

					for _, item := range listObj.Items {
						extendListObj.Items = append(extendListObj.Items, item)
					}
					stack.Push(extendListObj)
				} else if t.Lexeme == "index" || t.Lexeme == "lastIndexOf" {
					// Find the first or last index of a substring in a string
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					subStr, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The first parameter in '%s' is expected to be stringable, found a %s (%s)\n", t.Line, t.Column, t.Lexeme, obj1.TypeName(), obj1.DebugString()))
					}

					str, err := obj2.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The second parameter in '%s' is expected to be stringable, found a %s (%s)\n", t.Line, t.Column, t.Lexeme, obj2.TypeName(), obj2.DebugString()))
					}

					index := strings.Index(str, subStr)
					if t.Lexeme == "lastIndexOf" {
						index = strings.LastIndex(str, subStr)
					}
					if index == -1 {
						stack.Push(&Maybe{obj: nil}) // No match found
					} else {
						stack.Push(&Maybe{obj: MShellInt{Value: index}})
					}
				} else if t.Lexeme == "absPath" {
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'absPath' operation on an empty stack.\n", t.Line, t.Column))
					}

					pathStr, err := obj.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s to a path.\n", t.Line, t.Column, obj.TypeName()))
					}

					// Convert to absolute path
					absPath, err := filepath.Abs(pathStr)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error converting path '%s' to absolute path: %s\n", t.Line, t.Column, pathStr, err.Error()))
					}

					stack.Push(MShellPath{Path: absPath})
				} else if t.Lexeme == "bind" {
					// Maybe monad bind
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					// Check if obj1 is a function
					fn, ok := obj1.(*MShellQuotation)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The first parameter in 'bind' is expected to be a function, found a %s (%s)\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					// Check if obj2 is a Maybe
					maybeObj, ok := obj2.(*Maybe)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The second parameter in 'bind' is expected to be a Maybe, found a %s (%s)\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString()))
					}

					if maybeObj.obj == nil {
						stack.Push(maybeObj) // Push the Maybe as is
					} else {
						stack.Push(maybeObj.obj) // Push the object inside the Maybe
						preStackLen := len(*stack)

						result, err := state.EvaluateQuote(*fn, stack, context, definitions)
						if err != nil {
							return state.FailWithMessage(err.Error())
						}
						if result.ShouldPassResultUpStack() {
							return result
						}
						if len(*stack) != preStackLen {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: The function in 'bind' did not return a single value, found %d values.\n", t.Line, t.Column, len(*stack)-preStackLen))
						}
						mapResult, _ := stack.Pop()

						mapResultMaybe, ok := mapResult.(*Maybe)
						if !ok {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: The function in 'bind' did not return a Maybe, found a %s (%s).\n", t.Line, t.Column, mapResult.TypeName(), mapResult.DebugString()))
						}
						stack.Push(mapResultMaybe)
					}
				} else if t.Lexeme == "md5" {
					// Calculate the MD5 hash of a string
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'md5' operation on an empty stack.\n", t.Line, t.Column))
					}

					// Work either on string or path
					var data []byte
					switch objTyped := obj.(type) {
					case MShellString:
						data = []byte(objTyped.Content)
					case MShellPath:
						pathStr, err := obj.CastString()
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s to a string for MD5 hashing.\n", t.Line, t.Column, obj.TypeName()))
						}
						file, err := os.Open(pathStr)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s for MD5 hashing: %s\n", t.Line, t.Column, pathStr, err.Error()))
						}
						data, err = io.ReadAll(file)
						// Don't defer, do it now, because we don't really exit this loop until all tokens are consumed.
						file.Close()

						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error reading file %s for MD5 hashing: %s\n", t.Line, t.Column, pathStr, err.Error()))
						}

					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot calculate MD5 hash of a %s.\n", t.Line, t.Column, obj.TypeName()))
					}

					hash := md5.Sum(data)
					hashStr := hex.EncodeToString(hash[:])
					stack.Push(MShellString{hashStr})
				} else if t.Lexeme == "take" {
					// Take the first n items from a list
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					// obj1 should be an integer
					intObj, ok := obj1.(MShellInt)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The first parameter in 'take' is expected to be an integer, found a %s (%s)\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					if intObj.Value < 0 {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot take a negative number of items from a list.\n", t.Line, t.Column))
					}

					// obj2 should be a list or string
					switch obj2Typed := obj2.(type) {
					case *MShellList:
						listObj := obj2Typed
						length := min(intObj.Value, len(listObj.Items)) // Adjust to max length
						newList := NewList(length)
						for i := range length {
							newList.Items[i] = listObj.Items[i]
						}

						stack.Push(newList)
					case MShellString:
						strObj := obj2Typed
						length := min(intObj.Value, len(strObj.Content)) // Adjust to max length
						newStr := strObj.Content[:length]
						stack.Push(MShellString{newStr})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The second parameter in 'take' is expected to be a list or string, found a %s (%s)\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString()))
					}
				} else if t.Lexeme == "skip" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					// obj1 should be an integer
					intObj, ok := obj1.(MShellInt)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The first parameter in 'skip' is expected to be an integer, found a %s (%s)\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					if intObj.Value < 0 {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot skip a negative number of items.\n", t.Line, t.Column))
					}

					// obj2 should be a list or string
					switch obj2Typed := obj2.(type) {
					case *MShellList:
						listObj := obj2Typed
						length := max(0, len(listObj.Items)-intObj.Value)

						newList := NewList(length)
						for i := range length {
							newList.Items[i] = listObj.Items[i+intObj.Value]
						}

						stack.Push(newList)
					case MShellString:
						strObj := obj2Typed
						length := max(0, len(strObj.Content)-intObj.Value)
						newStr := strObj.Content[length:]
						stack.Push(MShellString{newStr})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The second parameter in 'skip' is expected to be a list or string, found a %s (%s)\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString()))
					}
				} else if t.Lexeme == "hostname" {
					host, err := os.Hostname()
					if err != nil {
						stack.Push(MShellString{"unknown"})
					} else {
						stack.Push(MShellString{host})
					}
				} else if t.Lexeme == "removeWindowsVolumePrefix" {
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'removeWindowsVolumePrefix' operation on an empty stack.\n", t.Line, t.Column))
					}

					asStr, err := obj.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s to a string for removing Windows volume prefix.\n", t.Line, t.Column, obj.TypeName()))
					}

					if runtime.GOOS == "windows" {
						stack.Push(MShellString{StripVolumePrefix(asStr)})
					} else {
						stack.Push(MShellString{asStr})
					}
				} else if t.Lexeme == "pop" {
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'pop' operation on an empty stack.\n", t.Line, t.Column))
					}

					if listObj, ok := obj.(*MShellList); ok {
						if len(listObj.Items) == 0 {
							stack.Push(&Maybe{obj: nil}) // No items to pop
						} else {
							item := listObj.Items[len(listObj.Items)-1]
							listObj.Items = listObj.Items[:len(listObj.Items)-1]
							stack.Push(&Maybe{obj: item}) // Push the popped item
						}
					} else {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot pop from a %s.\n", t.Line, t.Column, obj.TypeName()))
					}
				} else if t.Lexeme == "sortByCmp" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					// Obj2 should be list
					listToBeSorted, ok := obj2.(*MShellList)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The 2nd from top of stack in 'sortByCmp' is expected to be a list, found a %s (%s)\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString()))
					}
					// Obj1 should be a quotation
					quotation, ok := obj1.(*MShellQuotation)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The top of stack for 'sortByCmp' is expected to be a function, found a %s (%s)\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					// Using merge sort below because it is stable which allows for multiple levels of sorting, like "then by".
					// Merge sort generally also has good enough performance for now.
					n := len(obj2.(*MShellList).Items)
					array1 := obj2.(*MShellList).Items
					array2 := make([]MShellObject, n)

					curr_array := 0

					sorted_array := array1
					work_array := array2

					cmpStack := MShellStack{}

					for width := 1; width < n; width = 2 * width {
						for i := 0; i < n; i = i + 2*width {
							iLeftStart := i                // Inclusive left start
							iRightStart := min(i+width, n) // Inclusive right start, exclusive left end
							iEnd := min(i+2*width, n)      // Exclusive End

							leftIndex := iLeftStart
							rightIndex := iRightStart

							for k := iLeftStart; k < iEnd; k++ {
								if leftIndex < iRightStart {
									if rightIndex >= iEnd {
										work_array[k] = sorted_array[leftIndex]
										leftIndex++
									} else {
										// Do comparison
										// Clear stack
										cmpStack.Clear()
										cmpStack.Push(sorted_array[leftIndex])
										cmpStack.Push(sorted_array[rightIndex])

										result, err := state.EvaluateQuote(*quotation, &cmpStack, context, definitions)
										if err != nil {
											return state.FailWithMessage(err.Error())
										}

										if result.ShouldPassResultUpStack() {
											return result
										}

										// Check that an integer is on top of the stack.
										cmpResult, err := cmpStack.Pop()
										if err != nil {
											return state.FailWithMessage(fmt.Sprintf("%d:%d: The function in 'sortByCmp' did not return a value, found an empty stack.\n", t.Line, t.Column))
										}

										cmpInt, ok := cmpResult.(MShellInt)
										if !ok {
											return state.FailWithMessage(fmt.Sprintf("%d:%d: The function in 'sortByCmp' did not return an integer, found a %s (%s).\n", t.Line, t.Column, cmpResult.TypeName(), cmpResult.DebugString()))
										}

										// fmt.Fprintf(os.Stderr, "Comparing %s and %s: %d\n", sorted_array[leftIndex].DebugString(), sorted_array[rightIndex].DebugString(), cmpInt.Value)

										if cmpInt.Value <= 0 { // left <= right
											work_array[k] = sorted_array[leftIndex]
											leftIndex++
										} else { // left > right
											work_array[k] = sorted_array[rightIndex]
											rightIndex++
										}
									}
								} else {
									work_array[k] = sorted_array[rightIndex]
									rightIndex++
								}
							}
						}

						curr_array = 1 - curr_array
						if curr_array == 0 {
							sorted_array = array1
							work_array = array2
						} else {
							sorted_array = array2
							work_array = array1
						}
					}

					listToBeSorted.Items = sorted_array
					stack.Push(listToBeSorted)
				} else if t.Lexeme == "versionSortCmp" {
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					// objects should be castable as strings
					str2, err := obj1.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The first parameter in 'versionSortCmp' is expected to be stringable, found a %s (%s)\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}

					str1, err := obj2.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The second parameter in 'versionSortCmp' is expected to be stringable, found a %s (%s)\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString()))
					}

					stack.Push(MShellInt{Value: VersionSortCmp(str1, str2)})
				} else if t.Lexeme == "httpGet" || t.Lexeme == "httpPost" {
					// Expect a dictionary on the stack
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
					}

					dict, ok := obj.(*MShellDict)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The parameter in '%s' is expected to be a dictionary, found a %s (%s)\n", t.Line, t.Column, t.Lexeme, obj.TypeName(), obj.DebugString()))
					}

					urlStr, ok := dict.Items["url"]
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The dictionary in '%s' must contain a 'url' key.\n", t.Line, t.Column, t.Lexeme))
					}

					urlStrValue, err := urlStr.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The 'url' value in '%s' must be stringable, found a %s (%s)\n", t.Line, t.Column, t.Lexeme, urlStr.TypeName(), urlStr.DebugString()))
					}

					// Create HTTP client
					client := &http.Client{}
					client.Timeout = 30 * time.Second

					// Check for optional time out in seconds
					timeoutValue, ok := dict.Items["timeout"]
					if ok {
						timeoutInt, ok := timeoutValue.(MShellInt)
						if !ok {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: The 'timeout' value in '%s' must be an integer, found a %s (%s)\n", t.Line, t.Column, t.Lexeme, timeoutValue.TypeName(), timeoutValue.DebugString()))
						}

						if timeoutInt.Value <= 0 {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: The 'timeout' value in '%s' must be a positive integer, found %d.\n", t.Line, t.Column, t.Lexeme, timeoutInt.Value))
						}

						client.Timeout = time.Duration(timeoutInt.Value) * time.Second
					}

					// Create request
					var method string
					switch t.Lexeme {
					case "httpGet":
						method = "GET"
					case "httpPost":
						method = "POST"
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Unknown HTTP method '%s'.\n", t.Line, t.Column, t.Lexeme))
					}

					var body io.Reader
					// Doesn't make sense for GETs, but doesn't break anything?
					bodyStr, ok := dict.Items["body"]
					if ok {
						// Body should be a string
						bodyStrValue, err := bodyStr.CastString()
						// fmt.Fprintf(os.Stderr, "Body value in '%s': %s\n", t.Lexeme, bodyStrValue)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: The 'body' value in '%s' must be stringable, found a %s (%s)\n", t.Line, t.Column, t.Lexeme, bodyStr.TypeName(), bodyStr.DebugString()))
						}

						// Set the request body
						body = strings.NewReader(bodyStrValue)
					} else {
						body = nil
					}

					req, err := http.NewRequest(method, urlStrValue, body)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error creating HTTP request for '%s': %s\n", t.Line, t.Column, t.Lexeme, err.Error()))
					}

					headersList, ok := dict.Items["headers"]
					if ok {
						// Headers should be a dictionary of key-value pairs
						headersDict, ok := headersList.(*MShellDict)
						if !ok {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: The 'headers' value in '%s' must be a dictionary, found a %s (%s)\n", t.Line, t.Column, t.Lexeme, headersList.TypeName(), headersList.DebugString()))
						}
						// Create HTTP headers
						// reqHeaders := make(http.Header)
						for key, value := range headersDict.Items {
							strValue, err := value.CastString()
							if err != nil {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: The header value '%s' in '%s' must be stringable, found a %s (%s)\n", t.Line, t.Column, key, t.Lexeme, value.TypeName(), value.DebugString()))
							}

							req.Header.Add(key, strValue)
						}
					}

					// Dump the request to stderr for debugging
					// dump, _ := httputil.DumpRequestOut(req, true)
					// fmt.Fprintf(os.Stderr, "HTTP Request:\n%s\n", dump)

					// Make request
					resp, err := client.Do(req)

					if err != nil {
						stack.Push(&Maybe{obj: nil}) // No response
					} else {
						responseDict := NewDict()
						responseDict.Items["status"] = MShellInt{Value: resp.StatusCode}
						responseDict.Items["reason"] = MShellString{Content: resp.Status}
						responseHeaders := NewDict()
						for key, values := range resp.Header {
							valueList := NewList(len(values))
							for i, value := range values {
								valueList.Items[i] = MShellString{Content: value}
							}

							responseHeaders.Items[key] = valueList
						}
						responseDict.Items["headers"] = responseHeaders

						// Read body as a UTF-8 encoded string
						bodyBytes, err := io.ReadAll(resp.Body)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error reading response body in '%s': %s\n", t.Line, t.Column, t.Lexeme, err.Error()))
						}
						resp.Body.Close() // Close the response body
						responseDict.Items["body"] = MShellBinary(bodyBytes)

						// Push the response dictionary onto the stack
						stack.Push(&Maybe{obj: responseDict})
					}
				} else if t.Lexeme == "urlEncode" {
					// Top of stack should be dictionary or string.
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
					}

					var encodedStr string
					switch typedObj := obj.(type) {
					case MShellString:
						// URL encode the string
						encodedStr = url.QueryEscape(typedObj.Content)
					case MShellLiteral:
						// URL encode the literal string
						encodedStr = url.QueryEscape(typedObj.LiteralText)
					case *MShellDict:
						// URL encode the dictionary
						values := url.Values{}

						for key, value := range typedObj.Items {
							// If the value is a list, handle that case by duplicating
							if list, ok := value.(*MShellList); ok {
								for _, item := range list.Items {
									strValue, err := item.CastString()
									if err != nil {
										return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot URL encode a %s in dictionary value for key '%s'.\n", t.Line, t.Column, item.TypeName(), key))
									}
									values.Add(key, strValue)
								}
							} else if strValue, err := value.CastString(); err != nil {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot URL encode a %s in dictionary value for key '%s'.\n", t.Line, t.Column, value.TypeName(), key))
							} else {
								values.Add(key, strValue)
							}
						}

						encodedStr = values.Encode()
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on a %s.\n", t.Line, t.Column, t.Lexeme, obj.TypeName()))
					}

					stack.Push(MShellString{Content: encodedStr})
				} else if t.Lexeme == "base64encode" {
					// Convert binary data to a base64 string.
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'base64encode' operation on an empty stack.\n", t.Line, t.Column))
					}

					binaryObj, ok := obj.(MShellBinary)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The top of stack in 'base64encode' is expected to be a binary, found a %s (%s)\n", t.Line, t.Column, obj.TypeName(), obj.DebugString()))
					}

					encoded := base64.StdEncoding.EncodeToString([]byte(binaryObj))
					stack.Push(MShellString{Content: encoded})
				} else if t.Lexeme == "base64decode" {
					// Convert a base64 string to binary data.
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'base64decode' operation on an empty stack.\n", t.Line, t.Column))
					}

					strObj, ok := obj.(MShellString)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The top of stack in 'base64decode' is expected to be a string, found a %s (%s)\n", t.Line, t.Column, obj.TypeName(), obj.DebugString()))
					}

					decoded, err := base64.StdEncoding.DecodeString(strObj.Content)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error decoding base64 in 'base64decode': %s\n", t.Line, t.Column, err.Error()))
					}

					stack.Push(MShellBinary(decoded))
				} else if t.Lexeme == "utf8Str" {
					// Convert MShellBinary on the top of the stack to a UTF-8 string
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'utf8str' operation on an empty stack.\n", t.Line, t.Column))
					}

					binaryObj, ok := obj.(MShellBinary)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The top of stack in 'utf8str' is expected to be a binary, found a %s (%s)\n", t.Line, t.Column, obj.TypeName(), obj.DebugString()))
					}

					// Convert binary to string
					strContent := string(binaryObj)
					stack.Push(MShellString{Content: strContent})
				} else if t.Lexeme == "utf8Bytes" {
					// Convert MShellString on the top of the stack to UTF-8 bytes
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'utf8bytes' operation on an empty stack.\n", t.Line, t.Column))
					}

					strObj, ok := obj.(MShellString)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The top of stack in 'utf8bytes' is expected to be a string, found a %s (%s)\n", t.Line, t.Column, obj.TypeName(), obj.DebugString()))
					}

					// Convert string to bytes
					bytesContent := []byte(strObj.Content)
					stack.Push(MShellBinary(bytesContent))
				} else if t.Lexeme == "sleep" {
					// Sleep float number of seconds.
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'sleep' operation on an empty stack.\n", t.Line, t.Column))
					}

					// Expect a float or int
					if !obj.IsNumeric() {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot sleep for a %s.\n", t.Line, t.Column, obj.TypeName()))
					}

					secs := obj.FloatNumeric()
					if secs < 0 {
						// Ignore
					} else {
						// Sleep for the specified number of seconds
						time.Sleep(time.Duration(secs * float64(time.Second)))
					}
				} else if t.Lexeme == "parseLinkHeader" {
					// Parse a string in the form of https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Link#specifications
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'parseLinkHeader' operation on an empty stack.\n", t.Line, t.Column))
					}

					strObj, ok := obj.(MShellString)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The top of stack in 'parseLinkHeader' is expected to be a string, found a %s (%s)\n", t.Line, t.Column, obj.TypeName(), obj.DebugString()))
					}

					// Parse the link header
					links, err := ParseLinkHeaders(strObj.Content)
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing link header '%s': %s\n", t.Line, t.Column, strObj.Content, err.Error()))
					}

					// Create a new list to hold the parsed links
					linksList := NewList(len(links))
					for i, link := range links {
						linkDict := NewDict()
						linkDict.Items["url"] = MShellString{Content: link.Uri}
						linkDict.Items["rel"] = MShellString{Content: link.Rel}
						paramDict := NewDict()
						for key, value := range link.Params {
							paramDict.Items[key] = MShellString{Content: value}
						}
						linkDict.Items["params"] = paramDict
						linksList.Items[i] = linkDict
					}

					stack.Push(linksList)
				} else if t.Lexeme == "fileExists" {
					// Check if a file exists
					obj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do 'fileExists' operation on an empty stack.\n", t.Line, t.Column))
					}

					pathStr, err := obj.CastString()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert a %s to a string for checking file existence.\n", t.Line, t.Column, obj.TypeName()))
					}

					_, err = os.Lstat(pathStr)
					if err == nil {
						stack.Push(MShellBool{Value: true}) // File exists
					} else if os.IsNotExist(err) {
						stack.Push(MShellBool{Value: false}) // File definitely does not exist
					} else {
						// Something else went wrong (permissions, I/O error, etc.)
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Error checking file existence for '%s': %s\n", t.Line, t.Column, pathStr, err.Error()))
					}
				} else if t.Lexeme == "floatCmp" {
					// Implement compare function for floats
					obj1, obj2, err := stack.Pop2(t)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

					// Both objects should be floats
					float1, ok := obj1.(MShellFloat)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The second parameter in 'floatCmp' is expected to be a float, found a %s (%s)\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString()))
					}
					float2, ok := obj2.(MShellFloat)
					if !ok {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: The first parameter in 'floatCmp' is expected to be a float, found a %s (%s)\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString()))
					}

					// Compare the floats
					if float2.Value < float1.Value {
						stack.Push(MShellInt{Value: -1})
					} else if float2.Value > float1.Value {
						stack.Push(MShellInt{Value: 1})
					} else {
						stack.Push(MShellInt{Value: 0})
					}
				} else if t.Lexeme == "nullDevice" {
					stack.Push(MShellPath{Path: nullDevice})
				} else if t.Lexeme == "return" {
					// Return from the current function
					return EvalResult{
						Success:    true,
						Continue:   false,
						BreakNum:   0,
						ExitCode:   0,
						ExitCalled: false,
					}

				} else { // last new function
					// If we aren't in a list context, throw an error.
					// Nearly always this is unintended.
					if callStackItem.CallStackType != CALLSTACKLIST {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Found literal token '%s' outside of a list context. Normally this is unintended. Either make it a string literal or path, or ensure the definition is available.\n", t.Line, t.Column, t.Lexeme))
					}

					stack.Push(MShellLiteral{t.Lexeme})
				}
			} else if t.Type == ASTERISK {
				obj1, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
				}

				if asList, ok := obj1.(*MShellList); ok {
					asList.StdoutBehavior = STDOUT_COMPLETE
					stack.Push(asList)
				} else {
					obj2, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on a stack with only one item.\n", t.Line, t.Column, t.Lexeme))
					}

					if !obj1.IsNumeric() || !obj2.IsNumeric() {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot multiply a %s and a %s. If you are looking for wildcard glob, you want `\"*\" glob`.\n", t.Line, t.Column, obj2.TypeName(), obj1.TypeName()))
					}

					switch obj1.(type) {
					case MShellInt:
						switch obj2.(type) {
						case MShellInt:
							stack.Push(MShellInt{obj2.(MShellInt).Value * obj1.(MShellInt).Value})
						case MShellFloat:
							stack.Push(MShellFloat{obj2.(MShellFloat).Value * float64(obj1.(MShellInt).Value)})
						}
					case MShellFloat:
						switch obj2.(type) {
						case MShellInt:
							stack.Push(MShellFloat{float64(obj2.(MShellInt).Value) * float64(obj1.(MShellFloat).Value)})
					case MShellFloat:
							stack.Push(MShellFloat{obj2.(MShellFloat).Value * obj1.(MShellFloat).Value})
						}
					}
				}
			} else if t.Type == ASTERISKBINARY {
				obj1, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
				}

				switch objTyped := obj1.(type) {
				case *MShellList:
					objTyped.StdoutBehavior = STDOUT_BINARY
				case *MShellPipe:
					objTyped.StdoutBehavior = STDOUT_BINARY
				default:
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot capture binary stdout for a %s.\n", t.Line, t.Column, obj1.TypeName()))
				}
				stack.Push(obj1)

			} else if t.Type == CARET {
				// '^' is analogous to '*', but for stderr
				obj1, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
				}

				switch objTyped := obj1.(type) {
				case *MShellList:
					objTyped.StderrBehavior = STDERR_COMPLETE
				case *MShellPipe:
					objTyped.StderrBehavior = STDERR_COMPLETE
				default:
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot capture stderr for a %s.\n", t.Line, t.Column, obj1.TypeName()))
				}
				stack.Push(obj1)
			} else if t.Type == CARETBINARY {
				obj1, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
				}

				switch objTyped := obj1.(type) {
				case *MShellList:
					objTyped.StderrBehavior = STDERR_BINARY
				case *MShellPipe:
					objTyped.StderrBehavior = STDERR_BINARY
				default:
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot capture binary stderr for a %s.\n", t.Line, t.Column, obj1.TypeName()))
				}
				stack.Push(obj1)
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
				var stdout []byte
				var stderr []byte
				var stdoutBehavior StdoutBehavior
				var stderrBehavior StderrBehavior

				switch topTyped := top.(type) {
				case *MShellList:
					result, exitCode, stdout, stderr = RunProcess(*topTyped, context, state)
					stdoutBehavior = topTyped.StdoutBehavior
					stderrBehavior = topTyped.StderrBehavior
				case *MShellPipe:
					result, exitCode, stdout, stderr = state.RunPipeline(*topTyped, context, stack)
					stdoutBehavior = topTyped.StdoutBehavior
					stderrBehavior = topTyped.StderrBehavior
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

				if stdoutBehavior == STDOUT_LINES {
					newMShellList := NewList(0)
					scanner := bufio.NewScanner(bytes.NewReader(stdout))
					for scanner.Scan() {
						newMShellList.Items = append(newMShellList.Items, MShellString{scanner.Text()})
					}
					stack.Push(newMShellList)
				} else if stdoutBehavior == STDOUT_STRIPPED {
					stripped := strings.TrimSpace(string(stdout))
					stack.Push(MShellString{stripped})
				} else if stdoutBehavior == STDOUT_COMPLETE {
					stack.Push(MShellString{string(stdout)})
				} else if stdoutBehavior == STDOUT_BINARY {
					stack.Push(MShellBinary(stdout))
				} else if stdoutBehavior != STDOUT_NONE {
					// panic for unhandled
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Unhandled stdout behavior %d.\n", t.Line, t.Column, stdoutBehavior))
				}

				if stderrBehavior == STDERR_LINES {
					newMShellList := NewList(0)
					scanner := bufio.NewScanner(bytes.NewReader(stderr))
					for scanner.Scan() {
						newMShellList.Items = append(newMShellList.Items, MShellString{scanner.Text()})
					}
					stack.Push(newMShellList)
				} else if stderrBehavior == STDERR_STRIPPED {
					stripped := strings.TrimSpace(string(stderr))
					stack.Push(MShellString{stripped})
				} else if stderrBehavior == STDERR_COMPLETE {
					stack.Push(MShellString{string(stderr)})
				} else if stderrBehavior == STDERR_BINARY {
					stack.Push(MShellBinary(stderr))
				} else if stderrBehavior != STDERR_NONE {
					// panic for unhandled
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Unhandled stderr behavior %d.\n", t.Line, t.Column, stderrBehavior))
				}

				// Push the exit code onto the stack if a question was used to execute
				if t.Type == QUESTION {
					stack.Push(MShellInt{exitCode})
				}
			} else if t.Type == TRUE { // Token Type
				stack.Push(MShellBool{true})
			} else if t.Type == FALSE { // Token Type
				stack.Push(MShellBool{false})
			} else if t.Type == INTEGER { // Token Type
				intVal, err := strconv.Atoi(t.Lexeme)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing integer: %s\n", t.Line, t.Column, err.Error()))
				}

				stack.Push(MShellInt{intVal})
			} else if t.Type == STRING { // Token Type
				parsedString, err := ParseRawString(t.Lexeme)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Error parsing string: %s\n", t.Line, t.Column, err.Error()))
				}
				stack.Push(MShellString{parsedString})
			} else if t.Type == SINGLEQUOTESTRING { // Token Type
				stack.Push(MShellString{t.Lexeme[1 : len(t.Lexeme)-1]})
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
				switch secondTyped := secondObj.(type) {
				case *MShellQuotation:
					falseQuote = firstQuote

					trueQuote = secondTyped

					// Read the next object, should be bool or integer
					thrirdObj, err := stack.Pop()
					if err != nil {
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do an '%s' on a stack with only two quotes.\n", t.Line, t.Column, iff_name))
					}

					switch thirdTyped := thrirdObj.(type) {
					case MShellBool:
						condition = thirdTyped.Value
					case MShellInt:
						condition = thirdTyped.Value == 0
					}
				case MShellBool:
					trueQuote = firstQuote
					condition = secondTyped.Value
				case MShellInt:
					trueQuote = firstQuote
					condition = secondTyped.Value == 0
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
					result, err := state.EvaluateQuote(*quoteToExecute, stack, context, definitions)
					if err != nil {
						return state.FailWithMessage(err.Error())
					}

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
				case MShellInt:
					switch obj2.(type) {
					case MShellInt:
						stack.Push(MShellInt{obj2.(MShellInt).Value + obj1.(MShellInt).Value})
					case MShellFloat:
						stack.Push(MShellFloat{float64(obj2.(MShellFloat).Value) + float64(obj1.(MShellInt).Value)})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot add an integer to a %s (%s).\n", t.Line, t.Column, obj2.TypeName(), obj2.DebugString()))
					}
				case MShellFloat:
					switch obj2.(type) {
					case MShellFloat:
						stack.Push(MShellFloat{obj2.(MShellFloat).Value + obj1.(MShellFloat).Value})
					case MShellInt:
						stack.Push(MShellFloat{float64(obj2.(MShellInt).Value) + obj1.(MShellFloat).Value})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot add a float to a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				case MShellString:
					switch obj2.(type) {
					case MShellString:
						stack.Push(MShellString{obj2.(MShellString).Content + obj1.(MShellString).Content})
					case MShellLiteral:
						stack.Push(MShellString{obj2.(MShellLiteral).LiteralText + obj1.(MShellString).Content})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot add a string ('%s') to a %s (%s).\n", t.Line, t.Column, obj1.(MShellString).Content, obj2.TypeName(), obj2.DebugString()))
					}
				case MShellLiteral:
					switch obj2.(type) {
					case MShellString:
						stack.Push(MShellString{obj2.(MShellString).Content + obj1.(MShellLiteral).LiteralText})
					case MShellLiteral:
						stack.Push(MShellString{obj2.(MShellLiteral).LiteralText + obj1.(MShellLiteral).LiteralText})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot add a literal (%s) to a %s.\n", t.Line, t.Column, obj1.DebugString(), obj2.TypeName()))
					}
				case *MShellList:
					switch obj2.(type) {
					case *MShellList:
						newList := NewList(len(obj2.(*MShellList).Items) + len(obj1.(*MShellList).Items))
						copy(newList.Items, obj2.(*MShellList).Items)
						copy(newList.Items[len(obj2.(*MShellList).Items):], obj1.(*MShellList).Items)
						stack.Push(newList)
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot add a list to a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				case MShellPath:
					switch obj2.(type) {
					case MShellPath:
						// Do string join, not path join. Concat the strings
						stack.Push(MShellPath{obj2.(MShellPath).Path + obj1.(MShellPath).Path})
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
				case MShellInt:
					switch obj2.(type) {
					case MShellInt:
						stack.Push(MShellInt{obj2.(MShellInt).Value - obj1.(MShellInt).Value})
					case MShellFloat:
						stack.Push(MShellFloat{obj2.(MShellFloat).Value - float64(obj1.(MShellInt).Value)})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot subtract an integer from a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				case MShellFloat:
					switch obj2.(type) {
					case MShellFloat:
						stack.Push(MShellFloat{obj2.(MShellFloat).Value - obj1.(MShellFloat).Value})
					case MShellInt:
						stack.Push(MShellFloat{float64(obj2.(MShellInt).Value) - obj1.(MShellFloat).Value})
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot subtract a float from a %s.\n", t.Line, t.Column, obj2.TypeName()))
					}
				case *MShellDateTime:
					switch obj2.(type) {
					case *MShellDateTime:
						// Return a float with the difference in days.
						days := obj2.(*MShellDateTime).Time.Sub(obj1.(*MShellDateTime).Time).Hours() / 24
						stack.Push(MShellFloat{days})
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
				case MShellBool:
					switch obj2.(type) {
					case MShellBool:
						if t.Type == AND {
							stack.Push(MShellBool{obj2.(MShellBool).Value && obj1.(MShellBool).Value})
						} else {
							stack.Push(MShellBool{obj2.(MShellBool).Value || obj1.(MShellBool).Value})
						}
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '%s' to a %s and %s.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName(), obj1.TypeName()))
					}
				case *MShellQuotation:
					if t.Type == AND {
						if obj2.(MShellBool).Value {
							result, err := state.EvaluateQuote(*obj1.(*MShellQuotation), stack, context, definitions)
							if err != nil {
								return state.FailWithMessage(err.Error())
							}

							// Pop the top off the stack
							secondObj, err := stack.Pop()
							if err != nil {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: After executing the quotation in %s, the stack was empty.\n", t.Line, t.Column, t.Lexeme))
							}

							if result.ShouldPassResultUpStack() {
								return result
							}

							seconObjBool, ok := secondObj.(MShellBool)
							if !ok {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Expected a boolean after executing the quotation in %s, received a %s.\n", t.Line, t.Column, t.Lexeme, secondObj.TypeName()))
							}

							stack.Push(MShellBool{seconObjBool.Value})
						} else {
							stack.Push(MShellBool{false})
						}
					} else {
						if obj2.(MShellBool).Value {
							stack.Push(MShellBool{true})
						} else {

							result, err := state.EvaluateQuote(*obj1.(*MShellQuotation), stack, context, definitions)
							if err != nil {
								return state.FailWithMessage(err.Error())
							}

							// Pop the top off the stack
							secondObj, err := stack.Pop()
							if err != nil {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: After executing the quotation in %s, the stack was empty.\n", t.Line, t.Column, t.Lexeme))
							}

							if result.ShouldPassResultUpStack() {
								return result
							}

							seconObjBool, ok := secondObj.(MShellBool)
							if !ok {
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Expected a boolean after executing the quotation in %s, received a %s.\n", t.Line, t.Column, t.Lexeme, secondObj.TypeName()))
							}

							stack.Push(MShellBool{seconObjBool.Value})
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

				switch objTyped := obj.(type) {
				case MShellBool:
					stack.Push(MShellBool{!objTyped.Value})
				case MShellInt:
					if objTyped.Value == 0 {
						stack.Push(MShellBool{false})
					} else {
						stack.Push(MShellBool{true})
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
						stack.Push(MShellBool{obj2.FloatNumeric() >= obj1.FloatNumeric()})
					} else {
						stack.Push(MShellBool{obj2.FloatNumeric() <= obj1.FloatNumeric()})
					}
				} else {

					obj1Date, ok1 := obj1.(*MShellDateTime)
					obj2Date, ok2 := obj2.(*MShellDateTime)

					if ok1 && ok2 {
						if t.Type == GREATERTHANOREQUAL {
							stack.Push(MShellBool{obj2Date.Time.After(obj1Date.Time) || obj2Date.Time.Equal(obj1Date.Time)})
						} else {
							stack.Push(MShellBool{obj2Date.Time.Before(obj1Date.Time) || obj2Date.Time.Equal(obj1Date.Time)})
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
						stack.Push(MShellBool{obj2.FloatNumeric() > obj1.FloatNumeric()})
					} else {
						stack.Push(MShellBool{obj2.FloatNumeric() < obj1.FloatNumeric()})
					}
				} else {
					switch obj1.(type) {
					case MShellString:
						switch obj2.(type) {
						case *MShellList:
							if t.Type == GREATERTHAN {
								obj2.(*MShellList).StandardOutputFile = obj1.(MShellString).Content
							} else { // LESSTHAN, input redirection
								obj2.(*MShellList).StdinBehavior = STDIN_CONTENT
								obj2.(*MShellList).StandardInputContents = obj1.(MShellString).Content
							}
							stack.Push(obj2)
						case *MShellQuotation:
							if t.Type == GREATERTHAN {
								obj2.(*MShellQuotation).StandardOutputFile = obj1.(MShellString).Content
							} else { // LESSTHAN, input redirection
								obj2.(*MShellQuotation).StdinBehavior = STDIN_CONTENT
								obj2.(*MShellQuotation).StandardInputContents = obj1.(MShellString).Content
							}
							stack.Push(obj2)
						case *MShellPipe:
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a string (%s) to a Pipe (%s). Add the redirection to the final item in the pipeline.\n", t.Line, t.Column, obj1.DebugString(), obj2.DebugString()))
						default:
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a string (%s) to a %s (%s).\n", t.Line, t.Column, obj1.DebugString(), obj2.TypeName(), obj2.DebugString()))
						}
					case MShellBinary:
						if t.Type == LESSTHAN {
							switch obj2.(type) {
							case *MShellList:
								obj2.(*MShellList).StdinBehavior = STDIN_BINARY
								obj2.(*MShellList).StandardInputBinary = obj1.(MShellBinary)
								obj2.(*MShellList).StandardInputContents = ""
								obj2.(*MShellList).StandardInputFile = ""
								stack.Push(obj2)
							case *MShellQuotation:
								obj2.(*MShellQuotation).StdinBehavior = STDIN_BINARY
								obj2.(*MShellQuotation).StandardInputBinary = obj1.(MShellBinary)
								obj2.(*MShellQuotation).StandardInputContents = ""
								obj2.(*MShellQuotation).StandardInputFile = ""
								stack.Push(obj2)
							case *MShellPipe:
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect binary data (%s) to a Pipe (%s). Add the redirection to the final item in the pipeline.\n", t.Line, t.Column, obj1.DebugString(), obj2.DebugString()))
							default:
								return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect binary data (%s) to a %s (%s).\n", t.Line, t.Column, obj1.DebugString(), obj2.TypeName(), obj2.DebugString()))
							}
						} else {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect binary data (%s) to a %s (%s). Use '<' for input redirection.\n", t.Line, t.Column, obj1.DebugString(), obj2.TypeName(), obj2.DebugString()))
						}
					case MShellLiteral:
						switch obj2.(type) {
						case *MShellList:
							if t.Type == GREATERTHAN {
								obj2.(*MShellList).StandardOutputFile = obj1.(MShellLiteral).LiteralText
							} else { // LESSTHAN, input redirection
								obj2.(*MShellList).StdinBehavior = STDIN_CONTENT
								obj2.(*MShellList).StandardInputFile = obj1.(MShellLiteral).LiteralText
							}
							stack.Push(obj2)
						case *MShellQuotation:
							if t.Type == GREATERTHAN {
								obj2.(*MShellQuotation).StandardOutputFile = obj1.(MShellLiteral).LiteralText
							} else {
								obj2.(*MShellQuotation).StdinBehavior = STDIN_CONTENT
								obj2.(*MShellQuotation).StandardInputContents = obj1.(MShellLiteral).LiteralText
							}
						default:
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a %s (%s) to a %s (%s).\n", t.Line, t.Column, obj1.TypeName(), obj1.DebugString(), obj2.TypeName(), obj2.DebugString()))
						}

					case MShellPath:
						switch obj2.(type) {
						case *MShellList:
							if t.Type == GREATERTHAN {
								obj2.(*MShellList).StandardOutputFile = obj1.(MShellPath).Path
							} else { // LESSTHAN, input redirection
								obj2.(*MShellList).StdinBehavior = STDIN_FILE
								obj2.(*MShellList).StandardInputFile = obj1.(MShellPath).Path
							}
							stack.Push(obj2)
						case *MShellQuotation:
							if t.Type == GREATERTHAN {
								obj2.(*MShellQuotation).StandardOutputFile = obj1.(MShellPath).Path
							} else {
								obj2.(*MShellQuotation).StdinBehavior = STDIN_FILE
								obj2.(*MShellQuotation).StandardInputFile = obj1.(MShellPath).Path
							}
							stack.Push(obj2)
						default:
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a path (%s) to a %s (%s).\n", t.Line, t.Column, obj1.DebugString(), obj2.TypeName(), obj2.DebugString()))
						}
					case *MShellDateTime:
						switch obj2.(type) {
						case *MShellDateTime:
							if t.Type == GREATERTHAN {
								stack.Push(MShellBool{obj2.(*MShellDateTime).Time.After(obj1.(*MShellDateTime).Time)})
							} else {
								stack.Push(MShellBool{obj2.(*MShellDateTime).Time.Before(obj1.(*MShellDateTime).Time)})
							}
						default:
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot %s a datetime (%s) to a %s (%s).\n", t.Line, t.Column, t.Lexeme, obj1.DebugString(), obj2.TypeName(), obj2.DebugString()))
						}
					default:
						return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot do a %s operation with a %s (%s) and a %s (%s).\n", t.Line, t.Column, t.Lexeme, obj1.TypeName(), obj1.DebugString(), obj2.TypeName(), obj2.DebugString()))
					}
				}
			} else if t.Type == STDERRREDIRECT || t.Type == STDERRAPPEND { // Token Type
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

				switch obj2Typed := obj2.(type) {
				case *MShellList:
					obj2Typed.StandardErrorFile = redirectFile
					obj2Typed.AppendError = t.Type == STDERRAPPEND
					stack.Push(obj2Typed)
				case *MShellQuotation:
					obj2Typed.StandardErrorFile = redirectFile
					stack.Push(obj2Typed)
				default:
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect stderr to a %s.\n", t.Line, t.Column, obj2.TypeName()))
				}
			} else if t.Type == ENVSTORE { // Token Type
				obj, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Nothing on stack to set into %s environment variable.\n", t.Line, t.Column, t.Lexeme))
				}

				// Strip off the leading '$' and trailing '!' for the environment variable name
				varName := t.Lexeme[1 : len(t.Lexeme)-1]

				varValue, err := obj.CastString()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot export a %s.\n", t.Line, t.Column, obj.TypeName()))
				}

				err = os.Setenv(varName, varValue)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Could not set the environment variable '%s' to '%s'.\n", t.Line, t.Column, varName, varValue))
				}

				// If it was the PATH, refresh all the binaries
				if varName == "PATH" {
					context.Pbm.Update()
				}
			} else if t.Type == ENVCHECK {
				// Strip off the leading '$' and trailing '!' for the environment variable name
				varName := t.Lexeme[1 : len(t.Lexeme)-1]
				_, found := os.LookupEnv(varName)
				stack.Push(MShellBool{found})
			} else if t.Type == ENVRETREIVE { // Token Type
				envVarName := t.Lexeme[1:len(t.Lexeme)]
				varValue, found := os.LookupEnv(envVarName)

				if !found {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Could not get the environment variable '%s'.\n", t.Line, t.Column, envVarName))
				}

				stack.Push(MShellString{varValue})

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
						stack.Push(MShellString{envValue})
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
					Pbm:            context.Pbm,
				}

				if quotation.StdinBehavior != STDIN_NONE {
					if quotation.StdinBehavior == STDIN_CONTENT {
						loopContext.StandardInput = strings.NewReader(quotation.StandardInputContents)
					} else if quotation.StdinBehavior == STDIN_BINARY {
						loopContext.StandardInput = bytes.NewReader(quotation.StandardInputBinary)
					} else if quotation.StdinBehavior == STDIN_FILE {
						file, err := os.Open(quotation.StandardInputFile)
						if err != nil {
							return state.FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s for reading: %s\n", t.Line, t.Column, quotation.StandardInputFile, err.Error()))
						}
						loopContext.StandardInput = file
						// TODO: This probably shouldn't be done here like this
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
					// TODO: This probably shouldn't be done here like this
					defer file.Close()
				}

				maxLoops := 15000000
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

				stack.Push(MShellBool{doesEqual})
			} else if t.Type == INTERPRET { // Token Type
				obj, err := stack.Pop()
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Cannot interpret an empty stack.\n", t.Line, t.Column))
				}

				quotation, ok := obj.(*MShellQuotation)
				if !ok {
					return state.FailWithMessage(fmt.Sprintf("%d:%d: Argument for interpret expected to be a quotation, received a %s (%s)\n", t.Line, t.Column, obj.TypeName(), obj.DebugString()))
				}

				result, err := state.EvaluateQuote(*quotation, stack, context, definitions)
				if err != nil {
					return state.FailWithMessage(err.Error())
				}

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

				stack.Push(MShellString{state.PositionalArgs[posIndex-1]})
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
							stack.Push(MShellString{""})
							stack.Push(MShellBool{false})
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

						stack.Push(MShellString{line})
						stack.Push(MShellBool{true})
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
									stack.Push(MShellString{""})
									stack.Push(MShellBool{false})
								} else {
									// Else, we have a final that wasn't terminated by a newline. Still try to remove '\r' if it's there
									builderStr := line.String()
									if len(builderStr) > 0 && builderStr[len(builderStr)-1] == '\r' {
										builderStr = builderStr[:len(builderStr)-1]
									}
									stack.Push(MShellString{builderStr})
									stack.Push(MShellBool{true})
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

							stack.Push(MShellString{builderStr})
							stack.Push(MShellBool{true})
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

				stack.Push(MShellString{obj.ToString()})
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

				switch objTyped := obj.(type) {
				case *MShellList:
					list := objTyped
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
					pipe := objTyped
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
				stack.Push(MShellFloat{floatVal})
			} else if t.Type == PATH { // Token Type
				stack.Push(MShellPath{t.Lexeme[1 : len(t.Lexeme)-1]})
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
				stack.Push(&MShellDateTime{Time: dt, OriginalString: t.Lexeme})
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

				stack.Push(MShellBool{!doesEqual})
			} else {
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

func (state *EvalState) EvaluateFormatString(lexeme string, context ExecuteContext, definitions []MShellDefinition, callStackItem CallStackItem) (MShellString, error) {

	allRunes := []rune(lexeme)

	if len(allRunes) < 3 {
		return MShellString{""}, fmt.Errorf("Found format string with less than 3 characters: %s", lexeme)
	}

	var b strings.Builder

	index := 2
	mode := FORMATMODENORMAL

	formatStrStartIndex := -1
	formatStrEndIndex := -1

	lexer := NewLexer("", nil)
	parser := MShellParser{lexer: lexer}

	for index < len(allRunes)-1 {
		c := allRunes[index]
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
				return MShellString{""}, fmt.Errorf("invalid escape character '%c'", c)
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
				formatStr := string(allRunes[formatStrStartIndex+1 : formatStrEndIndex])

				// Evaluate the format string
				lexer.resetInput(formatStr)
				parser.NextToken()
				contents, err := parser.ParseFile()
				if err != nil {
					return MShellString{""}, fmt.Errorf("Error parsing format string %s: %s", formatStr, err)
				}

				// Evaluate the format string contents
				var stack MShellStack
				stack = []MShellObject{}

				result := state.Evaluate(contents.Items, &stack, context, definitions, callStackItem)

				if !result.Success {
					return MShellString{""}, fmt.Errorf("Error evaluating format string %s", formatStr)
				}

				if len(stack) != 1 {
					return MShellString{""}, fmt.Errorf("Format string %s did not evaluate to a single value", formatStr)
				}

				// Get the string representation of the result
				resultStr, err := stack[0].CastString()
				if err != nil {
					return MShellString{""}, fmt.Errorf("Format string contents %s did not evaluate to a stringable value", formatStr)
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
		return MShellString{""}, fmt.Errorf("Format string ended in an invalid state")
	}

	return MShellString{b.String()}, nil
}

type Executable interface {
	Execute(state *EvalState, context ExecuteContext, stack *MShellStack) (EvalResult, int, []byte, []byte)
	GetStandardInputFile() string
	GetStandardOutputFile() string
}

func (list *MShellList) Execute(state *EvalState, context ExecuteContext, stack *MShellStack) (EvalResult, int, []byte, []byte) {
	result, exitCode, stdoutResult, stderrResult := RunProcess(*list, context, state)
	return result, exitCode, stdoutResult, stderrResult
}

func (quotation *MShellQuotation) Execute(state *EvalState, context ExecuteContext, stack *MShellStack, definitions []MShellDefinition, callStack CallStack) (EvalResult, int) {
	quotationContext := ExecuteContext{
		StandardInput:  nil,
		StandardOutput: nil,
		Variables:      quotation.Variables,
		Pbm:            context.Pbm,
	}

	if quotation.StdinBehavior != STDIN_NONE {
		if quotation.StdinBehavior == STDIN_CONTENT {
			quotationContext.StandardInput = strings.NewReader(quotation.StandardInputContents)
		} else if quotation.StdinBehavior == STDIN_BINARY {
			quotationContext.StandardInput = bytes.NewReader(quotation.StandardInputBinary)
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

func (state *EvalState) ChangeDirectory(dir string) (EvalResult, int, []byte, []byte) {
	cwd, currDirErr := os.Getwd()

	err := os.Chdir(dir)
	if err != nil {
		return state.FailWithMessage(fmt.Sprintf("Error changing directory to %s: %s\n", dir, err.Error())), 1, nil, nil
	}

	if currDirErr == nil {
		// Update OLDPWD and PWD
		err = os.Setenv("OLDPWD", cwd)
		if err != nil {
			return state.FailWithMessage(fmt.Sprintf("Error setting OLDPWD: %s\n", err.Error())), 1, nil, nil
		}

		err = os.Setenv("PWD", dir)
		if err != nil {
			return state.FailWithMessage(fmt.Sprintf("Error setting PWD: %s\n", err.Error())), 1, nil, nil
		}
	}

	return SimpleSuccess(), 0, nil, nil
}

func RunProcess(list MShellList, context ExecuteContext, state *EvalState) (EvalResult, int, []byte, []byte) {
	// Returns the result of running the process, the exit code, and the stdout result

	// Check for empty list
	if len(list.Items) == 0 {
		return state.FailWithMessage("Cannot execute an empty list.\n"), 1, nil, nil
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
			return state.FailWithMessage(fmt.Sprintf("Item (%s) cannot be used as a command line argument.\n", item.DebugString())), 1, nil, nil
		} else {
			commandLineArgs = append(commandLineArgs, item.CommandLine())
		}
	}

	// Need to check length here. You could have had a list of empty lists like:
	// [[] [] [] []]
	if len(commandLineArgs) == 0 {
		return state.FailWithMessage("After list flattening, there still were no arguments to execute.\n"), 1, nil , nil
	}

	// Handle cd command specially
	if commandLineArgs[0] == "cd" {
		if len(commandLineArgs) > 3 {
			fmt.Fprint(os.Stderr, "cd command only takes one argument.\n")
		} else if len(commandLineArgs) == 2 {
			// Check for -h or --help
			if commandLineArgs[1] == "-h" || commandLineArgs[1] == "--help" {
				fmt.Fprint(os.Stderr, "cd: cd [dir]\nChange the shell working directory.\n")
				return SimpleSuccess(), 0, nil, nil
			} else {
				return state.ChangeDirectory(commandLineArgs[1])
			}
		} else {
			// else cd to home directory
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return state.FailWithMessage(fmt.Sprintf("Error getting home directory: %s\n", err.Error())), 1, nil, nil
			}

			return state.ChangeDirectory(homeDir)
		}

		return SimpleSuccess(), 0, nil, nil
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
			return state.FailWithMessage(fmt.Sprintf("Command '%s' not found in path.\n", commandLineArgs[0])), 1, nil, nil
		}

		cmdPath, found = context.Pbm.Lookup(commandLineArgs[0])
		if !found {
			return state.FailWithMessage(fmt.Sprintf("Command '%s' not found in path.\n", commandLineArgs[0])), 1, nil, nil
		}
	}

	// For interpreted files on Windows, we need to essentially do what a shebang on Linux does
	cmdItems, err := context.Pbm.ExecuteArgs(cmdPath)
	if err != nil {
		return state.FailWithMessage(fmt.Sprintf("On Windows, we currently don't handle this file/extension: %s\n", cmdPath)), 1, nil, nil
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
			return state.FailWithMessage(fmt.Sprintf("Error opening file %s for writing: %s\n", list.StandardOutputFile, err.Error())), 1, nil, nil
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
		} else if list.StdinBehavior == STDIN_BINARY {
			cmd.Stdin = bytes.NewReader(list.StandardInputBinary)
		} else if list.StdinBehavior == STDIN_FILE {
			// Open the file for reading
			file, err := os.Open(list.StandardInputFile)
			if err != nil {
				return state.FailWithMessage(fmt.Sprintf("Error opening file %s for reading: %s\n", list.StandardInputFile, err.Error())), 1, nil, nil
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
		var file *os.File
		if list.AppendError {
			file, err = os.OpenFile(list.StandardErrorFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
		} else {
			file, err = os.Create(list.StandardErrorFile)
		}
		if err != nil {
			return state.FailWithMessage(fmt.Sprintf("Error opening file %s for writing: %s\n", list.StandardErrorFile, err.Error())), 1, nil, nil
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
	return SimpleSuccess(), exitCode, commandSubWriter.Bytes(), stderrBuffer.Bytes()
}

func (state *EvalState) RunPipeline(MShellPipe MShellPipe, context ExecuteContext, stack *MShellStack) (EvalResult, int, []byte, []byte) {
	if len(MShellPipe.List.Items) == 0 {
		return state.FailWithMessage("Cannot execute an empty pipe.\n"), 1, nil, nil
	}

	// Check that all list items are Executables
	for i, item := range MShellPipe.List.Items {
		if _, ok := item.(Executable); !ok {
			return state.FailWithMessage(fmt.Sprintf("Item %d (%s) in pipe is not a list or a quotation.\n", i, item.DebugString())), 1, nil, nil
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
			return state.FailWithMessage(fmt.Sprintf("Error creating pipe: %s\n", err.Error())), 1, nil, nil
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
			Pbm:            context.Pbm,
		}

		if i == 0 {
			// Stdin should use the context of this function, or the file marked on the initial object
			executableStdinFile := MShellPipe.List.Items[i].(Executable).GetStandardInputFile()

			if executableStdinFile != "" {
				file, err := os.Open(executableStdinFile)
				if err != nil {
					return state.FailWithMessage(fmt.Sprintf("Error opening file %s for reading: %s\n", executableStdinFile, err.Error())), 1, nil, nil
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

	var stdoutBytes []byte
	var stderrBytes []byte

	if MShellPipe.StdoutBehavior != STDOUT_NONE {
		stdoutBytes = buf.Bytes()
	} else {
		stdoutBytes = nil
	}

	if MShellPipe.StderrBehavior != STDERR_NONE {
		stderrBytes = stdErrBuf.Bytes()
	} else {
		stderrBytes = nil
	}

	// Check for errors
	for i, result := range results {
		if !result.Success {
			return result, exitCodes[i], stdoutBytes, stderrBytes
		}
	}

	return SimpleSuccess(), exitCodes[len(exitCodes)-1], stdoutBytes, stderrBytes
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

func ParseJsonObjToMshell(jsonObj any) MShellObject {
	// See https://pkg.go.dev/encoding/json#Unmarshal
	switch o := jsonObj.(type) {
	case []any:
		list := NewList(0)
		for _, item := range o {
			parsedItem := ParseJsonObjToMshell(item)
			list.Items = append(list.Items, parsedItem)
		}
		return list

	case map[string]any:
		dict := NewDict()
		// TODO: decide and document how to handle duplicate keys in JSON objects
		for key, value := range o {
			dict.Items[key] = ParseJsonObjToMshell(value)
		}
		return dict

	case string:
		return MShellString{o}
	case float64:
		return MShellFloat{o}
	case bool:
		if o {
			return MShellBool{true}
		} else {
			return MShellBool{false}
		}
	case nil:
		return &Maybe{obj: nil}
	default:
		panic(fmt.Sprintf("Unknown JSON object type: %T", jsonObj))
		// There should be no other types in JSON, but if there are, we can handle them here
	}
}

// These 2 are courtesy of GPT-5.
// Don't make me regret this.

// StripVolumePrefix removes the Windows volume prefix from p (e.g. "C:",
// "\\server\\share", "\\\\?\\C:", "\\\\?\\UNC\\server\\share", "\\\\.\\COM1",
// "\\\\?\\Volume{GUID}") and returns the remainder. The result is guaranteed
// NOT to start with '\' or '/'.
//
// Examples (on Windows):
//
//	"C:\\foo\\bar"                         -> "foo\\bar"
//	"C:relative\\path"                     -> "relative\\path"
//	"\\\\server\\share\\dir\\file"         -> "dir\\file"
//	"\\\\?\\C:\\path\\to\\file"            -> "path\\to\\file"
//	"\\\\?\\UNC\\server\\share\\dir"       -> "dir"
//	"\\\\?\\Volume{GUID}\\Windows\\Temp"   -> "Windows\\Temp"
//	"\\\\.\\COM1"                          -> ""  (device path, no remainder)
func StripVolumePrefix(p string) string {
	if runtime.GOOS != "windows" {
		return p
	}
	vol := filepath.VolumeName(p)
	rest := strings.TrimPrefix(p, vol)
	// Remove any leading separators so it doesn't begin with "\" or "/".
	rest = strings.TrimLeft(rest, `\/`)
	return rest
}

// DirNoVolume is a convenience wrapper that returns the directory of p
// with any Windows volume removed and no leading separator.
func DirNoVolume(p string) string {
	if runtime.GOOS != "windows" {
		return filepath.Dir(p)
	}
	dir := filepath.Dir(p)
	return StripVolumePrefix(dir)
}

func VersionSortCmp(s1 string, s2 string) int {
	i := 0
	j := 0

	for {
		if i >= len(s1) {
			if j >= len(s2) {
				return 0
			}
			return -1
		}
		if j >= len(s2) {
			return 1
		}

		iStart := i
		jStart := j

		char1 := s1[i]
		char2 := s2[j]
		i++
		j++
		isDigit1 := false
		isDigit2 := false

		if char1 >= '0' && char1 <= '9' {
			isDigit1 = true
		}

		if char2 >= '0' && char2 <= '9' {
			isDigit2 = true
		}

		if isDigit1 && !isDigit2 {
			return 1
		}

		if !isDigit1 && isDigit2 {
			return -1
		}

		if !isDigit1 && !isDigit2 {
			if char1 < char2 {
				return -1
			} else if char1 > char2 {
				return 1
			} else {
				continue
			}
		}

		// Both digits, read in all digits
		for i < len(s1) {
			if s1[i] >= '0' && s1[i] <= '9' {
				i++
			} else {
				break
			}
		}

		// Both digits, read in all digits
		for j < len(s2) {
			if s2[j] >= '0' && s2[j] <= '9' {
				j++
			} else {
				break
			}
		}

		// Get the integer representations
		num1, _ := strconv.Atoi(s1[iStart:i])
		num2, _ := strconv.Atoi(s2[jStart:j])

		if num1 < num2 {
			return -1
		} else if num1 > num2 {
			return 1
		} else if (i - iStart) > (j - jStart) { // Sort more leading zeros after
			return 1
		} else if (i - iStart) < (j - jStart) {
			return -1
		}
		// else continue because they are equal
	}
}

type LinkHeader struct {
	Uri    string
	Rel    string
	Params map[string]string
}

type zipPackItem struct {
	SourcePath   string
	ArchivePath  string
	ModeOverride *os.FileMode
	PreserveRoot bool
}

type zipEntryMetadata struct {
	Name             string
	CompressedSize   int
	UncompressedSize int
	Modified         time.Time
	IsDir            bool
	Mode             os.FileMode
}

type zipWriteOptions struct {
	overwrite           bool
	skipExisting        bool
	preservePermissions bool
}

type zipExtractOptions struct {
	zipWriteOptions
	stripComponents int
	pattern         string
}

type zipExtractEntryOptions struct {
	zipWriteOptions
	mkdirs bool
}

func zipDirectory(sourceDir, zipPath string, preserveRoot bool) error {
	info, err := os.Stat(sourceDir)
	if err != nil {
		return fmt.Errorf("Error stating %s: %w", sourceDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("zipDir expects a directory. %s is not a directory", sourceDir)
	}

	srcAbs, err := filepath.Abs(sourceDir)
	if err != nil {
		return fmt.Errorf("Error resolving %s: %w", sourceDir, err)
	}
	if err := ensureZipTargetNotInsideSource(srcAbs, zipPath); err != nil {
		return err
	}

	packItem := zipPackItem{
		SourcePath:   sourceDir,
		PreserveRoot: preserveRoot,
	}
	return buildZipFromEntries([]zipPackItem{packItem}, zipPath)
}

func ensureZipTargetNotInsideSource(sourceAbs string, zipPath string) error {
	zipAbs, err := filepath.Abs(zipPath)
	if err != nil {
		return fmt.Errorf("Error resolving destination %s: %w", zipPath, err)
	}
	sourceWithSep := ensureTrailingSeparator(sourceAbs)
	if zipAbs == sourceAbs || strings.HasPrefix(zipAbs, sourceWithSep) {
		return fmt.Errorf("Zip destination %s cannot be inside the source directory %s", zipPath, sourceAbs)
	}
	return nil
}

func buildZipFromEntries(items []zipPackItem, zipPath string) error {
	if len(items) == 0 {
		return fmt.Errorf("zipPack requires at least one entry")
	}

	if err := os.MkdirAll(filepath.Dir(zipPath), 0755); err != nil {
		return fmt.Errorf("Error creating parent directory for %s: %w", zipPath, err)
	}

	output, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("Error creating %s: %w", zipPath, err)
	}
	defer output.Close()

	zipWriter := zip.NewWriter(output)
	defer zipWriter.Close()

	zipAbs, err := filepath.Abs(zipPath)
	if err != nil {
		return fmt.Errorf("Error resolving %s: %w", zipPath, err)
	}

	for _, item := range items {
		info, err := os.Stat(item.SourcePath)
		if err != nil {
			return fmt.Errorf("Error stating %s: %w", item.SourcePath, err)
		}

		sourceAbs, err := filepath.Abs(item.SourcePath)
		if err != nil {
			return fmt.Errorf("Error resolving %s: %w", item.SourcePath, err)
		}
		sourceAbsWithSep := ensureTrailingSeparator(sourceAbs)
		if zipAbs == sourceAbs || strings.HasPrefix(zipAbs, sourceAbsWithSep) {
			return fmt.Errorf("Zip destination %s cannot be inside the source path %s", zipPath, sourceAbs)
		}

		if info.IsDir() {
			prefix := strings.Trim(item.ArchivePath, "/")
			if prefix == "" && item.PreserveRoot {
				prefix = filepath.Base(sourceAbs)
			}
			if err := addDirectoryToZip(zipWriter, item.SourcePath, prefix, item.ModeOverride); err != nil {
				return err
			}
			continue
		}

		name := item.ArchivePath
		if name == "" {
			name = filepath.Base(item.SourcePath)
		}
		name = strings.Trim(name, "/")
		if name == "" {
			return fmt.Errorf("zipPack entry for %s produced an empty archive path", item.SourcePath)
		}

		if err := addFileToZip(zipWriter, item.SourcePath, name, info, item.ModeOverride); err != nil {
			return err
		}
	}

	if err := zipWriter.Close(); err != nil {
		return fmt.Errorf("Error finalizing zip %s: %w", zipPath, err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("Error closing %s: %w", zipPath, err)
	}
	return nil
}

func addDirectoryToZip(zipWriter *zip.Writer, sourcePath, archivePrefix string, modeOverride *os.FileMode) error {
	cleanPrefix := strings.Trim(archivePrefix, "/")
	return filepath.WalkDir(sourcePath, func(pathStr string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(sourcePath, pathStr)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		var entryName string
		if relPath == "." {
			entryName = cleanPrefix
		} else if cleanPrefix == "" {
			entryName = relPath
		} else {
			entryName = path.Join(cleanPrefix, relPath)
		}

		entryName = strings.Trim(entryName, "/")
		if entryName == "" {
			// We skip the implicit root when no prefix is requested.
			return nil
		}
		if info.IsDir() {
			entryName += "/"
		}

		return addFileToZip(zipWriter, pathStr, entryName, info, modeOverride)
	})
}

func addFileToZip(zipWriter *zip.Writer, sourcePath, entryName string, info os.FileInfo, modeOverride *os.FileMode) error {
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = path.Clean(strings.ReplaceAll(entryName, "\\", "/"))
	if info.IsDir() && !strings.HasSuffix(header.Name, "/") {
		header.Name += "/"
	}
	if modeOverride != nil {
		header.SetMode(*modeOverride)
	}
	if !info.IsDir() {
		header.Method = zip.Deflate
	}

	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return nil
	}

	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := io.Copy(writer, file); err != nil {
		return err
	}
	return nil
}

func safeSizeToInt(size uint64) (int, error) {
	if size > math.MaxInt {
		return 0, fmt.Errorf("Zip entry size %d exceeds supported range", size)
	}
	return int(size), nil
}

func collectZipMetadata(zipPath string) ([]zipEntryMetadata, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("Error opening %s: %w", zipPath, err)
	}
	defer reader.Close()

	entries := make([]zipEntryMetadata, 0, len(reader.File))
	for _, file := range reader.File {
		compressed, err := safeSizeToInt(file.CompressedSize64)
		if err != nil {
			return nil, err
		}
		uncompressed, err := safeSizeToInt(file.UncompressedSize64)
		if err != nil {
			return nil, err
		}
		info := file.FileInfo()
		entry := zipEntryMetadata{
			Name:             file.Name,
			CompressedSize:   compressed,
			UncompressedSize: uncompressed,
			Modified:         file.Modified,
			IsDir:            info.IsDir(),
			Mode:             info.Mode(),
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

func parseZipExtractOptions(dict *MShellDict) (zipExtractOptions, error) {
	options := zipExtractOptions{
		zipWriteOptions: zipWriteOptions{
			overwrite:           false,
			skipExisting:        false,
			preservePermissions: true,
		},
		stripComponents: 0,
		pattern:         "",
	}

	if val, ok, err := boolOption(dict, "overwrite"); err != nil {
		return options, err
	} else if ok {
		options.overwrite = val
	}

	if val, ok, err := boolOption(dict, "skipExisting"); err != nil {
		return options, err
	} else if ok {
		options.skipExisting = val
	}

	if options.overwrite && options.skipExisting {
		return options, fmt.Errorf("zipExtract options 'overwrite' and 'skipExisting' are mutually exclusive")
	}

	if val, ok, err := boolOption(dict, "preservePermissions"); err != nil {
		return options, err
	} else if ok {
		options.preservePermissions = val
	}

	if val, ok, err := intOption(dict, "stripComponents"); err != nil {
		return options, err
	} else if ok {
		if val < 0 {
			return options, fmt.Errorf("zipExtract option 'stripComponents' must be >= 0")
		}
		options.stripComponents = val
	}

	if val, ok, err := stringOption(dict, "pattern"); err != nil {
		return options, err
	} else if ok {
		options.pattern = val
	}

	return options, nil
}

func parseZipExtractEntryOptions(dict *MShellDict) (zipExtractEntryOptions, error) {
	options := zipExtractEntryOptions{
		zipWriteOptions: zipWriteOptions{
			overwrite:           false,
			skipExisting:        false,
			preservePermissions: true,
		},
		mkdirs: true,
	}

	if val, ok, err := boolOption(dict, "overwrite"); err != nil {
		return options, err
	} else if ok {
		options.overwrite = val
	}

	if val, ok, err := boolOption(dict, "skipExisting"); err != nil {
		return options, err
	} else if ok {
		options.skipExisting = val
	}

	if options.overwrite && options.skipExisting {
		return options, fmt.Errorf("zipExtractEntry options 'overwrite' and 'skipExisting' are mutually exclusive")
	}

	if val, ok, err := boolOption(dict, "preservePermissions"); err != nil {
		return options, err
	} else if ok {
		options.preservePermissions = val
	}

	if val, ok, err := boolOption(dict, "mkdirs"); err != nil {
		return options, err
	} else if ok {
		options.mkdirs = val
	}

	return options, nil
}

func boolOption(dict *MShellDict, key string) (bool, bool, error) {
	item, ok := dict.Items[key]
	if !ok {
		return false, false, nil
	}
	boolVal, ok := item.(MShellBool)
	if !ok {
		return false, true, fmt.Errorf("Option '%s' must be a bool, found %s", key, item.TypeName())
	}
	return boolVal.Value, true, nil
}

func intOption(dict *MShellDict, key string) (int, bool, error) {
	item, ok := dict.Items[key]
	if !ok {
		return 0, false, nil
	}
	intVal, ok := item.(MShellInt)
	if !ok {
		return 0, true, fmt.Errorf("Option '%s' must be an int, found %s", key, item.TypeName())
	}
	return intVal.Value, true, nil
}

func stringOption(dict *MShellDict, key string) (string, bool, error) {
	item, ok := dict.Items[key]
	if !ok {
		return "", false, nil
	}
	val, err := item.CastString()
	if err != nil {
		return "", true, fmt.Errorf("Option '%s' must be a string/path, found %s", key, item.TypeName())
	}
	return val, true, nil
}

func extractZipArchive(zipPath, destDir string, options zipExtractOptions) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("Error opening %s: %w", zipPath, err)
	}
	defer reader.Close()

	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("Error resolving destination %s: %w", destDir, err)
	}

	if err := os.MkdirAll(absDest, 0755); err != nil {
		return fmt.Errorf("Error creating destination %s: %w", absDest, err)
	}
	baseWithSep := ensureTrailingSeparator(absDest)

	for _, file := range reader.File {
		if file.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Refusing to extract symlink %s", file.Name)
		}

		entryName := normalizeZipEntryName(file.Name)
		if entryName == "" {
			continue
		}

		if options.pattern != "" {
			match, err := path.Match(options.pattern, entryName)
			if err != nil {
				return fmt.Errorf("Invalid zipExtract pattern '%s': %w", options.pattern, err)
			}
			if !match {
				continue
			}
		}

		stripped, err := stripZipComponents(entryName, options.stripComponents, file.FileInfo().IsDir())
		if err != nil {
			return err
		}
		if stripped == "" {
			continue
		}

		target := filepath.Join(absDest, filepath.FromSlash(stripped))
		target = filepath.Clean(target)
		if err := ensureWithinBase(target, absDest, baseWithSep); err != nil {
			return err
		}

		if err := writeZipFileToDisk(file, target, options.zipWriteOptions, true); err != nil {
			return err
		}
	}

	return nil
}

func extractZipEntry(zipPath, entryPath, destPath string, options zipExtractEntryOptions) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("Error opening %s: %w", zipPath, err)
	}
	defer reader.Close()

	targetName := normalizeZipEntryName(entryPath)
	if targetName == "" {
		return fmt.Errorf("Entry path %s resolves to an empty name", entryPath)
	}

	var fileEntry *zip.File
	dirExists := false

	for _, file := range reader.File {
		name := normalizeZipEntryName(file.Name)
		if name != targetName {
			continue
		}

		if file.FileInfo().IsDir() {
			dirExists = true
			break
		}
		fileEntry = file
		break
	}

	if dirExists {
		return extractZipDirectoryEntries(reader.File, targetName, destPath, options)
	}

	if fileEntry != nil {
		return extractZipFileEntry(fileEntry, destPath, options)
	}

	prefix := targetName + "/"
	for _, file := range reader.File {
		name := normalizeZipEntryName(file.Name)
		if strings.HasPrefix(name, prefix) {
			dirExists = true
			break
		}
	}

	if dirExists {
		return extractZipDirectoryEntries(reader.File, targetName, destPath, options)
	}

	return fmt.Errorf("Entry '%s' not found in %s", entryPath, zipPath)
}

func extractZipDirectoryEntries(files []*zip.File, targetName, destPath string, options zipExtractEntryOptions) error {
	absDest, err := filepath.Abs(destPath)
	if err != nil {
		return fmt.Errorf("Error resolving destination %s: %w", destPath, err)
	}

	if options.mkdirs {
		if err := os.MkdirAll(absDest, 0755); err != nil {
			return fmt.Errorf("Error creating destination %s: %w", absDest, err)
		}
	} else {
		info, err := os.Stat(absDest)
		if err != nil {
			return fmt.Errorf("Destination %s does not exist", absDest)
		}
		if !info.IsDir() {
			return fmt.Errorf("Destination %s is not a directory", absDest)
		}
	}

	baseWithSep := ensureTrailingSeparator(absDest)
	prefix := targetName + "/"

	for _, file := range files {
		if file.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Refusing to extract symlink %s", file.Name)
		}

		name := normalizeZipEntryName(file.Name)
		if name == targetName && file.FileInfo().IsDir() {
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		relative := strings.TrimPrefix(name, prefix)
		if relative == "" {
			continue
		}

		target := filepath.Join(absDest, filepath.FromSlash(relative))
		target = filepath.Clean(target)
		if err := ensureWithinBase(target, absDest, baseWithSep); err != nil {
			return err
		}

		if err := writeZipFileToDisk(file, target, options.zipWriteOptions, true); err != nil {
			return err
		}
	}

	return nil
}

func extractZipFileEntry(file *zip.File, destPath string, options zipExtractEntryOptions) error {
	if file.FileInfo().Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Refusing to extract symlink %s", file.Name)
	}

	absDest, err := filepath.Abs(destPath)
	if err != nil {
		return fmt.Errorf("Error resolving destination %s: %w", destPath, err)
	}

	parent := filepath.Dir(absDest)
	if options.mkdirs {
		if err := os.MkdirAll(parent, 0755); err != nil {
			return fmt.Errorf("Error creating parent directory %s: %w", parent, err)
		}
	} else {
		if _, err := os.Stat(parent); err != nil {
			return fmt.Errorf("Parent directory %s does not exist", parent)
		}
	}

	return writeZipFileToDisk(file, absDest, options.zipWriteOptions, false)
}

func writeZipFileToDisk(file *zip.File, destPath string, options zipWriteOptions, ensureParents bool) error {
	info := file.FileInfo()

	if info.IsDir() {
		if ensureParents {
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return fmt.Errorf("Error creating directory %s: %w", destPath, err)
			}
		} else if err := os.Mkdir(destPath, 0755); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("Error creating directory %s: %w", destPath, err)
		}

		if options.preservePermissions {
			if err := os.Chmod(destPath, info.Mode()); err != nil && !errors.Is(err, os.ErrPermission) {
				return fmt.Errorf("Error setting permissions on %s: %w", destPath, err)
			}
		}
		return nil
	}

	parentDir := filepath.Dir(destPath)
	if ensureParents {
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("Error creating parent directory %s: %w", parentDir, err)
		}
	} else {
		if _, err := os.Stat(parentDir); err != nil {
			return fmt.Errorf("Parent directory %s does not exist", parentDir)
		}
	}

	if options.skipExisting {
		if _, err := os.Stat(destPath); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else {
		if _, err := os.Stat(destPath); err == nil && !options.overwrite {
			return fmt.Errorf("Destination %s already exists", destPath)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	reader, err := file.Open()
	if err != nil {
		return err
	}
	defer reader.Close()

	outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, reader); err != nil {
		return err
	}

	if options.preservePermissions {
		if err := os.Chmod(destPath, info.Mode()); err != nil && !errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("Error setting permissions on %s: %w", destPath, err)
		}
	}
	return nil
}

func readZipEntry(zipPath, entryPath string) ([]byte, bool, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, false, fmt.Errorf("Error opening %s: %w", zipPath, err)
	}
	defer reader.Close()

	target := normalizeZipEntryName(entryPath)
	if target == "" {
		return nil, false, fmt.Errorf("Entry path %s resolves to an empty name", entryPath)
	}

	for _, file := range reader.File {
		name := normalizeZipEntryName(file.Name)
		if name != target {
			continue
		}

		if file.FileInfo().IsDir() {
			return nil, false, fmt.Errorf("zipRead cannot read directory entries (%s)", entryPath)
		}

		rc, err := file.Open()
		if err != nil {
			return nil, false, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, false, err
		}
		return data, true, nil
	}

	return nil, false, nil
}

func normalizeZipEntryName(name string) string {
	cleaned := strings.ReplaceAll(name, "\\", "/")
	for strings.HasPrefix(cleaned, "./") {
		cleaned = strings.TrimPrefix(cleaned, "./")
	}
	cleaned = path.Clean(cleaned)
	cleaned = strings.TrimPrefix(cleaned, "./")
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func stripZipComponents(name string, components int, isDir bool) (string, error) {
	if components == 0 || name == "" {
		return name, nil
	}

	parts := strings.Split(name, "/")
	if len(parts) <= components {
		if isDir {
			return "", nil
		}
		return "", fmt.Errorf("Cannot strip %d components from entry %s", components, name)
	}
	return strings.Join(parts[components:], "/"), nil
}

func ensureWithinBase(candidate, base, baseWithSep string) error {
	candidate = filepath.Clean(candidate)
	if candidate == base {
		return nil
	}
	if !strings.HasPrefix(candidate, baseWithSep) {
		return fmt.Errorf("Refusing to write outside destination directory %s (attempted %s)", base, candidate)
	}
	return nil
}

func ensureTrailingSeparator(path string) string {
	if strings.HasSuffix(path, string(os.PathSeparator)) {
		return path
	}
	return path + string(os.PathSeparator)
}

func EatLinkHeaderWhitespace(linkHeader string, i int) int {
	for i < len(linkHeader) {
		if unicode.IsSpace(rune(linkHeader[i])) {
			i++
		} else {
			break
		}
	}
	return i
}

// https://datatracker.ietf.org/doc/html/rfc8288

// Link       = #link-value
// link-value = "<" URI-Reference ">" *( OWS ";" OWS link-param )
// link-param = token BWS [ "=" BWS ( token / quoted-string ) ]

// https://datatracker.ietf.org/doc/html/rfc7230

// token          = 1*tchar

// tchar          = "!" / "#" / "$" / "%" / "&" / "'" / "*"
// 					 / "+" / "-" / "." / "^" / "_" / "`" / "|" / "~"
// 					/ DIGIT / ALPHA
// 					; any VCHAR, except delimiters

// quoted-string  = DQUOTE *( qdtext / quoted-pair ) DQUOTE
// qdtext         = HTAB / SP / %x21 / %x23-5B / %x5D-7E / obs-text
// obs-text       = %x80-FF
// quoted-pair    = "\" ( HTAB / SP / VCHAR / obs-text )

func IsTChar(char rune) bool {
	return unicode.IsLetter(char) || unicode.IsDigit(char) || char == '!' || char == '#' || char == '$' || char == '%' ||
		char == '&' || char == '\'' || char == '*' || char == '+' || char == '-' ||
		char == '.' || char == '^' || char == '_' || char == '`' || char == '|' ||
		char == '~'
}

func ParseLinkHeaders(linkHeader string) ([]LinkHeader, error) {
	var headers []LinkHeader
	i := 0

	for {
		i = EatLinkHeaderWhitespace(linkHeader, i)
		if i >= len(linkHeader) {
			break
		}

		header, nextIndex, err := ParseLinkHeader(linkHeader, i)
		if err != nil {
			return nil, fmt.Errorf("Error parsing link header at position %d: %s", i, err.Error())
		}
		headers = append(headers, header)
		i = nextIndex

		i = EatLinkHeaderWhitespace(linkHeader, i)
		if i >= len(linkHeader) {
			break
		}

		if linkHeader[i] != ',' {
			return nil, fmt.Errorf("Expected ',' after link header at position %d, found '%c'", i, linkHeader[i])
		}
		i++
	}

	if len(headers) == 0 {
		return nil, fmt.Errorf("Empty link header")
	}

	return headers, nil
}

func ParseLinkHeader(linkHeader string, i int) (LinkHeader, int, error) {
	i = EatLinkHeaderWhitespace(linkHeader, i)
	if i >= len(linkHeader) {
		return LinkHeader{}, i, fmt.Errorf("Empty link header")
	}

	if linkHeader[i] != '<' {
		return LinkHeader{}, i, fmt.Errorf("Link header does not start with '<': %s", linkHeader)
	}
	i++

	uriStart := i
	for i < len(linkHeader) && linkHeader[i] != '>' {
		i++
	}
	if i >= len(linkHeader) {
		return LinkHeader{}, i, fmt.Errorf("Empty link header")
	}
	uri := linkHeader[uriStart:i]
	i++ // Skip '>'

	params := make(map[string]string)
	rel := ""

	for {
		i = EatLinkHeaderWhitespace(linkHeader, i)
		if i >= len(linkHeader) || linkHeader[i] == ',' {
			if len(rel) == 0 {
				return LinkHeader{}, i, fmt.Errorf("Link header does not have a 'rel' parameter: %s", linkHeader)
			}
			return LinkHeader{Uri: uri, Rel: rel, Params: params}, i, nil
		}

		if linkHeader[i] != ';' {
			return LinkHeader{}, i, fmt.Errorf("Expected ';' before link parameter at position %d: %s", i, linkHeader)
		}
		i++

		paramName, paramValue, nextIndex, err := ParseLinkParam(linkHeader, i)
		if err != nil {
			return LinkHeader{}, i, fmt.Errorf("Error parsing link parameter: %s", err.Error())
		}
		i = nextIndex

		if paramName == "" {
			return LinkHeader{}, i, fmt.Errorf("Empty link parameter name")
		}

		if paramValue == "" {
			return LinkHeader{}, i, fmt.Errorf("Empty link parameter value")
		}

		if paramName == "rel" {
			rel = paramValue
		} else {
			params[paramName] = paramValue
		}
	}
}

func ParseLinkParam(linkHeader string, i int) (string, string, int, error) {
	i = EatLinkHeaderWhitespace(linkHeader, i)
	if i >= len(linkHeader) {
		return "", "", i, fmt.Errorf("Empty link parameter")
	}

	paramStart := i
	for i < len(linkHeader) && IsTChar(rune(linkHeader[i])) {
		i++
	}

	if paramStart == i {
		return "", "", i, fmt.Errorf("Empty link parameter name")
	}

	paramName := linkHeader[paramStart:i]

	i = EatLinkHeaderWhitespace(linkHeader, i)
	if i >= len(linkHeader) || linkHeader[i] != '=' {
		return "", "", i, fmt.Errorf("Link parameter '%s' does not have a value", paramName)
	}
	i++

	i = EatLinkHeaderWhitespace(linkHeader, i)
	if i >= len(linkHeader) {
		return "", "", i, fmt.Errorf("Link parameter '%s' does not have a value, expected value after '='", paramName)
	}

	var paramValue strings.Builder

	if linkHeader[i] == '"' {
		i++
		for {
			if i >= len(linkHeader) {
				return "", "", i, fmt.Errorf("Unfinished quoted string for link parameter '%s'", paramName)
			}

			c := linkHeader[i]
			i++

			if c == '"' {
				break
			}

			if c == '\\' {
				if i >= len(linkHeader) {
					return "", "", i, fmt.Errorf("Unfinished escape sequence in quoted string for link parameter '%s'", paramName)
				}
				paramValue.WriteByte(linkHeader[i])
				i++
				continue
			}

			paramValue.WriteByte(c)
		}
	} else {
		valueStart := i
		for i < len(linkHeader) && IsTChar(rune(linkHeader[i])) {
			i++
		}
		if valueStart == i {
			return "", "", i, fmt.Errorf("Link parameter '%s' does not have a value, expected value after '='", paramName)
		}
		paramValue.WriteString(linkHeader[valueStart:i])
	}

	i = EatLinkHeaderWhitespace(linkHeader, i)
	return paramName, paramValue.String(), i, nil
}
