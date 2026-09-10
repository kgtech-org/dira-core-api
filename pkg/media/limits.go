package media

import (
	"fmt"
	"io"
	"time"
)

// MaxShortDuration is the ceiling for a feed video.
//
// ⚠️ Décision produit : le feed est un format court. La valeur est ici et non
// dans la configuration parce qu'elle définit le format lui-même — la changer
// change le produit, pas un réglage d'exploitation.
const MaxShortDuration = 60 * time.Second

// ErrTooLong reports a video that exceeds MaxShortDuration.
type ErrTooLong struct{ Got time.Duration }

func (e ErrTooLong) Error() string {
	return fmt.Sprintf("media: video lasts %.0fs, the limit is %.0fs",
		e.Got.Seconds(), MaxShortDuration.Seconds())
}

// CheckShort validates that the stream is a playable short. Le lecteur est
// rembobiné : l'appelant enchaîne sur le stockage.
func CheckShort(r io.ReadSeeker, contentType string) (time.Duration, error) {
	d, err := Duration(r, contentType)
	if err != nil {
		return 0, err
	}
	if d > MaxShortDuration {
		return d, ErrTooLong{Got: d}
	}
	return d, nil
}
