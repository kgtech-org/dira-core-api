package user

import (
	"context"
	"log/slog"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
)

// UN CHAUFFEUR VTC N'EST JAMAIS LIVREUR — l'appartenance d'un compte d'agent.
//
// ⚠️ LA DOCTRINE N'ÉTAIT APPLIQUÉE PAR RIEN. Les deux profils d'agent — le
// chauffeur des courses, le livreur de la livraison — naissaient d'une simple
// LECTURE : `GET /vtc/drivers/me` garantissait le profil de chauffeur,
// `PATCH /food/agent/availability` celui du livreur. Un compte de rôle
// `driver` qui ouvrait les deux applications obtenait les deux profils, en
// silence, et la plateforme comptait deux métiers pour une personne.
//
// ⚠️ ET LA RÈGLE « UN SEUL APPAREIL PAR CHAUFFEUR » REND CETTE VIOLATION
// BEAUCOUP PLUS COÛTEUSE. Les deux applications déclaraient `app: "driver"` et
// le registre d'appareils est clé par COMPTE ; deux applications sur le MÊME
// téléphone sont deux installations, donc deux `device_id`. Chacune chassait
// l'autre : la personne ouvrait la livraison, elle était déconnectée des
// courses ; elle rouvrait les courses, elle était déconnectée de la livraison
// — en boucle, sur un seul téléphone, avec un écran qui annonçait « vous vous
// êtes connecté sur un autre appareil » en nommant SON PROPRE téléphone.
// Exact, et incompréhensible.
//
// D'où ce mot sur le compte, et les DEUX moitiés qui le font vivre :
//
//  1. les VERTICALES le RÉCLAMENT au moment où elles garantissent le profil,
//     de façon idempotente (`ClaimAgentApp` ci-dessous) ;
//  2. le socle REFUSE À LA CONNEXION quand l'appartenance existe et que l'`app`
//     déclarée la contredit — voir `allowedInApp` dans `service.go`.
//
// ⚠️ LA PREMIÈRE MOITIÉ EST CE QUI REND LA MIGRATION SÛRE, et ce n'est pas un
// détail d'implémentation. On ne peut PAS laisser « le premier qui se
// connecte » décider : aucun compte existant ne porte d'appartenance, et si un
// livreur en place ouvrait par curiosité l'application des courses, il
// réserverait `driver` puis se verrait refuser l'entrée DANS SA PROPRE
// APPLICATION, pour toujours. L'appartenance doit donc venir de la RÉALITÉ —
// le profil qui existe déjà —, et c'est pourquoi ce sont les verticales qui la
// déclarent : elles la réclament à chaque ouverture d'application, et les
// comptes existants se rangent tout seuls.
//
// ⚠️ LA FRONTIÈRE NOUVELLE N'EST PAS LE RÔLE, C'EST LE MÉTIER. Un `courier` a
// le rôle `driver` au socle, comme un chauffeur : il reçoit le même
// portefeuille de jetons, il est borné au même appareil unique, il ouvre les
// mêmes pièces de conformité. Tout ce qui dérive du RÔLE ne bouge pas.

// Les deux applications d'agent. Ce sont aussi les deux valeurs que `app`
// accepte à la connexion pour ces comptes.
const (
	AgentAppDriver  = "driver"  // les courses (VTC)
	AgentAppCourier = "courier" // la livraison
)

// agentApps : les applications dont l'appartenance se décide. `client`,
// `merchant` et `console` n'en font pas partie — deux applications de client
// se partagent la même personne sans difficulté, et c'est très bien ainsi.
var agentApps = map[string]bool{AgentAppDriver: true, AgentAppCourier: true}

// IsAgentApp dit si ce mot désigne une application d'agent.
func IsAgentApp(app string) bool { return agentApps[app] }

// newAgentApp est l'appartenance d'un compte QUI VIENT DE NAÎTRE : celle que
// l'application déclare, et rien si elle ne déclare rien.
//
// ⚠️ SEULEMENT POUR LE RÔLE `driver`. Un client, un marchand, un
// administrateur n'appartiennent à aucune application d'agent, et leur en
// poser une les enfermerait dehors de la leur à la connexion suivante.
func newAgentApp(role, app string) string {
	if role != auth.RoleDriver || !agentApps[app] {
		return ""
	}
	return app
}

// ClaimAgentApp réserve l'appartenance de ce compte à une application d'agent,
// et rend celle qui est EN VIGUEUR après l'appel.
//
// IDEMPOTENTE, et elle doit le rester : les verticales l'appellent à chaque
// ouverture d'application. Réclamer ce qui est déjà acquis n'écrit rien.
//
// ⚠️ LA PREMIÈRE RÉCLAMATION GAGNE, LA SECONDE EST REFUSÉE — `403 wrong_app`,
// avec `reason` nommant l'application à laquelle le compte appartient. C'est
// le MÊME refus que celui de la connexion, et c'est délibéré : une application
// qui sait déjà afficher « ouvrez l'autre application » n'a rien de nouveau à
// apprendre, et une seule phrase couvre les deux portes.
//
// ⚠️ ELLE NE SUPPRIME AUCUN PROFIL et n'en juge aucun. Deux comptes de la
// recette portent déjà les deux profils — nés avant cette règle : la verticale
// qui réclame la première les emporte, l'autre est refusée, et c'est à
// l'exploitation de trancher ce qu'il faut en faire. Décider ici, en silence,
// effacerait le travail de quelqu'un.
//
// `fromProfile` dit si la verticale VOIT DÉJÀ un profil pour ce compte, c'est
// à dire si l'appartenance vient d'un fait ou d'une application qui frappe à
// la porte. Elle ne change rien à l'écriture : elle est journalisée, pour
// qu'on puisse relire la migration et savoir d'où vient chaque appartenance.
func (s *Service) ClaimAgentApp(ctx context.Context, userID, app string, fromProfile bool) (string, error) {
	if !agentApps[app] {
		return "", apperr.Validation("app must be driver or courier").
			WithMeta(map[string]any{"fields": []string{"app"}})
	}
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return "", errUserNotFound.WithCause(err)
	}
	u, err := s.repo.FindByID(ctx, oid)
	if err != nil {
		return "", apperr.Internal(err)
	}
	if u == nil {
		return "", errUserNotFound
	}
	// ⚠️ L'ADMINISTRATION PASSE PARTOUT, ici comme à la connexion : un
	// opérateur du support ouvre l'application d'un chauffeur pour reproduire
	// ce qu'on lui décrit. Lui POSER une appartenance l'enfermerait ensuite
	// dans cette application-là — et le rendrait aveugle à l'autre.
	if u.Role == auth.RoleAdmin {
		return "", nil
	}
	if u.AgentApp == app {
		return app, nil
	}
	if u.AgentApp != "" {
		// ⚠️ JOURNALISÉ, PAS TRACÉ À L'AUDIT. Ce refus se répète à chaque
		// ouverture de l'application perdante, tous les jours : au journal
		// d'audit il noierait les gestes humains qu'on y cherche. Dans les
		// logs, il se compte et se retrouve.
		slog.WarnContext(ctx, "user: appartenance d'agent déjà prise par l'autre application",
			"user_id", userID, "agent_app", u.AgentApp, "claimed", app, "from_profile", fromProfile)
		return u.AgentApp, refuseApp(app, u.AgentApp, u.Role)
	}
	if err := s.repo.SetAgentApp(ctx, oid, app); err != nil {
		return "", apperr.Internal(err)
	}
	slog.InfoContext(ctx, "user: appartenance d'agent réclamée",
		"user_id", userID, "agent_app", app, "from_profile", fromProfile)
	if s.auditor != nil {
		// ⚠️ TRACÉE, et c'est ce qui permettra de relire la migration : 24
		// livreurs et 33 chauffeurs se rangent tout seuls à leur première
		// ouverture, et sans cette entrée personne ne pourrait dire lesquels
		// l'ont fait, quand, ni depuis quel profil.
		s.auditor.Record(ctx, "user.agent_app.claim", "user", userID, nil,
			map[string]any{"agent_app": app, "from_profile": fromProfile})
	}
	return app, nil
}

// ReleaseAgentApp LIBÈRE l'appartenance d'un compte, et rend celle qui vient
// d'être rendue (vide s'il n'y en avait pas).
//
// ⚠️ C'EST LA SORTIE DU SUPPORT, ET ELLE EST INDISPENSABLE. Une personne
// change de métier — livreuse hier, chauffeuse aujourd'hui, la moto vendue et
// la voiture louée. Sans ce geste, le support n'a AUCUNE issue : il ouvrira un
// second compte à la même personne, avec un second téléphone, un second
// portefeuille et un second historique — c'est-à-dire exactement le désordre
// que cette règle existe pour éviter.
//
// IDEMPOTENTE : libérer ce qui est déjà libre réussit sans rien écrire et sans
// rien tracer. Un geste d'administration qui échoue parce qu'il a déjà été
// fait pousse à s'acharner.
//
// ⚠️ ELLE NE SUPPRIME AUCUN PROFIL. La personne garde son profil de livreuse
// et tout ce qu'il porte ; elle peut maintenant, en ouvrant l'application des
// courses, réclamer `driver`. Ce qui ne doit surtout pas être fait ICI, en
// devinant : c'est l'application qu'elle ouvre qui le dira.
func (s *Service) ReleaseAgentApp(ctx context.Context, id string) (string, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return "", errUserNotFound.WithCause(err)
	}
	u, err := s.repo.FindByID(ctx, oid)
	if err != nil {
		return "", apperr.Internal(err)
	}
	if u == nil {
		return "", errUserNotFound
	}
	if u.AgentApp == "" {
		return "", nil
	}
	released := u.AgentApp
	if err := s.repo.SetAgentApp(ctx, oid, ""); err != nil {
		return "", apperr.Internal(err)
	}
	if s.auditor != nil {
		// ⚠️ AU JOURNAL D'AUDIT, celui-ci, à la différence de la réclamation :
		// c'est un geste HUMAIN, rare, et il rouvre une porte que la
		// plateforme avait fermée. « Qui a laissé ce compte changer de
		// métier, et quand ? » est une question qui sera posée.
		s.auditor.Record(ctx, "user.agent_app.release", "user", id,
			map[string]any{"agent_app": released}, map[string]any{"agent_app": nil})
	}
	return released, nil
}
