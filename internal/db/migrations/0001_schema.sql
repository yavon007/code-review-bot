-- +goose Up
create table if not exists webhook_deliveries (
  id bigserial primary key,
  delivery_id text,
  event_name text not null,
  action text,
  repo_full_name text not null,
  pr_number int,
  head_sha text,
  sender text,
  signature_valid boolean not null,
  status text not null default 'received',
  error_message text,
  job_id bigint,
  received_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table if not exists review_jobs (
  id bigserial primary key,
  delivery_id text unique,
  event_name text not null,
  action text,
  repo_full_name text not null,
  owner_name text not null,
  repo_name text not null,
  pr_number int not null,
  head_sha text not null,
  base_sha text,
  sender text,
  status text not null,
  status_reason text,
  model text,
  attempt_count int not null default 0,
  error_message text,
  summary text,
  gitea_comment_id text,
  input_tokens int,
  output_tokens int,
  estimated_cost numeric,
  created_at timestamptz not null default now(),
  queued_at timestamptz not null default now(),
  started_at timestamptz,
  heartbeat_at timestamptz,
  worker_id text,
  finished_at timestamptz,
  stale_at timestamptz,
  status_sync_pending boolean not null default false,
  status_sync_error text,
  status_sync_attempt_count int not null default 0,
  next_status_sync_at timestamptz,
  status_sync_worker_id text,
  status_sync_started_at timestamptz,
  status_synced_at timestamptz,
  unique(repo_full_name, pr_number, head_sha)
);

create table if not exists review_findings (
  id bigserial primary key,
  job_id bigint not null,
  finding_hash text not null unique,
  path text not null,
  side text not null,
  line int,
  severity text not null,
  category text not null,
  title text not null,
  body text not null,
  confidence numeric,
  is_inline boolean not null default false,
  is_posted boolean not null default false,
  gitea_comment_id text,
  gitea_comment_url text,
  post_error text,
  created_at timestamptz not null default now()
);

create table if not exists admin_users (
  id bigserial primary key,
  username text not null unique,
  password_hash text not null,
  created_at timestamptz not null default now()
);

create table if not exists app_settings (
  key text primary key,
  value text not null,
  updated_at timestamptz not null default now()
);

create index if not exists review_jobs_status_id_idx on review_jobs(status, id);
create index if not exists review_jobs_repo_pr_idx on review_jobs(repo_full_name, pr_number, id desc);
create index if not exists webhook_deliveries_received_at_idx on webhook_deliveries(received_at desc);
create index if not exists webhook_deliveries_delivery_id_idx on webhook_deliveries(delivery_id);
create index if not exists review_jobs_running_heartbeat_idx on review_jobs(status, heartbeat_at);
create index if not exists review_jobs_status_sync_idx on review_jobs(status_sync_pending, next_status_sync_at, id);

-- +goose Down
drop index if exists review_jobs_status_sync_idx;
drop index if exists review_jobs_running_heartbeat_idx;
drop index if exists webhook_deliveries_delivery_id_idx;
drop index if exists webhook_deliveries_received_at_idx;
drop index if exists review_jobs_repo_pr_idx;
drop index if exists review_jobs_status_id_idx;
drop table if exists app_settings;
drop table if exists admin_users;
drop table if exists review_findings;
drop table if exists review_jobs;
drop table if exists webhook_deliveries;
