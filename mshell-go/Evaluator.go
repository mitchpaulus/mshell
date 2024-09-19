package main

import (
    "io"
    "fmt"
    "os"
    "os/exec"
)

type MShellStack []MShellObject

func (objList *MShellStack) Peek() (MShellObject, error) {
    if len(*objList) == 0 {
        return nil, fmt.Errorf("Empty stack")
    }
    return (*objList)[len(*objList) - 1], nil
}

func (objList *MShellStack) Pop() (MShellObject, error) {
    if len(*objList) == 0 {
        return nil, fmt.Errorf("Empty stack")
    }
    popped := (*objList)[len(*objList) - 1]
    *objList = (*objList)[:len(*objList) - 1]
    return popped, nil
}

func (objList *MShellStack) Push(obj MShellObject) {
    *objList = append(*objList, obj)
}

func (objList *MShellStack) String() {
    fmt.Fprintf(os.Stderr, "Stack contents:\n")
    for i, obj := range *objList {
        fmt.Fprintf(os.Stderr, "%d: %s\n", i, obj.DebugString())
    }
    fmt.Fprintf(os.Stderr, "End of stack contents\n")
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

func (state EvalState) Evaluate(tokens []Token, stack *MShellStack, context ExecuteContext) EvalResult { 
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
            leftSquareBracketStack = append(leftSquareBracketStack, index - 1)

            for {
                currentToken := tokens[index]
                index++

                if currentToken.Type == EOF {
                    return FailWithMessage(fmt.Sprintf("%d:%d: Found unbalanced bracket.\n", currentToken.Line, currentToken.Column))
                }

                if currentToken.Type == LEFT_SQUARE_BRACKET {
                    leftSquareBracketStack = append(leftSquareBracketStack, index - 1)
                } else if currentToken.Type == RIGHT_SQUARE_BRACKET {
                    if len(leftSquareBracketStack) > 0 {
                        leftIndex := leftSquareBracketStack[len(leftSquareBracketStack) - 1]
                        leftSquareBracketStack = leftSquareBracketStack[:len(leftSquareBracketStack) - 1]

                        if len(leftSquareBracketStack) == 0 {
                            var listStack MShellStack
                            listStack = []MShellObject{}
                            tokensWithinList := tokens[leftIndex + 1:index - 1]

                            result := state.Evaluate(tokensWithinList, &listStack, context)
                            if !result.Success {
                                return result
                            }

                            if result.BreakNum > 0 {
                                return FailWithMessage("Encountered break within list.\n")
                            }

                            l := &MShellList { listStack, "", "" }
                            stack.Push(l)

                            break
                        }

                    } else {
                        return FailWithMessage(fmt.Sprintf("%d:%d: Found unbalanced square bracket.\n", currentToken.Line, currentToken.Column))
                    }
                } 
            }
        } else if t.Type == EXECUTE || t.Type == QUESTION {
            top, err := stack.Pop()
            if err != nil {
                return FailWithMessage(fmt.Sprintf("%d:%d: Cannot execute an empty stack.\n", t.Line, t.Column))
            }

            // Switch on type
            var result EvalResult
            var exitCode int

            switch top.(type) {
            case *MShellList:
                result, exitCode = RunProcess(*top.(*MShellList), context)
            default:
                return FailWithMessage(fmt.Sprintf("%d:%d: Cannot execute a non-list object.\n", t.Line, t.Column))
            }

            if !result.Success {
                return result
            }

            // Push the exit code onto the stack if a question was used to execute
            if t.Type == QUESTION {
                stack.Push(&MShellInt { exitCode })
            }
        } else if t.Type == TRUE {
            stack.Push(&MShellBool { true })
        } else if t.Type == FALSE {
            stack.Push(&MShellBool { false })
        } else if t.Type == STRING {
            parsedString, err := ParseRawString(t.Lexeme)
            if err != nil {
                return FailWithMessage(fmt.Sprintf("%d:%d: Error parsing string: %s\n", t.Line, t.Column, err.Error()))
            }
            stack.Push(&MShellString { parsedString })
        } else {
            return FailWithMessage(fmt.Sprintf("%d:%d: We haven't implemented the token type '%s' yet.\n", t.Line, t.Column, t.Type))
        }
    }

    return EvalResult { true, -1 }
}


func RunProcess(list MShellList, context ExecuteContext) (EvalResult, int) {
    // Check for empty list
    if len(list.Items) == 0 {
        return FailWithMessage("Cannot execute an empty list.\n"), -1
    }

    // Check that all list items are commandlineable
    for i, item := range list.Items {
        if !item.IsCommandLineable() {
            return FailWithMessage(fmt.Sprintf("Item %d (%s) cannot be used as a command line argument.\n", i, item.DebugString())), -1
        }
    }

    commandLineArguments := make([]string, len(list.Items))
    for i, item := range list.Items {
        commandLineArguments[i] = item.CommandLine()
    }


    cmd := exec.Command(commandLineArguments[0], commandLineArguments[1:]...)

    if list.StandardOutputFile != "" {
        // Open the file for writing
        file, err := os.Create(list.StandardOutputFile)
        if err != nil {
            return FailWithMessage(fmt.Sprintf("Error opening file %s for writing: %s\n", list.StandardOutputFile, err.Error())), -1
        }
        cmd.Stdout = file
        defer file.Close()
    } else if context.StandardOutput != nil {
        cmd.Stdout = context.StandardOutput
    } else {
        // Default to stdout of this process itself
        cmd.Stdout = os.Stdout
    }

    if list.StandardInputFile != "" {
        // Open the file for reading
        file, err := os.Open(list.StandardInputFile)
        if err != nil {
            return FailWithMessage(fmt.Sprintf("Error opening file %s for reading: %s\n", list.StandardInputFile, err.Error())), -1
        }
        cmd.Stdin = file
        defer file.Close()
    } else if context.StandardInput != nil {
        cmd.Stdin = context.StandardInput
    } else {
        // Default to stdin of this process itself
        cmd.Stdin = os.Stdin
    }

    // No redirection for stderr currently, just use the stderr of this process
    cmd.Stderr = os.Stderr

    err := cmd.Run()
    if err != nil {
        return FailWithMessage(fmt.Sprintf("Error running command: %s\n", err.Error())), -1
    }

    exitCode := cmd.ProcessState.ExitCode()

    return SimpleSuccess(), exitCode
}
