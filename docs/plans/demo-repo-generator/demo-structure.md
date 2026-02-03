# Demo Repository Structure

## Layout

```
git-sample/
├── .git/
├── .gitignore
├── .git-hop/
│   └── hop.json
├── .git-hop-deps/
│   └── .registry.json
├── docker-compose.yml
├── README.md
├── socket-server/
│   ├── go.mod
│   ├── go.sum
│   └── main.go
├── services/
│   ├── requirements.txt
│   ├── venv/
│   ├── worker/
│   └── scheduler/
├── api/
│   ├── composer.json
│   ├── composer.lock
│   ├── artisan
│   └── app/
├── frontend/
│   ├── package.json
│   ├── package-lock.json
│   └── src/
├── database/
│   ├── sqlite/
│   └── app.db
└── scripts/
    ├── env-prestart.sh
    └── env-poststart.sh
```

## Components

### Root

.git: Git metadata
.gitignore: Version control exclusions
.git-hop: Git-hop configuration (from setup script)
.git-hop-deps: Dependency sharing registry
docker-compose.yml: Service orchestration
README.md: Project documentation

### socket-server

Go service using gorilla/websocket.
Listens for connections.
Broadcasts to connected clients.

### services

Python microservices.
worker: Background job processor.
scheduler: Task scheduler.

### api

PHP Laravel framework application.
RESTful API endpoints.
Business logic layer.

### frontend

React SPA.
Communicates with API.
WebSocket client for real-time updates.

### database

SQLite database (for demo).
Migrations directory.
app.db: SQLite database file.

### scripts

Environment management scripts.
env-prestart.sh: Before Docker start.
env-poststart.sh: After Docker start.

## Related

- [Problem & Solution](../problem-solution.md)
- [Implementation Guide](../implementation-guide.md)
- [Workflow Flow](../diagrams/workflow-flow.mmd)
