package sos

import "time"

// Response est une alerte telle que la console et l'application la lisent.
type Response struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	// UserName : le nom, résolu AU MIEUX. ⚠️ Vide, l'écran doit afficher
	// l'identifiant plutôt que rien : un opérateur peut appeler un compte sans
	// nom, il ne peut rien faire d'une ligne vide.
	UserName  string `json:"user_name,omitempty"`
	Status    string `json:"status"`
	Source    string `json:"source"`
	Confirmed bool   `json:"confirmed"`
	// Trigger : la source ET la confirmation en une phrase lisible.
	//
	// ⚠️ SERVI PLUTÔT QUE LAISSÉ À L'ÉCRAN, parce que la combinaison se lit à
	// l'envers : « choc détecté, NON confirmé » est plus grave que « choc
	// détecté, confirmé » — personne n'a annulé, ce qui veut souvent dire que
	// personne ne POUVAIT annuler. Chaque écran qui recomposerait la phrase se
	// tromperait une fois sur deux.
	Trigger string `json:"trigger"`
	// Grave dit que cette alerte doit passer DEVANT les autres.
	//
	// ⚠️ CALCULÉ ICI, UNE FOIS. Trier « par date » mettrait un choc non
	// confirmé d'il y a trois minutes sous un bouton pressé d'il y a dix
	// secondes.
	Grave      bool       `json:"grave"`
	Country    string     `json:"country,omitempty"`
	Vertical   string     `json:"vertical,omitempty"`
	RideID     string     `json:"ride_id,omitempty"`
	DeliveryID string     `json:"delivery_id,omitempty"`
	Note       string     `json:"note,omitempty"`
	Battery    int        `json:"battery,omitempty"`
	RaisedPos  *Position  `json:"raised_pos,omitempty"`
	LastPos    *Position  `json:"last_pos,omitempty"`
	Trail      []Position `json:"trail,omitempty"`

	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedBy string     `json:"acknowledged_by,omitempty"`
	ClosedAt       *time.Time `json:"closed_at,omitempty"`
	ClosedBy       string     `json:"closed_by,omitempty"`
	Outcome        string     `json:"outcome,omitempty"`
	Resolution     string     `json:"resolution,omitempty"`
	// CancelledSeconds : combien de secondes après le déclenchement la personne
	// a annulé.
	//
	// ⚠️ SERVI EXPRÈS, ET C'EST UN SIGNAL. Une annulation en quatre secondes
	// peut être un dos-d'âne — ou quelqu'un à qui on a arraché le téléphone.
	// Sans ce nombre, les deux se ressemblent dans une liste.
	CancelledSeconds int `json:"cancelled_seconds,omitempty"`
}

// TriggerLabel dit en clair ce qui a déclenché l'alerte.
//
// ⚠️ LA CONFIRMATION SE LIT À L'ENVERS SUR UNE DÉTECTION, et c'est toute la
// raison de cette fonction : « non confirmé » après un choc ne veut pas dire
// « probablement rien », il veut dire « le compte à rebours s'est écoulé sans
// que personne n'annule ». C'est écrit dans le texte pour qu'aucun écran n'ait
// à le deviner.
func TriggerLabel(source string, confirmed bool) string {
	switch source {
	case SourceButton:
		return "bouton pressé"
	case SourceShake:
		if confirmed {
			return "appareil secoué, confirmé"
		}
		return "appareil secoué, SANS confirmation"
	case SourceCrash:
		if confirmed {
			return "choc détecté, confirmé"
		}
		return "choc détecté, PERSONNE N'A ANNULÉ"
	case SourceVoice:
		if confirmed {
			return "mot-clé vocal, confirmé"
		}
		return "mot-clé vocal, SANS confirmation"
	default:
		return source
	}
}

// OutcomeLabel nomme un dénouement.
func OutcomeLabel(o string) string {
	switch o {
	case OutcomeReal:
		return "il se passait quelque chose"
	case OutcomeFalseAlarm:
		return "fausse alerte"
	case OutcomeUnreachable:
		return "personne injoignable"
	case OutcomeTest:
		return "essai"
	default:
		return o
	}
}

// Grave dit qu'une alerte passe devant les autres.
//
// ⚠️ UNE DÉTECTION NON CONFIRMÉE EST GRAVE, et c'est le point qu'il faut tenir.
// L'intuition dit l'inverse — « il n'a pas confirmé, c'est sûrement un faux » —
// et elle est dangereuse : après un choc violent, personne n'a annulé parce que
// personne ne pouvait. Un bouton pressé est grave aussi : c'est une décision.
func Grave(source string, confirmed bool) bool {
	return source == SourceButton || !confirmed || source == SourceCrash
}
