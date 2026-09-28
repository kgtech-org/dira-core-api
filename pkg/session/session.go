// Package session tient LE REGISTRE DE L'APPAREIL COURANT d'un compte :
// quel téléphone détient la session, et depuis quand.
//
// ⚠️ IL EXISTE PARCE QU'UN JETON D'ACCÈS EST APATRIDE. Un chauffeur connecté
// sur deux téléphones pousse DEUX flux de positions pour un seul véhicule, et
// les deux reçoivent les appels de course : le vivier voit une voiture à deux
// endroits, et l'appel part vers le téléphone resté à la maison. Chasser la
// session précédente en base ne suffit pas — le jeton déjà émis continue d'être
// accepté partout où il est vérifié hors ligne, c'est-à-dire dans les six
// services, jusqu'à son expiration.
//
// Le registre est donc la LISTE DE RÉVOCATION, et elle vit dans Redis pour une
// raison précise : `dira-tracking` doit pouvoir la consulter. Ce service est le
// seul de la plateforme à ne dépendre d'AUCUN autre — c'est le seul qui puisse
// tomber sans emporter les courses — et lui faire appeler le socle à chaque
// poignée de main WebSocket aurait fait du socle le point de panne unique de la
// mise en ligne : socle en maintenance, plus un chauffeur en ligne. Redis est
// déjà partagé par les deux (positions, vivier, cadence) ; une clé de plus n'y
// crée aucune dépendance de code.
//
// ⚠️ AU MIEUX, TOUJOURS DANS LE SENS DE LAISSER TRAVAILLER. Redis injoignable,
// clé absente, base vidée : le registre répond « je ne sais pas » et l'appareil
// passe. La règle inverse — refuser ce qu'on ne sait pas — aurait déconnecté
// TOUS les chauffeurs de la plateforme au premier redémarrage de Redis, pour
// résoudre un problème qui concerne un chauffeur sur mille. La porte durable
// reste le rafraîchissement, qui lit Mongo : un appareil chassé y est refusé
// même si Redis a tout oublié, donc au pire il travaille le temps d'un jeton
// d'accès — quinze minutes.
package session

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// KeyPrefix est le préfixe des clés du registre. Le compte complète : une clé
// par compte, et non par jeton — le nombre de clés est donc borné par le
// nombre de chauffeurs, pas par le nombre de connexions.
const KeyPrefix = "dira:session:device:"

// Channel est le canal d'annonce d'une session chassée.
//
// ⚠️ IL NE REMPLACE PAS LA CLÉ, il la complète. Un message pub/sub est remis
// AU PLUS UNE FOIS : une réplique du suivi en train de redémarrer ne le voit
// jamais. Le canal donne l'IMMÉDIATETÉ (le socket de l'ancien téléphone se
// ferme dans la seconde) ; la clé donne la CERTITUDE (toute vérification
// ultérieure la lit). Se contenter du canal laissait un téléphone chassé
// pousser des positions jusqu'à l'expiration de son jeton, à chaque
// redéploiement du suivi.
const Channel = "dira:session:superseded"

// Device est l'appareil qui détient la session.
type Device struct {
	// ID est l'identifiant d'INSTALLATION de l'application, tiré par
	// l'application elle-même et conservé à côté du jeton de
	// rafraîchissement. Opaque pour nous.
	ID string `json:"id"`
	// Name est le libellé LISIBLE — « Tecno Spark 10 · Android 13 ». Il ne
	// sert qu'à une chose, et elle compte : dire à la personne OÙ sa session
	// est ouverte. « Vous avez été déconnecté » sans nommer l'appareil laisse
	// croire à une panne.
	Name string `json:"name,omitempty"`
}

// Superseded est ce qui passe sur le canal : le compte, et l'appareil qui
// vient de PRENDRE la place.
//
// C'est le nouvel appareil qu'on annonce, pas l'ancien : le lecteur ferme
// alors tout ce qui n'est PAS lui, ce qui reste juste même s'il en existait
// trois — un socket resté ouvert sur un téléphone éteint depuis la veille est
// fermé lui aussi.
type Superseded struct {
	UserID   string `json:"user_id"`
	DeviceID string `json:"device_id"`
	// DeviceName sert les journaux d'exploitation : « la session de ce
	// compte est passée sur <appareil> » se lit, un identifiant d'installation
	// ne se lit pas.
	DeviceName string `json:"device_name,omitempty"`
	At         int64  `json:"at"` // millisecondes UTC
}

// Registry est le registre, appuyé sur Redis.
//
// Un `*Registry` NUL est utilisable et ne refuse rien : c'est l'état d'un
// déploiement où le registre n'est pas branché, et il doit se comporter comme
// avant ce mécanisme.
type Registry struct {
	rdb *redis.Client
	ttl time.Duration
}

// New construit le registre. `ttl` est la durée de vie d'une entrée : la même
// que le jeton de rafraîchissement, parce qu'une session qui ne peut plus se
// rafraîchir est morte, et qu'une clé qui lui survivrait ne désignerait plus
// rien. Chaque rafraîchissement la remet à neuf (voir `Assert`).
func New(rdb *redis.Client, ttl time.Duration) *Registry {
	if rdb == nil {
		return nil
	}
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	return &Registry{rdb: rdb, ttl: ttl}
}

func key(userID string) string { return KeyPrefix + userID }

// bindScript pose l'appareil courant et REND le précédent, d'un seul geste.
//
// ⚠️ Un `HGETALL` suivi d'un `HSET` aurait suffi 999 fois sur 1000 — et la
// millième, deux connexions simultanées (le chauffeur appuie deux fois, le
// réseau double la requête) se seraient chacune annoncé comme ayant chassé
// l'autre. Deux annonces contradictoires sur le canal ferment les DEUX
// sockets : le chauffeur se retrouve déconnecté partout, sans comprendre.
//
//	KEYS[1] la clé du compte
//	ARGV[1] identifiant du nouvel appareil   ARGV[2] son libellé
//	ARGV[3] horodatage (ms)                  ARGV[4] TTL (ms)
//
// Rend {ancien identifiant, ancien libellé} — deux chaînes vides si aucune
// session n'était ouverte.
var bindScript = redis.NewScript(`
local prev = redis.call('HMGET', KEYS[1], 'id', 'name')
redis.call('HSET', KEYS[1], 'id', ARGV[1], 'name', ARGV[2], 'at', ARGV[3])
redis.call('PEXPIRE', KEYS[1], ARGV[4])
return { prev[1] or '', prev[2] or '' }
`)

// assertScript pose l'appareil courant et sa péremption d'un seul geste.
//
// ⚠️ UN SCRIPT PLUTÔT QUE DEUX APPELS : un `HSET` suivi d'un `PEXPIRE` laisse,
// si le processus meurt entre les deux, une clé SANS péremption — donc un
// appareil inscrit pour toujours, y compris après que son jeton de
// rafraîchissement a expiré. Une clé par compte, cela ne remplit pas Redis,
// mais cela désigne un téléphone dont plus rien ne prouve qu'il existe.
//
//	KEYS[1] la clé du compte
//	ARGV[1] identifiant   ARGV[2] libellé   ARGV[3] horodatage (ms)   ARGV[4] TTL (ms)
var assertScript = redis.NewScript(`
redis.call('HSET', KEYS[1], 'id', ARGV[1], 'name', ARGV[2], 'at', ARGV[3])
redis.call('PEXPIRE', KEYS[1], ARGV[4])
return 1
`)

// releaseScript n'efface QUE si l'appareil nommé est bien le courant.
//
// ⚠️ LA COMPARAISON EST DANS LE SCRIPT, PAS DANS LE CODE APPELANT. Lue puis
// comparée en Go, une nouvelle connexion pouvait se glisser entre la lecture et
// l'effacement : la déconnexion du téléphone chassé emportait alors la session
// de son remplaçant, qui se retrouvait sans appareil inscrit — donc plus
// protégé du tout, et de nouveau chassable par n'importe quoi.
//
//	KEYS[1] la clé du compte   ARGV[1] l'appareil qui se déconnecte
var releaseScript = redis.NewScript(`
if redis.call('HGET', KEYS[1], 'id') == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

// Bind fait de `d` l'appareil courant du compte et rend celui qu'il chasse.
//
// L'annonce sur le canal n'est faite QUE s'il y avait quelqu'un à chasser, et
// que ce n'était pas déjà lui : une reconnexion sur le même téléphone — le cas
// de loin le plus fréquent, à chaque réinstallation du jeton — ne doit
// réveiller personne.
func (r *Registry) Bind(ctx context.Context, userID string, d Device) (previous Device, chased bool) {
	if r == nil || userID == "" || d.ID == "" {
		return Device{}, false
	}
	now := time.Now().UTC()
	res, err := bindScript.Run(ctx, r.rdb, []string{key(userID)},
		d.ID, d.Name, now.UnixMilli(), r.ttl.Milliseconds()).Slice()
	if err != nil {
		// ⚠️ ON NE REFUSE PAS LA CONNEXION POUR ÇA. Un registre injoignable
		// doit coûter un doublon de session, pas une journée de travail
		// perdue : le chauffeur qui n'arrive pas à se connecter appelle le
		// support, et le support ne peut rien pour lui.
		slog.WarnContext(ctx, "session: registre injoignable, la session précédente n'est pas chassée",
			"user_id", userID, "error", err)
		return Device{}, false
	}
	previous = Device{ID: str(res, 0), Name: str(res, 1)}
	if previous.ID == "" || previous.ID == d.ID {
		return previous, false
	}
	r.announce(ctx, Superseded{
		UserID: userID, DeviceID: d.ID, DeviceName: d.Name, At: now.UnixMilli(),
	})
	return previous, true
}

// Assert réaffirme l'appareil courant SANS rien annoncer : ce que fait un
// rafraîchissement de jeton.
//
// ⚠️ C'EST LA RÉPARATION DU REGISTRE. Redis n'est pas durable — un
// redémarrage, un `FLUSHDB` de maintenance, et le registre est vide. Sans
// cette ligne, il resterait vide jusqu'à la prochaine CONNEXION de chaque
// chauffeur, c'est-à-dire potentiellement des semaines : pendant tout ce temps,
// un téléphone chassé serait de nouveau accepté par le suivi. Chaque
// rafraîchissement — au pire toutes les quinze minutes pour un chauffeur au
// travail — le remet en place.
func (r *Registry) Assert(ctx context.Context, userID string, d Device) {
	if r == nil || userID == "" || d.ID == "" {
		return
	}
	err := assertScript.Run(ctx, r.rdb, []string{key(userID)},
		d.ID, d.Name, time.Now().UTC().UnixMilli(), r.ttl.Milliseconds()).Err()
	if err != nil {
		slog.WarnContext(ctx, "session: registre non réaffirmé", "user_id", userID, "error", err)
	}
}

// Release efface l'entrée — une déconnexion volontaire.
//
// ⚠️ SEULEMENT SI C'EST BIEN CET APPAREIL. Un téléphone chassé qui se
// déconnecte proprement ne doit pas emporter la session de celui qui l'a
// remplacé : sans cette comparaison, il suffisait de se déconnecter sur
// l'ancien téléphone pour rendre le nouveau de nouveau chassable par
// n'importe quoi.
func (r *Registry) Release(ctx context.Context, userID, deviceID string) {
	if r == nil || userID == "" || deviceID == "" {
		return
	}
	if err := releaseScript.Run(ctx, r.rdb, []string{key(userID)}, deviceID).Err(); err != nil {
		slog.WarnContext(ctx, "session: appareil non libéré à la déconnexion",
			"user_id", userID, "error", err)
	}
}

// Current rend l'appareil courant. `known` est faux quand on ne sait pas —
// aucune session enregistrée, ou registre injoignable. Les deux se traitent
// pareil : on ne refuse rien sur une ignorance.
func (r *Registry) Current(ctx context.Context, userID string) (Device, bool) {
	if r == nil || userID == "" {
		return Device{}, false
	}
	vals, err := r.rdb.HMGet(ctx, key(userID), "id", "name").Result()
	if err != nil {
		slog.WarnContext(ctx, "session: registre illisible, l'appareil est accepté",
			"user_id", userID, "error", err)
		return Device{}, false
	}
	d := Device{ID: str(vals, 0), Name: str(vals, 1)}
	if d.ID == "" {
		return Device{}, false
	}
	return d, true
}

// Accepts dit si cet appareil est encore celui du compte.
//
// Vrai dès qu'un doute existe : appareil non nommé (jeton émis avant ce
// mécanisme, ou application pas encore à jour), aucune session enregistrée,
// registre muet. `chased` n'est vrai que sur une CERTITUDE — le registre
// nomme un AUTRE appareil — et il vient avec celui qui a pris la place, pour
// que le refus puisse le dire à la personne.
func (r *Registry) Accepts(ctx context.Context, userID, deviceID string) (ok bool, holder Device) {
	if r == nil || userID == "" || deviceID == "" {
		return true, Device{}
	}
	cur, known := r.Current(ctx, userID)
	if !known || cur.ID == deviceID {
		return true, cur
	}
	return false, cur
}

// announce dépose l'annonce sur le canal. Un échec est journalisé et non
// remonté : la clé, elle, est déjà posée — c'est elle qui garantit le refus.
func (r *Registry) announce(ctx context.Context, s Superseded) {
	payload, err := json.Marshal(s)
	if err != nil {
		return
	}
	if err := r.rdb.Publish(ctx, Channel, payload).Err(); err != nil {
		slog.WarnContext(ctx, "session: annonce non diffusée — l'ancien socket se fermera au prochain contrôle",
			"user_id", s.UserID, "error", err)
	}
}

func str(vals []any, i int) string {
	if i >= len(vals) {
		return ""
	}
	s, _ := vals[i].(string)
	return s
}
