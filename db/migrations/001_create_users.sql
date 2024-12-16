CREATE TABLE users
(
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    github_id         INTEGER UNIQUE NOT NULL,
    github_username   TEXT           NOT NULL,
    github_avatar_url TEXT,
    github_email      TEXT,
    created_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_login        TIMESTAMP
);

CREATE TABLE tokens
(
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER     NOT NULL,
    token_hash  TEXT UNIQUE NOT NULL,
    description TEXT,
    last_used   TIMESTAMP,
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at  TIMESTAMP   NOT NULL,
    revoked_at  TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users (id)
);


CREATE INDEX idx_tokens_hash ON tokens (token_hash);
CREATE INDEX idx_tokens_user ON tokens (user_id);