package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"log/slog"

	"github.com/kgtech-org/dira-core-api/internal/marker"
	"github.com/kgtech-org/dira-core-api/pkg/storage"
)

// LES MÉDIAS DU JEU DE DÉMONSTRATION — repris, pas régénérés.
//
// `media.json` est le relevé des images que le jeu de démonstration de
// staging portait en base (22 septembre 2026) : ici les marqueurs de carte,
// réglés depuis la console. Rejouer le seed après une remise à zéro de la
// base — sans toucher au stockage d'objets — les remet en place tels quels.
//
// Une URL n'est reprise QUE si l'objet existe dans le stockage configuré
// (même bucket) : sur un poste de développement, ou après un ménage du
// bucket, la liste ne promet rien que la carte ne saurait charger.
//
//go:embed media.json
var mediaManifest []byte

type coreMedia struct {
	MapMarkers map[string]struct {
		IconURL    string `json:"icon_url"`
		MapIconURL string `json:"map_icon_url"`
	} `json:"map_markers"`
}

// keeper garde une URL quand son objet existe, sinon rend "".
type keeper struct {
	media *storage.Store
}

func (k keeper) keep(ctx context.Context, url string) string {
	if url == "" || k.media == nil || !k.media.Exists(ctx, url) {
		return ""
	}
	return url
}

// seedMarkers pose les marqueurs de carte du relevé. Un marqueur DÉJÀ réglé
// n'est pas touché : le seed comble, il n'écrase pas ce que l'exploitation a
// choisi depuis.
func seedMarkers(ctx context.Context, logger *slog.Logger, svc *marker.Service, media *storage.Store) error {
	var m coreMedia
	if err := json.Unmarshal(mediaManifest, &m); err != nil {
		return err
	}
	if len(m.MapMarkers) == 0 {
		return nil
	}
	if media == nil {
		logger.Warn("seed: object storage unavailable — map markers keep their silhouettes")
		return nil
	}
	current, err := svc.List(ctx)
	if err != nil {
		return err
	}
	set := map[string]bool{}
	for _, c := range current {
		set[c.Kind] = c.IconURL != "" || c.MapIconURL != ""
	}
	k := keeper{media: media}
	restored := 0
	for kind, urls := range m.MapMarkers {
		if set[kind] {
			continue
		}
		req := marker.UpdateRequest{IconURL: k.keep(ctx, urls.IconURL), MapIconURL: k.keep(ctx, urls.MapIconURL)}
		if req.IconURL == "" && req.MapIconURL == "" {
			continue
		}
		if _, err := svc.Update(ctx, kind, req); err != nil {
			return err
		}
		restored++
	}
	logger.Info("seed: map markers restored", "count", restored)
	return nil
}
