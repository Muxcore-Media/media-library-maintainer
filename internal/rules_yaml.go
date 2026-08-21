package internal

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	maintainv1 "github.com/Muxcore-Media/media-library-maintainer/proto/maintainv1"
)

type yamlRulesFile struct {
	Rules []yamlRule `yaml:"rules"`
}

type yamlRule struct {
	Name             string         `yaml:"name"`
	Enabled          *bool          `yaml:"enabled"`
	Scope            string         `yaml:"scope"`
	CollectionID     string         `yaml:"collection_id"`
	Outcome          string         `yaml:"outcome"`
	Action           string         `yaml:"action"`
	ArrAction        string         `yaml:"arr_action"`
	AutoActEnabled   *bool          `yaml:"auto_act_enabled"`
	AutoActDelayDays int            `yaml:"auto_act_delay_days"`
	TagEnabled       *bool          `yaml:"tag_enabled"`
	ArrTag           string         `yaml:"arr_tag"`
	MaxActionsPerRun int            `yaml:"max_actions_per_run"`
	QualityProfileID string         `yaml:"quality_profile_id"`
	Definition       RuleDefinition `yaml:"definition"`
	DefinitionJSON   string         `yaml:"definition_json"`
}

func parseRulesYAML(raw string) ([]*maintainv1.RuleGroup, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty rules_yaml")
	}
	var file yamlRulesFile
	if err := yaml.Unmarshal([]byte(raw), &file); err != nil {
		return nil, fmt.Errorf("invalid yaml: %w", err)
	}
	if len(file.Rules) == 0 {
		return nil, fmt.Errorf("no rules in yaml")
	}
	var out []*maintainv1.RuleGroup
	for _, yr := range file.Rules {
		r, err := yamlRuleToProto(yr)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func yamlRuleToProto(yr yamlRule) (*maintainv1.RuleGroup, error) {
	if strings.TrimSpace(yr.Name) == "" {
		return nil, fmt.Errorf("rule name required")
	}
	defJSON := strings.TrimSpace(yr.DefinitionJSON)
	if defJSON == "" {
		if yr.Definition.Op == "" && len(yr.Definition.Conditions) == 0 && len(yr.Definition.Groups) == 0 {
			return nil, fmt.Errorf("rule %q missing definition", yr.Name)
		}
		b, err := json.Marshal(yr.Definition)
		if err != nil {
			return nil, err
		}
		defJSON = string(b)
	}
	action := yr.Action
	if action == "" {
		action = yr.ArrAction
	}
	enabled := true
	if yr.Enabled != nil {
		enabled = *yr.Enabled
	}
	autoAct := false
	if yr.AutoActEnabled != nil {
		autoAct = *yr.AutoActEnabled
	}
	tagEn := false
	if yr.TagEnabled != nil {
		tagEn = *yr.TagEnabled
	}
	maxAct := yr.MaxActionsPerRun
	if maxAct <= 0 {
		maxAct = 50
	}
	return &maintainv1.RuleGroup{
		Name:             yr.Name,
		Enabled:          enabled,
		Scope:            yamlScope(yr.Scope),
		CollectionId:     yr.CollectionID,
		DefinitionJson:   defJSON,
		Outcome:          yamlOutcome(yr.Outcome),
		ArrAction:        yamlAction(action),
		AutoActEnabled:   autoAct,
		AutoActDelayDays: int32(yr.AutoActDelayDays),
		TagEnabled:       tagEn,
		ArrTag:           yr.ArrTag,
		MaxActionsPerRun: int32(maxAct),
		QualityProfileId: yr.QualityProfileID,
	}, nil
}

func rulesToYAML(rules []*maintainv1.RuleGroup) (string, error) {
	var items []yamlRule
	for _, r := range rules {
		if r == nil {
			continue
		}
		def, _ := parseRuleDefinition(r.GetDefinitionJson())
		items = append(items, yamlRule{
			Name:             r.GetName(),
			Enabled:          boolPtr(r.GetEnabled()),
			Scope:            protoScopeString(r.GetScope()),
			CollectionID:     r.GetCollectionId(),
			Outcome:          strings.ToLower(strings.TrimPrefix(r.GetOutcome().String(), "RULE_OUTCOME_")),
			Action:           strings.ToLower(strings.TrimPrefix(r.GetArrAction().String(), "ARR_ACTION_")),
			AutoActEnabled:   boolPtr(r.GetAutoActEnabled()),
			AutoActDelayDays: int(r.GetAutoActDelayDays()),
			TagEnabled:       boolPtr(r.GetTagEnabled()),
			ArrTag:           r.GetArrTag(),
			MaxActionsPerRun: int(r.GetMaxActionsPerRun()),
			QualityProfileID: r.GetQualityProfileId(),
			Definition:       def,
		})
	}
	raw, err := yaml.Marshal(yamlRulesFile{Rules: items})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func yamlScope(s string) maintainv1.MediaScope {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "movie", "movies":
		return maintainv1.MediaScope_MEDIA_SCOPE_MOVIE
	case "series", "show", "tv":
		return maintainv1.MediaScope_MEDIA_SCOPE_SERIES
	case "season":
		return maintainv1.MediaScope_MEDIA_SCOPE_SEASON
	case "episode":
		return maintainv1.MediaScope_MEDIA_SCOPE_EPISODE
	case "movie_file", "moviefile", "movie-version", "movie_version":
		return maintainv1.MediaScope_MEDIA_SCOPE_MOVIE_FILE
	default:
		return maintainv1.MediaScope_MEDIA_SCOPE_UNSPECIFIED
	}
}

func protoScopeString(s maintainv1.MediaScope) string {
	switch s {
	case maintainv1.MediaScope_MEDIA_SCOPE_MOVIE:
		return "movie"
	case maintainv1.MediaScope_MEDIA_SCOPE_SERIES:
		return "series"
	case maintainv1.MediaScope_MEDIA_SCOPE_SEASON:
		return "season"
	case maintainv1.MediaScope_MEDIA_SCOPE_EPISODE:
		return "episode"
	case maintainv1.MediaScope_MEDIA_SCOPE_MOVIE_FILE:
		return "movie_file"
	default:
		return ""
	}
}

func yamlOutcome(s string) maintainv1.RuleOutcome {
	if strings.EqualFold(s, "protect") {
		return maintainv1.RuleOutcome_RULE_OUTCOME_PROTECT
	}
	return maintainv1.RuleOutcome_RULE_OUTCOME_CANDIDATE
}

func yamlAction(s string) maintainv1.ArrAction {
	s = strings.ToUpper(strings.TrimSpace(s))
	if v, ok := maintainv1.ArrAction_value["ARR_ACTION_"+s]; ok {
		return maintainv1.ArrAction(v)
	}
	return maintainv1.ArrAction_ARR_ACTION_DELETE
}

func boolPtr(v bool) *bool { return &v }
