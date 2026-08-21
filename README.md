# Media Library Maintainer

MuxCore sidecar module (`media-library-maintainer`) that replaces **Maintainerr**, **Reclaimerr**, and **Deleterr** with a native cleanup pipeline integrated into the MuxCore mesh.

## How it works

```
Rules (JSON AND/OR) ──→ Scan ──→ Candidates (+ optional Collection grace)
                                      │
                                      ├── Protections / manual approve
                                      └── Act ──→ media-movies / media-tvshows
                                                    (delete / unmonitor)
```

Watch history comes from **userdata-local** (MuxCore-native, replaces Tautulli/Plex stats). Request awareness uses **request-media** (Seerr equivalent).

## Quick start

```bash
cd media-library-maintainer
make proto   # if you changed maintainer.proto
make build

export MUXCORE_INSECURE_DISABLE_TLS=true
export MAINTAINER_DB_PATH=/tmp/maintainer.db
./media-library-maintainer --muxcore-mesh-addr localhost:9090
```

### Example rule (unwatched movies added 90+ days ago)

```bash
grpcurl -plaintext -d '{
  "rule": {
    "name": "Stale unwatched movies",
    "enabled": true,
    "scope": "MEDIA_SCOPE_MOVIE",
    "definitionJson": "{\"op\":\"and\",\"conditions\":[{\"field\":\"watch.never_watched\",\"operator\":\"equals\",\"value\":true},{\"field\":\"media.days_since_added\",\"operator\":\"greater_than\",\"value\":90}]}",
    "outcome": "RULE_OUTCOME_CANDIDATE",
    "arrAction": "ARR_ACTION_DELETE",
    "autoActEnabled": false
  }
}' :9545 muxcore.library.maintainer.v1.MaintainerService/UpsertRule
```

Dry-run scan:

```bash
grpcurl -plaintext -d '{"dryRun": true}' :9545 muxcore.library.maintainer.v1.MaintainerService/ScanNow
```

## Configuration

| Env / setting | Default | Description |
|---------------|---------|-------------|
| `MAINTAINER_DB_PATH` | `/var/lib/media-library-maintainer/maintainer.db` | SQLite state |
| `MAINTAINER_GRPC_ADDR` | `:9545` | gRPC listen address |
| `MAINTAINER_USERDATA_DIR` | `/var/lib/muxcore-userdata` | Watch progress store |
| `scan_interval_minutes` | `1440` | Rule evaluation interval |
| `act_interval_minutes` | `360` | Action executor interval |
| `auto_act_enabled` | `false` | Auto-delete pending candidates after delay |
| `dry_run` | `false` | Log-only mode |
| `max_actions_per_run` | `50` | Batch delete limit |

## gRPC API

Service: `muxcore.library.maintainer.v1.MaintainerService`

| RPC | Purpose |
|-----|---------|
| `UpsertRule` / `ListRules` / `DeleteRule` | Rule CRUD |
| `PreviewRule` | Test rule against live library |
| `UpsertCollection` | Grace-period / leaving-soon bucket |
| `ListCandidates` / `ApproveCandidate` / `PostponeCandidate` | Candidate workflow |
| `ListProtections` / `UpsertProtection` | Exclusions |
| `ScanNow` / `ActNow` | Manual scan or act pass |
| `ListRuns` | Audit log |

## Rule fields

Conditions support nested `and` / `or` groups. Leaf fields include:

- **Library:** `media.title`, `media.year`, `media.tmdb_id`, `media.imdb_id`, `media.genres`, `media.monitored`, `media.has_file`, `media.days_since_added`, `media.file_size_bytes`, `media.vote_average`, `media.quality_profile`, `media.root_folder`, `media.series_type`
- **Watch (userdata-local):** `watch.never_watched`, `watch.view_count`, `watch.days_since_last_watched`
- **Requests:** `request.is_requested`, `request.days_since_requested`
- **Protection:** `protection.is_protected`

## Actions

| Action | Behavior |
|--------|----------|
| `delete` | `RemoveMovie` / `RemoveTVShow` / `RemoveEpisodeFile` with files |
| `unmonitor` | Unmonitor then delete files |
| `unmonitor_only` | Set `monitored=false`, keep files |
| `remove_if_empty` | Delete series only when no episode files remain |
| `do_nothing` | Match only (audit) |

## Parity

See [MAINTAINER-PARITY.md](./MAINTAINER-PARITY.md) for Maintainerr / Reclaimerr / Deleterr coverage.

## License

GPL-3.0
