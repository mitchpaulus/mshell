"""Median launch-to-first-Wayland-commit for a command, via firstcommit.so."""
import os, signal, statistics, subprocess, sys, tempfile, time

HERE = os.path.dirname(os.path.abspath(__file__))

def once(cmd):
    out = tempfile.mktemp(prefix='firstcommit-')
    env = dict(os.environ, LD_PRELOAD=os.environ.get('FC_SO', os.path.join(HERE, 'firstcommit.so')), FC_OUT=out)
    env['FC_LAUNCH_NS'] = str(time.clock_gettime_ns(time.CLOCK_MONOTONIC))
    p = subprocess.Popen(cmd, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                         start_new_session=True)
    deadline = time.monotonic() + 10
    val = None
    while time.monotonic() < deadline:
        try:
            with open(out) as f:
                s = f.read()
            if s.strip():
                val = float(s); break
        except FileNotFoundError:
            pass
        if p.poll() is not None and not os.path.exists(out):
            break
        time.sleep(0.002)
    try: os.killpg(p.pid, signal.SIGKILL)
    except ProcessLookupError: pass
    p.wait()
    if os.path.exists(out): os.unlink(out)
    time.sleep(0.3)  # let the compositor settle between windows
    return val

n = int(sys.argv[1])
cmds = [c.split() for c in sys.argv[2:]]
for c in cmds: once(c)  # warm-up
res = {i: [] for i in range(len(cmds))}
for _ in range(n):  # interleave, so drift hits every command alike
    for i, c in enumerate(cmds):
        v = once(c)
        if v is not None: res[i].append(v)
for i, c in enumerate(cmds):
    v = sorted(res[i])
    print(f"median {statistics.median(v):7.2f} ms  min {v[0]:7.2f}  max {v[-1]:7.2f}  n={len(v)}/{n}  {' '.join(c)}")
