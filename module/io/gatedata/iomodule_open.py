import _io, sys

path = sys.argv[1]

# Binary write/read goes through the buffered layer in both interpreters.
f = _io.open(path, "wb")
print("wb-type", type(f).__name__.rsplit(".", 1)[-1])
f.write(b"hello")
f.close()

f = _io.open(path, "rb")
print("rb-type", type(f).__name__.rsplit(".", 1)[-1])
print("rb-data", f.read())
f.close()

# Text write/read returns TextIOWrapper.
f = _io.open(path, "w", encoding="utf-8")
print("w-type", type(f).__name__.rsplit(".", 1)[-1])
f.write("alpha\nbeta\n")
f.close()

f = _io.open(path, "r", encoding="utf-8")
print("r-type", type(f).__name__.rsplit(".", 1)[-1])
print("r-data", [ord(c) for c in f.read()])
f.close()

# Unbuffered binary mode (buffering=0) returns the raw FileIO.
f = _io.open(path, "rb", buffering=0)
print("rb0-type", type(f).__name__.rsplit(".", 1)[-1])
f.close()

# Unbuffered text mode is rejected.
try:
    _io.open(path, "r", buffering=0, encoding="utf-8")
except ValueError as e:
    print("text-buffering-0", "ValueError")

# open_code delegates to open(path, 'rb').
f = _io.open_code(path)
print("open_code-type", type(f).__name__.rsplit(".", 1)[-1])
f.close()

# text_encoding returns the encoding unchanged, "locale" for None.
print("te-utf8", _io.text_encoding("utf-8"))
print("te-none", _io.text_encoding(None))
