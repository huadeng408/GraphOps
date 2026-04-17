from __future__ import annotations

import os
from contextlib import asynccontextmanager
from pathlib import Path

from langgraph.checkpoint.memory import MemorySaver


@asynccontextmanager
async def build_checkpointer_context():
    backend = os.getenv("CHECKPOINTER_BACKEND", "sqlite").lower()
    if backend == "memory":
        yield MemorySaver()
        return

    if backend != "sqlite":
        raise ValueError(f"unsupported CHECKPOINTER_BACKEND: {backend}")

    from langgraph.checkpoint.sqlite.aio import AsyncSqliteSaver

    db_path = Path(os.getenv("CHECKPOINTER_SQLITE_PATH", "./data/langgraph.sqlite"))
    db_path.parent.mkdir(parents=True, exist_ok=True)
    async with AsyncSqliteSaver.from_conn_string(str(db_path)) as saver:
        yield saver
