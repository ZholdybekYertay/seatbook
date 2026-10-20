package main

import (
	"context"
	"errors"
	"github.com/ZholdybekYertay/seatbook/internal/config"
	"github.com/ZholdybekYertay/seatbook/internal/httpapi"
	"github.com/ZholdybekYertay/seatbook/internal/store"
	"github.com/ZholdybekYertay/seatbook/web"
	"golang.org/x/crypto/bcrypt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	s, err := store.Open(startup, c.DatabaseURL, c.Hold)
	if err != nil {
		cancel()
		return err
	}
	defer s.DB.Close()
	if err = s.Migrate(startup); err != nil {
		cancel()
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(c.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		cancel()
		return err
	}
	if err = s.Bootstrap(startup, c.AdminEmail, string(hash)); err != nil {
		cancel()
		return err
	}
	cancel()
	workerCtx, workerStop := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				cctx, ccancel := context.WithTimeout(workerCtx, 5*time.Second)
				err := s.Expire(cctx)
				ccancel()
				if err != nil && workerCtx.Err() == nil {
					slog.Error("expire bookings", "error", err)
				}
			}
		}
	}()
	defer func() { workerStop(); wg.Wait() }()
	server := &http.Server{Addr: c.Addr, Handler: httpapi.New(s, c, web.Handler()), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	errs := make(chan error, 1)
	go func() {
		slog.Info("SeatBook ready", "address", c.Addr, "origin", c.Origin)
		errs <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
	case err = <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	if err = server.Shutdown(shutdown); err != nil {
		_ = server.Close()
		return err
	}
	return nil
}
