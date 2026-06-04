"""Minimal locale stub for gopy.

Only locale.normalize is used by gettext._expand_lang at runtime.
The full CPython locale.py requires encodings and _collections_abc which
are not yet ported; this stub covers the surface gettext and argparse need.

CPython: Lib/locale.py
"""

import sys

# CPython: Lib/locale.py:66 LC category constants (from _locale C module)
LC_CTYPE = 0
LC_COLLATE = 1
LC_TIME = 2
LC_MONETARY = 3
LC_NUMERIC = 4
LC_MESSAGES = 5
LC_ALL = 6


class Error(Exception):
    pass


# CPython: Lib/locale.py:575 setlocale
def setlocale(category, locale=None):
    raise Error("locale not supported in gopy")


# CPython: Lib/locale.py:591 localeconv
def localeconv():
    return {
        'decimal_point': '.',
        'thousands_sep': '',
        'grouping': [],
        'int_curr_symbol': '',
        'currency_symbol': '',
        'mon_decimal_point': '',
        'mon_thousands_sep': '',
        'mon_grouping': [],
        'positive_sign': '',
        'negative_sign': '',
        'int_frac_digits': 127,
        'frac_digits': 127,
        'p_cs_precedes': 127,
        'p_sep_by_space': 127,
        'n_cs_precedes': 127,
        'n_sep_by_space': 127,
        'p_sign_posn': 127,
        'n_sign_posn': 127,
    }


# Gettext calls locale.normalize(loc) to expand locale codes like "en_US"
# into "en_US.UTF-8". When no translation catalog is installed the result
# is unused; returning the input unchanged preserves the fallback-to-identity
# path that dgettext follows when no .mo file is present.
#
# CPython: Lib/locale.py:386 normalize
def normalize(localename):
    return localename


# gopy has no _locale C module, so getencoding falls back to the Python
# filesystem encoding the same way CPython does when _locale.getencoding
# is unavailable. open() uses this as the default text encoding.
#
# CPython: Lib/locale.py:624 getencoding (ImportError fallback)
def getencoding():
    return sys.getfilesystemencoding()


# gopy never defines CODESET, so getpreferredencoding takes the
# no-CODESET branch: utf-8 mode wins, otherwise it defers to
# getencoding(). The do_setlocale argument is accepted for signature
# parity and ignored because setlocale is a stub.
#
# CPython: Lib/locale.py:635 getpreferredencoding (no-CODESET branch)
def getpreferredencoding(do_setlocale=True):
    if sys.flags.utf8_mode:
        return 'utf-8'
    return getencoding()
