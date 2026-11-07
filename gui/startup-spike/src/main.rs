//! Startup spike for the mshell GUI frontend (ai/gui-terminal.typ, "Current recommendation").
//!
//! Measures launch to first frame: window, wgpu device, one font file loaded
//! directly (no system font scan), an ASCII glyph atlas, and one frame of a
//! cell grid drawn as instanced quads. Every stage is traced to stderr.
//!
//! The cell grid is filled from a text file for now. When libghostty-vt is
//! available, `Grid` is filled from its render state instead.
//!
//! Options:
//!   --serial                 do GPU setup and font work one after another on
//!                            the main thread, to time each stage on its own
//!   --exit-after-first-frame print the trace and exit once a frame is presented
//!   --file PATH              text to show (default: lib/std.msh)
//!   --font PATH              font file (default: Noto Sans Mono)
//!   --size PX                font size in pixels (default: 16)
//!   --cols N --rows N        grid size (default: 100 x 30)
//! The backend can be chosen with WGPU_BACKEND=vulkan|gl.

use std::sync::{Arc, Mutex, OnceLock};
use std::thread::JoinHandle;
use std::time::{Duration, Instant};

use winit::application::ApplicationHandler;
use winit::dpi::PhysicalSize;
use winit::event::WindowEvent;
use winit::event_loop::{ActiveEventLoop, ControlFlow, EventLoop};
use winit::window::{Window, WindowId};

// ---------------------------------------------------------------- tracing

static START: OnceLock<Instant> = OnceLock::new();
static TRACE: Mutex<Vec<(Duration, &'static str)>> = Mutex::new(Vec::new());

fn trace(stage: &'static str) {
    let t = START.get().expect("START set in main").elapsed();
    TRACE.lock().unwrap().push((t, stage));
}

fn print_trace() {
    let mut entries = TRACE.lock().unwrap().clone();
    entries.sort_by_key(|e| e.0);
    for (t, stage) in entries {
        eprintln!("{:8.2} ms  {stage}", t.as_secs_f64() * 1e3);
    }
}

// ---------------------------------------------------------------- options

struct Opts {
    serial: bool,
    exit_after_first_frame: bool,
    file: String,
    font: String,
    size: f32,
    cols: u32,
    rows: u32,
}

fn parse_opts() -> Opts {
    let mut o = Opts {
        serial: false,
        exit_after_first_frame: false,
        file: concat!(env!("CARGO_MANIFEST_DIR"), "/../../lib/std.msh").to_string(),
        font: "/usr/share/fonts/noto/NotoSansMono-Regular.ttf".to_string(),
        size: 16.0,
        cols: 100,
        rows: 30,
    };
    let mut args = std::env::args().skip(1);
    while let Some(a) = args.next() {
        let mut val = || args.next().unwrap_or_else(|| panic!("{a} needs a value"));
        match a.as_str() {
            "--serial" => o.serial = true,
            "--exit-after-first-frame" => o.exit_after_first_frame = true,
            "--file" => o.file = val(),
            "--font" => o.font = val(),
            "--size" => o.size = val().parse().expect("--size"),
            "--cols" => o.cols = val().parse().expect("--cols"),
            "--rows" => o.rows = val().parse().expect("--rows"),
            _ => panic!("unknown option {a}"),
        }
    }
    o
}

// ---------------------------------------------------------------- font atlas

const FIRST_GLYPH: u8 = 0x20;
const GLYPH_COUNT: u32 = 95; // printable ASCII, 0x20..=0x7e
const ATLAS_COLS: u32 = 16;

/// Printable ASCII rasterised into cell-sized slots of an R8 bitmap.
struct Atlas {
    cell_w: u32,
    cell_h: u32,
    width: u32,
    height: u32,
    pixels: Vec<u8>,
}

fn build_atlas(path: &str, px: f32) -> Atlas {
    use swash::scale::{Render, ScaleContext, Source};
    use swash::zeno::Format;

    let data = std::fs::read(path).unwrap_or_else(|e| panic!("reading font {path}: {e}"));
    trace("font file read");
    let font = swash::FontRef::from_index(&data, 0).expect("not a font file");
    let metrics = font.metrics(&[]).scale(px);
    let gm = font.glyph_metrics(&[]).scale(px);
    let charmap = font.charmap();

    let cell_w = gm.advance_width(charmap.map('M')).ceil() as u32;
    let cell_h = (metrics.ascent + metrics.descent + metrics.leading).ceil() as u32;
    let baseline = metrics.ascent.round() as i32;
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
            let ty = baseline - p.top + y;
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
    Atlas { cell_w, cell_h, width, height, pixels }
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

fn grid_from_text(text: &str, cols: u32, rows: u32) -> Vec<CellInstance> {
    const FG: u32 = 0xffd8d8d8; // A=ff, B=d8, G=d8, R=d8
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

fn as_bytes<T: Copy>(v: &[T]) -> &[u8] {
    // SAFETY: T is a plain #[repr(C)] value type with no padding we rely on.
    unsafe { std::slice::from_raw_parts(v.as_ptr().cast(), std::mem::size_of_val(v)) }
}

// ---------------------------------------------------------------- GPU

struct Gpu {
    instance: wgpu::Instance,
    adapter: wgpu::Adapter,
    device: wgpu::Device,
    queue: wgpu::Queue,
}

fn new_instance(display: winit::event_loop::OwnedDisplayHandle) -> wgpu::Instance {
    let desc = wgpu::InstanceDescriptor::new_with_display_handle_from_env(Box::new(display));
    let inst = wgpu::Instance::new(desc);
    trace("wgpu instance created");
    inst
}

fn finish_gpu(instance: wgpu::Instance, surface: Option<&wgpu::Surface<'_>>) -> Gpu {
    let adapter = pollster::block_on(instance.request_adapter(&wgpu::RequestAdapterOptions {
        power_preference: wgpu::PowerPreference::None,
        force_fallback_adapter: false,
        compatible_surface: surface,
        apply_limit_buckets: false,
    }))
    .expect("no GPU adapter");
    trace("adapter selected");
    let (device, queue) = pollster::block_on(adapter.request_device(&wgpu::DeviceDescriptor {
        label: Some("spike"),
        memory_hints: wgpu::MemoryHints::MemoryUsage,
        ..Default::default()
    }))
    .expect("request_device");
    trace("device created");
    Gpu { instance, adapter, device, queue }
}

struct Renderer {
    window: Arc<Window>,
    gpu: Gpu,
    surface: wgpu::Surface<'static>,
    config: wgpu::SurfaceConfiguration,
    pipeline: wgpu::RenderPipeline,
    bind_group: wgpu::BindGroup,
    globals: wgpu::Buffer,
    instances: wgpu::Buffer,
    instance_count: u32,
    atlas_size: [f32; 2],
    cell: [f32; 2],
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

impl Renderer {
    fn new(window: Arc<Window>, gpu: Gpu, surface: wgpu::Surface<'static>, atlas: Atlas, cells: &[CellInstance]) -> Self {
        let Gpu { adapter, device, queue, .. } = &gpu;
        let size = window.inner_size();
        let mut config = surface
            .get_default_config(adapter, size.width.max(1), size.height.max(1))
            .expect("surface not supported by adapter");
        let caps = surface.get_capabilities(adapter);
        if caps.present_modes.contains(&wgpu::PresentMode::Mailbox) {
            config.present_mode = wgpu::PresentMode::Mailbox;
        }
        if let Some(f) = caps.formats.iter().find(|f| !f.is_srgb()) {
            config.format = *f; // colours are given in sRGB already
        }
        config.desired_maximum_frame_latency = 1;
        surface.configure(device, &config);
        trace("surface configured");

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
        let instances = device.create_buffer(&wgpu::BufferDescriptor {
            label: Some("cells"),
            size: (std::mem::size_of_val(cells).max(16)) as u64,
            usage: wgpu::BufferUsages::VERTEX | wgpu::BufferUsages::COPY_DST,
            mapped_at_creation: false,
        });
        queue.write_buffer(&instances, 0, as_bytes(cells));

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
                    format: config.format,
                    blend: Some(wgpu::BlendState::ALPHA_BLENDING),
                    write_mask: wgpu::ColorWrites::ALL,
                })],
            }),
            multiview_mask: None,
            cache: None,
        });
        trace("pipeline created");

        let atlas_size = [atlas.width as f32, atlas.height as f32];
        let cell = [atlas.cell_w as f32, atlas.cell_h as f32];
        Renderer {
            window,
            gpu,
            surface,
            config,
            pipeline,
            bind_group,
            globals,
            instances,
            instance_count: cells.len() as u32,
            atlas_size,
            cell,
        }
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
        let g = Globals {
            screen: [self.config.width as f32, self.config.height as f32],
            cell: self.cell,
            atlas_size: self.atlas_size,
            atlas_cols: ATLAS_COLS,
            _pad: 0,
        };
        self.gpu.queue.write_buffer(&self.globals, 0, as_bytes(&[g]));
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
                        load: wgpu::LoadOp::Clear(wgpu::Color { r: 0.07, g: 0.07, b: 0.08, a: 1.0 }),
                        store: wgpu::StoreOp::Store,
                    },
                })],
                depth_stencil_attachment: None,
                timestamp_writes: None,
                occlusion_query_set: None,
                multiview_mask: None,
            });
            pass.set_pipeline(&self.pipeline);
            pass.set_bind_group(0, &self.bind_group, &[]);
            pass.set_vertex_buffer(0, self.instances.slice(..));
            pass.draw(0..4, 0..self.instance_count);
        }
        self.gpu.queue.submit([enc.finish()]);
        self.window.pre_present_notify();
        self.gpu.queue.present(frame);
        true
    }
}

// ---------------------------------------------------------------- app

struct App {
    opts: Opts,
    gpu_thread: Option<JoinHandle<Gpu>>,
    font_thread: Option<JoinHandle<Atlas>>,
    text: Option<String>,
    renderer: Option<Renderer>,
    first_frame_done: bool,
}

impl ApplicationHandler for App {
    fn resumed(&mut self, el: &ActiveEventLoop) {
        if self.renderer.is_some() {
            return;
        }
        // The window size depends on the cell size, so in parallel mode the
        // window is created at the expected size from the default font metrics
        // and corrected after the atlas arrives if needed.
        let attrs = Window::default_attributes()
            .with_title("msh gui spike")
            .with_inner_size(PhysicalSize::new(self.opts.cols * 10, self.opts.rows * 22));
        let window = Arc::new(el.create_window(attrs).expect("create_window"));
        trace("window created");

        let (gpu, surface, atlas) = if self.opts.serial {
            let instance = new_instance(el.owned_display_handle());
            let surface = instance.create_surface(window.clone()).expect("create_surface");
            trace("surface created");
            let gpu = finish_gpu(instance, Some(&surface));
            let atlas = build_atlas(&self.opts.font, self.opts.size);
            (gpu, surface, atlas)
        } else {
            let gpu = self.gpu_thread.take().unwrap().join().expect("gpu thread");
            trace("gpu thread joined");
            let surface = gpu.instance.create_surface(window.clone()).expect("create_surface");
            trace("surface created");
            assert!(gpu.adapter.is_surface_supported(&surface), "adapter can't present to this window");
            let atlas = self.font_thread.take().unwrap().join().expect("font thread");
            trace("font thread joined");
            (gpu, surface, atlas)
        };

        let want = PhysicalSize::new(self.opts.cols * atlas.cell_w, self.opts.rows * atlas.cell_h);
        if window.inner_size() != want {
            let _ = window.request_inner_size(want);
        }
        let text = self.text.take().unwrap();
        let cells = grid_from_text(&text, self.opts.cols, self.opts.rows);
        self.renderer = Some(Renderer::new(window.clone(), gpu, surface, atlas, &cells));
        window.request_redraw();
    }

    fn window_event(&mut self, el: &ActiveEventLoop, _id: WindowId, event: WindowEvent) {
        let Some(r) = self.renderer.as_mut() else { return };
        match event {
            WindowEvent::CloseRequested => el.exit(),
            WindowEvent::Resized(size) => {
                r.resize(size);
                r.window.request_redraw();
            }
            WindowEvent::RedrawRequested => {
                if r.draw() && !self.first_frame_done {
                    self.first_frame_done = true;
                    trace("first frame presented");
                    print_trace();
                    if self.opts.exit_after_first_frame {
                        // Skip teardown: it isn't part of startup.
                        std::process::exit(0);
                    }
                }
            }
            _ => {}
        }
    }
}

fn main() {
    START.set(Instant::now()).unwrap();
    trace("main entered");
    let opts = parse_opts();

    // The event loop comes first: it is the connection to the display server,
    // and the wgpu instance needs its display handle (the GL backend does).
    let event_loop = EventLoop::new().expect("event loop");
    event_loop.set_control_flow(ControlFlow::Wait);
    trace("event loop created");

    let (gpu_thread, font_thread) = if opts.serial {
        (None, None)
    } else {
        // GPU device setup doesn't need the window; neither does the font.
        let display = event_loop.owned_display_handle();
        let gpu = std::thread::spawn(move || finish_gpu(new_instance(display), None));
        let (font, size) = (opts.font.clone(), opts.size);
        let atlas = std::thread::spawn(move || build_atlas(&font, size));
        (Some(gpu), Some(atlas))
    };

    let text = std::fs::read_to_string(&opts.file).unwrap_or_else(|e| panic!("reading {}: {e}", opts.file));
    trace("text file read");

    let mut app = App { opts, gpu_thread, font_thread, text: Some(text), renderer: None, first_frame_done: false };
    event_loop.run_app(&mut app).expect("run_app");
}
