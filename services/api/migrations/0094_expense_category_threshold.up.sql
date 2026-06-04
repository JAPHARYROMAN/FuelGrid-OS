-- 0094_expense_category_threshold: add an approval threshold to expense
-- categories (Feature 8.1).
--
-- An expense category already carries its accounting mapping (account_key,
-- migration 0047). This adds an optional approval_threshold: the money amount at
-- or above which an expense in this category warrants approval scrutiny. It is
-- advisory metadata surfaced in the categories management UI; NULL means "no
-- threshold set". numeric(14,2) matches the expenses money precision — never a
-- float.

ALTER TABLE expense_categories
    ADD COLUMN approval_threshold numeric(14, 2);

ALTER TABLE expense_categories
    ADD CONSTRAINT chk_expense_category_threshold_nonneg
        CHECK (approval_threshold IS NULL OR approval_threshold >= 0);
