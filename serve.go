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

//go:embed prompts/system.md
var systemPromptMarkdown string

const defaultServeAddr = "127.0.0.1:8080"

// server holds the dependencies wired in serveCmd. Keeping handlers as
// methods means they can read llm/systemPrompt without package-level globals.
type server struct {
	llm          LLM
	systemPrompt string
}

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

	llm, err := loadLLM()
	if err != nil {
		return err
	}
	s := &server{
		llm:          llm,
		systemPrompt: strings.TrimSpace(systemPromptMarkdown),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/chat", s.handleChat)
	mux.Handle("/", spaHandler(dist))

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Fprintf(os.Stderr, "hiero-pay serve listening on http://%s\n", *addr)
	return srv.ListenAndServe()
}

// loadLLM picks an LLM implementation based on the LLM_MODEL env var's
// prefix. Slice 5 will extend the switch with openai for gpt-*/o* models.
func loadLLM() (LLM, error) {
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		return nil, errors.New("LLM_MODEL is not set (e.g. LLM_MODEL=claude-sonnet-4-5)")
	}
	switch {
	case strings.HasPrefix(model, "claude-"):
		return NewAnthropicClient(os.Getenv("ANTHROPIC_API_KEY"), model)
	default:
		return nil, fmt.Errorf("unsupported LLM_MODEL prefix: %q (expected claude-*)", model)
	}
}

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type chatRequest struct {
	Messages []Message `json:"messages"`
}

type chatResponse struct {
	Message string `json:"message"`
}

func (s *server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req chatRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %s", err.Error()))
		return
	}
	if len(req.Messages) == 0 {
		writeJSONError(w, http.StatusBadRequest, "messages is required")
		return
	}
	for i, m := range req.Messages {
		if m.Role != RoleUser && m.Role != RoleAssistant {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("messages[%d].role must be user or assistant", i))
			return
		}
		if strings.TrimSpace(m.Content) == "" {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("messages[%d].content must be non-empty", i))
			return
		}
	}

	resp, err := s.llm.Chat(r.Context(), s.systemPrompt, req.Messages, nil)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(chatResponse{Message: resp.Text})
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
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
