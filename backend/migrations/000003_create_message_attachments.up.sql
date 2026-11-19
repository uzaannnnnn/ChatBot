-- 000003: message attachments metadata for generated images and multimodal content.
-- Binary image bytes are stored in private Supabase Storage; PostgreSQL stores metadata only.

CREATE TABLE chatbot.message_attachments (
    id           UUID         NOT NULL DEFAULT gen_random_uuid(),
    message_id   UUID         NOT NULL,
    kind         VARCHAR(32)  NOT NULL,
    storage_path TEXT         NOT NULL,
    mime_type    VARCHAR(100) NOT NULL,
    width        INTEGER      NOT NULL,
    height       INTEGER      NOT NULL,
    prompt       TEXT         NOT NULL DEFAULT '',
    alt_text     TEXT         NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT message_attachments_pkey PRIMARY KEY (id),
    CONSTRAINT message_attachments_message_id_fkey FOREIGN KEY (message_id)
        REFERENCES chatbot.messages (id) ON DELETE CASCADE,
    CONSTRAINT message_attachments_kind_chk CHECK (kind IN ('image')),
    CONSTRAINT message_attachments_storage_path_not_blank_chk CHECK (storage_path ~ '[^[:space:]]'),
    CONSTRAINT message_attachments_mime_type_chk CHECK (mime_type IN ('image/jpeg', 'image/png', 'image/webp')),
    CONSTRAINT message_attachments_dimensions_chk CHECK (width > 0 AND height > 0)
);

COMMENT ON TABLE chatbot.message_attachments IS 'Attachment metadata (e.g. generated images) linked to chatbot.messages.';

CREATE INDEX message_attachments_message_id_created_at_id_idx
    ON chatbot.message_attachments (message_id, created_at, id);

DO $$
DECLARE
    api_role TEXT;
BEGIN
    FOREACH api_role IN ARRAY ARRAY['anon', 'authenticated'] LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = api_role) THEN
            EXECUTE format('REVOKE ALL ON TABLE chatbot.message_attachments FROM %I', api_role);
        END IF;
    END LOOP;
END
$$;
