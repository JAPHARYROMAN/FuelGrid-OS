// Brings the @testing-library/jest-dom matcher types (toBeInTheDocument,
// toHaveTextContent, …) into vitest's Assertion interface for tsc. The
// runtime registration lives in test/setup.ts; this file is types-only so
// `tsc --noEmit` over src/**/*.tsx sees the augmented matchers.
import '@testing-library/jest-dom/vitest';
