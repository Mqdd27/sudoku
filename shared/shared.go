// Package shared embeds the config and test vectors that the web app and the server both use.
package shared

import (
	"embed"
	"encoding/json"
)

//go:embed config.json
var configJSON []byte

//go:embed fixtures/*.json
var Fixtures embed.FS

type SizeConfig struct {
	BoxRows int    `json:"boxRows"`
	BoxCols int    `json:"boxCols"`
	Name    string `json:"name"`
}

type Config struct {
	Sizes  map[string]SizeConfig       `json:"sizes"`
	Givens map[string]map[string][2]int `json:"givens"`
	PvP    struct {
		Hints          bool `json:"hints"`
		HintPenaltySec int  `json:"hintPenaltySec"`
	} `json:"pvp"`
	TimeAttack struct {
		Minutes        map[string][]int `json:"minutes"`        // allowed durations per size; others are hidden and refused
		DefaultMinutes map[string]int   `json:"defaultMinutes"` // preselected duration per size
		CooldownMs     int              `json:"cooldownMs"`     // a wrong digit locks that cell this long
		LateMs         int              `json:"lateMs"`         // moves this late after the end still count (latency)
	} `json:"timeAttack"`
}

// Load parses the embedded config. It panics on a malformed file: that is a build error, not a runtime one.
func Load() Config {
	var c Config
	if err := json.Unmarshal(configJSON, &c); err != nil {
		panic("shared/config.json: " + err.Error())
	}
	return c
}
