package main

import "fmt"

// genRoles generates the Chef roles for every server type not already in the base
// seed. Each role's run-list pulls in the cookbook that configures that function,
// so roles, cookbooks, and nodes form one coherent set.
func genRoles() map[string]map[string]any {
	roles := map[string]map[string]any{}
	for _, st := range serverTypes {
		if !st.newRole {
			continue
		}
		cb := newRoleCookbook[st.role]
		roles[st.role] = map[string]any{
			"name":                st.role,
			"description":         st.desc,
			"chef_type":           "role",
			"json_class":          "Chef::Role",
			"run_list":            []any{"recipe[chef-client]", "recipe[" + cb + "]"},
			"default_attributes":  map[string]any{},
			"override_attributes": map[string]any{},
			"env_run_lists":       map[string]any{},
		}
	}
	return roles
}

// genEnvironments generates the environments the base seed lacks: a QA tier and a
// disaster-recovery standby. (production/staging/development come from the base
// seed and are not regenerated, so their richer attributes are preserved.)
func genEnvironments() map[string]map[string]any {
	mk := func(name, desc string, override map[string]any) map[string]any {
		return map[string]any{
			"name":        name,
			"description": desc,
			"chef_type":   "environment",
			"json_class":  "Chef::Environment",
			"default_attributes": map[string]any{
				"chef_client": map[string]any{"interval": 1800, "splay": 300},
			},
			"override_attributes": override,
			"cookbook_versions":   map[string]any{},
		}
	}
	return map[string]map[string]any{
		"qa": mk("qa", "QA / pre-release verification fleet", map[string]any{
			"monitoring": map[string]any{"enabled": true, "scrape_interval": "30s"},
		}),
		"dr": mk("dr", "Disaster-recovery standby fleet (pdx)", map[string]any{
			"monitoring": map[string]any{"enabled": true, "scrape_interval": "60s"},
			"standby":    true,
		}),
	}
}

// cookbookSpec is the static description of one generated cookbook.
type cookbookSpec struct{ version, desc, pkg string }

// cookbookSpecs are the real-sounding cookbooks the generated roles install.
var cookbookSpecs = map[string]cookbookSpec{
	"gateway":          {"3.4.1", "API gateway and edge routing", "envoy"},
	"sidekiq":          {"7.2.0", "Sidekiq background job workers", "sidekiq"},
	"rabbitmq":         {"5.9.0", "RabbitMQ message broker", "rabbitmq-server"},
	"elasticsearch":    {"8.12.0", "Elasticsearch search cluster", "elasticsearch"},
	"envoy":            {"1.29.0", "Envoy reverse proxy", "envoy"},
	"fluentd":          {"6.1.0", "Fluentd log shipping", "td-agent"},
	"nfs":              {"5.0.1", "NFS network storage server", "nfs-kernel-server"},
	"bind":             {"9.18.0", "BIND DNS resolver", "bind9"},
	"postfix":          {"6.3.0", "Postfix SMTP relay", "postfix"},
	"vault":            {"4.1.0", "HashiCorp Vault secrets server", "vault"},
	"restic":           {"2.5.0", "Restic backup agent", "restic"},
	"openssh":          {"3.0.1", "Hardened OpenSSH bastion host", "openssh-server"},
	"active_directory": {"7.0.0", "Windows Active Directory domain services", "ActiveDirectory"},
}

func genCookbooks() map[string]cookbook {
	out := map[string]cookbook{}
	for name, s := range cookbookSpecs {
		recipe := fmt.Sprintf("# %s\npackage %q\n\nservice %q do\n  action [:enable, :start]\nend\n", s.desc, s.pkg, name)
		out[name] = cookbook{version: s.version, description: s.desc, recipe: recipe}
	}
	return out
}

// cookbookMetadata renders a cookbook's metadata.rb in the same shape as the base
// seed's cookbooks.
func cookbookMetadata(name string, cb cookbook) string {
	return fmt.Sprintf(`name '%s'
maintainer 'ACME Platform'
maintainer_email 'platform@acme.test'
license 'Apache-2.0'
description '%s'
version '%s'
chef_version '>= 16.0'
source_url 'https://github.com/acme/%s'
issues_url 'https://github.com/acme/%s/issues'
`, name, cb.description, cb.version, name, name)
}
