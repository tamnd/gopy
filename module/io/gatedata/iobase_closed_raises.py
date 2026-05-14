import _io

b = _io.BytesIO(b"hi")
b.close()
for name, args in (
    ("read",  ()),
    ("write", (b"x",)),
    ("tell",  ()),
    ("seek",  (0,)),
    ("flush", ()),
):
    try:
        getattr(b, name)(*args)
    except ValueError:
        print(name, "ValueError")
print("closed", b.closed)
b.close()
print("closed-again", b.closed)
