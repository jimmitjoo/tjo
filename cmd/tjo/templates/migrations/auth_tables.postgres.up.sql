CREATE OR REPLACE FUNCTION trigger_set_timestamp()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = NOW();
RETURN NEW;
END;
$$ LANGUAGE plpgsql;

drop table if exists users cascade;

CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    first_name character varying(255) NOT NULL,
    last_name character varying(255) NOT NULL,
    user_active integer NOT NULL DEFAULT 0,
    email character varying(255) NOT NULL UNIQUE,
    password character varying(60) NOT NULL,
    totp_secret character varying(255) DEFAULT '',
    totp_enabled boolean NOT NULL DEFAULT false,
    -- The last TOTP time step this account authenticated with. RFC 6238 §5.2
    -- requires that a code be accepted only once, and this is what remembers.
    totp_last_step bigint NOT NULL DEFAULT 0,
    created_at timestamp without time zone NOT NULL DEFAULT now(),
    updated_at timestamp without time zone NOT NULL DEFAULT now()
);

CREATE TRIGGER set_timestamp
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE PROCEDURE trigger_set_timestamp();

-- No remember_tokens table.
--
-- "Remember me" now uses the framework's single-use token store
-- (tjo_reset_tokens), which the auth package creates and which keeps only a
-- hash. The table this replaces held the cookie's value verbatim and had no
-- expiry column, so reading it was a working login for every user who had
-- ticked the box.
drop table if exists remember_tokens;

drop table if exists tokens;

CREATE TABLE tokens (
    id SERIAL PRIMARY KEY,
    user_id integer NOT NULL REFERENCES users(id) ON DELETE CASCADE ON UPDATE CASCADE,
    first_name character varying(255) NOT NULL,
    email character varying(255) NOT NULL,
    token_hash bytea NOT NULL,
    created_at timestamp without time zone NOT NULL DEFAULT now(),
    updated_at timestamp without time zone NOT NULL DEFAULT now(),
    expiry timestamp without time zone NOT NULL
);

CREATE TRIGGER set_timestamp
    BEFORE UPDATE ON tokens
    FOR EACH ROW
    EXECUTE PROCEDURE trigger_set_timestamp();