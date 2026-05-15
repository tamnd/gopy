# CPython: Modules/_io/textio.c PyTextIOWrapper_Type tp_dictoffset
#
# Exercises the instance-dict and tp_setattro fallback that tokenize.open
# relies on: `text.mode = 'r'` must persist on the instance, arbitrary
# attributes go through the instance dict, and the C-level data
# descriptors stay read-only.
import sys, _io

path = sys.argv[1]
with open(path, "wb") as f:
    f.write(b"hello\n")

buf = _io.FileIO(path, "rb")
text = _io.TextIOWrapper(buf, "utf-8")

# tokenize.open's mode override
text.mode = "r"
print("mode:", text.mode)

# arbitrary user attribute
text.tag = "spam"
print("tag:", text.tag)

# C-level data descriptors stay read-only
for name in ("encoding", "buffer", "line_buffering", "write_through",
             "closed", "newlines", "errors", "name"):
    try:
        setattr(text, name, "x")
    except AttributeError as e:
        print(f"readonly {name}: AttributeError")

# `with` statement uses LOAD_SPECIAL -> type-level __exit__
with text as fp:
    print("read:", fp.read().strip())
