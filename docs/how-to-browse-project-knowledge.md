# How to browse project knowledge

**The question:** how can I see documentation and API operations from a
registered project in Omakiten, and optionally follow links to another project?

Open the project in the TUI with `Ctrl+P`, then press `K`. The catalog reads
files from the registered project root. Select a resource with `j`/`k` and
`enter`; `esc` returns to the list. Press `r` to read the files again.
The catalog does not write resources or relations to SQLite.

## Start with Markdown

If a project has `docs/` and no `docs/knowledge.yaml`, Omakiten reads its
Markdown files automatically. A page with YAML frontmatter can declare an OKF
type and title:

```markdown
---
type: Reference
title: Checkout flow
---

The client calls [createOrder](okt://api/openapi:createOrder).
```

Relative Markdown links inside a source directory become knowledge relations.
`okt://<project>/<resource-id>` links name a resource in a related project.
Resource IDs for Markdown are `markdown:<path-without-.md>` relative to the
project root. For example, `docs/checkout.md` has ID
`markdown:docs/checkout`.

## Declare sources

Add `docs/knowledge.yaml` when the project has an OpenAPI document, command
documentation, or an explicit set of documentation directories:

```yaml
version: 1
sources:
  - {format: markdown, path: docs}
  - {format: openapi, path: api/openapi.yaml}
  - {format: cli, path: docs/commands.yaml}
related_projects: [api]
```

`markdown` accepts ordinary Markdown and OKF frontmatter. Use `format: okf`
to require a `type` on every page. OpenAPI 3 operations become resources named
`openapi:<operationId>`; operations without `operationId` use `openapi:<METHOD>
<path>`. Referenced component schemas become `openapi:schema:<name>`.

The optional CLI source is a versioned YAML file for command documentation:

```yaml
version: 1
commands:
  - id: orders-create
    name: shop orders create
    summary: Create an order from the terminal
    description: Pass --customer and --item to submit an order.
    links: [openapi:createOrder]
```

Its resource ID is `cli:orders-create`. This format documents the commands a
project chooses to expose; it does not run the CLI or infer behavior from help
text. Files remain authoritative and should be updated alongside the command.

## Read from the CLI

```bash
okt --project client knowledge validate
okt --project client knowledge list --type "CLI Command"
okt --project client knowledge show markdown:docs/checkout
okt --project client knowledge search "order"
okt --project client knowledge search "order" --include-related
okt --project client knowledge show api:openapi:createOrder
```

Only slugs named in `related_projects` are read by `--include-related` or a
qualified `show` ID. If the other project is not registered, validation with
`--include-related` reports the unresolved link. Search without the flag stays
in the selected project.
The TUI includes declared related projects when opening or refreshing its
catalog. Removing `related_projects` keeps the project local.

Invalid files, duplicate IDs, and unresolved links appear as diagnostics in
the TUI. `knowledge validate` reports them as a `validation_error` with a
nonzero exit status. Files must be UTF-8, regular, at most 1 MiB each, and
under the project root. Source paths and traversed files cannot be symlinks.
