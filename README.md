# cinc-zero

A fully in-memory [Chef Infra Server](https://docs.chef.io/server/) implemented
in Go, for use in test pipelines. It speaks the real Chef Infra Server API and
authenticates real `chef-client` / `knife` / `cinc` clients using genuine
[Mixlib::Authentication](https://github.com/chef/mixlib-authentication) signed
requests — but keeps everything in memory, so it starts instantly and leaves
nothing behind.

Compared to [chef-zero](https://github.com/chef/chef-zero), cinc-zero treats
**Policyfiles and policy groups** as first-class.

## Status

| Area | State |
|------|-------|
| In-memory store (org-scoped + global) | ✅ |
| Mixlib auth v1.0 / 1.1 / 1.3 (verified against the real gem) | ✅ |
| Nodes, roles, environments | ✅ |
| Clients, users (with key generation) | ✅ |
| Data bags + items | ✅ |
| Policyfiles, policy revisions, policy groups (deploy/pull) | ✅ |
| Multi-org + organization management API | ✅ |
| Embeddable library, standalone binary, Docker image | ✅ |
| Cookbooks + sandboxes + file store (upload/download, `_latest`, `_recipes`) | ✅ |
| Cookbook artifacts + `/universe` | ✅ |
| Search (in-process Solr query engine + Chef document expander) | ✅ |
| Authz groups / containers (structural) | ✅ |
| ACL endpoints (`_acl`, permissive/structural) | ✅ |
| Key management API (client/user named keys, v1) | ✅ |
| `authenticate_user`, user↔org association + invite flow (`association_requests`) | ✅ |
| Environment/role sub-endpoints (cookbook filtering, depsolve, recipes, nodes, run lists) | ✅ |
| Server endpoints (`_stats`, `required_recipe`, `principals`, API-version negotiation) | ✅ |
| chef-repo loader (JSON objects, data bags, cookbook dirs) | ✅ |

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

## Use as a binary

```sh
go build -o cinc-zero ./cmd/cinc-zero
./cinc-zero --addr 127.0.0.1:8889 --orgs test --key-out admin.pem
```

Pass `--repo ./chef-repo` to preload an on-disk chef-repo (its `nodes/`,
`roles/`, `environments/`, `clients/`, `policies/`, `policy_groups/`,
`data_bags/`, and `cookbooks/`) into the first org at startup, mirroring
`knife upload`. Cookbook directories are checksummed into the blob store and
served with a synthesized manifest.

## Docker

```sh
docker build -t cinc-zero .
docker run -p 8889:8889 cinc-zero
```

## Development

```sh
go test ./... -race -cover
```

Authentication golden vectors under `internal/auth/testdata` are generated from
the real `mixlib-authentication` gem via `ruby gen_vectors.rb`, guaranteeing
byte-for-byte compatibility with Chef clients.

## License

cinc-zero is licensed under the [Business Source License 1.1](LICENSE).
