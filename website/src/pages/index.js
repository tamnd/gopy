import React from 'react';
import Layout from '@theme/Layout';
import Link from '@docusaurus/Link';

export default function Home() {
  return (
    <Layout
      title="gopy"
      description="A from-scratch CPython 3.14, written in Go. Same bytecode, same objects, same error messages."
    >
      <main style={{padding: '4rem 1.5rem', maxWidth: 900, margin: '0 auto'}}>
        <h1 style={{fontSize: '3rem', marginBottom: '0.5rem'}}>gopy</h1>
        <p style={{fontSize: '1.25rem', opacity: 0.85, marginBottom: '2rem'}}>
          A from-scratch CPython 3.14, written in Go. Same bytecode, same
          objects, same error messages.
        </p>

        <div style={{display: 'flex', gap: '0.75rem', marginBottom: '3rem'}}>
          <Link className="button button--primary button--lg" to="/docs/manual">
            Read the manual
          </Link>
          <Link className="button button--secondary button--lg" to="/docs/cpython">
            CPython internals
          </Link>
          <Link className="button button--secondary button--lg" to="/docs/gopy">
            gopy internals
          </Link>
        </div>

        <section style={{marginBottom: '2.5rem'}}>
          <h2>What gopy is</h2>
          <p>
            gopy compiles and runs Python 3.14 source. It uses the same PEG
            grammar, the same bytecode layout, and the same eval loop
            structure as CPython. Where CPython has a function in C, gopy has
            the same function in Go, ported with citations back to the C
            source.
          </p>
        </section>

        <section style={{marginBottom: '2.5rem'}}>
          <h2>Reading order</h2>
          <ol>
            <li>
              <Link to="/docs/manual/install">Install and run</Link> a hello-world.
            </li>
            <li>
              Skim the <Link to="/docs/cpython">CPython pillar</Link>. It is the
              source of truth. Even readers who have never touched gopy can
              learn how CPython 3.14 works from these pages.
            </li>
            <li>
              Cross-read the <Link to="/docs/gopy">gopy pillar</Link>. Each page
              mirrors its CPython counterpart and notes Go-specific differences.
            </li>
            <li>
              Use the <Link to="/docs/reference">reference tables</Link> for
              quick lookups: opcodes, types, builtins, errors.
            </li>
          </ol>
        </section>

        <section>
          <h2>Status</h2>
          <p>
            See the <Link to="/docs/manual/status">subsystem status table</Link>
            {' '}for which parts of CPython have been ported and which are
            still in flight.
          </p>
        </section>
      </main>
    </Layout>
  );
}
