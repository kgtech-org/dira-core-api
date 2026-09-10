package staff

import (
	"context"
)

// AccountReader est ce que l'adaptateur ci-dessous attend du module des
// comptes — déclaré ici, comme tout le reste, côté consommateur.
type AccountReader interface {
	AccountByID(ctx context.Context, id string) (*AccountRow, error)
	AccountsByIDs(ctx context.Context, ids []string) ([]AccountRow, error)
}

// AccountRow reprend les champs de compte que ce module lit.
//
// ⚠️ Un type LOCAL plutôt que celui de `internal/user` : ce paquet n'a pas à
// dépendre du module des comptes pour compiler, et l'adaptateur qui les relie
// vit au câblage — c'est ce qui permet de tester les règles de portée sans
// base de données ni annuaire.
type AccountRow struct {
	ID     string
	Role   string
	Name   string
	Phone  string
	Email  string
	Status string
}

// FromAccounts adapte un lecteur de comptes à l'interface `Accounts`.
type FromAccounts struct{ Reader AccountReader }

var _ Accounts = FromAccounts{}

func (f FromAccounts) RoleOf(ctx context.Context, userID string) (string, error) {
	row, err := f.Reader.AccountByID(ctx, userID)
	if err != nil {
		return "", err
	}
	if row == nil {
		return "", nil
	}
	return row.Role, nil
}

func (f FromAccounts) Identities(ctx context.Context, ids []string) (map[string]Identity, error) {
	rows, err := f.Reader.AccountsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Identity, len(rows))
	for _, r := range rows {
		out[r.ID] = Identity{Name: r.Name, Phone: r.Phone, Email: r.Email, Status: r.Status}
	}
	return out, nil
}
