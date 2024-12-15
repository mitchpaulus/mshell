package main

import (
	"fmt"
)

type TypeCheckError struct {
	Token Token
	Message string
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

func TypeCheck(objects []MShellParseItem, stack *MShellTypeStack, definitions []MShellDefinition, inQuote bool) TypeCheckResult {
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

MainLoop:
	for index < len(objects) {
		t := objects[index]
		index++

		switch t.(type) {
		case *MShellParseList:
			list := t.(*MShellParseList)
			var listStack MShellTypeStack
			listStack = []MShellType{}

			result := TypeCheck(list.Items, &listStack, definitions, false)

			// if result.BreakNum > 0 {
				// return FailWithMessage("Encountered break within list.\n")
			// }

			// stack.Push(&MShellList{Items: listStack, StandardInputFile: "", StandardOutputFile: "", StdoutBehavior: STDOUT_NONE})

			typeCheckResult.Errors = append(typeCheckResult.Errors, result.Errors...)

			typeTuple := TypeTuple{
				Types: listStack,
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

			quoteResult := TypeCheck(quote.Items, &quoteStack, definitions, true)

			typeQuote.InputTypes = quoteResult.InputTypes
			typeQuote.OutputTypes = quoteResult.OutputTypes

			stack.Push(&typeQuote)

		case Token:
			t := t.(Token)

			if t.Type == EOF {
				return typeCheckResult
			} else if t.Type == LITERAL {
				var typeDef *TypeDefinition = nil

				// Check for definitions
				for _, definition := range definitions {
					if definition.Name == t.Lexeme {
						// Evaluate the definition
						typeDef = &definition.TypeDef
					}
				}

				if typeDef == nil {
					// Search built-in definitions
				}

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
