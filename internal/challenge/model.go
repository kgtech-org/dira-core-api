package challenge

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/promo"
)

// Challenge est UN objectif : ce qu'il faut atteindre, sur quelle fenêtre, et
// ce que ça rapporte.
type Challenge struct {
	ID      primitive.ObjectID `bson:"_id,omitempty"`
	Country string             `bson:"country"`
	// Audience : `driver`, `courier` ou `client`.
	Audience string `bson:"audience"`
	Metric   string `bson:"metric"`
	// Target : la cible. Un COMPTE, ou un MONTANT quand la mesure l'est.
	Target int `bson:"target"`
	// RewardXOF : le bonus, dans la monnaie du pays.
	RewardXOF int `bson:"reward_xof"`
	// Title, Description : ce que la personne lit. ⚠️ Écrits par
	// l'exploitation, et non calculés depuis la mesure : « 5 courses avant
	// dimanche, 2 000 F pour vous » se lit mieux que « rides_done >= 5 », et
	// c'est ce texte qui donne envie.
	Title       string `bson:"title"`
	Description string `bson:"description,omitempty"`

	Window Window `bson:"window"`
	Repeat string `bson:"repeat"`
	// Limits : l'enveloppe, de `pkg/promo` — le même compte d'argent que les
	// promotions et les codes.
	Limits promo.Limits `bson:"limits"`
	// Counters : ce qui a été engagé. ⚠️ Sur l'OCCURRENCE, jamais sur la
	// série : une enveloppe hebdomadaire se rouvre chaque semaine, et la
	// partager entre les occurrences aurait fait que la semaine 1 mange le
	// budget de la semaine 4.
	Counters promo.Counters `bson:"counters"`

	Status string `bson:"status"`
	// SeriesID : l'objectif MODÈLE dont cette occurrence est née. Vide sur le
	// modèle lui-même.
	//
	// ⚠️ CHAQUE OCCURRENCE EST UN OBJECTIF À PART ENTIÈRE, avec sa fenêtre, son
	// enveloppe et ses avancements. C'est la leçon des programmations
	// d'abonnement : une série qui garderait un seul compteur ferait que
	// « 20 courses cette semaine » devient « 20 courses depuis toujours », et
	// l'objectif cesse d'être atteignable par quelqu'un qui arrive au mois
	// deux.
	SeriesID *primitive.ObjectID `bson:"series_id,omitempty"`

	CreatedBy  string     `bson:"created_by,omitempty"`
	CreatedAt  time.Time  `bson:"created_at"`
	UpdatedAt  time.Time  `bson:"updated_at"`
	LaunchedAt *time.Time `bson:"launched_at,omitempty"`
	EndedAt    *time.Time `bson:"ended_at,omitempty"`
}

// Progress est l'avancement D'UNE personne sur UN objectif.
type Progress struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`
	ChallengeID primitive.ObjectID `bson:"challenge_id"`
	UserID      primitive.ObjectID `bson:"user_id"`
	Country     string             `bson:"country,omitempty"`
	// Value : où la personne en est.
	Value int `bson:"value"`
	// Refs : les références DÉJÀ COMPTÉES — identifiants de course, de
	// commande, ou jours (`2026-10-09`) pour `days_active`.
	//
	// ⚠️ C'EST LA DÉDUPLICATION, ET ELLE N'EST PAS OPTIONNELLE. Une verticale
	// qui réessaie un appel après un délai d'attente compterait deux fois la
	// même course — et un objectif à 20 se gagnerait à 10. Le compteur seul
	// aurait été faux sans que rien ne le dise, et le bonus payé pour de bon.
	//
	// ⚠️ BORNÉE : `$slice` garde les dernières. Au-delà de la cible, la
	// déduplication n'a plus d'objet (c'est déjà gagné), et un tableau sans
	// borne ferait grossir le document à chaque course d'un chauffeur actif.
	Refs []string `bson:"refs,omitempty"`
	// ReachedAt : quand la cible a été franchie. C'est CE moment qui ouvre le
	// droit au bonus, pas la fin de la fenêtre.
	ReachedAt *time.Time `bson:"reached_at,omitempty"`
	// PaidAt, PaidXOF : le bonus versé.
	//
	// ⚠️ DEUX TEMPS, PARCE QUE ATTEINDRE ET ÊTRE PAYÉ NE SONT PAS LE MÊME
	// INSTANT. Le versement peut échouer (portefeuille illisible, verticale
	// muette) ; fondre les deux aurait fait disparaître le droit de quelqu'un
	// parce qu'un appel a raté, et personne ne l'aurait su.
	PaidAt  *time.Time `bson:"paid_at,omitempty"`
	PaidXOF int        `bson:"paid_xof,omitempty"`
	// Missed : la cible a été franchie, mais l'enveloppe était vide.
	//
	// ⚠️ IL EXISTE POUR ÊTRE LU, ET C'EST UN AVEU. Si ce champ se remplit, une
	// promesse a été faite et non tenue : l'exploitation DOIT le voir, décider
	// de payer à la main ou d'augmenter l'enveloppe, et comprendre que son
	// objectif était sous-doté. Le cacher aurait transformé un engagement en
	// loterie silencieuse.
	Missed bool `bson:"missed,omitempty"`

	UpdatedAt time.Time `bson:"updated_at"`
}

// RefsKept borne les références gardées pour la déduplication.
//
// ⚠️ DEUX CENTS, et c'est un compromis : au-delà de la cible on n'a plus besoin
// de dédupliquer, et un chauffeur très actif ferait grossir le document à chaque
// course. Deux cents couvre largement les objectifs réalistes (la plus haute
// cible humainement sûre est de l'ordre de 12 × 90 jours, et une cible aussi
// grande est refusée par `CheckTarget`).
const RefsKept = 200
