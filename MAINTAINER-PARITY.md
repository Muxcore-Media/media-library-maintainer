# Maintainerr / Reclaimerr / Deleterr → MuxCore (`media-library-maintainer`) parity

**Goal:** One native MuxCore module replaces all three cleanup tools using `media-movies`, `media-tvshows`, `userdata-local`, and `request-media`.

Legend: `[x]` shipped · `[~]` partial · `[ ]` missing · **Waiver** = accepted MuxCore-native equivalent

---

## A. Core pipeline (shared by all three)

| Feature | Status |
|---------|--------|
| Rule-driven candidate selection | [x] JSON AND/OR engine + `PreviewRule` |
| Watch / age based retention | [x] userdata-local + `days_since_added` |
| Grace period before delete | [x] Collections + `act_after` + `PostponeCandidate` |
| Leaving-soon staging | [x] Candidate status + Jellyfin/Plex sync + notifications + poster overlay |
| Delete via library modules | [x] `RemoveMovie` / `RemoveTVShow` / `RemoveEpisodeFile` / `RemoveFile` |
| Unmonitor variants | [x] `unmonitor`, `unmonitor_only`, `remove_if_empty` |
| Manual approval workflow | [x] `ApproveCandidate` / `CancelCandidate` |
| Protections / exclusions | [x] Protections table + protect outcome rules |
| Request-aware rules | [x] `request-media` integration |
| Scheduled scan + act | [x] Background scheduler + mesh settings |
| Dry run | [x] Global setting + `ScanNow`/`ActNow` dry_run |
| Max actions per run | [x] Setting + `ActNow.max_actions` |
| Run audit log | [x] `run_log` + `ListRuns` |
| Episode / season / series scope | [x] `MediaScope` on rules |
| Movie file / version scope | [x] `MEDIA_SCOPE_MOVIE_FILE` per-file rules + `RemoveFile` act |
| Multi-rule merge (conservative action) | [x] `mergeArrAction` |

---

## B. Maintainerr-specific

| Feature | Status |
|---------|--------|
| Collection handler after N days | [x] Collections.grace_days + act scheduler |
| Plex / Jellyfin / Emby rule operands | **Waiver** — MuxCore library modules + userdata |
| Seerr request cleanup on delete | [x] DenyRequest on successful act |
| Quality profile change action | [x] `ARR_ACTION_CHANGE_QUALITY_PROFILE` + rule `quality_profile_id` |
| Poster overlays / leaving shelf | [x] Maintainerr-style JSON templates on poster/backdrop/still (episode title cards) |
| Sportarr | N/A |
| Tautulli / Tracearr / Streamystats operands | [x] `playback-monitor` gRPC when registered; userdata-local fallback |
| Community rule YAML import | [x] gRPC + admin UI export/import |
| Download client cleanup | [x] qBittorrent + Transmission + Deluge via Radarr/Sonarr history |
| Storage metrics dashboard | [x] `GetStorageMetrics` gRPC + admin UI storage panel |
| Notifications (Discord, etc.) | [x] optional via `notification` capability |
| Tag in Radarr/Sonarr while staged | [x] `tag_enabled` + `SetItemTags` |
| List exclusion on delete | [x] `add_list_exclusion_on_delete` + Radarr API when configured |

---

## C. Reclaimerr-specific

| Feature | Status |
|---------|--------|
| Rich unified field catalog (80+ fields) | **Waiver** — ~70 fields via core + Reclaimerr aliases (playback, per-user playback, probe, ratings); extensible JSON engine |
| Protect vs candidate dual rules | [x] `RuleOutcome` |
| Move instead of delete | [x] `ARR_ACTION_MOVE` + `MAINTAINER_MOVE_PATH` |
| Multi-instance Radarr/Sonarr routing | **Waiver** — single movies/tvshows mesh |
| External API v1 + webhooks | **Waiver** — gRPC API only |
| OIDC / RBAC | **Waiver** — core auth |
| Background job pool | **Waiver** — inline scheduler |
| Ratings sync (IMDb, AniList, …) | **Waiver** — OMDB + Trakt + AniList + Letterboxd via MDBList |
| Movie version scope | [x] `movie_file` scope with per-file inventory |
| Auto-delete per rule delay | [x] `auto_act_delay_days` |

---

## D. Deleterr-specific

| Feature | Status |
|---------|--------|
| YAML-only config | **Waiver** — gRPC + mesh settings (MuxCore-native) |
| Plex-only library binding | **Waiver** — native movies/tvshows |
| Trakt / Mdblist / JustWatch exclusions | [x] Trakt + MDBList lists + JustWatch streaming policies |
| `--free-up` emergency disk reclaim | [x] `ActNow.free_up` + `target_free_percent` |
| Leaving-soon Plex collections + user notify | [x] Jellyfin BoxSet + Plex collections + notifications |
| Watch status watched/unwatched filter | [x] via `watch.never_watched` / view count |
| Seerr protect unwatched requesters | [x] Auto-protect via userdata + `protect_unwatched_requesters` |
| Disk threshold gate (only delete when full) | [x] `disk_act_max_free_percent` + `disk.free_percent` rule field |

---

## E. Admin / ops surfaces

| Surface | Status |
|---------|--------|
| gRPC API | [x] `MaintainerService` |
| admin-ui pages | [x] `/maintainer` rules (YAML import/export), candidates, runs, exclusions, storage, free-up |
| Example grpcurl flows | [x] README |
| Docker / compose | [x] template from module-starter |
| CI | [x] workflow from module-starter |

---

## F. MuxCore integrations

| Module | Capability | Use |
|--------|------------|-----|
| `media-movies` | `media.library.movies` | Inventory, delete, unmonitor, tags, poster overlay |
| `media-tvshows` | `media.library.tv` | Series inventory & actions, poster/backdrop/episode still overlays |
| `userdata-local` | `userdata` | Watch progress + requester watch checks |
| `request-media` | `media.request` | Pending request exclusions + requester identity |
| Jellyfin (HTTP) | env `JELLYFIN_BASE_URL` | Leaving-soon BoxSet collections |
| Plex (HTTP) | env `PLEX_URL` + `PLEX_TOKEN` | Leaving-soon Plex collections |
| Radarr / Sonarr (HTTP) | env `RADARR_URL`, `SONARR_URL` | History download IDs + import exclusions |
| qBittorrent / Transmission / Deluge (HTTP) | env URLs | Torrent cleanup after delete |
| OMDB / Trakt (HTTP) | env `OMDB_API_KEY`, `TRAKT_CLIENT_ID` | Rating enrichment (movies + TV) |
| MDBList (HTTP) | env `MDBLIST_API_KEY` | Letterboxd + supplemental ratings; exclusion lists |
| AniList (HTTP GraphQL) | public API | Anime score enrichment for Animation/Anime genres |
| `media-ffprobe` | `media.analyzer` | File probe operands (codec, bitrate, HDR, channels) |
| `notification` | `notification` | Run summaries + leaving-soon alerts |

---

## G. Remaining work (optional polish)

1. **Overlay template visual editor** — **Waiver**: JSON templates + mesh settings (`overlay_template_json`) are the MuxCore-native equivalent of Maintainerr's canvas editor

---

## H. Baseline

`media-library-maintainer` **v0.1.12** — native cleanup module replacing Maintainerr, Reclaimerr, and Deleterr: rule engine with ~70 Reclaimerr field aliases, per-user playback rules, full probe operands, OMDB/Trakt/AniList/Letterboxd ratings, Maintainerr-style overlay templates (poster/backdrop/episode still), `ListPlaybackUsers`, candidate workflow, grace collections, scheduled scan/act, Jellyfin + Plex leaving-soon, download client cleanup, movie_file scope, admin UI at `/maintainer`.

Reference repos analyzed at `/tmp/muxcore-ref/{maintainerr,reclaimerr,deleterr}`.
