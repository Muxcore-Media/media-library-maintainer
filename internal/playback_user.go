package internal

import (
	"fmt"
	"strings"
)

func evalUserScopedPlayback(values map[string]float64, op string, raw any) (bool, error) {
	spec, ok := raw.(map[string]any)
	if !ok {
		return false, fmt.Errorf("user-scoped playback requires {\"usernames\":[], \"amount\":n}")
	}
	usernames, ok := spec["usernames"].([]any)
	if !ok || len(usernames) == 0 {
		return false, fmt.Errorf("user-scoped playback requires at least one username")
	}
	amount, err := toFloat(spec["amount"])
	if err != nil {
		return false, err
	}
	if values == nil {
		values = map[string]float64{}
	}
	for _, name := range usernames {
		key := normalizePlaybackUser(fmt.Sprint(name))
		if key == "" {
			continue
		}
		actual := values[key]
		ok, err := evalNumber(actual, op, amount)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func normalizePlaybackUser(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func evalPlaybackUsernames(values map[string]float64, op string, want any) (bool, error) {
	names := make([]string, 0, len(values))
	for user := range values {
		if user != "" {
			names = append(names, user)
		}
	}
	return evalGenre(names, op, want)
}
