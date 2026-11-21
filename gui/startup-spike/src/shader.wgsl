// One instanced quad per terminal cell. The glyph atlas is a grid of
// cell-sized slots, so a glyph index maps straight to atlas coordinates.

struct Globals {
    screen: vec2<f32>,     // surface size in pixels
    cell: vec2<f32>,       // cell size in pixels
    atlas_size: vec2<f32>, // atlas size in pixels
    atlas_cols: u32,       // slots per atlas row
    _pad: u32,
};

@group(0) @binding(0) var<uniform> g: Globals;
@group(0) @binding(1) var atlas: texture_2d<f32>;
@group(0) @binding(2) var samp: sampler;

struct VsOut {
    @builtin(position) pos: vec4<f32>,
    @location(0) uv: vec2<f32>,
    @location(1) fg: vec4<f32>,
};

@vertex
fn vs_main(
    @builtin(vertex_index) vi: u32,
    @location(0) cell: vec2<u32>,
    @location(1) glyph: u32,
    @location(2) fg: u32,
) -> VsOut {
    let corner = vec2<f32>(f32(vi & 1u), f32(vi >> 1u));
    let pixel = (vec2<f32>(cell) + corner) * g.cell;
    let ndc = pixel / g.screen * 2.0 - 1.0;
    let slot = vec2<f32>(f32(glyph % g.atlas_cols), f32(glyph / g.atlas_cols));
    var out: VsOut;
    out.pos = vec4<f32>(ndc.x, -ndc.y, 0.0, 1.0);
    out.uv = (slot + corner) * g.cell / g.atlas_size;
    out.fg = unpack4x8unorm(fg);
    return out;
}

@fragment
fn fs_main(in: VsOut) -> @location(0) vec4<f32> {
    let a = textureSample(atlas, samp, in.uv).r;
    return vec4<f32>(in.fg.rgb, in.fg.a * a);
}
