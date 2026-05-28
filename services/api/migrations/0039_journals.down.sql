-- Reverse of 0039_journals.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('journal.read', 'journal.adjust'));
DELETE FROM permissions WHERE code IN ('journal.read', 'journal.adjust');

DROP TABLE IF EXISTS journal_lines;
DROP TABLE IF EXISTS journal_entries;
