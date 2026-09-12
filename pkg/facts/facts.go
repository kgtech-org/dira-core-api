// Package facts est le fil des FAITS de la plateforme : ce qui s'est passé,
// où, et quand — pour que quelqu'un d'autre en tire quelque chose.
//
// Les services métier ne voient que ce qui leur arrive : une course sans
// chauffeur, une livraison que personne ne prend. Aucun d'eux ne sait que
// c'est la cinquième en dix minutes dans le même quartier. Ce fil existe pour
// que la couche analytique (`dira-analytics`) regarde l'ensemble — dans le
// temps et dans l'espace — et en déduise des situations.
//
// ⚠️ Un fait est ÉMIS AU MIEUX et n'échoue jamais l'action qui le produit :
// une commande passée reste passée si le fil est tombé. C'est pour cela que
// `Emit` ne rend rien. Le prix : un trou dans l'analyse, journalisé — et
// jamais une course perdue.
//
// Le transport est un FLUX Redis (XADD / XREADGROUP), pas un appel HTTP : si
// l'analytique est arrêtée dix minutes, elle rattrape en repartant ; un appel
// HTTP aurait perdu ces dix minutes, et le rapport qui en dépend aurait menti
// par omission. Le flux est BORNÉ (MAXLEN ≈) : un consommateur mort un mois ne
// fait pas grossir Redis à l'infini.
package facts

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Stream est le nom du flux. Un seul pour toute la plateforme : la verticale
// est un champ du fait, pas un flux à part — l'analytique doit pouvoir lire
// « ce qui s'est passé à Bè entre 18 h et 19 h » sans savoir d'avance quels
// métiers regarder.
const Stream = "dira:facts"

// Group est le groupe de consommateurs de l'analytique.
const Group = "analytics"

// Les natures de faits connues. Une chaîne, pas un type fermé : un fait que
// l'analytique ne connaît pas encore est conservé, pas rejeté — c'est en
// lisant le fil qu'on découvre ce qu'il faut détecter ensuite.
const (
	// DemandRequested : quelqu'un a demandé un service (une course, une
	// livraison) à un endroit. C'est le dénominateur de tout taux.
	DemandRequested = "demand.requested"
	// DemandMet : la demande a trouvé preneur — un chauffeur, un livreur.
	DemandMet = "demand.met"
	// DemandUnmet : la demande N'A PAS trouvé preneur. `Reason` dit
	// pourquoi : `exhausted` (l'appel a fait le tour), `gave_up` (le client
	// a renoncé pendant la recherche). ⚠️ Un paiement refusé n'est PAS une
	// demande non satisfaite : l'offre n'y est pour rien.
	DemandUnmet = "demand.unmet"
)

// Raisons d'une demande non satisfaite.
const (
	ReasonExhausted = "exhausted"
	ReasonGaveUp    = "gave_up"
)

// Verticales.
const (
	VerticalVTC  = "vtc"
	VerticalFood = "food"
)

// Fact est un événement daté et situé.
type Fact struct {
	Kind     string    `json:"kind"`
	Vertical string    `json:"vertical"`
	At       time.Time `json:"at"`
	// Geo est le lieu du fait, [lng, lat]. Pour une demande, c'est là où le
	// client attend — le point de départ d'une course, la collecte d'une
	// livraison. Zéro = fait sans lieu (conservé, jamais regroupé).
	Geo [2]float64 `json:"geo"`
	// Ref est l'objet métier (identifiant de course ou de livraison) : c'est
	// lui qui relie « demandé » à « servi » ou « non servi ».
	Ref string `json:"ref"`
	// Actor est le client derrière la demande, Supplier celui qui l'a servie.
	Actor    string `json:"actor,omitempty"`
	Supplier string `json:"supplier,omitempty"`
	Reason   string `json:"reason,omitempty"`
	// Attrs porte ce que le métier veut ajouter (classe, montant, tentatives).
	// Des chaînes, pour que le fil reste lisible par n'importe quoi.
	Attrs map[string]string `json:"attrs,omitempty"`
}

// Emitter dépose un fait sur le fil.
type Emitter interface {
	Emit(ctx context.Context, f Fact)
}

// Nop est l'émetteur de l'absence de fil : rien n'est déposé, rien n'échoue.
type Nop struct{}

func (Nop) Emit(context.Context, Fact) {}

// Redis émet sur un flux Redis.
type Redis struct {
	rdb    *redis.Client
	stream string
	maxLen int64
}

// NewRedis construit l'émetteur. `maxLen` borne le flux (approximatif) ;
// zéro = 200 000 faits, soit plusieurs jours d'une plateforme active.
func NewRedis(rdb *redis.Client, maxLen int64) *Redis {
	if maxLen <= 0 {
		maxLen = 200_000
	}
	return &Redis{rdb: rdb, stream: Stream, maxLen: maxLen}
}

// Emit dépose le fait. Au mieux : une erreur est journalisée, jamais rendue.
func (e *Redis) Emit(ctx context.Context, f Fact) {
	if f.At.IsZero() {
		f.At = time.Now().UTC()
	}
	values, err := Encode(f)
	if err != nil {
		slog.WarnContext(ctx, "facts: unencodable fact dropped", "kind", f.Kind, "error", err)
		return
	}
	// Détaché du contexte de la requête : la requête peut se terminer avant
	// que Redis réponde, et un fait ne doit pas dépendre de la patience du
	// client HTTP.
	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := e.rdb.XAdd(bg, &redis.XAddArgs{
		Stream: e.stream, MaxLen: e.maxLen, Approx: true, Values: values,
	}).Err(); err != nil {
		slog.WarnContext(ctx, "facts: fact dropped — the stream is unreachable",
			"kind", f.Kind, "ref", f.Ref, "error", err)
	}
}

// Encode rend les champs d'une entrée de flux. Le fait entier est dans `json`
// et les clés de tri (`kind`, `vertical`, `at`) sont à plat, pour qu'un
// `XRANGE` à la main reste lisible.
func Encode(f Fact) (map[string]any, error) {
	raw, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"kind": f.Kind, "vertical": f.Vertical,
		"at":   strconv.FormatInt(f.At.UnixMilli(), 10),
		"json": string(raw),
	}, nil
}

// Decode relit une entrée de flux.
func Decode(values map[string]any) (Fact, error) {
	var f Fact
	raw, _ := values["json"].(string)
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return f, err
	}
	return f, nil
}
