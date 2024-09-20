package main

import (
    "io"
    "fmt"
    "os"
    "os/exec"
    "strconv"
    "strings"
    "sync"
    "bufio"
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

func (objList *MShellStack) String() string {
    var builder strings.Builder
    builder.WriteString("Stack contents:\n")
    for i, obj := range *objList {
        builder.WriteString(fmt.Sprintf("%d: %s\n", i, obj.DebugString()))
    }
    builder.WriteString("End of stack contents\n")
    return builder.String()
}

type EvalState  struct {
    PositionalArgs[] string
    LoopDepth int

    // private Dictionary<string, MShellObject> _variables = new();
    Variables map[string]MShellObject
}

type EvalResult struct {
    Success bool
    BreakNum int
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

func (state *EvalState) Evaluate(tokens []Token, stack *MShellStack, context ExecuteContext) EvalResult { 
    index := 0

    // Need a stack of integers
    quotationStack := []int{}
    leftSquareBracketStack := []int{}

    for index < len(tokens) {
        t := tokens[index]
        index++

        if t.Type == EOF {
            return SimpleSuccess()
        } else if t.Type == LITERAL {

            if t.Lexeme == ".s" {
                // Print current stack
                fmt.Fprintf(os.Stderr, stack.String())
            } else if t.Lexeme == "dup" {
                top, err := stack.Peek()
                if err != nil {
                    return FailWithMessage(fmt.Sprintf("%d:%d: Cannot duplicate an empty stack.\n", t.Line, t.Column))
                }
                stack.Push(top)
            } else if t.Lexeme == "drop" {
                _, err := stack.Pop()
                if err != nil {
                    return FailWithMessage(fmt.Sprintf("%d:%d: Cannot drop an empty stack.\n", t.Line, t.Column))
                }
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
            } else {
                stack.Push(&MShellLiteral { t.Lexeme })
            }
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
        } else if t.Type == LEFT_PAREN {
            quotationStack = append(quotationStack, index - 1)

            for {
                currentToken := tokens[index]
                index++

                if currentToken.Type == EOF {
                    return FailWithMessage(fmt.Sprintf("%d:%d: Found unbalanced parenthesis.\n", currentToken.Line, currentToken.Column))
                }

                if currentToken.Type == LEFT_PAREN {
                    quotationStack = append(quotationStack, index - 1)
                } else if currentToken.Type == RIGHT_PAREN {
                    if len(quotationStack) > 0 {
                        leftIndex := quotationStack[len(quotationStack) - 1]
                        quotationStack = quotationStack[:len(quotationStack) - 1]

                        if len(quotationStack) == 0 {
                            tokensWithinQuotation := tokens[leftIndex + 1:index - 1]
                            q := &MShellQuotation { tokensWithinQuotation, "", "" }
                            stack.Push(q)

                            break
                        }

                    } else {
                        return FailWithMessage(fmt.Sprintf("%d:%d: Found unbalanced parenthesis.\n", currentToken.Line, currentToken.Column))
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
            case *MShellPipe:
                result, exitCode = state.RunPipeline(*top.(*MShellPipe), context, stack)
            default:
                return FailWithMessage(fmt.Sprintf("%d:%d: Cannot execute a non-list object. Found %s %s\n", t.Line, t.Column, top.TypeName(), top.DebugString()))
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
        } else if t.Type == INTEGER {
            intVal, err := strconv.Atoi(t.Lexeme)
            if err != nil {
                return FailWithMessage(fmt.Sprintf("%d:%d: Error parsing integer: %s\n", t.Line, t.Column, err.Error()))
            }

            stack.Push(&MShellInt { intVal})
        } else if t.Type == STRING {
            parsedString, err := ParseRawString(t.Lexeme)
            if err != nil {
                return FailWithMessage(fmt.Sprintf("%d:%d: Error parsing string: %s\n", t.Line, t.Column, err.Error()))
            }
            stack.Push(&MShellString { parsedString })
        } else if t.Type == IF {
            obj, err := stack.Pop()
            if err != nil {
                return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do an 'if' on an empty stack.\n", t.Line, t.Column))
            }

            list, ok := obj.(*MShellList)
            if !ok {
                return FailWithMessage(fmt.Sprintf("%d:%d: Argument for if expected to be a list of quoations, received a %s\n", t.Line, t.Column, obj.TypeName()))
            }

            if len(list.Items) < 2 {
                return FailWithMessage(fmt.Sprintf("%d:%d: If statement requires at least two arguments. Found %d.\n", t.Line, t.Column, len(list.Items)))
            }

            // Check that all items are quotations
            for i, item := range list.Items {
                if _, ok := item.(*MShellQuotation); !ok {
                    return FailWithMessage(fmt.Sprintf("%d:%d: Item %d in if statement is not a quotation.\n", t.Line, t.Column, i))
                }
            }

            trueIndex := -1

            ListLoop:
            for i := 0; i < len(list.Items) - 1; i += 2 {
                quotation := list.Items[i].(*MShellQuotation)
                result := state.Evaluate(quotation.Tokens, stack, context)

                if !result.Success {
                    return result
                }

                if result.BreakNum > 0 {
                    return FailWithMessage("Encountered break within if statement.\n")
                }

                top, err := stack.Pop()
                if err != nil {
                    conditionNum := i / 2 + 1
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
                    return FailWithMessage(fmt.Sprintf("%d:%d: Expected an integer or boolean for condition #%d, received a %s.\n", t.Line, t.Column, i / 2 + 1, top.TypeName()))
                }
            }

            if trueIndex > -1 {
                quotation := list.Items[trueIndex + 1].(*MShellQuotation)
                result := state.Evaluate(quotation.Tokens, stack, context)

                // If we encounter a break, we should return it up the stack
                if !result.Success || result.BreakNum != -1 {
                    return result
                }
            } else if len(list.Items) % 2 == 1 { // Try to find a final else statement, will be the last item in the list if odd number of items
                quotation := list.Items[len(list.Items) - 1].(*MShellQuotation)
                result := state.Evaluate(quotation.Tokens, stack, context)

                if !result.Success || result.BreakNum != -1 {
                    return result
                }
            }
        } else if t.Type == PLUS {
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
                    stack.Push(&MShellInt { obj2.(*MShellInt).Value + obj1.(*MShellInt).Value })
                default:
                    return FailWithMessage(fmt.Sprintf("%d:%d: Cannot add an integer to a %s.\n", t.Line, t.Column, obj2.TypeName()))
                }
            case *MShellString:
                switch obj2.(type) {
                case *MShellString:
                    stack.Push(&MShellString { obj2.(*MShellString).Content + obj1.(*MShellString).Content })
                case *MShellLiteral:
                    stack.Push(&MShellString { obj2.(*MShellLiteral).LiteralText + obj1.(*MShellString).Content })
                default:
                    return FailWithMessage(fmt.Sprintf("%d:%d: Cannot add a string to a %s.\n", t.Line, t.Column, obj2.TypeName()))
                }
            case *MShellLiteral:
                switch obj2.(type) {
                case *MShellString:
                    stack.Push(&MShellString { obj2.(*MShellString).Content + obj1.(*MShellLiteral).LiteralText })
                case *MShellLiteral:
                    stack.Push(&MShellString { obj2.(*MShellLiteral).LiteralText + obj1.(*MShellLiteral).LiteralText })
                default:
                    return FailWithMessage(fmt.Sprintf("%d:%d: Cannot add a literal to a %s.\n", t.Line, t.Column, obj2.TypeName()))
                }
            default:
                return FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '+' to a %s to a %s.\n", t.Line, t.Column, obj2.TypeName(), obj1.TypeName()))
            }
        } else if t.Type == MINUS {
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
                    stack.Push(&MShellInt { obj2.(*MShellInt).Value - obj1.(*MShellInt).Value })
                default:
                    return FailWithMessage(fmt.Sprintf("%d:%d: Cannot subtract an integer from a %s.\n", t.Line, t.Column, obj2.TypeName()))
                }
            default:
                return FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '-' to a %s and %s.\n", t.Line, t.Column, obj2.TypeName(), obj1.TypeName()))
            }
        } else if t.Type == AND || t.Type == OR {
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
                        stack.Push(&MShellBool { obj2.(*MShellBool).Value && obj1.(*MShellBool).Value })
                    } else {
                        stack.Push(&MShellBool { obj2.(*MShellBool).Value || obj1.(*MShellBool).Value })
                    }
                default:
                    return FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '%s' to a %s and %s.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName(), obj1.TypeName()))
                }
            default:
                return FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '%s' to a %s and %s.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName(), obj1.TypeName()))
            }
        } else if t.Type == NOT {
            obj, err := stack.Pop()
            if err != nil {
                return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
            }

            switch obj.(type) {
            case *MShellBool:
                stack.Push(&MShellBool { !obj.(*MShellBool).Value })
            default:
                return FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '%s' to a %s.\n", t.Line, t.Column, t.Lexeme, obj.TypeName()))
            }
        } else if t.Type == GREATERTHANOREQUAL || t.Type == LESSTHANOREQUAL {
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
                    stack.Push(&MShellBool { obj2.FloatNumeric() >= obj1.FloatNumeric() })
                } else {
                    stack.Push(&MShellBool { obj2.FloatNumeric() <= obj1.FloatNumeric() })
                }
            } else {
                return FailWithMessage(fmt.Sprintf("%d:%d: Cannot apply '%s' to a %s and a %s.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName(), obj1.TypeName()))
            }
        } else if t.Type == GREATERTHAN || t.Type == LESSTHAN {
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
                    stack.Push(&MShellBool { obj2.FloatNumeric() > obj1.FloatNumeric() })
                } else {
                    stack.Push(&MShellBool { obj2.FloatNumeric() < obj1.FloatNumeric() })
                }
            } else {
                switch obj1.(type) {
                case *MShellString:
                    switch obj2.(type) {
                    case *MShellList:
                        if t.Type == GREATERTHAN {
                            obj2.(*MShellList).StandardOutputFile = obj1.(*MShellString).Content
                        } else {
                            obj2.(*MShellList).StandardInputFile = obj1.(*MShellString).Content
                        }
                        stack.Push(obj2)
                    case *MShellQuotation:
                        if t.Type == GREATERTHAN {
                            obj2.(*MShellQuotation).StandardOutputFile = obj1.(*MShellString).Content
                        } else {
                            obj2.(*MShellQuotation).StandardInputFile = obj1.(*MShellString).Content
                        }
                        stack.Push(obj2)
                    default:
                        return FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a string to a %s.\n", t.Line, t.Column, obj2.TypeName()))
                    }
                default:
                    return FailWithMessage(fmt.Sprintf("%d:%d: Cannot redirect a %s to a %s.\n", t.Line, t.Column, obj1.TypeName(), obj2.TypeName()))
                }
            }
        } else if t.Type == VARSTORE {
            obj, err := stack.Pop()
            if err != nil {
                return FailWithMessage(fmt.Sprintf("%d:%d: Nothing on stack to store into variable %s.\n", t.Line, t.Column, t.Lexeme[1:]))
            }

            state.Variables[t.Lexeme[1:]] = obj
        } else if t.Type == VARRETRIEVE {
            name := t.Lexeme[:len(t.Lexeme) - 1]
            obj, ok := state.Variables[name]
            if !ok {
                var message strings.Builder
                message.WriteString(fmt.Sprintf("%d:%d: Variable %s not found.\n", t.Line, t.Column, name))
                message.WriteString("Variables:\n")
                for key, _ := range state.Variables {
                    message.WriteString(fmt.Sprintf("  %s\n", key))
                }
                return FailWithMessage(message.String())
            }

            stack.Push(obj)
        } else if t.Type == LOOP {
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

            context := ExecuteContext {
                StandardInput: nil,
                StandardOutput: nil,
            }

            if quotation.StandardInputFile != "" {
                file, err := os.Open(quotation.StandardInputFile)
                if err != nil {
                    return FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s for reading: %s\n", t.Line, t.Column, quotation.StandardInputFile, err.Error()))
                }
                context.StandardInput = file
                defer file.Close()
            }

            if quotation.StandardOutputFile != "" {
                file, err := os.Create(quotation.StandardOutputFile)
                if err != nil {
                    return FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s for writing: %s\n", t.Line, t.Column, quotation.StandardOutputFile, err.Error()))
                }
                context.StandardOutput = file
                defer file.Close()
            }

            maxLoops := 15000
            loopCount := 0
            state.LoopDepth++

            breakDiff := 0 

            for loopCount < maxLoops {
                result := state.Evaluate(quotation.Tokens, stack, context)

                if !result.Success {
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
                return FailWithMessage(fmt.Sprintf("%d:%d: Loop exceeded maximum number of iterations.\n", t.Line, t.Column))
            }

            state.LoopDepth--

            if breakDiff > 0 {
                return EvalResult { true, breakDiff - 1 }
            }
        } else if t.Type == BREAK {
            return EvalResult { true, 1 }
        } else if t.Type == EQUALS {
            obj1, err := stack.Pop()
            if err != nil {
                return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '=' operation on an empty stack.\n", t.Line, t.Column))
            }
            obj2, err := stack.Pop()
            if err != nil {
                return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '=' operation on a stack with only one item.\n", t.Line, t.Column))
            }

            // Implement for integers right now.
            switch obj1.(type) {
            case *MShellInt:
                switch obj2.(type) {
                case *MShellInt:
                    stack.Push(&MShellBool { obj2.(*MShellInt).Value == obj1.(*MShellInt).Value })
                default:
                    return FailWithMessage(fmt.Sprintf("%d:%d: Cannot compare an integer to a %s.\n", t.Line, t.Column, obj2.TypeName()))
                }
            default:
                return FailWithMessage(fmt.Sprintf("%d:%d: Cannot complete %s with a %s to a %s.\n", t.Line, t.Column, t.Lexeme, obj2.TypeName(), obj1.TypeName()))
            }
        } else if t.Type == INTERPRET {
            obj, err := stack.Pop()
            if err != nil {
                return FailWithMessage(fmt.Sprintf("%d:%d: Cannot interpret an empty stack.\n", t.Line, t.Column))
            }

            quotation, ok := obj.(*MShellQuotation)
            if !ok {
                return FailWithMessage(fmt.Sprintf("%d:%d: Argument for interpret expected to be a quotation, received a %s\n", t.Line, t.Column, obj.TypeName()))
            }

            quoteContext := ExecuteContext {
                StandardInput: nil,
                StandardOutput: nil,
            }

            if quotation.StandardInputFile != "" {
                file, err := os.Open(quotation.StandardInputFile)
                if err != nil {
                    return FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s for reading: %s\n", t.Line, t.Column, quotation.StandardInputFile, err.Error()))
                }
                quoteContext.StandardInput = file
                defer file.Close()
            } else if context.StandardInput != nil {
                quoteContext.StandardInput = context.StandardInput
            } else {
                // Default to stdin of this process itself
                quoteContext.StandardInput = os.Stdin
            }

            if quotation.StandardOutputFile != "" {
                file, err := os.Create(quotation.StandardOutputFile)
                if err != nil {
                    return FailWithMessage(fmt.Sprintf("%d:%d: Error opening file %s for writing: %s\n", t.Line, t.Column, quotation.StandardOutputFile, err.Error()))
                }
                quoteContext.StandardOutput = file
                defer file.Close()
            } else if context.StandardOutput != nil {
                quoteContext.StandardOutput = context.StandardOutput
            } else {
                // Default to stdout of this process itself
                quoteContext.StandardOutput = os.Stdout
            }

            result := state.Evaluate(quotation.Tokens, stack, quoteContext)
            if !result.Success {
                return result
            }

            if result.BreakNum > 0 {
                return result
            }
        } else if t.Type == POSITIONAL {
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

            stack.Push(&MShellString { state.PositionalArgs[posIndex - 1] })
        } else if t.Type == PIPE {
            obj1, err := stack.Pop()
            if err != nil {
                return FailWithMessage(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, t.Lexeme))
            }

            // obj1 should be a list
            list, ok := obj1.(*MShellList)
            if !ok {
                return FailWithMessage(fmt.Sprintf("%d:%d: Cannot pipe a %s.\n", t.Line, t.Column, obj1.TypeName()))
            }

            stack.Push( &MShellPipe { *list } )
        } else if t.Type == READ {
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
                        stack.Push(&MShellString { "" })
                        stack.Push(&MShellBool { false })
                    } else {
                        return FailWithMessage(fmt.Sprintf("%d:%d: Error reading from stdin: %s\n", t.Line, t.Column, err.Error()))
                    }
                } else {
                    // Check if the last character is a '\r' and remove it if it is. Also remove the '\n' itself
                    if len(line) > 0 && line[len(line) - 1] == '\n' {
                        line = line[:len(line) - 1]
                    }
                    if len(line) > 0 && line[len(line) - 1] == '\r' {
                        line = line[:len(line) - 1]
                    }

                    stack.Push(&MShellString { line })
                    stack.Push(&MShellBool { true })
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
                                stack.Push(&MShellString { "" })
                                stack.Push(&MShellBool { false })
                            } else {
                                // Else, we have a final that wasn't terminated by a newline. Still try to remove '\r' if it's there
                                builderStr := line.String()
                                if len(builderStr) > 0 && builderStr[len(builderStr) - 1] == '\r' {
                                    builderStr = builderStr[:len(builderStr) - 1]
                                }
                                stack.Push(&MShellString { builderStr })
                                stack.Push(&MShellBool { true })
                            }
                            break
                        } else {
                            return FailWithMessage(fmt.Sprintf("%d:%d: Error reading from stdin: %s\n", t.Line, t.Column, err.Error()))
                        }
                    }

                    if b[0] == '\n' {
                        builderStr := line.String()

                        // Check if the last character is a '\r' and remove it if it is
                        if len(builderStr) > 0 && builderStr[len(builderStr) - 1] == '\r' {
                            builderStr = builderStr[:len(builderStr) - 1]
                        }

                        stack.Push(&MShellString { builderStr })
                        stack.Push(&MShellBool { true })
                        break
                    } else {
                        line.WriteByte(b[0])
                    }
                }
            }
        } else if t.Type == STR {
            obj, err := stack.Pop()
            if err != nil {
                return FailWithMessage(fmt.Sprintf("%d:%d: Cannot convert an empty stack to a string.\n", t.Line, t.Column))
            }

            stack.Push(&MShellString { obj.DebugString() })
        } else {
            return FailWithMessage(fmt.Sprintf("%d:%d: We haven't implemented the token type '%s' yet.\n", t.Line, t.Column, t.Type))
        }
    } 

    return EvalResult { true, -1 }
}

type Executable interface {
    Execute(state *EvalState, context ExecuteContext, stack *MShellStack) (EvalResult, int) 
}


func (list *MShellList) Execute(state *EvalState, context ExecuteContext, stack *MShellStack) (EvalResult, int) {
    return RunProcess(*list, context)
}

func (quotation *MShellQuotation) Execute(state *EvalState, context ExecuteContext, stack *MShellStack) (EvalResult, int) {
    quotationContext := ExecuteContext {
        StandardInput: nil,
        StandardOutput: nil,
    }

    if quotation.StandardInputFile != "" {
        file, err := os.Open(quotation.StandardInputFile)
        if err != nil {
            return FailWithMessage(fmt.Sprintf("Error opening file %s for reading: %s\n", quotation.StandardInputFile, err.Error())), 1
        }
        quotationContext.StandardInput = file
        defer file.Close()
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

    result := state.Evaluate(quotation.Tokens, stack, quotationContext)
    if !result.Success {
        return result, 1
    } else {
        return SimpleSuccess(), 0
    }
}


func RunProcess(list MShellList, context ExecuteContext) (EvalResult, int) {
    // Check for empty list
    if len(list.Items) == 0 {
        return FailWithMessage("Cannot execute an empty list.\n"), 1
    }

    // Check that all list items are commandlineable
    for i, item := range list.Items {
        if !item.IsCommandLineable() {
            return FailWithMessage(fmt.Sprintf("Item %d (%s) cannot be used as a command line argument.\n", i, item.DebugString())), 1
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
            return FailWithMessage(fmt.Sprintf("Error opening file %s for writing: %s\n", list.StandardOutputFile, err.Error())), 1
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
            return FailWithMessage(fmt.Sprintf("Error opening file %s for reading: %s\n", list.StandardInputFile, err.Error())), 1
        }
        cmd.Stdin = file
        defer file.Close()
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

    // No redirection for stderr currently, just use the stderr of this process
    cmd.Stderr = os.Stderr

    // fmt.Fprintf(os.Stderr, "Running command: %s\n", cmd.String())
    cmd.Run() // Manually deal with the exit code upstream
    // fmt.Fprintf(os.Stderr, "Command finished\n")
    exitCode := cmd.ProcessState.ExitCode()

    return SimpleSuccess(), exitCode
}

func (state *EvalState) RunPipeline(MShellPipe MShellPipe, context ExecuteContext, stack *MShellStack) (EvalResult, int) {
    if len(MShellPipe.List.Items) == 0 {
        return FailWithMessage("Cannot execute an empty pipe.\n"), 1
    }

    // Check that all list items are Executables
    for i, item := range MShellPipe.List.Items {
        if _, ok := item.(Executable); !ok {
            return FailWithMessage(fmt.Sprintf("Item %d (%s) in pipe is not a list or a quotation.\n", i, item.DebugString())), 1
        }
    }

    if len(MShellPipe.List.Items) == 1 {
        // Just run the Execute on the first item
        asExecutable, _ := MShellPipe.List.Items[0].(Executable)
        return asExecutable.Execute(state, context, stack)
    }

    // Have at least 2 items here, create pipeline of Executables, set up list of contexts
    contexts := make([]ExecuteContext, len(MShellPipe.List.Items))

    pipeReaders := make([]io.Reader, len(MShellPipe.List.Items) - 1)
    pipeWriters := make([]io.Writer, len(MShellPipe.List.Items) - 1)

    // Set up pipes
    for i := 0; i < len(MShellPipe.List.Items) - 1; i++ {
        pipeReader, pipeWriter, err := os.Pipe()
        if err != nil {
            return FailWithMessage(fmt.Sprintf("Error creating pipe: %s\n", err.Error())), 1
        }
        pipeReaders[i] = pipeReader
        pipeWriters[i] = pipeWriter
    }

    for i := 0; i < len(MShellPipe.List.Items); i++ {
        newContext := ExecuteContext {
            StandardInput: nil,
            StandardOutput: nil,
        }

        if i == 0 {
            // Stdin should use the context of this function
            newContext.StandardInput = context.StandardInput
            newContext.StandardOutput = pipeWriters[0]
        } else if  i == len(MShellPipe.List.Items) - 1 {
            // Stdout should use the context of this function
            newContext.StandardInput = pipeReaders[len(pipeReaders) - 1]
            newContext.StandardOutput = context.StandardOutput
        } else {
            newContext.StandardInput = pipeReaders[i - 1]
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
            results[i], exitCodes[i] = item.Execute(state, contexts[i], stack)

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
            return result, exitCodes[i]
        }
    }

    // Return the exit code of the last item
    return SimpleSuccess(), exitCodes[len(exitCodes) - 1]
}
