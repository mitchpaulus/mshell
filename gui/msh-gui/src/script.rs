//! Running the app without a window, from a script of keys, and writing the
//! frame it would draw: for checking the GUI end to end.
//!
//! msh-gui --script SCRIPT --dump OUT.ppm [--size COLSxROWS] [MSH]
//!
//! SCRIPT is typed at the prompt a key at a time: plain characters, and
//!   <Enter> <Tab> <Esc> <BS> <Left> <Right> <Up> <Down>
//!   <C-x> <A-x>     Ctrl or Alt and a key
//!   <wait>          until no block is running (then the output is drawn)
//!   <idle N>        N ms more between this key and the next (for timings)
//!   <input TEXT>    TEXT, raw, to the running program's PTY (\r, \n, \xHH)
//!   <scroll N>      the view N rows up from the bottom
//!   <sleep N>       N ms of real time, for the program to react

use crate::render::{draw_cpu, Glyphs};
use crate::{App, UserEvent, BG};
use msh_editor::keys::{KeyCode, MOD_ALT, MOD_CTRL};
use std::sync::mpsc::Receiver;
use std::time::{Duration, Instant};

enum Step {
    Key(KeyCode),
    Wait,
    Idle(i64),
    Input(Vec<u8>),
    Sleep(u64),
    Scroll(usize),
}

/// TEXT with \r, \n and \xHH.
fn unescape(text: &str) -> Result<Vec<u8>, String> {
    let mut out = Vec::new();
    let b = text.as_bytes();
    let mut i = 0;
    while i < b.len() {
        if b[i] == b'\\' && i + 1 < b.len() {
            match b[i + 1] {
                b'r' => out.push(b'\r'),
                b'n' => out.push(b'\n'),
                b'x' if i + 3 < b.len() => {
                    let h = std::str::from_utf8(&b[i + 2..i + 4]).map_err(|_| "\\xHH")?;
                    out.push(u8::from_str_radix(h, 16).map_err(|_| "\\xHH")?);
                    i += 2;
                }
                c => out.push(c),
            }
            i += 2;
        } else {
            out.push(b[i]);
            i += 1;
        }
    }
    Ok(out)
}

fn parse(script: &str) -> Result<Vec<Step>, String> {
    let mut steps = Vec::new();
    let mut rest = script;
    while let Some(c) = rest.chars().next() {
        if c != '<' {
            steps.push(Step::Key(KeyCode::char(c)));
            rest = &rest[c.len_utf8()..];
            continue;
        }
        let end = rest.find('>').ok_or("unclosed <")?;
        let tag = &rest[1..end];
        rest = &rest[end + 1..];
        let (name, arg) = tag.split_once(' ').unwrap_or((tag, ""));
        steps.push(match name {
            "Enter" => Step::Key(KeyCode::ENTER),
            "Tab" => Step::Key(KeyCode::TAB),
            "Esc" => Step::Key(KeyCode::ESCAPE),
            "BS" => Step::Key(KeyCode::BACKSPACE),
            "Left" => Step::Key(KeyCode::LEFT),
            "Right" => Step::Key(KeyCode::RIGHT),
            "Up" => Step::Key(KeyCode::UP),
            "Down" => Step::Key(KeyCode::DOWN),
            "wait" => Step::Wait,
            "idle" => Step::Idle(arg.parse().map_err(|_| "<idle N>")?),
            "sleep" => Step::Sleep(arg.parse().map_err(|_| "<sleep N>")?),
            "input" => Step::Input(unescape(arg)?),
            "scroll" => Step::Scroll(arg.parse().map_err(|_| "<scroll N>")?),
            _ if name.len() == 3 && (name.starts_with("C-") || name.starts_with("A-")) => {
                let c = name.chars().nth(2).unwrap();
                Step::Key(KeyCode::char(c).with(if name.starts_with('C') { MOD_CTRL } else { MOD_ALT }))
            }
            _ => return Err(format!("unknown <{tag}>")),
        });
    }
    Ok(steps)
}

/// Handles engine events until `done` holds, or for up to 20 s.
fn pump(app: &mut App, rx: &Receiver<UserEvent>, done: impl Fn(&App) -> bool) -> Result<(), String> {
    let deadline = Instant::now() + Duration::from_secs(20);
    while !done(app) {
        let left = deadline.saturating_duration_since(Instant::now());
        match rx.recv_timeout(left) {
            Ok(UserEvent::Engine(e)) => {
                if app.engine_event(e) {
                    return Err("the engine ended".into());
                }
            }
            Err(_) => return Err("timed out waiting for the engine".into()),
        }
    }
    Ok(())
}

pub fn run(mut app: App, rx: Receiver<UserEvent>, script: &str, dump: &str, cols: u16, rows: u16) -> Result<(), String> {
    let steps = parse(script)?;
    let glyphs = Glyphs::new(std::mem::take(&mut app.font), app.font_size);
    let (cw, ch) = (glyphs.cell.w, glyphs.cell.h);
    app.glyphs = Some(glyphs);
    app.resize(cols as u32 * cw, rows as u32 * ch);
    pump(&mut app, &rx, |a| a.cfg.is_some())?;
    let mut now = 0i64;
    for step in steps {
        // Output that has arrived is handled before each step.
        while let Ok(UserEvent::Engine(e)) = rx.try_recv() {
            if app.engine_event(e) {
                return Err("the engine ended".into());
            }
        }
        match step {
            Step::Key(code) => {
                now += 50_000_000;
                if app.running.is_some() {
                    return Err("a key while a block runs: use <input …>".into());
                }
                if app.editor_key(code, now) {
                    break;
                }
            }
            Step::Wait => pump(&mut app, &rx, |a| a.running.is_none())?,
            Step::Idle(ms) => now += ms * 1_000_000,
            Step::Input(bytes) => {
                let _ = app.engine.input(&bytes);
            }
            Step::Scroll(n) => app.scroll = n,
            Step::Sleep(ms) => {
                let until = Instant::now() + Duration::from_millis(ms);
                while let Some(left) = until.checked_duration_since(Instant::now()) {
                    if let Ok(UserEvent::Engine(e)) = rx.recv_timeout(left) {
                        if app.engine_event(e) {
                            return Err("the engine ended".into());
                        }
                    }
                }
            }
        }
    }
    app.compose();
    let glyphs = app.glyphs.as_ref().unwrap();
    let (w, h, px) = draw_cpu(glyphs, &app.quads, app.cols as u32, app.rows as u32, BG);
    let mut out = format!("P6\n{w} {h}\n255\n").into_bytes();
    out.extend_from_slice(&px);
    std::fs::write(dump, out).map_err(|e| format!("{dump}: {e}"))
}
