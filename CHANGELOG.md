# Changelog

## [0.1.15] - 2026-10-05

### Changed
- Built on core v0.6.14 / sdk/go/module v0.6.4: unregisters on shutdown and re-registers after core restarts (ADR-0022).

## [0.1.14] - 2026-10-05


### Changed
- Reported version comes from muxcore.json (ADR-0021); built on core v0.6.12 / sdk/go/module v0.6.3 (mesh enrollment, ADR-0017).

## [0.1.13] - 2026-10-05


### Fixed
- Exclusion-list sync no longer deadlocks on the single-connection SQLite pool: the list cursor is drained and closed before the per-list `UPDATE` writes.

## [0.1.12] - 2026-10-05

### Fixed
- Deny actions send a valid `DenyRequestRequest` (the denier is recorded in the reason).

### Changed
- Dependencies resolve from published GitHub tags (core v0.6.2, request-media v0.3.2); CI on GitHub-hosted runners from the umbrella template.

## [0.1.12] — 2026-08-31

### Fixed

- Preserve `act_after` on rescan; apply collection `grace_days` when a rule assigns a collection.
- Resume postponed candidates after `postponed_until` expires.
- Season delete/unmonitor and TV series/episode/season move actions.
- Honor per-rule and global `auto_act_enabled`; free-up respects the pending gate.
- Persist mesh settings to SQLite (`settings_kv`) and reload on startup.
- Cancel scheduler on module stop; close ffprobe mesh connection.
- TV episode/season file size and ffprobe enrichment during scan.

### Added

- `internal/act_test.go` covering pending gate, dry-run, disk gate, free-up ordering, and delete RPCs.
- Admin UI collections CRUD, protections CRUD, and candidate postpone on `/maintainer`.
- Full env documentation in README and `.env.example`.

## [0.1.0] — 2026-08

- Initial MuxCore library maintainer with rules, candidates, collections, and act pipeline.
