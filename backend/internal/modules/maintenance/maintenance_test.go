package maintenance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func ptr(v int64) *int64 { return &v }

func TestCosts(t *testing.T) {
	if got, err := costs(nil, ptr(4000), ptr(1500)); err != nil || *got != 5500 {
		t.Fatalf("sum = %v %v", got, err)
	}
	if got, err := costs(ptr(6000), ptr(4000), ptr(1500)); err != nil || *got != 6000 {
		t.Fatalf("total with GST/extras = %v %v", got, err)
	}
	if _, err := costs(ptr(5000), ptr(4000), ptr(1500)); err == nil {
		t.Fatal("a total below parts + labour must be refused")
	}
	if got, err := costs(nil, nil, nil); err != nil || got != nil {
		t.Fatalf("no costs = %v %v", got, err)
	}
}

func TestSummarise(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	r := summarise("2026-01-01", "2026-11-14", []CostRow{
		{EquipmentID: a, EquipmentTitle: "EX200", Kind: "service", Month: "2026-10", Jobs: 1, CostPaise: 1000000, DowntimeHours: 8},
		{EquipmentID: a, EquipmentTitle: "EX200", Kind: "repair", Month: "2026-11", Jobs: 2, CostPaise: 4500000, DowntimeHours: 30},
		{EquipmentID: b, EquipmentTitle: "JCB 3DX", Kind: "service", Month: "2026-11", Jobs: 1, CostPaise: 800000},
	})
	if r.TotalPaise != 6300000 || r.Jobs != 4 || r.DowntimeHours != 38 {
		t.Fatalf("totals = %+v", r)
	}
	if len(r.ByEquipment) != 2 || r.ByEquipment[0].Label != "EX200" || r.ByEquipment[0].CostPaise != 5500000 {
		t.Fatalf("by equipment = %+v", r.ByEquipment)
	}
	if len(r.ByKind) != 2 || len(r.ByMonth) != 2 || r.ByKind[0].Label != "Service" {
		t.Fatalf("by kind/month = %+v %+v", r.ByKind, r.ByMonth)
	}
}

func TestReminderText(t *testing.T) {
	today := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	for day, want := range map[int]string{
		0: "EX200: 500 h service is scheduled today.",
		1: "EX200: 500 h service is scheduled tomorrow.",
		3: "EX200: 500 h service is scheduled on Tue 17 Nov.",
	} {
		if got := ReminderText("EX200", "500 h service", today.AddDate(0, 0, day), today); got != want {
			t.Errorf("day %d: %q", day, got)
		}
	}
}

func TestWindow(t *testing.T) {
	if window(pgtype.Range[pgtype.Timestamptz]{}) != nil {
		t.Fatal("NULL range must give no window")
	}
	s, e := time.Now().UTC().Truncate(time.Second), time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	w := window(pgtype.Range[pgtype.Timestamptz]{Lower: pgtype.Timestamptz{Time: s, Valid: true}, Upper: pgtype.Timestamptz{Time: e, Valid: true}, Valid: true})
	if w == nil || !w.Start.Equal(s) || !w.End.Equal(e) {
		t.Fatalf("window = %+v", w)
	}
	svc := &Service{now: time.Now}
	if err := svc.validWindow(&Window{Start: s.Add(-48 * time.Hour), End: s.Add(-24 * time.Hour)}, "scheduled"); err == nil {
		t.Fatal("an open job's downtime in the past must be refused")
	}
	if err := svc.validWindow(&Window{Start: s.Add(-48 * time.Hour), End: s.Add(-24 * time.Hour)}, "done"); err != nil {
		t.Fatalf("logging past downtime: %v", err)
	}
	if err := svc.validWindow(&Window{Start: s, End: s.Add(200 * 24 * time.Hour)}, "scheduled"); err == nil {
		t.Fatal("over-long downtime accepted")
	}
}

type conflictRepo struct {
	Repository
	conflicts []string
	created   bool
	blockErr  error
}

func (r *conflictRepo) Conflicts(context.Context, uuid.UUID, Window) ([]string, error) {
	return r.conflicts, nil
}

func (r *conflictRepo) CreateBlock(context.Context, uuid.UUID, Window, string, uuid.UUID) (uuid.UUID, error) {
	r.created = true
	return uuid.New(), r.blockErr
}

func (r *conflictRepo) SetBlock(context.Context, uuid.UUID, uuid.NullUUID) error { return nil }

func TestBlockRefusesBookedDowntime(t *testing.T) {
	repo := &conflictRepo{conflicts: []string{"ER-2026-0001"}}
	svc := &Service{repo: repo}
	err := svc.block(context.Background(), uuid.New(), uuid.New(), Window{Start: time.Now(), End: time.Now().Add(time.Hour)}, "engine service", uuid.New())
	if !errors.Is(err, apperror.ErrBookingOverlap) {
		t.Fatalf("expected booking overlap, got %v", err)
	}
	if repo.created {
		t.Fatal("calendar block created despite overlapping booking")
	}
}

func TestBlockCreatesCalendarReservation(t *testing.T) {
	repo := &conflictRepo{}
	svc := &Service{repo: repo}
	if err := svc.block(context.Background(), uuid.New(), uuid.New(), Window{Start: time.Now(), End: time.Now().Add(time.Hour)}, "inspection", uuid.New()); err != nil {
		t.Fatalf("block: %v", err)
	}
	if !repo.created {
		t.Fatal("calendar reservation was not created")
	}
}
