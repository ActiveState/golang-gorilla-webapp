CREATE TABLE "user" (
    user_id  CHAR(64)   PRIMARY KEY, -- a SHA256 token for web requests
    name     TEXT       NOT NULL,
    credit   BIGINT     DEFAULT 0 -- credits in cents
);

CREATE TABLE hash_text (
    hash     CHAR(64)   PRIMARY KEY,
    text     TEXT
);
