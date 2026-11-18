-- 000001: chatbot schema foundation (conversations and messages).
-- The whole file executes as a single implicit transaction.

CREATE SCHEMA IF NOT EXISTS chatbot;

CREATE TABLE chatbot.conversations (
    id         UUID         NOT NULL DEFAULT gen_random_uuid(),
    title      VARCHAR(200) NOT NULL DEFAULT 'New conversation',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT conversations_pkey PRIMARY KEY (id),
    CONSTRAINT conversations_title_not_blank_chk CHECK (title ~ '[^[:space:]]'),
    CONSTRAINT conversations_updated_at_chk CHECK (updated_at >= created_at)
);

COMMENT ON TABLE chatbot.conversations IS 'Chat conversations. updated_at reflects the latest title change or message insert.';

CREATE TABLE chatbot.messages (
    id              UUID        NOT NULL DEFAULT gen_random_uuid(),
    conversation_id UUID        NOT NULL,
    role            VARCHAR(20) NOT NULL,
    content         TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT messages_pkey PRIMARY KEY (id),
    CONSTRAINT messages_conversation_id_fkey FOREIGN KEY (conversation_id)
        REFERENCES chatbot.conversations (id) ON DELETE CASCADE,
    CONSTRAINT messages_role_chk CHECK (role IN ('user', 'assistant')),
    CONSTRAINT messages_content_not_blank_chk CHECK (content ~ '[^[:space:]]')
);

COMMENT ON TABLE chatbot.messages IS 'Messages belonging to a conversation, ordered by created_at ASC, id ASC.';

-- Conversation listing: ORDER BY updated_at DESC, id DESC.
CREATE INDEX conversations_updated_at_id_idx
    ON chatbot.conversations (updated_at DESC, id DESC);

-- Message retrieval per conversation: WHERE conversation_id = $1 ORDER BY created_at, id.
-- Also serves the foreign key for ON DELETE CASCADE lookups.
CREATE INDEX messages_conversation_id_created_at_id_idx
    ON chatbot.messages (conversation_id, created_at, id);

-- Defense in depth for Supabase: the chatbot schema must never be reachable
-- through the Data API roles. Roles are revoked only when they exist so the
-- migration also runs on plain PostgreSQL.
DO $$
DECLARE
    api_role TEXT;
BEGIN
    FOREACH api_role IN ARRAY ARRAY['anon', 'authenticated'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = api_role) THEN
            EXECUTE format('REVOKE ALL ON SCHEMA chatbot FROM %I', api_role);
            EXECUTE format('REVOKE ALL ON ALL TABLES IN SCHEMA chatbot FROM %I', api_role);
        END IF;
    END LOOP;
END
$$;
