import os, sys
sys.path.insert(0, os.environ['CASES_GENERATOR_DIR'])
from go_generators_common import render_macro
print(render_macro("TOP", ()))
