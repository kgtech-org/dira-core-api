package country

// LE FOND DE CARTE D'UN PAYS.
//
// Deux fonds : le nôtre, et Google. Le pays choisit celui qu'il sert par
// DÉFAUT ; la personne choisit ensuite celui qui lui va, et son choix vit chez
// elle — c'est un réglage de confort, pas une politique.
//
// ⚠️ LA CLÉ GOOGLE ATTEINT FORCÉMENT LE CLIENT, et il faut le savoir avant de
// construire quoi que ce soit autour. Google exige la clé sur CHAQUE requête de
// tuile, en plus du jeton de session : aucun montage ne permet d'afficher un
// fond Google en la gardant au chaud ici. La chiffrer pour que l'application la
// déchiffre ne protège rien — la phrase de passe est dans le même binaire que
// la clé.
//
// Ce qui la protège vraiment, et pour quoi c'est fait : la RESTREINDRE côté
// Google (empreinte SHA-1 + nom de paquet sur Android, identifiant de bundle
// sur iOS, référent HTTP sur le web) et plafonner son quota. Une clé volée
// devient alors inutilisable ailleurs.
//
// Ce qu'on gagne à la servir d'ici plutôt qu'à la compiler dans l'application :
// la CHANGER sans republier, pays par pays, le jour où elle fuit ou où la
// facture s'envole.

import (
	"context"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// platforms est la liste fermée, dans l'ordre où la console les montre.
var platforms = []string{PlatformWeb, PlatformAndroid, PlatformIOS}

// Maps rend le réglage d'un pays tel que la console le lit — sans les clés.
func (s *Service) Maps(ctx context.Context, code string) (*MapsResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := mapsResponse(inst.Maps)
	return &out, nil
}

// UpdateMaps règle le fond d'un pays et ses clés.
func (s *Service) UpdateMaps(ctx context.Context, code string, req MapsUpdateRequest) (*MapsResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	if req.Basemap == nil && len(req.Google) == 0 {
		return nil, errNoMapsUpdate
	}
	for platform := range req.Google {
		if !knownPlatform(platform) {
			return nil, errUnknownPlatform.WithMeta(map[string]any{"fields": []string{"google." + platform}})
		}
	}

	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	before := mapsResponse(inst.Maps)

	// Les clés d'abord, le fond ensuite : c'est ce qui permet de poser une clé
	// ET de basculer sur Google dans le MÊME appel, sans se faire refuser par
	// la garde ci-dessous pour une clé qu'on vient justement d'envoyer.
	for platform, up := range req.Google {
		switch {
		case up.Key != nil:
			mapType := ""
			if up.MapType != nil {
				mapType = *up.MapType
			}
			if err := s.repo.SetGoogleKey(ctx, info.Code, platform, *up.Key, mapType); err != nil {
				return nil, apperr.Internal(err)
			}
		case up.MapType != nil:
			if err := s.repo.SetGoogleMapType(ctx, info.Code, platform, *up.MapType); err != nil {
				return nil, apperr.Internal(err)
			}
		}
	}

	if req.Basemap != nil {
		inst, _, err = s.repo.One(ctx, info.Code)
		if err != nil {
			return nil, apperr.Internal(err)
		}
		// ⚠️ PAS DE `google` SANS CLÉ. Sans cette garde, la bascule « réussit »
		// et toutes les cartes du pays deviennent grises : l'exploitation
		// croit avoir changé de fond, les gens croient l'application cassée,
		// et rien nulle part ne dit pourquoi.
		if *req.Basemap == BasemapGoogle && !hasAnyKey(inst.Maps) {
			return nil, errGoogleWithoutKey
		}
		if err := s.repo.SetBasemap(ctx, info.Code, *req.Basemap); err != nil {
			return nil, apperr.Internal(err)
		}
	}

	inst, _, err = s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := mapsResponse(inst.Maps)
	if s.audit != nil {
		// ⚠️ LE JOURNAL D'AUDIT NE VOIT PAS LES CLÉS. On y inscrit qu'une clé a
		// changé, et sa fin — jamais sa valeur : un journal d'audit se relit,
		// s'exporte et se garde des années.
		s.audit.Record(ctx, "country.maps", "country", info.Code, before, out)
	}
	return &out, nil
}

// Grant est ce qu'une application reçoit à l'authentification, pour SA
// plateforme : le fond par défaut, la clé Google et la vue à afficher.
//
// ⚠️ TROIS CHAÎNES, PAS UNE STRUCTURE, et c'est délibéré. Le module des comptes
// sert cette réponse à la connexion ; lui faire importer celui des pays pour un
// type partagé aurait noué deux modules que rien n'oblige à se connaître. Avec
// trois chaînes, le service de pays satisfait l'interface du module des comptes
// SANS que ni l'un ni l'autre ne l'ait décidé — et sans adaptateur de colle
// dans `main`.
//
// Plateforme vide ou inconnue = pas de clé, donc notre fond.
func (s *Service) Grant(ctx context.Context, code, platform string) (basemap, googleKey, googleMapType string) {
	basemap = BasemapDira
	info, ok := country.Lookup(code)
	if !ok {
		return basemap, "", ""
	}
	inst, found, err := s.repo.One(ctx, info.Code)
	if err != nil || !found {
		// ⚠️ AU MIEUX, TOUJOURS. Une base injoignable ne doit pas empêcher une
		// connexion : on rend le fond qui ne demande rien à personne.
		return basemap, "", ""
	}
	return resolveGrant(inst.Maps, platform)
}

// resolveGrant est la DÉCISION, séparée de sa lecture en base.
//
// ⚠️ SÉPARÉE EXPRÈS, POUR ÊTRE ÉPROUVABLE. Le service tient un dépôt Mongo
// concret : laissée dans `Grant`, cette logique n'aurait pu être testée sans
// base — donc, en pratique, pas testée. Or c'est elle qui décide si une
// application affiche une carte ou un rectangle gris.
func resolveGrant(m Maps, platform string) (basemap, googleKey, googleMapType string) {
	basemap = BasemapDira
	if m.Basemap != "" {
		basemap = m.Basemap
	}
	g, ok := m.Google[platform]
	if !knownPlatform(platform) || !ok || g.Key == "" {
		// ⚠️ PAS DE CLÉ POUR CETTE PLATEFORME → ON REVIENT À NOTRE FOND, même
		// si le pays est réglé sur Google. Annoncer un fond qu'aucune clé ne
		// sert donne un rectangle gris, et la personne croit l'application
		// cassée — alors que le réglage du pays, lui, est parfaitement valide
		// pour les DEUX autres plateformes.
		if basemap == BasemapGoogle {
			basemap = BasemapDira
		}
		return basemap, "", ""
	}
	googleMapType = g.MapType
	if googleMapType == "" {
		googleMapType = MapTypeRoadmap
	}
	// ⚠️ LA CLÉ EST SERVIE MÊME QUAND LE DÉFAUT DU PAYS EST `dira`. C'est ce qui
	// permet à la personne de CHOISIR Google : sans la clé, le choix serait
	// affiché et ne marcherait pas. Le réglage du pays dit ce qu'on montre
	// d'abord, pas ce qu'on autorise.
	return basemap, g.Key, googleMapType
}

func knownPlatform(p string) bool {
	for _, known := range platforms {
		if p == known {
			return true
		}
	}
	return false
}

func hasAnyKey(m Maps) bool {
	for _, g := range m.Google {
		if g.Key != "" {
			return true
		}
	}
	return false
}

// mapsResponse traduit le réglage en ce que la console a le droit de voir.
func mapsResponse(m Maps) MapsResponse {
	out := MapsResponse{Basemap: m.Basemap, Google: map[string]GoogleKeyStatus{}}
	if out.Basemap == "" {
		out.Basemap = BasemapDira
	}
	for _, platform := range platforms {
		g := m.Google[platform]
		status := GoogleKeyStatus{Configured: g.Key != "", MapType: g.MapType}
		if status.Configured {
			status.Hint = hint(g.Key)
			if !g.UpdatedAt.IsZero() {
				at := g.UpdatedAt.UTC()
				status.UpdatedAt = &at
			}
			if status.MapType == "" {
				status.MapType = MapTypeRoadmap
			}
		}
		out.Google[platform] = status
	}
	return out
}

// hint rend les quatre derniers caractères d'une clé : assez pour reconnaître
// laquelle est en place, pas assez pour s'en servir.
//
// ⚠️ ET RIEN POUR UNE CLÉ TROP COURTE. Quatre caractères sur une clé de six en
// diraient les deux tiers ; une clé trop courte est de toute façon une erreur
// de saisie, et la console dira seulement « configurée ».
func hint(key string) string {
	if len(key) < 12 {
		return ""
	}
	return "…" + key[len(key)-4:]
}
