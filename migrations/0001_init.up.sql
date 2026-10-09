CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE documents (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title      TEXT NOT NULL,
    content    TEXT NOT NULL CHECK (octet_length(content) <= 1048576),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE chunks (
    id          BIGSERIAL PRIMARY KEY,
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    content     TEXT NOT NULL,
    position    INT  NOT NULL
);

CREATE TABLE embeddings (
    chunk_id   BIGINT PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE,
    embedding  vector(768) NOT NULL   -- размерность соответствует модели Ollama (nomic-embed-text / paraphrase-multilingual)
);

CREATE INDEX ON embeddings USING hnsw (embedding vector_cosine_ops);