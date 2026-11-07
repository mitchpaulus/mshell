//! Startup spike for the mshell GUI frontend (ai/gui-terminal.typ, "Current recommendation").
//!
//! Measures launch to first frame: window, wgpu device, one font file loaded
//! directly (no system font scan), an ASCII glyph atlas, and one frame of a
//! cell grid drawn as instanced quads. Every stage is traced to stderr.
//!
//! The cell grid is filled from a text file for now. When libghostty-vt is
//! available, `CellInstance`s are filled from its render state instead.
//!
//! Options (each experiment is a switch, so they can be compared):
//!   --serial                 do GPU setup and font work one after another on
//!                            the main thread, to time each stage on its own
//!   --exit-after-first-frame print the trace and exit once a GPU frame is presented
//!   --backend B              vulkan (default), gl, or all
//!   --gpu-early              start Vulkan setup at main entry, before the
//!                            display connection (Vulkan doesn't need it)
//!   --early-pipeline         upload the atlas and build the pipeline on the GPU
//!                            thread, before the window exists
//!   --vk-cache               remember which Vulkan driver worked and load only
//!                            that one next time (VK_DRIVER_FILES)
//!   --no-implicit-layers     don't load implicit Vulkan layers
//!   --preload-libs           do everything the main thread needs that takes glibc's
//!                            loader lock (dlopen of libwayland-client and
//!                            libxkbcommon, dlsym of getrandom, thread_local
//!                            destructor registration) before the GPU thread starts,
//!                            so the main thread never waits for the driver load
//!   --quiet-malloc           tune glibc malloc (one arena, 64 MB top pad, no mmap
//!                            for big blocks) so allocating doesn't call brk/mmap,
//!                            which wait for the driver load's mprotects
//!   --atlas-cache            keep the rasterised glyph atlas (and cell metrics)
//!                            between launches, keyed by font file and size
//!   --cpu-first-frame        draw the first frame on the CPU (wl_shm) while the
//!                            GPU starts, then switch the surface to wgpu
//!   --gpu-after-first-frame  start the GPU only once the CPU frame is presented, so
//!                            the driver load overlaps nothing (needs --cpu-first-frame)
//!   --no-gpu                 never start the GPU: the CPU frame stays (to compare
//!                            the two frames' pixels)
//!   --present-mode M         mailbox (default if supported) or fifo
//!   --file PATH              text to show (default: lib/std.msh)
//!   --font PATH              font file (default: Noto Sans Mono)
//!   --size PX                font size in logical pixels (default: 16)
//!   --scale S                display scale to draw the first frame at (default:
//!                            the last one seen, cached; else 1)
//!   --cols N --rows N        grid size (default: 100 x 30)
//! If FC_LAUNCH_NS (CLOCK_MONOTONIC at launch) is set, the time from launch
//! to main is traced too.

// With the no-rt-init feature, main is a plain C main: Rust's runtime setup
// (stack-overflow handler, which reads /proc/self/maps) is skipped. ~0.4 ms.
#![cfg_attr(feature = "no-rt-init", no_main)]

use std::path::PathBuf;
use std::sync::mpsc;
use std::sync::{Arc, Mutex, OnceLock};
use std::time::{Duration, Instant};

use winit::application::ApplicationHandler;
use winit::dpi::PhysicalSize;
use winit::event::WindowEvent;
use winit::event_loop::{ActiveEventLoop, ControlFlow, EventLoop, EventLoopProxy, OwnedDisplayHandle};
use winit::window::{Window, WindowId};

mod shm;

// ---------------------------------------------------------------- tracing

static START: OnceLock<Instant> = OnceLock::new();
static TRACE: Mutex<Vec<(Duration, &'static str, String)>> = Mutex::new(Vec::new());
static THREAD_STATS: OnceLock<bool> = OnceLock::new();

/// With SPIKE_THREADSTATS set, each stage also records the calling thread's
/// CPU time, context switches and page faults so far, to tell a thread that
/// is blocked (wall time grows, CPU time doesn't, voluntary switches) from
/// one that is slowed (CPU time grows too).
fn thread_stats() -> String {
    if !*THREAD_STATS.get_or_init(|| std::env::var_os("SPIKE_THREADSTATS").is_some()) {
        return String::new();
    }
    // SAFETY: getrusage fills the struct it is given.
    let mut ru: libc::rusage = unsafe { std::mem::zeroed() };
    unsafe { libc::getrusage(libc::RUSAGE_THREAD, &mut ru) };
    let ms = |t: libc::timeval| t.tv_sec as f64 * 1e3 + t.tv_usec as f64 / 1e3;
    let name = std::thread::current().name().unwrap_or("?").to_string();
    format!(
        "  [{name:>5} user {:6.2} sys {:6.2} ms, vcsw {:3}, ivcsw {:3}, minflt {:5}]",
        ms(ru.ru_utime),
        ms(ru.ru_stime),
        ru.ru_nvcsw,
        ru.ru_nivcsw,
        ru.ru_minflt
    )
}

fn trace(stage: &'static str) {
    let t = START.get().expect("START set in main").elapsed();
    let stats = thread_stats();
    TRACE.lock().unwrap().push((t, stage, stats));
}

fn print_trace() {
    let mut entries = TRACE.lock().unwrap().clone();
    entries.sort_by_key(|e| e.0);
    for (t, stage, stats) in entries {
        eprintln!("{:8.2} ms  {stage}{stats}", t.as_secs_f64() * 1e3);
    }
}

fn monotonic_ns() -> u64 {
    let mut ts = libc::timespec { tv_sec: 0, tv_nsec: 0 };
    // SAFETY: valid pointer to a timespec.
    unsafe { libc::clock_gettime(libc::CLOCK_MONOTONIC, &mut ts) };
    ts.tv_sec as u64 * 1_000_000_000 + ts.tv_nsec as u64
}

// ---------------------------------------------------------------- options

struct Opts {
    serial: bool,
    exit_after_first_frame: bool,
    backends: wgpu::Backends,
    gpu_early: bool,
    early_pipeline: bool,
    vk_cache: bool,
    no_implicit_layers: bool,
    preload_libs: bool,
    quiet_malloc: bool,
    atlas_cache: bool,
    no_gpu: bool,
    gpu_after_first_frame: bool,
    cpu_first_frame: bool,
    present_mode: Option<wgpu::PresentMode>,
    file: String,
    font: String,
    size: f32,
    scale: Option<f64>,
    cols: u32,
    rows: u32,
}

fn parse_opts() -> Opts {
    let mut o = Opts {
        serial: false,
        exit_after_first_frame: false,
        backends: wgpu::Backends::VULKAN,
        gpu_early: false,
        early_pipeline: false,
        vk_cache: false,
        no_implicit_layers: false,
        preload_libs: false,
        quiet_malloc: false,
        atlas_cache: false,
        no_gpu: false,
        gpu_after_first_frame: false,
        cpu_first_frame: false,
        present_mode: None,
        file: concat!(env!("CARGO_MANIFEST_DIR"), "/../../lib/std.msh").to_string(),
        font: "/usr/share/fonts/noto/NotoSansMono-Regular.ttf".to_string(),
        size: 16.0,
        scale: None,
        cols: 100,
        rows: 30,
    };
    let mut args = std::env::args().skip(1);
    while let Some(a) = args.next() {
        let mut val = || args.next().unwrap_or_else(|| panic!("{a} needs a value"));
        match a.as_str() {
            "--serial" => o.serial = true,
            "--exit-after-first-frame" => o.exit_after_first_frame = true,
            "--backend" => {
                o.backends = match val().as_str() {
                    "vulkan" => wgpu::Backends::VULKAN,
                    "gl" => wgpu::Backends::GL,
                    "all" => wgpu::Backends::all(),
                    b => panic!("unknown backend {b}"),
                }
            }
            "--gpu-early" => o.gpu_early = true,
            "--early-pipeline" => o.early_pipeline = true,
            "--vk-cache" => o.vk_cache = true,
            "--no-implicit-layers" => o.no_implicit_layers = true,
            "--preload-libs" => o.preload_libs = true,
            "--quiet-malloc" => o.quiet_malloc = true,
            "--atlas-cache" => o.atlas_cache = true,
            "--no-gpu" => o.no_gpu = true,
            "--gpu-after-first-frame" => o.gpu_after_first_frame = true,
            "--cpu-first-frame" => o.cpu_first_frame = true,
            "--present-mode" => {
                o.present_mode = Some(match val().as_str() {
                    "mailbox" => wgpu::PresentMode::Mailbox,
                    "fifo" => wgpu::PresentMode::Fifo,
                    m => panic!("unknown present mode {m}"),
                })
            }
            "--file" => o.file = val(),
            "--font" => o.font = val(),
            "--size" => o.size = val().parse().expect("--size"),
            "--scale" => o.scale = Some(val().parse().expect("--scale")),
            "--cols" => o.cols = val().parse().expect("--cols"),
            "--rows" => o.rows = val().parse().expect("--rows"),
            _ => panic!("unknown option {a}"),
        }
    }
    if o.gpu_early && o.backends.contains(wgpu::Backends::GL) {
        panic!("--gpu-early needs --backend vulkan: GL needs the display connection");
    }
    o
}

// ---------------------------------------------------------------- Vulkan driver cache

fn cache_dir() -> PathBuf {
    let base = std::env::var_os("XDG_CACHE_HOME")
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from(std::env::var_os("HOME").expect("HOME")).join(".cache"));
    base.join("msh-gui-spike")
}

fn vk_cache_path() -> PathBuf {
    cache_dir().join("vk-icd")
}

// ---------------------------------------------------------------- scale and window size

/// The display scale is only known once the window exists, but the glyphs
/// (and the CPU first frame) are drawn before that, at the last scale seen.
fn scale_cache_path() -> PathBuf {
    cache_dir().join("scale")
}

fn cached_scale() -> Option<f64> {
    std::fs::read_to_string(scale_cache_path()).ok()?.trim().parse().ok().filter(|s: &f64| *s > 0.0)
}

fn save_scale(scale: f64) {
    let _ = std::fs::create_dir_all(cache_dir());
    let _ = std::fs::write(scale_cache_path(), scale.to_string());
}

/// The window for a grid: the smallest logical size that covers it, and the
/// physical size winit will make of that (it rounds logical x scale).
#[derive(Clone, Copy)]
struct Geometry {
    scale: f64,
    logical: (u32, u32),
    physical: (u32, u32),
}

fn geometry(cols: u32, rows: u32, cell: CellMetrics, scale: f64) -> Geometry {
    let (gw, gh) = (cols * cell.w, rows * cell.h);
    let logical = ((gw as f64 / scale).ceil() as u32, (gh as f64 / scale).ceil() as u32);
    let physical = ((logical.0 as f64 * scale).round() as u32, (logical.1 as f64 * scale).round() as u32);
    Geometry { scale, logical, physical }
}

/// The ICD manifest for the driver behind `info`, so the next launch can tell
/// the Vulkan loader to load only that driver.
fn icd_manifest_for(info: &wgpu::AdapterInfo) -> Option<PathBuf> {
    let d = info.driver.to_lowercase();
    let stem = if d.contains("radv") {
        "radeon_icd"
    } else if d.contains("hasvk") {
        "intel_hasvk_icd"
    } else if d.contains("intel") {
        "intel_icd"
    } else if d.contains("nvidia") {
        "nvidia_icd"
    } else if d.contains("llvmpipe") {
        "lvp_icd"
    } else if d.contains("nvk") {
        "nouveau_icd"
    } else {
        return None;
    };
    ["/etc/vulkan/icd.d", "/usr/share/vulkan/icd.d"].iter().find_map(|dir| {
        std::fs::read_dir(dir).ok()?.flatten().map(|e| e.path()).find(|p| {
            p.file_stem().and_then(|s| s.to_str()).is_some_and(|s| s == stem || s.starts_with(&format!("{stem}.")))
        })
    })
}

// ---------------------------------------------------------------- font atlas

const FIRST_GLYPH: u8 = 0x20;
const GLYPH_COUNT: u32 = 95; // printable ASCII, 0x20..=0x7e
const ATLAS_COLS: u32 = 16;

#[derive(Clone, Copy)]
struct CellMetrics {
    w: u32,
    h: u32,
    baseline: i32,
}

/// Cell size from the font's tables alone: cheap, so the window can be
/// created at its final size before any glyph is rasterised.
fn cell_metrics(data: &[u8], px: f32) -> CellMetrics {
    let font = swash::FontRef::from_index(data, 0).expect("not a font file");
    let metrics = font.metrics(&[]).scale(px);
    let gm = font.glyph_metrics(&[]).scale(px);
    CellMetrics {
        w: gm.advance_width(font.charmap().map('M')).ceil() as u32,
        h: (metrics.ascent + metrics.descent + metrics.leading).ceil() as u32,
        baseline: metrics.ascent.round() as i32,
    }
}

/// Printable ASCII rasterised into cell-sized slots of an R8 bitmap.
struct Atlas {
    cell: CellMetrics,
    width: u32,
    height: u32,
    pixels: Vec<u8>,
}

fn build_atlas(data: &[u8], px: f32, cell: CellMetrics) -> Atlas {
    use swash::scale::{Render, ScaleContext, Source};
    use swash::zeno::Format;

    let font = swash::FontRef::from_index(data, 0).expect("not a font file");
    let charmap = font.charmap();
    let (cell_w, cell_h) = (cell.w, cell.h);
    let width = ATLAS_COLS * cell_w;
    let height = GLYPH_COUNT.div_ceil(ATLAS_COLS) * cell_h;
    let mut pixels = vec![0u8; (width * height) as usize];

    let mut ctx = ScaleContext::new();
    let mut scaler = ctx.builder(font).size(px).hint(true).build();
    let mut render = Render::new(&[Source::Outline]);
    render.format(Format::Alpha);

    for i in 0..GLYPH_COUNT {
        let ch = (FIRST_GLYPH + i as u8) as char;
        let Some(img) = render.render(&mut scaler, charmap.map(ch)) else { continue };
        let (sx, sy) = ((i % ATLAS_COLS * cell_w) as i32, (i / ATLAS_COLS * cell_h) as i32);
        let p = img.placement;
        for y in 0..p.height as i32 {
            let ty = cell.baseline - p.top + y;
            if ty < 0 || ty >= cell_h as i32 {
                continue;
            }
            for x in 0..p.width as i32 {
                let tx = p.left + x;
                if tx < 0 || tx >= cell_w as i32 {
                    continue;
                }
                let src = img.data[(y * p.width as i32 + x) as usize];
                pixels[((sy + ty) as u32 * width + (sx + tx) as u32) as usize] = src;
            }
        }
    }
    trace("ASCII atlas rasterised");
    Atlas { cell, width, height, pixels }
}

// ---------------------------------------------------------------- cell grid

/// One instance per non-blank cell. This is the seam libghostty-vt's render
/// state will fill: cell position, glyph, foreground colour.
#[repr(C)]
#[derive(Clone, Copy)]
struct CellInstance {
    col: u32,
    row: u32,
    glyph: u32,
    fg: u32, // RGBA8, little-endian packed for unpack4x8unorm
}

const FG: u32 = 0xffd8d8d8; // A=ff, B=d8, G=d8, R=d8
const BG: [f64; 3] = [0.07, 0.07, 0.08];

fn grid_from_text(text: &str, cols: u32, rows: u32) -> Vec<CellInstance> {
    let mut cells = Vec::with_capacity((cols * rows) as usize);
    for (row, line) in text.lines().take(rows as usize).enumerate() {
        let mut col = 0u32;
        for b in line.bytes() {
            if col >= cols {
                break;
            }
            match b {
                b'\t' => col = (col / 8 + 1) * 8,
                b' ' => col += 1,
                0x21..=0x7e => {
                    let glyph = (b - FIRST_GLYPH) as u32;
                    cells.push(CellInstance { col, row: row as u32, glyph, fg: FG });
                    col += 1;
                }
                _ => {
                    let glyph = (b'?' - FIRST_GLYPH) as u32;
                    if b & 0xc0 != 0x80 {
                        cells.push(CellInstance { col, row: row as u32, glyph, fg: FG });
                        col += 1;
                    }
                }
            }
        }
    }
    cells
}

// ---------------------------------------------------------------- atlas cache

/// The atlas depends only on the font file, the pixel size and the glyph
/// set, so it can be kept between launches: then the font isn't even read.
fn atlas_cache_path(font: &str, px: f32) -> Option<PathBuf> {
    use std::hash::{Hash, Hasher};
    use std::os::unix::fs::MetadataExt;
    let m = std::fs::metadata(font).ok()?;
    // A fixed-key hasher: the name must be the same in every process.
    let mut h = std::hash::DefaultHasher::new();
    (font, m.len(), m.mtime(), m.mtime_nsec(), m.ino(), px.to_bits(), GLYPH_COUNT, ATLAS_COLS).hash(&mut h);
    Some(cache_dir().join(format!("atlas-{:016x}", h.finish())))
}

fn read_atlas(path: &PathBuf) -> Option<Atlas> {
    let bytes = std::fs::read(path).ok()?;
    let word = |i: usize| Some(u32::from_le_bytes(bytes.get(i * 4..i * 4 + 4)?.try_into().ok()?));
    let (w, h, b, width, height) = (word(0)?, word(1)?, word(2)? as i32, word(3)?, word(4)?);
    let pixels = bytes.get(20..)?.to_vec();
    (pixels.len() == (width * height) as usize)
        .then_some(Atlas { cell: CellMetrics { w, h, baseline: b }, width, height, pixels })
}

fn write_atlas(path: &PathBuf, a: &Atlas) {
    let mut bytes = Vec::with_capacity(20 + a.pixels.len());
    for v in [a.cell.w, a.cell.h, a.cell.baseline as u32, a.width, a.height] {
        bytes.extend_from_slice(&v.to_le_bytes());
    }
    bytes.extend_from_slice(&a.pixels);
    let _ = std::fs::create_dir_all(cache_dir());
    // Write then rename, so a reader never sees half a file.
    let tmp = path.with_extension(format!("tmp{}", std::process::id()));
    if std::fs::write(&tmp, bytes).is_ok() {
        let _ = std::fs::rename(tmp, path);
    }
}

fn load_font(path: &str, px: f32) -> (Vec<u8>, CellMetrics) {
    let data = std::fs::read(path).unwrap_or_else(|e| panic!("reading font {path}: {e}"));
    let cell = cell_metrics(&data, px);
    trace("font read, cell metrics");
    (data, cell)
}

fn load_cells(path: &str, cols: u32, rows: u32) -> Vec<CellInstance> {
    let text = std::fs::read_to_string(path).unwrap_or_else(|e| panic!("reading {path}: {e}"));
    let cells = grid_from_text(&text, cols, rows);
    trace("text file read, grid built");
    cells
}

/// What the frame thread produces once the glyphs exist.
struct FrameParts {
    atlas: Arc<Atlas>,
    cells: Vec<CellInstance>,
    cpu: Option<shm::PreparedFrame>,
}

fn as_bytes<T: Copy>(v: &[T]) -> &[u8] {
    // SAFETY: T is a plain #[repr(C)] value type with no padding we rely on.
    unsafe { std::slice::from_raw_parts(v.as_ptr().cast(), std::mem::size_of_val(v)) }
}

// ---------------------------------------------------------------- CPU frame

/// The same picture the GPU path draws, into an XRGB8888 buffer.
fn draw_cpu(buf: &mut [u32], width: u32, height: u32, atlas: &Atlas, cells: &[CellInstance]) {
    let to8 = |c: f64| (c * 255.0).round() as u32;
    let (br, bg, bb) = (to8(BG[0]), to8(BG[1]), to8(BG[2]));
    buf.fill(br << 16 | bg << 8 | bb);
    let (cw, ch) = (atlas.cell.w, atlas.cell.h);
    for c in cells {
        let (x0, y0) = (c.col * cw, c.row * ch);
        if x0 + cw > width || y0 + ch > height {
            continue;
        }
        let (fr, fg, fb) = (c.fg & 0xff, c.fg >> 8 & 0xff, c.fg >> 16 & 0xff);
        let (sx, sy) = (c.glyph % ATLAS_COLS * cw, c.glyph / ATLAS_COLS * ch);
        for y in 0..ch {
            let src = &atlas.pixels[((sy + y) * atlas.width + sx) as usize..][..cw as usize];
            let dst = &mut buf[((y0 + y) * width + x0) as usize..][..cw as usize];
            for (d, &a) in dst.iter_mut().zip(src) {
                if a == 0 {
                    continue;
                }
                let (a, ia) = (a as u32, 255 - a as u32);
                let mix = |f: u32, b: u32| (f * a + b * ia + 127) / 255;
                *d = mix(fr, br) << 16 | mix(fg, bg) << 8 | mix(fb, bb);
            }
        }
    }
}

// ---------------------------------------------------------------- GPU

struct Gpu {
    // Held for the renderer's lifetime.
    _instance: wgpu::Instance,
    _adapter: wgpu::Adapter,
    device: wgpu::Device,
    queue: wgpu::Queue,
}

fn new_instance(backends: wgpu::Backends, display: Option<OwnedDisplayHandle>) -> wgpu::Instance {
    let mut desc = match display {
        Some(d) => wgpu::InstanceDescriptor::new_with_display_handle(Box::new(d)),
        None => wgpu::InstanceDescriptor::new_without_display_handle(),
    };
    desc.backends = backends;
    let inst = wgpu::Instance::new(desc.with_env());
    trace("wgpu instance created");
    inst
}

fn select_adapter(instance: &wgpu::Instance, surface: Option<&wgpu::Surface<'_>>, vk_cache: bool) -> wgpu::Adapter {
    let adapter = pollster::block_on(instance.request_adapter(&wgpu::RequestAdapterOptions {
        power_preference: wgpu::PowerPreference::None,
        force_fallback_adapter: false,
        compatible_surface: surface,
        apply_limit_buckets: false,
    }));
    let adapter = match adapter {
        Ok(a) => a,
        Err(e) => {
            if vk_cache {
                // The cached driver no longer works: forget it. The next
                // launch loads every driver again (a product would re-exec).
                let _ = std::fs::remove_file(vk_cache_path());
            }
            panic!("no GPU adapter: {e}");
        }
    };
    trace("adapter selected");
    if vk_cache && std::env::var_os("VK_DRIVER_FILES").is_none() {
        if let Some(icd) = icd_manifest_for(&adapter.get_info()) {
            let path = vk_cache_path();
            let _ = std::fs::create_dir_all(path.parent().unwrap());
            let _ = std::fs::write(&path, icd.as_os_str().as_encoded_bytes());
        }
    }
    adapter
}

fn create_device(adapter: &wgpu::Adapter) -> (wgpu::Device, wgpu::Queue) {
    let dq = pollster::block_on(adapter.request_device(&wgpu::DeviceDescriptor {
        label: Some("spike"),
        memory_hints: wgpu::MemoryHints::MemoryUsage,
        ..Default::default()
    }))
    .expect("request_device");
    trace("device created");
    dq
}

/// Surface configuration from one capabilities query: on Wayland each query
/// costs Mesa several compositor roundtrips (get_default_config is another).
fn surface_config(
    surface: &wgpu::Surface<'_>,
    adapter: &wgpu::Adapter,
    size: PhysicalSize<u32>,
    present_mode: Option<wgpu::PresentMode>,
) -> wgpu::SurfaceConfiguration {
    let caps = surface.get_capabilities(adapter);
    trace("surface capabilities queried");
    // Colours are given in sRGB already, so use a non-sRGB format: the one
    // the early pipeline guessed if offered.
    let format = if caps.formats.contains(&GUESSED_FORMAT) {
        GUESSED_FORMAT
    } else {
        let first = *caps.formats.first().expect("surface not supported by adapter");
        caps.formats.iter().copied().find(|f| !f.is_srgb()).unwrap_or(first)
    };
    let mode = present_mode.unwrap_or(wgpu::PresentMode::Fifo);
    wgpu::SurfaceConfiguration {
        usage: wgpu::TextureUsages::RENDER_ATTACHMENT,
        format,
        width: size.width.max(1),
        height: size.height.max(1),
        present_mode: if caps.present_modes.contains(&mode) { mode } else { wgpu::PresentMode::Fifo },
        desired_maximum_frame_latency: 1,
        alpha_mode: wgpu::CompositeAlphaMode::Auto,
        view_formats: vec![],
        color_space: wgpu::SurfaceColorSpace::Auto,
    }
}

fn configure(surface: &wgpu::Surface<'_>, device: &wgpu::Device, config: &wgpu::SurfaceConfiguration) {
    surface.configure(device, config);
    trace("surface configured");
}

#[repr(C)]
#[derive(Clone, Copy)]
struct Globals {
    screen: [f32; 2],
    cell: [f32; 2],
    atlas_size: [f32; 2],
    atlas_cols: u32,
    _pad: u32,
}

/// Everything the frame needs that doesn't depend on the window, so it can be
/// built before the window exists if the surface format is guessed right.
struct Resources {
    format: wgpu::TextureFormat,
    pipeline: wgpu::RenderPipeline,
    bind_group: wgpu::BindGroup,
    globals: wgpu::Buffer,
    atlas_size: [f32; 2],
    cell: [f32; 2],
}

/// Wayland Vulkan drivers all offer this one; checked against the surface later.
const GUESSED_FORMAT: wgpu::TextureFormat = wgpu::TextureFormat::Bgra8Unorm;

fn build_resources(device: &wgpu::Device, queue: &wgpu::Queue, atlas: &Atlas, format: wgpu::TextureFormat) -> Resources {
    let texture = device.create_texture(&wgpu::TextureDescriptor {
        label: Some("atlas"),
        size: wgpu::Extent3d { width: atlas.width, height: atlas.height, depth_or_array_layers: 1 },
        mip_level_count: 1,
        sample_count: 1,
        dimension: wgpu::TextureDimension::D2,
        format: wgpu::TextureFormat::R8Unorm,
        usage: wgpu::TextureUsages::TEXTURE_BINDING | wgpu::TextureUsages::COPY_DST,
        view_formats: &[],
    });
    queue.write_texture(
        wgpu::TexelCopyTextureInfo {
            texture: &texture,
            mip_level: 0,
            origin: wgpu::Origin3d::ZERO,
            aspect: wgpu::TextureAspect::All,
        },
        &atlas.pixels,
        wgpu::TexelCopyBufferLayout { offset: 0, bytes_per_row: Some(atlas.width), rows_per_image: None },
        wgpu::Extent3d { width: atlas.width, height: atlas.height, depth_or_array_layers: 1 },
    );
    let view = texture.create_view(&Default::default());
    let sampler = device.create_sampler(&wgpu::SamplerDescriptor {
        mag_filter: wgpu::FilterMode::Nearest,
        min_filter: wgpu::FilterMode::Nearest,
        ..Default::default()
    });
    trace("atlas uploaded");

    let globals = device.create_buffer(&wgpu::BufferDescriptor {
        label: Some("globals"),
        size: std::mem::size_of::<Globals>() as u64,
        usage: wgpu::BufferUsages::UNIFORM | wgpu::BufferUsages::COPY_DST,
        mapped_at_creation: false,
    });

    let bgl = device.create_bind_group_layout(&wgpu::BindGroupLayoutDescriptor {
        label: Some("bgl"),
        entries: &[
            wgpu::BindGroupLayoutEntry {
                binding: 0,
                visibility: wgpu::ShaderStages::VERTEX,
                ty: wgpu::BindingType::Buffer {
                    ty: wgpu::BufferBindingType::Uniform,
                    has_dynamic_offset: false,
                    min_binding_size: None,
                },
                count: None,
            },
            wgpu::BindGroupLayoutEntry {
                binding: 1,
                visibility: wgpu::ShaderStages::FRAGMENT,
                ty: wgpu::BindingType::Texture {
                    sample_type: wgpu::TextureSampleType::Float { filterable: true },
                    view_dimension: wgpu::TextureViewDimension::D2,
                    multisampled: false,
                },
                count: None,
            },
            wgpu::BindGroupLayoutEntry {
                binding: 2,
                visibility: wgpu::ShaderStages::FRAGMENT,
                ty: wgpu::BindingType::Sampler(wgpu::SamplerBindingType::Filtering),
                count: None,
            },
        ],
    });
    let bind_group = device.create_bind_group(&wgpu::BindGroupDescriptor {
        label: Some("bg"),
        layout: &bgl,
        entries: &[
            wgpu::BindGroupEntry { binding: 0, resource: globals.as_entire_binding() },
            wgpu::BindGroupEntry { binding: 1, resource: wgpu::BindingResource::TextureView(&view) },
            wgpu::BindGroupEntry { binding: 2, resource: wgpu::BindingResource::Sampler(&sampler) },
        ],
    });

    let shader = device.create_shader_module(wgpu::ShaderModuleDescriptor {
        label: Some("cells"),
        source: wgpu::ShaderSource::Wgsl(include_str!("shader.wgsl").into()),
    });
    let layout = device.create_pipeline_layout(&wgpu::PipelineLayoutDescriptor {
        label: Some("layout"),
        bind_group_layouts: &[Some(&bgl)],
        immediate_size: 0,
    });
    let attrs = wgpu::vertex_attr_array![0 => Uint32x2, 1 => Uint32, 2 => Uint32];
    let pipeline = device.create_render_pipeline(&wgpu::RenderPipelineDescriptor {
        label: Some("cells"),
        layout: Some(&layout),
        vertex: wgpu::VertexState {
            module: &shader,
            entry_point: Some("vs_main"),
            compilation_options: Default::default(),
            buffers: &[Some(wgpu::VertexBufferLayout {
                array_stride: std::mem::size_of::<CellInstance>() as u64,
                step_mode: wgpu::VertexStepMode::Instance,
                attributes: &attrs,
            })],
        },
        primitive: wgpu::PrimitiveState { topology: wgpu::PrimitiveTopology::TriangleStrip, ..Default::default() },
        depth_stencil: None,
        multisample: Default::default(),
        fragment: Some(wgpu::FragmentState {
            module: &shader,
            entry_point: Some("fs_main"),
            compilation_options: Default::default(),
            targets: &[Some(wgpu::ColorTargetState {
                format,
                blend: Some(wgpu::BlendState::ALPHA_BLENDING),
                write_mask: wgpu::ColorWrites::ALL,
            })],
        }),
        multiview_mask: None,
        cache: None,
    });
    trace("pipeline created");
    Resources {
        format,
        pipeline,
        bind_group,
        globals,
        atlas_size: [atlas.width as f32, atlas.height as f32],
        cell: [atlas.cell.w as f32, atlas.cell.h as f32],
    }
}

struct Renderer {
    window: Arc<Window>,
    gpu: Gpu,
    surface: wgpu::Surface<'static>,
    config: wgpu::SurfaceConfiguration,
    res: Resources,
    instances: wgpu::Buffer,
    instance_count: u32,
    frames: u64,
}

impl Renderer {
    /// `surface` is already configured with `config`.
    fn new(
        window: Arc<Window>,
        gpu: Gpu,
        surface: wgpu::Surface<'static>,
        config: wgpu::SurfaceConfiguration,
        early: Option<Resources>,
        atlas: &Atlas,
        cells: &[CellInstance],
    ) -> Self {
        let res = match early {
            Some(r) if r.format == config.format => r,
            _ => build_resources(&gpu.device, &gpu.queue, atlas, config.format),
        };
        let instances = gpu.device.create_buffer(&wgpu::BufferDescriptor {
            label: Some("cells"),
            size: (std::mem::size_of_val(cells).max(16)) as u64,
            usage: wgpu::BufferUsages::VERTEX | wgpu::BufferUsages::COPY_DST,
            mapped_at_creation: false,
        });
        gpu.queue.write_buffer(&instances, 0, as_bytes(cells));
        Renderer { window, gpu, surface, config, res, instances, instance_count: cells.len() as u32, frames: 0 }
    }

    fn resize(&mut self, size: PhysicalSize<u32>) {
        self.config.width = size.width.max(1);
        self.config.height = size.height.max(1);
        self.surface.configure(&self.gpu.device, &self.config);
    }

    /// Returns false if no frame was drawn.
    fn draw(&mut self) -> bool {
        let frame = match self.surface.get_current_texture() {
            wgpu::CurrentSurfaceTexture::Success(f) | wgpu::CurrentSurfaceTexture::Suboptimal(f) => f,
            wgpu::CurrentSurfaceTexture::Outdated | wgpu::CurrentSurfaceTexture::Lost => {
                self.surface.configure(&self.gpu.device, &self.config);
                return false;
            }
            _ => return false,
        };
        let first = self.frames == 0;
        if first {
            trace("first surface texture acquired");
        }
        let g = Globals {
            screen: [self.config.width as f32, self.config.height as f32],
            cell: self.res.cell,
            atlas_size: self.res.atlas_size,
            atlas_cols: ATLAS_COLS,
            _pad: 0,
        };
        self.gpu.queue.write_buffer(&self.res.globals, 0, as_bytes(&[g]));
        let view = frame.texture.create_view(&Default::default());
        let mut enc = self.gpu.device.create_command_encoder(&Default::default());
        {
            let mut pass = enc.begin_render_pass(&wgpu::RenderPassDescriptor {
                label: Some("frame"),
                color_attachments: &[Some(wgpu::RenderPassColorAttachment {
                    view: &view,
                    depth_slice: None,
                    resolve_target: None,
                    ops: wgpu::Operations {
                        load: wgpu::LoadOp::Clear(wgpu::Color { r: BG[0], g: BG[1], b: BG[2], a: 1.0 }),
                        store: wgpu::StoreOp::Store,
                    },
                })],
                depth_stencil_attachment: None,
                timestamp_writes: None,
                occlusion_query_set: None,
                multiview_mask: None,
            });
            pass.set_pipeline(&self.res.pipeline);
            pass.set_bind_group(0, &self.res.bind_group, &[]);
            pass.set_vertex_buffer(0, self.instances.slice(..));
            pass.draw(0..4, 0..self.instance_count);
        }
        self.gpu.queue.submit([enc.finish()]);
        if first {
            trace("first frame submitted");
        }
        self.window.pre_present_notify();
        self.gpu.queue.present(frame);
        self.frames += 1;
        true
    }
}

// ---------------------------------------------------------------- app

/// The GPU thread reports each stage as it finishes, so the main thread can
/// do the surface work that depends on it in parallel with the next stage.
enum UserEvent {
    AdapterReady(wgpu::Instance, wgpu::Adapter),
    DeviceReady(wgpu::Device, wgpu::Queue),
    /// The window-independent resources, if --early-pipeline built them.
    GpuReady(Option<Resources>),
}

fn spawn_gpu(
    backends: wgpu::Backends,
    display: Option<OwnedDisplayHandle>,
    vk_cache: bool,
    atlas: Option<mpsc::Receiver<Arc<Atlas>>>,
    proxy: mpsc::Receiver<EventLoopProxy<UserEvent>>,
    configured: mpsc::Receiver<()>,
) {
    let thread = std::thread::Builder::new().name("gpu".into());
    let _ = thread.spawn(move || {
        trace("gpu thread started");
        let instance = new_instance(backends, display);
        let adapter = select_adapter(&instance, None, vk_cache);
        let proxy = proxy.recv().expect("proxy");
        let _ = proxy.send_event(UserEvent::AdapterReady(instance, adapter.clone()));
        let (device, queue) = create_device(&adapter);
        let _ = proxy.send_event(UserEvent::DeviceReady(device.clone(), queue.clone()));
        let early = atlas.map(|rx| {
            let res = build_resources(&device, &queue, &rx.recv().expect("atlas"), GUESSED_FORMAT);
            // Run the queued atlas upload now rather than in the first frame,
            // but only once the main thread has configured the surface:
            // wgpu fails a configure if another thread submits during it.
            let _ = configured.recv();
            queue.submit([]);
            trace("uploads submitted");
            res
        });
        let _ = proxy.send_event(UserEvent::GpuReady(early));
    });
}

/// GPU setup as it arrives from the GPU thread, before the renderer exists.
#[derive(Default)]
struct GpuParts {
    instance: Option<wgpu::Instance>,
    adapter: Option<wgpu::Adapter>,
    surface: Option<wgpu::Surface<'static>>,
    config: Option<wgpu::SurfaceConfiguration>,
    device: Option<(wgpu::Device, wgpu::Queue)>,
    configured: bool,
    resources: Option<Option<Resources>>,
}

struct App {
    opts: Opts,
    font_data: Option<Vec<u8>>, // --serial only
    geo: Option<(CellMetrics, Geometry)>,
    geo_rx: Option<mpsc::Receiver<(CellMetrics, Geometry)>>,
    frame_rx: Option<mpsc::Receiver<FrameParts>>,
    atlas: Option<Arc<Atlas>>,
    cells: Vec<CellInstance>,
    window: Option<Arc<Window>>,
    parts: GpuParts,
    configured_tx: Option<mpsc::Sender<()>>,
    cpu_frame: Option<shm::Presented>,
    renderer: Option<Renderer>,
    first_frame_done: bool,
    /// --gpu-after-first-frame: starts the GPU thread once the CPU frame is up.
    start_gpu: Option<Box<dyn FnOnce()>>,
}

impl App {
    /// The atlas and cells from the frame thread (and the CPU frame, the
    /// first time).
    fn receive_frame(&mut self) -> Option<shm::PreparedFrame> {
        let rx = self.frame_rx.take()?;
        trace("waiting for frame parts");
        let parts = rx.recv().expect("frame thread");
        trace("frame parts received");
        self.atlas = Some(parts.atlas);
        self.cells = parts.cells;
        parts.cpu
    }

    fn atlas(&mut self) -> Arc<Atlas> {
        self.receive_frame();
        self.atlas.clone().unwrap()
    }

    fn first_gpu_frame(&mut self) {
        let r = self.renderer.as_mut().unwrap();
        if r.draw() && !self.first_frame_done {
            self.first_frame_done = true;
            trace("first GPU frame presented");
            // The CPU frame's buffers can go once a GPU buffer is attached.
            self.cpu_frame = None;
            print_trace();
            if self.opts.exit_after_first_frame {
                // Skip teardown: it isn't part of startup.
                std::process::exit(0);
            }
        }
    }

    /// Do whatever surface work the parts that have arrived allow, and move
    /// the window to wgpu once everything is there.
    fn advance_gpu(&mut self) {
        let Some(window) = self.window.clone() else { return };
        if self.renderer.is_some() {
            return;
        }
        let p = &mut self.parts;
        if let (None, Some(instance), Some(adapter)) = (&p.surface, &p.instance, &p.adapter) {
            let surface = instance.create_surface(window.clone()).expect("create_surface");
            trace("surface created");
            p.config = Some(surface_config(&surface, adapter, window.inner_size(), self.opts.present_mode));
            p.surface = Some(surface);
        }
        if let (false, Some(surface), Some(config), Some((device, _))) = (p.configured, &p.surface, &p.config, &p.device)
        {
            configure(surface, device, config);
            p.configured = true;
            if let Some(tx) = self.configured_tx.take() {
                let _ = tx.send(());
            }
        }
        if !p.configured || p.resources.is_none() {
            return;
        }
        let p = std::mem::take(&mut self.parts);
        let (device, queue) = p.device.unwrap();
        let gpu = Gpu { _instance: p.instance.unwrap(), _adapter: p.adapter.unwrap(), device, queue };
        let atlas = self.atlas();
        let r = Renderer::new(window, gpu, p.surface.unwrap(), p.config.unwrap(), p.resources.unwrap(), &atlas, &self.cells);
        self.renderer = Some(r);
        self.first_gpu_frame();
    }
}

impl ApplicationHandler<UserEvent> for App {
    fn resumed(&mut self, el: &ActiveEventLoop) {
        if self.window.is_some() {
            return;
        }
        let (cell, geo) = match self.geo {
            Some(g) => g,
            None => {
                let g = self.geo_rx.take().unwrap().recv().expect("frame thread");
                trace("cell metrics received");
                self.geo = Some(g);
                g
            }
        };
        let size = winit::dpi::LogicalSize::new(geo.logical.0, geo.logical.1);
        let attrs = Window::default_attributes().with_title("msh gui spike").with_inner_size(size);
        let window = Arc::new(el.create_window(attrs).expect("create_window"));
        trace("window created");
        if window.scale_factor() != geo.scale || cached_scale().is_none() {
            // Glyphs were drawn for another scale; right next launch.
            save_scale(window.scale_factor());
        }
        self.window = Some(window.clone());

        if self.opts.serial {
            let instance = new_instance(self.opts.backends, Some(el.owned_display_handle()));
            let surface = instance.create_surface(window.clone()).expect("create_surface");
            trace("surface created");
            let adapter = select_adapter(&instance, Some(&surface), self.opts.vk_cache);
            let (device, queue) = create_device(&adapter);
            let config = surface_config(&surface, &adapter, window.inner_size(), self.opts.present_mode);
            configure(&surface, &device, &config);
            let data = self.font_data.take().unwrap();
            let atlas = build_atlas(&data, self.opts.size * geo.scale as f32, cell);
            self.cells = load_cells(&self.opts.file, self.opts.cols, self.opts.rows);
            let gpu = Gpu { _instance: instance, _adapter: adapter, device, queue };
            self.renderer = Some(Renderer::new(window, gpu, surface, config, None, &atlas, &self.cells));
            self.first_gpu_frame();
            return;
        }

        if self.opts.cpu_first_frame {
            let mut frame = self.receive_frame().expect("CPU frame");
            let size = window.inner_size();
            if (frame.width, frame.height) != (size.width, size.height) {
                // The compositor chose another size: draw again, here.
                let (atlas, cells) = (self.atlas.clone().unwrap(), &self.cells);
                let (w, h) = (size.width, size.height);
                frame = shm::prepare(shm::FrameMemory::new(), w, h, |px| draw_cpu(px, w, h, &atlas, cells));
                trace("CPU frame redrawn at the window's size");
            }
            use raw_window_handle::{HasDisplayHandle, HasWindowHandle, RawDisplayHandle, RawWindowHandle};
            let (RawDisplayHandle::Wayland(d), RawWindowHandle::Wayland(w)) =
                (el.display_handle().unwrap().as_raw(), window.window_handle().unwrap().as_raw())
            else {
                panic!("--cpu-first-frame is Wayland only");
            };
            // SAFETY: both come from the live window.
            self.cpu_frame = Some(unsafe { shm::present(d.display.as_ptr(), w.surface.as_ptr(), frame) });
            trace("CPU frame presented");
        }
        if let Some(start) = self.start_gpu.take() {
            start();
        }
        self.advance_gpu();
    }

    fn user_event(&mut self, _el: &ActiveEventLoop, event: UserEvent) {
        match event {
            UserEvent::AdapterReady(instance, adapter) => {
                self.parts.instance = Some(instance);
                self.parts.adapter = Some(adapter);
            }
            UserEvent::DeviceReady(device, queue) => self.parts.device = Some((device, queue)),
            UserEvent::GpuReady(res) => self.parts.resources = Some(res),
        }
        self.advance_gpu();
    }

    fn window_event(&mut self, el: &ActiveEventLoop, _id: WindowId, event: WindowEvent) {
        match event {
            WindowEvent::CloseRequested => el.exit(),
            WindowEvent::Resized(size) => {
                if let Some(r) = self.renderer.as_mut() {
                    r.resize(size);
                    r.window.request_redraw();
                }
            }
            WindowEvent::RedrawRequested => {
                if let Some(r) = self.renderer.as_mut() {
                    r.draw();
                }
            }
            _ => {}
        }
    }
}

#[cfg(feature = "no-rt-init")]
#[unsafe(no_mangle)]
pub extern "C" fn main(_argc: libc::c_int, _argv: *const *const libc::c_char) -> libc::c_int {
    spike_main();
    0
}

#[cfg(not(feature = "no-rt-init"))]
fn main() {
    spike_main();
}

fn spike_main() {
    let main_ns = monotonic_ns();
    START.set(Instant::now()).unwrap();
    trace("main entered");
    let opts = parse_opts();
    if let Some(launch) = std::env::var("FC_LAUNCH_NS").ok().and_then(|s| s.parse::<u64>().ok()) {
        eprintln!("{:8.2} ms  launch to main", (main_ns.saturating_sub(launch)) as f64 / 1e6);
    }

    if opts.quiet_malloc {
        // SAFETY: mallopt only adjusts allocator parameters; no threads yet.
        unsafe {
            libc::mallopt(libc::M_ARENA_MAX, 1);
            libc::mallopt(libc::M_MMAP_THRESHOLD, 256 << 20);
            libc::mallopt(libc::M_TOP_PAD, 64 << 20);
            libc::mallopt(libc::M_TRIM_THRESHOLD, 512 << 20);
            // Grow the heap now, while no one else can be holding mmap_lock.
            libc::free(libc::malloc(1 << 20));
        }
        trace("malloc tuned");
    }

    // Loader settings are read when the Vulkan instance is created, so they
    // must be set before the GPU thread starts (set_var isn't thread safe).
    if opts.vk_cache {
        if let Ok(icd) = std::fs::read_to_string(vk_cache_path()) {
            // SAFETY: no other threads exist yet.
            unsafe { std::env::set_var("VK_DRIVER_FILES", icd.trim()) };
        }
    }
    if opts.no_implicit_layers {
        // SAFETY: no other threads exist yet.
        unsafe { std::env::set_var("VK_LOADER_LAYERS_DISABLE", "~implicit~") };
    }

    // glibc holds one lock for every dlopen and dlsym. The Vulkan driver
    // takes 10-20 ms to load (RADV pulls in libLLVM), so any library winit
    // loads in that window waits for it. winit loads its own
    // through these lazy statics; filling them now means they never dlopen.
    if opts.preload_libs {
        wayland_sys::client::wayland_client_option().expect("libwayland-client");
        xkbcommon_dl::xkbcommon_option().expect("libxkbcommon");
        let _ = xkbcommon_dl::xkbcommon_compose_option();
        // The getrandom crate (ahash, under winit) looks up getrandom with
        // dlsym the first time it is called; std's HashMap seeding may too.
        let _ = getrandom::u64();
        let _ = std::collections::hash_map::RandomState::new();
        // The first time a thread blocks on a std channel, std sets up a
        // thread_local with a destructor, and registering that destructor
        // (__cxa_thread_atexit_impl) takes the loader lock too.
        let (_tx, rx) = mpsc::channel::<()>();
        let _ = rx.recv_timeout(Duration::from_micros(1));
        trace("display libraries loaded");
    }

    let (proxy_tx, proxy_rx) = mpsc::channel();
    let (configured_tx, configured_rx) = mpsc::channel();
    let mut configured_rx = Some(configured_rx);
    let (geo_tx, geo_rx) = mpsc::channel();
    let (frame_tx, frame_rx) = mpsc::channel();
    let (gpu_atlas_tx, gpu_atlas_rx) = mpsc::channel();
    let mut proxy_rx = Some(proxy_rx);
    let mut gpu_atlas_rx = Some(gpu_atlas_rx);
    let early_rx = |rx: &mut Option<_>, early: bool| if early { rx.take() } else { None };

    // The frame thread does everything that doesn't need the window: font,
    // cell metrics (sent first: they size the window), glyph atlas, cells,
    // and the CPU first frame, drawn ahead into shared memory.
    let scale = opts.scale.or_else(cached_scale).unwrap_or(1.0);
    let (mut serial_font, mut serial_geo) = (None, None);
    if opts.serial {
        let (data, cell) = load_font(&opts.font, opts.size * scale as f32);
        (serial_font, serial_geo) = (Some(data), Some((cell, geometry(opts.cols, opts.rows, cell, scale))));
    } else {
        let (font, file, cols, rows) = (opts.font.clone(), opts.file.clone(), opts.cols, opts.rows);
        let size = opts.size * scale as f32;
        let (send_gpu, atlas_cache) = (opts.early_pipeline, opts.atlas_cache);
        let frame_mem = opts.cpu_first_frame.then(shm::FrameMemory::new);
        let frame_thread = std::thread::Builder::new().name("frame".into());
        let _ = frame_thread.spawn(move || {
            trace("frame thread started");
            let cache = atlas_cache.then(|| atlas_cache_path(&font, size)).flatten();
            let cached = cache.as_ref().and_then(read_atlas);
            let atlas = match cached {
                Some(a) => {
                    trace("atlas read from cache");
                    let _ = geo_tx.send((a.cell, geometry(cols, rows, a.cell, scale)));
                    a
                }
                None => {
                    let (data, cell) = load_font(&font, size);
                    let _ = geo_tx.send((cell, geometry(cols, rows, cell, scale)));
                    let a = build_atlas(&data, size, cell);
                    if let Some(path) = &cache {
                        write_atlas(path, &a);
                    }
                    a
                }
            };
            let geo = geometry(cols, rows, atlas.cell, scale);
            let atlas = Arc::new(atlas);
            if send_gpu {
                let _ = gpu_atlas_tx.send(atlas.clone());
            }
            let cells = load_cells(&file, cols, rows);
            let cpu = frame_mem.map(|mem| {
                let (w, h) = geo.physical;
                let frame = shm::prepare(mem, w, h, |px| draw_cpu(px, w, h, &atlas, &cells));
                trace("CPU frame drawn");
                frame
            });
            let _ = frame_tx.send(FrameParts { atlas, cells, cpu });
            trace("frame parts sent");
        });
    }

    // pthread_create allocates TLS under the dynamic linker's lock, which the
    // Vulkan driver load holds for 10-20 ms, so every other thread is created
    // before the GPU thread starts.
    let mut gpu_started = false;
    if !opts.serial && opts.gpu_early && !opts.no_gpu {
        let rx = early_rx(&mut gpu_atlas_rx, opts.early_pipeline);
        spawn_gpu(opts.backends, None, opts.vk_cache, rx, proxy_rx.take().unwrap(), configured_rx.take().unwrap());
        gpu_started = true;
    }

    // The event loop is the connection to the display server; the GL backend
    // needs its display handle.
    let event_loop = EventLoop::<UserEvent>::with_user_event().build().expect("event loop");
    event_loop.set_control_flow(ControlFlow::Wait);
    trace("event loop created");

    let mut start_gpu: Option<Box<dyn FnOnce()>> = None;
    if !opts.serial && !gpu_started && !opts.no_gpu && opts.gpu_after_first_frame {
        let rx = early_rx(&mut gpu_atlas_rx, opts.early_pipeline);
        let (backends, vk_cache, proxy, configured) =
            (opts.backends, opts.vk_cache, proxy_rx.take().unwrap(), configured_rx.take().unwrap());
        start_gpu = Some(Box::new(move || spawn_gpu(backends, None, vk_cache, rx, proxy, configured)));
    } else if !opts.serial && !gpu_started && !opts.no_gpu {
        let rx = early_rx(&mut gpu_atlas_rx, opts.early_pipeline);
        let display = Some(event_loop.owned_display_handle());
        spawn_gpu(opts.backends, display, opts.vk_cache, rx, proxy_rx.take().unwrap(), configured_rx.take().unwrap());
    }
    let _ = proxy_tx.send(event_loop.create_proxy());

    let mut app = App {
        font_data: serial_font,
        geo: serial_geo,
        geo_rx: (!opts.serial).then_some(geo_rx),
        frame_rx: (!opts.serial).then_some(frame_rx),
        opts,
        atlas: None,
        cells: Vec::new(),
        window: None,
        parts: GpuParts::default(),
        configured_tx: Some(configured_tx),
        cpu_frame: None,
        renderer: None,
        first_frame_done: false,
        start_gpu,
    };
    event_loop.run_app(&mut app).expect("run_app");
}
