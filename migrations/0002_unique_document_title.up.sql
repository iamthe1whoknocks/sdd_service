-- Add unique constraint on documents.title for idempotent vulnist indexing.
-- This prevents duplicate documents on repeated service startups.

CREATE UNIQUE INDEX idx_documents_title_unique ON documents (title);
