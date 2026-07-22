CREATE INDEX platform_jobs_running_locked_at_id_idx
    ON platform_jobs (locked_at, id)
    WHERE state = 'running';
