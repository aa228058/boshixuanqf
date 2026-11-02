-- +goose Up
-- 064: text_dict_entries 唯一约束(2026-10-22, 审计 P0-13)
-- 原来缺 UNIQUE(dict_id, word)——ON CONFLICT DO NOTHING 失效, 重复词条刷权重失衡。
-- 先清存量重复(保留最早一条), 再加约束。
DELETE FROM text_dict_entries a USING text_dict_entries b
WHERE a.entry_id > b.entry_id AND a.dict_id = b.dict_id AND a.word = b.word;

ALTER TABLE text_dict_entries
    ADD CONSTRAINT text_dict_entries_dict_word_unique UNIQUE (dict_id, word);

-- +goose Down
ALTER TABLE text_dict_entries DROP CONSTRAINT IF EXISTS text_dict_entries_dict_word_unique;
