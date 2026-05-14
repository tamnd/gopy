import os, sys

print("cpu>=1", os.cpu_count() >= 1)

b = os.urandom(8)
print("urandom-len", len(b))
print("urandom-type", type(b).__name__)

print("ids-are-int", all(isinstance(x, int) for x in
    (os.geteuid(), os.getegid(), os.getgid())))
print("groups-list", isinstance(os.getgroups(), list))

d = sys.argv[1]
# Pick a per-run subdir keyed on the interpreter executable so the two
# gate runs don't trip over each other when they share a temp dir.
sub = os.path.join(d, os.path.basename(sys.argv[0]) or "run")
if not os.path.exists(sub):
    os.mkdir(sub)
target = os.path.join(sub, "t")
open(target, "w").close()
sym = os.path.join(sub, "s")
os.symlink(target, sym)
print("readlink", os.readlink(sym) == target)
hard = os.path.join(sub, "h")
os.link(target, hard)
print("link-exists", os.path.exists(hard))
os.chmod(target, 0o600)
print("chmod-ok")
prev = os.umask(0o022)
restored = os.umask(prev)
print("umask-restore", restored == 0o022)
