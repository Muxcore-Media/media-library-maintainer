package internal

import "strings"

func normalizeRuleField(field string) string {
	field = strings.ToLower(strings.TrimSpace(field))
	aliases := map[string]string{
		"tmdb.vote_average":                      "media.vote_average",
		"tmdb.id":                                "media.tmdb_id",
		"imdb.id":                                "media.imdb_id",
		"imdb.rating":                            "ratings.imdb",
		"metacritic.metascore":                   "ratings.metacritic",
		"trakt.rating":                           "ratings.trakt",
		"arr.monitored":                          "media.monitored",
		"media.size":                             "media.file_size_bytes",
		"media.path":                             "media.file_path",
		"media.file_name":                        "media.file_path",
		"seerr.requested":                        "request.is_requested",
		"seerr.requester_has_watched":            "request.requester_watched",
		"seerr.days_since_last_requested":        "request.days_since_requested",
		"episode.number":                         "episode.number",
		"episode.season_number":                  "season.season_number",
		"season.season_number":                   "season.season_number",
		"series.status":                          "media.series_status",
		"movie.version_count":                    "media.version_count",
		"tmdb.runtime_minutes":                   "media.runtime_minutes",
		"tmdb.days_since_first_air_date":         "tmdb.days_since_first_air_date",
		"tmdb.days_since_last_air_date":          "tmdb.days_since_last_air_date",
		"season.days_since_air_date":             "season.days_since_air_date",
		"episode.days_since_air_date":            "episode.days_since_air_date",
		"series.library_season_count":            "series.season_count",
		"playback.play_count":                    "playback.play_count",
		"playback.total_duration_minutes":        "playback.total_duration_minutes",
		"playback.longest_duration_minutes":      "playback.longest_duration_minutes",
		"playback.unique_user_count":             "playback.unique_user_count",
		"playback.has_activity":                  "playback.has_activity",
		"anilist.score":                          "ratings.anilist",
		"letterboxd.score":                       "ratings.letterboxd",
		"playback.user_watched_percent":          "playback.user_watched_percent",
		"playback.user_watched_duration_minutes": "playback.user_watched_duration_minutes",
		"media.container":                        "media.container",
		"video.resolution":                       "video.resolution",
		"video.codec":                            "video.codec",
		"video.bitrate_kbps":                     "video.bitrate_kbps",
		"video.width":                            "video.width",
		"video.height":                           "video.height",
		"video.hdr":                              "video.hdr",
		"audio.channels":                         "audio.channels",
		"audio.codec":                            "audio.codec",
		"video.bit_depth":                        "video.bit_depth",
	}
	if v, ok := aliases[field]; ok {
		return v
	}
	return field
}
