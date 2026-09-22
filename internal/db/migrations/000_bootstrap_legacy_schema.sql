-- Sanitized bootstrap for new Go deployments.
--
-- This is the Laravel application's durable domain schema as it stood at the
-- cutover, expressed as PostgreSQL DDL. It deliberately creates tables only:
-- it does not insert users, receivers, API keys, application settings, or any
-- other operational data. Every statement is idempotent so applying this to
-- an existing Laravel database is a no-op before the numbered Go migrations
-- make their additive changes.
--
-- PostgreSQL/PostGIS are required by the application. pgcrypto provides
-- gen_random_uuid(), which the Go registration flows use.
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL UNIQUE,
    name VARCHAR(255),
    email VARCHAR(255) NOT NULL UNIQUE,
    email_verified_at TIMESTAMP NULL,
    password VARCHAR(255) NOT NULL,
    remember_token VARCHAR(100) NULL,
    current_team_id BIGINT NULL,
    profile_photo_path TEXT NULL,
    subscription_product_id VARCHAR(255) NULL,
    subscription_valid BOOLEAN NOT NULL DEFAULT false,
    subscription_expiry TIMESTAMP NULL,
    superuser BOOLEAN NOT NULL DEFAULT false,
    capcodes JSON NULL,
    cancellation_reason VARCHAR(255) NULL,
    avatar VARCHAR(255) NULL,
    adapty_id VARCHAR(255) NULL,
    last_adapty_sync_at TIMESTAMP NULL,
    will_renew BOOLEAN NULL DEFAULT false,
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);

CREATE TABLE IF NOT EXISTS password_resets (
    email VARCHAR(255) NOT NULL,
    token VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NULL
);
CREATE INDEX IF NOT EXISTS password_resets_email_index ON password_resets (email);

CREATE TABLE IF NOT EXISTS failed_jobs (
    id BIGSERIAL PRIMARY KEY,
    uuid VARCHAR(255) NOT NULL UNIQUE,
    connection TEXT NOT NULL,
    queue TEXT NOT NULL,
    payload TEXT NOT NULL,
    exception TEXT NOT NULL,
    failed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS jobs (
    id BIGSERIAL PRIMARY KEY,
    queue VARCHAR(255) NOT NULL,
    payload TEXT NOT NULL,
    attempts SMALLINT NOT NULL,
    reserved_at INTEGER NULL,
    available_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS jobs_queue_index ON jobs (queue);

CREATE TABLE IF NOT EXISTS job_batches (
    id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    total_jobs INTEGER NOT NULL,
    pending_jobs INTEGER NOT NULL,
    failed_jobs INTEGER NOT NULL,
    failed_job_ids TEXT NOT NULL,
    options TEXT NULL,
    cancelled_at INTEGER NULL,
    created_at INTEGER NOT NULL,
    finished_at INTEGER NULL
);

CREATE TABLE IF NOT EXISTS social_providers (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider VARCHAR(255) NOT NULL,
    provider_id VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);
CREATE INDEX IF NOT EXISTS social_providers_provider_index ON social_providers (provider);
CREATE INDEX IF NOT EXISTS social_providers_provider_id_index ON social_providers (provider_id);

CREATE TABLE IF NOT EXISTS personal_access_tokens (
    id BIGSERIAL PRIMARY KEY,
    tokenable_type VARCHAR(255) NOT NULL,
    tokenable_id BIGINT NOT NULL,
    name VARCHAR(255) NOT NULL,
    token VARCHAR(64) NOT NULL UNIQUE,
    abilities TEXT NULL,
    last_used_at TIMESTAMP NULL,
    expires_at TIMESTAMP NULL,
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);
CREATE INDEX IF NOT EXISTS personal_access_tokens_tokenable_type_tokenable_id_index
    ON personal_access_tokens (tokenable_type, tokenable_id);

CREATE TABLE IF NOT EXISTS systems (
    id BIGSERIAL PRIMARY KEY,
    system_id UUID NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id),
    name VARCHAR(255) NOT NULL,
    lat DOUBLE PRECISION NULL,
    lng DOUBLE PRECISION NULL,
    sync BOOLEAN NOT NULL DEFAULT false,
    frequency INTEGER NOT NULL DEFAULT 1,
    base_url VARCHAR(255) NULL,
    steal_capcodes VARCHAR(255) NOT NULL DEFAULT '0',
    last_sync_at TIMESTAMP NULL,
    deleted_at TIMESTAMP NULL,
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);
CREATE INDEX IF NOT EXISTS systems_user_id_index ON systems (user_id);

CREATE TABLE IF NOT EXISTS capcodes (
    id BIGSERIAL PRIMARY KEY,
    address VARCHAR(255) NOT NULL,
    alias VARCHAR(255) NOT NULL,
    agency VARCHAR(255) NOT NULL,
    icon VARCHAR(255) NOT NULL,
    colour VARCHAR(255) NOT NULL,
    "ignore" BOOLEAN NOT NULL,
    deleted_at TIMESTAMP NULL,
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);
CREATE INDEX IF NOT EXISTS capcodes_agency_index ON capcodes (agency);

CREATE TABLE IF NOT EXISTS messages (
    id BIGSERIAL PRIMARY KEY,
    system_id BIGINT NOT NULL REFERENCES systems(id),
    capcode_id BIGINT NULL REFERENCES capcodes(id),
    address VARCHAR(255) NOT NULL,
    message VARCHAR(255) NOT NULL,
    md5 VARCHAR(255) NOT NULL,
    deleted_at TIMESTAMP NULL,
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);
CREATE INDEX IF NOT EXISTS messages_system_id_index ON messages (system_id);
CREATE INDEX IF NOT EXISTS messages_capcode_id_index ON messages (capcode_id);
CREATE INDEX IF NOT EXISTS messages_md5_index ON messages (md5);
CREATE INDEX IF NOT EXISTS messages_deleted_at_index ON messages (deleted_at);
CREATE INDEX IF NOT EXISTS messages_system_id_deleted_at_index ON messages (system_id, deleted_at);
CREATE INDEX IF NOT EXISTS messages_system_id_md5_deleted_at_index ON messages (system_id, md5, deleted_at);
CREATE INDEX IF NOT EXISTS messages_capcode_id_deleted_at_index ON messages (capcode_id, deleted_at);
CREATE INDEX IF NOT EXISTS messages_capcode_id_deleted_at_created_at_index ON messages (capcode_id, deleted_at, created_at);

CREATE TABLE IF NOT EXISTS sessions (
    id VARCHAR(255) PRIMARY KEY,
    user_id BIGINT NULL REFERENCES users(id),
    ip_address VARCHAR(45) NULL,
    user_agent TEXT NULL,
    payload TEXT NOT NULL,
    last_activity INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_user_id_index ON sessions (user_id);
CREATE INDEX IF NOT EXISTS sessions_last_activity_index ON sessions (last_activity);

CREATE TABLE IF NOT EXISTS installations (
    id UUID PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    push_type VARCHAR(255) NOT NULL,
    push_token VARCHAR(255) NULL,
    expo_token VARCHAR(255) NULL,
    vendor VARCHAR(255) NOT NULL,
    model VARCHAR(255) NOT NULL,
    version VARCHAR(255) NOT NULL,
    app_id VARCHAR(255) NOT NULL,
    app_name VARCHAR(255) NOT NULL,
    app_version VARCHAR(255) NOT NULL,
    timezone VARCHAR(255) NOT NULL,
    deleted_at TIMESTAMP NULL,
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);
CREATE INDEX IF NOT EXISTS installations_user_id_index ON installations (user_id);

CREATE TABLE IF NOT EXISTS notifications (
    id UUID PRIMARY KEY,
    type VARCHAR(255) NOT NULL,
    notifiable_type VARCHAR(255) NOT NULL,
    notifiable_id BIGINT NOT NULL,
    data TEXT NOT NULL,
    read_at TIMESTAMP NULL,
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);
CREATE INDEX IF NOT EXISTS notifications_notifiable_type_notifiable_id_read_at_index
    ON notifications (notifiable_type, notifiable_id, read_at);

CREATE TABLE IF NOT EXISTS subscriptions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    capcode_id BIGINT NOT NULL REFERENCES capcodes(id),
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);
CREATE INDEX IF NOT EXISTS subscriptions_user_id_capcode_id_index ON subscriptions (user_id, capcode_id);

CREATE TABLE IF NOT EXISTS filters (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    capcode_id BIGINT NOT NULL REFERENCES capcodes(id),
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);
CREATE INDEX IF NOT EXISTS filters_user_id_capcode_id_index ON filters (user_id, capcode_id);

CREATE TABLE IF NOT EXISTS alertzones (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    name VARCHAR(255) NOT NULL,
    lat DOUBLE PRECISION NOT NULL,
    lng DOUBLE PRECISION NOT NULL,
    radius INTEGER NOT NULL DEFAULT 5,
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);
CREATE INDEX IF NOT EXISTS alertzones_user_id_index ON alertzones (user_id);

CREATE TABLE IF NOT EXISTS hazardreductions (
    id BIGSERIAL PRIMARY KEY,
    title VARCHAR(255) NULL,
    upstream_id VARCHAR(255) NULL,
    "leadAgency" VARCHAR(255) NULL,
    "supportingAgencies" VARCHAR(255) NULL,
    lga VARCHAR(255) NULL,
    size REAL NULL,
    tenure VARCHAR(255) NULL,
    geometry JSON NULL,
    start_at TIMESTAMP NULL,
    end_at TIMESTAMP NULL,
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);

CREATE TABLE IF NOT EXISTS majorincidents (
    id BIGSERIAL PRIMARY KEY,
    title VARCHAR(255) NULL,
    upstream_id VARCHAR(255) NULL,
    category VARCHAR(255) NULL,
    geometry JSON NULL,
    data JSON NULL,
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);

CREATE TABLE IF NOT EXISTS responding (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    message_id BIGINT NOT NULL REFERENCES messages(id),
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);

CREATE TABLE IF NOT EXISTS photos (
    id BIGSERIAL PRIMARY KEY,
    capcode_id BIGINT NULL REFERENCES capcodes(id),
    message_id BIGINT NULL REFERENCES messages(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    photo_id UUID NOT NULL,
    thumb VARCHAR(255) NULL,
    url VARCHAR(255) NULL,
    upload_complete BOOLEAN NOT NULL DEFAULT false,
    labels JSON NULL,
    safe_search JSON NULL,
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);

CREATE TABLE IF NOT EXISTS system_checkins (
    id BIGSERIAL PRIMARY KEY,
    system_id BIGINT NOT NULL REFERENCES systems(id),
    ip_address VARCHAR(255) NOT NULL,
    hostname VARCHAR(255) NULL,
    version VARCHAR(255) NULL,
    os VARCHAR(255) NULL,
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);

CREATE TABLE IF NOT EXISTS websockets_statistics_entries (
    id SERIAL PRIMARY KEY,
    app_id VARCHAR(255) NOT NULL,
    peak_connection_count INTEGER NOT NULL,
    websocket_message_count INTEGER NOT NULL,
    api_message_count INTEGER NOT NULL,
    created_at TIMESTAMP NULL,
    updated_at TIMESTAMP NULL
);
