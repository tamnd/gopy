import os, sys
sys.path.insert(0, os.environ['CASES_GENERATOR_DIR'])
from go_generators_common import render_with_bindings, tier2_macro_bindings
print(render_with_bindings(tier2_macro_bindings(), "ERROR_IF", ("err != nil", "label")))
