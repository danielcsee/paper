This project implements a tool for searching, downloading, and analyzing scientific medical papers.

## Architecture

Frontend: React/Typescript
API server: FastAPI (Python)
Ingest pipeline: Celery
Datastore (for papers): PostgreSQL
Knowledge graph: Neo4j

Authentication is purposefully excluded: this is a local-only project.

## Commands

scripts/dev.sh: launches the whole project


## Coding Conventions

Use the `developer` skill to write code. It applies to all code in this project and delegates to the `backend` and `frontend` skills as appropriate.


## Testing

When testing, respect PubTator's API and minimize the calls you make to them. Ask for approval before running any test that will fetch more than 5 articles at a time.