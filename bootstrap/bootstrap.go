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

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/obcode/tallox.go/graph"
	"github.com/obcode/tallox.go/graph/generated"
	"github.com/obcode/tallox.go/internal/buildinfo"
)

// Serve parses flags, sets up logging and runs the HTTP server until a signal arrives.
func Serve(build buildinfo.Info) {
	var (
		verbose = flag.Bool("v", false, "verbose (debug) logging")
		addr    = flag.String("addr", ":8080", "listen address")
		// The playground is a development convenience, not a public surface: in production
		// Caddy routes only /query and /api/graphql to this container, so "/" is not
		// reachable from outside anyway. Introspection is a separate matter and stays on —
		// see CLAUDE.md, the API is a product here.
		playgroundEnabled = flag.Bool("playground", true, "serve the GraphQL playground at /")
	)
	flag.Parse()

	setupLogging(*verbose)
	log.Info().
		Str("version", build.Version).
		Str("commit", build.Commit).
		Str("builtAt", build.BuiltAt).
		Msg("tallox starting")

	srv := &http.Server{
		Addr:              *addr,
		Handler:           router(build, *playgroundEnabled),
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

// Handler builds the same http.Handler that Serve mounts, without listening on a port.
//
// Exported for tests, and deliberately the *same* function Serve uses rather than a
// test-only reassembly of the routes. A harness that wires its own router proves that the
// harness is correct; this one proves that the server is. Every rule that lands in the
// middleware chain is therefore exercised by the integration tests automatically, including
// the ones nobody remembered to write a test for.
func Handler(build buildinfo.Info, playgroundEnabled bool) http.Handler {
	return router(build, playgroundEnabled)
}

func router(build buildinfo.Info, playgroundEnabled bool) http.Handler {
	r := chi.NewRouter()

	// Liveness for the container healthcheck and the deploy workflow. Deliberately outside
	// any auth: it must answer before the database or the auth proxy are reachable.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"version": build.Version,
		})
	})

	// ONE handler on both doors. /query gets the identity from the auth proxy's
	// X-Remote-User, /api/graphql from a bearer token — but they share a schema, a resolver
	// root and an AroundOperations chain, so a rule added for the browser cannot be missing
	// on the token path. The authenticating middleware differs per mount and goes on here
	// once it exists; the schema below it never does.
	gql := graphqlHandler(build)
	r.Handle("/query", gql)
	r.Handle("/api/graphql", gql)

	if playgroundEnabled {
		r.Handle("/", playground.Handler("Tallox", "/query"))
	}

	return r
}

// graphqlHandler builds the gqlgen server. Transports are listed explicitly rather than
// taken from NewDefaultServer: the default set includes websockets, and a subscription
// transport that nobody has thought about is an auth path that nobody has thought about.
func graphqlHandler(build buildinfo.Info) http.Handler {
	srv := handler.New(generated.NewExecutableSchema(generated.Config{
		Resolvers: &graph.Resolver{Build: build},
	}))

	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})

	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000))

	// Introspection stays on, in production too. The API is a product: it is what makes
	// editor completion, codegen and schema exploration work for colleagues writing their
	// own evaluations against a Personal Access Token.
	srv.Use(extension.Introspection{})
	srv.Use(extension.AutomaticPersistedQuery{Cache: lru.New[string](100)})

	return srv
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
