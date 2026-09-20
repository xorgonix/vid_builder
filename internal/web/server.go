package web

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"vidbuild/internal/auth"
	"vidbuild/internal/config"
	"vidbuild/internal/llm"
	"vidbuild/internal/planner"
	"vidbuild/internal/store"
)

const loginCSRFCookie = "vidbuild_login_csrf"

// Server hosts the shared Chi/templ/HTMX application used by desktop and browser.
type Server struct {
	cfg       *config.Config
	store     *store.Store
	log       *slog.Logger
	dummyHash string
	planner   *planner.Planner
}

// New constructs a Server.
func New(cfg *config.Config, st *store.Store, log *slog.Logger) *Server {
	params := auth.DefaultParams(cfg.Auth.Argon2MemoryKB, cfg.Auth.Argon2Iterations, cfg.Auth.Argon2Parallelism)
	dummy, _ := auth.HashPassword("vid-build-timing-equalizer", params)
	return &Server{cfg: cfg, store: st, log: log, dummyHash: dummy, planner: planner.New(llm.New(cfg.LLM))}
}

// Bootstrap creates the first administrator and personal workspace when empty.
func (s *Server) Bootstrap(ctx context.Context) error {
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	params := auth.DefaultParams(s.cfg.Auth.Argon2MemoryKB, s.cfg.Auth.Argon2Iterations, s.cfg.Auth.Argon2Parallelism)
	hash, err := auth.HashPassword(s.cfg.Bootstrap.AdminPassword, params)
	if err != nil {
		return err
	}
	if err := s.store.SeedBootstrap(s.cfg.Bootstrap.AdminEmail, hash, s.cfg.Bootstrap.AdminName); err != nil {
		return err
	}
	s.log.Info("bootstrap admin created", "email", s.cfg.Bootstrap.AdminEmail)
	return nil
}

// Handler returns the fully wired HTTP handler.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(s.recover)
	r.Use(s.requestLog)
	r.Use(s.loadSession)
	r.Use(s.methodOverride)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/readyz", s.handleReady)

	r.Get("/login", s.handleLoginForm)
	r.With(s.loginCSRFCheck).Post("/login", s.handleLoginPost)

	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/", s.handleRoot)
		r.Get("/account", s.handleAccount)
		r.With(s.csrfProtect).Post("/account/sessions/{id}", s.handleSessionRevoke)
		r.With(s.csrfProtect).Post("/logout", s.handleLogout)
		r.Route("/workspaces", func(r chi.Router) {
			r.Get("/", s.handleWorkspaceList)
			r.With(s.csrfProtect).Post("/", s.handleWorkspaceCreate)
		})
		r.Route("/w/{w}", func(r chi.Router) {
			r.Use(s.workspaceScope)
			r.Use(s.csrfProtect)
			r.Get("/", s.handleWorkspaceDashboard)
			r.Get("/projects", s.handleProjectList)
			r.Get("/projects/new", s.handleProjectNew)
			r.With(s.requireRole("owner", "editor")).Post("/projects", s.handleProjectCreate)
			r.Get("/projects/{p}", s.handleProjectDashboard)
			r.With(s.requireRole("owner", "editor")).Post("/projects/{p}/videos", s.handleVideoCreate)
			r.Route("/projects/{p}/videos/{v}", func(r chi.Router) {
				r.Get("/", s.handleVideo)
				r.Get("/develop", s.handleDevelop)
				r.Post("/discovery/threads", s.handleCreateThread)
				r.Post("/discovery/threads/{threadID}/messages", s.handlePostMessage)
				r.Post("/discovery/threads/{threadID}/ask", s.handleAskPlanner)
				r.Post("/proposals/brief", s.handleProposeBrief)
				r.Post("/proposals/outline", s.handleProposeOutline)
				r.Get("/proposals/{proposalID}", s.handleProposalView)
				r.Post("/proposals/{proposalID}/apply", s.handleProposalApply)
				r.Post("/proposals/{proposalID}/reject", s.handleProposalReject)
				r.Post("/brief/revise", s.handleBriefRevise)
				r.Get("/brief/history", s.handleBriefHistory)
				r.Get("/outline", s.handleOutline)
				r.Post("/sections", s.handleCreateSection)
				r.Patch("/sections/{sectionID}", s.handleUpdateSection)
				r.Delete("/sections/{sectionID}", s.handleDeleteSection)
				r.Post("/sections/{sectionID}/move", s.handleMoveSection)
				r.Post("/sections/{sectionID}/scenes", s.handleCreateScene)
				r.Patch("/scenes/{sceneID}", s.handleUpdateScene)
				r.Delete("/scenes/{sceneID}", s.handleDeleteScene)
				r.Post("/scenes/{sceneID}/move", s.handleMoveScene)
				r.Post("/scenes/{sceneID}/shots", s.handleCreateShot)
				r.Patch("/shots/{shotID}", s.handleUpdateShot)
				r.Delete("/shots/{shotID}", s.handleDeleteShot)
				r.Post("/shots/{shotID}/move", s.handleMoveShot)
			})
			r.Get("/settings", s.handleWorkspaceSettings)
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		s.notFound(w, r)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		s.renderError(w, r, http.StatusMethodNotAllowed, "Method not allowed", "That action is not supported here.")
	})
	return r
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	if err := s.store.Ping(); err != nil {
		http.Error(w, "db not ready", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte("ready"))
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/workspaces", http.StatusSeeOther)
}

// ensureLoginCSRF returns the double-submit token for the login form.
func (s *Server) ensureLoginCSRF(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(loginCSRFCookie); err == nil && c.Value != "" {
		return c.Value
	}
	tok := auth.CSRFToken(auth.NewCSRFSecret())
	http.SetCookie(w, &http.Cookie{
		Name:     loginCSRFCookie,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.Security.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((10 * time.Minute).Seconds()),
	})
	return tok
}

func (s *Server) loginCSRFCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(loginCSRFCookie)
		if err != nil || c.Value == "" || !auth.EqualToken(c.Value, r.FormValue("_csrf")) {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
