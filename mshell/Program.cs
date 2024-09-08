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
        string input = args.Length > 0 ? File.ReadAllText(args[0], Encoding.UTF8) : Console.In.ReadToEnd();

        Lexer l = new Lexer(input);

        var tokens = l.Tokenize();

        Evaluator e = new(false);
        bool success = e.Evaluate(tokens, new Stack<MShellObject>());

        return success ? 0 : 1;

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
            default:
                return ParseLiteralOrNumber();
        }
    }

    private TokenNew ParseLiteralOrNumber()
    {
        while (true)
        {
            char c = Peek();
            if (!char.IsWhiteSpace(c) && c != ']' && c != ')') Advance();
            else break;
        }

        string literal = _input.Substring(_start, _current - _start);

        if (literal == "-") return MakeToken(TokenType.MINUS);
        if (literal == "if") return MakeToken(TokenType.IF);

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

    public bool Evaluate(List<TokenNew> tokens, Stack<MShellObject> stack)
    {
        int index = 0;

        // This is a stack of left parens for tracking nested quotations.
        Stack<int> quotationStack = new();
        Stack<int> leftSquareBracketStack = new();

        while (index < tokens.Count)
        {
            TokenNew t = tokens[index];
            if (t.TokenType == TokenType.EOF) return true;

            if (t.TokenType == TokenType.LITERAL)
            {
                _push(new LiteralToken(t), stack);
                // stack.Push(lt);
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
                        Console.Error.Write($"{currToken.Line}:{currToken.Column}: Found unbalanced bracket.\n");
                        return false;
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
                                Evaluate(tokensWithinList, listStack);
                                MShellList l = new(listStack.Reverse());
                                _push(l, stack);
                                // stack.Push(l);
                                break;
                            }

                            index++;
                        }
                        else
                        {
                            Console.Error.Write($"{currToken.Line}:{currToken.Column}: Found unbalanced square bracket.\n");
                            return false;
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
                Console.Error.Write("Found unbalanced list.\n");
                return false;
            }
            else if (t.TokenType == TokenType.LEFT_PAREN)
            {
                quotationStack.Push(index);
                index++;

                while (true)
                {
                    if (index >= tokens.Count || tokens[index].TokenType == TokenType.EOF)
                    {
                        Console.Error.Write("Found unbalanced bracket.\n");
                        return false;
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
                            }
                        }
                        else
                        {
                            Console.Error.Write("Found unbalanced quotation.\n");
                            return false;
                        }

                        break;
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
                Console.Error.Write("Unbalanced parenthesis found.\n");
                return false;
            }
            else if (t.TokenType == TokenType.IF)
            {
                if (stack.TryPop(out var o))
                {
                    if (o.TryPickList(out var qList))
                    {
                        if (qList.Items.Count < 2)
                        {
                            Console.Error.Write("Quotation list for if should have a minimum of 2 elements.\n");
                            return false;
                        }

                        if (qList.Items.Any(i => !i.IsQuotation))
                        {
                            Console.Error.Write("All items within list for if are required to be quotations. Received:\n");
                            foreach (var i in qList.Items)
                            {
                                Console.Error.Write(i.TypeName());
                                Console.Error.Write('\n');
                            }

                            return false;
                        }

                        // Loop through the even index quotations, looking for the first one that has a true condition.
                        var trueIndex = -1;
                        for (int i = 0; i < qList.Items.Count - 1; i += 2)
                        {
                            MShellQuotation q = qList.Items[i].AsT2;
                            Evaluate(q.Tokens, stack);
                            if (stack.TryPop(out var condition))
                            {
                                if (condition.TryPickIntToken(out var intVal))
                                {
                                    if (intVal.IntVal == 0)
                                    {
                                        trueIndex = i;
                                        break;
                                    }
                                }
                                else
                                {
                                    Console.Error.Write($"Can't evaluate condition for type.\n");
                                }
                            }
                            else
                            {
                                Console.Error.Write("Evaluation of condition quotation removed all stacks.");
                                return false;
                            }
                        }

                        if (trueIndex > -1)
                        {
                            // Run the quotation on the index after the true index
                            if (!qList.Items[trueIndex + 1].IsQuotation)
                            {
                                Console.Error.Write($"True branch of if statement must be quotation. Received a {qList.Items[trueIndex + 1].TypeName()}");
                                return false;
                            }

                            MShellQuotation q = qList.Items[trueIndex + 1].AsQuotation;
                            Evaluate(q.Tokens, stack);
                        }
                        else if (qList.Items.Count % 2 == 1)
                        {
                            // Run the last quotation if there was no true condition.
                            if (!qList.Items[^1].IsQuotation)
                            {
                                Console.Error.Write($"Else branch of if statement must be quotation. Received a {qList.Items[^1].TypeName()}");
                                return false;
                            }

                            MShellQuotation q = qList.Items[^1].AsQuotation;
                            Evaluate(q.Tokens, stack);
                        }
                    }
                    else
                    {
                        Console.Error.Write("Argument for if expected to be a list of quotations.\n");
                        return false;
                    }
                }
                else
                {
                     Console.Error.Write("Nothing on stack for if.\n");
                     return false;
                }

                index++;
            }
            else if (t.TokenType == TokenType.EXECUTE)
            {
                if (stack.TryPop(out var arg))
                {
                    arg.Switch(
                        literalToken =>
                        {
                            RunProcess(new MShellList(new List<MShellObject>(1) { literalToken }));
                        },
                        RunProcess,
                        _ => { Console.Error.Write("Cannot execute a quotation.\n"); },
                        _ => { Console.Error.Write("Cannot execute an integer.\n"); }

                    );
                }
                else
                {
                    Console.Error.Write("Nothing on stack to execute.\n");
                    return false;
                }
                index++;
            }
            else if (t.TokenType == TokenType.INTEGER)
            {
                _push(new IntToken(t), stack);
                // stack.Push(iToken);
                index++;
            }
            else
            {
                Console.Error.Write($"Token type '{t.TokenType}' not implemented yet.\n");
                return false;
                // throw new NotImplementedException($"Token type {t.TokenType()} not implemented yet.");
            }
        }

        return true;
    }

    private void ExecuteQuotation(MShellQuotation q)
    {
        int qIndex = 0;
        while (qIndex < q.Tokens.Count)
        {



        }
    }

    public void RunProcess(MShellList list)
    {
        if (list.Items.Any(o => !o.IsT0))
        {
            throw new NotImplementedException("Can't handle a process with anything but literals as arguments for now.");
        }
        else
        {
            List<string> arguments = list.Items.Select(o => o.AsT0.Text()).ToList();

            if (arguments.Count == 0)
            {
                throw new ArgumentException("Cannot execute an empty list");
            }

            ProcessStartInfo info = new ProcessStartInfo()
            {
                FileName = arguments[0],
                UseShellExecute = false,
                RedirectStandardError = false,
                RedirectStandardInput = false,
                RedirectStandardOutput = false,
                CreateNoWindow = true,
            };
            foreach (string arg in arguments.Skip(1)) info.ArgumentList.Add(arg);

            Process p = new Process()
            {
                StartInfo = info
            };

            try
            {
                using (p)
                {
                    p.Start();

                    // string stdout = p.StandardOutput.ReadToEnd();
                    // string stderr = p.StandardError.ReadToEnd();
                    p.WaitForExit();

                    // Console.Out.Write(stdout);
                    // Console.Error.Write(stderr);
                }
            }
            catch (Exception e)
            {
                Console.Error.Write(e.Message);
                throw new Exception("There was an exception running process.");

            }
        }
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
    ERROR
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
    private readonly TokenNew _literal;

    public LiteralToken(TokenNew literal)
    {
        _literal = literal;
    }

    public string Text() => _literal.RawText;
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

public class MShellObject : OneOfBase<LiteralToken, MShellList, MShellQuotation, IntToken>
{
    protected MShellObject(OneOf<LiteralToken, MShellList, MShellQuotation, IntToken> input) : base(input)
    {
    }

    public string TypeName()
    {
        return Match(
            literalToken => "Literal",
            list => "List",
            quotation => "Quotation",
            token => "Integer"
        );
    }

    public bool IsLiteralToken => IsT0;
    public bool IsList => IsT1;
    public bool IsQuotation => IsT2;
    public bool IsIntToken => IsT3;

    public LiteralToken AsLiteralToken => AsT0;
    public MShellList AsList => AsT1;
    public MShellQuotation AsQuotation => AsT2;
    public IntToken AsIntToken => AsT3;


    public static implicit operator MShellObject(LiteralToken t) => new(t);
    public static explicit operator LiteralToken(MShellObject t) => t.AsT0;

    public bool TryPickLiteral(out LiteralToken l) => TryPickT0(out l, out _);
    public bool TryPickLiteral(out LiteralToken l, out OneOf<MShellList, MShellQuotation, IntToken> remainder) => TryPickT0(out l, out remainder);

    public static implicit operator MShellObject(MShellList t) => new(t);
    public static explicit operator MShellList(MShellObject t) => t.AsT1;

    public bool TryPickList(out MShellList l) => TryPickT1(out l, out _);
    public bool TryPickList(out MShellList l, out OneOf<LiteralToken, MShellQuotation, IntToken> remainder) => TryPickT1(out l, out remainder);

    public static implicit operator MShellObject(MShellQuotation t) => new(t);
    public static explicit operator MShellQuotation(MShellObject t) => t.AsT2;

    public bool TryPickQuotation(out MShellQuotation l) => TryPickT2(out l, out _);
    public bool TryPickQuotation(out MShellQuotation l, out OneOf<LiteralToken, MShellList, IntToken> remainder) => TryPickT2(out l, out remainder);

    public static implicit operator MShellObject(IntToken t) => new(t);
    public static explicit operator IntToken(MShellObject t) => t.AsT3;

    public bool TryPickIntToken(out IntToken l) => TryPickT3(out l, out _);
    public bool TryPickIntToken(out IntToken l, out OneOf<LiteralToken, MShellList, MShellQuotation> remainder) => TryPickT3(out l, out remainder);

}

public class MShellQuotation
{
    private int StartIndex { get; }
    private int EndIndexExc { get; }
    public List<TokenNew> Tokens { get; }

    public MShellQuotation(List<TokenNew> tokens, int startIndex, int endIndexExc)
    {
        StartIndex = startIndex;
        EndIndexExc = endIndexExc;
        Tokens = tokens;
    }
}

public class MShellList
{
    public readonly List<MShellObject> Items;

    public MShellList(IEnumerable<MShellObject> items)
    {
        Items = items.ToList();
    }
}
