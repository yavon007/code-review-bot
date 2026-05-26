-- +goose Up
create table if not exists review_job_events (
  id bigserial primary key,
  job_id bigint not null,
  event_type text not null,
  message text,
  created_at timestamptz not null default now()
);

create index if not exists review_job_events_job_id_idx on review_job_events(job_id, id);

-- +goose Down
drop index if exists review_job_events_job_id_idx;
drop table if exists review_job_events;
