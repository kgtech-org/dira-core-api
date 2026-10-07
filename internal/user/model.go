// Package user manages accounts, authentication (phone + password, JWT
// access/refresh) and profiles for the four platform roles.
package user

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Collection names.
const (
	usersCollection         = "users"
	refreshTokensCollection = "refresh_tokens"

	// Exportées pour le PROVISIONNEMENT (`cmd/seed`), qui doit pouvoir les
	// vider — et pour lui seul. Les nommer ailleurs ferait un second écrivain
	// sur les comptes.
	CollectionUsers         = usersCollection
	CollectionRefreshTokens = refreshTokensCollection
)

// Account statuses.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
	// StatusClosed : la personne a demandé la suppression de son compte. Il
	// ne se connecte plus, et son identité part à l'échéance du délai de
	// grâce — voir `erasure.go`.
	//
	// ⚠️ DISTINCT DE `suspended`, et il faut que ça le reste. Une suspension
	// est une décision de l'exploitation, qui se lève ; une fermeture est la
	// décision de la personne, et elle ne se lève que par le support. Les
	// confondre aurait fait « réactiver » des comptes dont quelqu'un a demandé
	// l'effacement.
	StatusClosed = "closed"
)

// User is a platform account. The phone number (E.164) is the primary
// identifier. PasswordHash is an encoded argon2id string and must never leave
// the service layer.
type User struct {
	ID    primitive.ObjectID `bson:"_id,omitempty"`
	Role  string             `bson:"role"`  // "client" | "driver" | "merchant" | "admin"
	Phone string             `bson:"phone"` // E.164, unique
	// Name est le nom d'AFFICHAGE. Il reste la source unique de vérité pour
	// tout ce qui affiche un nom : les prénom/nom séparés servent les
	// formulaires d'état civil, pas les écrans.
	Name string `bson:"name"`
	// FirstName et LastName sont facultatifs, et le resteront : une bonne
	// partie des utilisateurs s'inscrit avec un seul mot. Les DÉDUIRE en
	// coupant `name` à l'espace produirait des « Nom : Diallo Ndiaye » chez
	// tous ceux qui ont deux prénoms.
	FirstName string     `bson:"first_name,omitempty"`
	LastName  string     `bson:"last_name,omitempty"`
	BirthDate *time.Time `bson:"birth_date,omitempty"`
	Gender    string     `bson:"gender,omitempty"`
	Email     string     `bson:"email,omitempty"`
	AvatarURL string     `bson:"avatar_url,omitempty"`
	// Preferences : notifications par catégorie, thème, langue, sons.
	Preferences *Preferences `bson:"preferences,omitempty"`
	// Country est le PAYS DU COMPTE (ISO 3166-1 alpha-2) : la borne « haut
	// niveau » de tout ce que ce compte voit, inscrite dans son jeton à
	// chaque émission. Posé à l'inscription — en-tête de l'application,
	// sinon indicatif du téléphone, sinon pays par défaut — et réaligné par
	// `POST /me/country/resolve` quand l'application situe la personne
	// ailleurs. Voir `pkg/country`.
	Country      string `bson:"country,omitempty"`
	PasswordHash string `bson:"password_hash"`
	Status       string `bson:"status"` // "active" | "suspended" | "closed"
	// DeletionRequestedAt : la personne a demandé la suppression. Le compte
	// est fermé tout de suite, l'identité part à l'échéance du délai de grâce
	// — voir `erasure.go`.
	DeletionRequestedAt *time.Time `bson:"deletion_requested_at,omitempty"`
	// AnonymisedAt : l'identité EST partie. La ligne reste pour que les
	// courses et les commandes gardent une référence valide — elles ne
	// désignent plus personne.
	AnonymisedAt *time.Time `bson:"anonymised_at,omitempty"`
	CreatedAt    time.Time  `bson:"created_at"`
	UpdatedAt    time.Time  `bson:"updated_at"`
	// Device est L'APPAREIL COURANT, et il ne concerne QUE les chauffeurs et
	// les livreurs : eux seuls ne peuvent tenir qu'une session à la fois.
	// Absent pour les clients, les marchands et le staff — un client a le
	// droit d'être sur sa tablette et son téléphone.
	//
	// ⚠️ SUR LE COMPTE, ET NON DANS UNE COLLECTION À PART. Le document est
	// déjà lu à la connexion et à chaque rafraîchissement : y ranger
	// l'appareil ne coûte aucune requête de plus. Une collection dédiée
	// aurait demandé un index unique, une borne de pays, une exemption
	// nommée dans le test de frontière — pour trois champs qui n'existent
	// jamais sans leur compte.
	Device *Device `bson:"device,omitempty"`
	// AgentApp est L'APPLICATION D'AGENT à laquelle ce compte appartient —
	// `driver` (les courses) ou `courier` (la livraison). Voir `agentapp.go`
	// pour la doctrine et pour ce qui l'écrit.
	//
	// ⚠️ SUR LE COMPTE, comme `Device`, et pour les mêmes raisons : le
	// document est déjà lu à la connexion, y ranger un mot ne coûte aucune
	// requête de plus, et une collection dédiée aurait demandé un index
	// unique, une borne de pays et une exemption nommée dans le test de
	// frontière — pour UN champ qui n'existe jamais sans son compte.
	//
	// ⚠️ VIDE = APPARTENANCE INCONNUE, et rien n'est refusé. C'est la même
	// convention que `app` et `device_id` avant elle : le déploiement du
	// serveur précède toujours celui des applications, de plusieurs semaines
	// quand un magasin est lent à valider, et une application pas encore mise
	// à jour ne doit pas voir ses utilisateurs enfermés dehors.
	AgentApp string `bson:"agent_app,omitempty"`
}

// Device est l'appareil qui détient la session d'un chauffeur ou d'un livreur.
//
// ⚠️ C'EST LA TRACE DURABLE DE LA RÈGLE. Le registre Redis fait appliquer le
// refus dans les six services, mais Redis n'est pas durable : un redémarrage
// et il ne sait plus rien. Ce document-ci, lui, survit — c'est donc lui qui
// refuse le RAFRAÎCHISSEMENT d'un appareil chassé, et c'est ce qui borne la
// dérive à la durée d'un jeton d'accès quand le registre a été perdu.
type Device struct {
	// ID est l'identifiant d'INSTALLATION tiré par l'application et conservé
	// à côté de son jeton de rafraîchissement. Opaque : nous ne le
	// fabriquons pas et ne le déchiffrons pas.
	ID string `bson:"id"`
	// Name est le libellé LISIBLE — « Tecno Spark 10 · Android 13 ». Il sert
	// à dire à la personne OÙ sa session est ouverte, et au support à
	// distinguer « un second téléphone » d'« une réinstallation ».
	Name string `bson:"name,omitempty"`
	// App est l'application qui s'est connectée (`driver` pour les courses,
	// `courier` pour la livraison). Conservée pour le support : elle dit sur
	// quelle application cette session-ci a été ouverte, là où `AgentApp` dit
	// à laquelle le COMPTE appartient.
	App string `bson:"app,omitempty"`
	// Since est le moment où CET appareil a pris la session.
	Since time.Time `bson:"since"`
	// PreviousID et PreviousName sont l'appareil CHASSÉ par celui-ci, gardés
	// pour une seule question, la plus posée au support : « est-ce que
	// quelqu'un d'autre utilise mon compte ? ». Un même libellé avec un
	// identifiant différent, c'est une réinstallation ; deux libellés
	// différents, c'est un second téléphone.
	PreviousID   string     `bson:"previous_id,omitempty"`
	PreviousName string     `bson:"previous_name,omitempty"`
	SupersededAt *time.Time `bson:"superseded_at,omitempty"`
}

// RefreshToken stores the sha256 hash of an issued refresh token. Rotation
// deletes the old hash and inserts the new one; a TTL index on expires_at
// purges expired entries.
type RefreshToken struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	TokenHash string             `bson:"token_hash"` // sha256 hex, unique
	UserID    primitive.ObjectID `bson:"user_id"`
	// DeviceID est l'INSTALLATION qui tient cette session, quand
	// l'application l'a déclarée. Vide = inconnue, et la session compte alors
	// pour un appareil à elle seule — voir `devices.go`.
	DeviceID  string    `bson:"device_id,omitempty"`
	ExpiresAt time.Time `bson:"expires_at"`
	CreatedAt time.Time `bson:"created_at"`
}
