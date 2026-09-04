---
name: developer
description: Use whenever writing, modifying, or refactoring code in this project - backend or frontend. Covers directory layout, the per-directory README requirement, and the feature-branch git workflow.
---

Identify the language you're using and the area you're working on (backend, frontend, or both), and use the skills which best correspond to that. When writing backend code (Python/Celery, PostgreSQL, or Neo4j), use the `backend` skill. When writing frontend code (React/Typescript), use the `frontend` skill.

Before doing any work, use git to check out a new feature branch from the dev branch. Do all your work on that branch, then merge your feature branch back into dev. Always merge your feature branches into dev.

Do not use deprecated functions, libraries, or components. If you detect that library code is deprecated, look up its supported replacement and use that instead.

## Coding Conventions

Put major features into their own directories, and export a public interface when necessary.

Before doing work on a file, check the directory for a README and read that first.

Whenever you add a new directory, add a README to that directory summarizing what the feature does, what dependencies it relies on, and what its subdirectories are for. Keep the README under 250 words and update it whenever you make changes to its directory.