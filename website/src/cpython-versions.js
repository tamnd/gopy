// Single source of truth for which CPython revision the annotations pin to.
//
// To upgrade the annotation pillar to a new CPython release:
//   1. Add a new entry to `revs` below with the release branch head commit.
//   2. Change `current` to the new version string.
//   3. Optionally bulk-update frontmatter `cpython_version` across docs/annotations/.
//
// Old annotation pages can continue to reference an older version explicitly via
// the `version` prop on <Cpy ... version="3.14">, so the upgrade can be staged
// per subsystem rather than flag-day.

const versions = {
  current: '3.14',
  revs: {
    '3.14': 'ab2d84fe1023',
    // '3.15': '...',  // add when we move the pin
  },
};

module.exports = versions;
