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
// ⚠️ ET AUCUN NUMÉRO N'EST PRÉCHARGÉ. C'est la décision la plus importante de
// ce fichier, et elle va contre l'habitude du reste de la base, où l'on
// précharge des défauts raisonnables. Un numéro d'urgence approximatif serait
// COMPOSÉ PAR QUELQU'UN EN DANGER : « probablement le 17 » n'est pas une valeur
// par défaut acceptable. Tant que l'exploitation n'a pas saisi le numéro de ce
// pays, les applications N'AFFICHENT PAS le bouton d'appel — c'est écrit dans
// les specs. Un bouton absent envoie chercher le 112 ; un bouton qui compose un
// mauvais numéro fait perdre les trente secondes qui comptent.

import (
	"context"
	"strings"

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
}

// SOSResponse est la politique telle que la console la lit et que les
// applications la reçoivent — tous les champs posés, aucun à deviner.
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
	// Numbers peut être VIDE, et l'application doit le supporter : pas de
	// bouton d'appel plutôt qu'un mauvais numéro.
	Numbers []EmergencyNumber `json:"numbers"`
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
		// ⚠️ UNE TRANCHE VIDE, JAMAIS `nil` : `numbers: null` en JSON fait
		// planter une application qui boucle dessus sans vérifier, et c'est
		// l'écran d'urgence.
		Numbers: []EmergencyNumber{},
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
	out.Numbers = append(out.Numbers, s.Numbers...)
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

// SOSPolicy rend la politique d'un pays, telle que la console la lit.
func (s *Service) SOSPolicy(ctx context.Context, code string) (*SOSResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := sosResponse(inst.Security.SOS)
	return &out, nil
}

// UpdateSOS règle la politique d'alerte d'un pays.
func (s *Service) UpdateSOS(ctx context.Context, code string, req SOSUpdateRequest) (*SOSResponse, error) {
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
		cur.Numbers = nums
	}
	sec := inst.Security
	sec.SOS = cur
	if err := s.repo.SetSecurity(ctx, info.Code, sec); err != nil {
		return nil, apperr.Internal(err)
	}
	out := sosResponse(cur)
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
