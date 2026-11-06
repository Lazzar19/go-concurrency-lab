package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const shutdownTimeout = 10 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := listenAddr(os.Getenv("HOST"), os.Getenv("PORT"))
	log.Printf("listening on %s", addr)
	app := newApp(ctx)
	if err := serve(ctx, addr, app.mux, app.handler.Wait, nil); err != nil {
		log.Fatal(err)
	}
}

func listenAddr(host, port string) string {
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "8080"
	}
	return net.JoinHostPort(host, port)
}

// serve accepts connections until ctx is cancelled, then shuts the HTTP
// server down and waits for benchmarks to finish, both within shutdownTimeout.
// bound, when set, receives the address after the listener is open.
func serve(ctx context.Context, addr string, handler http.Handler, wait func(context.Context) error, bound func(string)) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if bound != nil {
		bound(ln.Addr().String())
	}
	srv := &http.Server{Handler: handler}
	errCh := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	shutErr := srv.Shutdown(shutCtx)
	var waitErr error
	if wait != nil {
		waitErr = wait(shutCtx)
	}
	if shutErr != nil {
		return shutErr
	}
	return waitErr
}
