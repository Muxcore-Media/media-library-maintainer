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
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}

// EvalContext holds one library item's facts for rule evaluation.
type EvalContext struct {
	Scope MediaScope

	ItemID   string
	Title    string
	Year     int
	TmdbID   int
	ImdbID   string
	Genres   []string
	TagIDs   []string
	Monitored bool
	HasFile  bool

	AddedAt         time.Time
	FileSizeBytes   int64
	FilePath        string
	FileQuality     string
	MovieID         string
	VoteAverage     float64
	ImdbRating      float64
	CriticScore     float64
	TraktRating     float64
	RuntimeMinutes  int
	MovieVersionCount int
	SeasonCount     int
	SeriesStatus    string
	FirstAirDate    time.Time
	LastAirDate     time.Time
	EpisodeAirDate  time.Time
	QualityProfile  string
	RootFolderPath  string
	SeriesType      string
	SeasonNumber    int
	EpisodeNumber   int
	SeriesID        string

	ViewCount           int
	DaysSinceLastWatch  int
	NeverWatched        bool
	LastWatchedAt       time.Time

	PlaybackPlayCount            int
	PlaybackUniqueUsers          int
	PlaybackTotalDurationMinutes float64
	PlaybackLongestDurationMinutes float64
	PlaybackHasActivity          bool

	AnilistScore float64
	LetterboxdScore     float64
	LetterboxdVoteCount int

	MediaContainer  string
	VideoResolution string
	VideoCodec      string
	VideoBitrateKbps float64
	VideoWidth       int
	VideoHeight      int
	AudioChannels    int
	VideoHDR         bool
	VideoBitDepth    int
	AudioCodec       string

	UserWatchedPercent         map[string]float64
	UserWatchedDurationMinutes map[string]float64

	Requested       bool
	RequestedBy     string
	DaysSinceRequest int
	RequesterWatched bool

	DiskFreePercent float64
	Protected       bool
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
	ActionDelete         ArrAction = "delete"
	ActionUnmonitor      ArrAction = "unmonitor"
	ActionUnmonitorOnly  ArrAction = "unmonitor_only"
	ActionRemoveIfEmpty         ArrAction = "remove_if_empty"
	ActionDoNothing             ArrAction = "do_nothing"
	ActionMove                  ArrAction = "move"
	ActionChangeQualityProfile  ArrAction = "change_quality_profile"
)

type CandidateStatus string

const (
	StatusPending      CandidateStatus = "pending"
	StatusLeavingSoon  CandidateStatus = "leaving_soon"
	StatusApproved     CandidateStatus = "approved"
	StatusPostponed    CandidateStatus = "postponed"
	StatusCancelled    CandidateStatus = "cancelled"
	StatusCompleted    CandidateStatus = "completed"
	StatusFailed       CandidateStatus = "failed"
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
