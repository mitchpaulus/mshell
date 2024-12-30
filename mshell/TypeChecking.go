package main

import (
	"fmt"
	"strings"
)

var builtInDefs = map[string][]TypeDefinition {
	"str": {
		{
			InputTypes: []MShellType { TypeInt{} },
			OutputTypes: []MShellType { TypeString{} },
		},
		{
			InputTypes: []MShellType { TypeFloat{} },
			OutputTypes: []MShellType { TypeString{} },
		},
		{
			InputTypes: []MShellType { TypeString{} },
			OutputTypes: []MShellType { TypeString{} },
		},
		{
			InputTypes: []MShellType { TypeBool{} },
			OutputTypes: []MShellType { TypeString{} },
		},
		{
			InputTypes: []MShellType { &TypeList{} },
			OutputTypes: []MShellType { TypeString{} },
		},
		{
			InputTypes: []MShellType { &TypeTuple{} },
			OutputTypes: []MShellType { TypeString{} },
		},
		{
			InputTypes: []MShellType { &TypeQuote{} },
			OutputTypes: []MShellType { TypeString{} },
		},
	},
}

var typeDefAdd = []TypeDefinition {
	{
		InputTypes: []MShellType{TypeInt{}, TypeInt{}},
		OutputTypes: []MShellType{TypeInt{}},
	},
	{
		InputTypes: []MShellType{TypeString{}, TypeString{}},
		OutputTypes: []MShellType{TypeString{}},
	},
	{
		InputTypes: []MShellType{TypeFloat{}, TypeFloat{}},
		OutputTypes: []MShellType{TypeFloat{}},
	},
}

type TypeCheckError struct {
	Token Token
	Message string
}

func (err TypeCheckError) String() string {
	return fmt.Sprintf("%d:%d: %s", err.Token.Line, err.Token.Column, err.Message)
}

type TypeCheckResult struct {
	Errors []TypeCheckError
	InputTypes []MShellType
	OutputTypes []MShellType
}

type MShellTypeStack []MShellType

func (objList *MShellTypeStack) Peek() (MShellType, error) {
	if len(*objList) == 0 {
		return nil, fmt.Errorf("Empty stack")
	}
	return (*objList)[len(*objList)-1], nil
}

func (objList *MShellTypeStack) Pop() (MShellType, error) {
	if len(*objList) == 0 {
		return nil, fmt.Errorf("Empty stack")
	}
	popped := (*objList)[len(*objList)-1]
	*objList = (*objList)[:len(*objList)-1]
	return popped, nil
}

func (objList *MShellTypeStack) Push(obj MShellType) {
	*objList = append(*objList, obj)
}

func (stack *MShellTypeStack) InsertAtBeginning(types []MShellType) {
	*stack = append(types, *stack...)
}

func (stack *MShellTypeStack) Len() int {
	return len(*stack)
}

func (stack *MShellTypeStack) Clear() {
	*stack = (*stack)[:0]
}

type TypeCheckContext struct {
	InQuote bool
}

func TypeCheckTypeDef(stack MShellTypeStack, typeDef TypeDefinition) bool {
	if (len(typeDef.InputTypes) > stack.Len()) {
		return false
	}

	for i := 0; i < len(typeDef.InputTypes); i++ {
		stackIndex := len(stack) - len(typeDef.InputTypes) + i
		stackType := stack[stackIndex]
		if !stackType.Equals(typeDef.InputTypes[i]) {
			return false
		}
	}

	return true
}

func TypeCheckStack(stack MShellTypeStack, typeDefs []TypeDefinition) (int) {
	for idx, typeDef := range typeDefs {
		if TypeCheckTypeDef(stack, typeDef) {
			return idx
		}
	}

	return -1
}

func TypeCheckErrorMessage(stack MShellTypeStack, typeDefs []TypeDefinition, tokenName string) (string) {
	// First check for a length mismatch
	minInputLength := 10000
	maxInputLength := 0
	for _, typeDef := range typeDefs {
		if len(typeDef.InputTypes) < minInputLength {
			minInputLength = len(typeDef.InputTypes)
		}

		if len(typeDef.InputTypes) > maxInputLength {
			maxInputLength = len(typeDef.InputTypes)
		}
	}

	if minInputLength > stack.Len() {
		return fmt.Sprintf("Expected at least %d arguments on the stack for %s, but found %d.\n", minInputLength, tokenName, stack.Len())
	}

	// Start a builder for the error message
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Could not find a matching type definition for %s.\n", tokenName))
	builder.WriteString(fmt.Sprintf("Current stack:\n"))
	for i := stack.Len() - maxInputLength; i < stack.Len(); i++ {
		builder.WriteString(fmt.Sprintf("  %s\n", stack[i].ToMshell()))
	}
	builder.WriteString(fmt.Sprintf("Expected types:\n"))
	for _, typeDef := range typeDefs {
		builder.WriteString(fmt.Sprintf("  %s\n", typeDef.ToMshell()))
	}

	return builder.String()
}

func CheckKeyword(slice1 []string, slice2 []string) bool {
	if len(slice1) != len(slice2) {
		return false
	}

	for idx, item := range slice1 {
		if slice2[idx] != item {
			return false
		}
	}

	return true
}

func TypeCheck(objects []MShellParseItem, stack MShellTypeStack, definitions []MShellDefinition, inQuote bool) TypeCheckResult {
	// Short circuit if there are no objects to type check
	if len(objects) == 0 {
		return TypeCheckResult{
			Errors: make([]TypeCheckError, 0),
			InputTypes: make([]MShellType, 0),
			OutputTypes: make([]MShellType, 0),
		}
	}

	inputTypes := make([]MShellType, 0)
	outputTypes := make([]MShellType, 0)

	index := 0

	typeCheckResult := TypeCheckResult{
		Errors: make([]TypeCheckError, 0),
		InputTypes: inputTypes,
		OutputTypes: outputTypes,
	}

	builtInTypeSlice := make([]TypeDefinition, 0)

MainLoop:
	for index < len(objects) {
		t := objects[index]
		index++

		switch t.(type) {
		case *MShellParseList:
			list := t.(*MShellParseList)
			var listStack MShellTypeStack
			listStack = []MShellType{}

			result := TypeCheck(list.Items, listStack, definitions, false)

			// if result.BreakNum > 0 {
				// return FailWithMessage("Encountered break within list.\n")
			// }

			// stack.Push(&MShellList{Items: listStack, StandardInputFile: "", StandardOutputFile: "", StdoutBehavior: STDOUT_NONE})
			typeCheckResult.Errors = append(typeCheckResult.Errors, result.Errors...)

			typeTuple := TypeTuple{
				Types: listStack,
				StdoutBehavior: STDOUT_NONE,
			}

			stack.Push(&typeTuple)
		case *MShellParseQuote:
			quote := t.(*MShellParseQuote)

			typeQuote := TypeQuote{
				InputTypes: make([]MShellType, 0),
				OutputTypes: make([]MShellType, 0),
			}

			var quoteStack MShellTypeStack
			quoteStack = []MShellType{}

			quoteResult := TypeCheck(quote.Items, quoteStack, definitions, true)

			typeQuote.InputTypes = quoteResult.InputTypes
			typeQuote.OutputTypes = quoteResult.OutputTypes

			stack.Push(&typeQuote)

		case Token:
			t := t.(Token)

			if t.Type == EOF {
				return typeCheckResult
			} else if t.Type == LITERAL {
				// var typeDef *TypeDefinition = nil
				builtInTypeSlice = builtInTypeSlice[:0]

				// Check for definitions
				for _, definition := range definitions {
					if definition.Name == t.Lexeme {
						// Evaluate the definition
						builtInTypeSlice = append(builtInTypeSlice, definition.TypeDef)
						// typeDef = &definition.TypeDef
					}
				}

				if len(builtInTypeSlice) == 0 {
					// Search built-in definitions
					if defs, ok := builtInDefs[t.Lexeme]; ok {
						builtInTypeSlice = append(builtInTypeSlice, defs...)
					}
				}

				if len(builtInTypeSlice) == 0 {
					stack.Push(&TypeString{})
					continue MainLoop
				}

				// Check the stack for a matching type definition
				idx := TypeCheckStack(stack, builtInTypeSlice)
				if idx == -1 {
					message := TypeCheckErrorMessage(stack, builtInTypeSlice, t.Lexeme)
					typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: message})
					continue MainLoop
				}

				typeDef := builtInTypeSlice[idx]

				if inQuote {
					if len(typeDef.InputTypes) > stack.Len() {
						// For the difference, add the types to the input stack
						diff := len(typeDef.InputTypes) - stack.Len()
						stack.InsertAtBeginning(typeDef.InputTypes[:diff])
					}
				} 

				if len(typeDef.InputTypes) > stack.Len() {
					errorMessage := fmt.Sprintf("Definition %s expects %d arguments on the stack, but only %d were provided.\n", t.Lexeme, len(typeDef.InputTypes), stack.Len())
					typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: errorMessage})
					// Assume all are consumed, and place the output types on the stack
					stack.Clear()
					for _, outputType := range typeDef.OutputTypes {
						// TODO: Deal with generics.
						stack.Push(outputType)
					}
					continue MainLoop
				}

				// Check the input types
				for i := 0; i < len(inputTypes); i++ {
					stackType, _ := stack.Pop()
					inputTypeIndex := len(inputTypes) - i - 1
					if stackType.Equals(inputTypes[inputTypeIndex]) {
						typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: fmt.Sprintf("Expected type %s, but found type %s.\n", inputTypes[inputTypeIndex].String(), stackType.String())})
					}
				}

				// Assume all are consumed, and place the output types on the stack
				for _, outputType := range outputTypes {
					stack.Push(outputType)
				}
			} else if t.Type == TRUE || t.Type == FALSE {
				stack.Push(TypeBool{})
			} else if t.Type == INTEGER {
				stack.Push(TypeInt{})
			} else if t.Type == STRING || t.Type == SINGLEQUOTESTRING {
				stack.Push(TypeString{})
			} else if t.Type == PLUS {
				idx := TypeCheckStack(stack, typeDefAdd)
				if idx == -1 {
					message := TypeCheckErrorMessage(stack, typeDefAdd, "+")

					typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: message})
					return typeCheckResult
					// continue MainLoop
				}
			} else if t.Type == IF {
				obj, err := stack.Pop()
				if err != nil {
					typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: "Expected a list on the stack, but found none.\n"})
					continue MainLoop
				}

				// Check the type of the object
				switch obj.(type) {
				case *TypeTuple:
					tuple := obj.(*TypeTuple)
					// Check that all elements are of type quote
					for idx, obj := range tuple.Types {
						if _, ok := obj.(*TypeQuote); !ok {
							typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: fmt.Sprintf("Expected a quote at index %d, but found %s.\n", idx, obj.String())})
							continue MainLoop
						}
					}

					for i := 0; i < len(tuple.Types) - 1; i += 2 {
						// Check that the final element is a boolean
						outputTypes = tuple.Types[i].(*TypeQuote).OutputTypes

						if len(outputTypes) == 0 {
							typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: "Expected a quote to have an output with a boolean on the top of the stack, but found no outputs.\n"})
							continue MainLoop
						}

						if _, ok := outputTypes[len(outputTypes) - 1].(*TypeBool); !ok {
							badType := outputTypes[len(outputTypes) - 1].String()
							typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: fmt.Sprintf("Expected a quote to have an output with a boolean on the top of the stack, but found a %s.\n", badType)})
							continue MainLoop
						}

						// If the condition takes anything off the stack, it needs to match the current stack, and it needs to put it back.
						inputTypes := tuple.Types[i].(*TypeQuote).InputTypes
						outputTypes := tuple.Types[i].(*TypeQuote).OutputTypes

						if len(inputTypes) > stack.Len() {
							typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: fmt.Sprintf("Quote for condition takes %d arguments, but only %d items are on the stack.\n", len(inputTypes), stack.Len())})
							continue MainLoop
						}

						for i := 0; i < len(inputTypes); i++ {
							stackIndex := len(stack) - len(inputTypes) + i
							stackType := stack[stackIndex]
							if !stackType.Equals(inputTypes[i]) {
								typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: fmt.Sprintf("Expected type %s at stack index %d, but found type %s.\n", inputTypes[i].String(), stackIndex, stackType.String())})
								continue MainLoop
							}

							if !inputTypes[i].Equals(outputTypes[i]) {
								typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: fmt.Sprintf("Expected input/output types at index %d to match for quote, but found input type %s and output type %s.\n", i, inputTypes[i].String(), outputTypes[i].String())})
								continue MainLoop
							}
						}

						// Check that the input and output types match, except for the boolean
					}
				case *TypeList:
					list := obj.(*TypeList)

					if _, ok := list.ListType.(*TypeQuote); !ok {
						typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: fmt.Sprintf("Expected a list of quotes, but found a list of %s.\n", list.ListType.String())})
					} else {
						// Check that we have a known number of quotes
						if list.Count < 0 {
							typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: "Can not type check an unbounded list.\n"})
						}
					}
				default:
					typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: fmt.Sprintf("Expected a list on the stack, but found %s.\n", obj.String())})
				}

				// The the types of the quotes for the conditions:
				// They should all be of the same type, and if they consume anything on the stack, they should put it back.
				// The top of the stack should be a boolean.
			} else if t.Type == EXECUTE || t.Type == QUESTION {
				obj, err := stack.Pop()
				if err != nil {
					typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: "Expected a list on the stack, but found none.\n"})
					continue MainLoop
				}

				switch obj.(type) {
				case *TypeTuple:
					tuple := obj.(*TypeTuple)
					stdoutBehavior := tuple.StdoutBehavior
					if stdoutBehavior == STDOUT_LINES {
						stack.Push(&TypeList{ListType: TypeString{}, Count: -1})
					} else if stdoutBehavior == STDOUT_STRIPPED || stdoutBehavior == STDOUT_COMPLETE {
						stack.Push(TypeString{})
					} else if stdoutBehavior != STDOUT_NONE {
						typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: fmt.Sprintf("Expected a tuple with a known stdout behavior, but found %d.\n", stdoutBehavior)})
					}
				case *TypeList:
					list := obj.(*TypeList)
					stdoutBehavior := list.StdoutBehavior
					if stdoutBehavior == STDOUT_LINES {
						stack.Push(&TypeList{ListType: TypeString{}, Count: -1})
					} else if stdoutBehavior == STDOUT_STRIPPED || stdoutBehavior == STDOUT_COMPLETE {
						stack.Push(TypeString{})
					} else if stdoutBehavior != STDOUT_NONE {
						typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: fmt.Sprintf("Expected a tuple with a known stdout behavior, but found %d.\n", stdoutBehavior)})
					}
				}

				if t.Type == QUESTION {
					stack.Push(TypeInt{})
				}
			} else if t.Type == STR {
				idx := TypeCheckStack(stack, builtInDefs["str"])
				// This should only happen for length mismatch.
				if idx == -1 {
					message := TypeCheckErrorMessage(stack, builtInDefs["str"], "str")
					typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: message})
				}

				stack.Pop()
				stack.Push(TypeString{})
			} else {
				typeCheckResult.Errors = append(typeCheckResult.Errors, TypeCheckError{Token: t, Message: fmt.Sprintf("Unexpected token %s.\n", t.Lexeme)})
			}
		}
	}

	return typeCheckResult
}


func BuiltInDefs(name string) (*TypeDefinition, error) {
	switch name {
	case ".s":
		return &TypeDefinition{
			InputTypes: make([]MShellType, 0),
			OutputTypes: make([]MShellType, 0),
		}, nil
	
	default:
		return nil, fmt.Errorf("No built-in definition found for %s.\n", name)
	}
}
