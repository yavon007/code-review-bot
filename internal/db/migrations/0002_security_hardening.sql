-- +goose Up
alter table webhook_deliveries alter column repo_full_name drop not null;
alter table admin_users add column if not exists session_version int not null default 0;

-- +goose Down
alter table admin_users drop column if exists session_version;
