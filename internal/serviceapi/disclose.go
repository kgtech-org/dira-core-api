package serviceapi

// CE QU'UNE VERTICALE A LE DROIT DE MONTRER D'UNE PERSONNE À L'AUTRE.
//
// ⚠️ LA VERTICALE NE REÇOIT PAS CE QU'ELLE NE DOIT PAS MONTRER, et c'est toute
// la raison d'être de cette porte. Filtrer après coup — lire le contact nu puis
// cacher les champs à l'affichage — aurait fait traverser le fil à un numéro de
// téléphone que l'exploitation venait de fermer, pour le laisser dormir dans un
// journal de requêtes, un cache ou une réponse mise en mémoire. Un champ qu'on
// n'envoie pas ne fuit pas.
//
// ⚠️ DISTINCTE DE `/internal/accounts/contact`, qui rend le nom et le téléphone
// SANS filtre. Celle-là est la vérité du back-office, celle que le support doit
// voir pour traiter une réclamation ; celle-ci est ce que deux inconnus
// s'échangent sur une course. Une verticale qui appelle la mauvaise des deux
// divulgue exactement ce qu'on venait de fermer — c'est l'erreur à ne pas
// faire, et c'est pourquoi les deux portes ne se ressemblent pas.

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"unicode"

	"github.com/kgtech-org/dira-core-api/internal/country"
	"github.com/kgtech-org/dira-core-api/internal/user"
	pkgcountry "github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
)

// Privacy rend la politique d'un pays pour un métier et un public.
//
// Déclarée côté consommateur — c'est `internal/country.Service`. FACULTATIVE :
// sans elle, la porte rend les DÉFAUTS DU MÉTIER, qui reproduisent ce qui
// s'échangeait avant ce réglage, et le journal le dit. Mieux vaut une
// divulgation non réglée qu'une course qui ne s'affiche plus.
type Privacy interface {
	DiscloseOf(ctx context.Context, code, vertical, audience string) country.DisclosureResponse
}

// SetPrivacy branche la politique de confidentialité (câblage).
func (h *Handler) SetPrivacy(p Privacy) { h.privacy = p }

// DiscloseResponse est ce que la verticale reçoit — et rien de plus.
//
// ⚠️ TOUS LES CHAMPS SONT `omitempty` : un champ absent veut dire « non
// divulgué », et c'est la même chose qu'une donnée vide côté application. La
// distinction n'intéresse personne à l'écran, et la faire voyager aurait
// obligé chaque application à la gérer.
type DiscloseResponse struct {
	Name      string `json:"name,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
	Gender    string `json:"gender,omitempty"`
	// Phone n'est envoyé que s'il peut être AFFICHÉ ou COMPOSÉ.
	//
	// ⚠️ UN BOUTON D'APPEL A BESOIN DU NUMÉRO. C'est la limite honnête de
	// « appeler sans voir » : pour composer, le téléphone doit le connaître,
	// donc il arrive ici et il atterrira dans le journal d'appels de
	// l'appareil. Ce que `ShowPhone: false` retire, c'est l'affichage, la
	// recopie et la capture d'écran — pas la trace dans le téléphone. Le seul
	// masquage réel serait un relais chez un opérateur, qui n'est pas branché.
	Phone string `json:"phone,omitempty"`
	// ShowPhone : le numéro peut être AFFICHÉ. DirectCall : un bouton d'appel
	// peut être proposé. InAppAlert : on peut faire sonner l'autre dans
	// l'application. ShowRating : la note — que la VERTICALE détient — peut
	// être montrée.
	ShowPhone  bool `json:"show_phone"`
	DirectCall bool `json:"direct_call"`
	InAppAlert bool `json:"in_app_alert"`
	ShowRating bool `json:"show_rating"`
}

// POST /internal/accounts/disclose {user_id, vertical, audience, country?}
func (h *Handler) disclose(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID   string `json:"user_id" validate:"required,len=24,hexadecimal"`
		Vertical string `json:"vertical" validate:"required,oneof=vtc food"`
		// Audience dit QUI REGARDE : `client` ou `agent`.
		Audience string `json:"audience" validate:"required,oneof=client agent"`
		// Country est celui de L'OPÉRATION — la course, la commande —, et non
		// celui du compte regardé ni celui de la requête.
		//
		// ⚠️ C'EST L'OPÉRATION QUI DÉCIDE, comme partout sur la plateforme. Un
		// Togolais qui prend une course à Dakar relève de la politique
		// sénégalaise : c'est là que la course a lieu, et c'est la loi de ce
		// marché qui s'applique. Facultatif — à défaut l'en-tête de la
		// requête, puis le pays du compte.
		Country string `json:"country" validate:"omitempty,len=2,alpha"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	person, err := h.accounts.PersonOf(r.Context(), req.UserID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	code := pkgcountry.Normalize(req.Country)
	if code == "" {
		code = pkgcountry.FromContext(r.Context())
	}
	if code == "" {
		code = person.Country
	}

	var d country.DisclosureResponse
	if h.privacy == nil {
		// ⚠️ ON CRIE, PARCE QUE LE SILENCE SERAIT PIRE. Sans politique
		// branchée, cette porte rend les défauts du métier : les applications
		// continuent de fonctionner, mais la console ne commande plus rien —
		// et personne ne s'en apercevrait sans cette ligne.
		slog.ErrorContext(r.Context(), "serviceapi: no privacy policy wired — the console does not govern what parties exchange",
			"vertical", req.Vertical, "audience", req.Audience)
		d = country.DefaultDisclosure(req.Vertical, req.Audience)
	} else {
		d = h.privacy.DiscloseOf(r.Context(), code, req.Vertical, req.Audience)
	}
	httpx.JSON(w, http.StatusOK, discloseResponse(person, d))
}

// discloseResponse applique la politique à une identité nue.
func discloseResponse(p *user.Person, d country.DisclosureResponse) DiscloseResponse {
	out := DiscloseResponse{
		Name:       country.MaskName(d.Name, p.Name, p.FirstName),
		ShowPhone:  d.Phone,
		DirectCall: d.DirectCall,
		InAppAlert: d.InAppAlert,
		ShowRating: d.Rating,
	}
	// ⚠️ LE NOM D'UN COMPTE NÉ PAR CODE EST SON NUMÉRO DE TÉLÉPHONE, et c'est
	// le trou qui annulerait tout ce réglage. L'inscription par code pose le
	// numéro comme nom d'affichage tant que la personne n'en a pas donné un :
	// servir ce « nom » avec `phone` fermé donnerait le numéro quand même, à
	// l'écran, sous un libellé qui ne le dit pas.
	//
	// Le nom part donc — sauf si le numéro était de toute façon divulgué, où
	// il n'y a plus rien à protéger. L'application affiche alors son propre
	// libellé (« Votre livreur »), comme pour tout nom absent.
	if out.Name != "" && p.Phone != "" && !d.Phone && nameIsThePhone(out.Name, p.Phone) {
		out.Name = ""
	}
	if d.FirstName {
		out.FirstName = p.FirstName
	}
	if d.LastName {
		out.LastName = p.LastName
	}
	if d.Photo {
		out.AvatarURL = p.AvatarURL
	}
	if d.Gender {
		out.Gender = p.Gender
	}
	// Le numéro n'est envoyé que s'il sert : à être affiché, ou à être
	// composé. Un bouton d'appel sans numéro ne compose rien.
	if d.Phone || d.DirectCall {
		out.Phone = p.Phone
	}
	return out
}

// nameIsThePhone dit si un nom d'affichage n'est que le numéro du compte —
// masqué ou non.
//
// ⚠️ ON COMPARE AUSSI LES FORMES MASQUÉES, parce que « +228 90 00 00 00 »
// réduit en initiales donne « 2. » et en prénom donne « +22890000000 ». C'est
// le niveau `first` qui est le piège : il rend le numéro ENTIER quand celui-ci
// n'a pas d'espace.
func nameIsThePhone(name, phone string) bool {
	if name == phone {
		return true
	}
	// Un nom qui commence par le même préfixe que le numéro et ne porte aucune
	// lettre n'est pas un nom.
	for _, r := range name {
		if unicode.IsLetter(r) {
			return false
		}
	}
	return strings.HasPrefix(phone, strings.TrimSpace(name)) ||
		strings.HasPrefix(strings.TrimSpace(name), phone)
}
