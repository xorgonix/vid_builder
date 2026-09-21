package webapp

import (
	"io"
	"log/slog"
	"net/http"
	"testing"

	"vidbuild/internal/config"
	"vidbuild/internal/media"
	"vidbuild/internal/store"
	"vidbuild/internal/web"
)

// NewServer builds the application HTTP handler against a test store using a
// temp media root and a quiet logger. If cfg is nil, defaults are used.
func NewServer(t *testing.T, st *store.Store, cfg *config.Config) (http.Handler, *config.Config) {
	t.Helper()
	if cfg == nil {
		cfg = config.Default()
	}
	ms := media.New(t.TempDir())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := web.New(cfg, st, ms, logger)
	return srv.Handler(), cfg
}
