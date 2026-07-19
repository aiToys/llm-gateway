-- 模型展示元数据(模型广场/定价页用)
ALTER TABLE models
    ADD COLUMN IF NOT EXISTS description    text     NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS long_desc      text     NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS tags           text[]   NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS modality       text     NOT NULL DEFAULT 'text',
    ADD COLUMN IF NOT EXISTS context_length int      NOT NULL DEFAULT 0;
