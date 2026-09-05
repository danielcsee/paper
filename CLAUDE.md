This project implements a tool for searching, downloading, and analyzing scientific medical papers.

## Architecture

Frontend: React/Typescript
API server: FastAPI (Python)
Ingest pipeline: Celery
Datastore (for papers): PostgreSQL
Knowledge graph: Neo4j

Authentication is purposefully excluded: this is a local-only project.

# Communication Style

In all communication, format your responses to this style as much as possible: A 2-3 sentence opinion, followed by a list of titles with bullet points explaining your reasoning, concluding with a 2-4 sentence summary. Example:

"
Reason/Step 1: Create Client
- Init a client with POST /service route. Validate data against ServiceCall format.
- Check db connectivity: db must be alive to serve call. Otherwise, return error.

Reason/Step 2: Add Trigger
- On persistence, db must run a new trigger.

We'll implement the client and new route first. Then validate POST data against ServiceCall. Finally, we'll add a trigger and invoke the new trigger on persistence.
"

## Commands

scripts/dev.sh: launches the whole project


## Coding Conventions

Use the `developer` skill to write code. It applies to all code in this project and delegates to the `backend` and `frontend` skills as appropriate.


## Testing

When testing, respect PubTator's API and minimize the calls you make to them. Ask for approval before running any test that will fetch more than 5 articles at a time.