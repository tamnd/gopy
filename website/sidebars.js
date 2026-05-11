/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
  manualSidebar: [
    'manual/index',
    {
      label: 'Get started',
      type: 'category',
      className: 'sidebar-heading',
      collapsed: false,
      items: [
        'manual/install',
        'manual/run',
        'manual/hello-world',
      ],
    },
    {
      label: 'Using gopy',
      type: 'category',
      className: 'sidebar-heading',
      collapsed: false,
      items: [
        'manual/cli',
        'manual/repl',
        'manual/embedding',
      ],
    },
    {
      label: 'Project',
      type: 'category',
      className: 'sidebar-heading',
      collapsed: false,
      items: [
        'manual/status',
        'manual/parity',
      ],
    },
  ],

  gopyInternalsSidebar: [
    'gopy/index',
    {
      label: 'Pipeline',
      type: 'category',
      className: 'sidebar-heading',
      collapsed: false,
      items: [
        'gopy/pipeline',
        'gopy/parser',
        'gopy/ast',
        'gopy/symtable',
        'gopy/compiler',
        'gopy/flowgraph',
        'gopy/assembler',
      ],
    },
    {
      label: 'Execution',
      type: 'category',
      className: 'sidebar-heading',
      collapsed: false,
      items: [
        'gopy/vm',
        'gopy/frames',
        'gopy/specializer',
        'gopy/tier2',
        'gopy/monitor',
        'gopy/gil',
        'gopy/exceptions',
      ],
    },
    {
      label: 'Runtime',
      type: 'category',
      className: 'sidebar-heading',
      collapsed: false,
      items: [
        'gopy/objects',
        'gopy/types',
        'gopy/gc',
        'gopy/imports',
        'gopy/generators',
      ],
    },
  ],

  cpythonInternalsSidebar: [
    'cpython/index',
    {
      label: 'Pipeline',
      type: 'category',
      className: 'sidebar-heading',
      collapsed: false,
      items: [
        'cpython/pipeline',
        'cpython/parser',
        'cpython/ast',
        'cpython/symtable',
        'cpython/compiler',
        'cpython/flowgraph',
        'cpython/assembler',
      ],
    },
    {
      label: 'Execution',
      type: 'category',
      className: 'sidebar-heading',
      collapsed: false,
      items: [
        'cpython/vm',
        'cpython/frames',
        'cpython/specializer',
        'cpython/tier2',
        'cpython/monitor',
        'cpython/gil',
        'cpython/exceptions',
      ],
    },
    {
      label: 'Runtime',
      type: 'category',
      className: 'sidebar-heading',
      collapsed: false,
      items: [
        'cpython/objects',
        'cpython/types',
        'cpython/gc',
        'cpython/imports',
        'cpython/generators',
      ],
    },
  ],

  annotationsSidebar: [
    'annotations/index',
    {
      label: 'Parser/',
      type: 'category',
      collapsed: true,
      link: {type: 'doc', id: 'annotations/parser/index'},
      items: [
        'annotations/parser/token',
        'annotations/parser/lexer',
        'annotations/parser/pegen',
      ],
    },
    {
      label: 'Python/',
      type: 'category',
      collapsed: true,
      link: {type: 'doc', id: 'annotations/python/index'},
      items: [
        'annotations/python/compile',
        'annotations/python/symtable',
        'annotations/python/ceval',
      ],
    },
    {type: 'doc', id: 'annotations/objects/index', label: 'Objects/'},
    {type: 'doc', id: 'annotations/include/index', label: 'Include/'},
    {type: 'doc', id: 'annotations/modules/index', label: 'Modules/'},
    {type: 'doc', id: 'annotations/lib/index', label: 'Lib/'},
  ],

  referenceSidebar: [
    'reference/index',
    'reference/opcodes',
    'reference/types',
    'reference/builtins',
    'reference/errors',
  ],

  modulesSidebar: [
    'modules/index',
    {
      label: 'Built-in',
      type: 'category',
      className: 'sidebar-heading',
      collapsed: false,
      items: [
        'modules/sys',
        'modules/builtins',
      ],
    },
    {
      label: 'Standard library',
      type: 'category',
      className: 'sidebar-heading',
      collapsed: false,
      items: [
        'modules/io',
        'modules/os',
        'modules/functools',
        'modules/collections',
        'modules/dataclasses',
        'modules/contextlib',
        'modules/traceback',
        'modules/weakref',
        'modules/types',
      ],
    },
  ],
};

module.exports = sidebars;
