---
title: Changelog
sidebar_label: Overview
slug: /changelog
---

# Changelog

Release notes for every shipped version of gopy. Each page is a
human-written walkthrough of what landed, why we built it the way
we did, and where it lives in the code.

## Latest

- [**v0.12.3** (May 15, 2026)](v0.12.3.md). The io subsystem and
  deferred annotations drop. `Modules/_io/*` ports in full from
  CPython, PEP 649 / 749 lazy annotations land end to end, and the
  object protocol full port closes phases 2 through 8.
- [v0.12.2 (May 12, 2026)](v0.12.2.md). The stdlib import
  chain drop. Thirty modules vendor real CPython sources, a real
  regex engine replaces the RE2 shim, and Phase 1 of the object
  protocol full port lands.
- [v0.12.0 (May 8, 2026)](v0.12.0.md). The Tier-2 optimizer drop.
  Trace projector, side-table install path, and the uop dispatch
  loop land end to end.
- [v0.11.0 (May 7, 2026)](v0.11.0.md). Adaptive specialization
  and `sys.monitoring`.
- [v0.10.2 (May 7, 2026)](v0.10.2.md). Parser drop. PEG parser,
  tokenizer, and AST builder ported 1:1 from CPython.
- [v0.10.1 (May 7, 2026)](v0.10.1.md). The backlog drop.

## All releases

| Version | Date | Theme |
|---------|------|-------|
| [v0.12.3](v0.12.3.md) | 2026-05-15 | io subsystem + deferred annotations |
| [v0.12.2](v0.12.2.md) | 2026-05-12 | Stdlib import chain |
| [v0.12.0](v0.12.0.md) | 2026-05-08 | Tier-2 optimizer |
| [v0.11.0](v0.11.0.md) | 2026-05-07 | Specialization + monitoring |
| [v0.10.2](v0.10.2.md) | 2026-05-07 | Parser drop |
| [v0.10.1](v0.10.1.md) | 2026-05-07 | Backlog drop |
| [v0.10.0](v0.10.0.md) | 2026-05-06 | GC |
| [v0.9.0](v0.9.0.md) | earlier | VM remaining |
| [v0.8.0](v0.8.0.md) | earlier | Imports |
| [v0.7.0](v0.7.0.md) | earlier | Lifecycle |
| [v0.5.5](v0.5.5.md) | earlier | Lexer |
| [v0.5.0](v0.5.0.md) | earlier | Compile |
| [v0.4.0](v0.4.0.md) | earlier | Number / string |
| [v0.3.0](v0.3.0.md) | earlier | Exceptions |
| [v0.2.0](v0.2.0.md) | earlier | Types |
| [v0.1.0](v0.1.0.md) | earlier | First runnable bytecode |
| [v0.0.0](v0.0.0.md) | earlier | Repo scaffold |

## Reading guide

If you're new to gopy, start with the latest release notes for the
state of the world today, then work backwards for context on how
the runtime grew. Every release page follows the same layout:

1. **Highlights**. The two or three things that matter most.
2. **What's new**. The full feature breakdown, grouped by area.
3. **Why we built it this way**. Design rationale and tradeoffs.
4. **Where it lives**. File and package pointers into the repo.
5. **Compatibility**. Anything that changed user-visibly.
6. **What's next**. The handoff to the next release.

Spec references throughout link into the internal design docs
under `~/notes/Spec/1700`. Where the spec is private, the prose
captures the same information so the changelog stands alone.
