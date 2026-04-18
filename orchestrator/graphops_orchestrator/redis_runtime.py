from __future__ import annotations

import os
import socket
from contextlib import asynccontextmanager

from redis.asyncio import Redis


class RunLockError(RuntimeError):
    pass


class RedisRuntimeCoordinator:
    def __init__(self, client: Redis, ttl_seconds: int = 600) -> None:
        self._client = client
        self._owner = socket.gethostname()
        self._ttl_seconds = ttl_seconds

    @classmethod
    async def from_env(cls) -> "RedisRuntimeCoordinator | None":
        redis_url = os.getenv("REDIS_URL", "").strip()
        if redis_url == "":
            return None
        client = Redis.from_url(redis_url, decode_responses=True)
        return cls(client, ttl_seconds=int(os.getenv("RUN_LOCK_TTL_SECONDS", "600")))

    @asynccontextmanager
    async def hold_run_lock(self, incident_id: str):
        key = f"runlock:incident:{incident_id}"
        locked = False
        try:
            locked = bool(await self._client.set(key, self._owner, ex=self._ttl_seconds, nx=True))
            if not locked:
                raise RunLockError(f"incident {incident_id} is already running")
            yield
        finally:
            if locked:
                await self._client.delete(key)

    async def close(self) -> None:
        await self._client.aclose()
