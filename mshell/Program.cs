namespace mshell;

class Program
{
    static void Main(string[] args)
    {
        string input = Console.In.ReadToEnd();

        Lexer l = new Lexer(input);

        var tokens = l.Tokenize();

        foreach (var t in tokens)
        {
            Console.Write($"{t.TokenType()}: {t.Print()}\n");
        }

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
            if (!char.IsWhiteSpace(c)) Advance();
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
