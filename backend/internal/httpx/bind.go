package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
)

var (
	e164Pattern   = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
	objKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*/[A-Za-z0-9._/-]+$`)
	setupOnce     sync.Once
)

// SetupValidator configures Gin's validator once per process: field errors
// use JSON names and the custom tags below are registered.
//
//	e164       phone number in E.164 (+919876543210)
//	objkey     bucket-relative object key (mirrors SQL is_object_key)
//	statecode  two-digit GST state code
//	pincode    six-digit Indian PIN code
func SetupValidator() {
	setupOnce.Do(func() {
		v, ok := binding.Validator.Engine().(*validator.Validate)
		if !ok {
			return
		}
		v.RegisterTagNameFunc(func(f reflect.StructField) string {
			for _, tag := range []string{"json", "form", "uri"} {
				name := strings.SplitN(f.Tag.Get(tag), ",", 2)[0]
				if name == "-" {
					return ""
				}
				if name != "" {
					return name
				}
			}
			return f.Name
		})
		must(v.RegisterValidation("e164", func(fl validator.FieldLevel) bool {
			return e164Pattern.MatchString(fl.Field().String())
		}))
		must(v.RegisterValidation("objkey", func(fl validator.FieldLevel) bool {
			k := fl.Field().String()
			return objKeyPattern.MatchString(k) && !strings.Contains(k, "..") && len(k) <= 512
		}))
		must(v.RegisterValidation("statecode", func(fl validator.FieldLevel) bool {
			s := fl.Field().String()
			return len(s) == 2 && s[0] >= '0' && s[0] <= '9' && s[1] >= '0' && s[1] <= '9'
		}))
		must(v.RegisterValidation("pincode", func(fl validator.FieldLevel) bool {
			s := fl.Field().String()
			if len(s) != 6 || s[0] < '1' || s[0] > '9' {
				return false
			}
			for _, r := range s {
				if r < '0' || r > '9' {
					return false
				}
			}
			return true
		}))
	})
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// BindJSON decodes and validates the JSON body into dst. On failure it
// records a 400 problem (with per-field errors) and returns false.
func BindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		Fail(c, BindError(err))
		return false
	}
	return true
}

// BindQuery decodes and validates query parameters into dst.
func BindQuery(c *gin.Context, dst any) bool {
	if err := c.ShouldBindQuery(dst); err != nil {
		Fail(c, BindError(err))
		return false
	}
	return true
}

// BindError converts binding/decoding errors into API errors.
func BindError(err error) error {
	if ve, ok := errors.AsType[validator.ValidationErrors](err); ok {
		fields := make([]apperror.FieldError, 0, len(ve))
		for _, fe := range ve {
			fields = append(fields, apperror.FieldError{
				Field:   fieldPath(fe),
				Code:    fe.Tag(),
				Message: fieldMessage(fe),
			})
		}
		return apperror.ErrValidation.WithFields(fields...)
	}
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return apperror.ErrPayloadTooLarge.Wrap(err)
	}
	if errors.Is(err, io.EOF) {
		return apperror.ErrBadRequest.WithDetail("request body is required")
	}
	if se, ok := errors.AsType[*json.SyntaxError](err); ok {
		return apperror.ErrBadRequest.WithDetail(fmt.Sprintf("malformed JSON at offset %d", se.Offset))
	}
	if te, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
		return apperror.ErrValidation.WithFields(apperror.FieldError{
			Field: te.Field, Code: "type", Message: "must be " + te.Type.String(),
		})
	}
	return apperror.ErrBadRequest.WithDetail(err.Error())
}

// fieldPath drops the top-level struct name: "CreateBooking.period.start" -> "period.start".
func fieldPath(fe validator.FieldError) string {
	ns := fe.Namespace()
	if i := strings.IndexByte(ns, '.'); i >= 0 {
		return ns[i+1:]
	}
	return fe.Field()
}

func fieldMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required", "required_without", "required_with":
		return "is required"
	case "min", "gte":
		return "must be at least " + fe.Param()
	case "max", "lte":
		return "must be at most " + fe.Param()
	case "gt":
		return "must be greater than " + fe.Param()
	case "len":
		return "must have length " + fe.Param()
	case "oneof":
		return "must be one of: " + strings.ReplaceAll(fe.Param(), " ", ", ")
	case "uuid", "uuid4", "uuid7":
		return "must be a UUID"
	case "e164":
		return "must be an E.164 phone number such as +919876543210"
	case "email":
		return "must be a valid email address"
	case "gtfield":
		return "must be after " + fe.Param()
	case "latitude", "longitude":
		return "must be a valid " + fe.Tag()
	}
	return "is invalid (" + fe.Tag() + ")"
}

// UUIDParam parses a path parameter as a UUID or records a 400.
func UUIDParam(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		Fail(c, apperror.ErrValidation.WithFields(apperror.FieldError{
			Field: name, Code: "uuid", Message: "must be a UUID",
		}))
		return uuid.Nil, false
	}
	return id, true
}

// E164 reports whether s is an E.164 phone number.
func E164(s string) bool { return e164Pattern.MatchString(s) }
