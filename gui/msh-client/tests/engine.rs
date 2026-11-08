//! The Rust editor against a real engine: msh server, with the editor's
//! requests answered by it. Needs mshell/mshell built (mshell/build.sh), or
//! MSH_BIN.

use msh_client::{Engine, EngineHost, Event};
use msh_editor::edit::{EditOp, Line};
use msh_editor::keys::{KeyCode, MOD_CTRL};
use msh_editor::resolver::Editor;
use std::path::PathBuf;
use std::process::Command;
use std::sync::mpsc;
use std::time::Duration;

fn engine() -> (Engine, mpsc::Receiver<Event>, PathBuf) {
    let repo = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../..");
    let msh = std::env::var("MSH_BIN").map(PathBuf::from).unwrap_or_else(|_| repo.join("mshell/mshell"));
    let tmp = std::env::temp_dir().join(format!("msh-client-test-{}", std::process::id()));
    std::fs::create_dir_all(&tmp).unwrap();
    std::fs::write(tmp.join("init.msh"), "").unwrap();
    let mut cmd = Command::new(msh);
    cmd.arg("server")
        .env("MSHSTDLIB", repo.join("lib/std.msh"))
        .env("MSHINIT", tmp.join("init.msh"))
        .env("XDG_DATA_HOME", &tmp);
    let (tx, rx) = mpsc::channel();
    let engine = Engine::spawn_command(cmd, move |e| {
        let _ = tx.send(e);
    })
    .expect("start msh server; build mshell/mshell or set MSH_BIN");
    (engine, rx, tmp)
}

fn next(rx: &mpsc::Receiver<Event>) -> Event {
    rx.recv_timeout(Duration::from_secs(20)).expect("no event from the engine")
}

/// Waits for a block to finish, collecting its output.
fn block(rx: &mpsc::Receiver<Event>, id: u64) -> (String, i32) {
    let mut out = Vec::new();
    loop {
        match next(rx) {
            Event::PtyOutput { id: i, bytes } if i == id => out.extend(bytes),
            Event::BlockFinished { id: i, exit_code, .. } if i == id => {
                return (String::from_utf8_lossy(&out).into_owned(), exit_code);
            }
            Event::Closed { error } => panic!("closed: {error:?}"),
            _ => {}
        }
    }
}

#[test]
fn editor_with_engine() {
    let (mut engine, rx, tmp) = engine();
    assert!(matches!(next(&rx), Event::Hello { version: 1, .. }));
    assert!(matches!(next(&rx), Event::EditorConfig { version: 1 }));
    let defaults = engine.take_config().expect("first config");
    assert!(defaults.keymap.bindings().len() > 30, "{} default bindings", defaults.keymap.bindings().len());

    // Bindings and abbreviations added by a line reach the editor with the
    // block.
    engine
        .run(
            1,
            "{keys: [r'x' keyCtrl], action: (drop ['hi' editInsert] just) bindingFunc} bindingAdd \
             {abbr: 'gs' abbrWord, expansion: (drop 'git status' just) abbrFunc} abbrAdd \
             {abbr: 'll' abbrWord, expansion: 'ls -la %' abbrText, cursorMarker: '%'} abbrAdd",
        )
        .unwrap();
    let (out, code) = block(&rx, 1);
    assert_eq!((out.as_str(), code), ("", 0));
    let cfg = engine.take_config().expect("the config the line changed");
    assert_eq!(cfg.abbrs.len(), 2);

    let mut ed = Editor::new("", 0);
    let mut accepted = false;
    let mut now = 0i64;
    let mut type_keys = |ed: &mut Editor, engine: &mut Engine, keys: &[KeyCode], accepted: &mut bool| {
        for &k in keys {
            now += 100_000_000;
            let mut host = EngineHost {
                engine: &mut *engine,
                effect: |_: &mut Engine, op: &EditOp, _: &mut Line| {
                    if *op == EditOp::Accept {
                        *accepted = true;
                    }
                    false
                },
                problem: None,
            };
            ed.key(&cfg, k, None, now, &mut host);
            assert_eq!(host.problem, None);
        }
    };
    let chars = |s: &str| s.chars().map(KeyCode::char).collect::<Vec<_>>();

    // An abbrFunc runs in the engine; the bindingFunc too.
    type_keys(&mut ed, &mut engine, &chars("gs "), &mut accepted);
    assert_eq!(ed.line.text, "git status ");
    type_keys(&mut ed, &mut engine, &[KeyCode::char('x').with(MOD_CTRL)], &mut accepted);
    assert_eq!(ed.line.text, "git status hi");

    // A text abbreviation with a cursor marker: the Space is dropped.
    ed.line = Line::default();
    type_keys(&mut ed, &mut engine, &chars("ll "), &mut accepted);
    assert_eq!((ed.line.text.as_str(), ed.line.cursor), ("ls -la ", 7));

    // Enter is the frontend's: it accepts the line, which then runs.
    type_keys(&mut ed, &mut engine, &[KeyCode::ENTER], &mut accepted);
    assert!(accepted);
    engine.run(2, "echo done").unwrap();
    assert_eq!(block(&rx, 2), ("done\r\n".to_string(), 0));

    engine.close().unwrap();
    std::fs::remove_dir_all(tmp).unwrap();
}
