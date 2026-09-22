package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kgtech-org/dira-core-api/internal/equipment"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// LE CATALOGUE DU MATÉRIEL — ce que l'exploitation LOUE aux livreurs et
// chauffeurs, par pays : un téléphone portable (l'application tourne
// dessus) et un sac de livraison isotherme. Loyers et cautions dans la
// monnaie du pays, en entiers ; la Guinée compte en francs guinéens, quinze
// fois plus petits.
//
// Idempotent : un article du même genre et du même nom dans le pays n'est
// pas recréé — ni ses prix réécrits, l'exploitation les règle ensuite depuis
// la console.
type equipmentSeed struct {
	kind, name, description               string
	audiences                             []string
	sale, daily, weekly, monthly, deposit int
	stock                                 int
}

func equipmentCatalogue(currency string) []equipmentSeed {
	// 1 XOF/XAF ≈ 15 GNF.
	x := 1
	if currency == "GNF" {
		x = 15
	}
	return []equipmentSeed{
		{
			kind: equipment.KindPhone, name: "Téléphone portable Dira",
			description: "Smartphone Android avec l'application installée et une carte SIM data. Loué à la journée, à la semaine ou au mois ; caution rendue au retour en bon état.",
			daily:       500 * x, weekly: 3000 * x, monthly: 10000 * x, deposit: 15000 * x, stock: 20,
		},
		{
			kind: equipment.KindBag, name: "Sac de livraison isotherme",
			description: "Sac à dos isotherme 45 L aux couleurs Dira, pour les repas et les courses. En location ou à l'achat.",
			audiences:   []string{"food"},
			sale:        8000 * x, daily: 200 * x, weekly: 1000 * x, monthly: 3000 * x, deposit: 5000 * x, stock: 40,
		},
	}
}

func seedEquipment(ctx context.Context, logger *slog.Logger, svc *equipment.Service) error {
	total := 0
	for _, c := range country.Preloaded {
		info, ok := country.Lookup(c)
		if !ok {
			continue
		}
		cctx := country.WithCountry(ctx, c, country.SourceClaims)
		existing, err := svc.ListItems(cctx, false)
		if err != nil {
			return fmt.Errorf("seed: equipment %s: %w", c, err)
		}
		have := map[string]bool{}
		for _, it := range existing {
			have[it.Kind+"|"+it.Name] = true
		}
		for _, e := range equipmentCatalogue(info.Currency) {
			if have[e.kind+"|"+e.name] {
				continue
			}
			active := true
			if _, err := svc.CreateItem(cctx, equipment.ItemInput{
				Kind: e.kind, Name: e.name, Description: e.description, Audiences: e.audiences,
				SalePriceXOF: e.sale, RentalDailyXOF: e.daily, RentalWeeklyXOF: e.weekly, RentalMonthlyXOF: e.monthly,
				DepositXOF: e.deposit, Stock: e.stock, TrackStock: true, Active: &active,
			}); err != nil {
				return fmt.Errorf("seed: equipment %s %s: %w", c, e.name, err)
			}
			total++
		}
	}
	logger.Info("seed: equipment catalogue ready", "created", total, "countries", len(country.Preloaded))
	return nil
}
