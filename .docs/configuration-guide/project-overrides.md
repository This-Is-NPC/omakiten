# Project overrides

Each project runtime owns an immutable `config.Snapshot`, its operation service,
its editor and its hook engine. A reload builds a replacement runtime; readers
already holding the previous snapshot keep a coherent view until their call ends.

For config-root precedence and disk layout, see [path-resolution.md](path-resolution.md).
For configuration fields, see [system.md](system.md).

## Discovery and selection

Bootstrap resolves an explicit config path first. Otherwise, repository-local
`.omakiten/` configuration takes precedence over the user installation. Active
preset selection is resolved through that installation's `config/.active` marker.

The runtime cache is keyed by project ID. A project with a repository-local
installation resolves its own active bundle. A project without one uses the
selected default bundle. Invalid project selectors, failed discovery and invalid
local bundles return errors; they cannot silently select a different project's
configuration.

## Snapshot ownership

`config.BuildSnapshot` prepares workflow lookup tables, catalogs, entity metadata,
event definitions and settings. Application services capture the snapshot at
construction. Operations receive fully wired services from `agentruntime`.

`BundleCache` owns rebuilds and publication. TUI receives `contract.RuntimeView`
through its runtime-cache port. It does not import the concrete runtime, reload
configuration through application services, or install a synthetic snapshot when
reload fails. `internal/terminal` binds the port for an interactive session.

Template services retain an editor and a bounded file-reading port. They do not
capture unused snapshots. Notification catalogs are bound directly from the
runtime snapshot in `agentruntime`; the application layer does not construct hook
adapter payloads.

## Reload transaction

A replacement remains inactive while loading and consumer rebinding run. The
consumer stages its replacement state before accepting the candidate. A failed
load or rebind leaves the live runtime and consumer unchanged.

After acceptance, the cache drains the previous hook engine, commits store
settings, publishes the replacement and starts its engine. `PreviousSnapshot`
preserves the previous workflow's bucket metadata for orphan detection. Bundle
swap events are emitted after acceptance and publication.

Entity edits use the same reload port. Reload errors remain visible to the caller;
a successful file write cannot manufacture a successful runtime reload.

## Project isolation

Workflow policies, enum registries, synonyms, notification catalogs and event
registries belong to a runtime or snapshot rather than mutable process globals.
SQLite holds operational state and project-indexed event registry references.
Logs and metrics interpret each event against its project's registry, including
cross-project queries. Recovery snapshots remain standalone database images.
Logging, broadcast, recent-row limits and automatic retention use the originating
project's policy; projects without an override inherit the default policy.
Orphan maintenance is store-wide and uses the default policy.

Cache lifecycle operations serialize rebuilds. Closing a runtime drains every
cached hook engine before closing its store. Closed caches reject further builds.

## Maintenance

Keep adapter I/O behind application ports. Add snapshot data through its owner;
do not add compatibility setters, configuration mirrors on the store, or a second
reload path. Update this guide when discovery, snapshot ownership, reload
transactions or project isolation changes.

See [architecture.md](../internal/architecture.md) for package ownership and
[studio.md](studio.md) for staged configuration editing.
