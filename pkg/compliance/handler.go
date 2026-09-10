package compliance

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

// Routes porte les segments d'URL de la VERTICALE.
//
// ⚠️ Une structure à champs NOMMÉS, et non deux arguments `string`. Les deux
// valeurs sont du même type et se ressemblent — « agent » et « agents » —, si
// bien qu'une inversion compilerait, passerait les tests unitaires de la
// bibliothèque, et ne se verrait qu'à l'appel d'une route en production.
//
// Elles ne sont pas non plus DÉDUITES l'une de l'autre : mettre un « s » est
// une convention qui tient pour ces deux mots et casse au premier segment qui
// ne se pluralise pas ainsi.
type Routes struct {
	// Self est le segment du porteur qui parle de LUI-MÊME : « agent » pour
	// la livraison (`/agent/documents`), « driver » pour les courses.
	Self string
	// Owners est le segment de l'ADMINISTRATION désignant quelqu'un :
	// « agents » (`/admin/agents/{id}/documents`), « drivers ».
	Owners string
}

// Handler expose les quatre routes de conformité.
//
// ⚠️ Le Handler est DANS la bibliothèque, alors que le paquet ne connaît ni
// livreurs ni chauffeurs. C'est délibéré : les quatre routes sont les mêmes
// des deux côtés — déposer, lire, la file, arbitrer — et les recopier dans
// chaque verticale ferait diverger deux surfaces qui doivent rester jumelles.
// Ce qui diffère tient en deux segments d'URL, et c'est un ARGUMENT.
type Handler struct {
	svc    *Service
	routes Routes
}

// NewHandler builds the HTTP surface for one vertical.
func NewHandler(svc *Service, routes Routes) *Handler {
	return &Handler{svc: svc, routes: routes}
}

// Mount registers the module routes on the /api/v1 router.
//
// authMW est le middleware JWT construit au câblage.
func (h *Handler) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	self := "/" + h.routes.Self + "/documents"
	owner := "/admin/" + h.routes.Owners + "/{id}/documents"

	r.Group(func(g chi.Router) {
		g.Use(authMW)

		// Le porteur : son état, et le dépôt d'une pièce.
		driver := middleware.RequireRole(auth.RoleDriver)
		g.With(driver).Get(self, h.compliance)
		g.With(driver).Post(self, h.submitDocument)

		admin := middleware.RequireRole(auth.RoleAdmin)
		// ⚠️ La FILE est la contrepartie d'une décision produit : rien n'est
		// bloqué côté serveur. Sans elle, « l'exploitation suspend à la
		// main » voudrait dire « personne ne suspend », parce que personne ne
		// saurait qui est en défaut.
		g.With(admin).Get("/admin/compliance", h.complianceQueue)
		g.With(admin).Get(owner, h.ownerCompliance)
		g.With(admin).Patch("/admin/documents/{id}", h.reviewDocument)
	})
}

// GET /{agent|driver}/documents — l'état de conformité de l'appelant.
func (h *Handler) compliance(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	resp, err := h.svc.Compliance(r.Context(), userID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// POST /{agent|driver}/documents — dépose une pièce, en remplaçant la précédente.
func (h *Handler) submitDocument(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserFromContext(r.Context())
	var req SubmitDocumentRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.SubmitDocument(r.Context(), userID, req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, resp)
}

// GET /admin/compliance — la file de ce qu'il faut regarder.
func (h *Handler) complianceQueue(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.svc.ComplianceQueue(r.Context(), limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// ⚠️ Les lignes portent des IDENTIFIANTS, pas des noms : cette
	// bibliothèque ne connaît pas les comptes. C'est la verticale qui attache
	// les noms dans sa propre vue d'administration.
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// GET /admin/{agents|drivers}/{id}/documents — la conformité d'un porteur désigné.
func (h *Handler) ownerCompliance(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.ComplianceOfDriver(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// PATCH /admin/documents/{id} — valider ou refuser une pièce.
func (h *Handler) reviewDocument(w http.ResponseWriter, r *http.Request) {
	adminID, _ := auth.UserFromContext(r.Context())
	var req ReviewDocumentRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := h.svc.ReviewDocument(r.Context(), adminID, chi.URLParam(r, "id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}
