package user

import "context"

// IDByPhone resolves a user id from a phone number. Used by the WhatsApp
// wiring adapter to attach off-app conversations to accounts.
func (s *Service) IDByPhone(ctx context.Context, phone string) (string, error) {
	phone, err := canonPhone(phone)
	if err != nil {
		return "", err
	}
	u, err := s.repo.FindByPhone(ctx, phone)
	if err != nil {
		return "", err
	}
	if u == nil {
		return "", errUserNotFound
	}
	return u.ID.Hex(), nil
}
