// Package frontenddocs porte les CONTRATS que lisent les applications
// mobiles, et les rend lisibles par un programme.
//
// ⚠️ POURQUOI UN PAQUET GO DANS UN DOSSIER DE DOCUMENTATION. Ces cinq
// documents décrivent TROIS services — le socle, la livraison, les courses —
// et aucun des trois ne peut vérifier seul qu'ils disent vrai : chacun ne
// connaît que ses propres routes.
//
// Les rendre importables donne à chaque verticale le moyen de vérifier SA
// moitié, depuis son propre dépôt, sans copie et sans appel réseau. Sans cela,
// le déménagement de ces documents ici aurait fait tomber la vérification de
// 133 citations à 39 — une garantie vidée en silence, ce qui est pire qu'une
// garantie absente : on continue de croire qu'elle protège.
//
// Le fichier reste à un chemin qu'un humain trouve — `docs/frontend/` — parce
// que son premier lecteur est une équipe mobile, pas un compilateur.
package frontenddocs

import (
	"embed"
	"io/fs"
	"strings"
)

// Specs porte les cinq contrats de rôle, plus le journal et le brief de design.
//
// ⚠️ NOMMÉS UN PAR UN, PAS `*.md`. Ce dossier reçoit aussi les RELEVÉS des
// équipes mobiles (`SERVEUR-…-et-decisions.md`), déposés là pour être lus à côté
// des contrats qu'ils commentent. Avec `*.md`, le relevé du 29 septembre 2026
// est entré dans le binaire comme documentation de l'assistant — qui l'aurait
// cité comme une vérité — et a fait rougir le test de présence des contrats.
// Un relevé dit ce qu'une équipe a OBSERVÉ ; un contrat dit ce qui est PROMIS.
// Seul le second se sert à quelqu'un qui pose une question.
//
//go:embed README.md DESIGN-BRIEF.md FOOD-CLIENT.md FOOD-DELIVERY.md FOOD-MERCHANT.md VTC-CLIENT.md VTC-DRIVER.md
var Specs embed.FS

// All rend le contenu de chaque document, par nom de fichier.
//
// ⚠️ Rend une ERREUR plutôt qu'une carte vide si le dossier est vide. Un
// balayage qui ne trouve rien et se tait fait passer au vert tous les tests
// bâtis dessus — c'est exactement ainsi qu'un test de dérive d'index de ce
// projet a cessé de regarder quoi que ce soit, sans que personne l'apprenne.
func All() (map[string]string, error) {
	entries, err := fs.ReadDir(Specs, ".")
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body, err := Specs.ReadFile(e.Name())
		if err != nil {
			return nil, err
		}
		out[e.Name()] = string(body)
	}
	if len(out) == 0 {
		return nil, errNoSpecs
	}
	return out, nil
}

type noSpecsError struct{}

func (noSpecsError) Error() string {
	return "frontenddocs: aucun document embarqué — le paquet ne prouve plus rien"
}

var errNoSpecs = noSpecsError{}
