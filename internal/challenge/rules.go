package challenge

// LES RÈGLES D'UN OBJECTIF VALABLE — et la plus importante n'est pas technique.

import (
	"fmt"
	"net/http"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

var (
	errUnknownAudience = apperr.Validation(
		"unknown audience: use driver, courier or client").
		WithMeta(map[string]any{"fields": []string{"audience"}, "allowed": audiences})
	errUnknownRepeat = apperr.Validation(
		"unknown repeat: use none, weekly or monthly").
		WithMeta(map[string]any{"fields": []string{"repeat"}, "allowed": repeats})
	errNoTitle = apperr.Validation(
		"a title is required: it is what the person reads and what makes them want to try").
		WithMeta(map[string]any{"fields": []string{"title"}})
	errNoTarget = apperr.Validation("target must be at least 1").
			WithMeta(map[string]any{"fields": []string{"target"}})
	errNoReward = apperr.Validation(
		"reward_xof must be at least 1: an objective without a bonus is an announcement, not a challenge").
		WithMeta(map[string]any{"fields": []string{"reward_xof"}})
	errBadWindow = apperr.Validation("window: `from` must be before `to`").
			WithMeta(map[string]any{"fields": []string{"window"}})
	errWindowTooLong = apperr.Validation(
		fmt.Sprintf("window is longer than %d days: that is a loyalty programme, not an objective", MaxDays)).
		WithMeta(map[string]any{"fields": []string{"window"}, "max_days": MaxDays})
	// ⚠️ LE REFUS LE PLUS IMPORTANT DE CE FICHIER — voir `CheckTarget`.
	// ⚠️⚠️ CES DEUX REFUS PORTENT UN CODE, et les autres de ce fichier non. La
	// différence n'est pas cosmétique : la TRADUCTION SE FAIT PAR CODE
	// (`httpx.Message` → `errors.<code>`), et `apperr.Validation` produit le
	// code générique `validation_failed`. Toute phrase écrite là est donc
	// remplacée par « Requête invalide ».
	//
	// Pour la plupart des validations, ça suffit : le champ nommé dans `meta`
	// dit ce qui n'allait pas, et l'écran connaît le contexte. Mais ces deux-ci
	// PROPOSENT UNE SORTIE — « allongez la fenêtre », « dotez l'enveloppe » —, et
	// une sortie que personne ne lit n'existe pas. C'est la seule raison
	// d'écrire une phrase dans un refus : qu'elle évite un appel au support.
	//
	// Relevé le 9 octobre en essayant une route voisine pour de vrai : le
	// serveur répondait « Requête invalide » sur un refus dont le texte avait
	// été pesé mot à mot.
	errTargetUnsafe = apperr.New("challenge_target_unsafe",
		"target asks for more work per day than anyone should do: lower it, or lengthen the window",
		http.StatusUnprocessableEntity).
		WithMeta(map[string]any{"fields": []string{"target", "window"}})
	errBudgetTooSmall = apperr.New("challenge_budget_too_small",
		"budget_xof cannot pay a single winner: raise the budget or lower the reward",
		http.StatusUnprocessableEntity).
		WithMeta(map[string]any{"fields": []string{"budget_xof", "reward_xof"}})
)

// errMetricRefused dit POURQUOI une mesure a été écartée, avec sa raison.
//
// ⚠️ LE MESSAGE PORTE LA RAISON, et ne dit pas « inconnue ». Quelqu'un qui tente
// `acceptance_rate` ne s'est pas trompé de frappe : il a eu une idée, et elle a
// déjà été examinée. Lui répondre « mesure inconnue » le ferait chercher une
// faute d'orthographe, puis demander à un développeur de l'ajouter.
func errMetricRefused(key, reason string) error {
	return apperr.Validation("metric `" + key + "` is refused on purpose: " + reason).
		WithMeta(map[string]any{"fields": []string{"metric"}, "reason": reason})
}

var errUnknownMetric = apperr.Validation("unknown metric").
	WithMeta(map[string]any{"fields": []string{"metric"}})

// errMetricNotForAudience : la mesure existe, mais pas pour ce public.
func errMetricNotForAudience(metric, audience string) error {
	return apperr.Validation("metric `" + metric + "` does not apply to " + audience).
		WithMeta(map[string]any{
			"fields": []string{"metric", "audience"},
			"allowed": func() []string {
				out := []string{}
				for _, m := range MetricsFor(audience) {
					out = append(out, m.Key)
				}
				return out
			}(),
		})
}

// Check valide un objectif, dans l'ordre où les refus se lisent.
func Check(c *Challenge) error {
	if !ValidAudience(c.Audience) {
		return errUnknownAudience
	}
	if !ValidRepeat(c.Repeat) {
		return errUnknownRepeat
	}
	if c.Title == "" {
		return errNoTitle
	}
	// ⚠️ LA MESURE REFUSÉE AVANT LA MESURE INCONNUE. Les trois mesures écartées
	// sont techniquement disponibles : répondre « inconnue » enverrait quelqu'un
	// chercher une faute de frappe, puis demander qu'on l'ajoute. La raison du
	// refus est dans le message.
	if reason, refused := Forbidden[c.Metric]; refused {
		return errMetricRefused(c.Metric, reason)
	}
	m, ok := LookupMetric(c.Metric)
	if !ok {
		return errUnknownMetric
	}
	if !metricAllows(m, c.Audience) {
		return errMetricNotForAudience(c.Metric, c.Audience)
	}
	if c.Target < 1 {
		return errNoTarget
	}
	if c.RewardXOF < 1 {
		return errNoReward
	}
	if !c.Window.From.Before(c.Window.To) {
		return errBadWindow
	}
	if c.Window.Days() > MaxDays {
		return errWindowTooLong
	}
	if err := CheckTarget(m, c.Target, c.Window); err != nil {
		return err
	}
	// ⚠️ UNE ENVELOPPE QUI NE PAIE PERSONNE EST UN PIÈGE. Un budget de 1 000 F
	// sur un bonus de 2 000 F s'affiche comme une offre et ne récompense
	// personne : le premier gagnant passe déjà le plafond. Mieux vaut le refuser
	// à l'écriture que de le découvrir dans les réclamations.
	if c.Limits.BudgetXOF > 0 && c.Limits.BudgetXOF < c.RewardXOF {
		return errBudgetTooSmall
	}
	return nil
}

func metricAllows(m Metric, audience string) bool {
	for _, a := range m.For {
		if a == audience {
			return true
		}
	}
	return false
}

// CheckTarget REFUSE UNE CIBLE QUI DEMANDE PLUS DE TRAVAIL QU'IL N'EST SÛR D'EN
// FAIRE.
//
// ⚠️⚠️ C'EST LA RÈGLE LA PLUS IMPORTANTE DE CE PAQUET, ET ELLE N'EST PAS
// TECHNIQUE. Un bonus sur le nombre de courses dans une fenêtre courte pousse
// des gens à conduire fatigués — « encore trois et j'ai les 5 000 F », à vingt-
// trois heures, après onze heures de volant. Ce n'est pas une hypothèse : c'est
// le mécanisme par lequel ce genre de prime a tué des chauffeurs sur d'autres
// plateformes.
//
// On ne peut pas empêcher quelqu'un de se fatiguer. On peut refuser d'ÉCRIRE
// l'objectif qui le lui demande, et c'est ce que fait cette fonction : la cible
// est bornée par jour de fenêtre, selon la mesure.
//
// ⚠️ ET LE MESSAGE PROPOSE LA SORTIE : « baissez la cible, ou ALLONGEZ LA
// FENÊTRE ». La même récompense sur deux semaines au lieu d'une est le même
// coût pour l'entreprise et une semaine de sommeil pour la personne.
//
// ⚠️ LES MESURES CLIENT N'ONT PAS DE BORNE, et c'est volontaire : un client qui
// commande trop ne se met pas en danger physique. Une cible absurde y reste
// malhonnête, mais c'est l'enveloppe et la lecture de la console qui l'arrêtent,
// pas une règle de sécurité.
func CheckTarget(m Metric, target int, w Window) error {
	if m.MaxPerDay <= 0 {
		return nil
	}
	if target > m.MaxPerDay*w.Days() {
		return errTargetUnsafe.WithMeta(map[string]any{
			"fields":      []string{"target", "window"},
			"metric":      m.Key,
			"days":        w.Days(),
			"max_per_day": m.MaxPerDay,
			"max_target":  m.MaxPerDay * w.Days(),
		})
	}
	return nil
}
