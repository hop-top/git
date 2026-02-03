package docker

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ServiceConfig represents a simplified view of docker-compose services
type ServiceConfig struct {
	Services map[string]interface{} `yaml:"services"`
	Volumes  map[string]interface{} `yaml:"volumes"`
}

// GetConfig returns the canonical docker compose config
func (d *Docker) GetConfig(dir string) (*ServiceConfig, error) {
	out, err := d.Runner.RunInDir(dir, "docker", "compose", "config")
	if err != nil {
		return d.getConfigRaw(dir)
	}

	var config ServiceConfig
	if err := yaml.Unmarshal([]byte(out), &config); err != nil {
		return nil, fmt.Errorf("failed to parse docker compose config: %w", err)
	}

	return &config, nil
}

func (d *Docker) getConfigRaw(dir string) (*ServiceConfig, error) {
	candidates := []string{"docker-compose.yml", "docker-compose.yaml"}
	var content []byte
	var err error

	for _, f := range candidates {
		p := filepath.Join(dir, f)
		content, err = os.ReadFile(p)
		if err == nil {
			break
		}
	}

	if len(content) == 0 {
		return nil, fmt.Errorf("failed to read docker-compose file: %v", err)
	}

	var config ServiceConfig
	if err := yaml.Unmarshal(content, &config); err != nil {
		return nil, fmt.Errorf("failed to parse raw docker-compose file: %w", err)
	}

	return &config, nil
}

// GetServiceNames returns a list of service names from the config
func (d *Docker) GetServiceNames(dir string) ([]string, error) {
	config, err := d.GetConfig(dir)
	if err != nil {
		return nil, err
	}

	var names []string
	for name := range config.Services {
		names = append(names, name)
	}
	return names, nil
}

// GetVolumeNames returns a list of volume names from the config
func (d *Docker) GetVolumeNames(dir string) ([]string, error) {
	config, err := d.GetConfig(dir)
	if err != nil {
		return nil, err
	}

	var names []string
	for name := range config.Volumes {
		names = append(names, name)
	}
	return names, nil
}

// HasDockerEnv checks if directory has a valid docker-compose config
func (d *Docker) HasDockerEnv(dir string) bool {
	_, err := d.GetConfig(dir)
	return err == nil
}
