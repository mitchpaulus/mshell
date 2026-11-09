//! Drawing with wgpu: instanced quads in pixels, each a solid rectangle or
//! a region of an atlas that holds what has been rasterised on first use:
//! monospace cells (program output, the prompt), proportional glyphs (the
//! app's own text), and vector icons. The startup spike measured this path
//! (gui/startup-spike).

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::Arc;
use swash::scale::{Render, ScaleContext, Source};
use swash::shape::ShapeContext;
use swash::zeno::{Angle, Format, Mask, Transform};
use swash::FontRef;
use winit::window::Window;

/// A quad, in pixels: solid when `u` is SOLID, else the atlas region at
/// (u, v), as large as the quad.
#[repr(C)]
#[derive(Clone, Copy, Debug)]
pub struct Quad {
    pub x: u32,
    pub y: u32,
    pub w: u32,
    pub h: u32,
    pub u: u32,
    pub v: u32,
    /// RGBA8, little-endian packed for unpack4x8unorm.
    pub color: u32,
}

pub const SOLID: u32 = u32::MAX;

impl Quad {
    pub fn solid(x: u32, y: u32, w: u32, h: u32, color: u32) -> Quad {
        Quad { x, y, w, h, u: SOLID, v: 0, color }
    }

    fn image(x: i32, y: i32, r: Region, color: u32) -> Option<Quad> {
        (x >= 0 && y >= 0 && r.w > 0).then_some(Quad { x: x as u32, y: y as u32, w: r.w, h: r.h, u: r.u, v: r.v, color })
    }
}

pub fn rgba(r: u8, g: u8, b: u8, a: u8) -> u32 {
    r as u32 | (g as u32) << 8 | (b as u32) << 16 | (a as u32) << 24
}

#[repr(C)]
#[derive(Clone, Copy)]
struct Globals {
    screen: [f32; 2],
    atlas_size: [f32; 2],
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

/// A region of the atlas.
#[derive(Clone, Copy, Debug, Default)]
pub struct Region {
    pub u: u32,
    pub v: u32,
    pub w: u32,
    pub h: u32,
}

/// A glyph's image and where it goes from the pen, on the baseline.
#[derive(Clone, Copy, Debug, Default)]
struct GlyphImage {
    region: Region,
    left: i32,
    top: i32,
}

/// Vector icons: layers of SVG path data in a 24 x 24 box, each in its own
/// colour.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum Icon {
    /// The back and tab of a folder, and its front.
    FolderBack,
    FolderFront,
    /// A page with its corner folded.
    File,
}

impl Icon {
    fn path(self) -> &'static str {
        match self {
            Icon::FolderBack => "M2 7 C2 5.3 3.3 4 5 4 H9.2 C9.9 4 10.5 4.3 10.9 4.8 L12.2 6.5 H19 C20.7 6.5 22 7.8 22 9.5 V11 H2 Z",
            Icon::FolderFront => "M2 9.5 H22 V18 C22 19.7 20.7 21 19 21 H5 C3.3 21 2 19.7 2 18 Z",
            Icon::File => "M6 2.5 H13.5 L19 8 V20 C19 20.8 18.3 21.5 17.5 21.5 H6 C5.2 21.5 4.5 20.8 4.5 20 V4 C4.5 3.2 5.2 2.5 6 2.5 Z M13 3.5 V8.5 H18",
        }
    }
}

/// Shelves of regions in the atlas.
struct Packer {
    x: u32,
    y: u32,
    row: u32,
}

const ATLAS: u32 = 2048;

/// A face's style: bold, italic, or both.
pub const BOLD: u8 = 1;
pub const ITALIC: u8 = 2;

/// Font families, by slot: the UI font (the app's own text), the monospace
/// font (program output, and the fallback for a character another family
/// lacks), and the prompt box's (the UI font, unless one is chosen).
pub const UI: u8 = 0;
pub const MONO: u8 = 1;
pub const PROMPT: u8 = 2;

/// A font family: its regular face, and its bold, italic and bold italic,
/// read on first use from files named by variables, else given with the
/// regular (a platform's default fonts), else beside it (NotoSans-Bold.ttf
/// beside NotoSans-Regular.ttf). A style without a file is made from the
/// nearest one that has, emboldened or slanted.
pub struct Family {
    regular: Vec<u8>,
    paths: [Option<PathBuf>; 4],
    faces: [Option<Option<Vec<u8>>>; 4],
}

impl Family {
    /// The family of a regular font file. `var` names the variables that
    /// choose its styles' files: `{var}_BOLD`, `_ITALIC`, `_BOLD_ITALIC`;
    /// `styles` are its bold, italic and bold italic files when those
    /// aren't set.
    pub fn load(path: &str, var: &str, styles: Option<[String; 3]>) -> Result<Family, String> {
        let regular = std::fs::read(path).map_err(|e| format!("reading font {path}: {e}"))?;
        if FontRef::from_index(&regular, 0).is_none() {
            return Err(format!("{path}: not a font file"));
        }
        let mut paths: [Option<PathBuf>; 4] = Default::default();
        for (i, (style, suffix, name)) in [(BOLD, "_BOLD", "-Bold"), (ITALIC, "_ITALIC", "-Italic"), (BOLD | ITALIC, "_BOLD_ITALIC", "-BoldItalic")].into_iter().enumerate() {
            paths[style as usize] = match std::env::var(format!("{var}{suffix}")) {
                Ok(p) => Some(p.into()),
                Err(_) => match &styles {
                    Some(files) => Some(files[i].clone().into()),
                    None => path.contains("-Regular").then(|| path.replacen("-Regular", name, 1).into()),
                },
            };
        }
        Ok(Family { regular, paths, faces: Default::default() })
    }

    /// Whether a style has a file of its own, read now if it wasn't.
    fn has(&mut self, style: u8) -> bool {
        let i = style as usize;
        if self.faces[i].is_none() {
            self.faces[i] = Some(self.paths[i].as_ref().and_then(|p| std::fs::read(p).ok()).filter(|d| FontRef::from_index(d, 0).is_some()));
        }
        self.faces[i].as_ref().is_some_and(|d| d.is_some())
    }

    /// The face a style is drawn from (its own, or the nearest), and what
    /// of the style that face lacks.
    fn pick(&mut self, style: u8) -> (u8, u8) {
        let base = [style, style & ITALIC, style & BOLD, 0].into_iter().find(|&s| s == 0 || self.has(s)).unwrap_or(0);
        (base, style & !base)
    }

    fn face(&self, base: u8) -> FontRef<'_> {
        self.faces[base as usize]
            .as_ref()
            .and_then(|d| d.as_deref())
            .and_then(|d| FontRef::from_index(d, 0))
            .unwrap_or_else(|| FontRef::from_index(&self.regular, 0).unwrap())
    }
}

/// A family's vertical metrics, in pixels: its ascent and descent, and the
/// height of a line of it.
#[derive(Clone, Copy, Debug)]
pub struct Metrics {
    pub ascent: f32,
    pub descent: f32,
    pub line: f32,
}

/// Text shaped in a family: its clusters in order, and their glyphs.
#[derive(Default)]
pub struct Shaped {
    pub clusters: Vec<Cluster>,
    pub glyphs: Vec<ShapedGlyph>,
}

/// A cluster of shaped text: its bytes of the text, its advance, and its
/// glyphs, `glyphs[first..last]`.
#[derive(Clone, Copy, Debug)]
pub struct Cluster {
    pub start: usize,
    pub end: usize,
    pub advance: f32,
    pub first: usize,
    pub last: usize,
}

/// A glyph of shaped text: its family (the text's, or the monospace one's
/// for a character the text's lacks), style and id, and how far it is from
/// its cluster's start.
#[derive(Clone, Copy, Debug)]
pub struct ShapedGlyph {
    pub slot: u8,
    pub style: u8,
    pub id: u16,
    pub dx: f32,
}

/// The atlas and what has been rasterised into it.
pub struct Glyphs {
    /// UI, MONO, and PROMPT when the prompt has a font of its own.
    families: Vec<Family>,
    px: f32,
    pub cell: CellMetrics,
    metrics: [Metrics; 3],
    pixels: Vec<u8>,
    width: u32,
    height: u32,
    /// Rows of the atlas changed since the last upload.
    dirty: Option<(u32, u32)>,
    packer: Packer,
    /// By character and style (BOLD, ITALIC).
    mono: HashMap<(char, u8), Region>,
    /// By family, style and glyph id.
    glyphs: HashMap<(u8, u8, u16), GlyphImage>,
    icons: HashMap<(Icon, u32), Region>,
    /// Set when the atlas filled and was cleared: regions handed out before
    /// are stale, so the frame is laid out again.
    pub reset: bool,
    scale: ScaleContext,
    shape: ShapeContext,
    scratch: Shaped,
}

impl Glyphs {
    pub fn new(ui: Family, mono: Family, prompt: Option<Family>, px: f32) -> Glyphs {
        let cell = {
            let f = FontRef::from_index(&mono.regular, 0).unwrap();
            let m = f.metrics(&[]).scale(px);
            let gm = f.glyph_metrics(&[]).scale(px);
            CellMetrics {
                w: gm.advance_width(f.charmap().map('M')).ceil() as u32,
                h: (m.ascent + m.descent + m.leading).ceil() as u32,
                baseline: m.ascent.round() as i32,
            }
        };
        let mut families = vec![ui, mono];
        families.extend(prompt);
        let metrics_of = |f: &Family| {
            let m = FontRef::from_index(&f.regular, 0).unwrap().metrics(&[]).scale(px);
            Metrics { ascent: m.ascent, descent: m.descent, line: (m.ascent + m.descent + m.leading).ceil() }
        };
        let metrics = [metrics_of(&families[0]), metrics_of(&families[1]), metrics_of(families.get(2).unwrap_or(&families[0]))];
        Glyphs {
            families,
            px,
            cell,
            metrics,
            pixels: vec![0; (ATLAS * ATLAS) as usize],
            width: ATLAS,
            height: ATLAS,
            dirty: None,
            packer: Packer { x: 0, y: 0, row: 0 },
            mono: HashMap::new(),
            glyphs: HashMap::new(),
            icons: HashMap::new(),
            reset: false,
            scale: ScaleContext::new(),
            shape: ShapeContext::new(),
            scratch: Shaped::default(),
        }
    }

    /// The family a slot draws with: the prompt's is the UI font's unless
    /// it has one of its own.
    fn slot(&self, slot: u8) -> usize {
        if (slot as usize) < self.families.len() { slot as usize } else { UI as usize }
    }

    /// A family's vertical metrics.
    pub fn metrics(&self, slot: u8) -> Metrics {
        self.metrics[slot as usize]
    }

    /// A free region of the atlas; when it is full, it is cleared first.
    fn alloc(&mut self, w: u32, h: u32) -> Region {
        let p = &mut self.packer;
        if p.x + w > self.width {
            (p.x, p.y, p.row) = (0, p.y + p.row, 0);
        }
        if p.y + h > self.height {
            self.mono.clear();
            self.glyphs.clear();
            self.icons.clear();
            self.pixels.fill(0);
            self.packer = Packer { x: 0, y: 0, row: 0 };
            self.dirty = Some((0, self.height));
            self.reset = true;
            return self.alloc(w, h);
        }
        let p = &mut self.packer;
        let r = Region { u: p.x, v: p.y, w, h };
        p.x += w + 1;
        p.row = p.row.max(h + 1);
        r
    }

    /// Copies an alpha image into a region.
    fn blit(&mut self, r: Region, data: &[u8], pitch: u32) {
        for y in 0..r.h {
            let src = &data[(y * pitch) as usize..][..r.w as usize];
            let at = ((r.v + y) * self.width + r.u) as usize;
            self.pixels[at..at + r.w as usize].copy_from_slice(src);
        }
        let (lo, hi) = self.dirty.unwrap_or((r.v, r.v + r.h));
        self.dirty = Some((lo.min(r.v), hi.max(r.v + r.h)));
    }

    /// A renderer of outlines, made up to what of a style the face lacks:
    /// emboldened, and slanted right (outlines are y-up) about `pivot`
    /// pixels above the baseline, so the glyph stays over its place.
    fn render(&self, lacks: u8, pivot: f32) -> Render<'static> {
        let mut render = Render::new(&[Source::Outline]);
        render.format(Format::Alpha);
        if lacks & BOLD != 0 {
            render.embolden((self.px / 24.0).max(0.5));
        }
        if lacks & ITALIC != 0 {
            let slant = Angle::from_degrees(12.0);
            let shift = -slant.to_radians().tan() * pivot;
            render.transform(Some(Transform::skew(slant, Angle::ZERO).then_translate(shift, 0.0)));
        }
        render
    }

    /// A character of the monospace font, in a cell-sized region.
    pub fn mono(&mut self, c: char) -> Region {
        self.mono_styled(c, 0)
    }

    /// A character of the monospace font in a style (BOLD, ITALIC), in a
    /// cell-sized region: from the style's own file, or made from the
    /// nearest that there is, emboldened and slanted as it lacks.
    pub fn mono_styled(&mut self, c: char, style: u8) -> Region {
        if let Some(&r) = self.mono.get(&(c, style)) {
            return r;
        }
        let (cw, ch) = (self.cell.w, self.cell.h);
        let mut cellbuf = vec![0u8; (cw * ch) as usize];
        let (base, lacks) = self.families[MONO as usize].pick(style);
        let render = self.render(lacks, self.cell.baseline as f32 * 0.3);
        let family = &self.families[MONO as usize];
        let regular = FontRef::from_index(&family.regular, 0).unwrap();
        let face = family.face(base);
        // A character the face lacks comes from the regular, made up to
        // the style.
        let (font, id, render) = match face.charmap().map(c) {
            0 => (regular, regular.charmap().map(c), if lacks == style { render } else { self.render(style, self.cell.baseline as f32 * 0.3) }),
            id => (face, id, render),
        };
        let mut scaler = self.scale.builder(font).size(self.px).hint(lacks & ITALIC == 0).build();
        if let Some(img) = render.render(&mut scaler, id) {
            let p = img.placement;
            for y in 0..p.height as i32 {
                let ty = self.cell.baseline - p.top + y;
                if ty < 0 || ty >= ch as i32 {
                    continue;
                }
                for x in 0..p.width as i32 {
                    let tx = p.left + x;
                    if tx >= 0 && tx < cw as i32 {
                        cellbuf[(ty as u32 * cw + tx as u32) as usize] = img.data[(y * p.width as i32 + x) as usize];
                    }
                }
            }
        }
        let r = self.alloc(cw, ch);
        self.blit(r, &cellbuf, cw);
        self.mono.insert((c, style), r);
        r
    }

    /// A glyph of a family in a style, rasterised on first use.
    fn glyph(&mut self, slot: u8, style: u8, id: u16) -> GlyphImage {
        let si = self.slot(slot);
        if let Some(&g) = self.glyphs.get(&(si as u8, style, id)) {
            return g;
        }
        let (base, lacks) = self.families[si].pick(style);
        let render = self.render(lacks, self.metrics[si].ascent * 0.3);
        let font = self.families[si].face(base);
        let mut scaler = self.scale.builder(font).size(self.px).hint(lacks & ITALIC == 0).build();
        let g = match render.render(&mut scaler, id) {
            Some(img) if img.placement.width > 0 => {
                let p = img.placement;
                let r = self.alloc(p.width, p.height);
                self.blit(r, &img.data, p.width);
                GlyphImage { region: r, left: p.left, top: p.top }
            }
            _ => GlyphImage::default(),
        };
        self.glyphs.insert((si as u8, style, id), g);
        g
    }

    /// The quad of a shaped glyph, its cluster starting at `x`, on a
    /// baseline at `baseline`.
    pub fn glyph_quad(&mut self, g: ShapedGlyph, x: f32, baseline: f32, color: u32) -> Option<Quad> {
        let img = self.glyph(g.slot, g.style, g.id);
        Quad::image((x + g.dx).round() as i32 + img.left, baseline.round() as i32 - img.top, img.region, color)
    }

    /// Shapes text in a family and style, into `out`: its clusters in
    /// order, each with its advance and glyphs. A character the family
    /// lacks comes from the monospace font. Returns the text's width.
    pub fn shape(&mut self, slot: u8, style: u8, text: &str, out: &mut Shaped) -> f32 {
        out.clusters.clear();
        out.glyphs.clear();
        let si = self.slot(slot);
        let (base, _) = self.families[si].pick(style);
        let (mono_base, _) = self.families[MONO as usize].pick(style);
        let font = self.families[si].face(base);
        let mono = self.families[MONO as usize].face(mono_base);
        let mono_metrics = mono.glyph_metrics(&[]).scale(self.px);
        let mut shaper = self.shape.builder(font).size(self.px).build();
        shaper.add_str(text);
        let mut pen = 0.0;
        shaper.shape_with(|cluster| {
            let (start, end) = (cluster.source.start as usize, cluster.source.end as usize);
            let first = out.glyphs.len();
            let missing = !cluster.glyphs.is_empty() && cluster.glyphs.iter().all(|g| g.id == 0);
            let mut advance = 0.0;
            if missing && si != MONO as usize {
                for c in text[start..end].chars() {
                    let id = mono.charmap().map(c);
                    out.glyphs.push(ShapedGlyph { slot: MONO, style, id, dx: advance });
                    advance += mono_metrics.advance_width(id);
                }
            } else {
                for g in cluster.glyphs {
                    out.glyphs.push(ShapedGlyph { slot: si as u8, style, id: g.id, dx: advance + g.x });
                    advance += g.advance;
                }
            }
            out.clusters.push(Cluster { start, end, advance, first, last: out.glyphs.len() });
            pen += advance;
        });
        pen
    }

    /// The width of text in a family, in pixels.
    pub fn width(&mut self, slot: u8, text: &str) -> f32 {
        let mut sh = std::mem::take(&mut self.scratch);
        let w = self.shape(slot, 0, text, &mut sh);
        self.scratch = sh;
        w
    }

    /// The width of text in the UI font, in pixels.
    pub fn ui_width(&mut self, text: &str) -> f32 {
        self.width(UI, text)
    }

    /// Quads for text in a family, its baseline at (x, y), cut to fit `max`
    /// pixels with an ellipsis; returns its width.
    pub fn text(&mut self, slot: u8, out: &mut Vec<Quad>, x: f32, y: f32, text: &str, color: u32, max: f32) -> f32 {
        let mut run = std::mem::take(&mut self.scratch);
        let width = self.shape(slot, 0, text, &mut run);
        let mut shown = width;
        let mut pen = 0.0;
        let mut ellipsis = Shaped::default();
        let mut ellipsis_at = None;
        if width > max {
            let ew = self.shape(slot, 0, "…", &mut ellipsis);
            let mut at = 0.0;
            for (i, c) in run.clusters.iter().enumerate() {
                if at + c.advance > max - ew {
                    run.clusters.truncate(i);
                    break;
                }
                at += c.advance;
            }
            shown = at + ew;
            ellipsis_at = Some(at);
        }
        for c in &run.clusters {
            for &g in &run.glyphs[c.first..c.last] {
                if let Some(q) = self.glyph_quad(g, x + pen, y, color) {
                    out.push(q);
                }
            }
            pen += c.advance;
        }
        if let Some(at) = ellipsis_at {
            for &g in &ellipsis.glyphs {
                if let Some(q) = self.glyph_quad(g, x + at, y, color) {
                    out.push(q);
                }
            }
        }
        self.scratch = run;
        shown
    }

    /// An icon at `size` pixels square.
    pub fn icon(&mut self, icon: Icon, size: u32) -> Region {
        if let Some(&r) = self.icons.get(&(icon, size)) {
            return r;
        }
        let s = size as f32 / 24.0;
        let (data, _) = Mask::new(icon.path()).transform(Some(Transform::scale(s, s))).size(size, size).render();
        let r = self.alloc(size, size);
        self.blit(r, &data, size);
        self.icons.insert((icon, size), r);
        r
    }
}

/// Draws quads into an RGB image on the CPU, as the GPU does: for checking
/// frames without a window (msh-gui --script).
pub fn draw_cpu(glyphs: &Glyphs, quads: &[Quad], w: u32, h: u32, bg: [u8; 3]) -> (u32, u32, Vec<u8>) {
    let mut px = Vec::with_capacity((w * h * 3) as usize);
    for _ in 0..w * h {
        px.extend_from_slice(&bg);
    }
    for q in quads {
        let [r, g, b, a] = q.color.to_le_bytes();
        for y in q.y..(q.y + q.h).min(h) {
            for x in q.x..(q.x + q.w).min(w) {
                let alpha = if q.u == SOLID {
                    a as u32
                } else {
                    glyphs.pixels[((q.v + y - q.y) * glyphs.width + q.u + x - q.x) as usize] as u32 * a as u32 / 255
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
        let attrs = wgpu::vertex_attr_array![0 => Uint32x4, 1 => Uint32x2, 2 => Uint32];
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
        // Only the rows of the atlas that changed.
        if let Some((lo, hi)) = glyphs.dirty.take() {
            self.queue.write_texture(
                wgpu::TexelCopyTextureInfo {
                    texture: &self.atlas,
                    mip_level: 0,
                    origin: wgpu::Origin3d { x: 0, y: lo, z: 0 },
                    aspect: wgpu::TextureAspect::All,
                },
                &glyphs.pixels[(lo * glyphs.width) as usize..(hi * glyphs.width) as usize],
                wgpu::TexelCopyBufferLayout { offset: 0, bytes_per_row: Some(glyphs.width), rows_per_image: None },
                wgpu::Extent3d { width: glyphs.width, height: hi - lo, depth_or_array_layers: 1 },
            );
        }
        if quads.len() > self.capacity {
            self.capacity = quads.len().next_power_of_two();
            self.instances = Renderer::instance_buffer(&self.device, self.capacity);
        }
        self.queue.write_buffer(&self.instances, 0, as_bytes(quads));
        let g = Globals {
            screen: [self.config.width as f32, self.config.height as f32],
            atlas_size: [glyphs.width as f32, glyphs.height as f32],
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
