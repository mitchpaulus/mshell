"""Evict files from the page cache (posix_fadvise DONTNEED), then report how
much of each is still resident. Pages another process has mapped stay, so
system libraries the compositor uses (libLLVM, Mesa) can't be evicted.

Usage: python3 -I tools/evict.py FILE...
"""
import mmap, os, sys, ctypes

libc = ctypes.CDLL(None, use_errno=True)

def resident(path):
    size = os.path.getsize(path)
    if size == 0:
        return 0.0
    with open(path, 'rb') as f:
        # A private writable mapping (nothing is written) so ctypes can take
        # its address; mincore reports the file's pages all the same.
        m = mmap.mmap(f.fileno(), size, access=mmap.ACCESS_COPY)
        pages = (size + mmap.PAGESIZE - 1) // mmap.PAGESIZE
        vec = (ctypes.c_ubyte * pages)()
        anchor = ctypes.c_char.from_buffer(m)
        r = libc.mincore(ctypes.c_void_p(ctypes.addressof(anchor)), ctypes.c_size_t(size), vec)
        del anchor
        m.close()
        if r != 0:
            return float('nan')
        return sum(v & 1 for v in vec) / pages

for p in sys.argv[1:]:
    fd = os.open(p, os.O_RDONLY)
    os.posix_fadvise(fd, 0, 0, os.POSIX_FADV_DONTNEED)
    os.close(fd)
    print(f'{resident(p) * 100:5.1f}% resident  {p}')
