package internal

import (
	"context"
	"strings"

	maintainv1 "github.com/Muxcore-Media/media-library-maintainer/proto/maintainv1"
)

func (m *Module) CheckImportBlocked(ctx context.Context, req *maintainv1.CheckImportBlockedRequest) (*maintainv1.CheckImportBlockedResponse, error) {
	tmdbID := int(req.GetTmdbId())
	if tmdbID <= 0 {
		return &maintainv1.CheckImportBlockedResponse{}, nil
	}
	scope := MediaScope(strings.ToLower(strings.TrimSpace(req.GetScope())))
	if scope == "tv" {
		scope = ScopeSeries
	}
	excludedLists := m.loadExclusionTMDBSet(ctx)
	if m.isExcludedByList(tmdbID, excludedLists) {
		return &maintainv1.CheckImportBlockedResponse{Blocked: true, Reason: "exclusion list"}, nil
	}
	importExcluded := m.loadImportExclusionSet(ctx)
	if m.isImportExcluded(scope, tmdbID, importExcluded) {
		return &maintainv1.CheckImportBlockedResponse{Blocked: true, Reason: "import exclusion"}, nil
	}
	return &maintainv1.CheckImportBlockedResponse{}, nil
}
