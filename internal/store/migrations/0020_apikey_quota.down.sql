ALTER TABLE api_keys
    DROP COLUMN IF EXISTS daily_request_limit,
    DROP COLUMN IF EXISTS monthly_request_limit,
    DROP COLUMN IF EXISTS daily_token_limit,
    DROP COLUMN IF EXISTS monthly_token_limit;
