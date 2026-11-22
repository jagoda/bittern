// Command bitternd serves the read-only Bittern status API on loopback.
//
// This daemon build loads and validates appliance.yaml, renders it in memory,
// probes Frigate, storage, and the two interfaces, drives the setup wizard, and
// discovers cameras on cam0. It does not update or reset the appliance.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/auth"
	"bittern.invalid/agent/internal/discovery"
	"bittern.invalid/agent/internal/logging"
	"bittern.invalid/agent/internal/redact"
	"bittern.invalid/agent/internal/status"
	"bittern.invalid/agent/internal/tlsutil"
	bitternui "bittern.invalid/agent/internal/ui"
)

const (
	defaultListen          = "127.0.0.1:8080"
	defaultUIListen        = "0.0.0.0:443"
	defaultConfigPath      = "/etc/bittern/appliance.yaml"
	defaultCertificatePath = "/etc/bittern/tls/server.crt"
	defaultSecretsPath     = "/etc/bittern/secrets.yaml"
	defaultAuthPath        = "/var/lib/bittern/auth.json"
	statusTimeout          = 6 * time.Second
)

// version is set at build time with -ldflags="-X main.version=...".
var version = "dev"

// factoryBaseline is set only by the preparation kit build. A kit baseline is
// generation 0 and is not a published bundle, so it must be reported
// distinctly even when it was built from the same commit as generation 1.
var factoryBaseline = "false"

func isFactoryBaseline() bool {
	return factoryBaseline == "true"
}

// mockApplyApplier lets the browser check finish the wizard's apply page in the
// preview build. It writes nothing; the real applier is never selected with
// --ui-mock. The short pause and the synthetic image step let the browser check
// see the waiting-for-the-recording-software state before the result page.
type mockApplyApplier struct{}

const mockApplyDelay = 2 * time.Second

func (a mockApplyApplier) Apply(ctx context.Context, request bitternui.ApplyRequest) error {
	_, err := a.ApplyWithProgress(ctx, request, nil)
	return err
}

func (mockApplyApplier) ApplyWithProgress(ctx context.Context, _ bitternui.ApplyRequest, progress func(apply.StepProgress)) (apply.Result, error) {
	step := apply.Step{Key: "image", Kind: "image"}
	if progress != nil {
		progress(apply.StepProgress{Phase: "apply", Index: 1, Total: 9, Completed: 1, Step: step})
	}
	select {
	case <-time.After(mockApplyDelay):
		return apply.Result{Status: apply.StatusApplied, Generation: 1}, nil
	case <-ctx.Done():
		return apply.Result{Status: apply.StatusInterrupted}, ctx.Err()
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "bitternd:", err)
		os.Exit(1)
	}
}

func run() error {
	flags := flag.NewFlagSet("bitternd", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	listen := flags.String("listen", defaultListen, "loopback address for the status API")
	configPath := flags.String("config", defaultConfigPath, "appliance.yaml path to load and validate")
	uiEnabled := flags.Bool("ui", false, "serve the first-run wizard and status pages")
	uiMock := flags.Bool("ui-mock", false, "serve mock status states and setup checks (requires --ui)")
	uiMockApply := flags.Bool("ui-mock-apply", false, "let --ui-mock complete an apply without writing files")
	uiListen := flags.String("ui-listen", defaultUIListen, "address for the TLS UI listener")
	uiInsecureHTTP := flags.Bool("ui-insecure-http", false, "serve the UI over plain HTTP; local development only")
	uiCertificate := flags.String("ui-certificate", defaultCertificatePath, "TLS certificate for the UI")
	uiSecrets := flags.String("ui-secrets", defaultSecretsPath, "secrets.yaml holding tls.private_key_pem")
	uiAuthState := flags.String("ui-auth-state", defaultAuthPath, "authentication state file for the administrator password")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *uiMock && !*uiEnabled {
		return fmt.Errorf("--ui-mock requires --ui")
	}
	if *uiMockApply && !*uiMock {
		return fmt.Errorf("--ui-mock-apply requires --ui-mock")
	}
	if *uiInsecureHTTP && !*uiEnabled {
		return fmt.Errorf("--ui-insecure-http requires --ui")
	}
	if err := validateListen(*listen); err != nil {
		return err
	}
	if *uiEnabled {
		if err := validateUIListen(*uiListen); err != nil {
			return err
		}
		if *uiInsecureHTTP && !listenIsLoopback(*uiListen) {
			return fmt.Errorf("--ui-insecure-http is only allowed on a loopback --ui-listen address")
		}
	}

	redactor := redact.New()
	if data, err := os.ReadFile(*uiSecrets); err == nil {
		redactor.Add(redact.ValuesFromYAML(data)...)
	}
	logger := logging.New(os.Stderr, redactor)
	builder := status.New(*configPath, status.Options{
		Probes:          status.DefaultProbes(),
		Redactor:        redactor,
		BaselineVersion: version,
	})
	var uiHandler http.Handler
	var tlsMaterial tlsutil.Material
	if *uiEnabled {
		if !*uiInsecureHTTP {
			material, err := tlsutil.Load(*uiCertificate, *uiSecrets)
			if err != nil {
				return fmt.Errorf("UI TLS is not ready: %w", err)
			}
			tlsMaterial = material
		}
		authStore, err := auth.Open(auth.Options{
			SecretsPath: *uiSecrets,
			StatePath:   *uiAuthState,
		})
		if err != nil {
			return err
		}
		statusProvider := bitternui.StatusProvider(bitternui.BuilderStatusProvider{Builder: builder})
		checksProvider := bitternui.ChecksProvider(bitternui.AgentChecks{Builder: builder})
		applier := bitternui.Applier(bitternui.AgentApplier{SecretsPath: *uiSecrets})
		cameraDiscovery := bitternui.CameraDiscovery(discovery.NewService(discovery.Options{
			Interface:  discovery.DefaultInterface,
			ConfigPath: *configPath,
		}))
		cameraSnapshot := bitternui.SnapshotChecker(bitternui.FrigateSnapshotChecker{})
		if *uiMock {
			statusProvider = bitternui.MockStatusProvider{Now: time.Now}
			checksProvider = bitternui.MockChecks{}
			if *uiMockApply {
				applier = mockApplyApplier{}
			} else {
				applier = bitternui.StubApplier{}
			}
			cameraDiscovery = bitternui.MockDiscovery{}
			cameraSnapshot = bitternui.MockSnapshotChecker{}
		}
		handler, err := bitternui.NewHandler(bitternui.Options{
			Status:                 statusProvider,
			Checks:                 checksProvider,
			Applier:                applier,
			Discovery:              cameraDiscovery,
			Snapshot:               cameraSnapshot,
			ConfigPath:             *configPath,
			StateRoot:              filepath.Dir(*uiAuthState),
			Now:                    time.Now,
			MockPreview:            *uiMock,
			Auth:                   authStore,
			CertificateFingerprint: tlsMaterial.Fingerprint,
		})
		if err != nil {
			return err
		}
		uiHandler = handler
	}

	type runningServer struct {
		server *http.Server
		tls    bool
	}
	servers := []runningServer{{
		server: newHTTPServer(*listen, newAPIHandler(builder, logger, version)),
	}}
	if uiHandler != nil {
		uiServer := newHTTPServer(*uiListen, newUIHandler(uiHandler, logger))
		// A first apply can pull the pinned Frigate image and wait for the
		// container health gate. Keep the response connection open for that
		// transaction instead of the short API timeout.
		uiServer.WriteTimeout = 20 * time.Minute
		if !*uiInsecureHTTP {
			uiServer.TLSConfig = tlsMaterial.TLSConfig()
		}
		servers = append(servers, runningServer{server: uiServer, tls: !*uiInsecureHTTP})
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startAttributes := []any{
		"listen", *listen,
		"config", *configPath,
		"version", version,
	}
	if uiHandler != nil {
		startAttributes = append(startAttributes,
			"ui_listen", *uiListen,
			"ui_tls", !*uiInsecureHTTP,
			"ui_certificate", *uiCertificate,
			"ui_certificate_fingerprint", tlsMaterial.Fingerprint,
		)
	}
	logger.Info("daemon.start", startAttributes...)

	serverErrors := make(chan error, len(servers))
	for _, entry := range servers {
		entry := entry
		go func() {
			if entry.tls {
				serverErrors <- entry.server.ListenAndServeTLS("", "")
				return
			}
			serverErrors <- entry.server.ListenAndServe()
		}()
	}

	var serveErr error
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("daemon.failed", "error", err)
			serveErr = err
		}
	case <-ctx.Done():
		logger.Info("daemon.stopping")
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var shutdownErr error
	for _, entry := range servers {
		if err := entry.server.Shutdown(shutdownContext); err != nil {
			logger.Error("daemon.shutdown_failed", "error", err)
			shutdownErr = err
		}
	}
	if serveErr != nil {
		return serveErr
	}
	if shutdownErr != nil {
		return shutdownErr
	}
	logger.Info("daemon.stopped")
	return nil
}

func newHandler(builder *status.Builder, logger *slog.Logger, version string, uiHandlers ...http.Handler) http.Handler {
	mux := newStatusMux(builder, version)
	for _, uiHandler := range uiHandlers {
		if uiHandler == nil {
			continue
		}
		mux.Handle("/ui/", uiHandler)
		mux.Handle("GET /{$}", http.RedirectHandler(bitternui.BasePath+"/", http.StatusSeeOther))
	}
	return withLogging(mux, logger)
}

func newStatusMux(builder *status.Builder, version string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), statusTimeout)
		defer cancel()
		document := builder.Build(ctx)
		writeJSON(w, http.StatusOK, document)
	})
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, struct {
			Version      string `json:"version"`
			StatusSchema int    `json:"status_schema"`
			Baseline     bool   `json:"baseline"`
		}{
			Version:      version,
			StatusSchema: status.SchemaVersion,
			Baseline:     isFactoryBaseline(),
		})
	})
	return mux
}

func newAPIHandler(builder *status.Builder, logger *slog.Logger, version string) http.Handler {
	return withLogging(newStatusMux(builder, version), logger)
}

func newUIHandler(uiHandler http.Handler, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/ui/", uiHandler)
	mux.Handle("GET /{$}", http.RedirectHandler(bitternui.BasePath+"/", http.StatusSeeOther))
	return withLogging(mux, logger)
}

func newHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(statusCode)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	if r.status == 0 {
		r.status = statusCode
	}
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(data)
}

func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func withLogging(next http.Handler, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		if recorder.status == 0 {
			recorder.status = http.StatusOK
		}
		logger.Info("http.request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(started).Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}

func validateUIListen(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid --ui-listen address %q: %w", address, err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 0 || portNumber > 65535 {
		return fmt.Errorf("--ui-listen %q has an invalid port", address)
	}
	if host == "" || host == "localhost" {
		return nil
	}
	if net.ParseIP(host) == nil {
		return fmt.Errorf("--ui-listen %q must name an IP address, localhost, or every interface", address)
	}
	return nil
}

func listenIsLoopback(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateListen(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid --listen address %q: %w", address, err)
	}
	if host == "" {
		return fmt.Errorf("--listen %q must name a loopback address, not every interface", address)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("--listen %q has an invalid port", address)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("--listen %q is not loopback; bitternd serves 127.0.0.1 or ::1 only", address)
	}
	return nil
}
