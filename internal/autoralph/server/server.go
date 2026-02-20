package server

import (
	"context"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/ccusage"
	"github.com/uesteibar/ralph/internal/autoralph/db"
	"github.com/uesteibar/ralph/web"
)

// WorkspaceRemover removes a Ralph workspace (git worktree, directory, branch).
type WorkspaceRemover interface {
	RemoveWorkspace(ctx context.Context, repoPath, name string) error
}

// BuildChecker checks whether a build worker is active for a given issue
// and can cancel running workers.
type BuildChecker interface {
	IsRunning(issueID string) bool
	Cancel(issueID string) bool
}

// CCUsageProvider provides current Claude Code usage data.
type CCUsageProvider interface {
	Current() []ccusage.UsageGroup
}

// Config holds server configuration.
type Config struct {
	// DevMode enables reverse-proxying non-API requests to the Vite dev server.
	DevMode bool
	// ViteURL is the Vite dev server URL (default "http://localhost:5173").
	ViteURL string
	// Hub is the WebSocket hub for real-time updates. When non-nil, the
	// /api/ws endpoint is registered to serve WebSocket connections.
	Hub *Hub
	// DB is the database connection for API endpoints. When non-nil, REST
	// API endpoints are registered.
	DB *db.DB
	// WorkspaceRemover removes Ralph workspaces on issue deletion. Optional.
	WorkspaceRemover WorkspaceRemover
	// BuildChecker checks whether a build worker is active for a given issue. Optional.
	BuildChecker BuildChecker
	// PRDPathFn computes the PRD file path for a project+workspace. Optional.
	// When set, the issue detail API returns story and test data from the PRD.
	PRDPathFn func(projectLocalPath, workspaceName string) string
	// CCUsageProvider provides Claude Code usage data. Optional.
	CCUsageProvider CCUsageProvider
	// Wake is an optional channel used to notify the orchestrator loop that it
	// should re-evaluate immediately (e.g. after a retry or resume). A non-blocking
	// send is performed; if the channel is nil or full the signal is silently dropped.
	Wake chan<- struct{}
	// ModelName is the resolved Claude model display name (e.g. "Sonnet 4.5").
	// Included in issue list and detail API responses.
	ModelName string
	// LinearURL overrides the Linear API endpoint (for mock servers in E2E tests).
	LinearURL string
	// GithubURL overrides the GitHub API endpoint (for mock servers in E2E tests).
	GithubURL string
}

// Server wraps the autoralph HTTP server.
type Server struct {
	mux      *http.ServeMux
	listener net.Listener
}

// New creates a Server bound to the given address (e.g. "127.0.0.1:7749").
// It does not start serving; call Serve() for that.
func New(addr string, cfg Config) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	s := &Server{mux: mux, listener: ln}
	s.registerRoutes(cfg)
	return s, nil
}

// Addr returns the listener's address (useful when binding to :0 in tests).
func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

// Serve starts accepting connections. It blocks until the listener is closed.
func (s *Server) Serve() error {
	return http.Serve(s.listener, s.mux)
}

// Close shuts down the listener.
func (s *Server) Close() error {
	return s.listener.Close()
}

func (s *Server) registerRoutes(cfg Config) {
	if cfg.DB != nil {
		api := &apiHandler{db: cfg.DB, startAt: time.Now(), workspaceRemover: cfg.WorkspaceRemover, buildChecker: cfg.BuildChecker, prdPathFn: cfg.PRDPathFn, wake: cfg.Wake, modelName: cfg.ModelName}
		s.mux.HandleFunc("GET /api/status", api.handleStatus)
		s.mux.HandleFunc("GET /api/projects", api.handleListProjects)
		s.mux.HandleFunc("GET /api/issues", api.handleListIssues)
		s.mux.HandleFunc("GET /api/issues/{id}", api.handleGetIssue)
		s.mux.HandleFunc("POST /api/issues/{id}/pause", api.handlePauseIssue)
		s.mux.HandleFunc("POST /api/issues/{id}/resume", api.handleResumeIssue)
		s.mux.HandleFunc("POST /api/issues/{id}/retry", api.handleRetryIssue)
		s.mux.HandleFunc("DELETE /api/issues/{id}", api.handleDeleteIssue)
		s.mux.HandleFunc("POST /api/issues/{id}/transition", api.handleTransitionIssue)
		s.mux.HandleFunc("POST /api/issues/{id}/reset", api.handleResetFields)
		s.mux.HandleFunc("GET /api/issues/{id}/transitions", api.handleGetTransitions)
		s.mux.HandleFunc("GET /api/activity", api.handleListActivity)
	} else {
		s.mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
	}

	s.mux.HandleFunc("GET /api/cc-usage", handleCCUsage(cfg.CCUsageProvider))

	if cfg.Hub != nil {
		s.mux.HandleFunc("GET /api/ws", cfg.Hub.ServeWS)
	}

	// Catch-all for unregistered /api/ routes — return 404.
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	if cfg.DevMode {
		viteURL := cfg.ViteURL
		if viteURL == "" {
			viteURL = "http://localhost:5173"
		}
		target, _ := url.Parse(viteURL)
		proxy := httputil.NewSingleHostReverseProxy(target)
		s.mux.Handle("/", proxy)
	} else {
		s.mux.Handle("/", spaHandler())
	}
}

// spaHandler returns an http.Handler that serves the embedded SPA.
// For any path that doesn't match a real file, it serves index.html
// to support client-side routing.
func spaHandler() http.Handler {
	distFS, err := web.DistFS()
	if err != nil {
		panic("failed to load embedded web assets: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Try to open the file. If it exists, serve it directly.
		if f, err := distFS.Open(path); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// File not found — serve index.html for client-side routing.
		indexFile, err := fs.ReadFile(distFS, "index.html")
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexFile)
	})
}
