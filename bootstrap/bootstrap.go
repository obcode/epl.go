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
	"github.com/obcode/tallox.go/internal/auth"
	"github.com/obcode/tallox.go/internal/buildinfo"
	"github.com/obcode/tallox.go/internal/store"
)

// EnvDatabaseURL is where the connection string comes from.
//
// The environment rather than the configuration file, because it is the one value that
// differs between the DevContainer, CI and the host, and because it is the value a deploy
// script sets. Secrets that are not per-environment belong in tallox.yaml.
const EnvDatabaseURL = "TALLOX_DB_URL"

// Options is everything Handler needs. A struct rather than a parameter list: this grows with
// every subsystem, and a positional bool that means "playground" in one call site and
// something else in the next is a bug waiting for a hurried afternoon.
type Options struct {
	// Build is the version stamp, served by /healthz and by the buildInfo query.
	Build buildinfo.Info
	// Playground enables the GraphQL playground at "/".
	Playground bool
	// Auth configures both doors: the mode, and the two lookups.
	Auth auth.Config
}

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
		// The default is production. A server that falls back to dev mode when nobody
		// configured it is a server that hands out an administrator on the day somebody
		// forgets a flag.
		authMode = flag.String("auth-mode", string(auth.ModeProxy),
			"how to authenticate: dev | proxy | off-token")
		devUser = flag.String("auth-dev-user", auth.DefaultDevUser,
			"mail address of the injected development user (auth-mode=dev only)")
	)
	flag.Parse()

	setupLogging(*verbose)
	log.Info().
		Str("version", build.Version).
		Str("commit", build.Commit).
		Str("builtAt", build.BuiltAt).
		Msg("tallox starting")

	mode, err := auth.ParseMode(*authMode)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot start")
	}

	dsn := os.Getenv(EnvDatabaseURL)
	if dsn == "" {
		log.Fatal().Str("variable", EnvDatabaseURL).
			Msg("no database url — the server cannot authenticate anybody without one")
	}

	ctx := context.Background()

	// Migrate before opening the pool that serves requests. Embedded migrations plus "apply at
	// startup" means a container that has the binary has the schema, by construction: there is
	// no deploy step that can copy one and forget the other.
	applied, err := store.MigrateUpDSN(ctx, dsn)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot migrate the database")
	}
	log.Info().Int("applied", applied).Msg("migrations up to date")

	pool, err := store.Open(ctx, dsn)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot reach the database")
	}
	defer pool.Close()

	directory := store.NewDirectory(pool)

	srv := &http.Server{
		Addr: *addr,
		Handler: Handler(Options{
			Build:      build,
			Playground: *playgroundEnabled,
			Auth: auth.Config{
				Mode:    mode,
				Users:   directory,
				Tokens:  directory,
				DevUser: *devUser,
			},
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Str("addr", *addr).Msg("cannot listen")
		}
	}()
	log.Info().Str("addr", *addr).Str("authMode", string(mode)).Msg("listening")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Info().Msg("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
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
func Handler(opts Options) http.Handler {
	return router(opts)
}

func router(opts Options) http.Handler {
	r := chi.NewRouter()

	// Liveness for the container healthcheck and the deploy workflow. Deliberately outside
	// any auth: it must answer before the database or the auth proxy are reachable.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"version": opts.Build.Version,
		})
	})

	// ONE handler on both doors. /query gets the identity from the auth proxy's
	// X-Remote-User, /api/graphql from a bearer token — but they share a schema, a resolver
	// root and an AroundOperations chain, so a rule added for the browser cannot be missing
	// on the token path. What differs is the authenticating middleware, and only that.
	gql := graphqlHandler(opts.Build)

	r.With(auth.Middleware(auth.NewProxyAuthenticator(opts.Auth))).Handle("/query", gql)

	if opts.Auth.Mode.TokenDoorEnabled() {
		r.With(auth.Middleware(auth.NewTokenAuthenticator(opts.Auth))).Handle("/api/graphql", gql)
	} else {
		// Not mounted at all rather than mounted and refusing. The emergency stop has to
		// leave no code path that could be wrong about whether it is engaged, and a 404 is
		// also the honest answer: on this instance, that door does not exist.
		log.Warn().Msg("auth.mode=off-token: /api/graphql is not served")
	}

	if opts.Playground {
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
