//! Replays msh's resolver cases (msh --resolver-cases) on the Rust editor
//! and reports every event whose line, cursor, pending keys or fired
//! bindings differ (tools/check-resolver.sh).
//!
//! Usage: cargo run --release --example resolvercheck CASES.jsonl [--show N]

use msh_editor::abbr::{Abbr, AbbrAnswer, AbbrExpansion, AbbrMatch, AbbrPosition};
use msh_editor::edit::{EditOp, Line};
use msh_editor::keys::KeyCode;
use msh_editor::resolver::{Action, Binding, Config, EditState, Editor, Host, Keymap, Mode};
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

/// The engine's side, from the case: what each bindingFunc and abbrFunc
/// returns, and the answers Go gave to what the frontend would ask. A
/// question Go didn't answer is a difference: Rust asked something Go
/// didn't need.
struct Recorded {
    results: HashMap<u32, Option<Vec<EditOp>>>,
    abbr_answers: HashMap<u32, AbbrAnswer>,
    programs: HashMap<String, bool>,
    regexes: HashMap<(u32, String), bool>,
    unexpected: Vec<String>,
}

impl Host for Recorded {
    fn call(&mut self, binding: &Binding, _: &EditState) -> Option<Vec<EditOp>> {
        self.results[&binding.id].clone()
    }
    fn is_program(&mut self, word: &str) -> bool {
        self.programs.get(word).copied().unwrap_or_else(|| {
            self.unexpected.push(format!("is_program({word:?})"));
            false
        })
    }
    fn regex_matches(&mut self, abbr: &Abbr, word: &str) -> bool {
        self.regexes.get(&(abbr.id, word.to_string())).copied().unwrap_or_else(|| {
            self.unexpected.push(format!("regex_matches({}, {word:?})", abbr.id));
            false
        })
    }
    fn call_abbr(&mut self, abbr: &Abbr, _: &str, _: Option<&str>, _: &str) -> AbbrAnswer {
        self.abbr_answers[&abbr.id].clone()
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
        let mut host = Recorded {
            results: HashMap::new(),
            abbr_answers: HashMap::new(),
            programs: HashMap::new(),
            regexes: HashMap::new(),
            unexpected: Vec::new(),
        };
        let mut abbrs = Vec::new();
        for (i, a) in case["abbrs"].as_array().into_iter().flatten().enumerate() {
            let id = i as u32;
            let s = |k: &str| a[k].as_str().unwrap_or("").to_string();
            let b = |k: &str| a[k].as_bool().unwrap_or(false);
            let expansion = if b("func") {
                host.abbr_answers.insert(
                    id,
                    if b("declines") {
                        AbbrAnswer::Decline
                    } else if b("fails") {
                        AbbrAnswer::Fail
                    } else {
                        AbbrAnswer::Text(s("result"))
                    },
                );
                AbbrExpansion::Func(id)
            } else {
                AbbrExpansion::Text(s("text"))
            };
            abbrs.push(Abbr {
                id,
                matcher: if b("regex") { AbbrMatch::Regex(s("word")) } else { AbbrMatch::Word(s("word")) },
                position: match a["position"].as_str().unwrap() {
                    "command" => AbbrPosition::Command,
                    "anywhere" => AbbrPosition::Anywhere,
                    _ => AbbrPosition::ArgOf(
                        a["arg_of"].as_array().unwrap().iter().map(|c| c.as_str().unwrap().to_string()).collect(),
                    ),
                },
                expansion,
                marker: s("marker"),
            });
        }
        if let Some(p) = case["programs"].as_object() {
            for (w, v) in p {
                host.programs.insert(w.clone(), v.as_bool().unwrap());
            }
        }
        for rx in case["regexes"].as_array().into_iter().flatten() {
            host.regexes.insert(
                (rx["abbr"].as_u64().unwrap() as u32, rx["word"].as_str().unwrap().to_string()),
                rx["matched"].as_bool().unwrap(),
            );
        }
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
        let cfg = Config { keymap: Keymap::new(bindings), abbrs, names: Default::default() };
        let mut ed = Editor::new(case["line"].as_str().unwrap(), case["cursor"].as_u64().unwrap() as usize);
        let base = 1_000_000_000_000_000_000i64;
        let mut case_bad = false;
        for (ev, want) in case["events"].as_array().unwrap().iter().zip(case["expect"].as_array().unwrap()) {
            events += 1;
            let now = base + ev["at_ms"].as_i64().unwrap() * 1_000_000;
            if ev["enter"].as_bool().unwrap_or(false) {
                ed.fired.clear();
                ed.expand_on_enter(&cfg, &mut host);
            } else if ev["is_paste"].as_bool().unwrap_or(false) {
                ed.paste(ev["paste"].as_str().unwrap_or(""), now);
            } else {
                let code = KeyCode(ev["key"].as_u64().unwrap() as u32);
                let esc_of = ev["esc_of"].as_u64().map(|k| KeyCode(k as u32));
                ed.key(&cfg, code, esc_of, now, &mut host);
            }
            let got = (
                ed.line.text.as_str(),
                ed.line.cursor as u64,
                ed.pending().map(|k| k.0 as u64).collect::<Vec<_>>(),
                ed.fired.iter().map(|&i| i as u64).collect::<Vec<_>>(),
            );
            let u64s = |v: &Value| v.as_array().unwrap().iter().map(|x| x.as_u64().unwrap()).collect::<Vec<_>>();
            let expected = (want["line"].as_str().unwrap(), want["cursor"].as_u64().unwrap(), u64s(&want["pending"]), u64s(&want["fired"]));
            if got != expected || !host.unexpected.is_empty() {
                if !host.unexpected.is_empty() {
                    println!("  asked what Go didn't: {:?}", host.unexpected);
                    host.unexpected.clear();
                }
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
