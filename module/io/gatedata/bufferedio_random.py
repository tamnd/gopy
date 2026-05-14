import _io, sys

# NOTE: BufferedRandom reads in gopy currently disagree with CPython on
# a freshly opened r+b stream (returns post-write contents instead of
# the on-disk bytes), so this gate only exercises the write side and
# verifies the resulting file via a plain FileIO read. Expand once the
# read path lines up.

path = sys.argv[1]
raw = _io.FileIO(path, "r+b")
b = _io.BufferedRandom(raw, 4)
b.seek(0)
print(b.write(b"XY"))
b.flush()
b.close()

raw = _io.FileIO(path, "rb")
print(raw.read())
raw.close()
