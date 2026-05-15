package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed all:web/dist
var webDist embed.FS

const defaultServeAddr = "127.0.0.1:8080"

func serveCmd(args []string) error {
	fset := flag.NewFlagSet("serve", flag.ContinueOnError)
	fset.SetOutput(os.Stderr)
	addr := fset.String("addr", defaultServeAddr, "bind address (host:port)")
	if err := fset.Parse(args); err != nil {
		return err
	}

	dist, err := fs.Sub(webDist, "web/dist")
	if err != nil {
		return fmt.Errorf("frontend assets unavailable: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", handleHealth)
	mux.Handle("/", spaHandler(dist))

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Fprintf(os.Stderr, "hiero-pay serve listening on http://%s\n", *addr)
	return srv.ListenAndServe()
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// spaHandler serves embedded assets, falling back to index.html for any path
// that does not resolve to a file. The fallback enables client-side routing.
func spaHandler(root fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			clean = "index.html"
		}
		f, err := root.Open(clean)
		if err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		if !errors.Is(err, fs.ErrNotExist) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}
