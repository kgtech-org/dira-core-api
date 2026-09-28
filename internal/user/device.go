package user

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/audit"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/session"
)

// UN SEUL APPAREIL PAR CHAUFFEUR — et par livreur.
//
// ⚠️ LA RAISON N'EST PAS LA SÉCURITÉ, C'EST LE TERRAIN. Un chauffeur connecté
// sur deux téléphones pousse DEUX flux de positions pour un seul véhicule : le
// vivier d'appel voit la même voiture à deux endroits, l'appel part vers le
// téléphone resté à la maison, et la course meurt d'un « personne n'a répondu »
// que rien n'explique. Le passager, lui, regarde une carte où sa voiture
// saute d'un quartier à l'autre.
//
// La dernière connexion gagne. Pas la première : un chauffeur dont le téléphone
// tombe en panne en pleine course doit pouvoir reprendre sur un autre appareil
// à l'instant même — et c'est précisément là qu'il ne faut surtout pas lui
// répondre « vous êtes déjà connecté ailleurs ».
//
// ⚠️ NE CONCERNE QUE LE RÔLE `driver`. Un client a le droit d'être sur sa
// tablette ET son téléphone ; un membre du staff ouvre trois onglets de
// console ; un marchand tient sa caisse sur deux écrans. Leur jeton ne nomme
// aucun appareil et rien ne change pour eux. C'est le RÔLE qui décide, pas
// l'application demandée : un opérateur du support qui ouvre l'application
// chauffeur pour reproduire un incident ne doit pas déconnecter le chauffeur
// qu'il essaie d'aider.

// errSessionSuperseded : ce jeton de rafraîchissement appartient à un appareil
// qui n'est plus celui du compte.
//
// ⚠️ UN CODE À LUI, ET C'EST TOUT L'INTÉRÊT DU MÉCANISME côté application. Avec
// `invalid_token`, l'écran affiche « session expirée » ; la personne se
// reconnecte, ça marche, et elle chasse à son tour l'autre téléphone — une
// bascule sans fin entre deux appareils dont personne ne comprend la cause, et
// un ticket de support par jour. Nommé, le refus devient une phrase : « vous
// avez été déconnecté parce que vous vous êtes connecté sur <appareil> ».
//
// `reason` porte le LIBELLÉ de l'appareil qui a pris la place. C'est la seule
// clé de `Meta` qui atteint le réseau, avec `fields`.
var errSessionSuperseded = apperr.Unauthorized("session_superseded",
	"this device is no longer the one signed in to this account")

// KeySessionSuperseded est la clé du message « votre compte vient d'être ouvert
// sur un autre appareil ».
//
// Déclarée ICI parce que c'est ce module qui l'émet — la même convention que
// les clés du guichet de support, déclarées dans `pkg/support`. Le gabarit,
// lui, vit dans `internal/notify` avec tous les autres, et un test de ce
// paquet-là vérifie qu'il existe : une clé sans gabarit est une notification
// qui ne part jamais, sans erreur.
const KeySessionSuperseded = "session_superseded"

// SessionRegistry est ce que ce service demande au registre des appareils.
//
// Déclarée ICI, côté consommateur : `internal/user` n'a pas à connaître Redis,
// et un déploiement sans registre doit démarrer — le champ reste nul et le
// mécanisme se limite alors à la trace durable sur le compte.
type SessionRegistry interface {
	Bind(ctx context.Context, userID string, d session.Device) (session.Device, bool)
	Assert(ctx context.Context, userID string, d session.Device)
	Release(ctx context.Context, userID, deviceID string)
}

// Notifier est ce que ce service demande au module de notification : prévenir
// la personne qu'une session vient de s'ouvrir ailleurs sur son compte.
type Notifier interface {
	Notify(ctx context.Context, userID, key string, vars map[string]string, data map[string]string)
}

// SetSessions branche le registre des appareils. FACULTATIF, et il doit le
// rester : sans lui, la connexion et le rafraîchissement fonctionnent comme
// avant — c'est l'état d'un déploiement où Redis n'est pas partagé.
func (s *Service) SetSessions(r SessionRegistry) { s.sessions = r }

// SetNotifier branche les notifications. FACULTATIF : une notification qui
// n'arrive pas ne doit pas empêcher quelqu'un de se connecter.
func (s *Service) SetNotifier(n Notifier) { s.notifier = n }

// SetAuditor branche le journal d'audit.
//
// ⚠️ « Pourquoi ai-je été déconnecté ? » est la question que le support va
// recevoir, et sans entrée de journal il n'a rien à répondre : la chasse ne
// laisse aucune trace ailleurs — le compte porte l'appareil COURANT, pas
// l'histoire.
func (s *Service) SetAuditor(a *audit.Recorder) { s.auditor = a }

// maxDeviceID et maxDeviceName bornent ce qu'une application nous fait écrire.
//
// ⚠️ CE QUI ARRIVE DU CLIENT EST STOCKÉ, PUIS RENVOYÉ DANS UN MESSAGE
// D'ERREUR. Non borné, il suffit d'une application qui envoie dix kilo-octets
// de « nom d'appareil » pour que chaque refus les recopie. On tronque plutôt
// qu'on refuse : un libellé trop long est un détail d'affichage, et refuser la
// CONNEXION d'un chauffeur pour ça serait hors de proportion — à la différence
// de l'identifiant, qui décide de qui travaille et doit donc être exact.
const (
	maxDeviceID   = 128
	maxDeviceName = 120
)

// deviceFrom lit l'appareil déclaré par l'application, ou rien.
//
// ⚠️ ABSENT = AUCUNE RÈGLE D'APPAREIL, le comportement d'avant. Une application
// pas encore mise à jour ne doit pas voir ses utilisateurs enfermés dehors du
// jour au lendemain — c'est la même convention que le champ `app`, et elle vaut
// pour la même raison : le déploiement du serveur précède toujours celui des
// applications, de plusieurs semaines quand un magasin est lent à valider.
func deviceFrom(app, id, name string) (deviceID, deviceName string) {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > maxDeviceID {
		return "", ""
	}
	name = strings.TrimSpace(name)
	if len(name) > maxDeviceName {
		name = name[:maxDeviceName]
	}
	_ = app
	return id, name
}

// singleDevice dit si ce compte est borné à un seul appareil.
func singleDevice(u *User) bool { return u != nil && u.Role == auth.RoleDriver }

// claimDevice fait de l'appareil déclaré CELUI du compte, et chasse le
// précédent. Rend l'identifiant à inscrire dans le jeton — vide quand il n'y a
// pas de règle d'appareil pour ce compte.
//
// L'ORDRE DES GESTES N'EST PAS LIBRE :
//
//  1. jeter les jetons de rafraîchissement AVANT d'en émettre un nouveau,
//     sinon on jette celui qu'on vient de créer et personne ne reste connecté ;
//  2. écrire l'appareil sur le compte AVANT d'annoncer la chasse, sinon
//     l'ancien téléphone se fait fermer son socket puis se rafraîchit avec
//     succès, se reconnecte, et la bascule recommence ;
//  3. annoncer EN DERNIER : l'annonce ferme des sockets, et il ne faut pas
//     fermer celui de quelqu'un dont on n'a pas encore enregistré le
//     remplaçant.
func (s *Service) claimDevice(ctx context.Context, u *User, app, rawID, rawName string) (string, bool) {
	if !singleDevice(u) {
		return "", false
	}
	id, name := deviceFrom(app, rawID, rawName)
	if id == "" {
		// ⚠️ ON NE TOUCHE À RIEN. L'appareil déjà enregistré reste en place et
		// le jeton émis ne nomme aucun appareil : cette session-ci n'est ni
		// protégée ni chassante. C'est le prix exact de la compatibilité, et
		// il cesse d'être payé dès que l'application envoie son identifiant.
		return "", false
	}

	previous := u.Device
	chased := previous != nil && previous.ID != id
	now := time.Now().UTC()

	// (1) Les jetons de l'appareil précédent ne valent plus rien. Y compris
	// ceux du MÊME appareil : une reconnexion remplace la session, elle ne
	// s'ajoute pas à elle.
	if _, err := s.repo.DeleteRefreshTokensOfUser(ctx, u.ID); err != nil {
		// ⚠️ ON N'ARRÊTE PAS LA CONNEXION POUR ÇA. Le chauffeur qui n'arrive
		// pas à se connecter ne travaille pas, et le support ne peut rien pour
		// lui ; un jeton de rafraîchissement survivant, lui, sera refusé au
		// premier usage par la comparaison d'appareil ci-dessous.
		slog.ErrorContext(ctx, "user: anciens jetons de rafraîchissement non jetés",
			"user_id", u.ID.Hex(), "error", err)
	}

	d := &Device{ID: id, Name: name, App: app, Since: now}
	if chased {
		d.PreviousID, d.PreviousName = previous.ID, previous.Name
		d.SupersededAt = &now
	} else if previous != nil {
		// Même appareil : on garde l'histoire de ce qu'il avait chassé, sinon
		// le support perd la seule trace d'un second téléphone au premier
		// redémarrage de l'application.
		d.PreviousID, d.PreviousName, d.SupersededAt = previous.PreviousID, previous.PreviousName, previous.SupersededAt
		d.Since = previous.Since
	}

	// (2) La trace DURABLE, avant tout le reste.
	if err := s.repo.SetDevice(ctx, u.ID, d); err != nil {
		slog.ErrorContext(ctx, "user: appareil non enregistré sur le compte",
			"user_id", u.ID.Hex(), "error", err)
		return "", false
	}
	u.Device = d

	// (3) Le registre partagé : c'est lui que lisent les verticales et le
	// suivi. Il annonce la chasse sur son canal.
	if s.sessions != nil {
		s.sessions.Bind(ctx, u.ID.Hex(), session.Device{ID: id, Name: name})
	}

	if chased {
		s.recordSupersede(ctx, u, previous, d)
	}
	return id, chased
}

// recordSupersede laisse une trace de la chasse et prévient la personne.
//
// ⚠️ LA NOTIFICATION EST ÉCRITE COMME UNE ANNONCE, PAS COMME UN REPROCHE : le
// module de notification adresse un COMPTE, pas un appareil, et pousse donc à
// TOUS les téléphones du compte. « Vous avez été déconnecté » arriverait aussi
// sur le téléphone qui vient de se connecter, qui ne comprendrait rien. « Votre
// compte vient d'être ouvert sur <appareil> » se lit juste des deux côtés — et
// c'est aussi, pour celui qui n'a pas fait la manipulation, la seule alerte
// qu'il reçoive.
func (s *Service) recordSupersede(ctx context.Context, u *User, previous, current *Device) {
	slog.InfoContext(ctx, "user: session reprise par un autre appareil",
		"user_id", u.ID.Hex(), "device", current.ID, "previous_device", previous.ID,
		"reinstall", previous.Name != "" && previous.Name == current.Name)
	if s.auditor != nil {
		s.auditor.Record(ctx, "session.superseded", "user", u.ID.Hex(),
			map[string]any{"device_id": previous.ID, "device_name": previous.Name},
			map[string]any{"device_id": current.ID, "device_name": current.Name, "app": current.App})
	}
	if s.notifier == nil {
		return
	}
	name := current.Name
	if name == "" {
		name = "un nouvel appareil"
	}
	s.notifier.Notify(ctx, u.ID.Hex(), KeySessionSuperseded,
		map[string]string{"device": name},
		map[string]string{"type": "session_superseded", "device_id": current.ID})
}

// deviceForRefresh vérifie que le jeton présenté vient bien de l'appareil qui
// détient la session, et réaffirme le registre.
//
// ⚠️ C'EST LA PORTE DURABLE, celle qui tient quand Redis a tout oublié. Le
// registre fait refuser un jeton d'ACCÈS dans les six services ; mais un jeton
// d'accès vit quinze minutes, et sans ce contrôle-ci l'appareil chassé s'en
// serait simplement refait un autre, indéfiniment. C'est ici que la chasse
// devient définitive.
//
// ⚠️ ELLE PASSE AVANT LA CONSOMMATION DU JETON, et cet ordre porte tout le
// message : le hash a déjà été effacé par la connexion qui a chassé, donc la
// consommation aurait répondu `invalid_token` — « reconnectez-vous » — sans
// jamais dire pourquoi. En vérifiant l'appareil d'abord, l'ancien téléphone
// reçoit un refus NOMMÉ, à chaque tentative, tant qu'il n'a pas compris.
func (s *Service) deviceForRefresh(ctx context.Context, u *User, claimed string) (string, error) {
	if !singleDevice(u) || claimed == "" {
		return "", nil
	}
	if u.Device == nil {
		// Aucun appareil enregistré : rien ne dit que celui-ci a été chassé,
		// et refuser sur une ignorance déconnecterait un chauffeur au travail.
		return claimed, nil
	}
	if u.Device.ID != claimed {
		holder := u.Device.Name
		if holder == "" {
			return "", errSessionSuperseded
		}
		return "", errSessionSuperseded.WithMeta(map[string]any{
			"reason": holder,
			"device": holder,
		})
	}
	// LA RÉPARATION DU REGISTRE. Redis n'est pas durable ; sans cette ligne,
	// un `FLUSHDB` de maintenance laissait le registre vide jusqu'à la
	// prochaine CONNEXION de chaque chauffeur — des semaines pour qui ne se
	// déconnecte jamais — et pendant tout ce temps un téléphone chassé
	// redevenait accepté par le suivi.
	if s.sessions != nil {
		s.sessions.Assert(ctx, u.ID.Hex(), session.Device{ID: u.Device.ID, Name: u.Device.Name})
	}
	return claimed, nil
}

// releaseDevice rend la session à personne — une déconnexion volontaire.
//
// ⚠️ SEULEMENT SI C'EST BIEN L'APPAREIL COURANT. Sans cette comparaison, il
// suffisait de se déconnecter proprement sur l'ancien téléphone pour effacer la
// session du nouveau, qui se retrouvait sans appareil enregistré — donc de
// nouveau chassable par n'importe quoi, et surtout plus protégé du tout.
func (s *Service) releaseDevice(ctx context.Context, u *User, claimed string) {
	if !singleDevice(u) || claimed == "" || u.Device == nil || u.Device.ID != claimed {
		return
	}
	if err := s.repo.SetDevice(ctx, u.ID, nil); err != nil {
		slog.WarnContext(ctx, "user: appareil non effacé à la déconnexion",
			"user_id", u.ID.Hex(), "error", err)
	}
	if s.sessions != nil {
		s.sessions.Release(ctx, u.ID.Hex(), claimed)
	}
}

// sessionResponse décrit à l'application la session qu'elle vient d'ouvrir :
// sur quel appareil, et lequel elle a chassé.
//
// ⚠️ ELLE EXISTE POUR QUE LE NOUVEAU TÉLÉPHONE PUISSE LE DIRE. Sans elle,
// personne n'apprend jamais qu'une session a été fermée ailleurs : l'ancien
// téléphone, lui, est peut-être éteint. « Votre session sur Tecno Spark 10 a
// été fermée » au moment de la connexion est la seule occasion de prévenir
// quelqu'un dont le compte servait ailleurs.
// ⚠️ LA SESSION CHASSÉE N'EST ANNONCÉE QUE SI ELLE VIENT DE L'ÊTRE. Le compte
// garde l'histoire de ce que cet appareil a chassé — le support en a besoin —
// mais la rendre à chaque connexion ferait afficher « votre session sur X a été
// fermée » tous les matins, six mois après, pour un téléphone perdu depuis
// longtemps.
func sessionResponse(u *User, deviceID string, chased bool) *SessionResponse {
	if deviceID == "" || u.Device == nil {
		return nil
	}
	out := &SessionResponse{
		DeviceID:     u.Device.ID,
		DeviceName:   u.Device.Name,
		SingleDevice: true,
	}
	if chased {
		out.SupersededDeviceID = u.Device.PreviousID
		out.SupersededDeviceName = u.Device.PreviousName
	}
	return out
}
