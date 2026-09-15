package country

import (
	_ "embed"
	"encoding/json"
	"sync"
)

// borders.json : les frontières des pays du catalogue, simplifiées à ~100 m
// (Douglas-Peucker, tolérance 0,001°) depuis geoBoundaries ADM0 (ODbL,
// OpenStreetMap). Embarquées pour que CHAQUE service sache situer un point
// sans base ni réseau : le socle pour répondre à une application, une
// verticale pour vérifier qu'une course est dans son pays.
//
// ⚠️ ~100 m de tolérance, c'est la précision d'une FRONTIÈRE, pas d'une
// adresse. Elle suffit à dire dans quel pays une personne se trouve — sauf
// à cent mètres d'un poste-frontière, où l'application garde le pays du
// compte plutôt que d'en changer.
//
//go:embed borders.json
var bordersJSON []byte

type border struct {
	Code     string `json:"code"`
	Geometry struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	} `json:"geometry"`
}

type ring [][2]float64

type polygon struct {
	outer ring
	holes []ring
	// bbox de l'anneau extérieur : écarte 99 % des tests sans parcourir
	// l'anneau.
	minX, minY, maxX, maxY float64
}

type shape struct {
	code  string
	polys []polygon
}

var (
	loadOnce sync.Once
	shapes   []shape
)

func load() {
	var raw []border
	if err := json.Unmarshal(bordersJSON, &raw); err != nil {
		panic("country: embedded borders are not valid JSON: " + err.Error())
	}
	for _, b := range raw {
		s := shape{code: b.Code}
		var polys [][]ring
		switch b.Geometry.Type {
		case "Polygon":
			var p []ring
			if err := json.Unmarshal(b.Geometry.Coordinates, &p); err != nil {
				panic("country: border of " + b.Code + ": " + err.Error())
			}
			polys = [][]ring{p}
		case "MultiPolygon":
			if err := json.Unmarshal(b.Geometry.Coordinates, &polys); err != nil {
				panic("country: border of " + b.Code + ": " + err.Error())
			}
		}
		for _, rings := range polys {
			if len(rings) == 0 || len(rings[0]) < 4 {
				continue
			}
			p := polygon{outer: rings[0], holes: rings[1:]}
			p.minX, p.minY, p.maxX, p.maxY = rings[0][0][0], rings[0][0][1], rings[0][0][0], rings[0][0][1]
			for _, pt := range rings[0] {
				p.minX, p.maxX = min(p.minX, pt[0]), max(p.maxX, pt[0])
				p.minY, p.maxY = min(p.minY, pt[1]), max(p.maxY, pt[1])
			}
			s.polys = append(s.polys, p)
		}
		shapes = append(shapes, s)
	}
}

// Locate rend le pays du catalogue dans lequel tombe un point, ou "" s'il
// n'en touche aucun — en mer, ou dans un pays que Dira ne connaît pas.
//
// Lancer de rayon (pair-impair) sur des frontières simplifiées ; voir
// `borders.json`. Pas de « pays le plus proche » : un point en mer devant
// Lomé est en mer, et c'est à l'appelant de décider quoi en faire — le pays
// du compte, l'adresse IP, le défaut.
func Locate(lng, lat float64) (string, bool) {
	loadOnce.Do(load)
	for _, s := range shapes {
		for _, p := range s.polys {
			if lng < p.minX || lng > p.maxX || lat < p.minY || lat > p.maxY {
				continue
			}
			if !inRing(p.outer, lng, lat) {
				continue
			}
			inHole := false
			for _, h := range p.holes {
				if inRing(h, lng, lat) {
					inHole = true
					break
				}
			}
			if !inHole {
				return s.code, true
			}
		}
	}
	return "", false
}

func inRing(r ring, x, y float64) bool {
	inside := false
	for i, j := 0, len(r)-1; i < len(r); j, i = i, i+1 {
		xi, yi := r[i][0], r[i][1]
		xj, yj := r[j][0], r[j][1]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			inside = !inside
		}
	}
	return inside
}
