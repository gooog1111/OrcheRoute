package updater

import (
	"context"
	"time"

	"github.com/gooog1111/orcheroute/internal/core/qualification"
)

type PipelineQualifier struct {
	Backend  qualification.Backend
	Now      func() time.Time
	Progress func(pool, stage string, current, total int)
}

func (qualifier PipelineQualifier) Qualify(ctx context.Context, pool string, proxies []map[string]any, settings map[string]any, sources map[string]qualification.Source) (qualification.Result, error) {
	if backend, ok := qualifier.Backend.(interface{ SetProgress(func(string, int, int)) }); ok {
		if qualifier.Progress == nil {
			backend.SetProgress(nil)
		} else {
			backend.SetProgress(func(stage string, current, total int) {
				qualifier.Progress(pool, stage, current, total)
			})
		}
		defer backend.SetProgress(nil)
	}
	return qualification.Qualify(ctx, pool, proxies, settings, sources, qualifier.Backend, qualifier.Now)
}
