package docker

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// PortMapping represents a single port mapping from a docker-compose service
type PortMapping struct {
	HostPort      string
	ContainerPort string
	Protocol      string // default "tcp"
	IP            string // e.g., "127.0.0.1" if IP-bound
	IsVariable    bool   // true if host port is ${...}
}

// composeFile is a minimal representation for parsing ports only
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Ports []any `yaml:"ports"`
}

var varPattern = regexp.MustCompile(`^\$\{.+\}$`)

// ParsePortMappings parses all services' port mappings from compose YAML content.
// Handles short syntax ("8000:8000", "127.0.0.1:8000:8000", "8000:8000/tcp")
// and long syntax ({target: 8000, published: 8000}).
func ParsePortMappings(composeContent []byte) (map[string][]PortMapping, error) {
	var cf composeFile
	if err := yaml.Unmarshal(composeContent, &cf); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	result := make(map[string][]PortMapping)
	for svcName, svc := range cf.Services {
		if len(svc.Ports) == 0 {
			continue
		}

		var mappings []PortMapping
		for _, p := range svc.Ports {
			switch v := p.(type) {
			case string:
				pm, err := parseShortSyntax(v)
				if err != nil {
					return nil, fmt.Errorf("service %s: %w", svcName, err)
				}
				mappings = append(mappings, pm)
			case map[string]any:
				pm, err := parseLongSyntax(v)
				if err != nil {
					return nil, fmt.Errorf("service %s: %w", svcName, err)
				}
				mappings = append(mappings, pm)
			default:
				return nil, fmt.Errorf("service %s: unsupported port format: %v", svcName, p)
			}
		}
		if len(mappings) > 0 {
			result[svcName] = mappings
		}
	}

	return result, nil
}

func parseShortSyntax(s string) (PortMapping, error) {
	pm := PortMapping{Protocol: "tcp"}

	// Extract protocol suffix
	if idx := strings.LastIndex(s, "/"); idx != -1 {
		pm.Protocol = s[idx+1:]
		s = s[:idx]
	}

	// If string starts with "${", find the closing "}" to capture the entire
	// variable token — including default-value syntax like ${VAR:-default}.
	// Splitting naively on ":" would break ${VAR:-default}:containerport.
	if strings.HasPrefix(s, "${") {
		closeIdx := strings.Index(s, "}")
		if closeIdx == -1 {
			return pm, fmt.Errorf("invalid port mapping (unclosed variable): %s", s)
		}
		varToken := s[:closeIdx+1]
		rest := s[closeIdx+1:]
		if rest == "" {
			// Container-only port expressed as a variable (unusual but valid)
			pm.ContainerPort = varToken
		} else if strings.HasPrefix(rest, ":") {
			pm.HostPort = varToken
			pm.ContainerPort = rest[1:]
		} else {
			return pm, fmt.Errorf("invalid port mapping: %s", s)
		}
		if varPattern.MatchString(pm.HostPort) {
			pm.IsVariable = true
		}
		return pm, nil
	}

	parts := strings.Split(s, ":")
	switch len(parts) {
	case 1:
		// Just container port, no host port
		pm.ContainerPort = parts[0]
	case 2:
		// host:container
		pm.HostPort = parts[0]
		pm.ContainerPort = parts[1]
	case 3:
		// ip:host:container
		pm.IP = parts[0]
		pm.HostPort = parts[1]
		pm.ContainerPort = parts[2]
	default:
		return pm, fmt.Errorf("invalid port mapping: %s", s)
	}

	if pm.HostPort != "" && varPattern.MatchString(pm.HostPort) {
		pm.IsVariable = true
	}

	return pm, nil
}

func parseLongSyntax(m map[string]any) (PortMapping, error) {
	pm := PortMapping{Protocol: "tcp"}

	if target, ok := m["target"]; ok {
		pm.ContainerPort = fmt.Sprintf("%v", target)
	}
	if published, ok := m["published"]; ok {
		pubStr := fmt.Sprintf("%v", published)
		pm.HostPort = pubStr
		if varPattern.MatchString(pubStr) {
			pm.IsVariable = true
		}
	}
	if proto, ok := m["protocol"]; ok {
		pm.Protocol = fmt.Sprintf("%v", proto)
	}
	if hostIP, ok := m["host_ip"]; ok {
		pm.IP = fmt.Sprintf("%v", hostIP)
	}

	return pm, nil
}

// NeedsOverride returns true if any port has a hardcoded (non-variable) host port.
func NeedsOverride(servicePorts map[string][]PortMapping) bool {
	for _, ports := range servicePorts {
		for _, p := range ports {
			if p.HostPort != "" && !p.IsVariable {
				return true
			}
		}
	}
	return false
}

// GenerateOverride generates override YAML content and a map of variable names to container ports.
// The override replaces hardcoded host ports with ${HOP_PORT_*} variables.
func GenerateOverride(servicePorts map[string][]PortMapping) ([]byte, map[string]string) {
	type overrideService struct {
		Ports []string `yaml:"ports"`
	}

	overrideServices := make(map[string]overrideService)
	varMap := make(map[string]string) // variable name -> container port

	// Sort service names for deterministic output
	svcNames := make([]string, 0, len(servicePorts))
	for name := range servicePorts {
		svcNames = append(svcNames, name)
	}
	sort.Strings(svcNames)

	for _, svcName := range svcNames {
		ports := servicePorts[svcName]
		var overridePorts []string

		for i, p := range ports {
			if p.HostPort == "" {
				// No host port, skip (container-only port)
				continue
			}

			varName := makeVarName(svcName, i, len(ports))

			var portStr string
			if p.IP != "" {
				portStr = fmt.Sprintf("%s:${%s}:%s", p.IP, varName, p.ContainerPort)
			} else {
				portStr = fmt.Sprintf("${%s}:%s", varName, p.ContainerPort)
			}
			if p.Protocol != "" && p.Protocol != "tcp" {
				portStr += "/" + p.Protocol
			}

			overridePorts = append(overridePorts, portStr)
			varMap[varName] = p.ContainerPort
		}

		if len(overridePorts) > 0 {
			overrideServices[svcName] = overrideService{Ports: overridePorts}
		}
	}

	// Build override YAML structure
	override := map[string]any{
		"services": overrideServices,
	}

	out, _ := yaml.Marshal(override)
	return out, varMap
}

// ComputePortVarNames returns the expanded list of port variable names for the port allocator.
// These replace raw service names when an override is in use.
func ComputePortVarNames(servicePorts map[string][]PortMapping) []string {
	var names []string

	// Sort service names for deterministic output
	svcNames := make([]string, 0, len(servicePorts))
	for name := range servicePorts {
		svcNames = append(svcNames, name)
	}
	sort.Strings(svcNames)

	for _, svcName := range svcNames {
		ports := servicePorts[svcName]
		for i, p := range ports {
			if p.HostPort == "" {
				continue
			}
			names = append(names, makeVarName(svcName, i, len(ports)))
		}
	}

	return names
}

// makeVarName creates the HOP_PORT_* variable name for a service port.
// Single port: HOP_PORT_<SERVICE>, multiple: HOP_PORT_<SERVICE>, HOP_PORT_<SERVICE>_2, etc.
func makeVarName(serviceName string, index, total int) string {
	normalized := normalizeServiceName(serviceName)
	if total <= 1 {
		return "HOP_PORT_" + normalized
	}
	if index == 0 {
		return "HOP_PORT_" + normalized
	}
	return fmt.Sprintf("HOP_PORT_%s_%d", normalized, index+1)
}

// normalizeServiceName converts a service name to a valid env var component.
// Uppercases, replaces hyphens and dots with underscores.
func normalizeServiceName(name string) string {
	name = strings.ToUpper(name)
	name = strings.ReplaceAll(name, "-", "_")
	name = strings.ReplaceAll(name, ".", "_")
	return name
}
