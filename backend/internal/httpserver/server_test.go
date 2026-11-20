package httpserver

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/logger"
)

// Cancelling the context stops accepting connections but lets in-flight
// requests finish before Run returns.
func TestRunShutsDownGracefully(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	started := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		time.Sleep(300 * time.Millisecond) // slow in-flight request
		_, _ = io.WriteString(w, "done")
	})
	srv := NewServer(config.HTTPConfig{ReadTimeout: time.Second, WriteTimeout: 5 * time.Second}, handler, logger.Discard())
	srv.Addr = addr

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- Run(ctx, srv, 5*time.Second, logger.Discard()) }()

	var resp *http.Response
	reqErr := make(chan error, 1)
	go func() {
		for i := 0; i < 50; i++ { // wait for the listener
			r, err := http.Get("http://" + addr + "/")
			if err == nil {
				resp = r
				reqErr <- nil
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		reqErr <- io.ErrUnexpectedEOF
	}()

	<-started
	cancel() // shutdown while the request is in flight

	if err := <-reqErr; err != nil {
		t.Fatalf("in-flight request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "done" {
		t.Fatalf("body = %q", body)
	}
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after shutdown")
	}
	if _, err := http.Get("http://" + addr + "/"); err == nil {
		t.Fatal("server still accepting connections after shutdown")
	}
}
