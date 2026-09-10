package fcm

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// Vérifie que le VRAI compte de service du projet est accepté : une clé
// Firebase est au format PKCS#8 (« BEGIN PRIVATE KEY »), pas PKCS#1, et une
// bibliothèque qui n'accepterait que le second échouerait seulement en
// production, au premier envoi.
//
// Ignoré quand le fichier n'est pas là : le test doit passer sur une machine
// qui n'a pas le secret.
func TestRealServiceAccountParses(t *testing.T) {
	path := os.Getenv("FCM_SERVICE_ACCOUNT_FILE")
	if path == "" {
		t.Skip("FCM_SERVICE_ACCOUNT_FILE non défini")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("compte de service illisible: %v", err)
	}
	c, err := New(string(data))
	if err != nil {
		t.Fatalf("le compte de service du projet est refusé: %v", err)
	}
	if c.ProjectID() == "" {
		t.Fatal("project_id vide")
	}
	t.Logf("compte de service accepté, projet %s", c.ProjectID())
}

// --- client complet, contre un faux Google ---

// fakeGoogle joue le jeton OAuth2 et l'envoi FCM.
type fakeGoogle struct {
	srv       *httptest.Server
	tokenHits int
	sendHits  int
	sendCode  int
	sendBody  string
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	t.Helper()
	g := &fakeGoogle{sendCode: 200, sendBody: `{"name":"projects/p/messages/1"}`}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		g.tokenHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at-1","expires_in":3600}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		g.sendHits++
		w.WriteHeader(g.sendCode)
		_, _ = w.Write([]byte(g.sendBody))
	})
	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	return g
}

// testClient construit un client sur une clé RSA jetable, pointé vers le faux.
func testClient(t *testing.T, g *fakeGoogle) *Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("clé: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("pkcs8: %v", err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	sa, _ := json.Marshal(ServiceAccount{
		Type: "service_account", ProjectID: "p",
		ClientEmail: "svc@p.iam.gserviceaccount.com",
		PrivateKey:  pemKey, TokenURI: g.srv.URL + "/token",
	})
	c, err := New(string(sa))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.http = g.srv.Client()
	c.sendBase = g.srv.URL
	return c
}

// Un compte de service absent ne doit pas être une erreur fatale : une
// plateforme sans notifications doit démarrer, en le disant.
func TestEmptyServiceAccountIsNotConfigured(t *testing.T) {
	if _, err := New("   "); err != ErrNotConfigured {
		t.Fatalf("err = %v, attendu ErrNotConfigured", err)
	}
}

func TestIncompleteServiceAccountIsRefused(t *testing.T) {
	if _, err := New(`{"type":"service_account","project_id":"p"}`); err == nil {
		t.Fatal("un compte sans clé ni email doit être refusé")
	}
	if _, err := New("pas du json"); err == nil {
		t.Fatal("un JSON invalide doit être refusé")
	}
}

// Le jeton d'accès est MIS EN CACHE : le redemander à chaque notification
// ferait un aller-retour OAuth2 de plus par message, et Google plafonne.
func TestAccessTokenIsReused(t *testing.T) {
	g := newFakeGoogle(t)
	c := testClient(t, g)
	msgs := []Message{{Token: "t1"}, {Token: "t2"}}
	if _, err := c.Send(context.Background(), msgs); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := c.Send(context.Background(), msgs); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if g.tokenHits != 1 {
		t.Fatalf("jetons demandés = %d, attendu 1", g.tokenHits)
	}
	if g.sendHits != 4 {
		t.Fatalf("envois = %d, attendu 4 (un par jeton, l'API v1 n'a pas d'envoi groupé)", g.sendHits)
	}
}

// Un jeton mort se distingue d'une panne : il ne guérira pas, et le réessayer
// indéfiniment finit par faire refuser le projet.
func TestUnregisteredTokenIsFlagged(t *testing.T) {
	g := newFakeGoogle(t)
	g.sendCode, g.sendBody = 404, `{"error":{"status":"UNREGISTERED","message":"gone"}}`
	c := testClient(t, g)
	res, err := c.Send(context.Background(), []Message{{Token: "dead"}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !res[0].Unregistered || res[0].Err == nil {
		t.Fatalf("résultat inattendu: %+v", res[0])
	}
}

// Une panne serveur n'est PAS un jeton mort. Les confondre désactiverait
// toute la flotte après un incident chez Google.
func TestServerErrorIsNotAnUnregisteredToken(t *testing.T) {
	g := newFakeGoogle(t)
	g.sendCode, g.sendBody = 503, `{"error":{"status":"UNAVAILABLE","message":"try later"}}`
	c := testClient(t, g)
	res, err := c.Send(context.Background(), []Message{{Token: "alive"}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res[0].Err == nil {
		t.Fatal("l'échec doit remonter")
	}
	if res[0].Unregistered {
		t.Fatal("503 n'est pas une mort d'appareil : le jeton doit être conservé")
	}
}

// L'échec d'un appareil n'emporte pas les autres : chaque résultat est rendu
// séparément.
func TestOneResultPerToken(t *testing.T) {
	g := newFakeGoogle(t)
	c := testClient(t, g)
	res, err := c.Send(context.Background(), []Message{{Token: "a"}, {Token: "b"}, {Token: "c"}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("résultats = %d, attendu 3", len(res))
	}
	for i, want := range []string{"a", "b", "c"} {
		if res[i].Token != want {
			t.Fatalf("res[%d].Token = %q, attendu %q", i, res[i].Token, want)
		}
	}
}
