package state

type State struct {
	HasSelection bool `json:"hasSelection"`

	SelectedSongID int    `json:"selectedSongId"`
	SelectedTitle  string `json:"selectedTitle"`
	SelectedTempo  int    `json:"selectedTempo"`

	PlayingSongID int    `json:"playingSongId"`
	PlayingTitle  string `json:"playingTitle"`
	PlayingTempo  int    `json:"playingTempo"`

	Running    bool   `json:"running"` // PlayingSongID != 0
	LogoActive bool   `json:"logoActive"`
	Error      string `json:"error,omitempty"`
}
