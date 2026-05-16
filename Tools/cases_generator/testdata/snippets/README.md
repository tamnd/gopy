# Snippet parity corpus (spec 1714 Phase 1)

Each snippet covers one binding row in
`go_generators_common.MACRO_BINDINGS` or one structural feature of
`gowriter.GoWriter`. The format is intentionally simple so both
the Python driver and the Go test harness can read it:

```
snippets/
  <name>/
    input.py   # python invocation that drives the writer
    want.txt   # exact expected output
```

`input.py` is run with `Tools/cases_generator/` on `PYTHONPATH`.
It prints the produced Go text to stdout. The Go test asserts
`stdout == want.txt`.

Running locally:

    go test ./Tools/regen-cases -run TestSnippetParity

Adding a new snippet:

1. Create the directory under `snippets/`.
2. Write `input.py`; print to stdout exactly.
3. Run the test and copy the actual output into `want.txt`.
4. Confirm a second run matches.
