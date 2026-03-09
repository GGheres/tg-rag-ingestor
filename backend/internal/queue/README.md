# Queue

Reserved for Redis-backed async job queue.

Current MVP executes sync jobs inline via API and persists progress/status in `jobs` table.
