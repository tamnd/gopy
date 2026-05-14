import _io

b = _io.BytesIO(b"one\ntwo\nthree\n")
print(list(iter(b)))

b = _io.BytesIO(b"")
print(list(iter(b)))

b = _io.BytesIO(b"a\nb")
print(list(iter(b)))
