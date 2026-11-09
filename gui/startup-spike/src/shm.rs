//! The CPU first frame on Wayland: drawn into shared memory on another thread
//! before the window exists, then attached to the window's surface.
//!
//! Drawing ahead matters: a fresh 2.6 MB buffer costs ~650 page faults
//! (~1.5 ms of kernel time) plus ~1 ms of blending, all of which used to sit
//! between window creation and the first commit.

use std::os::fd::{AsFd, AsRawFd, FromRawFd, OwnedFd};

use wayland_client::backend::{Backend, ObjectId};
use wayland_client::globals::{registry_queue_init, GlobalListContents};
use wayland_client::protocol::{wl_buffer, wl_registry, wl_shm, wl_shm_pool, wl_surface};
use wayland_client::{Connection, Dispatch, EventQueue, Proxy, QueueHandle};

/// Shared memory for the first frame, mapped before any other thread starts.
///
/// mmap, munmap and mprotect take the process's mmap_lock for writing, and
/// the Vulkan driver load holds it for milliseconds at a time (it
/// write-protects libLLVM's relocated pages). Mapping this up front leaves
/// the frame thread only ftruncate (an inode operation) and page faults
/// (per-VMA locks), neither of which waits for that lock.
pub struct FrameMemory {
    fd: OwnedFd,
    ptr: *mut libc::c_void,
    reserved: usize,
}

// SAFETY: the mapping is owned by this value alone.
unsafe impl Send for FrameMemory {}

/// Enough for a 4K frame. Only what is drawn is ever faulted in.
const RESERVE: usize = 64 << 20;

impl FrameMemory {
    pub fn new() -> FrameMemory {
        // SAFETY: plain syscalls; results checked.
        unsafe {
            let fd = libc::memfd_create(c"msh-first-frame".as_ptr(), libc::MFD_CLOEXEC);
            assert!(fd >= 0, "memfd_create");
            let fd = OwnedFd::from_raw_fd(fd);
            let ptr = libc::mmap(
                std::ptr::null_mut(),
                RESERVE,
                libc::PROT_READ | libc::PROT_WRITE,
                libc::MAP_SHARED,
                fd.as_raw_fd(),
                0,
            );
            assert_ne!(ptr, libc::MAP_FAILED, "mmap");
            // Shmem THP is usually "advise": ask, so a 6 MB frame is three
            // faults rather than 1500.
            libc::madvise(ptr, RESERVE, libc::MADV_HUGEPAGE);
            FrameMemory { fd, ptr, reserved: RESERVE }
        }
    }
}

impl Drop for FrameMemory {
    fn drop(&mut self) {
        // SAFETY: ptr/reserved came from a successful mmap.
        unsafe { libc::munmap(self.ptr, self.reserved) };
    }
}

/// An XRGB8888 frame in shared memory, ready to hand to the compositor.
pub struct PreparedFrame {
    mem: FrameMemory,
    len: usize,
    pub width: u32,
    pub height: u32,
}

pub fn prepare(mem: FrameMemory, width: u32, height: u32, draw: impl FnOnce(&mut [u32])) -> PreparedFrame {
    let len = (width * height * 4) as usize;
    assert!(len <= mem.reserved, "frame larger than the reservation");
    // SAFETY: the fd is ours; the mapping covers len bytes once the file does.
    unsafe {
        assert_eq!(libc::ftruncate(mem.fd.as_raw_fd(), len as libc::off_t), 0, "ftruncate");
        draw(std::slice::from_raw_parts_mut(mem.ptr.cast::<u32>(), len / 4));
    }
    PreparedFrame { mem, len, width, height }
}

struct State;

impl Dispatch<wl_registry::WlRegistry, GlobalListContents> for State {
    fn event(_: &mut Self, _: &wl_registry::WlRegistry, _: wl_registry::Event, _: &GlobalListContents, _: &Connection, _: &QueueHandle<Self>) {}
}
impl Dispatch<wl_shm::WlShm, ()> for State {
    fn event(_: &mut Self, _: &wl_shm::WlShm, _: wl_shm::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {}
}
impl Dispatch<wl_shm_pool::WlShmPool, ()> for State {
    fn event(_: &mut Self, _: &wl_shm_pool::WlShmPool, _: wl_shm_pool::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {}
}
impl Dispatch<wl_buffer::WlBuffer, ()> for State {
    fn event(_: &mut Self, _: &wl_buffer::WlBuffer, _: wl_buffer::Event, _: &(), _: &Connection, _: &QueueHandle<Self>) {}
}

/// Keeps the first frame's buffer alive until the GPU path replaces it.
pub struct Presented {
    buffer: wl_buffer::WlBuffer,
    pool: wl_shm_pool::WlShmPool,
    _frame: PreparedFrame,
    _queue: EventQueue<State>,
    conn: Connection,
}

impl Drop for Presented {
    fn drop(&mut self) {
        self.buffer.destroy();
        self.pool.destroy();
        let _ = self.conn.flush();
    }
}

/// Attach `frame` to the window's surface and commit.
///
/// # Safety
/// `display` and `surface` must be the live wl_display and wl_surface of the window.
pub unsafe fn present(display: *mut libc::c_void, surface: *mut libc::c_void, frame: PreparedFrame) -> Presented {
    // SAFETY: the caller guarantees both pointers.
    let backend = unsafe { Backend::from_foreign_display(display.cast()) };
    let conn = Connection::from_backend(backend);
    let (globals, queue) = registry_queue_init::<State>(&conn).expect("wayland registry");
    crate::trace("CPU frame: wl_shm registry");
    let qh = queue.handle();
    let shm: wl_shm::WlShm = globals.bind(&qh, 1..=1, ()).expect("wl_shm");
    let pool = shm.create_pool(frame.mem.fd.as_fd(), frame.len as i32, &qh, ());
    let (w, h) = (frame.width as i32, frame.height as i32);
    let buffer = pool.create_buffer(0, w, h, w * 4, wl_shm::Format::Xrgb8888, &qh, ());
    // SAFETY: the caller guarantees the surface pointer.
    let id = unsafe { ObjectId::from_ptr(wl_surface::WlSurface::interface(), surface.cast()) }.expect("surface id");
    let surface = wl_surface::WlSurface::from_id(&conn, id).expect("surface proxy");
    surface.attach(Some(&buffer), 0, 0);
    surface.damage_buffer(0, 0, w, h);
    surface.commit();
    conn.flush().expect("wayland flush");
    Presented { buffer, pool, _frame: frame, _queue: queue, conn }
}
