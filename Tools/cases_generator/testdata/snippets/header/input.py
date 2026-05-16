import os, sys
sys.path.insert(0, os.environ['CASES_GENERATOR_DIR'])
import sys
from go_generators_common import write_go_header
write_go_header(sys.stdout, "demo_generator.py",
                ["Python/bytecodes.c"], "vm")
