This is a tool for searching, downloading, and analyzing scientific medical papers.

## Architecture

- Frontend: React/Typescript
- API server: FastAPI (Python)
- Ingest pipeline: Celery
- Datastore (for papers): PostgreSQL
- Knowledge graph: Neo4j


## Commands

- scripts/dev.sh: launches the whole project


## Coding Conventions

- Python: always use type annotations
- React/Typescript: Never use plain javascript. Always use React or Typescript.