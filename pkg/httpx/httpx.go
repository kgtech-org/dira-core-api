// Package httpx provides shared HTTP helpers: JSON writing, request decoding
// with validation, the standard error envelope (localized via i18n) and
// cursor pagination.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/i18n"
)

var validate = validator.New()

var translator *i18n.Translator

// SetTranslator installs the process-wide translator used to localize error
// messages. Called once at startup; without it English fallbacks are used.
func SetTranslator(t *i18n.Translator) { translator = t }

// JSON writes v as a JSON response with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			slog.Error("httpx: encode response", "error", err)
		}
	}
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error writes err using the standard envelope {"error":{"code","message"}},
// localizing the message from the request language when a translation exists.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	appErr := apperr.From(err)
	if appErr.HTTPStatus >= 500 {
		slog.ErrorContext(r.Context(), "request failed", "code", appErr.Code, "error", appErr.Error())
	}
	msg := appErr.Message
	if translator != nil {
		if localized, ok := translator.Translate(i18n.LangFromContext(r.Context()), "errors."+appErr.Code, appErr.Meta); ok {
			msg = localized
		}
	}
	JSON(w, appErr.HTTPStatus, errorBody{Error: errorDetail{Code: appErr.Code, Message: msg}})
}

// Decode parses the JSON body into dst and validates it ("validate" tags).
func Decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return apperr.Validation("invalid JSON body").WithCause(err)
	}
	if err := validate.Struct(dst); err != nil {
		var vErrs validator.ValidationErrors
		if errors.As(err, &vErrs) && len(vErrs) > 0 {
			fields := make([]string, 0, len(vErrs))
			for _, fe := range vErrs {
				fields = append(fields, fe.Field())
			}
			return apperr.Validation(fmt.Sprintf("invalid fields: %s", strings.Join(fields, ", "))).WithCause(err)
		}
		return apperr.Validation("invalid request").WithCause(err)
	}
	return nil
}

// Page holds cursor pagination parameters (?limit=20&cursor=<id>).
type Page struct {
	Limit  int
	Cursor string
}

const (
	defaultPageLimit = 20
	maxPageLimit     = 100
)

// PageFromRequest extracts pagination parameters with bounds.
func PageFromRequest(r *http.Request) Page {
	p := Page{Limit: defaultPageLimit, Cursor: r.URL.Query().Get("cursor")}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			p.Limit = min(n, maxPageLimit)
		}
	}
	return p
}

type listBody struct {
	Items      any    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// List writes the standard paginated list envelope.
func List(w http.ResponseWriter, items any, nextCursor string) {
	JSON(w, http.StatusOK, listBody{Items: items, NextCursor: nextCursor})
}
