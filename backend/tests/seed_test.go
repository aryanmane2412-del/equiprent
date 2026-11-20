package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/equiprent/backend/internal/seed"
)

// The development seed runs through the real API and is idempotent.
func TestDevelopmentSeed(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	first, err := seed.Run(ctx, e.router, e.pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	again, err := seed.Run(ctx, e.router, e.pool, nil)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if again.Created != 0 || again.Skipped != first.Created+first.Skipped {
		t.Fatalf("not idempotent: first %+v, again %+v", first, again)
	}

	// Approved seed listings are browsable with photos, in every category.
	for _, cat := range []string{"backhoe-loader", "excavator", "bulldozer", "crane", "dump-truck", "wheel-loader",
		"road-roller", "forklift", "concrete-mixer", "tractor", "other"} {
		r := must(t, e.call("GET", "/equipment?category="+cat+"&limit=100", "", "", nil, nil), 200, "browse "+cat)
		if len(r.list("data")) == 0 || !strings.Contains(r.raw, "cover_url") {
			t.Errorf("no seeded listing with photos in %s", cat)
		}
	}
	login := must(t, e.call("POST", "/auth/login", "", "", map[string]any{"identifier": "owner.pune@equiprent.dev", "password": seed.Password}, nil), 200, "seed owner login")
	owner := &user{e: e, token: login.str("access_token"), org: login.str("roles.0.organization_id")}
	mine := must(t, owner.do("GET", "/owner/equipment?limit=100", nil), 200, "seed owner listings")
	for _, st := range []string{`"status":"active"`, `"status":"pending_review"`} {
		if !strings.Contains(mine.raw, st) {
			t.Errorf("seed owner has no %s listing", st)
		}
	}
}
