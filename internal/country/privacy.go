package country

// QUI VOIT QUOI DE QUI — réglé par PAYS et par MÉTIER, depuis la console.
//
// Un client et l'agent qui le sert doivent se reconnaître sans pour autant
// échanger leur état civil. Jusqu'ici, ce que chacun voyait de l'autre était
// décidé dans le code, et il y avait QUATRE réponses différentes à la même
// question, dans quatre fichiers, aucune réglable :
//
//   - course, passager → chauffeur : le nom, pas le téléphone ;
//   - course, chauffeur → passager : RIEN, la conversation seulement ;
//   - livraison, client → livreur : le nom ET le téléphone ;
//   - livraison, livreur → client : le nom ET le téléphone.
//
// ⚠️ CE N'ÉTAIT PAS UNE INCOHÉRENCE, mais quatre arbitrages pris séparément,
// et chacun se défend. Ce qui manquait, c'était un ENDROIT pour les lire, et
// la possibilité de les reprendre quand un marché, une loi ou un incident
// l'exige — sans redéployer.
//
// ⚠️ PAR MÉTIER, et c'est la décision qui porte tout le reste. En livraison, le
// livreur ENCAISSE en espèces et cherche un portail qui ne s'ouvre pas : le
// téléphone est un outil de travail. En course, le chauffeur n'a aucun de ces
// besoins — la conversation suffit, et son dessin d'origine ne donnait de
// numéro ni d'un côté ni de l'autre. Une politique unique aurait obligé à
// trancher pareil pour les deux, donc à casser l'un des deux.
//
// ⚠️ ET PAR PAYS, comme le verrou, les appareils, le fond de carte et la
// monnaie : la norme sociale et la loi ne se ressemblent pas d'un marché à
// l'autre, et un réglage global aurait obligé à trancher pour tout le monde
// depuis Lomé.
//
// ⚠️ LE DÉFAUT REPRODUIT EXACTEMENT CE QUI S'ÉCHANGEAIT AVANT CE RÉGLAGE.
// Rien ne change à la mise en production : la console ne sert qu'à resserrer
// ou ouvrir ENSUITE, en connaissance de cause. Un déploiement qui change un
// comportement en silence est une panne qu'on met des semaines à relier à sa
// cause.

import (
	"context"
	"strings"
	"unicode"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Les deux MÉTIERS, tels qu'une verticale se nomme.
const (
	VerticalVTC  = "vtc"
	VerticalFood = "food"
)

// Les deux PUBLICS d'une divulgation — QUI REGARDE, et non « de qui ».
//
// ⚠️ NOMMÉ PAR LE REGARD, exprès. « client → agent » demande de savoir dans
// quel sens lire la flèche, et une verticale qui se trompe de sens divulgue
// exactement ce qu'elle voulait cacher. « Ce qu'un CLIENT a le droit de voir »
// ne s'inverse pas par inattention.
const (
	// ToClient : ce qu'un CLIENT voit de l'agent qui le sert.
	ToClient = "client"
	// ToAgent : ce qu'un AGENT — chauffeur, livreur — voit de son client.
	ToAgent = "agent"
)

// Les niveaux d'un NOM D'AFFICHAGE.
//
// ⚠️ QUATRE NIVEAUX ET NON UN INTERRUPTEUR, parce que le besoin n'est pas
// binaire : « A. D. » laisse reconnaître la personne qui arrive sans la
// nommer, et c'est souvent le bon compromis. Un oui/non aurait obligé à
// choisir entre tout donner et laisser deux inconnus se chercher.
const (
	NameFull     = "full"     // « Awa Diallo »
	NameFirst    = "first"    // « Awa »
	NameInitials = "initials" // « A. D. »
	NameHidden   = "hidden"   // rien
)

// Disclosure est ce qu'un public a le droit de voir d'une personne.
//
// ⚠️ DES POINTEURS, et pas des booléens nus. « Non réglé » et « refusé » ne
// sont pas la même chose : un `bool` nu aurait fait tomber tous les pays
// enregistrés avant ce réglage sur « tout refusé » — c'est-à-dire aurait coupé
// les téléphones de la livraison le jour du déploiement, sans que personne ne
// l'ait demandé. Un champ absent prend le DÉFAUT DU MÉTIER, qui reproduit ce
// qui s'échangeait avant.
type Disclosure struct {
	// Name : `full`, `first`, `initials` ou `hidden`. Vide = non réglé.
	Name string `bson:"name,omitempty"`
	// FirstName et LastName sont l'ÉTAT CIVIL, distinct du nom d'affichage.
	//
	// ⚠️ RÉGLABLES À PART, parce qu'ils ne disent pas la même chose : une
	// bonne partie des comptes s'inscrit avec un seul mot — le nom
	// d'affichage —, et c'est le NOM DE FAMILLE qui identifie vraiment
	// quelqu'un dans une ville.
	FirstName *bool `bson:"first_name,omitempty"`
	LastName  *bool `bson:"last_name,omitempty"`
	// Photo : la photo de profil. Ce qui fait reconnaître quelqu'un à
	// l'arrivée, et ce qui le rend reconnaissable ailleurs.
	Photo *bool `bson:"photo,omitempty"`
	// Gender n'est servi NULLE PART aujourd'hui, et le défaut le laisse
	// fermé. Il existe ici pour qu'un marché qui l'exige — une course réservée
	// aux femmes, demandée dans plusieurs pays — puisse l'ouvrir sans
	// redéploiement, et pour que ce choix soit VISIBLE quand il est fait.
	Gender *bool `bson:"gender,omitempty"`
	// Rating est une PERMISSION, pas une donnée : la note d'un agent vit dans
	// la verticale, pas au socle. Le socle dit si elle peut être montrée.
	Rating *bool `bson:"rating,omitempty"`

	// --- COMMENT ON JOINT L'AUTRE, et non ce qu'on voit de lui -----------
	//
	// ⚠️ TROIS RÉGLAGES DISTINCTS, et les confondre est l'erreur à ne pas
	// faire : montrer un numéro, pouvoir appeler, et pouvoir faire sonner
	// l'autre ne sont pas la même permission — ni le même risque.

	// Phone : le numéro est AFFICHÉ. C'est le réglage le plus sensible de ce
	// bloc, parce qu'un numéro à l'écran se recopie, se photographie et se
	// garde : il sort de la plateforme à la seconde où il s'affiche.
	Phone *bool `bson:"phone,omitempty"`
	// DirectCall : l'application propose un BOUTON D'APPEL.
	//
	// ⚠️ DISTINCT DE `Phone`, ET C'EST TOUT L'INTÉRÊT. « Appeler sans voir le
	// numéro » est le compromis que demandent la plupart des marchés : le
	// client joint son livreur d'un geste, sans repartir avec ses
	// coordonnées.
	//
	// ⚠️ MAIS CE N'EST PAS DU SECRET, C'EST DE LA FRICTION, et il faut le
	// dire ici plutôt que le laisser découvrir : pour composer un appel, le
	// téléphone doit connaître le numéro — il arrive donc sur l'appareil, et
	// il atterrit dans le JOURNAL D'APPELS. Ce que ce réglage retire, c'est
	// l'affichage, la recopie et la capture d'écran ; ce qu'il ne retire pas,
	// c'est la trace dans le téléphone. Le seul masquage réel serait un
	// RELAIS chez un opérateur, qui n'est pas branché. La même honnêteté que
	// pour le verrou des applications : ce réglage arrête l'usage ordinaire,
	// pas quelqu'un qui cherche.
	DirectCall *bool `bson:"direct_call,omitempty"`
	// InAppAlert : faire SONNER l'application de l'autre — « je suis là ».
	//
	// C'est le klaxon des courses, et il existe précisément parce qu'aucun
	// numéro ne s'échange : le chauffeur est arrivé, il ne voit personne, et
	// le passager ne regarde pas la conversation puisqu'il croyait avoir
	// encore cinq minutes.
	//
	// ⚠️ UNE ALERTE SONORE SE BORNE, TOUJOURS. Un bouton qui sonne fort et
	// qu'on presse dix fois devient du harcèlement en dix secondes, et c'est
	// celui qui descend les escaliers qui le prend. La cadence vit dans la
	// verticale (`HonkCooldown`) ; ce réglage dit seulement si le bouton
	// existe.
	InAppAlert *bool `bson:"in_app_alert,omitempty"`
}

// Exchange est ce que les deux parties d'un métier voient l'une de l'autre.
type Exchange struct {
	ToClient Disclosure `bson:"to_client,omitempty"`
	ToAgent  Disclosure `bson:"to_agent,omitempty"`
}

// Privacy rassemble les deux métiers.
type Privacy struct {
	VTC  Exchange `bson:"vtc,omitempty"`
	Food Exchange `bson:"food,omitempty"`
}

// --- LES DÉFAUTS : CE QUI S'ÉCHANGEAIT AVANT CE RÉGLAGE --------------------
//
// ⚠️ CE BLOC EST LE CONTRAT DE NON-RÉGRESSION. Chaque valeur ici reproduit une
// décision déjà prise et déjà en production ; en changer une, c'est changer le
// comportement de la plateforme pour tous les pays qui n'ont rien réglé. Les
// tests comparent ce bloc à ce que les verticales servaient.

func yes() *bool { t := true; return &t }
func no() *bool  { f := false; return &f }

// DefaultDisclosure rend la divulgation d'un (métier, public) non réglé, telle
// qu'une réponse la porte — le repli quand aucune politique n'est branchée.
func DefaultDisclosure(vertical, audience string) DisclosureResponse {
	return disclosureResponse(vertical, audience, Disclosure{})
}

// defaultDisclosure rend la divulgation d'un (métier, public) non réglé.
func defaultDisclosure(vertical, audience string) Disclosure {
	switch {
	// LA COURSE, CE QUE LE PASSAGER VOIT DU CHAUFFEUR : le nom et la note,
	// pour savoir qui monte le chercher et à qui il parle. Pas de téléphone —
	// la conversation de la course existe pour cela.
	case vertical == VerticalVTC && audience == ToClient:
		return Disclosure{Name: NameFull, FirstName: no(), LastName: no(),
			Photo: no(), Gender: no(), Rating: yes(),
			// Aucun numéro, aucun appel : la conversation de la course est le
			// canal, et c'est le dessin d'origine. Et le passager ne peut pas
			// faire sonner son chauffeur — qui conduit.
			Phone: no(), DirectCall: no(), InAppAlert: no()}

	// LA COURSE, CE QUE LE CHAUFFEUR VOIT DU PASSAGER : RIEN. C'est la
	// position d'origine de la plateforme, et elle tient : un chauffeur
	// enchaîne vingt courses par jour, il n'a aucun usage de vingt identités.
	case vertical == VerticalVTC && audience == ToAgent:
		return Disclosure{Name: NameHidden, FirstName: no(), LastName: no(),
			Photo: no(), Gender: no(), Rating: no(),
			Phone: no(), DirectCall: no(),
			// ⚠️ SAUF LE KLAXON, qui existe DÉJÀ et depuis longtemps : le
			// chauffeur arrivé fait sonner l'application du passager. C'est
			// la seule chose qu'un chauffeur peut faire à son passager
			// aujourd'hui, et le défaut doit la garder — l'éteindre ici
			// couperait une fonction en production le jour du déploiement.
			InAppAlert: yes()}

	// LA LIVRAISON, CE QUE LE CLIENT VOIT DU LIVREUR : nom, note ET
	// téléphone. Le relais téléphonique masquant prévu au dessin demande un
	// prestataire qui n'a pas été choisi ; l'attendre laissait le client sans
	// aucun moyen d'atteindre celui qui a son repas.
	case vertical == VerticalFood && audience == ToClient:
		return Disclosure{Name: NameFull, FirstName: no(), LastName: no(),
			Photo: no(), Gender: no(), Rating: yes(),
			// Le numéro est affiché aujourd'hui, donc l'appel est déjà
			// possible : le refuser ici retirerait une fonction en place.
			// Pas d'alerte sonore — elle n'existe pas en livraison.
			Phone: yes(), DirectCall: yes(), InAppAlert: no()}

	// LA LIVRAISON, CE QUE LE LIVREUR VOIT DU CLIENT : nom et téléphone. En
	// espèces il encaisse à l'arrivée, et le téléphone est le seul recours qui
	// aboutisse quand personne ne répond au portail.
	case vertical == VerticalFood && audience == ToAgent:
		return Disclosure{Name: NameFull, FirstName: no(), LastName: no(),
			Photo: no(), Gender: no(), Rating: no(),
			Phone: yes(), DirectCall: yes(), InAppAlert: no()}
	}
	// ⚠️ UN MÉTIER OU UN PUBLIC INCONNU NE DIVULGUE RIEN. La règle se FERME,
	// elle ne s'ouvre pas : le jour où une troisième verticale appellera cette
	// porte sans que personne n'ait réglé sa politique, elle ne doit pas
	// hériter de celle de la livraison.
	return Disclosure{Name: NameHidden, FirstName: no(), LastName: no(),
		Photo: no(), Gender: no(), Rating: no(),
		Phone: no(), DirectCall: no(), InAppAlert: no()}
}

// DisclosureResponse est la divulgation telle que la console la lit — tous les
// champs posés, aucun à deviner.
type DisclosureResponse struct {
	Name      string `json:"name"`
	FirstName bool   `json:"first_name"`
	LastName  bool   `json:"last_name"`
	Photo     bool   `json:"photo"`
	Gender    bool   `json:"gender"`
	Rating    bool   `json:"rating"`
	// Phone : le numéro est AFFICHÉ. DirectCall : un bouton d'appel est
	// proposé. InAppAlert : on peut faire sonner l'autre dans l'application.
	// Trois permissions, trois risques — voir `Disclosure`.
	Phone      bool `json:"phone"`
	DirectCall bool `json:"direct_call"`
	InAppAlert bool `json:"in_app_alert"`
}

// ExchangeResponse porte les deux sens d'un métier.
type ExchangeResponse struct {
	ToClient DisclosureResponse `json:"to_client"`
	ToAgent  DisclosureResponse `json:"to_agent"`
}

// PrivacyResponse est le bloc complet, les deux métiers.
type PrivacyResponse struct {
	VTC  ExchangeResponse `json:"vtc"`
	Food ExchangeResponse `json:"food"`
}

// disclosureResponse complète les trous avec le défaut du métier.
func disclosureResponse(vertical, audience string, d Disclosure) DisclosureResponse {
	def := defaultDisclosure(vertical, audience)
	out := DisclosureResponse{
		Name:       def.Name,
		FirstName:  *def.FirstName,
		LastName:   *def.LastName,
		Photo:      *def.Photo,
		Gender:     *def.Gender,
		Rating:     *def.Rating,
		Phone:      *def.Phone,
		DirectCall: *def.DirectCall,
		InAppAlert: *def.InAppAlert,
	}
	if d.Name != "" {
		out.Name = d.Name
	}
	for _, f := range []struct {
		set *bool
		out *bool
	}{
		{d.FirstName, &out.FirstName}, {d.LastName, &out.LastName},
		{d.Photo, &out.Photo}, {d.Gender, &out.Gender}, {d.Rating, &out.Rating},
		{d.Phone, &out.Phone}, {d.DirectCall, &out.DirectCall},
		{d.InAppAlert, &out.InAppAlert},
	} {
		if f.set != nil {
			*f.out = *f.set
		}
	}
	return out
}

func exchangeResponse(vertical string, e Exchange) ExchangeResponse {
	return ExchangeResponse{
		ToClient: disclosureResponse(vertical, ToClient, e.ToClient),
		ToAgent:  disclosureResponse(vertical, ToAgent, e.ToAgent),
	}
}

func privacyResponse(p Privacy) PrivacyResponse {
	return PrivacyResponse{
		VTC:  exchangeResponse(VerticalVTC, p.VTC),
		Food: exchangeResponse(VerticalFood, p.Food),
	}
}

// PrivacyUpdateRequest règle UN SENS d'UN métier.
//
// ⚠️ UN SEUL SENS PAR APPEL, exprès. Un formulaire qui enverrait les quatre
// divulgations d'un coup rendrait impossible de dire, dans un journal d'audit,
// laquelle vient de changer — et c'est précisément la question qu'on posera
// après un incident. Le pays et le métier sont nommés, le reste est
// facultatif : on change un champ sans réécrire les autres.
type PrivacyUpdateRequest struct {
	Vertical string `json:"vertical" validate:"required,oneof=vtc food"`
	Audience string `json:"audience" validate:"required,oneof=client agent"`

	Name      *string `json:"name" validate:"omitempty,oneof=full first initials hidden"`
	FirstName *bool   `json:"first_name"`
	LastName  *bool   `json:"last_name"`
	Photo     *bool   `json:"photo"`
	Gender    *bool   `json:"gender"`
	Rating    *bool   `json:"rating"`
	// Les trois canaux de contact — voir `Disclosure`.
	Phone      *bool `json:"phone"`
	DirectCall *bool `json:"direct_call"`
	InAppAlert *bool `json:"in_app_alert"`
}

var errNoPrivacyUpdate = apperr.Validation(
	"nothing to update: send name, first_name, last_name, photo, gender, rating, phone, direct_call or in_app_alert")

// Privacy rend la politique d'un pays, telle que la console la lit.
func (s *Service) Privacy(ctx context.Context, code string) (*PrivacyResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := privacyResponse(inst.Privacy)
	return &out, nil
}

// UpdatePrivacy règle un sens d'un métier dans un pays.
func (s *Service) UpdatePrivacy(ctx context.Context, code string, req PrivacyUpdateRequest) (*PrivacyResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	if req.Name == nil && req.FirstName == nil && req.LastName == nil &&
		req.Photo == nil && req.Gender == nil && req.Rating == nil &&
		req.Phone == nil && req.DirectCall == nil && req.InAppAlert == nil {
		return nil, errNoPrivacyUpdate
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	p := inst.Privacy
	ex := &p.VTC
	if req.Vertical == VerticalFood {
		ex = &p.Food
	}
	d := &ex.ToClient
	if req.Audience == ToAgent {
		d = &ex.ToAgent
	}
	if req.Name != nil {
		d.Name = *req.Name
	}
	for _, f := range []struct {
		in  *bool
		out **bool
	}{
		{req.FirstName, &d.FirstName}, {req.LastName, &d.LastName},
		{req.Photo, &d.Photo}, {req.Gender, &d.Gender}, {req.Rating, &d.Rating},
		{req.Phone, &d.Phone}, {req.DirectCall, &d.DirectCall},
		{req.InAppAlert, &d.InAppAlert},
	} {
		if f.in != nil {
			v := *f.in
			*f.out = &v
		}
	}
	if err := s.repo.SetPrivacy(ctx, info.Code, p); err != nil {
		return nil, apperr.Internal(err)
	}
	if s.audit != nil {
		// ⚠️ TRACÉ, ET C'EST TOUT L'INTÉRÊT D'UN SENS PAR APPEL : le journal
		// dit quel métier, quel public, et ce qui a changé. « La politique de
		// confidentialité a été modifiée » ne répond à aucune question.
		s.audit.Record(ctx, "country.privacy", "country", info.Code, nil,
			map[string]any{"vertical": req.Vertical, "audience": req.Audience,
				"disclosure": disclosureResponse(req.Vertical, req.Audience, *d)})
	}
	out := privacyResponse(p)
	return &out, nil
}

// DiscloseOf rend ce qu'un public a le droit de voir d'une personne, dans un
// pays et un métier donnés — l'adaptateur que la surface de service appelle.
//
// ⚠️ AU MIEUX, ET AU PLUS FERMÉ. Une base muette ou un pays inconnu rendent le
// DÉFAUT DU MÉTIER, jamais une erreur : faire échouer l'affichage d'une course
// parce qu'un réglage est illisible serait hors de proportion. Mais un métier
// ou un public inconnu ne divulguent RIEN — la règle se ferme.
func (s *Service) DiscloseOf(ctx context.Context, code, vertical, audience string) DisclosureResponse {
	info, ok := country.Lookup(code)
	if !ok {
		return disclosureResponse(vertical, audience, Disclosure{})
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return disclosureResponse(vertical, audience, Disclosure{})
	}
	ex := inst.Privacy.VTC
	if vertical == VerticalFood {
		ex = inst.Privacy.Food
	}
	d := ex.ToClient
	if audience == ToAgent {
		d = ex.ToAgent
	}
	return disclosureResponse(vertical, audience, d)
}

// --- LE MASQUAGE D'UN NOM --------------------------------------------------

// MaskName rend le nom d'affichage au niveau demandé.
//
// `display` est le nom tel que le compte le porte, `first` le prénom d'état
// civil quand il existe — une bonne partie des comptes n'en a pas, et couper
// `display` à l'espace produirait « Nom : Diallo Ndiaye » chez tous ceux qui
// ont deux prénoms.
func MaskName(level, display, first string) string {
	display = strings.TrimSpace(display)
	switch level {
	case NameHidden:
		return ""
	case NameFull:
		return display
	case NameFirst:
		if f := strings.TrimSpace(first); f != "" {
			return f
		}
		if i := strings.IndexFunc(display, unicode.IsSpace); i > 0 {
			return display[:i]
		}
		return display
	case NameInitials:
		return initialsOf(display)
	}
	return display
}

// initialsOf rend « A. D. » à partir de « Awa Diallo ».
//
// ⚠️ DEUX INITIALES AU PLUS. « A. B. C. D. » sur un compte à quatre mots ne
// masque plus rien et ne se lit pas : la personne reste identifiable dans un
// quartier, ce qui est exactement ce que ce niveau existe pour éviter.
func initialsOf(display string) string {
	var out []string
	for _, w := range strings.Fields(display) {
		for _, r := range w {
			if unicode.IsLetter(r) {
				out = append(out, strings.ToUpper(string(r))+".")
				break
			}
		}
		if len(out) == 2 {
			break
		}
	}
	return strings.Join(out, " ")
}
