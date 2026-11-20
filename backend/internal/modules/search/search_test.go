package search

import (
	"context"
	"testing"
	"time"

	"github.com/equiprent/backend/internal/db"
)

func query() Query {
	start := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	return Query{Lat: 18.52, Lng: 73.85, Start: start, End: start.Add(48 * time.Hour)}
}

func TestParamsDefaults(t *testing.T) {
	p, limit, err := Params(query())
	if err != nil {
		t.Fatal(err)
	}
	if p.Unit != "day" || !p.WithOperator || p.MaxKm != 100 || p.Sort != "distance" || limit != 20 || p.ResultLimit != 21 || p.ResultOffset != 0 {
		t.Fatalf("params = %+v limit=%d", p, limit)
	}
	if p.CategorySlug != nil || p.Query != nil || p.MakeID.Valid {
		t.Fatal("unset filters must be NULL")
	}
}

func TestParamsFiltersAndEscaping(t *testing.T) {
	q := query()
	dry := false
	q.WithOperator, q.Category, q.Q, q.Cursor, q.Limit = &dry, "excavator", `50%_off\`, "40", 10
	p, _, err := Params(q)
	if err != nil {
		t.Fatal(err)
	}
	if p.WithOperator || *p.CategorySlug != "excavator" || p.ResultOffset != 40 || p.ResultLimit != 11 {
		t.Fatalf("params = %+v", p)
	}
	if *p.Query != `50\%\_off\\` {
		t.Fatalf("LIKE pattern not escaped: %q", *p.Query)
	}
}

func TestParamsRejectsBadInput(t *testing.T) {
	q := query()
	q.Cursor = "-5"
	if _, _, err := Params(q); err == nil {
		t.Error("negative cursor accepted")
	}
	q = query()
	q.End = q.Start.Add(200 * 24 * time.Hour)
	if _, _, err := Params(q); err == nil {
		t.Error("200-day window accepted")
	}
}

type stubRepo struct{ rows []Result }

func (s stubRepo) Search(context.Context, db.SearchListingsParams) ([]Result, error) {
	return s.rows, nil
}

func TestSearchPaginates(t *testing.T) {
	rows := make([]Result, 3)
	q := query()
	q.Limit = 2
	res, err := NewService(stubRepo{rows: rows}).Search(context.Background(), q)
	if err != nil || len(res.Data) != 2 || res.NextCursor != "2" {
		t.Fatalf("res = %+v %v", res, err)
	}
}
