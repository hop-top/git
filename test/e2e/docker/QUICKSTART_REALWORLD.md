# Quick Start: Real-World Django Test

## TL;DR

```bash
# Run the test
go test -v -run TestDockerRealWorld_Django ./test/e2e/docker/
```

## What This Test Does

Clones a **real production Django application** from GitHub (Wagtail CMS demo) and:

1. Creates 2 isolated environments: `production` and `development`
2. Each gets unique ports and volumes
3. Runs both simultaneously without conflicts
4. Tests realistic development workflow

## Test Flow

```
1. Clone wagtail/bakerydemo from GitHub ✓
2. Initialize git-hop hub ✓
3. Add production branch → start services ✓
4. Add development branch → start services ✓
5. Verify isolation:
   - Production web: localhost:XXXX
   - Development web: localhost:YYYY
   - Different database ports
   - Different Redis ports
   - Separate volumes
6. Simulate dev workflow:
   - Modify code in development
   - Stop development
   - Production still running ✓
   - Restart development
   - Both running independently ✓
```

## Example Output

```
=== RUN   TestDockerRealWorld_Django
Cloning real Django application from https://github.com/wagtail/bakerydemo.git...
Successfully cloned repository
Initialized hub at /tmp/git-hop-e2e-XXX/hub
Adding production branch...
Found docker-compose configuration: /tmp/.../docker-compose.yml
Identified web service: web
==== Starting production environment ====
Waiting for production services to become healthy (this may take several minutes)...
Service web is healthy
Database service is healthy
==== Starting development environment ====
Service web is healthy
==== Verifying port isolation ====
SUCCESS: Port isolation verified - prod:8001, dev:8002
==== Testing HTTP endpoints ====
production endpoint http://localhost:8001 is accessible (status: 200)
development endpoint http://localhost:8002 is accessible (status: 200)
✓ Production accessible at http://localhost:8001
✓ Development accessible at http://localhost:8002
==== Verifying database isolation ====
SUCCESS: Database isolation verified - prod:5433, dev:5434
SUCCESS: Database volume isolation verified
==== Verifying Redis/broker isolation ====
SUCCESS: Broker/cache isolation verified - prod:6380, dev:6381
==== Simulating development workflow ====
Stopping development environment...
Verifying production remains accessible while development is stopped...
production endpoint http://localhost:8001 is accessible (status: 200)
✓ Production unaffected by development stop
Restarting development environment...
Verifying both environments are now accessible...
✓ Both environments running independently
==== Stopping all environments ====
✓✓✓ Real-world Django multi-branch workflow completed successfully ✓✓✓
--- PASS: TestDockerRealWorld_Django (XXX.XXs)
```

## Why This Test Matters

1. **Real Application**: Not a toy example - actual production Django app with Wagtail CMS
2. **Complex Stack**: Django + PostgreSQL + Redis + Celery (if available)
3. **Production Workflow**: Mirrors real maintainer scenarios
4. **Isolation Proof**: Demonstrates true multi-environment capability

## Troubleshooting

**Test skipped with "Docker is not available"**
```bash
# Check Docker is running
docker ps
docker compose version
```

**Test skipped with "Failed to clone Django app"**
- Check internet connection
- GitHub may be down (rare)
- Try again in a few minutes

**Timeout waiting for services**
- First run takes longer (downloads images, installs packages)
- Ensure you have good internet connection
- Check Docker has sufficient resources (4GB+ RAM recommended)

**Port conflict errors**
- Another test may still be running
- Clean up: `docker ps -a | grep git-hop`
- Stop all: `docker stop $(docker ps -q)`

## Time Expectations

| Phase | Duration | Notes |
|-------|----------|-------|
| First run | 5-10 min | Downloads images, installs packages |
| Subsequent runs | 3-5 min | Uses cached images |
| With fast internet | 2-3 min | Network is bottleneck |

## What Gets Tested

| Feature | Verification |
|---------|--------------|
| Port isolation | ✓ Different ports for web, db, Redis |
| Volume isolation | ✓ Separate database data |
| Concurrent operation | ✓ Both environments run together |
| Independence | ✓ Stop dev, prod keeps running |
| HTTP accessibility | ✓ Both respond on their ports |
| Environment variables | ✓ Different .env files |
| Service health | ✓ All services start properly |

## Advanced Usage

```bash
# Run with detailed output
go test -v -run TestDockerRealWorld_Django ./test/e2e/docker/ 2>&1 | tee django-test.log

# Run multiple times
for i in {1..3}; do
  echo "Run $i"
  go test -v -run TestDockerRealWorld_Django ./test/e2e/docker/
done

# Check what containers are created during test
docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
```

## Integration with CI/CD

```yaml
# Example GitHub Actions
- name: Run Real-World Django Test
  run: |
    go test -v -run TestDockerRealWorld_Django ./test/e2e/docker/
  timeout-minutes: 15
```

## Real Repository Used

- **Primary**: [wagtail/bakerydemo](https://github.com/wagtail/bakerydemo)
  - Production-ready Wagtail CMS demo
  - Full Docker Compose setup
  - PostgreSQL, Redis included
  - Actively maintained

- **Fallback**: [django/djangoproject.com](https://github.com/django/djangoproject.com)
  - Django's official website source
  - Production-grade Django application
  - Full stack with database

Both are excellent examples of production Django applications with Docker support.
