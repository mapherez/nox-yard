package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mapherez/nox-yard/internal/application"
	"github.com/mapherez/nox-yard/internal/auth"
	"github.com/mapherez/nox-yard/internal/httpapi"
	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/lifecycle"
	"github.com/mapherez/nox-yard/internal/managed"
	"github.com/mapherez/nox-yard/internal/mcpapi"
	"github.com/mapherez/nox-yard/internal/selfupdate"
	"github.com/mapherez/nox-yard/internal/store"
	"golang.org/x/term"
)

var buildSHA = ""
var buildVersion = ""

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if len(os.Args) == 3 && os.Args[1] == "restart-worker" {
		return lifecycle.RunRestartWorker(os.Args[2])
	}
	dataDir := environment("NOX_DATA_DIR", "./data")
	data, err := store.Open(dataDir)
	if err != nil {
		return err
	}
	defer data.Close()
	if err := data.PruneSessions(); err != nil {
		return err
	}

	if len(os.Args) > 1 {
		if len(os.Args) == 4 && os.Args[1] == "restart-worker" {
			return lifecycle.RunDurableRestartWorker(data, os.Args[2], os.Args[3])
		}
		if len(os.Args) == 4 && os.Args[1] == "managed-worker" {
			return managed.RunWorker(data, os.Args[2], os.Args[3])
		}
		if len(os.Args) == 2 && os.Args[1] == "reset-admin-password" {
			return resetAdminPassword(data)
		}
		if len(os.Args) == 4 && os.Args[1] == "update-worker" {
			return selfupdate.RunWorker(data, dataDir, os.Args[2], os.Args[3])
		}
		return fmt.Errorf("unknown command: %s", strings.Join(os.Args[1:], " "))
	}

	control, err := controlConfiguration(os.Getenv)
	if err != nil {
		return err
	}
	changes := inventory.NewNotifier()
	app := application.New(data, changes)
	api, err := httpapi.New(data, environment("NOX_WEB_DIR", "./web/dist"), os.Getenv("NOX_PUBLIC_URL"), app)
	if err != nil {
		return err
	}
	if err := api.ConfigureControlAPI(control); err != nil {
		return err
	}
	dockerInventory, err := inventory.NewDockerReader()
	if err != nil {
		return err
	}
	defer dockerInventory.Close()
	api.SetInventory(dockerInventory)
	managedProjects, err := managed.NewManager(data, dockerInventory)
	if err != nil {
		return err
	}
	api.SetManaged(managedProjects)
	api.SetLogs(dockerInventory)
	api.SetTerminal(dockerInventory)
	dockerLifecycle, err := lifecycle.New()
	if err != nil {
		return err
	}
	defer dockerLifecycle.Close()
	dockerLifecycle.SetStore(data)
	api.SetLifecycle(dockerLifecycle)
	updates := selfupdate.New(data, buildSHA)
	api.SetSelfUpdate(updates)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app.ResourceKeys = jobs.TargetResources
	app.ResourceImages = jobs.Images
	observer := &jobs.Observer{Data: data, Changes: changes, Verify: func(ctx context.Context, job store.Job) error {
		if job.Domain == "managed" {
			return managedProjects.Reconcile(ctx, job)
		}
		return store.ErrJobChanged
	}}
	app.JobObserver = observer
	if err := app.ReconcileDirectJobs(ctx); err != nil {
		return err
	}
	stopObserver := observer.Start(ctx)
	defer stopObserver()
	dockerInventory.Start(ctx, changes.Notify)
	mcpRuntime, err := mcpapi.New(app, control.Version)
	if err != nil {
		return err
	}
	api.SetMCPHandler(mcpRuntime.HTTPHandler())
	server := &http.Server{
		Addr:              environment("NOX_LISTEN_ADDR", ":8080"),
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      130 * time.Second,
		IdleTimeout:       60 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	updates.Start(ctx)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("NoX Yard listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func resetAdminPassword(data *store.Store) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("reset-admin-password requires an interactive terminal")
	}
	fmt.Fprint(os.Stderr, "New password: ")
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return err
	}
	fmt.Fprint(os.Stderr, "Confirm password: ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return err
	}
	if string(first) != string(second) {
		return errors.New("passwords do not match")
	}
	hash, err := auth.HashPassword(string(first))
	if err != nil {
		return err
	}
	if err := data.ResetAdminPassword(hash); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "Administrator password updated. All sessions have been signed out.")
	return nil
}

func environment(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func resolvedVersion(runtime, built string) string {
	if version := strings.TrimSpace(runtime); version != "" {
		return version
	}
	if version := strings.TrimSpace(built); version != "" {
		return version
	}
	return "dev"
}

func controlConfiguration(getenv func(string) string) (httpapi.ControlConfig, error) {
	config := httpapi.ControlConfig{Key: getenv("NOX_YARD_API_KEY"), Version: resolvedVersion(getenv("NOX_YARD_VERSION"), buildVersion)}
	if value := getenv("NOX_YARD_API_ENABLED"); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return httpapi.ControlConfig{}, errors.New("NOX_YARD_API_ENABLED must be a boolean")
		}
		config.Enabled = enabled
	}
	return config, nil
}
