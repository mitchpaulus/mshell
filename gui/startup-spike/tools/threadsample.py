"""Sample every thread of a command from /proc while it starts.

Prints, per thread, a timeline of (state, kernel wait channel, syscall) runs:
R = running, S = sleeping (wchan says on what), D = uninterruptible.
Usage: python3 -I tools/threadsample.py [--until-ms N] cmd args...
"""
import os, signal, subprocess, sys, time

args = sys.argv[1:]
until = 120.0
if args[:1] == ['--until-ms']:
    until = float(args[1]); args = args[2:]

SYSCALLS = {0: 'read', 1: 'write', 7: 'poll', 9: 'mmap', 10: 'mprotect', 11: 'munmap', 12: 'brk',
            16: 'ioctl', 17: 'pread64', 46: 'sendmsg', 47: 'recvmsg', 56: 'clone', 202: 'futex',
            228: 'clock_gettime', 232: 'epoll_wait', 257: 'openat', 262: 'newfstatat', 281: 'epoll_pwait',
            435: 'clone3', 441: 'epoll_pwait2', 3: 'close', 5: 'fstat', 21: 'access', 89: 'readlink'}

def read(path):
    try:
        with open(path) as f: return f.read()
    except OSError: return None

t0 = time.monotonic()
env = dict(os.environ, FC_LAUNCH_NS=str(time.clock_gettime_ns(time.CLOCK_MONOTONIC)))
p = subprocess.Popen(args, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, start_new_session=True, env=env)
samples = {}  # tid -> list of (t_ms, state, wchan, syscall)
names = {}
while True:
    t = (time.monotonic() - t0) * 1e3
    if t > until or p.poll() is not None: break
    try: tids = os.listdir(f'/proc/{p.pid}/task')
    except OSError: break
    for tid in tids:
        stat = read(f'/proc/{p.pid}/task/{tid}/stat')
        if not stat: continue
        name = stat[stat.index('(') + 1:stat.rindex(')')]
        state = stat[stat.rindex(')') + 2]
        wchan = read(f'/proc/{p.pid}/task/{tid}/wchan') or ''
        sc = (read(f'/proc/{p.pid}/task/{tid}/syscall') or '').split()
        scn = SYSCALLS.get(int(sc[0]), sc[0]) if sc and sc[0].lstrip('-').isdigit() and sc[0] != '-1' else (sc[0] if sc else '')
        if sc and sc[0] == 'running': scn = ''
        names[tid] = name
        samples.setdefault(tid, []).append((t, state, wchan if state != 'R' else '', scn if state != 'R' else ''))
try: os.killpg(p.pid, signal.SIGKILL)
except ProcessLookupError: pass
err = p.stderr.read().decode(errors='replace'); p.wait()

for tid, ss in samples.items():
    print(f'--- tid {tid} ({names[tid]}), {len(ss)} samples')
    runs = []
    for t, st, wc, sc in ss:
        key = (st, wc, sc)
        if runs and runs[-1][2] == key: runs[-1][1] = t
        else: runs.append([t, t, key])
    for a, b, (st, wc, sc) in runs:
        if b - a < 0.3 and st == 'R': continue
        print(f'  {a:7.2f}-{b:7.2f} ms  {st} {wc} {sc}')
print('--- stderr (ms since main; add "launch to main" to compare with the samples)'); print(err)
