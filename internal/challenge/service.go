package challenge

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Purse verse le bonus au portefeuille du socle.
//
// ⚠️ IDEMPOTENT PAR CLÉ : la séquence qui paie est « prendre une place → noter
// le franchissement → verser → noter le versement ». Si le processus meurt entre
// les deux derniers, la reprise verserait une seconde fois.
type Purse interface {
	CreditBonus(ctx context.Context, userID string, amountXOF int, challengeID, key string) error
}

// Ledger verse le bonus au GRAND LIVRE d'une verticale.
//
// ⚠️ IL EXISTE PARCE QU'UN CHAUFFEUR VTC N'A PAS DE PORTEFEUILLE AU SOCLE : son
// argent vit dans le grand livre des courses, et c'est la verticale qui le lui
// rend à sa prochaine lecture. C'est la même asymétrie que les cautions de
// matériel, et la méconnaître aurait fait créditer un portefeuille que le
// chauffeur ne regarde jamais — un bonus versé que personne ne voit.
type Ledger interface {
	CreditBonus(ctx context.Context, driverUserID string, amountXOF int, challengeID, title string) error
}

// Notifier annonce à la personne ce qui lui arrive.
type Notifier interface {
	Notify(ctx context.Context, userID, key string, vars, data map[string]string)
}

// Auditor enregistre les gestes sensibles.
type Auditor interface {
	Record(ctx context.Context, action, resourceType, resourceID string, before, after any)
}

// Store est la persistance dont ce service a besoin.
//
// Déclarée côté CONSOMMATEUR, et satisfaite par `*Repository` : c'est ce qui
// permet d'éprouver la logique de FRANCHISSEMENT — celle qui décide qui est
// payé — sans base de données. Une règle d'argent qu'on ne peut tester qu'avec
// Mongo finit par n'être testée qu'en production.
type Store interface {
	Insert(ctx context.Context, c *Challenge) error
	ByID(ctx context.Context, id primitive.ObjectID) (*Challenge, error)
	Save(ctx context.Context, c *Challenge) error
	List(ctx context.Context, f Filter, limit int) ([]Challenge, error)
	AddProgress(ctx context.Context, challengeID, userID primitive.ObjectID, countryCode, ref string, delta int, now time.Time) (*Progress, error)
	ClaimSlot(ctx context.Context, c *Challenge, now time.Time) (bool, error)
	SettleSlot(ctx context.Context, id primitive.ObjectID, rewardXOF int, now time.Time) error
	ReleaseSlot(ctx context.Context, id primitive.ObjectID, rewardXOF int, now time.Time) error
	MarkReached(ctx context.Context, challengeID, userID primitive.ObjectID, now time.Time) (bool, error)
	MarkPaid(ctx context.Context, challengeID, userID primitive.ObjectID, amountXOF int, now time.Time) error
	MarkMissed(ctx context.Context, challengeID, userID primitive.ObjectID, now time.Time) error
	ClaimOwed(ctx context.Context, userID primitive.ObjectID, now time.Time) ([]Progress, error)
	ProgressOf(ctx context.Context, userID primitive.ObjectID, ids []primitive.ObjectID) (map[primitive.ObjectID]Progress, error)
	Winners(ctx context.Context, challengeID primitive.ObjectID, limit int) ([]Progress, error)
}

// Service tient les objectifs.
type Service struct {
	repo   Store
	purse  Purse
	ledger Ledger
	notify Notifier
	audit  Auditor
	now    func() time.Time
}

func NewService(repo Store) *Service {
	return &Service{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) SetPurse(p Purse)       { s.purse = p }
func (s *Service) SetLedger(l Ledger)     { s.ledger = l }
func (s *Service) SetNotifier(n Notifier) { s.notify = n }
func (s *Service) SetAuditor(a Auditor)   { s.audit = a }

// KeyReached est la clé du message « objectif atteint ».
const KeyReached = "challenge_reached"

var (
	errNotFound = apperr.NotFound("challenge_not_found", "this objective does not exist")
	errNotDraft = apperr.Conflict("challenge_not_draft",
		"only a draft can be rewritten: end it and write a new one")
	errAlreadyLive = apperr.Conflict("challenge_already_live", "this objective is already live")
)

// Create écrit un objectif, en brouillon.
//
// ⚠️ EN BROUILLON, TOUJOURS. Un objectif qui démarrerait à l'écriture
// afficherait une faute de frappe à dix mille personnes avant qu'on la voie —
// et une cible fausse déjà atteinte ne se retire plus. Lancer est un second
// geste, et c'est voulu.
func (s *Service) Create(ctx context.Context, actorID string, c *Challenge) (*Challenge, error) {
	Normalise(c)
	c.Status = StatusDraft
	if err := Check(c); err != nil {
		return nil, err
	}
	now := s.now()
	c.Country = country.FromContext(ctx)
	c.CreatedBy, c.CreatedAt, c.UpdatedAt = actorID, now, now
	if err := s.repo.Insert(ctx, c); err != nil {
		return nil, apperr.Internal(err)
	}
	s.record(ctx, "challenge.created", c, nil)
	return c, nil
}

// Update réécrit un BROUILLON.
//
// ⚠️ UN OBJECTIF LANCÉ NE SE RÉÉCRIT PAS. Changer la cible ou le bonus pendant
// qu'il court changerait les règles sous les pieds de gens qui jouent déjà : un
// chauffeur à 18 courses sur 20 verrait soudain 30. C'est le genre de décision
// qui se sait en une journée, et qui coûte plus que ce qu'elle économise.
func (s *Service) Update(ctx context.Context, id string, c *Challenge) (*Challenge, error) {
	cur, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if cur.Status != StatusDraft {
		return nil, errNotDraft
	}
	Normalise(c)
	c.ID, c.Country, c.Status = cur.ID, cur.Country, StatusDraft
	c.CreatedBy, c.CreatedAt = cur.CreatedBy, cur.CreatedAt
	c.Counters = cur.Counters
	if err := Check(c); err != nil {
		return nil, err
	}
	if err := s.repo.Save(ctx, c); err != nil {
		return nil, apperr.Internal(err)
	}
	s.record(ctx, "challenge.updated", c, auditView(cur))
	return c, nil
}

// Launch met un objectif en route.
func (s *Service) Launch(ctx context.Context, id string) (*Challenge, error) {
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.Status == StatusLive {
		return nil, errAlreadyLive
	}
	if c.Status != StatusDraft {
		return nil, errNotDraft
	}
	// ⚠️ ON REVALIDE AU LANCEMENT. Un brouillon écrit il y a trois semaines
	// peut avoir une fenêtre déjà passée : le lancer afficherait un objectif
	// que personne ne peut gagner.
	if err := Check(c); err != nil {
		return nil, err
	}
	now := s.now()
	c.Status, c.LaunchedAt, c.UpdatedAt = StatusLive, &now, now
	if err := s.repo.Save(ctx, c); err != nil {
		return nil, apperr.Internal(err)
	}
	slog.InfoContext(ctx, "challenge: launched",
		"challenge_id", c.ID.Hex(), "audience", c.Audience, "metric", c.Metric,
		"target", c.Target, "reward_xof", c.RewardXOF, "country", c.Country,
		"budget_xof", c.Limits.BudgetXOF, "max_winners", c.Limits.MaxUses)
	s.record(ctx, "challenge.launched", c, nil)
	return c, nil
}

// End arrête un objectif.
//
// ⚠️ ARRÊTER N'EFFACE PAS CE QUI EST GAGNÉ. Quelqu'un qui a franchi la cible
// avant l'arrêt garde son droit : les versements en cours aboutissent, et les
// avancements restent lisibles. Retirer un bonus parce qu'on a coupé l'offre
// serait indéfendable.
func (s *Service) End(ctx context.Context, id string) (*Challenge, error) {
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	now := s.now()
	c.Status, c.EndedAt, c.UpdatedAt = StatusEnded, &now, now
	if err := s.repo.Save(ctx, c); err != nil {
		return nil, apperr.Internal(err)
	}
	s.record(ctx, "challenge.ended", c, nil)
	return c, nil
}

// Report compte UN fait pour une personne, et paie si la cible est franchie.
//
// C'est le point d'entrée des verticales : « cette course est terminée », « cette
// commande est payée ». Une seule méthode, parce que la logique de
// franchissement ne doit exister qu'une fois.
//
// ⚠️ `ref` EST OBLIGATOIRE, ET C'EST LA DÉDUPLICATION. Une verticale réessaie —
// c'est tout l'intérêt de sa file hors ligne — et sans référence la même course
// compterait deux fois : un objectif à 20 se gagnerait à 10, et le bonus serait
// payé pour de bon.
func (s *Service) Report(ctx context.Context, audience, metric, userID, ref string, delta int) error {
	if ref == "" {
		return apperr.Validation("ref is required: it is what stops the same event counting twice").
			WithMeta(map[string]any{"fields": []string{"ref"}})
	}
	if delta <= 0 {
		return nil
	}
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil
	}
	now := s.now()
	live, err := s.repo.List(ctx, Filter{Audience: audience, Live: true, At: now}, 50)
	if err != nil {
		return apperr.Internal(err)
	}
	for i := range live {
		c := &live[i]
		if c.Metric != metric {
			continue
		}
		if err := s.advance(ctx, c, uid, userID, ref, delta, now); err != nil {
			// ⚠️ UN OBJECTIF QUI ÉCHOUE N'ARRÊTE PAS LES AUTRES. Deux objectifs
			// peuvent courir sur la même mesure (« 5 cette semaine » et « 20 ce
			// mois-ci ») : abandonner au premier échec ferait perdre
			// l'avancement du second, sans que rien ne le dise.
			slog.ErrorContext(ctx, "challenge: progress not recorded",
				"challenge_id", c.ID.Hex(), "user_id", userID, "ref", ref, "error", err)
		}
	}
	return nil
}

// advance compte un fait sur UN objectif, et paie au franchissement.
func (s *Service) advance(ctx context.Context, c *Challenge, uid primitive.ObjectID, userID, ref string, delta int, now time.Time) error {
	p, err := s.repo.AddProgress(ctx, c.ID, uid, c.Country, ref, delta, now)
	if err != nil {
		return err
	}
	if p == nil {
		// Doublon correctement ignoré.
		return nil
	}
	if p.Value < c.Target || p.ReachedAt != nil {
		return nil
	}
	// ⚠️ LE FRANCHISSEMENT EST NOTÉ AVANT TOUT LE RESTE, ET UNE SEULE FOIS.
	// Deux courses terminées dans la même seconde franchiraient la cible deux
	// fois ; la seconde ne modifie rien, et ne paie donc rien.
	first, err := s.repo.MarkReached(ctx, c.ID, uid, now)
	if err != nil {
		return err
	}
	if !first {
		return nil
	}
	// L'enveloppe, atomiquement.
	ok, err := s.repo.ClaimSlot(ctx, c, now)
	if err != nil {
		return err
	}
	if !ok {
		// ⚠️ LA CIBLE EST FRANCHIE ET IL N'Y A PLUS D'ARGENT. C'est un aveu,
		// pas un détail : une promesse a été faite et non tenue. On l'écrit sur
		// l'avancement ET on crie dans le journal, pour que l'exploitation
		// décide — payer à la main, ou augmenter l'enveloppe.
		slog.ErrorContext(ctx, "challenge: TARGET REACHED BUT ENVELOPE EXHAUSTED",
			"challenge_id", c.ID.Hex(), "user_id", userID,
			"reward_xof", c.RewardXOF, "budget_xof", c.Limits.BudgetXOF,
			"max_winners", c.Limits.MaxUses)
		return s.repo.MarkMissed(ctx, c.ID, uid, now)
	}
	s.pay(ctx, c, uid, userID, now)
	return nil
}

// pay verse le bonus, par le canal du public.
//
// ⚠️ LE CANAL DÉPEND DU PUBLIC, et c'est l'asymétrie à ne pas rater : un
// chauffeur VTC n'a pas de portefeuille au socle — son argent vit dans le grand
// livre des courses. Créditer le portefeuille lui aurait versé un bonus qu'il ne
// voit jamais.
func (s *Service) pay(ctx context.Context, c *Challenge, uid primitive.ObjectID, userID string, now time.Time) {
	// ⚠️ UN CHAUFFEUR VTC N'EST PAS PAYÉ ICI, ET CE N'EST PAS UN TROU. Il n'a
	// pas de portefeuille au socle : son argent vit dans le grand livre des
	// courses, et le socle ne peut pas appeler une verticale. Le bonus reste
	// donc DÛ — la place de l'enveloppe est tenue, le droit est enregistré —, et
	// la verticale vient le chercher (`ClaimOwed`, surface de service). C'est
	// exactement le chemin des cautions de matériel.
	//
	// ⚠️ ET LA PERSONNE EST PRÉVENUE QUAND MÊME : « objectif atteint, bonus en
	// cours de versement ». Attendre l'argent pour annoncer la victoire ferait
	// douter quelqu'un qui a compté ses courses.
	if c.Audience == ForDriver && s.ledger == nil {
		slog.InfoContext(ctx, "challenge: driver bonus OWED, waiting for the vertical to collect",
			"challenge_id", c.ID.Hex(), "user_id", userID, "reward_xof", c.RewardXOF)
		s.announce(ctx, c, userID)
		return
	}
	key := fmt.Sprintf("challenge:%s:%s", c.ID.Hex(), userID)
	var err error
	switch {
	case c.Audience == ForDriver:
		err = s.ledger.CreditBonus(ctx, userID, c.RewardXOF, c.ID.Hex(), c.Title)
	case s.purse != nil:
		err = s.purse.CreditBonus(ctx, userID, c.RewardXOF, c.ID.Hex(), key)
	default:
		err = fmt.Errorf("no payout channel wired for %s", c.Audience)
	}
	if err != nil {
		// ⚠️ LA PLACE EST RENDUE, sinon l'enveloppe se bloque sur de l'argent
		// intact : au bout de quelques pannes, l'objectif n'accepterait plus
		// personne. ⚠️ Et `reached_at` RESTE : le droit est acquis, et
		// l'exploitation voit un gagnant non payé plutôt qu'un gagnant disparu.
		slog.ErrorContext(ctx, "challenge: BONUS NOT PAID — the right stands, the money did not move",
			"challenge_id", c.ID.Hex(), "user_id", userID, "reward_xof", c.RewardXOF, "error", err)
		if rerr := s.repo.ReleaseSlot(ctx, c.ID, c.RewardXOF, now); rerr != nil {
			slog.ErrorContext(ctx, "challenge: slot not released", "error", rerr)
		}
		return
	}
	if err := s.repo.SettleSlot(ctx, c.ID, c.RewardXOF, now); err != nil {
		slog.ErrorContext(ctx, "challenge: slot not settled", "error", err)
	}
	if err := s.repo.MarkPaid(ctx, c.ID, uid, c.RewardXOF, now); err != nil {
		slog.ErrorContext(ctx, "challenge: payment not recorded", "error", err)
	}
	slog.InfoContext(ctx, "challenge: bonus paid",
		"challenge_id", c.ID.Hex(), "user_id", userID, "reward_xof", c.RewardXOF)
	s.announce(ctx, c, userID)
}

// announce dit à la personne qu'elle a gagné.
func (s *Service) announce(ctx context.Context, c *Challenge, userID string) {
	if s.notify == nil {
		return
	}
	s.notify.Notify(ctx, userID, KeyReached,
		map[string]string{"title": c.Title, "amount": money(c.RewardXOF)},
		map[string]string{"type": "challenge", "challenge_id": c.ID.Hex()})
}

// OwedResponse est un bonus DÛ, tel qu'une verticale le réclame.
type OwedResponse struct {
	ChallengeID string `json:"challenge_id"`
	Title       string `json:"title"`
	AmountXOF   int    `json:"amount_xof"`
}

// ClaimOwed rend les bonus dus à une personne et les solde.
//
// ⚠️ APPELÉE PAR UNE VERTICALE, et elle MARQUE AVANT DE RENDRE. Si l'appel
// aboutit côté socle et échoue au retour, la verticale réessaiera et ne verra
// plus rien : le bonus serait perdu. C'est le compromis assumé — et il penche du
// bon côté, parce que l'inverse (rendre puis marquer) ferait payer DEUX fois,
// ce qui est de l'argent sorti que personne ne réclame. Un bonus perdu, lui,
// laisse une trace : `reached_at` posé, `paid_at` posé, et aucune écriture dans
// le grand livre de la verticale — c'est réconciliable.
func (s *Service) ClaimOwed(ctx context.Context, userID string) ([]OwedResponse, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("invalid user id")
	}
	now := s.now()
	rows, err := s.repo.ClaimOwed(ctx, uid, now)
	if err != nil {
		slog.ErrorContext(ctx, "challenge: owed bonuses partially claimed",
			"user_id", userID, "claimed", len(rows), "error", err)
	}
	out := make([]OwedResponse, 0, len(rows))
	for _, p := range rows {
		c, err := s.repo.ByID(ctx, p.ChallengeID)
		if err != nil || c == nil {
			// ⚠️ UN OBJECTIF ILLISIBLE NE FAIT PAS DISPARAÎTRE LE BONUS : la
			// ligne est déjà marquée payée, et la taire ferait perdre l'argent.
			// On rend ce qu'on sait, sans titre.
			slog.ErrorContext(ctx, "challenge: owed bonus without its objective",
				"challenge_id", p.ChallengeID.Hex(), "user_id", userID)
			out = append(out, OwedResponse{ChallengeID: p.ChallengeID.Hex(), AmountXOF: 0})
			continue
		}
		if err := s.repo.SettleSlot(ctx, c.ID, c.RewardXOF, now); err != nil {
			slog.ErrorContext(ctx, "challenge: slot not settled on claim", "error", err)
		}
		out = append(out, OwedResponse{
			ChallengeID: c.ID.Hex(), Title: c.Title, AmountXOF: c.RewardXOF,
		})
	}
	return out, nil
}

func (s *Service) load(ctx context.Context, id string) (*Challenge, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errNotFound
	}
	c, err := s.repo.ByID(ctx, oid)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if c == nil {
		return nil, errNotFound
	}
	return c, nil
}

func (s *Service) record(ctx context.Context, action string, c *Challenge, before any) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, action, "challenge", c.ID.Hex(), before, auditView(c))
}

func auditView(c *Challenge) map[string]any {
	if c == nil {
		return nil
	}
	return map[string]any{
		"status": c.Status, "audience": c.Audience, "metric": c.Metric,
		"target": c.Target, "reward_xof": c.RewardXOF,
		"budget_xof": c.Limits.BudgetXOF, "max_winners": c.Limits.MaxUses,
	}
}

// money met un montant en forme pour un message.
func money(n int) string {
	s := fmt.Sprintf("%d", n)
	// Des espaces tous les trois chiffres : « 5 000 », pas « 5000 ».
	out := make([]byte, 0, len(s)+len(s)/3)
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ' ')
		}
		out = append(out, c)
	}
	return string(out)
}

// Mine rend les objectifs EN COURS d'une personne, avec son avancement.
//
// ⚠️ LE PUBLIC VIENT DU RÔLE, et non d'un paramètre. Laisser l'application
// choisir aurait permis à un client de lire les objectifs des chauffeurs — et
// surtout d'en réclamer le bonus, puisque le franchissement se fait sur le
// public de l'objectif.
func (s *Service) Mine(ctx context.Context, userID, audience string) ([]MineResponse, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Unauthorized("challenge_unknown_actor", "this session has no account")
	}
	now := s.now()
	live, err := s.repo.List(ctx, Filter{Audience: audience, Live: true, At: now}, 50)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	ids := make([]primitive.ObjectID, 0, len(live))
	for i := range live {
		ids = append(ids, live[i].ID)
	}
	progress, err := s.repo.ProgressOf(ctx, uid, ids)
	if err != nil {
		// AU MIEUX : un avancement illisible ne doit pas cacher les objectifs.
		// Montrer « 0 sur 20 » est faux, mais montrer un écran vide ferait
		// croire qu'il n'y a rien à gagner.
		slog.WarnContext(ctx, "challenge: progress not read", "user_id", userID, "error", err)
		progress = map[primitive.ObjectID]Progress{}
	}
	out := make([]MineResponse, 0, len(live))
	for i := range live {
		c := &live[i]
		var p *Progress
		if v, ok := progress[c.ID]; ok {
			p = &v
		}
		out = append(out, toMine(c, p))
	}
	return out, nil
}

// List rend les objectifs à la console, avec leurs gagnants comptés.
func (s *Service) List(ctx context.Context, f Filter, limit int) ([]Response, error) {
	f.At = s.now()
	rows, err := s.repo.List(ctx, f, limit)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]Response, 0, len(rows))
	for i := range rows {
		c := &rows[i]
		// ⚠️ LES COMPTEURS DE L'ENVELOPPE DISENT LES GAGNANTS PAYÉS ; les
		// MANQUÉS, eux, ne sont que dans les avancements. Un objectif
		// sous-doté a donc des gagnants non comptés par l'enveloppe, et c'est
		// exactement ce qu'il faut voir.
		winners, missed := c.Counters.Uses(), 0
		if rows[i].Status != StatusDraft {
			if w, err := s.repo.Winners(ctx, c.ID, 500); err == nil {
				winners = len(w)
				for _, p := range w {
					if p.Missed {
						missed++
					}
				}
			}
		}
		out = append(out, toResponse(c, winners, missed))
	}
	return out, nil
}

// Get rend un objectif.
func (s *Service) Get(ctx context.Context, id string) (*Response, error) {
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	winners, missed := c.Counters.Uses(), 0
	if w, err := s.repo.Winners(ctx, c.ID, 500); err == nil {
		winners = len(w)
		for _, p := range w {
			if p.Missed {
				missed++
			}
		}
	}
	out := toResponse(c, winners, missed)
	return &out, nil
}

// WinnerResponse est un gagnant tel que la console le lit.
type WinnerResponse struct {
	UserID    string     `json:"user_id"`
	Value     int        `json:"value"`
	ReachedAt *time.Time `json:"reached_at,omitempty"`
	PaidAt    *time.Time `json:"paid_at,omitempty"`
	PaidXOF   int        `json:"paid_xof,omitempty"`
	// Missed : il a atteint la cible et n'a pas été payé. ⚠️ À MONTRER EN
	// PREMIER : c'est une promesse non tenue, et l'exploitation doit décider.
	Missed bool `json:"missed,omitempty"`
}

// WinnersOf rend les gagnants d'un objectif.
func (s *Service) WinnersOf(ctx context.Context, id string) ([]WinnerResponse, error) {
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.Winners(ctx, c.ID, 500)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := make([]WinnerResponse, 0, len(rows))
	for _, p := range rows {
		out = append(out, WinnerResponse{
			UserID: p.UserID.Hex(), Value: p.Value,
			ReachedAt: p.ReachedAt, PaidAt: p.PaidAt, PaidXOF: p.PaidXOF,
			Missed: p.Missed,
		})
	}
	return out, nil
}
