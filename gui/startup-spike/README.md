# Startup spike

First step of the GUI frontend (`ai/gui-terminal.typ`, "Current recommendation").
It measures launch to first frame for a winit window drawing a cell grid with wgpu:
one font file loaded directly, an ASCII glyph atlas rasterised with swash, and instanced quads.

GPU setup and font rasterising run on their own threads while the window is created.
`--serial` does them one after another, to time each stage on its own.

The grid is filled from a text file for now.
When Zig is installed, libghostty-vt's render state fills it instead (`CellInstance` is the seam).

## Running

```
cargo build --release
./target/release/startup-spike                           # window stays open
./target/release/startup-spike --exit-after-first-frame  # print the stage trace and exit
WGPU_BACKEND=gl ./target/release/startup-spike ...       # OpenGL instead of Vulkan
```

Other options: `--serial`, `--file PATH`, `--font PATH`, `--size PX`, `--cols N`, `--rows N`.

## Measuring

Stage medians over several runs:

```
for i in $(seq 15); do ./target/release/startup-spike --exit-after-first-frame 2>&1; done | python3 -I tools/stages.py
```

Comparison with other terminals, using the same criterion for all:
the time from launch to the first `wl_surface.attach` (with a buffer) and `commit`,
recorded by an `LD_PRELOAD` probe that wraps `sendmsg`.
`WAYLAND_DEBUG` and `strace` were tried first, but added 40--80 ms; the probe adds nothing measurable.
Runs are interleaved, because timings on the benchmark machine drift.

```
gcc -O2 -shared -fPIC -o tools/firstcommit.so tools/firstcommit.c -ldl
python3 -I tools/firstcommit.py 15 ./target/release/startup-spike alacritty "ghostty --gtk-single-instance=false"
```

## Results: FX-8350, Radeon Pro WX 3100, Wayland, 2026-11-07

Stage trace (ms since `main`, median of 15, Vulkan, parallel), from a run while the machine was fast:

| Stage | ms |
|---|---|
| event loop created (Wayland connection) | 8.5 |
| window created | 11.6 |
| ASCII atlas rasterised (font thread) | 15.5 |
| wgpu instance created | 36.0 |
| adapter selected | 49.0 |
| device created | 53.4 |
| surface configured | 61.5 |
| pipeline created | 62.7 |
| first frame presented | 66.9 |

- Vulkan instance creation (about 25 ms) is the largest stage, then adapter selection (13 ms), surface configuration (6 ms) and the event loop (8 ms).
- `VK_DRIVER_FILES=.../radeon_icd.json` saves about 10 ms: by default the Vulkan loader also loads both Intel drivers.
- wgpu's GL backend is slower to first frame (about 77 ms): EGL setup is about 40 ms.
- Running GPU setup and font work in parallel saves only about 2--3 ms, because GPU setup is the critical path.
- Later the same day the machine ran slower (CPU at 1.4 GHz under `schedutil`), and instance creation rose to about 50 ms.

First Wayland commit, interleaved, median of 15 (slow state):

| Program | ms |
|---|---|
| startup-spike (Vulkan) | 107 |
| startup-spike, Radeon driver only | 99 |
| Alacritty | 165 |
| Ghostty (GTK, `--gtk-single-instance=false`) | 384 |

The spike already beats both references, but misses the 50 ms target: GPU setup alone is over 40 ms.
