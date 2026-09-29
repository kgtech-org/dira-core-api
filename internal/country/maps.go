package country

// LE FOND DE CARTE D'UN PAYS — UN SEUL FAIT, ET RIEN D'AUTRE.
//
// Deux fonds : le nôtre, et Google. Le pays dit lequel on montre par DÉFAUT ;
// la personne choisit ensuite celui qui lui va, et son choix vit chez elle.
// C'est un réglage de confort, pas une politique.
//
// ⚠️ LE SERVEUR NE GARDE AUCUNE CLÉ GOOGLE, ET N'EN SERT AUCUNE (v4.40.0).
// Il en a gardé une par pays et par plateforme du 22 au 29 septembre 2026,
// pour pouvoir la faire tourner sans republier les applications. L'idée était
// juste et le montage inutilisable : chaque frontend porte DÉJÀ sa clé,
// restreinte à son empreinte SHA-1, à son identifiant de bundle ou à son
// référent HTTP — c'est cette restriction qui protège une clé, et elle
// appartient à la plateforme, pas au pays. Une clé servie par l'API arrivait
// donc trop tard, pour quelqu'un qui n'en avait pas besoin, et elle obligeait
// en plus l'exploitation à ranger trois secrets par pays dans un écran
// d'administration.
//
// Ce qui reste est la seule question à laquelle le serveur peut répondre mieux
// que l'application : **dans ce pays, quel fond montre-t-on d'abord ?**

import (
	"context"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Maps rend le réglage d'un pays tel que la console le lit.
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

// UpdateMaps règle le fond par défaut d'un pays.
func (s *Service) UpdateMaps(ctx context.Context, code string, req MapsUpdateRequest) (*MapsResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	if req.Basemap == nil {
		return nil, errNoMapsUpdate
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	before := mapsResponse(inst.Maps)

	if err := s.repo.SetBasemap(ctx, info.Code, *req.Basemap); err != nil {
		return nil, apperr.Internal(err)
	}
	out := MapsResponse{Basemap: *req.Basemap}
	if s.audit != nil {
		s.audit.Record(ctx, "country.maps", "country", info.Code, before, out)
	}
	return &out, nil
}

// Grant est ce qu'une application reçoit à l'authentification : le fond à
// montrer d'abord dans ce pays.
//
// ⚠️ UNE CHAÎNE, PAS UNE STRUCTURE, et c'est délibéré. Le module des comptes
// sert cette réponse à la connexion ; lui faire importer celui des pays pour un
// type partagé aurait noué deux modules que rien n'oblige à se connaître. Avec
// une chaîne, le service de pays satisfait l'interface du module des comptes
// SANS que ni l'un ni l'autre ne l'ait décidé — et sans adaptateur de colle
// dans `main`.
func (s *Service) Grant(ctx context.Context, code string) string {
	info, ok := country.Lookup(code)
	if !ok {
		return BasemapDira
	}
	inst, found, err := s.repo.One(ctx, info.Code)
	if err != nil || !found {
		// ⚠️ AU MIEUX, TOUJOURS. Une base injoignable ne doit pas empêcher une
		// connexion : on rend le fond qui ne demande rien à personne.
		return BasemapDira
	}
	return basemapOf(inst.Maps)
}

// basemapOf : le fond effectif d'un réglage. Vide = le nôtre.
//
// ⚠️ UNE FONCTION, POUR ÊTRE ÉPROUVABLE SANS BASE — et pour que la console et
// les applications ne puissent pas répondre deux choses différentes à la même
// question, ce qui est exactement ce qui fait chercher une heure pourquoi « le
// réglage n'a pas d'effet ».
func basemapOf(m Maps) string {
	if m.Basemap == BasemapGoogle {
		return BasemapGoogle
	}
	return BasemapDira
}

// mapsResponse traduit le réglage en ce que la console lit.
func mapsResponse(m Maps) MapsResponse {
	return MapsResponse{Basemap: basemapOf(m)}
}
