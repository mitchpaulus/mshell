package main


type ArityCheckResult struct {
	InputCount int
	OutputCount int
}

type ArityTracking struct {
	CurrentSize int
	MinSize int // Expected to be <= 0
}

func (a *ArityTracking) Apply(input int, output int) {
	// Assert that input and output are non-negative
	if input < 0 || output < 0 {
		panic("Apply called with negative input or output")
	}

	if a.CurrentSize - input < a.MinSize {
		a.MinSize = a.CurrentSize - input
	}

	a.CurrentSize += output - input
}

func (a *ArityTracking) Result() ArityCheckResult {
	return ArityCheckResult{
		InputCount:  -a.MinSize,
		OutputCount: a.CurrentSize,
	}
}


func (a *ArityTracking) Decrement(num int) {
	// Assert that num is > 0
	if num <= 0 {
		panic("Decrement called with non-positive number")
	}
	a.CurrentSize -= num
	if a.CurrentSize < a.MinSize {
		a.MinSize = a.CurrentSize
	}
}

func ArityCheck(objects []MShellParseItem, definitions []MShellDefinition) ArityCheckResult {
	index := 0

	arity := ArityTracking {
		CurrentSize: 0,
		MinSize: 0,
	}

MainLoop:
	for index < len(objects) {
		t := objects[index]
		index++

		switch t := t.(type) {
		case *MShellParseList:
		case *MShellParseDict:
		case *MShellParseQuote:
			arity.Apply(0, 1)
		case *MShellIndexerList:
			arity.Apply(1, 1)
		case MShellVarstoreList:
			arity.Apply(len(t.VarStores), 0)
		case Token:
			if t.Type == EOF {
				return arity.Result()
			} else if t.Type == LITERAL {
				// Check for definitions
				for _, definition := range definitions {
					if definition.Name == t.Lexeme {
						arity.Apply(len(definition.TypeDef.InputTypes), len(definition.TypeDef.OutputTypes))
						continue MainLoop
					}
				}
			}
		}
	}

	return arity.Result()
}
