package web

import (
	"context"

	"vidbuild/internal/store"
)

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxSession
	ctxWorkspace
	ctxRole
	ctxMember
)

func withUser(ctx context.Context, u *store.User) context.Context {
	return context.WithValue(ctx, ctxUser, u)
}

// UserFrom returns the authenticated user, or nil.
func UserFrom(ctx context.Context) *store.User {
	u, _ := ctx.Value(ctxUser).(*store.User)
	return u
}

func withSession(ctx context.Context, s *store.Session) context.Context {
	return context.WithValue(ctx, ctxSession, s)
}

// SessionFrom returns the current session, or nil.
func SessionFrom(ctx context.Context) *store.Session {
	s, _ := ctx.Value(ctxSession).(*store.Session)
	return s
}

func withWorkspace(ctx context.Context, w *store.Workspace, role string) context.Context {
	ctx = context.WithValue(ctx, ctxWorkspace, w)
	return context.WithValue(ctx, ctxRole, role)
}

// WorkspaceFrom returns the scoped workspace, or nil.
func WorkspaceFrom(ctx context.Context) *store.Workspace {
	w, _ := ctx.Value(ctxWorkspace).(*store.Workspace)
	return w
}

// RoleFrom returns the actor's role in the scoped workspace.
func RoleFrom(ctx context.Context) string {
	r, _ := ctx.Value(ctxRole).(string)
	return r
}
