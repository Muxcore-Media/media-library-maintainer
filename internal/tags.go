package internal

import (
	"context"
	"log/slog"

	mgmntv1 "github.com/Muxcore-Media/media-movies/proto/mgmntv1"
	tvmgmtv1 "github.com/Muxcore-Media/media-tvshows/proto/tvmgmtv1"
)

func (m *Module) applyCandidateTag(ctx context.Context, match candidateMatch) {
	if !match.TagEnabled || match.ArrTag == "" {
		return
	}
	tagID, err := m.ensureTag(ctx, match.Ctx.Scope, match.ArrTag)
	if err != nil {
		slog.Debug("maintainer: ensure tag", "tag", match.ArrTag, "error", err)
		return
	}
	switch match.Ctx.Scope {
	case ScopeMovie:
		if err := m.ensureMovies(ctx); err != nil {
			return
		}
		m.mu.RLock()
		mc := m.moviesClient
		m.mu.RUnlock()
		_, err = mc.SetItemTags(ctx, &mgmntv1.SetItemTagsRequest{ItemId: match.Ctx.ItemID, TagIds: []string{tagID}})
	case ScopeSeries, ScopeSeason, ScopeEpisode:
		if err := m.ensureTV(ctx); err != nil {
			return
		}
		itemID := match.Ctx.ItemID
		if match.Ctx.Scope == ScopeEpisode && match.Ctx.SeriesID != "" {
			itemID = match.Ctx.SeriesID
		}
		m.mu.RLock()
		tc := m.tvClient
		m.mu.RUnlock()
		_, err = tc.SetItemTags(ctx, &tvmgmtv1.SetItemTagsRequest{ItemId: itemID, TagIds: []string{tagID}})
	}
	if err != nil {
		slog.Debug("maintainer: set item tag", "item", match.Ctx.ItemID, "error", err)
	}
}

func (m *Module) ensureTag(ctx context.Context, scope MediaScope, label string) (string, error) {
	switch scope {
	case ScopeMovie:
		if err := m.ensureMovies(ctx); err != nil {
			return "", err
		}
		m.mu.RLock()
		mc := m.moviesClient
		m.mu.RUnlock()
		tags, err := mc.ListTags(ctx, &mgmntv1.ListTagsRequest{})
		if err != nil {
			return "", err
		}
		for _, t := range tags.GetTags() {
			if t.GetLabel() == label {
				return t.GetId(), nil
			}
		}
		resp, err := mc.CreateTag(ctx, &mgmntv1.CreateTagRequest{Label: label})
		if err != nil {
			return "", err
		}
		return resp.GetTagId(), nil
	default:
		if err := m.ensureTV(ctx); err != nil {
			return "", err
		}
		m.mu.RLock()
		tc := m.tvClient
		m.mu.RUnlock()
		tags, err := tc.ListTags(ctx, &tvmgmtv1.ListTagsRequest{})
		if err != nil {
			return "", err
		}
		for _, t := range tags.GetTags() {
			if t.GetLabel() == label {
				return t.GetId(), nil
			}
		}
		resp, err := tc.CreateTag(ctx, &tvmgmtv1.CreateTagRequest{Label: label})
		if err != nil {
			return "", err
		}
		return resp.GetTagId(), nil
	}
}
