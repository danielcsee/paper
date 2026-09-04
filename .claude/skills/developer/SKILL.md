---
name: developer
description: Use whenever writing, modifying, or refactoring code in this project - backend or frontend. Covers directory layout, the per-directory README requirement, and the feature-branch git workflow.
---

Identify the language you're using and the area you're working on (backend, frontend, or both), and use the skills which best correspond to that.

When writing backend code (Python/Celery, PostgreSQL, or Neo4j), use the `backend` skill.

When writing frontend code (React/Typescript), use the `frontend` skill.

## Coding Conventions

The following conventions apply to all types of coding.

Put major features into their own directories, and export a public interface when necessary.

Before doing work on a file, check the directory for a README and read that first.

Whenever you add a new directory, add a README to that directory summarizing what the feature does, what dependencies it relies on, and what its subdirectories (if any) are for. Keep the README under 250 words and update it whenever you make changes to its directory.

## Git

Before doing any work, check out a new feature branch from 'dev'. Do all your work on that branch, and merge your branch back into 'dev' when you're done.