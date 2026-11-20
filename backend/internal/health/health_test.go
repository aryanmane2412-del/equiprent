package health

import (
	"context"
	"errors"
	"testing"
	"time"
)

func ok(name string) Checker {
	return CheckFunc{CheckName: name, Fn: func(context.Context) error { return nil }}
}

func failing(name string, err error) Checker {
	return CheckFunc{CheckName: name, Fn: func(context.Context) error { return err }}
}

func TestReadyAllUp(t *testing.T) {
	r := NewService(time.Second, true, ok("postgres")).Disable("redis").Ready(context.Background())
	if r.Status != StatusUp {
		t.Fatalf("status = %s", r.Status)
	}
	if r.Checks["redis"].Status != StatusDisabled {
		t.Errorf("redis = %+v", r.Checks["redis"])
	}
}

func TestReadyDownHidesErrorWhenNotExposed(t *testing.T) {
	r := NewService(time.Second, false, ok("redis"), failing("postgres", errors.New("dial tcp 10.0.0.5:5432: refused"))).
		Ready(context.Background())
	if r.Status != StatusDown {
		t.Fatalf("status = %s", r.Status)
	}
	if got := r.Checks["postgres"].Error; got != "unavailable" {
		t.Errorf("error leaked: %q", got)
	}
}

func TestReadyTimesOutSlowChecks(t *testing.T) {
	slow := CheckFunc{CheckName: "slow", Fn: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	start := time.Now()
	r := NewService(50*time.Millisecond, true, slow).Ready(context.Background())
	if r.Status != StatusDown {
		t.Fatal("slow check should fail")
	}
	if time.Since(start) > time.Second {
		t.Fatal("timeout not applied")
	}
}
