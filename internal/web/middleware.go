package web

import (
	"net/http"
	"strings"
	"time"

	"vidbuild/internal/auth"
)

func (s *Server) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(s.cfg.Security.CookieName)
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		sess, err := s.store.GetSessionByTokenHash(r.Context(), auth.HashToken(c.Value))
		if err != nil {
			s.clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}
		user, err := s.store.GetUser(r.Context(), sess.UserID)
		if err != nil || user.Status != "active" {
			s.clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}
		ctx := withSession(r.Context(), sess)
		ctx = withUser(ctx, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserFrom(r.Context()) == nil {
			if r.Header.Get("HX-Request") == "true" {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) csrfProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		sess := SessionFrom(r.Context())
		if sess == nil {
			http.Error(w, "session required", http.StatusForbidden)
			return
		}
		token := r.Header.Get("X-CSRF-Token")
		if token == "" {
			token = r.FormValue("_csrf")
		}
		if !auth.CheckCSRF(sess.CSRFSecret, token) {
			if r.Header.Get("HX-Request") == "true" {
				http.Error(w, "invalid CSRF token", http.StatusForbidden)
				return
			}
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// workspaceScope resolves /w/{w} membership and attaches the workspace and role.
func (s *Server) workspaceScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFrom(r.Context())
		if user == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		wsID := chiURLParam(r, "w")
		ws, err := s.store.GetWorkspace(r.Context(), wsID)
		if err != nil {
			s.notFound(w, r)
			return
		}
		role, err := s.store.RoleFor(r.Context(), wsID, user.ID)
		if err != nil {
			if user.IsSystemAdmin {
				role = "owner"
			} else {
				s.forbidden(w, r)
				return
			}
		}
		ctx := withWorkspace(r.Context(), ws, role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireRole returns middleware rejecting actors without one of the roles.
func (s *Server) requireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !allowed[RoleFrom(r.Context())] {
				s.forbidden(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusNotFound, "Not found", "That page or resource does not exist.")
}

func (s *Server) forbidden(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusForbidden, "Not allowed", "You do not have permission to do that.")
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, code int, title, detail string) {
	w.WriteHeader(code)
	s.renderComponent(w, r, errorPage(title, detail, code))
}

func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic", "err", rec, "path", r.URL.Path)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(ww, r)
		s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", ww.status, "ms", time.Since(start).Milliseconds())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.Security.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.Security.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.Security.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.Security.CookieSecure,
		MaxAge:   -1,
	})
}

func actorID(r *http.Request) string {
	if u := UserFrom(r.Context()); u != nil {
		return u.ID
	}
	return ""
}

func isHX(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("HX-Request"), "true")
}

// methodOverride lets HTML forms issue PATCH/DELETE via a hidden _method field.
func (s *Server) methodOverride(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if m := strings.ToUpper(r.FormValue("_method")); m == http.MethodPatch || m == http.MethodDelete || m == http.MethodPut {
				r.Method = m
			}
		}
		next.ServeHTTP(w, r)
	})
}
