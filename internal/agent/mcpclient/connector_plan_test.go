package mcpclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

func loadConnectorScenario(path string) (connectorScenario, error) {
	var scenario connectorScenario
	data, err := os.ReadFile(path)
	if err != nil {
		return scenario, connectorPlanFailure(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&scenario); err != nil {
		return scenario, connectorPlanFailure(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err != nil {
			return scenario, connectorPlanFailure(err)
		}
		return scenario, fmt.Errorf("connector scenario must contain exactly one JSON object")
	}
	return scenario, scenario.validate()
}

func (s connectorScenario) validate() error {
	steps := []struct {
		name string
		step connectorStep
	}{
		{"identity", s.Identity}, {"create", s.Create}, {"read", s.Read},
		{"update", s.Update}, {"reject", s.Reject}, {"cleanup", s.Cleanup},
	}
	for _, item := range steps {
		if strings.TrimSpace(item.step.Tool) == "" {
			return fmt.Errorf("%s.tool is required", item.name)
		}
		if !json.Valid(item.step.Arguments) || !strings.HasPrefix(strings.TrimSpace(string(item.step.Arguments)), "{") {
			return fmt.Errorf("%s.arguments must be a JSON object", item.name)
		}
	}
	if strings.TrimSpace(s.IDPath) == "" {
		return fmt.Errorf("id_path is required")
	}
	if strings.TrimSpace(s.OwnershipPath) == "" {
		return fmt.Errorf("ownership_path is required")
	}
	if !strings.Contains(s.OwnershipValue, "{{run}}") {
		return fmt.Errorf("ownership_value must contain {{run}}")
	}
	if strings.TrimSpace(s.MissingError) == "" {
		return fmt.Errorf("missing_error is required")
	}
	if len(s.Identity.Expect) == 0 {
		return fmt.Errorf("identity.expect requires explicit assertions")
	}
	if len(s.Read.Expect) == 0 {
		return fmt.Errorf("read.expect requires explicit assertions")
	}
	for _, item := range steps {
		if item.name == "read" || item.name == "update" || item.name == "cleanup" {
			if !strings.Contains(string(item.step.Arguments), "{{id}}") {
				return fmt.Errorf("%s.arguments must address the created {{id}}", item.name)
			}
		}
		if item.name == "create" || item.name == "update" {
			if !strings.Contains(string(item.step.Arguments), "{{marker}}") {
				return fmt.Errorf("%s.arguments must write {{marker}}", item.name)
			}
		}
	}
	readExpect, err := json.Marshal(s.Read.Expect)
	if err != nil {
		return fmt.Errorf("read.expect must contain valid JSON assertions")
	}
	if !strings.Contains(string(readExpect), "{{marker}}") {
		return fmt.Errorf("read.expect must assert {{marker}}")
	}
	return nil
}
