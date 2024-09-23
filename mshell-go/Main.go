package main

import (
    "os"
    "fmt"
    "io"
)

func main() {

    printLex := false
    i := 1

    input := ""
    inputSet := false
    positionalArgs := []string{}

    for i < len(os.Args) {
        arg := os.Args[i]
        i++
        if arg == "--lex" {
            printLex = true
        } else if arg == "-h" || arg == "--help" {
            fmt.Println("Usage: mshell [options] INPUT")
            fmt.Println("Usage: mshell [options] < INPUT")
            fmt.Println("Options:")
            fmt.Println("  --lex      Print the tokens of the input")
            fmt.Println("  -h, --help Print this help message")
            os.Exit(0)
            return
        } else if input != "" {
            positionalArgs = append(positionalArgs, arg)
        } else {
            inputSet = true
            inputBytes, err := os.ReadFile(arg)
            if err != nil {
                fmt.Println(err)
                os.Exit(1)
                return
            }
            input = string(inputBytes)
        }
    }

    if !inputSet {
        inputBytes, err := io.ReadAll(os.Stdin)
        if err != nil {
            fmt.Println(err)
            // Set exit code to 1
            os.Exit(1)
            return
        }
        input = string(inputBytes)
    }

    l := NewLexer(input)
    tokens := l.Tokenize()

    if printLex {
        fmt.Println("Tokens:")
        for _, t := range tokens {
            //                 Console.Write($"{t.Line}:{t.Column}:{t.TokenType} {t.RawText}\n");
            fmt.Printf("%d:%d:%s %s\n", t.Line, t.Column, t.Type, t.Lexeme)
        }
        return
    }

    state := EvalState {
        PositionalArgs: positionalArgs,
        LoopDepth: 0,
        Variables: make(map[string]MShellObject),
        ExportedVariables: make(map[string]struct{}),
    }

    var stack MShellStack
    stack = []MShellObject{}
    context := ExecuteContext {
        StandardInput: os.Stdin,
        StandardOutput: os.Stdout,
    }

    result := state.Evaluate(tokens, &stack, context)

    if !result.Success {
        os.Exit(1)
    } 
}
