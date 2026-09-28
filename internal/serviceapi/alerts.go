package serviceapi

// LES ALERTES DE LA SUPERVISION, REMISES LÀ OÙ LES GENS REGARDENT.
//
// ⚠️ UN CANAL DE PLUS EST UN CANAL QU'ON IGNORE. Un courriel que personne
// n'ouvre, une interface qu'il faut penser à visiter : au bout de trois
// semaines, plus rien n'est lu. La plateforme sait déjà prévenir son staff —
// par pays, par fonction, avec la même mécanique que « un chauffeur attend sa
// validation ». Les alertes machine empruntent ce chemin-là, et pas un autre.
//
// ⚠️ ET ELLE NE FAIT PAS CONFIANCE À CE QU'ON LUI ENVOIE. Ce point d'entrée
// est INTERNE — la passerelle refuse `/api/v1/internal/` de l'extérieur, et le
// jeton de service est vérifié — mais le corps vient d'un autre logiciel :
// tout ce qu'il contient est traité comme du TEXTE, jamais comme une
// instruction, et sa longueur est bornée.

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// alertPayload est la forme qu'Alertmanager envoie.
type alertPayload struct {
	Status string `json:"status"` // firing | resolved
	Alerts []struct {
		Status      string            `json:"status"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	} `json:"alerts"`
}

// maxAlertsPerBatch borne ce qu'un lot peut contenir.
//
// ⚠️ UNE PANNE DE BASE DÉCLENCHE TRENTE ALERTES EN TRENTE SECONDES. Elles
// arrivent groupées, et c'est bien ; mais un lot de deux cents notifications
// envoyées au staff ferait du téléphone de l'astreinte un réveil inutilisable
// — et c'est ainsi qu'on coupe les notifications, juste avant l'incident
// suivant.
const maxAlertsPerBatch = 10

// POST /internal/alerts — le crochet d'Alertmanager.
func (h *Handler) alerts(w http.ResponseWriter, r *http.Request) {
	var req alertPayload
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	if h.staff == nil || h.notifier == nil {
		// AU MIEUX : une alerte perdue vaut mieux qu'un `500` rendu à la
		// supervision, qui réessaierait en boucle.
		slog.WarnContext(r.Context(), "serviceapi: alert dropped, no staff directory", "count", len(req.Alerts))
		w.WriteHeader(http.StatusAccepted)
		return
	}

	sent := 0
	for i, a := range req.Alerts {
		if i >= maxAlertsPerBatch {
			slog.WarnContext(r.Context(), "serviceapi: alert batch truncated",
				"kept", maxAlertsPerBatch, "dropped", len(req.Alerts)-maxAlertsPerBatch)
			break
		}
		status := a.Status
		if status == "" {
			status = req.Status
		}
		// ⚠️ LE RÉTABLISSEMENT SE DIT AUSSI. Une alerte qui ne se referme
		// jamais oblige à aller vérifier soi-même que c'est fini — et on
		// finit par ne plus y croire, dans un sens comme dans l'autre.
		key := "staff_platform_alert"
		if status == "resolved" {
			key = "staff_platform_alert_resolved"
		}
		vars := map[string]string{
			"alert":    clip(a.Labels["alertname"], 80),
			"severity": clip(a.Labels["severity"], 20),
			"summary":  clip(a.Annotations["summary"], 200),
			"what":     clip(a.Annotations["description"], 600),
			"service":  clip(a.Labels["service"], 40),
		}
		data := map[string]string{
			"type": "platform_alert", "alert": vars["alert"],
			"severity": vars["severity"], "status": status,
		}
		// ⚠️ PORTÉE `core` : une alerte machine n'appartient à aucune
		// verticale — même quand elle nomme un service. C'est l'équipe
		// plateforme qui la traite, pas l'exploitation des courses.
		//
		// Et PAS DE PAYS : la supervision mesure la plateforme entière, et
		// une alerte adressée à un seul pays serait lue par une personne qui
		// n'a pas la main sur la machine.
		ids, err := h.staff.Recipients(r.Context(), "core", "")
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		for _, id := range ids {
			h.notifier.Notify(r.Context(), id, key, vars, data)
		}
		sent += len(ids)
		slog.WarnContext(r.Context(), "platform alert relayed",
			"alert", vars["alert"], "severity", vars["severity"],
			"status", status, "recipients", len(ids))
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"notified": sent})
}

// clip borne un texte venu d'ailleurs.
//
// ⚠️ CE QUI ARRIVE ICI EST ÉCRIT PAR UN AUTRE LOGICIEL, et une description
// d'alerte peut contenir la sortie entière d'une requête. Non bornée, elle
// partirait telle quelle dans une notification poussée — que le téléphone
// tronquerait de toute façon, après l'avoir transportée.
func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
