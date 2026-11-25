package state

import "songarooni/data"

type State struct{}

type Controller struct{} //TODO

func (c *Controller) GetState() *State {
	return &State{}
}

func (c *Controller) Data() []data.Data {
	return nil
}

func (c *Controller) Select(id int) error {
	return nil
}
func (c *Controller) SelectNext() error {
	return nil
}
func (c *Controller) SelectPrevious() error {
	return nil
}

func (c *Controller) Start() error {
	return nil

}
func (c *Controller) Stop() error {
	return nil
}
