//! Keys from winit: as the line editor's KeyCode (mshell/Keys.go), and as
//! libghostty's key events for a running program.

use ghostty_vt_sys as g;
use msh_editor::keys::{KeyCode, MOD_ALT, MOD_CTRL, MOD_SHIFT, MOD_SUPER, NAMED_BASE};
use winit::event::KeyEvent;
use winit::keyboard::{Key, KeyCode as Physical, ModifiersState, NamedKey, PhysicalKey};

/// The editor's KeyCode for a key press, normalised as Go's normalizeKey:
/// a letter with Shift alone is the shifted letter; with Ctrl, Alt or Super
/// it is the lower-case letter and Shift.
pub fn editor_key(event: &KeyEvent, mods: ModifiersState) -> Option<KeyCode> {
    let mut m = 0;
    if mods.control_key() {
        m |= MOD_CTRL;
    }
    if mods.alt_key() {
        m |= MOD_ALT;
    }
    if mods.super_key() {
        m |= MOD_SUPER;
    }
    let named = |n: u32| Some(KeyCode(NAMED_BASE + n));
    let base = match &event.logical_key {
        Key::Named(NamedKey::Space) => KeyCode::char(' '),
        Key::Named(n) => {
            let code = match n {
                NamedKey::Enter => named(0),
                NamedKey::Tab => named(1),
                NamedKey::Backspace => named(2),
                NamedKey::Escape => named(3),
                NamedKey::Delete => named(4),
                NamedKey::Insert => named(5),
                NamedKey::ArrowUp => named(6),
                NamedKey::ArrowDown => named(7),
                NamedKey::ArrowLeft => named(8),
                NamedKey::ArrowRight => named(9),
                NamedKey::Home => named(10),
                NamedKey::End => named(11),
                NamedKey::PageUp => named(12),
                NamedKey::PageDown => named(13),
                NamedKey::F1 => named(14),
                NamedKey::F2 => named(15),
                NamedKey::F3 => named(16),
                NamedKey::F4 => named(17),
                NamedKey::F5 => named(18),
                NamedKey::F6 => named(19),
                NamedKey::F7 => named(20),
                NamedKey::F8 => named(21),
                NamedKey::F9 => named(22),
                NamedKey::F10 => named(23),
                NamedKey::F11 => named(24),
                NamedKey::F12 => named(25),
                _ => None,
            }?;
            // A named key keeps Shift.
            if mods.shift_key() {
                m |= MOD_SHIFT;
            }
            return Some(code.with(m));
        }
        Key::Character(s) => {
            let mut chars = s.chars();
            let c = chars.next()?;
            if chars.next().is_some() {
                return None;
            }
            KeyCode::char(c)
        }
        _ => return None,
    };
    let c = char::from_u32(base.0)?;
    if m != 0 && c.is_alphabetic() && c.is_uppercase() {
        let lower = c.to_lowercase().next().unwrap_or(c);
        if lower != c {
            return Some(KeyCode::char(lower).with(m | MOD_SHIFT));
        }
    }
    Some(base.with(m))
}

pub fn ghostty_mods(mods: ModifiersState) -> g::GhosttyMods {
    let mut m = 0;
    if mods.shift_key() {
        m |= 1 << 0;
    }
    if mods.control_key() {
        m |= 1 << 1;
    }
    if mods.alt_key() {
        m |= 1 << 2;
    }
    if mods.super_key() {
        m |= 1 << 3;
    }
    m as g::GhosttyMods
}

/// libghostty's key for winit's physical key (both are W3C key codes).
pub fn ghostty_key(event: &KeyEvent) -> g::GhosttyKey {
    use Physical::*;
    let PhysicalKey::Code(code) = event.physical_key else { return g::GHOSTTY_KEY_UNIDENTIFIED };
    let letter = |i: u32| g::GHOSTTY_KEY_A + i;
    let digit = |i: u32| g::GHOSTTY_KEY_DIGIT_0 + i;
    match code {
        KeyA => letter(0),
        KeyB => letter(1),
        KeyC => letter(2),
        KeyD => letter(3),
        KeyE => letter(4),
        KeyF => letter(5),
        KeyG => letter(6),
        KeyH => letter(7),
        KeyI => letter(8),
        KeyJ => letter(9),
        KeyK => letter(10),
        KeyL => letter(11),
        KeyM => letter(12),
        KeyN => letter(13),
        KeyO => letter(14),
        KeyP => letter(15),
        KeyQ => letter(16),
        KeyR => letter(17),
        KeyS => letter(18),
        KeyT => letter(19),
        KeyU => letter(20),
        KeyV => letter(21),
        KeyW => letter(22),
        KeyX => letter(23),
        KeyY => letter(24),
        KeyZ => letter(25),
        Digit0 => digit(0),
        Digit1 => digit(1),
        Digit2 => digit(2),
        Digit3 => digit(3),
        Digit4 => digit(4),
        Digit5 => digit(5),
        Digit6 => digit(6),
        Digit7 => digit(7),
        Digit8 => digit(8),
        Digit9 => digit(9),
        Backquote => g::GHOSTTY_KEY_BACKQUOTE,
        Backslash => g::GHOSTTY_KEY_BACKSLASH,
        BracketLeft => g::GHOSTTY_KEY_BRACKET_LEFT,
        BracketRight => g::GHOSTTY_KEY_BRACKET_RIGHT,
        Comma => g::GHOSTTY_KEY_COMMA,
        Equal => g::GHOSTTY_KEY_EQUAL,
        Minus => g::GHOSTTY_KEY_MINUS,
        Period => g::GHOSTTY_KEY_PERIOD,
        Quote => g::GHOSTTY_KEY_QUOTE,
        Semicolon => g::GHOSTTY_KEY_SEMICOLON,
        Slash => g::GHOSTTY_KEY_SLASH,
        Backspace => g::GHOSTTY_KEY_BACKSPACE,
        Enter => g::GHOSTTY_KEY_ENTER,
        Space => g::GHOSTTY_KEY_SPACE,
        Tab => g::GHOSTTY_KEY_TAB,
        Delete => g::GHOSTTY_KEY_DELETE,
        End => g::GHOSTTY_KEY_END,
        Home => g::GHOSTTY_KEY_HOME,
        Insert => g::GHOSTTY_KEY_INSERT,
        PageDown => g::GHOSTTY_KEY_PAGE_DOWN,
        PageUp => g::GHOSTTY_KEY_PAGE_UP,
        ArrowDown => g::GHOSTTY_KEY_ARROW_DOWN,
        ArrowLeft => g::GHOSTTY_KEY_ARROW_LEFT,
        ArrowRight => g::GHOSTTY_KEY_ARROW_RIGHT,
        ArrowUp => g::GHOSTTY_KEY_ARROW_UP,
        Escape => g::GHOSTTY_KEY_ESCAPE,
        F1 => g::GHOSTTY_KEY_F1,
        F2 => g::GHOSTTY_KEY_F2,
        F3 => g::GHOSTTY_KEY_F3,
        F4 => g::GHOSTTY_KEY_F4,
        F5 => g::GHOSTTY_KEY_F5,
        F6 => g::GHOSTTY_KEY_F6,
        F7 => g::GHOSTTY_KEY_F7,
        F8 => g::GHOSTTY_KEY_F8,
        F9 => g::GHOSTTY_KEY_F9,
        F10 => g::GHOSTTY_KEY_F10,
        F11 => g::GHOSTTY_KEY_F11,
        F12 => g::GHOSTTY_KEY_F12,
        ShiftLeft => g::GHOSTTY_KEY_SHIFT_LEFT,
        ShiftRight => g::GHOSTTY_KEY_SHIFT_RIGHT,
        ControlLeft => g::GHOSTTY_KEY_CONTROL_LEFT,
        ControlRight => g::GHOSTTY_KEY_CONTROL_RIGHT,
        AltLeft => g::GHOSTTY_KEY_ALT_LEFT,
        AltRight => g::GHOSTTY_KEY_ALT_RIGHT,
        SuperLeft => g::GHOSTTY_KEY_META_LEFT,
        SuperRight => g::GHOSTTY_KEY_META_RIGHT,
        _ => g::GHOSTTY_KEY_UNIDENTIFIED,
    }
}

/// A key as a card header names it: ;r, A-x, F1, Enter.
pub fn key_label(k: KeyCode) -> String {
    let mut s = String::new();
    for (bit, name) in [(MOD_CTRL, "C-"), (MOD_ALT, "A-"), (MOD_SUPER, "S-")] {
        if k.mods() & bit != 0 {
            s.push_str(name);
        }
    }
    if k.is_named() {
        let names = ["Enter", "Tab", "Backspace", "Esc", "Delete", "Insert", "Up", "Down", "Left", "Right", "Home", "End", "PageUp", "PageDown"];
        let n = (k.base() - NAMED_BASE) as usize;
        if n < names.len() {
            s.push_str(names[n]);
        } else {
            s.push_str(&format!("F{}", n - names.len() + 1));
        }
    } else if let Some(c) = char::from_u32(k.base()) {
        if k.mods() & MOD_SHIFT != 0 {
            s.extend(c.to_uppercase());
        } else {
            s.push(c);
        }
    }
    s
}
