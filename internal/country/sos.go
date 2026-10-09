package country

// CE QUE LE PAYS DÉCIDE DU BOUTON D'ALERTE.
//
// Le bouton SOS lui-même n'est pas réglable : il existe dans toutes les
// applications, partout, et c'est la seule chose de ce fichier qui ne se
// discute pas. Ce qui se règle, c'est ce qui l'entoure — les DÉTECTIONS qui le
// proposent sans qu'on le touche, le temps laissé pour annuler, et le numéro de
// secours du pays.
//
// ⚠️ POURQUOI PAR PAYS. Les numéros d'urgence ne sont pas les mêmes d'un pays à
// l'autre, et c'est la raison qui suffirait à elle seule. Mais les détections
// aussi : là où les routes défoncées font claquer un téléphone dix fois par
// trajet, la détection de choc remplit la file de faux et l'exploitation cesse
// de la regarder. C'est une décision d'exploitation, pays par pays, pas une
// constante de code.
//
// ⚠️⚠️ LES APPLICATIONS NE REÇOIVENT AUCUN NUMÉRO À COMPOSER, ET C'EST LA
// DÉCISION CENTRALE DE CE FICHIER.
//
// Ce n'est pas une précaution de données, c'est le PROTOCOLE : l'alerte part au
// SERVICE CLIENT, un opérateur appelle d'abord la personne, et c'est LUI qui
// appelle les secours s'il le faut. Le téléphone de quelqu'un en danger ne
// compose rien.
//
// Pourquoi c'est mieux, et pas seulement différent :
//   - QUELQU'UN RÉPOND TOUJOURS. Les secours d'un pays peuvent sonner dans le
//     vide — un relevé mené en Guinée en 2024 a trouvé plusieurs numéros
//     officiels hors service. Un opérateur qui tombe sur un numéro mort
//     l'entend, raccroche et prend le suivant ; une personne en panique, non.
//   - L'OPÉRATEUR SAIT CE QU'IL DIT. « Un chauffeur à tel carrefour, voiture
//     grise, immatriculée, course en cours » se transmet ; un passager terrorisé
//     ne décrit pas sa position.
//   - ET LE PREMIER APPEL EST SOUVENT LE BON : la moitié des cas se règlent en
//     joignant la personne — un téléphone tombé, un dos-d'âne, une dispute qui
//     s'est calmée. Appeler la police pour ça la ferait cesser de nous écouter.
//
// Les numéros, eux, viennent du CATALOGUE (`pkg/country.Emergency`), au même
// titre que la monnaie : ce sont des faits du pays. L'exploitation les CONFIRME
// ou les corrige ici, pays par pays, et c'est son réglage qui fait foi — parce
// que les sources publiques se contredisent et qu'un numéro change sans
// prévenir. Ils ne sortent que vers la CONSOLE.

import (
	"context"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Défauts des détections, quand le pays n'a rien réglé.
const (
	// defaultSOSShake : la SECOUSSE est allumée d'office.
	//
	// Elle ne coûte rien (l'accéléromètre tourne déjà), elle ne demande aucune
	// permission, et c'est le geste le plus utile du lot : on secoue un
	// téléphone qu'on ne peut pas regarder.
	defaultSOSShake = true
	// defaultSOSCrash : la DÉTECTION DE CHOC est allumée d'office.
	//
	// Un accident est le cas où personne n'appuiera sur rien. Le compte à
	// rebours est là pour les faux positifs.
	defaultSOSCrash = true
	// defaultSOSVoice : le MOT-CLÉ VOCAL est ÉTEINT d'office.
	//
	// ⚠️ ET C'EST LE SEUL DES TROIS QUI LE SOIT. Il demande d'écouter le micro
	// en permanence : une permission que l'utilisateur doit comprendre, une
	// batterie qui descend, et une surveillance qu'on n'allume pas à l'insu de
	// quelqu'un. Un pays qui le veut l'allume — explicitement, et en l'ayant
	// expliqué dans son application.
	defaultSOSVoice = false
	// defaultSOSCountdown : dix secondes pour annuler.
	//
	// ⚠️ LE RÉGLAGE LE PLUS DÉLICAT DU FICHIER, parce qu'il se trompe dans les
	// DEUX sens. Trop court (3 s) : chaque dos-d'âne part en alerte, l'opérateur
	// apprend à les ignorer, et la vraie se noie. Trop long (60 s) : quelqu'un
	// à qui on arrache son téléphone a tout le temps de voir l'alerte annulée
	// par son agresseur. Dix secondes laissent le temps de lire, de comprendre
	// et d'appuyer — et pas celui de fouiller une poche.
	defaultSOSCountdown = 10
)

// Genres de numéros de secours.
const (
	EmergencyPolice    = "police"
	EmergencyAmbulance = "ambulance"
	EmergencyFire      = "fire"
	// EmergencyPlatform : le numéro de l'EXPLOITATION elle-même.
	//
	// ⚠️ IL COMPTE AUTANT QUE LES AUTRES. Quelqu'un dont la course tourne mal
	// n'a pas toujours affaire à la police : un passager agressif, une dispute
	// sur un prix, une route bloquée. Appeler Dira est souvent le bon geste, et
	// sans ce numéro l'application n'a que la police à proposer — ce qui fait
	// soit un appel de trop, soit aucun appel.
	EmergencyPlatform = "platform"
)

// EmergencyKinds est la liste admise.
var EmergencyKinds = []string{EmergencyPolice, EmergencyAmbulance, EmergencyFire, EmergencyPlatform}

func validEmergencyKind(k string) bool {
	for _, v := range EmergencyKinds {
		if v == k {
			return true
		}
	}
	return false
}

// EmergencyNumber est UN numéro à composer, tel que l'application le montre.
type EmergencyNumber struct {
	Kind string `bson:"kind" json:"kind"`
	// Label : ce qui s'écrit sur le bouton, dans la langue de l'exploitation —
	// « Police secours », « SAMU », « Astreinte Dira ». Vide, l'application
	// nomme le genre elle-même.
	Label string `bson:"label,omitempty" json:"label,omitempty"`
	// Number : tel qu'on le COMPOSE dans ce pays. Les numéros courts (17, 118)
	// ne s'écrivent pas en E.164 et ne doivent pas être « corrigés » en +228 17.
	Number string `bson:"number" json:"number"`
}

// SOS est la politique d'alerte d'un pays.
type SOS struct {
	// Shake, Crash, Voice : des POINTEURS, parce que « non réglé » et « éteint »
	// ne sont pas la même chose. Un `bool` nu aurait éteint la secousse dans
	// tout pays enregistré avant ce réglage — c'est-à-dire tous.
	Shake *bool `bson:"shake,omitempty"`
	Crash *bool `bson:"crash,omitempty"`
	Voice *bool `bson:"voice,omitempty"`
	// CountdownSeconds : le délai d'annulation d'une détection. Zéro = le défaut.
	CountdownSeconds int               `bson:"countdown_seconds,omitempty"`
	Numbers          []EmergencyNumber `bson:"numbers,omitempty"`
	// VerifiedAt, VerifiedBy : QUELQU'UN A COMPOSÉ CES NUMÉROS, et ça a répondu.
	//
	// ⚠️ C'EST UN FAIT DISTINCT DE « DES NUMÉROS SONT ENREGISTRÉS », et les
	// confondre était un défaut de la première version : `confirmed` valait
	// « la liste n'est pas vide », si bien qu'un simple enregistrement — ou un
	// jeu de données rejoué — se présentait à l'opérateur comme une
	// vérification humaine. Or ce drapeau ne sert qu'à une chose : lui dire
	// s'il est le PREMIER à essayer. Mentir dessus le rendait inutile.
	//
	// ⚠️ ON NE PEUT PAS LE DÉDUIRE, ni le programmer. Vérifier un numéro de
	// secours, c'est le COMPOSER et entendre quelqu'un répondre. Aucun code ne
	// fait ça : la console a donc un geste explicite, et c'est une personne qui
	// l'engage — avec son nom, parce qu'on vient le lui demander le jour où le
	// numéro ne répond plus.
	VerifiedAt *time.Time          `bson:"numbers_verified_at,omitempty"`
	VerifiedBy *primitive.ObjectID `bson:"numbers_verified_by,omitempty"`
}

// SOSResponse est la politique telle que les APPLICATIONS la reçoivent — tous
// les champs posés, aucun à deviner.
//
// ⚠️ ELLE NE PORTE AUCUN NUMÉRO, et c'est structurel plutôt que documentaire.
// Écrire « n'affichez pas de bouton d'appel » dans une spec, en servant le
// champ quand même, aurait fini par un bouton : un champ qui existe se câble.
// Les numéros vivent dans `SOSAdminResponse`, que seule la console lit.
type SOSResponse struct {
	// Button est TOUJOURS `true`. Il est rendu quand même, et c'est délibéré :
	// une application qui lit ce bloc doit pouvoir écrire son écran sans cas
	// particulier, et le jour où quelqu'un demanderait à couper le bouton dans
	// un pays, le champ existe pour qu'on puisse répondre non par écrit.
	Button bool `json:"button"`
	Shake  bool `json:"shake"`
	Crash  bool `json:"crash"`
	Voice  bool `json:"voice"`
	// CountdownSeconds : le délai AVANT envoi d'une détection. ⚠️ Il ne
	// s'applique PAS au bouton : appuyer, c'est avoir déjà décidé.
	CountdownSeconds int `json:"countdown_seconds"`
	// CallsBack dit à l'application CE QU'ELLE DOIT PROMETTRE : « le service
	// client a été prévenu et va vous appeler ».
	//
	// ⚠️ SERVI PLUTÔT QUE SUPPOSÉ, parce que c'est la seule chose que la
	// personne cherche à savoir après avoir appuyé, et parce qu'une application
	// qui écrirait « appelez la police » à la place enverrait quelqu'un composer
	// un numéro qu'on ne lui a pas donné. Vaut toujours `true` aujourd'hui ; le
	// champ existe pour que l'écran n'ait rien à deviner, et pour qu'on puisse
	// répondre par écrit le jour où on demanderait l'inverse.
	CallsBack bool `json:"calls_back"`
}

// SOSAdminResponse est la politique telle que LA CONSOLE la lit — avec les
// numéros, parce que c'est elle qui appelle.
type SOSAdminResponse struct {
	SOSResponse
	// Numbers : ce que l'opérateur compose. Le catalogue du pays, remplacé par
	// le réglage de l'exploitation quand il y en a un.
	Numbers []EmergencyNumber `json:"numbers"`
	// VerifiedAt : quand quelqu'un les a composés. ⚠️ SERVI avec `Confirmed`,
	// parce que « vérifié il y a trois ans » et « vérifié hier » ne valent pas
	// la même chose — un numéro d'urgence change sans prévenir, et une
	// confirmation vieillit.
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	// Confirmed dit si quelqu'un a COMPOSÉ ces numéros et entendu une réponse.
	//
	// ⚠️ SERVI, ET LA CONSOLE DOIT LE MONTRER. Les sources publiques se
	// contredisent, et un numéro officiel peut être hors service : un opérateur
	// qui compose doit savoir si quelqu'un chez nous a déjà vérifié, ou s'il
	// est le premier à essayer. « Non confirmé » n'empêche pas d'appeler — ça
	// dit de vérifier qu'on est bien tombé au bon endroit.
	//
	// ⚠️ ET IL NE SE DÉDUIT PAS DE LA PRÉSENCE DE NUMÉROS. Enregistrer une
	// liste n'est pas l'avoir appelée ; un jeu de données rejoué non plus.
	Confirmed bool `json:"confirmed"`
}

// SOSUpdateRequest règle la politique depuis la console. Tout est facultatif.
type SOSUpdateRequest struct {
	Shake *bool `json:"shake"`
	Crash *bool `json:"crash"`
	Voice *bool `json:"voice"`
	// CountdownSeconds : de 3 à 60 secondes.
	//
	// ⚠️ LES DEUX BORNES SONT DES GARDE-FOUS, pas des préférences. En dessous de
	// 3 s, personne n'a le temps de lire l'écran : l'application enverrait des
	// alertes que son propriétaire n'a pas voulues. Au-delà de 60 s, un
	// agresseur a le temps de trouver le téléphone et d'annuler — le compte à
	// rebours devient le délai qui protège l'agresseur.
	CountdownSeconds *int `json:"countdown_seconds" validate:"omitempty,min=3,max=60"`
	// Numbers remplace la liste ENTIÈRE quand il est présent. Une liste vide
	// explicite efface les numéros — ce qui cache les boutons d'appel.
	Numbers *[]EmergencyNumber `json:"numbers" validate:"omitempty,max=6,dive"`
}

var (
	errNothingToVerify = apperr.Validation(
		"nothing to verify: this country has no emergency number at all").
		WithMeta(map[string]any{"fields": []string{"numbers"}})
	errNoSOSUpdate = apperr.Validation(
		"nothing to update: send shake, crash, voice, countdown_seconds or numbers")
	errBadEmergencyKind = apperr.Validation(
		"unknown emergency number kind: use police, ambulance, fire or platform").
		WithMeta(map[string]any{"fields": []string{"numbers"}})
	// ⚠️ UN NUMÉRO VIDE EST REFUSÉ, alors que tout le reste de ce module est
	// tolérant. Un bouton d'appel sans numéro est le seul cas vraiment
	// indéfendable : il a l'air de marcher, on appuie, et rien ne se passe.
	// Mieux vaut l'absence de bouton, et c'est ce que donne une liste vide.
	errEmptyEmergencyNumber = apperr.Validation(
		"an emergency number cannot be blank: remove the entry instead").
		WithMeta(map[string]any{"fields": []string{"numbers"}})
)

// sosResponse complète les trous avec les défauts.
func sosResponse(s SOS) SOSResponse {
	out := SOSResponse{
		Button:           true,
		Shake:            defaultSOSShake,
		Crash:            defaultSOSCrash,
		Voice:            defaultSOSVoice,
		CountdownSeconds: s.CountdownSeconds,
		CallsBack:        true,
	}
	if s.Shake != nil {
		out.Shake = *s.Shake
	}
	if s.Crash != nil {
		out.Crash = *s.Crash
	}
	if s.Voice != nil {
		out.Voice = *s.Voice
	}
	if out.CountdownSeconds == 0 {
		out.CountdownSeconds = defaultSOSCountdown
	}
	return out
}

// sosAdminResponse ajoute les numéros, pour la console.
//
// ⚠️ LE CATALOGUE D'ABORD, LE RÉGLAGE ENSUITE — exactement comme la monnaie
// d'un pays. Le catalogue donne le fait connu, l'exploitation le corrige quand
// elle a vérifié, et c'est son réglage qui gagne.
//
// ⚠️ UN GENRE RÉGLÉ REMPLACE CELUI DU CATALOGUE, IL NE S'AJOUTE PAS. Deux
// « police » sur l'écran d'un opérateur, c'est une hésitation d'une seconde au
// moment où il n'en a pas.
func sosAdminResponse(code string, s SOS) SOSAdminResponse {
	out := SOSAdminResponse{
		SOSResponse: sosResponse(s),
		// ⚠️ UNE TRANCHE VIDE, JAMAIS `nil` : `numbers: null` en JSON fait
		// planter une console qui boucle dessus sans vérifier.
		Numbers: []EmergencyNumber{},
		// ⚠️ « CONFIRMÉ » VEUT DIRE « QUELQU'UN LES A COMPOSÉS », et rien
		// d'autre. Il valait « la liste n'est pas vide » dans la première
		// version : un enregistrement suffisait à faire croire à une
		// vérification humaine, et l'opérateur perdait l'avertissement qui
		// l'aurait fait vérifier qu'il tombe au bon endroit.
		Confirmed:  s.VerifiedAt != nil,
		VerifiedAt: s.VerifiedAt,
	}
	seen := make(map[string]bool, len(s.Numbers))
	for _, n := range s.Numbers {
		seen[n.Kind] = true
		out.Numbers = append(out.Numbers, n)
	}
	if info, ok := country.Lookup(code); ok {
		for _, n := range catalogueNumbers(info.Emergency) {
			if !seen[n.Kind] {
				out.Numbers = append(out.Numbers, n)
			}
		}
	}
	return out
}

// catalogueNumbers traduit les numéros du catalogue en lignes affichables.
//
// Les libellés sont écrits ici, dans la langue de l'exploitation : le catalogue
// porte des FAITS (un numéro), pas la façon de les présenter.
func catalogueNumbers(e country.Emergency) []EmergencyNumber {
	out := make([]EmergencyNumber, 0, 3)
	for _, c := range []struct {
		kind, label, number string
	}{
		{EmergencyPolice, "Police secours", e.Police},
		{EmergencyFire, "Sapeurs-pompiers", e.Fire},
		{EmergencyAmbulance, "Ambulance / SAMU", e.Ambulance},
	} {
		// ⚠️ UN NUMÉRO VIDE NE DONNE PAS DE LIGNE. Les sources ne concordent
		// pas pour l'ambulance de plusieurs pays ; une ligne « Ambulance : »
		// sans numéro serait un bouton qui ne mène à rien, ce qui est pire que
		// son absence.
		if c.number == "" {
			continue
		}
		out = append(out, EmergencyNumber{Kind: c.kind, Label: c.label, Number: c.number})
	}
	return out
}

// normaliseNumbers met la liste en forme et refuse ce qui ne se compose pas.
func normaliseNumbers(in []EmergencyNumber) ([]EmergencyNumber, error) {
	out := make([]EmergencyNumber, 0, len(in))
	seen := map[string]bool{}
	for _, n := range in {
		n.Kind = strings.ToLower(strings.TrimSpace(n.Kind))
		if !validEmergencyKind(n.Kind) {
			return nil, errBadEmergencyKind
		}
		// ⚠️ ON NE GARDE QUE CE QUI SE COMPOSE : chiffres, `+`, `*`, `#`. Les
		// espaces et tirets de présentation sont retirés — un composeur de
		// téléphone les accepte, mais pas tous, et « 17 » doit rester « 17 ».
		n.Number = keepDialable(n.Number)
		if n.Number == "" {
			return nil, errEmptyEmergencyNumber
		}
		n.Label = strings.TrimSpace(n.Label)
		if len(n.Label) > 60 {
			n.Label = n.Label[:60]
		}
		// ⚠️ UN SEUL NUMÉRO PAR GENRE. Deux « police » feraient deux boutons
		// identiques sur l'écran d'urgence, et personne ne saurait lequel
		// appuyer — au moment précis où il ne faut pas réfléchir.
		if seen[n.Kind] {
			continue
		}
		seen[n.Kind] = true
		out = append(out, n)
	}
	return out, nil
}

func keepDialable(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '+' || r == '*' || r == '#' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SOSPolicy rend la politique d'un pays, telle que la console la lit — avec les
// numéros.
func (s *Service) SOSPolicy(ctx context.Context, code string) (*SOSAdminResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := sosAdminResponse(info.Code, inst.Security.SOS)
	return &out, nil
}

// EmergencyOf rend les numéros EFFECTIFS d'un pays — l'adaptateur que le module
// d'alerte appelle pour les poser sur l'écran de l'opérateur.
//
// ⚠️ AU MIEUX : un pays inconnu ou une base muette rendent le catalogue, jamais
// une erreur. Faire échouer l'affichage d'une alerte SOS parce qu'un réglage est
// illisible serait exactement la mauvaise façon d'échouer.
func (s *Service) EmergencyOf(ctx context.Context, code string) ([]EmergencyNumber, bool) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, false
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		out := sosAdminResponse(info.Code, SOS{})
		return out.Numbers, false
	}
	out := sosAdminResponse(info.Code, inst.Security.SOS)
	return out.Numbers, out.Confirmed
}

// UpdateSOS règle la politique d'alerte d'un pays.
func (s *Service) UpdateSOS(ctx context.Context, code string, req SOSUpdateRequest) (*SOSAdminResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	if req.Shake == nil && req.Crash == nil && req.Voice == nil &&
		req.CountdownSeconds == nil && req.Numbers == nil {
		return nil, errNoSOSUpdate
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	cur := inst.Security.SOS
	if req.Shake != nil {
		cur.Shake = req.Shake
	}
	if req.Crash != nil {
		cur.Crash = req.Crash
	}
	if req.Voice != nil {
		cur.Voice = req.Voice
	}
	if req.CountdownSeconds != nil {
		cur.CountdownSeconds = *req.CountdownSeconds
	}
	if req.Numbers != nil {
		nums, err := normaliseNumbers(*req.Numbers)
		if err != nil {
			return nil, err
		}
		// ⚠️ CHANGER UN NUMÉRO RETIRE LA CONFIRMATION. Elle portait sur la
		// liste qu'on avait appelée ; la garder après une modification
		// présenterait un numéro jamais composé comme vérifié — exactement le
		// mensonge que ce drapeau existe pour éviter.
		if !sameNumbers(cur.Numbers, nums) {
			cur.VerifiedAt, cur.VerifiedBy = nil, nil
		}
		cur.Numbers = nums
	}
	sec := inst.Security
	sec.SOS = cur
	if err := s.repo.SetSecurity(ctx, info.Code, sec); err != nil {
		return nil, apperr.Internal(err)
	}
	out := sosAdminResponse(info.Code, cur)
	return &out, nil
}

// VerifyNumbers enregistre que quelqu'un a COMPOSÉ les numéros de ce pays.
//
// ⚠️ UN GESTE À PART DE L'ENREGISTREMENT, et c'est tout l'intérêt. Saisir une
// liste et l'avoir appelée sont deux choses : la première se fait depuis un
// bureau avec une page de résultats de recherche, la seconde demande un
// téléphone et trente secondes par numéro. Les fondre en un seul bouton aurait
// fait de « confirmé » un synonyme de « enregistré », c'est-à-dire rien.
//
// ⚠️ ET ELLE PORTE UN NOM. On vient le demander à quelqu'un le jour où un
// numéro ne répond plus — pas pour le blâmer, mais parce qu'il sait quand il a
// appelé et ce qu'il a entendu.
//
// ⚠️ ELLE EXIGE QU'IL Y AIT DES NUMÉROS À AVOIR APPELÉS : confirmer une liste
// vide n'est pas une vérification, c'est une case cochée.
func (s *Service) VerifyNumbers(ctx context.Context, code, actorID string) (*SOSAdminResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	cur := inst.Security.SOS
	// ⚠️ ON VÉRIFIE SUR LA LISTE EFFECTIVE — catalogue compris. L'exploitation
	// n'a rien à ressaisir pour confirmer : si le catalogue donne 17 et 18 et
	// qu'ils répondent, il n'y a rien à corriger, et exiger une saisie
	// identique aurait fait recopier des chiffres pour rien (donc en faire
	// une faute de frappe une fois sur dix).
	effective := sosAdminResponse(info.Code, cur)
	if len(effective.Numbers) == 0 {
		return nil, errNothingToVerify
	}
	// ⚠️ LA LISTE EST FIGÉE AU MOMENT DE LA CONFIRMATION. Sans cela, on
	// confirmerait le catalogue — qui peut changer au prochain déploiement —, et
	// la confirmation porterait sur des numéros que personne n'a appelés.
	cur.Numbers = effective.Numbers
	now := time.Now().UTC()
	cur.VerifiedAt = &now
	if aid, err := primitive.ObjectIDFromHex(actorID); err == nil {
		cur.VerifiedBy = &aid
	}
	sec := inst.Security
	sec.SOS = cur
	if err := s.repo.SetSecurity(ctx, info.Code, sec); err != nil {
		return nil, apperr.Internal(err)
	}
	out := sosAdminResponse(info.Code, cur)
	return &out, nil
}

// SOSOf rend la politique pour les APPLICATIONS — l'adaptateur que le module
// d'alerte appelle.
//
// ⚠️ AU MIEUX, comme tout le reste de ce fichier : une base muette rend les
// DÉFAUTS. Faire échouer l'écran d'urgence parce qu'un réglage est illisible
// serait exactement la mauvaise façon d'échouer.
func (s *Service) SOSOf(ctx context.Context, code string) SOSResponse {
	info, ok := country.Lookup(code)
	if !ok {
		return sosResponse(SOS{})
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return sosResponse(SOS{})
	}
	return sosResponse(inst.Security.SOS)
}

// sameNumbers dit si deux listes portent les mêmes numéros.
//
// ⚠️ ELLE COMPARE LE GENRE ET LE NUMÉRO, PAS LE LIBELLÉ. Renommer un bouton
// « Police » en « Police secours » ne change pas ce qu'on compose : retirer la
// confirmation pour une correction d'orthographe aurait appris à l'exploitation
// que ce drapeau ne veut rien dire.
func sameNumbers(a, b []EmergencyNumber) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]string, len(a))
	for _, n := range a {
		seen[n.Kind] = n.Number
	}
	for _, n := range b {
		if seen[n.Kind] != n.Number {
			return false
		}
	}
	return true
}
