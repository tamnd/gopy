"""Minimal locale stub for gopy.

Only locale.normalize is used by gettext._expand_lang at runtime.
The full CPython locale.py requires encodings and _collections_abc which
are not yet ported; this stub covers the surface gettext and argparse need.

CPython: Lib/locale.py
"""

import sys

import _collections_abc

# CHAR_MAX is the sentinel _locale uses in grouping lists and the
# 127 "not available" value for the monetary fields. Without the C
# _locale module we define it here the way CPython would receive it.
#
# CPython: Lib/locale.py:38 CHAR_MAX = 127
CHAR_MAX = 127

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


# CPython: Lib/locale.py:135 _grouping_intervals
def _grouping_intervals(grouping):
    last_interval = None
    for interval in grouping:
        # if grouping is -1, we are done
        if interval == CHAR_MAX:
            return
        # 0: re-use last group ad infinitum
        if interval == 0:
            if last_interval is None:
                raise ValueError("invalid grouping")
            while True:
                yield last_interval
        yield interval
        last_interval = interval


# perform the grouping from right to left
#
# CPython: Lib/locale.py:150 _group
def _group(s, monetary=False):
    conv = localeconv()
    thousands_sep = conv[monetary and 'mon_thousands_sep' or 'thousands_sep']
    grouping = conv[monetary and 'mon_grouping' or 'grouping']
    if not grouping:
        return (s, 0)
    if s[-1] == ' ':
        stripped = s.rstrip()
        right_spaces = s[len(stripped):]
        s = stripped
    else:
        right_spaces = ''
    left_spaces = ''
    groups = []
    for interval in _grouping_intervals(grouping):
        if not s or s[-1] not in "0123456789":
            # only non-digit characters remain (sign, spaces)
            left_spaces = s
            s = ''
            break
        groups.append(s[-interval:])
        s = s[:-interval]
    if s:
        groups.append(s)
    groups.reverse()
    return (
        left_spaces + thousands_sep.join(groups) + right_spaces,
        len(thousands_sep) * (len(groups) - 1)
    )


# Strip a given amount of excess padding from the given string
#
# CPython: Lib/locale.py:182 _strip_padding
def _strip_padding(s, amount):
    lpos = 0
    while amount and s[lpos] == ' ':
        lpos += 1
        amount -= 1
    rpos = len(s) - 1
    while amount and s[rpos] == ' ':
        rpos -= 1
        amount -= 1
    return s[lpos:rpos+1]


_percent_re = None


# CPython: Lib/locale.py:194 _format
def _format(percent, value, grouping=False, monetary=False, *additional):
    if additional:
        formatted = percent % ((value,) + additional)
    else:
        formatted = percent % value
    if percent[-1] in 'eEfFgGdiu':
        formatted = _localize(formatted, grouping, monetary)
    return formatted


# Transform formatted as locale number according to the locale settings
#
# CPython: Lib/locale.py:205 _localize
def _localize(formatted, grouping=False, monetary=False):
    # floats and decimal ints need special action!
    if '.' in formatted:
        seps = 0
        parts = formatted.split('.')
        if grouping:
            parts[0], seps = _group(parts[0], monetary=monetary)
        decimal_point = localeconv()[monetary and 'mon_decimal_point'
                                              or 'decimal_point']
        formatted = decimal_point.join(parts)
        if seps:
            formatted = _strip_padding(formatted, seps)
    else:
        seps = 0
        if grouping:
            formatted, seps = _group(formatted, monetary=monetary)
        if seps:
            formatted = _strip_padding(formatted, seps)
    return formatted


# CPython: Lib/locale.py:227 format_string
def format_string(f, val, grouping=False, monetary=False):
    """Formats a string in the same way that the % formatting would use,
    but takes the current locale into account.

    Grouping is applied if the third parameter is true.
    Conversion uses monetary thousands separator and grouping strings if
    forth parameter monetary is true."""
    global _percent_re
    if _percent_re is None:
        import re

        _percent_re = re.compile(r'%(?:\((?P<key>.*?)\))?(?P<modifiers'
                                 r'>[-#0-9 +*.hlL]*?)[eEfFgGdiouxXcrs%]')

    percents = list(_percent_re.finditer(f))
    new_f = _percent_re.sub('%s', f)

    if isinstance(val, _collections_abc.Mapping):
        new_val = []
        for perc in percents:
            if perc.group()[-1] == '%':
                new_val.append('%')
            else:
                new_val.append(_format(perc.group(), val, grouping, monetary))
    else:
        if not isinstance(val, tuple):
            val = (val,)
        new_val = []
        i = 0
        for perc in percents:
            if perc.group()[-1] == '%':
                new_val.append('%')
            else:
                starcount = perc.group('modifiers').count('*')
                new_val.append(_format(perc.group(),
                                      val[i],
                                      grouping,
                                      monetary,
                                      *val[i+1:i+1+starcount]))
                i += (1 + starcount)
    val = tuple(new_val)

    return new_f % val


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
