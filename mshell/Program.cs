using System.Collections.ObjectModel;
using System.Diagnostics;
using System.Text;
using OneOf;

namespace mshell;

class Program
{
    static void Main(string[] args)
    {
        string input = args.Length > 0 ? File.ReadAllText(args[0], Encoding.UTF8) : Console.In.ReadToEnd();

        Lexer l = new Lexer(input);

        var tokens = l.Tokenize();

        Evaluator e = new Evaluator(tokens);

        e.Evaluate();
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
    private int _line = 1;

    private readonly string _input;

    private bool AtEnd() => _current >= _input.Length;

    public Lexer(string input)
    {
        _input = input;
    }

    private char Advance()
    {
        var c = _input[_current];
        _current++;
        return c;
    }

    private char Peek() => _input[_current];

    private char PeekNext() => AtEnd() ? '\0' : _input[_current + 1];

    private Token ScanToken()
    {
        EatWhitespace();
        _start = _current;
        if (AtEnd()) return new Eof();

        char c = Advance();

        switch (c)
        {
            case '[':
                return new LeftBrace();
            case ']':
                return new RightBrace();
            case ';':
                return new Execute();
            default:
                return ParseLiteralOrNumber();
        }

        return new ErrorToken($"Line {_line}: Unexpected character '{c}'");
    }

    private Token ParseLiteralOrNumber()
    {
        while (true)
        {
            char c = Peek();
            if (!char.IsWhiteSpace(c) && c != ']') Advance();
            else break;
        }

        string literal = _input.Substring(_start, _current - _start);

        if (literal == "-") return new Minus();

        if (int.TryParse(literal, out int i)) return new IntToken(literal, i);
        if (double.TryParse(literal, out double d)) return new DoubleToken(literal, d);
        return new LiteralToken(literal);
    }

    public List<Token> Tokenize()
    {
        List<Token> tokens = new();
        while (true)
        {
            var t = ScanToken();
            tokens.Add(t);
            if (t is ErrorToken or Eof) break;
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
    private readonly List<Token> _tokens;

    private int _index = 0;

    public Evaluator(List<Token> tokens)
    {
        _tokens = tokens;
    }

    public void Evaluate()
    {
        _index = 0;

        Stack<Stack<MShellObject>> stack = new();
        stack.Push(new Stack<MShellObject>());

        while (_index <= _tokens.Count)
        {
            Token t = _tokens[_index];
            if (t is Eof) return;

            if (t is LiteralToken lt)
            {
                stack.Peek().Push(lt);
                _index++;
            }
            else if (t is LeftBrace)
            {
                stack.Push(new Stack<MShellObject>());
                _index++;
            }
            else if (t is RightBrace)
            {
                if (stack.TryPop(out var outerStack))
                {
                    var list = new MShellList(outerStack.Reverse());

                    if (stack.TryPeek(out var currentStack))
                    {
                        currentStack.Push(list);
                    }
                    else
                    {
                        Console.Error.Write("Found unbalanced list.\n");
                    }
                }
                else
                {
                    Console.Error.Write($"Found Unbalanced list.");
                }
                _index++;
            }
            else if (t is Execute)
            {
                if (stack.Peek().TryPeek(out var arg))
                {
                    arg.Switch(
                        literalToken =>
                        {
                            RunProcess(new MShellList(new List<MShellObject>(1) { literalToken }));
                        },
                        RunProcess
                    );
                }
                _index++;
            }
            else
            {
                Console.Error.Write($"Token type {t.TokenType()} not implemented yet.\n");
                return;
                // throw new NotImplementedException($"Token type {t.TokenType()} not implemented yet.");
            }
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
            List<string> arguments = list.Items.Select(o => o.AsT0.Print()).ToList();

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
    public override string Print() => ";";

    public override string TokenType() => "Execute";
}

public abstract class Token
{
    public abstract string Print();
    public abstract string TokenType();
}

public class LeftBrace : Token
{
    public override string Print() => "[";
    public override string TokenType() => "Left Brace";
}

public class RightBrace : Token
{
    public override string Print() => "]";
    public override string TokenType() => "Right Brace";
}

public class Eof : Token
{
    public override string Print() => "EOF";
    public override string TokenType() => "End of File";
}

public class Minus : Token
{
    public override string Print() => "-";
    public override string TokenType() => "Minus";
}

public class Plus : Token
{
    public override string Print() => "+";

    public override string TokenType()
    {
        return "Plus";
    }
}

public class ErrorToken : Token
{
    private readonly string _message;

    public ErrorToken(string message)
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

public class IntToken : Token
{
    private readonly string _token;
    private readonly int _intVal;

    public IntToken(string token, int intVal)
    {
        _token = token;
        _intVal = intVal;
    }

    public override string Print()
    {
        return _token;
    }

    public override string TokenType()
    {
        return "Integer";
    }
}

public class DoubleToken : Token
{
    private readonly string _token;
    private readonly double _d;

    public DoubleToken(string token, double d)
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

public class LiteralToken : Token
{
    private readonly string _token;

    public LiteralToken(string token)
    {
        _token = token;
    }

    public override string Print()
    {
        return _token;
    }

    public override string TokenType()
    {
        return "Literal";
    }
}

public class MShellObject : OneOfBase<LiteralToken, MShellList>
{
    protected MShellObject(OneOf<LiteralToken, MShellList> input) : base(input)
    {
    }

    public static implicit operator MShellObject(LiteralToken t) => new(t);
    public static explicit operator LiteralToken(MShellObject t) => t.AsT0;

    public static implicit operator MShellObject(MShellList t) => new(t);
    public static explicit operator MShellList(MShellObject t) => t.AsT1;

}

public class MShellList
{
    public readonly List<MShellObject> Items;

    public MShellList(IEnumerable<MShellObject> items)
    {
        Items = items.ToList();
    }
}
