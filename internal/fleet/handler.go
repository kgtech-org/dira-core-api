package fleet

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

// Handler exposes the fleet routes.
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount registers the back-office routes on the /api/v1 router.
//
// ⚠️ ADMINISTRATION SEULEMENT, et aucune route publique. Une flotte est un
// contrat commercial : son taux de commission et sa référence de contrat n'ont
// rien à faire dans une application mobile, et une lecture publique aurait
// exposé la grille de négociation d'une société à ses concurrentes.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		admin := middleware.RequireRole(auth.RoleAdmin)
		g.With(admin).Get("/admin/fleets", h.list)
		g.With(admin).Post("/admin/fleets", h.create)
		g.With(admin).Get("/admin/fleets/{id}", h.get)
		g.With(admin).Patch("/admin/fleets/{id}", h.update)
		// OUVRIR L'ACCÈS À LA CONSOLE PARTENAIRE — voir `access.go`.
		//
		// ⚠️ UNE ROUTE D'EXPLOITATION, et il ne peut pas en être autrement : un
		// rôle `partner` donne des pouvoirs sur le TRAVAIL D'AUTRES PERSONNES
		// (poser une limite de dette, reprendre une voiture). Un rôle qu'on
		// obtient en postant un formulaire d'inscription ne peut pas porter
		// cela — c'est pourquoi `/auth/register` le refuse, et pourquoi
		// l'ouverture se fait ici, au moment où l'on enregistre le contrat.
		g.With(admin).Post("/admin/fleets/{id}/access", h.openAccess)
	})
	r.Group(func(g chi.Router) {
		// LA CONSOLE PARTENAIRE.
		//
		// ⚠️ UNE SEULE ROUTE, ET AUCUN IDENTIFIANT DANS LE CHEMIN. C'est la
		// garantie d'isolement de toute la surface partenaire : il n'existe
		// pas de `GET /partner/fleets/{id}` à essayer. La flotte vient du
		// JETON, et un partenaire curieux n'a aucun chiffre à changer dans
		// une URL.
		g.Use(authMW, middleware.RequireRole(auth.RolePartner))
		g.Get("/partner/me", h.partnerMe)
	})
}

// POST /admin/fleets/{id}/access {password} — ouvrir l'accès du partenaire.
//
// ⚠️ LE CORPS NE PORTE QUE LE MOT DE PASSE : le nom, le numéro et l'adresse
// viennent du CONTACT déjà enregistré sur la fiche. Deux endroits où taper le
// gérant auraient divergé le jour où l'un des deux change — et c'est l'autre
// qui sert à le joindre.
func (h *Handler) openAccess(w http.ResponseWriter, r *http.Request) {
	var req struct {
		// ⚠️ Huit caractères au minimum, et c'est l'exploitant qui le CHOISIT
		// plutôt qu'un tirage du serveur : il doit le transmettre par
		// téléphone à quelqu'un qui le notera, et un mot de passe illisible au
		// téléphone finit écrit sur la fiche de la flotte.
		Password string `json:"password" validate:"required,min=8,max=120"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.OpenAccess(r.Context(), chi.URLParam(r, "id"), req.Password)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GET /partner/me — MA flotte.
//
// ⚠️ ELLE SERT AUSSI DE PORTE : un compte partenaire sans flotte rattachée
// reçoit `403 partner_no_fleet`, et une flotte suspendue
// `403 partner_fleet_suspended`. La console appelle donc cette route AVANT
// d'afficher quoi que ce soit — un écran vide ferait croire à une panne là où
// il s'agit d'un contrat à enregistrer.
func (h *Handler) partnerMe(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	out, err := h.svc.FleetOfOwner(r.Context(), userID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// MountService registers what a VERTICAL is allowed to ask.
//
// ⚠️ Une seule route, et elle ne rend que des NOMS. Une verticale affiche
// « Flotte Sodigaz » à côté d'une plaque ; lui ouvrir la fiche entière lui
// confierait un contrat et une commission qu'elle n'a aucune raison de porter
// — et qu'elle finirait par recopier chez elle, où les deux copies
// divergeraient.
func (h *Handler) MountService(r chi.Router, serviceMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(serviceMW)
		g.Post("/internal/fleets/names", h.names)
		// LA FLOTTE D'UN COMPTE PARTENAIRE — ce qu'une verticale demande pour
		// borner les listes de sa console partenaire.
		//
		// ⚠️ DEMANDÉE À CHAQUE REQUÊTE, ET NON MISE DANS LE JETON. Une
		// revendication de flotte dans le JWT aurait évité cet aller-retour,
		// et laissé un partenaire détaché de sa flotte continuer à la gérer
		// jusqu'à l'expiration de son jeton — trente jours de rafraîchissement.
		// Le contrôle de SUSPENSION, lui aussi, serait resté figé à la
		// connexion. La console partenaire compte quelques dizaines de
		// personnes : l'appel ne coûte rien, et il dit la vérité d'maintenant.
		g.Post("/internal/fleets/of-owner", h.ownerFleet)
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	items, next, err := h.svc.List(r.Context(),
		r.URL.Query().Get("status"), r.URL.Query().Get("q"), httpx.PageFromRequest(r))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	actorID, _ := auth.UserFromContext(r.Context())
	var req CreateRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Create(r.Context(), actorID, req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	actorID, _ := auth.UserFromContext(r.Context())
	var req UpdateRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Update(r.Context(), actorID, chi.URLParam(r, "id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// POST /internal/fleets/names — résolution en lot, pour une verticale.
func (h *Handler) names(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids" validate:"required,max=200"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	names, err := h.svc.Names(r.Context(), req.IDs)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"names": names})
}

// POST /internal/fleets/of-owner {user_id} — la flotte d'un partenaire.
//
// ⚠️ ELLE PORTE LES MÊMES REFUS QUE `/partner/me` : pas de flotte, flotte
// suspendue. Une verticale qui n'aurait reçu qu'un identifiant sans ces
// contrôles aurait laissé un partenaire suspendu gérer son parc par une autre
// porte — et c'est exactement le genre d'écart qu'on ne découvre jamais, parce
// que la porte principale, elle, refuse correctement.
func (h *Handler) ownerFleet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id" validate:"required,len=24,hexadecimal"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.FleetOfOwner(r.Context(), req.UserID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"fleet_id": out.ID, "name": out.Name, "status": out.Status,
		// ⚠️ LE RÔLE TRAVERSE JUSQU'AUX VERTICALES, et il le faut : ce sont
		// elles qui portent les écritures (reprendre une voiture, poser une
		// limite de dette). Sans lui, un `viewer` — un comptable — pourrait
		// couper le travail de quelqu'un depuis la console des courses, alors
		// que le socle lui refuse déjà d'ajouter un collègue.
		"role": out.Role,
	})
}

// ---------------------------------------------------------------------------
// LE PERSONNEL DE LA FLOTTE — voir members.go.
// ---------------------------------------------------------------------------

// MountPartnerStaff expose la gestion du personnel au PROPRIÉTAIRE.
//
// ⚠️ TOUJOURS SANS IDENTIFIANT DE FLOTTE DANS LE CHEMIN : elle vient du jeton.
// Le seul identifiant de ces routes est celui d'un MEMBRE, qu'on retire — et il
// est vérifié contre la flotte de l'appelant.
func (h *Handler) MountPartnerStaff(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW, middleware.RequireRole(auth.RolePartner))
		g.Get("/partner/members", h.partnerMembers)
		g.Post("/partner/members", h.partnerAddMember)
		g.Delete("/partner/members/{userID}", h.partnerRemoveMember)
	})
}

// GET /partner/members — le personnel de la flotte.
//
// ⚠️ LISIBLE PAR TOUT LE PERSONNEL, y compris un `viewer` : savoir qui a accès
// à la console n'est pas un pouvoir, c'est le contraire — c'est ce qui permet à
// quelqu'un de signaler un compte qui ne devrait plus être là.
func (h *Handler) partnerMembers(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	items, err := h.svc.Members(r.Context(), userID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// POST /partner/members {name, phone, email, role, password}
//
// ⚠️ SEUL LE PROPRIÉTAIRE, et c'est la règle qui tient les autres : un
// `manager` qui pourrait ajouter quelqu'un s'ajouterait un second compte de
// `manager` — ou mettrait le propriétaire dehors.
func (h *Handler) partnerAddMember(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	var req AddMemberRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.AddMember(r.Context(), userID, req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// DELETE /partner/members/{userID} — détacher un compte.
//
// ⚠️ DÉTACHER, PAS SUPPRIMER : c'est peut-être le compte personnel de
// quelqu'un, et une société n'a pas à pouvoir effacer la personne qu'elle
// congédie.
func (h *Handler) partnerRemoveMember(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	if err := h.svc.RemoveMember(r.Context(), userID, chi.URLParam(r, "userID")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
