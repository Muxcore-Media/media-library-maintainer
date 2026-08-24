package internal

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	mgmntv1 "github.com/Muxcore-Media/media-movies/proto/mgmntv1"
	tvmgmtv1 "github.com/Muxcore-Media/media-tvshows/proto/tvmgmtv1"
)

func (m *Module) moveItem(ctx context.Context, c storedCandidate) error {
	destRoot := m.getMovePath()
	if destRoot == "" {
		return fmt.Errorf("MAINTAINER_MOVE_PATH not configured")
	}
	paths, err := m.itemFilePaths(ctx, c)
	if err != nil {
		return err
	}
	for _, src := range paths {
		if src == "" {
			continue
		}
		dst := filepath.Join(destRoot, filepath.Base(src))
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return err
		}
		if err := moveFile(src, dst); err != nil {
			return err
		}
	}
	return m.removeFromLibrary(ctx, c)
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src) //nolint:gosec // source path comes from arr-managed library files
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640) //nolint:gosec // maintainer move copies with group-readable perms
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}

func (m *Module) itemFilePaths(ctx context.Context, c storedCandidate) ([]string, error) {
	switch c.Scope {
	case ScopeMovie:
		if err := m.ensureMovies(ctx); err != nil {
			return nil, err
		}
		m.mu.RLock()
		mc := m.moviesClient
		m.mu.RUnlock()
		resp, err := mc.ListFiles(ctx, &mgmntv1.ListFilesRequest{MovieId: c.ItemID})
		if err != nil {
			return nil, err
		}
		var paths []string
		for _, f := range resp.GetFiles() {
			paths = append(paths, f.GetFilePath())
		}
		return paths, nil
	case ScopeEpisode:
		return nil, fmt.Errorf("episode move requires file path in criteria")
	default:
		return nil, fmt.Errorf("move not supported for scope %s", c.Scope)
	}
}

func (m *Module) changeQualityProfile(ctx context.Context, c storedCandidate, profileID string) error {
	if profileID == "" {
		return fmt.Errorf("quality profile id required")
	}
	switch c.Scope {
	case ScopeMovie:
		if err := m.ensureMovies(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		mc := m.moviesClient
		m.mu.RUnlock()
		_, err := mc.UpdateMovie(ctx, &mgmntv1.UpdateMovieRequest{
			MovieId:          c.ItemID,
			QualityProfileId: &profileID,
		})
		return err
	case ScopeSeries:
		if err := m.ensureTV(ctx); err != nil {
			return err
		}
		m.mu.RLock()
		tc := m.tvClient
		m.mu.RUnlock()
		_, err := tc.UpdateTVShow(ctx, &tvmgmtv1.UpdateTVShowRequest{
			SeriesId:         c.ItemID,
			QualityProfileId: &profileID,
		})
		return err
	default:
		return fmt.Errorf("quality profile change not supported for scope %s", c.Scope)
	}
}
