# Route authorization audit (v0.02, Phase 0.3)

Spec reference: v0.02 §13.1. Authorization is a release blocker, and CSRF
protection is **not** authorization.

## Role model

| Role | Permitted |
|---|---|
| Owner | Membership, services, execution targets, destructive administration, plus everything an editor may do |
| Editor | Creative and production mutations (structure, brief/outline, blocking, prompts, generation, edits, campaigns, project bible, workflows, model profiles) |
| Reviewer | Annotations, assessments and approvals only |
| Viewer | Read-only |

System administrators (`users.is_system_admin`) are treated as owners of any
workspace they open.

## Enforcement

* `requireAuth` — outer group; unauthenticated requests redirect to `/login`
  (303), or 401 for HTMX requests.
* `workspaceScope` — resolves `/w/{w}` membership and attaches the role;
  non-members get 403.
* `csrfProtect` — requires a valid session CSRF token on every non-GET request
  inside a workspace.
* `requireRole(...)` — applied per mutation route, *after* CSRF, so an
  unauthorized-but-authenticated request is always 403 regardless of body.

## Coverage

`internal/web/authz_test.go` (`TestAuthorizationMatrix`) enumerates every
mutation family and asserts the expected status for owner, editor, reviewer,
viewer, non-member and anonymous actors. Families covered:

* Discovery threads and brief/outline proposals (owner, editor)
* Section / scene / shot create, update, delete, move (owner, editor)
* Shot specs, blocking and prompt workbench (owner, editor)
* Generation requests and asset selection (owner, editor)
* Edit items, validation and exports (owner, editor)
* Campaigns and execution targets (campaigns: owner, editor; targets: owner)
* Annotations, assessments and approvals (owner, editor, reviewer)
* Membership, LAN settings, diagnostics/backup, workflows, model profiles,
  project bible, project/video creation and packaging (owner, or owner+editor)

## Notes

* `POST /settings/local-services/test` and `/unload` are self-scoped service
  probes that act on the caller's own configured endpoints; they are gated by
  `requireAuth` and do not mutate workspace state.
* Entity-scope checks (e.g. an asset that belongs to a different workspace)
  return 403 independently of role, and are exercised by the matrix through
  missing-entity paths.
