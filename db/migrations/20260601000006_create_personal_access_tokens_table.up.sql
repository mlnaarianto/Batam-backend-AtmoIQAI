CREATE TABLE personal_access_tokens (
    id          BINARY(16)   NOT NULL,
    user_id     BINARY(16)   NOT NULL,
    family_id   BINARY(16)   NOT NULL,
    token_hash  CHAR(64)     NOT NULL,
    type        VARCHAR(50)  NOT NULL DEFAULT 'refresh',
    user_agent  VARCHAR(255) NULL,
    ip_address  VARCHAR(45)  NULL,
    expired_at  DATETIME     NOT NULL,
    revoked_at  DATETIME     NULL,
    created_at  TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    CONSTRAINT pk_personal_access_tokens PRIMARY KEY (id),
    CONSTRAINT uq_pat_token_hash UNIQUE (token_hash),
    INDEX idx_pat_family_id (family_id),
    INDEX idx_pat_expired_at (expired_at),
    CONSTRAINT fk_user_personal_access_tokens FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB
  DEFAULT CHARSET=utf8mb4
  COLLATE=utf8mb4_0900_ai_ci;