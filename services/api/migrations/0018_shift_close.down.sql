-- Reverse of 0018_shift_close.

DELETE FROM role_permissions
WHERE permission_id = (SELECT id FROM permissions WHERE code = 'cash.submit');
DELETE FROM permissions WHERE code = 'cash.submit';

DROP TABLE IF EXISTS cash_submissions;
DROP TABLE IF EXISTS shift_close_lines;
