//! The mshell lexer, ported from `mshell/Lexer.go` for the GUI's line editor
//! (ai/gui-terminal.typ, "The Rust port").
//!
//! It gives the same token kinds and positions as the Go lexer; `msh --lex`
//! is the reference, and `tools/lexcompare.py` checks every case of a corpus
//! against it. It doesn't decode literal values or build error messages: an
//! error is a token of kind `Error` at the place the Go lexer reports one.
//!
//! The input is a `&str`, so it is valid UTF-8, and the Go lexer's
//! invalid-UTF-8 error never arises.
//!
//! Positions follow Go: lines and columns are one-based and count code
//! points, as does `start`. Byte offsets are kept as well, for slicing.
//!
//! No allocation on the keystroke path: `Lexer` keeps its interpolation
//! stack between calls, and tokens go into a caller's `Vec`.

use crate::unicode_tables::{DIGIT, WHITE_SPACE};

#[derive(Clone, Copy, PartialEq, Eq, Debug, Hash)]
#[repr(u8)]
pub enum TokenKind {
    Eof,
    Error,
    LeftSquareBracket,
    RightSquareBracket,
    LeftParen,
    RightParen,
    Execute,
    Pipe,
    Question,
    Positional,
    String,
    UnfinishedString,
    UnfinishedFormatString,
    SingleQuoteString,
    UnfinishedSingleQuoteString,
    Minus,
    Plus,
    Equals,
    FatArrow,
    Interpret,
    If,
    Iff,
    Loop,
    Read,
    Str,
    Break,
    Continue,
    Not,
    GreaterThanOrEqual,
    LessThanOrEqual,
    LessThan,
    GreaterThan,
    True,
    False,
    VarRetrieve,
    VarStore,
    EnvRetrieve,
    EnvStore,
    EnvCheck,
    Integer,
    Float,
    Literal,
    Indexer,
    EndIndexer,
    StartIndexer,
    SliceIndexer,
    TildeExpansion,
    StopOnError,
    Def,
    End,
    StderrRedirect,
    StderrAppend,
    TypeInt,
    TypeFloat,
    TypeBool,
    DoubleDash,
    Ampersand,
    Path,
    UnfinishedPath,
    Comma,
    DateTime,
    FormatString,
    FormatStringStart,
    FormatStringMid,
    FormatStringEnd,
    LeftCurly,
    RightCurly,
    Colon,
    MatchArmDup,
    NotEqual,
    Bang,
    StdAppend,
    Whitespace,
    LineComment,
    Asterisk,
    AsteriskBinary,
    Caret,
    CaretBinary,
    Else,
    ElseStar,
    StarIf,
    StdoutAndStderrRedirect,
    StdoutAndStderrAppend,
    InPlaceRedirect,
    GridOpen,
    GridClose,
    PrefixQuote,
    Match,
    Ver,
    StderrToStdout,
    StdoutToStderr,
    As,
    Type,
    Enum,
    Try,
    FailKeyword,
    Pure,
    TryAs,
    Rune,
    UnfinishedRune,
}

impl TokenKind {
    /// The name `msh --lex` prints (Go's `TokenType.String`).
    pub fn name(self) -> &'static str {
        use TokenKind::*;
        match self {
            Eof => "EOF",
            Error => "ERROR",
            LeftSquareBracket => "LEFT_SQUARE_BRACKET",
            RightSquareBracket => "RIGHT_SQUARE_BRACKET",
            LeftParen => "LEFT_PAREN",
            RightParen => "RIGHT_PAREN",
            Execute => "EXECUTE",
            Pipe => "PIPE",
            Question => "QUESTION",
            Positional => "POSITIONAL",
            String => "STRING",
            UnfinishedString => "UNFINISHEDSTRING",
            UnfinishedFormatString => "UNFINISHEDFORMATSTRING",
            SingleQuoteString => "SINGLEQUOTESTRING",
            UnfinishedSingleQuoteString => "UNFINISHEDSINGLEQUOTESTRING",
            Minus => "MINUS",
            Plus => "PLUS",
            Equals => "EQUALS",
            FatArrow => "FATARROW",
            Interpret => "INTERPRET",
            If => "IF",
            Iff => "IFF",
            Loop => "LOOP",
            Read => "READ",
            Str => "STR",
            Break => "BREAK",
            Continue => "CONTINUE",
            Not => "NOT",
            GreaterThanOrEqual => "GREATERTHANOREQUAL",
            LessThanOrEqual => "LESSTHANOREQUAL",
            LessThan => "LESSTHAN",
            GreaterThan => "GREATERTHAN",
            True => "TRUE",
            False => "FALSE",
            VarRetrieve => "VARRETRIEVE",
            VarStore => "VARSTORE",
            EnvRetrieve => "ENVRETREIVE",
            EnvStore => "ENVSTORE",
            EnvCheck => "ENVCHECK",
            Integer => "INTEGER",
            Float => "FLOAT",
            Literal => "LITERAL",
            Indexer => "INDEXER",
            EndIndexer => "ENDINDEXER",
            StartIndexer => "STARTINDEXER",
            SliceIndexer => "SLICEINDEXER",
            TildeExpansion => "TILDEEXPANSION",
            StopOnError => "STOP_ON_ERROR",
            Def => "DEF",
            End => "END",
            StderrRedirect => "STDERRREDIRECT",
            StderrAppend => "STDERRAPPEND",
            TypeInt => "TYPEINT",
            TypeFloat => "TYPEFLOAT",
            TypeBool => "TYPEBOOL",
            DoubleDash => "DOUBLEDASH",
            Ampersand => "AMPERSAND",
            Path => "PATH",
            UnfinishedPath => "UNFINISHEDPATH",
            Comma => "COMMA",
            DateTime => "DATETIME",
            FormatString => "FORMATSTRING",
            FormatStringStart => "FORMATSTRINGSTART",
            FormatStringMid => "FORMATSTRINGMID",
            FormatStringEnd => "FORMATSTRINGEND",
            LeftCurly => "LEFT_CURLY",
            RightCurly => "RIGHT_CURLY",
            Colon => "COLON",
            MatchArmDup => "MATCHARMDUP",
            NotEqual => "NOTEQUAL",
            Bang => "BANG",
            StdAppend => "STDAPPEND",
            Whitespace => "WHITESPACE",
            LineComment => "LINECOMMENT",
            Asterisk => "ASTERISK",
            AsteriskBinary => "ASTERISKBINARY",
            Caret => "CARET",
            // Sic: Go prints it with a space.
            CaretBinary => "CARET BINARY",
            Else => "ELSE",
            ElseStar => "ELSESTAR",
            StarIf => "STARIF",
            StdoutAndStderrRedirect => "STDOUTANDSTDERRREDIRECT",
            StdoutAndStderrAppend => "STDOUTANDSTDERRAPPEND",
            InPlaceRedirect => "INPLACEREDIRECT",
            GridOpen => "GRID_OPEN",
            GridClose => "GRID_CLOSE",
            PrefixQuote => "PREFIXQUOTE",
            Match => "MATCH",
            Ver => "VER",
            StderrToStdout => "STDERRTOSTDOUT",
            StdoutToStderr => "STDOUTTOSTDERR",
            As => "AS",
            Type => "TYPE",
            Enum => "ENUM",
            Try => "TRY",
            FailKeyword => "FAIL_KEYWORD",
            Pure => "PURE",
            TryAs => "TRYAS",
            Rune => "RUNE",
            UnfinishedRune => "UNFINISHEDRUNE",
        }
    }
}

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub struct Token {
    pub kind: TokenKind,
    /// One-based line.
    pub line: u32,
    /// One-based column, in code points.
    pub col: u32,
    /// Code point offset of the token in the input (Go's `Token.Start`).
    pub start: u32,
    /// Byte range of the token's text in the input. For an error, the text
    /// the Go lexer had scanned when it stopped.
    pub byte_start: u32,
    pub byte_end: u32,
}

impl Token {
    pub fn text<'a>(&self, input: &'a str) -> &'a str {
        &input[self.byte_start as usize..self.byte_end as usize]
    }
}

/// Go's `unicode.IsDigit`.
pub fn is_digit(c: char) -> bool {
    if (c as u32) <= 0xFF {
        return c.is_ascii_digit();
    }
    in_table(DIGIT, c)
}

/// Go's `unicode.IsSpace`.
pub fn is_space(c: char) -> bool {
    match c as u32 {
        0x09..=0x0D | 0x20 | 0x85 | 0xA0 => true,
        0..=0xFF => false,
        _ => in_table(WHITE_SPACE, c),
    }
}

fn in_table(table: &[(u32, u32)], c: char) -> bool {
    let c = c as u32;
    table
        .binary_search_by(|&(lo, hi)| {
            if hi < c {
                std::cmp::Ordering::Less
            } else if lo > c {
                std::cmp::Ordering::Greater
            } else {
                std::cmp::Ordering::Equal
            }
        })
        .is_ok()
}

/// ASCII characters a literal can't hold (Go's `notAllowedLiteralChars`).
fn not_allowed_literal(c: char) -> bool {
    matches!(
        c,
        '[' | ']' | '(' | ')' | '{' | '}' | '<' | '>' | ':' | ';' | '?' | '!' | '@' | ',' | '*' | '&' | '|' | '"' | '\''
            | '\0' | '^'
    )
}

/// Go's `literalStop`, for ASCII.
const LITERAL_STOP: [bool; 128] = {
    let mut stop = [false; 128];
    let mut c = 0;
    while c < 128 {
        let ch = c as u8 as char;
        stop[c] = matches!(c, 0x09..=0x0D | 0x20)
            || matches!(
                ch,
                '[' | ']' | '(' | ')' | '{' | '}' | '<' | '>' | ':' | ';' | '?' | '!' | '@' | ',' | '*' | '&' | '|'
                    | '"' | '\'' | '\0' | '^'
            );
        c += 1;
    }
    stop
};

/// Go's `isAllowedLiteral`.
pub fn is_allowed_literal(c: char) -> bool {
    if c.is_ascii() {
        return !LITERAL_STOP[c as usize];
    }
    !is_space(c) && !not_allowed_literal(c)
}

fn is_hex_digit(c: char) -> bool {
    c.is_ascii_hexdigit()
}

fn base_from_prefix(c: char) -> Option<u32> {
    match c {
        'o' | 'O' => Some(8),
        'x' | 'X' => Some(16),
        'b' | 'B' => Some(2),
        _ => None,
    }
}

fn is_base_digit(base: u32, c: char) -> bool {
    match base {
        8 => ('0'..='7').contains(&c),
        16 => c.is_ascii_hexdigit(),
        2 => c == '0' || c == '1',
        _ => false,
    }
}

/// Go's `escapedRune`: whether `\c` is an escape a double-quoted string allows.
fn is_escape(c: char) -> bool {
    matches!(c, 'e' | 'n' | 't' | 'r' | '\\' | '"' | '{' | '}')
}

/// Whether Go's `numberLexemeError` would reject this number, index or
/// positional token: the evaluator couldn't read it.
fn number_error(kind: TokenKind, lexeme: &str) -> bool {
    use TokenKind::*;
    let atoi = |s: &str| s.parse::<i64>().is_err();
    match kind {
        Integer => parse_int_literal(lexeme).is_none(),
        // Overflow is an error in Go's ParseFloat, underflow to zero isn't.
        Float => lexeme.parse::<f64>().map_or(true, |v| v.is_infinite()),
        Indexer => atoi(&lexeme[1..lexeme.len() - 1]),
        EndIndexer | Positional => atoi(&lexeme[1..]),
        StartIndexer => atoi(&lexeme[..lexeme.len() - 1]),
        SliceIndexer => {
            let (start, end) = lexeme.split_once(':').unwrap_or((lexeme, ""));
            atoi(start) || atoi(end)
        }
        _ => false,
    }
}

/// Go's `parseIntLiteral`, which strips the sign before parsing, so
/// -9223372036854775808 doesn't fit.
fn parse_int_literal(s: &str) -> Option<i64> {
    let (neg, mut body) = match s.strip_prefix('-') {
        Some(rest) => (true, rest),
        None => (false, s),
    };
    let mut base = 10;
    let b = body.as_bytes();
    if b.len() > 2 && b[0] == b'0' {
        if let Some(prefix_base) = base_from_prefix(b[1] as char) {
            base = prefix_base;
            body = &body[2..];
        }
    }
    let v = i64::from_str_radix(body, base).ok()?;
    Some(if neg { -v } else { v })
}

/// A reusable lexer. Settings are the Go lexer's fields of the same names.
#[derive(Default)]
pub struct Lexer {
    /// An unterminated string, path or rune is a token of its own
    /// (`Unfinished*`) rather than an error: the line being typed.
    pub allow_unterminated: bool,
    pub emit_whitespace: bool,
    pub emit_comments: bool,
    format_depths: Vec<u32>,
}

impl Lexer {
    /// The settings of the interactive line editor (Go's `lineTokens`).
    pub fn editor() -> Lexer {
        Lexer { allow_unterminated: true, emit_whitespace: true, emit_comments: true, format_depths: Vec::new() }
    }

    /// Lex `input` into `out` (cleared first), up to and including EOF or the
    /// first error, as Go's `TokenizeInto`. Returns whether it ended in an
    /// error.
    pub fn tokenize_into(&mut self, input: &str, out: &mut Vec<Token>) -> bool {
        out.clear();
        self.format_depths.clear();
        let mut s = Scanner {
            input,
            bytes: input.as_bytes(),
            start: 0,
            current: 0,
            rune_start: 0,
            rune_current: 0,
            col: 0,
            start_col: 0,
            line: 1,
            start_line: 1,
            allow_unterminated: self.allow_unterminated,
            depths: &mut self.format_depths,
        };
        loop {
            let t = s.scan_token(self.emit_whitespace, self.emit_comments);
            out.push(t);
            match t.kind {
                TokenKind::Error => return true,
                TokenKind::Eof => return false,
                _ => {}
            }
        }
    }
}

struct Scanner<'a, 'd> {
    input: &'a str,
    bytes: &'a [u8],
    start: usize,
    current: usize,
    rune_start: u32,
    rune_current: u32,
    col: u32,
    start_col: u32,
    line: u32,
    start_line: u32,
    allow_unterminated: bool,
    depths: &'d mut Vec<u32>,
}

impl Scanner<'_, '_> {
    fn at_end(&self) -> bool {
        self.current >= self.bytes.len()
    }

    /// Code points in the token so far.
    fn cur_len(&self) -> u32 {
        self.rune_current - self.rune_start
    }

    fn cur_lexeme(&self) -> &str {
        &self.input[self.start..self.current]
    }

    fn make(&self, kind: TokenKind) -> Token {
        let kind = if number_error(kind, self.cur_lexeme()) { TokenKind::Error } else { kind };
        Token {
            kind,
            line: self.start_line,
            col: self.start_col + 1,
            start: self.rune_start,
            byte_start: self.start as u32,
            byte_end: self.current as u32,
        }
    }

    fn error(&self) -> Token {
        self.make(TokenKind::Error)
    }

    fn rune_at(&self, i: usize) -> (char, usize) {
        match self.input.get(i..).and_then(|s| s.chars().next()) {
            Some(c) => (c, c.len_utf8()),
            None => ('\0', 0),
        }
    }

    fn advance(&mut self) -> char {
        self.col += 1;
        self.rune_current += 1;
        let b = self.bytes[self.current];
        if b < 0x80 {
            self.current += 1;
            return b as char;
        }
        let (c, size) = self.rune_at(self.current);
        self.current += size;
        c
    }

    /// Advance over the next n bytes, which hold no newline.
    fn skip(&mut self, n: usize) {
        let runes = self.input[self.current..self.current + n].chars().count() as u32;
        self.current += n;
        self.col += runes;
        self.rune_current += runes;
    }

    fn peek(&self) -> char {
        self.rune_at(self.current).0
    }

    fn peek_next(&self) -> char {
        self.peek_at(1)
    }

    fn peek_at(&self, offset: usize) -> char {
        let mut i = self.current;
        for _ in 0..offset {
            let (_, size) = self.rune_at(i);
            if size == 0 {
                return '\0';
            }
            i += size;
        }
        self.rune_at(i).0
    }

    fn handle_newline(&mut self) {
        self.line += 1;
        self.col = 0;
    }

    /// Byte offset, from `current`, of the first of `set` (ASCII), if any.
    fn index_any(&self, set: &[u8]) -> Option<usize> {
        self.bytes[self.current..].iter().position(|b| set.contains(b))
    }

    fn scan_literal_chars(&mut self) {
        while self.current < self.bytes.len() {
            let b = self.bytes[self.current];
            if b < 0x80 {
                if LITERAL_STOP[b as usize] {
                    return;
                }
                self.current += 1;
                self.col += 1;
                self.rune_current += 1;
                continue;
            }
            let (c, size) = self.rune_at(self.current);
            if !is_allowed_literal(c) {
                return;
            }
            self.current += size;
            self.col += 1;
            self.rune_current += 1;
        }
    }

    fn scan_token(&mut self, emit_whitespace: bool, emit_comments: bool) -> Token {
        loop {
            let t = self.scan_token_all();
            match t.kind {
                TokenKind::Whitespace if !emit_whitespace => continue,
                TokenKind::LineComment if !emit_comments => continue,
                _ => return t,
            }
        }
    }

    fn scan_token_all(&mut self) -> Token {
        use TokenKind::*;
        self.start = self.current;
        self.rune_start = self.rune_current;
        self.start_line = self.line;
        self.start_col = self.col;

        if self.at_end() {
            return self.make(Eof);
        }

        let c = self.advance();
        match c {
            ' ' | '\t' | '\r' | '\x0B' | '\x0C' | '\u{85}' | '\u{A0}' | '\n' => {
                if c == '\n' {
                    self.handle_newline();
                }
                loop {
                    match self.peek() {
                        ' ' | '\t' | '\r' | '\x0B' | '\x0C' | '\u{85}' | '\u{A0}' => {
                            self.advance();
                        }
                        '\n' => {
                            self.advance();
                            self.handle_newline();
                        }
                        // At the end, peek is '\0'.
                        _ => return self.make(Whitespace),
                    }
                }
            }
            '"' => self.parse_string(),
            '`' => self.parse_path(),
            '#' => {
                let n = self.index_any(b"\n").unwrap_or(self.bytes.len() - self.current);
                self.skip(n);
                self.make(LineComment)
            }
            '0'..='9' => self.parse_number_or_start_indexer(),
            '\'' => self.parse_single_quote_string(),
            'r' => {
                if self.peek() == '\'' {
                    self.parse_rune()
                } else {
                    self.parse_literal_or_keyword()
                }
            }
            'U' => {
                if self.peek() == '+' && is_hex_digit(self.peek_next()) {
                    self.parse_code_point()
                } else {
                    self.parse_literal_or_keyword()
                }
            }
            '[' => {
                if self.peek() == '|' {
                    self.advance();
                    return self.make(GridOpen);
                }
                self.make(LeftSquareBracket)
            }
            ']' => self.make(RightSquareBracket),
            '(' => self.make(LeftParen),
            ')' => self.make(RightParen),
            '{' => {
                if let Some(top) = self.depths.last_mut() {
                    *top += 1;
                }
                self.make(LeftCurly)
            }
            '}' => {
                if let Some(top) = self.depths.last_mut() {
                    if *top == 0 {
                        return self.scan_format_chunk(true);
                    }
                    *top -= 1;
                }
                self.make(RightCurly)
            }
            ';' => self.make(Execute),
            '|' => {
                if self.peek() == ']' {
                    self.advance();
                    return self.make(GridClose);
                }
                self.make(Pipe)
            }
            '?' => self.make(Question),
            '$' => {
                let p = self.peek();
                if is_digit(p) {
                    self.parse_positional()
                } else if p == '"' {
                    self.advance();
                    self.scan_format_chunk(false)
                } else if is_allowed_literal(p) {
                    self.parse_env_var()
                } else {
                    self.parse_literal_or_keyword()
                }
            }
            '=' => {
                if self.peek() == '>' {
                    self.advance();
                    return self.make(FatArrow);
                }
                self.make(Equals)
            }
            ',' => self.make(Comma),
            '&' => {
                if self.peek() == '>' {
                    self.advance();
                    if self.peek() == '>' {
                        self.advance();
                        return self.make(StdoutAndStderrAppend);
                    }
                    return self.make(StdoutAndStderrRedirect);
                }
                self.make(Ampersand)
            }
            '<' => match self.peek() {
                '>' => {
                    self.advance();
                    self.make(InPlaceRedirect)
                }
                '=' => {
                    self.advance();
                    self.make(LessThanOrEqual)
                }
                _ => self.make(LessThan),
            },
            '>' => match self.peek() {
                '=' => {
                    self.advance();
                    self.make(GreaterThanOrEqual)
                }
                '>' => {
                    self.advance();
                    self.make(StdAppend)
                }
                _ => self.make(GreaterThan),
            },
            ':' => self.parse_indexer_or_colon(),
            '-' => {
                let p = self.peek();
                if is_digit(p) {
                    // The first digit; the minus is already consumed.
                    self.advance();
                    self.parse_number_or_start_indexer()
                } else if is_space(p) || !is_allowed_literal(p) {
                    self.make(Minus)
                } else {
                    self.parse_literal_or_keyword()
                }
            }
            '@' => {
                self.scan_literal_chars();
                self.make(VarRetrieve)
            }
            '!' => {
                if self.peek() == '=' {
                    self.advance();
                    return self.make(NotEqual);
                }
                self.make(Bang)
            }
            '*' => {
                if self.peek() == 'b' {
                    self.advance();
                    self.make(AsteriskBinary)
                } else if self.peek() == 'i' && self.peek_next() == 'f' {
                    self.advance();
                    self.advance();
                    if !is_allowed_literal(self.peek()) {
                        return self.make(StarIf);
                    }
                    self.parse_literal_or_keyword()
                } else {
                    self.make(Asterisk)
                }
            }
            '^' => {
                if self.peek() == 'b' {
                    self.advance();
                    return self.make(CaretBinary);
                }
                self.make(Caret)
            }
            _ => self.parse_literal_or_keyword(),
        }
    }

    fn parse_literal_or_keyword(&mut self) -> Token {
        self.scan_literal_chars();
        // A literal followed by '!' is a variable store, whatever its name.
        if self.peek() == '!' {
            self.advance();
            return self.make(TokenKind::VarStore);
        }
        let lexeme = self.cur_lexeme();
        if lexeme.len() > 1 && lexeme.ends_with('.') {
            return self.make(TokenKind::PrefixQuote);
        }
        let kind = self.literal_or_keyword_type();
        self.make(kind)
    }

    fn byte(&self, i: usize) -> u8 {
        self.bytes[self.start + i]
    }

    fn literal_or_keyword_type(&mut self) -> TokenKind {
        use TokenKind::*;
        let n = self.cur_len();
        // Each arm either decides or falls through to the end, as Go's switch.
        match self.byte(0) {
            b'-' => {
                if n > 1 {
                    return self.check_keyword(1, "-", DoubleDash);
                }
            }
            b'+' => return self.check_keyword(1, "", Plus),
            b'a' => return self.check_keyword(1, "s", As),
            b'b' => {
                if n > 1 {
                    match self.byte(1) {
                        b'r' => return self.check_keyword(2, "eak", Break),
                        b'o' => return self.check_keyword(2, "ol", TypeBool),
                        _ => {}
                    }
                }
            }
            b'c' => return self.check_keyword(1, "ontinue", Continue),
            b'd' => return self.check_keyword(1, "ef", Def),
            b'e' => {
                if n > 1 {
                    match self.byte(1) {
                        b'l' => {
                            if self.cur_lexeme() == "else" && self.peek() == '*' {
                                self.advance();
                                return ElseStar;
                            }
                            return self.check_keyword(2, "se", Else);
                        }
                        b'n' => {
                            if n > 2 && self.byte(2) == b'u' {
                                return self.check_keyword(3, "m", Enum);
                            }
                            return self.check_keyword(2, "d", End);
                        }
                        _ => {}
                    }
                }
            }
            b'f' => {
                if n > 1 {
                    match self.byte(1) {
                        b'a' => {
                            if n > 2 {
                                match self.byte(2) {
                                    b'l' => return self.check_keyword(3, "se", False),
                                    b'i' => return self.check_keyword(3, "l", FailKeyword),
                                    _ => {}
                                }
                            }
                        }
                        b'l' => return self.check_keyword(2, "oat", TypeFloat),
                        _ => {}
                    }
                }
            }
            b'i' => {
                if n > 1 {
                    match self.byte(1) {
                        b'f' => {
                            if n > 2 && self.byte(2) == b'f' {
                                return self.check_keyword(3, "", Iff);
                            }
                            return self.check_keyword(2, "", If);
                        }
                        b'n' => return self.check_keyword(2, "t", TypeInt),
                        _ => {}
                    }
                }
            }
            b'l' => return self.check_keyword(1, "oop", Loop),
            b'm' => return self.check_keyword(1, "atch", Match),
            b'n' => return self.check_keyword(1, "ot", Not),
            b'p' => return self.check_keyword(1, "ure", Pure),
            b'r' => return self.check_keyword(1, "ead", Read),
            b's' => {
                if n > 1 {
                    match self.byte(1) {
                        b'o' => return self.check_keyword(2, "e", StopOnError),
                        b't' => return self.check_keyword(2, "r", Str),
                        _ => {}
                    }
                }
            }
            b't' => {
                if n > 1 {
                    match self.byte(1) {
                        b'r' => {
                            if n > 2 {
                                match self.byte(2) {
                                    b'u' => return self.check_keyword(3, "e", True),
                                    b'y' => {
                                        if n > 3 {
                                            return self.check_keyword(3, "As", TryAs);
                                        }
                                        return self.check_keyword(3, "", Try);
                                    }
                                    _ => {}
                                }
                            }
                        }
                        b'y' => return self.check_keyword(2, "pe", Type),
                        _ => {}
                    }
                }
            }
            b'V' => return self.check_keyword(1, "ER", Ver),
            b'x' => {
                if n == 1 {
                    return Interpret;
                }
            }
            _ => {}
        }
        if self.peek() == '!' {
            self.advance();
            return VarStore;
        }
        Literal
    }

    fn check_keyword(&mut self, start: u32, rest: &str, kind: TokenKind) -> TokenKind {
        // rest is ASCII, so the token ends with it exactly when its last
        // rest.len() code points are rest.
        if self.cur_len() == start + rest.len() as u32 && self.cur_lexeme().ends_with(rest) {
            return kind;
        }
        if self.peek() == '!' {
            self.advance();
            return TokenKind::VarStore;
        }
        TokenKind::Literal
    }

    fn parse_single_quote_string(&mut self) -> Token {
        loop {
            match self.index_any(b"'\n") {
                Some(n) if n > 0 => self.skip(n),
                Some(_) => {}
                None => self.skip(self.bytes.len() - self.current),
            }
            if self.at_end() {
                return if self.allow_unterminated {
                    self.make(TokenKind::UnfinishedSingleQuoteString)
                } else {
                    self.error()
                };
            }
            let c = self.advance();
            if c == '\n' {
                self.handle_newline();
            } else if c == '\'' {
                break;
            }
        }
        self.make(TokenKind::SingleQuoteString)
    }

    /// r'…', one code point, closed on the same line.
    fn parse_rune(&mut self) -> Token {
        self.advance(); // the opening quote
        let Some(n) = self.index_any(b"'\n") else {
            self.skip(self.bytes.len() - self.current);
            return if self.allow_unterminated { self.make(TokenKind::UnfinishedRune) } else { self.error() };
        };
        let content_start = self.current;
        self.skip(n);
        if self.peek() == '\n' {
            return self.error();
        }
        self.advance(); // the closing quote
        let content = &self.input[content_start..content_start + n];
        if content.is_empty() && self.peek() == '\'' {
            self.advance();
            return self.error();
        }
        // Exactly one code point, not a control character.
        let mut chars = content.chars();
        match (chars.next(), chars.next()) {
            (Some(c), None) if !c.is_control() => self.make(TokenKind::Rune),
            _ => self.error(),
        }
    }

    /// U+ and 4 to 6 hex digits; the whole word is read.
    fn parse_code_point(&mut self) -> Token {
        self.scan_literal_chars();
        let digits = &self.cur_lexeme()[2..];
        let ok = digits.chars().all(is_hex_digit)
            && (4..=6).contains(&digits.len())
            && u32::from_str_radix(digits, 16).is_ok_and(|v| v <= 0x10FFFF && !(0xD800..=0xDFFF).contains(&v));
        if ok { self.make(TokenKind::Rune) } else { self.error() }
    }

    fn consume_literal(&mut self) -> Token {
        self.scan_literal_chars();
        if self.peek() == '!' {
            self.advance();
            return self.make(TokenKind::VarStore);
        }
        self.make(TokenKind::Literal)
    }

    fn parse_env_var(&mut self) -> Token {
        self.scan_literal_chars();
        match self.peek() {
            '!' => {
                self.advance();
                self.make(TokenKind::EnvStore)
            }
            '?' => {
                self.advance();
                self.make(TokenKind::EnvCheck)
            }
            _ => self.make(TokenKind::EnvRetrieve),
        }
    }

    fn advance_digits(&mut self) {
        while is_digit(self.peek()) {
            self.advance();
        }
    }

    fn parse_number_or_start_indexer(&mut self) -> Token {
        use TokenKind::*;
        self.advance_digits();
        let peek = self.peek();

        // 0x…, 0o…, 0b…: only after exactly "0" or "-0", with a digit of the
        // base after the letter.
        if let Some(base) = base_from_prefix(peek) {
            let int_part = self.cur_lexeme();
            if (int_part == "0" || int_part == "-0") && is_base_digit(base, self.peek_next()) {
                self.advance();
                while !self.at_end() && is_base_digit(base, self.peek()) {
                    self.advance();
                }
                if is_allowed_literal(self.peek()) {
                    return self.consume_literal();
                }
                return self.make(Integer);
            }
        }

        if peek == ':' {
            self.advance();
            let c = self.peek();
            if c == '-' {
                if is_digit(self.peek_next()) {
                    self.advance();
                    self.advance_digits();
                    return self.make(SliceIndexer);
                }
                return self.make(StartIndexer);
            } else if is_digit(c) {
                self.advance_digits();
                return self.make(SliceIndexer);
            }
            return self.make(StartIndexer);
        } else if peek == '>' {
            if self.peek_next() == '>' {
                self.advance();
                self.advance();
                return self.make(StderrAppend);
            }
            if self.peek_next() == '&' {
                let int_part = self.cur_lexeme();
                if int_part == "2" && self.peek_at(2) == '1' {
                    self.advance();
                    self.advance();
                    self.advance();
                    return self.make(StderrToStdout);
                }
                if int_part == "1" && self.peek_at(2) == '2' {
                    self.advance();
                    self.advance();
                    self.advance();
                    return self.make(StdoutToStderr);
                }
            }
            self.advance();
            return self.make(StderrRedirect);
        } else if peek == '.' {
            self.advance();
            self.advance_digits();
            if !is_allowed_literal(self.peek()) {
                return self.make(Float);
            }
            return self.consume_literal();
        } else if self.cur_len() == 4 && peek == '-' {
            return self.parse_date_time();
        }

        if !is_allowed_literal(peek) { self.make(Integer) } else { self.consume_literal() }
    }

    /// After four digits and a '-': YYYY-MM-DD, then optionally THH, :MM, :SS.
    fn parse_date_time(&mut self) -> Token {
        use TokenKind::*;
        self.advance();
        if !self.two_digits() {
            return self.consume_literal();
        }
        if self.peek() != '-' {
            return self.consume_literal();
        }
        self.advance();
        if !self.two_digits() {
            return self.consume_literal();
        }
        if self.peek() != 'T' {
            return self.make(DateTime);
        }
        self.advance();
        if !self.two_digits() {
            return self.consume_literal();
        }
        if self.peek() != ':' {
            return self.make(DateTime);
        }
        self.advance();
        if !self.two_digits() {
            return self.consume_literal();
        }
        if self.peek() != ':' {
            return self.make(DateTime);
        }
        self.advance();
        if !self.two_digits() {
            return self.consume_literal();
        }
        self.make(DateTime)
    }

    /// Advances over two digits; false (having advanced over any first one)
    /// if they aren't there.
    fn two_digits(&mut self) -> bool {
        for _ in 0..2 {
            if !is_digit(self.peek()) {
                return false;
            }
            self.advance();
        }
        true
    }

    fn parse_indexer_or_colon(&mut self) -> Token {
        use TokenKind::*;
        let c = self.peek();
        if c == '\0' {
            return self.make(Colon);
        }
        if c == '>' {
            self.advance();
            return self.make(MatchArmDup);
        }
        if is_digit(c) || c == '-' {
            self.advance();
            while !self.at_end() && is_digit(self.peek()) {
                self.advance();
            }
        } else {
            return self.make(Colon);
        }
        if self.peek() == ':' {
            self.advance();
            return self.make(Indexer);
        }
        self.make(EndIndexer)
    }

    fn parse_positional(&mut self) -> Token {
        while !self.at_end() && is_digit(self.peek()) {
            self.advance();
        }
        self.make(TokenKind::Positional)
    }

    fn parse_string(&mut self) -> Token {
        // Go's consumeString: Ok, or Err(true) for unterminated, Err(false)
        // for an invalid escape.
        let mut in_escape = false;
        let result = loop {
            if !in_escape {
                match self.index_any(b"\"\\\n") {
                    Some(n) if n > 0 => self.skip(n),
                    Some(_) => {}
                    None => self.skip(self.bytes.len() - self.current),
                }
            }
            if self.at_end() {
                break Err(true);
            }
            let c = self.advance();
            if in_escape {
                if !is_escape(c) {
                    break Err(false);
                }
                in_escape = false;
            } else if c == '"' {
                break Ok(());
            } else if c == '\\' {
                in_escape = true;
            } else if c == '\n' {
                self.handle_newline();
            }
        };
        match result {
            Ok(()) => self.make(TokenKind::String),
            Err(true) if self.allow_unterminated => self.make(TokenKind::UnfinishedString),
            Err(_) => self.error(),
        }
    }

    /// The literal text of a format string up to and including '{' or '"',
    /// after the opening `$"` or an interpolation's closing '}'.
    fn scan_format_chunk(&mut self, after_interpolation: bool) -> Token {
        use TokenKind::*;
        loop {
            if self.at_end() {
                return if self.allow_unterminated { self.make(UnfinishedFormatString) } else { self.error() };
            }
            match self.advance() {
                '\\' => {
                    if self.at_end() {
                        continue; // reported as unterminated
                    }
                    if !is_escape(self.advance()) {
                        return self.error();
                    }
                }
                '{' => {
                    if after_interpolation {
                        return self.make(FormatStringMid);
                    }
                    self.depths.push(0);
                    return self.make(FormatStringStart);
                }
                '"' => {
                    if after_interpolation {
                        self.depths.pop();
                        return self.make(FormatStringEnd);
                    }
                    return self.make(FormatString);
                }
                '\n' => self.handle_newline(),
                _ => {}
            }
        }
    }

    fn parse_path(&mut self) -> Token {
        loop {
            match self.index_any(b"`\n") {
                Some(n) if n > 0 => self.skip(n),
                Some(_) => {}
                None => self.skip(self.bytes.len() - self.current),
            }
            if self.at_end() {
                return if self.allow_unterminated { self.make(TokenKind::UnfinishedPath) } else { self.error() };
            }
            let c = self.advance();
            if c == '`' {
                break;
            } else if c == '\n' {
                self.handle_newline();
            }
        }
        self.make(TokenKind::Path)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn kinds(lexer: &mut Lexer, input: &str) -> Vec<(TokenKind, String)> {
        let mut tokens = Vec::new();
        lexer.tokenize_into(input, &mut tokens);
        tokens.iter().map(|t| (t.kind, t.text(input).to_string())).collect()
    }

    // The corpus comparison (tools/check-lexer.sh) checks kinds and
    // positions against Go; these check what it can't see.

    #[test]
    fn byte_offsets_and_code_point_columns() {
        let input = "世 'é' @x";
        let mut tokens = Vec::new();
        Lexer::editor().tokenize_into(input, &mut tokens);
        let at: Vec<(u32, u32, u32, &str)> = tokens.iter().map(|t| (t.col, t.start, t.byte_start, t.text(input))).collect();
        assert_eq!(at, [(1, 0, 0, "世"), (2, 1, 3, " "), (3, 2, 4, "'é'"), (6, 5, 8, " "), (7, 6, 9, "@x"), (9, 8, 11, "")]);
    }

    #[test]
    fn unfinished_tokens_only_in_editor_mode() {
        use TokenKind::*;
        assert_eq!(kinds(&mut Lexer::editor(), r#"$"a{@x}b "unfin"#).last().unwrap().0, Eof);
        assert_eq!(
            kinds(&mut Lexer::editor(), r#"$"a{"#),
            [(FormatStringStart, r#"$"a{"#.into()), (Eof, "".into())]
        );
        let batch = kinds(&mut Lexer::default(), "'unfinished");
        assert_eq!(batch, [(Error, "'unfinished".into())]);
    }

    #[test]
    fn reuse_resets_interpolation_state() {
        use TokenKind::*;
        let mut lexer = Lexer::editor();
        // An open interpolation must not leak into the next line.
        kinds(&mut lexer, r#"$"a{"#);
        assert_eq!(kinds(&mut lexer, "}"), [(RightCurly, "}".into()), (Eof, "".into())]);
    }

    #[test]
    fn numbers_that_dont_fit_are_errors() {
        use TokenKind::*;
        let mut lexer = Lexer::default();
        assert_eq!(kinds(&mut lexer, "9223372036854775807")[0].0, Integer);
        assert_eq!(kinds(&mut lexer, "9223372036854775808")[0].0, Error);
        // The sign is stripped before parsing, as in Go's parseIntLiteral.
        assert_eq!(kinds(&mut lexer, "-9223372036854775808")[0].0, Error);
        // A digit outside ASCII starts a literal, but after '-' it makes a
        // number Go's strconv can't read.
        assert_eq!(kinds(&mut lexer, "\u{663}")[0].0, Literal);
        assert_eq!(kinds(&mut lexer, "-\u{663}")[0].0, Error);
    }
}
