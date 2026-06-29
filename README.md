# cinc-zero

A lightweight, drop-in alternative to
[Chef Infra Server](https://docs.chef.io/server/), implemented in Go. It speaks
the real Chef Infra Server API and authenticates unmodified `chef-client` /
`knife` / `cinc` clients using genuine
[Mixlib::Authentication](https://github.com/chef/mixlib-authentication) signed
requests. Run it fully **in memory** for instant, disposable test servers, or
back it with **SQLite** for durable state that survives restarts — the same
server scaling from a throwaway CI fixture to lightweight production
infrastructure. It ships as a single static binary, with no Ruby runtime and no
external database to operate — packaged as a minimal container image, it drops
cleanly into modern orchestration such as Kubernetes (a persistent volume for the
SQLite database is all the state it needs).

## Why cinc-zero

**Complete Chef Infra Server API, including Policyfiles.** cinc-zero implements
the full surface a real client touches: nodes, roles, environments, clients,
users, data bags, cookbooks (sandboxes, file store, artifacts, `/universe`),
search, authz groups/containers, ACLs, key management, user↔org association and
invites, and multi-org management. Mixlib authentication (v1.0 / 1.1 / 1.3) is
verified byte-for-byte against the real gem, so unmodified `chef-client`,
`knife`, and `cinc` clients just work. **WebUI-key impersonation
(`X-Ops-Request-Source: web`)** is supported too, so a management console such as
cinc-console can sign on a user's behalf and have the server enforce that user's
ACLs. Crucially, **Policyfiles and policy groups are first-class**, with deploy
and pull flows that chef-zero leaves on the table.

**Performance.** Built in Go, cinc-zero handles requests concurrently across all
available cores. Its goroutine-per-request model scales naturally under the
parallel load that real test fleets generate, with none of the global-lock
contention that single-threaded Ruby servers hit. State lives entirely in
memory, so there's no database or disk round-trip in the request path. On top of
that, the hot code paths have been deliberately optimized (cached flattened
search documents and parsed actor keys, batched and scoped scans, and copy-free
reads) to keep search, auth, and object access fast even with large numbers of
nodes and concurrent clients.

**Easy installation: a single binary, no Ruby.** cinc-zero ships as one static
Go binary (or a Docker image) with zero Ruby or gem dependencies. Drop it in,
run it, and point your clients at it; embed it directly in Go tests as a
library. No bundler, no rbenv, no native extensions.

**Security: a tiny footprint to attack and to patch.** A production Chef Infra
Server is a large, multi-service stack — Erlang (oc_erchef), Ruby, PostgreSQL, a
search service, a message queue, and a reverse proxy — assembled from thousands
of dependencies that all have to be tracked, audited, and kept patched. cinc-zero
is the opposite: orders of magnitude less code, whose **only third-party
dependencies are the pure-Go SQLite driver and its support libraries** —
everything else (the Chef API, Mixlib authentication, search, and storage) is
written against the Go standard library, and those SQLite deps only run when you
choose the durable backend. There is no database server, message broker, or web server to run and
harden; no Ruby or Erlang runtime in the image; and the distroless/scratch
container has no shell or OS package manager to exploit. The result is a
dramatically smaller attack surface and far less dependency-patching toil than a
full Chef Infra Server — while still authenticating clients with the same genuine
Mixlib signed-request protocol, so the reduced footprint is not a reduced-security
shortcut.

See [`docs/specs`](docs/specs) for the full design.

## Use as a Go library

```go
import "github.com/tas50/cinc-zero/server"

srv, _ := server.New(server.Options{Orgs: []string{"test"}})
_ = srv.Start()
defer srv.Stop(context.Background())

baseURL  := srv.URL()                 // http://127.0.0.1:NNNNN
adminKey := srv.AdminKey()            // PEM private key for the admin user
adminID  := srv.AdminName()           // "pivotal"
// Sign requests with auth.SignRequest, or point knife/chef-client at baseURL.
```

For tests that don't want to sign requests, set `Options{DisableAuth: true}`.

As a Go library the zero value is permissive: ACLs and group membership are
stored but not enforced, so every authenticated actor is permitted and test
pipelines stay friction-free. To exercise authorization-dependent behavior
(requests a real server answers with `403 Forbidden`), set
`Options{EnforceACL: true}`. (The standalone `cinc-zero` binary takes the
opposite, production-leaning default — it **enforces** unless told otherwise; see
"Use as a binary".) Enforcement matches a real Chef Infra Server: the creator of
an object is granted full control of it, a registered client joins the org's
`clients` group and can create and manage its own node, and the standard
chef-client bootstrap works end to end. It honors the default groups/ACLs seeded
at org creation, resolves actor membership through nested groups, and checks
authentication → existence → authorization in that order (so a missing object
reports `404`, not `403`). Enforcement covers the
org-scoped object endpoints (nodes, roles, data bags, cookbooks, groups,
containers, …), the org's own `_acl`, and the global actor endpoints: the
`/users` collection is superuser-only (a user may still read or update its own
record), and `/users/<name>/_acl` is governed by the grant permission on that
user. The bootstrap admin is a superuser and bypasses ACLs, mirroring Chef's
`pivotal`. `EnforceACL` requires authentication and cannot be combined with
`DisableAuth`.

## Use as a binary

```sh
go build -o cinc-zero ./cmd/cinc-zero
./cinc-zero --addr 127.0.0.1:8889 --orgs test --key-out admin.pem
```

The binary **enforces ACLs by default** — a freshly bootstrapped org behaves like
a real Chef Infra Server, and the standard chef-client lifecycle (a validator
registers a client, which then creates and updates its own node) works out of the
box. Pass `--enforce-acls=false` for a permissive server where every authenticated
actor is allowed. Pass `--no-auth` to disable signature verification entirely
(this also disables enforcement, since it needs an authenticated actor); asking
for `--no-auth` together with an explicit `--enforce-acls` is a contradiction and
errors out.

Pass `--repo ./chef-repo` to preload an on-disk chef-repo (its `nodes/`,
`roles/`, `environments/`, `clients/`, `policies/`, `policy_groups/`,
`data_bags/`, and `cookbooks/`) into the first org at startup, mirroring
`knife upload`. Files under `policies/` are Policyfile locks (named
`<name>-<revision>.json`); each loads as a policy revision keyed by its
`revision_id`, and `policy_groups/<group>.json` pins policies to a group.
Cookbook directories are checksummed into the blob store and served with a
synthesized manifest.

## Persistence and storage

By default cinc-zero keeps all state in memory — the ephemeral "zero" experience
that needs no disk and resets on exit. To persist state across restarts, point it
at a SQLite database:

```sh
./cinc-zero --storage sqlite --db ./cinc.db
```

`--storage` accepts `memory` (default) or `sqlite`; `--storage sqlite` requires
`--db <path>`. Both flags also read from the environment
(`CINC_ZERO_STORAGE`, `CINC_ZERO_DB`), which is handy in containers. SQLite uses
the pure-Go [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) driver,
so the static binary and `scratch`/`distroless` images keep working with
`CGO_ENABLED=0`.

The storage layer is pluggable behind a small `store.Backend` interface
(`(org, collection, key) → bytes` plus a blob store), so PostgreSQL/RDS can be
added later as a driver swap rather than a rewrite.

**Restarts.** A SQLite-backed server is safe to stop and restart on the same
database: it reloads existing organizations and data instead of recreating them,
and the bootstrap admin/validator keys are persisted so the key written by
`--key-out` keeps authenticating after a restart. (The in-memory backend always
starts fresh.)

**Backups** are delegated to the backend — cinc-zero ships no backup subsystem.
For SQLite, take a consistent online copy while the server runs:

```sh
sqlite3 cinc.db "VACUUM INTO 'backup.db'"
```

or simply copy the `.db` file while the server is stopped. (A future Postgres/RDS
backend would use `pg_dump` or managed snapshots.)

**Upgrades** are forward-only: the SQLite schema carries a `schema_migrations`
version and any pending migrations are applied automatically at startup, so
upgrading the binary against an existing database just works. Because object
bodies are stored as opaque JSON, the schema is tiny and rarely changes.
Downgrading the binary against a newer database is not supported.

## Docker

```sh
docker build -t cinc-zero .
docker run -p 8889:8889 cinc-zero
```

Release images are published to GitHub Container Registry:

```sh
docker run -p 8889:8889 ghcr.io/tas50/cinc-zero:latest
```

To persist state across container restarts, mount a volume and point SQLite at
it (a single static binary on a `scratch`/`distroless` base, so the volume is the
only stateful piece):

```sh
docker run -p 8889:8889 -v cinc-data:/data \
  ghcr.io/tas50/cinc-zero:latest --storage sqlite --db /data/cinc.db
```

## Development

Building, testing, the `knife` conformance suite, the dev fixtures
(`dev/test-repo` and the SQLite database `make dev-db` bakes from it), running a
fully-seeded local server, the test account logins, and connecting a management
console such as cinc-console are all covered in
**[`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md)**. A quick taste:

```sh
make test             # go test ./... -race -cover
make run-dev          # in-memory, no auth, pre-loaded with the dev/test-repo seed
make run-dev-sqlite   # a durable SQLite copy of the same data, auth on (for cinc-console)
```

## License

cinc-zero is licensed under the [Business Source License 1.1](LICENSE).
