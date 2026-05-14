import _io

s = _io.StringIO("abc")
s.close()
for name, args in (
    ("read",  ()),
    ("write", ("x",)),
    ("tell",  ()),
    ("seek",  (0,)),
    ("getvalue", ()),
):
    try:
        getattr(s, name)(*args)
    except ValueError:
        print(name, "ValueError")
print("closed", s.closed)
