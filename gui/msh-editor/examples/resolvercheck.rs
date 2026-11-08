//! Replays msh's resolver cases (msh --resolver-cases) on the Rust editor
//! and reports every event whose line, cursor, pending keys or fired
//! bindings differ (tools/check-resolver.sh).
//!
//! Usage: cargo run --release --example resolvercheck CASES.jsonl [--show N]

use msh_editor::edit::{EditOp, Line};
use msh_editor::keys::KeyCode;
use msh_editor::resolver::{Action, Binding, EditState, Editor, Host, Keymap, Mode};
use serde_json::Value;
use std::collections::HashMap;
use std::io::BufRead;

fn ops(v: &Value) -> Vec<EditOp> {
    v.as_array()
        .map(|a| {
            a.iter()
                .map(|op| {
                    let s = |k: &str| op.get(k).and_then(Value::as_str);
                    let member = s("op").unwrap();
                    // An empty payload is omitted from the JSON; ops that
                    // take one get "" back.
                    let (a, b) = match member {
                        "editInsert" | "editMessage" => (Some(s("a").unwrap_or("")), None),
                        "editSetLine" => (Some(s("a").unwrap_or("")), Some(s("b").unwrap_or(""))),
                        _ => (None, None),
                    };
                    EditOp::from_member(member, a, b).unwrap_or_else(|| panic!("unknown op {op}"))
                })
                .collect()
        })
        .unwrap_or_default()
}

/// The engine's side, from the case: what each bindingFunc returns.
struct Recorded {
    results: HashMap<u32, Option<Vec<EditOp>>>,
}

impl Host for Recorded {
    fn call(&mut self, binding: &Binding, _: &EditState) -> Option<Vec<EditOp>> {
        self.results[&binding.id].clone()
    }
    fn effect(&mut self, op: &EditOp, _: &mut Line) -> bool {
        // The cases use editMessage as their only effect.
        assert!(matches!(op, EditOp::Message(_)), "unexpected effect {op:?}");
        false
    }
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    let path = &args[1];
    let show: usize = args.iter().position(|a| a == "--show").map_or(5, |i| args[i + 1].parse().unwrap());
    let file = std::io::BufReader::new(std::fs::File::open(path).unwrap());
    let (mut cases, mut events, mut bad_cases, mut bad_events, mut shown) = (0, 0, 0, 0, 0);
    for line in file.lines() {
        let case: Value = serde_json::from_str(&line.unwrap()).unwrap();
        cases += 1;
        let mut bindings = Vec::new();
        let mut host = Recorded { results: HashMap::new() };
        for (i, b) in case["bindings"].as_array().unwrap().iter().enumerate() {
            let id = i as u32;
            let is_func = b["func"].as_bool().unwrap_or(false);
            if is_func {
                let result = if b["declines"].as_bool().unwrap_or(false) { None } else { Some(ops(&b["func_result"])) };
                host.results.insert(id, result);
            }
            if !b["active"].as_bool().unwrap() {
                continue;
            }
            bindings.push(Binding {
                id,
                keys: b["keys"].as_array().unwrap().iter().map(|k| KeyCode(k.as_u64().unwrap() as u32)).collect(),
                mode: Mode::Insert,
                within: b["within_ms"].as_i64().unwrap() * 1_000_000,
                idle_before: b["idle_before_ms"].as_i64().unwrap() * 1_000_000,
                action: if is_func { Action::Func(id) } else { Action::Ops(ops(&b["ops"])) },
            });
        }
        let km = Keymap::new(bindings);
        let mut ed = Editor::new(case["line"].as_str().unwrap(), case["cursor"].as_u64().unwrap() as usize);
        let base = 1_000_000_000_000_000_000i64;
        let mut case_bad = false;
        for (ev, want) in case["events"].as_array().unwrap().iter().zip(case["expect"].as_array().unwrap()) {
            events += 1;
            let now = base + ev["at_ms"].as_i64().unwrap() * 1_000_000;
            if ev["is_paste"].as_bool().unwrap_or(false) {
                ed.paste(ev["paste"].as_str().unwrap_or(""), now);
            } else {
                let code = KeyCode(ev["key"].as_u64().unwrap() as u32);
                let esc_of = ev["esc_of"].as_u64().map(|k| KeyCode(k as u32));
                ed.key(&km, code, esc_of, now, &mut host);
            }
            let got = (
                ed.line.text.as_str(),
                ed.line.cursor as u64,
                ed.pending().map(|k| k.0 as u64).collect::<Vec<_>>(),
                ed.fired.iter().map(|&i| i as u64).collect::<Vec<_>>(),
            );
            let u64s = |v: &Value| v.as_array().unwrap().iter().map(|x| x.as_u64().unwrap()).collect::<Vec<_>>();
            let expected = (want["line"].as_str().unwrap(), want["cursor"].as_u64().unwrap(), u64s(&want["pending"]), u64s(&want["fired"]));
            if got != expected {
                bad_events += 1;
                if !case_bad && shown < show {
                    shown += 1;
                    println!("--- case {} event {:?}\n  want {:?}\n  got  {:?}\n  case {}", cases - 1, ev, expected, got, case);
                }
                case_bad = true;
            }
        }
        bad_cases += case_bad as usize;
    }
    println!("{} cases, {} events: {} cases and {} events differ", cases, events, bad_cases, bad_events);
    std::process::exit(if bad_events > 0 { 1 } else { 0 });
}
