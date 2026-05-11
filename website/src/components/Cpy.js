import React from 'react';
import versions from '@site/src/cpython-versions';

// CPY_VERSION is the default semantic version (e.g. "3.14").
// CPY_REV is the pinned commit hash for that version.
// To upgrade, edit src/cpython-versions.js.
export const CPY_VERSION = versions.current;
export const CPY_REV = versions.revs[versions.current];
export const CPY_REPO = 'https://github.com/python/cpython';

function resolveRev(version) {
  if (!version || version === CPY_VERSION) return CPY_REV;
  const rev = versions.revs[version];
  if (!rev) {
    throw new Error(
      `Unknown CPython version "${version}". Add it to src/cpython-versions.js.`,
    );
  }
  return rev;
}

function buildUrl(path, lines, version) {
  const rev = resolveRev(version);
  let url = `${CPY_REPO}/blob/${rev}/${path}`;
  if (lines == null) return url;
  if (typeof lines === 'number') return `${url}#L${lines}`;
  const [a, b] = String(lines).split('-').map((s) => s.trim());
  return b ? `${url}#L${a}-L${b}` : `${url}#L${a}`;
}

// <Cpy path="Python/ceval.c" lines="60-180">label</Cpy>
// <Cpy path="Python/ceval.c" lines={200}>label</Cpy>
// <Cpy path="Python/ceval.c" version="3.15">label</Cpy>
export function Cpy({path, lines, version, children}) {
  return (
    <a href={buildUrl(path, lines, version)} target="_blank" rel="noreferrer noopener">
      {children || (lines != null ? `${path} #L${lines}` : path)}
    </a>
  );
}

// Permalink banner. Drop directly under a section heading.
//   <CpyLink path="Python/ceval.c" lines="200-340" />
export function CpyLink({path, lines, version}) {
  const v = version || CPY_VERSION;
  const rev = resolveRev(version);
  return (
    <p style={{margin: '0 0 0.75rem 0', fontSize: '0.9em', opacity: 0.8}}>
      <a href={buildUrl(path, lines, version)} target="_blank" rel="noreferrer noopener">
        cpython {v} @ {rev.slice(0, 12)}/{path}
        {lines != null ? `#L${lines}` : ''} <span aria-hidden>↗</span>
      </a>
    </p>
  );
}

export default Cpy;
