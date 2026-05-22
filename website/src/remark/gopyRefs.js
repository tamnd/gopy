// Remark plugin: turn backticked gopy commit hashes into links to the
// tamnd/gopy commit viewer on GitHub.
//
// Matches inline code that is a hex string of 7–40 characters, e.g.:
//   `a72ac60`
//   `905979e2`
//
// Produces: https://github.com/tamnd/gopy/commit/<hash>

const GOPY_REPO = 'tamnd/gopy';
// 7–12 chars covers short hashes; 40 covers full SHA-1.
const COMMIT_RE = /^[0-9a-f]{7,40}$/;

function transform() {
  return (tree) => {
    const visit = (node, index, parent) => {
      if (!node || !parent) return;
      if (node.type === 'inlineCode') {
        if (COMMIT_RE.test(node.value)) {
          const hash = node.value;
          const url = `https://github.com/${GOPY_REPO}/commit/${hash}`;
          const link = {
            type: 'link',
            url,
            title: null,
            children: [{ type: 'inlineCode', value: hash }],
          };
          parent.children.splice(index, 1, link);
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
module.exports.GOPY_REPO = GOPY_REPO;
