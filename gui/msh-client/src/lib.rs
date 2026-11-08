//! The frontend's side of the `msh server` protocol (mshell/Server.go and
//! ServerEditor.go hold the specification): spawning the engine, framing,
//! events, the editor config, and the editor's requests.
//!
//! A reader thread turns the engine's frames into `Event`s for the
//! frontend, and routes each `call_result` to the request waiting for it.

use msh_editor::abbr::{Abbr, AbbrAnswer, AbbrExpansion, AbbrMatch, AbbrPosition};
use msh_editor::edit::{EditOp, Line};
use msh_editor::keys::KeyCode;
use msh_editor::resolver::{Action, Binding, Config, EditState, Host, Keymap, Mode};
use std::collections::HashMap;
use std::io::{self, BufReader, BufWriter, Read, Write};
use std::process::{Child, ChildStdin, Command, Stdio};
use std::sync::mpsc::{self, Receiver, Sender};
use std::sync::{Arc, Mutex};

pub const PROTOCOL_VERSION: u32 = 1;

// Frontend to engine.
const MSG_RUN: u8 = 0x01;
const MSG_INPUT: u8 = 0x02;
const MSG_RESIZE: u8 = 0x03;
const MSG_BINDING_CALL: u8 = 0x04;
const MSG_ABBR_CALL: u8 = 0x05;
const MSG_REGEX_MATCH: u8 = 0x06;
const MSG_IS_PROGRAM: u8 = 0x07;
const MSG_EDIT: u8 = 0x08;
const MSG_RUN_QUOTE: u8 = 0x09;
const MSG_FM_INPUT: u8 = 0x0A;
const MSG_LINE_KIND: u8 = 0x0B;

// Engine to frontend.
const MSG_HELLO: u8 = 0x81;
const MSG_BLOCK_STARTED: u8 = 0x82;
const MSG_PTY_OUTPUT: u8 = 0x83;
const MSG_BLOCK_FINISHED: u8 = 0x84;
const MSG_EXIT: u8 = 0x85;
const MSG_EDITOR_CONFIG: u8 = 0x86;
const MSG_CALL_RESULT: u8 = 0x87;
const MSG_PROMPT: u8 = 0x88;
const MSG_OPS: u8 = 0x89;
const MSG_FM_VIEW: u8 = 0x8A;

const CALL_NO: u8 = 0;
const CALL_YES: u8 = 1;
const CALL_FAILED: u8 = 2;
const CALL_STALE: u8 = 3;

/// What the engine says, in order.
#[derive(Debug)]
pub enum Event {
    Hello { version: u32, pid: u32 },
    BlockStarted { id: u64, start_unix_ns: i64, cwd: String },
    /// Output read from the PTY, tagged with the block current when it was
    /// read (0: outside any block).
    PtyOutput { id: u64, bytes: Vec<u8> },
    BlockFinished { id: u64, exit_code: i32, duration_ns: u64 },
    /// A new editor config arrived; `Engine::take_config` has it.
    EditorConfig { version: u32 },
    /// The prompt for the next line: text with SGR and OSC 8 escapes and
    /// \r\n line breaks, and the window title it sets, if any.
    Prompt { title: Option<String>, text: Vec<u8> },
    /// The file manager's view (mshFileManager under msh server): the GUI
    /// shows it as a dialog while `shown`.
    FileManager(FmView),
    /// What an editRunOps quote returned, before its block_finished.
    Ops { id: u64, ops: Vec<EditOp> },
    /// The engine ran `exit`; it exits after this.
    Exit { code: i32 },
    /// The protocol closed or broke.
    Closed { error: Option<String> },
}

/// An `editor_config`'s ops name quotations by id: `EditOp::Run` and
/// `RunOps` hold them as they are.
pub fn decode_config(b: &[u8]) -> Result<Config, String> {
    let mut r = Reader { b };
    let mut bindings = Vec::new();
    for id in 0..r.u32()? {
        let mode = match r.u8()? {
            0 => Mode::Insert,
            1 => Mode::Complete,
            m => return Err(format!("unknown mode {m}")),
        };
        let within = r.u32()? as i64 * 1_000_000;
        let idle_before = r.u32()? as i64 * 1_000_000;
        let keys = (0..r.u8()?).map(|_| r.u32().map(KeyCode)).collect::<Result<Vec<_>, _>>()?;
        let action = match r.u8()? {
            0 => Action::Ops((0..r.u16()?).map(|_| r.op()).collect::<Result<_, _>>()?),
            1 => Action::Func(r.u32()?),
            a => return Err(format!("unknown action {a}")),
        };
        bindings.push(Binding { id, keys, mode, within, idle_before, action });
    }
    let mut abbrs = Vec::new();
    for id in 0..r.u32()? {
        let regex = r.u8()? == 1;
        let word = r.str()?;
        let position = match r.u8()? {
            0 => AbbrPosition::Command,
            1 => AbbrPosition::Anywhere,
            2 => AbbrPosition::ArgOf((0..r.u16()?).map(|_| r.str()).collect::<Result<_, _>>()?),
            p => return Err(format!("unknown position {p}")),
        };
        let expansion = match r.u8()? {
            0 => AbbrExpansion::Text(r.str()?),
            1 => AbbrExpansion::Func(r.u32()?),
            e => return Err(format!("unknown expansion {e}")),
        };
        let marker = r.str()?;
        let matcher = if regex { AbbrMatch::Regex(word) } else { AbbrMatch::Word(word) };
        abbrs.push(Abbr { id, matcher, position, expansion, marker });
    }
    if !r.b.is_empty() {
        return Err(format!("{} bytes after the config", r.b.len()));
    }
    Ok(Config { keymap: Keymap::new(bindings), abbrs })
}

/// The EditOp members in the engine's order (Bindings.go's editOpKind).
const EDIT_OP_MEMBERS: [&str; 36] = [
    "editInsert", "editSelfInsert", "editSetLine",
    "editLeft", "editRight", "editWordLeft", "editWordRight", "editLineStart", "editLineEnd",
    "editBackspace", "editDelete", "editDeleteWordLeft", "editDeleteToStart", "editDeleteToEnd",
    "editExpandAbbr",
    "editAccept", "editExit", "editRun", "editRunOps",
    "editForward", "editAcceptSuggestion",
    "editHistoryPrev", "editHistoryNext", "editHistorySearchBack", "editHistorySearchForward",
    "editLastArg", "editComplete", "editCompleteBack",
    "editCompleteNext", "editCompletePrev", "editCompleteLeft", "editCompleteRight", "editCompleteAccept",
    "editClearScreen", "editRemeasure", "editMessage",
];

struct Reader<'a> {
    b: &'a [u8],
}

impl Reader<'_> {
    fn take(&mut self, n: usize) -> Result<&[u8], String> {
        if self.b.len() < n {
            return Err("message ends early".into());
        }
        let (v, rest) = self.b.split_at(n);
        self.b = rest;
        Ok(v)
    }
    fn u8(&mut self) -> Result<u8, String> {
        Ok(self.take(1)?[0])
    }
    fn u16(&mut self) -> Result<u16, String> {
        Ok(u16::from_le_bytes(self.take(2)?.try_into().unwrap()))
    }
    fn u32(&mut self) -> Result<u32, String> {
        Ok(u32::from_le_bytes(self.take(4)?.try_into().unwrap()))
    }
    fn u64(&mut self) -> Result<u64, String> {
        Ok(u64::from_le_bytes(self.take(8)?.try_into().unwrap()))
    }
    fn str(&mut self) -> Result<String, String> {
        let n = self.u32()? as usize;
        String::from_utf8(self.take(n)?.to_vec()).map_err(|e| e.to_string())
    }
    fn rest(&mut self) -> &[u8] {
        std::mem::take(&mut self.b)
    }
    fn op(&mut self) -> Result<EditOp, String> {
        let kind = self.u8()? as usize;
        let member = *EDIT_OP_MEMBERS.get(kind).ok_or_else(|| format!("unknown op {kind}"))?;
        Ok(match member {
            "editInsert" | "editMessage" => EditOp::from_member(member, Some(&self.str()?), None).unwrap(),
            "editSetLine" => {
                let (a, b) = (self.str()?, self.str()?);
                EditOp::SetLine(a, b)
            }
            "editRun" => EditOp::Run(self.u32()?),
            "editRunOps" => EditOp::RunOps(self.u32()?),
            _ => EditOp::from_member(member, None, None).unwrap(),
        })
    }
}

/// A `call_result`: the status and what follows it.
struct CallResult {
    status: u8,
    data: Vec<u8>,
}

type Pending = Arc<Mutex<HashMap<u32, Sender<CallResult>>>>;

/// The newest editor config the reader has seen, for the frontend to switch
/// to before it handles another key: the engine sends a changed config
/// before the reply to the request that changed it.
type LatestConfig = Arc<Mutex<Option<(u32, Config)>>>;

/// The engine process and the frontend's half of the protocol.
pub struct Engine {
    child: Child,
    out: BufWriter<ChildStdin>,
    pending: Pending,
    latest: LatestConfig,
    next_request: u32,
    /// The version of the editor config in use, which requests name.
    pub config_version: u32,
}

impl Engine {
    /// Starts `msh server` (`program` is the msh binary) and the reader
    /// thread, which gives each event to `on_event`, from that thread.
    pub fn spawn(program: &str, on_event: impl FnMut(Event) + Send + 'static) -> io::Result<Engine> {
        let mut cmd = Command::new(program);
        cmd.arg("server");
        Engine::spawn_command(cmd, on_event)
    }

    /// As `spawn`, with a command set up by the caller (`msh server` with
    /// its environment); its stdin and stdout become the protocol.
    pub fn spawn_command(mut cmd: Command, on_event: impl FnMut(Event) + Send + 'static) -> io::Result<Engine> {
        let mut child = cmd.stdin(Stdio::piped()).stdout(Stdio::piped()).spawn()?;
        let stdin = child.stdin.take().unwrap();
        let stdout = child.stdout.take().unwrap();
        let pending: Pending = Arc::default();
        let latest: LatestConfig = Arc::default();
        let (routes, slot) = (pending.clone(), latest.clone());
        std::thread::Builder::new().name("engine".into()).spawn(move || read_events(stdout, routes, slot, on_event))?;
        Ok(Engine { child, out: BufWriter::new(stdin), pending, latest, next_request: 1, config_version: 0 })
    }

    /// A newer editor config than the one in use, if one has arrived. The
    /// frontend calls this before each key and after each request.
    pub fn take_config(&mut self) -> Option<Config> {
        let (version, config) = self.latest.lock().unwrap().take()?;
        if version <= self.config_version {
            return None;
        }
        self.config_version = version;
        Some(config)
    }

    fn send(&mut self, kind: u8, fixed: &[u8], tail: &[u8]) -> io::Result<()> {
        let len = (1 + fixed.len() + tail.len()) as u32;
        self.out.write_all(&len.to_le_bytes())?;
        self.out.write_all(&[kind])?;
        self.out.write_all(fixed)?;
        self.out.write_all(tail)?;
        self.out.flush()
    }

    pub fn run(&mut self, block: u64, source: &str) -> io::Result<()> {
        self.send(MSG_RUN, &block.to_le_bytes(), source.as_bytes())
    }

    /// Runs an editRun (or with `state`, editRunOps) quote as a block; its
    /// EditState is the line around the cursor.
    pub fn run_quote(&mut self, block: u64, quote: u32, with_state: bool, before: &str, after: &str) -> io::Result<()> {
        let mut p = Vec::new();
        p.extend_from_slice(&block.to_le_bytes());
        p.extend_from_slice(&self.config_version.to_le_bytes());
        p.extend_from_slice(&quote.to_le_bytes());
        p.push(with_state as u8);
        p.extend_from_slice(&(before.len() as u32).to_le_bytes());
        p.extend_from_slice(before.as_bytes());
        self.send(MSG_RUN_QUOTE, &p, after.as_bytes())
    }

    /// A key for the file manager, as the editor names keys; `esc_of` when
    /// the legacy encoding sent it as ESC and that key.
    pub fn fm_key(&mut self, key: KeyCode, esc_of: Option<KeyCode>) -> io::Result<()> {
        let mut p = vec![0u8];
        p.extend_from_slice(&key.0.to_le_bytes());
        p.extend_from_slice(&esc_of.map_or(0, |k| k.0).to_le_bytes());
        self.send(MSG_FM_INPUT, &p, &[])
    }

    pub fn fm_paste(&mut self, text: &str) -> io::Result<()> {
        self.send(MSG_FM_INPUT, &[1], text.as_bytes())
    }

    /// The dialog's size in cells, which the file manager lays out for.
    pub fn fm_resize(&mut self, cols: u16, rows: u16) -> io::Result<()> {
        let mut p = vec![2u8];
        p.extend_from_slice(&cols.to_le_bytes());
        p.extend_from_slice(&rows.to_le_bytes());
        self.send(MSG_FM_INPUT, &p, &[])
    }

    pub fn fm_close(&mut self) -> io::Result<()> {
        self.send(MSG_FM_INPUT, &[3], &[])
    }

    /// Keys for the running program, written to the PTY.
    pub fn input(&mut self, bytes: &[u8]) -> io::Result<()> {
        self.send(MSG_INPUT, &[], bytes)
    }

    pub fn resize(&mut self, cols: u16, rows: u16, width_px: u16, height_px: u16) -> io::Result<()> {
        let mut b = Vec::with_capacity(8);
        for v in [cols, rows, width_px, height_px] {
            b.extend_from_slice(&v.to_le_bytes());
        }
        self.send(MSG_RESIZE, &b, &[])
    }

    /// Sends a request and waits for its result. Events that arrive in the
    /// meantime (a new config before the reply) go to `on_event` as usual.
    fn call(&mut self, kind: u8, payload: &[u8]) -> io::Result<CallResult> {
        let request = self.next_request;
        self.next_request += 1;
        let (tx, rx): (Sender<CallResult>, Receiver<CallResult>) = mpsc::channel();
        self.pending.lock().unwrap().insert(request, tx);
        self.send(kind, &request.to_le_bytes(), payload)?;
        rx.recv().map_err(|_| io::Error::new(io::ErrorKind::BrokenPipe, "the engine closed the protocol"))
    }

    /// Runs a bindingFunc: its ops, or None when it declined. A failure's
    /// message is in the Err of the inner result. Stale: the config changed;
    /// the frontend resolves the keys again with the new one.
    pub fn binding_call(&mut self, quote: u32, state: &EditState) -> io::Result<Answer<Vec<EditOp>>> {
        let mut p = Vec::new();
        p.extend_from_slice(&self.config_version.to_le_bytes());
        p.extend_from_slice(&quote.to_le_bytes());
        p.push(state.mode as u8);
        p.push(state.keys.len() as u8);
        for k in state.keys {
            p.extend_from_slice(&k.0.to_le_bytes());
        }
        p.extend_from_slice(&(state.before.len() as u32).to_le_bytes());
        p.extend_from_slice(state.before.as_bytes());
        p.extend_from_slice(state.after.as_bytes());
        let r = self.call(MSG_BINDING_CALL, &p)?;
        Ok(match r.status {
            CALL_YES => {
                let mut rd = Reader { b: &r.data };
                let n = rd.u16().map_err(bad)?;
                Answer::Yes((0..n).map(|_| rd.op()).collect::<Result<_, _>>().map_err(bad)?)
            }
            status => Answer::from_status(status, &r.data),
        })
    }

    /// Runs an abbrFunc on a word.
    pub fn abbr_call(&mut self, quote: u32, word: &str, command: Option<&str>, line: &str) -> io::Result<Answer<String>> {
        let mut p = Vec::new();
        p.extend_from_slice(&self.config_version.to_le_bytes());
        p.extend_from_slice(&quote.to_le_bytes());
        p.push(command.is_some() as u8);
        for s in [word, command.unwrap_or("")] {
            p.extend_from_slice(&(s.len() as u32).to_le_bytes());
            p.extend_from_slice(s.as_bytes());
        }
        p.extend_from_slice(line.as_bytes());
        let r = self.call(MSG_ABBR_CALL, &p)?;
        Ok(match r.status {
            CALL_YES => Answer::Yes(String::from_utf8_lossy(&r.data).into_owned()),
            status => Answer::from_status(status, &r.data),
        })
    }

    /// Whether the regex of abbreviation `abbr` (its index in the config)
    /// matches a word.
    pub fn regex_match(&mut self, abbr: u32, word: &str) -> io::Result<Answer<()>> {
        let mut p = Vec::new();
        p.extend_from_slice(&self.config_version.to_le_bytes());
        p.extend_from_slice(&abbr.to_le_bytes());
        p.extend_from_slice(word.as_bytes());
        let r = self.call(MSG_REGEX_MATCH, &p)?;
        Ok(match r.status {
            CALL_YES => Answer::Yes(()),
            status => Answer::from_status(status, &r.data),
        })
    }

    /// Runs a completion op (`EditOp::Complete` and the rest) on the line in
    /// the engine, which keeps the completion cycle between requests.
    pub fn edit(&mut self, op: &EditOp, line: &str, cursor: usize) -> io::Result<Edited> {
        let member = op_member(op).ok_or_else(|| bad(format!("{op:?} is not an op the engine edits")))?;
        let kind = EDIT_OP_MEMBERS.iter().position(|m| *m == member).unwrap() as u8;
        let mut p = vec![kind];
        p.extend_from_slice(&(cursor as u32).to_le_bytes());
        p.extend_from_slice(line.as_bytes());
        let r = self.call(MSG_EDIT, &p)?;
        if r.status != CALL_YES {
            return Err(bad(String::from_utf8_lossy(&r.data).into_owned()));
        }
        let mut rd = Reader { b: &r.data };
        let cursor = rd.u32().map_err(bad)? as usize;
        let cycling = rd.u8().map_err(bad)? == 1;
        let selected = rd.u32().map_err(bad)? as i32;
        let bell = rd.u8().map_err(bad)? == 1;
        let candidates = (0..rd.u16().map_err(bad)?).map(|_| rd.str()).collect::<Result<_, _>>().map_err(bad)?;
        let line = String::from_utf8_lossy(rd.rest()).into_owned();
        Ok(Edited { line, cursor, cycling, selected: (selected >= 0).then_some(selected as usize), bell, candidates })
    }

    /// Whether a line runs as a command line (the simple CLI parser) rather
    /// than as mshell, by the rule the engine runs lines with.
    pub fn is_command_line(&mut self, line: &str) -> io::Result<bool> {
        Ok(self.call(MSG_LINE_KIND, line.as_bytes())?.status == CALL_YES)
    }

    pub fn is_program(&mut self, word: &str) -> io::Result<bool> {
        Ok(self.call(MSG_IS_PROGRAM, word.as_bytes())?.status == CALL_YES)
    }

    /// Closes the protocol; the engine exits, hanging up its PTY.
    pub fn close(mut self) -> io::Result<std::process::ExitStatus> {
        drop(self.out);
        self.child.wait()
    }
}

fn bad(e: String) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, e)
}

/// The engine's answer to a request.
#[derive(Debug, PartialEq, Eq)]
pub enum Answer<T> {
    Yes(T),
    No,
    /// The quotation failed; the message to show.
    Failed(String),
    /// The config changed since the request named it.
    Stale,
}

impl<T> Answer<T> {
    fn from_status(status: u8, data: &[u8]) -> Answer<T> {
        match status {
            CALL_NO => Answer::No,
            CALL_FAILED => Answer::Failed(String::from_utf8_lossy(data).into_owned()),
            CALL_STALE => Answer::Stale,
            _ => Answer::No,
        }
    }

    /// For the editor: an abbrFunc that failed stops trying abbreviations.
    pub fn abbr(self) -> AbbrAnswer
    where
        T: Into<String>,
    {
        match self {
            Answer::Yes(t) => AbbrAnswer::Text(t.into()),
            Answer::No | Answer::Stale => AbbrAnswer::Decline,
            Answer::Failed(_) => AbbrAnswer::Fail,
        }
    }
}

fn read_events(stdout: impl Read, pending: Pending, latest: LatestConfig, mut on_event: impl FnMut(Event)) {
    let mut r = BufReader::with_capacity(64 << 10, stdout);
    let mut buf = Vec::new();
    let error = loop {
        let mut head = [0u8; 4];
        if let Err(e) = r.read_exact(&mut head) {
            break (e.kind() != io::ErrorKind::UnexpectedEof).then(|| e.to_string());
        }
        let n = u32::from_le_bytes(head) as usize;
        if n == 0 || n > 64 << 20 {
            break Some(format!("frame length {n}"));
        }
        buf.resize(n, 0);
        if let Err(e) = r.read_exact(&mut buf) {
            break Some(e.to_string());
        }
        let mut rd = Reader { b: &buf[1..] };
        let event = match decode_event(buf[0], &mut rd) {
            Ok(Frame::Event(e)) => e,
            Ok(Frame::Config(version, config)) => {
                *latest.lock().unwrap() = Some((version, config));
                Event::EditorConfig { version }
            }
            Ok(Frame::CallResult) => {
                // A call_result: to the request waiting for it.
                let request = u32::from_le_bytes(buf[1..5].try_into().unwrap());
                if let Some(tx) = pending.lock().unwrap().remove(&request) {
                    let _ = tx.send(CallResult { status: buf[5], data: buf[6..].to_vec() });
                }
                continue;
            }
            Err(e) => break Some(e),
        };
        on_event(event);
    };
    // Anyone still waiting gets an error.
    pending.lock().unwrap().clear();
    on_event(Event::Closed { error });
}

enum Frame {
    Event(Event),
    Config(u32, Config),
    CallResult,
}

fn decode_event(kind: u8, r: &mut Reader) -> Result<Frame, String> {
    Ok(Frame::Event(match kind {
        MSG_HELLO => Event::Hello { version: r.u32()?, pid: r.u32()? },
        MSG_BLOCK_STARTED => Event::BlockStarted {
            id: r.u64()?,
            start_unix_ns: r.u64()? as i64,
            cwd: String::from_utf8_lossy(r.rest()).into_owned(),
        },
        MSG_PTY_OUTPUT => Event::PtyOutput { id: r.u64()?, bytes: r.rest().to_vec() },
        MSG_BLOCK_FINISHED => Event::BlockFinished { id: r.u64()?, exit_code: r.u32()? as i32, duration_ns: r.u64()? },
        MSG_EXIT => Event::Exit { code: r.u32()? as i32 },
        MSG_FM_VIEW => Event::FileManager(decode_fm_view(r)?),
        MSG_OPS => {
            let id = r.u64()?;
            Event::Ops { id, ops: (0..r.u16()?).map(|_| r.op()).collect::<Result<_, _>>()? }
        }
        MSG_PROMPT => {
            let has_title = r.u8()? == 1;
            let title = r.str()?;
            Event::Prompt { title: has_title.then_some(title), text: r.rest().to_vec() }
        }
        MSG_EDITOR_CONFIG => {
            let version = r.u32()?;
            return Ok(Frame::Config(version, decode_config(r.rest())?));
        }
        MSG_CALL_RESULT => {
            if r.b.len() < 5 {
                return Err("short call_result".into());
            }
            return Ok(Frame::CallResult);
        }
        k => return Err(format!("unknown message 0x{k:02x}")),
    }))
}

/// The editor's host backed by the engine: bindingFuncs, abbrFuncs, regex
/// abbreviations and program words go to the engine; effects to `effect`,
/// which is given the engine too (completion runs there).
pub struct EngineHost<'a, F: FnMut(&mut Engine, &EditOp, &mut Line) -> bool> {
    pub engine: &'a mut Engine,
    pub effect: F,
    /// The message of the last quotation that failed, to show.
    pub problem: Option<String>,
}

impl<F: FnMut(&mut Engine, &EditOp, &mut Line) -> bool> Host for EngineHost<'_, F> {
    fn call(&mut self, binding: &Binding, state: &EditState) -> Option<Vec<EditOp>> {
        let Action::Func(quote) = binding.action else { return None };
        match self.engine.binding_call(quote, state) {
            Ok(Answer::Yes(ops)) => Some(ops),
            Ok(Answer::Failed(problem)) => {
                self.problem = Some(problem);
                None
            }
            _ => None,
        }
    }
    fn effect(&mut self, op: &EditOp, line: &mut Line) -> bool {
        (self.effect)(self.engine, op, line)
    }
    fn is_program(&mut self, word: &str) -> bool {
        self.engine.is_program(word).unwrap_or(false)
    }
    fn regex_matches(&mut self, abbr: &Abbr, word: &str) -> bool {
        matches!(self.engine.regex_match(abbr.id, word), Ok(Answer::Yes(())))
    }
    fn call_abbr(&mut self, abbr: &Abbr, word: &str, command: Option<&str>, line: &str) -> AbbrAnswer {
        let AbbrExpansion::Func(quote) = abbr.expansion else { return AbbrAnswer::Decline };
        match self.engine.abbr_call(quote, word, command, line) {
            Ok(Answer::Failed(problem)) => {
                self.problem = Some(problem);
                AbbrAnswer::Fail
            }
            Ok(answer) => answer.abbr(),
            Err(_) => AbbrAnswer::Fail,
        }
    }
}

/// The line after a completion op in the engine.
#[derive(Debug, PartialEq, Eq)]
pub struct Edited {
    pub line: String,
    pub cursor: usize,
    /// A completion cycle is on: the editor is in complete mode.
    pub cycling: bool,
    pub selected: Option<usize>,
    pub bell: bool,
    pub candidates: Vec<String>,
}

/// The member name of a completion op.
fn op_member(op: &EditOp) -> Option<&'static str> {
    Some(match op {
        EditOp::Complete => "editComplete",
        EditOp::CompleteBack => "editCompleteBack",
        EditOp::CompleteNext => "editCompleteNext",
        EditOp::CompletePrev => "editCompletePrev",
        EditOp::CompleteLeft => "editCompleteLeft",
        EditOp::CompleteRight => "editCompleteRight",
        EditOp::CompleteAccept => "editCompleteAccept",
        _ => return None,
    })
}

/// The file manager's view (FileManagerGUI.go).
#[derive(Debug, Default, Clone)]
pub struct FmView {
    /// Shown as a dialog; false when it ends or lends the pane to a program.
    pub shown: bool,
    /// The help screen: `preview` holds its lines.
    pub help: bool,
    pub header: String,
    pub clipboard: String,
    pub total: u32,
    pub offset: u32,
    pub entries: Vec<FmEntry>,
    pub preview: Vec<String>,
    /// 0 none, 1 the directory summary, 2 a message.
    pub status_kind: u8,
    pub status: String,
    /// The prompt line: its label, text, and cursor (bytes).
    pub line: Option<(String, String, usize)>,
    /// The key popup, or a question.
    pub overlay: Vec<String>,
}

#[derive(Debug, Default, Clone)]
pub struct FmEntry {
    pub name: String,
    pub size: String,
    pub marker: String,
    pub dir: bool,
    pub cursor: bool,
    pub selected: bool,
    pub cut: bool,
    pub copied: bool,
}

fn decode_fm_view(r: &mut Reader) -> Result<FmView, String> {
    let flags = r.u8()?;
    let mut v = FmView { shown: flags & 1 != 0, help: flags & 2 != 0, ..FmView::default() };
    v.header = r.str()?;
    v.clipboard = r.str()?;
    v.total = r.u32()?;
    v.offset = r.u32()?;
    for _ in 0..r.u16()? {
        let f = r.u8()?;
        v.entries.push(FmEntry {
            name: r.str()?,
            size: r.str()?,
            marker: r.str()?,
            dir: f & 1 != 0,
            cursor: f & 2 != 0,
            selected: f & 4 != 0,
            cut: f & 8 != 0,
            copied: f & 16 != 0,
        });
    }
    for _ in 0..r.u16()? {
        v.preview.push(r.str()?);
    }
    v.status_kind = r.u8()?;
    v.status = r.str()?;
    let has_line = r.u8()? == 1;
    let (label, text, cursor) = (r.str()?, r.str()?, r.u32()? as usize);
    v.line = has_line.then_some((label, text, cursor));
    for _ in 0..r.u16()? {
        v.overlay.push(r.str()?);
    }
    Ok(v)
}
