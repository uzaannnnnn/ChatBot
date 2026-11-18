ALTER TABLE chatbot.conversations
    ADD COLUMN IF NOT EXISTS owner_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';

ALTER TABLE chatbot.conversations
    ALTER COLUMN owner_id DROP DEFAULT;

CREATE INDEX IF NOT EXISTS idx_conversations_owner_updated_at
    ON chatbot.conversations (owner_id, updated_at DESC, id DESC);
