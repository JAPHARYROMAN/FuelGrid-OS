/**
 * Money/litre formatters for the decimal-string contract, re-exported from
 * @fuelgrid/ui so web pages have a stable local import path. The backend emits
 * money/litre/rate fields as exact decimal strings; format them for display
 * with these helpers instead of Number(x).toFixed(2), which drifts/NaNs.
 */
export {
  formatMoney,
  formatLitres,
  parseDecimal,
  sumMoney,
  type FormatOptions,
} from '@fuelgrid/ui';
