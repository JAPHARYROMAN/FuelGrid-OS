import base from '@fuelgrid/config/eslint/base';
import next from '@fuelgrid/config/eslint/next';

export default [{ ignores: ['**/public/sw.js'] }, ...base, ...next];
