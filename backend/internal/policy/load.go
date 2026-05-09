package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// RulesFile is the on-disk shape for `policy.rules_path`. Rules under
// "" apply globally; keyed entries apply to a specific org_id.
type RulesFile struct {
	Global []Rule            `yaml:"global" json:"global"`
	Orgs   map[string][]Rule `yaml:"orgs" json:"orgs"`
}

// LoadFile reads rules from a YAML or JSON file and returns a configured
// Engine. Missing path returns an empty engine (Evaluate becomes a no-op
// that allows everything).
func LoadFile(path string) (*Engine, error) {
	e := NewEngine()
	if path == "" {
		return e, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return e, nil
		}
		return nil, fmt.Errorf("read policy file %s: %w", path, err)
	}
	var rf RulesFile
	if strings.HasSuffix(path, ".json") {
		if err := json.Unmarshal(data, &rf); err != nil {
			return nil, fmt.Errorf("parse policy json: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, &rf); err != nil {
			return nil, fmt.Errorf("parse policy yaml: %w", err)
		}
	}
	if len(rf.Global) > 0 {
		e.SetRules("", rf.Global)
	}
	for org, rules := range rf.Orgs {
		e.SetRules(org, rules)
	}
	return e, nil
}
