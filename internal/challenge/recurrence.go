package challenge

// LA RÉCURRENCE — « une campagne périodique lancée ».
//
// Un objectif hebdomadaire n'est pas UN objectif qui dure : c'est une SÉRIE qui
// en produit un par semaine. La distinction porte tout le reste.
//
// ⚠️ PARCE QU'UN COMPTEUR QUI NE SE REMET PAS À ZÉRO N'EST PLUS UN OBJECTIF.
// « 20 courses cette semaine » sur un seul document deviendrait « 20 courses
// depuis toujours » : gagné une fois par les anciens, inatteignable pour
// quelqu'un qui arrive au mois deux, et le bonus ne se verserait plus jamais.
// Chaque occurrence a donc sa fenêtre, son enveloppe et ses avancements.
//
// ⚠️ ET L'ENVELOPPE SE ROUVRE AVEC. Une enveloppe partagée entre les occurrences
// aurait fait que la semaine 1 mange le budget de la semaine 4 — l'offre
// s'éteindrait d'elle-même sans que personne ne l'ait décidé.

import (
	"context"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// NextWindow rend la fenêtre de l'occurrence qui couvre `at`.
//
// ⚠️ ELLE S'ALIGNE SUR LE CALENDRIER, pas sur la date de création. Une série
// hebdomadaire lancée un mercredi doit courir du lundi au lundi : sinon
// « cette semaine » ne veut pas dire la même chose pour la plateforme et pour
// la personne, et un chauffeur qui compte ses courses du lundi au dimanche se
// trompe sans comprendre pourquoi.
func NextWindow(repeat string, at time.Time) Window {
	at = at.UTC()
	switch repeat {
	case RepeatWeekly:
		// Lundi 00:00 UTC de la semaine de `at`.
		day := int(at.Weekday())
		if day == 0 {
			day = 7 // dimanche compte pour le 7e jour, pas le premier
		}
		start := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC).
			AddDate(0, 0, -(day - 1))
		return Window{From: start, To: start.AddDate(0, 0, 7)}
	case RepeatMonthly:
		start := time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
		return Window{From: start, To: start.AddDate(0, 1, 0)}
	default:
		return Window{}
	}
}

// occurrence fabrique l'objectif d'une période depuis son modèle.
func occurrence(model *Challenge, w Window, now time.Time) *Challenge {
	id := model.ID
	return &Challenge{
		// ⚠️ LE PAYS EST RECOPIÉ, et c'est le piège des programmations
		// d'abonnement : une occurrence née sans pays n'apparaît dans aucune
		// console, parce que chaque liste est bornée par pays. Elle compterait
		// pourtant, et paierait — un objectif invisible qui dépense.
		Country:     model.Country,
		Audience:    model.Audience,
		Metric:      model.Metric,
		Target:      model.Target,
		RewardXOF:   model.RewardXOF,
		Title:       model.Title,
		Description: model.Description,
		Window:      w,
		// ⚠️ L'OCCURRENCE NE SE RÉPÈTE PAS ELLE-MÊME : c'est la série qui
		// répète. Sans cela, chaque occurrence en engendrerait d'autres, et le
		// balayage produirait une arborescence au lieu d'une suite.
		Repeat: RepeatNone,
		// L'enveloppe du MODÈLE, remise à neuf : compteurs à zéro.
		Limits:     model.Limits,
		Status:     StatusLive,
		SeriesID:   &id,
		CreatedBy:  model.CreatedBy,
		CreatedAt:  now,
		UpdatedAt:  now,
		LaunchedAt: &now,
	}
}

// RunDue matérialise les occurrences manquantes des séries en cours.
//
// ⚠️ ELLE NE MATÉRIALISE QUE LA PÉRIODE COURANTE. Si le balayage n'a pas tourné
// pendant trois semaines, il ne crée PAS trois semaines d'objectifs passés :
// personne n'aurait pu les jouer, et ils apparaîtraient comme des offres
// manquées — ou, pire, se gagneraient d'un coup avec des courses déjà faites.
// Le rattrapage d'un objectif n'a pas de sens ; seul le présent en a.
func (s *Service) RunDue(ctx context.Context) {
	now := s.now()
	series, err := s.repo.List(ctx, Filter{Status: StatusLive}, 200)
	if err != nil {
		slog.ErrorContext(ctx, "challenge: due sweep could not read series", "error", err)
		return
	}
	for i := range series {
		m := &series[i]
		if m.Repeat == RepeatNone || m.SeriesID != nil {
			continue
		}
		w := NextWindow(m.Repeat, now)
		if w.From.IsZero() {
			continue
		}
		has, err := s.hasOccurrence(ctx, m, w)
		if err != nil {
			slog.ErrorContext(ctx, "challenge: occurrence lookup failed",
				"series_id", m.ID.Hex(), "error", err)
			continue
		}
		if has {
			continue
		}
		occ := occurrence(m, w, now)
		if err := s.repo.Insert(ctx, occ); err != nil {
			slog.ErrorContext(ctx, "challenge: occurrence not created",
				"series_id", m.ID.Hex(), "error", err)
			continue
		}
		slog.InfoContext(ctx, "challenge: occurrence launched",
			"series_id", m.ID.Hex(), "challenge_id", occ.ID.Hex(),
			"from", w.From, "to", w.To, "country", occ.Country)
	}
}

// hasOccurrence dit si la période est déjà couverte.
//
// ⚠️ ON COMPARE LA FENÊTRE, pas la date de création. Deux balayages dans la même
// minute — deux instances de l'API, un redémarrage — créeraient sinon deux
// occurrences de la même semaine, chacune avec son enveloppe : l'entreprise
// paierait deux fois ce qu'elle a budgété une.
func (s *Service) hasOccurrence(ctx context.Context, model *Challenge, w Window) (bool, error) {
	rows, err := s.repo.List(ctx, Filter{Audience: model.Audience}, 200)
	if err != nil {
		return false, err
	}
	for _, c := range rows {
		if c.SeriesID != nil && *c.SeriesID == model.ID && c.Window.From.Equal(w.From) {
			return true, nil
		}
	}
	return false, nil
}

// seriesOf rend l'identifiant de série d'un objectif — lui-même s'il est le
// modèle.
func seriesOf(c *Challenge) primitive.ObjectID {
	if c.SeriesID != nil {
		return *c.SeriesID
	}
	return c.ID
}
