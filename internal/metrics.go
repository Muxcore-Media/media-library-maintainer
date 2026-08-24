package internal

import (
	"context"

	maintainv1 "github.com/Muxcore-Media/media-library-maintainer/proto/maintainv1"
)

func (m *Module) GetStorageMetrics(ctx context.Context, req *maintainv1.GetStorageMetricsRequest) (*maintainv1.GetStorageMetricsResponse, error) {
	contexts, err := m.buildEvalContexts(ctx)
	if err != nil {
		return nil, err
	}
	type agg struct {
		bytes int64
		count int
	}
	byPath := make(map[string]*agg)
	for _, ec := range contexts {
		if !ec.HasFile || ec.RootFolderPath == "" {
			continue
		}
		p := ec.RootFolderPath
		if byPath[p] == nil {
			byPath[p] = &agg{}
		}
		byPath[p].bytes += ec.FileSizeBytes
		byPath[p].count++
	}
	var paths []*maintainv1.StoragePathMetric
	for path, a := range byPath {
		freePct := diskFreePercent(path)
		freeB := diskFreeBytes(path)
		totalB := int64(0)
		if freePct > 0 && freePct < 100 {
			totalB = int64(float64(freeB) / (freePct / 100))
		}
		paths = append(paths, &maintainv1.StoragePathMetric{
			Path:         path,
			FreePercent:  freePct,
			FreeBytes:    freeB,
			TotalBytes:   totalB,
			LibraryBytes: a.bytes,
			ItemCount:    int32(a.count), //nolint:gosec // per-path item counts are bounded by library size
		})
	}
	return &maintainv1.GetStorageMetricsResponse{Paths: paths}, nil
}
