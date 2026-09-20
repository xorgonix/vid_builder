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
	"vidbuild/internal/media"
	"vidbuild/internal/planner"
	"vidbuild/internal/store"
	"vidbuild/internal/workflows"
)

const loginCSRFCookie = "vidbuild_login_csrf"

// Server hosts the shared Chi/templ/HTMX application used by desktop and browser.
type Server struct {
	cfg       *config.Config
	store     *store.Store
	media     *media.Store
	log       *slog.Logger
	dummyHash string
	planner   *planner.Planner
}

// New constructs a Server.
func New(cfg *config.Config, st *store.Store, ms *media.Store, log *slog.Logger) *Server {
	params := auth.DefaultParams(cfg.Auth.Argon2MemoryKB, cfg.Auth.Argon2Iterations, cfg.Auth.Argon2Parallelism)
	dummy, _ := auth.HashPassword("vid-build-timing-equalizer", params)
	return &Server{cfg: cfg, store: st, media: ms, log: log, dummyHash: dummy, planner: planner.New(llm.New(cfg.LLM))}
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

// SeedBundledWorkflow imports the bundled workflow package once.
func (s *Server) SeedBundledWorkflow(ctx context.Context) error {
	pkg, err := workflows.Load()
	if err != nil {
		return err
	}
	bindings := make([]store.WorkflowBinding, 0, len(pkg.Manifest.Bindings))
	for _, b := range pkg.Manifest.Bindings {
		bindings = append(bindings, store.WorkflowBinding{
			SemanticField: b.SemanticField, NodeID: b.NodeID, InputName: b.InputName,
			ValueType: b.ValueType, Required: b.Required,
		})
	}
	return s.store.SeedBundledWorkflow(ctx, pkg.Manifest.Name, pkg.Manifest.Slug, pkg.Manifest.Task,
		pkg.Manifest.Description, pkg.GraphJSON, pkg.GraphSHA, pkg.ManifestJSON, pkg.Manifest.TestedComfyUIVersion, bindings)
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
	r.Handle("/static/*", staticHandler())
	r.Get("/readyz", s.handleReady)

	r.Get("/login", s.handleLoginForm)
	r.With(s.loginCSRFCheck).Post("/login", s.handleLoginPost)

	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/", s.handleRoot)
		r.Get("/account", s.handleAccount)
		r.Get("/assets/{assetID}", s.handleAssetFile)
		r.Get("/settings/local-services", s.handleLocalServices)
		r.With(s.csrfProtect).Post("/settings/local-services/test", s.handleTestLocalServices)
		r.Get("/app/capabilities", s.handleCapabilities)
		r.Route("/admin", func(r chi.Router) {
			r.Use(s.requireSystemAdmin)
			r.Get("/users", s.handleAdminUsers)
			r.With(s.csrfProtect).Post("/users", s.handleAdminCreateUser)
			r.With(s.csrfProtect).Patch("/users/{userID}", s.handleAdminUpdateUser)
		})
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
			r.Get("/projects/{p}/package", s.handleProjectPackage)
			r.With(s.requireRole("owner", "editor")).Post("/projects/{p}/package", s.handleQueuePackage)
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
				r.Get("/board", s.handleBoard)
				r.Get("/shots/{shotID}", s.handleShot)
				r.Patch("/shots/{shotID}/specs/{kind}", s.handleUpdateSpec)
				r.Post("/shots/{shotID}/specs/{kind}/suggest", s.handleSuggestSpec)
				r.Get("/shots/{shotID}/blocking", s.handleBlockingEditor)
				r.Post("/shots/{shotID}/blocking/suggest", s.handleSuggestBlocking)
				r.Patch("/shots/{shotID}/blocking/{role}", s.handleSaveBlocking)
				r.Post("/shots/{shotID}/blocking/{role}/copy", s.handleCopyBlocking)
				r.Get("/shots/{shotID}/history", s.handleShotHistory)
				r.Get("/shots/{shotID}/prompt-workbench", s.handlePromptWorkbench)
				r.Post("/shots/{shotID}/prompt-workbench", s.handlePromptSave)
				r.Post("/shots/{shotID}/prompt-preview", s.handlePromptPreview)
				r.Get("/more", s.handleMore)
				r.Get("/produce", s.handleProduce)
				r.Get("/shots/{shotID}/generate/{role}", s.handleGenerateForm)
				r.Post("/generation-requests", s.handleCreateRequest)
				r.Get("/jobs/{jobID}", s.handleJob)
				r.Get("/jobs/{jobID}/status", s.handleJobStatus)
				r.Get("/review", s.handleReview)
				r.Post("/shots/{shotID}/selections", s.handleSelectAsset)
				r.Post("/assets/{assetID}/approvals", s.handleApprove)
				r.Post("/assets/{assetID}/assessments", s.handleAssess)
				r.Post("/annotations", s.handleCreateAnnotation)
				r.Patch("/annotations/{annotationID}", s.handleUpdateAnnotation)
				r.Get("/edit", s.handleEdit)
				r.Post("/edits/{editID}/items", s.handleAddEditItem)
				r.Patch("/edit-items/{itemID}", s.handleUpdateEditItem)
				r.Post("/edit-items/{itemID}/move", s.handleMoveEditItem)
				r.Delete("/edit-items/{itemID}", s.handleDeleteEditItem)
				r.Post("/edits/{editID}/validate", s.handleValidateEdit)
				r.Get("/edits/{editID}/export", s.handleExportForm)
				r.Post("/edits/{editID}/exports", s.handleCreateExport)
				r.Get("/exports/{exportID}", s.handleExport)
				r.Get("/compute", s.handleCompute)
				r.Post("/campaigns", s.handleCreateCampaign)
				r.Get("/campaigns/{campaignID}", s.handleCampaign)
				r.Post("/campaigns/{campaignID}/preflight", s.handleCampaignPreflight)
				r.Post("/campaigns/{campaignID}/launch", s.handleCampaignLaunch)
				r.Post("/campaigns/{campaignID}/pause", s.handleCampaignPause)
				r.Post("/campaigns/{campaignID}/cancel", s.handleCampaignCancel)
				r.Post("/campaigns/{campaignID}/reconcile", s.handleCampaignReconcile)
			})
			r.Get("/execution-targets", s.handleExecutionTargets)
			r.With(s.requireRole("owner", "editor")).Post("/execution-targets", s.handleCreateExecutionTarget)
			r.Get("/workflows", s.handleWorkflows)
			r.With(s.requireRole("owner", "editor")).Post("/workflows/import", s.handleWorkflowImport)
			r.Get("/workflow-versions/{versionID}", s.handleWorkflowVersion)
			r.With(s.requireRole("owner", "editor")).Patch("/workflow-versions/{versionID}/bindings", s.handleWorkflowBindings)
			r.With(s.requireRole("owner", "editor")).Post("/workflow-versions/{versionID}/validate", s.handleWorkflowValidate)
			r.With(s.requireRole("owner", "editor")).Post("/workflow-versions/{versionID}/activate", s.handleWorkflowActivate)
			r.With(s.requireRole("owner", "editor")).Post("/workflow-versions/{versionID}/retire", s.handleWorkflowRetire)
			r.Get("/projects/{p}/bible", s.handleBible)
			r.With(s.requireRole("owner", "editor")).Post("/projects/{p}/characters", s.handleCreateCharacter)
			r.With(s.requireRole("owner", "editor")).Post("/projects/{p}/locations", s.handleCreateLocation)
			r.With(s.requireRole("owner", "editor")).Post("/projects/{p}/style-rules", s.handleCreateStyleRule)
			r.Get("/settings", s.handleWorkspaceSettings)
			r.Get("/members", s.handleMembers)
			r.With(s.requireRole("owner")).Post("/members", s.handleAddMember)
			r.With(s.requireRole("owner")).Patch("/members/{userID}", s.handleUpdateMember)
			r.With(s.requireRole("owner")).Delete("/members/{userID}", s.handleRemoveMember)
			r.Get("/diagnostics", s.handleDiagnostics)
			r.Get("/model-profiles", s.handleModelProfiles)
			r.With(s.requireRole("owner", "editor")).Post("/model-profiles", s.handleCreateModelProfile)
			r.With(s.requireRole("owner", "editor")).Delete("/model-profiles/{profileID}", s.handleDeleteModelProfile)
			r.With(s.requireRole("owner", "editor")).Post("/projects/{p}/default-profile", s.handleSetProjectDefaultProfile)
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
