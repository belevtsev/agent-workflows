-- About 40 million rows in production.
CREATE TABLE entries (
    id         BIGSERIAL PRIMARY KEY,
    account_id BIGINT           NOT NULL,
    amount     DOUBLE PRECISION NOT NULL,
    currency   CHAR(3)          NOT NULL,
    posted_at  TIMESTAMPTZ      NOT NULL
);

CREATE INDEX entries_account ON entries (account_id);
