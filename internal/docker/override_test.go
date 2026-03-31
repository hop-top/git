package docker

import (
	"strings"
	"testing"
)

func TestParsePortMappings_ShortSyntax(t *testing.T) {
	compose := []byte(`
services:
  web:
    image: nginx:alpine
    ports:
      - "8080:80"
  cache:
    image: redis:alpine
    ports:
      - "6379:6379"
`)

	result, err := ParsePortMappings(compose)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 services, got %d", len(result))
	}

	webPorts := result["web"]
	if len(webPorts) != 1 {
		t.Fatalf("expected 1 web port, got %d", len(webPorts))
	}
	if webPorts[0].HostPort != "8080" || webPorts[0].ContainerPort != "80" {
		t.Errorf("web port: got %s:%s, want 8080:80", webPorts[0].HostPort, webPorts[0].ContainerPort)
	}
	if webPorts[0].IsVariable {
		t.Error("web port should not be variable")
	}

	cachePorts := result["cache"]
	if len(cachePorts) != 1 {
		t.Fatalf("expected 1 cache port, got %d", len(cachePorts))
	}
	if cachePorts[0].HostPort != "6379" || cachePorts[0].ContainerPort != "6379" {
		t.Errorf("cache port: got %s:%s, want 6379:6379", cachePorts[0].HostPort, cachePorts[0].ContainerPort)
	}
}

func TestParsePortMappings_IPBound(t *testing.T) {
	compose := []byte(`
services:
  web:
    image: nginx:alpine
    ports:
      - "127.0.0.1:8080:80"
`)

	result, err := ParsePortMappings(compose)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	webPorts := result["web"]
	if len(webPorts) != 1 {
		t.Fatalf("expected 1 web port, got %d", len(webPorts))
	}
	if webPorts[0].IP != "127.0.0.1" {
		t.Errorf("expected IP 127.0.0.1, got %s", webPorts[0].IP)
	}
	if webPorts[0].HostPort != "8080" || webPorts[0].ContainerPort != "80" {
		t.Errorf("port: got %s:%s, want 8080:80", webPorts[0].HostPort, webPorts[0].ContainerPort)
	}
}

func TestParsePortMappings_Protocol(t *testing.T) {
	compose := []byte(`
services:
  dns:
    image: coredns
    ports:
      - "5353:53/udp"
`)

	result, err := ParsePortMappings(compose)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dnsPorts := result["dns"]
	if len(dnsPorts) != 1 {
		t.Fatalf("expected 1 dns port, got %d", len(dnsPorts))
	}
	if dnsPorts[0].Protocol != "udp" {
		t.Errorf("expected protocol udp, got %s", dnsPorts[0].Protocol)
	}
	if dnsPorts[0].HostPort != "5353" || dnsPorts[0].ContainerPort != "53" {
		t.Errorf("port: got %s:%s, want 5353:53", dnsPorts[0].HostPort, dnsPorts[0].ContainerPort)
	}
}

func TestParsePortMappings_VariableDetection(t *testing.T) {
	compose := []byte(`
services:
  web:
    image: nginx:alpine
    ports:
      - "${HOP_PORT_WEB}:80"
  cache:
    image: redis:alpine
    ports:
      - "${HOP_PORT_CACHE}:6379"
`)

	result, err := ParsePortMappings(compose)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result["web"][0].IsVariable {
		t.Error("web port should be detected as variable")
	}
	if !result["cache"][0].IsVariable {
		t.Error("cache port should be detected as variable")
	}
}

func TestParsePortMappings_LongSyntax(t *testing.T) {
	compose := []byte(`
services:
  web:
    image: nginx:alpine
    ports:
      - target: 80
        published: 8080
        protocol: tcp
`)

	result, err := ParsePortMappings(compose)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	webPorts := result["web"]
	if len(webPorts) != 1 {
		t.Fatalf("expected 1 web port, got %d", len(webPorts))
	}
	if webPorts[0].HostPort != "8080" || webPorts[0].ContainerPort != "80" {
		t.Errorf("port: got %s:%s, want 8080:80", webPorts[0].HostPort, webPorts[0].ContainerPort)
	}
	if webPorts[0].Protocol != "tcp" {
		t.Errorf("protocol: got %s, want tcp", webPorts[0].Protocol)
	}
}

func TestParsePortMappings_MultiplePorts(t *testing.T) {
	compose := []byte(`
services:
  app:
    image: myapp
    ports:
      - "8080:80"
      - "8443:443"
`)

	result, err := ParsePortMappings(compose)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	appPorts := result["app"]
	if len(appPorts) != 2 {
		t.Fatalf("expected 2 app ports, got %d", len(appPorts))
	}
}

func TestParsePortMappings_NoPorts(t *testing.T) {
	compose := []byte(`
services:
  worker:
    image: myworker
`)

	result, err := ParsePortMappings(compose)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 services with ports, got %d", len(result))
	}
}

func TestParsePortMappings_ContainerOnlyPort(t *testing.T) {
	compose := []byte(`
services:
  web:
    image: nginx:alpine
    ports:
      - "80"
`)

	result, err := ParsePortMappings(compose)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	webPorts := result["web"]
	if len(webPorts) != 1 {
		t.Fatalf("expected 1 web port, got %d", len(webPorts))
	}
	if webPorts[0].HostPort != "" {
		t.Errorf("expected empty host port, got %s", webPorts[0].HostPort)
	}
	if webPorts[0].ContainerPort != "80" {
		t.Errorf("expected container port 80, got %s", webPorts[0].ContainerPort)
	}
}

func TestNeedsOverride_HardcodedPorts(t *testing.T) {
	ports := map[string][]PortMapping{
		"web": {{HostPort: "8080", ContainerPort: "80"}},
	}
	if !NeedsOverride(ports) {
		t.Error("should need override for hardcoded ports")
	}
}

func TestNeedsOverride_VariablePorts(t *testing.T) {
	ports := map[string][]PortMapping{
		"web": {{HostPort: "${HOP_PORT_WEB}", ContainerPort: "80", IsVariable: true}},
	}
	if NeedsOverride(ports) {
		t.Error("should not need override for variable ports")
	}
}

func TestNeedsOverride_MixedPorts(t *testing.T) {
	ports := map[string][]PortMapping{
		"web":   {{HostPort: "${HOP_PORT_WEB}", ContainerPort: "80", IsVariable: true}},
		"cache": {{HostPort: "6379", ContainerPort: "6379"}},
	}
	if !NeedsOverride(ports) {
		t.Error("should need override when any port is hardcoded")
	}
}

func TestNeedsOverride_NoHostPorts(t *testing.T) {
	ports := map[string][]PortMapping{
		"web": {{ContainerPort: "80"}},
	}
	if NeedsOverride(ports) {
		t.Error("should not need override when no host ports")
	}
}

func TestGenerateOverride_SinglePort(t *testing.T) {
	ports := map[string][]PortMapping{
		"web": {{HostPort: "8080", ContainerPort: "80", Protocol: "tcp"}},
	}

	yamlOut, varMap := GenerateOverride(ports)

	yamlStr := string(yamlOut)
	if !strings.Contains(yamlStr, "${HOP_PORT_WEB}:80") {
		t.Errorf("expected override to contain ${HOP_PORT_WEB}:80, got:\n%s", yamlStr)
	}

	if varMap["HOP_PORT_WEB"] != "80" {
		t.Errorf("expected varMap HOP_PORT_WEB=80, got %s", varMap["HOP_PORT_WEB"])
	}
}

func TestGenerateOverride_MultiplePortsPerService(t *testing.T) {
	ports := map[string][]PortMapping{
		"app": {
			{HostPort: "8080", ContainerPort: "80", Protocol: "tcp"},
			{HostPort: "8443", ContainerPort: "443", Protocol: "tcp"},
		},
	}

	_, varMap := GenerateOverride(ports)

	if varMap["HOP_PORT_APP"] != "80" {
		t.Errorf("expected HOP_PORT_APP=80, got %s", varMap["HOP_PORT_APP"])
	}
	if varMap["HOP_PORT_APP_2"] != "443" {
		t.Errorf("expected HOP_PORT_APP_2=443, got %s", varMap["HOP_PORT_APP_2"])
	}
}

func TestGenerateOverride_IPBound(t *testing.T) {
	ports := map[string][]PortMapping{
		"web": {{HostPort: "8080", ContainerPort: "80", Protocol: "tcp", IP: "127.0.0.1"}},
	}

	yamlOut, _ := GenerateOverride(ports)

	yamlStr := string(yamlOut)
	if !strings.Contains(yamlStr, "127.0.0.1:${HOP_PORT_WEB}:80") {
		t.Errorf("expected IP-bound override, got:\n%s", yamlStr)
	}
}

func TestGenerateOverride_UDPProtocol(t *testing.T) {
	ports := map[string][]PortMapping{
		"dns": {{HostPort: "5353", ContainerPort: "53", Protocol: "udp"}},
	}

	yamlOut, _ := GenerateOverride(ports)

	yamlStr := string(yamlOut)
	if !strings.Contains(yamlStr, "${HOP_PORT_DNS}:53/udp") {
		t.Errorf("expected UDP protocol in override, got:\n%s", yamlStr)
	}
}

func TestGenerateOverride_NamingNormalization(t *testing.T) {
	ports := map[string][]PortMapping{
		"my-service.name": {{HostPort: "8080", ContainerPort: "80", Protocol: "tcp"}},
	}

	_, varMap := GenerateOverride(ports)

	if _, ok := varMap["HOP_PORT_MY_SERVICE_NAME"]; !ok {
		t.Errorf("expected normalized name HOP_PORT_MY_SERVICE_NAME, got keys: %v", varMap)
	}
}

func TestGenerateOverride_SkipsContainerOnlyPorts(t *testing.T) {
	ports := map[string][]PortMapping{
		"web": {
			{HostPort: "8080", ContainerPort: "80", Protocol: "tcp"},
			{ContainerPort: "9090", Protocol: "tcp"},
		},
	}

	_, varMap := GenerateOverride(ports)

	if len(varMap) != 1 {
		t.Errorf("expected 1 var, got %d: %v", len(varMap), varMap)
	}
}

func TestComputePortVarNames_Basic(t *testing.T) {
	ports := map[string][]PortMapping{
		"web":   {{HostPort: "8080", ContainerPort: "80"}},
		"cache": {{HostPort: "6379", ContainerPort: "6379"}},
	}

	names := ComputePortVarNames(ports)

	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}

	// Should be sorted alphabetically by service name
	if names[0] != "HOP_PORT_CACHE" {
		t.Errorf("expected first name HOP_PORT_CACHE, got %s", names[0])
	}
	if names[1] != "HOP_PORT_WEB" {
		t.Errorf("expected second name HOP_PORT_WEB, got %s", names[1])
	}
}

func TestParsePortMappings_DefaultValueSyntax(t *testing.T) {
	// ${VAR:-default}:container must be parsed without creating nested interpolation
	compose := []byte(`
services:
  db:
    image: postgres:14
    ports:
      - "${POSTGRES_PORT:-5432}:5432"
`)

	result, err := ParsePortMappings(compose)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dbPorts := result["db"]
	if len(dbPorts) != 1 {
		t.Fatalf("expected 1 db port, got %d", len(dbPorts))
	}
	if dbPorts[0].HostPort != "${POSTGRES_PORT:-5432}" {
		t.Errorf("host port: got %q, want ${POSTGRES_PORT:-5432}", dbPorts[0].HostPort)
	}
	if dbPorts[0].ContainerPort != "5432" {
		t.Errorf("container port: got %q, want 5432", dbPorts[0].ContainerPort)
	}
	// Port with default value is already a variable — no override needed
	if !dbPorts[0].IsVariable {
		t.Error("${POSTGRES_PORT:-5432} should be detected as variable (IsVariable=true)")
	}
}

func TestNeedsOverride_DefaultValueVariable(t *testing.T) {
	ports := map[string][]PortMapping{
		"db": {{HostPort: "${POSTGRES_PORT:-5432}", ContainerPort: "5432", IsVariable: true}},
	}
	if NeedsOverride(ports) {
		t.Error("should not need override for ${VAR:-default} style variable")
	}
}

func TestComputePortVarNames_MultiPort(t *testing.T) {
	ports := map[string][]PortMapping{
		"app": {
			{HostPort: "8080", ContainerPort: "80"},
			{HostPort: "8443", ContainerPort: "443"},
		},
	}

	names := ComputePortVarNames(ports)

	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	if names[0] != "HOP_PORT_APP" {
		t.Errorf("expected HOP_PORT_APP, got %s", names[0])
	}
	if names[1] != "HOP_PORT_APP_2" {
		t.Errorf("expected HOP_PORT_APP_2, got %s", names[1])
	}
}
