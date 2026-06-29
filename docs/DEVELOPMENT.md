# Developing cinc-zero

Building, testing, the dev fixtures, and running a fully-populated local server
you can point a management console at.

## Build and test

```sh
make build          # compile ./cinc-zero (version metadata via ldflags)
make test           # go test ./... -race -cover (the full suite)
make vet            # go vet ./...
make fmt            # gofmt -w .
```

Always run `make test && make vet` before committing. Development is strict TDD:
write a failing test first.

Authentication golden vectors under `internal/auth/testdata` are generated from
the real `mixlib-authentication` gem via `ruby gen_vectors.rb`, guaranteeing
byte-for-byte compatibility with Chef clients.

## Conformance

A build-tagged suite drives the real **`knife` CLI** (from Cinc Workstation)
against an in-process cinc-zero server, exercising signed reads/writes, search,
and the cookbook sandbox/upload flow:

```sh
make conformance    # needs knife from Cinc Workstation: https://omnitruck.cinc.sh/install.sh
```

It skips automatically when no runnable `knife` is present, and runs in CI
(`.github/workflows/conformance.yml`) after installing Cinc Workstation via
omnitruck.

## Dev fixtures

Two representations of the same realistic dataset live under `dev/`:

- **`dev/test-repo/`** — the canonical, diffable seed: a full server-state
  directory (the `--state` format) with global users, the `acme` organization,
  ~100 nodes, roles, environments, data bags, cookbooks, Policyfiles/policy
  groups, authz groups, and org membership. This is the source of truth; edit it
  as text.
- **`dev/cinc-dev.db`** — a SQLite database **generated from** `dev/test-repo`
  via `make dev-db`. It's a build artifact (git-ignored), so it never drifts from
  the canonical seed. Use it to run a durable server that already has all the dev
  data in it.

`make dev-db` simply bakes the seed into the database and exits, using the
`--init` flag (which seeds the store and exits without serving — also handy for
pre-baking a database into a container image):

```sh
make dev-db
# equivalently:
./cinc-zero --storage sqlite --db dev/cinc-dev.db --state dev/test-repo \
  --key-out dev-admin.pem --init
```

## Running a dev server

```sh
# In-memory, no auth — fastest for poking at the API with curl/knife:
make run-dev            # --no-auth --state dev/test-repo --key-out dev-admin.pem

# Durable SQLite, auth ON — what a management console needs (builds the DB first
# if missing):
make run-dev-sqlite     # --storage sqlite --db dev/cinc-dev.db --key-out dev-admin.pem
```

Both write the bootstrap admin private key to `./dev-admin.pem`. With the SQLite
backend that key is **stable across restarts** (it's persisted in the database),
so a console configured once keeps working as you stop and start the server.

## Test accounts

The seed ships these credentials (all members of org **`acme`**):

| User      | Password   | Role                                                    |
|-----------|------------|---------------------------------------------------------|
| `anna`    | `anna123`  | Regular user, member of `acme` (and its `devs` group).  |
| `ben`     | _(none)_   | Regular user, member of `acme`; no password set.        |
| `pivotal` | _(none)_   | Bootstrap **superuser** (key-based). Its private key is `dev-admin.pem`. |

`pivotal` authenticates with the key in `dev-admin.pem`, not a password. `anna`
can sign in with a password, which is what a console's user login uses:

```sh
curl -X POST http://127.0.0.1:8889/authenticate_user \
  -d '{"username":"anna","password":"anna123"}'
# => {"status":"linked", ...}
```

## Connecting cinc-console

[cinc-console](https://github.com/cinc-project) (the Cinc build of Chef Manage)
signs requests on a user's behalf using the **webui key** via the
`X-Ops-Request-Source: web` mechanism, which cinc-zero supports. By default the
webui key **is** the bootstrap admin key, i.e. the `dev-admin.pem` written above —
no extra setup. (To use a distinct key instead, pass `--webui-key <path>`.)

1. Start the durable dev server with auth on:

   ```sh
   make run-dev-sqlite          # listens on http://127.0.0.1:8889 by default
   ```

2. Point the console at the server and give it the webui key:
   - **Chef server URL:** `http://127.0.0.1:8889/organizations/acme`
   - **webui key:** the contents of `./dev-admin.pem`

3. Sign in to the console as a seeded user — **`anna` / `anna123`** — to browse
   the `acme` org's nodes, roles, environments, data bags, cookbooks, and
   Policyfiles, with the server enforcing that user's view.

Run the server with `--enforce-acls` to have it answer with real `403`s where a
production Chef Infra Server would, so the console exercises authorization the
same way it would against the real thing.
