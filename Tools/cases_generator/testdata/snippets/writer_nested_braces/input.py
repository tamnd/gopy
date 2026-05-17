import os, sys
sys.path.insert(0, os.environ['CASES_GENERATOR_DIR'])
import sys
from gowriter import GoWriter
w = GoWriter(sys.stdout)
w.emit_str("func loop() {\n")
w.emit_str("for i := 0; i < 3; i++ {\n")
w.emit_str("x++\n")
w.emit_str("}\n")
w.emit_str("}\n")
