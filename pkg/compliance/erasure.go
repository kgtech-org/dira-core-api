package compliance

// L'EFFACEMENT D'UN COMPTE, VU D'ICI : les pièces de conformité sont ce que la
// plateforme détient de plus intime sur un agent — photo de permis, carte
// d'identité, assurance. Quand le socle annonce qu'un compte est effacé, elles
// doivent partir, LIGNE ET IMAGE.
//
// ⚠️ LA LIGNE SANS L'IMAGE EST LE PIRE DES DEUX MONDES. Il resterait dans le
// bucket une photo de carte d'identité que plus rien ne désigne : impossible à
// retrouver pour la supprimer, impossible à justifier si on la trouve.

import (
	"context"
	"log/slog"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Purger est ce que le service attend du dépôt pour effacer.
type Purger interface {
	DocumentsOf(ctx context.Context, ownerID primitive.ObjectID) ([]Document, error)
	DeleteDocumentsOf(ctx context.Context, ownerID primitive.ObjectID) (int64, error)
}

// Files retire un fichier du stockage d'objets, désigné par son URL publique.
//
// Déclarée côté consommateur — c'est `pkg/storage.Store`. FACULTATIVE : sans
// elle, les lignes partent quand même et le journal crie. Mieux vaut une
// image orpheline qu'un droit à l'effacement bloqué par un stockage
// indisponible, mais il faut pouvoir le savoir.
type Files interface {
	Remove(ctx context.Context, publicURL string) error
}

// SetFiles branche le stockage d'objets (câblage).
func (s *Service) SetFiles(f Files) { s.files = f }

// PurgeOf jette les pièces d'un agent effacé, et rend combien de lignes sont
// parties.
//
// ⚠️ LES IMAGES D'ABORD, LES LIGNES ENSUITE. L'inverse perdrait les URL avant
// d'avoir retiré les fichiers, et il ne resterait aucun moyen de les retrouver.
// Une image qui résiste n'empêche pas la ligne de partir : on journalise, et la
// relance de l'annonce ne la retrouvera pas — c'est le prix assumé de ne pas
// bloquer un effacement sur un bucket.
// ⚠️ ET ELLE NE TOUCHE PAS AUX PAPIERS D'UNE VOITURE DE SOCIÉTÉ. Ceux-là
// portent la FLOTTE comme `owner_id` (voir `Document.FleetID`), donc ce
// balayage par propriétaire les laisse en place — et c'est juste : la carte
// grise d'une voiture d'entreprise n'est pas une donnée personnelle du
// chauffeur qui s'en va, et la voiture continue de rouler avec quelqu'un
// d'autre. L'effacer aurait mis une société en défaut parce qu'un de ses
// conducteurs a fermé son compte.
func (s *Service) PurgeOf(ctx context.Context, ownerID primitive.ObjectID) (int64, error) {
	purger, ok := s.repo.(Purger)
	if !ok {
		slog.ErrorContext(ctx, "compliance: this store cannot purge — papers of an erased account stay",
			"owner_id", ownerID.Hex())
		return 0, nil
	}
	docs, err := purger.DocumentsOf(ctx, ownerID)
	if err != nil {
		return 0, err
	}
	if s.files == nil {
		if len(docs) > 0 {
			slog.ErrorContext(ctx, "compliance: no object store wired — the ID and licence IMAGES of an erased account stay in the bucket",
				"owner_id", ownerID.Hex(), "documents", len(docs))
		}
	} else {
		for _, d := range docs {
			if d.FileURL == "" {
				continue
			}
			if err := s.files.Remove(ctx, d.FileURL); err != nil {
				slog.ErrorContext(ctx, "compliance: a paper's image survived the erasure",
					"owner_id", ownerID.Hex(), "kind", d.Kind, "error", err)
			}
		}
	}
	return purger.DeleteDocumentsOf(ctx, ownerID)
}
