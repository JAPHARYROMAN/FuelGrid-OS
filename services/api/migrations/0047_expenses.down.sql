-- Reverse of 0047_expenses.

DELETE FROM role_permissions
WHERE permission_id IN (SELECT id FROM permissions WHERE code IN ('expense.manage', 'expense.approve', 'expense.post'));
DELETE FROM permissions WHERE code IN ('expense.manage', 'expense.approve', 'expense.post');

DROP TABLE IF EXISTS expenses;
DROP TABLE IF EXISTS expense_categories;
