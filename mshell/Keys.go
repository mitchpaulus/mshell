package main

// Keys as the key bindings see them (ai/cli_bindings.typ, section 5.1). A
// binding names a key with the Key type: a rune, a NamedKey, or a key
// wrapped in modifiers. Every key, from a binding or from the terminal, is
// turned into one KeyCode, so two spellings of a key are the same key, and
// the editor compares keys as integers.

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// KeyCode is a key with its modifiers: a code point, or a named key above
// U+10FFFF, in the low 21 bits, and the modifiers in bits 24 to 27.
type KeyCode uint32

const (
	keyNamedBase KeyCode = 0x110000
	keyBaseMask  KeyCode = 1<<21 - 1

	KeyModShift KeyCode = 1 << 24
	KeyModAlt   KeyCode = 1 << 25
	KeyModCtrl  KeyCode = 1 << 26
	KeyModSuper KeyCode = 1 << 27
	keyModMask          = KeyModShift | KeyModAlt | KeyModCtrl | KeyModSuper
)

// The named keys, in the order of the NamedKey members.
const (
	NamedEnter KeyCode = keyNamedBase + iota
	NamedTab
	NamedBackspace
	NamedEscape
	NamedDelete
	NamedInsert
	NamedUp
	NamedDown
	NamedLeft
	NamedRight
	NamedHome
	NamedEnd
	NamedPageUp
	NamedPageDown
	NamedF1
)

// namedKeyCount is the number of named keys: the ones above and F1 to F35.
const namedKeyCount = int(NamedF1-keyNamedBase) + 35

// noKey is no key: the last key of a program that no key ran, which
// editSelfInsert types nothing for.
const noKey = keyNamedBase + 0xFFFF

// NamedF is the key Fn, for n from 1 to 35.
func NamedF(n int) KeyCode {
	return NamedF1 + KeyCode(n-1)
}

// namedKeyMembers are the NamedKey members, by the key's number.
var namedKeyMembers = func() []string {
	names := []string{
		"keyEnter", "keyTab", "keyBackspace", "keyEscape", "keyDelete", "keyInsert",
		"keyUp", "keyDown", "keyLeft", "keyRight", "keyHome", "keyEnd", "keyPageUp", "keyPageDown",
	}
	for n := 1; n <= 35; n++ {
		names = append(names, "keyF"+strconv.Itoa(n))
	}
	return names
}()

// keyModifiers are the ModifiedKey members, innermost first, as keySource
// writes them.
var keyModifiers = []struct {
	member string
	bit    KeyCode
}{
	{"keyShift", KeyModShift},
	{"keyAlt", KeyModAlt},
	{"keyCtrl", KeyModCtrl},
	{"keySuper", KeyModSuper},
}

func (k KeyCode) base() KeyCode      { return k & keyBaseMask }
func (k KeyCode) mods() KeyCode      { return k & keyModMask }
func (k KeyCode) isNamed() bool      { return k.base() >= keyNamedBase }
func (k KeyCode) baseRune() rune     { return rune(k.base()) }
func (k KeyCode) is(base KeyCode) bool { return k == base }

// text is the text a key types: its rune, when it has no modifier.
func (k KeyCode) text() (rune, bool) {
	if k.mods() != 0 || k.isNamed() {
		return 0, false
	}
	return k.baseRune(), true
}

// normalizeKey writes a key one way (section 5.1). A letter with Shift and
// no other modifier is the shifted letter. With Ctrl, Alt or Super, a
// letter is its lower case and Shift, as the terminal reports the key. A
// non-letter with Shift alone is left as it is: no terminal sends it, so a
// binding of it never fires.
func normalizeKey(k KeyCode) KeyCode {
	if k.isNamed() {
		return k
	}
	r, mods := k.baseRune(), k.mods()
	if !unicode.IsLetter(r) {
		return k
	}
	if mods == KeyModShift {
		if upper := unicode.ToUpper(r); upper != r || unicode.IsUpper(r) {
			return KeyCode(upper)
		}
		return k
	}
	if mods&(KeyModCtrl|KeyModAlt|KeyModSuper) != 0 && unicode.IsUpper(r) {
		if lower := unicode.ToLower(r); lower != r {
			return KeyCode(lower) | mods | KeyModShift
		}
	}
	return k
}

// keyCodeOf reads a Key value. It fails only on a value that is not a Key,
// which the type checker refuses.
func keyCodeOf(v MShellObject) (KeyCode, error) {
	var mods KeyCode
	for {
		switch x := v.(type) {
		case MShellRune:
			return normalizeKey(KeyCode(x.Value) | mods), nil
		case *MShellEnum:
			switch x.EnumName {
			case "NamedKey":
				return normalizeKey(keyNamedBase + KeyCode(x.MemberIndex) | mods), nil
			case "ModifiedKey":
				if len(x.Payload) != 1 {
					return 0, fmt.Errorf("'%s' must hold a Key", x.Member)
				}
				for _, m := range keyModifiers {
					if m.member == x.Member {
						mods |= m.bit
					}
				}
				v = x.Payload[0]
				continue
			}
		}
		return 0, fmt.Errorf("a key must be a rune, a NamedKey or a ModifiedKey, not a %s (%s)", v.TypeName(), v.DebugString())
	}
}

// keyValue is the Key value of a key: its rune or named key, in its
// modifiers, Shift innermost.
func (state *EvalState) keyValue(k KeyCode) MShellObject {
	var v MShellObject
	if k.isNamed() {
		v = state.builtinEnumValue(namedKeyMembers[k.base()-keyNamedBase])
	} else {
		v = MShellRune{k.baseRune()}
	}
	for _, m := range keyModifiers {
		if k&m.bit != 0 {
			v = state.builtinEnumValue(m.member, v)
		}
	}
	return v
}

// keySource writes a key as it is written in a binding: r'x' keyCtrl.
func keySource(k KeyCode) string {
	var sb strings.Builder
	if k.isNamed() {
		if i := int(k.base() - keyNamedBase); i < len(namedKeyMembers) {
			sb.WriteString(namedKeyMembers[i])
		} else {
			fmt.Fprintf(&sb, "<key %d>", i)
		}
	} else {
		sb.WriteString(runeSource(k.baseRune()))
	}
	for _, m := range keyModifiers {
		if k&m.bit != 0 {
			sb.WriteByte(' ')
			sb.WriteString(m.member)
		}
	}
	return sb.String()
}

// keysSource writes a sequence of keys, separated by spaces.
func keysSource(keys []KeyCode) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = keySource(k)
	}
	return strings.Join(parts, " ")
}

// KeyToken is a key the terminal lexer decoded that has no token of its
// own among the older ones: an Alt chord, a key with modifiers, a key the
// kitty keyboard protocol reports. EscOf is set when the legacy encoding
// sent it as ESC and a key: it is that key, which acts on its own when
// Alt and it has no binding.
type KeyToken struct {
	Code  KeyCode
	EscOf KeyCode
}

func (t KeyToken) String() string {
	return "KeyToken: " + keySource(t.Code)
}

// asciiKey is the key a byte below 128 is, in the legacy encoding
// (section 5.1.3).
func asciiKey(c byte) KeyCode {
	switch {
	case c == 0:
		return ' ' | KeyModCtrl
	case c == 8:
		return 'h' | KeyModCtrl
	case c == 9:
		return NamedTab
	case c == 10:
		return 'j' | KeyModCtrl
	case c == 13:
		return NamedEnter
	case c == 27:
		return NamedEscape
	case c >= 1 && c <= 26:
		return KeyCode('a'+c-1) | KeyModCtrl
	case c >= 28 && c <= 31:
		return KeyCode("\\]^_"[c-28]) | KeyModCtrl
	case c == 127:
		return NamedBackspace
	}
	return KeyCode(c)
}

// specialKeys are the keys of the older SpecialKey tokens.
var specialKeys = map[SpecialKey]KeyCode{
	KEY_UP: NamedUp, KEY_DOWN: NamedDown, KEY_LEFT: NamedLeft, KEY_RIGHT: NamedRight,
	KEY_DELETE: NamedDelete, KEY_HOME: NamedHome, KEY_END: NamedEnd,
	KEY_ALT_B: 'b' | KeyModAlt, KEY_ALT_D: 'd' | KeyModAlt, KEY_ALT_F: 'f' | KeyModAlt,
	KEY_ALT_O: 'o' | KeyModAlt, KEY_ALT_DOT: '.' | KeyModAlt,
	KEY_ALT_SHIFT_R: 'r' | KeyModAlt | KeyModShift,
	KEY_CTRL_DELETE: NamedDelete | KeyModCtrl,
	KEY_SHIFT_TAB:   NamedTab | KeyModShift,
}

// keyOfToken is the key a terminal token is, if it is one.
func keyOfToken(token TerminalToken) (KeyCode, bool) {
	switch t := token.(type) {
	case KeyToken:
		return t.Code, true
	case AsciiToken:
		return asciiKey(t.Char), true
	case MutliByteToken:
		return KeyCode(t.Char), true
	case SpecialKey:
		if t >= KEY_F1 && t <= KEY_F12 {
			return NamedF(int(t-KEY_F1) + 1), true
		}
		k, ok := specialKeys[t]
		return k, ok
	}
	return 0, false
}

// altKey is the key Alt and the key k is, as ESC and k's bytes send it.
func altKey(k KeyCode) KeyCode {
	return normalizeKey(k | KeyModAlt)
}

// csiModifiers reads the modifier parameter of a key's escape sequence:
// one more than a bit field of Shift 1, Alt 2, Ctrl 4, Super 8 (xterm and
// kitty). Hyper, Meta, Caps Lock and Num Lock are not modifiers a binding
// can name, and are dropped.
func csiModifiers(param string) (KeyCode, bool) {
	if param == "" {
		return 0, true
	}
	if i := strings.IndexByte(param, ':'); i >= 0 {
		param = param[:i] // the kitty event type follows
	}
	n, err := strconv.Atoi(param)
	if err != nil || n < 1 {
		return 0, false
	}
	bits := KeyCode(n - 1)
	var mods KeyCode
	if bits&1 != 0 {
		mods |= KeyModShift
	}
	if bits&2 != 0 {
		mods |= KeyModAlt
	}
	if bits&4 != 0 {
		mods |= KeyModCtrl
	}
	if bits&8 != 0 {
		mods |= KeyModSuper
	}
	return mods, true
}

// tildeKeys are the keys of CSI n ~ (VT220, xterm, kitty).
var tildeKeys = map[int]KeyCode{
	1: NamedHome, 2: NamedInsert, 3: NamedDelete, 4: NamedEnd, 5: NamedPageUp, 6: NamedPageDown,
	7: NamedHome, 8: NamedEnd,
	11: NamedF(1), 12: NamedF(2), 13: NamedF(3), 14: NamedF(4), 15: NamedF(5),
	17: NamedF(6), 18: NamedF(7), 19: NamedF(8), 20: NamedF(9), 21: NamedF(10),
	23: NamedF(11), 24: NamedF(12),
}

// letterKeys are the keys of CSI 1 ; m X, and of SS3 X.
var letterKeys = map[byte]KeyCode{
	'A': NamedUp, 'B': NamedDown, 'C': NamedRight, 'D': NamedLeft,
	'H': NamedHome, 'F': NamedEnd,
	'P': NamedF(1), 'Q': NamedF(2), 'R': NamedF(3), 'S': NamedF(4),
	'Z': NamedTab | KeyModShift,
}

// kittyFunctionalKey is the key of a kitty keyboard protocol code that is
// not text: the C0 keys it reports by their legacy numbers, and F13 to F35
// in its Private Use Area.
func kittyFunctionalKey(code int) (KeyCode, bool) {
	switch {
	case code == 9:
		return NamedTab, true
	case code == 13:
		return NamedEnter, true
	case code == 27:
		return NamedEscape, true
	case code == 127:
		return NamedBackspace, true
	case code >= 57376 && code <= 57398:
		return NamedF(13 + code - 57376), true
	case code >= 57344 && code <= 63743:
		// Keypad, media and modifier keys: no NamedKey yet.
		return 0, false
	}
	return 0, false
}

// decodeCsiKey reads a key from a CSI sequence with parameters: the legacy
// forms with a modifier (CSI 1;5C is Ctrl-Right, CSI 3;2~ is Shift-Delete)
// and the kitty keyboard protocol's CSI code[:shifted[:base]];mods u.
// Cursor reports (final R) and replies to queries (a parameter starting
// with ?, >, = or <) are not keys.
func decodeCsiKey(final byte, params string) (KeyCode, bool) {
	if final == 'R' || params == "" || strings.IndexByte("?>=<", params[0]) >= 0 {
		return 0, false
	}
	fields := strings.Split(params, ";")
	mods, ok := KeyCode(0), true
	if len(fields) > 1 {
		if mods, ok = csiModifiers(fields[1]); !ok {
			return 0, false
		}
	}
	switch final {
	case '~':
		n, err := strconv.Atoi(fields[0])
		if err != nil {
			return 0, false
		}
		k, ok := tildeKeys[n]
		if !ok {
			return 0, false
		}
		return normalizeKey(k | mods), true
	case 'u':
		codes := strings.Split(fields[0], ":")
		code, err := strconv.Atoi(codes[0])
		if err != nil || code < 0 {
			return 0, false
		}
		// With alternate keys reported, the third number is the key in
		// the same place on a US layout: Ctrl and Alt follow the
		// keyboard's place, so ctrl-w works on any layout.
		if len(codes) > 2 && mods&(KeyModCtrl|KeyModAlt|KeyModSuper) != 0 {
			if baseCode, err := strconv.Atoi(codes[2]); err == nil && baseCode > 0 {
				code = baseCode
			}
		}
		if k, ok := kittyFunctionalKey(code); ok {
			return normalizeKey(k | mods), true
		}
		if code >= 57344 && code <= 63743 {
			return 0, false
		}
		if code > unicode.MaxRune || (code >= 0xD800 && code <= 0xDFFF) {
			return 0, false
		}
		return normalizeKey(KeyCode(code) | mods), true
	default:
		k, ok := letterKeys[final]
		if !ok || (fields[0] != "1" && fields[0] != "") {
			return 0, false
		}
		return normalizeKey(k | mods), true
	}
}
