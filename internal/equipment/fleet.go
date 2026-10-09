package equipment

// LE MATÉRIEL D'UNE SOCIÉTÉ — gilets, casques, supports de téléphone, pris par
// la FLOTTE et non par le chauffeur.
//
// ⚠️⚠️ UN CONTRAT DE FLOTTE N'A PAS DE PORTEUR-PERSONNE, ET C'EST TOUTE LA
// DIFFICULTÉ. Tout ce module est bâti autour d'un `UserID` : la retenue sur
// gains, le prélèvement sur solde, le blocage de la mise en ligne, la purge
// d'un compte effacé — chaque chemin part de la personne. Une société n'est
// aucune de ces choses.
//
// `UserID` reste donc VIDE sur un contrat de flotte, et `FleetID` le porte. Ce
// n'est pas une astuce : c'est ce qui fait que les chemins de l'agent ne
// trouvent jamais ce contrat.
//
//   - `ByUser(uid)` ne le rend pas (zéro ≠ uid) ;
//   - `Collect(userID, …)`, appelé au règlement d'une course, ne le voit pas —
//     donc le matériel d'une société N'EST JAMAIS PRÉLEVÉ sur les gains d'un
//     chauffeur, ce qui serait lui faire payer le gilet de son patron ;
//   - le blocage `402 equipment_overdue` ne touche personne : un impayé de
//     société ne coupe pas le travail d'un chauffeur ;
//   - la purge d'un compte effacé (RGPD) ne l'emporte pas : ce ne sont pas les
//     données d'une personne.
//
// ⚠️⚠️ ET IL N'Y A AUCUN PRÉLÈVEMENT AUTOMATIQUE, parce qu'IL N'EXISTE AUCUN
// GRAND LIVRE DE FLOTTE. Dira enregistre ce que la société doit ; le règlement
// se fait hors de la plateforme, sur facture. Les canaux de recouvrement sont
// donc FORCÉS À FAUX à la création — un plan qui dirait « prélever sur le
// solde » décrirait un prélèvement que rien n'exécute, et l'exploitation
// attendrait un encaissement qui n'arrive jamais.

import (
	"context"
	"strings"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Fleets rend la flotte du compte appelant, son nom et son rôle — déclarée
// côté consommateur, branchée au câblage.
//
// ⚠️ LE RÔLE AVEC, parce que demander du matériel engage la société : un
// LECTEUR (le comptable) n'a pas à commander dix casques.
type Fleets interface {
	MemberFleet(ctx context.Context, userID string) (fleetID, name, role string, err error)
}

// SetFleets branche la résolution de flotte (câblage). Sans elle, la surface
// partenaire n'est pas montée.
func (s *Service) SetFleets(f Fleets) { s.fleets = f }

var (
	errFleetReadOnly = apperr.Forbidden("partner_read_only",
		"your role on this fleet is read-only: ask its owner for manager access")
	errNoFleetSurface = apperr.Forbidden("partner_unavailable",
		"the partner console is not available on this deployment")
)

// RequestForFleet enregistre une demande de matériel AU NOM DE LA SOCIÉTÉ.
func (s *Service) RequestForFleet(ctx context.Context, userID, vertical string, req RequestInput) (*ContractResponse, error) {
	if s.fleets == nil {
		return nil, errNoFleetSurface
	}
	fleetID, fleetName, role, err := s.fleets.MemberFleet(ctx, userID)
	if err != nil {
		// Les refus du socle passent tels quels : leur phrase dit quoi faire.
		return nil, err
	}
	// ⚠️ UN LECTEUR NE COMMANDE PAS. Demander du matériel engage la société
	// pour une caution et des loyers — c'est une écriture, pas une lecture.
	if role != "owner" && role != "manager" {
		return nil, errFleetReadOnly.WithMeta(map[string]any{"role": role})
	}
	fid, err := primitive.ObjectIDFromHex(fleetID)
	if err != nil {
		return nil, errNoFleetSurface
	}

	cc := country.FromContext(ctx)
	st, err := s.repo.Settings(ctx, cc)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	// ⚠️ LE MÊME INTERRUPTEUR DE PAYS QUE POUR UN AGENT. Un pays qui n'ouvre
	// pas la demande en libre-service ne doit pas l'ouvrir aux sociétés par une
	// autre porte — c'est le genre d'écart qu'on ne découvre qu'en voyant des
	// contrats apparaître là où personne ne les attendait.
	if !st.AgentCanRequest {
		return nil, errNoRequest
	}
	if !contains(st.AllowedModes, req.Mode) {
		return nil, errModeNotAllowed
	}
	iid, err := primitive.ObjectIDFromHex(req.ItemID)
	if err != nil {
		return nil, errItemNotFound
	}
	it, err := s.repo.ItemByID(ctx, iid)
	if err != nil {
		return nil, err
	}
	if !it.Active || (len(it.Audiences) > 0 && !contains(it.Audiences, vertical)) {
		return nil, errItemNotFound
	}
	plan := fitPlanToMode(st.Defaults, req.Mode)
	if it.DefaultPlan != nil {
		plan = fitPlanToMode(merge(*it.DefaultPlan, st.Defaults), req.Mode)
	}
	plan = fleetPlan(plan)
	qty := req.Quantity
	if qty < 1 {
		qty = 1
	}
	unit, offered := priceOf(it, req.Mode, plan.Period)
	if !offered {
		return nil, errNotOffered
	}
	now := s.now()
	c := &Contract{
		Country: cc, Vertical: vertical, ItemID: it.ID, ItemName: it.Name, ItemKind: it.Kind,
		Quantity: qty, Mode: req.Mode, Plan: plan, Status: StatusRequested,
		PriceXOF: unit * qty, DepositXOF: it.DepositXOF * qty,
		Notes: strings.TrimSpace(req.Note), RequestedAt: &now, CreatedBy: userID,
		// ⚠️ `UserID` RESTE VIDE : voir l'en-tête de ce fichier. C'est ce qui
		// fait que les chemins de l'agent — retenue sur gains, blocage,
		// purge — ne trouvent jamais ce contrat.
		FleetID:   &fid,
		FleetName: fleetName,
	}
	if err := s.repo.InsertContract(ctx, c); err != nil {
		return nil, apperr.Internal(err)
	}
	// ⚠️ L'ALERTE NOMME LA SOCIÉTÉ, pas la personne qui a cliqué : c'est la
	// société qui signera la décharge, et l'exploitation prépare dix casques
	// pour une flotte, pas pour un répartiteur.
	s.alert(ctx, c, KeyStaffRequested, map[string]string{
		"who": fleetName, "item": c.ItemName, "mode": modeLabel(c.Mode),
	})
	out := toContract(c, now)
	return &out, nil
}

// fleetPlan ferme les canaux de recouvrement automatiques.
//
// ⚠️⚠️ IL N'EXISTE AUCUN GRAND LIVRE DE FLOTTE : ni solde Dira, ni retenue sur
// gains — une société ne fait pas de courses. Garder ces canaux ouverts aurait
// décrit un prélèvement que rien n'exécute, et l'exploitation aurait attendu un
// encaissement qui n'arrive jamais. Le règlement se fait sur FACTURE, hors de
// la plateforme, et l'écran du partenaire le dit.
//
// ⚠️ ET LE BLOCAGE EST RETIRÉ AUSSI (`BlockAfterDays` à zéro) : il coupe la
// mise en ligne d'un AGENT. Sur un contrat de société, il n'a personne à
// couper — mais le laisser à une valeur non nulle ferait croire, en lisant le
// plan, qu'un impayé de flotte bloque quelqu'un.
func fleetPlan(p Plan) Plan {
	p.CollectFromEarnings = false
	p.CollectFromWallet = false
	p.AllowPartial = false
	p.EarningsPercent, p.EarningsFixedXOF = 0, 0
	p.BlockAfterDays = 0
	return p
}

// FleetContracts rend le matériel détenu par la société du compte appelant.
//
// ⚠️ LISIBLE PAR TOUT LE PERSONNEL, lecteur compris : savoir ce que la société
// détient et ce qu'elle doit est précisément le travail d'un comptable.
func (s *Service) FleetContracts(ctx context.Context, userID string) ([]ContractResponse, error) {
	if s.fleets == nil {
		return nil, errNoFleetSurface
	}
	fleetID, _, _, err := s.fleets.MemberFleet(ctx, userID)
	if err != nil {
		return nil, err
	}
	fid, err := primitive.ObjectIDFromHex(fleetID)
	if err != nil {
		return nil, errNoFleetSurface
	}
	items, err := s.repo.ContractsOfFleet(ctx, fid)
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make([]ContractResponse, 0, len(items))
	for i := range items {
		out = append(out, toContract(&items[i], now))
	}
	return out, nil
}
