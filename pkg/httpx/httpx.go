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
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/i18n"
	"github.com/kgtech-org/dira-core-api/pkg/phone"
)

var validate = newValidator()

// newValidator construit le validateur avec DEUX choix qui regardent le client :
//
//   - les champs sont nommés par leur clé JSON. Le validateur nomme les champs
//     Go (`Phone`, `FirstName`) ; une application qui reçoit « FirstName »
//     ne sait pas à quelle case le rattacher, elle envoie `first_name` ;
//   - `e164` exige le `+`. Celui de la bibliothèque le rend facultatif :
//     « 22899000001 » passait, était stocké tel quel, et devenait un second
//     compte à côté de « +22899000001 » — que la personne ne retrouvait plus.
func newValidator() *validator.Validate {
	v := validator.New()
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name, _, _ := strings.Cut(fld.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			return fld.Name
		}
		return name
	})
	_ = v.RegisterValidation("e164", func(fl validator.FieldLevel) bool {
		return phone.Valid(fl.Field().String())
	})
	return v
}

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
	// Fields nomme les clés JSON en cause dans un `validation_failed` — les
	// champs invalides, ou la clé inconnue. C'est ce qui permet à une
	// application de souligner LA case fautive au lieu d'afficher « vérifiez
	// vos informations » sous un formulaire entièrement correct.
	Fields []string `json:"fields,omitempty"`
	// Reason précise un `validation_failed` quand la cause n'est pas la valeur
	// d'un champ : `unknown_field` (une clé que le serveur ne connaît pas),
	// `invalid_json` (corps illisible).
	Reason string `json:"reason,omitempty"`
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
	detail := errorDetail{Code: appErr.Code, Message: msg}
	if fields, ok := appErr.Meta["fields"].([]string); ok {
		detail.Fields = fields
	}
	if reason, ok := appErr.Meta["reason"].(string); ok {
		detail.Reason = reason
	}
	JSON(w, appErr.HTTPStatus, errorBody{Error: detail})
}

// unknownFieldRe extrait la clé d'une erreur `json: unknown field "x"`.
// encoding/json ne l'expose pas autrement que dans le texte.
var unknownFieldRe = regexp.MustCompile(`json: unknown field "([^"]*)"`)

// Decode parses the JSON body into dst and validates it ("validate" tags).
func Decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	// ⚠️ Une clé inconnue est REFUSÉE, pas ignorée. Une application qui
	// envoie `first_name` à une route qui ne le lit pas croirait l'avoir
	// enregistré. Mais un refus qui ne NOMME pas la clé n'apprend rien : la
	// réponse la porte dans `fields`, avec `reason: unknown_field`.
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if m := unknownFieldRe.FindStringSubmatch(err.Error()); m != nil {
			return apperr.Validation("unknown field: " + m[1]).
				WithMeta(map[string]any{"fields": []string{m[1]}, "reason": "unknown_field"}).
				WithCause(err)
		}
		return apperr.Validation("invalid JSON body").
			WithMeta(map[string]any{"reason": "invalid_json"}).WithCause(err)
	}
	if err := validate.Struct(dst); err != nil {
		var vErrs validator.ValidationErrors
		if errors.As(err, &vErrs) && len(vErrs) > 0 {
			fields := make([]string, 0, len(vErrs))
			for _, fe := range vErrs {
				fields = append(fields, fe.Field())
			}
			return apperr.Validation(fmt.Sprintf("invalid fields: %s", strings.Join(fields, ", "))).
				WithMeta(map[string]any{"fields": fields}).WithCause(err)
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
