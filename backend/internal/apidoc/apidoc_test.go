package apidoc

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type inner struct {
	Lat float64 `json:"lat" binding:"latitude"`
}

type request struct {
	Phone    string     `json:"phone" binding:"required,e164" doc:"E.164"`
	Unit     string     `json:"unit" binding:"required,oneof=hour day"`
	Count    int32      `json:"count" binding:"omitempty,min=1,max=10"`
	Note     *string    `json:"note"`
	Tags     []string   `json:"tags" binding:"max=3"`
	At       time.Time  `json:"at"`
	ID       uuid.UUID  `json:"id"`
	Where    inner      `json:"where" binding:"required"`
	Ignored  string     `json:"-"`
	Optional *time.Time `json:"optional,omitempty"`
}

type respStruct struct {
	ID    uuid.UUID `json:"id"`
	Extra string    `json:"extra,omitempty"`
}

type query struct {
	Limit int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Q     string `form:"q" binding:"required"`
}

func build(t *testing.T) map[string]any {
	t.Helper()
	s := New(Info{Title: "T", Version: "1", ServerURL: "http://x"})
	s.Add("POST", "/api/v1/things/:id/items/:item_id", Op{Summary: "make", Request: request{}, Response: respStruct{}, Status: 201, Idempotent: true, OrgScoped: true, Tags: []string{"things"}})
	s.Add("GET", "/api/v1/things", Op{Summary: "list", Query: query{}, Response: []respStruct{}, Public: true})
	raw, err := s.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if !s.Has("POST", "/api/v1/things/:id/items/:item_id") || len(s.Operations()) != 2 {
		t.Fatal("operations not recorded")
	}
	return doc
}

func get(v any, path ...string) any {
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[p]
	}
	return v
}

func TestPathsAndParameters(t *testing.T) {
	doc := build(t)
	op := get(doc, "paths", "/api/v1/things/{id}/items/{item_id}", "post")
	if op == nil {
		t.Fatal("gin params not converted")
	}
	params, _ := json.Marshal(get(op, "parameters"))
	for _, want := range []string{`"name":"id"`, `"name":"item_id"`, `"format":"uuid"`, `"Idempotency-Key"`, `"X-Org-Id"`} {
		if !strings.Contains(string(params), want) {
			t.Errorf("parameters missing %s: %s", want, params)
		}
	}
	if get(op, "responses", "201") == nil || get(op, "responses", "401") == nil || get(op, "security") == nil {
		t.Error("success / auth responses missing")
	}
	list := get(doc, "paths", "/api/v1/things", "get")
	if get(list, "security") != nil || get(list, "responses", "401") != nil {
		t.Error("public operations must not require auth")
	}
	qp, _ := json.Marshal(get(list, "parameters"))
	if !strings.Contains(string(qp), `"name":"q","required":true`) || !strings.Contains(string(qp), `"maximum":100`) {
		t.Errorf("query params = %s", qp)
	}
}

func TestSchemasFromBindingTags(t *testing.T) {
	doc := build(t)
	sc := get(doc, "components", "schemas", "apidoc.request")
	req, _ := json.Marshal(get(sc, "required"))
	if string(req) != `["phone","unit","where"]` {
		t.Errorf("required = %s", req)
	}
	props := get(sc, "properties").(map[string]any)
	if _, ok := props["Ignored"]; ok {
		t.Error(`json:"-" must be skipped`)
	}
	checks := map[string]string{
		"phone": `"pattern"`, "unit": `"enum":["hour","day"]`, "count": `"maximum":10`,
		"note": `["string","null"]`, "tags": `"maxItems":3`, "at": `"date-time"`, "id": `"uuid"`, "where": `"$ref"`,
	}
	for field, want := range checks {
		b, _ := json.Marshal(props[field])
		if !strings.Contains(string(b), want) {
			t.Errorf("%s = %s, want %s", field, b, want)
		}
	}
	// Response structs: fields without omitempty are always present.
	resp, _ := json.Marshal(get(doc, "components", "schemas", "apidoc.respStruct", "required"))
	if string(resp) != `["id"]` {
		t.Errorf("response required = %s", resp)
	}
}

func TestOpenAPIPath(t *testing.T) {
	if got := OpenAPIPath("/a/:id/b/:org_id"); got != "/a/{id}/b/{org_id}" {
		t.Fatal(got)
	}
}
