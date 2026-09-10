// Package diramaps talks to the dira-maps SIG for road-network routing.
//
// ⚠️ dira-maps ne rend AUCUN fond de carte : c'est du JSON sur HTTP — un tracé
// routier et des adresses. Le fond de carte vient du composant natif du
// téléphone, côté application.
package diramaps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client calls the SIG. Base d'URL DISTINCTE de l'API et du suivi : les trois
// services se déplacent indépendamment.
type Client struct {
	baseURL string
	http    *http.Client
}

// New builds the client. Une base vide rend un client INERTE plutôt que nil :
// l'appelant n'a pas à tester la présence à chaque usage, et l'absence de SIG
// se traduit par une absence de durée, pas par une panne.
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		// Court, et c'est délibéré : la durée est un CONFORT. Retenir la
		// création d'une course pendant trente secondes parce qu'un service
		// de cartographie rame serait un très mauvais échange.
		http: &http.Client{Timeout: 4 * time.Second},
	}
}

// Enabled dit si un SIG est configuré.
func (c *Client) Enabled() bool { return c != nil && c.baseURL != "" }

// RouteDuration returns how long the road network says this tour takes.
//
// Le tracé, lui, reste calculé localement : c'est lui qui sert la TARIFICATION,
// et la faire dépendre d'un service externe ferait varier le prix d'une
// commande selon la disponibilité d'un tiers. On ne demande ici que la durée.
func (c *Client) RouteDuration(ctx context.Context, city string, points [][2]float64) (int, error) {
	if !c.Enabled() {
		return 0, nil
	}
	if len(points) < 2 {
		return 0, nil
	}
	body, err := json.Marshal(map[string]any{
		"ville":       city,
		"coordinates": points, // [[lon, lat], …]
		"mode":        "driving",
	})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/calc/route", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("diramaps: route: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("diramaps: route: status %d", resp.StatusCode)
	}
	var out struct {
		Routes []struct {
			DurationS float64 `json:"duration_s"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, fmt.Errorf("diramaps: route: %w", err)
	}
	if len(out.Routes) == 0 {
		return 0, nil
	}
	// La PREMIÈRE route : le service rend les alternatives par ordre de
	// préférence, et c'est celle-là que la carte du livreur affichera.
	return int(out.Routes[0].DurationS + 0.5), nil
}
