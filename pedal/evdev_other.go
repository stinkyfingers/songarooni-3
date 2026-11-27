//go:build !linux

package pedal

import (
	"context"
	"log"
	"time"
)

// Watch is a no-op outside Linux (e.g. Mac development builds) — evdev is a
// Linux-only kernel interface. The phone's HTTP control works regardless.
func Watch(ctx context.Context, nameSubstr string, c Controller) context.CancelFunc {
	log.Println("pedal: foot pedal input requires Linux (evdev); disabled on this platform")
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return

			default:
				log.Println("searching for mock pedal")
				time.Sleep(time.Second * 5)
			}
		}
	}()
	return cancel
}
