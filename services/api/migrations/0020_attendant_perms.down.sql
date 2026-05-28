-- Reverse of 0020_attendant_perms. Only remove reading.edit — attendant's
-- cash.submit grant belongs to 0018 and is reversed there.

DELETE FROM role_permissions
WHERE role_id = (SELECT id FROM roles WHERE code = 'attendant' AND is_system)
  AND permission_id = (SELECT id FROM permissions WHERE code = 'reading.edit');
