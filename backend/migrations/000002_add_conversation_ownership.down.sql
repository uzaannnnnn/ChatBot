DROP INDEX IF EXISTS chatbot.idx_conversations_owner_updated_at;

ALTER TABLE chatbot.conversations
    DROP COLUMN IF EXISTS owner_id;
