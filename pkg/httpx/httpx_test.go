package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type signup struct {
	Phone    string `json:"phone" validate:"required,e164"`
	Name     string `json:"name" validate:"required,min=1"`
	Password string `json:"password" validate:"required,min=8"`
}

func decodeErr(t *testing.T, body string) map[string]any {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	var dst signup
	err := Decode(r, &dst)
	require.Error(t, err)
	w := httptest.NewRecorder()
	Error(w, r, err)
	var out struct {
		Error map[string]any `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	return out.Error
}

// Un 422 qui ne dit pas QUOI laisse l'application afficher « vérifiez vos
// informations » sous un formulaire correct. Chaque cause nomme sa clé JSON.
func TestDecodeNamesTheOffendingFields(t *testing.T) {
	e := decodeErr(t, `{"phone":"+22899000001","name":"","password":"court"}`)
	assert.Equal(t, "validation_failed", e["code"])
	assert.ElementsMatch(t, []any{"name", "password"}, e["fields"], "les clés JSON, pas les noms Go")
	assert.Nil(t, e["reason"])

	e = decodeErr(t, `{"phone":"+22899000001","name":"Ko","password":"Passw0rd!","first_name":"Ko"}`)
	assert.Equal(t, "unknown_field", e["reason"])
	assert.Equal(t, []any{"first_name"}, e["fields"])

	e = decodeErr(t, `{"phone":`)
	assert.Equal(t, "invalid_json", e["reason"])
}

// « 22899000001 » passait le `e164` de la bibliothèque (le `+` y est
// facultatif) et devenait un second compte à côté de « +22899000001 ».
func TestE164RequiresThePlus(t *testing.T) {
	for body, want := range map[string]bool{
		`{"phone":"+22899000001","name":"Ko","password":"Passw0rd!"}`:     true,
		`{"phone":"+228 99 00 00 01","name":"Ko","password":"Passw0rd!"}`: true,
		`{"phone":"22899000001","name":"Ko","password":"Passw0rd!"}`:      false,
		`{"phone":"99000001","name":"Ko","password":"Passw0rd!"}`:         false,
	} {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		var dst signup
		err := Decode(r, &dst)
		if want {
			assert.NoError(t, err, body)
		} else {
			assert.Error(t, err, body)
		}
	}
}
