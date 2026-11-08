// One instanced quad per cell: a solid rectangle (backgrounds, the cursor)
// or a glyph from the atlas, a grid of cell-sized slots.

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

const SOLID: u32 = 0xffffffffu;

struct VsOut {
    @builtin(position) pos: vec4<f32>,
    @location(0) uv: vec2<f32>,
    @location(1) color: vec4<f32>,
    @location(2) @interpolate(flat) solid: u32,
};

@vertex
fn vs_main(
    @builtin(vertex_index) vi: u32,
    @location(0) rect: vec4<u32>, // col, row, width and height in cells
    @location(1) glyph: u32,
    @location(2) color: u32,
) -> VsOut {
    let corner = vec2<f32>(f32(vi & 1u), f32(vi >> 1u));
    let pixel = (vec2<f32>(rect.xy) + corner * vec2<f32>(rect.zw)) * g.cell;
    let ndc = pixel / g.screen * 2.0 - 1.0;
    var out: VsOut;
    out.pos = vec4<f32>(ndc.x, -ndc.y, 0.0, 1.0);
    out.color = unpack4x8unorm(color);
    out.solid = select(0u, 1u, glyph == SOLID);
    let slot = vec2<f32>(f32(glyph % g.atlas_cols), f32(glyph / g.atlas_cols));
    out.uv = (slot + corner) * g.cell / g.atlas_size;
    return out;
}

@fragment
fn fs_main(in: VsOut) -> @location(0) vec4<f32> {
    if (in.solid == 1u) {
        return in.color;
    }
    let a = textureSample(atlas, samp, in.uv).r;
    return vec4<f32>(in.color.rgb, in.color.a * a);
}
