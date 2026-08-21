package internal

import "testing"

func TestParseRulesYAML(t *testing.T) {
	raw := `rules:
  - name: Unwatched old
    scope: movie
    outcome: candidate
    action: delete
    auto_act_delay_days: 14
    definition:
      op: and
      conditions:
        - field: watch.never_watched
          operator: equals
          value: true
        - field: media.days_since_added
          operator: greater_than
          value: 90
`
	rules, err := parseRulesYAML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].GetName() != "Unwatched old" {
		t.Fatalf("got %+v", rules)
	}
	yamlOut, err := rulesToYAML(rules)
	if err != nil || yamlOut == "" {
		t.Fatalf("yaml export: %v", err)
	}
}
