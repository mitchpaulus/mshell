// LD_PRELOAD probe: records when the process first sends wl_surface.attach
// (non-null buffer) followed by wl_surface.commit on the same object.
// Writes "<ms since FC_LAUNCH_NS>" to FC_OUT once. Wayland wire format:
// u32 object id, u32 (size << 16 | opcode), then arguments.
#define _GNU_SOURCE
#include <dlfcn.h>
#include <fcntl.h>
#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <time.h>
#include <unistd.h>

static atomic_int done;
static uint32_t attached[64]; static atomic_int nattached;

static void record(void) {
    if (atomic_exchange(&done, 1)) return;
    struct timespec t; clock_gettime(CLOCK_MONOTONIC, &t);
    const char *l = getenv("FC_LAUNCH_NS"), *out = getenv("FC_OUT");
    if (!l || !out) return;
    double ms = ((double)t.tv_sec * 1e9 + t.tv_nsec - strtod(l, 0)) / 1e6;
    char buf[64]; int n = snprintf(buf, sizeof buf, "%.3f\n", ms);
    int fd = open(out, O_WRONLY | O_CREAT | O_EXCL, 0644);
    if (fd >= 0) { write(fd, buf, n); close(fd); }
}

static void scan(const uint8_t *d, size_t len) {
    size_t off = 0;
    while (off + 8 <= len) {
        uint32_t obj, word; memcpy(&obj, d + off, 4); memcpy(&word, d + off + 4, 4);
        uint32_t size = word >> 16, op = word & 0xffff;
        if (size < 8 || off + size > len) return;
        if (op == 1 && size == 20) {
            uint32_t buf; memcpy(&buf, d + off + 8, 4);
            int i = atomic_fetch_add(&nattached, 1);
            if (buf && i < 64) attached[i] = obj;
        } else if (op == 6 && size == 8) {
            int n = atomic_load(&nattached); if (n > 64) n = 64;
            for (int i = 0; i < n; i++) if (attached[i] == obj) { record(); return; }
        }
        off += size;
    }
}

ssize_t sendmsg(int fd, const struct msghdr *msg, int flags) {
    static ssize_t (*real)(int, const struct msghdr *, int);
    if (!real) real = dlsym(RTLD_NEXT, "sendmsg");
    if (!atomic_load(&done) && msg->msg_iovlen <= 4) {
        uint8_t tmp[65536]; size_t len = 0;
        for (size_t i = 0; i < msg->msg_iovlen; i++) {
            size_t n = msg->msg_iov[i].iov_len;
            if (len + n > sizeof tmp) break;
            memcpy(tmp + len, msg->msg_iov[i].iov_base, n); len += n;
        }
        scan(tmp, len);
    }
    return real(fd, msg, flags);
}
