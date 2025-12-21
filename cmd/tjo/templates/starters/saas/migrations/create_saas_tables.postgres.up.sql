-- Teams table
CREATE TABLE IF NOT EXISTS teams (
    id SERIAL PRIMARY KEY,
    name character varying(255) NOT NULL,
    slug character varying(255) NOT NULL UNIQUE,
    owner_id INT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Team members table
CREATE TABLE IF NOT EXISTS team_members (
    id SERIAL PRIMARY KEY,
    team_id INT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id INT NOT NULL,
    role character varying(50) NOT NULL DEFAULT 'member',
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Subscriptions table
CREATE TABLE IF NOT EXISTS subscriptions (
    id SERIAL PRIMARY KEY,
    team_id INT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    plan_id character varying(50) NOT NULL DEFAULT 'free',
    status character varying(50) NOT NULL DEFAULT 'active',
    stripe_customer_id character varying(255) DEFAULT '',
    stripe_subscription_id character varying(255) DEFAULT '',
    current_period_end TIMESTAMP,
    canceled_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_teams_slug ON teams(slug);
CREATE INDEX IF NOT EXISTS idx_teams_owner ON teams(owner_id);
CREATE INDEX IF NOT EXISTS idx_team_members_team ON team_members(team_id);
CREATE INDEX IF NOT EXISTS idx_team_members_user ON team_members(user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_team_members_unique ON team_members(team_id, user_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_team ON subscriptions(team_id);
CREATE INDEX IF NOT EXISTS idx_subscriptions_status ON subscriptions(status);
