# cinc-zero

A fully in-memory [Chef Infra Server](https://docs.chef.io/server/) implemented
in Go, for use in test pipelines. It speaks the real Chef Infra Server API and
authenticates real `chef-client` / `knife` / `cinc` clients using genuine
[Mixlib::Authentication](https://github.com/chef/mixlib-authentication) signed
requests, but keeps everything in memory, so it starts instantly and leaves
nothing behind.

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

See [`docs/superpowers/specs`](docs/superpowers/specs) for the full design.

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

ACLs and group membership are stored but not enforced by default; every
authenticated actor is permitted, which keeps test pipelines friction-free. To
exercise authorization-dependent behavior (requests a real server answers with
`403 Forbidden`), set `Options{EnforceACL: true}`. Enforcement honors the
default groups/ACLs seeded at org creation, resolves actor membership through
nested groups, and checks authentication → existence → authorization in that
order (so a missing object reports `404`, not `403`). Enforcement covers the
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

Pass `--enforce-acls` to turn on ACL enforcement (off by default; see the
`EnforceACL` option above). Pass `--no-auth` to disable signature verification
(the two are mutually exclusive; enforcement needs an authenticated actor).

Pass `--repo ./chef-repo` to preload an on-disk chef-repo (its `nodes/`,
`roles/`, `environments/`, `clients/`, `policies/`, `policy_groups/`,
`data_bags/`, and `cookbooks/`) into the first org at startup, mirroring
`knife upload`. Files under `policies/` are Policyfile locks (named
`<name>-<revision>.json`); each loads as a policy revision keyed by its
`revision_id`, and `policy_groups/<group>.json` pins policies to a group.
Cookbook directories are checksummed into the blob store and served with a
synthesized manifest.

## Docker

```sh
docker build -t cinc-zero .
docker run -p 8889:8889 cinc-zero
```

Release images are published to GitHub Container Registry:

```sh
docker run -p 8889:8889 ghcr.io/tas50/cinc-zero:latest
```

## Development

```sh
go test ./... -race -cover
```

Authentication golden vectors under `internal/auth/testdata` are generated from
the real `mixlib-authentication` gem via `ruby gen_vectors.rb`, guaranteeing
byte-for-byte compatibility with Chef clients.

### Conformance

A build-tagged suite drives the real **`knife` CLI** (from Cinc Workstation)
against an in-process cinc-zero server, exercising signed reads/writes, search,
and the cookbook sandbox/upload flow:

```sh
make conformance        # needs knife from Cinc Workstation: https://omnitruck.cinc.sh/install.sh
```

It skips automatically when no runnable `knife` is present, and runs in CI
(`.github/workflows/conformance.yml`) after installing Cinc Workstation via
omnitruck.

## License

cinc-zero is licensed under the [Business Source License 1.1](LICENSE).
