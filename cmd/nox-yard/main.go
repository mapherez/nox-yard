package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mapherez/nox-yard/internal/auth"
	"github.com/mapherez/nox-yard/internal/httpapi"
	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/selfupdate"
	"github.com/mapherez/nox-yard/internal/store"
	"golang.org/x/term"
)

var buildSHA = ""

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
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
		if len(os.Args) == 2 && os.Args[1] == "reset-admin-password" {
			return resetAdminPassword(data)
		}
		if len(os.Args) == 4 && os.Args[1] == "update-worker" {
			return selfupdate.RunWorker(data, dataDir, os.Args[2], os.Args[3])
		}
		return fmt.Errorf("unknown command: %s", strings.Join(os.Args[1:], " "))
	}

	api, err := httpapi.New(data, environment("NOX_WEB_DIR", "./web/dist"), os.Getenv("NOX_PUBLIC_URL"))
	if err != nil {
		return err
	}
	dockerInventory, err := inventory.NewDockerReader()
	if err != nil {
		return err
	}
	defer dockerInventory.Close()
	api.SetInventory(dockerInventory)
	updates := selfupdate.New(data, buildSHA)
	api.SetSelfUpdate(updates)
	server := &http.Server{
		Addr:              environment("NOX_LISTEN_ADDR", ":8080"),
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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
