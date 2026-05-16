# regex_compile — exercises re.compile + cache. Mirrors pyperformance's
# regex_compile in spirit; smaller corpus so wall-clock stays sub-second.
import re, os
_S = max(1, int(os.environ.get("GOPY_BENCH_SCALE", "1")))

PATTERNS = [
    r"(?P<num>\d+)",
    r"(?P<word>\w+)\s+(?P<rest>.*)",
    r"^\s*[A-Z][a-zA-Z0-9_]*\s*=\s*.+$",
    r"(?:foo|bar|baz)\b",
    r"\b\d{4}-\d{2}-\d{2}\b",
    r"[a-zA-Z]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}",
    r"https?://[^\s/$.?#].[^\s]*",
    r"<[^>]+>",
    r"^[ \t]*#.*$",
    r"(?P<key>[A-Za-z_]\w*)\s*:\s*(?P<val>[^,;]+)",
]

def run():
    for _ in range(max(1, 100 // _S)):
        re.purge()
        for p in PATTERNS:
            re.compile(p)

run()
