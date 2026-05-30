// Package server wires HTTP routing, middleware, and handlers together.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/Adyn-Cox/CoorsHeavy/internal/auth"
	"github.com/Adyn-Cox/CoorsHeavy/internal/config"
	"github.com/Adyn-Cox/CoorsHeavy/internal/store"
)

// Server holds shared dependencies for the HTTP layer.
type Server struct {
	cfg      config.Config
	log      *slog.Logger
	authn    *auth.Authenticator
	handlers *Handlers
}

// New constructs a Server with its dependencies injected.
func New(cfg config.Config, log *slog.Logger, st store.Store, authn *auth.Authenticator) *Server {
	return &Server{
		cfg:      cfg,
		log:      log,
		authn:    authn,
		handlers: &Handlers{store: st, authn: authn},
	}
}

func (s *Server) httpServer() *http.Server {
	return &http.Server{
		Addr:         ":" + s.cfg.Port,
		Handler:      s.routes(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}

// Run starts the HTTP server and blocks until ctx is cancelled, then shuts down
// gracefully (draining in-flight requests up to a 10s deadline).
func (s *Server) Run(ctx context.Context) error {
	srv := s.httpServer()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	s.log.Info("server listening", "addr", srv.Addr, "env", s.cfg.Env)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
