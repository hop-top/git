# Real-World Docker Integration Tests

This directory contains integration tests using actual production Django applications from GitHub to verify git-hop's Docker isolation capabilities.

## TestDockerRealWorld_Django

**File:** `docker_realworld_django_test.go`

**Purpose:** Tests git-hop with a real production Django application that includes:
- Django web framework
- PostgreSQL database
- Redis (for caching/message broker)
- Celery workers (if available)
- Complex multi-service Docker Compose setup

**Target Repository:**
- Primary: [wagtail/bakerydemo](https://github.com/wagtail/bakerydemo) - Production-ready Wagtail CMS demo
- Fallback: [django/djangoproject.com](https://github.com/django/djangoproject.com) - Django's official website

**What it Tests:**

1. **Real Application Cloning**
   - Clones actual OSS Django application with Docker Compose
   - Handles real-world complexity (migrations, collectstatic, etc.)

2. **Multi-Branch Environment Management**
   - Creates production (`main`) branch
   - Creates development branch
   - Manages both simultaneously

3. **Port Isolation**
   - Verifies different ports for web, database, and Redis
   - Tests concurrent access to both environments

4. **Volume Isolation**
   - Database volumes isolated between branches
   - Cache/broker volumes isolated

5. **Development Workflow**
   - Simulates code changes in development
   - Stops development while production runs
   - Restarts development without affecting production

6. **Service Health Monitoring**
   - Waits for Django application to start (including pip install)
   - Verifies database health
   - Checks Redis/broker availability

**Running the Test:**

```bash
# Run only Django real-world test
go test -v -run TestDockerRealWorld_Django ./test/e2e/docker/

# Run with verbose Docker output
go test -v -run TestDockerRealWorld_Django ./test/e2e/docker/ 2>&1 | tee test.log

# Run all real-world tests
go test -v -run TestDockerRealWorld ./test/e2e/docker/
```

**Prerequisites:**

- Docker Engine running
- Docker Compose installed
- Internet connection (to clone GitHub repositories)
- ~5-10 minutes for test completion (includes pip install, migrations)

**Test Phases:**

1. Clone production Django app from GitHub
2. Initialize git-hop hub
3. Add production branch
4. Create and add development branch
5. Start both environments
6. Verify port/volume isolation
7. Test HTTP endpoints
8. Simulate development workflow
9. Verify production stability
10. Stop all environments

**Expected Duration:** 5-10 minutes

The test may take longer on first run due to:
- Docker image pulls
- Python package installation (Django, psycopg2, Celery, etc.)
- Database migrations
- Static file collection

**Skip Conditions:**

The test will be skipped if:
- Docker is not available
- Docker Compose is not installed
- Network connection fails
- Repository doesn't have docker-compose.yml
- GitHub is unreachable

**Success Criteria:**

✓ Both environments start successfully
✓ Different ports allocated to each branch
✓ Database and cache isolation verified
✓ Production remains stable during development changes
✓ Both environments accessible via HTTP
✓ Clean shutdown of all services

## Notes

- These tests use actual production applications, so they depend on external GitHub repositories
- Test execution time varies based on network speed and system resources
- Docker images are cached after first run, speeding up subsequent tests
- All containers are cleaned up after test completion (even on failure)
