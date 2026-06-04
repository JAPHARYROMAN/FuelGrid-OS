-- Reverse of 0094_expense_category_threshold.

ALTER TABLE expense_categories
    DROP CONSTRAINT IF EXISTS chk_expense_category_threshold_nonneg;

ALTER TABLE expense_categories
    DROP COLUMN IF EXISTS approval_threshold;
