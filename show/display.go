package show

import (
	"context"
	"log"
	"net/url"
	"time"

	"songarooni/data"
)

type Display interface {
	Show(id int)
}

// crossfadePadding is added to every slide's dwell time, matching the
// client's own crossfade transition length — without it the next slide
// would start loading/revealing before the current one finishes fading
// out.
const crossfadePadding = 1 * time.Second

// maxRevealWait caps how long displayData waits for the browser's
// POST /advance before giving up and moving on anyway.
const maxRevealWait = 10 * time.Second

// Show implements state.Display: it resolves id's slide deck and starts
// cycling it, pushing each slide's URL to every connected SSE client.
func (s *Slideshow) Show(id int) {
	deck, err := s.Resolve(id)
	if err != nil {
		log.Printf("display: error resolving slides for song id %d: %v", id, err)
		return
	}

	s.showMu.Lock()
	if s.cancelShow != nil {
		s.cancelShow()
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.cancelShow = cancel
	s.showMu.Unlock()

	go s.cycle(ctx, id, deck)
}

// cycle pushes deck's slides to stateChan one at a time, holding each on
// screen for its own dwell duration (see dwell) before moving to the next,
// until ctx is done.
func (s *Slideshow) cycle(ctx context.Context, id int, feature data.Feature) {
	i := 0
	advanceIndex := func() {
		i = (i + 1) % len(feature.FeatureItems)
	}

	type categoryIteration struct {
		data.Feature
		currentIndex int
	}

	// category frequency map defaults and overrides
	categoryFrequencyMap := make(map[string]categoryIteration)
	for _, feature := range s.data {
		if feature.Category == "" {
			continue
		}
		categoryFrequencyMap[feature.Category] = categoryIteration{Feature: feature, currentIndex: 0}
	}
	for _, categoryConfig := range feature.CategoryConfigs {
		category := categoryFrequencyMap[categoryConfig.Category]
		category.Feature.Frequency = categoryConfig.Frequency
		categoryFrequencyMap[categoryConfig.Category] = category
	}


	for {
		data := feature.FeatureItems[i]
		if !s.displayData(ctx, data) {
			return // ctx was canceled (or the path was unjoinable) — stop the goroutine, same as the old inline return did
		}
		for category, categoryIteration := range categoryFrequencyMap {
			if categoryIteration.Frequency == 0 {
				continue
			}

			if len(categoryIteration.FeatureItems) <= categoryIteration.currentIndex {
				continue
			}

			if !s.displayData(ctx, categoryIteration.FeatureItems[categoryIteration.currentIndex]) {
				log.Println("error displaying category", category)
				continue
			}

			if categoryIteration.Frequency%i == categoryIteration.Frequency-1 {
				categoryIteration.currentIndex += 1
			}
		}

		advanceIndex()
	}
}

// displayData pushes data featureItem's URL, waits for the browser to actually reveal
// it (POST /advance — see app.js's reveal() and its load-error path,
// which both call it), and only then dwells for data's own duration.
func (s *Slideshow) displayData(ctx context.Context, featureItem data.FeatureItem) bool {
	u, err := url.JoinPath("/images", featureItem.Path)
	if err != nil {
		log.Printf("display: error joining path for slide %s: %v", featureItem.Path, err)
		return false
	}

	select {
	case s.stateChan <- u:
	case <-ctx.Done():
		return false
	}

	// Discard any stale signal left over from a previous slide (e.g. a
	// late POST that arrived after we'd already given up waiting for it)
	// so it can't short-circuit this slide's own wait below.
	select {
	case <-s.advanceChan:
	default:
	}

	select {
	case <-s.advanceChan:
	case <-time.After(maxRevealWait):
		log.Printf("display: %s never acknowledged within %s; advancing anyway", u, maxRevealWait)
	case <-ctx.Done():
		return false
	}

	select {
	case <-time.After(featureItem.Length + crossfadePadding):
	case <-ctx.Done():
		return false
	}

	return true
}

// Resolve picks id's slide deck. An id not in songMap isn't treated as a
// hard error — it resolves to a single empty-path marker instead, so Show
// still pushes something and starts cycling rather than silently freezing
// the display on whatever was showing before. The browser's onerror
// handler on that one bad path still fires and advances past it (see
// app.js's showSlide), so this just degrades to looking like a single
// broken slide rather than the kiosk going unresponsive with no visible
// sign anything's wrong.
func (s *Slideshow) Resolve(id int) (data.Feature, error) {
	featureDir, ok := s.dataMap[id]
	if !ok {
		log.Println("slides: sodatang id", id, "not found in map")
		return data.Feature{FeatureItems: []data.FeatureItem{{Path: ""}}}, nil
	}
	err := featureDir.GetFeatureItems(s.parentDir, s.defaultLength)
	if err != nil {
		return data.Feature{}, err
	}
	return featureDir, nil
}

// resolveOptional is Resolve, but for a category that's allowed to be
// legitimately empty — Commercials, Logo, and (once wired in) Candid.
func (s *Slideshow) resolveOptional(id int) []data.FeatureItem {
	featureItem, err := s.Resolve(id)
	if err != nil {
		log.Printf("display: %v (treating as empty)", err)
		return nil
	}
	return featureItem.FeatureItems
}
