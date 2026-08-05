drop table if exists users cascade;

CREATE TABLE `users` (
    `id` int(10) unsigned NOT NULL AUTO_INCREMENT,
    `first_name` varchar(255) CHARACTER SET utf8 COLLATE utf8_unicode_ci NOT NULL,
    `last_name` varchar(255) CHARACTER SET utf8 COLLATE utf8_unicode_ci NOT NULL,
    `user_active` int(11) NOT NULL,
    `email` varchar(255) CHARACTER SET utf8 COLLATE utf8_unicode_ci NOT NULL,
    `password` char(60) CHARACTER SET utf8 COLLATE utf8_unicode_ci NOT NULL,
    `totp_secret` varchar(255) DEFAULT '',
    `totp_enabled` tinyint(1) NOT NULL DEFAULT 0,
    -- The last TOTP time step this account authenticated with. RFC 6238 §5.2
    -- requires that a code be accepted only once, and this is what remembers.
    `totp_last_step` bigint NOT NULL DEFAULT 0,
    `created_at` timestamp NULL DEFAULT NULL,
    `updated_at` timestamp NULL DEFAULT NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `users_email_unique` (`email`),
    KEY `users_email_index` (`email`)
) ENGINE=InnoDB AUTO_INCREMENT=17 DEFAULT CHARSET=utf8mb4;

-- No remember_tokens table.
--
-- "Remember me" now uses the framework's single-use token store
-- (tjo_reset_tokens), which the auth package creates and which keeps only a
-- hash. The table this replaces held the cookie's value verbatim and had no
-- expiry column, so reading it was a working login for every user who had
-- ticked the box.
drop table if exists remember_tokens cascade;

drop table if exists tokens cascade;

CREATE TABLE `tokens` (
    `id` int(11) NOT NULL AUTO_INCREMENT,
    `user_id` int(11) unsigned NOT NULL,
    `name` varchar(255) NOT NULL,
    `email` varchar(255) NOT NULL,
    `token_hash` varbinary(255) NOT NULL,
    `created_at` datetime NOT NULL DEFAULT current_timestamp(),
    `updated_at` datetime NOT NULL DEFAULT current_timestamp(),
    `expiry` datetime NOT NULL,
    PRIMARY KEY (`id`),
    FOREIGN KEY (user_id) REFERENCES users(id) ON UPDATE cascade ON DELETE cascade
) ENGINE=InnoDB AUTO_INCREMENT=30 DEFAULT CHARSET=utf8mb4;