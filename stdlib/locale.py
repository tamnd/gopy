"""Minimal locale stub for gopy.

Only locale.normalize is used by gettext._expand_lang at runtime.
The full CPython locale.py requires encodings and _collections_abc which
are not yet ported; this stub covers the surface gettext and argparse need.

CPython: Lib/locale.py
"""

# Gettext calls locale.normalize(loc) to expand locale codes like "en_US"
# into "en_US.UTF-8". When no translation catalog is installed the result
# is unused; returning the input unchanged preserves the fallback-to-identity
# path that dgettext follows when no .mo file is present.
#
# CPython: Lib/locale.py:386 normalize
def normalize(localename):
    return localename
