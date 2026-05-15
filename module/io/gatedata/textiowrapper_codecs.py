import io
import sys

# Force stdout to UTF-8 so non-ASCII chars in repr() do not blow up
# under Windows' default cp1252 console codepage. Both interpreters
# need the same stdout encoding for the gate diff to be meaningful.
try:
    sys.stdout.reconfigure(encoding="utf-8")
except AttributeError:
    pass

# Round-trip a small known-tricky sample across each codec the
# TextIOWrapper port now supports. Compare bytes-on-disk and the
# decoded string side by side so any drift in the encode/decode tables
# shows up in the gate diff.
SAMPLES = [
    ("utf-16",    "Hello, World!"),
    ("utf-16-le", "abc def"),
    ("utf-16-be", "xyz 123"),
    ("utf-32",    "a"),
    ("utf-32-le", "abc"),
    ("utf-32-be", "z"),
    ("cp1252",    "Euro: € and quotes: “x”"),
    ("cp1250",    "Czech: Č"),
    ("cp1251",    "Cyrillic: Мир"),
    ("cp437",     "Box: │ ─"),
    ("mac-roman", "cafe"),
]

path = sys.argv[1]

for enc, text in SAMPLES:
    f = open(path, "w", encoding=enc)
    f.write(text)
    f.close()
    f = open(path, "rb")
    raw = f.read()
    f.close()
    f = open(path, "r", encoding=enc)
    got = f.read()
    f.close()
    print(enc, len(raw), repr(got))
