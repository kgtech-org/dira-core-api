// Package notify porte les GABARITS de messages et leur envoi en notification
// poussée.
//
// Deux choses qu'on sépare volontiers mais qui n'ont de sens qu'ensemble : un
// texte traduit et paramétré, et le tuyau qui le porte jusqu'au téléphone.
package notify

import (
	"sort"
	"strings"
)

// Locales servies. Le FRANÇAIS est la langue par défaut de la plateforme, et
// sert de repli : un gabarit traduit à moitié part quand même, en français,
// plutôt que de ne pas partir.
const (
	LocaleFR = "fr"
	LocaleEN = "en"

	DefaultLocale = LocaleFR
)

// Bornes d'un gabarit. Une notification poussée n'est ni un courriel ni un
// article : au-delà, le système d'exploitation tronque, et c'est lui qui
// décide où.
const (
	MaxTitle = 120
	MaxBody  = 500
)

// Render remplace les variables `[nom]` d'un gabarit par leurs valeurs.
//
// La syntaxe est délibérément la plus courte possible : un exploitant qui
// écrit vingt gabarits tape `[client_name]`, pas `[var=client_name]`. Le
// crochet n'ouvre AUCUN formatage — pas de `[b]`, pas de `[url]` : une
// notification poussée n'affiche ni gras ni lien, et un balisage qui ne se
// rend nulle part finirait par s'afficher tel quel chez le destinataire.
//
// ⚠️ Une variable SANS VALEUR est remplacée par du VIDE, et le message part
// quand même. C'est un choix produit : mieux vaut « Bonjour , votre commande
// est prête » qu'une notification jamais reçue. Les manquantes sont rendues à
// l'appelant pour qu'il les journalise — sans quoi le défaut ne se verrait
// nulle part, sinon chez le client.
func Render(tmpl string, vars map[string]string) (out string, missing []string) {
	if tmpl == "" {
		return "", nil
	}
	var b strings.Builder
	b.Grow(len(tmpl))
	seen := map[string]bool{}

	for i := 0; i < len(tmpl); {
		open := strings.IndexByte(tmpl[i:], '[')
		if open < 0 {
			b.WriteString(tmpl[i:])
			break
		}
		open += i
		b.WriteString(tmpl[i:open])

		close := strings.IndexByte(tmpl[open:], ']')
		if close < 0 {
			// Crochet jamais refermé : c'est du texte, pas une variable.
			b.WriteString(tmpl[open:])
			break
		}
		close += open
		name := tmpl[open+1 : close]

		if !isVarName(name) {
			// « [1] », « [voir ici] » : un crochet littéral que l'auteur
			// voulait afficher. On le laisse passer intact plutôt que de le
			// faire disparaître.
			b.WriteString(tmpl[open : close+1])
			i = close + 1
			continue
		}
		if v, ok := vars[name]; ok {
			b.WriteString(v)
		} else if !seen[name] {
			seen[name] = true
			missing = append(missing, name)
		}
		i = close + 1
	}
	sort.Strings(missing)
	return b.String(), missing
}

// isVarName dit si le contenu d'un crochet est un nom de variable.
//
// Restreint à `[a-z0-9_]` : c'est ce qui permet de laisser passer intacts les
// crochets que l'auteur voulait vraiment écrire — « [1] », « [voir ici] », un
// intervalle « [0-5] ». Sans cette distinction, tout crochet deviendrait une
// variable et disparaîtrait du message faute de valeur.
//
// DEUX CARACTÈRES AU MOINS, et c'est ce qui règle le seul vrai conflit de
// cette syntaxe : `[b]`, `[i]`, `[u]` sont des balises BBCode qu'un rédacteur
// écrit par réflexe, et elles ressemblent trait pour trait à une variable.
// Les traiter comme telles les ferait disparaître du message — un « [b]gras
// [/b] » deviendrait « gras[/b] », ce qui est le pire des deux mondes. Aucune
// variable réelle ne s'appelle d'une seule lettre : la règle ne coûte rien et
// rend ces balises littérales, donc visibles et corrigeables.
func isVarName(s string) bool {
	if len(s) < 2 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
		default:
			return false
		}
	}
	// Un nom qui commence par un chiffre est une référence, pas une variable.
	return !(s[0] >= '0' && s[0] <= '9')
}

// Variables extrait les variables déclarées par un gabarit, dédoublonnées et
// triées.
//
// Sert l'écran d'administration : montrer à l'exploitant ce que son texte
// attend, pour qu'il ne découvre pas un trou à l'envoi.
func Variables(parts ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range parts {
		// Rendre avec un dictionnaire vide fait remonter TOUTES les variables
		// comme manquantes : c'est exactement la liste cherchée, et cela
		// garantit que l'extraction et le rendu lisent la même syntaxe.
		_, missing := Render(p, nil)
		for _, name := range missing {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Truncate borne un texte sans couper au milieu d'un caractère.
//
// Le faire ICI plutôt que de laisser le système d'exploitation tronquer :
// lui coupe où il veut, y compris au milieu d'un montant.
func Truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return strings.TrimRight(string(r[:max-1]), " ") + "…"
}
