-- 能力从单值 modality 改为多标签 capabilities(参考智谱/OpenAI:一个模型可同时具备视觉/工具/推理/代码等)。
ALTER TABLE models ADD COLUMN IF NOT EXISTS capabilities jsonb NOT NULL DEFAULT '["text"]';
UPDATE models SET capabilities = CASE
  WHEN modality = 'vision'     THEN '["text","vision"]'::jsonb
  WHEN modality = 'audio'      THEN '["audio"]'::jsonb
  WHEN modality = 'multimodal' THEN '["text","vision"]'::jsonb
  ELSE '["text"]'::jsonb
END;
ALTER TABLE models DROP COLUMN IF EXISTS modality;
