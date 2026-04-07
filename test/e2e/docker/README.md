# Docker Integration Tests

Comprehensive integration tests for git-hop's Docker environment management and multi-service isolation.

## Running

These tests are gated behind the `dockere2e` build tag and require a real
local Docker daemon. They are **excluded from default `go test ./...`** and
from PR CI runs.

```sh
# Run all docker e2e tests (requires docker + docker compose locally)
go test -tags dockere2e ./test/e2e/docker/...

# Run a single test
go test -tags dockere2e -run TestDockerIsolation_PortIsolation ./test/e2e/docker/...
```

Default `go test ./...` skips this directory entirely — it does not even
compile the files. The build-tag gate is the right approach here because
the tests assert on real OS-allocated state (port numbers, container IDs,
volume directory contents, real HTTP responses) which cannot be
deterministically replayed via xrr cassettes. See
`hop-top/git` track `git-test-determinism` T-0015 for the analysis.

## Overview

These tests verify actual Docker container behavior, not just file generation. They ensure:
- Containers actually start and become healthy
- Services are accessible on allocated ports
- Isolation between branches (ports, volumes, networks)
- Complex multi-service scenarios work correctly
- Edge cases are handled gracefully

## Current vs. Planned Coverage

### Current (Limited)
- ✅ `.env` file generation
- ✅ Command execution (start/stop)
- ❌ No verification containers actually run
- ❌ No service accessibility checks
- ❌ No data isolation verification

### Planned (Comprehensive)
- ✅ Actual container startup/health verification
- ✅ HTTP endpoint accessibility testing
- ✅ Port isolation between branches
- ✅ Volume data isolation
- ✅ Network isolation
- ✅ Multi-service dependencies (5+ services)
- ✅ Database connectivity
- ✅ Edge cases (conflicts, errors, concurrent)

## Test Architecture

### Test Files

```
test/e2e/docker/
├── README.md                        # This file
├── docker_integration_test.go       # Basic startup, health, multi-service
├── docker_isolation_test.go         # Port/volume/network isolation
├── docker_edge_cases_test.go        # Error handling, conflicts
├── docker_concurrent_test.go        # Parallel branch execution
├── docker_helpers.go                # Shared test utilities
├── cleanup.sh                       # Manual cleanup script
└── fixtures/
    ├── docker-compose-simple.yml    # 2 services (nginx, redis)
    ├── docker-compose-complex.yml   # 5+ services with dependencies
    ├── docker-compose-network.yml   # Custom network isolation
    ├── docker-compose-web-db.yml    # Web app + database
    ├── docker-compose-invalid.yml   # Corrupted for error testing
    └── test-data/                   # Sample data for volumes
```

### Docker Fixtures

#### Simple Fixture (2 services)
Basic web + cache setup for fundamental tests:
- nginx:alpine on `${HOP_PORT_WEB}:80`
- redis:alpine on `${HOP_PORT_CACHE}:6379`
- Health checks for both services

#### Complex Fixture (5+ services)
Full stack with dependencies:
- **web** (nginx) → depends on app
- **app** (http-echo) → depends on db + cache
- **db** (postgres) with persistent storage
- **cache** (redis) with persistent storage
- **worker** (busybox) background process

Tests service dependency ordering and health propagation.

#### Network Isolation Fixture
Custom networks testing:
- **frontend** network (web + backend)
- **backend** network (backend + database)
- Verifies network segmentation

### Helper Functions

**docker_helpers.go** provides:

**Docker Availability:**
- `IsDockerAvailable(t)` - Check Docker daemon
- `IsDockerComposeAvailable(t)` - Check compose CLI
- `SkipIfDockerNotAvailable(t)` - Auto-skip tests

**Container Management:**
- `GetRunningContainers(t, projectName)` - List containers
- `WaitForServiceHealthy(t, dir, service, timeout)` - Wait for health
- `CleanupContainers(t, dir)` - Force cleanup

**Verification:**
- `CheckHTTPEndpoint(t, url, expectedStatus)` - HTTP accessibility
- `VerifyPortIsolation(t, port1, port2)` - Different ports
- `VerifyVolumeIsolation(t, vol1, vol2)` - Different volumes

**Data Operations:**
- `WriteDataToVolume(t, volumePath, filename, content)` - Test data
- `ReadDataFromVolume(t, volumePath, filename)` - Verify data
- `ExecInContainer(t, containerName, command...)` - Run commands

## Test Scenarios

### 1. Basic Container Startup (docker_integration_test.go)

**TestDockerIntegration_BasicStartup**
```
Setup → Start services → Verify health → Check accessibility → Stop → Verify stopped
```

Verifies:
- Containers start successfully
- Services become healthy within timeout
- HTTP endpoints are accessible
- Services stop cleanly

### 2. Port Isolation (docker_isolation_test.go)

**TestDockerIsolation_PortIsolation**
```
Create branch-a → Create branch-b → Start both → Verify different ports → Both accessible
```

Verifies:
- Each branch gets unique ports
- No port conflicts
- Both environments run concurrently
- Services accessible on their allocated ports

### 3. Volume Data Isolation (docker_isolation_test.go)

**TestDockerIsolation_VolumeDataIsolation**
```
Setup branches → Write different data → Start services → Verify separate content → Restart → Data persists
```

Verifies:
- Each branch has separate volumes
- Data written to branch-a ≠ data in branch-b
- Volume data persists across restarts
- No data leakage between branches

### 4. Complex Multi-Service (docker_integration_test.go)

**TestDockerIntegration_ComplexServices**
```
Setup 5-service stack → Start all → Verify dependencies → Test connectivity → Verify all healthy
```

Verifies:
- Service dependency ordering (db/cache before app before web)
- All services start and become healthy
- Database is accessible and queryable
- Redis responds to commands
- Worker process runs

### 5. Edge Cases (docker_edge_cases_test.go)

**TestDockerEdgeCases_AlreadyRunning**
- Start services
- Start again (should be idempotent)
- Verify no errors, still running

**TestDockerEdgeCases_AlreadyStopped**
- Stop without starting
- Verify graceful handling

**TestDockerEdgeCases_CorruptedComposeFile**
- Use invalid docker-compose.yml
- Verify error message
- Verify graceful failure

**TestDockerEdgeCases_PortConflict**
- Verify port allocator prevents conflicts
- Different branches get different ports

### 6. Concurrent Services (docker_concurrent_test.go)

**TestDockerConcurrent_ParallelStartup**
```
Create 3 branches → Start all concurrently → Verify all healthy → No conflicts
```

Verifies:
- Parallel startup works
- No resource conflicts
- Proper port/volume isolation under load
- All services reachable

## Running Tests

### Prerequisites

```bash
# Verify Docker is running
docker version
docker compose version

# Build git-hop
make build
```

### Execute Tests

```bash
# Run all Docker integration tests
go test -v -timeout 10m ./test/e2e/docker/...

# Run specific test
go test -v -run TestDockerIntegration_BasicStartup ./test/e2e/docker/

# Run with race detector
go test -v -race -timeout 10m ./test/e2e/docker/...

# Skip if Docker not available (automatic)
go test -v ./test/e2e/docker/...  # Shows skip message if Docker unavailable
```

### Debugging

```bash
# Check running containers during test
docker compose ps

# View logs
docker compose logs

# Inspect specific container
docker inspect <container-name>

# Manual cleanup after test failure
./test/e2e/docker/cleanup.sh
```

### Test Output

Successful test output:
```
=== RUN   TestDockerIntegration_BasicStartup
    docker_integration_test.go:45: Service web is healthy
    docker_integration_test.go:49: Service cache is healthy
    docker_integration_test.go:62: Web service accessible at http://localhost:10234
--- PASS: TestDockerIntegration_BasicStartup (15.32s)
```

Failed test with debugging info:
```
=== RUN   TestDockerIntegration_BasicStartup
    docker_integration_test.go:45: Service web not healthy: timeout waiting for health
    docker_integration_test.go:70: Container logs:
        2024/02/04 nginx: [emerg] bind() to 0.0.0.0:80 failed (98: Address already in use)
--- FAIL: TestDockerIntegration_BasicStartup (30.00s)
```

## CI/CD Integration

### GitHub Actions

Add to `.github/workflows/ci.yml`:

```yaml
docker-integration-tests:
  name: Docker Integration Tests
  runs-on: ubuntu-latest

  steps:
    - name: Checkout
      uses: actions/checkout@v4

    - name: Set up Go
      uses: actions/setup-go@v5
      with:
        go-version: "1.21"

    - name: Verify Docker
      run: |
        docker version
        docker compose version

    - name: Run Docker Integration Tests
      run: go test -v -timeout 10m ./test/e2e/docker/...

    - name: Cleanup Docker Resources
      if: always()
      run: |
        docker compose down -v --remove-orphans || true
        docker system prune -af --volumes || true
```

### Build Tags (Optional)

Use build tags for selective execution:

```go
// +build docker

package docker
```

Then run with:
```bash
go test -v -tags docker ./test/e2e/docker/...
```

## Cleanup Strategy

### Automatic Cleanup

Tests use `t.Cleanup()` to ensure cleanup runs even on failure:

```go
t.Cleanup(func() {
    CleanupContainers(t, branchPath)
})
```

### Manual Cleanup

If tests fail and leave containers running:

```bash
# Stop all test containers
docker ps -a --filter "name=git-hop-test" -q | xargs docker stop
docker ps -a --filter "name=git-hop-test" -q | xargs docker rm

# Remove test volumes
docker volume ls --filter "name=hop_" -q | xargs docker volume rm

# Prune networks
docker network prune -f

# Or use cleanup script
./test/e2e/docker/cleanup.sh
```

## Expected Test Duration

- **BasicStartup**: ~15-20s
- **PortIsolation**: ~30-40s (2 branches)
- **VolumeDataIsolation**: ~35-45s (write/read operations)
- **ComplexServices**: ~45-60s (5+ services)
- **EdgeCases**: ~10-15s per test
- **ConcurrentStartup**: ~60-90s (3 parallel branches)

**Total suite**: ~5-7 minutes with Docker caching, ~10-15 minutes cold start

## Test Quality Levels

### Current: 🟡 Smoke Tests
- Commands execute without error
- Files are created
- No actual Docker verification

### Target: 🟢 Integration Tests
- Containers actually start
- Services are accessible
- Isolation is verified
- Data persistence confirmed

## Troubleshooting

### Tests Skip Automatically

**Issue**: `Docker is not available - skipping integration test`

**Solution**: Ensure Docker Desktop/daemon is running:
```bash
docker version  # Should show client and server versions
```

### Port Already in Use

**Issue**: `bind: address already in use`

**Solution**: Clean up orphaned containers:
```bash
./test/e2e/docker/cleanup.sh
```

### Tests Timeout

**Issue**: Services don't become healthy within timeout

**Solution**:
1. Check Docker logs: `docker compose logs`
2. Increase timeout in test (currently 30-60s)
3. Pull images beforehand: `docker compose pull`

### Permission Denied

**Issue**: `permission denied while trying to connect to Docker daemon`

**Solution**: Add user to docker group (Linux):
```bash
sudo usermod -aG docker $USER
newgrp docker
```

## Future Enhancements

- [ ] Test with different Docker Compose versions
- [ ] Test with Podman as Docker alternative
- [ ] Add performance benchmarks
- [ ] Test volume backup/restore
- [ ] Test custom networks with bridge/host modes
- [ ] Add chaos testing (kill containers mid-test)
- [ ] Test resource limits (CPU/memory constraints)
- [ ] Add metrics collection (container stats)

## Contributing

When adding new Docker tests:

1. **Follow naming convention**: `TestDocker<Category>_<Scenario>`
2. **Always use cleanup**: `t.Cleanup(func() { CleanupContainers(t, dir) })`
3. **Check Docker availability**: `SkipIfDockerNotAvailable(t)`
4. **Add health checks**: Wait for services before testing
5. **Document fixtures**: Add comments to docker-compose files
6. **Test isolation**: Verify no interference between tests
7. **Log useful info**: Use `t.Logf()` for debugging

## References

- [Docker Compose Documentation](https://docs.docker.com/compose/)
- [Testing Best Practices](https://github.com/golang/go/wiki/TestComments)
- [git-hop Docker Implementation](../../internal/docker/)
- [Existing E2E Tests](../e2e_test.go)
