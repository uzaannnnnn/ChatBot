-- 000001 rollback. DESTRUCTIVE: removes all conversations and messages.
-- Dependency order: messages (child) before conversations (parent).
-- Indexes and constraints are dropped together with their tables.
-- The chatbot schema itself is intentionally retained because it hosts the
-- golang-migrate tracking table chatbot.schema_migrations.

DROP TABLE IF EXISTS chatbot.messages;
DROP TABLE IF EXISTS chatbot.conversations;
