//! Running the app without a window, from a script of keys, and writing the
//! frame it would draw: for checking the GUI end to end.
//!
//! msh-gui --script SCRIPT --dump OUT.ppm [--size COLSxROWS] [MSH]
//!
//! SCRIPT is typed at the prompt a key at a time: plain characters, and
//!   <Enter> <Tab> <Esc> <BS> <Left> <Right> <Up> <Down> <Space>
//!   <C-x> <A-x>     Ctrl or Alt and a key (<A-Enter> too);
//!                   the window's keys work as in the window
//!   <wait>          until no block is running (then the output is drawn)
//!   <idle N>        N ms more between this key and the next (for timings)
//!   <input TEXT>    TEXT, raw, to the running program's PTY (\r, \n, \xHH)
//!   <scroll N>      the view N rows up from the bottom
//!   <click X Y>     a click at pixel X, Y (at the script's time: two
//!                   with no <idle> between are a double click)
//!   <wheel X Y N>   the wheel at pixel X, Y, N notches down (up when
//!                   negative)
//!   <back X Y>      the Back button at pixel X, Y
//!   <sleep N>       N ms of real time, for the program to react

use crate::render::{draw_cpu, Glyphs};
use crate::{App, UserEvent, BG, DRAIN_BUDGET};
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
    Click(f64, f64),
    Wheel(f64, f64, f32),
    Back(f64, f64),
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
            "Space" => Step::Key(KeyCode::char(' ')),
            "A-Enter" => Step::Key(KeyCode::ENTER.with(MOD_ALT)),
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
            "click" | "back" => {
                let usage = || format!("<{name} X Y>");
                let (x, y) = arg.split_once(' ').ok_or_else(usage)?;
                let (x, y) = (x.parse().map_err(|_| usage())?, y.parse().map_err(|_| usage())?);
                if name == "click" { Step::Click(x, y) } else { Step::Back(x, y) }
            }
            "wheel" => {
                let usage = "<wheel X Y N>";
                let mut it = arg.split(' ');
                let mut num = || it.next().and_then(|v| v.parse::<f64>().ok()).ok_or(usage);
                let (x, y, n) = (num()?, num()?, num()?);
                Step::Wheel(x, y, n as f32)
            }
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
        if left.is_zero() || !events(app, rx, left)? {
            return Err("timed out waiting for the engine".into());
        }
    }
    Ok(())
}

/// One drain of the engines' events, a frame's budget, as the window takes
/// them between frames: what was left waiting, or else what arrives within
/// `timeout`. Returns whether there were any.
fn events(app: &mut App, rx: &Receiver<UserEvent>, timeout: Duration) -> Result<bool, String> {
    if !app.backlog {
        match rx.recv_timeout(timeout) {
            Ok(UserEvent::Wake) => {}
            Err(_) => return Ok(false),
        }
    }
    if app.drain(DRAIN_BUDGET) {
        return Err("the last pane closed".into());
    }
    Ok(true)
}

/// The focused pane is ready for keys: its config is in, nothing runs.
fn ready(app: &App) -> bool {
    app.focused_pane().is_some_and(|p| p.cfg.is_some() && p.running.is_none())
}

pub fn run(mut app: App, rx: Receiver<UserEvent>, script: &str, dump: &str, cols: u16, rows: u16) -> Result<(), String> {
    let steps = parse(script)?;
    let glyphs = Glyphs::new(std::mem::take(&mut app.font), std::mem::take(&mut app.ui_font), app.font_size);
    let (cw, ch) = (glyphs.cell.w, glyphs.cell.h);
    app.glyphs = Some(glyphs);
    app.resize(cols as u32 * cw, rows as u32 * ch);
    pump(&mut app, &rx, ready)?;
    let mut now = 0i64;
    for step in steps {
        // Output that has arrived is handled before each step (a frame's
        // worth, under a flood).
        events(&mut app, &rx, Duration::ZERO)?;
        match step {
            Step::Key(code) => {
                now += 50_000_000;
                // A new pane's keys wait for its config, as in the window.
                pump(&mut app, &rx, |a| a.focused_pane().is_some_and(|p| p.cfg.is_some()))?;
                if app.key(code, now) {
                    break;
                }
            }
            Step::Wait => pump(&mut app, &rx, ready)?,
            Step::Idle(ms) => now += ms * 1_000_000,
            Step::Input(bytes) => {
                if let Some(p) = app.focused() {
                    let _ = p.engine.input(&bytes);
                }
            }
            Step::Scroll(n) => {
                if let Some(p) = app.focused() {
                    p.scroll = n;
                }
            }
            Step::Click(x, y) => {
                app.pointer = (x, y);
                app.click(now);
            }
            Step::Wheel(x, y, n) => {
                app.pointer = (x, y);
                app.wheel(winit::event::MouseScrollDelta::LineDelta(0.0, -n));
            }
            Step::Back(x, y) => {
                app.pointer = (x, y);
                app.back();
            }
            Step::Sleep(ms) => {
                let until = Instant::now() + Duration::from_millis(ms);
                while let Some(left) = until.checked_duration_since(Instant::now()) {
                    events(&mut app, &rx, left)?;
                }
            }
        }
    }
    // What is still arriving (a prompt, output) is drawn, as the window
    // would a moment later.
    let until = Instant::now() + Duration::from_secs(2);
    while Instant::now() < until && events(&mut app, &rx, Duration::from_millis(150))? {}
    app.compose();
    if app.glyphs.as_ref().is_some_and(|g| g.reset) {
        app.glyphs.as_mut().unwrap().reset = false;
        app.compose();
    }
    let glyphs = app.glyphs.as_ref().unwrap();
    let (w, h, px) = draw_cpu(glyphs, &app.quads, app.width as u32, app.height as u32, BG);
    let mut out = format!("P6\n{w} {h}\n255\n").into_bytes();
    out.extend_from_slice(&px);
    std::fs::write(dump, out).map_err(|e| format!("{dump}: {e}"))
}
