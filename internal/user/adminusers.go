package user

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/audit"
	"github.com/kgtech-org/dira-core-api/pkg/auth"
	"github.com/kgtech-org/dira-core-api/pkg/httpx"
	"github.com/kgtech-org/dira-core-api/pkg/middleware"
)

// AdminUsers exposes back-office account management (create/update/delete;
// status changes live in the admin module). Kept separate from Service so
// the public auth flow and its test fakes stay untouched.
type AdminUsers struct {
	repo    *Repository
	wallets WalletCreator
	auditor *audit.Recorder
}

func NewAdminUsers(repo *Repository, wallets WalletCreator, auditor *audit.Recorder) *AdminUsers {
	return &AdminUsers{repo: repo, wallets: wallets, auditor: auditor}
}

// AdminCreateUserRequest creates an account with any role (admin included).
type AdminCreateUserRequest struct {
	Role     string `json:"role" validate:"required,oneof=client driver merchant admin"`
	Name     string `json:"name" validate:"required,min=1,max=120"`
	Phone    string `json:"phone" validate:"required,e164"`
	Email    string `json:"email" validate:"omitempty,email"`
	Password string `json:"password" validate:"required,min=8,max=128"`
}

// AdminUpdateUserRequest patches identity fields; nil fields are unchanged.
type AdminUpdateUserRequest struct {
	Name      *string `json:"name" validate:"omitempty,min=1,max=120"`
	Email     *string `json:"email" validate:"omitempty,email"`
	Phone     *string `json:"phone" validate:"omitempty,e164"`
	Password  *string `json:"password" validate:"omitempty,min=8,max=128"`
	AvatarURL *string `json:"avatar_url" validate:"omitempty,url"`
}

// Create adds an account; drivers get their token wallet.
func (a *AdminUsers) Create(ctx context.Context, req AdminCreateUserRequest) (UserResponse, error) {
	phoneNumber, err := canonPhone(req.Phone)
	if err != nil {
		return UserResponse{}, err
	}
	req.Phone = phoneNumber
	hash, err := HashPassword(req.Password)
	if err != nil {
		return UserResponse{}, apperr.Internal(err)
	}
	now := time.Now().UTC()
	u := &User{
		Role: req.Role, Name: req.Name, Phone: req.Phone,
		Email:        strings.ToLower(req.Email),
		PasswordHash: hash, Status: StatusActive,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := a.repo.CreateUser(ctx, u); err != nil {
		if err == ErrDuplicatePhone {
			return UserResponse{}, errPhoneTaken
		}
		return UserResponse{}, apperr.Internal(err)
	}
	if req.Role == "driver" && a.wallets != nil {
		if err := a.wallets.CreateWallet(ctx, u.ID.Hex(), "driver"); err != nil {
			_ = a.repo.DeleteUser(ctx, u.ID)
			return UserResponse{}, apperr.Internal(err)
		}
	}
	if a.auditor != nil {
		a.auditor.Record(ctx, "user.create", "user", u.ID.Hex(), nil,
			map[string]any{"role": u.Role, "phone": u.Phone})
	}
	return newUserResponse(u), nil
}

// Update patches identity fields (and optionally resets the password).
func (a *AdminUsers) Update(ctx context.Context, id string, req AdminUpdateUserRequest) (UserResponse, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return UserResponse{}, errUserNotFound.WithCause(err)
	}
	u, err := a.repo.FindByID(ctx, oid)
	if err != nil {
		return UserResponse{}, apperr.Internal(err)
	}
	if u == nil {
		return UserResponse{}, errUserNotFound
	}
	before := map[string]any{"name": u.Name, "email": u.Email, "phone": u.Phone}
	if req.Name != nil {
		u.Name = *req.Name
	}
	if req.Email != nil {
		u.Email = strings.ToLower(*req.Email)
	}
	if req.Phone != nil {
		phoneNumber, err := canonPhone(*req.Phone)
		if err != nil {
			return UserResponse{}, err
		}
		u.Phone = phoneNumber
	}
	if req.AvatarURL != nil {
		u.AvatarURL = *req.AvatarURL
	}
	if req.Password != nil {
		hash, err := HashPassword(*req.Password)
		if err != nil {
			return UserResponse{}, apperr.Internal(err)
		}
		u.PasswordHash = hash
	}
	u.UpdatedAt = time.Now().UTC()
	if err := a.repo.UpdateUser(ctx, u); err != nil {
		if errors.Is(err, ErrDuplicatePhone) {
			return UserResponse{}, errPhoneTaken
		}
		return UserResponse{}, apperr.Internal(err)
	}
	if a.auditor != nil {
		a.auditor.Record(ctx, "user.update", "user", id, before,
			map[string]any{"name": u.Name, "email": u.Email, "phone": u.Phone})
	}
	return newUserResponse(u), nil
}

// Delete removes an account. Business records referencing the user (orders,
// deliveries, wallets) are kept for accounting — ⚠️ retention policy to
// validate with the team (a soft-delete may be preferable in production).
func (a *AdminUsers) Delete(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return errUserNotFound.WithCause(err)
	}
	u, err := a.repo.FindByID(ctx, oid)
	if err != nil {
		return apperr.Internal(err)
	}
	if u == nil {
		return errUserNotFound
	}
	if actorID, _ := auth.UserFromContext(ctx); actorID == id {
		return apperr.Conflict("cannot_delete_self", "you cannot delete your own account")
	}
	if err := a.repo.DeleteUser(ctx, oid); err != nil {
		return apperr.Internal(err)
	}
	if a.auditor != nil {
		a.auditor.Record(ctx, "user.delete", "user", id,
			map[string]any{"role": u.Role, "phone": u.Phone, "name": u.Name}, nil)
	}
	return nil
}

// Mount registers the admin account-management routes.
func (a *AdminUsers) Mount(r chi.Router, authMW func(http.Handler) http.Handler) {
	r.Group(func(g chi.Router) {
		g.Use(authMW)
		admin := middleware.RequireRole(auth.RoleAdmin)
		g.With(admin).Post("/admin/users", a.create)
		g.With(admin).Patch("/admin/users/{id}", a.update)
		g.With(admin).Delete("/admin/users/{id}", a.delete)
	})
}

func (a *AdminUsers) create(w http.ResponseWriter, r *http.Request) {
	var req AdminCreateUserRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := a.Create(r.Context(), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, resp)
}

func (a *AdminUsers) update(w http.ResponseWriter, r *http.Request) {
	var req AdminUpdateUserRequest
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, r, err)
		return
	}
	resp, err := a.Update(r.Context(), chi.URLParam(r, "id"), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func (a *AdminUsers) delete(w http.ResponseWriter, r *http.Request) {
	if err := a.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
