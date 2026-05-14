import _io

# Universal newline translation with pendingcr across chunk boundaries.
d = _io.IncrementalNewlineDecoder(None, True)
print(d.decode("abc\r"))            # "abc", \r held as pendingcr
print(d.newlines)                   # None: nothing committed yet
print(d.decode("\ndef"))            # "\ndef": \r\n collapsed
print(d.newlines)                   # "\r\n"

# Standalone \r at end of stream when final=True flushes.
d = _io.IncrementalNewlineDecoder(None, True)
print(d.decode("abc\r", True))      # "abc\n"
print(d.newlines)                   # "\r"

# Mixed line endings: \r, \n, \r\n produce the 3-tuple in newlines.
# Print the tuple shape via length + per-element ord-list to avoid
# gopy's repr() rendering raw \r/\n inside sequences.
def show_newlines(nl):
    if nl is None:
        print("newlines:none")
        return
    if isinstance(nl, str):
        print("newlines:str", [ord(c) for c in nl])
        return
    print("newlines:tuple", len(nl), [[ord(c) for c in s] for s in nl])

d = _io.IncrementalNewlineDecoder(None, True)
print(d.decode("a\rb\nc\r\n", True))
show_newlines(d.newlines)

# translate=False preserves bytes but still tracks newline kinds.
d = _io.IncrementalNewlineDecoder(None, False)
print(d.decode("a\rb\nc\r\n", True))
show_newlines(d.newlines)

# reset clears seennl and pendingcr.
d = _io.IncrementalNewlineDecoder(None, True)
d.decode("abc\r")
d.reset()
print(d.newlines)
print(d.decode("\nxyz"))            # \n stays \n (no pendingcr after reset)
