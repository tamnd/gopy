// @ts-check
const { themes } = require('prism-react-renderer');

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'gopy',
  tagline:
    'A from-scratch CPython 3.14, written in Go. Same bytecode, same objects, same error messages.',
  favicon: 'img/favicon.png',

  url: 'https://tamnd.github.io',
  baseUrl: '/gopy/',

  organizationName: 'tamnd',
  projectName: 'gopy',
  trailingSlash: false,

  onBrokenLinks: 'warn',
  onBrokenMarkdownLinks: 'warn',

  markdown: {
    format: 'detect',
  },

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  stylesheets: [
    {
      href: 'https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700;800&family=JetBrains+Mono:wght@400;500;600&display=swap',
      type: 'text/css',
    },
  ],

  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      ({
        docs: {
          sidebarPath: require.resolve('./sidebars.js'),
          editUrl: 'https://github.com/tamnd/gopy/tree/main/website/',
          breadcrumbs: true,
          showLastUpdateTime: false,
        },
        blog: false,
        theme: {
          customCss: require.resolve('./src/css/custom.css'),
        },
        sitemap: {
          changefreq: 'weekly',
          priority: 0.7,
        },
      }),
    ],
  ],

  themeConfig:
    /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
    ({
      image: 'img/og-card.png',
      colorMode: {
        defaultMode: 'dark',
        disableSwitch: false,
        respectPrefersColorScheme: true,
      },
      docs: {
        sidebar: {
          hideable: true,
          autoCollapseCategories: false,
        },
      },
      navbar: {
        title: 'gopy',
        logo: {
          alt: 'gopy logo',
          src: 'img/logo.png',
          srcDark: 'img/logo.png',
          width: 32,
          height: 32,
        },
        hideOnScroll: false,
        items: [
          {
            type: 'docSidebar',
            sidebarId: 'manualSidebar',
            position: 'left',
            label: 'Manual',
          },
          {
            type: 'docSidebar',
            sidebarId: 'cpythonInternalsSidebar',
            position: 'left',
            label: 'CPython',
          },
          {
            type: 'docSidebar',
            sidebarId: 'gopyInternalsSidebar',
            position: 'left',
            label: 'gopy',
          },
          {
            type: 'docSidebar',
            sidebarId: 'annotationsSidebar',
            position: 'left',
            label: 'Annotations',
          },
          {
            type: 'docSidebar',
            sidebarId: 'referenceSidebar',
            position: 'left',
            label: 'Reference',
          },
          {
            type: 'docSidebar',
            sidebarId: 'modulesSidebar',
            position: 'left',
            label: 'Modules',
          },
          {
            type: 'docSidebar',
            sidebarId: 'specsSidebar',
            position: 'left',
            label: 'Specs',
          },
          {
            type: 'docSidebar',
            sidebarId: 'changelogSidebar',
            position: 'left',
            label: 'Changelog',
          },
          {
            href: 'https://github.com/tamnd/gopy',
            label: 'GitHub',
            position: 'right',
          },
        ],
      },
      footer: {
        style: 'light',
        logo: {
          alt: 'gopy logo',
          src: 'img/logo.png',
          width: 32,
          height: 32,
        },
        links: [
          {
            title: 'Learn',
            items: [
              { label: 'Install', to: '/docs/manual/install' },
              { label: 'Run', to: '/docs/manual/run' },
              { label: 'Hello, gopy', to: '/docs/manual/hello-world' },
              { label: 'Manual', to: '/docs/manual/' },
            ],
          },
          {
            title: 'CPython',
            items: [
              { label: 'Pipeline', to: '/docs/cpython/pipeline' },
              { label: 'Compiler', to: '/docs/cpython/compiler' },
              { label: 'VM', to: '/docs/cpython/vm' },
              { label: 'Objects', to: '/docs/cpython/objects' },
            ],
          },
          {
            title: 'gopy',
            items: [
              { label: 'Pipeline', to: '/docs/gopy/pipeline' },
              { label: 'Compiler', to: '/docs/gopy/compiler' },
              { label: 'VM', to: '/docs/gopy/vm' },
              { label: 'Objects', to: '/docs/gopy/objects' },
            ],
          },
          {
            title: 'Project',
            items: [
              {
                label: 'GitHub',
                href: 'https://github.com/tamnd/gopy',
              },
              {
                label: 'Issues',
                href: 'https://github.com/tamnd/gopy/issues',
              },
              {
                label: 'Releases',
                href: 'https://github.com/tamnd/gopy/releases',
              },
              {
                label: 'CPython',
                href: 'https://github.com/python/cpython',
              },
            ],
          },
        ],
        copyright: `© ${new Date().getFullYear()} The gopy Authors. Built with Docusaurus.`,
      },
      prism: {
        theme: themes.oneLight,
        darkTheme: themes.oneDark,
        additionalLanguages: [
          'bash',
          'docker',
          'json',
          'yaml',
          'python',
          'go',
          'c',
          'rust',
          'typescript',
        ],
        magicComments: [
          {
            className: 'theme-code-block-highlighted-line',
            line: 'highlight-next-line',
            block: { start: 'highlight-start', end: 'highlight-end' },
          },
        ],
      },
      algolia: undefined,
      announcementBar: {
        id: 'announcement-v012',
        content:
          'gopy v0.12 ships the Tier-2 trace projector and the PEP 659 adaptive specializer. <a href="/gopy/docs/gopy/tier2">Read the internals →</a>',
        backgroundColor: '#26233a',
        textColor: '#e0def4',
        isCloseable: true,
      },
    }),
};

module.exports = config;
