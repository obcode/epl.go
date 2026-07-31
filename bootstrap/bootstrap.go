// Package bootstrap is the server entrypoint: flags, configuration, wiring, HTTP router and
// graceful shutdown. It is the only package allowed to call log.Fatal.
package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// BuildInfo carries the ldflags-injected version stamp from main.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// Serve parses flags, sets up logging and runs the HTTP server until a signal arrives.
func Serve(build BuildInfo) {
	var (
		verbose = flag.Bool("v", false, "verbose (debug) logging")
		addr    = flag.String("addr", ":8080", "listen address")
	)
	flag.Parse()

	setupLogging(*verbose)
	log.Info().
		Str("version", build.Version).
		Str("commit", build.Commit).
		Str("date", build.Date).
		Msg("tallox starting")

	srv := &http.Server{
		Addr:              *addr,
		Handler:           router(build),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Str("addr", *addr).Msg("cannot listen")
		}
	}()
	log.Info().Str("addr", *addr).Msg("listening")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Info().Msg("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("cannot shut down cleanly")
	}
}

func router(build BuildInfo) http.Handler {
	r := chi.NewRouter()

	// Liveness for the container healthcheck and the deploy workflow. Deliberately outside
	// any auth: it must answer before the database or the auth proxy are reachable.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"version": build.Version,
		})
	})

	// The two GraphQL mounts (/query behind the proxy header, /api/graphql behind bearer
	// tokens) are added here once the schema exists. Until then the paths answer 501 rather
	// than 404, so a misconfigured Caddy branch is distinguishable from a missing route.
	notReady := func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"error": "GraphQL endpoint not implemented yet",
		})
	}
	r.Handle("/query", http.HandlerFunc(notReady))
	r.Handle("/api/graphql", http.HandlerFunc(notReady))

	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Error().Err(err).Msg("cannot write response")
	}
}

func setupLogging(verbose bool) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	level := zerolog.InfoLevel
	if verbose {
		level = zerolog.DebugLevel
	}
	zerolog.SetGlobalLevel(level)
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout}).
		With().Caller().Timestamp().Logger()
}
