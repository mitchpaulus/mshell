//! Keys as the key bindings see them, ported from `mshell/Keys.go`: a code
//! point or a named key in the low 21 bits, and the modifiers in bits 24 to
//! 27. The engine sends bindings' keys already in this form, and the
//! frontend turns each key event into one.

#[derive(Clone, Copy, PartialEq, Eq, Hash, Debug, PartialOrd, Ord, Default)]
pub struct KeyCode(pub u32);

pub const NAMED_BASE: u32 = 0x110000;
const BASE_MASK: u32 = (1 << 21) - 1;

pub const MOD_SHIFT: u32 = 1 << 24;
pub const MOD_ALT: u32 = 1 << 25;
pub const MOD_CTRL: u32 = 1 << 26;
pub const MOD_SUPER: u32 = 1 << 27;
const MOD_MASK: u32 = MOD_SHIFT | MOD_ALT | MOD_CTRL | MOD_SUPER;

impl KeyCode {
    pub const ENTER: KeyCode = KeyCode(NAMED_BASE);
    pub const TAB: KeyCode = KeyCode(NAMED_BASE + 1);
    pub const BACKSPACE: KeyCode = KeyCode(NAMED_BASE + 2);
    pub const ESCAPE: KeyCode = KeyCode(NAMED_BASE + 3);
    pub const DELETE: KeyCode = KeyCode(NAMED_BASE + 4);
    pub const INSERT: KeyCode = KeyCode(NAMED_BASE + 5);
    pub const UP: KeyCode = KeyCode(NAMED_BASE + 6);
    pub const DOWN: KeyCode = KeyCode(NAMED_BASE + 7);
    pub const LEFT: KeyCode = KeyCode(NAMED_BASE + 8);
    pub const RIGHT: KeyCode = KeyCode(NAMED_BASE + 9);
    pub const HOME: KeyCode = KeyCode(NAMED_BASE + 10);
    pub const END: KeyCode = KeyCode(NAMED_BASE + 11);
    pub const PAGE_UP: KeyCode = KeyCode(NAMED_BASE + 12);
    pub const PAGE_DOWN: KeyCode = KeyCode(NAMED_BASE + 13);
    /// F1; Fn is F1 + n - 1, up to F35.
    pub const F1: KeyCode = KeyCode(NAMED_BASE + 14);
    /// No key: the last key of a program no key ran, which editSelfInsert
    /// types nothing for.
    pub const NONE: KeyCode = KeyCode(NAMED_BASE + 0xFFFF);

    pub fn char(c: char) -> KeyCode {
        KeyCode(c as u32)
    }

    pub fn with(self, mods: u32) -> KeyCode {
        KeyCode(self.0 | mods)
    }

    pub fn base(self) -> u32 {
        self.0 & BASE_MASK
    }

    pub fn mods(self) -> u32 {
        self.0 & MOD_MASK
    }

    pub fn is_named(self) -> bool {
        self.base() >= NAMED_BASE
    }

    /// The text a key types: its character, when it has no modifier.
    pub fn text(self) -> Option<char> {
        if self.mods() != 0 || self.is_named() {
            return None;
        }
        char::from_u32(self.base())
    }
}
