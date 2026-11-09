# Startup spike

First step of the GUI frontend (`ai/gui-terminal.typ`, "Current recommendation").
It measures launch to first frame for a winit window that draws a cell grid:
one font file loaded directly, an ASCII glyph atlas rasterised with swash,
and either wgpu instanced quads or a CPU frame in shared memory.

The grid comes from libghostty-vt (1.0.0, linked statically through `gui/ghostty-vt-sys`):
the file's bytes are written to a terminal as a program's output would be,
and its render state fills one `CellInstance` per cell, with its colour.
Only foreground colours and the ASCII atlas are drawn so far; no backgrounds, bold or other glyphs.

## Result

On the FX-8350 / Radeon Pro WX 3100 under KDE Wayland, **the first frame reaches the compositor
18–19 ms after launch** (median), against 160 ms for Alacritty, 279 ms for WezTerm and Konsole,
and 462 ms for Ghostty.
The GPU takes over the window later (about 40–75 ms after launch, depending on the configuration), with the same picture.
The original version of this spike took 100 ms.

The design's target is under 100 ms (`ai/gui-terminal.typ`, Targets).
Even the plain configuration, with the GPU drawing the first frame and no options, meets it on every launch measured (slowest 88 ms).
A patch to winit gets the CPU frame to 11–12 ms, but it isn't applied (see "Running").

The win comes from not putting GPU setup on the path to the first frame at all,
and then from removing every place where the main thread waited on the GPU thread without meaning to.

## Running

libghostty-vt is built once per OS, outside Cargo (Zig 0.17.x, ~5 minutes), from `gui/`:

```
msh ghostty-vt-sys/fetch.msh   # downloads the 1.0.0 tarballs, checks SHA-256, builds into gui/third_party/libghostty-vt-1.0.0-{os}/
```

Or set `GHOSTTY_VT_PREFIX` to an existing install prefix.
The Rust bindings are committed (`gui/ghostty-vt-sys/src/bindings.rs`), so building needs no libclang;
`gui/ghostty-vt-bindgen` rewrites them when the libghostty-vt version changes.

```
cargo build --release
./target/release/startup-spike $FAST            # window stays open
./target/release/startup-spike $FAST --exit-after-first-frame   # print the stage trace and exit
```

where `FAST` is everything that helped:

```
FAST="--gpu-early --preload-libs --early-pipeline --vk-cache --no-implicit-layers --cpu-first-frame --atlas-cache"
```

With no options the spike behaves like a straightforward implementation (Vulkan only),
so each experiment can be switched on alone. `--backend all` is the original behaviour.
The options are documented at the top of `src/main.rs`.
`--file` shows any file, escape sequences included (`ls --color=always -l > f`), and `--no-vt` lays it out directly instead, as before.
`SPIKE_DUMP_FRAME=out.ppm` writes the CPU frame to a file and exits, without opening a window.
`SPIKE_THREADSTATS=1` adds each thread's CPU time, context switches and page faults to the trace.

The spike builds against stock winit 0.30.13.
`winit-defer-keymap.patch` is a patch to it, measured but no longer applied:
it defers keymap compilation and Compose parsing until after the app's first frame (item 7 below),
and lets the app hand winit an already connected Wayland socket (which didn't help).
It halves the CPU first frame (23.8 → 11.9 ms median, interleaved) and saves ~7 ms on the GPU-only path,
which the 100 ms target doesn't need, so it isn't worth carrying a copy of winit for.
To try it, copy winit 0.30.13 into the tree, `git apply` the patch there,
and point `[patch.crates-io]` in `gui/Cargo.toml` at it.

## Measuring

Stage medians over several runs:

```
for i in $(seq 15); do ./target/release/startup-spike --exit-after-first-frame $FAST 2>&1; done | python3 -I tools/stages.py
```

Comparison with other programs, all judged the same way, from launch to the first `wl_surface.attach` (with a buffer) and `commit`,
recorded by an `LD_PRELOAD` probe that wraps `sendmsg`.
`WAYLAND_DEBUG` and `strace` add 40–80 ms; the probe adds nothing measurable.
Runs are interleaved, because timings on this machine drift.

```
gcc -O2 -shared -fPIC -o tools/firstcommit.so tools/firstcommit.c -ldl
python3 -I tools/firstcommit.py 15 "./target/release/startup-spike $FAST" alacritty "ghostty --gtk-single-instance=false"
```

There is no `perf` on this machine, so the other tools find where a thread waits:

- `tools/dltime.c`: an `LD_PRELOAD` probe that logs every `dlopen`, slow `dlsym` and `pthread_create`,
  thread-local destructor registration, and xkbcommon's slow calls, with thread and duration.
- `tools/threadsample.py`: samples every thread's state, kernel wait channel and syscall from `/proc` every ~0.1 ms.
- `tools/evict.py`: drops files from the page cache, for cold-start runs.

## Results: FX-8350, Radeon Pro WX 3100, KDE Plasma (Wayland), display scale 1.5, 2026-11-07

### Launch to first committed frame, monitors on

Session unlocked, both monitors on, after Insync and Baloo had settled following a reboot.
The spike's rows are stock winit, interleaved, 20 runs each; the other programs are an earlier interleaved run of 15 (with the patched spike) in the same session.
"Patched winit" is the same configuration built with `winit-defer-keymap.patch`, from that earlier run.

| Program | Median | Min | Max | Patched winit |
|---|---|---|---|---|
| spike, Vulkan only (no options) | 68.0 ms | 58.5 | 87.7 | 65.9 ms |
| spike, + `--no-implicit-layers` | 57.8 ms | 52.9 | 75.8 | 56.5 ms |
| spike, GPU only, every GPU fix | 43.6 ms | 35.0 | 49.1 | 44.9 ms |
| **spike, simple: `--cpu-first-frame --atlas-cache --no-implicit-layers --gpu-after-first-frame`** | **18.9 ms** | **12.8** | **29.5** | 12.3 ms |
| spike, `$FAST` | 18.1 ms | 13.9 | 30.4 | 10.5 ms |
| Alacritty | 159.8 ms | 156.0 | 172.8 | |
| WezTerm (`start --always-new-process`) | 278.8 ms | 274.5 | 287.1 | |
| Konsole | 279.6 ms | 269.0 | 293.3 | |
| Ghostty (`--gtk-single-instance=false`) | 461.5 ms | 370.2 | 481.7 | |

The simple configuration matches `$FAST` without any of the loader-lock workarounds:
nothing loads the Vulkan driver until the first frame is up.
With stock winit, the CPU frame's time is mostly winit's keyboard setup (item 7) and KWin.

Every launch measured with no options at all, 70 of them across three runs, was under 90 ms.
The stage loop runs launches back to back, so KWin is still tearing down the previous window;
its first-frame times are a few ms worse than the probe's, which waits between launches.

### Launch to first committed frame, session locked (interleaved, median of 15)

The earlier runs, with the patched winit; the monitors were off for this one, with the GPU at its idle clock.

| Program | Median | Min |
|---|---|---|
| spike, original behaviour (`--backend all`) | 100.5 ms | 93.6 |
| spike, GPU only, every GPU fix | 48.3 ms | 41.7 |
| **spike, `$FAST`** | **12.9 ms** | **11.4** |
| Alacritty | 168.1 ms | 163.1 |
| WezTerm (`start --always-new-process`) | 293.9 ms | 282.3 |
| Konsole | 324.9 ms | 303.7 |
| Ghostty (`--gtk-single-instance=false`) | 499.2 ms | 379.0 |

About 2.3 ms of each is Python starting the process (a trivial C program shows the same).

### Stages with `$FAST` and the patched winit (ms since `main`, median of 15)

| Stage | ms | Thread |
|---|---|---|
| libraries preloaded | 0.4 | main |
| atlas read from cache, cells built | 0.9 | frame |
| CPU frame drawn into shared memory | 3.6 | frame |
| event loop created (Wayland connection) | 6.5 | main |
| window created | 7.4 | main |
| **CPU frame presented** | **7.7** | main |
| Vulkan instance created (RADV loaded) | 19.6 | GPU |
| adapter selected | 21.9 | GPU |
| device created | 26.2 | GPU |
| first GPU frame presented | 33–47 | main |

### What cost what, and what fixed it

1. **wgpu initialised OpenGL too.** `Backends::default()` is all of them, and EGL setup cost ~15 ms inside instance creation.
   Fix: Vulkan only.
2. **Mesa's `VK_LAYER_MESA_device_select` implicit layer** opens its own connection to the compositor to find the default GPU.
   KWin takes 3.5–5.5 ms to answer the first request on any new connection, so this alone cost ~10 ms of adapter selection.
   Fix: `VK_LOADER_LAYERS_DISABLE=~implicit~` (or `NODEVICE_SELECT=1`), set before any thread starts.
   This also disables overlays such as MangoHud, which a terminal doesn't need.
3. **The Vulkan loader loads every installed driver**: both Intel drivers (~2 ms) as well as RADV.
   Fix: remember the driver that worked (`--vk-cache`, set `VK_DRIVER_FILES`); if it stops working, forget it.
4. **Loading RADV takes 13–23 ms**: it links `libLLVM.so` (172 MB of C++, with `-z now`).
   That can't be avoided, so it starts first thing in `main` on its own thread (Vulkan needs no display connection).
5. **glibc's loader lock.** `dlopen` holds it for the whole of that load, and four other things need it:
   - other `dlopen`s (winit loading libwayland-client and libxkbcommon),
   - `dlsym` (the `getrandom` crate, used by ahash under winit, looks up `getrandom` this way; it cost 10 ms),
   - `pthread_create`, which allocates TLS,
   - the first registration of a thread-local destructor (`__cxa_thread_atexit_impl`; the first blocking channel `recv`), which cost 18 ms.

   Fix: do each of these once on the main thread, and create every thread, before the GPU thread starts (`--preload-libs`).
6. **The kernel's `mmap_lock`.** While loading, the driver `mprotect`s LLVM's relocated pages, holding the lock for ms at a time,
   so `mmap`/`munmap`/`brk` on other threads wait.
   `MAP_POPULATE` on the frame buffer made it worse: it holds the lock while populating.
   Fix: map the frame buffer before any thread starts and fault it in by writing it, which only takes per-VMA locks.
   `MADV_HUGEPAGE` (shmem THP is `advise` here) makes a 6 MB frame 3 faults instead of 1,500.
7. **winit compiles the keymap (1.7–5 ms) and parses the locale's Compose file (3 ms) on the main thread** inside `EventLoop::new`.
   Fix (`winit-defer-keymap.patch`, not applied): defer both until the event loop is first idle, after the app's first frame,
   or until first use. The main thread is idle then, waiting for the GPU.
8. **The first frame doesn't need the GPU.** The frame thread rasterises the atlas, lays out the cells and draws the frame into shared memory
   while the main thread is still waiting for KWin; the main thread attaches it as soon as the window exists.
   The rasterised atlas is cached on disk (`--atlas-cache`), so the font file isn't even read.
   Inside the window, screenshots of the CPU frame and the GPU frame match except for rounding:
   3,040 of the 3,073 differing pixels (of 1.5 million) are off by 1/255.
9. **The display scale is only known once the window exists**, but the first frame is drawn before that.
   The spike draws at the last scale seen (cached) and sizes the window in logical pixels so that winit's rounding gives the same physical size.
10. **GPU handoff in stages.** The GPU thread reports adapter, device and pipeline separately, so the main thread
    queries surface capabilities during device creation and configures the swapchain during pipeline creation.
    Only one capabilities query: each one costs Mesa several compositor roundtrips.
    The atlas upload is submitted on the GPU thread, but only after the configure,
    because wgpu fails a configure if another thread submits during it (seen in 1 of 40 runs before the fix; none in 550 runs after).
11. **Before `main`.** A non-PIE executable (no relative relocations) saves 0.7 ms, and skipping Rust's runtime setup
    (`--features no-rt-init`) 0.4 ms. Neither shows end to end, because the main thread then waits for KWin longer.
    Non-PIE also gives up ASLR for the executable, which is a decision to make deliberately.

### What didn't help

- Connecting to the compositor earlier (the patch's preconnected socket): KWin's ~4 ms is spent on the first request, not the connection.
- Compact relocations (`-z pack-relative-relocs`): the same pages are still written.
- Tuning malloc (`--quiet-malloc`) once the frame buffer was mapped up front.
- Mailbox against FIFO presentation.
- Trimming wgpu and winit features: `.text` went from 9.0 to 7.2 MB, but cold start didn't change.

### Cold start

With the spike's binary, atlas cache and text file evicted from the page cache (an SSD),
the first frame takes 23.6 ms instead of 13.2 ms, mostly page-ins of the binary's code.
System libraries (RADV, libLLVM, libc) can't be evicted while the compositor has them mapped, so they're always warm in practice.
Reordering the code for locality (PGO or BOLT) is the known fix, if this matters.

## libghostty-vt, 2026-11-08

Same machine, monitors on. `vtbench` (`src/bin/vtbench.rs`) measures the library on its own:

```
./target/release/vtbench [FILE]
```

**At startup it costs nothing measurable.** On the frame thread, creating the terminal takes 0.1 ms,
writing one screen 0.05 ms, and reading the render state into cells 0.25–0.8 ms, all before the window exists.
Launch to first frame, interleaved, 20 runs each:

| Configuration | libghostty | Direct layout (`--no-vt`) |
|---|---|---|
| simple CPU first frame, median (min) | 25.9 ms (16.9) | 19.6 ms (16.0) |
| plain, GPU first frame, median (min) | 72.2 ms (62.6) | 73.1 ms (60.0) |

The CPU path is bimodal in both (17–19 ms or 26–30 ms, on KWin's side), and the medians fell on different sides.
For a plain file, the frame is pixel-identical either way.
The binary grows from 52 to 66 MB (with debug info).

**Per terminal** (100 x 30):

| | std.msh | `ls -la --color` | 24-bit colour on every character |
|---|---|---|---|
| first `terminal_new` in the process | 47 µs | 130 µs | 69 µs |
| `terminal_new`, warm (median) | 12 µs | 12 µs | 11 µs |
| `terminal_free` (median) | 12 µs | 12 µs | 10 µs |
| RSS per idle terminal holding one screen (1,000 terminals) | 34 KB | 38 KB | 175 KB |

So a terminal per block is cheap to create; a thousand finished one-screen blocks are ~35 MB,
more when many cells have their own style.

**Parse throughput**, ~68 MB written in 64 KB chunks into one terminal:

| Input | Default scrollback | Unlimited scrollback | RSS with unlimited |
|---|---|---|---|
| std.msh (plain text, short lines) | 214 MB/s | 66 MB/s | +1,325 MB |
| `ls -la --color` | 169 MB/s | 82 MB/s | +632 MB |
| 24-bit colour on every character | 75 MB/s | 76 MB/s | +163 MB |

The default scrollback is 10,000 bytes, which is one ~400 KB page in practice, so that column is parsing alone.
With unlimited scrollback the time goes into allocating and faulting in new pages.
History costs a full row of cells per line however short the line is:
67 MB of short lines became 1.3 GB.
So each block needs an explicit scrollback limit, and finished blocks should be compressed (the library can, in small steps when idle).

Not yet compared with Alacritty and Ghostty on vtebench, which measures the whole terminal, not just the parser.

### Still to do

- Run on GNOME and Sway, whose first-request cost may differ from KWin's.
- Mac and Windows.
- One panic in 486 runs of the fast build (non-PIE, `no-rt-init`), on the first run after building; it didn't recur, and its message was lost.
- What the product does between the CPU frame and GPU readiness if output arrives: draw on the CPU too, or hold for ~30 ms.
- With two monitors at different scales, the cached scale is wrong when the window opens on the other monitor,
  and the CPU frame is drawn again at the window's size (~3 ms).
- libghostty: cell backgrounds, bold, non-ASCII glyphs, the cursor; vtebench; scrollback compression's cost.
