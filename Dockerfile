# ---- stage 1: compile the React bundle ----
FROM node:22-alpine AS ui-build
WORKDIR /ui
COPY ui/package.json ui/package-lock.json* ./
RUN npm ci
COPY ui/ ./
RUN npm run build

# ---- stage 2: FastAPI serving that bundle ----
FROM python:3.12-slim
WORKDIR /app
ENV PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1

COPY api/requirements.txt ./api/requirements.txt
RUN pip install --no-cache-dir -r api/requirements.txt

COPY api/ ./api/
# Migrations run from this image, as a one-off task before the services start.
# alembic.ini lives at the repository root and names api/db/migrations as its
# script_location, so without it `alembic upgrade head` has no config to read
# and every deploy would be stuck on an unmigrated database.
COPY alembic.ini ./alembic.ini
COPY --from=ui-build /ui/dist ./ui/dist
ENV SCITERM_UI_DIST=/app/ui/dist

# Stamped at build time and echoed by /health, so a deploy can be confirmed
# from outside without shelling into anything.
ARG SCITERM_VERSION=unknown
ENV SCITERM_VERSION=$SCITERM_VERSION

EXPOSE 8000
CMD ["uvicorn", "api.app.main:app", "--host", "0.0.0.0", "--port", "8000"]
