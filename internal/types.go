package internal

import (
	"encoding/json"
	"time"
)

// RuleDefinition is a nested AND/OR rule tree stored as JSON.
type RuleDefinition struct {
	Op         string           `json:"op"` // "and" | "or"
	Conditions []RuleCondition  `json:"conditions,omitempty"`
	Groups     []RuleDefinition `json:"groups,omitempty"`
}

type RuleCondition struct {
	Value    any    `json:"value"`
	Field    string `json:"field"`
	Operator string `json:"operator"`
}

// EvalContext holds one library item's facts for rule evaluation.
type EvalContext struct {
	EpisodeAirDate                 time.Time
	LastAirDate                    time.Time
	FirstAirDate                   time.Time
	LastWatchedAt                  time.Time
	AddedAt                        time.Time
	UserWatchedPercent             map[string]float64
	UserWatchedDurationMinutes     map[string]float64
	FilePath                       string
	VideoResolution                string
	AudioCodec                     string
	ItemID                         string
	VideoCodec                     string
	QualityProfile                 string
	FileQuality                    string
	MovieID                        string
	Scope                          MediaScope
	Title                          string
	MediaContainer                 string
	ImdbID                         string
	SeriesID                       string
	SeriesType                     string
	RootFolderPath                 string
	SeriesStatus                   string
	RequestedBy                    string
	TagIDs                         []string
	Genres                         []string
	VoteAverage                    float64
	LetterboxdVoteCount            int
	MovieVersionCount              int
	SeasonNumber                   int
	EpisodeNumber                  int
	RuntimeMinutes                 int
	ViewCount                      int
	DaysSinceLastWatch             int
	DiskFreePercent                float64
	TraktRating                    float64
	PlaybackPlayCount              int
	PlaybackUniqueUsers            int
	PlaybackTotalDurationMinutes   float64
	PlaybackLongestDurationMinutes float64
	DaysSinceRequest               int
	AnilistScore                   float64
	LetterboxdScore                float64
	SeasonCount                    int
	CriticScore                    float64
	ImdbRating                     float64
	FileSizeBytes                  int64
	VideoBitrateKbps               float64
	VideoWidth                     int
	VideoHeight                    int
	AudioChannels                  int
	Year                           int
	VideoBitDepth                  int
	TmdbID                         int
	Monitored                      bool
	HasFile                        bool
	Requested                      bool
	VideoHDR                       bool
	PlaybackHasActivity            bool
	RequesterWatched               bool
	NeverWatched                   bool
	Protected                      bool
}

type MediaScope string

const (
	ScopeMovie     MediaScope = "movie"
	ScopeMovieFile MediaScope = "movie_file"
	ScopeSeries    MediaScope = "series"
	ScopeSeason    MediaScope = "season"
	ScopeEpisode   MediaScope = "episode"
)

type RuleOutcome string

const (
	OutcomeCandidate RuleOutcome = "candidate"
	OutcomeProtect   RuleOutcome = "protect"
)

type ArrAction string

const (
	ActionDelete               ArrAction = "delete"
	ActionUnmonitor            ArrAction = "unmonitor"
	ActionUnmonitorOnly        ArrAction = "unmonitor_only"
	ActionRemoveIfEmpty        ArrAction = "remove_if_empty"
	ActionDoNothing            ArrAction = "do_nothing"
	ActionMove                 ArrAction = "move"
	ActionChangeQualityProfile ArrAction = "change_quality_profile"
)

type CandidateStatus string

const (
	StatusPending     CandidateStatus = "pending"
	StatusLeavingSoon CandidateStatus = "leaving_soon"
	StatusApproved    CandidateStatus = "approved"
	StatusPostponed   CandidateStatus = "postponed"
	StatusCancelled   CandidateStatus = "cancelled"
	StatusCompleted   CandidateStatus = "completed"
	StatusFailed      CandidateStatus = "failed"
)

func parseRuleDefinition(raw string) (RuleDefinition, error) {
	if raw == "" {
		return RuleDefinition{Op: "and"}, nil
	}
	var def RuleDefinition
	if err := json.Unmarshal([]byte(raw), &def); err != nil {
		return RuleDefinition{}, err
	}
	if def.Op == "" {
		def.Op = "and"
	}
	return def, nil
}

func mergeArrAction(current, next ArrAction) ArrAction {
	rank := map[ArrAction]int{
		ActionDoNothing:            0,
		ActionChangeQualityProfile: 1,
		ActionUnmonitorOnly:        2,
		ActionUnmonitor:            3,
		ActionMove:                 3,
		ActionRemoveIfEmpty:        4,
		ActionDelete:               5,
	}
	if rank[next] > rank[current] {
		return next
	}
	return current
}
