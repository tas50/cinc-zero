# fauxhai-backed realism for the test-repo nodes

Date: 2026-06-29

## Problem

The 107 node fixtures under `dev/test-repo/organizations/acme/nodes/` carry a thin,
hand-built `automatic` block: every node reports `cpu.total: 1`, a generic
`"Intel(R) Xeon(R) CPU"` model, `memory.total: "1048576kB"`, and a five-key
`kernel`. There is no `dmi`, `filesystem`, `block_device`, or real `network`
interface data, so the fleet does not resemble Ohai output from a real fleet.
Nodes also lack the human-readable `uptime` string that Ohai emits alongside
`uptime_seconds` (the original request that prompted this work).

## Goal

Replace each node's `automatic` block with the structurally-rich attributes from
the matching [chef/fauxhai](https://github.com/chef/fauxhai) platform dump, while
preserving every per-node identity field and the realism contract the existing
`internal/state/seed_test.go` suite already pins (version drift, splayed
check-ins, bare nodes, curated platform versions, ≥4 platforms, count of 107).
Add the `uptime` string.

What fauxhai actually buys us is *structural* fidelity — real `dmi`,
`filesystem` (mount options, inodes), `block_device`, full `network` interfaces
with address families, `kernel.modules`, `lsb`. Its scalar values are normalized
placeholders (`cpu.total: 1`, `memory.total: "1048576kB"`, `ipaddress: 10.0.0.2`,
`macaddress: 11:11:11:11:11:11`) — which is in fact where the current fixtures'
fake-looking numbers originated. We keep fauxhai's scalars verbatim (decision
below); realism comes from the structure, not from inventing RAM figures.

## Platform → fauxhai mapping

fauxhai ships major versions only, so several curated patch versions map to a
major-version dump:

| fixture platform / version | fauxhai dump |
|---|---|
| ubuntu 20.04 | `ubuntu/20.04` |
| ubuntu 22.04 | `ubuntu/22.04` |
| debian 12 | `debian/12` |
| redhat 9.3 | `redhat/9` |
| rocky 9.1 | `rocky/9` |
| almalinux 9.1 | `almalinux/9` |
| amazon 2023 | `amazon/2023` |
| suse 15.2 | `suse/15` |
| windows (10.0.20348) | `windows/2022` |

## Merge rule

Build each node's new `automatic` by starting from the mapped fauxhai dump and
applying overlays. A curated subset of keys is kept — not the full ~70KB dump —
to keep the files reviewable (~15–25KB each).

**Preserved from the existing node (authoritative identity / contract):**
`fqdn`, `hostname`, `machinename`, `domain`, `ipaddress`, `macaddress`,
`platform_version`, `os_version`, `kernel.release`, `kernel.machine`,
`chef_packages` (drives the version-drift test), `ohai_time` (drives the
check-in-splay test), `uptime_seconds`, `virtualization` (the node's
`{system: kvm, role: guest}` beats fauxhai's empty `{systems: {}}`).

**Added:** `uptime` — computed from the node's own `uptime_seconds` using Ohai's
exact `seconds_to_human` algorithm, including the singular `"1 day …"` case:

```
days  = secs / 86400;  secs -= 86400*days
hours = secs / 3600;   secs -= 3600*hours
mins  = secs / 60;     secs -= 60*mins
days > 1  -> "%d days %02d hours %02d minutes %02d seconds"
days == 1 -> "%d day %02d hours %02d minutes %02d seconds"
hours > 0 -> "%d hours %02d minutes %02d seconds"
mins  > 0 -> "%d minutes %02d seconds"
else      -> "%d seconds"
```

**Taken from fauxhai (the realism):** `cpu`, `memory`, `dmi`, `filesystem`,
`block_device`, `network`, the rest of `kernel` (`name`, `version`, `modules`,
`processor`, `os`), `lsb`, `init_package`, `os`, `platform`, `platform_family`,
`languages`, `shells`. `dmi` and `block_device` are platform-dependent —
fauxhai ships an empty `dmi` for debian/12, rocky/9, and almalinux/9, and no
`block_device` for windows — so they appear wherever fauxhai provides them
rather than on every node.

**Dropped (bulky / volatile, not needed for fixtures):** `packages`, `keys`,
`counters`, `command`, `shard_seed`, `time`, `current_user`, `idle`,
`idletime_seconds`, `fips`. fauxhai's `filesystem` also triplicates the same
mounts across `by_device`, `by_mountpoint`, and `by_pair` (~24KB); only the
canonical `by_device` view is kept, landing nodes at ~24KB each instead of the
~70KB full dump.

**Identity consistency:** fauxhai keys network/dmi data by its placeholder
`10.0.0.2` / `11:11:11:11:11:11`. After overlay, token-replace those throughout
the merged block with the node's real ip / mac, so `network.interfaces` and any
dmi/address references carry the node's address rather than the placeholder.

### Scalars: keep fauxhai verbatim

`cpu` and `memory` scalars stay as fauxhai ships them (`cpu.total: 1`,
`memory.total: "1048576kB"`). Rationale: faithful to "use fauxhai data", fully
deterministic, invents no numbers; the realism is carried by structure.

## Method (strict TDD)

1. **Failing tests first**, added to `internal/state/seed_test.go`:
   - `TestSeedNodeUptime` — every node's `automatic.uptime` is a non-empty
     string equal to `ohaiUptime(uptime_seconds)`, where `ohaiUptime` is a small
     helper in the test mirroring the formatter above.
   - `TestSeedNodesHaveRichOhai` — every node's `automatic` carries non-empty
     `filesystem` and `network` (the signals every fauxhai platform ships;
     `dmi`/`block_device` are platform-dependent, so they are not asserted
     per-node); and the node's `ipaddress` and `macaddress` each appear within
     the serialized `network` block (proving the identity overlay propagated).
   - All existing seed tests must stay green.
2. **Generator** — a throwaway script in the session scratchpad (NOT committed;
   no fauxhai-source path leaks into the repo) reads a local fauxhai checkout and
   each node file, emits the merged file with 2-space indent and trailing
   newline.
3. `make test && make vet`.

## Out of scope

- No changes to Go source, the search engine, or the repo loader.
- Non-node fixtures (roles, environments, data bags, cookbooks, policies)
  untouched.
- Node `count` stays 107; the platform spread, version drift, check-in splay,
  and bare-node sets are all preserved by construction.
