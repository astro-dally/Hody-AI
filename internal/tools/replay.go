package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
)

// Recording is one recorded tool output in a scenario folder.
type Recording struct {
	Tool string            `json:"tool"`
	Args map[string]string `json:"args"`
	File string            `json:"file"`
}

// LoadRecordings reads recorded/index.json from a scenario directory.
func LoadRecordings(dir string) ([]Recording, error) {
	b, err := os.ReadFile(filepath.Join(dir, "recorded", "index.json"))
	if err != nil {
		return nil, err
	}
	var recs []Recording
	return recs, json.Unmarshal(b, &recs)
}

// Replayed binds specs to recorded outputs instead of a live system. A
// request with no recording fails like a tool error would.
func Replayed(specs []Tool, dir string, recs []Recording) []Tool {
	runs := map[string]RunFunc{}
	for _, s := range specs {
		runs[s.Name] = func(_ context.Context, args map[string]string) (string, error) {
			for _, r := range recs {
				if r.Tool == s.Name && maps.Equal(r.Args, args) {
					b, err := os.ReadFile(filepath.Join(dir, "recorded", r.File))
					return string(b), err
				}
			}
			return "", fmt.Errorf("no recorded output for %s %v", s.Name, args)
		}
	}
	return WithRuns(specs, runs)
}
