---
name: Markdown
description: Frontmatter, tables, code fences, mermaid; renders correctly in GitHub and editor previews.
schema_version: 2
role_affinity:
  - Scribe
---
Markdown is the lingua franca of repository documentation. The discipline is mechanical correctness: the document must render the same in GitHub and in a local editor preview, because a source-readable file that renders broken defeats the format's purpose. Target CommonMark plus GitHub Flavored Markdown (GFM), the de-facto standard for repo docs.

## Constructs to get right

- **Frontmatter** — YAML between leading and trailing `---` fences, valid and parseable. Malformed frontmatter breaks both rendering and any tooling that reads the metadata; keep keys consistent and values correctly typed.
- **Tables** — GFM pipe tables with a header separator row; align columns for source readability. Confirm cell counts match the header — a ragged table renders unpredictably.
- **Code fences** — triple-backtick blocks with a language tag for syntax highlighting. Use fences, not indentation, so the block survives reformatting; pick a fence longer than any backticks inside the content.
- **Mermaid** — fenced blocks tagged `mermaid` render as diagrams on GitHub. Keep node labels free of characters that break the parser, and verify the diagram renders rather than assuming the syntax is valid.

## Render-correctly discipline

Write for the renderer, not just the raw view. Check that headings nest without skipping levels, links resolve, lists keep consistent markers, and the document previews cleanly before committing. The cost of a broken render is paid by every reader; the cost of checking it is paid once by the author.

## Boundaries

This skill governs the *mechanics* of Markdown, not the content's structure or tone — those belong to the document type (README, ADR, design doc) that owns the file. Use it to ensure whatever content those skills produce renders faithfully wherever it is read.
