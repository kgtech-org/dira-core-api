package payment

import "sort"

// ProviderInfo is one payment operator the client may choose.
//
// L'application ne doit PAS coder la liste en dur. La maquette en propose
// quatre ; un seul est branché aujourd'hui, et afficher les trois autres
// ferait échouer trois paiements sur quatre — au moment précis où le client
// vient de valider son panier.
type ProviderInfo struct {
	ID string `json:"id"`
	// Label est ce qui s'affiche. Rendu par le serveur pour que l'ajout d'un
	// opérateur ne demande PAS une mise à jour de l'application : une
	// intégration mobile money se déploie côté serveur, elle ne doit pas
	// attendre une revue de magasin d'applications.
	Label string `json:"label"`
}

// labels nomme les opérateurs connus. Un identifiant absent de cette table est
// rendu tel quel plutôt que masqué : un opérateur branché mais non nommé doit
// rester utilisable, quitte à s'afficher moins joliment.
var labels = map[string]string{
	"mock":   "Simulation (développement)",
	"orange": "Orange Money",
	"wave":   "Wave",
	"mtn":    "MTN MoMo",
	"moov":   "Moov Money",
	"free":   "Free Money",
	"tmoney": "T-Money",
}

// Providers lists what the server can ACTUALLY charge, alphabetically.
//
// Rend le REGISTRE, pas un catalogue d'intentions : ce qui n'est pas branché
// n'apparaît pas. C'est la seule liste sur laquelle une application peut
// s'appuyer sans risquer de proposer un paiement qui échouera.
func (s *Service) Providers() []ProviderInfo {
	out := make([]ProviderInfo, 0, len(s.providers))
	for id := range s.providers {
		label, ok := labels[id]
		if !ok {
			label = id
		}
		out = append(out, ProviderInfo{ID: id, Label: label})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
