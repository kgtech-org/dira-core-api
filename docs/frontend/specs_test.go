package frontenddocs_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	frontenddocs "github.com/kgtech-org/dira-core-api/docs/frontend"
)

// Ce fichier vérifie que les contrats mobiles ne citent pas une route du SOCLE
// qui n'existe pas.
//
// ⚠️ IL NE COUVRE QU'UN TIERS DE CES DOCUMENTS, et c'est structurel. Les cinq
// specs décrivent trois services ; les routes de la livraison et des courses
// vivent dans d'autres dépôts, et rien ici ne peut les confirmer. Chaque
// verticale vérifie SA moitié depuis chez elle, en important
// `docs/frontend` — c'est la raison d'être du paquet.
//
// Dit plutôt que masqué : un test qui laisse croire qu'il vérifie tout est
// pire qu'un test absent.

// sharedSegments sont les segments que le socle ET une verticale servent.
//
// ⚠️ Depuis ce dépôt, un chemin cité sous l'un d'eux ne peut pas être
// attribué : `/me/preferences` est ici, `/me/favorites` est à la livraison, et
// ses sources ne sont pas là pour trancher. Le test les ÉCARTE au lieu de
// deviner — accuser une spec exacte est pire que ne rien vérifier.
var sharedSegments = map[string]bool{
	"me":     true,
	"admin":  true,
	"stores": true,
	"dishes": true,
}

var (
	routeCall    = regexp.MustCompile(`(?:^|[.\s])(?:Get|Post|Put|Patch|Delete)\("(/[^"]*)"`)
	specCitation = regexp.MustCompile(`\b(?:GET|POST|PUT|PATCH|DELETE)\s+` + "`?" + `(/[a-zA-Z0-9/{}_.:-]*)`)
	anyParam     = regexp.MustCompile(`\{[^}]*\}|:[a-zA-Z_][a-zA-Z0-9_]*`)
)

// normaliseParams ramène tout paramètre de chemin à `{}`.
//
// Deux notations cohabitent sans qu'aucune soit fautive : `{id}` et `:id`, et
// `{pickup_id}` côté spec contre `{pickupID}` côté code — le nom d'une
// variable de route ne se voit pas depuis un téléphone. Sans cette
// normalisation, le test signalerait des routes parfaitement servies, et on
// apprend vite à ignorer un test qui crie à tort.
func normaliseParams(p string) string { return anyParam.ReplaceAllString(p, "{}") }

func firstSegment(p string) string {
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// mountedRoutes balaie les sources du service pour les routes qu'il sert.
func mountedRoutes(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, root := range []string{"../../internal", "../../cmd"} {
		require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range routeCall.FindAllStringSubmatch(string(body), -1) {
				out[normaliseParams(m[1])] = true
			}
			return nil
		}))
	}
	// Un balayage vide signale un chemin cassé, pas un service sans routes.
	require.NotEmpty(t, out, "aucune route trouvée : le balayage regarde-t-il au bon endroit ?")
	return out
}

// TestSpecsCiteRoutesThisServiceServes : une spec ne peut pas citer une route
// du socle qui n'existe pas.
//
// Ces documents sont ce que lisent les équipes mobiles. Une route renommée ici
// et non corrigée là-bas ne se découvre qu'à l'intégration, du côté de gens
// qui n'ont pas accès à ce code pour vérifier.
//
// L'appartenance se déduit du SEGMENT DE TÊTE plutôt que d'une liste blanche :
// rien à entretenir, et une route renommée est signalée le jour même.
func TestSpecsCiteRoutesThisServiceServes(t *testing.T) {
	mounted := mountedRoutes(t)

	owned := map[string]bool{}
	for route := range mounted {
		if seg := firstSegment(route); seg != "" && !sharedSegments[seg] && seg != "internal" && seg != "webhooks" {
			owned[seg] = true
		}
	}
	require.NotEmpty(t, owned, "aucun segment propre au socle : le test ne vérifierait rien")

	specs, err := frontenddocs.All()
	require.NoError(t, err)
	require.NotEmpty(t, specs, "aucune spec embarquée : ce test ne vérifierait rien")

	checked := 0
	for name, body := range specs {
		for _, m := range specCitation.FindAllStringSubmatch(body, -1) {
			cited := normaliseParams(strings.TrimRight(m[1], ".,;`"))
			if strings.HasPrefix(cited, "/api/v1") {
				continue // exemple d'URL complète, pas une citation de route
			}
			if !owned[firstSegment(cited)] {
				continue // à une verticale, ou au suivi
			}
			checked++
			assert.True(t, mounted[cited],
				"%s cite %s, que le SOCLE ne sert pas — une équipe mobile appellera une route absente",
				name, cited)
		}
	}
	require.NotZero(t, checked, "aucune citation vérifiée : le motif ne reconnaît plus rien")
	t.Logf("%d citations de routes du socle vérifiées dans %d specs", checked, len(specs))
}

// TestEveryRoleSpecIsPresent : les cinq contrats de rôle sont là.
//
// ⚠️ Une spec SUPPRIMÉE ne casse rien — elle laisse simplement une équipe sans
// contrat, et personne ne s'en aperçoit avant qu'on la réclame. Les nommer ici
// fait de leur absence une erreur de construction.
func TestEveryRoleSpecIsPresent(t *testing.T) {
	specs, err := frontenddocs.All()
	require.NoError(t, err)

	var have []string
	for name := range specs {
		have = append(have, name)
	}
	sort.Strings(have)

	assert.Equal(t, []string{
		"DESIGN-BRIEF.md",
		"FOOD-CLIENT.md",
		"FOOD-DELIVERY.md",
		"FOOD-MERCHANT.md",
		"README.md",
		"VTC-CLIENT.md",
		"VTC-DRIVER.md",
	}, have)
}

// TestSpecsShareOneVersion : les cinq documents portent la MÊME version.
//
// C'est la règle écrite dans le journal — « un frontend qui cite v2.1.0
// désigne un contrat précis ». Elle ne tient pas toute seule : il suffit d'en
// oublier un lors d'une montée de version pour que deux équipes travaillent
// sur deux contrats en croyant parler du même.
func TestSpecsShareOneVersion(t *testing.T) {
	specs, err := frontenddocs.All()
	require.NoError(t, err)

	version := regexp.MustCompile(`\*\*Version (\d+\.\d+\.\d+)\*\*`)
	seen := map[string][]string{}
	for name, body := range specs {
		if name == "DESIGN-BRIEF.md" {
			continue // le brief renvoie au contrat, il n'en porte pas la version
		}
		m := version.FindStringSubmatch(body)
		require.NotNil(t, m, "%s ne porte pas de version en en-tête", name)
		seen[m[1]] = append(seen[m[1]], name)
	}
	require.Len(t, seen, 1,
		"les specs ne partagent plus une seule version — deux équipes croiraient parler du même contrat : %v", seen)
}
