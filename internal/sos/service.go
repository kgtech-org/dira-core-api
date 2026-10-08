package sos

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Accounts résout un nom et un pays — juste de quoi écrire la ligne que
// l'opérateur lit.
type Accounts interface {
	UserNames(ctx context.Context, ids []string) (map[string]string, error)
	CountryOf(ctx context.Context, id string) (string, error)
}

// StaffAlerts prévient les membres du staff dont le périmètre couvre une
// verticale.
type StaffAlerts interface {
	AlertStaff(ctx context.Context, scope, country, key string, vars, data map[string]string)
}

// Policy rend la politique d'alerte d'un pays.
type Policy interface {
	SOSSettings(ctx context.Context, countryCode string) Settings
}

// Settings est la politique telle que ce module la sert aux applications.
type Settings struct {
	Button           bool     `json:"button"`
	Shake            bool     `json:"shake"`
	Crash            bool     `json:"crash"`
	Voice            bool     `json:"voice"`
	CountdownSeconds int      `json:"countdown_seconds"`
	Numbers          []Number `json:"numbers"`
}

// Number est un numéro de secours à composer.
type Number struct {
	Kind   string `json:"kind"`
	Label  string `json:"label,omitempty"`
	Number string `json:"number"`
}

// Auditor écrit au journal des actions.
type Auditor interface {
	Record(ctx context.Context, action, resourceType, resourceID string, before, after any)
}

// Service tient les alertes.
type Service struct {
	repo     *Repository
	accounts Accounts
	staff    StaffAlerts
	policy   Policy
	audit    Auditor
	now      func() time.Time
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) SetAccounts(a Accounts)       { s.accounts = a }
func (s *Service) SetStaffAlerts(a StaffAlerts) { s.staff = a }
func (s *Service) SetPolicy(p Policy)           { s.policy = p }
func (s *Service) SetAuditor(a Auditor)         { s.audit = a }

var (
	errNotFound = apperr.NotFound("sos_not_found", "this alert does not exist")
	// ⚠️ LE SEUL REFUS DU CHEMIN DE DÉCLENCHEMENT, et il ne porte pas sur le
	// contenu : un identifiant de compte illisible veut dire que le jeton est
	// cassé, pas que l'alerte est mauvaise.
	errNoActor = apperr.Unauthorized("sos_unknown_actor", "this session has no account")
	errClosed  = apperr.Conflict("sos_already_closed", "this alert is already closed")
	errOutcome = apperr.Validation("outcome is required: real, false_alarm, unreachable or test").
			WithMeta(map[string]any{"fields": []string{"outcome"}, "allowed": Outcomes})
	// ⚠️ NOMMÉ À PART d'un refus d'accès ordinaire : on annule SON alerte, et
	// pas celle d'un autre.
	errNotYours = apperr.Forbidden("sos_not_yours", "this alert belongs to someone else")
)

// RaiseInput est ce qu'une application envoie.
//
// ⚠️ AUCUN `validate` SUR AUCUN CHAMP, et c'est le cœur du module. Un
// `validate:"required"` sur la position aurait transformé un appel au secours
// sans GPS en `422` — c'est-à-dire en rien du tout. Tout ce qui est bancal est
// corrigé par `Normalise`, jamais rejeté.
type RaiseInput struct {
	Source     string   `json:"source"`
	Confirmed  *bool    `json:"confirmed"`
	Lat        *float64 `json:"lat"`
	Lng        *float64 `json:"lng"`
	AccuracyM  int      `json:"accuracy_m"`
	Battery    int      `json:"battery"`
	Vertical   string   `json:"vertical"`
	RideID     string   `json:"ride_id"`
	DeliveryID string   `json:"delivery_id"`
	Note       string   `json:"note"`
}

// Raise ouvre une alerte — ou rend celle qui est déjà ouverte.
//
// ⚠️ LE DOUBLE APPUI N'EST PAS UNE ERREUR, C'EST LE COMPORTEMENT NORMAL de
// quelqu'un qui panique. Il rend donc l'alerte existante et y ajoute la
// nouvelle position, plutôt que d'en créer une seconde : cinq alertes dans la
// file, c'est quatre que l'exploitation prend pour d'autres gens, et le temps
// qu'elle les trie, personne n'est parti sur place.
func (s *Service) Raise(ctx context.Context, userID string, in RaiseInput) (*Response, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errNoActor
	}
	now := s.now()

	if open, err := s.repo.OpenOf(ctx, uid); err == nil && open != nil {
		// Déjà ouverte : on enrichit et on rend la même.
		if p := position(in, now); p != nil {
			if err := s.repo.AppendPosition(ctx, open.ID, *p, now); err != nil {
				slog.ErrorContext(ctx, "sos: position not appended", "alert_id", open.ID.Hex(), "error", err)
			} else {
				open.Trail = append(open.Trail, *p)
			}
		}
		out := s.response(ctx, open)
		return &out, nil
	} else if err != nil {
		// ⚠️ UNE LECTURE RATÉE NE FAIT PAS ÉCHOUER LE DÉCLENCHEMENT. On
		// continue et on crée : un doublon dans la file est un désagrément,
		// une alerte perdue est un accident.
		slog.ErrorContext(ctx, "sos: open lookup failed, raising anyway", "user_id", userID, "error", err)
	}

	a := &Alert{
		UserID: uid, Status: StatusOpen, Source: in.Source,
		Country:    countryOf(ctx, s.accounts, userID),
		Vertical:   in.Vertical,
		RideID:     strings.TrimSpace(in.RideID),
		DeliveryID: strings.TrimSpace(in.DeliveryID),
		Note:       in.Note, Battery: in.Battery,
		RaisedPos: position(in, now),
		CreatedAt: now, UpdatedAt: now,
	}
	if in.Confirmed != nil {
		a.Confirmed = *in.Confirmed
	}
	Normalise(a)
	if a.RaisedPos != nil {
		a.Trail = []Position{*a.RaisedPos}
	}
	if err := s.repo.Insert(ctx, a); err != nil {
		// ⚠️ LE SEUL ÉCHEC POSSIBLE DE CE CHEMIN, ET IL CRIE. Si la base refuse
		// l'écriture, l'application doit le savoir pour réessayer — et le
		// journal doit le dire en ERROR, parce qu'une alerte perdue est la
		// seule panne de ce module qui ne se répare pas après coup.
		slog.ErrorContext(ctx, "sos: ALERT NOT WRITTEN", "user_id", userID, "error", err)
		return nil, apperr.Internal(err)
	}
	slog.WarnContext(ctx, "sos: ALERT RAISED",
		"alert_id", a.ID.Hex(), "user_id", userID, "source", a.Source,
		"confirmed", a.Confirmed, "country", a.Country,
		"ride_id", a.RideID, "delivery_id", a.DeliveryID)

	// ⚠️ TOUT CE QUI SUIT EST APRÈS L'ÉCRITURE, et rien n'en fait échouer le
	// retour. Prévenir le staff peut échouer, le journal d'audit peut échouer :
	// l'alerte, elle, existe.
	out := s.response(ctx, a)
	s.alertStaff(ctx, a, &out)
	s.record(ctx, "sos.raised", a, nil)
	return &out, nil
}

// position extrait le point envoyé, s'il y en a un d'utilisable.
func position(in RaiseInput, now time.Time) *Position {
	if in.Lat == nil || in.Lng == nil {
		return nil
	}
	if !Plausible(*in.Lat, *in.Lng) {
		return nil
	}
	acc := in.AccuracyM
	if acc < 0 || acc > 100_000 {
		acc = 0
	}
	return &Position{Lat: *in.Lat, Lng: *in.Lng, AccuracyM: acc, At: now}
}

// countryOf prend le pays du COMPTE, et non celui de la requête.
//
// ⚠️ LA DIFFÉRENCE COMPTE ICI. Ailleurs on prend le pays de l'OPÉRATION ; une
// alerte, elle, n'a pas toujours d'opération. Le pays du compte est le seul
// qu'on soit sûr d'avoir, et c'est lui qui désigne l'exploitation capable
// d'envoyer quelqu'un. Et s'il manque, l'alerte part SANS pays : une alerte
// visible de toutes les consoles vaut infiniment mieux qu'une alerte rangée
// dans un pays où personne ne regarde.
func countryOf(ctx context.Context, accounts Accounts, userID string) string {
	if accounts != nil {
		if c, err := accounts.CountryOf(ctx, userID); err == nil && c != "" {
			return c
		}
	}
	return country.FromContext(ctx)
}

// Position : la personne bouge, et l'opérateur a besoin de savoir OÙ ELLE EST,
// pas où elle était quand elle a appuyé.
func (s *Service) Position(ctx context.Context, userID, id string, lat, lng float64, accuracyM int) error {
	a, err := s.load(ctx, id)
	if err != nil {
		return err
	}
	if a.UserID.Hex() != userID {
		return errNotYours
	}
	if a.Status == StatusClosed {
		// ⚠️ ON NE REFUSE PAS BRUYAMMENT : une application qui pousse encore sa
		// position après une fermeture n'a rien fait de mal, elle n'a pas
		// encore appris. Un `409` l'aurait fait réessayer en boucle.
		return nil
	}
	if !Plausible(lat, lng) {
		return nil
	}
	if accuracyM < 0 || accuracyM > 100_000 {
		accuracyM = 0
	}
	return s.repo.AppendPosition(ctx, a.ID, Position{
		Lat: lat, Lng: lng, AccuracyM: accuracyM, At: s.now(),
	}, s.now())
}

// Cancel : la personne dit « fausse alerte ».
//
// ⚠️ L'ALERTE EST FERMÉE, PAS SUPPRIMÉE, ET ELLE RESTE À L'ÉCRAN QUINZE
// MINUTES. Une annulation peut être CONTRAINTE — c'est le scénario même que ce
// bouton existe pour couvrir. Une annulation quatre secondes après le
// déclenchement est une information, et la faire disparaître aurait rendu
// invisible exactement le cas le plus grave.
func (s *Service) Cancel(ctx context.Context, userID, id string) (*Response, error) {
	a, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.UserID.Hex() != userID {
		return nil, errNotYours
	}
	uid := a.UserID
	closed, err := s.repo.Close(ctx, a.ID, ClosedByRaiser, &uid, OutcomeFalseAlarm, "", s.now())
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if closed == nil {
		// Déjà refermée — par l'exploitation, ou par un double appui. On rend
		// l'état courant plutôt qu'une erreur : la personne a voulu annuler,
		// c'est annulé.
		out := s.response(ctx, a)
		return &out, nil
	}
	slog.WarnContext(ctx, "sos: cancelled by the raiser",
		"alert_id", a.ID.Hex(), "user_id", userID,
		"seconds_after", int(s.now().Sub(a.CreatedAt).Seconds()))
	out := s.response(ctx, closed)
	s.record(ctx, "sos.cancelled", closed, auditView(a))
	return &out, nil
}

// Mine rend l'alerte vivante de la personne, s'il y en a une.
//
// ⚠️ ELLE EXISTE POUR QUE L'ÉCRAN SE RETROUVE. Un téléphone qui redémarre après
// un choc, une application tuée par le système, un retour de réseau : sans
// cette route, la personne ne sait plus si son alerte est partie, et elle
// appuie encore.
func (s *Service) Mine(ctx context.Context, userID string) (*Response, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errNoActor
	}
	a, err := s.repo.OpenOf(ctx, uid)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if a == nil {
		return nil, nil
	}
	out := s.response(ctx, a)
	return &out, nil
}

// SettingsFor rend la politique du pays de la requête.
func (s *Service) SettingsFor(ctx context.Context) Settings {
	if s.policy == nil {
		// Sans politique branchée : le bouton existe, les détections aussi, et
		// aucun numéro — jamais un numéro inventé.
		return Settings{Button: true, Shake: true, Crash: true, CountdownSeconds: 10, Numbers: []Number{}}
	}
	return s.policy.SOSSettings(ctx, country.FromContext(ctx))
}

// --- L'EXPLOITATION ------------------------------------------------------

// Live rend ce qui demande une action, et le compte.
func (s *Service) Live(ctx context.Context, limit int) ([]Response, int, error) {
	rows, err := s.repo.List(ctx, Filter{Live: true}, limit)
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}
	n, err := s.repo.CountLive(ctx)
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}
	return s.responses(ctx, rows), n, nil
}

// History rend l'historique d'un pays — pour compter les vraies, les fausses,
// et voir qu'une même personne en déclenche une par semaine.
func (s *Service) History(ctx context.Context, status string, since *time.Time, limit int) ([]Response, error) {
	rows, err := s.repo.List(ctx, Filter{Status: status, Since: since}, limit)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return s.responses(ctx, rows), nil
}

// Get rend une alerte à l'exploitation.
func (s *Service) Get(ctx context.Context, id string) (*Response, error) {
	a, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	out := s.response(ctx, a)
	return &out, nil
}

// Acknowledge : un opérateur nommé prend l'alerte.
func (s *Service) Acknowledge(ctx context.Context, actorID, id string) (*Response, error) {
	a, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	by, err := primitive.ObjectIDFromHex(actorID)
	if err != nil {
		return nil, errNoActor
	}
	ok, err := s.repo.Acknowledge(ctx, a.ID, by, s.now())
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if !ok {
		// Quelqu'un l'a prise entre-temps, ou elle est fermée. On rend l'état
		// courant : l'opérateur doit voir QUI l'a, pas une erreur.
		return s.Get(ctx, id)
	}
	s.record(ctx, "sos.acknowledged", a, nil)
	return s.Get(ctx, id)
}

// Close referme avec un dénouement.
func (s *Service) Close(ctx context.Context, actorID, id, outcome, resolution string) (*Response, error) {
	if !ValidOutcome(outcome) {
		return nil, errOutcome
	}
	a, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	by, err := primitive.ObjectIDFromHex(actorID)
	if err != nil {
		return nil, errNoActor
	}
	closed, err := s.repo.Close(ctx, a.ID, ClosedByStaff, &by, outcome, strings.TrimSpace(resolution), s.now())
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if closed == nil {
		return nil, errClosed
	}
	out := s.response(ctx, closed)
	s.record(ctx, "sos.closed", closed, auditView(a))
	// ⚠️ ON PRÉVIENT LES AUTRES OPÉRATEURS QUE C'EST TRAITÉ. Plusieurs ont reçu
	// l'alerte ; sans ce message, trois personnes appellent le même chauffeur
	// pendant que la suivante attend.
	s.notifyClosed(ctx, closed, &out)
	return &out, nil
}

// --- LE DEDANS -----------------------------------------------------------

func (s *Service) load(ctx context.Context, id string) (*Alert, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errNotFound
	}
	a, err := s.repo.ByID(ctx, oid)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if a == nil {
		return nil, errNotFound
	}
	return a, nil
}

func (s *Service) record(ctx context.Context, action string, a *Alert, before any) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, action, "sos_alert", a.ID.Hex(), before, auditView(a))
}

func auditView(a *Alert) map[string]any {
	if a == nil {
		return nil
	}
	return map[string]any{
		"status": a.Status, "source": a.Source, "confirmed": a.Confirmed,
		"outcome": a.Outcome, "closed_by": a.ClosedBy,
	}
}

// alertStaff pousse l'alerte aux opérateurs dont le périmètre couvre la
// verticale.
//
// ⚠️ LE PÉRIMÈTRE EST `core` QUAND L'ALERTE N'A PAS D'OPÉRATION, et c'est
// voulu : une personne en danger hors course concerne l'exploitation entière,
// pas une verticale. Dans le doute, on prévient plus large.
func (s *Service) alertStaff(ctx context.Context, a *Alert, out *Response) {
	if s.staff == nil {
		slog.ErrorContext(ctx, "sos: NO STAFF ALERT WIRED — the console is the only way this is seen",
			"alert_id", a.ID.Hex())
		return
	}
	scope := a.Vertical
	if scope == "" {
		scope = "core"
	}
	vars := map[string]string{
		"who":     firstNonEmpty(out.UserName, "un compte sans nom"),
		"trigger": TriggerLabel(a.Source, a.Confirmed),
		"where":   whereLabel(a),
	}
	data := map[string]string{
		"type": "sos", "sos_id": a.ID.Hex(), "source": a.Source,
		"vertical": a.Vertical, "ride_id": a.RideID, "delivery_id": a.DeliveryID,
	}
	s.staff.AlertStaff(ctx, scope, a.Country, "staff_sos", vars, data)
}

func (s *Service) notifyClosed(ctx context.Context, a *Alert, out *Response) {
	if s.staff == nil {
		return
	}
	scope := a.Vertical
	if scope == "" {
		scope = "core"
	}
	by := "l'exploitation"
	if a.ClosedBy == ClosedByRaiser {
		by = "la personne elle-même"
	}
	s.staff.AlertStaff(ctx, scope, a.Country, "staff_sos_closed", map[string]string{
		"who":     firstNonEmpty(out.UserName, "un compte sans nom"),
		"outcome": OutcomeLabel(a.Outcome),
		"by":      by,
	}, map[string]string{"type": "sos_closed", "sos_id": a.ID.Hex()})
}

// whereLabel écrit la position en une poignée de caractères — ce qui tient dans
// une bannière de notification.
func whereLabel(a *Alert) string {
	p := a.RaisedPos
	if len(a.Trail) > 0 {
		p = &a.Trail[len(a.Trail)-1]
	}
	if p == nil {
		// ⚠️ ON LE DIT. « Position inconnue » est une information qui change le
		// geste : il faut appeler, pas regarder la carte.
		return "position inconnue"
	}
	return formatCoord(p.Lat) + ", " + formatCoord(p.Lng)
}

func formatCoord(v float64) string {
	// Cinq décimales : ~1 m. Au-delà, c'est du bruit de capteur.
	s := strconv.FormatFloat(v, 'f', 5, 64)
	return s
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// response compose UNE alerte. Le nom est résolu au mieux.
func (s *Service) response(ctx context.Context, a *Alert) Response {
	out := s.responses(ctx, []Alert{*a})
	return out[0]
}

// responses compose une liste, en résolvant TOUS les noms d'un coup.
//
// ⚠️ UNE SEULE RÉSOLUTION POUR TOUTE LA LISTE : une file de trente alertes
// aurait fait trente lectures de comptes, et c'est l'écran qu'on veut le plus
// rapide de la console.
func (s *Service) responses(ctx context.Context, rows []Alert) []Response {
	ids := make([]string, 0, len(rows)*2)
	for _, a := range rows {
		ids = append(ids, a.UserID.Hex())
		if a.AcknowledgedBy != nil {
			ids = append(ids, a.AcknowledgedBy.Hex())
		}
	}
	names := map[string]string{}
	if s.accounts != nil && len(ids) > 0 {
		if m, err := s.accounts.UserNames(ctx, ids); err == nil {
			names = m
		} else {
			// AU MIEUX : une alerte sans nom reste une alerte. L'écran montre
			// l'identifiant.
			slog.WarnContext(ctx, "sos: names not resolved", "error", err)
		}
	}
	out := make([]Response, 0, len(rows))
	for _, a := range rows {
		r := Response{
			ID: a.ID.Hex(), UserID: a.UserID.Hex(), UserName: names[a.UserID.Hex()],
			Status: a.Status, Source: a.Source, Confirmed: a.Confirmed,
			Trigger: TriggerLabel(a.Source, a.Confirmed),
			Grave:   Grave(a.Source, a.Confirmed),
			Country: a.Country, Vertical: a.Vertical,
			RideID: a.RideID, DeliveryID: a.DeliveryID,
			Note: a.Note, Battery: a.Battery,
			RaisedPos: a.RaisedPos, Trail: a.Trail,
			CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
			AcknowledgedAt: a.AcknowledgedAt,
			ClosedAt:       a.ClosedAt, ClosedBy: a.ClosedBy,
			Outcome: a.Outcome, Resolution: a.Resolution,
		}
		if a.AcknowledgedBy != nil {
			r.AcknowledgedBy = names[a.AcknowledgedBy.Hex()]
			if r.AcknowledgedBy == "" {
				r.AcknowledgedBy = a.AcknowledgedBy.Hex()
			}
		}
		if n := len(a.Trail); n > 0 {
			last := a.Trail[n-1]
			r.LastPos = &last
		} else if a.RaisedPos != nil {
			r.LastPos = a.RaisedPos
		}
		if a.ClosedBy == ClosedByRaiser && a.ClosedAt != nil {
			if d := int(a.ClosedAt.Sub(a.CreatedAt).Seconds()); d >= 0 {
				r.CancelledSeconds = d
			}
		}
		out = append(out, r)
	}
	return out
}
