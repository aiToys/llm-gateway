ALTER TABLE models
    DROP COLUMN IF EXISTS context_length,
    DROP COLUMN IF EXISTS modality,
    DROP COLUMN IF EXISTS tags,
    DROP COLUMN IF EXISTS long_desc,
    DROP COLUMN IF EXISTS description;
