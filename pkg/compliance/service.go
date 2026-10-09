package compliance

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// SubmitDocumentRequest is the driver's upload of one compliance paper.
type SubmitDocumentRequest struct {
	Kind    string `json:"kind" validate:"required,oneof=licence id_card registration insurance"`
	FileURL string `json:"file_url" validate:"required,url,max=2048"`
	// VehicleID est REQUIS pour une carte grise ou une assurance, et REFUSÉ
	// pour un permis ou une pièce d'identité. Le type de la pièce décide ; ce
	// n'est pas à l'application de le savoir, mais elle est prévenue.
	VehicleID string `json:"vehicle_id" validate:"omitempty,len=24,hexadecimal"`
	// ExpiresAt : absent = la pièce ne périme pas (une carte grise). Une
	// valeur DÉJÀ passée est refusée : déposer un papier périmé n'est pas une
	// mise en conformité, et l'accepter ferait croire à l'une comme à l'autre.
	ExpiresAt *time.Time `json:"expires_at"`
}

// ReviewDocumentRequest is the administrator's decision.
type ReviewDocumentRequest struct {
	Status string `json:"status" validate:"required,oneof=valid rejected"`
	// Reason n'a de sens qu'au refus, et le livreur la lit : « photo floue »
	// lui dit quoi refaire, un refus muet le laisse redéposer la même image.
	Reason string `json:"reason" validate:"omitempty,max=300"`
}

// DocumentResponse is one compliance paper as the apps see it.
type DocumentResponse struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	VehicleID string `json:"vehicle_id,omitempty"`
	// ByFleet : déposé par le PROPRIÉTAIRE du véhicule.
	//
	// ⚠️ À DIRE AU CHAUFFEUR, et non à cacher : il voit l'assurance de la
	// voiture qu'il conduit, et il doit comprendre pourquoi elle est là sans
	// qu'il l'ait envoyée — sinon il la redépose, et un opérateur regarde deux
	// fois la même pièce.
	ByFleet bool   `json:"by_fleet,omitempty"`
	FileURL string `json:"file_url,omitempty"`
	// State est l'état EFFECTIF, calculé à la lecture : `pending`, `valid`,
	// `expiring`, `expired` ou `rejected`. C'est lui qu'il faut afficher.
	State          string     `json:"state"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	RejectedReason string     `json:"rejected_reason,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ComplianceResponse is a driver's whole compliance state.
type ComplianceResponse struct {
	Documents []DocumentResponse `json:"documents"`
	// Missing sont les pièces JAMAIS déposées. Rendues à part parce qu'une
	// liste vide ne se distingue pas d'une liste complète : sans ce champ,
	// un livreur qui n'a rien déposé verrait un écran sans défaut.
	//
	// Les pièces de VÉHICULE ne peuvent manquer que si le livreur a un
	// véhicule : on ne réclame pas une assurance à quelqu'un qui n'a rien
	// déclaré.
	Missing []string `json:"missing,omitempty"`
	// MissingFleet sont les pièces attendues d'une VOITURE DE SOCIÉTÉ — celles
	// que son PROPRIÉTAIRE doit déposer, pas son conducteur.
	//
	// ⚠️⚠️ ELLES SONT SORTIES DE `Missing`, ET C'EST TOUT L'INTÉRÊT : la
	// relance automatique se fonde sur `Missing`, et elle réclamait donc à un
	// chauffeur la carte grise d'une voiture qui n'est pas la sienne, tous les
	// trois jours, sans qu'il puisse rien y faire. Servies à part, elles
	// restent visibles — l'exploitation voit ce qui manque, le partenaire voit
	// ce qu'il doit déposer — sans mettre quelqu'un en demeure de fournir le
	// papier d'un autre.
	MissingFleet []string `json:"missing_fleet,omitempty"`
	// Compliant dit si TOUT est en règle — pièces personnelles ET pièces de
	// chaque véhicule déclaré.
	//
	// ⚠️ Informatif : rien n'est bloqué côté serveur. C'est l'exploitation qui
	// suspend, depuis la liste de conformité de la console.
	//
	// ⚠️ ET IL COMPTE LES DEUX LISTES. Un dossier dont il ne manque que la
	// carte grise de la voiture de société n'est pas « en règle » : la voiture
	// roule sans papiers, que la faute soit celle du chauffeur ou non. C'est
	// le DESTINATAIRE du rappel qui change, pas la réalité.
	Compliant bool `json:"compliant"`
}

// Fleet is the VERTICALE, qui possède ses chauffeurs et leurs véhicules.
//
// Déclarée côté consommateur, et en identifiants NUS : cette bibliothèque n'a
// pas à connaître le type `Agent` de la livraison ni le type `Driver` des
// courses. Elle a besoin de quatre réponses, pas de leurs structures.
type Fleet interface {
	// DriverOf rend le chauffeur derrière un compte, ou "" s'il n'y en a pas.
	DriverOf(ctx context.Context, userID string) (driverID string, err error)
	// DriverExists dit si cet identifiant est bien l'un des nôtres.
	DriverExists(ctx context.Context, driverID string) (bool, error)
	// VehicleOwner rend le propriétaire d'un véhicule, ou "".
	//
	// ⚠️ C'est ce qui empêche de déposer une assurance sur le véhicule d'un
	// autre — ce qui le rendrait conforme sans que son propriétaire le sache.
	VehicleOwner(ctx context.Context, vehicleID string) (driverID string, err error)
	// AccountOf rend le COMPTE derrière un chauffeur, ou "".
	//
	// L'inverse de `DriverOf`, et pour une seule raison : la file de
	// conformité est lue par un humain qui doit pouvoir ouvrir la fiche de la
	// personne. Sans ce lien, la console n'affiche qu'un identifiant de
	// chauffeur, et retrouver le compte demande une recherche à la main.
	//
	// ⚠️ AU MIEUX : une erreur ici ne doit pas faire échouer la file. Une file
	// sans noms reste actionnable par ses identifiants ; une file qui ne
	// s'affiche pas ne l'est pas du tout.
	AccountOf(ctx context.Context, driverID string) (userID string, err error)
	// VehicleFleet rend la FLOTTE PRIVÉE d'un véhicule, ou "" quand il
	// appartient à son conducteur.
	//
	// ⚠️ C'est ce qui empêche un propriétaire de déposer l'assurance d'une
	// voiture qui n'est pas la sienne — ce qui la rendrait conforme sans que
	// personne ne le sache. Le pendant exact de `VehicleOwner` pour l'autre
	// déposant.
	VehicleFleet(ctx context.Context, vehicleID string) (fleetID string, err error)
	// VehiclesOfFleet liste le parc d'une flotte, pour dire à son propriétaire
	// ce qu'il lui reste à déposer.
	//
	// ⚠️ Une verticale qui n'a pas de flottes rend une liste VIDE, pas une
	// erreur : la conformité de ses livreurs ne doit pas dépendre d'une
	// fonctionnalité qui ne la concerne pas.
	VehiclesOfFleet(ctx context.Context, fleetID string) ([]VehicleRef, error)
	// VehiclesOf liste les véhicules d'un chauffeur, avec ce qu'il faut pour
	// savoir quels papiers ils exigent.
	//
	// Ils décident de ce qui MANQUE : on ne réclame pas une assurance à
	// quelqu'un qui n'a déclaré aucun véhicule.
	VehiclesOf(ctx context.Context, driverID string) ([]VehicleRef, error)
	// DriverIDs liste les chauffeurs à vérifier, page par page — ce qui permet
	// de trouver les gens à qui il manque des pièces.
	//
	// ⚠️ ON PARCOURT LES GENS, ET NON LES DOCUMENTS, et c'est la raison d'être
	// de cette méthode : ceux qui n'ont RIEN envoyé n'ont aucun document, et un
	// balayage bâti sur les documents les aurait tous ratés — c'est-à-dire
	// exactement ceux qu'il faut relancer.
	//
	// `after` est le dernier identifiant rendu ; vide pour la première page.
	// La liste est bornée au PAYS de la requête par la verticale.
	DriverIDs(ctx context.Context, after string, limit int) ([]string, error)
}

// VehicleRef est un véhicule vu par la conformité : un identifiant, et le
// seul fait qui décide de ses papiers.
//
// ⚠️ MOTORISÉ, et pas le « type » de la verticale. Ce paquet sert deux métiers
// dont les vocabulaires diffèrent — « moto », « velo », « pieton » d'un côté,
// des classes tarifaires de l'autre. Lui faire connaître les deux listes
// l'aurait obligé à changer chaque fois qu'une verticale ajoute un type, et
// c'est bien le CARACTÈRE MOTORISÉ, pas le nom, qui décide qu'un véhicule a
// une carte grise.
type VehicleRef struct {
	ID string
	// Plate : ⚠️ SERVIE POUR LE PROPRIÉTAIRE, qui ne reconnaît pas ses
	// voitures à un hexadécimal. Facultative — un vélo n'en a pas, et c'est
	// d'ailleurs la raison pour laquelle on lui demande une photo.
	Plate string
	// FleetID : la société PROPRIÉTAIRE, ou "" quand le véhicule appartient à
	// son conducteur.
	//
	// ⚠️ SERVI PAR `VehiclesOf`, et non demandé véhicule par véhicule : la
	// verticale lit déjà la ligne du véhicule, elle a le champ sous la main.
	// Une seconde lecture par voiture aurait fait payer la conformité d'un
	// chauffeur au nombre de ses véhicules.
	FleetID string
	// Motorised : un vélo et un livreur à pied n'ont ni carte grise, ni
	// assurance, ni contrôle technique.
	//
	// ⚠️ Sans cette distinction, la plateforme réclamait une carte grise à
	// un livreur À PIED — et il restait « non conforme » pour toujours, sur un
	// écran qui ne lui proposait aucun moyen de régulariser.
	Motorised bool
}

// PersonKindsFor rend les pièces attendues de la PERSONNE, compte tenu de ce
// qu'elle conduit.
//
// ⚠️ LE PERMIS SUIT LES VÉHICULES. On le réclamait à tout le monde — donc à
// un livreur à vélo, qui n'en a pas et n'en aura jamais, et qui restait « non
// conforme » sans aucun moyen de régulariser. Le même défaut que la carte
// grise, un cran plus haut : c'est le caractère motorisé qui crée
// l'obligation, pas le fait d'être livreur.
//
// La règle SUIT : un cycliste qui déclare une moto demain devra son permis
// dès ce jour-là, sans qu'on touche à sa fiche.
func PersonKindsFor(vehicles []VehicleRef) []string {
	out := append([]string(nil), PersonKinds...)
	for _, v := range vehicles {
		if v.Motorised {
			return append(out, DocLicence)
		}
	}
	return out
}

// KindsFor rend les pièces attendues d'un véhicule.
//
// ⚠️ UN VÉHICULE NON MOTORISÉ N'ATTEND AUCUN PAPIER — ni carte grise, ni
// assurance, ni contrôle technique — MAIS IL ATTEND UNE PHOTO, et c'est le seul
// endroit du paquet où le non-motorisé demande plus, pas moins.
//
// La raison est qu'IL N'A PAS DE PLAQUE. Pour une moto ou une voiture, la
// plaque identifie le véhicule : un client qui attend s'entend dire
// « AB-1234-CD », et l'exploitation retrouve l'engin par ce numéro. Un vélo n'a
// rien de tel. La photo est alors la SEULE façon de dire à quelqu'un ce qu'il
// doit chercher dans la rue, et la seule preuve que le véhicule déclaré
// existe. L'absence de papiers n'est pas une absence d'identité.
//
// ⚠️ UNE SEULE PHOTO, ET C'EST `vehicle_side`. Trois vues d'un vélo seraient
// trois fois le même objet — rien à l'arrière, et l'avant d'un deux-roues sans
// plaque ne montre rien. Le côté porte la couleur, le cadre, le panier :
// exactement ce qui le fait reconnaître. `vehicle_front` est documenté comme
// « plaque lisible » ; le réclamer ici aurait fait porter à la pièce une
// attente qu'un vélo ne peut pas satisfaire.
//
// ⚠️ ET UN LIVREUR À PIED ? Il n'a pas de véhicule du tout : aucune
// `VehicleRef` n'est déclarée pour lui, donc cette fonction n'est jamais
// appelée. Le défaut historique — réclamer une carte grise à quelqu'un qui
// marche — venait d'ailleurs, et il est corrigé.
func KindsFor(v VehicleRef) []string {
	if !v.Motorised {
		return []string{DocVehicleSide}
	}
	return VehicleKinds
}

// Auditor enregistre les gestes sensibles. Facultatif.
type Auditor interface {
	Record(ctx context.Context, action, resourceType, resourceID string, before, after any)
}

// Store is the persistence this service needs.
//
// Déclarée côté consommateur, et satisfaite par `*Repository` : c'est ce qui
// permet de vérifier les RÈGLES — qui possède quoi, quand une pièce cesse de
// couvrir — sans base de données. Une règle qu'on ne peut tester qu'avec Mongo
// finit par n'être testée qu'en production.
type Store interface {
	UpsertDocument(ctx context.Context, d *Document) (*Document, error)
	DocumentsByOwner(ctx context.Context, ownerID primitive.ObjectID) ([]Document, error)
	DocumentByID(ctx context.Context, id primitive.ObjectID) (*Document, error)
	ReviewDocument(ctx context.Context, id, reviewer primitive.ObjectID, status, reason string) (*Document, error)
	PendingOrExpiredDocuments(ctx context.Context, now time.Time, limit int) ([]Document, error)
	// FleetDocumentsOfVehicles : les pièces déposées par une SOCIÉTÉ sur ses
	// voitures. Lues par véhicule, parce que c'est lui qu'elles couvrent.
	FleetDocumentsOfVehicles(ctx context.Context, vehicleIDs []primitive.ObjectID) ([]Document, error)
}

// Service applies the compliance rules of ONE vertical.
type Service struct {
	repo  Store
	fleet Fleet
	audit Auditor
	watch Watcher
	// files retire les IMAGES des pièces quand un compte est effacé — voir
	// `erasure.go`. Facultatif.
	files Files
	// remind relance les gens à qui il manque des pièces — voir `campaign.go`.
	// Facultatif : sans elle, `Remind` ne prétend rien avoir envoyé.
	remind Reminder
}

// Watcher est prévenu d'un DÉPÔT : la verticale en fait une alerte au staff
// (« pièce à vérifier ») — le socle ne sait pas qui, dans l'équipe, vérifie
// les papiers de la livraison ou des courses. Facultatif, AU MIEUX.
type Watcher interface {
	DocumentSubmitted(ctx context.Context, driverID, userID, kind, vehicleID string)
}

// SetWatcher branche l'alerte de dépôt (câblage).
func (s *Service) SetWatcher(w Watcher) { s.watch = w }

// NewService builds the compliance service.
func NewService(repo Store, fleet Fleet) *Service {
	return &Service{repo: repo, fleet: fleet}
}

// SetAuditor branche la trace d'audit (câblage).
func (s *Service) SetAuditor(a Auditor) { s.audit = a }

func (s *Service) record(ctx context.Context, action, id string, before, after any) {
	if s.audit == nil {
		return
	}
	s.audit.Record(ctx, action, "compliance_document", id, before, after)
}

var (
	errDriverNotFound  = apperr.NotFound("driver_not_found", "driver not found")
	errVehicleNotFound = apperr.NotFound("vehicle_not_found", "vehicle not found")
)

// SubmitDocument records a driver's paper, replacing the previous one of the
// same kind.
//
// ⚠️ Un dépôt repasse TOUJOURS la pièce en `pending`, même si l'ancienne était
// validée : une image nouvelle est une image que personne n'a regardée.
func (s *Service) SubmitDocument(ctx context.Context, userID string, req SubmitDocumentRequest) (*DocumentResponse, error) {
	driverID, err := s.fleet.DriverOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	if driverID == "" {
		return nil, errDriverNotFound
	}
	ownerOID, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		return nil, errDriverNotFound
	}
	var vehicleOID *primitive.ObjectID
	if req.VehicleID != "" {
		vid, err := primitive.ObjectIDFromHex(req.VehicleID)
		if err != nil {
			return nil, errVehicleNotFound
		}
		owner, err := s.fleet.VehicleOwner(ctx, req.VehicleID)
		if err != nil {
			return nil, err
		}
		// Le véhicule doit être LE SIEN : déposer une assurance sur la moto
		// d'un autre la rendrait conforme sans que son propriétaire le sache.
		if owner != driverID {
			return nil, errVehicleNotFound
		}
		vehicleOID = &vid
	}
	if err := CheckOwner(req.Kind, vehicleOID); err != nil {
		return nil, err
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now().UTC()) {
		return nil, apperr.Validation("expires_at is already past: this document does not bring the driver back in order")
	}
	// ⚠️ CERTAINS TYPES EXIGENT UNE DATE — le casier judiciaire. Sans elle, la
	// pièce vaudrait pour toujours (c'est ce que dit `ExpiresAt == nil`), et un
	// extrait vieux de cinq ans resterait « valide » : le contrôle le plus
	// sensible de la plateforme deviendrait décoratif. Voir `expiryRequired`.
	if req.ExpiresAt == nil && NeedsExpiry(req.Kind) {
		return nil, errDocNeedsExpiry.WithMeta(map[string]any{
			"kind": req.Kind, "fields": []string{"expires_at"},
		})
	}
	doc, err := s.repo.UpsertDocument(ctx, &Document{
		OwnerID: ownerOID, VehicleID: vehicleOID, Kind: req.Kind,
		FileURL: req.FileURL, ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		return nil, err
	}
	s.record(ctx, "compliance.document.submit", doc.ID.Hex(), nil,
		map[string]any{"kind": doc.Kind, "owner_id": driverID})
	if s.watch != nil {
		s.watch.DocumentSubmitted(ctx, driverID, userID, doc.Kind, req.VehicleID)
	}
	out := toDocumentResponse(doc, time.Now().UTC())
	return &out, nil
}

// Compliance rend l'état de conformité d'un livreur.
func (s *Service) Compliance(ctx context.Context, userID string) (*ComplianceResponse, error) {
	driverID, err := s.fleet.DriverOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	if driverID == "" {
		return nil, errDriverNotFound
	}
	return s.complianceOf(ctx, driverID)
}

// ComplianceOfDriver rend l'état de conformité d'un chauffeur DÉSIGNÉ (admin).
func (s *Service) ComplianceOfDriver(ctx context.Context, driverID string) (*ComplianceResponse, error) {
	if _, err := primitive.ObjectIDFromHex(driverID); err != nil {
		return nil, errDriverNotFound
	}
	ok, err := s.fleet.DriverExists(ctx, driverID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errDriverNotFound
	}
	return s.complianceOf(ctx, driverID)
}

func (s *Service) complianceOf(ctx context.Context, driverID string) (*ComplianceResponse, error) {
	oid, err := primitive.ObjectIDFromHex(driverID)
	if err != nil {
		return nil, errDriverNotFound
	}
	docs, err := s.repo.DocumentsByOwner(ctx, oid)
	if err != nil {
		return nil, err
	}
	vehicles, err := s.fleet.VehiclesOf(ctx, driverID)
	if err != nil {
		return nil, err
	}
	// ⚠️ LES PIÈCES DE SES VOITURES DE SOCIÉTÉ, DÉPOSÉES PAR LEUR PROPRIÉTAIRE.
	// Sans cette seconde lecture, un chauffeur dont le partenaire a tout
	// déposé restait « non conforme » : ses papiers existent, mais ils ne
	// portent pas son nom. On lit par VÉHICULE, parce que c'est le véhicule
	// qu'ils couvrent.
	fleetDocs, err := s.repo.FleetDocumentsOfVehicles(ctx, fleetVehicleIDs(vehicles))
	if err != nil {
		return nil, err
	}
	docs = append(docs, fleetDocs...)
	now := time.Now().UTC()
	out := &ComplianceResponse{Documents: make([]DocumentResponse, 0, len(docs)), Compliant: true}

	// Ce qui est COUVERT, par (type, véhicule). La clé porte le véhicule :
	// une assurance couvre UNE moto, pas le parc.
	covered := make(map[string]bool, len(docs))
	for i := range docs {
		d := &docs[i]
		out.Documents = append(out.Documents, toDocumentResponse(d, now))
		if d.Compliant(now) {
			covered[docKey(d.Kind, d.VehicleID)] = true
		}
	}
	for _, kind := range PersonKindsFor(vehicles) {
		if !covered[docKey(kind, nil)] {
			out.Missing = append(out.Missing, kind)
			out.Compliant = false
		}
	}
	for _, v := range vehicles {
		vid, err := primitive.ObjectIDFromHex(v.ID)
		if err != nil {
			continue
		}
		for _, kind := range KindsFor(v) {
			if covered[docKey(kind, &vid)] {
				continue
			}
			// Le véhicule est nommé : « assurance manquante » sur un parc
			// de deux motos ne dit pas laquelle rouler.
			//
			// ⚠️ MAIS LA LISTE DÉPEND DU PROPRIÉTAIRE. Une voiture de société
			// réclame ses papiers à la SOCIÉTÉ : laisser la pièce dans
			// `Missing` mettait le chauffeur en demeure de fournir le papier
			// d'un autre — et la relance le lui redisait tous les trois jours.
			if v.FleetID != "" {
				out.MissingFleet = append(out.MissingFleet, kind+":"+v.ID)
			} else {
				out.Missing = append(out.Missing, kind+":"+v.ID)
			}
			out.Compliant = false
		}
	}
	return out, nil
}

// fleetVehicleIDs garde les véhicules de SOCIÉTÉ, en identifiants.
func fleetVehicleIDs(vehicles []VehicleRef) []primitive.ObjectID {
	out := make([]primitive.ObjectID, 0, len(vehicles))
	for _, v := range vehicles {
		if v.FleetID == "" {
			continue
		}
		if vid, err := primitive.ObjectIDFromHex(v.ID); err == nil {
			out = append(out, vid)
		}
	}
	return out
}

var (
	errNotYourFleetVehicle = apperr.Forbidden("vehicle_not_in_your_fleet",
		"this vehicle does not belong to your fleet")
	// errPersonalDocumentForFleet : une société ne dépose pas un permis.
	//
	// ⚠️ LE REFUS EST EXPLICITE PLUTÔT QUE SILENCIEUX. Un propriétaire qui
	// enverrait le permis de son chauffeur ferait une chose compréhensible et
	// profondément fausse : ce document est une pièce D'IDENTITÉ, elle
	// appartient à la personne, et c'est elle qui décide de la confier à Dira.
	// Accepté, il aurait mis la pièce d'identité de quelqu'un dans le dossier
	// d'une société.
	errPersonalDocumentForFleet = apperr.Validation(
		"this document belongs to the person, not to the company: only vehicle papers can be submitted by a fleet")
)

// SubmitFleetDocument enregistre le papier d'une voiture de SOCIÉTÉ, déposé par
// son propriétaire.
//
// ⚠️⚠️ IL EXISTE PARCE QUE LES PAPIERS D'UNE VOITURE DE SOCIÉTÉ SONT CHEZ SON
// PROPRIÉTAIRE. Jusqu'ici, la carte grise et l'assurance ne pouvaient être
// déposées que par le conducteur : il restait en défaut pour une pièce qu'il
// n'a pas, et la relance le lui redisait tous les trois jours.
//
// ⚠️ ET LE DÉPÔT DU CONDUCTEUR RESTE POSSIBLE, délibérément : les papiers sont
// souvent dans la boîte à gants, et un chauffeur qui peut régulariser sa
// voiture lui-même ne doit pas en être empêché parce que son patron ne répond
// pas. Les deux pièces coexistent alors — clés d'unicité différentes —, et
// l'une comme l'autre prouve l'assurance.
func (s *Service) SubmitFleetDocument(ctx context.Context, fleetID string, req SubmitDocumentRequest) (*DocumentResponse, error) {
	fleetOID, err := primitive.ObjectIDFromHex(fleetID)
	if err != nil {
		return nil, errNotYourFleetVehicle
	}
	// ⚠️ LE VÉHICULE EST OBLIGATOIRE ICI, alors qu'il est facultatif pour une
	// personne : une société n'a que des papiers de voiture. Sans ce contrôle,
	// `CheckOwner` aurait refusé la plupart des cas mais laissé passer un
	// `licence` sans véhicule — c'est-à-dire le permis de quelqu'un rangé dans
	// le dossier d'une entreprise.
	if req.VehicleID == "" {
		return nil, errPersonalDocumentForFleet.WithMeta(map[string]any{"kind": req.Kind})
	}
	vid, err := primitive.ObjectIDFromHex(req.VehicleID)
	if err != nil {
		return nil, errNotYourFleetVehicle
	}
	owner, err := s.fleet.VehicleFleet(ctx, req.VehicleID)
	if err != nil {
		return nil, err
	}
	if owner == "" || owner != fleetID {
		return nil, errNotYourFleetVehicle
	}
	if err := CheckOwner(req.Kind, &vid); err != nil {
		return nil, err
	}
	// ⚠️ ET LES PIÈCES DE LA PERSONNE SONT REFUSÉES MÊME AVEC UN VÉHICULE :
	// `CheckOwner` dit qu'un permis ne porte pas de véhicule, pas qu'une
	// société n'a pas à le déposer. Les deux règles ne disent pas la même
	// chose, et c'est la seconde qui protège la pièce d'identité de quelqu'un.
	if !isVehicleKind(req.Kind) {
		return nil, errPersonalDocumentForFleet.WithMeta(map[string]any{"kind": req.Kind})
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now().UTC()) {
		return nil, apperr.Validation("expires_at is already past: this document does not bring the vehicle back in order")
	}
	if req.ExpiresAt == nil && NeedsExpiry(req.Kind) {
		return nil, errDocNeedsExpiry.WithMeta(map[string]any{
			"kind": req.Kind, "fields": []string{"expires_at"},
		})
	}
	doc, err := s.repo.UpsertDocument(ctx, &Document{
		// ⚠️ LA FLOTTE EST L'OWNER : c'est elle qui répond de cette pièce, et
		// la clé d'unicité (`owner_id`, `kind`, `vehicle_id`) la distingue
		// ainsi du dépôt d'un conducteur sur la même voiture.
		OwnerID: fleetOID, FleetID: &fleetOID, VehicleID: &vid, Kind: req.Kind,
		FileURL: req.FileURL, ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		return nil, err
	}
	s.record(ctx, "compliance.document.submit", doc.ID.Hex(), nil,
		map[string]any{"kind": doc.Kind, "fleet_id": fleetID, "vehicle_id": req.VehicleID})
	// ⚠️ LE MÊME SIGNAL QUE POUR UNE PERSONNE : sans lui, une pièce déposée par
	// un partenaire n'aurait prévenu personne, et elle aurait attendu dans la
	// file qu'un opérateur pense à regarder. C'est exactement le défaut qu'on
	// vient de corriger côté VTC.
	if s.watch != nil {
		s.watch.DocumentSubmitted(ctx, fleetID, "", doc.Kind, req.VehicleID)
	}
	out := toDocumentResponse(doc, time.Now().UTC())
	return &out, nil
}

// FleetCompliance rend l'état des papiers du parc d'une SOCIÉTÉ.
//
// ⚠️ PAR VÉHICULE, et jamais un drapeau global : « votre flotte n'est pas en
// règle » sur douze voitures n'appelle aucun geste. C'est la plaque qui dit
// quoi faire, et c'est pourquoi la verticale rend aussi les plaques.
func (s *Service) FleetCompliance(ctx context.Context, fleetID string) (*FleetComplianceResponse, error) {
	if _, err := primitive.ObjectIDFromHex(fleetID); err != nil {
		return nil, errNotYourFleetVehicle
	}
	vehicles, err := s.fleet.VehiclesOfFleet(ctx, fleetID)
	if err != nil {
		return nil, err
	}
	ids := make([]primitive.ObjectID, 0, len(vehicles))
	for _, v := range vehicles {
		if vid, err := primitive.ObjectIDFromHex(v.ID); err == nil {
			ids = append(ids, vid)
		}
	}
	docs, err := s.repo.FleetDocumentsOfVehicles(ctx, ids)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	byVehicle := map[string][]DocumentResponse{}
	covered := map[string]bool{}
	for i := range docs {
		d := &docs[i]
		if d.VehicleID == nil {
			continue
		}
		key := d.VehicleID.Hex()
		byVehicle[key] = append(byVehicle[key], toDocumentResponse(d, now))
		if d.Compliant(now) {
			covered[docKey(d.Kind, d.VehicleID)] = true
		}
	}
	out := &FleetComplianceResponse{
		Vehicles: make([]FleetVehicleCompliance, 0, len(vehicles)), Compliant: true,
	}
	for _, v := range vehicles {
		vid, err := primitive.ObjectIDFromHex(v.ID)
		if err != nil {
			continue
		}
		row := FleetVehicleCompliance{
			VehicleID: v.ID, Plate: v.Plate,
			Documents: byVehicle[v.ID], Compliant: true,
		}
		for _, kind := range KindsFor(v) {
			if !covered[docKey(kind, &vid)] {
				row.Missing = append(row.Missing, kind)
				row.Compliant = false
				out.Compliant = false
			}
		}
		out.Vehicles = append(out.Vehicles, row)
	}
	return out, nil
}

// FleetComplianceResponse est l'état des papiers d'un parc.
type FleetComplianceResponse struct {
	Vehicles []FleetVehicleCompliance `json:"vehicles"`
	// Compliant : tout le parc est en règle.
	Compliant bool `json:"compliant"`
}

// FleetVehicleCompliance est une voiture et ses papiers.
type FleetVehicleCompliance struct {
	VehicleID string `json:"vehicle_id"`
	// Plate : ⚠️ SERVIE, parce qu'un propriétaire ne reconnaît pas ses
	// voitures à un hexadécimal de vingt-quatre caractères.
	Plate     string             `json:"plate,omitempty"`
	Documents []DocumentResponse `json:"documents,omitempty"`
	// Missing porte des TYPES NUS (`insurance`), sans identifiant : le
	// véhicule est déjà nommé par la ligne.
	Missing   []string `json:"missing,omitempty"`
	Compliant bool     `json:"compliant"`
}

// ReviewDocument records an administrator's decision on one paper.
func (s *Service) ReviewDocument(ctx context.Context, adminID, documentID string, req ReviewDocumentRequest) (*DocumentResponse, error) {
	did, err := primitive.ObjectIDFromHex(documentID)
	if err != nil {
		return nil, errDocumentNotFound
	}
	aid, err := primitive.ObjectIDFromHex(adminID)
	if err != nil {
		return nil, apperr.Validation("invalid admin id").WithCause(err)
	}
	if req.Status == DocRejected && req.Reason == "" {
		// Un refus muet laisse le livreur redéposer la même image. Le motif
		// est ce qui lui dit quoi refaire.
		return nil, apperr.Validation("a rejection needs a reason: the driver reads it")
	}
	doc, err := s.repo.ReviewDocument(ctx, did, aid, req.Status, req.Reason)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, errDocumentNotFound
	}
	s.record(ctx, "delivery.document.review", documentID, nil,
		map[string]any{"status": req.Status, "reason": req.Reason})
	out := toDocumentResponse(doc, time.Now().UTC())
	return &out, nil
}

// ComplianceQueue lists the papers an operator must act on.
//
// ⚠️ C'est la CONTREPARTIE du choix de ne rien bloquer automatiquement. Sans
// cette file, « l'exploitation suspend à la main » veut dire « personne ne
// suspend » : un livreur dont l'assurance a expiré continue d'être appelé, et
// rien ne le signale nulle part.
func (s *Service) ComplianceQueue(ctx context.Context, limit int) ([]ComplianceQueueItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	now := time.Now().UTC()
	docs, err := s.repo.PendingOrExpiredDocuments(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	out := make([]ComplianceQueueItem, 0, len(docs))
	// Le compte est résolu UNE FOIS par chauffeur : une file de cinquante
	// pièces porte souvent trois personnes, et une requête par ligne ferait
	// payer l'affichage d'une liste au nombre de pièces plutôt qu'au nombre
	// de gens.
	accounts := make(map[string]string, len(docs))
	for i := range docs {
		d := &docs[i]
		// ⚠️ UNE PIÈCE DE SOCIÉTÉ N'A PAS DE CHAUFFEUR, et surtout pas celui
		// dont l'identifiant se trouverait là. `owner_id` vaut alors la
		// FLOTTE : la rendre comme un `driver_id` aurait fait cliquer
		// l'opérateur sur la fiche d'un chauffeur qui n'existe pas — et pire,
		// `AccountOf` aurait pu rendre un compte par coïncidence d'identifiant.
		// On nomme la flotte, et le champ du chauffeur reste VIDE.
		if d.FleetID != nil {
			out = append(out, ComplianceQueueItem{
				DocumentResponse: toDocumentResponse(d, now),
				FleetID:          d.FleetID.Hex(),
			})
			continue
		}
		driverID := d.OwnerID.Hex()
		userID, seen := accounts[driverID]
		if !seen {
			// AU MIEUX : l'erreur est IGNORÉE, délibérément. Une file sans
			// noms reste actionnable par ses identifiants ; une file qui
			// échoue ne l'est pas du tout — et c'est précisément l'écran qui
			// existe pour que personne ne passe à côté d'un défaut.
			userID, _ = s.fleet.AccountOf(ctx, driverID)
			accounts[driverID] = userID
		}
		out = append(out, ComplianceQueueItem{
			DocumentResponse: toDocumentResponse(d, now),
			DriverID:         driverID,
			UserID:           userID,
		})
	}
	return out, nil
}

// ComplianceQueueItem is one paper to look at, with who it belongs to.
//
// ⚠️ Le CHAUFFEUR, pas son compte : cette bibliothèque ne connaît pas les
// comptes. La verticale — qui les connaît — y attache le nom avant de rendre
// la file à la console, comme elle le fait pour ses autres listes.
type ComplianceQueueItem struct {
	DocumentResponse
	DriverID string `json:"driver_id,omitempty"`
	// UserID est le COMPTE, quand la verticale a su le donner. Absent plutôt
	// que vide : la console distingue « pas de compte lié » de « compte
	// inconnu », et n'affiche un lien mort ni dans un cas ni dans l'autre.
	UserID string `json:"user_id,omitempty"`
	// FleetID est la SOCIÉTÉ qui a déposé la pièce — et alors `DriverID` est
	// vide.
	//
	// ⚠️ L'UN OU L'AUTRE, JAMAIS LES DEUX : c'est ce qui dit à l'opérateur à
	// qui il doit redemander une photo floue. Une pièce de voiture de société
	// arbitrée comme celle d'un chauffeur aurait fait réclamer la carte grise à
	// quelqu'un qui ne l'a pas.
	FleetID string `json:"fleet_id,omitempty"`
}

func docKey(kind string, vehicleID *primitive.ObjectID) string {
	if vehicleID == nil {
		return kind
	}
	return kind + ":" + vehicleID.Hex()
}

func toDocumentResponse(d *Document, now time.Time) DocumentResponse {
	out := DocumentResponse{
		ID: d.ID.Hex(), Kind: d.Kind, FileURL: d.FileURL,
		State: d.State(now), ExpiresAt: d.ExpiresAt,
		RejectedReason: d.RejectedReason, UpdatedAt: d.UpdatedAt,
	}
	if d.VehicleID != nil {
		out.VehicleID = d.VehicleID.Hex()
	}
	out.ByFleet = d.FleetID != nil
	return out
}
