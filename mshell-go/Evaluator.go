package main

import (
    "io"
    "fmt"
    "os"
)

type MShellStack []MShellObject

func (objList MShellStack) Peek() (MShellObject, error) {
    if len(objList) == 0 {
        return nil, fmt.Errorf("Empty stack")
    }
    return objList[len(objList) - 1], nil
}

func (objList MShellStack) Pop() (MShellObject, error) {
    if len(objList) == 0 {
        return nil, fmt.Errorf("Empty stack")
    }
    popped := objList[len(objList) - 1]
    objList = objList[:len(objList) - 1]
    return popped, nil
}

func (objList MShellStack) Push(obj MShellObject) {
    objList = append(objList, obj)
}

type EvalState  struct {
    PositionalArgs[] string
    LoopDepth int32

    // private Dictionary<string, MShellObject> _variables = new();
    Variables map[string]MShellObject
}

type EvalResult struct {
    Success bool
    BreakNum int32
}

type ExecuteContext struct {
    StandardInput io.Reader
    StandardOutput io.Writer
}

func SimpleSuccess() EvalResult {
    return EvalResult { true, -1 }
}

func FailWithMessage(message string) EvalResult {
    // Log message to stderr
    fmt.Fprintf(os.Stderr, message)
    return EvalResult { false, -1 }
}

func (state EvalState) Evaluate(tokens []Token, stack MShellStack, context ExecuteContext) EvalResult { 
    index := 0

    // Need a stack of integers
    // quotationStack := []int32{}
    leftSquareBracketStack := []int{}

    for index < len(tokens) {
        t := tokens[index]
        index++

        if t.Type == EOF {
            return SimpleSuccess()
        } else if t.Type == LITERAL {
            stack.Push(&MShellLiteral { t.Lexeme })
        } else if t.Type == LEFT_SQUARE_BRACKET {
            leftSquareBracketStack = append(leftSquareBracketStack, index)

            for {
                currentToken := tokens[index]
                index++

                if index >= len(tokens) || tokens[index].Type == EOF {
                    return FailWithMessage(fmt.Sprintf("%d:%d: Found unbalanced bracket.\n", currentToken.Line, currentToken.Column))
                }

                if tokens[index].Type == LEFT_SQUARE_BRACKET {
                    leftSquareBracketStack = append(leftSquareBracketStack, index)
                } else if tokens[index].Type == RIGHT_SQUARE_BRACKET {
                    if len(leftSquareBracketStack) > 0 {
                        leftIndex := leftSquareBracketStack[len(leftSquareBracketStack)-1]

                        if len(leftSquareBracketStack) == 0 {
                            listStack := []MShellObject{}
                            tokensWithinList := tokens[leftIndex + 1:index - leftIndex - 1]
                            result := state.Evaluate(tokensWithinList, listStack, context)
                            if !result.Success {
                                return result
                            }

                            if result.BreakNum > 0 {
                                return FailWithMessage("Encountered break within list.\n")
                            }

                            l := &MShellList { listStack, nil, nil }
                            stack.Push(l)
                            break
                        }

                    } else {
                        return FailWithMessage(fmt.Sprintf("%d:%d: Found unbalanced square bracket.\n", currentToken.Line, currentToken.Column))
                    }
                } 
            }
        } else {
            return FailWithMessage(fmt.Sprintf("%d:%d: We haven't implemented the token type '%s' yet.\n", t.Line, t.Column, t.Type))
        }
    }

    return EvalResult { true, -1 }
}
