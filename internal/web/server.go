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
	lan       lanState
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

// SeedBundledWorkflow imports the bundled workflow packages once.
func (s *Server) SeedBundledWorkflow(ctx context.Context) error {
	pkgs, err := workflows.LoadAll()
	if err != nil {
		return err
	}
	for _, pkg := range pkgs {
		bindings := make([]store.WorkflowBinding, 0, len(pkg.Manifest.Bindings))
		for _, b := range pkg.Manifest.Bindings {
			bindings = append(bindings, store.WorkflowBinding{
				SemanticField: b.SemanticField, NodeID: b.NodeID, InputName: b.InputName,
				ValueType: b.ValueType, Required: b.Required,
			})
		}
		if err := s.store.SeedBundledWorkflow(ctx, pkg.Manifest.Name, pkg.Manifest.Slug, pkg.Manifest.Task,
			pkg.Manifest.Description, pkg.GraphJSON, pkg.GraphSHA, pkg.ManifestJSON, pkg.Manifest.TestedComfyUIVersion, bindings); err != nil {
			return err
		}
	}
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
	r.Handle("/static/*", staticHandler())
	r.Get("/readyz", s.handleReady)

	r.Get("/login", s.handleLoginForm)
	r.With(s.loginCSRFCheck).Post("/login", s.handleLoginPost)
	r.Get("/pair", s.handlePair)
	r.Get("/manifest.webmanifest", s.handleManifest)
	r.Get("/sw.js", s.handleServiceWorker)
	r.Get("/offline", s.handleOffline)

	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/", s.handleRoot)
		r.Get("/account", s.handleAccount)
		r.Get("/tutorials", s.handleTutorials)
		r.Get("/tutorials/{slug}/script.json", s.handleTutorialScript)
		r.Get("/assets/{assetID}", s.handleAssetFile)
		r.Get("/settings/local-services", s.handleLocalServices)
		r.With(s.csrfProtect).Post("/settings/local-services/test", s.handleTestLocalServices)
		r.With(s.csrfProtect).Post("/settings/local-services/unload", s.handleUnloadPlanner)
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
				canEdit := s.requireRole("owner", "editor")
				canReview := s.requireRole("owner", "editor", "reviewer")

				r.Get("/", s.handleVideo)
				r.Get("/develop", s.handleDevelop)
				r.With(canEdit).Post("/discovery/threads", s.handleCreateThread)
				r.With(canEdit).Post("/discovery/threads/{threadID}/messages", s.handlePostMessage)
				r.With(canEdit).Post("/discovery/threads/{threadID}/ask", s.handleAskPlanner)
				r.With(canEdit).Post("/proposals/brief", s.handleProposeBrief)
				r.With(canEdit).Post("/proposals/outline", s.handleProposeOutline)
				r.Get("/proposals/{proposalID}", s.handleProposalView)
				r.With(canEdit).Post("/proposals/{proposalID}/apply", s.handleProposalApply)
				r.With(canEdit).Post("/proposals/{proposalID}/reject", s.handleProposalReject)
				r.With(canEdit).Post("/proposals/{proposalID}/regenerate", s.handleProposalRegenerate)
				r.With(canEdit).Post("/brief/revise", s.handleBriefRevise)
				r.Get("/brief/history", s.handleBriefHistory)
				r.Get("/outline", s.handleOutline)
				r.With(canEdit).Post("/sections", s.handleCreateSection)
				r.With(canEdit).Patch("/sections/{sectionID}", s.handleUpdateSection)
				r.With(canEdit).Delete("/sections/{sectionID}", s.handleDeleteSection)
				r.With(canEdit).Post("/sections/{sectionID}/move", s.handleMoveSection)
				r.With(canEdit).Post("/sections/{sectionID}/scenes", s.handleCreateScene)
				r.With(canEdit).Patch("/scenes/{sceneID}", s.handleUpdateScene)
				r.With(canEdit).Delete("/scenes/{sceneID}", s.handleDeleteScene)
				r.With(canEdit).Post("/scenes/{sceneID}/move", s.handleMoveScene)
				r.With(canEdit).Post("/scenes/{sceneID}/shots", s.handleCreateShot)
				r.With(canEdit).Post("/scenes/{sceneID}/beats", s.handleCreateBeat)
				r.With(canEdit).Patch("/beats/{beatID}", s.handleUpdateBeat)
				r.With(canEdit).Delete("/beats/{beatID}", s.handleDeleteBeat)
				r.With(canEdit).Post("/beats/{beatID}/move", s.handleMoveBeat)
				r.With(canEdit).Post("/scenes/{sceneID}/propose-beats", s.handleProposeBeats)
				r.With(canEdit).Post("/beats/{beatID}/propose-shots", s.handleProposeShots)
				r.With(canEdit).Patch("/shots/{shotID}", s.handleUpdateShot)
				r.With(canEdit).Delete("/shots/{shotID}", s.handleDeleteShot)
				r.With(canEdit).Post("/shots/{shotID}/move", s.handleMoveShot)
				r.Get("/board", s.handleBoard)
				r.Get("/shots/{shotID}", s.handleShot)
				r.With(canEdit).Patch("/shots/{shotID}/specs/{kind}", s.handleUpdateSpec)
				r.With(canEdit).Post("/shots/{shotID}/specs/{kind}/suggest", s.handleSuggestSpec)
				r.Get("/shots/{shotID}/blocking", s.handleBlockingEditor)
				r.With(canEdit).Post("/shots/{shotID}/blocking/suggest", s.handleSuggestBlocking)
				r.With(canEdit).Patch("/shots/{shotID}/blocking/{role}", s.handleSaveBlocking)
				r.With(canEdit).Post("/shots/{shotID}/blocking/{role}/copy", s.handleCopyBlocking)
				r.Get("/shots/{shotID}/state", s.handleShotState)
				r.With(canEdit).Patch("/shots/{shotID}/state/{endpoint}", s.handleSaveShotState)
				r.Get("/shots/{shotID}/transition-out", s.handleTransitionOut)
				r.With(canEdit).Put("/shots/{shotID}/transition-out", s.handleSaveTransitionOut)
				r.With(canEdit).Post("/shots/{shotID}/transition-out/derive-next", s.handleDeriveNext)
				r.With(canEdit).Post("/shots/{shotID}/cues", s.handleCreateCue)
				r.With(canEdit).Patch("/cues/{cueID}", s.handleUpdateCue)
				r.With(canEdit).Delete("/cues/{cueID}", s.handleDeleteCue)
				r.With(canEdit).Post("/cues/{cueID}/move", s.handleMoveCue)
				r.Get("/animatic", s.handleAnimatic)
				r.Get("/animatic/data", s.handleAnimaticData)
				r.With(canEdit).Patch("/animatic/settings", s.handleAnimaticSettings)
				r.With(canEdit).Post("/animatic/approve", s.handleAnimaticApprove)
				r.With(canEdit).Post("/metrics", s.handleSetMetric)
				r.Get("/drafts", s.handleDrafts)
				r.With(canEdit).Post("/drafts/sync", s.handleDraftsSync)
				r.Get("/shots/{shotID}/history", s.handleShotHistory)
				r.Get("/shots/{shotID}/prompt-workbench", s.handlePromptWorkbench)
				r.With(canEdit).Post("/shots/{shotID}/prompt-workbench", s.handlePromptSave)
				r.With(canEdit).Post("/shots/{shotID}/prompt-preview", s.handlePromptPreview)
				r.Get("/more", s.handleMore)
				r.Get("/produce", s.handleProduce)
				r.Get("/duration", s.handleDurationVideo)
				r.With(canEdit).Post("/duration", s.handleCreateDurationVideo)
				r.With(canEdit).Post("/duration/segments/{segmentID}/retry", s.handleRetrySegment)
				r.Get("/shots/{shotID}/generate/{role}", s.handleGenerateForm)
				r.With(canEdit).Post("/generation-requests", s.handleCreateRequest)
				r.Get("/jobs/{jobID}", s.handleJob)
				r.Get("/jobs/{jobID}/status", s.handleJobStatus)
				r.Get("/review", s.handleReview)
				r.With(canEdit).Post("/shots/{shotID}/selections", s.handleSelectAsset)
				r.With(canReview).Post("/assets/{assetID}/approvals", s.handleApprove)
				r.With(canReview).Post("/assets/{assetID}/assessments", s.handleAssess)
				r.With(canReview).Post("/annotations", s.handleCreateAnnotation)
				r.With(canReview).Patch("/annotations/{annotationID}", s.handleUpdateAnnotation)
				r.Get("/shots/{shotID}/candidates/{role}", s.handleCandidates)
				r.Get("/edit", s.handleEdit)
				r.With(canEdit).Post("/edits/{editID}/items", s.handleAddEditItem)
				r.With(canEdit).Patch("/edit-items/{itemID}", s.handleUpdateEditItem)
				r.With(canEdit).Post("/edit-items/{itemID}/move", s.handleMoveEditItem)
				r.With(canEdit).Delete("/edit-items/{itemID}", s.handleDeleteEditItem)
				r.With(canEdit).Post("/edits/{editID}/validate", s.handleValidateEdit)
				r.Get("/edits/{editID}/export", s.handleExportForm)
				r.With(canEdit).Post("/edits/{editID}/exports", s.handleCreateExport)
				r.Get("/exports/{exportID}", s.handleExport)
				r.Get("/compute", s.handleCompute)
				r.With(canEdit).Post("/campaigns", s.handleCreateCampaign)
				r.Get("/campaigns/{campaignID}", s.handleCampaign)
				r.With(canEdit).Post("/campaigns/{campaignID}/preflight", s.handleCampaignPreflight)
				r.With(canEdit).Post("/campaigns/{campaignID}/launch", s.handleCampaignLaunch)
				r.With(canEdit).Post("/campaigns/{campaignID}/pause", s.handleCampaignPause)
				r.With(canEdit).Post("/campaigns/{campaignID}/cancel", s.handleCampaignCancel)
				r.With(canEdit).Post("/campaigns/{campaignID}/reconcile", s.handleCampaignReconcile)
			})
			r.Get("/execution-targets", s.handleExecutionTargets)
			r.With(s.requireRole("owner")).Post("/execution-targets", s.handleCreateExecutionTarget)
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
			r.With(s.requireRole("owner")).Get("/settings/lan", s.handleLanStatus)
			r.With(s.requireRole("owner")).Post("/settings/lan/enable", s.handleLanEnable)
			r.With(s.requireRole("owner")).Post("/settings/lan/disable", s.handleLanDisable)
			r.With(s.requireRole("owner")).Post("/settings/lan/regenerate", s.handleLanRegenerate)
			r.With(s.requireRole("owner")).Post("/members", s.handleAddMember)
			r.With(s.requireRole("owner")).Patch("/members/{userID}", s.handleUpdateMember)
			r.With(s.requireRole("owner")).Delete("/members/{userID}", s.handleRemoveMember)
			r.Get("/diagnostics", s.handleDiagnostics)
			r.With(s.requireRole("owner")).Post("/diagnostics/backup", s.handleBackup)
			r.With(s.requireRole("owner")).Post("/diagnostics/backups/verify", s.handleVerifyBackup)
			r.With(s.requireRole("owner")).Post("/diagnostics/thumbnails", s.handleBackfillThumbnails)
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
