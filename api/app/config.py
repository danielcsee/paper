from functools import lru_cache
from pathlib import Path

from pydantic_settings import BaseSettings, SettingsConfigDict

# api/app/config.py -> api/app -> api -> repo root
REPO_ROOT = Path(__file__).resolve().parents[2]


class Settings(BaseSettings):
    """Runtime configuration, overridable by environment or a root .env file."""

    model_config = SettingsConfigDict(
        env_file=REPO_ROOT / ".env", env_file_encoding="utf-8", extra="ignore"
    )

    # Where the compiled React bundle lives. The Docker image overrides this.
    litgraph_ui_dist: Path = REPO_ROOT / "ui" / "dist"

    # Datastore connections. Nothing connects to these yet; they are here so the
    # image and the host process read their addresses from one place.
    database_url: str = "postgresql://litgraph:litgraph@localhost:5432/litgraph"
    neo4j_uri: str = "bolt://localhost:7687"


@lru_cache
def get_settings() -> Settings:
    return Settings()
