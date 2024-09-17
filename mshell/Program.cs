using System.Collections;
using System.Collections.ObjectModel;
using System.Diagnostics;
using System.Text;
using OneOf;

namespace mshell;

class Program
{
    static int Main(string[] args)
    {
        int i = 0;

        bool printLex = false;
        string? input = null;

        while (i < args.Length)
        {
            if (args[i] == "--lex")
            {
                printLex = true;
            }
            else
            {
                input = File.ReadAllText(args[i]);
            }

            i++;
        }

        input ??= Console.In.ReadToEnd();

        // input = args.Length > 0 ? File.ReadAllText(args[0], Encoding.UTF8) : Console.In.ReadToEnd();

        Lexer l = new Lexer(input);

        var tokens = l.Tokenize();

        if (printLex)
        {
            foreach (var t in tokens)
            {
                Console.Write($"{t.Line}:{t.Column}:{t.TokenType} {t.RawText}\n");
            }

            return 0;
        }

        Evaluator e = new(false);
        EvalResult result = e.Evaluate(tokens, new Stack<MShellObject>(), new ExecuteContext());

        return result.Success ? 0 : 1;

        // foreach (var t in tokens)
        // {
        //     Console.Write($"{t.TokenType()}: {t.Print()}\n");
        // }

        // Console.WriteLine("Hello, World!");
    }
}


public class Lexer
{
    private int _start = 0;
    private int _current = 0;
    private int _col = 0;
    private int _line = 1;

    private readonly string _input;

    private bool AtEnd() => _current >= _input.Length;

    public Lexer(string input)
    {
        _input = input;
    }

    private TokenNew MakeToken(TokenType tokenType) => new(_line, _col, _start, _input.Substring(_start, _current - _start), tokenType);

    private char Advance()
    {
        _col++;
        var c = _input[_current];
        _current++;
        return c;
    }

    private char Peek() => _input[_current];

    private char PeekNext() => AtEnd() ? '\0' : _input[_current + 1];

    private TokenNew ScanToken()
    {
        EatWhitespace();
        _start = _current;
        if (AtEnd()) return MakeToken(TokenType.EOF);

        char c = Advance();

        if (c == '"') return ParseString();

        switch (c)
        {
            case '[':
                return MakeToken(TokenType.LEFT_SQUARE_BRACKET);
            case ']':
                return MakeToken(TokenType.RIGHT_SQUARE_BRACKET);
            case '(':
                return MakeToken(TokenType.LEFT_PAREN);
            case ')':
                return MakeToken(TokenType.RIGHT_PAREN);
            case ';':
                return MakeToken(TokenType.EXECUTE);
            case '|':
                return MakeToken(TokenType.PIPE);
            case '?':
                return MakeToken(TokenType.QUESTION);
            default:
                return ParseLiteralOrNumber();
        }
    }

    private TokenNew ParseString()
    {
        // When this is called, we've already consumed a single double quote.
        bool inEscape = false;
        while (true)
        {
            if (AtEnd())
            {
                Console.Error.Write($"{_line}:{_col}: Unterminated string.\n");
                return MakeToken(TokenType.ERROR);
            }

            char c = Advance();
            if (inEscape)
            {
                if (c != 'n' && c != 't' && c != 'r' && c != '\\' && c != '"')
                {
                    Console.Error.Write($"{_line}:{_col}: Invalid escape character '{c}'.\n");
                    return MakeToken(TokenType.ERROR);
                }
                inEscape = false;
            }
            else {
                if (c == '"') break;
                if (c == '\\') inEscape = true;
            }
        }

        return MakeToken(TokenType.STRING);
    }

    private TokenNew ParseLiteralOrNumber()
    {
        while (true)
        {
            char c = Peek();
            if (!char.IsWhiteSpace(c) && c != ']' && c != ')' && c != '<' && c != '>' && c != ';' && c != '?') Advance();
            else break;
        }

        string literal = _input.Substring(_start, _current - _start);

        if (literal == "-") return MakeToken(TokenType.MINUS);
        if (literal == "+") return MakeToken(TokenType.PLUS);
        if (literal == "=") return MakeToken(TokenType.EQUALS);
        if (literal == "x") return MakeToken(TokenType.INTERPRET);
        if (literal == "if") return MakeToken(TokenType.IF);
        if (literal == "loop") return MakeToken(TokenType.LOOP);
        if (literal == "break") return MakeToken(TokenType.BREAK);
        if (literal == "not") return MakeToken(TokenType.NOT);
        if (literal == "and") return MakeToken(TokenType.AND);
        if (literal == "or") return MakeToken(TokenType.OR);
        if (literal == ">=") return MakeToken(TokenType.GREATERTHANOREQUAL);
        if (literal == "<=") return MakeToken(TokenType.LESSTHANOREQUAL);
        if (literal == "<") return MakeToken(TokenType.LESSTHAN);
        if (literal == ">") return MakeToken(TokenType.GREATERTHAN);
        if (literal == "true") return MakeToken(TokenType.TRUE);
        if (literal == "false") return MakeToken(TokenType.FALSE);

        if (literal.EndsWith("!")) return MakeToken(TokenType.VARRETRIEVE);
        if (literal.StartsWith("@")) return MakeToken(TokenType.VARSTORE);

        if (int.TryParse(literal, out int i)) return MakeToken(TokenType.INTEGER);
        if (double.TryParse(literal, out double d)) return MakeToken(TokenType.DOUBLE);
        return MakeToken(TokenType.LITERAL);
    }

    public List<TokenNew> Tokenize()
    {
        List<TokenNew> tokens = new();
        while (true)
        {
            var t = ScanToken();
            tokens.Add(t);
            if (t.TokenType is TokenType.ERROR or TokenType.EOF) break;
        }

        return tokens;
    }

    private void EatWhitespace()
    {
        while (true)
        {
            if (AtEnd()) return;
            char c = Peek();
            if (c == ' ' || c == '\t' || c == '\r')
            {
                Advance();
            }
            else if (c == '#')
            {
                while (!AtEnd() && Peek() != '\n') Advance();
            }
            else if (c == '\n')
            {
                _line++;
                _col = 0;
                Advance();
            }
            else
            {
                return;
            }
        }
    }
}

public class Evaluator
{
    private readonly Action<MShellObject, Stack<MShellObject>> _push;
    private int _loopDepth = 0;

    private Dictionary<string, MShellObject> _variables = new();

    // private Stack<Stack<MShellObject>> _stack = new();

    public Evaluator(bool debug)
    {
        _push = debug ? PushWithDebug : PushNoDebug;
        // _tokens = tokens;
        // Stack<Stack<MShellObject>> stack = new();
        // _stack.Push(new Stack<MShellObject>());
    }

    private void PushWithDebug(MShellObject o, Stack<MShellObject> stack)
    {
        Console.Error.Write($"Push {o.TypeName()}\n");
        stack.Push(o);
    }

    private void PushNoDebug(MShellObject o, Stack<MShellObject> stack) => stack.Push(o);

    private EvalResult FailResult() => new EvalResult(false, -1);

    public EvalResult Evaluate(List<TokenNew> tokens, Stack<MShellObject> stack, ExecuteContext context)
    {
        int index = 0;

        // This is a stack of left parens for tracking nested quotations.
        Stack<int> quotationStack = new();
        Stack<int> leftSquareBracketStack = new();

        while (index < tokens.Count)
        {
            TokenNew t = tokens[index];
            if (t.TokenType == TokenType.EOF) return new EvalResult(true, -1);

            if (t.TokenType == TokenType.LITERAL)
            {
                if (t.RawText == "dup")
                {
                    if (stack.TryPeek(out var o))
                    {
                        _push(o, stack);
                    }
                    else return FailWithMessage($"Nothing on stack to duplicate for 'dup'.\n");
                }
                else if (t.RawText == "drop")
                {
                    if (!stack.TryPop(out MShellObject? _))
                    {
                        return FailWithMessage($"Nothing on stack to drop.\n");
                    }
                }
                else if (t.RawText == ".s")
                {
                    Console.Error.Write("Current stack:");

                    foreach (var v in stack)
                    {
                        Console.Error.Write(v.DebugString());
                        Console.Error.Write('\n');
                    }
                }
                else
                {
                    _push(new LiteralToken(t.RawText), stack);
                }

                index++;
            }
            else if (t.TokenType == TokenType.LEFT_SQUARE_BRACKET)
            {
                leftSquareBracketStack.Push(index);
                // Search for balancing right bracket
                index++;
                while (true)
                {
                    var currToken = tokens[index];
                    if (index >= tokens.Count || tokens[index].TokenType == TokenType.EOF)
                    {
                        return FailWithMessage($"{currToken.Line}:{currToken.Column}: Found unbalanced bracket.\n");
                    }

                    if (tokens[index].TokenType == TokenType.LEFT_SQUARE_BRACKET)
                    {
                        leftSquareBracketStack.Push(index);
                        index++;
                    }
                    else if (tokens[index].TokenType == TokenType.RIGHT_SQUARE_BRACKET)
                    {
                        if (leftSquareBracketStack.TryPop(out var leftIndex) )
                        {
                            if (leftSquareBracketStack.Count == 0)
                            {
                                Stack<MShellObject> listStack = new();
                                var tokensWithinList = tokens.GetRange(leftIndex + 1, index - leftIndex - 1).ToList();
                                var result = Evaluate(tokensWithinList, listStack, context);
                                if (!result.Success) return result;
                                if (result.BreakNum > 0)
                                {
                                    return FailWithMessage("Encountered break within list.\n");
                                }

                                MShellList l = new(listStack.Reverse());
                                _push(l, stack);
                                // stack.Push(l);
                                break;
                            }

                            index++;
                        }
                        else
                        {
                            return FailWithMessage($"{currToken.Line}:{currToken.Column}: Found unbalanced square bracket.\n");
                        }

                    }
                    else
                    {
                        index++;
                    }
                }

                index++;
                // stack.Push(new Stack<MShellObject>());
            }
            else if (t.TokenType == TokenType.RIGHT_SQUARE_BRACKET)
            {
                return FailWithMessage($"{t.Line}:{t.Column}: Found unbalanced list.\n");
            }
            else if (t.TokenType == TokenType.LEFT_PAREN)
            {
                quotationStack.Push(index);
                index++;

                while (true)
                {
                    if (index >= tokens.Count || tokens[index].TokenType == TokenType.EOF)
                    {
                        return FailWithMessage("Found unbalanced bracket.\n");
                    }

                    if (tokens[index].TokenType == TokenType.LEFT_PAREN)
                    {
                        quotationStack.Push(index);
                        index++;
                    }
                    else if (tokens[index].TokenType == TokenType.RIGHT_PAREN)
                    {
                        if (quotationStack.TryPop(out var leftIndex))
                        {
                            if (quotationStack.Count == 0)
                            {
                                var tokensWithinList = tokens.GetRange(leftIndex + 1, index - leftIndex - 1).ToList();
                                MShellQuotation q = new (tokensWithinList, leftIndex + 1, index);
                                _push(q, stack);
                                // stack.Push(q);
                                break;
                            }

                            index++;
                        }
                        else
                        {
                            return FailWithMessage("Found unbalanced quotation.\n");
                        }
                    }
                    else
                    {
                        index++;
                    }
                }

                index++;
            }
            else if (t.TokenType == TokenType.RIGHT_PAREN)
            {
                return FailWithMessage("Unbalanced parenthesis found.\n");
            }
            else if (t.TokenType == TokenType.IF)
            {
                if (stack.TryPop(out var o))
                {
                    if (o.TryPickList(out var qList))
                    {
                        if (qList.Items.Count < 2)
                        {
                            return FailWithMessage("Quotation list for if should have a minimum of 2 elements.\n");
                        }

                        if (qList.Items.Any(i => !i.IsQuotation))
                        {
                            Console.Error.Write("All items within list for if are required to be quotations. Received:\n");
                            foreach (var i in qList.Items)
                            {
                                Console.Error.Write(i.TypeName());
                                Console.Error.Write('\n');
                            }
                            return FailResult();
                        }

                        // Loop through the even index quotations, looking for the first one that has a true condition.
                        var trueIndex = -1;
                        for (int i = 0; i < qList.Items.Count - 1; i += 2)
                        {
                            MShellQuotation q = qList.Items[i].AsT2;
                            var result = Evaluate(q.Tokens, stack, context);
                            if (!result.Success) return FailResult();
                            if (result.BreakNum > 0)
                            {
                                return FailWithMessage("Found break during evaluation of if condition.\n");
                            }

                            if (stack.TryPop(out var condition))
                            {
                                if (condition.TryPickIntToken(out var intVal, out var remainder))
                                {
                                    // 0 is used here for successful process exit codes.
                                    if (intVal.IntVal == 0)
                                    {
                                        trueIndex = i;
                                        break;
                                    }
                                }
                                else if (remainder.TryPickT3(out MShellBool booleanVal, out var remainder2))
                                {
                                    if (booleanVal.Value)
                                    {
                                        trueIndex = i;
                                        break;
                                    }
                                }
                                else
                                {
                                    return FailWithMessage($"Can't evaluate condition for type {condition.TypeName()}.\n");
                                }
                            }
                            else
                            {
                                return FailWithMessage("Evaluation of condition quotation removed all stacks.");
                            }
                        }

                        if (trueIndex > -1)
                        {
                            // Run the quotation on the index after the true index
                            if (!qList.Items[trueIndex + 1].IsQuotation)
                            {
                                return FailWithMessage($"True branch of if statement must be quotation. Received a {qList.Items[trueIndex + 1].TypeName()}.\n");
                            }

                            MShellQuotation q = qList.Items[trueIndex + 1].AsQuotation;
                            var result = Evaluate(q.Tokens, stack, context);
                            if (!result.Success) return FailResult();
                            // If we broke during the evaluation, pass it up the eval stack
                            if (result.BreakNum != -1) return result;
                        }
                        else if (qList.Items.Count % 2 == 1)
                        {
                            // Run the last quotation if there was no true condition.
                            if (!qList.Items[^1].IsQuotation)
                            {
                                return FailWithMessage($"Else branch of if statement must be quotation. Received a {qList.Items[^1].TypeName()}.\n");
                            }

                            MShellQuotation q = qList.Items[^1].AsQuotation;
                            var result = Evaluate(q.Tokens, stack, context);
                            if (!result.Success) return FailResult();
                            // If we broke during the evaluation, pass it up the eval stack
                            if (result.BreakNum != -1) return result;
                        }
                    }
                    else
                    {
                        return FailWithMessage("Argument for if expected to be a list of quotations.\n");
                    }
                }
                else
                {
                     return FailWithMessage("Nothing on stack for if.\n");
                }

                index++;
            }
            else if (t.TokenType == TokenType.EXECUTE || t.TokenType == TokenType.QUESTION)
            {
                if (stack.TryPop(out var arg))
                {
                    (EvalResult result, int exitCode) = arg.Match(
                        literalToken => RunProcess(new MShellList(new List<MShellObject>(1) { literalToken }), context),
                        list => RunProcess(list, context),
                        _ => (FailWithMessage("Cannot execute a quotation.\n"), 1),
                        _ => (FailWithMessage("Cannot execute an integer.\n"), 1),
                        _ => (FailWithMessage("Cannot execute a boolean.\n"), 1),
                        str => RunProcess(new MShellList(new List<MShellObject>(1) { str }), context),
                        pipe => RunProcess(pipe, context)
                    );

                    if (!result.Success) return result;

                    if (t.TokenType == TokenType.QUESTION)
                    {
                        _push(new IntToken(new TokenNew(t.Line, t.Column, t.Start, exitCode.ToString(), TokenType.INTEGER)), stack);
                    }
                }
                else
                {
                    return FailWithMessage("Nothing on stack to execute.\n");
                }
                index++;
            }
            else if (t.TokenType == TokenType.INTEGER)
            {
                _push(new IntToken(t), stack);
                // stack.Push(iToken);
                index++;
            }
            else if (t.TokenType == TokenType.LOOP)
            {
                if (stack.TryPop(out var o))
                {
                    if (o.IsQuotation)
                    {
                        _loopDepth++;
                        int thisLoopDepth = _loopDepth;
                        int loopCount = 1;
                        while (loopCount < 15000)
                        {
                            EvalResult result = Evaluate(o.AsQuotation.Tokens, stack, context);
                            if (!result.Success) return FailResult();

                            if ((_loopDepth + 1) - result.BreakNum <= thisLoopDepth) break;
                            loopCount++;
                        }

                        if (loopCount >= 15000)
                        {
                            return FailWithMessage("Looks like infinite loop.\n");
                        }

                        index++;
                    }
                    else
                    {
                        return FailWithMessage($"{t.Line}:{t.Column}: Expected quotation on top of stack for 'loop'.\n");
                    }
                }
                else
                {
                    return FailWithMessage($"{t.Line}:{t.Column}: Quotations expected on stack for 'loop'.\n");
                }
            }
            else if (t.TokenType == TokenType.BREAK)
            {
                index++;
                return new EvalResult(true, 1);
            }
            else if (t.TokenType == TokenType.EQUALS)
            {
                if (stack.Count < 2)
                {
                    return FailWithMessage($"{stack} tokens on the stack are not enough for the '=' operator.\n");
                }

                var arg1 = stack.Pop();
                var arg2 = stack.Pop();

                if (arg1.IsIntToken && arg2.IsIntToken)
                {
                    _push(arg1.AsIntToken.IntVal == arg2.AsIntToken.IntVal ? new MShellBool(true) : new MShellBool(false), stack);
                }
                else
                {
                    return FailWithMessage($"'=' is only currently implemented for integers. Received a {arg1.TypeName()} {arg1.DebugString()} {arg2.TypeName()} {arg2.DebugString()}\n");
                }

                index++;
            }
            else if (t.TokenType == TokenType.NOT)
            {
                if (stack.Count < 1)
                {
                    return FailWithMessage($"No tokens found on the stack for the 'not' operator.\n");
                }

                var arg = stack.Pop();
                if (arg.TryPickBool(out var b))
                {
                    _push(new MShellBool(!b.Value), stack);
                }
                else
                {
                    return FailWithMessage($"Not operator only implemented for boolean variables. Found a {arg.TypeName()} on top of the stack..\n");
                }
                index++;
            }
            else if (t.TokenType == TokenType.AND)
            {
                if (stack.Count < 2)
                {
                   return FailWithMessage($"Not enough tokens on the stack for the 'and' operator.\n");
                }

                var arg1 = stack.Pop();
                var arg2 = stack.Pop();

                if (arg1.TryPickBool(out var b1) && arg2.TryPickBool(out var b2))
                {
                    _push(new MShellBool(b1.Value && b2.Value), stack);
                }
                else
                {
                    return FailWithMessage($"'and' operator only implemented for boolean variables. Found a {arg1.TypeName()} and a {arg2.TypeName()} on top of the stack.\n");
                }
                index++;
            }
            else if (t.TokenType == TokenType.OR)
            {
                if (stack.Count < 2)
                {
                    return FailWithMessage($"Not enough tokens on the stack for the 'or' operator.\n");
                }

                var arg1 = stack.Pop();
                var arg2 = stack.Pop();

                if (arg1.TryPickBool(out var b1) && arg2.TryPickBool(out var b2))
                {
                    _push(new MShellBool(b1.Value || b2.Value), stack);
                }
                else
                {
                    return FailWithMessage($"'or' operator only implemented for boolean variables. Found a {arg1.TypeName()} and a {arg2.TypeName()} on top of the stack.\n");
                }
                index++;
            }
            else if (t.TokenType == TokenType.TRUE)
            {
                _push(new MShellBool(true), stack);
                index++;
            }
            else if (t.TokenType == TokenType.FALSE)
            {
                _push(new MShellBool(false), stack);
                index++;
            }
            else if (t.TokenType == TokenType.STRING)
            {
                _push(new MShellString(t), stack);
                index++;
            }
            else if (t.TokenType == TokenType.VARSTORE)
            {
                if (stack.TryPop(out var o))
                {
                    _variables[t.RawText.Substring(1, t.RawText.Length - 1)] = o;
                }
                else
                {
                    return FailWithMessage($"Nothing on stack to store into variable '{t.RawText.Substring(1, t.RawText.Length - 1)}'.\n");
                }

                index++;
            }
            else if (t.TokenType == TokenType.VARRETRIEVE)
            {
                var name = t.RawText.Substring(0, t.RawText.Length - 1);
                if (_variables.TryGetValue(name, out var o))
                {
                    _push(o, stack);
                }
                else
                {
                    StringBuilder message = new();
                    message.Append($"Variable '{name}' not found. Variables available:\n");
                    foreach (var n in _variables.Keys)
                    {
                        message.Append(n);
                        message.Append('\n');
                    }

                    return FailWithMessage(message.ToString());
                }

                index++;
            }
            else if (t.TokenType == TokenType.GREATERTHANOREQUAL || t.TokenType == TokenType.LESSTHANOREQUAL)
            {
                index++;
                if (stack.Count < 2) return FailWithMessage($"'{t.RawText}' operator requires at least two objects on the stack. Found {stack.Count} objects.\n");

                var arg1 = stack.Pop();
                var arg2 = stack.Pop();

                if (!arg1.IsNumeric()) return FailWithMessage($"Argument {arg1.DebugString()} is not a numeric value that can be compared in {t.RawText} operation.\n");
                if (!arg2.IsNumeric()) return FailWithMessage($"Argument {arg2.DebugString()} is not a numeric value that can be compared in {t.RawText} operation.\n");

                MShellBool b;
                if (t.TokenType == TokenType.GREATERTHANOREQUAL)
                {
                    b = new MShellBool(arg2.FloatNumeric() >= arg1.FloatNumeric());
                }
                else if (t.TokenType == TokenType.LESSTHANOREQUAL)
                {
                    b = new MShellBool(arg2.FloatNumeric() <= arg1.FloatNumeric());
                }
                else
                {
                    // Should never reach.
                    throw new Exception();
                }

                _push(b, stack);
            }
            else if (t.TokenType == TokenType.MINUS)
            {
                index++;
                if (stack.Count < 2) return FailWithMessage($"'{t.RawText}' operator requires at least two objects on the stack. Found {stack.Count} objects.\n");

                var arg1 = stack.Pop();
                var arg2 = stack.Pop();

                if (!arg1.IsNumeric()) return FailWithMessage($"Argument {arg1.DebugString()} is not a numeric value that can be compared in {t.RawText} operation.\n");
                if (!arg2.IsNumeric()) return FailWithMessage($"Argument {arg2.DebugString()} is not a numeric value that can be compared in {t.RawText} operation.\n");

                if (arg1.TryPickIntToken(out var int1) && arg2.TryPickIntToken(out var int2))
                {
                    int newInt = int2.IntVal - int1.IntVal;
                    _push(new IntToken(new TokenNew(t.Line, t.Column, t.Start, newInt.ToString(), TokenType.INTEGER)), stack);
                }
                else
                {
                    return FailWithMessage($"Currently only support integers for '{t.RawText}' operator.\n");
                }
            }
            else if (t.TokenType == TokenType.PLUS)
            {
                 index++;
                 if (stack.Count < 2) return FailWithMessage($"'{t.RawText}' operator requires at least two objects on the stack. Found {stack.Count} object.\n");

                 var arg1 = stack.Pop();
                 var arg2 = stack.Pop();

                 if (arg1.TryPickIntToken(out var int1) && arg2.TryPickIntToken(out var int2))
                 {
                     int newInt = int2.IntVal + int1.IntVal;
                     _push(new IntToken(new TokenNew(t.Line, t.Column, t.Start, newInt.ToString(), TokenType.INTEGER)), stack);
                 }
                 else if (arg1.TryPickString(out var string1) && arg2.TryPickString(out var string2))
                 {
                     string newString = string2.Content + string1.Content;
                     _push(new MShellString(newString), stack);
                 }
                 else if (arg1.TryPickLiteral(out var literal1) && arg2.TryPickLiteral(out var literal2))
                 {
                     _push(new LiteralToken(literal2.Text() + literal1.Text()), stack);
                 }
                 else if (arg1.TryPickString(out string1) && arg2.TryPickLiteral(out literal2))
                 {
                     _push(new MShellString(literal2.Text() + string1.Content), stack);
                 }
                 else if (arg1.TryPickLiteral(out literal1) && arg2.TryPickString(out string2))
                 {
                     _push(new MShellString(string2.Content + literal1.Text()), stack);
                 }
                 else
                 {
                     return FailWithMessage($"Currently only support integers for '{t.RawText}' operator. Top of stack was {arg1.TypeName()} and second to top was {arg2.TypeName()}.\n");
                 }
            }
            else if (t.TokenType == TokenType.GREATERTHAN)
            {
                // This can either be normal comparison for numbers, or it's a redirect on a list.
                index++;
                if (stack.Count < 2) return FailWithMessage($"'{t.RawText}' operator requires at least two objects on the stack. Found {stack.Count} object.\n");

                var arg1 = stack.Pop();
                var arg2 = stack.Pop();

                if (arg1.IsNumeric() && arg2.IsNumeric())
                {
                    _push(new MShellBool(arg2.FloatNumeric() > arg1.FloatNumeric()), stack);
                }
                else if (arg1.TryPickString(out var s) && arg2.TryPickList(out var list))
                {
                    list.StandardOutFile = s.Content;
                    // Push the list back on the stack
                    _push(list, stack);
                }
                else if (arg1.TryPickString(out s) && arg2.TryPickQuotation(out var quotation))
                {
                    using FileStream stream = new FileStream(s.Content, FileMode.OpenOrCreate);
                    // Truncate file, so all other operations can be a Append.
                    stream.SetLength(0);
                    quotation.StandardOutputFile = s.Content;
                    _push(quotation, stack);
                }
                else
                {
                     return FailWithMessage($"Currently only implemented redirection for '{t.RawText}' operator or numerics.\n");
                }

            }
            else if (t.TokenType == TokenType.PIPE)
            {
                index++;

                if (stack.TryPop(out var o))
                {
                    if (o.TryPickList(out var list))
                    {
                        MShellPipe p = new(list);
                        _push(p, stack);
                    }
                    else
                    {
                        return FailWithMessage($"'{t.RawText}' operator requires a list on top of the stack. Found a {o.TypeName()}.\n");
                    }
                }
                else
                {
                    return FailWithMessage($"'{t.RawText}' operator requires at least one object on the stack.\n");
                }
            }
            else if (t.TokenType == TokenType.INTERPRET)
            {
                index++;
                if (stack.TryPop(out var o))
                {
                    if (o.TryPickQuotation(out var quotation))
                    {
                        var evalResult = Evaluate(quotation.Tokens, stack, quotation.Context);
                        if (!evalResult.Success) return evalResult;
                        if (evalResult.BreakNum != -1) return evalResult;
                    }
                }
            }
            else
            {
                return FailWithMessage($"Token type '{t.TokenType}' (Raw Token: '{t.RawText}') not implemented yet.\n");
                // throw new NotImplementedException($"Token type {t.TokenType()} not implemented yet.");
            }
        }

        return new EvalResult(true, -1);
    }

    private EvalResult FailWithMessage(string message)
    {
        Console.Error.Write(message);
        return FailResult();
    }

    private void ExecuteQuotation(MShellQuotation q)
    {
        int qIndex = 0;
        while (qIndex < q.Tokens.Count)
        {



        }
    }

    public (EvalResult, int) RunProcess(MShellList list, ExecuteContext context)
    {
        if (list.Items.Any(o => !o.IsCommandLineable()))
        {
            var badTypes = list.Items.Where(o => !o.IsCommandLineable());
            return (FailWithMessage($"Can't handle a process argument of type {string.Join(", ", badTypes.Select(o => o.TypeName()))}."), 1);
        }

        List<string> arguments = list.Items.Select(o => o.CommandLine()).ToList();

        if (arguments.Count == 0) return (FailWithMessage("Cannot execute an empty list"), 1);

        // Console.Write(context);

        ProcessStartInfo info = new()
        {
            FileName = arguments[0],
            UseShellExecute = false,
            RedirectStandardError = false,
            RedirectStandardInput = list.StandardInputFile is not null,
            RedirectStandardOutput = list.StandardOutFile is not null || context.StandardOutput is not null,
            CreateNoWindow = true,
        };
        foreach (string arg in arguments.Skip(1)) info.ArgumentList.Add(arg);

        Process p = new Process()
        {
            StartInfo = info
        };

        int exitCode;
        try
        {
            using (p)
            {
                p.Start();

                // string stderr = p.StandardError.ReadToEnd();

                if (list.StandardOutFile is not null)
                {
                    // TODO: Use the BeginOutputReadLine methods instead to not have to have the entire thing in memory.
                    using StreamWriter w = new StreamWriter(list.StandardOutFile);
                    string content = p.StandardOutput.ReadToEnd();
                    w.Write(content);
                }
                else if (context.StandardOutput is not null)
                {
                    // TODO: Use the BeginOutputReadLine methods instead to not have to have the entire thing in memory.
                    using FileStream stream = new FileStream(context.StandardOutput, FileMode.Append);
                    p.StandardOutput.BaseStream.CopyTo(stream);
                }

                if (list.StandardInputFile is not null)
                {
                    using StreamWriter w = p.StandardInput;
                    w.Write(File.ReadAllBytes(list.StandardInputFile));
                }

                p.WaitForExit();
                // Console.Out.Write(stdout);
                // Console.Error.Write(stderr);
                exitCode = p.ExitCode;
            }
        }
        catch (Exception e)
        {
            return (FailWithMessage($"There was an exception running process with args { string.Join(", ", arguments.Select(s => $"'{s}'"))  }.\n{e.Message}\n"), 1);
        }

        return (new EvalResult(true, -1), exitCode);
    }

    public (EvalResult, int) RunProcess(MShellPipe pipe, ExecuteContext context)
    {
        if (pipe.List.Items.Count == 0) return (new EvalResult(true, -1), 1);

        List<MShellList> listItems = new List<MShellList>();
        foreach (var i in pipe.List.Items)
        {
            if (i.TryPickList(out var l))
            {
                listItems.Add(l);
            }
            else
            {
                return (FailWithMessage($"Pipelines are only supported with list items currently.\n"), 1);
            }
        }

        if (listItems.Count == 1) return RunProcess(listItems[0], context);

        // Minimum of two here
        List<Process> processes = new();
        var firstList = listItems[0];

        if (firstList.Items.Any(i => !i.IsCommandLineable()))
        {
            return (FailWithMessage("Not all elements in list are valid for command.\n"), 1);
        }

        var firstProcessStartInfo = new ProcessStartInfo()
           {
               FileName = listItems[0].Items[0].CommandLine(),
               RedirectStandardInput = firstList.StandardInputFile is not null,
               RedirectStandardOutput = true,
               UseShellExecute = false,
               CreateNoWindow = true,
           };
        foreach (var arg in listItems[0].Items.Skip(1)) { firstProcessStartInfo.ArgumentList.Add(arg.CommandLine()); }

        var firstProcess = new Process() { StartInfo = firstProcessStartInfo, };
        processes.Add(firstProcess);

        // Middle pipe items
        for (int i = 1; i < listItems.Count - 1; i++)
        {
            var startInfo = new ProcessStartInfo()
            {
                FileName = listItems[i].Items[0].CommandLine(),
                RedirectStandardInput = true,
                RedirectStandardOutput = true,
                UseShellExecute = false,
                CreateNoWindow = true,
            };
            foreach (var arg in listItems[i].Items.Skip(1))
            {
                startInfo.ArgumentList.Add(arg.CommandLine());
            }
            processes.Add(new Process { StartInfo = startInfo });
        }

        // Final pipe item
        var lastStartInfo = new ProcessStartInfo
        {
            FileName = listItems[^1].Items[0].CommandLine(),
            RedirectStandardInput = true,
            RedirectStandardOutput = listItems[^1].StandardOutFile is not null || context.StandardOutput is not null,
            UseShellExecute = false,
            CreateNoWindow = true,
        };
        foreach (var arg in listItems[^1].Items.Skip(1))
        {
            lastStartInfo.ArgumentList.Add(arg.CommandLine());
        }
        processes.Add(new Process() { StartInfo = lastStartInfo}) ;

        foreach (var p in processes) p.Start();

        string? stdinFile = firstList.StandardInputFile;

        if (stdinFile is not null)
        {
            using FileStream s = new(stdinFile, FileMode.Open);
            s.CopyTo(processes[0].StandardInput.BaseStream);
        }

        for (int i = 0; i < processes.Count - 1; i++)
        {
            using var output = processes[i].StandardOutput.BaseStream;
            using var input = processes[i + 1].StandardInput.BaseStream;
            processes[i].StandardOutput.BaseStream.CopyTo(processes[i + 1].StandardInput.BaseStream);
        }

        string? stdoutFile = listItems[^1].StandardOutFile;
        if (stdoutFile is not null)
        {
            using FileStream s = new(stdoutFile, FileMode.Truncate);
            processes[^1].StandardOutput.BaseStream.CopyTo(s);
        }
        else if (context.StandardOutput is not null)
        {
            using FileStream s = new(context.StandardOutput, FileMode.Append);
            processes[^1].StandardOutput.BaseStream.CopyTo(s);
        }

        foreach (var p in processes) p.WaitForExit();
        var exitCodes = processes.Select(process => process.ExitCode).ToList();
        foreach (var p in processes) p.Dispose();

        return (new EvalResult(true, -1), exitCodes.Last());
    }

    public byte[] ReadAllBytesFromStream(Stream stream)
    {
        using MemoryStream ms = new();
        stream.CopyTo(ms);
        return ms.ToArray();
    }
}


public class Execute : Token
{
    public Execute(int line, int col) : base(line, col)
    {
    }

    public override string Print() => ";";

    public override string TokenType() => "Execute";
}

public enum TokenType
{
    LEFT_SQUARE_BRACKET = 0,
    RIGHT_SQUARE_BRACKET = 1,
    EOF,
    LEFT_PAREN,
    RIGHT_PAREN,
    EXECUTE,
    MINUS,
    IF,
    INTEGER,
    DOUBLE,
    LITERAL,
    ERROR,
    LOOP,
    BREAK,
    EQUALS,
    NOT,
    AND,
    OR,
    GREATERTHANOREQUAL,
    TRUE,
    FALSE,
    STRING,
    VARRETRIEVE,
    VARSTORE,
    LESSTHANOREQUAL,
    LESSTHAN,
    GREATERTHAN,
    PLUS,
    PIPE,
    INTERPRET,
    QUESTION
}

public class TokenNew
{
    public int Line { get; }
    public int Column { get; }
    public int Start { get; }
    public string RawText { get; }
    public TokenType TokenType { get; }

    public TokenNew(int line, int column, int start, string rawText, TokenType tokenType)
    {
        Line = line;
        Column = column;
        Start = start;
        RawText = rawText;
        TokenType = tokenType;
    }
}

public abstract class Token
{
    private readonly int _line;
    private readonly int _col;
    public abstract string Print();
    public abstract string TokenType();

    protected Token(int line, int col)
    {
        _line = line;
        _col = col;
    }

    public int Line => _line;
    public int Column => _col;
}

public class LeftBrace : Token
{
    public LeftBrace(int line, int col) : base(line, col)
    {
    }

    public override string Print() => "[";
    public override string TokenType() => "Left Brace";
}

public class RightBrace : Token
{
    public RightBrace(int line, int col) : base(line, col)
    {
    }

    public override string Print() => "]";
    public override string TokenType() => "Right Brace";
}

public class LeftParen : Token
{
    public LeftParen(int line, int col) : base(line, col)
    {
    }

    public override string Print() => "(";
    public override string TokenType() => "Left Paren";
}

public class RightParen : Token
{
    public RightParen(int line, int col) : base(line, col)
    {
    }

    public override string Print() => ")";
    public override string TokenType() => "Right Paren";
}

public class Eof : Token
{
    public Eof(int line, int col) : base(line, col)
    {
    }

    public override string Print() => "EOF";
    public override string TokenType() => "End of File";
}

public class Minus : Token
{
    public Minus(int line, int col) : base(line, col)
    {
    }

    public override string Print() => "-";
    public override string TokenType() => "Minus";
}

public class Plus : Token
{
    public Plus(int line, int col) : base(line, col)
    {
    }

    public override string Print() => "+";

    public override string TokenType()
    {
        return "Plus";
    }
}

public class ErrorToken : Token
{
    private readonly string _message;

    public ErrorToken(string message, int line, int col) : base(line, col)
    {
        _message = message;
    }

    public override string Print()
    {
        return $"ERROR: {_message}";
    }

    public override string TokenType()
    {
        return "ERROR";
    }
}

public class IntToken
{
    private readonly TokenNew _token;
    public readonly int IntVal;

    public IntToken(TokenNew token)
    {
        _token = token;
        IntVal = int.Parse(token.RawText);
    }
}

public class DoubleToken : Token
{
    private readonly string _token;
    private readonly double _d;

    public DoubleToken(string token, double d, int line, int col) : base(line, col)
    {
        _token = token;
        _d = d;
    }
    public override string Print()
    {
        return _token;
    }

    public override string TokenType()
    {
        return "Double";
    }
}

public class LiteralToken
{
    private readonly string _text;

    public LiteralToken(string text)
    {
        _text = text;
    }

    public string Text() => _text;
}

public class IfToken : Token
{
    public IfToken(int line, int col) : base(line, col)
    {
    }

    public override string Print() => "if";
    public override string TokenType() => "if";
}

public class LoopToken : Token
{
    public LoopToken(int line, int col) : base(line, col)
    {

    }

    public override string Print() => "loop";

    public override string TokenType() => "loop";
}

public class MShellObject : OneOfBase<LiteralToken, MShellList, MShellQuotation, IntToken, MShellBool, MShellString, MShellPipe>
{
    protected MShellObject(OneOf<LiteralToken, MShellList, MShellQuotation, IntToken, MShellBool, MShellString, MShellPipe> input) : base(input)
    {
    }

    public string TypeName()
    {
        return Match(
            literalToken => "Literal",
            list => "List",
            quotation => "Quotation",
            token => "Integer",
            boolVal => "Boolean",
            stringVal => "String",
            pipe => "Pipeline"

        );
    }

    public bool IsCommandLineable()
    {
        return Match(
            token => true,
            list => false,
            quotation => false,
            intToken => true,
            boolVal => false,
            stringVal => true,
            pipe => false
        );
    }

    public bool IsNumeric()
    {
         return Match(
             token => false,
             list => false,
             quotation => false,
             intToken => true,
             boolVal => false,
             stringVal => false,
             pipe => false
         );
    }

    public double FloatNumeric()
    {
        return Match(
            token => throw new NotImplementedException(),
            list => throw new NotImplementedException(),
            quotation => throw new NotImplementedException(),
            intToken => (double)intToken.IntVal,
            boolVal => throw new NotImplementedException(),
            stringVal => throw new NotImplementedException(),
            pipe => throw new NotImplementedException()
        );
    }

    public string CommandLine()
    {
        return Match(
            token => token.Text(),
            list => throw new NotImplementedException(),
            quotation => throw new NotImplementedException(),
            intToken => intToken.IntVal.ToString(),
            boolVal => throw new NotImplementedException(),
            stringVal => stringVal.Content,
            pipe => throw new NotImplementedException()
        );
    }

    public bool IsLiteralToken => IsT0;
    public bool IsList => IsT1;
    public bool IsQuotation => IsT2;
    public bool IsIntToken => IsT3;
    public bool IsBool => IsT4;
    public bool IsString => IsT5;

    public LiteralToken AsLiteralToken => AsT0;
    public MShellList AsList => AsT1;
    public MShellQuotation AsQuotation => AsT2;
    public IntToken AsIntToken => AsT3;
    public MShellBool AsMShellBool => AsT4;
    public MShellString AsMShellString => AsT5;


    public static implicit operator MShellObject(LiteralToken t) => new(t);
    public static explicit operator LiteralToken(MShellObject t) => t.AsT0;

    public bool TryPickLiteral(out LiteralToken l) => TryPickT0(out l, out _);
    public bool TryPickLiteral(out LiteralToken l, out OneOf<MShellList, MShellQuotation, IntToken, MShellBool, MShellString, MShellPipe> remainder) => TryPickT0(out l, out remainder);

    public static implicit operator MShellObject(MShellList t) => new(t);
    public static explicit operator MShellList(MShellObject t) => t.AsT1;

    public bool TryPickList(out MShellList l) => TryPickT1(out l, out _);
    public bool TryPickList(out MShellList l, out OneOf<LiteralToken, MShellQuotation, IntToken, MShellBool, MShellString, MShellPipe> remainder) => TryPickT1(out l, out remainder);

    public static implicit operator MShellObject(MShellQuotation t) => new(t);
    public static explicit operator MShellQuotation(MShellObject t) => t.AsT2;

    public bool TryPickQuotation(out MShellQuotation l) => TryPickT2(out l, out _);
    public bool TryPickQuotation(out MShellQuotation l, out OneOf<LiteralToken, MShellList, IntToken, MShellBool, MShellString, MShellPipe> remainder) => TryPickT2(out l, out remainder);

    public static implicit operator MShellObject(IntToken t) => new(t);
    public static explicit operator IntToken(MShellObject t) => t.AsT3;

    public bool TryPickIntToken(out IntToken l) => TryPickT3(out l, out _);
    public bool TryPickIntToken(out IntToken l, out OneOf<LiteralToken, MShellList, MShellQuotation, MShellBool, MShellString, MShellPipe> remainder) => TryPickT3(out l, out remainder);

    public static implicit operator MShellObject(MShellBool t) => new(t);
    public static explicit operator MShellBool(MShellObject t) => t.AsT4;

    public bool TryPickBool(out MShellBool l) => TryPickT4(out l, out _);
    public bool TryPickBool(out MShellBool l, out OneOf<LiteralToken, MShellList, MShellQuotation, IntToken, MShellString, MShellPipe> remainder) => TryPickT4(out l, out remainder);

    public static implicit operator MShellObject(MShellString t) => new(t);
    public static explicit operator MShellString(MShellObject t) => t.AsT5;

    public bool TryPickString(out MShellString l) => TryPickT5(out l, out _);
    public bool TryPickString(out MShellString l, out OneOf<LiteralToken, MShellList, MShellQuotation, IntToken, MShellBool, MShellPipe> remainder) => TryPickT5(out l, out remainder);

    public static implicit operator MShellObject(MShellPipe t) => new(t);
    public static explicit operator MShellPipe(MShellObject t) => t.AsT6;

    public bool TryPickPipe(out MShellPipe l) => TryPickT6(out l, out _);
    public bool TryPickPipe(out MShellPipe l, out OneOf<LiteralToken, MShellList, MShellQuotation, IntToken, MShellBool, MShellString> remainder) => TryPickT6(out l, out remainder);


    public string DebugString()
    {
        return Match(
            token => token.Text(),
            list => "[" + string.Join(", ", list.Items.Select(o => o.DebugString())) + "]",
            quotation => "(" + string.Join(" ", quotation.Tokens.Select(o => o.RawText)) + ")",
            token => token.IntVal.ToString(),
            boolVal => boolVal.Value.ToString(),
            stringVal => stringVal.RawString,
            pipeline => string.Join(" | ", pipeline.List.Items.Select(o => o.DebugString()))
        );
    }

}

public class MShellBool
{
    public bool Value { get; }

    public MShellBool(bool value)
    {
        Value = value;
    }
}

public class MShellQuotation
{
    private int StartIndex { get; }
    private int EndIndexExc { get; }
    public List<TokenNew> Tokens { get; }

    public string? StandardInputFile { get; set; } = null;
    public string? StandardOutputFile { get; set; }= null;

    public ExecuteContext Context => new()
    {
        StandardOutput = StandardOutputFile,
        StandardInput = StandardInputFile,
    };

    public MShellQuotation(List<TokenNew> tokens, int startIndex, int endIndexExc)
    {
        StartIndex = startIndex;
        EndIndexExc = endIndexExc;
        Tokens = tokens;
    }
}

public class MShellList
{
    public string? StandardOutFile { get; set; }
    public string? StandardInputFile { get; set; }
    public readonly List<MShellObject> Items;

    public MShellList(IEnumerable<MShellObject> items)
    {
        Items = items.ToList();
    }
}

public class MShellString
{
    public string Content { get; }
    public string RawString { get; }

    public MShellString(TokenNew stringToken)
    {
        string rawText = stringToken.RawText;
        RawString = rawText;
        Content = ParseRawToken(rawText);
    }

    public MShellString(string inputStr)
    {
        Content = inputStr;
        RawString = inputStr;
    }

    private string ParseRawToken(string inputString)
    {
        if (inputString.Length < 2)
        {
            throw new ArgumentException($"Input string should have minimum length of 2 for surrounding double quotes\n");
        }

        StringBuilder b = new();
        int index = 1;

        bool inEscape = false;
        while (index < inputString.Length - 1)
        {
            if (inEscape)
            {
                char c = inputString[index];
                if (c == 'n') b.Append('\n');
                else if (c == 't') b.Append('\t');
                else if (c == 'r') b.Append('\r');
                else if (c == '\\') b.Append('\\');
                else if (c == '"') b.Append('"');
                else
                {
                    throw new ArgumentException($"Invalid escape character '{c}'\n");
                }
                inEscape = false;
            }
            else {
                char c = inputString[index];
                if (c == '\\') inEscape = true;
                else b.Append(c);
            }

            index++;
        }

        return b.ToString();
    }
}

public class MShellPipe
{
    public MShellList List { get; }

    public MShellPipe(MShellList list)
    {
        List = list;
    }
}

public class EvalResult
{
    public bool Success {get;}

    // -1 for no break encountered
    public int BreakNum { get; }

    public EvalResult(bool success, int breakNum)
    {
        Success = success;
        BreakNum = breakNum;
    }
}
public class ExecuteContext()
{
    public string? StandardInput { get; set; } = null;
    public string? StandardOutput { get; set; } = null;

    public override string ToString() => $"stdin: '{StandardInput}'\nstdout: '{StandardOutput}'\n";
}
