-- 0112_export_jobs_async: make the export-jobs surface genuinely asynchronous
-- (Reports Center Phase 13 — Export Center).
--
-- 0091 created export_jobs as a durable RECEIPT: the file was produced
-- synchronously and the row only recorded the {report_key, format, filters} and
-- the resulting same-origin URL. This migration turns that receipt into a real
-- async work queue: a row is now ENQUEUED in 'queued' status, an advisory-locked
-- worker picks it up ('running'), re-checks the requesting actor's permission at
-- generation time, re-runs the report, renders the file, and stores the rendered
-- BYTES durably IN THIS TABLE (no external blob store — everything stays on
-- Postgres). The download endpoint streams those stored bytes, permission-checked.
--
-- NO-NEW-INFRA: the rendered file lives in result_bytes (bytea) right here, so a
-- DO Spaces / S3 upload is never required. A large-object store can be gated on
-- later behind config without a schema change (the bytes column simply goes NULL
-- and a result_url is used instead), but the default is durable Postgres storage.

ALTER TABLE export_jobs
    -- The rendered file, stored inline. NULL until the worker completes the job.
    ADD COLUMN result_bytes        bytea,
    -- The MIME type of result_bytes, so the download endpoint sets Content-Type
    -- without re-deriving it from the format.
    ADD COLUMN result_content_type text,
    -- The friendly download filename the worker computed at render time.
    ADD COLUMN result_filename      text,
    -- Byte length of result_bytes, surfaced in the status view without loading
    -- the (potentially large) bytea. Kept in sync by the worker on completion.
    ADD COLUMN result_size          bigint,
    -- A sha256 hex checksum of result_bytes, mirroring the synchronous file
    -- handlers' audited checksum so an async export is just as provably recorded.
    ADD COLUMN result_checksum      text,
    -- When the worker started this job (claimed it out of 'queued').
    ADD COLUMN started_at           timestamptz,
    -- When the worker reached a terminal status (completed or failed).
    ADD COLUMN completed_at         timestamptz,
    -- How many times a worker has claimed this job. Bumped on each claim; the
    -- worker's stale-running reclaim re-queues an abandoned job until attempts
    -- reaches maxExportAttempts, then fails it permanently — so a poison job that
    -- keeps crashing the worker is capped and never retried forever.
    ADD COLUMN attempts             integer NOT NULL DEFAULT 0;

-- The worker claim query orders queued jobs oldest-first and skips locked rows;
-- a partial index on the queued set keeps that scan cheap as history accumulates.
CREATE INDEX idx_export_jobs_queued
    ON export_jobs (created_at)
    WHERE status = 'queued';

-- Existing rows were written by the synchronous path as already-'completed'
-- receipts (with a file_url, no stored bytes). Stamp completed_at so the history
-- view renders them consistently; they have no result_bytes and the download
-- endpoint falls back to their recorded file_url for back-compat.
UPDATE export_jobs SET completed_at = created_at WHERE status = 'completed';
