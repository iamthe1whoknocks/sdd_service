-- Revert unique constraint on documents.title

DROP INDEX IF EXISTS idx_documents_title_unique;
