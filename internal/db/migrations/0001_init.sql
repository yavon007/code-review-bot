create table if not exists webhook_deliveries (
  id bigserial primary key,
  delivery_id text not null unique,
  event_name text not null,
  repo_full_name text not null,
  pr_number int,
  head_sha text,
  signature_valid boolean not null,
  received_at timestamptz not null default now()
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
  finished_at timestamptz,
  stale_at timestamptz,
  unique(repo_full_name, pr_number, head_sha)
);

create index if not exists review_jobs_status_id_idx on review_jobs(status, id);
create index if not exists review_jobs_repo_pr_idx on review_jobs(repo_full_name, pr_number, id desc);

create table if not exists review_findings (
  id bigserial primary key,
  job_id bigint not null references review_jobs(id),
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
