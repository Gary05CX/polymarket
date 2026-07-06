CREATE SCHEMA log;

CREATE TABLE log.requests(
    id BIGSERIAL PRIMARY KEY,
    datetime TIMESTAMPTZ DEFAULT now(),
    request_url TEXT NOT NULL,
    status_code SMALLINT NOT NULL CHECK (status_code BETWEEN 100 AND 599),
    header JSONB NULL, 
    response_body JSONB NULL
);