import os, sys
sys.path.insert(0, os.environ['CASES_GENERATOR_DIR'])
import sys
from gowriter import GoWriter
w = GoWriter(sys.stdout)
w.emit_str("func opNop() {\n")
w.emit_str("return\n")
w.emit_str("}\n")
