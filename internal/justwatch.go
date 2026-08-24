package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
)

type justWatchPolicy struct {
	ID        string
	Name      string
	Country   string
	Language  string
	Mode      string // available_on | not_available_on
	Providers []string
}

const justWatchGraphQLQuery = `
query GetSearchTitles($country: Country!, $language: Language!, $first: Int!, $searchTitlesFilter: TitleFilter) {
  popularTitles(country: $country, first: $first, filter: $searchTitlesFilter) {
    edges {
      node {
        content(country: $country, language: $language) {
          title
          originalReleaseYear
          offers {
            monetizationType
            package {
              technicalName
            }
          }
        }
        objectType
      }
    }
  }
}`

type justWatchClient struct {
	country  string
	language string
	baseURL  string
	cli      *http.Client
	cache    sync.Map
}

func newJustWatchClient(country, language string, cli *http.Client) *justWatchClient {
	if country == "" {
		country = "US"
	}
	if language == "" {
		language = "en"
	}
	url := os.Getenv("JUSTWATCH_API_URL")
	if url == "" {
		url = "https://apis.justwatch.com/graphql"
	}
	return &justWatchClient{country: country, language: language, baseURL: url, cli: cli}
}

func (m *Module) loadJustWatchPolicies(ctx context.Context) []justWatchPolicy {
	m.mu.RLock()
	db := m.db
	m.mu.RUnlock()
	if db == nil {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id, name, list_url FROM exclusion_lists WHERE type = 'justwatch'`)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var out []justWatchPolicy
	for rows.Next() {
		var id, name, raw string
		if err := rows.Scan(&id, &name, &raw); err != nil {
			continue
		}
		p, err := parseJustWatchPolicy(id, name, raw)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

func parseJustWatchPolicy(id, name, raw string) (justWatchPolicy, error) {
	var cfg struct {
		Country   string   `json:"country"`
		Language  string   `json:"language"`
		Mode      string   `json:"mode"`
		Providers []string `json:"providers"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return justWatchPolicy{}, err
	}
	if cfg.Mode == "" {
		cfg.Mode = "available_on"
	}
	if len(cfg.Providers) == 0 {
		return justWatchPolicy{}, fmt.Errorf("providers required")
	}
	return justWatchPolicy{
		ID: id, Name: name,
		Country: cfg.Country, Language: cfg.Language,
		Mode: cfg.Mode, Providers: cfg.Providers,
	}, nil
}

func (m *Module) isExcludedByJustWatch(ctx context.Context, ec EvalContext, policies []justWatchPolicy) bool {
	if len(policies) == 0 {
		return false
	}
	mediaType := "movie"
	if ec.Scope == ScopeSeries || ec.Scope == ScopeEpisode || ec.Scope == ScopeSeason {
		mediaType = "show"
	}
	for _, p := range policies {
		jw := newJustWatchClient(p.Country, p.Language, m.httpCli)
		available := jw.availableOn(ctx, ec.Title, ec.Year, mediaType, p.Providers)
		switch p.Mode {
		case "not_available_on":
			if !available {
				return true
			}
		default:
			if available {
				return true
			}
		}
	}
	return false
}

func (jw *justWatchClient) availableOn(ctx context.Context, title string, year int, mediaType string, providers []string) bool {
	entry, err := jw.searchByTitleAndYear(ctx, title, year, mediaType)
	if err != nil || entry == nil {
		return false
	}
	if len(entry.Offers) == 0 {
		return false
	}
	providersLower := make([]string, len(providers))
	for i, p := range providers {
		providersLower[i] = strings.ToLower(strings.TrimSpace(p))
	}
	for _, p := range providersLower {
		if p == "any" {
			return true
		}
	}
	for _, offer := range entry.Offers {
		name := strings.ToLower(strings.TrimSpace(offer.Package.TechnicalName))
		if name == "" {
			continue
		}
		for _, p := range providersLower {
			if p == name {
				return true
			}
		}
	}
	return false
}

type jwSearchEntry struct {
	Title  string
	Offers []struct {
		Package struct {
			TechnicalName string `json:"technicalName"`
		} `json:"package"`
	} `json:"offers"`
	Year int
}

func (jw *justWatchClient) searchByTitleAndYear(ctx context.Context, title string, year int, mediaType string) (*jwSearchEntry, error) {
	key := strings.ToLower(title) + fmt.Sprintf(":%d:%s:%s:%s", year, mediaType, jw.country, jw.language)
	if v, ok := jw.cache.Load(key); ok {
		if e, ok := v.(jwSearchEntry); ok && e.Title == "" && len(e.Offers) == 0 {
			return nil, nil
		}
		if e, ok := v.(jwSearchEntry); ok {
			return &e, nil
		}
	}
	body := map[string]any{
		"operationName": "GetSearchTitles",
		"variables": map[string]any{
			"country":  jw.country,
			"language": jw.language,
			"first":    5,
			"searchTitlesFilter": map[string]any{
				"searchQuery": title,
			},
		},
		"query": justWatchGraphQLQuery,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, jw.baseURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; MuxCore-Maintainer/1.0)")
	resp, err := jw.cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("justwatch status %d", resp.StatusCode)
	}
	entry := parseJustWatchSearchResponse(respBody, title, year, mediaType)
	if entry == nil {
		jw.cache.Store(key, jwSearchEntry{})
		return nil, nil
	}
	jw.cache.Store(key, *entry)
	return entry, nil
}

func parseJustWatchSearchResponse(body []byte, title string, year int, mediaType string) *jwSearchEntry {
	var parsed struct {
		Data struct {
			PopularTitles struct {
				Edges []struct {
					Node struct {
						ObjectType string `json:"objectType"`
						Content    struct {
							Title  string `json:"title"`
							Offers []struct {
								Package struct {
									TechnicalName string `json:"technicalName"`
								} `json:"package"`
							} `json:"offers"`
							OriginalReleaseYear int `json:"originalReleaseYear"`
						} `json:"content"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"popularTitles"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	wantType := "MOVIE"
	if mediaType == "show" {
		wantType = "SHOW"
	}
	titleLower := strings.ToLower(strings.TrimSpace(title))
	for _, edge := range parsed.Data.PopularTitles.Edges {
		node := edge.Node
		if wantType != "" && !strings.EqualFold(node.ObjectType, wantType) {
			continue
		}
		c := node.Content
		if strings.ToLower(strings.TrimSpace(c.Title)) != titleLower {
			continue
		}
		if year > 0 && c.OriginalReleaseYear > 0 {
			diff := c.OriginalReleaseYear - year
			if diff < 0 {
				diff = -diff
			}
			if diff > 1 {
				continue
			}
		}
		return &jwSearchEntry{
			Title:  c.Title,
			Year:   c.OriginalReleaseYear,
			Offers: c.Offers,
		}
	}
	return nil
}
