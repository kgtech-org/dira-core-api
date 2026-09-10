// Package fcm envoie des notifications par Firebase Cloud Messaging (API v1).
//
// L'API v1 s'authentifie par un jeton OAuth2 obtenu à partir d'un compte de
// service. Le SDK officiel de Google le ferait, au prix d'un arbre de
// dépendances considérable pour deux appels HTTP : on signe l'assertion
// nous-mêmes avec la bibliothèque JWT déjà présente.
package fcm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	tokenURL    = "https://oauth2.googleapis.com/token"
	scope       = "https://www.googleapis.com/auth/firebase.messaging"
	sendBase    = "https://fcm.googleapis.com"
	sendPathFmt = "/v1/projects/%s/messages:send"
	// assertionTTL : Google plafonne à une heure. On reste dessous.
	assertionTTL = 45 * time.Minute
	// refreshMargin : on renouvelle AVANT l'expiration. Sans marge, un envoi
	// parti à la seconde près échouerait en 401 pour rien.
	refreshMargin = 2 * time.Minute
)

// ServiceAccount est le strict nécessaire du JSON de compte de service.
type ServiceAccount struct {
	Type        string `json:"type"`
	ProjectID   string `json:"project_id"`
	PrivateKey  string `json:"private_key"`
	ClientEmail string `json:"client_email"`
	TokenURI    string `json:"token_uri"`
}

// Client envoie à FCM.
type Client struct {
	sa   ServiceAccount
	key  any
	http *http.Client
	// sendBase est l'hôte de l'API d'envoi. Un champ, et non une constante,
	// pour que le client puisse être exercé contre un serveur de test — le
	// chemin traversé reste ENTIER : même requête, mêmes en-têtes, même
	// lecture de la réponse d'erreur.
	sendBase string

	mu      sync.Mutex
	token   string
	expires time.Time
}

// ErrNotConfigured dit qu'aucun compte de service n'est fourni.
var ErrNotConfigured = errors.New("fcm: no service account configured")

// New construit un client à partir du JSON de compte de service.
//
// Un JSON vide rend (nil, ErrNotConfigured) plutôt qu'une erreur fatale : une
// plateforme sans notifications doit démarrer, en le disant.
func New(serviceAccountJSON string) (*Client, error) {
	if strings.TrimSpace(serviceAccountJSON) == "" {
		return nil, ErrNotConfigured
	}
	var sa ServiceAccount
	if err := json.Unmarshal([]byte(serviceAccountJSON), &sa); err != nil {
		return nil, fmt.Errorf("fcm: service account json: %w", err)
	}
	if sa.ProjectID == "" || sa.PrivateKey == "" || sa.ClientEmail == "" {
		return nil, errors.New("fcm: service account missing project_id, private_key or client_email")
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(sa.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("fcm: private key: %w", err)
	}
	if sa.TokenURI == "" {
		sa.TokenURI = tokenURL
	}
	return &Client{
		sa:       sa,
		key:      key,
		sendBase: sendBase,
		// Une notification est utile MAINTENANT. Attendre trente secondes un
		// service injoignable retiendrait la requête métier qui l'a déclenchée.
		http: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// ProjectID rend le projet Firebase servi (journalisation, diagnostic).
func (c *Client) ProjectID() string { return c.sa.ProjectID }

// Message est une notification à envoyer à UN appareil.
type Message struct {
	Token string
	Title string
	Body  string
	// Data voyage avec la notification et sert à l'application : ouvrir la
	// bonne commande plutôt que l'écran d'accueil. Toutes les valeurs sont
	// des CHAÎNES — FCM refuse le reste.
	Data map[string]string
}

// Result dit ce qu'il est advenu d'un envoi.
type Result struct {
	Token string
	Err   error
	// Unregistered : FCM déclare ce jeton MORT (application désinstallée,
	// jeton remplacé). À distinguer d'une panne : un jeton mort ne guérira
	// pas, et le réessayer indéfiniment finit par faire refuser le projet.
	Unregistered bool
}

// Send envoie un message par appareil.
//
// UN APPEL PAR JETON, parce que l'API v1 n'a pas d'envoi groupé (`send/batch`
// est retiré depuis 2024). L'important est que l'échec d'un appareil
// n'emporte pas les autres : chaque résultat est rendu séparément.
func (c *Client) Send(ctx context.Context, msgs []Message) ([]Result, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}
	token, err := c.accessToken(ctx)
	if err != nil {
		// Panne d'authentification : AUCUN appareil n'est en cause, et il ne
		// faut surtout pas les marquer morts.
		return nil, err
	}
	out := make([]Result, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, c.sendOne(ctx, token, m))
	}
	return out, nil
}

func (c *Client) sendOne(ctx context.Context, accessToken string, m Message) Result {
	payload := map[string]any{
		"message": map[string]any{
			"token":        m.Token,
			"notification": map[string]any{"title": m.Title, "body": m.Body},
			"data":         m.Data,
			// Priorité haute sur Android : sur un réseau ouest-africain, une
			// notification différée par le mode économie d'énergie arrive
			// après la livraison qu'elle annonçait.
			"android": map[string]any{"priority": "high"},
			"apns": map[string]any{
				"headers": map[string]string{"apns-priority": "10"},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{Token: m.Token, Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.sendBase+fmt.Sprintf(sendPathFmt, c.sa.ProjectID), bytes.NewReader(body))
	if err != nil {
		return Result{Token: m.Token, Err: err}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return Result{Token: m.Token, Err: fmt.Errorf("fcm: send: %w", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 300 {
		return Result{Token: m.Token}
	}
	var e struct {
		Error struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&e)
	return Result{
		Token: m.Token,
		Err:   fmt.Errorf("fcm: send: %d %s: %s", resp.StatusCode, e.Error.Status, e.Error.Message),
		// UNREGISTERED (404) et INVALID_ARGUMENT sur le jeton (400) désignent
		// un appareil qui ne reviendra pas. 401/403/5xx sont des pannes : le
		// jeton n'y est pour rien et doit être conservé.
		Unregistered: resp.StatusCode == http.StatusNotFound ||
			e.Error.Status == "UNREGISTERED" || e.Error.Status == "NOT_FOUND",
	}
}

// accessToken rend un jeton d'accès valide, renouvelé au besoin.
func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expires.Add(-refreshMargin)) {
		return c.token, nil
	}
	now := time.Now()
	assertion, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":   c.sa.ClientEmail,
		"scope": scope,
		"aud":   c.sa.TokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(assertionTTL).Unix(),
	}).SignedString(c.key)
	if err != nil {
		return "", fmt.Errorf("fcm: sign assertion: %w", err)
	}

	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sa.TokenURI,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("fcm: token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("fcm: token: status %d", resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("fcm: token: %w", err)
	}
	if out.AccessToken == "" {
		return "", errors.New("fcm: token: empty access_token")
	}
	c.token = out.AccessToken
	c.expires = now.Add(time.Duration(out.ExpiresIn) * time.Second)
	return c.token, nil
}
