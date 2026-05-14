import _io

# To stay byte-stable across Windows text-mode stdout (which folds
# any \n to \r\n in print output and leaves embedded \r untouched),
# we never print decoded values directly. show() emits the codepoint
# list, and show_newlines() emits a structural form of the .newlines
# property.

def show(s):
    print("decoded", [ord(c) for c in s])

def show_newlines(nl):
    if nl is None:
        print("newlines:none")
    elif isinstance(nl, str):
        print("newlines:str", [ord(c) for c in nl])
    else:
        print("newlines:tuple", len(nl), [[ord(c) for c in s] for s in nl])

# Universal newline translation with pendingcr across chunk boundaries.
d = _io.IncrementalNewlineDecoder(None, True)
show(d.decode("abc\r"))            # "abc": trailing \r retained as pendingcr
show_newlines(d.newlines)          # None: nothing committed yet
show(d.decode("\ndef"))            # "\ndef": \r\n collapsed
show_newlines(d.newlines)          # "\r\n"

# Standalone \r at end of stream when final=True flushes.
d = _io.IncrementalNewlineDecoder(None, True)
show(d.decode("abc\r", True))      # "abc\n"
show_newlines(d.newlines)          # "\r"

# Mixed line endings: \r, \n, \r\n produce the 3-tuple in newlines.
d = _io.IncrementalNewlineDecoder(None, True)
show(d.decode("a\rb\nc\r\n", True))
show_newlines(d.newlines)

# translate=False preserves bytes but still tracks newline kinds.
d = _io.IncrementalNewlineDecoder(None, False)
show(d.decode("a\rb\nc\r\n", True))
show_newlines(d.newlines)

# reset clears seennl and pendingcr.
d = _io.IncrementalNewlineDecoder(None, True)
d.decode("abc\r")
d.reset()
show_newlines(d.newlines)
show(d.decode("\nxyz"))            # \n stays \n (no pendingcr after reset)
