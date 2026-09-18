-- Create admin_users table for Go admin panel authentication
CREATE TABLE IF NOT EXISTS admin_users (
    id BIGSERIAL PRIMARY KEY,
    email VARCHAR(254) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    role VARCHAR(50) NOT NULL DEFAULT 'admin',
    active BOOLEAN NOT NULL DEFAULT true,
    last_login_at TIMESTAMP NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_admin_users_email ON admin_users(email);

-- No admin is seeded here. The initial admin is created on boot from
-- ADMIN_EMAIL / ADMIN_PASSWORD (see internal/admin). A hardcoded seed with a
-- bcrypt hash committed to the repo would create a superadmin with a publicly
-- known password on every environment, including production.
