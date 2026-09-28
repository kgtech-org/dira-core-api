// Package faults RANGE les pannes de la plateforme : les paniques et les
// refus serveur, groupés, datés, comptés.
//
// ⚠️ IL EXISTE PARCE QU'UNE PILE D'EXÉCUTION PERDUE DANS UN FLUX DE TEXTE N'A
// JAMAIS RÉPARÉ PERSONNE. Une panique écrite dans les journaux d'un conteneur
// disparaît à la rotation, personne ne la voit passer, et la même panique
// répétée mille fois ressemble à mille problèmes. Ici, elle est groupée —
// « cette erreur, 412 fois depuis mardi, toujours au même endroit » —, elle
// attend qu'on la regarde, et elle se tait quand on l'a corrigée.
//
// ⚠️ IL N'APPELLE AUCUN SERVICE EXTÉRIEUR. Un Sentry auto-hébergé coûterait
// plus de mémoire que toute la supervision réunie ; un Sentry hébergé ferait
// sortir des traces d'exécution où figurent des identifiants de comptes et des
// morceaux de requêtes. La plateforme sait déjà stocker, notifier et afficher.
package faults

import (
	"context"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/obs"
)

// Collection est là où les pannes se rangent.
const Collection = "platform_faults"

// Retention : au-delà, une panne qu'on n'a jamais revue s'efface.
//
// ⚠️ TRENTE JOURS, ET C'EST UNE DÉCISION. Garder pour toujours remplit la base
// de pannes corrigées il y a un an ; garder deux jours fait disparaître celle
// qui n'arrive qu'au moment de la paie. Trente jours couvrent un cycle
// mensuel, qui est le rythme de cette plateforme.
const Retention = 30 * 24 * time.Hour

// Fault est une panne telle qu'elle est rangée — une LIGNE PAR EMPREINTE, pas
// par occurrence.
//
// ⚠️ C'EST LE CŒUR DU RANGEMENT. Une ligne par occurrence aurait donné cent
// mille lignes identiques le jour où une route casse, et la panne suivante —
// celle qu'on n'a jamais vue — serait invisible au milieu. Ici, la centième
// occurrence incrémente un compteur.
type Fault struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	Fingerprint string             `bson:"fingerprint" json:"fingerprint"`
	Service     string             `bson:"service" json:"service"`
	Version     string             `bson:"version,omitempty" json:"version,omitempty"`
	Kind        string             `bson:"kind" json:"kind"`
	Message     string             `bson:"message" json:"message"`
	// Stack n'est gardée que de la DERNIÈRE occurrence. Les garder toutes
	// pèserait des mégaoctets pour cent fois la même chose.
	Stack  string `bson:"stack,omitempty" json:"stack,omitempty"`
	Route  string `bson:"route,omitempty" json:"route,omitempty"`
	Method string `bson:"method,omitempty" json:"method,omitempty"`
	Status int    `bson:"status,omitempty" json:"status,omitempty"`
	// Le CONTEXTE de la dernière occurrence : de quoi rejouer. Sans lui, on
	// lit une pile sans savoir qui, où, ni quelle requête.
	RequestID string `bson:"request_id,omitempty" json:"request_id,omitempty"`
	Country   string `bson:"country,omitempty" json:"country,omitempty"`
	UserID    string `bson:"user_id,omitempty" json:"user_id,omitempty"`
	// Count, FirstSeen et LastSeen font toute la valeur de la liste : « trois
	// fois en un mois » et « trois mille fois depuis ce matin » ne se
	// traitent pas de la même façon.
	Count     int       `bson:"count" json:"count"`
	FirstSeen time.Time `bson:"first_seen" json:"first_seen"`
	LastSeen  time.Time `bson:"last_seen" json:"last_seen"`
	// ResolvedAt : quelqu'un a dit « c'est corrigé ».
	//
	// ⚠️ ELLE SE ROUVRE TOUTE SEULE si la panne revient. Une résolution qui
	// tient malgré les faits est un mensonge qu'on relit chaque semaine ;
	// celle-ci ne survit pas à la preuve du contraire.
	ResolvedAt *time.Time `bson:"resolved_at,omitempty" json:"resolved_at,omitempty"`
	ResolvedBy string     `bson:"resolved_by,omitempty" json:"resolved_by,omitempty"`
}

// Service range les pannes.
type Service struct {
	repo    *Repository
	version string
	// queue découple la capture de l'écriture.
	//
	// ⚠️ UNE SUPERVISION NE DOIT JAMAIS RALENTIR CE QU'ELLE OBSERVE. Écrire
	// en base dans le chemin de la requête ajouterait un aller-retour Mongo à
	// chaque `5xx` — c'est-à-dire précisément au moment où la base est
	// peut-être ce qui ne va pas. La file absorbe ; pleine, elle JETTE.
	queue chan obs.Fault
}

// QueueSize borne la file.
//
// ⚠️ ELLE JETTE PLUTÔT QUE D'ATTENDRE, et c'est le point. Une file qui bloque
// ferait tomber le service au moment de la panne — la supervision deviendrait
// la panne. Mille éléments absorbent un pic ; au-delà, on perd des
// occurrences, jamais le compte de celles qu'on a rangées.
const QueueSize = 1000

// NewService construit le rangement et lance son écrivain.
func NewService(ctx context.Context, repo *Repository, version string) *Service {
	s := &Service{repo: repo, version: version, queue: make(chan obs.Fault, QueueSize)}
	go s.run(ctx)
	return s
}

// Capture range une panne — sans attendre.
func (s *Service) Capture(_ context.Context, f obs.Fault) {
	select {
	case s.queue <- f:
	default:
		// ⚠️ SILENCIEUSEMENT, ET C'EST VOULU. Journaliser « file pleine » à
		// chaque panne ajouterait une ligne de journal par panne perdue,
		// c'est-à-dire du bruit au pire moment. Le compteur de paniques, lui,
		// continue de compter : c'est lui qui alerte.
	}
}

func (s *Service) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-s.queue:
			// ⚠️ UN CONTEXTE À PART. Celui de la requête est peut-être déjà
			// annulé — la requête a échoué, c'est bien pour cela qu'on est
			// là — et écrire avec lui ne rangerait jamais rien.
			write, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := s.repo.Record(write, f, s.version); err != nil {
				slog.WarnContext(write, "faults: not recorded", "error", err)
			}
			cancel()
		}
	}
}
