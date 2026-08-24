package internal

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func evaluateRule(def RuleDefinition, ctx EvalContext) (bool, error) {
	op := strings.ToLower(strings.TrimSpace(def.Op))
	if op == "" {
		op = "and"
	}

	var condResults []bool
	for _, c := range def.Conditions {
		ok, err := evalCondition(c, ctx)
		if err != nil {
			return false, err
		}
		condResults = append(condResults, ok)
	}
	for _, g := range def.Groups {
		ok, err := evaluateRule(g, ctx)
		if err != nil {
			return false, err
		}
		condResults = append(condResults, ok)
	}
	if len(condResults) == 0 {
		return false, nil
	}
	switch op {
	case "or":
		for _, ok := range condResults {
			if ok {
				return true, nil
			}
		}
		return false, nil
	default:
		for _, ok := range condResults {
			if !ok {
				return false, nil
			}
		}
		return true, nil
	}
}

func evalCondition(c RuleCondition, ctx EvalContext) (bool, error) { //nolint:gocyclo // mirrors maintainer rule field catalog
	field := normalizeRuleField(c.Field)
	op := strings.ToLower(strings.TrimSpace(c.Operator))

	switch field {
	case "media.title", "title":
		return evalText(ctx.Title, op, c.Value)
	case "media.year", "year":
		return evalNumber(float64(ctx.Year), op, c.Value)
	case "media.tmdb_id", "tmdb_id":
		return evalNumber(float64(ctx.TmdbID), op, c.Value)
	case "media.imdb_id", "imdb_id":
		return evalText(ctx.ImdbID, op, c.Value)
	case "media.genre", "media.genres", "genres":
		return evalGenre(ctx.Genres, op, c.Value)
	case "media.monitored", "monitored":
		return evalBool(ctx.Monitored, op, c.Value)
	case "media.has_file", "has_file":
		return evalBool(ctx.HasFile, op, c.Value)
	case "media.days_since_added", "days_since_added":
		days := daysSince(ctx.AddedAt)
		return evalNumber(float64(days), op, c.Value)
	case "media.file_size_bytes", "file_size_bytes":
		return evalNumber(float64(ctx.FileSizeBytes), op, c.Value)
	case "media.file_path", "file_path":
		return evalText(ctx.FilePath, op, c.Value)
	case "media.file_quality", "file_quality":
		return evalText(ctx.FileQuality, op, c.Value)
	case "media.movie_id", "movie_id":
		return evalText(ctx.MovieID, op, c.Value)
	case "media.vote_average", "vote_average", "media.rating", "ratings.tmdb", "ratings.vote_average":
		return evalNumber(ctx.VoteAverage, op, c.Value)
	case "ratings.imdb":
		return evalNumber(ctx.ImdbRating, op, c.Value)
	case "ratings.metacritic", "ratings.rotten_tomatoes":
		return evalNumber(ctx.CriticScore, op, c.Value)
	case "ratings.trakt":
		return evalNumber(ctx.TraktRating, op, c.Value)
	case "ratings.anilist", "anilist.score":
		return evalNumber(ctx.AnilistScore, op, c.Value)
	case "letterboxd.score", "ratings.letterboxd":
		return evalNumber(ctx.LetterboxdScore, op, c.Value)
	case "letterboxd.vote_count":
		return evalNumber(float64(ctx.LetterboxdVoteCount), op, c.Value)
	case "media.quality_profile", "quality_profile":
		return evalText(ctx.QualityProfile, op, c.Value)
	case "media.root_folder", "root_folder":
		return evalText(ctx.RootFolderPath, op, c.Value)
	case "media.series_type", "series_type":
		return evalText(ctx.SeriesType, op, c.Value)
	case "watch.view_count", "view_count":
		return evalNumber(float64(ctx.ViewCount), op, c.Value)
	case "playback.play_count":
		return evalNumber(float64(ctx.PlaybackPlayCount), op, c.Value)
	case "playback.total_duration_minutes":
		return evalNumber(ctx.PlaybackTotalDurationMinutes, op, c.Value)
	case "playback.longest_duration_minutes":
		return evalNumber(ctx.PlaybackLongestDurationMinutes, op, c.Value)
	case "playback.unique_user_count":
		return evalNumber(float64(ctx.PlaybackUniqueUsers), op, c.Value)
	case "playback.has_activity":
		return evalBool(ctx.PlaybackHasActivity, op, c.Value)
	case "playback.user_watched_percent":
		return evalUserScopedPlayback(ctx.UserWatchedPercent, op, c.Value)
	case "playback.user_watched_duration_minutes":
		return evalUserScopedPlayback(ctx.UserWatchedDurationMinutes, op, c.Value)
	case "playback.usernames":
		return evalPlaybackUsernames(ctx.UserWatchedDurationMinutes, op, c.Value)
	case "watch.days_since_last_watched", "days_since_last_watched":
		d := ctx.DaysSinceLastWatch
		if ctx.NeverWatched {
			d = 999999
		}
		return evalNumber(float64(d), op, c.Value)
	case "watch.never_watched", "never_watched":
		return evalBool(ctx.NeverWatched, op, c.Value)
	case "request.is_requested", "is_requested":
		return evalBool(ctx.Requested, op, c.Value)
	case "request.requester_watched":
		return evalBool(ctx.RequesterWatched, op, c.Value)
	case "request.requester_unwatched":
		return evalBool(!ctx.RequesterWatched, op, c.Value)
	case "request.days_since_requested", "days_since_requested":
		return evalNumber(float64(ctx.DaysSinceRequest), op, c.Value)
	case "protection.is_protected", "is_protected":
		return evalBool(ctx.Protected, op, c.Value)
	case "disk.free_percent":
		return evalNumber(ctx.DiskFreePercent, op, c.Value)
	case "disk.free_bytes":
		return evalNumber(float64(diskFreeBytes(ctx.RootFolderPath)), op, c.Value)
	case "media.runtime_minutes", "runtime_minutes":
		return evalNumber(float64(ctx.RuntimeMinutes), op, c.Value)
	case "media.version_count", "version_count":
		return evalNumber(float64(ctx.MovieVersionCount), op, c.Value)
	case "series.season_count", "season_count":
		return evalNumber(float64(ctx.SeasonCount), op, c.Value)
	case "media.series_status", "series_status":
		return evalText(ctx.SeriesStatus, op, c.Value)
	case "season.season_number":
		return evalNumber(float64(ctx.SeasonNumber), op, c.Value)
	case "episode.number":
		return evalNumber(float64(ctx.EpisodeNumber), op, c.Value)
	case "tmdb.days_since_first_air_date":
		return evalNumber(float64(daysSince(ctx.FirstAirDate)), op, c.Value)
	case "tmdb.days_since_last_air_date":
		return evalNumber(float64(daysSince(ctx.LastAirDate)), op, c.Value)
	case "season.days_since_air_date":
		return evalNumber(float64(daysSince(ctx.FirstAirDate)), op, c.Value)
	case "episode.days_since_air_date":
		return evalNumber(float64(daysSince(ctx.EpisodeAirDate)), op, c.Value)
	case "media.container", "container":
		return evalGenre([]string{ctx.MediaContainer}, op, c.Value)
	case "video.resolution":
		return evalText(ctx.VideoResolution, op, c.Value)
	case "video.codec":
		return evalText(ctx.VideoCodec, op, c.Value)
	case "video.bitrate_kbps":
		return evalNumber(ctx.VideoBitrateKbps, op, c.Value)
	case "video.width":
		return evalNumber(float64(ctx.VideoWidth), op, c.Value)
	case "video.height":
		return evalNumber(float64(ctx.VideoHeight), op, c.Value)
	case "video.hdr":
		return evalBool(ctx.VideoHDR, op, c.Value)
	case "audio.channels":
		return evalNumber(float64(ctx.AudioChannels), op, c.Value)
	case "audio.codec":
		return evalText(ctx.AudioCodec, op, c.Value)
	case "video.bit_depth":
		return evalNumber(float64(ctx.VideoBitDepth), op, c.Value)
	default:
		return false, fmt.Errorf("unknown field %q", c.Field)
	}
}

func evalText(have, op string, want any) (bool, error) {
	h := strings.ToLower(strings.TrimSpace(have))
	w := strings.ToLower(strings.TrimSpace(fmt.Sprint(want)))
	switch op {
	case "equals", "eq", "is":
		return h == w, nil
	case "not_equals", "neq":
		return h != w, nil
	case "contains":
		return strings.Contains(h, w), nil
	case "not_contains":
		return !strings.Contains(h, w), nil
	case "matches_regex", "regex":
		re, err := regexp.Compile(fmt.Sprint(want))
		if err != nil {
			return false, err
		}
		return re.MatchString(have), nil
	case "exists":
		return have != "", nil
	case "not_exists":
		return have == "", nil
	default:
		return false, fmt.Errorf("unknown text operator %q", op)
	}
}

func evalNumber(have float64, op string, want any) (bool, error) {
	target, err := toFloat(want)
	if err != nil {
		return false, err
	}
	switch op {
	case "equals", "eq", "is":
		return have == target, nil
	case "not_equals", "neq":
		return have != target, nil
	case "greater_than", "gt":
		return have > target, nil
	case "greater_or_equal", "gte", "greater_than_or_equal":
		return have >= target, nil
	case "less_than", "lt":
		return have < target, nil
	case "less_or_equal", "lte", "less_than_or_equal":
		return have <= target, nil
	default:
		return false, fmt.Errorf("unknown numeric operator %q", op)
	}
}

func evalBool(have bool, op string, want any) (bool, error) {
	target := strings.EqualFold(fmt.Sprint(want), "true") || fmt.Sprint(want) == "1"
	switch op {
	case "equals", "eq", "is", "is_true":
		return have == target, nil
	case "not_equals", "neq", "is_false":
		return have != target, nil
	default:
		return have == target, nil
	}
}

func evalGenre(have []string, op string, want any) (bool, error) {
	wantList := stringList(want)
	haveLower := make([]string, len(have))
	for i, g := range have {
		haveLower[i] = strings.ToLower(g)
	}
	switch op {
	case "contains_any", "contains":
		for _, w := range wantList {
			wl := strings.ToLower(w)
			for _, h := range haveLower {
				if h == wl {
					return true, nil
				}
			}
		}
		return false, nil
	case "contains_all":
		for _, w := range wantList {
			wl := strings.ToLower(w)
			found := false
			for _, h := range haveLower {
				if h == wl {
					found = true
					break
				}
			}
			if !found {
				return false, nil
			}
		}
		return true, nil
	case "not_contains_any":
		ok, err := evalGenre(have, "contains_any", want)
		return !ok, err
	default:
		return false, fmt.Errorf("unknown genre operator %q", op)
	}
}

func stringList(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			out = append(out, fmt.Sprint(x))
		}
		return out
	case []string:
		return t
	default:
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" {
			return nil
		}
		parts := strings.Split(s, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	}
}

func toFloat(v any) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case float32:
		return float64(t), nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(t), 64)
	default:
		return strconv.ParseFloat(fmt.Sprint(v), 64)
	}
}

func daysSince(t time.Time) int {
	if t.IsZero() {
		return 999999
	}
	d := time.Since(t)
	if d < 0 {
		return 0
	}
	return int(d.Hours() / 24)
}
