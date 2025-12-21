-- Blog posts table
CREATE TABLE IF NOT EXISTS posts (
    `id` INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `title` varchar(255) NOT NULL,
    `slug` varchar(255) NOT NULL UNIQUE,
    `content` TEXT DEFAULT NULL,
    `published` tinyint(1) NOT NULL DEFAULT 0,
    `created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    INDEX `idx_posts_slug` (`slug`),
    INDEX `idx_posts_published` (`published`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
