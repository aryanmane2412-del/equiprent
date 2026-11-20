// Package apidoc builds the OpenAPI 3.1 document from the routes the modules
// register. Request/response schemas are derived from the Go DTO types by
// reflection (json tags, binding rules), so the published contract cannot
// drift from the code: a route exists in the spec if and only if it is served.
package apidoc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Op documents one operation.
type Op struct {
	Summary     string
	Description string
	// Request is a zero value of the JSON body type (nil for none).
	Request any
	// Query is a zero value of a struct with `form` tags (nil for none).
	Query any
	// Response is a zero value of the success body type (nil for no body).
	Response any
	// Status is the success status code (default 200, or 204 without Response).
	Status int
	// Public marks operations that need no bearer token.
	Public bool
	// Idempotent marks operations that require an Idempotency-Key header.
	Idempotent bool
	// OrgScoped marks operations that act for the organisation in X-Org-Id.
	OrgScoped bool
	Tags      []string
	// Deprecated, if set, flags the operation.
	Deprecated bool
}

// Info is the document header.
type Info struct {
	Title       string
	Version     string
	Description string
	ServerURL   string
}

// Spec accumulates operations. It is safe for concurrent use.
type Spec struct {
	mu      sync.Mutex
	info    Info
	paths   map[string]map[string]*operation
	schemas map[string]*Schema
	tags    []string
	// requestMode is set while generating request bodies / query params:
	// only binding:"required" marks a field required there.
	requestMode bool
}

// New creates an empty spec.
func New(info Info) *Spec {
	return &Spec{info: info, paths: map[string]map[string]*operation{}, schemas: map[string]*Schema{}}
}

var ginParam = regexp.MustCompile(`[:*]([A-Za-z0-9_]+)`)

// OpenAPIPath converts a Gin path ("/bookings/:id") to OpenAPI ("/bookings/{id}").
func OpenAPIPath(ginPath string) string { return ginParam.ReplaceAllString(ginPath, "{$1}") }

// Add registers an operation for method + Gin path.
func (s *Spec) Add(method, ginPath string, op Op) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := OpenAPIPath(ginPath)
	o := &operation{
		Summary:     op.Summary,
		Description: op.Description,
		OperationID: operationID(method, path),
		Tags:        op.Tags,
		Deprecated:  op.Deprecated,
		Responses:   map[string]*response{},
	}
	for _, t := range op.Tags {
		if !slices.Contains(s.tags, t) {
			s.tags = append(s.tags, t)
		}
	}
	for _, m := range ginParam.FindAllStringSubmatch(ginPath, -1) {
		p := &parameter{Name: m[1], In: "path", Required: true, Schema: &Schema{Type: "string"}}
		if m[1] == "id" || strings.HasSuffix(m[1], "_id") {
			p.Schema.Format = "uuid"
		}
		o.Parameters = append(o.Parameters, p)
	}
	if op.Query != nil {
		o.Parameters = append(o.Parameters, s.queryParams(reflect.TypeOf(op.Query))...)
	}
	if op.OrgScoped {
		o.Parameters = append(o.Parameters, &parameter{
			Name: "X-Org-Id", In: "header", Schema: &Schema{Type: "string", Format: "uuid"},
			Description: "Organisation to act for. Optional when you belong to exactly one.",
		})
	}
	if op.Idempotent {
		o.Parameters = append(o.Parameters, &parameter{
			Name: "Idempotency-Key", In: "header", Required: true,
			Schema:      &Schema{Type: "string", MinLength: ptr(8), MaxLength: ptr(128)},
			Description: "Client-generated key (UUID). Replays return the original response.",
		})
	}
	if op.Request != nil {
		s.requestMode = true
		o.RequestBody = &requestBody{Required: true, Content: map[string]mediaType{
			"application/json": {Schema: s.schemaFor(reflect.TypeOf(op.Request))},
		}}
		s.requestMode = false
	}

	status := op.Status
	if status == 0 {
		status = http.StatusOK
		if op.Response == nil {
			status = http.StatusNoContent
		}
	}
	ok := &response{Description: http.StatusText(status)}
	if op.Response != nil {
		ok.Content = map[string]mediaType{"application/json": {Schema: s.schemaFor(reflect.TypeOf(op.Response))}}
	}
	o.Responses[strconv.Itoa(status)] = ok

	problems := []int{http.StatusBadRequest, http.StatusTooManyRequests, http.StatusInternalServerError}
	if !op.Public {
		o.Security = []map[string][]string{{"bearerAuth": {}}}
		problems = append(problems, http.StatusUnauthorized, http.StatusForbidden)
	}
	if strings.Contains(path, "{") {
		problems = append(problems, http.StatusNotFound)
	}
	if method != http.MethodGet {
		problems = append(problems, http.StatusConflict)
	}
	for _, code := range problems {
		o.Responses[strconv.Itoa(code)] = &response{Ref: "#/components/responses/Problem" + strconv.Itoa(code)}
	}

	if s.paths[path] == nil {
		s.paths[path] = map[string]*operation{}
	}
	s.paths[path][strings.ToLower(method)] = o
}

// Has reports whether method + Gin path is documented.
func (s *Spec) Has(method, ginPath string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.paths[OpenAPIPath(ginPath)][strings.ToLower(method)]
	return ok
}

// Operations returns "METHOD /path" for every documented operation.
func (s *Spec) Operations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for p, ops := range s.paths {
		for m := range ops {
			out = append(out, strings.ToUpper(m)+" "+p)
		}
	}
	sort.Strings(out)
	return out
}

// JSON renders the OpenAPI document (stable key order, indented).
func (s *Spec) JSON() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	responses := map[string]any{}
	for _, code := range []int{400, 401, 403, 404, 409, 429, 500} {
		responses["Problem"+strconv.Itoa(code)] = map[string]any{
			"description": http.StatusText(code),
			"content": map[string]any{"application/problem+json": map[string]any{
				"schema": map[string]string{"$ref": "#/components/schemas/Problem"},
			}},
		}
	}
	schemas := map[string]any{"Problem": problemSchema()}
	for k, v := range s.schemas {
		schemas[k] = v
	}
	tags := make([]map[string]string, 0, len(s.tags))
	sorted := slices.Clone(s.tags)
	sort.Strings(sorted)
	for _, t := range sorted {
		tags = append(tags, map[string]string{"name": t})
	}
	doc := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title": s.info.Title, "version": s.info.Version, "description": s.info.Description,
		},
		"servers": []map[string]string{{"url": s.info.ServerURL}},
		"tags":    tags,
		"paths":   s.paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]string{"type": "http", "scheme": "bearer", "bearerFormat": "JWT"},
			},
			"responses": responses,
			"schemas":   schemas,
		},
	}
	return json.MarshalIndent(doc, "", "  ")
}

func operationID(method, path string) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(method))
	for _, seg := range strings.Split(path, "/") {
		seg = strings.Trim(seg, "{}")
		for _, part := range strings.FieldsFunc(seg, func(r rune) bool { return r == '-' || r == '_' || r == '.' }) {
			if part == "" {
				continue
			}
			b.WriteString(strings.ToUpper(part[:1]))
			b.WriteString(part[1:])
		}
	}
	return b.String()
}

func ptr[T any](v T) *T { return &v }

// ---- JSON Schema --------------------------------------------------------------

// Schema is the subset of JSON Schema 2020-12 used by OpenAPI 3.1 here.
type Schema struct {
	Ref                  string             `json:"$ref,omitempty"`
	Type                 any                `json:"type,omitempty"` // string or []string (nullable)
	Format               string             `json:"format,omitempty"`
	Description          string             `json:"description,omitempty"`
	Enum                 []string           `json:"enum,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	AdditionalProperties any                `json:"additionalProperties,omitempty"`
	Minimum              *float64           `json:"minimum,omitempty"`
	Maximum              *float64           `json:"maximum,omitempty"`
	MinLength            *int               `json:"minLength,omitempty"`
	MaxLength            *int               `json:"maxLength,omitempty"`
	MinItems             *int               `json:"minItems,omitempty"`
	MaxItems             *int               `json:"maxItems,omitempty"`
	Pattern              string             `json:"pattern,omitempty"`
}

var (
	timeType    = reflect.TypeFor[time.Time]()
	uuidType    = reflect.TypeFor[uuid.UUID]()
	nullUUID    = reflect.TypeFor[uuid.NullUUID]()
	rawJSONType = reflect.TypeFor[json.RawMessage]()
)

func (s *Spec) schemaFor(t reflect.Type) *Schema {
	nullable := false
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
		nullable = true
	}
	sc := s.baseSchema(t)
	if nullable && sc.Ref == "" {
		sc.Type = []string{fmt.Sprint(sc.Type), "null"}
	}
	return sc
}

func (s *Spec) baseSchema(t reflect.Type) *Schema {
	switch t {
	case timeType:
		return &Schema{Type: "string", Format: "date-time"}
	case uuidType:
		return &Schema{Type: "string", Format: "uuid"}
	case nullUUID:
		return &Schema{Type: []string{"string", "null"}, Format: "uuid"}
	case rawJSONType:
		return &Schema{}
	}
	if enum, ok := enumValues(t); ok {
		return &Schema{Type: "string", Enum: enum}
	}
	switch t.Kind() {
	case reflect.String:
		return &Schema{Type: "string"}
	case reflect.Bool:
		return &Schema{Type: "boolean"}
	case reflect.Int, reflect.Int64, reflect.Uint64, reflect.Uint:
		return &Schema{Type: "integer", Format: "int64"}
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return &Schema{Type: "integer", Format: "int32"}
	case reflect.Float32, reflect.Float64:
		return &Schema{Type: "number"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return &Schema{Type: "string", Format: "byte"}
		}
		return &Schema{Type: "array", Items: s.schemaFor(t.Elem())}
	case reflect.Map:
		return &Schema{Type: "object", AdditionalProperties: s.schemaFor(t.Elem())}
	case reflect.Interface:
		return &Schema{}
	case reflect.Struct:
		name := schemaName(t)
		if _, seen := s.schemas[name]; !seen {
			s.schemas[name] = &Schema{Type: "object"} // placeholder breaks recursion
			s.schemas[name] = s.structSchema(t)
		}
		return &Schema{Ref: "#/components/schemas/" + name}
	}
	return &Schema{}
}

func (s *Spec) structSchema(t reflect.Type) *Schema {
	sc := &Schema{Type: "object", Properties: map[string]*Schema{}}
	s.addFields(sc, t)
	if len(sc.Required) == 0 {
		sc.Required = nil
	}
	return sc
}

func (s *Spec) addFields(sc *Schema, t reflect.Type) {
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, opts := tagName(f.Tag.Get("json"))
		if name == "-" {
			continue
		}
		if f.Anonymous && name == "" {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				s.addFields(sc, ft)
				continue
			}
		}
		if name == "" {
			name = f.Name
		}
		fs := s.schemaFor(f.Type)
		if fs.Ref != "" && (f.Tag.Get("doc") != "" || f.Tag.Get("binding") != "") {
			fs = &Schema{Ref: fs.Ref} // keep $ref pure; constraints don't apply to objects here
		}
		if d := f.Tag.Get("doc"); d != "" {
			if fs.Ref != "" {
				fs = &Schema{Ref: fs.Ref, Description: d}
			} else {
				fs.Description = d
			}
		}
		required := applyBinding(fs, f.Tag.Get("binding"))
		if !s.requestMode && !required && !strings.Contains(opts, "omitempty") && f.Type.Kind() != reflect.Pointer && f.Tag.Get("binding") == "" {
			// Response fields without omitempty are always present.
			required = true
		}
		if required {
			sc.Required = append(sc.Required, name)
		}
		sc.Properties[name] = fs
	}
}

func (s *Spec) queryParams(t reflect.Type) []*parameter {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	var out []*parameter
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous {
			out = append(out, s.queryParams(f.Type)...)
			continue
		}
		name, _ := tagName(f.Tag.Get("form"))
		if name == "" || name == "-" {
			continue
		}
		ps := s.schemaFor(f.Type)
		required := applyBinding(ps, f.Tag.Get("binding"))
		out = append(out, &parameter{Name: name, In: "query", Required: required, Schema: ps, Description: f.Tag.Get("doc")})
	}
	return out
}

// applyBinding maps validator rules onto the schema and reports "required".
func applyBinding(sc *Schema, binding string) bool {
	required := false
	isString := sc.Type == "string"
	isArray := sc.Type == "array"
	for _, rule := range strings.Split(binding, ",") {
		key, val, _ := strings.Cut(rule, "=")
		switch key {
		case "required":
			required = true
		case "oneof":
			sc.Enum = strings.Fields(val)
		case "min", "gte":
			setBound(sc, val, isString, isArray, true)
		case "max", "lte":
			setBound(sc, val, isString, isArray, false)
		case "len":
			setBound(sc, val, isString, isArray, true)
			setBound(sc, val, isString, isArray, false)
		case "email":
			sc.Format = "email"
		case "uuid", "uuid4", "uuid7":
			sc.Format = "uuid"
		case "e164":
			sc.Pattern = `^\+[1-9][0-9]{7,14}$`
		case "pincode":
			sc.Pattern = `^[1-9][0-9]{5}$`
		case "statecode":
			sc.Pattern = `^[0-9]{2}$`
		case "latitude":
			sc.Minimum, sc.Maximum = ptr(-90.0), ptr(90.0)
		case "longitude":
			sc.Minimum, sc.Maximum = ptr(-180.0), ptr(180.0)
		}
	}
	return required
}

func setBound(sc *Schema, val string, isString, isArray, lower bool) {
	n, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return
	}
	switch {
	case isString && lower:
		sc.MinLength = ptr(int(n))
	case isString:
		sc.MaxLength = ptr(int(n))
	case isArray && lower:
		sc.MinItems = ptr(int(n))
	case isArray:
		sc.MaxItems = ptr(int(n))
	case lower:
		sc.Minimum = ptr(n)
	default:
		sc.Maximum = ptr(n)
	}
}

func tagName(tag string) (string, string) {
	name, opts, _ := strings.Cut(tag, ",")
	return name, opts
}

// enumValues detects sqlc-style enums (named string types with AllXValues()).
func enumValues(t reflect.Type) ([]string, bool) {
	if t.Kind() != reflect.String || t.PkgPath() == "" {
		return nil, false
	}
	m, ok := reflect.New(t).Elem().Interface().(interface{ EnumValues() []string })
	if ok {
		return m.EnumValues(), true
	}
	return nil, false
}

var genericArgs = regexp.MustCompile(`\[(.*)\]`)

func schemaName(t reflect.Type) string {
	pkg := t.PkgPath()
	if i := strings.LastIndexByte(pkg, '/'); i >= 0 {
		pkg = pkg[i+1:]
	}
	name := t.Name()
	if m := genericArgs.FindStringSubmatch(name); m != nil {
		inner := m[1]
		if i := strings.LastIndexByte(inner, '/'); i >= 0 {
			inner = inner[i+1:]
		}
		name = strings.Replace(name, m[0], "_"+strings.ReplaceAll(inner, ".", "_"), 1)
	}
	if pkg == "" {
		return name
	}
	return pkg + "." + name
}

func problemSchema() map[string]any {
	str := map[string]string{"type": "string"}
	return map[string]any{
		"type":     "object",
		"required": []string{"type", "title", "status", "code"},
		"properties": map[string]any{
			"type": str, "title": str, "code": str, "detail": str, "instance": str, "request_id": str,
			"status": map[string]string{"type": "integer"},
			"errors": map[string]any{"type": "array", "items": map[string]any{
				"type": "object", "required": []string{"field", "code"},
				"properties": map[string]any{"field": str, "code": str, "message": str},
			}},
		},
	}
}

// ---- document types -----------------------------------------------------------

type operation struct {
	Summary     string                `json:"summary,omitempty"`
	Description string                `json:"description,omitempty"`
	OperationID string                `json:"operationId"`
	Tags        []string              `json:"tags,omitempty"`
	Deprecated  bool                  `json:"deprecated,omitempty"`
	Parameters  []*parameter          `json:"parameters,omitempty"`
	RequestBody *requestBody          `json:"requestBody,omitempty"`
	Responses   map[string]*response  `json:"responses"`
	Security    []map[string][]string `json:"security,omitempty"`
}

type parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Required    bool    `json:"required,omitempty"`
	Description string  `json:"description,omitempty"`
	Schema      *Schema `json:"schema"`
}

type requestBody struct {
	Required bool                 `json:"required"`
	Content  map[string]mediaType `json:"content"`
}

type response struct {
	Ref         string               `json:"$ref,omitempty"`
	Description string               `json:"description,omitempty"`
	Content     map[string]mediaType `json:"content,omitempty"`
}

type mediaType struct {
	Schema *Schema `json:"schema"`
}
