-- Blog posts table
CREATE TABLE IF NOT EXISTS posts (
    id SERIAL PRIMARY KEY,
    title character varying(255) NOT NULL,
    slug character varying(255) NOT NULL UNIQUE,
    content TEXT DEFAULT '',
    published boolean NOT NULL DEFAULT false,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Index for faster slug lookups
CREATE INDEX IF NOT EXISTS idx_posts_slug ON posts(slug);

-- Index for filtering by published status
CREATE INDEX IF NOT EXISTS idx_posts_published ON posts(published);
