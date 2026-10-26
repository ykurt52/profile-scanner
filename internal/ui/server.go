// Package ui is a local HTTP server that serves the interface on 127.0.0.1 only.
// The WebView2 window connects to it; it is not reachable from outside and every
// API call is checked against a random session key.
package ui

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"skillscanner/internal/analyze"
	"skillscanner/internal/config"
	"skillscanner/internal/github"
	"skillscanner/internal/report"
	"skillscanner/internal/scan"
)

//go:embed index.html
var page []byte

type Server struct {
	Dir  string
	Base string // GitHub API address, can be swapped in tests
	key  string

	mu     sync.Mutex
	cfg    config.Config
	client *github.Client
	plan   *scan.Plan
	ai     bool
	state  State
	cancel context.CancelFunc
	seen   time.Time
}

type State struct {
	Running bool     `json:"running"`
	Phase   string   `json:"phase"`
	Message string   `json:"message"`
	Done    int      `json:"done"`
	Total   int      `json:"total"`
	Used    int      `json:"used"`
	Left    int      `json:"remaining"`
	Limit   int      `json:"limit"`
	Error   string   `json:"error"`
	AIError string   `json:"ai_error"`
	Report  string   `json:"report_path"`
	Notes   []string `json:"notes"`
}

func New(dir string) *Server {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return &Server{Dir: dir, Base: github.BaseURL, key: hex.EncodeToString(b), cfg: config.Load()}
}

// Idle returns how long ago the interface last called the API and whether a scan is running.
func (s *Server) Idle() (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen.IsZero() {
		return 0, s.state.Running
	}
	return time.Since(s.seen), s.state.Running
}

// Start runs the server on a random local port and returns the start URL.
func (s *Server) Start() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.index)
	mux.HandleFunc("GET /api/config", s.auth(s.getConfig))
	mux.HandleFunc("POST /api/precheck", s.auth(s.precheck))
	mux.HandleFunc("POST /api/start", s.auth(s.start))
	mux.HandleFunc("POST /api/cancel", s.auth(s.stop))
	mux.HandleFunc("GET /api/status", s.auth(s.status))
	mux.HandleFunc("POST /api/open", s.auth(s.open))
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String() + "/?k=" + s.key, nil
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" || r.URL.Query().Get("k") != s.key {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(page)
}

func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Session") != s.key {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		s.mu.Lock()
		s.seen = time.Now()
		s.mu.Unlock()
		h(w, r)
	}
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	c := s.cfg
	s.mu.Unlock()
	reply(w, map[string]any{
		"has_github_token": c.Token != "",
		"has_anthropic":    c.Key != "",
		"model":            c.Model,
		"max_repos":        c.Repos,
		"request_budget":   c.Budget,
		"reserve_percent":  c.Reserve,
	})
}

type checkReq struct {
	Login  string `json:"login"`
	Token  string `json:"github_token"`
	Key    string `json:"anthropic_key"`
	Clear  bool   `json:"clear_secrets"`
	Keep   bool   `json:"remember"`
	AI     bool   `json:"use_llm"`
	Repos  int    `json:"max_repos"`
	Budget int    `json:"request_budget"`
}

func (s *Server) precheck(w http.ResponseWriter, r *http.Request) {
	var req checkReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		fail(w, err)
		return
	}
	s.mu.Lock()
	if s.state.Running {
		s.mu.Unlock()
		fail(w, errors.New("a scan is already running"))
		return
	}
	cfg := s.cfg
	if req.Clear {
		cfg.Token, cfg.Key = "", ""
	}
	if t := strings.TrimSpace(req.Token); t != "" {
		cfg.Token = t
	}
	if k := strings.TrimSpace(req.Key); k != "" {
		cfg.Key = k
	}
	if req.Repos > 0 {
		cfg.Repos = min(req.Repos, 100)
	}
	if req.Budget > 0 {
		cfg.Budget = min(req.Budget, 1500)
	}
	s.cfg = cfg
	s.mu.Unlock()
	_ = config.Save(cfg, req.Keep)

	c := github.New(cfg.Token, github.NewCache(filepath.Join(s.Dir, "cache")))
	c.Base = s.Base
	c.Budget = cfg.Budget
	c.Delay = time.Duration(cfg.Delay) * time.Millisecond

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	plan, err := scan.Precheck(ctx, c, req.Login, cfg.Repos, cfg.Reserve)
	if err != nil {
		fail(w, err)
		return
	}
	s.mu.Lock()
	s.client, s.plan = c, plan
	s.ai = req.AI && cfg.Key != ""
	ai := s.ai
	s.mu.Unlock()
	reply(w, map[string]any{"estimate": plan, "llm": ai, "llm_requested": req.AI})
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Running {
		fail(w, errors.New("a scan is already running"))
		return
	}
	if s.plan == nil || s.client == nil {
		fail(w, errors.New("run the precheck first"))
		return
	}
	if !s.plan.OK {
		fail(w, errors.New(s.plan.Message))
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.state = State{Running: true, Phase: "github", Message: "Starting...", Total: s.plan.Repos}
	go s.run(ctx, s.client, s.plan, s.ai, s.cfg)
	s.plan = nil // a plan can only be used once
	reply(w, map[string]bool{"ok": true})
}

func (s *Server) run(ctx context.Context, c *github.Client, plan *scan.Plan, ai bool, cfg config.Config) {
	defer func() {
		if rec := recover(); rec != nil {
			s.set(func(st *State) { st.Error = "unexpected error"; st.Running = false })
		}
	}()
	progress := func(msg string, done, total int) {
		rate := c.Rate()
		s.set(func(st *State) {
			st.Message, st.Done, st.Total = msg, done, total
			st.Used, st.Left, st.Limit = c.Used(), rate.Left, rate.Limit
		})
	}
	p, err := scan.Run(ctx, c, plan, progress)
	if p == nil {
		s.set(func(st *State) { st.Error = text(err); st.Running = false })
		return
	}
	if err != nil {
		p.Notes = append(p.Notes, err.Error())
	}
	if ai && ctx.Err() == nil {
		s.set(func(st *State) { st.Phase = "llm"; st.Message = "Asking Claude..." })
		res, err := analyze.Ask(ctx, cfg.Key, cfg.Model, p)
		if err != nil {
			s.set(func(st *State) { st.AIError = err.Error() })
			p.Notes = append(p.Notes, "AI interpretation failed: "+err.Error())
		} else {
			p.AI = res
		}
	}
	s.set(func(st *State) { st.Phase = "report"; st.Message = "Writing report..." })
	out, err := report.Write(filepath.Join(s.Dir, "reports"), p)
	s.set(func(st *State) {
		st.Running = false
		st.Phase = "done"
		st.Notes = p.Notes
		st.Used = c.Used()
		if err != nil {
			st.Error = "Cannot write report: " + err.Error()
			return
		}
		st.Report = out
		st.Message = "Done"
	})
	if err == nil {
		Open(out)
	}
}

func text(err error) string {
	if err == nil {
		return "unknown error"
	}
	if errors.Is(err, context.Canceled) {
		return "Cancelled"
	}
	return err.Error()
}

func (s *Server) set(f func(*State)) {
	s.mu.Lock()
	f(&s.state)
	s.mu.Unlock()
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	reply(w, map[string]bool{"ok": true})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	st := s.state
	s.mu.Unlock()
	reply(w, st)
}

func (s *Server) open(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	p := s.state.Report
	s.mu.Unlock()
	if p == "" {
		fail(w, errors.New("no report yet"))
		return
	}
	if r.URL.Query().Get("folder") == "1" {
		_ = exec.Command("explorer", "/select,", p).Start()
	} else {
		Open(p)
	}
	reply(w, map[string]bool{"ok": true})
}

// Open opens a file or URL with the default Windows program (usually the browser).
func Open(target string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
}
