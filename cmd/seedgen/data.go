package main

import (
	"fmt"
	"math/rand"
)

// genUsers returns 25 filler users with varied, realistic names. They are org
// members (so they populate the console's user list) but have no password and
// share the committed filler public key — they exist for list realism, not login.
func genUsers() []map[string]any {
	people := []struct{ first, last string }{
		{"Olivia", "Bennett"}, {"Liam", "Carter"}, {"Emma", "Diaz"}, {"Noah", "Foster"},
		{"Ava", "Greene"}, {"Ethan", "Howard"}, {"Sophia", "Ibrahim"}, {"Mason", "Jensen"},
		{"Isabella", "Kowalski"}, {"Lucas", "Lindqvist"}, {"Mia", "Moreau"}, {"Henry", "Nakamura"},
		{"Amelia", "Okafor"}, {"Jack", "Petrov"}, {"Harper", "Quinn"}, {"Leo", "Rossi"},
		{"Ella", "Santos"}, {"Benjamin", "Takahashi"}, {"Grace", "Ueno"}, {"Daniel", "Volkov"},
		{"Chloe", "Wang"}, {"Samuel", "Ximenes"}, {"Zoe", "Yamamoto"}, {"Oscar", "Zhang"},
		{"Nora", "Abboud"},
	}
	users := make([]map[string]any, 0, len(people))
	for _, p := range people {
		username := fmt.Sprintf("%s.%s", lower(p.first), lower(p.last))
		users = append(users, map[string]any{
			"username":     username,
			"display_name": p.first + " " + p.last,
			"first_name":   p.first,
			"last_name":    p.last,
			"email":        username + "@example.com",
			"public_key":   fillerKey,
		})
	}
	return users
}

// genApps returns the apps data bag expansion: a catalog of services with
// synthetic but plausible deployment metadata.
func genApps(rng *rand.Rand) map[string]map[string]any {
	names := []string{
		"auth-service", "billing", "search", "notifications", "gateway",
		"worker", "scheduler", "analytics", "cms", "payments",
		"inventory", "recommendations", "messaging", "profile", "reporting",
	}
	languages := []string{"go", "ruby", "python", "node", "java", "rust"}
	teams := []string{"platform", "payments", "growth", "data", "infra", "identity"}
	tierOf := func(n string) string {
		switch n {
		case "gateway", "cms", "profile", "search":
			return "frontend"
		default:
			return "backend"
		}
	}
	apps := map[string]map[string]any{}
	for _, n := range names {
		apps[n] = map[string]any{
			"id":          n,
			"repository":  "git@github.com:acme/" + n + ".git",
			"language":    languages[rng.Intn(len(languages))],
			"tier":        tierOf(n),
			"team":        teams[rng.Intn(len(teams))],
			"port":        8000 + rng.Intn(1000),
			"replicas":    2 + rng.Intn(11),
			"version":     fmt.Sprintf("%d.%d.%d", 1+rng.Intn(6), rng.Intn(20), rng.Intn(30)),
			"healthcheck": "/healthz",
			"cpu_limit":   fmt.Sprintf("%dm", 250*(1+rng.Intn(8))),
			"mem_limit":   fmt.Sprintf("%dMi", 256*(1+rng.Intn(8))),
			"feature_flags": map[string]any{
				"new_ui":        rng.Intn(2) == 1,
				"async_jobs":    rng.Intn(2) == 1,
				"rate_limiting": rng.Intn(2) == 1,
			},
		}
	}
	return apps
}

// genSecrets returns the secrets data bag expansion. All values are clearly
// synthetic (deterministic fake tokens, test-key prefixes) — never real secrets —
// but shaped to look like the real thing for UI/demo realism.
func genSecrets(rng *rand.Rand) map[string]map[string]any {
	secrets := map[string]map[string]any{
		"database": {
			"id": "database", "username": "app", "password": "fake-" + token(rng, 24),
			"host": "db-0001.acme.example.com", "port": 5432, "database": "acme_production",
		},
		"redis": {
			"id": "redis", "host": "cache-0001.acme.example.com", "port": 6379,
			"password": "fake-" + token(rng, 32),
		},
		"rabbitmq": {
			"id": "rabbitmq", "username": "acme", "password": "fake-" + token(rng, 24), "vhost": "/prod",
		},
		// NOTE: every value here is a clearly-synthetic placeholder. Vendor key
		// formats (Stripe sk_/pk_, AWS AKIA…, GitHub/Datadog keys) are
		// deliberately broken with "demo" markers so secret scanners do not flag
		// the committed fixture — these are not, and must never look like, real
		// credentials.
		"aws": {
			"id": "aws", "access_key_id": "DEMOACCESSKEYID" + upperToken(rng, 5),
			"secret_access_key": "demo-" + token(rng, 36), "region": "us-east-1", "bucket": "acme-prod-assets",
		},
		"stripe": {
			"id": "stripe", "publishable_key": "pk_demo_" + token(rng, 24),
			"secret_key": "sk_demo_" + token(rng, 24),
		},
		"smtp": {
			"id": "smtp", "host": "smtp.example.com", "port": 587,
			"username": "no-reply@example.com", "password": "fake-" + token(rng, 20),
		},
		"oauth_github": {
			"id": "oauth_github", "client_id": "demo-app-" + token(rng, 12),
			"client_secret": "demo-" + token(rng, 36),
		},
		"jwt": {
			"id": "jwt", "algorithm": "HS256", "signing_key": "demo-" + token(rng, 60),
		},
		"datadog": {
			"id": "datadog", "api_key": "demo-" + token(rng, 28), "app_key": "demo-" + token(rng, 36),
		},
		"tls_wildcard": {
			"id": "tls_wildcard", "common_name": "*.acme.example.com",
			"certificate": "-----BEGIN CERTIFICATE-----\nFAKE" + upperToken(rng, 40) + "\n-----END CERTIFICATE-----",
			"private_key": "-----BEGIN PRIVATE KEY-----\nFAKE" + upperToken(rng, 40) + "\n-----END PRIVATE KEY-----",
		},
	}
	return secrets
}

const hexDigits = "0123456789abcdef"

func token(rng *rand.Rand, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = hexDigits[rng.Intn(16)]
	}
	return string(b)
}

func upperToken(rng *rand.Rand, n int) string {
	const alpha = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	b := make([]byte, n)
	for i := range b {
		b[i] = alpha[rng.Intn(len(alpha))]
	}
	return string(b)
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
