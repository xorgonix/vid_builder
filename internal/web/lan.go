package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"vidbuild/internal/auth"
	"vidbuild/internal/db"
	"vidbuild/internal/templates"
)

const lanEnabledKey = "lan_enabled"

// lanState guards the optional LAN listener and its pairing code.
type lanState struct {
	mu        sync.Mutex
	srv       *http.Server
	addr      string
	code      string
	codeUntil time.Time
}

// LoadLAN restores LAN availability from durable settings on boot.
func (s *Server) LoadLAN(ctx context.Context) {
	v, ok, err := s.store.GetSetting(ctx, lanEnabledKey)
	if err != nil || !ok || v != "1" {
		return
	}
	if err := s.StartLAN(ctx); err != nil {
		s.log.Warn("LAN restore failed", "err", err)
	} else {
		s.log.Info("LAN access restored", "addr", s.lanAddress())
	}
}

// StartLAN binds an explicit LAN listener and records the setting.
func (s *Server) StartLAN(ctx context.Context) error {
	s.lan.mu.Lock()
	defer s.lan.mu.Unlock()
	if s.lan.srv != nil {
		return nil
	}
	host := s.cfg.Server.LANBindHost
	if host == "" {
		host = "0.0.0.0"
	}
	port := s.cfg.Server.LANPort
	if port == 0 {
		port = 8972
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("LAN listen %s: %w", addr, err)
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	s.lan.srv = srv
	s.lan.addr = addr
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.log.Warn("LAN listener stopped", "err", err)
		}
	}()
	return s.store.SetSetting(ctx, lanEnabledKey, "1")
}

// StopLAN shuts the LAN listener and revokes outstanding pairing codes.
func (s *Server) StopLAN(ctx context.Context) error {
	s.lan.mu.Lock()
	srv := s.lan.srv
	s.lan.srv = nil
	s.lan.addr = ""
	s.lan.mu.Unlock()
	if srv != nil {
		sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}
	_ = s.store.RevokePairingTokens(ctx)
	return s.store.SetSetting(ctx, lanEnabledKey, "0")
}

func (l *lanState) enabled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l != nil && l.srv != nil
}

func (s *Server) lanAddress() string {
	s.lan.mu.Lock()
	defer s.lan.mu.Unlock()
	return s.lan.addr
}

func (s *Server) lanCode() (string, time.Time) {
	s.lan.mu.Lock()
	defer s.lan.mu.Unlock()
	return s.lan.code, s.lan.codeUntil
}

// issuePairingCode mints a short-lived, single-use pairing code.
func (s *Server) issuePairingCode(ctx context.Context, userID, workspaceID string) (string, time.Time, error) {
	raw := make([]byte, 5)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	code := strings.ToUpper(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
	sum := sha256.Sum256([]byte(code))
	if _, err := s.store.CreatePairingToken(ctx, userID, workspaceID, "LAN device", hex.EncodeToString(sum[:]), 10*time.Minute); err != nil {
		return "", time.Time{}, err
	}
	until := time.Now().Add(10 * time.Minute)
	s.lan.mu.Lock()
	s.lan.code = code
	s.lan.codeUntil = until
	s.lan.mu.Unlock()
	return code, until, nil
}

// lanURL is the address a phone should open, using the primary LAN IPv4 address.
func (s *Server) lanURL() string {
	addr := s.lanAddress()
	if addr == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	if host == "0.0.0.0" || host == "::" || host == "" {
		host = primaryLANIPv4()
	}
	if host == "" {
		host = "<this-machine-ip>"
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}

func primaryLANIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				return v4.String()
			}
		}
	}
	return ""
}

func (s *Server) handleLanStatus(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	shell := s.shell(r, "LAN access")
	shell.ActiveNav = "settings"
	code, until := s.lanCode()
	data := templates.LanData{
		Shell:   shell,
		Enabled: s.lan.enabled(),
		Address: s.lanURL(),
		Code:    code,
		Until:   until.Format("15:04"),
	}
	if m := r.URL.Query().Get("msg"); m != "" {
		data.Shell.Flash = m
		data.Shell.FlashKind = r.URL.Query().Get("kind")
	}
	_ = ws
	s.renderPage(w, r, templates.LanPage(data))
}

func (s *Server) handleLanEnable(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	user := UserFrom(r.Context())
	if err := s.StartLAN(r.Context()); err != nil {
		http.Redirect(w, r, "/w/"+ws.ID+"/settings/lan?msg="+urlMsg("Could not start LAN access: "+err.Error())+"&kind=error", http.StatusSeeOther)
		return
	}
	_, _, _ = s.issuePairingCode(r.Context(), user.ID, ws.ID)
	_ = s.store.Audit(r.Context(), ws.ID, user.ID, "lan.enable", "workspace", ws.ID, "enabled LAN access", "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/settings/lan?msg="+urlMsg("LAN access enabled. Pair a device with the code below.")+"&kind=success", http.StatusSeeOther)
}

func (s *Server) handleLanDisable(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	if err := s.StopLAN(r.Context()); err != nil {
		s.log.Warn("stop LAN", "err", err)
	}
	_ = s.store.Audit(r.Context(), ws.ID, actorID(r), "lan.disable", "workspace", ws.ID, "disabled LAN access", "{}")
	http.Redirect(w, r, "/w/"+ws.ID+"/settings/lan?msg="+urlMsg("LAN access disabled and pairing codes revoked.")+"&kind=info", http.StatusSeeOther)
}

func (s *Server) handleLanRegenerate(w http.ResponseWriter, r *http.Request) {
	ws := WorkspaceFrom(r.Context())
	if !s.lan.enabled() {
		http.Redirect(w, r, "/w/"+ws.ID+"/settings/lan?msg="+urlMsg("Enable LAN access first.")+"&kind=error", http.StatusSeeOther)
		return
	}
	if _, _, err := s.issuePairingCode(r.Context(), actorID(r), ws.ID); err != nil {
		s.log.Warn("issue pairing code", "err", err)
	}
	http.Redirect(w, r, "/w/"+ws.ID+"/settings/lan?msg="+urlMsg("New pairing code issued.")+"&kind=success", http.StatusSeeOther)
}

// handlePair exchanges a valid one-time code for a revocable device session.
func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("code")))
	if code == "" {
		s.renderError(w, r, http.StatusBadRequest, "Pairing code required", "Open the pairing link shown on the desktop, or enter the code.")
		return
	}
	sum := sha256.Sum256([]byte(code))
	token, err := s.store.ConsumePairingToken(r.Context(), hex.EncodeToString(sum[:]))
	if err != nil {
		s.renderError(w, r, http.StatusForbidden, "Invalid or expired code", "Pairing codes are single-use and expire after ten minutes. Ask the owner to issue a new one.")
		return
	}
	raw, hash := auth.NewToken()
	csrf := auth.NewCSRFSecret()
	expires := time.Now().Add(time.Duration(s.cfg.Auth.SessionTTLHours) * time.Hour)
	if _, err := s.store.CreateSession(r.Context(), token.UserID, hash[:], csrf, db.Format(expires), "LAN device"); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Could not start session", err.Error())
		return
	}
	s.setSessionCookie(w, raw, expires)
	if token.WorkspaceID.Valid && token.WorkspaceID.String != "" {
		http.Redirect(w, r, "/w/"+token.WorkspaceID.String, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/workspaces", http.StatusSeeOther)
}
