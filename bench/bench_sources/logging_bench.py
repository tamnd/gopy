# logging_bench — stdlib logging at a no-op handler. Exercises % formatting
# and str builder paths.
import os, logging, io
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

log = logging.getLogger("bench")
log.setLevel(logging.INFO)
buf = io.StringIO()
handler = logging.StreamHandler(buf)
handler.setFormatter(logging.Formatter("%(levelname)s:%(name)s:%(message)s"))
log.addHandler(handler)
log.propagate = False

for i in range(max(1, 5000 // _S)):
    log.info("hello %s number %d (%.2f)", "world", i, i * 0.5)
