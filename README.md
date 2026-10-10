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

### Module env

| Variable | Default | Description |
|----------|---------|-------------|
| `MAINTAINER_DB_PATH` | `/var/lib/media-library-maintainer/maintainer.db` | SQLite state (rules, candidates, settings) |
| `MAINTAINER_GRPC_ADDR` | `:9545` | gRPC listen address |
| `MAINTAINER_USERDATA_DIR` | `/var/lib/muxcore-userdata` | Watch progress store for playback rules |
| `MAINTAINER_MOVE_PATH` | — | Archive destination for move actions |
| `MAINTAINER_FREE_UP_ROOT` | `/data` | Root path monitored for emergency free-up |
| `MAINTAINER_ADD_LIST_EXCLUSION` | `false` | Add Radarr TMDB exclusions on movie delete |
| `ERASURE_SWEEP_INTERVAL` | `5m` | How often the user-erasure reconciler reads the identity ledger (Go duration) |

### Mesh settings (persisted to SQLite)

| Key | Default | Description |
|-----|---------|-------------|
| `scan_interval_minutes` | `1440` | Rule evaluation interval |
| `act_interval_minutes` | `360` | Action executor interval |
| `auto_act_enabled` | `false` | Auto-act pending candidates after `act_after` |
| `dry_run` | `false` | Log-only scan/act (no persist or deletes) |
| `max_actions_per_run` | `50` | Batch delete limit per act cycle |
| `overlay_enabled` / `overlay_*` | — | Leaving-soon poster overlays via MediaAdmin |
| `download_client_*` | — | qBittorrent cleanup after delete |

See `.env.example` for Jellyfin, Plex, Radarr, Sonarr, qBittorrent, Transmission, Deluge, OMDB, Trakt, MDBList, and overlay env vars.

### Integration env (optional)

| Variable | Purpose |
|----------|---------|
| `JELLYFIN_BASE_URL` / `MAINTAINER_JELLYFIN_*` | Leaving-soon Jellyfin collections |
| `PLEX_URL` / `PLEX_TOKEN` / `MAINTAINER_PLEX_*` | Leaving-soon Plex collections |
| `RADARR_URL` / `RADARR_API_KEY` | Movie history, import exclusions |
| `SONARR_URL` / `SONARR_API_KEY` | TV file paths, episode metadata |
| `QBITTORRENT_*` / `MAINTAINER_QBITTORRENT_*` | Torrent cleanup after delete |
| `TRANSMISSION_*` / `MAINTAINER_TRANSMISSION_*` | Alternative download client |
| `DELUGE_*` / `MAINTAINER_DELUGE_*` | Alternative download client |
| `OMDB_API_KEY` / `MAINTAINER_OMDB_API_KEY` | OMDB ratings for rules |
| `TRAKT_CLIENT_ID` / `MAINTAINER_TRAKT_*` | Trakt exclusion lists |
| `MDBLIST_API_KEY` / `MAINTAINER_MDBLIST_*` | MDBList ratings and lists |
| `JUSTWATCH_API_URL` | JustWatch exclusion policies |

## User erasure (ADR-0035)

When an admin deletes a user, the identity provider records a tombstone in its
erasure ledger. This module runs a reconciler (startup, then every
`ERASURE_SWEEP_INTERVAL`) that reads the ledger **only** from the verified
provider of the exclusive `identity` capability, found through core.

For each tombstone it has not applied, it deletes every candidate whose stored
`criteria_json` names the erased user **id** as `RequestedBy` or as a key of a
per-user watch map (`UserWatchedPercent`, `UserWatchedDurationMinutes`). The
delete and the `erasure_applied` record are one SQLite transaction; re-applying
a tombstone is a no-op. Matching is exact on the id, never a username or a
prefix. Candidates are regenerated on the next scan from current data, and a
scan never persists an id that has an `erasure_applied` record.

- `household`/`staging` profiles refuse to start without `MUXCORE_GRPC_ADDR`
  (a core connection); `dev` logs a warning and disables the reconciler.
- Candidates carry no tenant, so the tombstone's tenant is recorded but not
  compared; user ids are globally unique.
- Not covered by this disposition: the poster overlay and notification state
  for a deleted candidate (`overlay_state`, `leaving_soon_notified`; they hold
  item ids, not user ids) and the userdata reads in `requester_protect.go`
  (roadmap C-39).

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
