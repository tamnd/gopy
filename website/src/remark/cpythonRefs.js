// Remark plugin: turn backticked CPython source references into
// links to github.com/python/cpython at the pinned tag.
//
// Matches inline code that looks like one of:
//   Path/File.c:1234
//   Path/File.c:1234 some_function_name
// where Path starts with one of the standard CPython top-level
// directories.  The function-name suffix (if any) is preserved as
// trailing plain text outside the link.
//
// To bump the pin, edit CPYTHON_REF below.

const CPYTHON_REF = 'v3.14.5';
const ROOTS =
  '(?:Python|Modules|Objects|Parser|Include|Lib|Tools|Doc|Programs|PC|PCbuild)';
const PATH_RE = new RegExp(
  `^(${ROOTS}\\/[\\w./\\-+]+\\.(?:c|h|cpp|hpp|py|asdl|pyx|inc))` +
    `:(\\d+)` +
    `(\\s+\\S.*)?$`,
);

function transform() {
  return (tree) => {
    const visit = (node, index, parent) => {
      if (!node || !parent) return;
      if (node.type === 'inlineCode') {
        const m = PATH_RE.exec(node.value);
        if (m) {
          const [, file, line, trailing] = m;
          const url = `https://github.com/python/cpython/blob/${CPYTHON_REF}/${file}#L${line}`;
          const link = {
            type: 'link',
            url,
            title: null,
            children: [{ type: 'inlineCode', value: `${file}:${line}` }],
          };
          if (trailing) {
            parent.children.splice(index, 1, link, {
              type: 'text',
              value: trailing,
            });
          } else {
            parent.children.splice(index, 1, link);
          }
          return;
        }
      }
      if (Array.isArray(node.children)) {
        for (let i = node.children.length - 1; i >= 0; i--) {
          visit(node.children[i], i, node);
        }
      }
    };
    if (Array.isArray(tree.children)) {
      for (let i = tree.children.length - 1; i >= 0; i--) {
        visit(tree.children[i], i, tree);
      }
    }
  };
}

module.exports = transform;
module.exports.CPYTHON_REF = CPYTHON_REF;
