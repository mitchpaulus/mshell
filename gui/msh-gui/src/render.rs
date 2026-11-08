//! Drawing cells with wgpu: instanced quads, each a solid rectangle or a
//! glyph from an atlas that rasterises characters with swash on first use.
//! The startup spike measured this path (gui/startup-spike).

use std::collections::HashMap;
use std::sync::Arc;
use winit::window::Window;

/// A quad, in pixels: solid when `glyph` is SOLID, else that atlas slot
/// (a glyph quad is one cell in size).
#[repr(C)]
#[derive(Clone, Copy, Debug)]
pub struct Quad {
    pub x: u32,
    pub y: u32,
    pub w: u32,
    pub h: u32,
    pub glyph: u32,
    /// RGBA8, little-endian packed for unpack4x8unorm.
    pub color: u32,
}

pub const SOLID: u32 = u32::MAX;

pub fn rgba(r: u8, g: u8, b: u8, a: u8) -> u32 {
    r as u32 | (g as u32) << 8 | (b as u32) << 16 | (a as u32) << 24
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

fn as_bytes<T: Copy>(v: &[T]) -> &[u8] {
    // SAFETY: T is plain #[repr(C)] data.
    unsafe { std::slice::from_raw_parts(v.as_ptr().cast(), std::mem::size_of_val(v)) }
}

#[derive(Clone, Copy, Debug)]
pub struct CellMetrics {
    pub w: u32,
    pub h: u32,
    pub baseline: i32,
}

const ATLAS_COLS: u32 = 64;
const ATLAS_ROWS: u32 = 64;

/// The glyph atlas: one cell-sized slot per character used so far.
pub struct Glyphs {
    font: Vec<u8>,
    px: f32,
    pub cell: CellMetrics,
    slots: HashMap<char, u32>,
    pixels: Vec<u8>,
    width: u32,
    height: u32,
    dirty: bool,
    ctx: swash::scale::ScaleContext,
}

impl Glyphs {
    pub fn new(font: Vec<u8>, px: f32) -> Glyphs {
        let cell = {
            let f = swash::FontRef::from_index(&font, 0).expect("not a font file");
            let m = f.metrics(&[]).scale(px);
            let gm = f.glyph_metrics(&[]).scale(px);
            CellMetrics {
                w: gm.advance_width(f.charmap().map('M')).ceil() as u32,
                h: (m.ascent + m.descent + m.leading).ceil() as u32,
                baseline: m.ascent.round() as i32,
            }
        };
        let (width, height) = (ATLAS_COLS * cell.w, ATLAS_ROWS * cell.h);
        Glyphs {
            font,
            px,
            cell,
            slots: HashMap::new(),
            pixels: vec![0; (width * height) as usize],
            width,
            height,
            dirty: true,
            ctx: swash::scale::ScaleContext::new(),
        }
    }

    /// The atlas slot of a character, rasterising it on first use. A full
    /// atlas gives '?'.
    pub fn slot(&mut self, c: char) -> u32 {
        if let Some(&s) = self.slots.get(&c) {
            return s;
        }
        let n = self.slots.len() as u32;
        if n >= ATLAS_COLS * ATLAS_ROWS {
            return if c == '?' { 0 } else { self.slot('?') };
        }
        self.rasterise(c, n);
        self.slots.insert(c, n);
        self.dirty = true;
        n
    }

    fn rasterise(&mut self, c: char, slot: u32) {
        use swash::scale::{Render, Source};
        use swash::zeno::Format;
        let font = swash::FontRef::from_index(&self.font, 0).unwrap();
        let mut scaler = self.ctx.builder(font).size(self.px).hint(true).build();
        let mut render = Render::new(&[Source::Outline]);
        render.format(Format::Alpha);
        let Some(img) = render.render(&mut scaler, font.charmap().map(c)) else { return };
        let (cw, ch) = (self.cell.w as i32, self.cell.h as i32);
        let (sx, sy) = ((slot % ATLAS_COLS) as i32 * cw, (slot / ATLAS_COLS) as i32 * ch);
        let p = img.placement;
        for y in 0..p.height as i32 {
            let ty = self.cell.baseline - p.top + y;
            if ty < 0 || ty >= ch {
                continue;
            }
            for x in 0..p.width as i32 {
                let tx = p.left + x;
                if tx < 0 || tx >= cw {
                    continue;
                }
                let a = img.data[(y * p.width as i32 + x) as usize];
                self.pixels[((sy + ty) * self.width as i32 + sx + tx) as usize] = a;
            }
        }
    }
}

/// Draws quads into an RGB image on the CPU, as the GPU does: for checking
/// frames without a window (msh-gui --script).
pub fn draw_cpu(glyphs: &Glyphs, quads: &[Quad], w: u32, h: u32, bg: [u8; 3]) -> (u32, u32, Vec<u8>) {
    let (cw, ch) = (glyphs.cell.w, glyphs.cell.h);
    let mut px = Vec::with_capacity((w * h * 3) as usize);
    for _ in 0..w * h {
        px.extend_from_slice(&bg);
    }
    for q in quads {
        let [r, g, b, a] = q.color.to_le_bytes();
        for y in q.y..(q.y + q.h).min(h) {
            for x in q.x..(q.x + q.w).min(w) {
                let alpha = if q.glyph == SOLID {
                    a as u32
                } else {
                    let (sx, sy) = ((q.glyph % ATLAS_COLS) * cw + (x - q.x), (q.glyph / ATLAS_COLS) * ch + (y - q.y));
                    glyphs.pixels[(sy * glyphs.width + sx) as usize] as u32 * a as u32 / 255
                };
                let i = ((y * w + x) * 3) as usize;
                for (k, c) in [r, g, b].into_iter().enumerate() {
                    px[i + k] = ((c as u32 * alpha + px[i + k] as u32 * (255 - alpha) + 127) / 255) as u8;
                }
            }
        }
    }
    (w, h, px)
}

pub struct Renderer {
    window: Arc<Window>,
    surface: wgpu::Surface<'static>,
    device: wgpu::Device,
    queue: wgpu::Queue,
    config: wgpu::SurfaceConfiguration,
    pipeline: wgpu::RenderPipeline,
    bind_group: wgpu::BindGroup,
    globals: wgpu::Buffer,
    atlas: wgpu::Texture,
    instances: wgpu::Buffer,
    capacity: usize,
}

impl Renderer {
    pub fn new(window: Arc<Window>, glyphs: &Glyphs) -> Renderer {
        let mut desc = wgpu::InstanceDescriptor::new_without_display_handle();
        // Vulkan only: wgpu sets up OpenGL too by default, which cost ~15 ms
        // of startup (gui/startup-spike).
        desc.backends = wgpu::Backends::VULKAN;
        let instance = wgpu::Instance::new(desc.with_env());
        let surface = instance.create_surface(window.clone()).expect("create_surface");
        let adapter = pollster::block_on(instance.request_adapter(&wgpu::RequestAdapterOptions {
            power_preference: wgpu::PowerPreference::None,
            force_fallback_adapter: false,
            compatible_surface: Some(&surface),
            apply_limit_buckets: false,
        }))
        .expect("no GPU adapter");
        let (device, queue) = pollster::block_on(adapter.request_device(&wgpu::DeviceDescriptor {
            label: Some("msh"),
            memory_hints: wgpu::MemoryHints::MemoryUsage,
            ..Default::default()
        }))
        .expect("request_device");
        let size = window.inner_size();
        let caps = surface.get_capabilities(&adapter);
        // Colours are sRGB values already: a non-sRGB format.
        let format = caps.formats.iter().copied().find(|f| !f.is_srgb()).unwrap_or(caps.formats[0]);
        let config = wgpu::SurfaceConfiguration {
            usage: wgpu::TextureUsages::RENDER_ATTACHMENT,
            format,
            width: size.width.max(1),
            height: size.height.max(1),
            present_mode: wgpu::PresentMode::Fifo,
            desired_maximum_frame_latency: 1,
            alpha_mode: wgpu::CompositeAlphaMode::Auto,
            view_formats: vec![],
            color_space: wgpu::SurfaceColorSpace::Auto,
        };
        surface.configure(&device, &config);

        let atlas = device.create_texture(&wgpu::TextureDescriptor {
            label: Some("atlas"),
            size: wgpu::Extent3d { width: glyphs.width, height: glyphs.height, depth_or_array_layers: 1 },
            mip_level_count: 1,
            sample_count: 1,
            dimension: wgpu::TextureDimension::D2,
            format: wgpu::TextureFormat::R8Unorm,
            usage: wgpu::TextureUsages::TEXTURE_BINDING | wgpu::TextureUsages::COPY_DST,
            view_formats: &[],
        });
        let view = atlas.create_view(&Default::default());
        let sampler = device.create_sampler(&wgpu::SamplerDescriptor {
            mag_filter: wgpu::FilterMode::Nearest,
            min_filter: wgpu::FilterMode::Nearest,
            ..Default::default()
        });
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
        let attrs = wgpu::vertex_attr_array![0 => Uint32x4, 1 => Uint32, 2 => Uint32];
        let pipeline = device.create_render_pipeline(&wgpu::RenderPipelineDescriptor {
            label: Some("cells"),
            layout: Some(&layout),
            vertex: wgpu::VertexState {
                module: &shader,
                entry_point: Some("vs_main"),
                compilation_options: Default::default(),
                buffers: &[Some(wgpu::VertexBufferLayout {
                    array_stride: std::mem::size_of::<Quad>() as u64,
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
        let capacity = 4096;
        let instances = Renderer::instance_buffer(&device, capacity);
        Renderer { window, surface, device, queue, config, pipeline, bind_group, globals, atlas, instances, capacity }
    }

    fn instance_buffer(device: &wgpu::Device, capacity: usize) -> wgpu::Buffer {
        device.create_buffer(&wgpu::BufferDescriptor {
            label: Some("quads"),
            size: (capacity * std::mem::size_of::<Quad>()) as u64,
            usage: wgpu::BufferUsages::VERTEX | wgpu::BufferUsages::COPY_DST,
            mapped_at_creation: false,
        })
    }

    pub fn resize(&mut self, width: u32, height: u32) {
        self.config.width = width.max(1);
        self.config.height = height.max(1);
        self.surface.configure(&self.device, &self.config);
    }

    /// Draws the quads, in order, over `clear`.
    pub fn draw(&mut self, glyphs: &mut Glyphs, quads: &[Quad], clear: [f64; 3]) {
        if glyphs.dirty {
            self.queue.write_texture(
                wgpu::TexelCopyTextureInfo {
                    texture: &self.atlas,
                    mip_level: 0,
                    origin: wgpu::Origin3d::ZERO,
                    aspect: wgpu::TextureAspect::All,
                },
                &glyphs.pixels,
                wgpu::TexelCopyBufferLayout { offset: 0, bytes_per_row: Some(glyphs.width), rows_per_image: None },
                wgpu::Extent3d { width: glyphs.width, height: glyphs.height, depth_or_array_layers: 1 },
            );
            glyphs.dirty = false;
        }
        if quads.len() > self.capacity {
            self.capacity = quads.len().next_power_of_two();
            self.instances = Renderer::instance_buffer(&self.device, self.capacity);
        }
        self.queue.write_buffer(&self.instances, 0, as_bytes(quads));
        let g = Globals {
            screen: [self.config.width as f32, self.config.height as f32],
            cell: [glyphs.cell.w as f32, glyphs.cell.h as f32],
            atlas_size: [glyphs.width as f32, glyphs.height as f32],
            atlas_cols: ATLAS_COLS,
            _pad: 0,
        };
        self.queue.write_buffer(&self.globals, 0, as_bytes(&[g]));
        let frame = match self.surface.get_current_texture() {
            wgpu::CurrentSurfaceTexture::Success(f) | wgpu::CurrentSurfaceTexture::Suboptimal(f) => f,
            wgpu::CurrentSurfaceTexture::Outdated | wgpu::CurrentSurfaceTexture::Lost => {
                self.surface.configure(&self.device, &self.config);
                return;
            }
            _ => return,
        };
        let view = frame.texture.create_view(&Default::default());
        let mut enc = self.device.create_command_encoder(&Default::default());
        {
            let mut pass = enc.begin_render_pass(&wgpu::RenderPassDescriptor {
                label: Some("frame"),
                color_attachments: &[Some(wgpu::RenderPassColorAttachment {
                    view: &view,
                    depth_slice: None,
                    resolve_target: None,
                    ops: wgpu::Operations {
                        load: wgpu::LoadOp::Clear(wgpu::Color { r: clear[0], g: clear[1], b: clear[2], a: 1.0 }),
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
            pass.draw(0..4, 0..quads.len() as u32);
        }
        self.queue.submit([enc.finish()]);
        self.window.pre_present_notify();
        self.queue.present(frame);
    }
}
