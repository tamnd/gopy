import _io

# NOTE: gopy's repr() on strings containing '\n' currently emits the raw
# newline instead of the '\\n' escape (a repr-side bug, not a stringio
# bug), so the gate prints length + content rather than repr to keep
# the focus on stringio behavior.

def show(line):
    print(len(line), "|", line.rstrip("\n"), "|", line.endswith("\n"))

s = _io.StringIO("abc\ndef\nghi")
show(s.readline())
show(s.readline())
show(s.readline())
show(s.readline())

s = _io.StringIO("one\ntwo\nthree\n")
for line in s.readlines():
    show(line)
