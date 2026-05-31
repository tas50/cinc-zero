# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

cinc-zero is a fully in-memory Chef Infra Server in Go for test pipelines. It speaks the real Chef Infra Server API and authenticates unmodified `chef-client`/`knife`/`cinc` clients via genuine Mixlib::Authentication signed requests, keeping all state in memory. Fidelity to real Chef Infra Server behavior is the goal; Policyfiles/policy groups are first-class.

## Commands

- `make build` — compile the `cinc-zero` binary (version metadata via ldflags).
- `make test` — `go test ./... -race -cover` (the full suite).
- `make vet` / `make fmt` — `go vet ./...` / `gofmt -w .`.
- `make conformance` — drives the real `knife` CLI against an in-process server; needs Cinc Workstation installed and is gated behind `-tags conformance` (skipped by default).
- Single test: `go test ./internal/api/ -run TestName -v` (most logic lives in `internal/api`).
- `make run ARGS="--enforce-acls --orgs acme"` — build and run; flags: `--addr`, `--orgs` (CSV), `--admin`, `--no-auth`, `--enforce-acls`, `--repo`, `--key-out`.

Always run `make test && make vet` before committing. Development is strict TDD: write a failing test first.

## Architecture

Request flow and layering (each layer is a separate package; understanding the request path requires all of them):

```
cmd/cinc-zero (flag parsing)
  └─ server.New(Options)            server/        — bootstraps store+admin+orgs, wires middleware
       authMiddleware               server/auth.go — verifies Mixlib signature (skipped if DisableAuth),
         └─ withAPIVersion          internal/api   — stores the actor in ctx via api.WithActor
              └─ authzMiddleware     (api, only when EnforceACL) — ACL/group enforcement
                   └─ withJSONErrors (api) — converts unrouted 404/405 to JSON
                        └─ mux       internal/api/api.go — http.ServeMux, one handler set per resource
```

- **`internal/store`** — the only state. `Store` holds a global space (collections `users`, `organizations`) plus per-org `Org`s. `Org.data` is `collection -> key -> raw JSON []byte` (e.g. `nodes`, `roles`, `acls`, `groups`, `association_users`); `Org.blobs` is `checksum -> bytes` (cookbook file store). Values are stored as canonical JSON so payloads round-trip exactly. Methods: `Get/Put/Create/Delete/Keys`, `PutBlob/Blob/HasBlob/DeleteBlob`.
- **`internal/api`** — all HTTP handlers, one file per resource (`nodes`/generic in `object.go`, `cookbooks.go`, `databags.go`, `policies.go`, `acl.go`, `authz.go`, `association*.go`, `search.go`, `keys.go`, `server_endpoints.go`, …). `api.Handler()` builds the mux; `register<Resource>Routes` registers each.
- **`internal/auth`** — Mixlib signed-header verification/signing (protocol 1.0/1.1/1.3), verified against the real gem.
- **`internal/search`** — in-process Solr-style query engine + Chef document flattener (no external search engine).
- **`internal/repo`** — loads an on-disk chef-repo (objects, data bags, cookbook dirs) into an org at startup.
- `internal/authz`, `cookbook`, `policyfile`, `repoloader`, `router` are **empty placeholder dirs** — ignore them; the real authz/cookbook/policy code is in `internal/api`.

## Conventions

- **Errors are always JSON.** Use `writeError(w, status, msg...)` → `{"error":[...]}`; never `http.Error`. Responses use `writeJSON` / `writeRaw` (`respond.go`). The `withJSONErrors` catch-all guarantees even unrouted 404/405 are JSON.
- **Handler shape:** `org := a.org(w, r)` (writes 404 and returns nil if the org is missing); read path params with `r.PathValue(...)`; resolve the actor (when needed) from context.
- **Authorization is structural by default, enforced opt-in.** ACLs/groups are stored and returned but only gate requests when `-enforce-acls` is set (`authz_enforce.go`); the bootstrap admin (`pivotal`) is a superuser. Don't assume enforcement in handlers.
- **API version negotiation** runs ahead of routing (`withAPIVersion`, `server_endpoints.go`): non-numeric `X-Ops-Server-API-Version` → 400, out-of-range → 406.
- **Tests:** API-layer tests use `newTestAPI(t)` + `do(t, method, url, body)` (no auth, raw store). Server-layer tests use `startServer(t, Options{})` + `signed(t, srv, …)` (full middleware, real signatures). Note `newTestAPI` does **not** seed default groups/ACLs — only `server.New`/`CreateOrganization` do.

The README status table is the authoritative feature map; package doc comments are accurate. Design specs live in `docs/superpowers/specs/`.
