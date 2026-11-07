// LD_PRELOAD probe: logs every dlopen with its thread, start and duration.
// Times are ms since FC_LAUNCH_NS (CLOCK_MONOTONIC at launch) if set, else
// since the first dlopen. Lines go to stderr as they finish.
//
//   gcc -O2 -shared -fPIC -o tools/dltime.so tools/dltime.c -ldl
//   LD_PRELOAD=tools/dltime.so FC_LAUNCH_NS=$(...) ./target/release/startup-spike ...
#define _GNU_SOURCE
#include <dlfcn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>
#include <sys/syscall.h>

static double now_ms(void) {
    static long long base = -1;
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    long long ns = ts.tv_sec * 1000000000LL + ts.tv_nsec;
    if (base < 0) {
        const char *s = getenv("FC_LAUNCH_NS");
        base = s ? atoll(s) : ns;
    }
    return (ns - base) / 1e6;
}

void *dlopen(const char *file, int mode) {
    static void *(*real)(const char *, int);
    if (!real) real = dlsym(RTLD_NEXT, "dlopen");
    double t0 = now_ms();
    void *h = real(file, mode);
    double t1 = now_ms();
    char line[512];
    int n = snprintf(line, sizeof line, "dlopen %8.2f +%6.2f ms tid %ld %s%s\n", t0, t1 - t0,
                     (long)syscall(SYS_gettid), file ? file : "(self)", h ? "" : " FAILED");
    write(2, line, n > 0 && n < (int)sizeof line ? n : 0);
    return h;
}

// dlsym and pthread_create also take the loader's locks; log slow calls only.
#include <pthread.h>
static void slow(const char *what, const char *arg, double t0, double t1) {
    if (t1 - t0 < 0.2) return;
    char line[512];
    int n = snprintf(line, sizeof line, "%s %8.2f +%6.2f ms tid %ld %s\n", what, t0, t1 - t0,
                     (long)syscall(SYS_gettid), arg ? arg : "");
    write(2, line, n > 0 && n < (int)sizeof line ? n : 0);
}

// Timing wrappers for libraries loaded with dlopen+dlsym (xkbcommon-dl),
// which LD_PRELOAD can't interpose directly: dlsym hands these out instead.
#define WRAP(ret, fn, params, args)                                   \
    static ret (*real_##fn) params;                                   \
    static ret wrap_##fn params {                                     \
        double t0 = now_ms();                                         \
        ret r = real_##fn args;                                       \
        double t1 = now_ms();                                         \
        char line[160];                                               \
        int n = snprintf(line, sizeof line, "call   %8.2f +%6.2f ms tid %ld %s\n", t0, \
                         t1 - t0, (long)syscall(SYS_gettid), #fn);    \
        write(2, line, n);                                            \
        return r;                                                     \
    }
WRAP(void *, xkb_context_new, (int f), (f))
WRAP(void *, xkb_keymap_new_from_string, (void *c, const char *s, int fmt, int fl), (c, s, fmt, fl))
WRAP(void *, xkb_keymap_new_from_buffer, (void *c, const char *s, size_t len, int fmt, int fl), (c, s, len, fmt, fl))
WRAP(void *, xkb_state_new, (void *k), (k))
WRAP(void *, xkb_compose_table_new_from_locale, (void *c, const char *l, int f), (c, l, f))
WRAP(void *, xkb_compose_state_new, (void *t, int f), (t, f))

void *dlsym(void *h, const char *name) {
    static void *(*real)(void *, const char *);
    if (!real) real = dlvsym(RTLD_NEXT, "dlsym", "GLIBC_2.34");
    if (!real) real = dlvsym(RTLD_NEXT, "dlsym", "GLIBC_2.2.5");
    double t0 = now_ms();
    void *r = real(h, name);
    slow("dlsym ", name, t0, now_ms());
#define HOOK(fn) if (r && !strcmp(name, #fn)) { real_##fn = r; return (void *)wrap_##fn; }
    HOOK(xkb_context_new) HOOK(xkb_keymap_new_from_string) HOOK(xkb_keymap_new_from_buffer)
    HOOK(xkb_state_new) HOOK(xkb_compose_table_new_from_locale) HOOK(xkb_compose_state_new)
    return r;
}

int pthread_create(pthread_t *t, const pthread_attr_t *a, void *(*f)(void *), void *arg) {
    static int (*real)(pthread_t *, const pthread_attr_t *, void *(*)(void *), void *);
    if (!real) real = dlsym(RTLD_NEXT, "pthread_create");
    double t0 = now_ms();
    int r = real(t, a, f, arg);
    slow("pthread_create", "", t0, now_ms());
    return r;
}

// First use of a thread_local with a destructor registers it here, and glibc
// takes the loader lock to pin the owning object: it waits for any dlopen.
int __cxa_thread_atexit_impl(void (*dtor)(void *), void *obj, void *dso) {
    static int (*real)(void (*)(void *), void *, void *);
    if (!real) real = dlsym(RTLD_NEXT, "__cxa_thread_atexit_impl");
    double t0 = now_ms();
    int r = real(dtor, obj, dso);
    double t1 = now_ms();
    char line[160];
    Dl_info info;
    const char *who = dladdr((void *)dtor, &info) && info.dli_sname ? info.dli_sname : "?";
    int n = snprintf(line, sizeof line, "tlsdtor %8.2f +%6.2f ms tid %ld %s\n", t0, t1 - t0,
                     (long)syscall(SYS_gettid), who);
    write(2, line, n);
    return r;
}
