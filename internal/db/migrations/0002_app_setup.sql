-- +goose Up
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

-- +goose Down
drop table if exists app_settings;
drop table if exists admin_users;
