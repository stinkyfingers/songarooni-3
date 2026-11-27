package state

import (
	"fmt"
	"sync"

	"songarooni/data"
	"songarooni/show"
)

type Controller struct {
	mu sync.Mutex

	features []data.Feature
	byID     map[int]*data.Feature

	selected *data.Feature
	playing  *data.Feature
	lastErr  string

	display show.Display
}

func NewController(features []data.Feature, display show.Display) *Controller {
	byID := make(map[int]*data.Feature)
	for _, feature := range features {
		byID[feature.ID] = &feature
	}
	return &Controller{
		mu:       sync.Mutex{},
		features: features,
		byID:     byID,
		display:  display,
	}
}

// Data returns the features catalog in file order.
func (c *Controller) Data() []data.Feature {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.features
}

// Select chooses feature by id id as the selected song.
func (c *Controller) Select(id int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	feature, ok := c.byID[id]
	if !ok {
		return fmt.Errorf("no song with id %d", id)
	}
	c.selected = feature
	c.lastErr = ""
	return nil
}

func (c *Controller) SelectNext() error {
	return c.SelectRelative(1)
}

func (c *Controller) SelectPrevious() error {
	return c.SelectRelative(-1)
}

func (c *Controller) SelectRelative(delta int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var order []data.Feature
	for _, s := range c.features {
		order = append(order, s)
	}
	if len(order) == 0 {
		return fmt.Errorf("no features in catalog")
	}

	idx := -1
	if c.selected != nil {
		for i, s := range order {
			if s.ID == c.selected.ID {
				idx = i
				break
			}
		}
	}

	var next int
	if idx == -1 {
		if delta > 0 {
			next = 0
		} else {
			next = len(order) - 1
		}
	} else {
		next = ((idx+delta)%len(order) + len(order)) % len(order)
	}

	c.selected = &order[next]
	c.lastErr = ""
	return nil
}

func (c *Controller) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.selected == nil {
		return fmt.Errorf("no song selected")
	}
	if c.playing != nil && c.playing.ID == c.selected.ID {
		return fmt.Errorf("already running")
	}

	if c.display == nil {
		return fmt.Errorf("controller has no display")
	}
	c.display.Show(c.selected.ID)
	c.playing = c.selected
	c.lastErr = ""
	return nil
}

// GetState returns a snapshot of the current state.
func (c *Controller) GetState() State {
	c.mu.Lock()
	defer c.mu.Unlock()

	var logoActive bool
	if c.playing != nil && c.playing.Category == "logo" {
		logoActive = true
	}

	s := State{LogoActive: logoActive, Error: c.lastErr}
	if c.selected != nil {
		s.HasSelection = true
		s.SelectedSongID = c.selected.ID
		s.SelectedTitle = c.selected.Title
	}
	if c.playing != nil {
		s.PlayingSongID = c.playing.ID
		s.PlayingTitle = c.playing.Title
		s.Running = true
	}
	return s
}
