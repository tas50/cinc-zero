# dev/test-repo — a ready-to-run cinc-zero server state

This directory is a **full server-state** fixture: a populated `acme`
organization plus the global users and authz group that a plain chef-repo
can't express. Boot a realistic dev/demo server from it with:

```sh
make run-dev
# equivalently:
cinc-zero --no-auth --state dev/test-repo --key-out dev-admin.pem
```

It is loaded by the experimental `--state` flag (`server.Options.StatePath`),
which is a superset of `--repo`: it hydrates global users, every organization,
and each org's chef-objects **plus** authz groups. Data is read from disk at
runtime — nothing is embedded in the binary.

## Layout

```
dev/test-repo/
  users/                       global users: anna, ben
  organizations/
    acme/
      nodes/                   24 nodes (see "Fleet" below)
      roles/                   base, web, database, cache, loadbalancer,
                               monitoring, app, ci
      environments/            production, staging, development
      policies/                web-app-1.0.0, database-1.0.0 (Policyfile locks)
      policy_groups/           production, staging (pin both policies @ 1.0.0)
      groups/                  devs (authz group)
```

`--state` loads each org's `nodes/ roles/ environments/ clients/ policies/
policy_groups/ data_bags/ cookbooks/` through the same loader `--repo` uses,
then layers on `users/` (global) and per-org `groups/`.

## Fleet

24 nodes spanning three environments and both run-list (role-based) and
Policyfile (policy-based) management:

- **production** — web (×3), database (×2), cache, loadbalancer, monitoring
- **staging** — web, database, app, ci
- **development** — web, a bare box, a Windows box, database
- **policy-based** — 8 nodes pinned to the `web-app` / `database` policies via
  the `production` and `staging` policy groups (`chef_environment` `_default`,
  as real policy nodes use)

Run-lists reference only roles that exist here, and policy nodes reference only
policies pinned in their group — the `internal/state` seed test enforces that
there are no dangling references.

## Node automatic attributes — fauxhai provenance

Each node's `automatic` attributes are derived from real [fauxhai][] Ohai
dumps (`lib/fauxhai/platforms/<platform>/<version>.json`), trimmed to the
commonly-used subset (platform/kernel/os, cpu, memory, virtualization, network
identity, chef version) so the fixture stays a few KB per node. Per-node
identity (`fqdn`, `hostname`, `ipaddress`, `macaddress`) is stamped on top.

Platforms vendored (fauxhai `main`):

| Platform key | fauxhai source            | family |
|--------------|---------------------------|--------|
| ubuntu 22.04 | `ubuntu/22.04.json`       | debian |
| ubuntu 20.04 | `ubuntu/20.04.json`       | debian |
| debian 12    | `debian/12.json`          | debian |
| redhat 9     | `redhat/9.json`           | rhel   |
| rocky 9      | `rocky/9.json`            | rhel   |
| almalinux 9  | `almalinux/9.json`        | rhel   |
| amazon 2023  | `amazon/2023.json`        | amazon |
| suse 15      | `suse/15.json`            | suse   |
| windows 2022 | `windows/2022.json`       | windows|

[fauxhai]: https://github.com/chef/fauxhai

## Not yet expressed

Object **ACLs** (`acls/`) are not part of this fixture yet. The org's default
container ACLs are seeded automatically when the org is created; custom ACLs
can be added to the `--state` format in a follow-up once their on-disk shape is
pinned against the API handlers.
