# Security policy

## Supported versions

`gopy` is in early development. Until v1.0 only the latest tagged
release receives security fixes.

| Version | Supported |
| ------- | --------- |
| 0.0.x   | yes       |
| < 0.0.0 | no        |

## Reporting a vulnerability

Please report vulnerabilities through GitHub's private vulnerability
reporting flow:
<https://github.com/tamnd/gopy/security/advisories/new>.

If you cannot use that flow, open a draft GitHub issue and request a
private contact channel without including any exploit detail.

We aim to:

* acknowledge a report within 72 hours,
* publish a fix or mitigation within 30 days for high-severity issues,
* credit the reporter in the release notes if they wish.

## Out of scope

* Vulnerabilities in CPython itself. Please report those upstream at
  <https://github.com/python/cpython/security>.
* Vulnerabilities in third-party Go modules. Please report those to
  the relevant maintainers.
